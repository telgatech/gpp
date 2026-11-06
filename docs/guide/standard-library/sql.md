# `gpp/sql`

`gpp/sql` adds typed `Get[T]` and `Select[T]` methods to Go's `*sql.DB` and
`*sql.Tx`. It keeps full SQL queries while returning scalars, inline records,
structs, and ORM models with static Go++ types.

Import `gpp/sql` to enable its database extension methods:

```gpp
import (
    "database/sql"
    gppsql "gpp/sql"
)
```

Keep this as a named import: Go++ uses the package import to resolve the
extension methods, so a blank import (`_`) does not make `db.Get[T]` available.

Then describe each query result at the call site:

```gpp
count := db.Get[int64]("SELECT COUNT(*) FROM employees")

events := db.Select[record(employee_id: int64, event: string)](
    "SELECT employee_id, event FROM employee_events WHERE employee_id = $1",
    employeeID,
)
```

`Get[T]` returns one value and `Select[T]` returns a slice. Use a pointer type
argument such as `db.Get[*Employee](...)` when you want pointer results.

The destination-based `Get` and `Select` overloads are deprecated. Use a type
argument to make a full-SQL result shape explicit. For model conditions and
CRUD helpers, use the model methods from [`gpp/orm`](/guide/standard-library/orm).

The [typed SQL feature guide](/features/sql) covers record mapping, pointer
results, and model queries. See the [SQL package specification](/reference/specifications/std.sql)
for the full API contract.
