# Go++, an attempt to add modern features to the Go Programming Language

A superset of Go that provides modern features while staying true to the spirit of Go language including offering full compatibility and side by side compilation.

## What's implemented?

- no package declaration => `package main`
- one package declaration per source file
- source files do not need to live in matching package directories
- ordinary Go `import` declarations
- `class` => Go struct + receiver methods
- class static methods and factories
- single and multiple inheritance => embedded structs, with qualified access
  when inherited members are ambiguous
- `Person("Bob", 42)` => `Person{Name: "Bob", Age: 42}`
- named construction arguments such as `Person(Name: "Bob", Age: 42)`
- `"Hello {{expr}}"` interpolation => `fmt.Sprintf(...)`
- arity-based overloading for functions and class methods
- initial interface-based polymorphism for class-typed variables, parameters,
  results, and fields
- named/default function arguments
- exact static-type overloads such as `Format(1)` and `Format("value")`
- known class-valued function and method results flow into base-typed calls
- explicit `?.` safe access for nullable class values
- anonymous structural records with deterministic generated Go structs
- compile-time-generated class metadata through `obj.class` and `Class.fields`
- declared, typed annotations with target validation, inherited metadata, and
  runtime introspection
- implicit prelude extensions for slices, maps, and strings
- concise lambda expressions such as `users.Any(user => user.Active)` that
  lower to ordinary Go function literals
- compile-time extension methods that lower to ordinary package-level functions
- multi-target extension blocks, such as `extend string, []byte { ... }`
- closed, named-scalar enums with validated `From`, ordered `values`, and
  member metadata such as `Status.Active.name` and `Status.Active.value`
- expression-level error fallback with lazy, short-circuiting `A ?? B`
- source-level `embed` declarations for rooted `fs.FS` directories and
  embedded `[]byte` files
- typed template declarations using standard `html/template` syntax, layout
  inheritance, static and dynamic execution, and ad-hoc templates through
  `gpp/tpl`
- external `.gpp.tpl` sources, with development-time reloads for
  `gpp/http.Server`
- mixed Go and Go++ builds in both directions
- generated JSON, YAML, and GOB serialization with field-name, ignore, and
  omit-empty annotations
- bundled `gpp/http`, `gpp/orm`, and `gpp/test` packages for HTTP servers,
  database access, and suite-based tests; HTTP support includes OpenAPI,
  Swagger UI, and OAuth/OIDC
- Go++-aware formatting, source documentation, diagnostics, and LSP support

### Exception handling

Go++ uses Go's `error` interface for exceptions. If a call returns a trailing
`error` and the caller omits that result, Go++ throws it automatically when it
is non-nil. Capture the error explicitly to keep ordinary Go error-value
handling:

```go
data := os.ReadFile("config.json") // throws on error
data, err := os.ReadFile("config.json") // or capture err as an ordinary Go value
```

Use `throw` to raise any value that implements `error`. `catch` clauses can
match one error type, several error types, or all errors. A catch binding gives
the handler access to the matched error. `finally` runs after the `try` whether
it completes, throws, or returns:

```go
try {
    data := os.ReadFile("config.json")
    fmt.Println(len(data))
} catch *os.PathError e {
    fmt.Println("missing:", e.Path)
} catch e {
    fmt.Println("failed:", e)
} finally {
    fmt.Println("finished")
}
```

Go++ catches only errors thrown explicitly or promoted from omitted trailing
`error` results; ordinary Go panics are not converted into catchable errors.

Go++ keeps ordinary `.` and explicit-error behavior compatible with Go. Safe
access is opt-in with `?.`; structured exception syntax lowers to Go-compatible
error handling.

## Install and build

The repository root is the installable Go++ CLI:

```bash
go install github.com/telgatech/gpp@latest
```

It provides one `gpp` executable with project commands:

```text
gpp init|compile|build|run|clean|fmt|test|doc|env|doctor|version|lsp
```

Create and run a starter project:

```bash
gpp init hello
cd hello
gpp run .
```

`gpp init` creates a root `go.mod`, runs `go mod tidy`, and initializes a Git
repository with an `init` commit when the target is not already inside a Git
worktree. The initial commit includes only the generated project files.

Build a native executable. Intermediate Go source remains in the hidden
compiler workspace rather than beside the Go++ source:

```bash
gpp build examples/hello.gpp -o ./hello
```

