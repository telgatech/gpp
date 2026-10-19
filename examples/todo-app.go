//go:build ignore

// This is the standard Go counterpart to todo-app.gpp. It implements the same
// pages, SQLite model, JSON API, OpenAPI document, Swagger UI, and cleanup job.
// To run without changing the repository's go.mod, make a temporary module
// from the repository root:
//
//	tmpdir=$(mktemp -d)
//	tail -n +2 examples/todo-app.go > "$tmpdir/main.go"
//	cd "$tmpdir"
//	go mod init todo-app
//	go get modernc.org/sqlite
//	go run .
//
// Swagger UI is loaded from unpkg.com; the Go++ version serves vendored assets.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

type Todo struct {
	ID          int64  `json:"Id"`
	Title       string `json:"Title"`
	Completed   bool   `json:"Completed"`
	CreatedAt   int64  `json:"CreatedAt"`
	CompletedAt int64  `json:"CompletedAt"`
}

var todoDB *sql.DB
var todoPage *template.Template

func openTodoDB(ctx context.Context) (*sql.DB, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS todos (
			id INTEGER PRIMARY KEY,
			title TEXT NOT NULL,
			completed BOOLEAN NOT NULL DEFAULT FALSE,
			created_at INTEGER NOT NULL DEFAULT 0,
			completed_at INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func todos(ctx context.Context) ([]Todo, error) {
	query := `
		SELECT id, title, completed, created_at, completed_at
		FROM todos
		ORDER BY rowid
	`
	return queryTodos(ctx, query)
}

func completedTodos(ctx context.Context) ([]Todo, error) {
	query := `
		SELECT id, title, completed, created_at, completed_at
		FROM todos
		WHERE completed = TRUE
		ORDER BY rowid
	`
	return queryTodos(ctx, query)
}

func queryTodos(ctx context.Context, query string) ([]Todo, error) {
	rows, err := todoDB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	todos := make([]Todo, 0)
	for rows.Next() {
		var todo Todo
		if err := rows.Scan(
			&todo.ID,
			&todo.Title,
			&todo.Completed,
			&todo.CreatedAt,
			&todo.CompletedAt,
		); err != nil {
			return nil, err
		}
		todos = append(todos, todo)
	}
	return todos, rows.Err()
}

func findTodo(ctx context.Context, id int64) (*Todo, error) {
	var todo Todo
	query := `
		SELECT id, title, completed, created_at, completed_at
		FROM todos
		WHERE id = ?
	`
	err := todoDB.QueryRowContext(ctx, query, id).Scan(
		&todo.ID,
		&todo.Title,
		&todo.Completed,
		&todo.CreatedAt,
		&todo.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &todo, nil
}

func createTodo(ctx context.Context, todo *Todo) error {
	todo.Title = strings.TrimSpace(todo.Title)
	if todo.Title == "" {
		return errors.New("todo title is required")
	}
	query := `
		INSERT INTO todos (id, title, completed, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := todoDB.ExecContext(
		ctx,
		query,
		todo.ID,
		todo.Title,
		todo.Completed,
		todo.CreatedAt,
		todo.CompletedAt,
	)
	return err
}

func markTodoCompleted(ctx context.Context, id, completedAt int64) error {
	if _, err := findTodo(ctx, id); err != nil {
		return err
	}
	query := `
		UPDATE todos
		SET completed = TRUE, completed_at = ?
		WHERE id = ?
	`
	_, err := todoDB.ExecContext(ctx, query, completedAt, id)
	return err
}

func markTodoActive(ctx context.Context, id int64) (*Todo, error) {
	if _, err := findTodo(ctx, id); err != nil {
		return nil, err
	}
	query := `
		UPDATE todos
		SET completed = FALSE, completed_at = 0
		WHERE id = ?
	`
	if _, err := todoDB.ExecContext(ctx, query, id); err != nil {
		return nil, err
	}
	return findTodo(ctx, id)
}

func deleteTodo(ctx context.Context, id int64) error {
	if _, err := findTodo(ctx, id); err != nil {
		return err
	}
	_, err := todoDB.ExecContext(
		ctx,
		`DELETE FROM todos WHERE id = ?`,
		id,
	)
	return err
}

func pruneTodos(ctx context.Context, cutoff int64) (int64, error) {
	query := `
		DELETE FROM todos
		WHERE completed = TRUE
			AND completed_at < ?
			AND completed_at > 0
	`
	result, err := todoDB.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

const todoHomeHTML = `<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Go++ Todo</title>
    <style>
        body {
            max-width: 42rem;
            margin: 3rem auto;
            padding: 0 1rem;
            font: 1rem/1.5 system-ui, sans-serif;
            color: #20242a;
        }

        h1 {
            margin-bottom: .25rem;
        }

        form {
            display: flex;
            gap: .5rem;
            margin: 1.5rem 0;
        }

        input {
            flex: 1;
            padding: .65rem;
            font: inherit;
        }

        button {
            padding: .55rem .8rem;
            font: inherit;
            cursor: pointer;
        }

        li {
            display: flex;
            align-items: center;
            gap: .5rem;
            padding: .65rem 0;
            border-bottom: 1px solid #ddd;
        }

        .actions {
            display: flex;
            gap: .5rem;
            margin-left: auto;
        }

        .actions form {
            margin: 0;
        }

        .done {
            color: #667;
            text-decoration: line-through;
        }

        footer {
            margin-top: 2rem;
            color: #667;
        }
    </style>
</head>
<body>
    <h1>Go++ Todo</h1>
    <p>A server-rendered todo list backed by SQLite.</p>
    <form method="post" action="/todos">
        <input name="title" placeholder="What needs doing?" required>
        <button type="submit">Add todo</button>
    </form>
    <ul>
        {{range .}}
        <li>
            <span class="{{if .Completed}}done{{end}}">{{.Title}}</span>
            <div class="actions">
                {{if .Completed}}
                <form method="post" action="/todos/{{.ID}}/reopen">
                    <button type="submit">Reopen</button>
                </form>
                {{else}}
                <form method="post" action="/todos/{{.ID}}/complete">
                    <button type="submit">Complete</button>
                </form>
                {{end}}
                <form method="post" action="/todos/{{.ID}}/delete">
                    <button type="submit">Remove</button>
                </form>
            </div>
        </li>
        {{else}}
        <li>Your list is empty. Add a todo above.</li>
        {{end}}
    </ul>
    <footer>
        JSON API and interactive docs:
        <a href="/api/swagger">Swagger UI</a>
    </footer>
</body>
</html>`

func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", home)
	mux.HandleFunc("POST /todos", createTodoFromForm)
	mux.HandleFunc("POST /todos/{id}/complete", completeTodoFromForm)
	mux.HandleFunc("POST /todos/{id}/reopen", reopenTodoFromForm)
	mux.HandleFunc("POST /todos/{id}/delete", deleteTodoFromForm)
	mux.HandleFunc("GET /api/todos", listTodos)
	mux.HandleFunc("GET /api/todos/completed", listCompletedTodos)
	mux.HandleFunc("POST /api/todos", createTodoAPI)
	mux.HandleFunc("GET /api/todos/{id}", getTodo)
	mux.HandleFunc("PUT /api/todos/{id}/complete", completeTodo)
	mux.HandleFunc("DELETE /api/todos/{id}/complete", reopenTodo)
	mux.HandleFunc("DELETE /api/todos/{id}", deleteTodoAPI)
	mux.HandleFunc("GET /api/openapi.json", openAPI)
	mux.HandleFunc("GET /api/swagger", swaggerUI)
	return mux
}

func home(w http.ResponseWriter, r *http.Request) {
	items, err := todos(r.Context())
	if err != nil {
		http.Error(w, "could not load todos", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := todoPage.Execute(w, items); err != nil {
		log.Printf("todo-app: render home: %v", err)
	}
}

func createTodoFromForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title != "" {
		now := time.Now()
		todo := &Todo{
			ID:        now.UnixNano(),
			Title:     title,
			CreatedAt: now.Unix(),
		}
		if err := createTodo(r.Context(), todo); err != nil {
			http.Error(w, "could not create todo", http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func completeTodoFromForm(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTodoID(w, r, false)
	if !ok {
		return
	}
	todo, err := findTodo(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Error(w, "could not load todo", http.StatusInternalServerError)
		return
	}
	if !todo.Completed {
		if err := markTodoCompleted(r.Context(), id, time.Now().Unix()); err != nil {
			http.Error(w, "could not complete todo", http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func reopenTodoFromForm(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTodoID(w, r, false)
	if !ok {
		return
	}
	_, err := markTodoActive(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Error(w, "could not reopen todo", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func deleteTodoFromForm(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTodoID(w, r, false)
	if !ok {
		return
	}
	err := deleteTodo(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Error(w, "could not remove todo", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func listTodos(w http.ResponseWriter, r *http.Request) {
	items, err := todos(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not list todos")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"Items": items,
	})
}

func listCompletedTodos(w http.ResponseWriter, r *http.Request) {
	items, err := completedTodos(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not list completed todos")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"Items": items,
	})
}

func createTodoAPI(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	var request struct {
		Title string
	}
	if err := json.NewDecoder(body).Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	title := strings.TrimSpace(request.Title)
	if title == "" {
		writeAPIError(w, http.StatusBadRequest, "todo title is required")
		return
	}
	now := time.Now()
	todo := &Todo{
		ID:        now.UnixNano(),
		Title:     title,
		CreatedAt: now.Unix(),
	}
	if err := createTodo(r.Context(), todo); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not create todo")
		return
	}
	writeJSON(w, http.StatusCreated, todo)
}

func getTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTodoID(w, r, true)
	if !ok {
		return
	}
	todo, err := findTodo(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeAPIError(w, http.StatusNotFound, "todo not found")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not get todo")
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func completeTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTodoID(w, r, true)
	if !ok {
		return
	}
	todo, err := findTodo(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeAPIError(w, http.StatusNotFound, "todo not found")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not get todo")
		return
	}
	if !todo.Completed {
		completedAt := time.Now().Unix()
		if err := markTodoCompleted(r.Context(), id, completedAt); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "could not complete todo")
			return
		}
		todo.Completed = true
		todo.CompletedAt = completedAt
	}
	writeJSON(w, http.StatusOK, todo)
}

func reopenTodo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTodoID(w, r, true)
	if !ok {
		return
	}
	todo, err := markTodoActive(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeAPIError(w, http.StatusNotFound, "todo not found")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not reopen todo")
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func deleteTodoAPI(w http.ResponseWriter, r *http.Request) {
	id, ok := parseTodoID(w, r, true)
	if !ok {
		return
	}
	err := deleteTodo(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeAPIError(w, http.StatusNotFound, "todo not found")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not delete todo")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseTodoID(w http.ResponseWriter, r *http.Request, api bool) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		return id, true
	}
	if api {
		writeAPIError(w, http.StatusBadRequest, "invalid todo ID")
	} else {
		http.Error(w, "invalid todo ID", http.StatusBadRequest)
	}
	return 0, false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status == http.StatusNoContent {
		return
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("todo-app: encode response: %v", err)
	}
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"Error": message,
	})
}

func pruneCompletedTodos(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
			count, err := pruneTodos(ctx, cutoff)
			if err != nil {
				log.Printf("todo-app: prune completed todos: %v", err)
			} else if count > 0 {
				log.Printf("todo-app: pruned %d completed todos", count)
			}
		}
	}
}

func openAPIDocument() map[string]any {
	jsonSchema := func(schema any) map[string]any {
		return map[string]any{
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": schema,
				},
			},
		}
	}
	ref := func(name string) map[string]any {
		return map[string]any{
			"$ref": "#/components/schemas/" + name,
		}
	}
	response := func(description string, schema any) map[string]any {
		result := map[string]any{
			"description": description,
		}
		if schema != nil {
			result["content"] = jsonSchema(schema)["content"]
		}
		return result
	}
	pathID := map[string]any{
		"name":     "id",
		"in":       "path",
		"required": true,
		"schema": map[string]any{
			"type":   "integer",
			"format": "int64",
		},
	}
	operation := func(summary, description string, responses map[string]any) map[string]any {
		return map[string]any{
			"summary":     summary,
			"description": description,
			"tags":        []string{"Todos"},
			"responses":   responses,
		}
	}

	listTodos := operation(
		"List todos",
		"Returns every todo, including its completion state and timestamps.",
		map[string]any{
			"200": response("Todo list", ref("TodoList")),
		},
	)
	createTodo := operation(
		"Create a todo",
		"Creates a todo from a JSON title and returns the stored record.",
		map[string]any{
			"201": response("Todo created", ref("Todo")),
			"400": response("The request body is invalid or the title is empty", ref("APIError")),
		},
	)
	createTodo["requestBody"] = map[string]any{
		"required": true,
		"content":  jsonSchema(ref("CreateTodoRequest"))["content"],
	}

	listCompletedTodos := operation(
		"List completed todos",
		"Returns todos that have been marked complete.",
		map[string]any{
			"200": response("Completed todo list", ref("TodoList")),
		},
	)

	getTodo := operation(
		"Get a todo",
		"Returns one todo by its numeric ID.",
		map[string]any{
			"200": response("Todo found", ref("Todo")),
			"400": response("The ID is not a number", ref("APIError")),
			"404": response("No todo has this ID", ref("APIError")),
		},
	)
	getTodo["parameters"] = []any{pathID}

	deleteTodo := operation(
		"Delete a todo",
		"Permanently removes a todo by its numeric ID.",
		map[string]any{
			"204": response("Todo deleted", nil),
			"400": response("The ID is not a number", ref("APIError")),
			"404": response("No todo has this ID", ref("APIError")),
		},
	)
	deleteTodo["parameters"] = []any{pathID}

	completeTodo := operation(
		"Complete a todo",
		"Marks a todo complete and records when it was completed.",
		map[string]any{
			"200": response("Todo completed", ref("Todo")),
			"400": response("The ID is not a number", ref("APIError")),
			"404": response("No todo has this ID", ref("APIError")),
		},
	)
	completeTodo["parameters"] = []any{pathID}

	reopenTodo := operation(
		"Reopen a todo",
		"Clears a todo's completion state and completion timestamp.",
		map[string]any{
			"200": response("Todo reopened", ref("Todo")),
			"400": response("The ID is not a number", ref("APIError")),
			"404": response("No todo has this ID", ref("APIError")),
		},
	)
	reopenTodo["parameters"] = []any{pathID}

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "TodoApp API",
			"version": "1.0.0",
		},
		"paths": map[string]any{
			"/api/todos": map[string]any{
				"get":  listTodos,
				"post": createTodo,
			},
			"/api/todos/completed": map[string]any{
				"get": listCompletedTodos,
			},
			"/api/todos/{id}": map[string]any{
				"get":    getTodo,
				"delete": deleteTodo,
			},
			"/api/todos/{id}/complete": map[string]any{
				"put":    completeTodo,
				"delete": reopenTodo,
			},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"Todo": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"Id": map[string]any{
							"type":   "integer",
							"format": "int64",
						},
						"Title": map[string]any{
							"type": "string",
						},
						"Completed": map[string]any{
							"type": "boolean",
						},
						"CreatedAt": map[string]any{
							"type":   "integer",
							"format": "int64",
						},
						"CompletedAt": map[string]any{
							"type":   "integer",
							"format": "int64",
						},
					},
				},
				"TodoList": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"Items": map[string]any{
							"type":  "array",
							"items": ref("Todo"),
						},
					},
				},
				"CreateTodoRequest": map[string]any{
					"type":     "object",
					"required": []string{"Title"},
					"properties": map[string]any{
						"Title": map[string]any{
							"type": "string",
						},
					},
				},
				"APIError": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"Error": map[string]any{
							"type": "string",
						},
					},
				},
			},
		},
	}
}

func openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(openAPIDocument()); err != nil {
		log.Printf("todo-app: write OpenAPI document: %v", err)
	}
}

func swaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Todo API</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
        SwaggerUIBundle({
            url: "/api/openapi.json",
            dom_id: "#swagger-ui"
        });
    </script>
</body>
</html>`)
}

func run() error {
	db, err := openTodoDB(context.Background())
	if err != nil {
		return err
	}
	todoDB = db
	defer todoDB.Close()
	todoPage, err = template.New("TodoHome").Parse(todoHomeHTML)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:    "127.0.0.1:8080",
		Handler: routes(),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	jobCtx, cancelJobs := context.WithCancel(ctx)
	jobDone := make(chan struct{})
	go func() {
		defer close(jobDone)
		pruneCompletedTodos(jobCtx)
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("todo-app: stop server: %v", err)
		}
	}()

	log.Println("todo-app: cleanup job scheduled")
	log.Println("todo-app: listening on http://127.0.0.1:8080")
	err = server.ListenAndServe()
	cancelJobs()
	stop()
	<-jobDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Printf("todo app: %v", err)
		os.Exit(1)
	}
}
