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

## Use inheritance with care

Multiple inheritance works best when each parent represents a cohesive,
reusable capability. It becomes harder to understand when parent classes carry
unrelated state, overlapping responsibilities, or many conflicting members.
In those cases, prefer a field that holds a collaborator, or define a smaller
shared parent. Use a Go interface when callers need only a contract and should
not inherit implementation.

See the [class inheritance rules](/reference/specifications/base) for member
resolution, construction, and polymorphic dispatch.
