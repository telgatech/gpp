# GPP Constructors and Instance Initialization

## Status

Implemented. Constructor-style class calls lower to pointers and ordinary
composite literals remain values. Classes may define overloaded instance
`init(...)` constructors; classes without `init` keep field-based positional
or named construction. Initializer sequencing supports inheritance and error
propagation.

## Goals

GPP classes should have a compact, predictable construction model that:

* returns a mutable class instance rather than a temporary value;
* allows a class to validate or normalize itself immediately after allocation;
* composes correctly with inheritance and polymorphism;
* supports GPP named and positional construction arguments; and
* keeps ordinary Go composite literals valid for interoperability.

The model is deliberately limited to instance initialization. It does not add
static initialization or a separate runtime object system.

## Constructor-style calls

For a class declaration:

```go
class Person {
    Name string
    Age int
}
```

GPP accepts positional construction:

```go
p := Person("Bob", 42)
```

and named construction:

```go
p := Person(
    Name: "Bob",
    Age: 42,
)
```

The constructor-style call produces a `*Person`, not a `Person` value. This
means mutations through pointer-receiver methods are applied to the same
instance:

```go
p := Person("Bob", 42)
p.Rename("Robert")
```

The result may be passed directly to a base-class or interface-typed parameter
without taking its address. This is also the natural representation for
polymorphic dispatch and classes whose initialization may modify state.

For a class with no user-defined initializer, the call is conceptually lowered
to an addressable Go composite literal:

```go
// GPP
p := Person("Bob", 42)

// Conceptual Go lowering
p := &Person{Name: "Bob", Age: 42}
```

The exact generated helper names and dispatch machinery are implementation
details.

## Post-construction hook: `_afterInit()`

A class may define one protected instance method named `_afterInit()` with no
parameters and no return values. GPP calls it after the object has been fully
constructed by a `Type(...)` expression:

1. Assign fields when the class has no `init` constructor.
2. Run the selected `init` constructor and its parent constructor chain, when
   present.
3. Call each parent class's `_afterInit()` in base-first order, following
   parent declaration order.
4. Call the constructed class's `_afterInit()`, when present.

Hooks run for field-based construction as well as `init` construction.
Delegating with `this(...)` does not call hooks for intermediate
initializers; the complete hook sequence runs after the constructor chain
returns. Ordinary Go composite literals such as `User{...}` do not call hooks.

Each class contributes its own hook, so overriding `_afterInit()` does not
skip parent setup. Do not call `super._afterInit()`; the compiler calls parent
hooks automatically. With multiple inheritance, hooks run once for each
embedded parent subobject, in declaration order, followed by the child hook.
Hooks run through direct compiler-generated calls, without runtime reflection.
An uncaught error inside a hook follows normal GPP exception propagation.

For example, a server class can keep mount setup with its own configuration
while callers supply only constructor fields:

```gpp
class App : http.Server {
    func _afterInit() {
        this.Mount(http.Blog(contentDir: "./content"), "/blog")
    }
}

app := App(PublicDir: "./public")
```

## Named struct construction

Class constructor-style calls return pointers. Named struct constructor-style
calls return struct values:

```go
struct User {
    Name string
}

user := User(Name: "Ada") // type: User, not *User
```

This struct form lowers to an ordinary Go keyed composite literal. It does not
run a class `init` hook.

## Field ordering and arguments

When a class declares no `init`, positional arguments map to fields in
declaration order. In an inherited class, parent fields come first in parent
declaration order, followed by the derived class's own fields. Named arguments
can identify fields directly.

Named arguments may initialize inherited fields:

```go
employee := Employee(
    Name: "Ada",
    EmployeeID: "E-100",
)
```

If multiple parents expose the same field name, the argument must qualify the
field with its parent name:

```go
value := C(A.Name: "left", B.Name: "right")
```

The normal GPP argument rules apply:

* unknown fields are compile-time errors;
* duplicate fields are compile-time errors;
* positional arguments cannot follow named arguments;
* required arguments must be supplied; and
* defaults and overload resolution are applied before construction lowering.

## Instance constructors

A class may declare one or more instance initializers named `init`. The
overloads define the class's accepted construction signatures:

```go
class Person {
    Name string
    Age int

    func init(name string) {
        this.Name = strings.TrimSpace(name)
    }
}
```

`Person(...)` allocates one object and invokes the matching `init` on it.
Ordinary `Person{...}` literals do not call `init`.

An initializer may return an error:

```go
class Person {
    Name string
    Age int

    func init(name string) error {
        if strings.TrimSpace(name) == "" {
            return errors.New("name is required")
        }
        this.Name = strings.TrimSpace(name)
        if this.Age < 0 {
            return errors.New("age cannot be negative")
        }
        return nil
    }
}
```

When the selected `init` or a parent initializer returns `error`,
constructor-style construction returns two results:

```go
p, err := Person("Bob", 42)
```

