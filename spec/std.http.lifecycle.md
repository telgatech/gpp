# gpp/http Server Lifecycle and Request Hooks Specification

## Goal

Define lifecycle behavior for `gpp/http.Server` covering both:

* server-process lifecycle
* per-request lifecycle

The design should preserve these principles:

* `Listen()` remains the main blocking server entry point.
* graceful shutdown on `SIGINT` / `SIGTERM` works by default.
* shutdown infrastructure is separate from application startup hooks.
* overriding `BeforeListen()` must not disable default signal handling.
* applications may override shutdown setup deliberately.
* request hooks provide a simple place for application-wide request setup, logging, context values, and cleanup.
* all hooks are ordinary polymorphic Go++ methods.
* no compiler special cases are required.

---

# Base API

`gpp/http.Server` should provide:

```go
class Server {
    func SetupShutdownHandler() {
        ...
    }

    func BeforeListen() error {
        return nil
    }

    func AfterListen() error {
        return nil
    }

    func BeforeShutdown() error {
        return nil
    }

    func AfterShutdown() error {
        return nil
    }

    func BeforeRequest(ctx *Context) error {
        return nil
    }

    func AfterRequest(ctx *Context) error {
        return nil
    }

    func Listen() error {
        ...
    }

    func Serve(listener net.Listener) error {
        ...
    }

    func Shutdown(ctx context.Context) error {
        ...
    }
}
```

The lifecycle methods are:

```text
SetupShutdownHandler

BeforeListen
AfterListen

BeforeRequest
AfterRequest

BeforeShutdown
AfterShutdown
```

---

# Lifecycle Categories

The methods have different responsibilities.

```text
SetupShutdownHandler
    server infrastructure

BeforeListen / AfterListen
    application startup

BeforeRequest / AfterRequest
    application-wide request lifecycle

BeforeShutdown / AfterShutdown
    application shutdown
```

These concerns should remain separate.

---

# Default Shutdown Handling

The default implementation of:

```go
SetupShutdownHandler()
```

should install graceful shutdown handling for:

```text
SIGINT
SIGTERM
```

Conceptually:

```go
func SetupShutdownHandler() {
    signals := make(chan os.Signal, 1)

    signal.Notify(
        signals,
        os.Interrupt,
        syscall.SIGTERM,
    )

    go func() {
        <-signals

        ctx, cancel := context.WithTimeout(
            context.Background(),
            10*time.Second,
        )
        defer cancel()

        this.Shutdown(ctx)
    }()
}
```

The exact implementation may differ.

Observable behavior should remain equivalent.

---

# Why Shutdown Setup Is Separate

Do not place default signal handling inside:

```go
BeforeListen()
```

Applications will commonly override `BeforeListen()`.

Example:

```go
class App : http.Server {
    func BeforeListen() error {
        return this.StartWorkers()
    }
}
```

This must not accidentally disable graceful SIGINT/SIGTERM handling.

Therefore:

```text
SetupShutdownHandler
```

must be independent from:

```text
BeforeListen
```

---

# Listen Lifecycle

Recommended `Listen()` sequence:

```text
inspect server annotations
↓
discover routes
↓
register routes
↓
validate configuration
↓
prepare listener
↓
SetupShutdownHandler()
↓
BeforeListen()
↓
begin serving
↓
AfterListen()
↓
block while serving
↓
shutdown / serve termination
↓
return
```

`Listen()` remains blocking.

Calling `Shutdown()` concurrently should cause the blocking serve operation to return.

---

# SetupShutdownHandler

## Default Behavior

Register handlers for:

```text
SIGINT
SIGTERM
```

and call:

```go
this.Shutdown(ctx)
```

from a separate goroutine.

The method itself must not block.

---

## Replacing Default Behavior

Applications may override it:

```go
class App : http.Server {
    func SetupShutdownHandler() {
        // custom implementation
    }
}
```

If the override does not call:

```go
super.SetupShutdownHandler()
```

the built-in signal behavior is intentionally replaced.

---

## Extending Default Behavior

Applications may preserve the built-in mechanism and add another shutdown trigger:

```go
class App : http.Server {
    func SetupShutdownHandler() {
        super.SetupShutdownHandler()

        go func() {
            <-this.CustomShutdownChannel
            this.Shutdown(context.Background())
        }()
    }
}
```

Shutdown itself must remain safe if triggered more than once.

---

# BeforeListen

Default:

```go
func BeforeListen() error {
    return nil
}
```

Runs after:

```text
configuration
route setup
listener preparation
shutdown handler setup
```

but before the application begins serving requests.

Typical uses:

* initialize resources
* warm caches
* load templates
* verify dependencies
* start workers
* prepare application state

Example:

```go
func BeforeListen() error {
    return this.Workers.Start()
}
```

---

# AfterListen

Default:

