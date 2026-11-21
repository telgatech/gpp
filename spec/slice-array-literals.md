# GPP Typed Slice and Array Literal Specification

## Status

Implemented. Typed bracket literals lower to ordinary Go slice and array
composite literals. Existing Go composite literals, conversions, indexing, and
class constructor calls retain their current meaning.

## Goals

Provide concise typed slice and fixed-array literals that:

* put the element type before the values;
* allow nested literals to inherit their expected type;
* construct class elements through the class constructor rules; and
* lower to ordinary Go slice and array literals without changing Go conversion
  syntax.

## Surface syntax

The grammar is:

```text
SliceLiteral      = SliceElementType "[" [LiteralElements] "]" .
ArrayLiteral      = "[" ArrayLength "]" ElementType "[" [LiteralElements] "]" .
LiteralElements   = LiteralElement { "," LiteralElement } [ "," ] .
LiteralElement    = Expression | ConstructorArgumentGroup .
ConstructorArgumentGroup = "(" [ConstructorArguments] ")" .
```

`SliceElementType` is the element type of the resulting slice. `ElementType`
may be any element type, including a slice, array, generic type, named struct,
or GPP class. `ArrayLength` is a Go-compatible integer constant expression.
`ConstructorArguments` follows the existing positional or named argument form
for construction calls; arguments in one group cannot mix the two forms.

A typed slice literal places its element type before a bracketed element list:

```gpp
numbers := int[1, 2, 3] // []int
```

A fixed-size array literal places its array type before the element list:

```gpp
numbers := [3]int[1, 2, 3] // [3]int
```

The array length is a compile-time constant. The number of supplied elements
must not exceed that length; omitted elements have their zero values. The
literal may be empty:

```gpp
emptySlice := int[]      // []int
zeroArray := [3]int[]    // [3]int with all elements zero
```

This proposal covers sequential elements. Existing Go brace literals remain
available for indexed elements and any form not covered by the new syntax:

```gpp
values := []int{2: 10}
fixed := [4]int{1: 10}
```

## Nested literals and contextual element types

The type prefix may itself be a slice or array type. Each element is checked
against that type. Nested bracket literals inherit the expected element type:

```gpp
matrix := []int[[1, 2], [3, 4]] // [][]int
matrixWithEmptyRow := []int[[], [3, 4]]
```

Here the outer element type is `[]int`; the inner literals therefore have
type `[]int`, including the empty row.

Fixed arrays may be nested in the same way:

```gpp
grid := [2][2]int[[1, 2], [3, 4]] // [2][2]int
```

## Generic element types

Generic type arguments precede the literal element list. A generic class name
followed by one bracket group supplies its type arguments; the next bracket
group supplies its values:

```gpp
boxes := Box[int][(value: 1), (value: 2)]
```

This constructs two `Box[int]` class instances. Because GPP class constructors
return pointers, the expression has type `[]*Box[int]`.

Named struct elements use the same argument grouping and produce struct values:

```gpp
struct User {
    Name string
}

users := User[(Name: "Ada"), (Name: "Lin")] // []User
```

## Element expressions and construction

An element may be an ordinary expression assignable to the element type. A
parenthesized argument group constructs one element using the element type's
existing constructor rules:

```gpp
existing := Box[int](value: 1)
boxes := Box[int][existing, (value: 2)]
```

For a class with one or more `init` methods, the argument group must match an
initializer overload. Initializers, parent initialization, and error
propagation behave as they do for an ordinary `Type(...)` constructor call. If
the class has no `init`, the existing positional or named field construction
rules apply. Named struct arguments follow named struct construction rules.

The parenthesized group belongs to this literal syntax; it is not a tuple
value. Named and positional arguments cannot be mixed within a group, matching
ordinary constructor calls.

Class elements are pointers, while named struct elements are values. For
example, `Box[int][(value: 1)]` has type `[]*Box[int]`, and
`User[(Name: "Ada")]` has type `[]User`.

## Bracket disambiguation

Bracket syntax is resolved from the parsed form and the compiler's type/value
metadata:

* Indexing and slicing a value retain their existing meaning: `items[0]` and
  `items[1:3]`.
* A prefix that resolves as a type, followed by a bracketed element list,
  forms a typed slice or array literal: `int[1, 2]` and `[2]int[1, 2]`.
* For a generic type, consume the bracket group containing type arguments
  first. A following bracket group is the literal's element list:
  `Box[int][...]`.
* A visible local or package value takes precedence over a same-named type,
  preserving ordinary indexing. Otherwise, a prefix known to the compiler as a
  type can introduce a literal. Unknown prefixes remain ordinary expressions
  and receive the usual Go type-checking diagnostics.

Ordinary conversions such as `[]byte(data)` remain conversions. Ordinary Go
composite literals such as `[]int{1, 2}` and `Box[int]{Value: 1}` remain valid
and retain their Go meanings. In particular, a Go class composite literal does
not invoke a GPP `init` method; constructor argument groups in the new syntax
do.

## Lowering and compatibility

Typed literals lower to ordinary Go slice or array composite literals. Each
class argument group lowers through the existing GPP constructor path before
being placed in the generated collection. This preserves constructor
validation, initialization order, and exception/error behavior.

The new syntax is available in GPP source. Generated Go remains valid Go;
native `.go` files continue to use Go's existing composite literal syntax.
The formatter and language server must parse, preserve, and format the new
syntax consistently with command-line compilation.

## Non-goals

* Replacing `Type(...)` class construction.
* Changing Go type conversions, indexing, or brace composite literals.
* Replacing inferred map syntax or anonymous struct syntax.
* Inferring a literal's element type from its first value. The type prefix is
  explicit in this proposal.
* Adding keyed/indexed entries to the new bracket form; Go brace literals
  continue to provide those forms.

## Acceptance criteria

1. Typed slice literals produce `[]T`; `[N]T` prefixes produce fixed-size
   `[N]T` arrays.
2. Nested literals receive expected element types, including empty nested
   slices.
3. Class element argument groups use the same overload, field, inheritance,
   visibility, pointer-result, and error rules as ordinary construction.
4. Existing Go conversions, generic type applications, indexing, slicing,
   and brace composite literals retain their meanings.
5. `gpp fmt`, editor formatting, parsing, and generated-Go output support the
   syntax consistently.
