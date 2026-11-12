# Classes

GPP classes bring fields and methods into one declaration. They are useful
when a value has behavior that belongs with its state, such as a domain model,
service, or application component. A class compiles to Go data structures and
methods, so it stays connected to the Go packages and runtime around it.

## Compare a struct with a class

Go uses separate declarations for a struct and its methods. GPP puts those
methods in the class declaration and makes the receiver available as `this`:

::: code-group
```go [GPP]
class User {
    Name string

    func Greeting() string {
        return "Hello, " + this.Name
    }
}
```

```go [Go]
type User struct {
    Name string
}

func (user *User) Greeting() string {
    return "Hello, " + user.Name
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

## Generics

A class can declare type parameters after its name. Use them for fields and
instance method parameters or results, just as you would in a Go generic type:

```go
class Box[T] {
    Value T

    func Get() T {
        return this.Value
    }

    func Set(value T) {
        this.Value = value
    }
}

numberBox := Box[int](Value: 42)
nameBox := Box[string](Value: "Ada")
fmt.Println(numberBox.Get(), nameBox.Get())
```

An omitted constraint defaults to `any`. Add a Go-compatible constraint when
an operation requires one. For example, map keys must be `comparable`:

```go
class Index[K comparable, V] {
    Entries map[K]V

    func Get(key K) (V, bool) {
        value, found := this.Entries[key]
        return value, found
    }
}

ages := Index[string, int](Entries: map[string]int{"Ada": 36})
age, found := ages.Get("Ada")
```

Generic class constructor calls require explicit type arguments, even when the
constructor fields might appear to determine them. Once instantiated, the
class type arguments supply the types used by its fields and ordinary instance
methods.

An instance method may also declare its own type parameters, separate from
those on its class. GPP infers method type arguments from arguments when
possible, or you can write them explicitly:

```go
class Echo {
    func Identity[T](value T) T {
        return value
    }
}

echo := Echo()
word := echo.Identity("hello")
number := echo.Identity[int](42)
```

Generic methods work on concrete class instances, including child instances
that inherit the method. They cannot be called through an interface-dispatched
base-class value because Go interfaces cannot declare generic methods. The
project requires Go 1.27 or newer. See the
[generic classes specification](/reference/specifications/generic-classes)
and runnable [generics example](https://github.com/telgatech/gpp/blob/main/examples/generics.gpp)
for further details.

Inside an instance method, class fields can be used without writing `this.`.
Unqualified calls to other instance methods use the same receiver. An explicit
`this.` remains valid when it makes the receiver clearer, and parameters or
local variables take precedence when they share a member's name:

```go
class User {
    Name string

    func Greeting() string {
        return "Hello, " + Name
    }

    func Rename(Name string) {
        this.Name = Name
    }
}
```

## Access: public, private, and protected

GPP uses member names to express access; these are conventions enforced by
the compiler, not declaration keywords. It follows Go's familiar
capitalization convention: names beginning with an uppercase letter are
exported, while lowercase names are unexported. GPP makes lowercase class
members private to their declaring class, and uses `_` as an additional
marker for subclass-only protected members.

- Names beginning with an uppercase letter are **public**. Callers and child
  classes can use them.
- Names beginning with a lowercase letter are **private**. Only the class that
  declares them can use them; child classes do not inherit them.
- Names beginning with `_` are **protected**. The declaring class and its
  descendants can use them, but ordinary callers cannot.

```go
class Account {
    Name string
    pin string
    _balance int

    func Deposit(amount int) { this._balance += amount }
    func _reset() { this._balance = 0 }
    func validPin() bool { return this.pin != "" }
}

class SavingsAccount : Account {
    func AddBonus(amount int) {
        this._balance += amount // valid: protected field
        this._reset()           // valid: protected method
        // this.pin = "1234"    // invalid: private to Account
    }
}

