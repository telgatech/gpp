# Go++ Annotation Inheritance Specification

## Goal

Define general annotation behavior across classes, subclasses, inherited members, and overridden members.

This is a language-level annotation/introspection rule.

It must not be specific to HTTP, ORM, serialization, or any other library.

Libraries such as:

```text
gpp/http
gpp/orm
gpp/encoding
```

should rely on these same general semantics.

---

# Core Principle

Annotations belong to declarations.

When a member declaration is inherited, its annotations remain attached to that inherited member.

When a member is overridden, the overriding declaration is a new declaration and does not automatically receive annotations from the overridden member.

Class annotations remain attached to the class where they were declared.

Subclass ancestry makes parent class annotations discoverable, but does not physically copy them onto the subclass.

---

# Example

```go
annotation (
    Foo on class, field, method
    Bar on class, field, method
)

class A @{Foo} {
    Id int @{Foo}

    func Run() @{Bar} {
    }
}

class B : A @{Bar} {
}
```

The declarations are:

```text
A
    annotations:
        Foo

A.Id
    annotations:
        Foo

A.Run
    annotations:
        Bar

B
    annotations:
        Bar
```

`B` inherits:

```text
A.Id
A.Run
```

Therefore its effective field/method sets contain those declarations with their original annotations intact.

---

# Member Inheritance

For fields and methods:

> inheriting the member also inherits the member's annotation metadata because the same declaration is being inherited.

Example:

```go
class Entity {
    Id int @{PK}
}

class User : Entity {
    Name string
}
```

The effective field set of `User` contains:

```text
Entity.Id
User.Name
```

`Entity.Id` still carries:

```text
PK
```

No annotation copying is required.

The original field descriptor remains annotated.

---

# Inherited Methods

Example:

```go
class Base {
    func Run() @{Foo} {
    }
}

class Child : Base {
}
```

The effective method set of `Child` contains:

```text
Base.Run
```

and its descriptor still contains:

```text
Foo
```

Therefore:

```go
for method := range Child.class.methods {
    ...
}
```

must expose the inherited `Run` method with its annotation metadata preserved.

---

# Inherited Parameter Annotations

Parameter annotations are part of the method declaration and are inherited with that method.

Example:

```go
annotation Query on parameter

class Base {
    func Search(
        q string @{Query}
    ) {
    }
}

class Child : Base {
}
```

The inherited `Search` descriptor must preserve:

```text
parameter:
    name = q
    type = string
    annotations = [Query]
```

This allows libraries to interpret inherited method parameters without special inheritance logic.

---

# Field Descriptors

Inherited field descriptors should preserve at least:

```text
name
type
owner
annotations
```

Example:

```go
class Base {
    Value int @{Foo}
}

class Child : Base {
}
```

When inspecting `Child.class.fields`, the inherited field should report:

```text
name:
    Value

owner:
    Base

annotations:
    Foo
```

The field does not become a newly declared `Child.Value`.

---

# Method Descriptors

Inherited method descriptors should preserve at least:

```text
name
owner
annotations
parameters
return types
static/instance distinction
```

Example:

```go
class Base {
    func Run() @{Foo} {
    }
}

class Child : Base {
}
```

The effective method descriptor should still report:

```text
owner:
    Base
```

This preserves declaration identity.

---

# Overriding Members

An overriding method is a new declaration.

Example:

```go
class Base {
    func Run() @{Foo} {
    }
}

class Child : Base {
    func Run() {
    }
}
```

The effective `Run` method for `Child` is:

```text
Child.Run
```

Its annotation set is empty.

`Foo` is not automatically copied from `Base.Run`.

---

# Explicit Annotation Restatement

If the overriding declaration should retain an annotation, it must declare it explicitly:

```go
class Child : Base {
    func Run() @{Foo} {
    }
}
```

The resulting descriptor is:

```text
owner:
    Child

annotations:
    Foo
```

---

# Why Overrides Do Not Copy Annotations

Automatic annotation copying on override can introduce surprising semantics.

Example:

```go
class Base {
    func Delete() @{
        Authorized,
        Audit
    } {
    }
}

class Child : Base {
    func Delete() {
        ...
    }
}
```

If annotations were silently copied, `Child.Delete` would acquire security/audit behavior the subclass did not explicitly declare.

Therefore:

> inherited declarations retain annotations; overriding declarations start with their own annotation set.

This rule is simple and predictable.

---

# Overridden Fields

If Go++ permits a subclass to redeclare/hide a field with the same name, the same rule applies.

Example:

```go
class Base {
    Value string @{Foo}
}

class Child : Base {
    Value int
}
```

The new `Child.Value` declaration does not inherit `Foo`.

The original `Base.Value` remains a separate inherited/hidden declaration according to normal class-member rules.

Annotation behavior must not change those rules.

---

# Class Annotations

Class annotations are declaration metadata.

Example:

```go
class Base @{Foo} {
}

class Child : Base @{Bar} {
}
```

Direct annotation lookup should produce:

```text
Base.class.annotations
    Foo

Child.class.annotations
    Bar
```