```go
func AfterListen() error {
    return nil
}
```

Runs after the server has successfully begun serving.

It must not mean:

```text
after Serve() eventually returns
```

because that would make it a shutdown event.

Typical uses:

* log readiness
* announce listening address
* update readiness state
* start tasks that require the listener to already exist

If `AfterListen()` returns an error, the server should initiate shutdown and return the error.

---

# Request Lifecycle

Every routed request should pass through:

```text
create Context
↓
BeforeRequest(ctx)
↓
route handler
↓
AfterRequest(ctx)
```

The exact same `Context` instance must be passed to:

```text
BeforeRequest
route handler
AfterRequest
```

---

# BeforeRequest

Default:

```go
func BeforeRequest(ctx *Context) error {
    return nil
}
```

Runs once before the selected route handler.

Typical uses:

* assign request IDs
* initialize request-local values
* start timing
* attach tracing information
* resolve tenant information
* load authenticated user metadata
* attach application-specific context data
* application-wide logging setup

Example:

```go
func BeforeRequest(ctx *http.Context) error {
    ctx.Set("started", time.Now())
    ctx.Set("requestId", uuid.NewString())

    return nil
}
```

---

# BeforeRequest Failure

If:

```go
BeforeRequest(ctx)
```

returns an error:

* the route handler must not run
* the error should be sent through the server's normal error handling
* `AfterRequest()` should not run

Reason:

`AfterRequest()` represents teardown corresponding to a successfully completed request setup phase.

---

# Route Handler

After successful `BeforeRequest()`:

```go
err := route(ctx)
```

The route may:

* succeed
* return an error

`AfterRequest()` must run in either case.

---

# AfterRequest

Default:

```go
func AfterRequest(ctx *Context) error {
    return nil
}
```

Runs once after the route handler finishes.

It must run even if the route handler returns an error.

Typical uses:

* request logging
* timing
* cleanup
* telemetry
* request-scoped resource release
* audit information
* metrics

Example:

```go
func AfterRequest(ctx *http.Context) error {
    started := ctx.Get("started").(time.Time)

    fmt.Println(
        ctx.Request.Method,
        ctx.Request.URL.Path,
        time.Since(started),
    )

    return nil
}
```

---

# Request Hook Ordering

Successful request:

```text
Context created
↓
BeforeRequest
↓
route handler
↓
AfterRequest
↓
response complete
```

Route-handler error:

```text
Context created
↓
BeforeRequest
↓
route handler returns error
↓
AfterRequest
↓
error handling
```

BeforeRequest error:

```text
Context created
↓
BeforeRequest returns error
↓
route handler skipped
↓
AfterRequest skipped
↓
error handling
```

---

# AfterRequest Reliability

The implementation should structure request dispatch so `AfterRequest()` runs reliably after a successful `BeforeRequest()`.

Conceptually:

```go
if err := this.BeforeRequest(ctx); err != nil {
    return this.HandleError(ctx, err)
}

routeErr := route(ctx)

afterErr := this.AfterRequest(ctx)

if routeErr != nil {
    return this.HandleError(ctx, routeErr)
}

if afterErr != nil {
    return this.HandleError(ctx, afterErr)
}

return nil
```

A `defer` may be used internally if it preserves correct error handling.

---

# Error Precedence

If both:

```text
route handler
AfterRequest
```

return errors, preserve the route error as the primary error for v1.

Do not introduce complex aggregate error handling solely for this feature.

The `AfterRequest` error may be logged or otherwise surfaced as appropriate.

If the route succeeds and only `AfterRequest()` fails, handle the `AfterRequest()` error normally.

---

# Context Request Data

`Context` should support request-local application data.

Recommended minimal API:

```go
class Context {
    Response http.ResponseWriter
    Request *http.Request

    Values map[string]any

    func Set(name string, value any) {
        this.Values[name] = value
    }

    func Get(name string) any {
        return this.Values[name]
    }
}
```

`Values` belongs to one request only.

A new value map should be created for every request.

No data should leak between requests.

---

# Context Lifetime

A `Context` represents a single HTTP request.

Conceptually:

```text
incoming request
↓
new Context
↓
BeforeRequest
↓
handler
↓
AfterRequest
↓
Context discarded
```

Do not reuse a Context between unrelated requests unless a future implementation deliberately introduces safe pooling while preserving these semantics.

---

# Context Data Example

```go
class App : http.Server {
    func BeforeRequest(ctx *http.Context) error {
        ctx.Set("startedAt", time.Now())

        return nil
    }

    func AfterRequest(ctx *http.Context) error {
        started := ctx.Get("startedAt").(time.Time)

        fmt.Println(
            "request took",
            time.Since(started),
        )

        return nil
    }
}
```

The route may also access values created by `BeforeRequest()`:

```go
func Home(ctx *http.Context) error @{http.GET("/")} {
    started := ctx.Get("startedAt")

    ...
}
```

