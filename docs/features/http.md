# HTTP servers and routes

The Go++ HTTP package puts route declarations beside handler methods and
provides a server lifecycle around Go's HTTP foundation. It keeps routing,
request handling, and response construction in one readable place.

## Compare route setup

A Go HTTP server registers handlers with a mux. Go++ lets a method declare its
route with an annotation:

::: code-group
```go [Go++]
class App : http.Server {
    func Health(ctx *http.Context) error
        @{http.GET("/health")} {
        return ctx.JSON(record(ok: true))
    }
}

app := App()
app.Listen(0)
```

```go [Go]
mux := http.NewServeMux()
mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    _, _ = w.Write([]byte(`{"ok":true}`))
})
```
:::

## Use request context and lifecycle hooks

`http.Context` provides path parameters, query values, headers, cookies, and
typed response helpers:

```go
class App : http.Server {
    func Hello(ctx *http.Context) error
        @{http.GET("/hello/{name}")} {
        return ctx.Text("Hello " + ctx.Param("name"))
    }

    func BeforeRequest(ctx *http.Context) error {
        return nil
    }
}
```

Optional hooks can run before listening, around requests, and during shutdown.
Handlers return errors for the server boundary to handle. See the [HTTP specification](/reference/specifications/std.http)
and [lifecycle guide](/reference/specifications/std.http.lifecycle).

## Handle requests before route dispatch

`http.Server` provides a default `Handle(ctx)` method that returns
`(false, nil)`, so existing annotated routes work unchanged. Override it when
one entry point should inspect every request before the server tries annotated
routes:

```go
class App : http.Server {
    func Handle(ctx *http.Context) (bool, error) {
        if ctx.Request.URL.Path == "/healthz" {
            return true, ctx.Text("ok")
        }
        return false, nil
    }

    func Hello(ctx *http.Context) error @{http.GET("/hello")} {
        return ctx.Text("hello")
    }
}
```

Return `true` when `Handle` has responded to the request. Return `false` to
continue to annotated routes; do not write a response before continuing. An
error goes through the server's normal error handler. The same context reaches
the selected route, and request hooks run once around the whole request.

## Bind into a typed value

`ctx.Bind[T]()` returns the request data as the type you ask for. `T` can be a
Go++ class, a Go struct, or a structural record. The request method and content
type select query, JSON, or form binding:

::: info
Use a pointer type argument when you want a pointer result:
`ctx.Bind[*UpdateUserRequest]()` returns `*UpdateUserRequest`.
:::

```go
func Create(ctx *http.Context) error @{http.POST("/users")} {
    input := ctx.Bind[record(name: string, age: int)]()
    return ctx.JSON(201, record(Name: input.name, Age: input.age))
}

func Update(ctx *http.Context) error @{http.PUT("/users/{id}")} {
    input := ctx.Bind[*UpdateUserRequest]()
    return ctx.JSON(200, input)
}
```

Go structs and classes use their exported fields and `form`/`json` tags.
Records bind through their generated field accessors, including lowercase
field names. Missing fields keep their zero or initial values; unknown input
fields are ignored. The existing destination-based forms remain available
when a value must be initialized before binding:

```go
input := CreateUserRequest{Name: "guest"}
ctx.Bind(&input)
```

An uncaptured binding error propagates automatically and can be caught at a
request boundary when local recovery is needed.

## WebSocket handlers

Use `http.WebSocket(path)` on a handler method to accept a WebSocket connection.
The handler owns the message loop, so local variables keep state for that
connection:

```go
class App : http.Server {
    func Echo(ctx *http.Context) error @{http.WebSocket("/echo")} {
        for {
            var message string
            if err := ctx.Conn.Read(&message); err != nil {
                return nil
            }
            if err := ctx.Conn.Write("echo: " + message); err != nil {
                return err
            }
        }
    }
}
```

`ctx.Conn` is the underlying `*websocket.Conn`. The `gpp/http` package adds
`Read(&message)` and `Write(message)` extensions for strings; its native
`Read([]byte)` and `Write([]byte)` methods remain available for byte I/O and
return `(n, error)`. Use `websocket.Message.Receive` or `Send` directly when
you need whole binary messages. Each handler invocation belongs to one
connection. Shared server fields remain shared across connections; store
connections there only when other work needs to send to clients, such as for
broadcasts. See the
[WebSocket specification](/reference/specifications/std.http.websocket).

## Server-Sent Events

Use an ordinary `http.GET(path)` handler and write events through
`ctx.SSE.Write`. The optional `http.SSE` marker makes the handler's intent
clear in the route declaration; it does not affect routing or runtime behavior.
Each call writes and flushes one WHATWG-formatted event, including multiline
data and optional event name, ID, and retry delay:

```go
import "time"

class App : http.Server {
    func Events(ctx *http.Context) error @{
        http.GET("/events"),
        http.SSE
    } {
        retryMS := 3000
        counter := 0
        for {
            counter++
            id := "{{counter}}"
            try {
                ctx.SSE.Write("update {{counter}}\nstream is active", "update", &id, &retryMS)
            } catch {
                return nil // the client disconnected
            }
            time.Sleep(time.Second)
        }
    }
}
```

SSE endpoints are typically GET routes for browser `EventSource` clients.
See the [Server-Sent Events specification](/reference/specifications/std.http.sse).

## Mount independent route groups

Keep route methods on the server class, or group related handlers in a class
marked with `http.Mountable` and mount it at one or more paths or hostnames:

```go
class BillingRoutes @{http.Mountable} {
    func Status(ctx *http.Context) error @{http.GET("/status")} {
        return ctx.JSON(record(Area: "billing"))
    }
}

app := App()
app.Mount(BillingRoutes(), "/billing", "billing.example.com")
app.Listen(0)
```

`http.Mountable` is a marker annotation; no router base class is required. Path
mounts start with `/`; host mounts use a hostname, optionally followed by a
path. The same class instance serves every supplied address. Mount classes
before starting the server. Server-level request hooks apply to mounted routes,
and the server prefix is combined with the mount path and class prefix.