`Foo` must not be physically copied into `Child.class.annotations`.

---

# Class Ancestry

A subclass can still discover annotations on its ancestors through normal class metadata.

Example:

```go
Child.class.parents
```

contains:

```text
Base
```

and:

```go
Base.class.annotations
```

contains:

```text
Foo
```

Therefore libraries can deliberately perform ancestry-aware annotation lookup.

---

# Direct vs Inherited Class Annotation Lookup

The introspection API should distinguish:

```text
annotations declared directly on this class
```

from:

```text
annotations discoverable through class ancestry
```

Possible API:

```go
Child.class.annotations
```

for direct annotations.

And one of:

```go
Child.class.allAnnotations
```

or:

```go
Child.class.annotations.has(Foo, inherited: true)
```

or:

```go
Child.class.annotations.resolve(Foo)
```

for ancestry-aware lookup.

Exact naming is implementation-defined.

The semantic distinction is required.

---

# Preferred API Shape

A clean possible design is:

```go
class.annotations
```

Direct annotations only.

And:

```go
class.annotation(Foo)
class.hasAnnotation(Foo)
class.annotationsOf(Foo)
```

with an explicit inheritance option where needed.

For example:

```go
class.hasAnnotation(Foo)
class.hasAnnotation(Foo, inherited: true)
```

or equivalent.

Do not make inherited lookup implicit unless clearly named.

---

# No Universal Class-Annotation Inheritance Semantics

The language must not assume that every class annotation semantically applies to subclasses.

Different annotations may have different meanings.

For example:

```go
@{Serializable}
```

may or may not be intended to affect subclasses.

Likewise:

```go
@{Table("employees")}
```

may describe exactly one class.

And:

```go
@{Auth}
```

may naturally be interpreted by a library as inherited policy.

Therefore:

> Go++ preserves class ancestry and class annotation metadata, but consuming libraries decide whether parent class annotations affect subclasses.

---

# Annotation Identity

Annotations must retain symbol identity through inheritance.

Example:

```go
field.annotations.has(PK)
```

must resolve against the actual declared annotation symbol.

Do not lower annotation identity to plain strings during inheritance.

This is important for:

```text
package-qualified annotations
type-safe annotation lookup
duplicate annotation handling
library-defined semantics
```

---

# Package-Qualified Annotations

Example:

```go
class User @{
    encoding.Serializable
}
```

The annotation identity is:

```text
gpp/encoding.Serializable
```

not merely:

```text
"Serializable"
```

If encountered through ancestor inspection, the identity remains unchanged.

---

# Multiple Inheritance

Annotation behavior follows normal multiple-inheritance member resolution.

Example:

```go
class A {
    func Run() @{Foo} {
    }
}

class B {
    func Run() @{Bar} {
    }
}

class C : A, B {
}
```

Because `Run` itself is ambiguous, annotation lookup must not silently merge:

```text
Foo
Bar
```

into one synthetic method.

Normal Go++ ambiguity rules apply first.

The programmer must resolve the member conflict.

---

# Distinct Members from Multiple Parents

Distinct inherited members retain their own annotations.

Example:

```go
class A {
    func FooMethod() @{Foo} {
    }
}

class B {
    func BarMethod() @{Bar} {
    }
}

class C : A, B {
}
```

The effective method set contains:

```text
A.FooMethod
    annotations = Foo

B.BarMethod
    annotations = Bar
```

No special merging is necessary.

---

# Multiple Parent Class Annotations

Example:

```go
class A @{Foo} {
}

class B @{Bar} {
}

class C : A, B {
}
```

Direct lookup:

```text
C.class.annotations
    empty
```

An ancestry-aware lookup may discover:

```text
A → Foo
B → Bar
```

The language should preserve source ownership of each annotation.

It should not manufacture:

```text
C.annotations = [Foo, Bar]
```

as direct declaration metadata.

---

# Duplicate Class Annotations Across Ancestry

Example:

```go
class A @{Role("admin")} {
}

class B @{Role("operator")} {
}

class C : A, B {
}
```

An ancestry-aware lookup should be able to expose both annotation instances.

It is the consuming library's responsibility to decide whether:

* both apply
* one overrides another
* the combination is invalid
* one is more specific

The language must not silently choose one.

---

# Annotation Ordering

Direct annotations retain source declaration order.

For inherited member annotations, retain the order from the original declaration.

For ancestry-aware class lookup, use deterministic ancestry traversal order consistent with Go++ class resolution.

Recommended:

```text
concrete class
then parents in declared order
recursively according to normal MRO
```

Do not invent a separate annotation-specific inheritance order.

---

# Immutable Declaration Metadata

Annotation descriptors should be treated as immutable declaration metadata.

Resolving annotations for a subclass must not mutate metadata belonging to its parent.

Example:

```go
Base.class.annotations
```

must remain unchanged after inspecting:

```go
Child.class
```

Effective/inherited views should be computed separately.

---

# Effective Member Sets

Class descriptors should expose effective inherited members.

Conceptually:

```text
declared parent members
        ↓
inheritance
        ↓
override resolution
        ↓
multiple-inheritance conflict resolution
        ↓
effective member set
```

