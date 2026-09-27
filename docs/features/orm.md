# ORM and SQL

The Go++ ORM maps annotated classes to database tables and supplies common
CRUD operations. It is designed around `database/sql`, so database handles,
transactions, and SQL remain part of the familiar Go ecosystem.

## Compare handwritten queries with a model

Handwritten SQL gives direct control. The ORM reduces repeated column mapping
for routine operations:

::: code-group

```go [database/sql]
row := db.QueryRow("SELECT name FROM employees WHERE id = ?", id)
var name string
if err := row.Scan(&name); err != nil {
    return err
}
```

```go [Go++ ORM]
class Employee : orm.Model @{orm.Table("employees")} {
    Name string @{orm.Column("name")}
}

var employee Employee
db.Get(&employee, "id = $1", id)
```

:::

## Use transactions and lifecycle hooks

Insert, update, delete, and select operations work with a database or
transaction executor:

```go
employee := Employee(Name: "Ada")
db.Insert(&employee)

tx, err := db.Begin()
if err != nil {
    return err
}
if err := tx.Insert(&employee); err != nil {
    tx.Rollback()
    return err
}
return tx.Commit()
```

Model hooks such as validation and before/after create or update keep
persistence rules near the model. Use direct SQL for specialized or
performance-sensitive queries. See the [ORM specification](/reference/specifications/std.orm).
