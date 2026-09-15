Go++ Feature Spec: Multi-Target Extension Methods

Goal

Allow one `extend` block to apply the same extension methods to multiple target types.

This is syntax sugar only.

A multi-target extension must behave exactly as if the extension block had been duplicated once for each target type.

Core syntax

extend string, []byte {
    func Empty() bool {
        return len(this) == 0
    }
}

Equivalent to:

extend string {
    func Empty() bool {
        return len(this) == 0
    }
}

extend []byte {
    func Empty() bool {
        return len(this) == 0
    }
}

Another example:

extend *sql.DB, *sql.Tx {
    func ExecOne(query string, args ...any) error {
        _, err := this.Exec(query, args...)
        return err
    }
}

Semantics

1. `extend` may accept one or more target types separated by commas.

Single target remains valid:

extend string {
    ...
}

Multiple targets:

extend string, []byte, []rune {
    ...
}

2. Multi-target extension blocks are independently instantiated for every target.

Conceptually:

extend A, B, C {
    methods...
}

is equivalent to:

extend A {
    methods...
}

extend B {
    methods...
}

extend C {
    methods...
}

3. `this` is NOT a union type.

Each target gets its own independently type-checked version of every method.

Example:

extend string, []byte {
    func Empty() bool {
        return len(this) == 0
    }
}

During semantic analysis:

for target string:
    this : string

for target []byte:
    this : []byte

No common receiver type needs to be created.

4. Method bodies must be valid independently for every target.

Valid:

extend string, []byte {
    func Empty() bool {
        return len(this) == 0
    }
}

Invalid:

extend string, int {
    func Empty() bool {
        return len(this) == 0
    }
}

because:

len(string)

is valid, but:

len(int)

is invalid.

Compilation should fail for the invalid target.

5. Diagnostics must identify which target caused the failure.

Example:

extension method Empty is invalid for target int:

    len cannot be applied to int

Declared targets:
    string
    int

Do not report only a generic error against the entire extension block.

6. All methods in the block are instantiated for every target.

Example:

extend string, []byte {
    func Empty() bool {
        return len(this) == 0
    }

    func Size() int {
        return len(this)
    }
}

produces logically:

string.Empty()
string.Size()

[]byte.Empty()
[]byte.Size()

7. Multi-target extensions must support generic extension methods.

Example:

extend *sql.DB, *sql.Tx {
    func QueryOne[T](query string, args ...any) (T, error) {
        ...
    }
}

Each receiver target gets its own generic extension method.

8. Target types may include:

- builtin types
- slices
- maps where otherwise valid
- pointers
- imported Go types
- Go++ classes
- instantiated generic types where extension methods already permit them

Examples:

extend string, []byte {
    ...
}

extend *sql.DB, *sql.Tx {
    ...
}

extend Employee, Customer {
    ...
}

9. Multi-target extensions do not create inheritance or interfaces.

This:

extend A, B {
    func Foo() {}
}

does NOT imply any relationship between A and B.

It is only equivalent to defining the same extension method separately on both.

AST

Update extension declaration representation.

Previously:

type ExtendDecl struct {
    Target  TypeExpr
    Methods []*FuncDecl
}

Change to:

type ExtendDecl struct {
    Targets []TypeExpr
    Methods []*FuncDecl
}

A single-target extension simply has:

len(Targets) == 1

Parser

Support:

extend Type1, Type2, Type3 {
    ...
}

Grammar conceptually:

ExtendDecl
    <- "extend" Type ("," Type)* Block

Target types use the existing normal type parser.

Examples:

extend string, []byte {
}

extend *sql.DB, *sql.Tx {
}

extend foo.Bar, baz.Qux {
}

Whitespace and line breaks should work normally:

extend
    *sql.DB,
    *sql.Tx,
    *sql.Conn
{
    ...
}

Semantic lowering

Recommended implementation:

Immediately after parsing/resolving target types, expand:

ExtendDecl{
    Targets: [A, B, C],
    Methods: [...]
}

into independent semantic extension instances:

ExtensionInstance{
    Target: A,
    Methods: cloned/resolved methods
}

ExtensionInstance{
    Target: B,
    Methods: cloned/resolved methods
}

ExtensionInstance{
    Target: C,
    Methods: cloned/resolved methods
}

Each instance is then processed through the existing single-target extension pipeline.

This keeps the rest of the compiler simple.

Do NOT add multi-receiver logic to method resolution.

Do NOT introduce union receiver types.

Type checking

For every target:

1. Bind `this` to that target.
2. Type-check each method body independently.
3. Resolve normal methods visible on that target.
4. Resolve extension method calls normally.
5. Resolve generic calls normally.
6. Report errors against the specific target.

Example:

extend *sql.DB, *sql.Tx {
    func ExecOne(q string, args ...any) error {
        _, err := this.Exec(q, args...)
        return err
    }
}

For *sql.DB:

this.Exec resolves against *sql.DB

For *sql.Tx:

this.Exec resolves against *sql.Tx

Both must succeed independently.

Generated Go

Generate one extension function per target/method combination.

Input:

extend string, []byte {
    func Empty() bool {
        return len(this) == 0
    }
}

Conceptual generated Go:

func __gpp_ext_string_Empty(this string) bool {
    return len(this) == 0
}

func __gpp_ext_slice_byte_Empty(this []byte) bool {
    return len(this) == 0
}

Calls:

s.Empty()

lower to:

__gpp_ext_string_Empty(s)

and:

b.Empty()

lower to:

__gpp_ext_slice_byte_Empty(b)

Generated names must remain deterministic and collision-safe.

Method lookup

No changes to existing lookup precedence.

Continue using:

1. real/native methods
2. Go++ class/inherited methods
3. visible extension methods

Multi-target extensions simply register one extension method independently against each target.

Example:

extend A, B {
    func Foo() {}
}

registry behaves as though:

A -> Foo
B -> Foo

had been declared separately.

Conflict handling

Existing extension conflict rules apply independently per target.

Example:

extend A, B {
    func Foo() {}
}

extend A {
    func Foo() {}
}

This causes an ambiguity/conflict for A according to normal extension rules.

B is unaffected.

Duplicate targets

Reject duplicate targets in the same extension declaration after type normalization.

Invalid:

extend string, string {
    func Foo() {}
}

Error:

duplicate extension target string

Equivalent aliases that resolve to the same type should also count as duplicates where type resolution makes that clear.

Partial success

Compilation must be atomic.

If an extension block is valid for A and B but invalid for C:

extend A, B, C {
    ...
}

the project must fail to compile.

Do not silently register extensions for A and B while ignoring C.

Diagnostics

Preferred error shape:

extension method ExecOne cannot be applied to target SomeType

method:
    ExecOne

target:
    SomeType

reason:
    SomeType has no method Exec

If multiple targets fail, report each failure where practical.

Source locations should point to the original shared method body and mention the failing instantiated target.

Generic targets

If generic extension targets are already supported:

extend Box[int], Box[string] {
    ...
}

should work exactly like any other multi-target extension.

Generic target declarations such as:

extend Box[T], Other[T] {
    ...
}

may be deferred unless the language already supports generic extension target declarations.

Interop

Multi-target extensions must preserve all existing Go interop guarantees.

Example:

extend *sql.DB, *sql.Tx {
    func ExecOne(...) ...
}

must not wrap or modify either type.

Both remain actual standard-library/database/sql types.

The extension only affects Go++ call resolution and lowering.

No runtime representation is needed.

No vtable changes are allowed.

No interface satisfaction changes are allowed.

Use cases

Collections:

extend string, []byte, []rune {
    func Empty() bool {
        return len(this) == 0
    }
}

database/sql:

extend *sql.DB, *sql.Tx {
    func ExecOne(query string, args ...any) error {
        _, err := this.Exec(query, args...)
        return err
    }
}

Related domain classes:

extend Employee, Customer {
    func DisplayName() string {
        return this.Name
    }
}

provided the body independently type-checks for both Employee and Customer.

Suggested implementation order

1. Change ExtendDecl.Target to ExtendDecl.Targets.
2. Parse comma-separated target list.
3. Resolve every target independently.
4. Detect duplicate normalized targets.
5. Expand multi-target declaration into single-target semantic instances.
6. Reuse existing extension registration.
7. Reuse existing type checking with target-specific `this`.
8. Reuse existing lowering/emission.
9. Add target-specific diagnostics.
10. Add tests.

Tests

Single target regression:

extend string {
    func Empty() bool {
        return len(this) == 0
    }
}

must continue working unchanged.

Basic multiple:

extend string, []byte {
    func Empty() bool {
        return len(this) == 0
    }
}

Both:

"".Empty()

and:

[]byte{}.Empty()

must compile.

Different imported Go types:

extend *sql.DB, *sql.Tx {
    func ExecOne(query string, args ...any) error {
        _, err := this.Exec(query, args...)
        return err
    }
}

Both receiver types must compile.

Invalid one target:

extend string, int {
    func Empty() bool {
        return len(this) == 0
    }
}

must fail specifically for int.

Multiple methods:

extend string, []byte {
    func Empty() bool {
        return len(this) == 0
    }

    func Size() int {
        return len(this)
    }
}

Both methods must be registered for both types.

Duplicate target:

extend string, string {
}

must fail.

Real method precedence:

If one target already has a real method with the same name, normal real-method precedence still applies independently for that target.

Design principle

Multi-target extensions are convenience syntax only.

The compiler should treat:

extend A, B, C {
    ...
}

as if the programmer had written:

extend A {
    ...
}

extend B {
    ...
}

extend C {
    ...
}

The implementation should reuse the existing single-target extension system rather than introducing a new receiver or dispatch model.
