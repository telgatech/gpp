# Multiple inheritance

A Go++ class can derive from more than one parent. This is useful when a type
needs to combine separate capabilities, such as a display name and audit
timestamps, without copying those fields and methods into every class.

## Compare composition with inheritance

Go often combines capabilities by embedding fields in a struct. Go++ supports
that familiar composition and also lets a class declare several parents:

::: code-group

```go [Go embedding]
type Named struct{ Name string }
type Timestamped struct{ CreatedAt time.Time }

type User struct {
    Named
    Timestamped
    Email string
}
```

```go [Go++ multiple inheritance]
class Named { Name string }
class Timestamped { CreatedAt time.Time }

class User : Named, Timestamped {
    Email string
}
```

:::

## Combine parent classes

List each parent after the colon:

```go
class Named {
    Name string

    func Label() string {
        return this.Name
    }
}

class Timestamped {
    CreatedAt time.Time
}

class User : Named, Timestamped {
    Email string
}

user := User(Name: "Ada", CreatedAt: time.Now(), Email: "ada@example.com")
fmt.Println(user.Label())
```

The derived class can use inherited fields and methods along with its own
members. Constructor-style initialization can set inherited fields by name.

## Resolve collisions explicitly

If two parents provide a member with the same name, Go++ does not silently
choose one. Qualify the member through the parent whose implementation you
intend to use:

```go
class ShortLabel {
    func Label() string { return "short" }
}

class LongLabel {
    func Label() string { return "long" }
}

class Product : ShortLabel, LongLabel {}

product := Product()
short := product.ShortLabel.Label()
long := product.LongLabel.Label()
```

Explicit qualification makes the choice visible to reviewers and avoids
depending on parent declaration order.

## Use inheritance with care

Multiple inheritance is most useful for small, stable capabilities that are
meaningful on their own. If parents share overlapping state or behavior, make
the choice explicit at the use site or define a narrower shared parent. Go
interfaces remain a good fit for contracts that need no inherited
implementation.

See the [class inheritance rules](/reference/specifications/base) for member
resolution, construction, and polymorphic dispatch.
