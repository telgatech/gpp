# Multiple inheritance

Application classes often combine several independent concerns. An invoice
may need an identity, audit timestamps, and soft deletion; a user account may
need identity, permissions, and audit history. Copying each concern's fields
and methods into every class duplicates logic. Go++ lets a class inherit from
several parents so those capabilities can be defined once and reused.

## Compare Go embedding with Go++ multiple inheritance

Go commonly combines data and behavior by embedding structs. Go++ keeps that
familiar model and adds class inheritance, including class-level
polymorphism:

::: code-group
```go [Go++]
class Identified {
    ID int
}

class AuditTrail {
    CreatedAt time.Time
    UpdatedAt time.Time
}

class Invoice : Identified, AuditTrail {
    Number string
}
```

```go [Go]
type Identified struct {
    ID int
}

type AuditTrail struct {
    CreatedAt time.Time
    UpdatedAt time.Time
}

type Invoice struct {
    Identified
    AuditTrail
    Number string
}
```
:::

The Go form is concise for composing data. Go++ lets the child class reuse
parent methods as well as fields and participate in its class inheritance and
polymorphic dispatch model. Choose interfaces when a type only needs to satisfy
a behavior contract and does not need the parent's implementation or state.

## Build a class from reusable capabilities

Consider an invoicing system with several kinds of entities. The identity,
audit, and archive rules are useful across invoices, customers, and payment
records, so define each capability once:

```go
class Identified {
    ID int

    func Key() string {
        return fmt.Sprintf("record:%d", this.ID)
    }
}

class AuditTrail {
    CreatedAt time.Time
    UpdatedAt time.Time

    func Touch() {
        this.UpdatedAt = time.Now()
    }
}

class SoftDeletable {
    DeletedAt *time.Time

    func Archive() {
        now := time.Now()
        this.DeletedAt = &now
    }

    func IsArchived() bool {
        return this.DeletedAt != nil
    }
}

class Invoice : Identified, AuditTrail, SoftDeletable {
    Number string
    CustomerID int
    TotalCents int64

    func Summary() string {
        return fmt.Sprintf("%s: invoice %s for customer %d ($%.2f)",
            this.Key(),
            this.Number,
            this.CustomerID,
            float64(this.TotalCents) / 100,
        )
    }
}
```

Construct the invoice with both its own fields and inherited fields, then use
the inherited methods through the resulting value:

```go
now := time.Now()
invoice := Invoice(
    ID: 1207,
    CreatedAt: now,
    UpdatedAt: now,
    DeletedAt: nil,
    Number: "INV-2026-041",
    CustomerID: 83,
    TotalCents: 2499,
)

invoice.Touch()
fmt.Println(invoice.Summary())
invoice.Archive()
fmt.Println(invoice.IsArchived())
```

`Invoice` declares its own invoice-specific data and behavior. The shared
capabilities stay with the parents, so another class can reuse `Identified`
and `AuditTrail` without copying their methods. Inheritance also gives each
capability one implementation to fix when its behavior changes.

## Override inherited methods

A child inherits its parents' instance methods. Declare a method with the
same name and parameter signature in the child to override the inherited
implementation. Calls through a parent-typed value still dispatch to the
child's override; parent-qualified calls select the parent implementation.
Go++ has no separate `override` keyword.

Lowercase fields and methods are private to the class that declares them.
Children do not inherit them, and the compiler rejects direct access from a
derived class. Uppercase fields and methods are public, inherited, and can be
used or overridden by children. Prefix a field name or method name with `_` to
make it available to the declaring class and its descendants without exposing
it to ordinary callers:

```go
class Base {
    _state string
    func _reset() { this._state = "" }
}

class Child : Base {
    func Clear() { this._reset() }
}
```

Protected state remains stored in the parent object. Use a public accessor when
ordinary callers need the capability; use private members for implementation
details that subclasses should not depend on. See the [class visibility spec](../../spec/class-visibility.md)
for the complete rules.

Protected is strictly for class code. A function or other caller outside the
class hierarchy cannot call `_reset` or access `_state`, even if it receives a
`Base` or `Child` value. Such callers must use a public method such as an
accessor instead:

```go
func ordinaryCaller(base Base, child Child) {
    _ = base.Count() // valid
    base._reset()    // invalid: protected
    child._reset()   // invalid: still protected on Child
}
```

If a method is a callback that a framework invokes by convention, mark it
with `std.Hook`. The annotation keeps the method available to the framework
and to overrides while removing it from editor completion suggestions,
including suggestions on child instances:

```go
import std "gpp/std"

class Model {
    func BeforeSave() error @{std.Hook} {
        return nil
    }
}

class User : Model {}
```

Annotations belong to declarations. If a child overrides a hook and the
override should also stay out of completions, annotate that child method too.
An override does not let the child call an inherited hook directly: only the
class that first declares a hook may call it from ordinary Go++ code. Framework
dispatch remains allowed, and normal method dispatch still selects a child's
override.
See the [`gpp/std` guide](/guide/standard-library/std#hook) for details.

## Resolve overlapping parent members

Independent capabilities can use the same field name for different values.
For example, a payment may have an internal database ID and a provider's
external ID. Go++ does not silently choose one parent or depend on declaration
order; qualify the field at construction and access:

```go
class DatabaseRecord {
    ID int
}

class ProviderRecord {
    ID string
}

class Payment : DatabaseRecord, ProviderRecord {
    AmountCents int64
}

payment := Payment(
    DatabaseRecord.ID: 9481,
    ProviderRecord.ID: "pay_7F3A",
    AmountCents: 2499,
)

internalID := payment.DatabaseRecord.ID
providerID := payment.ProviderRecord.ID
```

The qualification keeps both meanings visible at the call site. The same
parent-qualified access applies when two inherited methods share a name:
choose the parent whose implementation the code needs.

## Use either parent contract

A class with multiple parents can be passed through either parent type. Calls
through each parent retain the concrete child's overrides, while explicit
qualification selects a particular parent's implementation:

```go
func Describe(value Named) string { return value.Description() }
func Pay(value Salaried) int { return value.AnnualPay() }

employee := Employee(Named.Name: "Ada", Salaried.Salary: 120000, Role: "engineer")
fmt.Println(Describe(employee)) // employee: Ada (engineer)
fmt.Println(Pay(employee))      // 125000: Employee.AnnualPay override
fmt.Println(employee.Named.Description()) // named: Ada
fmt.Println(employee.Salaried.AnnualPay()) // 120000: parent implementation
```

The [runnable multiple inheritance example](https://github.com/telgatech/gpp/blob/main/examples/inheritance.gpp)
also assigns the instance to variables typed as each parent and reads
parent-qualified fields.

## Use inheritance with care

Multiple inheritance works best when each parent represents a cohesive,
reusable capability. It becomes harder to understand when parent classes carry
unrelated state, overlapping responsibilities, or many conflicting members.
In those cases, prefer a field that holds a collaborator, or define a smaller
shared parent. Use a Go interface when callers need only a contract and should
not inherit implementation.

See the [class inheritance rules](/reference/specifications/base) for member
resolution, construction, and polymorphic dispatch.
