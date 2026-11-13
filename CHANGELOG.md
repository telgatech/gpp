# Changelog

## 2026-11-13

- Added `http.Server.TemplateFiles` for live development template reloads and one-time parsing when `ENV=production`; `Templates` and `TemplateFiles` are mutually exclusive.
- Added an optional `http.Server.Name` for identifying server startup, SSE, and WebSocket logs.
- Added automatic HTTP response finalization from map-backed `Context.Data`, with registered HTML views available as GET/HEAD fallbacks when no explicit route matches.
- Extended overload lowering to native Go declarations, including GPP receiver types inferred through helper returns and nested function literals; bumped compiler and build cache versions to regenerate existing output.
- Made backtick strings literal so Go template delimiters and other raw content pass through unchanged; double-quoted strings retain GPP interpolation.
- Added `http.Server.Templates` for standard Go `html/template` sets, including named `define` execution and `{id}` path-pattern view lookup through `Context.HTML`.
- Fixed trailing-error promotion for calls nested in `go` statement arguments; argument errors propagate before the goroutine starts while the launched function's own error remains discarded.
- Added the general `regexp(pattern)` builtin and regex matching in tagged switches; literal switch patterns compile once per source file and are reused, while dynamic patterns compile when their case is checked.
- Consolidated regex demonstrations into `examples/regexp.gpp`, including `CompileRegex()`, native `regexp` selectors, and regex switch cases.
- Removed legacy `record(...)` construction and `record` collection markers in favor of anonymous struct syntax.

## 2026-11-12

- Improved native Go field and method navigation in template-based GPP files, and showed documentation for Go struct fields on hover.
- Added lazy, right-associative conditional expressions with the `condition ? value : fallback` syntax.
- Corrected generated source mappings for exception runtime and boundary frames so panic stack traces no longer point to unrelated GPP lines.

## 2026-11-10

- Raised the minimum Go version to 1.27 and enabled generic methods declared directly on classes; generated workspaces now use Go 1.27 and invalidate older cached output.
- Extended method overload resolution to distinguish generic and non-generic overloads through explicit type arguments, including methods with trailing defaults.
- Added inferred `map(...)` literals to the prelude, with strongly typed homogeneous maps, `any` fallback for mixed values, and generic `Each`, `Get`, `Keys`, `Values`, and `Delete` helpers. `Get(key)` returns `(value, found)`; `Get(key, fallback)` returns the fallback when absent.
- Added the experimental `gpp/exp/http.Client` with base URL and timeout constructor arguments, cookie persistence, overloaded raw and generic JSON `Get`/`Post` methods, optional request contexts, typed anonymous-struct form and upload fields, and streamed multipart file uploads.
- Added `http.Context.Respond(status, data)` to negotiate HTML or JSON from `Accept`, with JSON as the fallback and `406 Not Acceptable` for unsupported formats.
- Added per-instance `gpp/cron.Cron` schedulers that discover annotated class methods through generic runtime metadata and bind typed invokers to each instance.

## 2026-11-09

- Renamed full-SQL ORM model query helpers to `orm.Get[T]` and `orm.Select[T]`.
- Kept the ORM example focused on model operations, with a joined full-SQL model query; generic scalar and struct SQL queries are demonstrated in `examples/sql.gpp`.
- Added generic insertion-ordered maps and circular rings to `gpp/container`.
- Added Homebrew install and update instructions, and changed the first-program guide to start with `gpp init`.
- Updated BSD-3-Clause attribution to name Telga Technologies Pvt Ltd, aligned the docs footer, and clarified source redistribution notices in the README.
- Expanded prelude string helpers with Unicode code-point reversal, cut/trim/split/search operations, rune transforms, string joining, fallback-based string parsing, and scalar-to-string formatting.
- Added anonymous `struct(...)` values, short `struct Name { ... }` declarations, and named struct construction with `Name(...)`; retained compiler-internal shape metadata for static inference.
- Improved prelude extension lowering in expression and block lambdas and nested anonymous struct fields; backend diagnostics no longer expose generated helper names.
- Fixed LSP member completion after generic callback calls such as `Map(value => value.ToUpper()).`.
- Added whole-source compiler fuzzing alongside regression fuzzers for callback lowering and inherited default-method calls.
- Fixed inherited static method lowering when calls pass multiple values to a variadic parameter, including ORM model `Select` queries.
- Added `examples/sql.gpp` to demonstrate typed full-SQL queries through `gpp/sql` and shared execution across database handles and transactions.
- Updated managed incremental build workspaces when the project module path changes, avoiding stale generated `go.mod` mismatches.
- Fixed generic extension lowering and callback overload selection inside `try` blocks, including inline `.Each(...)` calls.
- Specified and fixed implicit error promotion for generic imported calls with multiple variadic arguments; bumped the build-cache schema so compiler updates regenerate stale Go output.

