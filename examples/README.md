# Go++ examples

Most standalone `.gpp` files can be compiled with:

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
- `assignments.gpp` — arithmetic, bitwise, and shift compound assignments.
- `foo.gpp` — import and call the companion `foo.bar.gpp` package by its Go++
  logical name; the compiler supplies the generated Go path internally.
- `constructors.gpp` — positional and named construction.
- `generics.gpp` — generic `Stack[T]`, constrained `Index[K comparable, V]`,
  multi-parameter `Pair[A, B]`, explicit type arguments, and methods whose
  fields and signatures use those parameters; also generic free and static
  functions.
- `containers.gpp` — typed `Set`, `Stack`, `Queue`, `Deque`, `List`, and
  comparator-based `PriorityQueue` from the bundled `gpp/container` package.
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
  return types, `let` bindings, and capitalization-based field visibility.
- `introspection.gpp` — runtime class names, parent classes, inherited field
  ownership and access, method and parameter metadata, and annotation lookup.
- `extension_methods.gpp` — extensions on strings and Go++ classes.
- `multi_extension.gpp` — one extension block shared by strings and slices,
  including a generic extension method.
- `enums.gpp` — typed enums, enum blocks, explicit values, lookup, iteration,
  and JSON encoding.
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
  use the active transaction automatically. The example records hook events as
  `Event` models through `orm.Insert(exec, &event)`.
- `prelude.gpp` — implicit slice, string, and map helpers from the Go++ prelude.
- `lambdas.gpp` — expression and block lambdas, contextual typing, explicit
  parameter types, ordinary function arguments, and closure capture.
- `exceptions.gpp` — automatic trailing-error promotion, `throw`, typed,
  multi-type, and catch-all handlers, `finally`, custom Go++ errors, and
  explicit error capture.
- `cron.gpp` — interval and full five-field cron schedules, explicit scheduler
  startup, context-aware jobs, and graceful shutdown.
- `todo-app.gpp` — a server-rendered SQLite todo app with a JSON API, OpenAPI
  and Swagger UI, ORM static query methods with exception-based error handling,
  and a context-aware cron cleanup job.
- `expr_catch.gpp` — expression-level error fallback with lazy fallback
  evaluation and chained `??` operators.
- `testing.gpp` — `test.Suite` lifecycle, inherited assertion helpers, suite,
  tag, and priority discovery through `gpp test`.
- `embed.gpp` — source-level file and directory embedding with rooted `fs.FS`
  and `[]byte` values using standard Go filesystem APIs.
- `tpl.gpp` — typed template declarations, standard `html/template` actions,
  static `tpl.Name` execution, dynamic `tpl.Execute`, path metadata, ad-hoc
  source execution, and registered template functions.
- `tpl_external.gpp.tpl` — an external template source compiled alongside the
  Go++ application template example.
- `serialization.gpp` — opt-in JSON/YAML/GOB serialization, generated class
  methods, renamed fields, ignored fields, and omitted empty fields. YAML uses
  `gopkg.in/yaml.v3`; run `go get gopkg.in/yaml.v3` in the generated module
  before running it.
- `string_interpolation.gpp` — basic and formatted interpolation, raw
  multiline strings, and escaped delimiters for generated template source.
- `regex.gpp` — explicit string-to-regex compilation, native regexp methods,
  direct chaining, and ordinary invalid-pattern error handling.
- `mixed/` — side-by-side compilation in both directions: Go++ calling
  handwritten Go functions and handwritten Go importing generated Go++ code.
- `http.gpp` — the bundled `gpp/http` module with an inherited server,
  annotated HTTP and WebSocket handlers, path parameters, status-aware JSON/text/template responses,
  custom error templates, request context values, lifecycle hooks, graceful
  shutdown, configurable ports, path and hostname mounts for independent route
  classes, and opt-in OpenAPI JSON plus self-contained Swagger UI endpoints
  under the configured prefix.
- `oauth.gpp` — the bundled `gpp/http` OAuth/OIDC login flow with a built-in
  provider, PKCE, callback state, normalized identity, and login/error hooks.

The `foo` dotted-package example is compiled as a pair:

```bash
gpp run examples/foo.gpp examples/foo.bar.gpp
```

The `packages/` example is also compiled as a pair:

```bash
gpp examples/packages/people.gpp examples/packages/main.gpp
(cd .gpp && go run .)
```

It demonstrates logical dotted packages and a qualified cross-package class
constructor. The CLI uses the default `generated` module path; pass `-module`
when the example imports a different generated module path.
