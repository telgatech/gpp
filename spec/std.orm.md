# gpp/orm Specification

## Goal

Provide a lightweight ORM-style convenience layer for GPP built directly on top of Go's `database/sql`.

The package should use existing GPP features such as:

* classes
* inheritance
* polymorphism
* annotations
* runtime class introspection
* field introspection
* static methods

It must not replace `database/sql`, introduce a custom database engine, or require a separate query/runtime abstraction.

Model methods should accept both:

```go
*sql.DB
*sql.Tx
```

through the same `SQLExecutor` interface.

---

## Package

The official package should be:

```go
import "gpp/orm"
```

Suggested implementation files:

```text
gpp/orm/
    annotations.gpp
    executor.gpp
    model.gpp
    sql.gpp
```

The implementation should preferably remain ordinary GPP source.

---

## Dependencies

The package may use:

```go
import (
	"database/sql"
	gppsql "gpp/sql"
    "fmt"
    "reflect"
    "strings"
)
```

`reflect` is currently required to inspect model metadata and materialize model
query results.

The implementation may later remove reflection if GPP generics and static class introspection make that possible.

---

# Annotations

Declare:

```go
annotation (
    Table(name string) on class
    Column(name string) on field
    PK on field
)
```

These annotations are ordinary GPP annotations.

The compiler must not attach special ORM semantics to them.

`gpp/orm` itself interprets them using class and field introspection.

---

## Table

Applied to a model class:

```go
class Employee : orm.Model @{
    orm.Table("employees")
} {
}
```

The argument specifies the SQL table name.

The ORM assumes a persisted model has a `Table` annotation.

---

## Column

Applied to fields that participate in persistence:

```go
Name string @{orm.Column("name")}
```

Only fields carrying `Column` are used for:

* SELECT
* INSERT
* UPDATE
* DELETE primary-key discovery when combined with `PK`

Fields without `Column` are ignored by persistence operations.

---

## PK

Marks a persisted field as the primary key:

```go
Id int64 @{
    orm.Column("id"),
    orm.PK
}
```

The current implementation expects a single primary key.

`Update` and `Delete` locate the primary-key field through:

```go
field.annotations.has(PK)
```

Composite keys are not part of the initial implementation.

---

# SQLExecutor

Expose:

```go
type SQLExecutor interface {
    gppsql.Executor
}
```

The purpose is to allow lifecycle hooks to operate identically with either:

```go
*sql.DB
*sql.Tx
```

Both types already provide the required methods.

---

# Model Base Class

Provide:

```go
class Model {
    static func Get(exec SQLExecutor, where string, args ...any) (*self, error)
    static func Select(exec SQLExecutor, where string, args ...any) ([]*self, error)
    static func All(exec SQLExecutor, sortBy string = "") ([]*self, error)
    static func Page(exec SQLExecutor, limit int = 50, afterID any = nil) ([]*self, error)
    static func Find(exec SQLExecutor, id any) (*self, error)
    static func First(exec SQLExecutor, sortBy string = "") (*self, error)
    static func Last(exec SQLExecutor, sortBy string = "") (*self, error)
    static func Truncate(exec SQLExecutor) error

    func Insert(exec SQLExecutor) error
    func Update(exec SQLExecutor) error
    func Destroy(exec SQLExecutor) error

    func Validate(exec SQLExecutor) error {
        return nil
    }

    func BeforeCreate(exec SQLExecutor) error {
        return nil
    }

    func AfterCreate(exec SQLExecutor) error {
        return nil
    }

    func BeforeUpdate(exec SQLExecutor) error {
        return nil
    }

    func AfterUpdate(exec SQLExecutor) error {
        return nil
    }

    func BeforeDelete(exec SQLExecutor) error {
        return nil
    }

    func AfterDelete(exec SQLExecutor) error {
        return nil
    }
}

func Get[T any](exec SQLExecutor, query string, args ...any) (*T, error)
func Select[T any](exec SQLExecutor, query string, args ...any) ([]*T, error)
```

All hooks are optional.

The base implementation returns `nil`.

Derived models override whichever hooks they require.

Because GPP methods are polymorphic, calling a hook through a `Model` parameter must dispatch to the concrete model implementation.

`All` selects every model row, ordered ascending by the primary key by default.
`Find` selects one row by the model's annotated primary key and requires exactly
one such column. `First` and `Last` select one row ordered by primary key
ascending and descending, respectively. `All`, `First`, and `Last` accept an
optional `sortBy` field or annotated column name; the ORM resolves it against
persisted model fields and rejects unknown or ambiguous names. `All` and
`First` use ascending order, while `Last` uses descending order. Supplying
`sortBy` removes the primary-key requirement for these three ordered helpers.

