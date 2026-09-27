# Compact class construction

Go++ class construction replaces repetitive field-by-field setup with a
checked call. Use positional values for compact, stable shapes and named
values when the call should explain itself.

## Go literals and Go++ construction

The same class can be initialized with a regular Go composite literal or with
Go++ constructor-style syntax:

::: code-group

```go [Go composite literal]
type Person struct {
    Name string
    Age  int
}

person := Person{Name: "Ada", Age: 36}
```

```go [Go++ class]
class Person {
    Name string
    Age int
}

positional := Person("Ada", 36)
named := Person(Age: 36, Name: "Ada")
```

:::

Go++ checks the class fields and constructor argument count while compiling.
Named fields can appear in any order; positional construction follows field
order. A call cannot mix positional and named arguments.

## Initialize inherited fields

Named construction can set inherited fields too. With multiple parents, use
the parent name if a field name appears more than once:

```go
class Named { Name string }
class Audited { CreatedAt time.Time }

class User : Named, Audited {
    Email string
}

user := User(
    Name: "Ada",
    CreatedAt: time.Now(),
    Email: "ada@example.com",
)
```

Ordinary Go composite literals remain useful when generated Go interop or a
specific literal shape is preferable. Read the [constructor specification](/reference/specifications/constructors)
for field ordering and validation rules.
