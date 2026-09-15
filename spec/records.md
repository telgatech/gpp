Go++ Feature Spec: Anonymous Records

Goal
Add anonymous, statically typed records for returning ad-hoc structured data without declaring named structs.

Primary use case
Database/query helpers where creating hundreds of one-off result structs is undesirable.

Example

func getUser() record {
    return record(
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

Required semantics

1. `record` is a built-in anonymous structural type.

2. Record literals use:

   record(
       field1: expr,
       field2: expr,
   )

3. Field types are inferred from their expressions.

Example:

record(
    name: "Bob",
    age: 42,
)

has inferred type equivalent to:

record {
    age int
    name string
}

4. Function return type may be written simply as:

   func getUser() record

The compiler infers the exact record shape from the function's return statements.

5. All return statements in a function returning `record` must resolve to the same structural record type.

Valid:

func foo(ok bool) record {
    if ok {
        return record(msg: "yes", num: 1)
    }

    return record(msg: "no", num: 0)
}

Invalid:

func foo(ok bool) record {
    if ok {
        return record(msg: "yes", num: 1)
    }

    return record(message: "no", num: 0)
}

because `msg` and `message` produce different record shapes.

6. Record fields retain full static type information at call sites.

Example:

let x = getUser()

x.name   // string
x.age    // int
x.foo    // compile error if foo does not exist

No map[string]any, reflection, interface{}, or dynamic field lookup.

7. Record types are structural, not nominal.

These two functions return the same Go++ type:

func a() record {
    return record(name: "A", age: 1)
}

func b() record {
    return record(age: 2, name: "B")
}

Field declaration order does NOT affect structural identity.

Canonical identity should be based on sorted:

    field name + resolved field type

Example canonical key:

    age:int;name:string

8. Duplicate field names in a record literal are compile errors.

Invalid:

record(
    name: "A",
    name: "B",
)

9. Record field names follow normal Go++ identifier rules.

10. Record values are ordinary value types unless a field itself contains a reference/pointer.

11. Records may be nested.

Example:

record(
    user: record(
        name: "Bob",
        age: 42,
    ),
    count: 10,
)

12. Records may be used inside ordinary Go++ types:

[]record
map[string]record

However, generic `record` without a known shape must only be accepted where the compiler can infer one concrete record type.

Example:

func users() []record {
    return []record{
        record(name: "A", age: 1),
        record(name: "B", age: 2),
    }
}

Both elements must resolve to the same structural type.

13. Records may be assigned when their structural types match.

Example:

let a = record(name: "A", age: 1)
let b = record(age: 2, name: "B")

a = b // valid

14. Records with different fields or field types are incompatible.

Invalid:

let a = record(name: "A", age: 1)
let b = record(name: "B", age: "2")

a = b

Compiler representation

Add AST nodes roughly equivalent to:

RecordType
RecordLiteral
RecordField

Example:

type RecordLiteral struct {
    Fields []RecordField
}

type RecordField struct {
    Name  string
    Value Expr
}

Semantic analysis

For each record literal:

1. Resolve each field expression type.
2. Reject duplicate field names.
3. Build canonical structural record type.
4. Intern/deduplicate record types globally or per package.
5. Attach resolved RecordType to the literal.

For functions declared:

func foo() record

infer the return type from returned record expressions.

After inference, replace the unresolved `record` return type internally with the exact resolved structural RecordType.

Generated Go

Every distinct record shape must lower to a generated Go struct.

Example Go++:

func getUser() record {
    return record(
        name: "Bob",
        age: 42,
    )
}

Generated Go conceptually:

type __gpp_record_1 struct {
    name string
    age  int
}

func getUser() __gpp_record_1 {
    return __gpp_record_1{
        name: "Bob",
        age: 42,
    }
}

Generated struct names must be deterministic.

Preferred approach:
hash the canonical structural signature.

Example:

__gpp_record_a81f09c2

rather than depending on declaration order.

This improves deterministic builds.

Cross-package behavior

Exported Go++ functions returning records must generate a Go-visible concrete return type.

Example:

func GetUser() record {
    return record(
        Name: "Bob",
        Age: 42,
    )
}

Generated Go:

type __GppRecord_<hash> struct {
    Name string
    Age  int
}

func GetUser() __GppRecord_<hash> {
    ...
}

The generated type itself may have an implementation-specific name, but it must be accessible enough for Go to compile exported function signatures.

Important:
exported record fields should preserve Go capitalization/export semantics.

A Go++ package caller should be able to write:

let u = users.GetUser()
println(u.Name)

Handwritten Go should also be able to call the generated function and access exported fields.

Interop requirements

Records must lower entirely to ordinary Go structs.

No custom runtime representation.

Therefore records should naturally support:

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

record(Name: "Bob", Age: 42)

may have identical fields but remain different Go-level types unless an explicit conversion feature is added later.

Parser

Add `record` as a contextual/built-in keyword where necessary.

Required syntax initially:

record(...)
func foo() record

Avoid adding explicit record type declarations in v1 unless required.

Possible future syntax:

record{
    name string
    age int
}

but NOT required for initial implementation.

Field access

Existing selector syntax should work unchanged:

value.field

The type checker resolves fields from RecordType exactly as it would fields from a struct/class.

Diagnostics

Good errors should include the inferred shapes.

Example:

inconsistent record return types in foo

expected:
    record{name string, age int}

got:
    record{name string, age string}

Suggested implementation order

1. Add RecordLiteral AST.
2. Parse `record(name: expr, ...)`.
3. Infer record field types.
4. Add structural RecordType.
5. Add canonical type interning.
6. Support selector access.
7. Emit generated Go structs.
8. Support `func foo() record` inference.
9. Validate all return paths.
10. Support []record inference.
11. Add cross-package/export handling.
12. Add tests.

Tests

Basic:

func foo() record {
    return record(msg: "ok", num: 42)
}

Ensure:

foo().msg == string
foo().num == int

Order independence:

record(a: 1, b: "x")
record(b: "y", a: 2)

must resolve to the same structural type.

Mismatch:

record(a: 1)
record(a: "1")

must be incompatible.

Return consistency:

func foo(x bool) record {
    if x {
        return record(a: 1)
    }
    return record(a: 2)
}

valid.

func foo(x bool) record {
    if x {
        return record(a: 1)
    }
    return record(b: 2)
}

compile error.

Database-style example:

func getUser(id int) record {
    return record(
        id: id,
        name: "Alice",
        email: "alice@example.com",
    )
}

func main() {
    user := getUser(1)

    println(user.id)
    println(user.name)
    println(user.email)
}

Design principle

`record` should feel like an anonymous Go struct whose exact type is inferred and preserved by the compiler.

It must provide:
- zero runtime overhead
- full static typing
- deterministic lowering to Go
- direct field access
- no mandatory named DTO/result struct

Current implementation status

The initial implementation supports record literals, nested records, structural
hash-based generated Go structs, inferred `record`/`[]record`/`map[string]record`
function returns, local record collection literals, `let` bindings, duplicate
field diagnostics, inconsistent-return diagnostics, and exported-field
cross-package returns. Structural compatibility is provided by reusing the same
deterministic generated type name; ordinary Go compilation remains responsible
for final assignment/equality/type-checking diagnostics.

Record parameters are inferred from same-program call sites. Cross-file or
cross-package inference, and arbitrary standalone `record` type positions
without an inferable concrete shape, remain intentionally deferred until the
type resolver can analyze all call sites. This preserves the spec rule that an
unresolved generic `record` must not become a dynamic map or `any` value.
