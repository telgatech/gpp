# Structural records

Records represent small data shapes without declaring a named class first.
They are handy for local results, configuration fragments, and values passed
between nearby functions when a reusable nominal type would add ceremony.

## Name fields at the point of use

Go usually introduces an anonymous shape with an anonymous struct type. A
Go++ record gives fields names directly in the value expression:

::: code-group

```go [Go anonymous struct]
user := struct {
    Name   string
    Active bool
}{Name: "Ada", Active: true}
```

```go [Go++ record]
user := record(
    Name: "Ada",
    Active: true,
)
```

:::

The compiler derives a deterministic Go struct shape from the record fields.
Field access remains statically checked:

```go
if user.Active {
    fmt.Println(user.Name)
}
```

## Nest records for related data

Records can contain other records and work naturally with inferred local
values:

```go
response := record(
    ok: true,
    user: record(
        Name: "Ada",
        Profile: record(Role: "admin"),
    ),
)

fmt.Println(response.user.Profile.Role)
```

## Return records from functions

A function can return `record` and infer its exact field names and types from
the record it returns. Callers receive that same statically typed shape, so
they can access fields directly and the compiler can catch misspellings:

```go
func loadSummary(id int) record {
    return record(
        id: id,
        title: "Quarterly report",
        pageCount: 18,
    )
}

func main() {
    summary := loadSummary(7)
    fmt.Println(summary.title, summary.pageCount)
    // summary.pagesCount would be a compile-time error.
}
```

Every return path must produce the same field names and types. That lets the
function keep one precise return type without first declaring a separate
result struct just for this operation.

## Pass records as function arguments

Functions can accept `record` parameters too. Go++ infers the parameter's
concrete shape from record values passed at call sites in the same program:

```go
func formatSummary(summary record) string {
    return fmt.Sprintf("%s (%d pages)", summary.title, summary.pageCount)
}

func main() {
    summary := record(
        title: "Quarterly report",
        pageCount: 18,
    )

    fmt.Println(formatSummary(summary))
    fmt.Println(formatSummary(record(title: "Release notes", pageCount: 4)))
}
```

The shape remains structural and statically checked through the call: a value
with missing fields or incompatible field types cannot be used as this
parameter. There is no conversion to `map[string]any`, reflection-based field
lookup, or loss of the field types at the function boundary.

## Composite results without a struct for every operation

Functions often need to return two or three related values: a query result and
a count, parsed settings with warnings, or a value together with display
metadata. Without records, each one-off combination tends to grow a named
struct declaration, even when it is only used by one caller. Records let the
function state the shape where it creates the value, then pass it to another
function or return it to its caller:

```go
func summarizeUser(id int) record {
    return record(
        name: "Ada",
        active: true,
        role: "admin",
    )
}

func welcome(user record) string {
    if user.active {
        return "Welcome, " + user.name + " (" + user.role + ")"
    }
    return "Account inactive: " + user.name
}

func main() {
    message := welcome(summarizeUser(42))
    fmt.Println(message)
}
```

The compiler still generates ordinary Go structs underneath. Records remove
the need to hand-author and maintain a new named type for every short-lived
composite result; they do not turn those values into dynamic maps. For
cross-file or cross-package APIs that need a stable, named contract, use a
class or named Go struct. Record parameter inference currently applies to
same-program call sites.

Use a class or named Go struct when a shape is part of a package's long-lived
public contract. See the [record specification](/reference/specifications/records)
for type identity and visibility rules.