## 2026-11-08

- Expanded LSP editor support with semantic token range and delta updates, inlay hints, selection ranges, code lenses, signature help, inline completions, and range/on-type formatting.
- Added workspace-aware GPP and native Go hover, completion, navigation, references, rename, type/call hierarchy, and implementation support across files and packages.
- Fixed member completions for class constructor call results, including inherited methods when completion is triggered immediately after the selector dot.
- Enforced lowercase class-member privacy across inheritance and completions; made HTTP shutdown/watcher state private, limited imported-package completions to exported symbols, and matched generic slice/map extensions.
- Limited constructor-field completions to open constructor argument lists, preserving member completions after a completed constructor call.
- Added completion for exported fields and methods on native Go values returned by imported Go functions, such as `sql.Open`.
- Added the `std.Hook` method annotation to hide framework callback methods from completion lists while keeping them callable and overridable.
- Restricted direct GPP calls to annotated hooks to the class that first declares each hook; framework dispatch and overrides remain supported.
- Moved ORM lifecycle hook ordering into `Model.Insert`, `Update`, and `Destroy`; a required interface dispatch preserves child overrides, and SQL helpers now only perform persistence.
- Refactored the ORM/SQL boundary so `gpp/orm` depends on the shared executor in `gpp/sql`; full-SQL model queries use `orm.Get`/`orm.Select`.
- Improved LSP code actions and diagnostics, including generated-Go type errors, pull diagnostics, watched-file and workspace-folder updates, and request cancellation.
- Fixed formatter spacing around inheritance colons in class declarations and collapsed repeated blank lines to match gofmt.
- Enforced class member visibility across selectors, composite literals, templates, extensions, constructors, and Go declarations embedded in `.gpp`: uppercase is public, lowercase is private, and a leading underscore marks protected members visible to subclasses. Updated generated Go names and LSP completions.
- Improved LSP navigation and completion for imported Go methods and types, chained call results, callback lambdas, and inferred return bindings that preserve GPP error promotion.

## 2026-11-07

- Fixed resolution of module-local package references during inheritance analysis, including transitive parents and method types from their declaring packages. This works across same-directory packages, conventional directory layouts, and multi-file dotted packages.
- `gpp fiximports` now checks current-module GPP packages before bundled and standard packages, matches candidates by exported symbol, and adds imports to an existing grouped import block.
- Go-compatible source formatting now follows Go tab indentation and trims blank lines at block edges; `gpp init` starters use the same formatting.
- `gpp build`, `gpp run`, and `gpp test` now use the current project's `go.mod` module path by default, improving resolution of local GPP packages.
- Fixed constructor lowering across package boundaries, including inherited fields and method calls with default arguments directly after construction.
- Simplified ORM model declarations: fields map to snake_case columns and classes map to snake_case table names by default. Use `orm.Column` and `orm.Table` to override names, and `orm.Ignore` to omit non-database fields. Schema-qualified table names such as `sales.employees` are supported.
- ORM models now infer the primary key from a persisted `Id` or `ID` field when no `orm.PK` is specified. Keep `orm.PK` for custom key names or to select among multiple candidates.
- Updated the ORM and todo examples to demonstrate inferred mappings, with explicit annotations shown as comments in the todo model.
- Added support for block comments between class header tokens, including between a parent class and the opening brace; compiler and LSP diagnostics now accept this syntax.
- Updated todo validation and normalization to use the GPP string methods `IsBlank()` and `TrimSpace()`.

## 2026-11-06

- Added `ctx.HTML` for HTML responses from named, path-matched, or inline templates; `ctx.Template` remains as an obsolete alias.
- Added class `init()` hooks that run after constructor fields are populated, in base-to-derived order; initializer errors participate in normal error promotion.
- Added the generic return-value form `ctx.Bind[T]()` alongside the existing destination-based binding. It supports GPP classes, Go structs, and structural records, including pointer result types; query, JSON, and form input are selected from the request method and content type.
- Added `http.Server.Handle(ctx) error` as a procedural handler; a response skips route dispatch, while an unwritten response falls through to annotated routes (if any).
- Added `http.Server.PublicDir` for root static files and made `http.Files` mounts run before procedural handlers and routes. Both prefer `index.html` then `index.htm`, disable directory listings by default, and use a five-minute cache policy with gzip for compressible responses.
- Simplified `http.Context.Template` to one call with optional status, data, and key arguments; an omitted key uses the request path.
- Split generic typed SQL queries into the new `gpp/sql` package; model mapping and model operations remain in `gpp/orm`.
- `http.Server.Listen(port)` takes the TCP port directly; its default of `0` lets the OS choose an available port. This replaces the `http.Port` annotation.
- Fixed automatic exception promotion for ignored errors returned by lowered calls, including extension calls with callback overloads; added runtime regression coverage.

