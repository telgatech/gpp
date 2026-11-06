# `gpp/sql` Specification

## Goal

Provide typed results for complete SQL queries on Go's `database/sql` handles.
The package extends `*sql.DB` and `*sql.Tx` without replacing the standard
library or introducing a query language.

## Package

```go
import (
    "database/sql"
    gppsql "gpp/sql"
)
```

Importing `gpp/sql` enables the extension methods on both database handles and
transactions.

## API

```go
extend *sql.DB, *sql.Tx {
    func Get[T any](query string, args ...any) (T, error)
    func Select[T any](query string, args ...any) ([]T, error)

    func Get(target any, query string, args ...any) error
    func Select(target any, query string, args ...any) error
}
```

The destination-based overloads are deprecated. New full-SQL queries should
use the generic result forms. ORM model condition queries should use the
model's static `Get` or `Select` method.

## Query contract

`Get` and `Select` require a complete `SELECT` or `WITH ... SELECT` query.
`Get[T]` returns one row and propagates `sql.ErrNoRows` when there is no result.
`Select[T]` returns every row. Scalar result types require one result column.
All methods pass `args` to `database/sql` without interpolation.

Result types may be scalars, structs, Go++ records, or ORM model classes. Use
pointer type arguments for pointer-shaped results:

```go
count := db.Get[int64]("SELECT COUNT(*) FROM users")
users := db.Select[record(ID: int64, Name: string)](
    "SELECT id, name FROM users WHERE active = $1", true,
)
user := db.Get[*Employee]("SELECT id, name FROM employees WHERE id = $1", id)
```

At most one pointer level is supported in a result type. A pointer `Get[T]`
allocates the value only after a row is found.

## Struct and record mapping

Column labels match exported result fields case-insensitively, ignoring
underscores and hyphens. Go++ records may provide their generated field
accessors. Unmatched result columns are scanned into discard destinations, and
unmatched fields retain their zero values. If multiple fields normalize to the
same name, the query must use an explicit alias to disambiguate them. Duplicate
columns mapping to one field are an error.

ORM models can participate by implementing the structural method
`GppSQLScanTargets(columnNames []string) ([]any, error)`. `gpp/orm` implements
this method using `orm.Column` annotations. `gpp/sql` must not import `gpp/orm`;
the structural interface keeps the dependency direction one-way.

## Destination overloads

The deprecated overloads retain pointer-based destination scanning for callers
migrating older code. For records and scalars they take a complete SQL query;
for ORM model targets they also accept the older SQL condition fragment and
build the query from the model mapping:

```go
var count int64
err := db.Get(&count, "SELECT COUNT(*) FROM users")

var users []User
err = db.Select(&users, "SELECT id, name FROM users")
```

The destination must be a non-nil pointer; `Select` requires a pointer to a
slice. Prefer the generic forms for full SQL and model-class methods such as
`Employee.Get(db, "id = $1", id)` for model conditions.
