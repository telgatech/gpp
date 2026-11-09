# Go++ Feature Spec: Anonymous and Named Structs

## Goal
Provide one struct model for anonymous structural values and named struct declarations. Preserve anonymous-shape metadata and type inference while using `struct` consistently in Go++ source.

## Primary use case
Database/query helpers where creating hundreds of one-off result structs is undesirable.

## Example

func getUser() struct {
    return struct(
        name: "Bob",
        age: 42,
        active: true,
    )
}

func main() {
    let user = getUser()

    println(user.name)
    println(user.age)
}

## Surface syntax

Anonymous values infer a deterministic structural type from their named fields:

```gpp
user := struct(
    Name: "Ada",
    Active: true,
)
```

Named structs support a short declaration alongside the ordinary Go form:

```gpp
struct User {
    Name string
    Active bool
}

// Equivalent spelling remains supported.
type User struct {
    Name string
    Active bool
}
```

The short form declares one struct at a time. Grouped declarations such as
`struct (Foo { ... } Bar { ... })` are not part of this syntax.

Named structs can be constructed with class-style named arguments. The compiler
lowers this to an ordinary Go keyed composite literal and applies the same field
visibility and type checks as other struct construction:

```gpp
user := User(Name: "Ada", Active: true)
```

The explicit Go composite literal form remains valid:

```gpp
user := User{Name: "Ada", Active: true}
```

`struct(...)` is not a separate type or runtime feature. Structural shape
metadata remains necessary to preserve anonymous struct identity, infer field
types across calls and returns, and emit deterministic Go structs. The metadata
is compiler-internal; Go++ source uses `struct`.

## Required semantics

1. `struct` is a built-in anonymous structural type.

2. `struct Name { ... }` declares one named Go struct. The existing
   `type Name struct { ... }` form remains valid. There is no grouped short
   declaration form.

3. Anonymous struct literals use:

   struct(
       field1: expr,
       field2: expr,
   )

4. Field types are inferred from their expressions.

Example:

struct(
    name: "Bob",
    age: 42,
)

has inferred type equivalent to:

struct {
    age int
    name string
}

5. Function return type may be written simply as:

   func getUser() struct

The compiler infers the exact struct shape from the function's return statements.

6. All return statements in a function returning `struct` must resolve to the same structural struct type.

Valid:

func foo(ok bool) struct {
    if ok {
        return struct(msg: "yes", num: 1)
    }

    return struct(msg: "no", num: 0)
}

Invalid:

func foo(ok bool) struct {
    if ok {
        return struct(msg: "yes", num: 1)
    }

    return struct(message: "no", num: 0)
}

because `msg` and `message` produce different struct shapes.

7. Struct fields retain full static type information at call sites.

Example:

let x = getUser()

x.name   // string
x.age    // int
x.foo    // compile error if foo does not exist

No map[string]any, reflection, interface{}, or dynamic field lookup.

8. Anonymous struct types are structural, not nominal.

These two functions return the same Go++ type:

func a() struct {
    return struct(name: "A", age: 1)
}

func b() struct {
    return struct(age: 2, name: "B")
}

Field declaration order does NOT affect structural identity.

Canonical identity should be based on sorted:

    field name + resolved field type

Example canonical key:

    age:int;name:string

9. Duplicate field names in an anonymous struct literal are compile errors.

Invalid:

struct(
    name: "A",
    name: "B",
)

10. Struct field names follow normal Go++ identifier rules.

11. Struct values are ordinary value types unless a field itself contains a reference/pointer.

12. Structs may be nested.

Example:

struct(
    user: struct(
        name: "Bob",
        age: 42,
    ),
    count: 10,
)

13. Structs may be used inside ordinary Go++ types:

[]struct
map[string]struct

However, generic `struct` without a known shape must only be accepted where the compiler can infer one concrete struct type.

Example:

func users() []struct {
    return []struct{
        struct(name: "A", age: 1),
        struct(name: "B", age: 2),
    }
}

Both elements must resolve to the same structural type.

14. Structs may be assigned when their structural types match.

Example:

let a = struct(name: "A", age: 1)
let b = struct(age: 2, name: "B")

a = b // valid

15. Structs with different fields or field types are incompatible.

Invalid:

let a = struct(name: "A", age: 1)
let b = struct(name: "B", age: "2")

a = b

## Compiler representation

Anonymous struct shape information remains compiler-internal metadata. The
compiler uses it to infer and compare types, then lowers each shape to an
ordinary Go struct. The internal names are not part of Go++ source syntax.

The metadata is conceptually equivalent to:

StructType
StructLiteral
StructField

Example:

type StructLiteral struct {
    Fields []StructField
}

type StructField struct {
    Name  string
    Value Expr
}

## Semantic analysis

For each struct literal:

1. Resolve each field expression type.
2. Reject duplicate field names.
3. Build canonical structural struct type.
4. Intern/deduplicate struct types globally or per package.
5. Attach resolved StructType to the literal.

For functions declared:

func foo() struct

infer the return type from returned struct expressions.

After inference, replace the unresolved `struct` return type internally with the exact resolved structural StructType.

