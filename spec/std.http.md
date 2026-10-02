# Go++ Feature Spec: Official `gpp` Standard Module

## Goal

Create an official Go++ standard module namespace:

    gpp/...

for higher-level reusable Go++ libraries that should ship with the compiler but should NOT be built into the language core or implicitly loaded everywhere.

The language/compiler remains small.

The official `gpp` module contains reusable classes, extensions, annotations, and helpers implemented primarily in Go++.

## 1. Architectural layers

Go++ should have three distinct layers:

1. Language/compiler

Provides only language/runtime primitives such as:

- classes
- inheritance
- polymorphism
- extension methods
- records
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

The `gpp` standard module must build on ordinary Go and Go++ features rather than replacing the Go ecosystem.

## 2. Official namespace

Reserve:

    gpp/...

for compiler-distributed official Go++ packages.

Examples:

import "gpp/http"
import "gpp/validate"
import "gpp/collections"
import "gpp/test"

Do not use this namespace for user packages.

## 3. Versioning

The official `gpp` module is versioned together with the Go++ compiler/distribution.

Example:

Go++ compiler v0.4.0

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
- HTTP method annotations
- listener annotations
- file-server annotations
- WebSocket integration
- optional auth-related metadata/hooks

Example:

import "gpp/http"

class App : http.Server @{
    http.IP("0.0.0.0"),
    http.Port(8080),
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
    app.Listen()
}

Implementation should use ordinary:

net/http
net.Listener
http.ServeMux
context

where possible.

## 8. Suggested `gpp/http` annotations

Potential declarations:

annotation (
    IP(addr string) on class
    Port(port int) on class
    Unix(path string) on class

    Prefix(path string) on class

    GET(path string) on method
    POST(path string) on method
    PUT(path string) on method
    PATCH(path string) on method
    DELETE(path string) on method

    WebSocket(path string) on method

    Files(path string, dir string) on class

    Auth on class, method
    NoAuth on method
    Role(name string) on class, method
)

These annotations are library metadata.

The compiler itself must not understand their HTTP meaning.

## 9. `gpp/http.Server`

Conceptual API:

class Server {
    func Listen() error
    func Serve(listener net.Listener) error
    func Shutdown(ctx context.Context) error
}

`Listen()` should introspect:

this.class.annotations
this.class.methods
method.annotations

Because inherited methods execute with runtime class identity, `Server.Listen()` sees the actual subclass metadata.

Example:

class App : http.Server @{http.Port(8080)} {
    ...
}

App().Listen()

If neither Port nor Unix is specified, Listen() must bind a TCP listener
on port 0, allowing the operating system to assign an available ephemeral
port. The bound address must be available through Server.HTTPServer.Addr
before BeforeListen() runs.

`Server.Listen()` must discover App routes/configuration.

## 10. `gpp/http.Context`

Keep Context lightweight.

Conceptual:

class Context {
    Response http.ResponseWriter
    Request *http.Request

    func Param(name string) string
    func Query(name string) string

    func Text(value string) error
    func JSON(value any) error
    func Redirect(url string, status int)
}

Do not hide native request/response objects.

Users should always be able to access:

ctx.Request
ctx.Response

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

Prefer implementing official packages in Go++ itself.

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

This dogfoods Go++ and acts as a practical regression suite for the language.

## 15. Native Go escape hatch

Official packages may include ordinary .go files where useful.

Example:

gpp/http/
    server.gpp
    websocket.go

Mixed Go/Go++ implementation should be supported if the existing build architecture permits it.

Prefer Go++ where practical.

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

Go++ semantic package identity for:

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

Official packages use normal Go/Go++ capitalization rules.

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

Official libraries must operate through normal Go++ capabilities:

- inheritance
- annotations
- introspection
- extension methods
- records
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

Go++ language
Go++ prelude
gpp/http
gpp/validate
gpp/collections
gpp/test
Go interoperability

Official packages should serve as examples of idiomatic Go++.

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
    http.Port(8080),
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
        return ctx.JSON(record(
            OK: true,
        ))
    }
}

func main() {
    app := App()
    app.Listen()
}

Underneath:

- HTTP uses net/http
- classes/annotations/introspection come from Go++
- higher-level behavior comes from gpp/http

## 30. Design principle

The official `gpp` standard module should answer:

"What useful things can we build once Go++ gives us better language primitives?"

rather than:

"What more behavior should we bake into the compiler?"

Keep compiler features general.

Build opinionated conveniences in:

gpp/...

Keep native Go packages directly accessible underneath.

The desired architecture is:

Go++ language primitives
        ↓
official gpp libraries
        ↓
native Go standard library/ecosystem

This keeps Go++ productive immediately without creating a closed ecosystem.
