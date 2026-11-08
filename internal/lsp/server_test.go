package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/telgatech/gpp/compiler"
)

func TestRunServesInitializeCompletionAndShutdown(t *testing.T) {
	root := t.TempDir()
	source := "package main\n\nfunc main() {\n\tname := \"Ada\"\n\t_ = name.\n}\n"
	path := filepath.Join(root, "main.gpp")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	uri := uriForPath(path)
	rootURI := uriForPath(root)
	var input bytes.Buffer
	writeTestMessage(&input, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"rootUri": rootURI},
	})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
	writeTestMessage(&input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "gpp", "version": 1, "text": source}},
	})
	writeTestMessage(&input, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion",
		"params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionAtOffset(source, strings.Index(source, "name.")+len("name."))},
	})
	writeTestMessage(&input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didChange",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": 2},
			"contentChanges": []any{map[string]any{"text": `package main
func main() { name := "Ada"; _ = name }
`}},
		},
	})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "shutdown", "params": nil})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	messages := readTestMessages(t, output.Bytes())
	if len(messages) < 3 {
		t.Fatalf("expected initialize, diagnostics, completion, and shutdown messages; got %d", len(messages))
	}
	var sawCompletion, sawShutdown, sawDiagnosticVersion1, sawDiagnosticVersion2 bool
	for _, message := range messages {
		if method, _ := message["method"].(string); method == "textDocument/publishDiagnostics" {
			params, _ := message["params"].(map[string]any)
			if params["uri"] == uri {
				version, _ := params["version"].(float64)
				sawDiagnosticVersion1 = sawDiagnosticVersion1 || version == 1
				sawDiagnosticVersion2 = sawDiagnosticVersion2 || version == 2
			}
			continue
		}
		id, _ := message["id"].(float64)
		switch int(id) {
		case 2:
			sawCompletion = true
			payload, ok := message["result"].([]any)
			if !ok {
				t.Fatalf("completion result has unexpected shape: %#v", message["result"])
			}
			found := false
			for _, value := range payload {
				item, _ := value.(map[string]any)
				if item["label"] == "TrimSpace" {
					found = true
				}
			}
			if !found {
				t.Fatalf("completion did not include TrimSpace: %#v", payload)
			}
		case 3:
			sawShutdown = true
			if result, exists := message["result"]; !exists || result != nil {
				t.Fatalf("shutdown response must include result: null, got %#v", message)
			}
		}
	}
	if !sawCompletion || !sawShutdown || !sawDiagnosticVersion1 || !sawDiagnosticVersion2 {
		t.Fatalf("missing responses or versioned diagnostics: completion=%v shutdown=%v diagnostic versions 1=%v 2=%v", sawCompletion, sawShutdown, sawDiagnosticVersion1, sawDiagnosticVersion2)
	}
}

