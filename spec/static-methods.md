# Go++ Static Methods Specification

## Goal

Add static methods to Go++ classes.

Static methods belong to the class namespace rather than to an instance.

They should support use cases such as:

* factory methods
* parsing
* conversion
* named constructors
* utility methods associated with a class
* generated APIs such as `User.FromJSON(...)`

Static methods should lower cleanly to ordinary Go package-level functions.

They are not virtual and do not participate in instance dispatch.

---

# Syntax

Use:

```go
class User {
    static func Guest() User {
        return User(
            Name: "Guest",
        )
    }

    static func FromString(value string) (User, error) {
        ...
    }
}
```

Invocation:

```go
user := User.Guest()

user, err := User.FromString(input)
```

---

# Instance vs Static Methods

Instance method:

```go
class User {
    func NameUpper() string {
        return strings.ToUpper(this.Name)
    }
}
```

Called as:

```go
user.NameUpper()
```

Static method:

```go
class User {
    static func Guest() User {
        return User(Name: "Guest")
    }
}
```

Called as:

```go
User.Guest()
```

---

# No `this`

Static methods do not operate on an instance.

Therefore:

```go
static func Foo() {
    println(this.Name)
}
```

must be a compile error.

Suggested diagnostic:

```text
static method Foo has no `this`
```

---

# Class Metadata

Static methods may refer to the declaring class explicitly:

```go
class User {
    static func Describe() string {
        return User.class.name
    }
}
```

This is valid.

Static methods do not require a special implicit class receiver.

---

# Lowering

A static method should lower to an ordinary package-level Go function.

Go++:

```go
class User {
    static func Guest() User {
        return User(Name: "Guest")
    }
}
```

Conceptually lowers to:

```go
func __gpp_User_Guest() User {
    return NewUserWithName("Guest")
}
```

The exact generated symbol name is implementation-defined.

The compiler rewrites:

```go
User.Guest()
```

to the generated function call.

---

# No Virtual Dispatch

Static methods are statically resolved.

Given:

```go
class A {
    static func Name() string {
        return "A"
    }
}

class B : A {
    static func Name() string {
        return "B"
    }
}
```

then:

```go
A.Name()
```

calls `A.Name`.

```go
B.Name()
```

calls `B.Name`.

No vtable lookup is involved.

---

# Static Method Shadowing

A subclass may declare a static method with the same name as a parent.

This hides/shadows the inherited static method for lookup through the subclass.

Example:

```go
class A {
    static func Parse(s string) A {
        ...
    }
}

class B : A {
    static func Parse(s string) B {
        ...
    }
}
```

Then:

```go
A.Parse(...)
B.Parse(...)
```

resolve independently.

This is not polymorphic overriding.

---

# Inherited Lookup

A subclass may access a parent static method if it does not define one itself.

Example:

```go
class A {
    static func Version() int {
        return 1
    }
}

class B : A {
}
```

Allow:

```go
B.Version()
```

to resolve to the inherited `A.Version()` implementation.

The implementation remains statically bound to `A`.

No late-bound `Self` semantics should be introduced in v1.

---

# `super` in Static Methods

`super` may be permitted inside a static method to access the next inherited static implementation.

Example:

```go
class A {
    static func Label() string {
        return "A"
    }
}

class B : A {
    static func Label() string {
        return super.Label() + "B"
    }
}
```

This should resolve statically through normal class method-resolution rules.

If the compiler's current `super` implementation only supports instance methods, static `super` may be deferred until the implementation supports it cleanly.

---

# Overloading

If Go++ already supports method overloading, static methods should participate in the same overload-resolution rules.

Example:

```go
class User {
    static func Parse(value string) User {
        ...
    }

    static func Parse(value []byte) User {
        ...
    }
}
```

Resolution is compile-time.

---

# Visibility

Static methods follow normal capitalization/export rules.

Example:

```go
class User {
    static func Parse(...) ...
    static func helper(...) ...
}
```

`Parse` is exported according to normal Go++ package rules.

`helper` remains package-private.

---

# Generics

Static methods may be generic where the language already permits generic functions.

Example:

```go
class Codec {
    static func Decode[T](data []byte) (T, error) {
        ...
    }
}
```

No additional generic semantics are required solely for static methods.

---

# Annotation Interaction

Annotations may generate static methods.

For example:

```go
class User @{encoding.Serializable} {
}
```

may synthesize:

```go
static func FromJSON(data []byte) (User, error)
static func FromYAML(data []byte) (User, error)
```

Generated static methods behave exactly like explicitly written static methods.

---

# Reflection / Introspection

Class metadata should distinguish static and instance methods.

Possible descriptor field:

```go
method.static
```

or equivalent.

Potential class APIs:

```go
User.class.methods
User.class.staticMethods
```

Exact surface may follow the existing method descriptor design.

At minimum, static method metadata must preserve:

* name
* owner
* parameters
* returns
* annotations
* static/instance distinction

---

# Name Conflicts

A class may not contain an instance method and static method with an indistinguishable declaration if that creates ambiguous lookup.

Example:

```go
class User {
    func Parse() {}
    static func Parse() {}
}
```

The compiler should either:

1. allow this because `user.Parse()` and `User.Parse()` are unambiguous, or
2. reject it for implementation simplicity.

Preferred behavior:

Allow it.

Instance and class namespaces are naturally distinguished by the call receiver.

---

# Generated Method Conflicts

When another feature generates a static method, such as serialization:

```go
class User @{encoding.Serializable} {
    static func FromJSON(...) ...
}
```

the compiler must not silently overwrite the user's method.

Preferred rule:

* if the user provides a compatible implementation, use the user's implementation
* otherwise report a conflict

Suggested diagnostic:

```text
generated static method User.FromJSON conflicts with user-defined method
```

---

# AST

Static method declarations should be represented as ordinary method declarations with a static flag.

Conceptually:

```text
MethodDecl {
    Name
    Params
    Returns
    Body
    IsStatic bool
    Annotations
}
```

Avoid inventing a separate AST hierarchy unless useful.

---

# Semantic Checks

For static methods:

* `this` is illegal
* instance fields cannot be referenced implicitly
* instance methods cannot be called without an instance
* class-qualified references are valid
* overload resolution remains compile-time
* no vtable slot is generated

---

# Code Generation

Do not generate:

* receiver parameter
* vtable entry
* instance thunk

Generate only a package-level function and class-qualified lookup metadata as needed.

---

# Example

```go
class User {
    Name string

    static func Guest() User {
        return User(
            Name: "Guest",
        )
    }

    static func Parse(name string) User {
        return User(
            Name: strings.TrimSpace(name),
        )
    }

    func Greeting() string {
        return "Hello " + this.Name
    }
}
```

Usage:

```go
guest := User.Guest()
user := User.Parse(" Ada ")

fmt.Println(guest.Greeting())
fmt.Println(user.Greeting())
```

---

# Non-goals

Do not add in this feature:

* late-bound static dispatch
* metaclasses
* class objects as runtime receivers
* static fields
* static constructors
* checked factory semantics
* implicit `Self` type
* Java/C#-style static initialization blocks

These can be considered separately if needed.

---

# Design Principle

Static methods are:

> class-namespaced package functions

They should provide useful organization and factory syntax without complicating the object model.

The desired mental model is:

```text
instance method
    object behavior
    virtual/polymorphic

static method
    class-associated function
    compile-time/static dispatch
```

They should make classes feel complete without introducing a second runtime dispatch system.
