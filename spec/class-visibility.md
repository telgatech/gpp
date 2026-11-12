# Class member visibility

Class fields and methods have one of three effective visibility levels. GPP
follows Go's capitalization convention: an uppercase initial letter means the
member is exported and a lowercase initial letter means it is unexported. GPP
makes lowercase class members private to their declaring class, and a leading
underscore adds a protected level for subclass access.

- **Public** members begin with an uppercase letter and are available to
  callers and subclasses.
- **Private** members begin with a lowercase letter and are available only to
  the class that declares them. They are not inherited, promoted into a child's
  constructor, or offered as members on a child value.
- **Protected** members begin with `_` and are available to the declaring
  class and its subclasses. They are not part of the GPP public API.

The convention applies to fields, instance methods, and static methods:

```go
class Base {
    _count int
    func _reset() { this._count = 0 }
    static func _newDefault() Base { return Base{} }

    func Count() int { return this._count }
}

class Child : Base {
    func Clear() { this._reset() }
}
```

Constructor arguments are a separate access path. Callers may pass public and
protected fields accepted by the constructor. A private field may be passed
only from a method of its declaring class, and a child constructor cannot
accept a parent's private field. Class composite literal field keys follow the
ordinary visibility rules.

`Child` can access `_count`, `_reset`, and `_newDefault`; ordinary callers
cannot. `Count` is public and can be called by both. A child can override an
inherited protected instance method by declaring a method with the same name
and signature. Visibility does not itself make a method an override or change
which code controls when a lifecycle hook runs.

Protected means subclass-only. Code outside a class cannot call a protected
method or read or write a protected field, even when it has a value of the
declaring or derived class:

```go
func ordinaryCaller(base Base, child Child) {
    _ = base.Count() // valid: public
    base._reset()    // invalid: protected
    child._reset()   // invalid: protected, despite Child inheriting it
    _ = base._count  // invalid: protected field
}
```

Protected members remain private to the class family. Use a public accessor or
method when ordinary callers need the capability. Use private members for
implementation details that subclasses should not depend on.

These checks apply to GPP source and Go declarations embedded in `.gpp` files.
Generated Go uses exported `GppProtected...` bridge names so subclasses in
other Go packages can reach protected members. Handwritten Go can call those
generated names directly; they are compiler implementation details, not a
supported GPP API. Separate `.go` files also follow Go's package-level access
rules and are outside GPP member-access checking.

The leading underscore is the protected marker. Uppercase names remain public,
and lowercase names without a leading underscore remain private.
