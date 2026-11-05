# ORM and SQL

The Go++ ORM maps annotated classes to database tables and supplies common
CRUD operations. It is designed around `database/sql`, so database handles,
transactions, and SQL remain part of the familiar Go ecosystem.

## Compare handwritten queries with a model

Handwritten SQL gives direct control. The ORM reduces repeated column mapping
for routine operations:

::: code-group
```gpp [Go++]
class Employee : orm.Model @{orm.Table("employees")} {
    ID int @{orm.Column("id"), orm.PK}
    Name string @{orm.Column("name")}
}

employee := Employee.Get(db, "id = $1", employeeID)
```

```go [Go]
type Employee struct {
    ID int
    Name string
}

var employee Employee
row := db.QueryRow("SELECT id, name FROM employees WHERE id = $1", employeeID)
if err := row.Scan(&employee.ID, &employee.Name); err != nil {
    return err
}
```
:::

## Query into ORM models

Use static methods on the model class to load one model or a slice. The ORM
builds the query from the model mapping and applies the supplied SQL condition:

```gpp
employee := Employee.Get(db, "id = $1", employeeID)
employees := Employee.Select(db, "name LIKE $1 ORDER BY name", "A%")
```

`Employee.Get` returns one `Employee`; `Employee.Select` returns
`[]Employee`. Both accept parameterized conditions and work with `*sql.DB` and
`*sql.Tx`.

The model methods take a SQL condition fragment. The generic `db.Get[T]` and
`db.Select[T]` methods take a complete `SELECT` statement for records, scalars,
and model result types.

## Use transactions and lifecycle hooks

Insert, update, delete, and select operations work with a database or
transaction executor:

```gpp
employee := Employee(Name: "Ada")
db.Insert(employee)

tx := db.Begin()
defer tx.Rollback()
tx.Insert(employee)
tx.Commit()
```

Model hooks such as validation and before/after create or update keep
persistence rules near the model. Use direct SQL for specialized or
performance-sensitive queries. See the [ORM specification](/reference/specifications/std.orm).

## Typed queries with structural records

Database queries are a common source of type explosion. Each projection,
join, or aggregate can produce a different result shape, and defining a named
struct for every one adds declarations that may only be used once. Go++ lets
you put that shape beside the query as an inline record type argument. The
compiler generates a concrete type for it, without losing static field names
or types.

`Select[T]` returns a slice of `T`, and `Get[T]` returns one `T`:

```go
users := db.Select[record(ID: int64, Name: string)](
    "SELECT id, name FROM users WHERE active = $1",
    true,
)

user := db.Get[record(ID: int64, Name: string)](
    "SELECT id, name FROM users WHERE id = $1",
    42,
)

count := db.Get[int64]("SELECT COUNT(*) FROM users")
```

Inline record shapes provide field names and types, so no prototype values or
slice initialization are needed. Record and scalar targets take a complete
`SELECT` (or `WITH ... SELECT`) query. Column labels match record field names
case-insensitively and ignore underscores, so `display_name` can scan into
`DisplayName`. Explicit SQL aliases are useful when a query expression or join
makes a label ambiguous. Result columns with no matching record field are
discarded. Scalar types can be used when the query returns one column, and
`Get[T]` reads one row while `Select[T]` returns all rows. A named model type
can also be used as `T` when you want to write the complete `SELECT` yourself.