func TestWatchedFilesAreRegisteredAndDeletedDiagnosticsAreCleared(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.gpp")
	source := "package main\nfunc main() {}\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	uri, rootURI := uriForPath(path), uriForPath(root)
	var input bytes.Buffer
	writeTestMessage(&input, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"rootUri": rootURI,
			"capabilities": map[string]any{"workspace": map[string]any{
				"didChangeWatchedFiles": map[string]any{"dynamicRegistration": true},
			}},
		},
	})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "method": "initialized"})
	writeTestMessage(&input, map[string]any{
		"jsonrpc": "2.0", "method": "workspace/didChangeWatchedFiles",
		"params": map[string]any{"changes": []any{map[string]any{"uri": uri, "type": 3}}},
	})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	var output bytes.Buffer
	if err := Run(&input, &output, io.Discard); err != nil {
		t.Fatal(err)
	}

	messages := readTestMessages(t, output.Bytes())
	var registered, cleared, cleanDiagnosticsArray bool
	registeredWatchers := map[string]bool{}
	uriDiagnosticPublications := 0
	for _, message := range messages {
		if message["method"] == "client/registerCapability" {
			params, _ := message["params"].(map[string]any)
			registrations, _ := params["registrations"].([]any)
			if len(registrations) != 1 {
				t.Fatalf("expected one watched-file registration, got %#v", message)
			}
			registration, _ := registrations[0].(map[string]any)
			if registration["method"] != "workspace/didChangeWatchedFiles" {
				t.Fatalf("unexpected registration method: %#v", registration)
			}
			options, _ := registration["registerOptions"].(map[string]any)
			watchers, _ := options["watchers"].([]any)
			for _, value := range watchers {
				watcher, _ := value.(map[string]any)
				if pattern, ok := watcher["globPattern"].(string); ok {
					registeredWatchers[pattern] = true
				}
			}
			registered = true
		}
		if message["method"] == "textDocument/publishDiagnostics" {
			params, _ := message["params"].(map[string]any)
			if params["uri"] == uri {
				uriDiagnosticPublications++
				diagnostics, _ := params["diagnostics"].([]any)
				if uriDiagnosticPublications == 1 {
					cleanDiagnosticsArray = diagnostics != nil && len(diagnostics) == 0
				} else if uriDiagnosticPublications == 2 {
					cleared = diagnostics != nil && len(diagnostics) == 0
				}
			}
		}
	}
	for _, pattern := range []string{"**/*.gpp", "**/*.gpp.tpl", "**/*.go", "**/go.mod", "**/go.sum"} {
		if !registeredWatchers[pattern] {
			t.Errorf("server did not register workspace watcher %q", pattern)
		}
	}
	if !registered || !cleanDiagnosticsArray || !cleared || uriDiagnosticPublications < 2 {
		t.Fatalf("source watchers, empty-array clean diagnostics, and deleted-file diagnostic clearing are required: registered=%v watchers=%v clean=%v cleared=%v publications=%d", registered, registeredWatchers, cleanDiagnosticsArray, cleared, uriDiagnosticPublications)
	}
}

func TestWatchedFileCreateChangeAndDeleteRefreshWorkspaceSymbols(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "models.gpp")
	writeSource(t, path, "package models\nclass Before {}\n")
	workspace := newWorkspace(log.New(io.Discard, "", 0))
	workspace.setRoots([]string{root})
	workspace.refresh()
	if !workspaceHasSymbol(workspace, "Before") {
		t.Fatal("initial workspace source should be indexed")
	}

	var output bytes.Buffer
	server := &server{workspace: workspace, writer: bufio.NewWriter(&output), logger: log.New(io.Discard, "", 0)}
	writeSource(t, path, "package models\nclass After {}\n")
	params := func(changeType int) json.RawMessage {
		encoded, err := json.Marshal(map[string]any{"changes": []any{map[string]any{"uri": uriForPath(path), "type": changeType}}})
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	if _, err := server.handle(rpcRequest{Method: "workspace/didChangeWatchedFiles", Params: params(2)}); err != nil {
		t.Fatalf("change notification failed: %v", err)
	}
	if workspaceHasSymbol(workspace, "Before") || !workspaceHasSymbol(workspace, "After") {
		t.Fatalf("file change should replace indexed symbols: %#v", workspace.symbols)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := server.handle(rpcRequest{Method: "workspace/didChangeWatchedFiles", Params: params(3)}); err != nil {
		t.Fatalf("delete notification failed: %v", err)
	}
	if workspaceHasSymbol(workspace, "After") {
		t.Fatalf("deleted source should leave no indexed symbols: %#v", workspace.symbols)
	}
}

func writeSource(t *testing.T, path, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceFolderChangesRefreshMultipleRoots(t *testing.T) {
	writeSource := func(root, filename, source string) string {
		t.Helper()
		path := filepath.Join(root, filename)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	rootA, rootB, rootC := t.TempDir(), t.TempDir(), t.TempDir()
	writeSource(rootA, "go.mod", "module example.test/roota\n\ngo 1.27\n")
	writeSource(rootB, "go.mod", "module example.test/rootb\n\ngo 1.27\n")
	writeSource(rootA, "a.gpp", "package models\nclass Alpha {}\nfunc use(value Entity) {}\n")
	writeSource(rootB, "b.gpp", "package models\nclass Beta {}\nclass Entity {}\n")
	writeSource(rootC, "c.gpp", "package c\nclass Gamma {}\n")

	workspace := newWorkspace(nil)
	workspace.setRoots([]string{rootA, rootB})
	workspace.refresh()
	if !workspaceHasSymbol(workspace, "Alpha") || !workspaceHasSymbol(workspace, "Beta") {
		t.Fatalf("both workspace roots should contribute symbols: %#v", workspace.symbols)
	}

	var output bytes.Buffer
	server := &server{workspace: workspace, writer: bufio.NewWriter(&output)}
	consumerPath := filepath.Join(rootA, "a.gpp")
	consumer := workspace.files[consumerPath]
	usePosition := positionAtOffset(consumer.Source, strings.Index(consumer.Source, "Entity"))
	if definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: consumer.URI}, Position: usePosition}); len(definitions) != 0 {
		t.Fatalf("a same-named package in another module must not satisfy an unresolved symbol: %#v", definitions)
	}
	_, err := server.handle(rpcRequest{
		Method: "workspace/didChangeWorkspaceFolders",
		Params: json.RawMessage(fmt.Sprintf(`{"event":{"removed":[{"uri":%q}],"added":[{"uri":%q}]}}`, uriForPath(rootA), uriForPath(rootC))),
	})
	if err != nil {
		t.Fatalf("workspace folder update should succeed: %v", err)
	}
	if workspaceHasSymbol(workspace, "Alpha") || !workspaceHasSymbol(workspace, "Beta") || !workspaceHasSymbol(workspace, "Gamma") {
		t.Fatalf("removed roots should disappear while remaining and added roots stay indexed: %#v", workspace.symbols)
	}
}

