# `gpp/orm`

`gpp/orm` is a lightweight helper layer over Go's `database/sql`. A model is a class that extends `orm.Model`; annotations identify its table and persisted columns. The package uses Go++ class and field metadata to generate straightforward CRUD operations while leaving the connection, transactions, and SQL driver in your hands.

## Why use it

Writing the same column lists, scan destinations, and insert argument lists for every model creates maintenance work. The ORM derives those details from the model declaration. It does not replace SQL with a new query language: you keep ordinary SQL where you need it, and can pass either `*sql.DB` or `*sql.Tx` to the same operations.

## Side by side: insert and load a model

With `database/sql`, each operation repeats the table and column mapping:

```go
_, err := db.Exec(
	"INSERT INTO employees (name, email) VALUES ($1, $2)",
	employee.Name, employee.Email,
)
if err != nil { return err }

row := db.QueryRow("SELECT id, name, email FROM employees WHERE id = $1", id)
err = row.Scan(&employee.ID, &employee.Name, &employee.Email)
```

With annotated model metadata:

```go
class Employee : orm.Model @{orm.Table("employees")} {
	ID int @{orm.Column("id"), orm.PK}
	Name string @{orm.Column("name")}
	Email string @{orm.Column("email")}
}

func LoadEmployee(db *sql.DB, id int) Employee {
	var employee Employee
	try {
		db.Get(&employee, "id = $1", id)
	} catch e {
		throw fmt.Errorf("load employee %d: %w", id, e)
	}
	return employee
}

employee := Employee(ID: 42, Name: "Ada", Email: "ada@example.com")
db.Insert(&employee) // a non-nil trailing error is thrown automatically
loaded := LoadEmployee(db, employee.ID)
```

Only fields marked with `orm.Column` participate in persistence. The primary-key field also carries `orm.PK`, as shown above.

The Go++ call omits the trailing `error`, so a database failure enters the nearest `catch`. Catch errors where the code can add context or recover; otherwise, let them propagate to a higher-level handler.

## More examples

```go
func ActiveEmployees(db *sql.DB) []Employee {
	var employees []Employee
	try {
		db.Select(&employees, "active = $1 ORDER BY name", true)
	} catch e {
		throw fmt.Errorf("load active employees: %w", e)
	}
	return employees
}

func SaveEmployee(db *sql.DB, employee *Employee) {
	try {
		db.Update(employee)
	} catch e {
		throw fmt.Errorf("update employee %d: %w", employee.ID, e)
	}
}

func RemoveEmployee(db *sql.DB, employee *Employee) {
	try {
		db.Delete(employee)
	} catch e {
		throw fmt.Errorf("delete employee %d: %w", employee.ID, e)
	}
}
```

The query condition is passed as a SQL fragment and values remain parameterized. For multi-step changes, a transaction can be used anywhere a database handle is accepted:

```go
func SaveChanges(db *sql.DB, employee *Employee, account *Account) error {
	var tx *sql.Tx
	try {
		tx = db.Begin()
		tx.Insert(employee)
		tx.Update(account)
		tx.Commit()
	} catch e {
		if tx != nil {
			_ = tx.Rollback()
		}
		return fmt.Errorf("save changes: %w", e)
	}
	return nil
}
```

The package also supports validation and lifecycle hooks on models. Use hooks when a model needs to validate or react to create, update, or delete operations; use explicit SQL for joins, reporting queries, and database-specific behavior.

## Further reading

- [ORM specification](/reference/specifications/std.orm)
- [ORM feature guide](/features/orm)
- [Runnable example](https://github.com/golang-plus-plus/gpp/blob/main/examples/orm.gpp)