`Page` returns rows with a positive limit (default 50), ordered by primary key
ascending. It does not accept an offset. With no `afterID`, it returns the
first batch; with an `afterID`, it selects rows whose primary key is greater
than that value. The optional limit is the first argument after the executor,
so it can be set positionally. It requires a declared primary key; invalid
limits return an error. It supports sequential traversal, not arbitrary page
jumps.

`Destroy` deletes the receiver by primary key and runs `BeforeDelete` and
`AfterDelete` around the SQL operation. `Truncate` removes every row from the
model table without running per-instance hooks. It uses `DELETE FROM` so the
operation works across supported SQL drivers; it does not reset generated
identity or sequence values.

The instance methods `Insert`, `Update`, and `Destroy` use the supplied
executor for SQL and lifecycle hooks. A hook can insert another model with
`event.Insert(exec)` to keep the write on the same executor.

---

# Lifecycle Semantics

## Insert

Order:

```text
Validate
BeforeCreate
SQL INSERT
AfterCreate
```

If any step returns an error, immediately return that error.

`AfterCreate` only runs when the SQL INSERT succeeds.

---

## Update

Order:

```text
Validate
BeforeUpdate
SQL UPDATE
AfterUpdate
```

If any step returns an error, immediately return that error.

`AfterUpdate` only runs when the SQL UPDATE succeeds.

---

## Delete

Order:

```text
BeforeDelete
SQL DELETE
AfterDelete
```

The current implementation does not call `Validate` before deletion.

Do not add validation to deletion unless deliberately changing the API.

---

# Model Query Helpers

`orm.Model` provides static `Get` and `Select` methods that accept a SQL
condition fragment and build the full query from the model's table and column
annotations:

```go
employee, err := Employee.Get(db, "id = $1", 42)
employees, err := Employee.Select(db, "active = $1", true)
```

These methods work with `*sql.DB` and `*sql.Tx`. `Get` returns one model and
`Select` returns a slice of model pointers. For full-SQL queries into model
types, use `orm.Get` and `orm.Select`. The generic `gpp/sql` package
handles scalar, anonymous struct, and named struct result shapes.

# Insert

Signature:

```go
func Insert(exec SQLExecutor) error
```

Typical usage:

```go
employee := Employee(...)
err := employee.Insert(db)
```

Because `Employee` derives from `Model`, it may be passed polymorphically while retaining its concrete runtime class metadata.

---

## Insert Lifecycle

Execute:

```go
entity.Validate(this)
entity.BeforeCreate(this)
```

before generating/executing SQL.

If either returns an error, return immediately.

After successful SQL execution:

```go
return entity.AfterCreate(this)
```

---

## Insert Metadata

Obtain:

```go
table := entity.class.annotations.find(Table)
```

Enumerate:

```go
entity.class.fields
```

For every field with a `Column` annotation:

```go
columns = append(columns, columnName)
values = append(values, field.get(any(entity)))
```

Fields without `Column` are ignored.

The current implementation includes primary-key fields in INSERT.

Do not silently skip `PK` unless `Auto` or another feature is explicitly introduced later.

---

## Insert SQL

Generate PostgreSQL-style placeholders:

```text
$1
$2
$3
...
```

Build:

```sql
INSERT INTO <table> (<column list>)
VALUES (<placeholder list>)
```

Execute:

```go
this.Exec(query, values...)
```

Return SQL errors directly.

---

# Update

Signature:

```go
func Update(exec SQLExecutor) error
```

Lifecycle:

```text
Validate
BeforeUpdate
SQL UPDATE
AfterUpdate
```

---

## Update Field Handling

Enumerate all concrete runtime fields.

For a field without `Column`:

```text
ignore
```

For a field with both:

```text
Column
PK
```

record:

```text
primaryKey
primaryValue
```

but do not include it in `SET`.

For ordinary column fields:

```go
assignments = append(
    assignments,
    "<column> = $N",
)
```

and append the current field value.

After normal values, append:

```go
primaryValue
```

as the final SQL parameter.

---

## Update SQL

Generate:

```sql
UPDATE <table>
SET <column1> = $1, <column2> = $2, ...
WHERE <primaryKey> = $N
```

where `$N` is the position of the appended primary-key value.

Execute:

```go
this.Exec(query, values...)
```

Return SQL errors directly.

Then call:

```go
entity.AfterUpdate(this)
```

---

# Destroy

Signature:

```go
func Destroy(exec SQLExecutor) error
```

Lifecycle:

```text
BeforeDelete
SQL DELETE
AfterDelete
```

---

## Delete Primary Key

Inspect concrete runtime fields.

Find the field satisfying:

```go
field.annotations.find(Column) != nil
field.annotations.has(PK)
```

Obtain:

```go
primaryKey
primaryValue
```

---

## Delete SQL

Generate:

```sql
DELETE FROM <table>
WHERE <primaryKey> = $1
```

Execute:

```go
this.Exec(query, primaryValue)
```

After success:

```go
return entity.AfterDelete(this)
```

---

# Runtime Class Introspection Requirement

The ORM depends heavily on GPP runtime class metadata.

For a derived class:

```go
class Employee : Model {
}
```

when passed to:

```go
employee.Insert(exec)
```

the runtime class must still resolve to:

```text
Employee
```

not:

```text
Model
```

Therefore:

```go
entity.class
```

inside inherited/polymorphic code must represent the most-derived runtime class.

This descriptor must expose:

```go
class.annotations
class.fields
```

Fields must expose:

```go
field.annotations
field.get(obj)
field.addr(obj)
```

---

# Annotation Introspection Requirement

The ORM must use annotation symbols, not string names.

Examples:

```go
descriptor.annotations.find(Table)

field.annotations.find(Column)

field.annotations.has(PK)
```

`Table`, `Column`, and `PK` are annotation declarations exported by `gpp/orm`.

---

# Polymorphic Hooks

Hooks declared on `Model` are real polymorphic methods.

Example:

```go
class Employee : Model {
    func Validate(exec SQLExecutor) error {
        ...
    }

    func BeforeCreate(exec SQLExecutor) error {
        ...
    }
}
```

Calling:

```go
employee.Insert(db)
```

must result in:

```text
Employee.Validate
Employee.BeforeCreate
SQL INSERT using Employee metadata
Employee.AfterCreate
```

where overridden.

No manual callback registration should be required.

---

# Transaction Support

A core requirement is identical behavior for:

```go
*sql.DB
*sql.Tx
```

Example:

```go
tx, err := db.Begin()

if err := employee.Insert(tx); err != nil {
    tx.Rollback()
    return err
}

if err := other.Update(tx); err != nil {
    tx.Rollback()
    return err
}

return tx.Commit()
```

Lifecycle hooks receive the actual executor.

Inside a hook:

```go
func BeforeCreate(exec SQLExecutor) error {
    _, err := exec.Exec(...)
    return err
}
```

This means hook SQL participates in the same transaction when the operation is invoked through `*sql.Tx`.

This behavior is important and must be preserved.

---

# Example Model

```go
class Employee : orm.Model @{
    orm.Table("employees")
} {
    Id int64 @{
        orm.Column("id"),
        orm.PK
    }

    Name string @{
        orm.Column("name")
    }

    Email string @{
        orm.Column("email")
    }

    func Validate(exec orm.SQLExecutor) error {
        if strings.TrimSpace(this.Name) == "" {
            return fmt.Errorf("employee name is required")
        }

        if strings.TrimSpace(this.Email) == "" {
            return fmt.Errorf("employee email is required")
        }

        return nil
    }

    func BeforeCreate(exec orm.SQLExecutor) error {
        this.Email = strings.ToLower(
            strings.TrimSpace(this.Email),
        )

        return nil
    }
}
```

---

# Example Usage

```go
employee := Employee(
    1,
    "Ada Lovelace",
    "ada@example.com",
)

if err := employee.Insert(db); err != nil {
    return err
}

loaded, err := Employee.Get(db, "id = $1", employee.Id)
if err != nil {
    return err
}

loaded.Email = "new@example.com"

if err := loaded.Update(db); err != nil {
    return err
}

employees, err := Employee.Select(db, "email LIKE $1", "%example.com")
if err != nil {
    return err
}

if err := loaded.Destroy(db); err != nil {
    return err
}
```

---

# SQL Philosophy

`gpp/orm` supplies model-oriented helpers and persistence behavior; it is not a
query-language abstraction. Model helpers accept ordinary SQL condition
fragments. ORM model metadata and full-SQL model scanning belong to `gpp/orm`.
Typed scalar and struct queries belong to `gpp/sql`; see [the SQL
package specification](./std.sql.md).

# Database Compatibility

The current implementation emits placeholders:

```text
$1
$2
...
```