func workspaceHasSymbol(workspace *workspaceState, name string) bool {
	for _, symbol := range workspace.symbols {
		if symbol.Symbol.Name == name {
			return true
		}
	}
	return false
}

func TestClosingUnsavedFileDropsWorkspaceStateAndClearsDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unsaved.gpp")
	uri := uriForPath(path)
	var output bytes.Buffer
	server := &server{
		writer: bufio.NewWriter(&output),
		logger: log.New(io.Discard, "", 0),
		workspace: &workspaceState{
			files:       map[string]*fileState{},
			diagnostics: map[string][]Diagnostic{},
		},
	}
	source := "package main\nclass UnsavedOnly {}\n"
	server.open(uri, source)
	if _, exists := server.workspace.files[path]; !exists || len(server.workspace.symbols) == 0 {
		t.Fatal("opening the unsaved file should add its buffer and symbols to the workspace")
	}
	server.close(uri)
	if _, exists := server.workspace.files[path]; exists {
		t.Fatal("closing a file with no disk copy should discard its stale buffer")
	}
	for _, symbol := range server.workspace.symbols {
		if symbol.Symbol.Name == "UnsavedOnly" {
			t.Fatal("closed unsaved file left a stale workspace symbol")
		}
	}
	messages := readTestMessages(t, output.Bytes())
	var cleared bool
	for _, message := range messages {
		if message["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		params, _ := message["params"].(map[string]any)
		if params["uri"] != uri {
			continue
		}
		diagnostics, _ := params["diagnostics"].([]any)
		cleared = diagnostics != nil && len(diagnostics) == 0
	}
	if !cleared {
		t.Fatalf("closing the unsaved document should clear its diagnostics, got %#v", messages)
	}
}

func TestHandleSafelyRecoversFromPanic(t *testing.T) {
	server := &server{logger: log.New(io.Discard, "", 0)}
	keepRunning, err := server.handleSafely(rpcRequest{Method: "initialize", Params: json.RawMessage(`{}`)})
	if !keepRunning {
		t.Fatal("server should continue after recovering from a handler panic")
	}
	if err == nil || !strings.Contains(err.Error(), "internal LSP error") {
		t.Fatalf("expected an internal LSP error, got %v", err)
	}
}

func TestUnknownRequestReturnsMethodNotFoundError(t *testing.T) {
	var output bytes.Buffer
	server := &server{writer: bufio.NewWriter(&output)}
	_, err := server.handle(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("7"), Method: "textDocument/unknown"})
	if err != nil {
		t.Fatalf("unknown request should be handled with a JSON-RPC error response: %v", err)
	}
	messages := readTestMessages(t, output.Bytes())
	if len(messages) != 1 {
		t.Fatalf("expected one error response, got %#v", messages)
	}
	response := messages[0]
	responseError, ok := response["error"].(map[string]any)
	if !ok || response["id"] != float64(7) || responseError["code"] != float64(-32601) || responseError["message"] != "method not found: textDocument/unknown" {
		t.Fatalf("unknown request response should use JSON-RPC method-not-found, got %#v", response)
	}
}