func show(account Account, savings SavingsAccount) {
    _ = account.Name       // valid: public
    _ = account._balance   // invalid: protected
    savings._reset()       // invalid: protected, even on a child value
    _ = account.pin        // invalid: private
}
```

The declaring class can use its own private and protected members. A child can
use public and protected inherited members, but cannot use private members.
Code outside the class hierarchy can use only public members. The compiler
also filters private and protected members from ordinary caller completions,
while showing protected members inside descendant classes. Constructor
argument completions follow the constructor rules below and include inherited
protected fields accepted by the child constructor.

Visibility controls member access and inheritance. Constructor arguments are a
separate access path: callers can pass public and protected fields accepted by
the constructor; private fields can be passed only from a method of the class
that declares them. A child constructor cannot accept a parent's private
fields. This does not make those fields readable or writable through the
resulting value. Class composite literal field keys follow ordinary access
rules.
See the [class visibility spec](../../spec/class-visibility.md)
for the complete rules.

## Construct values

GPP offers positional and named class construction. These constructor calls
return pointers to mutable class instances. Positional construction follows
declared field order; named construction can list fields in any order and makes
call sites easier to scan when a class has several fields:

```go
first := User("Ada", 36)
second := User(Age: 36, Name: "Ada")

fmt.Println(first.Greeting())
fmt.Println(second.Greeting())
```

This pointer result is specific to classes. Constructor-style calls for named
structs return struct values; see [named struct construction](./structs#declare-and-construct-named-structs).

GPP checks the class fields and constructor argument count while compiling.
Named fields can appear in any order; positional construction follows field
order. A call cannot mix positional and named arguments. Ordinary Go composite
literals remain available for interoperability and produce ordinary Go
values. Use `*User` in an explicit type annotation when storing a constructed
instance; `User{...}` remains a `User` value:

```go
user := User{
    Name: "Ada",
    Age: 36,
}
```

Named construction can set inherited public and protected fields too. Private
fields stay with their declaring class and are not constructor arguments on
child classes. A leading underscore marks a protected member: descendants can
use it, while ordinary callers cannot. For example, `this._reset()` inside a
derived class is valid; `server._reset()` in an ordinary function is not,
even when `server` has the derived type. See [Access: public, private, and
protected](#access-public-private-and-protected) for examples.
If multiple parents expose a field with the same name, qualify it with the
parent name:

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

## Constructor hook: `init()`

Classes may declare an instance method named `init()` for setup after
constructor fields are assigned. Use it to normalize values or check
invariants:

```go
class Person {
    Name string

    func init() {
        this.Name = strings.TrimSpace(this.Name)
    }
}

person := Person(" Ada ") // init runs after Name has been assigned
```

An `init()` method may return an `error` when the values are invalid.
Construction then returns `(*Person, error)`. If the caller omits the trailing
error, GPP promotes it to an exception:

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

person := Person(inputName) // a non-nil init error is promoted
```

The hook applies to GPP constructor-style calls such as `Person(...)`.
Ordinary Go composite literals such as `Person{Name: inputName}` remain
unchanged and bypass it. An initializer takes no parameters and returns either
nothing or one error-compatible result. A class may declare only one `init()`.

When classes inherit from other classes, initialization runs base-first and
left-to-right through the parent declarations. Each initializer sees the fully
allocated object with constructor fields populated. The first error stops the
remaining initializers. See the [constructor specification](/reference/specifications/constructors)
for the exact rules.

## When a builder pattern helps

An initializer fits work every instance should share. A builder solves a
different problem: it lets callers assemble a more complicated value in
stages, particularly when it has many optional fields or dependent options.

A GPP builder API might look like this:

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
example shows the familiar builder pattern, not a built-in GPP API. It can
already be implemented using ordinary classes and methods.

## Put creation rules beside the type

For named alternate creation paths, a static factory keeps creation policy
next to the class without introducing a separate builder framework:

```go
class User {
    Name string

    static func Guest() *User {
        return User(Name: "Guest")
    }
}

guest := User.Guest()
```

## When to use a class

Use a class when behavior and state form one concept or when you want to extend
that concept through inheritance and polymorphism. For simple data transfer,
GPP also supports anonymous [structs](/features/structs), and ordinary Go
structs remain fully usable.

Continue with [polymorphism](/features/polymorphism),
[multiple inheritance](/features/multiple-inheritance), and the exact
[class and constructor rules](/reference/specifications/constructors).
