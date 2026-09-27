# HTTP servers and routes

The Go++ HTTP package puts route declarations beside handler methods and
provides a server lifecycle around Go's HTTP foundation. It keeps routing,
request handling, and response construction in one readable place.

## Compare route setup

A Go HTTP server registers handlers with a mux. Go++ lets a method declare its
route with an annotation:

::: code-group

```go [Go net/http]
mux := http.NewServeMux()
mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    _, _ = w.Write([]byte(`{"ok":true}`))
})
```

```go [Go++ server]
class App : http.Server {
    func Health(ctx *http.Context) error
        @{http.GET("/health")} {
        return ctx.JSON(record(ok: true))
    }
}

app := App()
app.Listen()
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
