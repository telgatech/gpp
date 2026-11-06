# Changelog

## 2026-11-06

- Added the generic return-value form `ctx.Bind[T]()` alongside the existing destination-based binding. It supports Go++ classes, Go structs, and structural records, including pointer result types; query, JSON, and form input are selected from the request method and content type.
- Added an overridable `http.Server.Handle(ctx)` front controller. It can handle a request or return `false` to continue to annotated routes.
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
