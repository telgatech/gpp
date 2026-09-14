Go++ Feature Spec: Extension Methods

Goal

Allow Go++ code to add methods to existing Go and Go++ types without modifying, wrapping, embedding, or subclassing those types.

Primary use case:

extend sql.DB {
    func Find[T](id any) (T, error) {
        ...
    }

    func Exists[T](filters record) (bool, error) {
        ...
    }
}

Usage:

employee, err := db.Find[Employee](42)

exists, err := db.Exists[Employee](
    record(Email: "a@b.com"),
)

`db` remains the real `*sql.DB`.

No runtime monkey-patching or modification of database/sql occurs.

Core syntax

extend <type> {
    <method declarations>
}

Example:

extend sql.DB {
    func PingAgain() error {
        return this.Ping()
    }
}

Generic extension:

extend sql.DB {
    func Find[T](id any) (T, error) {
        ...
    }
}

Extension methods use implicit `this`, consistent with Go++ class methods.

Target types

Extensions must work with:

1. native Go types
2. imported Go types
3. Go++ classes
4. instantiated generic types where practical

Examples:

extend string {
    func Empty() bool {
        return len(this) == 0
    }
}

extend sql.DB {
    ...
}

extend http.Request {
    ...
}

extend Employee {
    ...
}

Pointer/value receiver semantics

The compiler must distinguish receiver type correctly.

Recommended syntax:

extend sql.DB {
    ...
}

should behave naturally for ordinary Go addressable `sql.DB` values and `*sql.DB` values according to Go method-call conventions.

If explicit control is needed, also allow:

extend *sql.DB {
    ...
}

Initial implementation may normalize:

extend sql.DB

to the same receiver-adjustment behavior Go uses for methods where the call target is addressable.

Do not introduce copies unexpectedly for mutable native Go structs.

AST

Add an extension declaration:

type ExtendDecl struct {
    Target  TypeExpr
    Methods []*FuncDecl
}

Each extension method should retain:

- declared name
- generic parameters
- parameters
- return types
- body
- resolved extension target type

Extension registration

During semantic analysis, maintain an extension-method registry.

Conceptually:

map[ResolvedType][]ExtensionMethod

Example:

sql.DB:
    Find[T]
    Where[T]
    Exists[T]

string:
    Empty
    Capitalize

Extensions are compile-time symbols only.

They do not alter Go runtime type metadata or Go++ vtables.

Call resolution

For an expression:

x.Method(args...)

resolve in this order:

1. Real/native methods declared by the receiver type.
2. Go++ inherited/polymorphic methods.
3. Visible extension methods.

A real method always wins over an extension with the same applicable signature.

Example:

If `sql.DB` someday contains:

func (db *DB) Find(...)

then:

db.Find(...)

must resolve to the real method, not the Go++ extension.

No silent override of actual methods.

Generic method resolution

Example:

employee, err := db.Find[Employee](42)

Resolve:

receiver:
    db : *sql.DB

extension:
    Find[T]

type arguments:
    T = Employee

Then type-check parameters and returns normally.

Generated Go

Extension methods lower to ordinary package-level functions.

Go++:

extend sql.DB {
    func Find[T](id any) (T, error) {
        ...
    }
}

Conceptual generated Go:

func __gpp_ext_sql_DB_Find[T any](
    this *sql.DB,
    id any,
) (T, error) {
    ...
}

Call:

db.Find[Employee](42)

lowers to:

__gpp_ext_sql_DB_Find[Employee](db, 42)

Exact generated symbol naming is implementation-defined but must be:

- deterministic
- collision-safe
- package-safe

No runtime lookup is required.

`this`

Inside an extension method:

this

refers to the receiver object/value.

Example:

extend sql.DB {
    func PingAgain() error {
        return this.Ping()
    }
}

Generated conceptually:

func __gpp_ext_sql_DB_PingAgain(this *sql.DB) error {
    return this.Ping()
}

Access restrictions

Extension methods do NOT gain privileged access to the extended type.

For foreign Go types they may only access what ordinary Go code could access.

Example:

extend http.Request {
    func Foo() {
        // exported fields/methods allowed
        // unexported net/http internals forbidden
    }
}

Extensions are syntax sugar over ordinary functions, not friend methods.

Dispatch semantics

Extension methods are always statically dispatched.

They do NOT:

- occupy vtable slots
- override class methods
- become virtual methods
- participate in polymorphic dispatch
- change interface satisfaction

Example:

extend Animal {
    func Foo() {}
}

does not mean subclasses override Foo polymorphically.

If polymorphic behavior is required, define a real class method.

Visibility

Extension methods follow normal package/import visibility rules.

An extension declared in package `foo` is available only when the package/module containing that extension is visible/imported according to normal Go++ rules.

Do not globally modify a type merely because some unrelated package declared an extension for it.

Conflict handling

If two visible extensions provide an equally applicable method and no actual method wins, compilation must fail.

Example:

package A:
extend string {
    func Foo() {}
}