func TestShutdownRejectsRequestsAndIgnoresNotificationsUntilExit(t *testing.T) {
	var output bytes.Buffer
	server := &server{
		writer: bufio.NewWriter(&output),
		workspace: &workspaceState{
			files:       map[string]*fileState{},
			diagnostics: map[string][]Diagnostic{},
		},
	}
	if keepRunning, err := server.handle(rpcRequest{ID: json.RawMessage("1"), Method: "shutdown"}); err != nil || !keepRunning {
		t.Fatalf("shutdown should reply and keep the process alive until exit: keepRunning=%v err=%v", keepRunning, err)
	}
	if _, err := server.handle(rpcRequest{ID: json.RawMessage("2"), Method: "textDocument/hover"}); err != nil {
		t.Fatalf("post-shutdown request should receive an error response: %v", err)
	}
	uri := "file:///tmp/post-shutdown.gpp"
	if _, err := server.handle(rpcRequest{Method: "textDocument/didOpen", Params: json.RawMessage(`{"textDocument":{"uri":"` + uri + `","text":"package main"}}`)}); err != nil {
		t.Fatalf("post-shutdown notification should be ignored: %v", err)
	}
	if len(server.workspace.files) != 0 {
		t.Fatalf("post-shutdown notifications must not mutate workspace state: %#v", server.workspace.files)
	}
	messages := readTestMessages(t, output.Bytes())
	if len(messages) != 2 || messages[0]["result"] != nil {
		t.Fatalf("shutdown and post-shutdown request should each receive one response: %#v", messages)
	}
	responseError, ok := messages[1]["error"].(map[string]any)
	if !ok || responseError["code"] != float64(-32600) {
		t.Fatalf("post-shutdown request should return InvalidRequest (-32600), got %#v", messages[1])
	}
	if keepRunning, err := server.handle(rpcRequest{Method: "exit"}); err != nil || keepRunning {
		t.Fatalf("exit notification should stop the server: keepRunning=%v err=%v", keepRunning, err)
	}
}

func TestShutdownCancelsBackendAndDiscardsQueuedDiagnostics(t *testing.T) {
	var output bytes.Buffer
	backendContext, cancel := context.WithCancel(context.Background())
	server := &server{
		writer:        bufio.NewWriter(&output),
		backendStop:   make(chan struct{}),
		backendCancel: cancel,
	}
	if _, err := server.handle(rpcRequest{ID: json.RawMessage("1"), Method: "shutdown"}); err != nil {
		t.Fatalf("shutdown should succeed: %v", err)
	}
	if backendContext.Err() == nil {
		t.Fatal("shutdown should cancel the generated-Go diagnostics job")
	}
	select {
	case <-server.backendStop:
	default:
		t.Fatal("shutdown should stop the backend diagnostics worker")
	}
	if server.acceptBackendDiagnostics(backendDiagnosticResult{generation: server.backendGeneration, diagnostics: map[string][]Diagnostic{"file:///main.gpp": {{Message: "stale"}}}}) {
		t.Fatal("shutdown must discard any queued backend diagnostics")
	}
}

func TestExitWithoutShutdownReturnsError(t *testing.T) {
	var input, output bytes.Buffer
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	if err := Run(&input, &output, io.Discard); !errors.Is(err, errExitWithoutShutdown) {
		t.Fatalf("exit without shutdown should produce a failing process result, got %v", err)
	}
}

