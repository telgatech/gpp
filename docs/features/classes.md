# Classes

Go++ classes bring fields and methods into one declaration. They are useful
when a value has behavior that belongs with its state, such as a domain model,
service, or application component. A class compiles to Go data structures and
methods, so it stays connected to the Go packages and runtime around it.

## Compare a struct with a class

Go uses separate declarations for a struct and its methods. Go++ puts those
methods in the class declaration and makes the receiver available as `this`:

::: code-group

```go [Go]
type User struct {
    Name string
}

func (user *User) Greeting() string {
    return "Hello, " + user.Name
}
```

```go [Go++]
class User {
    Name string

    func Greeting() string {
        return "Hello, " + this.Name
    }
}
```

:::

## Define a class

Declare fields as you would in a Go struct and put methods inside the class.
The method receiver is available as `this`:

```go
class User {
    Name string
    Age int

    func Greeting() string {
        return "Hello, " + this.Name
    }
}
```

The compiler emits a Go struct for the fields and Go receiver methods for the
behavior. That makes the relationship clear in source while preserving Go's
types and execution model in the result.

## Construct values

Go++ offers positional and named class construction. Positional construction
follows declared field order; named construction can list fields in any order
and makes call sites easier to scan when a class has several fields:

```go
first := User("Ada", 36)
second := User(Age: 36, Name: "Ada")

fmt.Println(first.Greeting())
fmt.Println(second.Greeting())
```

Ordinary Go composite literals remain available when you want Go syntax. Named
construction is checked by the compiler, so unknown fields and invalid
argument combinations are caught before the program runs.

## Put creation rules beside the type

Use a static factory when construction needs a meaningful name or validation.
This keeps creation policy next to the class without introducing a separate
builder framework:

```go
class User {
    Name string

    static func Guest() User {
        return User(Name: "Guest")
    }
}

guest := User.Guest()
```

## When to use a class

Use a class when behavior and state form one concept or when you want to extend
that concept through inheritance and polymorphism. For simple data transfer,
Go++ also supports anonymous [records](/reference/specifications/records), and
ordinary Go structs remain fully usable.

Continue with [polymorphism](/features/polymorphism),
[multiple inheritance](/features/multiple-inheritance), and the exact
[class and constructor rules](/reference/specifications/base).
