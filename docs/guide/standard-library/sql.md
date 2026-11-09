# `gpp/sql`

`gpp/sql` adds typed `Get[T]` and `Select[T]` methods to Go's `*sql.DB` and
`*sql.Tx`. It keeps full SQL queries while returning scalars, inline structs,
and structs with static Go++ types.

For Go++ model mapping and CRUD helpers, see the [ORM guide](/guide/standard-library/orm).

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

events := db.Select[struct(employee_id: int64, event: string)](
    "SELECT employee_id, event FROM employee_events WHERE employee_id = $1",
    employeeID,
)
```

`Get[T]` returns one value and `Select[T]` returns a slice. Use a pointer type
argument such as `db.Get[*User](...)` when you want pointer results.
`Select[T]` accepts one result type and returns `[]T`; tuple types such as
`[int64, string]` are not supported as row shapes. Use an anonymous struct
when a query selects multiple columns.

For struct results, SQL column labels match field names without regard
to case, underscores, or hyphens. For example, `created_at`, `CreatedAt`, and
`created-at` match `CreatedAt`. Extra columns are discarded, and unmatched
fields keep their zero values. Use explicit SQL aliases if labels are
ambiguous or multiple fields normalize to the same name. If two columns map to
the same field, the query returns an error.

The destination-based `Get` and `Select` overloads are deprecated. Use a type
argument to make a full-SQL result shape explicit.

The [typed SQL feature guide](/features/sql) covers struct mapping and pointer
results. See the [SQL package specification](/reference/specifications/std.sql)
for the full API contract.
