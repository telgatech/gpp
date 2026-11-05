# ORM and SQL

The Go++ ORM maps annotated classes to database tables and supplies common
CRUD operations. It is designed around `database/sql`, so database handles,
transactions, and SQL remain part of the familiar Go ecosystem.

## What's new in the ORM API

Queries can now return typed models, scalars, or inline record shapes. Use
`db.Get[T]` for one result and `db.Select[T]` for a slice; these take a complete
SQL query. ORM model classes also inherit helpers such as `Find`, `First`,
`Last`, and cursor-based `Page`, while model instances provide `Insert`,
`Update`, and `Destroy`.

```gpp
count := db.Get[int64]("SELECT COUNT(*) FROM employees")
event := db.Get[record(employee_id: int64, event: string)](
    "SELECT employee_id, event FROM employee_events WHERE employee_id = $1",
    employeeID,
)

employee := Employee.Find(db, employeeID)
employee.Update(db)
```

Generic DB queries use full SQL. Model-class queries such as
`Employee.Get(db, "id = $1", employeeID)` use a condition fragment, with SQL
built from the model mapping.

## Compare handwritten queries with a model

Handwritten SQL gives direct control. The ORM reduces repeated column mapping
for routine operations:

::: code-group
```gpp [Go++]
class Employee : orm.Model @{orm.Table("employees")} {
    ID int @{orm.Column("id"), orm.PK}
    Name string @{orm.Column("name")}
}

employee := Employee.Find(db, employeeID)
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

`Employee.Get` returns one `*Employee`; `Employee.Select` returns
`[]*Employee`. Both accept parameterized conditions and work with `*sql.DB`
and `*sql.Tx`.

The model methods take a SQL condition fragment. The generic `db.Get[T]` and
`db.Select[T]` methods take a complete `SELECT` statement for records, scalars,
and model result types.

## Common model operations

The base `orm.Model` also provides helpers that use the model's table and
primary-key annotations:

```gpp
employee := Employee.Find(db, employeeID)
allEmployees := Employee.All(db)
employeesByName := Employee.All(db, "name")
firstPage := Employee.Page(db)
if firstPage.NotEmpty() {
    nextPage := Employee.Page(db, 50, firstPage.Last().ID)
}
firstEmployee := Employee.First(db)
lastEmployee := Employee.Last(db)

employee.Destroy(db)
Employee.Truncate(db)
```

`Find` loads by primary key and requires one declared primary key. `All` sorts
by the primary key ascending by default. `First` and `Last` use that default
order, ascending and descending, respectively. These three helpers accept an
optional `sortBy` field or annotated column name, for example
`Employee.All(db, "name")`. A custom sort field removes the primary-key
requirement for `All`, `First`, and `Last`. The ORM resolves the name against
persisted model fields instead of inserting it as raw SQL.

`Page` returns up to 50 rows by default, ordered by primary key. Pass the last
row's primary-key value on subsequent calls to continue paging. Paging requires
a primary key and is sequential; it does not jump to arbitrary page numbers.
We use this cursor approach instead of `LIMIT`/`OFFSET` because large offsets
can make the database compute and skip many rows. The [PostgreSQL documentation
on `LIMIT` and `OFFSET`](https://www.postgresql.org/docs/current/queries-limit.html)
describes this cost.

`Destroy` deletes one instance and runs its delete hooks. `Truncate` removes
every row from the table without per-instance hooks. It uses `DELETE FROM` for
database portability and does not reset auto-increment sequences.

## Use transactions and lifecycle hooks

Insert, update, delete, and select operations work with a database or
transaction executor:

```gpp
employee := Employee(Name: "Ada")
employee.Insert(db)

tx := db.Begin()
defer tx.Rollback()
employee.Update(tx)
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
::: info
Use a pointer type argument when you want pointer-shaped results:
`db.Get[*Employee](...)` returns `*Employee`, and
`db.Select[*Employee](...)` returns `[]*Employee`. Pointer records and scalar
types work the same way.
:::
