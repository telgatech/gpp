# GPP Feature Spec: Official `gpp` Standard Module

## Goal

Create an official GPP standard module namespace:

    gpp/...

for higher-level reusable GPP libraries that should ship with the compiler but should NOT be built into the language core or implicitly loaded everywhere.

The language/compiler remains small.

The official `gpp` module contains reusable classes, extensions, annotations, and helpers implemented primarily in GPP.

## 1. Architectural layers

GPP should have three distinct layers:

1. Language/compiler

Provides only language/runtime primitives such as:

- classes
- inheritance
- polymorphism
- extension methods
- structs
- annotations
- introspection
- lambdas
- operators
- exception support if implemented
- versioned imports

2. Official `gpp` standard module

Provides reusable higher-level libraries such as:

- HTTP server abstractions
- validation
- collection utilities
- testing helpers

3. Native Go ecosystem

Existing Go packages remain directly usable:

- net/http
- database/sql
- encoding/json
- context
- sync
- slices
- maps
- etc.

The `gpp` standard module must build on ordinary Go and GPP features rather than replacing the Go ecosystem.

## 2. Official namespace

Reserve:

    gpp/...

for compiler-distributed official GPP packages.

Examples:

import "gpp/http"
import "gpp/validate"
import "gpp/collections"
import "gpp/test"

Do not use this namespace for user packages.

## 3. Versioning

The official `gpp` module is versioned together with the GPP compiler/distribution.

Example:

GPP compiler v0.4.0

ships with a matching:

gpp standard module v0.4.0

User code does NOT need to specify:

import "gpp/http@v0.4.0"

for built-in official packages.

The compiler automatically resolves bare:

import "gpp/http"

against the standard module bundled with that compiler.

## 4. No network fetch for official `gpp` packages

Official imports:

import "gpp/http"

must resolve locally from the compiler installation/distribution.

Do not fetch official `gpp` packages from GitHub or another network package registry during normal builds.

This provides:

- deterministic builds
- compiler/library compatibility
- offline use
- fast builds

## 5. Prelude relationship

`prelude.gpp` remains special and implicitly loaded.

It is NOT equivalent to:

import "gpp/prelude"

Conceptually:

prelude.gpp
    automatically available everywhere