package B:
extend string {
    func Foo() {}
}

If both extensions are visible:

"hello".Foo()

error:

ambiguous extension method Foo for string

Candidates:
    A.Foo
    B.Foo

Do not silently choose one based on import order.

Later we may add explicit qualification if needed.

Overloading

Extension methods participate in normal Go++ overload resolution.

Example:

extend string {
    func Parse() int
    func Parse(base int) int
}

Calls:

s.Parse()
s.Parse(16)

resolve statically.

An extension overload must never hide an applicable real method.

Records

Extension methods should work naturally with the `record` feature.

Example:

extend sql.DB {
    func Exists[T](filters record) (bool, error) {
        ...
    }
}

Usage:

db.Exists[Employee](
    record(
        Email: "bob@example.com",
        Active: true,
    ),
)

The record remains statically typed.

Class introspection

Extension methods must be compatible with planned Go++ class introspection.

Example:

extend sql.DB {
    func Find[T](id any) (T, error) {
        c := T.class

        obj := T()

        // build SELECT using c.fields
        // QueryRow using database/sql
        // Scan into field addresses

        return obj, err
    }
}

No ORM behavior should be built into extension methods themselves.

This feature merely enables library code such as:

db.Find[Employee](...)
db.Where[Employee](...)
db.Exists[Employee](...)

using normal database/sql underneath.

Go interop

Extension methods must preserve direct Go interoperability.

Given:

var db *sql.DB

Go++ may call:

db.Ping()

normally.

If a Go++ extension exists:

db.Find[Employee](42)

only that call is rewritten.

The underlying variable is still exactly:

*sql.DB

No wrapper object should be generated.

No adapters should be required for normal Go functions.

Interfaces

Extension methods must NOT cause a type to satisfy Go interfaces.

Example:

type Fooer interface {
    Foo()
}

extend ExternalType {
    func Foo() {}
}

ExternalType still does NOT implement Fooer from Go's perspective because no real Go method exists on the type.

This rule is important for interoperability.

If interface satisfaction is desired, use a wrapper/class or another explicit mechanism.

Addressability

Follow Go's normal receiver/addressability rules as closely as practical.

Example:

extend *Thing {
    func Mutate() {}
}

Then:

x.Mutate()

may work when x is an addressable Thing and the compiler can safely take &x.

Calling on a temporary/non-addressable value should follow Go-like restrictions.

Parser

Add parsing for:

extend Type {
    methods...
}

Target type parsing should reuse ordinary Go++/Go type parsing.

Examples:

extend string { ... }

extend *sql.DB { ... }

extend pkg.Type { ... }

Potential future syntax:

extend Foo[T] { ... }

but generic target extension declarations may be deferred if unnecessary for v1.

Semantic validation

For each extension declaration:

1. Resolve target type.
2. Ensure target is a valid extendable type.
3. Register methods.
4. Type-check each body with `this` bound to receiver type.
5. Check collisions within the same extension block.
6. Check overload validity.
7. Preserve package visibility.

Suggested v1 restrictions

Keep v1 intentionally simple:

- extension target must resolve to one concrete type
- generic extension methods allowed
- no runtime dispatch
- no extension properties
- no extension fields
- no extension constructors
- no interface satisfaction
- no private-member access
- no implicit import of extensions
- ambiguity is a compile error

Examples

Basic:

extend string {
    func Empty() bool {
        return len(this) == 0
    }
}

func main() {
    name := ""

    if name.Empty() {
        println("empty")
    }
}

Native Go type:

extend *sql.DB {
    func Healthy() bool {
        return this.Ping() == nil
    }
}

Generic DB helper:

extend *sql.DB {
    func Find[T](id any) (T, error) {
        c := T.class
        obj := T()

        // query using database/sql
        // scan fields into obj

        return obj, nil
    }
}

func main() {
    employee, err := db.Find[Employee](42)
}

Lowering example

Input:

extend *sql.DB {
    func Healthy() bool {
        return this.Ping() == nil
    }
}

if db.Healthy() {
    ...
}

Generated conceptual Go:

func __gpp_ext_sql_DB_Healthy(this *sql.DB) bool {
    return this.Ping() == nil
}

if __gpp_ext_sql_DB_Healthy(db) {
    ...
}

Tests

1. Extension on builtin type.
2. Extension on imported Go type.
3. Pointer receiver extension.
4. Generic extension method.
5. Extension on Go++ class.
6. Real method takes precedence.
7. Extension ambiguity produces error.
8. Extension cannot access foreign private field.
9. Extension does not satisfy Go interface.
10. Generated call is plain function call.
11. Go stdlib receiver remains unchanged.
12. Generic return type preserves static type.

Design principle

Extension methods are purely compile-time method-call sugar.

They must preserve the invariant:

    Go++ convenience on the surface
    ordinary Go underneath

They should make code such as:

    db.Find[Employee](42)

possible while `db` remains a normal `*sql.DB` and the generated program continues to use standard database/sql directly.
