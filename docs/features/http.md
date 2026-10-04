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
app.Listen()
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
app.Listen()
```

`http.Mountable` is a marker annotation; no router base class is required. Path
mounts start with `/`; host mounts use a hostname, optionally followed by a
path. The same class instance serves every supplied address. Mount classes
before starting the server. Server-level request hooks apply to mounted routes,
and the server prefix is combined with the mount path and class prefix.
