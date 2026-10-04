# gpp/orm Specification

## Goal

Provide a lightweight ORM-style convenience layer for Go++ built directly on top of Go's `database/sql`.

The package should use existing Go++ features such as:

* classes
* inheritance
* polymorphism
* annotations
* runtime class introspection
* field introspection
* extension methods
* multiple-target extensions

It must not replace `database/sql`, introduce a custom database engine, or require a separate query/runtime abstraction.

The primary API should remain usable with both:

```go
*sql.DB
*sql.Tx
```

through the same extension methods.

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
    model.gpp
    sql.gpp
```

The implementation should preferably remain ordinary Go++ source.

---

## Dependencies

The package may use:

```go
import (
    "database/sql"
    "fmt"
    "reflect"
    "strings"
)
```

`reflect` is currently required by `Select` for constructing arbitrary slice element types.

The implementation may later remove reflection if Go++ generics and static class introspection make that possible.

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

These annotations are ordinary Go++ annotations.

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
    Exec(query string, args ...any) (sql.Result, error)
    Query(query string, args ...any) (*sql.Rows, error)
    QueryRow(query string, args ...any) *sql.Row
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
```

All hooks are optional.

The base implementation returns `nil`.

Derived models override whichever hooks they require.

Because Go++ methods are polymorphic, calling a hook through a `Model` parameter must dispatch to the concrete model implementation.

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

# Extensions

Extend both:

```go
*sql.DB
*sql.Tx
```

with the same methods:

```go
extend *sql.DB, *sql.Tx {
    func Get(target any, where string, args ...any) error
    func Select(target any, where string, args ...any) error
    func Insert(entity Model) error
    func Update(entity Model) error
    func Delete(entity Model) error
}
```

This allows identical code with databases and transactions:

```go
db.Insert(employee)
tx.Insert(employee)

db.Get(employee, ...)
tx.Get(employee, ...)
```

No wrapper around `sql.DB` or `sql.Tx` should be introduced.

---

# Get

Signature:

```go
func Get(target any, where string, args ...any) error
```

Usage:

```go
employee := Employee()

err := db.Get(
    &employee,
    "id = $1",
    id,
)
```

`target` must be a pointer to a Go++ class instance that provides runtime class metadata.

Conceptually, it must satisfy:

```go
interface {
    GppRuntimeClass() *GppClass
}
```

If not:

```text
Get target must be a Go++ model pointer
```

should be returned.

---

## Get Metadata Resolution

Obtain the actual runtime descriptor:

```go
descriptor := entity.GppRuntimeClass()
```

Read table metadata:

```go
table := descriptor.annotations.find(Table)
```

Enumerate fields:

```go
for field := range descriptor.fields {
}
```

For every field with:

```go
field.annotations.find(Column)
```

collect:

* the SQL column name
* the actual field address via `field.addr(...)`

---

## Get Query

Build:

```sql
SELECT <columns>
FROM <table>
```

If:

```go
strings.TrimSpace(where) != ""
```

append:

```sql
WHERE <where>
```

Always append:

```sql
LIMIT 1
```

Then execute:

```go
this.QueryRow(query, args...).Scan(scanValues...)
```

Return the `Scan` error directly.

This means a missing row naturally returns:

```go
sql.ErrNoRows
```

---

# Select

Signature:

```go
func Select(target any, where string, args ...any) error
```

Usage:

```go
employees := []Employee{}

err := db.Select(
    &employees,
    "email LIKE $1",
    "%example.com",
)
```

`target` must be:

```text
non-nil pointer
    ↓
slice
    ↓
Go++ class values
```

Example valid target:

```go
&[]Employee{}
```

---

## Select Target Validation

Using reflection, verify:

```go
targetValue.Kind() == reflect.Ptr
targetValue != nil
targetValue.Elem().Kind() == reflect.Slice
```

Otherwise return:

```text
Select target must be a non-nil pointer to a model slice
```

The slice element type must currently be a struct.

Otherwise:

```text
Select target slice must contain Go++ model values
```

---

## Select Descriptor Discovery

Determine:

```go
elementType := targetValue.Elem().Type().Elem()
```

Construct a prototype:

```go
prototypeValue := reflect.New(elementType)
```

The pointer to that prototype must provide:

```go
GppRuntimeClass() *GppClass
```

Otherwise return:

```text
Select target slice must contain a Go++ class
```

Use the prototype's runtime class descriptor for table and field metadata.

---

## Select Query

Gather all fields carrying `Column`.

Build:

```sql
SELECT <columns>
FROM <table>
```

Append optional:

```sql
WHERE <where>
```

when `where` is not blank.

Do not append `LIMIT`.

Execute using:

```go
rows, err := this.Query(query, args...)
```

Always:

```go
defer rows.Close()
```

---

## Select Row Materialization

For every row:

1. Create a new instance of the slice element type.
2. Obtain its runtime class interface.
3. Build scan destinations using `field.addr(...)`.
4. Call:

```go
rows.Scan(scanValues...)
```

5. Append the populated value to the target slice.

After iteration return:

```go
rows.Err()
```

---

# Insert

Signature:

```go
func Insert(entity Model) error
```

Typical usage:

```go
employee := Employee(...)
err := db.Insert(employee)
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
func Update(entity Model) error
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

# Delete

Signature:

```go
func Delete(entity Model) error
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

The ORM depends heavily on Go++ runtime class metadata.

For a derived class:

```go
class Employee : Model {
}
```

when passed to:

```go
Insert(entity Model)
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
db.Insert(employee)
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

if err := tx.Insert(employee); err != nil {
    tx.Rollback()
    return err
}

if err := tx.Update(other); err != nil {
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

if err := db.Insert(employee); err != nil {
    return err
}

loaded := Employee()

if err := db.Get(
    loaded,
    "id = $1",
    employee.Id,
); err != nil {
    return err
}

loaded.Email = "new@example.com"

if err := db.Update(loaded); err != nil {
    return err
}

employees := []Employee{}

if err := db.Select(
    &employees,
    "email LIKE $1",
    "%example.com",
); err != nil {
    return err
}

if err := db.Delete(loaded); err != nil {
    return err
}
```

---

# SQL Philosophy

`gpp/orm` is intentionally small.

It should NOT attempt to become a query-language abstraction.

The caller supplies ordinary SQL fragments:

```go
db.Get(
    &employee,
    "name = $1",
    "Ada",
)

db.Select(
    &employees,
    "email LIKE $1",
    "%example.com",
)
```

The ORM handles:

* table names
* selected column lists
* scan destinations
* inserts
* updates
* deletes
* lifecycle hooks

The user retains direct control over SQL conditions and arguments.

---

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

`Select` currently uses Go reflection because it receives:

```go
target any
```

and must construct arbitrary values of the destination slice's element type.

This is acceptable for the current implementation.

Do not rewrite it merely for style.

If Go++ later supports sufficient generic static class introspection, a future implementation may use:

```text
T.class
T()
```

and remove most or all reflection.

That should be a later optimization/refactor, not part of this initial package extraction.

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

---

## Get

Verify:

* only Column fields are selected
* fields scan into correct concrete object
* blank where is allowed
* LIMIT 1 is added
* `sql.ErrNoRows` propagates

---

## Select

Verify:

* pointer-to-slice required
* rows append to destination
* inherited model fields scan correctly
* `rows.Err()` is propagated

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

passing `&employee` to:

```go
db.Insert(...)
```

must use:

```text
Employee.class
```

metadata, not `Model.class`.

---

# Design Principle

`gpp/orm` should demonstrate what can be achieved from ordinary Go++ language features rather than compiler magic.

Its architecture is:

```text
Go++ annotations + introspection + polymorphism
                ↓
          gpp/orm helpers
                ↓
          database/sql
```

The ORM knows how to remove repetitive CRUD plumbing.

The programmer still owns the database, SQL conditions, transactions, and model behavior.