func TestInitializeAndInitializedAreAcceptedOnlyOnce(t *testing.T) {
	var output bytes.Buffer
	server := &server{
		workspace:                       newWorkspace(log.New(io.Discard, "", 0)),
		writer:                          bufio.NewWriter(&output),
		supportsWatchedFileRegistration: true,
	}
	initialize := rpcRequest{ID: json.RawMessage("1"), Method: "initialize", Params: json.RawMessage(`{"capabilities":{"workspace":{"didChangeWatchedFiles":{"dynamicRegistration":true}}}}`)}
	if _, err := server.handle(initialize); err != nil {
		t.Fatalf("initial initialize request failed: %v", err)
	}
	if _, err := server.handle(rpcRequest{ID: json.RawMessage("2"), Method: "initialize", Params: json.RawMessage(`{"capabilities":{"workspace":{"didChangeWatchedFiles":{"dynamicRegistration":true}}}}`)}); err != nil {
		t.Fatalf("second initialize should be answered with an invalid request response: %v", err)
	}
	if _, err := server.handle(rpcRequest{Method: "initialized"}); err != nil {
		t.Fatalf("initialized notification failed: %v", err)
	}
	if _, err := server.handle(rpcRequest{Method: "initialized"}); err != nil {
		t.Fatalf("duplicate initialized notification should be ignored: %v", err)
	}
	messages := readTestMessages(t, output.Bytes())
	var registerCount int
	if len(messages) != 3 {
		t.Fatalf("expected initialize response, duplicate-initialize error, and one registration, got %#v", messages)
	}
	initError, ok := messages[1]["error"].(map[string]any)
	if !ok || initError["code"] != float64(-32600) {
		t.Fatalf("duplicate initialize should return InvalidRequest (-32600), got %#v", messages[1])
	}
	for _, message := range messages {
		if message["method"] == "client/registerCapability" {
			registerCount++
		}
	}
	if registerCount != 1 {
		t.Fatalf("watch capability should register exactly once, got %d messages", registerCount)
	}
}

