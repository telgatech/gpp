# Typed SQL queries

`gpp/sql` adds typed query methods to `*sql.DB` and `*sql.Tx`. Use it for full
SQL when a query returns a scalar, an inline struct, a Go struct, or an ORM
model. Import the package alongside `database/sql`:

```gpp
import (
    "database/sql"
    gppsql "gpp/sql"
    orm "gpp/orm"
)
```

Use a named import so GPP can resolve the extension methods; a blank import
does not expose them at call sites.

## Return a scalar or inline struct

`Get[T]` returns one result and `Select[T]` returns a slice. The query stays
ordinary SQL, while the type argument describes the result shape:

```gpp
count := db.Get[int64]("SELECT COUNT(*) FROM users")

users := db.Select[struct(ID: int64, Name: string)](
    "SELECT id, name FROM users WHERE active = $1",
    true,
)

user := db.Get[struct(ID: int64, Name: string)](
    "SELECT id, name FROM users WHERE id = $1",
    42,
)
```

`T` is one result type, so `Select[T]` returns `[]T`. GPP does not currently
have tuple types for storing each row; `[int64, string]` or `[[int64, string]]`
cannot describe a pair of columns. Use an anonymous struct to give each
selected column a name and type.

Database queries often create type explosion: projections, joins, and
aggregates can each return a different shape. Inline structs put that shape
beside the query without requiring a named struct for every result.

Anonymous structs and Go structs match column labels to field names
without regard to case, underscores, or hyphens. For example, `created_at`,
`CreatedAt`, and `created-at` all match `CreatedAt`. Extra result columns are
discarded, and fields with no matching column keep their zero values. If
multiple fields normalize to the same name, or a query produces ambiguous
labels, use explicit SQL aliases. If two result columns map to the same field,
the query returns an error. Scalar result types expect a single column.
`Get[T]` reads one row; `Select[T]` reads all rows.

## Pointer results

Use a pointer type argument when the result should be a pointer:

```gpp
user := db.Get[*User]("SELECT id, name FROM users WHERE id = $1", userID)
users := db.Select[*User]("SELECT id, name FROM users ORDER BY id")
```

This returns `*User` and `[]*User`. Pointer structs and scalar types work the
same way.

## Query ORM models

The ORM owns model-specific column mapping. Use `orm.Get` and
`orm.Select` for full-SQL queries into model classes:

```gpp
employee, err := orm.Get[Employee](db, "SELECT id, name FROM employees WHERE id = $1", id)
if err != nil { panic(err) }
employees, err := orm.Select[Employee](db, "SELECT id, name FROM employees ORDER BY id")
if err != nil { panic(err) }
```

For model operations that build SQL from the mapping, use `Employee.Get`,
`Employee.Select`, `Employee.Find`, and the other helpers from
[`gpp/orm`](/guide/standard-library/orm).

`Get` and `Select` also accept the older destination-based form, but that form
is deprecated. See [`gpp/sql` reference](/reference/specifications/std.sql)
for its exact contract.
