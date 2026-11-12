# Anonymous structs

Anonymous structs represent small data shapes without declaring a named class first.
They are handy for local results, configuration fragments, and values passed
between nearby functions when a reusable nominal type would add ceremony.
GPP uses `struct(...)` for anonymous values, `struct Name { ... }` for concise
named declarations, and `Name(...)` to construct named structs with fields.

## Name fields at the point of use

Go usually introduces an anonymous shape with an anonymous struct type. A
GPP struct gives fields names directly in the value expression:

::: code-group
```go [GPP]
user := struct(
    Name: "Ada",
    Active: true,
)
```

```go [Go]
user := struct {
    Name   string
    Active bool
}{Name: "Ada", Active: true}
```
:::

## Declare and construct named structs

Use the short declaration when defining a named data type. The Go declaration
form remains available:

```go
struct User {
    Name string
    Active bool
}

// Equivalent Go declaration:
type User struct {
    Name string
    Active bool
}
```

Named fields can be supplied with the class-style constructor syntax. This
lowers to a normal Go keyed composite literal, returns a `User` value, and
checks field names and types:

```go
user := User(Name: "Ada", Active: true)
```

The Go composite literal `User{Name: "Ada", Active: true}` remains supported.

The compiler derives a deterministic Go struct shape from the struct fields.
Field access remains statically checked:

```go
if user.Active {
    fmt.Println(user.Name)
}
```

## Nest anonymous structs for related data

Anonymous structs can contain other anonymous structs and work naturally with inferred local
values:

```go
response := struct(
    ok: true,
    user: struct(
        Name: "Ada",
        Profile: struct(Role: "admin"),
    ),
)

fmt.Println(response.user.Profile.Role)
```

## Return anonymous structs from functions

A function can return `struct` and infer its exact field names and types from
the struct it returns. Callers receive that same statically typed shape, so
they can access fields directly and the compiler can catch misspellings:

```go
func loadSummary(id int) struct {
    return struct(
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

## Pass anonymous structs as function arguments

Functions can accept `struct` parameters too. GPP infers the parameter's
concrete shape from struct values passed at call sites in the same program:

```go
func formatSummary(summary struct) string {
    return fmt.Sprintf("%s (%d pages)", summary.title, summary.pageCount)
}

func main() {
    summary := struct(
        title: "Quarterly report",
        pageCount: 18,
    )

    fmt.Println(formatSummary(summary))
    fmt.Println(formatSummary(struct(title: "Release notes", pageCount: 4)))
}
```

The shape remains structural and statically checked through the call: a value
with missing fields or incompatible field types cannot be used as this
parameter. There is no conversion to `map[string]any`, reflection-based field
lookup, or loss of the field types at the function boundary.

## Composite results without a struct for every operation

Functions often need to return two or three related values: a query result and
a count, parsed settings with warnings, or a value together with display
metadata. Without anonymous structs, each one-off combination tends to grow a named
struct declaration, even when it is only used by one caller. Anonymous structs let the
function state the shape where it creates the value, then pass it to another
function or return it to its caller:

```go
func summarizeUser(id int) struct {
    return struct(
        name: "Ada",
        active: true,
        role: "admin",
    )
}

func welcome(user struct) string {
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

The compiler still generates ordinary Go structs underneath. Anonymous structs remove
the need to hand-author and maintain a new named type for every short-lived
composite result; they do not turn those values into dynamic maps. For
cross-file or cross-package APIs that need a stable, named contract, use a
class or named Go struct. Anonymous struct parameter inference currently applies to
same-program call sites.

Use a class or named Go struct when a shape is part of a package's long-lived
public contract. See the [struct specification](/reference/specifications/structs)
for type identity and visibility rules.