## Generated Go

Every distinct struct shape must lower to a generated Go struct.

Example Go++:

func getUser() struct {
    return struct(
        name: "Bob",
        age: 42,
    )
}

Generated Go conceptually:

type generatedStructName struct {
    name string
    age  int
}

func getUser() generatedStructName {
    return generatedStructName{
        name: "Bob",
        age: 42,
    }
}

Generated struct names must be deterministic.

Preferred approach:
hash the canonical structural signature.

Example:

an implementation-specific deterministic name

rather than depending on declaration order.

This improves deterministic builds.

## Cross-package behavior

Exported Go++ functions returning structs must generate a Go-visible concrete return type.

Example:

func GetUser() struct {
    return struct(
        Name: "Bob",
        Age: 42,
    )
}

Generated Go:

type __GppStruct_<hash> struct {
    Name string
    Age  int
}

func GetUser() __GppStruct_<hash> {
    ...
}

The generated type itself may have an implementation-specific name, but it must be accessible enough for Go to compile exported function signatures.

Important:
exported struct fields should preserve Go capitalization/export semantics.

A Go++ package caller should be able to write:

let u = users.GetUser()
println(u.Name)

Handwritten Go should also be able to call the generated function and access exported fields.

## Interop requirements

Structs must lower entirely to ordinary Go structs.

No custom runtime representation.

Therefore structs should naturally support:

- assignment
- slices
- maps
- function arguments
- function returns
- equality whenever the generated Go struct is comparable
- JSON encoding when fields are exported
- passing to generic Go functions where structurally-generated Go types are accepted

Do NOT attempt structural compatibility with arbitrary native Go structs automatically in v1.

For example:

type User struct {
    Name string
    Age int
}

and:

struct(Name: "Bob", Age: 42)

may have identical fields but remain different Go-level types unless an explicit conversion feature is added later.

## Parser

`struct` introduces anonymous values, the anonymous structural type marker, and
the single named declaration shorthand. The existing Go `type` declaration is
also accepted.

Supported syntax:

struct(...)
func foo() struct
struct User { ... }
type User struct { ... }
User(Name: "Ada")

Grouped short declarations are not supported. Declare each shorthand struct
with its own `struct Name { ... }` declaration.

Native Go composite literals and struct types remain supported:

struct{
    name string
    age int
}

Named struct construction with positional or named arguments lowers to ordinary
Go composite literals. Anonymous values retain named fields and inferred shape
metadata internally.

## Field access

Existing selector syntax should work unchanged:

value.field

The type checker resolves fields from StructType exactly as it would fields from a struct/class.

## Diagnostics

Good errors should include the inferred shapes.

Example:

inconsistent struct return types in foo

expected:
    struct{name string, age int}

got:
    struct{name string, age string}

## Suggested implementation order

1. Keep anonymous struct metadata in the compiler for shape inference.
2. Lower anonymous values and inferred types to ordinary Go structs.
3. Parse one-at-a-time `struct Name { ... }` declarations through Go's type AST.
4. Lower named `Name(...)` construction to keyed Go composite literals.
5. Preserve native Go struct declarations and composite literals.
6. Test parsing, lowering, static typing, and editor completion.

## Tests

Basic:

func foo() struct {
    return struct(msg: "ok", num: 42)
}

Ensure:

foo().msg == string
foo().num == int

Order independence:

struct(a: 1, b: "x")
struct(b: "y", a: 2)

must resolve to the same structural type.

Mismatch:

struct(a: 1)
struct(a: "1")

must be incompatible.

Return consistency:

func foo(x bool) struct {
    if x {
        return struct(a: 1)
    }
    return struct(a: 2)
}

valid.

func foo(x bool) struct {
    if x {
        return struct(a: 1)
    }
    return struct(b: 2)
}

compile error.

Database-style example:

func getUser(id int) struct {
    return struct(
        id: id,
        name: "Alice",
        email: `"alice@example.com"`,
    )
}

func main() {
    user := getUser(1)

    println(user.id)
    println(user.name)
    println(user.email)
}

## Design principle

`struct` should feel like an anonymous Go struct whose exact type is inferred and preserved by the compiler.

It must provide:
- zero runtime overhead
- full static typing
- deterministic lowering to Go
- direct field access
- no mandatory named DTO/result struct

## Current implementation status

The initial implementation supports struct literals, nested structs, structural
hash-based generated Go structs, inferred `struct`/`[]struct`/`map[string]struct`
function returns, local struct collection literals, `let` bindings, duplicate
field diagnostics, inconsistent-return diagnostics, and exported-field
cross-package returns. Structural compatibility is provided by reusing the same
deterministic generated type name; ordinary Go compilation remains responsible
for final assignment/equality/type-checking diagnostics.

Struct parameters are inferred from same-program call sites. Cross-file or
cross-package inference, and arbitrary standalone `struct` type positions
without an inferable concrete shape, remain intentionally deferred until the
type resolver can analyze all call sites. This preserves the spec rule that an
unresolved generic `struct` must not become a dynamic map or `any` value.
