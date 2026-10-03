# Generic Go++ Classes and Shared Type Analysis

## Status

The initial generic-class slice is implemented: Go++ classes can declare Go
type parameters, instance methods can use them, and constructor calls can
instantiate classes with explicit type arguments. The generated Go compiler
checks the resulting constraints and uses. This spec also defines the follow-up
work needed for full semantic integration; those items are not implemented by
the initial slice.

## Goals

The complete feature must:

* declare type parameters and Go-compatible constraints;
* use type parameters in fields, methods, constructors, and nested types;
* instantiate as ordinary Go generic types;
* preserve Go's type identity, constraint, and assignability rules;
* work with native Go generic types and Go++ generic classes in the same
  expressions; and
* expose one compiler type model to semantic validation, lowering, templates,
  diagnostics, and the language server.

Generic classes add no runtime type system. The compiler emits ordinary Go
generic types and methods. The initial implementation supports generic class
declarations, instance methods that use class parameters, and explicit generic
constructor calls. Generic inheritance, compiler-side member substitution,
and runtime introspection for generic classes remain follow-up work.

## Declaration syntax

Type parameters follow the class name in square brackets, using Go's
type-parameter and constraint syntax:

```gpp
class Box[T any] {
    Value T

    func Get() T {
        return this.Value
    }
}
```

Multiple parameters and constraints use the corresponding Go forms:

```gpp
class Entry[K comparable, V any] {
    Key K
    Value V
}
```

Constraints are checked using Go's type rules. A type parameter may appear in
field types, method parameters and results, nested pointer/slice/map/array
types, and other type expressions accepted by Go. A reference to a type
parameter outside its declaring generic class is an error unless it is in
another declaration that declares that parameter.

Instance methods on a generic class use the class's type parameters through
their receiver. For example, `Stack[T].Push(value T)` and `Stack[T].Pop() T`
are ordinary methods on `Stack[T]`. Generic free functions, generic static
methods, and generic extension methods are already supported and lower to
ordinary generic Go functions.

Go 1.27 added type parameters declared directly on methods. The Go++ AST and
emitter can represent and emit this syntax, but generated modules currently
declare `go 1.26`, so the Go compiler rejects generic methods during a normal
build. End-to-end support needs the generated module's Go language version to
be raised to 1.27 when such methods are used, while retaining Go 1.26 for
programs that do not use them. The project plans to make Go 1.27 the baseline
soon so generic methods work by default in generated projects. Until that
change, generic methods are not supported end to end; generic free functions,
static methods, and extension methods remain available.

## Instantiation and construction

The first implementation requires explicit type arguments:

```gpp
numbers := Box[int](Value: 42)
names := Box[string](Value: "Ada")
```

Positional and named constructor arguments follow the existing class
constructor rules. Constructor lowering produces a Go composite literal of the
instantiated type:

```go
numbers := Box[int]{Value: 42}
```

Generic types may also appear in declarations and function signatures:

```gpp
func First(box Box[int]) int {
    return box.Get()
}

var empty Box[string]
```

The initial implementation relies on generated-Go type checking for type
argument arity, constraints, and assignments. Type-argument inference for
constructor-style calls is deferred; `Box(Value: 42)` is not inferred as
`Box[int]` in this version.

The runnable showcase in [`examples/generics.gpp`](../examples/generics.gpp)
covers a generic stack, a map-backed class with a `comparable` key constraint,
a two-parameter pair, generic free and static functions, and methods that use
class parameters in fields, arguments, and results. Run it with
`gpp run examples/generics.gpp`.

## Inheritance

A generic class may inherit from a generic class by supplying its type
arguments:

```gpp
class Named[T any] {
    Value T
}

class Audited[T any]: Named[T] {
    CreatedBy string
}
```

Parent type arguments are checked in the child class's type-parameter scope.
Inherited fields and methods retain their substituted types. Instantiating
`Audited[int]` therefore exposes `Value` as `int`.

Inheritance from a generic class requires explicit parent type arguments. An
uninstantiated generic parent is invalid. Existing multiple-inheritance
conflict and qualification rules continue to apply after substitution.

## Shared type analysis

The compiler must maintain a canonical semantic type representation for Go++
and Go expressions. Compiler passes must query this shared representation
rather than independently guessing types from source strings.

The type model must support:

* Go built-in, imported, aliased, and instantiated generic types;
* Go++ classes, including generic declarations and instantiated classes;
* type-parameter scopes and substitution through nested type expressions;
* fields, methods, inherited members, pointer/value method sets, and generic
  function signatures;
* assignability and constraint satisfaction using Go semantics where the
  types are Go-representable; and
* unknown types as an explicit result, without guessing a concrete type.

Native Go package and instantiated-type information should use `go/types`.
Go++ declarations must contribute equivalent semantic information before code
generation. The model must be available to constructor lowering, overload and
extension resolution, lambda inference, safe access, template checking, and
LSP diagnostics. Consumers may add feature-specific rules but must not create
separate conflicting type identities.

If a type cannot be resolved, the compiler may defer checks that require that
type to generated-Go compilation. It must not silently choose an unrelated
overload, extension, or generic instantiation.

## Go interoperability

The generated declaration for `Box[T any]` must be an ordinary Go declaration
equivalent to:

```go
type Box[T any] struct {
    Value T
}

func (this *Box[T]) Get() T {
    return this.Value
}
```

Go code must be able to import and use instantiated Go++ generic classes. Go++
code must be able to use imported Go generic types and functions. Generic class
types must not require reflection or a Go++ runtime representation.

## Diagnostics

Diagnostics for malformed declarations, unknown type parameters, invalid type
arguments, constraint failures, and incompatible constructor fields must:

* identify the relevant Go++ source file and line;
* name the generic declaration or instantiation involved; and
* preserve the underlying Go type-checking detail when it adds useful context.

The command-line compiler and LSP must report the same semantic problem at the
same source location.

## Initial implementation limits

The first implementation deliberately leaves these cases unsupported or to
generated-Go checking:

* generic inheritance and inherited member substitution;
* compiler-side type substitution for overload, extension, lambda, safe-access,
  and template analysis on instantiated generic classes;
* per-instantiation introspection descriptors and generated serialization
  methods for generic classes; and
* source-located compiler diagnostics for type-argument arity and constraint
  failures before generated Go compilation.

These limits must not be presented as completed behavior. Follow-up work should
add the shared type model below and remove each limit with focused tests.

## Non-goals for the complete feature

* Inferring type arguments for constructor-style calls.
* Higher-kinded types or non-Go constraint semantics.
* Runtime reification or dynamic generic dispatch.
* Structural compatibility between unrelated generic classes.

## Acceptance criteria

The implementation is ready when:

1. Generic class declarations parse into structured type-parameter AST nodes. **Done.**
2. Generated Go preserves constraints, type-parameter uses, and receiver
   instantiations. **Done for class declarations and instance methods.**
3. Explicit constructor calls lower to valid Go composite literals. **Done.**
4. Field and method types are substituted for each concrete instantiation.
5. Generic parent fields and methods resolve after substitution.
6. Invalid type arguments, constraints, constructors, and members produce
   source-located diagnostics in both CLI and LSP analysis.
7. Imported native Go generics and Go++ generic classes interoperate in the
   same program.
8. Fine-grained compiler tests cover declarations, substitutions, constraints,
   constructors, inheritance, overloads, extensions, lambdas, and templates.
9. Generated Go builds using the project's supported Go toolchain.
