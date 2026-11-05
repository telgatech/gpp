# Changelog

## 0.2.0 - 2026-11-05

### Added

- Generic ORM `Get[T]` and `Select[T]` queries for models, scalars, and inline records.
- Inherited ORM model helpers for find, select, ordering, cursor paging, truncation, and instance persistence.
- Static `self` typing for inherited class methods.
- Incremental builds and opt-in compiler warnings.

### Changed

- ORM model writes use instance methods such as `employee.Insert(db)`, `employee.Update(db)`, and `employee.Destroy(db)`.
- Destination-based ORM query overloads are deprecated in favor of generic result types.

### Fixed

- Error/Exception promotion for generic and imported calls that return a trailing error.
- ORM instance forwarding so inherited model operations use the concrete model metadata.
- Source locations and paths in compiler diagnostics.