func TestServerCancelsInFlightRequestAndReturnsLSPError(t *testing.T) {
	input, inputWriter := io.Pipe()
	var output bytes.Buffer
	server := &server{
		reader:                   bufio.NewReader(input),
		writer:                   bufio.NewWriter(&output),
		logger:                   log.New(io.Discard, "", 0),
		supportsWorkDoneProgress: true,
	}
	started := make(chan struct{})
	release := make(chan struct{})
	queuedCancellationCalls := 0
	server.handleRequest = func(request rpcRequest) (bool, error) {
		if request.Method == "workspace/symbol" {
			close(started)
			<-release
			_ = server.respond(request.ID, "late result")
			return true, nil
		}
		if request.Method == "test/queued" {
			_ = server.respond(request.ID, "queued result")
			return true, nil
		}
		if request.Method == "test/cancelled-queued" {
			queuedCancellationCalls++
			_ = server.respond(request.ID, "should not run")
			return true, nil
		}
		if request.Method == "shutdown" {
			return server.handle(request)
		}
		return false, nil
	}
	done := make(chan error, 1)
	go func() { done <- serve(server) }()

	write := func(value any) {
		t.Helper()
		var message bytes.Buffer
		writeTestMessage(&message, value)
		if _, err := inputWriter.Write(message.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	write(map[string]any{"jsonrpc": "2.0", "id": 10, "method": "workspace/symbol", "params": map[string]any{"workDoneToken": "search-10"}})
	<-started
	write(map[string]any{"jsonrpc": "2.0", "id": 11, "method": "test/queued"})
	write(map[string]any{"jsonrpc": "2.0", "id": 13, "method": "test/cancelled-queued"})
	write(map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": 13}})
	write(map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": 10}})
	write(map[string]any{"jsonrpc": "2.0", "id": 12, "method": "shutdown"})
	write(map[string]any{"jsonrpc": "2.0", "method": "exit"})
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("server returned an error: %v", err)
	}
	_ = inputWriter.Close()
	messages := readTestMessages(t, output.Bytes())
	var sawCancelled, sawQueuedResponse, sawQueuedCancellation, sawProgressBegin, sawProgressEnd bool
	var responseCount10 int
	for _, message := range messages {
		if message["id"] == float64(10) {
			responseCount10++
			responseError, ok := message["error"].(map[string]any)
			sawCancelled = ok && responseError["code"] == float64(-32800)
			if _, hasResult := message["result"]; hasResult {
				t.Fatalf("cancelled request must not also publish a late result: %#v", message)
			}
		}
		if message["id"] == float64(11) && message["result"] == "queued result" {
			sawQueuedResponse = true
		}
		if message["id"] == float64(13) {
			responseError, ok := message["error"].(map[string]any)
			sawQueuedCancellation = ok && responseError["code"] == float64(-32800)
		}
		if message["method"] == "$/progress" {
			params, _ := message["params"].(map[string]any)
			if params["token"] != "search-10" {
				continue
			}
			value, _ := params["value"].(map[string]any)
			sawProgressBegin = sawProgressBegin || value["kind"] == "begin"
			sawProgressEnd = sawProgressEnd || value["kind"] == "end"
		}
	}
	if !sawCancelled || responseCount10 != 1 || !sawQueuedResponse || !sawQueuedCancellation || queuedCancellationCalls != 0 || !sawProgressBegin || !sawProgressEnd {
		t.Fatalf("cancel response and complete progress lifecycle are required, got %#v", messages)
	}
}

func TestWorkDoneProgressCancelCancelsItsRequest(t *testing.T) {
	input, inputWriter := io.Pipe()
	var output bytes.Buffer
	server := &server{
		reader:                   bufio.NewReader(input),
		writer:                   bufio.NewWriter(&output),
		logger:                   log.New(io.Discard, "", 0),
		supportsWorkDoneProgress: true,
	}
	started := make(chan struct{})
	release := make(chan struct{})
	server.handleRequest = func(request rpcRequest) (bool, error) {
		if request.Method == "workspace/symbol" {
			close(started)
			<-release
			_ = server.respond(request.ID, "late result")
			return true, nil
		}
		if request.Method == "shutdown" {
			return server.handle(request)
		}
		if request.Method == "exit" {
			return false, nil
		}
		return true, nil
	}
	done := make(chan error, 1)
	go func() { done <- serve(server) }()
	write := func(value any) {
		t.Helper()
		var message bytes.Buffer
		writeTestMessage(&message, value)
		if _, err := inputWriter.Write(message.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	write(map[string]any{"jsonrpc": "2.0", "id": 21, "method": "workspace/symbol", "params": map[string]any{"workDoneToken": "progress-21"}})
	<-started
	write(map[string]any{"jsonrpc": "2.0", "method": "window/workDoneProgress/cancel", "params": map[string]any{"token": "progress-21"}})
	close(release)
	write(map[string]any{"jsonrpc": "2.0", "id": 22, "method": "shutdown"})
	write(map[string]any{"jsonrpc": "2.0", "method": "exit"})
	if err := <-done; err != nil {
		t.Fatalf("server returned an error: %v", err)
	}
	_ = inputWriter.Close()
	messages := readTestMessages(t, output.Bytes())
	var canceled, progressBegin, progressEnd bool
	for _, message := range messages {
		if message["id"] == float64(21) {
			responseError, ok := message["error"].(map[string]any)
			canceled = ok && responseError["code"] == float64(-32800)
			if _, hasResult := message["result"]; hasResult {
				t.Fatalf("progress-cancelled request must not return a late result: %#v", message)
			}
		}
		if message["method"] == "$/progress" {
			params, _ := message["params"].(map[string]any)
			if params["token"] != "progress-21" {
				continue
			}
			value, _ := params["value"].(map[string]any)
			progressBegin = progressBegin || value["kind"] == "begin"
			progressEnd = progressEnd || value["kind"] == "end"
		}
	}
	if !canceled || !progressBegin || !progressEnd {
		t.Fatalf("progress cancellation should cancel the matching request and close its progress: %#v", messages)
	}
}

func TestLSPUsesASTTokensForIdentifiers(t *testing.T) {
	source := "func main() {\n" +
		"\tname := \"name\" // name\n" +
		"\tprintln(name)\n" +
		"}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	spans := identifierSpans(file, source, "name")
	if len(spans) != 2 {
		t.Fatalf("expected only declaration and use identifiers, got %d: %#v", len(spans), spans)
	}
	for _, span := range spans {
		if got := source[offsetForTestPosition(source, span.Start):offsetForTestPosition(source, span.End)]; got != "name" {
			t.Fatalf("unexpected identifier span %q", got)
		}
	}

	state := &fileState{Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{"/main.gpp": state}}}
	if name, _ := server.symbolsAt("file:///main.gpp", Position{Line: 1, Character: 11}); name != "" {
		t.Fatalf("string contents were treated as an identifier: %q", name)
	}
}

func TestOffsetAtUsesUTF16AndClampsToRequestedLine(t *testing.T) {
	source := "a🌟b\r\nsecond"
	if got := offsetAt(source, Position{Line: 0, Character: 3}); got != len("a🌟") {
		t.Fatalf("UTF-16 character 3 should point after the supplementary rune, got byte %d", got)
	}
	lineEnd := len("a🌟b")
	if got := offsetAt(source, Position{Line: 0, Character: 99}); got != lineEnd {
		t.Fatalf("oversized character should clamp to current line end (%d), got %d", lineEnd, got)
	}
	if got := offsetAt(source, Position{Line: 1, Character: 99}); got != len(source) {
		t.Fatalf("oversized character on last line should clamp to EOF, got %d", got)
	}
	changed := applyChange(source, Range{Start: Position{Line: 0, Character: 1}, End: Position{Line: 0, Character: 3}}, "🌈")
	if want := "a🌈b\r\nsecond"; changed != want {
		t.Fatalf("incremental change should replace one supplementary rune without damaging CRLF: %q, want %q", changed, want)
	}
	changed = applyChange(changed, Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 6}}, "next")
	if want := "a🌈b\r\nnext"; changed != want {
		t.Fatalf("sequential incremental changes should use the updated document and UTF-16 positions: %q, want %q", changed, want)
	}
}

func TestDiagnosticDeduplicationPreservesSameLineOccurrences(t *testing.T) {
	diagnostics := []Diagnostic{
		{Range: Range{Start: Position{Line: 2, Character: 4}, End: Position{Line: 2, Character: 8}}, Message: "unknown name", Source: "gpp"},
		{Range: Range{Start: Position{Line: 2, Character: 19}, End: Position{Line: 2, Character: 23}}, Message: "unknown name", Source: "gpp"},
		{Range: Range{Start: Position{Line: 2, Character: 4}, End: Position{Line: 2, Character: 7}}, Message: "unknown name", Source: "go/types"},
	}
	got := deduplicateDiagnostics(diagnostics)
	if len(got) != 2 {
		t.Fatalf("same-line errors at different columns must remain distinct while duplicate reports at one position collapse, got %#v", got)
	}
	if got[0].Range.Start.Character != 4 || got[1].Range.Start.Character != 19 {
		t.Fatalf("deduplicated diagnostics lost a source location: %#v", got)
	}
}

func TestPositionAtOffsetExcludesCRFromCRLFLineColumns(t *testing.T) {
	source := "a\r\nb"
	for _, offset := range []int{1, 2} {
		if got := positionAtOffset(source, offset); got != (Position{Line: 0, Character: 1}) {
			t.Errorf("offset %d on the CRLF line ending mapped to %#v, want end-of-line position", offset, got)
		}
	}
	if got := positionAtOffset(source, 3); got != (Position{Line: 1, Character: 0}) {
		t.Fatalf("offset after CRLF mapped to %#v, want start of line 1", got)
	}
}

func offsetForTestPosition(source string, position Position) int {
	return offsetAt(source, position)
}

func writeTestMessage(output *bytes.Buffer, value any) {
	payload, _ := json.Marshal(value)
	output.WriteString("Content-Length: ")
	output.WriteString(strconvItoa(len(payload)))
	output.WriteString("\r\n\r\n")
	output.Write(payload)
}

func readTestMessages(t *testing.T, data []byte) []map[string]any {
	var result []map[string]any
	for len(data) > 0 {
		separator := bytes.Index(data, []byte("\r\n\r\n"))
		if separator < 0 {
			t.Fatalf("missing LSP header separator")
		}
		header := string(data[:separator])
		const prefix = "Content-Length: "
		if !strings.HasPrefix(header, prefix) {
			t.Fatalf("unexpected header %q", header)
		}
		length := 0
		if _, err := fmtSscanf(header[len(prefix):], &length); err != nil {
			t.Fatal(err)
		}
		data = data[separator+4:]
		if len(data) < length {
			t.Fatalf("short LSP payload")
		}
		var message map[string]any
		if err := json.Unmarshal(data[:length], &message); err != nil {
			t.Fatal(err)
		}
		result = append(result, message)
		data = data[length:]
	}
	return result
}

func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}

func fmtSscanf(value string, target *int) (int, error) {
	parsed := 0
	for _, r := range strings.TrimSpace(value) {
		if r < '0' || r > '9' {
			return 0, os.ErrInvalid
		}
		parsed = parsed*10 + int(r-'0')
	}
	*target = parsed
	return 1, nil
}
