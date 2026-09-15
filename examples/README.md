# Go++ examples

Each standalone `.gpp` file can be compiled with:

```bash
go run ./cmd/gpp examples/<name>.gpp
(cd .gpp && go run .)
```

The examples cover:

- `hello.gpp` — classes, methods, implicit `this`, imports, and interpolation.
- `foo.gpp` — an ordinary package declaration and generated package directory.
- `constructors.gpp` — positional and named construction.
- `inheritance.gpp` — multiple inheritance, embedded parents, and qualified
  parent access.
- `imports.gpp` — ordinary grouped Go imports and aliases.
- `overloading.gpp` — method/function overloading, typed overloads, named calls,
  and default parameters.
- `polymorphism.gpp` — base-typed dispatch, polymorphic fields, and a
  class-valued function result passed directly to a base-typed function.
- `safe_access.gpp` — explicit `?.` safe access for nullable class values.
- `records.gpp` — anonymous structural records, nested records, inferred
  return types, and `let` bindings.
- `introspection.gpp` — runtime class names, inherited field metadata, and field access.
- `extension_methods.gpp` — extensions on strings and Go++ classes.
- `multi_extension.gpp` — one extension block shared by strings and slices,
  including a generic extension method.
- `annotations.gpp` — declared annotations, target restrictions, field/method
  usage, and annotation introspection.

The dotted-package example is compiled as a pair:

```bash
go run ./cmd/gpp examples/packages/people.gpp examples/packages/main.gpp
(cd .gpp && go run .)
```

It demonstrates logical dotted packages and a qualified cross-package class
constructor. The CLI uses the default `generated` module path; pass `-module`
when the example imports a different generated module path.