gpp/*
    explicitly imported standard packages

The prelude should remain tiny.

Do NOT automatically load:

gpp/http
gpp/validate
etc.

## 6. Suggested initial package layout

Recommended:

gpp/
    http/
    validate/
    collections/
    test/

Potential future additions:

gpp/
    cli/
    config/
    cache/
    queue/
    mail/

Only add packages when there is a clear recurring use case.

## 7. `gpp/http`

Purpose:

Provide a lightweight HTTP application layer built on:

net/http

Potential contents:

- Server base class
- Context class
- cookie-aware HTTP client with generic JSON helpers
- HTTP method annotations
- listener annotations
- file-server annotations
- WebSocket integration
- optional auth-related metadata/hooks

Example:

import "gpp/http"

class App : http.Server @{
    http.IP("0.0.0.0"),
    http.Files("/static/", "./public")
} {
    func Home(ctx *http.Context) error @{http.GET("/")} {
        return ctx.Text("hello")
    }

    func User(ctx *http.Context) error @{http.GET("/users/{id}")} {
        ...
    }
}

func main() {
    app := App()
    app.Listen(0)
}

Implementation should use ordinary:

net/http
net.Listener
http.ServeMux
context

where possible.

The HTTP client has moved to the experimental [`gpp/exp/http` package](std.exp.http.md). This specification covers the `gpp/http` server API and annotations.

## 8. Suggested `gpp/http` annotations

Potential declarations:

annotation (
    IP(addr string) on class
    Unix(path string) on class

    Prefix(path string) on class
    Mountable on class

    GET(path string) on method
    POST(path string) on method
    PUT(path string) on method
    PATCH(path string) on method
    DELETE(path string) on method
    Path(name string = "") on parameter
    Query(name string = "") on parameter
    Body on parameter

    WebSocket(path string) on method

    Files(path string, dir string, DirListing bool = false) on class

    Auth on class, method
    NoAuth on method
    Role(name string) on class, method
)

These annotations are library metadata. The `gpp/http` runtime reads the
annotations to register routes; the compiler itself must not implement their
HTTP behavior.

## 9. `gpp/http.Server`

Conceptual API:

class Server {
    PublicDir string
    DirListing bool
    PublicCacheControl string
    func Listen(port int) error
    func Serve(listener net.Listener) error
    func Mount(mountable any, addresses ...string) error
    func Shutdown(ctx context.Context) error
}

`Listen(port)` binds the TCP listener to the supplied port. Passing `0` asks
the operating system to choose an available port. The `IP` annotation can
still select the local interface, while `Unix` selects a Unix-domain socket
and requires `Listen(0)`.

When `Server.PublicDir` is non-empty, matching files are served from that
directory at `/` before `Handle` or route dispatch. `http.Files(path, dir)`
annotations add prefix-based static mounts. All configured static mounts are
checked before `Handle`, ordered by longest matching prefix first. Only GET
and HEAD requests are served as files. If no file matches, normal handler and
route dispatch continues. An empty `PublicDir` disables its root mount.
Directory requests prefer `index.html`, then `index.htm`, at each directory
level. Directory listings are disabled by default; when an index is absent,
the request continues to `Handle` and route dispatch. Set `Server.DirListing`
to enable listings for `PublicDir`, or pass `DirListing: true` to an
`http.Files` annotation to enable them for that mount.

Public files use `Cache-Control: public, max-age=300` when
`PublicCacheControl` is empty. The standard file server's `Last-Modified`
conditional requests remain enabled. Compressible responses use gzip when
requested by the client; range responses and already encoded content are not
compressed. `PublicCacheControl` overrides the default cache policy for both
`PublicDir` and `http.Files` mounts.

`Mount` accepts an instance of a GPP class marked with the `Mountable`
annotation and one or more mount addresses. Its methods use the same HTTP
method annotations as a `Server` subclass. `Mountable` is a marker; classes do
not inherit from a router base type. An address may be a path prefix such as
`/admin`, a hostname such as `admin.example.com`, or a hostname followed by a
path. A leading slash identifies a path mount. Hostname mounts match the
request host; the listener address and port remain configured on the server.

The server's `Prefix` is applied before each mount path and the mountable
class's own `Prefix`; route annotation paths are appended after those prefixes.
A mountable class may be mounted at multiple addresses. Mounting must happen before `Serve` or
`Listen`. The server's request hooks and error handler apply to mounted routes.
Route-pattern conflicts must return an error from `Serve` instead of panicking.

Methods annotated with `http.WebSocket(path)` register GET endpoints in the
same route table. The handler receives the ordinary `*Context` with its
`Conn` field set after the WebSocket handshake succeeds. See the
[WebSocket specification](./std.http.websocket.md) for protocol integration,
message helpers, and connection lifecycle behavior.

OpenAPI and OAuth route generation currently use annotations on the server
class itself. Mounted class methods are routed normally but are not included
in the server's generated OpenAPI document.

`Listen(port)` should introspect:

this.class.annotations
this.class.methods
method.annotations

Because inherited methods execute with runtime class identity, `Server.Listen(port)` sees the actual subclass metadata.

Example:

    ...
}

App().Listen(0)

If Unix is not specified, Listen(port) must bind a TCP listener on the
supplied port. A port value of 0 allows the operating system to assign an
available ephemeral port. The bound address must be available through Server.HTTPServer.Addr
before BeforeListen() runs. After the listener is ready, log its clickable
`http://` URL for TCP listeners, including the actual assigned port; Unix-domain
listeners should log their socket path without an HTTP scheme. If a TCP listener
is bound to all interfaces, log `localhost` as the clickable host instead of an
unspecified address such as `[::]` or `0.0.0.0`.

`Server.Listen(port)` must discover App routes/configuration.

Every request passes through the server's `Handle(*Context) error` method before
route dispatch. The default implementation returns `nil`. If `Handle` writes a
response, route dispatch is skipped; otherwise the annotated, mounted, or
generated routes are tried. This allows `Handle` to serve requests procedurally
without declaring annotated routes, while also allowing routes to act as
fallbacks. Returning an error stops dispatch and sends it through the server
error handler. The same Context is passed to the selected route, and
`BeforeRequest` and `AfterRequest` run once around the request.

For ordinary HTTP route methods, the first parameter is `*http.Context`.
Additional parameters must each have exactly one request-binding annotation:
`http.Path`, `http.Query`, or `http.Body`. Path and query annotations accept an
optional source-name override and otherwise use the GPP parameter name. Path
names must match wildcards in the route path. Path values are required; missing
path values and conversion failures produce a 400
response through the configured error handler. Missing query values remain the
parameter type's zero value; repeated query values may bind to slice parameters.
The `Body` annotation decodes a struct or class parameter using the same
content-type-aware behavior and 1 MiB limit as `Context.Bind`. Binding errors
also produce a 400 response. WebSocket handlers continue to receive only their
context parameter; these binding annotations do not apply to upgraded routes.

## 10. `gpp/http.Context`

Keep Context lightweight.

Conceptual:

class Context {
    Response http.ResponseWriter
    Request *http.Request
    Conn *websocket.Conn

    func Param(name string) string
    func Query(name string) string
    func Bind(target any) error
    func BindQuery(target any) error
    func BindJSON(target any) error
    func BindForm(target any) error

    func Text(value string) error
    func JSON(value any) error
    func HTML(status int = 200, data any = nil, key string = "") error
    func Respond(status int, data any) error
    func Redirect(url string, status int)
}

`Respond` negotiates between JSON and HTML using the request's `Accept` header.
`text/html` selects HTML and renders the template keyed by the request path.
JSON is the default when `Accept` is absent or when JSON and HTML have equal
quality. A request that accepts neither supported representation receives
`406 Not Acceptable`. `Respond` adds `Vary: Accept`; callers that require one
specific representation should use `JSON` or `HTML` directly.

Do not hide native request/response objects.

Users should always be able to access:

ctx.Request
ctx.Response

`Bind(target)` selects query binding for GET and HEAD requests, JSON decoding for
JSON request bodies, and form binding for URL-encoded and multipart form bodies.
`BindQuery`, `BindJSON`, and `BindForm` select a source explicitly. Bind targets
must be non-nil pointers to structs. Query and form values map to exported
fields using the corresponding `query` or `form` struct tag; a `json` tag is
the fallback, followed by the lower-camel field name. GPP class fields use
their lower-camel names; native Go struct targets may use tags. Repeated
values populate slice fields. JSON and form request bodies are limited to 1
MiB. Binding errors are returned to the handler; validation remains the
application's responsibility. These methods decode into an explicitly supplied
target. Route methods can instead opt into argument binding with the
`http.Path`, `http.Query`, and `http.Body` parameter annotations described in
the server section.

`Conn` is non-nil only while a method annotated with `http.WebSocket` is
handling an upgraded request. See the [WebSocket specification](./std.http.websocket.md)
for the connection API and lifecycle.

## 11. `gpp/validate`

Purpose:

Generic annotation-driven validation.

Potential declarations:

annotation (
    Required on field, parameter
    Email on field, parameter
    Min(value int) on field, parameter
    Max(value int) on field, parameter
    Length(value int) on field, parameter
)

Potential API:

validate.Check(obj)

or extension-based helpers.

Implementation should use:

.class
.fields
.annotations

No validator semantics belong in the compiler.

## 12. `gpp/collections`

Purpose:

Larger or less universal collection algorithms that are useful but too large for prelude.gpp.

Prelude may contain only very common helpers such as:

Any
All
Find
Filter
Sort
Reverse
Contains
Index

`gpp/collections` may later contain:

Distinct
Chunk
GroupBy
Zip
Partition
Flatten
etc.

Do not move native collections into wrappers.

Continue operating on:

[]T
map[K]V

## 13. `gpp/test`

Purpose:

Testing conveniences while staying compatible with Go's testing package.

Potential:

import "gpp/test"

but implementation should still build on:

testing

Do not create a completely separate test runner unless necessary.

Potential helpers:

assertions
fixtures
small test utilities

## 14. Standard-module source language

Prefer implementing official packages in GPP itself.

Example layout:

stdlib/
    prelude.gpp

    gpp/
        http/
            server.gpp
            context.gpp
            annotations.gpp

        validate/
            validate.gpp
            annotations.gpp

        collections/
            collections.gpp

        test/
            test.gpp

This dogfoods GPP and acts as a practical regression suite for the language.

## 15. Native Go escape hatch

Official packages may include ordinary .go files where useful.

Example:

gpp/http/
    server.gpp
    websocket.go

Mixed Go/GPP implementation should be supported if the existing build architecture permits it.

Prefer GPP where practical.

## 16. Compiler lookup behavior

When encountering:

import "gpp/http"

resolution order should be:

1. official standard module shipped with compiler
2. error if unavailable

Do NOT search external dependency sources for official `gpp/*` paths.

For:

import "github.com/foo/bar@v1.2.3"

continue using normal Go module resolution.

## 17. Generated Go module integration

Official gpp imports may lower to generated/internal package paths as necessary.

User-facing:

import "gpp/http"

Generated Go may use an implementation-specific module path such as:

import "gpp.internal/http"

or another compiler-controlled path.

The user should not need to know this.

Compiler should rewrite official package paths during lowering if required.

## 18. Package identity

GPP semantic package identity for:

gpp/http

must be stable and canonical regardless of generated Go path.

This matters for:

- annotations
- class descriptors
- symbol identity
- extension resolution

Example:

http.GET

must resolve to the official annotation symbol from gpp/http.

## 19. Visibility

Official packages use normal Go/GPP capitalization rules.

Example:

package http

class Server ...
class Context ...

annotation (
    GET(path string) on method
    internalDebug on method
)

Only exported identifiers are visible to applications.

## 20. No compiler magic for library APIs

The compiler must not contain special cases such as:

if package == "gpp/http" and annotation == "GET" ...

Official libraries must operate through normal GPP capabilities:

- inheritance
- annotations
- introspection
- extension methods
- structs
- generics
- Go interop

This is a key design requirement.

## 21. Standard library replaceability

Users must be able to ignore official packages and use alternatives.

Example:

instead of gpp/http:

import "github.com/example/myweb"

No language feature should depend on using official packages.

## 22. Documentation

Treat official gpp packages as the recommended getting-started experience.

Suggested docs structure:

GPP language
GPP prelude
gpp/http
gpp/validate
gpp/collections
gpp/test
Go interoperability

Official packages should serve as examples of idiomatic GPP.

## 23. Prelude boundary

Keep this distinction strict:

prelude.gpp:
    implicit and universal

gpp/http:
    explicit import

gpp/validate:
    explicit import

gpp/collections:
    explicit import if functionality exceeds prelude

When uncertain, prefer explicit `gpp/...` package over expanding prelude.

## 24. Distribution layout

Suggested compiler installation:

<gpp-root>/
    bin/
        gpp

    lib/
        prelude.gpp

        gpp/
            http/
            validate/
            collections/
            test/

Compiler should know its standard-library root.

## 25. Standalone source builds

A single source file can still import:

import "gpp/http"

without requiring a manually created go.mod or downloaded dependency.

Compiler handles official standard-module resolution automatically.

## 26. `--no-stdlib`

Optional useful compiler switch:

gpp build --no-stdlib

Meaning:

- disable official gpp standard module lookup

Keep prelude control separate:

--no-prelude

Normal users should never need these.

## 27. Testing strategy

The official standard module should be part of the compiler's integration test suite.

Tests should prove:

- gpp/http can build on net/http
- gpp annotations introspect correctly
- extension methods work on native Go types
- inheritance works through standard-module classes
- runtime class identity works through inherited Server methods
- standard packages require no external network fetch

## 28. Initial milestone

Recommended first official packages:

1. prelude.gpp
2. gpp/http

Then add:

gpp/validate
gpp/collections
gpp/test

only when useful.

`gpp/http` is a strong real-world proof because it exercises:

- class inheritance
- method annotations
- class annotations
- introspection
- native Go interop
- runtime class identity

## 29. Example complete app

import (
    "gpp/http"
)

class App : http.Server @{
    http.Files("/static/", "./public")
} {
    func Home(ctx *http.Context) error @{
        http.GET("/")
    } {
        return ctx.Text("hello")
    }

    func Health(ctx *http.Context) error @{
        http.GET("/health")
    } {
        return ctx.JSON(struct(
            OK: true,
        ))
    }
}

func main() {
    app := App()
    app.Listen(0)
}

Underneath:

- HTTP uses net/http
- classes/annotations/introspection come from GPP
- higher-level behavior comes from gpp/http

## 30. Design principle

The official `gpp` standard module should answer:

"What useful things can we build once GPP gives us better language primitives?"

rather than:

"What more behavior should we bake into the compiler?"

Keep compiler features general.

Build opinionated conveniences in:

gpp/...

Keep native Go packages directly accessible underneath.

The desired architecture is:

GPP language primitives
        ↓
official gpp libraries
        ↓
native Go standard library/ecosystem

This keeps GPP productive immediately without creating a closed ecosystem.
