# Go++ examples

Each standalone `.gpp` file can be compiled with:

```bash
gpp examples/<name>.gpp
(cd .gpp && go run .)
```

For a complete build or run, use the CLI subcommands. They clear stale
generated source and resolve Go dependencies automatically:

```bash
gpp build examples/hello.gpp
gpp run examples/hello.gpp
```

The examples cover:

- `hello.gpp` — classes, methods, implicit `this`, imports, and interpolation.
- `foo.gpp` — an ordinary package declaration and generated package directory.
- `constructors.gpp` — positional and named construction.
- `static_methods.gpp` — class-qualified factory and parsing methods.
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
- `annotation_inheritance.gpp` — direct class annotations, inherited field and
  method metadata, original owners, parameter annotations, and unannotated
  overrides.
- `orm.gpp` — a mini in-memory SQLite app using the bundled `gpp/orm` package,
  annotated models, runtime metadata inspection, model validation, lifecycle
  hooks, and ordinary `database/sql`, including `Get` for one model and
  `Select` for model slices on both `*sql.DB` and `*sql.Tx`.
  It uses the pure-Go `modernc.org/sqlite` driver; `gpp build` and `gpp run`
  resolve it through `go mod tidy`.
  Query filters use numbered `$1`, `$2`, ... placeholders shared by SQLite and
  PostgreSQL. Lifecycle hooks receive a shared `SQLExecutor`, so child writes
  use the active transaction automatically.
- `prelude.gpp` — implicit slice, string, and map helpers from the Go++ prelude.
- `lambdas.gpp` — expression and block lambdas, contextual typing, explicit
  parameter types, ordinary function arguments, and closure capture.
- `exceptions.gpp` — automatic trailing-error promotion, `throw`, typed and
  catch-all handlers, `finally`, custom Go++ errors, and explicit error capture.
- `expr_catch.gpp` — expression-level error fallback with lazy fallback
  evaluation and chained `??` operators.
- `testing.gpp` — `test.Suite` lifecycle, inherited assertion helpers, suite,
  tag, and priority discovery through `gpp test`.
- `embed.gpp` — source-level file and directory embedding with rooted `fs.FS`
  and `[]byte` values using standard Go filesystem APIs.
- `tpl.gpp` — typed template declarations, standard `html/template` actions,
  static `tpl.Name` execution, dynamic `tpl.Execute`, and path metadata.
- `tpl_external.gpp.tpl` — an external template source compiled alongside the
  Go++ application template example.
- `serialization.gpp` — opt-in JSON/YAML/GOB serialization, generated class
  methods, renamed fields, ignored fields, and omitted empty fields. YAML uses
  `gopkg.in/yaml.v3`; run `go get gopkg.in/yaml.v3` in the generated module
  before running it.
- `http.gpp` — the bundled `gpp/http` module with an inherited server,
  annotated routes, path parameters, JSON responses, request context values,
  lifecycle hooks, graceful shutdown, and a configurable port.

The dotted-package example is compiled as a pair:

```bash
gpp examples/packages/people.gpp examples/packages/main.gpp
(cd .gpp && go run .)
```

It demonstrates logical dotted packages and a qualified cross-package class
constructor. The CLI uses the default `generated` module path; pass `-module`
when the example imports a different generated module path.
