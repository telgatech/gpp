# `gpp/orm`

`gpp/orm` is a lightweight helper layer over Go's `database/sql`. A model is a class that extends `orm.Model`; annotations identify its table and persisted columns. The package uses Go++ class and field metadata to generate straightforward CRUD operations while leaving the connection, transactions, and SQL driver in your hands.

## Why use it

Writing the same column lists, scan destinations, and insert argument lists for every model creates maintenance work. The ORM derives those details from the model declaration. It does not replace SQL with a new query language: you keep ordinary SQL where you need it, and can pass either `*sql.DB` or `*sql.Tx` to the same operations.

## Side by side: insert and load a model

The Go++ version declares the mapping once on a class and uses ORM methods for insert and load. Select the Go tab to compare the equivalent manual SQL:

::: code-group

```gpp [Go++]
class Employee : orm.Model @{orm.Table("employees")} {
	ID int @{orm.Column("id"), orm.PK}
	Name string @{orm.Column("name")}
	Email string @{orm.Column("email")}
}

employee := Employee(ID: 42, Name: "Ada", Email: "ada@example.com")
employee.Insert(db)
employee = Employee.Find(db, employee.ID)
```

```go [Go]
_, err := db.Exec(
	"INSERT INTO employees (name, email) VALUES ($1, $2)",
	employee.Name, employee.Email,
)
if err != nil { return err }

row := db.QueryRow("SELECT id, name, email FROM employees WHERE id = $1", id)
err = row.Scan(&employee.ID, &employee.Name, &employee.Email)
```

:::

Only fields marked with `orm.Column` participate in persistence. The primary-key field also carries `orm.PK`, as shown above.

For ORM model types, `Employee.Get` and `Employee.Select` generate the
`SELECT` from the model mapping. Their first argument is a SQL condition
fragment, such as `"id = $1"`, followed by its parameter values. `Find` looks
up a model by its annotated primary key. `All`, `First`, and `Last` sort by the
primary key by default; `All` and `First` use ascending order, while `Last` uses
descending order. They accept an optional persisted field or column name to
sort by. `Find` requires a primary key; a custom sort field lets the other
three work without one. By contrast,
when the result type is a record or scalar, use the separate
[`gpp/sql` package](/guide/standard-library/sql) and pass a complete `SELECT`
statement.

`Page` returns the first 50 rows in primary-key order. Pass the last row's
primary-key value on subsequent calls to continue paging. Paging is sequential
and requires a primary key.

Cursor paging avoids large `OFFSET` values, which can require the database to
compute and skip many rows. See PostgreSQL's [`LIMIT` and `OFFSET`
documentation](https://www.postgresql.org/docs/current/queries-limit.html).

The Go++ calls omit the trailing `error` result. A database failure propagates through Go++'s exception behavior; add a `try`/`catch` at a boundary when you need to recover or add context, and otherwise let it reach a higher-level handler.

## More examples

```gpp
func ActiveEmployees(db *sql.DB) []*Employee {
	return Employee.Select(db, "id > $1 ORDER BY name", 0)
}

func SaveEmployee(db *sql.DB, employee *Employee) {
	employee.Update(db)
}

func RemoveEmployee(db *sql.DB, employee *Employee) {
	employee.Destroy(db)
}
```

`Destroy` deletes one model by primary key and runs its delete hooks.
`Employee.Truncate(db)` removes every row from the model's table without
per-instance hooks. It uses `DELETE FROM` for database portability and does not
reset auto-increment sequences.

Models persist themselves with `employee.Insert(db)` and
`employee.Update(db)`.

These methods require an ORM model. For a plain struct, use `db.Exec` with
explicit SQL and values.

For ORM model types, the query condition is passed as a SQL fragment and
values remain parameterized. For multi-step changes, a transaction can be
used anywhere a database handle is accepted:

```go
func SaveChanges(db *sql.DB, employee *Employee, account *Account) {
	tx := db.Begin()
	defer tx.Rollback()
	employee.Update(tx)
	account.Update(tx)
	tx.Commit()
}
```

The package also supports validation and lifecycle hooks on models. Use hooks when a model needs to validate or react to create, update, or delete operations; use explicit SQL for joins, reporting queries, and database-specific behavior.

Hooks receive the active `SQLExecutor`, which can be either a database connection
or a transaction. Call `event.Insert(exec)` to insert another model from a
hook while keeping that write on the same connection or transaction.

## Further reading

- [ORM specification](/reference/specifications/std.orm)
- [ORM feature guide](/features/orm)
- [Runnable example](https://github.com/telgatech/gpp/blob/main/examples/orm.gpp)
