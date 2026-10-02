# Go++ Feature Spec: Declared Annotations

## Goal

Add first-class annotation declarations to Go++.

Annotations should behave like normal package declarations, similar in spirit to:

import
var
const
type
func

Annotations are reusable, type-checked metadata definitions that may be exported by libraries and applied using:

@{...}

Their meaning remains library-defined unless the compiler explicitly reserves a specific annotation in the future.

## 1. Declaration syntax

Single declaration:

annotation Table(name string) on class

annotation PK on field

annotation Required on field, parameter

Grouped declaration:

annotation (
    Table(name string) on class
    Column(name string) on field
    PK on field
    Auto on field
    Required on field, parameter
    Get(path string) on method
    Post(path string) on method
    Auth on class, method
)

This should feel consistent with:

import (...)
var (...)
const (...)

## 2. Annotation parameters

Annotations may have zero or more typed parameters.

Examples:

annotation PK on field

annotation Table(name string) on class

annotation Range(min int, max int) on field

annotation Route(method string, path string) on method

Parameter syntax should reuse ordinary Go++ parameter/type parsing where practical.

Annotations are invoked like functions:

@{Table("employees")}

@{Range(1, 100)}

Zero-argument annotations omit parentheses:

@{PK}

not:

@{PK()}

Both could optionally be tolerated, but the preferred form should be:

@{PK}

## 3. Group declarations

Syntax:

annotation (
    Foo
    Bar(x int)
    Baz(name string) on class
)

Each entry behaves exactly as an independent annotation declaration.

Conceptually:

annotation Foo
annotation Bar(x int)
annotation Baz(name string) on class

## 4. Package visibility

Annotation declarations follow normal Go/Go++ capitalization rules.

Example:

package model

annotation (
    Table(name string) on class
    PK on field
    internal on field
)

`Table` and `PK` are exported.

`internal` is package-private.

Consumer:

import "example.com/model"

class Employee @{model.Table("employees")} {
    Id string @{model.PK}
}

Accessing:

@{model.internal}

from another package must be rejected.

## 5. Annotation usage

Annotations are attached using postfix:

@{...}

Examples:

class Employee @{model.Table("employees")} {
    Id string @{model.PK, model.Auto}
    Name string @{validate.Required}
}

Method:

func Show(id int) record @{web.Get("/users/:id"), web.Auth} {
}

Parameter:

func Register(
    Email string @{validate.Required, validate.Email},
) {
}

Annotations in the current package may be unqualified:

@{PK, Auto}

Imported annotations normally require package qualification:

@{model.PK}

## 6. `on` target restriction

An annotation declaration may optionally restrict where it can be used.

Syntax:

annotation Name(...) on target

or:

annotation Name(...) on target, target

Examples:

annotation (
    Table(name string) on class
    Column(name string) on field
    Required on field, parameter
    Auth on class, method
)

If `on` is omitted, the annotation is valid on every annotation-capable declaration.

Example:

annotation Deprecated(message string)

may be used anywhere annotations are supported.

## 7. Initial annotation target vocabulary

Support these targets initially:

class
field
method
function
parameter
type
package

Recommended meanings:

class
    Go++ class declarations

field
    class fields and other supported field declarations

method
    methods declared inside classes/extensions where applicable

function
    package-level functions

parameter
    function/method parameters

type
    ordinary type declarations

package
    package declarations

Do not add targets such as variable, constant, import, etc. until there is a real use case.

## 8. Method vs function

`method` and `function` should remain distinct annotation targets.

Example:

annotation Route(path string) on method

cannot be used on:

func Helper() {}

if Helper is a package-level function.

If an annotation should work on both:

annotation Trace on method, function

## 9. Placement validation

The compiler must verify that each annotation is legal on its declaration target.

Example:

annotation PK on field

Invalid:

class Employee @{PK} {}

Error should resemble:

annotation PK cannot be applied to class
allowed targets: field

Another example:

annotation Get(path string) on method

Invalid:

Name string @{Get("/foo")}

Error:

annotation Get cannot be applied to field
allowed targets: method

## 10. Symbol resolution

Annotation use must resolve to a declared annotation symbol.

Example:

@{model.Table("employees")}

must resolve:

package:
    model

symbol:
    Table

kind:
    annotation

Using a non-annotation symbol inside `@{...}` must fail.

Example:

func Table(name string) {}

class X @{Table("x")} {}

should fail if Table resolves to a function rather than an annotation.

## 11. Undeclared annotations

Do NOT allow arbitrary undeclared annotation names.

Invalid:

Name string @{requird}

unless `requird` is actually declared.

This prevents typos from silently becoming metadata.

Local custom annotations remain cheap:

annotation Required on field

So there is little reason to permit undeclared names.

## 12. Argument type checking

Annotation arguments must be checked against the declaration.

Example:

annotation Table(name string) on class

Valid:

@{Table("employees")}

Invalid:

@{Table(123)}

Error:

cannot use int as string argument 1 to annotation Table

Arity must also be checked.

Invalid:

@{Table()}

@{Table("employees", "extra")}

## 13. Allowed annotation argument values

For v1, annotation arguments should be compile-time values.

Recommended supported forms:

string literals
integer literals
float literals
bool literals
nil where type-compatible
constant expressions
possibly enum-like/constants already known at compile time

Do not require arbitrary runtime expressions.

Example:

const TableName = "employees"

@{Table(TableName)}

should work.

This may be rejected initially if constant-expression evaluation is not yet available, but should be the intended direction.

## 14. AST representation

Declaration:

type AnnotationDecl struct {
    Name       string
    Params     []ParamDecl
    Targets    []AnnotationTarget
    Exported   bool
    Position   Position
}

Use:

type AnnotationUse struct {
    Symbol     *AnnotationDecl
    Args       []Expr
    Position   Position
}

Grouped declarations may be represented as:

type AnnotationDeclGroup struct {
    Decls []*AnnotationDecl
}

or flattened immediately during parsing.

## 15. Annotation targets representation

Use an enum/bitset internally.

Conceptually:

type AnnotationTarget int

const (
    AnnotationTargetClass AnnotationTarget = ...
    AnnotationTargetField
    AnnotationTargetMethod
    AnnotationTargetFunction
    AnnotationTargetParameter
    AnnotationTargetType
    AnnotationTargetPackage
)

AnnotationDecl may store:

Targets []AnnotationTarget

or a bitmask.

Empty target set means unrestricted.

## 16. Grammar

Conceptually:

AnnotationDecl
    <- "annotation" (
           AnnotationSpec
         / "(" AnnotationSpec* ")"
       )

AnnotationSpec
    <- Identifier AnnotationParams? AnnotationTargets?

AnnotationParams
    <- "(" Params? ")"

AnnotationTargets
    <- "on" AnnotationTarget ("," AnnotationTarget)*

AnnotationTarget
    <- "class"
     / "field"
     / "method"
     / "function"
     / "parameter"
     / "type"
     / "package"

Examples:

annotation PK on field

annotation Table(name string) on class

annotation (
    PK on field
    Table(name string) on class
)

## 17. Application grammar

Existing annotation application syntax:

@{ AnnotationUseList }

AnnotationUseList
    <- AnnotationUse ("," AnnotationUse)*

AnnotationUse
    <- QualifiedIdentifier AnnotationArgs?

Examples:

@{PK}

@{model.PK}

@{model.Table("employees")}

@{validate.Required, validate.Email}

## 18. Introspection integration

Declared annotation metadata must integrate with:

.class
.fields
.methods
.annotations

Examples:

Employee.class.annotations

field.annotations

method.annotations

Annotation descriptors should expose at least:

annotation.name
annotation.fullName
annotation.args

Recommended:

annotation.declaration

or equivalent internal link may exist, but does not need to be public in v1.

## 19. Annotation descriptor identity

Annotations with identical names from different packages are different annotation types.

Example:

web.Auth
rpc.Auth

must remain distinct.

Do not identify annotations only by string name.

Descriptor identity should resolve to the actual declaration symbol.

Therefore:

annotations.has(web.Auth)

would eventually be preferable to:

annotations.has("Auth")

But for ergonomic v1 introspection, string lookup may still exist.

Recommended long-term API:

annotations.has(web.Auth)
annotations.find(web.Auth)

Optional convenience:

annotations.has("Auth")

String lookup must not be the sole identity mechanism.

## 20. Recommended introspection improvement

Since annotations are now declared symbols, expose declaration-safe lookup.

Example:

if m.annotations.has(web.Get) {
    a := m.annotations.find(web.Get)
}

This is superior to:

m.annotations.find("get")

because it is:

- typo-safe
- package-safe
- rename-safe
- IDE-friendly

Recommended API:

annotations.has(annotationType) bool
annotations.find(annotationType) *Annotation
annotations.all(annotationType) []Annotation

String-based overloads may optionally remain.

## 21. Annotation metadata example

Library:

package model

annotation (
    Table(name string) on class
    Column(name string) on field
    PK on field
    Auto on field
)

Application:

class Employee @{model.Table("employees")} {
    Id string @{model.PK, model.Auto}
    Name string
}

Introspection:

table := Employee.class.annotations.find(model.Table)

println(table.args[0])

for f := range Employee.class.fields {
    if f.annotations.has(model.PK) {
        println("primary key:", f.name)
    }
}

## 22. Web example

Package:

package web

annotation (
    Get(path string) on method
    Post(path string) on method
    Auth on class, method
    Role(name string) on class, method
)

Application:

class Users @{web.Auth} {
    func Show(id int) record @{web.Get("/users/:id")} {
        ...
    }

    func Create() record @{web.Post("/users"), web.Role("admin")} {
        ...
    }
}

Router library may introspect:

Users.class.annotations
Users.class.methods
method.annotations