---

# Middleware Relationship

`BeforeRequest()` and `AfterRequest()` do not eliminate middleware.

Use request hooks for simple application-wide lifecycle behavior.

Use middleware when behavior needs:

* composition
* ordering
* per-route application
* wrapping of specific route groups
* reusable independent components

Examples better suited to hooks:

```text
request ID
application-wide logging
global timing
global request context setup
global cleanup
```

Examples often better suited to middleware:

```text
CORS
rate limiting
specific authentication policies
compression
route-group behavior
third-party HTTP middleware
```

Both mechanisms may coexist.

---

# BeforeShutdown

Default:

```go
func BeforeShutdown() error {
    return nil
}
```

Runs immediately before graceful HTTP shutdown.

Typical uses:

* mark readiness false
* stop accepting new application work
* stop workers
* flush queues
* persist pending state
* close resources

A returned error should not normally prevent shutdown from continuing.

---

# AfterShutdown

Default:

```go
func AfterShutdown() error {
    return nil
}
```

Runs after graceful HTTP shutdown completes.

Typical uses:

* final cleanup
* logging
* telemetry flush
* shutdown bookkeeping

---

# Shutdown

Conceptual API:

```go
func Shutdown(ctx context.Context) error
```

Recommended sequence:

```text
BeforeShutdown
↓
underlying http.Server.Shutdown(ctx)
↓
AfterShutdown
```

`Shutdown()` may be triggered by:

* SIGINT
* SIGTERM
* application code
* tests
* custom shutdown mechanisms

---

# Blocking Listen

Typical application:

```go
func main() {
    if err := App().Listen(); err != nil {
        panic(err)
    }
}
```

No signal-handling boilerplate should be required.

Default behavior:

```text
App().Listen()
↓
startup
↓
serve
↓
SIGINT / SIGTERM
↓
Shutdown()
↓
graceful shutdown
↓
Listen returns
```

---

# Default Shutdown Timeout

Default signal-triggered shutdown should use a bounded context.

Recommended initial default:

```text
10 seconds
```

Conceptually:

```go
ctx, cancel := context.WithTimeout(
    context.Background(),
    10*time.Second,
)
defer cancel()

this.Shutdown(ctx)
```

Timeout customization can be addressed separately.

---

# Multiple Shutdown Requests

Shutdown may be requested by multiple sources.

Example:

```text
SIGTERM
+
application calls Shutdown()
```

The server should ensure shutdown lifecycle hooks execute at most once.

Recommended conceptual states:

```text
created
starting
running
stopping
stopped
```

Implementation may use:

```text
sync.Once
mutex/state
atomic state
```

Exact mechanism is implementation-defined.

---

# Lifecycle Execution Count

For one server lifecycle:

```text
SetupShutdownHandler    once

BeforeListen            once
AfterListen             at most once

BeforeRequest           once per routed request
AfterRequest            at most once per routed request

BeforeShutdown          at most once
AfterShutdown           at most once
```

---

# Startup Failure

If route discovery, validation, or listener preparation fails before startup:

```text
BeforeListen
AfterListen
```

must not run.

If `BeforeListen()` returns an error:

* serving must not begin
* `AfterListen()` must not run

If serving fails to start successfully:

* `AfterListen()` must not run

Signal resources should be cleaned up where appropriate.

---

# AfterListen Timing Requirement

Do not implement:

```go
err := httpServer.Serve(listener)

this.AfterListen()
```

because this would invoke `AfterListen()` after the server exits.

Startup must be structured so `AfterListen()` executes once the server has actually entered the serving state.

---

# Polymorphism

All hooks are normal virtual Go++ methods.

Example:

```go
class App : http.Server {
    func BeforeRequest(ctx *http.Context) error {
        ...
    }

    func AfterRequest(ctx *http.Context) error {
        ...
    }
}
```

Inherited `Server` request dispatch must invoke:

```text
App.BeforeRequest
App.AfterRequest
```

through normal runtime dispatch.

Likewise inherited `Listen()` must dispatch to overridden server lifecycle methods.

---

# Runtime Class Identity

Inside inherited server behavior:

```go
this.class
```

must refer to the most-derived server class.

Given:

```go
class App : http.Server {}
```

then inside:

```text
Server.Listen
request dispatch
Shutdown
```

the runtime descriptor must still be:

```text
App.class
```

This remains important for route and annotation discovery.

---

# `super`

Applications may call base behavior explicitly:

```go
class App : http.Server {
    func SetupShutdownHandler() {
        super.SetupShutdownHandler()
        this.SetupAdditionalShutdownTrigger()
    }
}
```

Lifecycle hooks may also call their base implementation:

```go
func BeforeRequest(ctx *http.Context) error {
    if err := super.BeforeRequest(ctx); err != nil {
        return err
    }

    ctx.Set("foo", "bar")

    return nil
}
```

