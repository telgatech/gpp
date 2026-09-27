# Go++ Constructors and Instance Initialization

## Status

Proposed language specification. This document records the intended semantics
for the next constructor change; it does not claim that the current compiler
implements every rule below.

## Goals

Go++ classes should have a compact, predictable construction model that:

* returns a mutable class instance rather than a temporary value;
* allows a class to validate or normalize itself immediately after allocation;
* composes correctly with inheritance and polymorphism;
* supports Go++ named and positional construction arguments; and
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

Go++ accepts positional construction:

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
// Go++
p := Person("Bob", 42)

// Conceptual Go lowering
p := &Person{Name: "Bob", Age: 42}
```

The exact generated helper names and dispatch machinery are implementation
details.

## Field ordering and arguments

Positional arguments map to fields in declaration order. In an inherited class,
parent fields come first in parent declaration order, followed by the derived
class's own fields.

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

The normal Go++ argument rules apply:

* unknown fields are compile-time errors;
* duplicate fields are compile-time errors;
* positional arguments cannot follow named arguments;
* required arguments must be supplied; and
* defaults and overload resolution are applied before construction lowering.

## Instance initializer

A class may declare one reserved instance initializer named `init`:

```go
class Person {
    Name string
    Age int

    func init() {
        this.Name = strings.TrimSpace(this.Name)
    }
}
```

`init` is called automatically by constructor-style calls after the fields have
been populated. It is not called by ordinary `Person{...}` literals.

An initializer may return an error:

```go
class Person {
    Name string
    Age int

    func init() error {
        if this.Name == "" {
            return errors.New("name is required")
        }
        if this.Age < 0 {
            return errors.New("age cannot be negative")
        }
        return nil
    }
}
```

When `init` returns `error`, constructor-style construction returns two
results:

```go
p, err := Person("Bob", 42)
```

The first result is always `*Person`. The second result is the initializer's
error. A non-error initializer returns only `*Person`.

This follows Go++'s ordinary trailing-error rules. Therefore, where exception
promotion is enabled, omitting the trailing error promotes a non-nil error:

```go
p := Person("Bob", 42)
```

is valid only when the surrounding context permits automatic error promotion;
otherwise the compiler requires explicit capture:

```go
p, err := Person("Bob", 42)
```

No initializer may return any result other than either no result or one
trailing `error` result.

## Initializer restrictions

`init` has deliberately narrow semantics:

* it is an instance method and must not be declared `static`;
* it takes no parameters;
* it has at most one result, and that result must be `error`-compatible;
* a class may declare at most one `init`; and
* `init` is not an overloadable method.

The compiler should diagnose invalid declarations at the source location of
`init`, for example:

```text
class Person initializer `init` cannot have parameters
class Person initializer `init` must return no values or a trailing error
class Person initializer `init` cannot be static
```

`init` is not a general-purpose method. If a class needs multiple construction
paths, it should use static factory methods or overloaded constructor-style
calls that ultimately share the same initializer.

## Inheritance initialization order

For a derived class, constructor-style initialization proceeds from the
base-most class to the most-derived class:

1. allocate and populate the complete object;
2. run each parent initializer once, in left-to-right parent declaration order;
3. run the derived class initializer; and
4. return the initialized pointer, or the first error encountered.

Example:

```go
class Audited {
    func init() error {
        // runs first
        return nil
    }
}

class Employee : Audited {
    func init() error {
        // runs second
        return nil
    }
}
```

If an initializer returns an error, later initializers do not run. Each
initializer observes the same allocated object, including the fields populated
by the constructor call and any changes made by earlier initializers.

Initializer dispatch during construction is non-virtual and statically tied to
each class in the inheritance chain. This prevents a partially initialized
base from dispatching into a derived initializer unexpectedly.

## Composite literals remain distinct

Go-style composite literals remain valid Go++:

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
the same rules as any other Go or Go++ function returning `error`:

```go
person, err := Person(inputName, inputAge)
if err != nil {
    return err
}
```

With automatic error promotion enabled, this may be shortened to:

```go
person := Person(inputName, inputAge)
```

The compiler must not invent a second constructor-specific exception type or
silently discard initialization errors.

## Interoperability and generated Go

Constructor-style calls are Go++ syntax and may lower to generated helper
functions when an initializer or inheritance chain requires sequencing. The
generated API should preserve these properties:

* the successful result is a pointer to the declared class;
* initializer errors use Go's `error` interface;
* no package-level Go `init()` function is generated for a class initializer;
* ordinary Go callers can still use the emitted struct and methods; and
* ordinary composite literals remain available.

The compiler should represent constructor calls and initializer sequencing as
typed AST nodes. It must not implement this feature by rewriting generated
source strings or by making `Person{...}` behave differently after emission.

## Open implementation questions

The following details are intentionally left for compiler implementation and
tests to settle without changing the source-level contract:

* the generated helper naming convention;
* whether parent initializer helpers are emitted as private methods or package
  functions;
* how constructor overloads are represented in generated Go; and
* the exact diagnostic wording for inaccessible or ambiguous inherited fields.