Use `gpp doctor` to check the Go toolchain and embedded standard library, and
`gpp clean` to remove generated build artifacts.

Format Go++ source with the shared canonical formatter. It uses four-space
indentation, preserves comments and opaque literal/template content, and
supports CI-friendly check mode:

```bash
gpp fmt examples/hello.gpp
gpp fmt --check ./...
gpp fmt --stdout examples/hello.gpp
```

Pass `-emit-go` to `gpp build` when you want the generated Go workspace path
reported for inspection.

Go++ tests use the bundled `gpp/test` suite library and ordinary Go test
execution underneath:

```bash
gpp test examples/testing.gpp
gpp test --tag crud examples/testing.gpp
gpp test --priority high examples/testing.gpp
```

Inspect Go++ source-level documentation without exposing generated Go:

```bash
gpp doc gpp/http.Server
gpp doc string.TrimSpace
gpp doc --json gpp/http.Server
gpp doc --search template
```

Start the editor language server over standard input/output:

```bash
gpp lsp
```

The server keeps open-document overlays in memory and provides diagnostics,
completion, hover, navigation, references, rename, symbols, formatting, and
signature help without running a Go backend build on every change. Use
`gpp lsp --log=/tmp/gpp-lsp.log` when protocol-side diagnostics are needed;
logs never go to stdout.

## Documentation site

The technical documentation is built with VitePress. It includes a practical
landing page, getting-started and language guides, compiler/tooling notes, and
the specifications mirrored from `spec/` at build time:

```bash
npm install
npm run docs:dev
```

Build and preview the static site locally:

```bash
npm run docs:build
npm run docs:preview
```

Pushes to `main` build and deploy the site through GitHub Pages using the
workflow in `.github/workflows/docs.yml`. In the repository settings, set
Pages → Build and deployment → Source to **GitHub Actions** once.

## Build and run

The compiler can still be used as a transpiler:

```bash
gpp examples/hello.gpp
cd .gpp
go run .
```

For a complete project build, use the `build` subcommand. It clears stale
generated source, runs `go mod tidy` to resolve imports, and runs `go build`:

```bash
gpp build examples/hello.gpp
```

Use `run` for the same generation and dependency setup followed by execution:

```bash
gpp run examples/hello.gpp
```

During `gpp run`, external `.gpp.tpl` sources are watched when used with
`gpp/http.Server`; valid edits reload automatically and invalid edits leave
the previous templates active. Production `gpp build` uses the compiled
template sources without starting a watcher.

Use a separate output directory when switching between standalone examples:

```bash
gpp run -output /tmp/gpp-orm examples/orm.gpp
```

Multiple Go++ files can be passed to the same build. For example, the
cross-package example is built and run as one generated project:

```bash
gpp run \
  examples/packages/people.gpp \
  examples/packages/main.gpp
```

Go++ and Go can also live in the same build. The mixed-compilation examples
demonstrate both directions:

```bash
gpp run -output /tmp/gpp-go-from-gpp examples/mixed/go_from_gpp
gpp run -output /tmp/gpp-go-imports-gpp examples/mixed/go_imports_gpp
```

The first lets Go++ call functions from `native.go`; the second lets ordinary
Go import the generated `generated/mixed` package.

The ORM example also resolves its SQLite dependency automatically:

```bash
gpp run -output /tmp/gpp-orm examples/orm.gpp
```

Build an executable at a chosen path with `-o`:

```bash
gpp build -o ./hello examples/hello.gpp
```

Generated Go is written to `.gpp/`. The CLI creates `.gpp/go.mod` with the
default module path `generated`; choose another path with `-module` when local
Go package imports need a real module path:

```bash
gpp -module example.com/myapp examples/*.gpp
```

Choose a different generated output directory with `-output`:

```bash
gpp -output build/gpp examples/hello.gpp
```

The standard Go++ prelude is available automatically. Disable it for minimal
or diagnostic builds with `-no-prelude`.

The enum example demonstrates implicit and explicit values, grouped enum
declarations, validated conversion, and metadata:

```bash
gpp run examples/enums.gpp
```

Expression-level error fallback is demonstrated by:

```bash
gpp run examples/expr_catch.gpp
```

Class introspection exposes generated `name`, `fields`, `methods`,
`annotations`, `owner`, `type`, `get`, `set`, and `addr` metadata while
preserving ordinary Go structs and stdlib interoperability.