This works with PostgreSQL-style parameter syntax and with drivers such as the demonstrated SQLite driver where accepted.

Do not claim universal SQL driver portability while placeholder syntax is hard-coded.

A later feature may abstract placeholder formatting if needed.

Do not add it as part of this extraction unless requested.

---

# Error Behavior

Return underlying `database/sql` errors directly wherever possible.

Examples:

```go
sql.ErrNoRows
```

from `Get`.

Do not wrap errors unless additional context materially improves diagnostics.

Validation and lifecycle hook errors should be returned unchanged.

---

# Missing Metadata

The current implementation assumes models are correctly annotated.

A production implementation should preferably return useful errors for:

* missing `Table`
* no persisted `Column` fields
* missing `PK` during Update/Delete
* multiple `PK` fields if composite keys are unsupported

Recommended errors:

```text
orm: Employee has no Table annotation

orm: Employee has no persisted columns

orm: Employee has no primary key

orm: Employee has multiple primary keys; composite keys are not supported
```

Add these validations without changing normal successful behavior.

---

# Reflection

ORM model operations use reflection to inspect class fields and annotations and
to dispatch lifecycle hooks. Full-SQL model result scanning remains inside
`gpp/orm`; generic SQL result scanning is documented in
[the SQL package specification](./std.sql.md).

If GPP later supports sufficient static class introspection, a future
implementation may remove some reflection from both packages. This is a later
optimization, not part of the package split.

---

# Non-goals

Do NOT add automatically:

* migrations
* associations
* joins
* eager loading
* lazy loading
* query builders
* dynamic finder names
* schema generation
* dirty tracking
* automatic transactions
* connection pooling abstractions
* soft deletion
* timestamps
* pagination
* caching
* database-specific dialect layer
* implicit global database

These may be separate future additions if genuinely needed.

The package should remain a thin layer over `database/sql`.

---

# Suggested Package Layout

```text
gpp/orm/
    annotations.gpp
    executor.gpp
    model.gpp
    sql.gpp
```

Possible contents:

```text
annotations.gpp
    Table
    Column
    PK

executor.gpp
    SQLExecutor

model.gpp
    Model
    lifecycle hooks

sql.gpp
    extensions on *sql.DB and *sql.Tx
    Get
    Select
    Insert
    Update
    Delete
```

---

# Tests

## Insert

Verify:

```text
Validate
BeforeCreate
INSERT
AfterCreate
```

order.

Verify concrete model fields are used.

---

## Validation failure

If `Validate` returns an error:

* no SQL INSERT
* no BeforeCreate
* no AfterCreate

---

## BeforeCreate failure

If `BeforeCreate` returns an error:

* no INSERT
* no AfterCreate

---

## Update

Verify:

* PK is excluded from SET
* PK is used in WHERE
* hooks execute in correct order

---

## Delete

Verify:

* PK is used in WHERE
* BeforeDelete runs before SQL
* AfterDelete runs after SQL
* instance `Destroy` runs the documented delete hooks around SQL DELETE

---

## Model convenience methods

Verify:

* `All` selects every model column ordered by primary key ascending by default
* `Page` applies a positive limit, defaulting to 50, and orders by primary key
* `Page` continues after a supplied primary-key value in ascending order
* `Find` uses the single annotated primary key and propagates `sql.ErrNoRows`
* `First` orders by primary key ascending and limits to one row
* `Last` orders by primary key descending and limits to one row
* `Truncate` deletes all rows without running model hooks
* `Truncate` does not reset identity or sequence values

---

## Model Get and Select

Verify:

* only Column fields are selected
* fields scan into the concrete model
* condition fragments are parameterized
* `Get` adds `LIMIT 1`
* missing rows and `rows.Err()` propagate
* inherited model fields scan correctly

---

## Transaction

Verify lifecycle hook SQL runs inside the same `*sql.Tx`.

Rollback should roll back both:

* ORM operation
* hook SQL

---

## Derived runtime metadata

Given:

```go
class Employee : Model {
}
```

calling the model instance method:

```go
employee.Insert(db)
```

must use:

```text
Employee.class
```

metadata, not `Model.class`.

---

# Design Principle

`gpp/orm` should demonstrate what can be achieved from ordinary GPP language features rather than compiler magic.

Its architecture is:

```text
GPP annotations + introspection + polymorphism
                ↓
          gpp/orm helpers
                ↓
          database/sql
```

The ORM knows how to remove repetitive CRUD plumbing.

The programmer still owns the database, SQL conditions, transactions, and model behavior.
