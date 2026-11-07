# Changelog

## 2026-11-07

- Simplified ORM model declarations: fields map to snake_case columns and classes map to snake_case table names by default. Use `orm.Column` and `orm.Table` to override names, and `orm.Ignore` to omit non-database fields. Schema-qualified table names such as `sales.employees` are supported.
- ORM models now infer the primary key from a persisted `Id` or `ID` field when no `orm.PK` is specified. Keep `orm.PK` for custom key names or to select among multiple candidates.
- Updated the ORM and todo examples to demonstrate inferred mappings, with explicit annotations shown as comments in the todo model.
- Added support for block comments between class header tokens, including between a parent class and the opening brace; compiler and LSP diagnostics now accept this syntax.
- Updated todo validation and normalization to use the Go++ string methods `IsBlank()` and `TrimSpace()`.

## 2026-11-06

- Added `ctx.HTML` for HTML responses from named, path-matched, or inline templates; `ctx.Template` remains as an obsolete alias.
- Added class `init()` hooks that run after constructor fields are populated, in base-to-derived order; initializer errors participate in normal error promotion.
- Added the generic return-value form `ctx.Bind[T]()` alongside the existing destination-based binding. It supports Go++ classes, Go structs, and structural records, including pointer result types; query, JSON, and form input are selected from the request method and content type.
- Added `http.Server.Handle(ctx) error` as a procedural handler; a response skips route dispatch, while an unwritten response falls through to annotated routes (if any).
- Added `http.Server.PublicDir` for root static files and made `http.Files` mounts run before procedural handlers and routes. Both prefer `index.html` then `index.htm`, disable directory listings by default, and use a five-minute cache policy with gzip for compressible responses.
- Simplified `http.Context.Template` to one call with optional status, data, and key arguments; an omitted key uses the request path.
- Split generic typed SQL queries into the new `gpp/sql` package; model mapping and model operations remain in `gpp/orm`.
- `http.Server.Listen(port)` now takes the TCP port directly; pass `0` to let the OS choose an available port. This replaces the `http.Port` annotation.
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
- `gpp run` accepts `.go` files and still processes them through Go++ before running.
- Added incremental build caching and a gradual-adoption FAQ explaining how Go++ builds on Go.

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

- Added the Go++ language server, mixed Go/Go++ compilation, OAuth support for `http.Server`, and Go++ documentation/OpenAPI tooling.
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
- Renamed the project from GoPP to Go++ (`gpp`).

## 2026-09-14

- Public repository history begins with the initial Go++ source commit.
