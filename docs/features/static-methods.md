# Static methods and factories

Static methods put helpers in a class's namespace without requiring an
instance. They work well for named creation paths, parsing, and operations
that conceptually belong to one type.

## Free helper or class-qualified factory

A free function is already a natural Go option. A static method keeps the
operation discoverable beside the class it creates:

::: code-group
```go [GPP]
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

## Use `self` for the calling class's type

A base class can use `self` when a static method should create or
return the class used at the call site:

```go
class EntityFactory {
    static func New() *self {
        return new(self)
    }
}

class User : EntityFactory {
    Name string
}

user := User.New() // user is *User
user.Name = "Ada"
```

`New` is defined once on `EntityFactory`; the call through `User` makes `self`
refer to `User`, so `New` returns `*User`. The compiler treats `self` as an
implicit generic type parameter. It can also be used as a type argument inside
the method body, for example `Helper[self](...)`. `self` refers to a type;
`this` refers to an instance. The method implementation remains statically
resolved.
