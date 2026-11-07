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
- Incremental builds and opt-in compiler warnings.
- ORM model writes use instance methods such as `employee.Insert(db)`, `employee.Update(db)`, and `employee.Destroy(db)`.
- Destination-based ORM query overloads are deprecated in favor of generic result types.
- Error/Exception promotion for generic and imported calls that return a trailing error.
- ORM instance forwarding so inherited model operations use the concrete model metadata.
- Source locations and paths in compiler diagnostics.
