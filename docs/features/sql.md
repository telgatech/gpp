# Typed SQL queries

`gpp/sql` adds typed query methods to `*sql.DB` and `*sql.Tx`. Use it for full
SQL when a query returns a scalar, an inline record, a Go struct, or an ORM
model. Import the package alongside `database/sql`:

```gpp
import (
    "database/sql"
    gppsql "gpp/sql"
)
```

Use a named import so Go++ can resolve the extension methods; a blank import
does not expose them at call sites.

## Return a scalar or record

`Get[T]` returns one result and `Select[T]` returns a slice. The query stays
ordinary SQL, while the type argument describes the result shape:

```gpp
count := db.Get[int64]("SELECT COUNT(*) FROM users")

users := db.Select[record(ID: int64, Name: string)](
    "SELECT id, name FROM users WHERE active = $1",
    true,
)

user := db.Get[record(ID: int64, Name: string)](
    "SELECT id, name FROM users WHERE id = $1",
    42,
)
```

Database queries often create type explosion: projections, joins, and
aggregates can each return a different shape. Inline records put that shape
beside the query without requiring a named struct for every result.

Records and structs match column labels to field names case-insensitively and
ignore underscores. Unmatched result columns are discarded. Use explicit SQL
aliases when a query expression or join makes a label ambiguous. Scalar result
types expect a single column. `Get[T]` reads one row; `Select[T]` reads all rows.

## Pointer results

Use a pointer type argument when the result should be a pointer:

```gpp
user := db.Get[*User]("SELECT id, name FROM users WHERE id = $1", userID)
users := db.Select[*User]("SELECT id, name FROM users ORDER BY id")
```

This returns `*User` and `[]*User`. Pointer records and scalar types work the
same way.

## Use ORM models

`gpp/sql` can also scan into ORM models, using their `orm.Column` mappings:

```gpp
employee := db.Get[Employee]("SELECT id, name FROM employees WHERE id = $1", id)
employees := db.Select[Employee]("SELECT id, name FROM employees ORDER BY id")
```

Use these for full SQL queries. For model operations that build SQL from the
mapping, use `Employee.Get`, `Employee.Select`, `Employee.Find`, and the other
helpers from [`gpp/orm`](/guide/standard-library/orm).

`Get` and `Select` also accept the older destination-based form, but that form
is deprecated. See [`gpp/sql` reference](/reference/specifications/std.sql)
for its exact contract.
