# `gpp/orm`

`gpp/orm` is a lightweight helper layer over Go's `database/sql`. A model is a class that extends `orm.Model`; annotations identify its table and persisted columns. The package uses Go++ class and field metadata to generate straightforward CRUD operations while leaving the connection, transactions, and SQL driver in your hands.

## Why use it

Writing the same column lists, scan destinations, and insert argument lists for every model creates maintenance work. The ORM derives those details from the model declaration. It does not replace SQL with a new query language: you keep ordinary SQL where you need it, and can pass either `*sql.DB` or `*sql.Tx` to the same operations.

## Side by side: insert and load a model

The Go++ version declares the mapping once on a class and uses ORM methods for insert and load. Select the Go tab to compare the equivalent manual SQL:

::: code-group

```go [Go++]
class Employee : orm.Model @{orm.Table("employees")} {
	ID int @{orm.Column("id"), orm.PK}
	Name string @{orm.Column("name")}
	Email string @{orm.Column("email")}
}

employee := Employee(ID: 42, Name: "Ada", Email: "ada@example.com")
db.Insert(employee)
db.Get(employee, "id = $1", employee.ID)
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

The Go++ calls omit the trailing `error` result. A database failure propagates through Go++'s exception behavior; add a `try`/`catch` at a boundary when you need to recover or add context, and otherwise let it reach a higher-level handler.

## More examples

```go
func ActiveEmployees(db *sql.DB) []Employee {
	var employees []Employee
	db.Select(&employees, "active = $1 ORDER BY name", true)
	return employees
}

func SaveEmployee(db *sql.DB, employee *Employee) {
	db.Update(employee)
}

func RemoveEmployee(db *sql.DB, employee *Employee) {
	db.Delete(employee)
}
```

The query condition is passed as a SQL fragment and values remain parameterized. For multi-step changes, a transaction can be used anywhere a database handle is accepted:

```go
func SaveChanges(db *sql.DB, employee *Employee, account *Account) {
	tx := db.Begin()
	defer tx.Rollback()
	tx.Insert(employee)
	tx.Update(account)
	tx.Commit()
}
```

The package also supports validation and lifecycle hooks on models. Use hooks when a model needs to validate or react to create, update, or delete operations; use explicit SQL for joins, reporting queries, and database-specific behavior.

Hooks receive the active `SQLExecutor`, which can be either a database connection
or a transaction. Use `orm.Insert(exec, event)` to insert another model from a
hook while keeping that write on the same connection or transaction. The
equivalent `db.Insert(model)` and `tx.Insert(model)` methods remain available
when you have a concrete database handle.

## Further reading

- [ORM specification](/reference/specifications/std.orm)
- [ORM feature guide](/features/orm)
- [Runnable example](https://github.com/telgatech/gpp/blob/main/examples/orm.gpp)
