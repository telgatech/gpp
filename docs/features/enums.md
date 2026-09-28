# Enums: named, typed choices

Enums have been a recurring, sometimes heated topic in the Go community. Go
programs commonly model a set of alternatives with typed constants and `iota`,
while proposals for dedicated enum support have explored how much type safety
and convenience belongs in the language. The discussion has included the
scope of an enum, how it should fit Go's style, and how to validate values
that arrive from outside a program. See the [typed enum proposal](https://github.com/golang/go/issues/19814)
and a [later enum proposal](https://github.com/golang/go/issues/28438) for
examples of that discussion.

Go++ takes a focused position: when a value must be one of a declared set,
give that set its own enum type. Go++ enums are scalar enums with no payloads;
they solve the “which one of these named choices?” problem without trying to
define a general-purpose tagged union. The goal is to make the common choice
explicit and type-safe, using predictable defaults and ordinary Go
interoperability.

## Compare constants with an enum

A Go constant group is compact and useful, but it does not make the set of
values closed. A named string type and its constants still allow arbitrary
strings to be converted into that type. Every input boundary needs its own
validation, and the compiler cannot infer the intended list from the
constants:

::: code-group
```go [Go++]
enum Status string {
    Draft
    Published
}

status := Status.Published
// Status.From("archived") reports an invalid enum value.
```

```go [Go]
type Status string

const (
    Draft     Status = "draft"
    Published Status = "published"
)

status := Status("archived") // a value outside the declared choices
```
:::

With constants, teams often add a `Valid` method or a map/switch of allowed
values, then remember to call it during JSON decoding, request binding,
database reads, and other input paths. For integer constants, `iota` makes
sequences concise, but the meaning of a value can depend on declaration order
and its position in a block. String constants avoid that numbering concern,
but they still do not provide membership validation or a built-in way to
enumerate the declared choices.

## What the enum type adds

An enum makes the alternatives part of the type declaration. Members are
namespaced under their enum, and two enums with the same backing type remain
distinct types:

```go
enum Status int {
    Pending
    Active
    Suspended
}

enum Priority int {
    Low
    Normal
    High
}

current := Status.Active
// Status.Active and Priority.High are not interchangeable.
```

For integer-backed enums, omitted members receive sequential values. For
string-backed enums, omitted values use the member name. Explicit backing
values are available when an API or stored format already defines stable
values:

```go
enum Membership string {
    Basic
    Seller = "seller"
    Partner = "partner"
}

membership := Membership.Seller
fmt.Println(membership.name)  // Seller
fmt.Println(membership.value) // seller
```

## Declare related enums together

An `enum (...)` block groups multiple declarations when a package defines
several related choice sets. Each declaration still creates an independent
enum type with its own backing type and members:

```go
enum (
    Status string {
        Draft
        Published
        Archived
    }

    Visibility int {
        Private
        Unlisted
        Public
    }

    Priority int {
        Low
        Normal
        High
    }
)

status := Status.Published
visibility := Visibility.Public
priority := Priority.High
```

Grouping is only a declaration convenience. It does not combine the enums:
for example, `Status.Published` cannot be used where `Visibility` is
expected, even if two enums use the same backing type.

The member name identifies the choice in source code and metadata; the backing
value is the normal representation for JSON, SQL, and other boundaries. That
keeps a readable Go++ name separate from a wire value that may need to remain
stable.

## Validate values at boundaries

External input must be checked before it becomes an enum. Each enum provides
`From`, which validates a backing value against the declared members:

```go
role := Role.From("admin") // Role.Admin
```

An unknown value returns an error, which Go++ propagates automatically unless
the caller explicitly captures it:

```go
role := Role.From(request.Role)
```

The enum also exposes its declared values in declaration order, which is
useful for menus, validation messages, documentation, and introspection:

```go
for role := range Role.values {
    fmt.Println(role.name, role.value)
}
```

## When constants are still the right tool

Constants remain a good fit for independent compile-time values, numeric
limits, bit flags, and values that are not intended to form a closed set. A
true enum is usually a better fit when a field or parameter means “one of
these alternatives”: it groups the choices, prevents accidental mixing of
different domains, and gives conversion and metadata a single declared source
of truth.

That is Go++'s proposed resolution to the enum argument: preserve the
straightforward scalar representation and interoperation Go programmers
expect, while making the finite set explicit and validated. The [enum
specification](/reference/specifications/enums) documents the precise rules,
including serialization, SQL, HTTP binding, zero values, and Go lowering.