Annotations are inspected only after normal member resolution.

There should not be a separate annotation inheritance engine for fields/methods.

---

# Static Methods

Static methods follow the same annotation rules.

Example:

```go
class Base {
    static func Parse() @{Foo} {
    }
}

class Child : Base {
}
```

If static method lookup allows `Child.Parse`, the inherited static method retains `Foo`.

If `Child` shadows it:

```go
class Child : Base {
    static func Parse() {
    }
}
```

the new declaration does not inherit `Foo`.

---

# Generated Methods

Compiler- or annotation-generated methods are declarations too.

If a generated method carries annotations, those annotations follow the same inheritance rules as manually declared methods.

No special annotation inheritance semantics are needed for generated members.

---

# Generated Fields

Likewise, generated fields—if supported—must behave as ordinary declarations with ordinary annotation metadata.

---

# Annotation Introspection Requirements

The class metadata system should make it possible to determine:

```text
class ancestry

direct class annotations

effective inherited fields

effective inherited methods

member owner

member annotations

parameter annotations
```

This is sufficient for libraries to implement higher-level behavior.

---

# Example: General Library Use

Given:

```go
annotation (
    Persisted on field
    Handler on method
    Feature(name string) on class
)

class Base @{
    Feature("base")
} {
    Id int @{Persisted}

    func Run() @{Handler} {
    }
}

class App : Base @{
    Feature("app")
} {
}
```

Then:

```text
App.class.annotations
    Feature("app")

App.class.parents
    Base

App.class.fields
    Base.Id
        Persisted

App.class.methods
    Base.Run
        Handler
```

A library may additionally walk ancestry and discover:

```text
Base.Feature("base")
```

when it explicitly wants class-level inherited configuration.

---

# Override Example

```go
class App : Base {
    func Run() {
    }
}
```

Now:

```text
App.class.methods
    App.Run
        no annotations
```

The original:

```text
Base.Run
    Handler
```

still exists on `Base`, but is no longer the effective `Run` member of `App`.

---

# General Rule Summary

## Members

```text
inherited field
    keeps annotations

inherited method
    keeps annotations

inherited parameter metadata
    keeps annotations

overridden method
    new declaration
    no automatic annotation copy

hidden/redeclared field
    new declaration
    no automatic annotation copy
```

## Classes

```text
class annotation
    belongs to declaring class

subclass
    does not receive physical copies

ancestor annotations
    remain discoverable through ancestry

libraries
    decide whether ancestor class annotations semantically apply
```

---

# Compiler Responsibilities

The compiler should:

* parse and type-check annotations normally
* preserve annotation symbol identity
* attach annotations to declarations
* preserve annotations on inherited members
* preserve original declaration owner
* expose class ancestry
* expose effective member sets
* avoid copying class annotations into subclasses
* avoid copying overridden-member annotations to overriding declarations
* provide enough metadata for explicit ancestry-aware class lookup

The compiler should not:

* assign HTTP/ORM/serialization-specific inheritance meaning
* merge ambiguous method annotations
* silently flatten all parent class annotations
* decide library-specific precedence policies

---

# Tests

## Inherited Method Annotation

```go
class A {
    func Run() @{Foo} {}
}

class B : A {}
```

Verify `B`'s effective `Run` retains `Foo`.

---

## Overridden Method Annotation

```go
class B : A {
    func Run() {}
}
```

Verify `B.Run` does not contain `Foo`.

---

## Restated Annotation

```go
class B : A {
    func Run() @{Foo} {}
}
```

Verify `B.Run` contains `Foo`.

---

## Inherited Field Annotation

Verify inherited fields retain annotations and original owner.

---

## Parameter Annotation

Verify inherited methods retain parameter annotations.

---

## Direct Class Annotation Lookup

```go
class A @{Foo} {}
class B : A @{Bar} {}
```

Verify:

```text
A.class.annotations = Foo
B.class.annotations = Bar
```

---

## Ancestor Discovery

Verify `B` can discover `A` and inspect `A.class.annotations`.

---

## Multiple Inheritance

Verify distinct members retain their annotation metadata.

Verify ambiguous members are resolved by normal class rules before annotation inspection.

---

## Annotation Identity

Verify inherited annotation lookup uses the original annotation symbol rather than only its textual name.

---

## Metadata Immutability

Verify subclass introspection does not mutate parent descriptors.

---

# Design Principle

The language should not think of annotations as values that are copied down an inheritance tree.

Instead:

> annotations are metadata attached to declarations.

If the declaration itself is inherited, its annotations naturally remain present.

If a declaration is replaced by an override, the replacement has its own annotations.

For classes, ancestry preserves access to parent annotation metadata, while libraries decide whether that metadata should influence subclasses.

The resulting model is:

```text
inherit declaration
    → inherit its annotations

override declaration
    → new annotation set

inherit class
    → retain ancestry
    → do not copy class annotations
```

This provides one general, predictable rule that can support HTTP, ORM, serialization, validation, and future annotation-driven libraries without embedding domain-specific behavior into the Go++ compiler.
