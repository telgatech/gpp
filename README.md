# Go++ v0.2 experiment

A tiny source-to-source compiler that emits ordinary Go.

## Current ideas

- no package declaration => `package main`
- one package declaration per source file
- source files do not need to live in matching package directories
- ordinary Go `import` declarations
- `class` => Go struct + receiver methods
- parent classes => embedded structs
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
- declared, typed annotations with target validation and runtime metadata
- implicit prelude extensions for slices, maps, and strings
- concise lambda expressions such as `users.Any(user => user.Active)` that
  lower to ordinary Go function literals
- compile-time extension methods that lower to ordinary package-level functions
- multi-target extension blocks, such as `extend string, []byte { ... }`
- closed, named-scalar enums with validated `From`, ordered `values`, and
  member metadata such as `Status.Active.name` and `Status.Active.value`

Go++ keeps ordinary `.` and explicit-error behavior compatible with Go. Safe
access is opt-in with `?.`; exceptions remain deferred.

## Build and run

The compiler can still be used as a transpiler:

```bash
go run ./cmd/gpp examples/hello.gpp
cd .gpp
go run .
```

For a complete project build, use the `build` subcommand. It clears stale
generated source, runs `go mod tidy` to resolve imports, and runs `go build`:

```bash
go run ./cmd/gpp build examples/hello.gpp
```

Use `run` for the same generation and dependency setup followed by execution:

```bash
go run ./cmd/gpp run examples/hello.gpp
```

Use a separate output directory when switching between standalone examples:

```bash
go run ./cmd/gpp run -output /tmp/gpp-orm examples/orm.gpp
```

Multiple Go++ files can be passed to the same build. For example, the
cross-package example is built and run as one generated project:

```bash
go run ./cmd/gpp run \
  examples/packages/people.gpp \
  examples/packages/main.gpp
```

The ORM example also resolves its SQLite dependency automatically:

```bash
go run ./cmd/gpp run -output /tmp/gpp-orm examples/orm.gpp
```

Build an executable at a chosen path with `-o`:

```bash
go run ./cmd/gpp build -o ./hello examples/hello.gpp
```

Generated Go is written to `.gpp/`. The CLI creates `.gpp/go.mod` with the
default module path `generated`; choose another path with `-module` when local
Go package imports need a real module path:

```bash
go run ./cmd/gpp -module example.com/myapp examples/*.gpp
```

Choose a different generated output directory with `-output`:

```bash
go run ./cmd/gpp -output build/gpp examples/hello.gpp
```

The standard Go++ prelude is available automatically. Disable it for minimal
or diagnostic builds with `-no-prelude`.

The enum example demonstrates implicit and explicit values, grouped enum
declarations, validated conversion, and metadata:

```bash
go run ./cmd/gpp run examples/enums.gpp
```

Class introspection exposes generated `name`, `fields`, `methods`,
`annotations`, `owner`, `type`, `get`, `set`, and `addr` metadata while
preserving ordinary Go structs and stdlib interoperability.
