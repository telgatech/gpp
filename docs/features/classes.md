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

Go++ checks the class fields and constructor argument count while compiling.
Named fields can appear in any order; positional construction follows field
order. A call cannot mix positional and named arguments. Ordinary Go composite
literals remain available for interoperability and produce ordinary Go
values:

```go
user := User{
    Name: "Ada",
    Age: 36,
}
```

Named construction can set inherited fields too. If multiple parents expose a
field with the same name, qualify it with the parent name:

```go
class Named { Name string }
class Audited { CreatedAt time.Time }

class User : Named, Audited {
    Email string
}

user := User(
    Named.Name: "Ada",
    CreatedAt: time.Now(),
    Email: "ada@example.com",
)
```

## Constructor hook: `init()` (Planned)

The constructor specification reserves an instance method named `init()` for
setup after constructor fields are assigned. Use it to normalize values or
check invariants:

```go
class Person {
    Name string

    func init() {
        this.Name = strings.TrimSpace(this.Name)
    }
}

person := Person(" Ada ") // the planned constructor path runs init()
```

An `init()` method may return an `error` when the values are invalid. The
constructor then returns the object and error using Go++'s ordinary
error-handling rules:

```go
class Person {
    Name string

    func init() error {
        if this.Name == "" {
            return errors.New("name is required")
        }
        return nil
    }
}

person, err := Person(inputName)
```

The hook applies to Go++ constructor-style calls such as `Person(...)`.
Ordinary Go composite literals such as `Person{Name: inputName}` remain
unchanged and bypass it.

> **Status: Planned.** The [constructor specification](/reference/specifications/constructors)
> describes `init()` as the class-construction hook, but the current compiler
> does not yet invoke it.

## When a builder pattern helps

An initializer fits work every instance should share. A builder solves a
different problem: it lets callers assemble a more complicated value in
stages, particularly when it has many optional fields or dependent options.

A Go++ builder API might look like this:

```go
request := NewRequestBuilder().
    URL(target).
    Header("Accept", "application/json").
    Timeout(5 * time.Second).
    Build()
```

`Build()` can validate combinations of options and return an error. An
`init()` hook could still protect class invariants across construction paths;
a builder is useful on its own when incremental setup is the main need. This
example shows the familiar builder pattern, not a built-in Go++ API. It can
already be implemented using ordinary classes and methods.

## Put creation rules beside the type

For named alternate creation paths, a static factory keeps creation policy
next to the class without introducing a separate builder framework:

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
Go++ also supports anonymous [records](/features/records), and ordinary Go
structs remain fully usable.

Continue with [polymorphism](/features/polymorphism),
[multiple inheritance](/features/multiple-inheritance), and the exact
[class and constructor rules](/reference/specifications/constructors).
