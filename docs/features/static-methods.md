# Static methods and factories

Static methods put helpers in a class's namespace without requiring an
instance. They work well for named creation paths, parsing, and operations
that conceptually belong to one type.

## Free helper or class-qualified factory

A free function is already a natural Go option. A static method keeps the
operation discoverable beside the class it creates:

::: code-group
```go [Go++]
class User {
    Name string

    static func Guest() User {
        return User(Name: "Guest")
    }
}

guest := User.Guest()
```

```go [Go]
type User struct{ Name string }

func GuestUser() User {
    return User{Name: "Guest"}
}

guest := GuestUser()
```
:::

## Add named creation paths

Factories can make intent visible at the call site and centralize defaults or
validation:

```go
class User {
    Name string
    Active bool

    static func Guest() User {
        return User(Name: "Guest", Active: true)
    }

    static func FromName(name string) User {
        return User(Name: name, Active: true)
    }
}

guest := User.Guest()
ada := User.FromName("Ada")
```

Static methods have no implicit instance receiver. They are selected through
the class name, and their behavior is statically resolved. See [static method
rules](/reference/specifications/static-methods).
