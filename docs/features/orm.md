# ORM models

The Go++ ORM maps annotated classes to database tables and supplies common
CRUD operations. It is designed around `database/sql`, so database handles,
transactions, and SQL remain part of the familiar Go ecosystem.

## Model queries and persistence

ORM model classes inherit helpers such as `Find`, `First`, `Last`, and
cursor-based `Page`, while model instances provide `Insert`, `Update`, and
`Destroy`.

```gpp
employee := Employee.Find(db, employeeID)
employees := Employee.Select(db, "active = $1", true)
employee.Update(db)
```

Model-class queries such as `Employee.Get(db, "id = $1", employeeID)` use a
condition fragment, with SQL built from the model mapping. For full SQL queries
that return scalars, inline records, structs, or models, use the separate
[`gpp/sql` package](/guide/standard-library/sql).

## Compare handwritten queries with a model

Handwritten SQL gives direct control. The ORM reduces repeated column mapping
for routine operations:

::: code-group
```gpp [Go++]
class Employee : orm.Model {
    ID int
    Name string
    DisplayLabel string @{orm.Ignore}
}

employee := Employee.Find(db, employeeID)
```

```go [Go]
type Employee struct {
    ID int
    Name string
}

var employee Employee
row := db.QueryRow("SELECT id, name FROM employee WHERE id = $1", employeeID)
if err := row.Scan(&employee.ID, &employee.Name); err != nil {
    return err
}
```
:::

Fields map to snake_case columns by default (`DisplayName` becomes
`display_name`), and the class name supplies the default table name
(`Employee` becomes `employee`). Use `orm.Column("...")` or
`orm.Table("...")` to override a name. Mark fields that are not database
columns with `orm.Ignore`. A persisted field named `Id` or `ID` is inferred as
the primary key; use `orm.PK` for a differently named key, or to choose between
multiple candidates. Schema-qualified names such as
`orm.Table("sales.employees")` are supported.

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

The model methods take a SQL condition fragment. The `gpp/sql` package's
`db.Get[T]` and `db.Select[T]` methods take a complete `SELECT` statement.

## Common model operations

The base `orm.Model` also provides helpers that use the model's table and
primary-key mapping:

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

`Find` loads by primary key and requires one explicit or inferred primary key. `All` sorts
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

## Full SQL queries

Use [`gpp/sql`](/features/sql) for typed full-SQL queries. This keeps ad hoc
query result types and model-mapping operations in separate packages.