The compiler itself does not interpret Get/Post/Auth/Role.

## 23. Validation example

Package:

package validate

annotation (
    Required on field, parameter
    Email on field, parameter
    Min(value int) on field, parameter
    Max(value int) on field, parameter
)

Application:

class Registration {
    Email string @{validate.Required, validate.Email}
    Age int @{validate.Min(18)}
}

Validation library reads metadata.

No validation behavior is compiler built-in.

## 24. Database example

Package:

package model

annotation (
    Table(name string) on class
    Column(name string) on field
    PK on field
    Auto on field
    Ignore on field
)

Application:

class Employee : Model @{model.Table("employees")} {
    Id string @{model.PK, model.Auto}
    Name string
    PasswordHash string @{model.Ignore}
}

Generic database/sql helper code can inspect these annotations.

No ORM behavior is part of the language.

## 25. Inheritance semantics

Class-level annotations are not inherited automatically.

Example:

class Model @{A} {}
class Employee : Model {}

Employee.class.annotations.has(A)

recommended result:

false

Libraries may inspect:

Employee.class.parents

if they want inherited class metadata.

Field annotations remain attached to inherited fields.

Method annotations remain attached to inherited methods.

This preserves declaration ownership.

## 26. Duplicate annotation usage

Allow repeated applications of the same annotation unless the declaration later gains a uniqueness constraint.

Example:

annotation Index(name string) on field

Field:

Email string @{
    Index("email_lookup"),
    Index("email_unique")
}

valid.

Therefore:

annotations.find(Index)

returns first in source order.

annotations.all(Index)

returns all.

Do not add "single-use" annotation declarations in v1.

## 27. No compiler semantics by default

Declaring:

annotation PK on field

does not make a field a database primary key.

Declaring:

annotation Get(path string) on method

does not create a route.

Declaring:

annotation Required on field

does not create validation.

Annotations define:

- a metadata type
- allowed targets
- parameter types

Libraries define behavior.

## 28. Future macro integration

The declaration system should leave room for annotations to later trigger compile-time transformations.

Possible future syntax or metadata:

annotation Timestamps on class macro ...
annotation Derive(types...) on class macro ...

Do NOT implement macro execution as part of this feature.

Important:

ordinary declared annotations must remain passive metadata unless explicitly defined otherwise.

## 29. Go struct tags

Declared annotations do not automatically become Go struct tags.

Example:

annotation JSON(name string) on field

Name string @{JSON("name")}

should remain annotation metadata unless a library/compiler mapping explicitly exists.

A future feature may allow annotation declarations to specify tag lowering.

Do not couple this feature to Go tags initially.

## 30. Diagnostics

Unknown annotation:

@{Foo}

error:

undefined annotation Foo

Wrong symbol kind:

@{Foo}

when Foo is a function:

Foo is a function, not an annotation

Wrong target:

annotation PK on field

class X @{PK} {}

error:

annotation PK cannot be applied to class
allowed target: field

Wrong arity:

annotation Min(value int)

@{Min()}

error:

annotation Min expects 1 argument, got 0

Wrong argument type:

@{Min("18")}

error:

argument 1 to annotation Min:
expected int
got string

Unexported imported annotation:

@{other.internal}

error according to normal package visibility rules.

## 31. Suggested implementation order

1. Add `annotation` declaration keyword.
2. Parse single annotation declarations.
3. Parse grouped annotation declarations.
4. Parse parameters.
5. Parse `on` target lists.
6. Add AnnotationDecl AST.
7. Register annotation symbols in package scope.
8. Support export visibility.
9. Resolve `@{...}` uses to annotation declarations.
10. Validate argument count.
11. Validate argument types.
12. Validate `on` placement.
13. Generate annotation metadata descriptors.
14. Integrate with `.annotations`.
15. Support symbol-based introspection lookup.
16. Add inheritance tests.
17. Add package/import tests.

## 32. Core tests

Single declaration:

annotation PK on field

Grouped:

annotation (
    PK on field
    Table(name string) on class
)

Class use:

class Employee @{Table("employees")} {}

Field use:

class Employee {
    Id string @{PK}
}

Invalid target:

class Employee @{PK} {}

must fail.

Argument type:

annotation Min(n int) on field

Age int @{Min(18)}

valid.

Age int @{Min("18")}

invalid.

Export:

package foo

annotation Public on field
annotation private on field

other package may use foo.Public but not foo.private.

Package collision:

package web
annotation Auth on method

package rpc
annotation Auth on method

Both may be imported and used independently:

@{web.Auth}
@{rpc.Auth}

## 33. Design principle

Annotations are real declarations.

They should have:

- names
- package ownership
- visibility
- typed parameters
- legal targets
- stable symbol identity
- introspection metadata

Example:

annotation (
    Table(name string) on class
    PK on field
    Required on field, parameter
)

should feel as native to Go++ as:

import (...)
var (...)
const (...)

The language defines the annotation contract.

Libraries define what the annotation means.