## 2026-11-05

- Generic ORM queries support pointer-shaped results for models, inline records, and scalar types. Pointer-wrapped inline records are lowered correctly as generic type arguments.
- Generic ORM `Get[T]` and `Select[T]` queries for models, scalars, and inline records.
- Inherited ORM model helpers for find, select, ordering, cursor paging, truncation, and instance persistence.
- Static `self` typing for inherited class methods.
- ORM model writes use instance methods such as `employee.Insert(db)`, `employee.Update(db)`, and `employee.Destroy(db)`.
- Destination-based ORM query overloads are deprecated in favor of generic result types.
- Error/Exception promotion for generic and imported calls that return a trailing error.
- ORM instance forwarding so inherited model operations use the concrete model metadata.
- Source locations and paths in compiler diagnostics.

## 2026-11-04

- Improved record type inference and source mapping for errors from imported functions.
- Added compiler warnings, including an opt-in flag for warnings during `gpp build`; unused Go variables are removed from generated code where possible.
- `gpp run` accepts `.go` files and still processes them through GPP before running.
- Added incremental build caching and a gradual-adoption FAQ explaining how GPP builds on Go.

## 2026-10-23

- Added `gpp fiximports` to resolve package imports and name clashes.
- Fixed dynamically executed templates to retain package scope and preserved Go build constraints when copying native Go files.

## 2026-10-20

- Improved HTTP listener output with clickable local URLs.

## 2026-10-19

- Added annotation-based request binding and user-management endpoints to the HTTP standard library, with improved server security.

## 2026-10-07

- Clarified the language’s experimental status and Go compatibility in the documentation.

## 2026-10-05

- Improved dependency resolution for initialized projects and expanded inheritance and polymorphism coverage.

## 2026-10-04

- Added WebSocket handlers, mountable route classes, and Server-Sent Events support to `gpp/http`.
- Expanded exception matching for named error types and improved pointer handling in models and examples.

## 2026-10-03

- Added generic classes and functions, with compile-time validation for typed templates.
- Added compound assignment operators and the generic `gpp/container` package with sets, stacks, queues, deques, lists, and priority queues.

## 2026-10-02

- Added safe access and fallback expressions (`?.` and `??`), more slice operations, and string blank checks.
- Added `Each` for side-effect iteration and expanded random sampling, serialization, and YAML introspection examples.
- Improved callable, safe-access, and template diagnostics.

## 2026-10-01

- Expanded installation and PATH instructions for Windows, macOS, and Linux, and reorganized compiler source for easier maintenance.

## 2026-09-30

- Added program-exit hooks, semantic tokens, and code actions to the language server.
- Added CI coverage for the compiler and examples, and continued the todo app and documentation site.

## 2026-09-29

- Added `gpp/cron` scheduling and improved exception promotion and logical package imports.
- Improved `gpp init` to create a module-backed starter project with Git history; added workspace and cache management and target-aware binary names.

## 2026-09-28

- Added template rendering by request path and improved Go-compatible assignment and exception documentation.
- Expanded the landing page, FAQ, and annotation/introspection guides.

## 2026-09-27

- Added the VitePress documentation site and guides for the standard libraries, including HTTP, ORM, templates, testing, and the prelude.

## 2026-09-19

- Completed the compiler’s migration to a structured AST, establishing a more consistent basis for language analysis and code generation.

## 2026-09-18

- Added the GPP language server, mixed Go/GPP compilation, OAuth support for `http.Server`, and GPP documentation/OpenAPI tooling.
- Expanded exception handling to catch multiple exception types.

## 2026-09-17

- Added template layout inheritance and enum serialization examples, expanded native type resolution, and improved source-mapped diagnostics.
- Added ergonomic prelude extensions for common Go types.

## 2026-09-16

- Added exceptions and expression-level error fallback, enums, embedded resources, and HTTP template responses.
- Added template watching/reloading, standalone template execution, packaging, and testing workflows.

## 2026-09-15

- Added annotations and runtime metadata, including annotation inheritance, plus static methods and serialization support.
- Added the prelude and lambda expressions, and introduced the bundled `gpp/http` and `gpp/orm` libraries.
- Improved ORM query and transaction support, including portable placeholders and lifecycle hooks.
- Renamed the project to GPP (`gpp`).

## 2026-09-14

- Public repository history begins with the initial GPP source commit.
