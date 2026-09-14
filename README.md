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

Go++ keeps ordinary `.` and explicit-error behavior compatible with Go. Safe
access is opt-in with `?.`; exceptions remain deferred.

## Run

```bash
go run ./cmd/gopp examples/hello.gpp
cd .gopp
go run .
```

Generated Go is written to `.gopp/`. The CLI creates `.gopp/go.mod` with the
default module path `generated`; choose another path with `-module` when local
Go package imports need a real module path:

```bash
go run ./cmd/gopp -module example.com/myapp examples/*.gpp
```

Choose a different generated output directory with `-output`:

```bash
go run ./cmd/gopp -output build/gopp examples/hello.gpp
```

Class introspection exposes generated `name`, `fields`, `owner`, `type`,
`get`, `set`, and `addr` metadata while preserving ordinary Go structs and
stdlib interoperability.
