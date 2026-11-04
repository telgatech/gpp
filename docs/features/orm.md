# ORM and SQL

The Go++ ORM maps annotated classes to database tables and supplies common
CRUD operations. It is designed around `database/sql`, so database handles,
transactions, and SQL remain part of the familiar Go ecosystem.

## Compare handwritten queries with a model

Handwritten SQL gives direct control. The ORM reduces repeated column mapping
for routine operations:

::: code-group
```go [Go++]
class Employee : orm.Model @{orm.Table("employees")} {
    Name string @{orm.Column("name")}
}

var employee Employee
db.Get(&employee, "id = $1", id)
```

```go [Go]
row := db.QueryRow("SELECT name FROM employees WHERE id = ?", id)
var name string
if err := row.Scan(&name); err != nil {
    return err
}
```
:::

## Use transactions and lifecycle hooks

Insert, update, delete, and select operations work with a database or
transaction executor:

```go
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

## Select into structural records

For one-off query results, use a Go++ record as the slice element type. The
record fields define the scan destinations, and the SQL query supplies the
table and selected columns:

```go
users := []record{
    record(ID: int64(0), Name: ""),
}[:0]

if err := db.Select(
    &users,
    "SELECT id AS ID, name AS Name FROM users WHERE active = $1",
    true,
); err != nil {
    return err
}
```

Record targets take a complete `SELECT` (or `WITH ... SELECT`) query. Column
labels match record field names case-insensitively and ignore underscores, so
`display_name` can scan into `DisplayName`. Explicit SQL aliases are useful
when a query expression or join makes a label ambiguous. Result columns with
no matching record field are discarded. Class targets keep using their
`@Table` and `@Column` annotations with a `WHERE` fragment.
