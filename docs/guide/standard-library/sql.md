# `gpp/sql`

`gpp/sql` adds typed `Get[T]` and `Select[T]` methods to Go's `*sql.DB` and
`*sql.Tx`. It keeps full SQL queries while returning scalars, inline structs,
and structs with static Go++ types. ORM model mapping belongs to `gpp/orm`.

Import `gpp/sql` to enable its database extension methods:

```gpp
import (
    "database/sql"
    gppsql "gpp/sql"
    orm "gpp/orm"
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

For full-SQL queries into ORM models, use `orm.GetSQL[T]` and
`orm.SelectSQL[T]`. The ORM owns model metadata and uses the executor contract
from this package. They return `*T` or `[]*T` with an error:

```gpp
employee, err := orm.GetSQL[Employee](db, "SELECT id, name FROM employees WHERE id = $1", id)
if err != nil { panic(err) }
employees, err := orm.SelectSQL[Employee](db, "SELECT id, name FROM employees ORDER BY id")
if err != nil { panic(err) }
```

The destination-based `Get` and `Select` overloads are deprecated. Use a type
argument to make a full-SQL result shape explicit. For model conditions and
CRUD helpers, use the model methods from [`gpp/orm`](/guide/standard-library/orm).

The [typed SQL feature guide](/features/sql) covers struct mapping and pointer
results. See the [SQL package specification](/reference/specifications/std.sql)
for the full API contract.