The first result is always `*Person`. The second result carries the first
initializer error. If no initializer returns `error`, construction returns
only `*Person`; uncaptured fallible calls inside an initializer still
propagate as exceptions.

This follows GPP's ordinary trailing-error rules. Omitting the trailing error
promotes a non-nil error to an exception:

```go
p := Person("Bob", 42)
```

To handle the error as a value, capture it explicitly:

```go
p, err := Person("Bob", 42)
```

An initializer may return either no result or the built-in `error` type.
Constructors must be instance methods and cannot be static. Custom error values
can be returned through `error` just as they can from other Go functions.
An initializer does not need an `error` result merely to call fallible
functions: an uncaptured error propagates as an exception. Declare `error`
when constructor callers should receive a trailing error value that can be
captured or handled with `??`.

## Choosing a construction form

If a class has one or more `init` overloads, every `Type(...)` call must match
one. Calls do not fall back to assigning fields, and named field arguments are
not available for that class. If the class has no `init`, constructor
arguments initialize fields by position or name. Static factory methods are
ordinary methods and should be called explicitly by their descriptive names.
`Type(...)` only invokes a matching `init` overload or performs field-based
construction when the class has no initializer.

Constructor overloads use the same parameter and overload rules as other
instance methods. Duplicate signatures are compile-time errors.

The compiler diagnoses invalid declarations at the source location of `init`,
for example:

```text
class Person initializer `init` must return no values or a trailing error
class Person initializer `init` cannot be static
```

An initializer can delegate to another overload of the same class with
`this(...)`. The delegation must be the first statement. The selected overload
runs first, then the delegating initializer continues. Delegation cycles are
compile-time errors.

## Inheritance initialization order

For a derived class, constructor-style initialization proceeds from the
base-most class to the most-derived class. An allocated object is available to
all initializers, and each embedded parent constructor runs once in declared
parent order:

1. allocate the complete object;
2. initialize each parent from its own parents outward;
3. run the selected initializer for the class; and
4. return the initialized pointer, or the first error encountered.

Example:

```go
class Directory {
    Path string
    func init(path string) { this.Path = path }
}

class Audited {
    AuditID int
    func init(id int) { this.AuditID = id }
}

class Employee : Directory, Audited {
    Name string
    func init(path string, auditID int, name string) {
        super[Directory](path)
        super[Audited](auditID)
        this.Name = name
    }
}

employee := Employee("/archive", 42, "Ada")
```

`super(...)` selects the sole parent when there is one; `super[Parent](...)`
selects a parent explicitly when there are multiple. These calls must appear
before the initializer body. A parent with a no-argument initializer runs
automatically when omitted. Omitting a parent initializer that requires
arguments is a compile-time error. The parent calls execute in declared parent
order regardless of their order in the source.

`this(...)` delegates to another overload of the same class and must be the
first statement. The delegated constructor initializes the parent chain once;
then the delegating constructor continues. Cycles are compile-time errors.

If an initializer returns an error, later initializers do not run. Each
initializer observes the same allocated object and changes made by earlier
initializers.

Initializer dispatch during construction is non-virtual and statically tied to
each class in the inheritance chain. This prevents a partially initialized
base from dispatching into a derived initializer unexpectedly.

## Composite literals remain distinct

Go-style composite literals remain valid GPP:

```go
value := Person{
    Name: "Bob",
    Age:  42,
}
```

This expression produces a `Person` value and deliberately bypasses `init`.
It is the compatibility path for:

* ordinary Go code and generated Go interoperation;
* zero-value-like test fixtures;
* ORM hydration and serialization;
* deserialization paths that must not execute application lifecycle code; and
* callers that explicitly need a value rather than a pointer.

Code that wants initialization and validation must use `Person(...)`.
`Person{...}` must not silently acquire initializer behavior, because that
would change the meaning of valid Go code and could execute side effects during
existing literal construction.

## Interaction with errors and exceptions

An initializer error is an ordinary trailing error result. It participates in
the same rules as any other Go or GPP function returning `error`:

```go
person, err := Person(inputName, inputAge)
if err != nil {
    return err
}
```

With the trailing error omitted, this may be shortened to:

```go
person := Person(inputName, inputAge)
```

The compiler must not invent a second constructor-specific exception type or
silently discard initialization errors.

## Interoperability and generated Go

Constructor-style calls are GPP syntax and may lower to generated helper
functions when an initializer or inheritance chain requires sequencing. The
generated API should preserve these properties:

* the successful result is a pointer to the declared class;
* initializer errors use Go's `error` interface;
* no package-level Go `init()` function is generated for a class initializer;
* ordinary Go callers can still use the emitted struct and methods; and
* ordinary composite literals remain available.

The compiler represents constructor calls and initializer sequencing as typed
AST nodes. It does not make `Person{...}` behave differently after emission.
Generated initializer helpers are an implementation detail; `GppInitialize`
is reserved for compiler-generated use.