Calling `super` is optional when the inherited implementation is a no-op.

---

# Example Application

```go
import (
    "fmt"
    "gpp/http"
    "time"
)

class App : http.Server @{
    http.IP("0.0.0.0"),
    http.Port(8080)
} {
    func BeforeListen() error {
        fmt.Println("starting application")
        return nil
    }

    func AfterListen() error {
        fmt.Println("server ready")
        return nil
    }

    func BeforeRequest(ctx *http.Context) error {
        ctx.Set("started", time.Now())

        return nil
    }

    func AfterRequest(ctx *http.Context) error {
        started := ctx.Get("started").(time.Time)

        fmt.Println(
            ctx.Request.Method,
            ctx.Request.URL.Path,
            time.Since(started),
        )

        return nil
    }

    func Home(ctx *http.Context) error @{
        http.GET("/")
    } {
        return ctx.Text("hello")
    }

    func BeforeShutdown() error {
        fmt.Println("shutting down")
        return nil
    }

    func AfterShutdown() error {
        fmt.Println("shutdown complete")
        return nil
    }
}

func main() {
    if err := App().Listen(); err != nil {
        panic(err)
    }
}
```

Expected conceptual behavior:

```text
starting application
server ready

GET /...
BeforeRequest
Home
AfterRequest

...

SIGINT

shutting down
shutdown complete
```

---

# Custom Shutdown Example

Replace default signal handling:

```go
class App : http.Server {
    func SetupShutdownHandler() {
        go func() {
            <-this.CustomShutdownChannel

            this.Shutdown(
                context.Background(),
            )
        }()
    }
}
```

Because:

```go
super.SetupShutdownHandler()
```

is not called, default SIGINT/SIGTERM handling is intentionally removed.

---

# Extended Shutdown Example

Retain normal signals while adding another trigger:

```go
class App : http.Server {
    func SetupShutdownHandler() {
        super.SetupShutdownHandler()

        go func() {
            <-this.CustomShutdownChannel

            this.Shutdown(
                context.Background(),
            )
        }()
    }
}
```

Multiple shutdown requests must still result in one shutdown lifecycle.

---

# Tests

## Default shutdown handler

Verify default `SetupShutdownHandler()` responds to:

```text
SIGINT
SIGTERM
```

and invokes graceful shutdown.

---

## BeforeListen override

Override `BeforeListen()` without calling `super`.

Verify default shutdown signal handling still exists.

---

## SetupShutdownHandler override

Override without calling `super`.

Verify built-in signal behavior is replaced.

---

## Request ordering

Verify:

```text
BeforeRequest
route
AfterRequest
```

for successful requests.

---

## Route error

Verify:

```text
BeforeRequest
route returns error
AfterRequest
error handler
```

---

## BeforeRequest failure

Verify:

```text
BeforeRequest returns error
```

causes:

```text
route skipped
AfterRequest skipped
```

---

## Shared Context

Verify values set in:

```go
BeforeRequest(ctx)
```

are visible from:

```text
route handler
AfterRequest
```

using the same Context instance.

---

## Context isolation

Verify data stored in one request's Context is not visible in another request.

---

## Derived dispatch

Override request hooks in `App`.

Verify inherited Server request dispatch calls the App implementations.

---

## Shutdown ordering

Verify:

```text
BeforeShutdown
underlying shutdown
AfterShutdown
```

---

## Duplicate shutdown

Trigger shutdown through multiple mechanisms.

Verify shutdown hooks run only once.

---

## Blocking Listen

Verify `Listen()` blocks while serving and returns after shutdown completes.

---

# No Compiler Magic

The compiler must not specially recognize:

```text
SetupShutdownHandler

BeforeListen
AfterListen

BeforeRequest
AfterRequest

BeforeShutdown
AfterShutdown
```

They are ordinary methods implemented by `gpp/http.Server`.

The implementation should rely only on existing Go++ capabilities:

```text
classes
inheritance
polymorphism
annotations
introspection
Go interop
```

---

# Design Principle

`gpp/http.Server` should provide a small, predictable lifecycle around ordinary `net/http`.

The intended model is:

```text
                    Server.Listen()
                           |
            +--------------+--------------+
            |                             |
  SetupShutdownHandler              BeforeListen
     infrastructure               application startup
            |                             |
            +------------- serve ---------+
                           |
                    request arrives
                           |
                    create Context
                           |
                    BeforeRequest
                           |
                      route method
                           |
                    AfterRequest
                           |
                        repeat
                           |
                   shutdown trigger
                           |
                   BeforeShutdown
                           |
              graceful net/http shutdown
                           |
                   AfterShutdown
```

The request hooks provide a convenient application-wide setup/teardown mechanism, while middleware remains available for more composable HTTP behavior.
