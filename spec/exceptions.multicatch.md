# Go++ Multi-Type Catch Add-on Specification

## 1. Overview

Go++ `catch` clauses may match more than one error type.

Canonical syntax:

```gpp
try {
    ...
} catch Foo, Bar, Baz e {
    ...
}
```

This means:

> Handle the propagated error if it matches `Foo`, `Bar`, or `Baz`.

The feature extends the existing Go++ `try/catch/finally` model without introducing union types.

---

# 2. Existing Catch Forms

Go++ supports typed catches:

```gpp
try {
    ...
} catch NotFoundError e {
    ...
}
```

and a catch-all form:

```gpp
try {
    ...
} catch e {
    ...
}
```

Multiple catch clauses may be chained:

```gpp
try {
    ...
} catch NotFoundError e {
    ...
} catch ValidationError e {
    ...
} catch e {
    ...
}
```

Catch clauses are tested from top to bottom.

The first matching clause handles the error.

---

# 3. Multi-Type Catch

A single catch clause may list multiple alternative error types:

```gpp
try {
    ...
} catch NotFoundError, PermissionError, ValidationError e {
    ...
}
```

This is equivalent conceptually to:

```text
NotFoundError
OR PermissionError
OR ValidationError
```

All listed types share the same handler body.

---

# 4. Motivation

Without multi-type catch:

```gpp
try {
    ...
} catch NotFoundError e {
    return ClientError(e)
} catch PermissionError e {
    return ClientError(e)
} catch ValidationError e {
    return ClientError(e)
}
```

With multi-type catch:

```gpp
try {
    ...
} catch NotFoundError, PermissionError, ValidationError e {
    return ClientError(e)
}
```

The feature removes duplicated handlers without introducing a new exception hierarchy solely for grouping errors.

---

# 5. Canonical Syntax

The canonical syntax is:

```gpp
catch Foo, Bar, Baz e {
    ...
}
```

Do not use:

```gpp
catch (Foo | Bar | Baz) e {
}
```

or:

```gpp
catch Foo | Bar | Baz e {
}
```

or:

```gpp
catch (Foo, Bar, Baz) e {
}
```

The comma-separated form is intentionally simple and consistent with ordinary Go++ lists.

---

# 6. Single-Type Catch Remains Valid

A single type is simply the one-element form:

```gpp
catch Foo e {
    ...
}
```

There is no semantic distinction between:

```text
single typed catch
```

and:

```text
multi-type catch with one member
```

---

# 7. Catch-All Remains Distinct

The catch-all form remains:

```gpp
catch e {
    ...
}
```

It does not require an explicit `error` type.

Example:

```gpp
try {
    ...
} catch Foo, Bar e {
    ...
} catch e {
    ...
}
```

The final clause handles any propagated error not matched earlier.

---

# 8. Matching Semantics

Within:

```gpp
catch Foo, Bar, Baz e {
    ...
}
```

the runtime/compiler checks whether the propagated error matches any listed type.

Conceptually:

```text
matches Foo
OR
matches Bar
OR
matches Baz
```

If any match succeeds, the clause handles the error.

---

# 9. Catch Clause Ordering

Catch clauses continue to be evaluated from top to bottom.

Example:

```gpp
try {
    ...
} catch SpecificError e {
    ...
} catch BaseError, OtherError e {
    ...
} catch e {
    ...
}
```

If `SpecificError` matches the first clause, later clauses are not considered.

---

# 10. First Match Wins

The rule is:

> The first matching catch clause handles the propagated error.

This applies equally to single-type and multi-type catches.

Example:

```gpp
try {
    ...
} catch Foo e {
    HandleFoo(e)
} catch Foo, Bar e {
    HandleOther(e)
}
```

A `Foo` error is handled by the first clause.

---

# 11. Type Matching

Typed catch matching uses the same Go++ error matching semantics as existing typed catches.

A bare named catch type matches both its value and pointer forms when each form
implements Go's `error` interface. For example, `catch Foo` matches a thrown
`Foo` value and a thrown `*Foo` pointer when both implement `error`. This also
applies to imported Go error types: `catch os.PathError` matches a thrown
`*os.PathError`, even though `os.PathError` itself does not implement `error`.

The catch does not change the thrown value. The generated match handles each
eligible representation separately, so the catch variable retains the
representation that was thrown. Its body must therefore be valid for every
form matched by the clause. For example, accessing a shared field or calling
an `Error()` method works when available on both forms. If only `*Foo`
implements `error`, only the pointer form can match.

Pointer spelling does not narrow the match: `catch *Foo` also matches either
eligible representation of `Foo`. Writing both `Foo` and `*Foo` in one catch
is redundant because both select the same error type.

For ordinary Go errors, implementation may rely on behavior equivalent to:

```go
errors.As
```

where appropriate.

For Go++ throwable classes implementing:

```gpp
Error() string
```

normal Go++ type compatibility and inheritance rules apply.

---

# 12. No Union Types

Multi-type catch does not introduce general-purpose union types.

This syntax:

```gpp
catch Foo, Bar, Baz e {
}
```

does not create a type equivalent to:

```text
Foo | Bar | Baz
```

elsewhere in the language.

The comma-separated list is a catch-clause-specific construct.

---

# 13. Type of the Catch Variable

For:

```gpp
catch Foo, Bar, Baz e {
    ...
}
```

`e` must have a type that is valid for every listed alternative.

The default rule is:

```gpp
e error
```

conceptually.

This avoids inventing union-type semantics.

---

# 14. Common Parent Optimization

If all listed types share a meaningful common Go++ superclass that itself satisfies the throwable/error requirements, the compiler may expose that common type where this is statically safe.

Example:

```gpp
class AuthError {
    func Error() string
}

class LoginError : AuthError
class PermissionError : AuthError
```

Then:

```gpp
catch LoginError, PermissionError e {
    ...
}
```

could conceptually type `e` as `AuthError`.

However, this is optional for v0.1.

The required behavior is that `e` is always safely usable as `error`.

---

# 15. Recommended V0.1 Rule

For simplicity and predictability, v0.1 should type the variable of every multi-type catch as:

```gpp
error
```

unless the implementation already has a straightforward common-parent calculation.

This keeps the feature small.

---

# 16. Single-Type Catch Variable

Single-type catches retain the precise Go type matched by each generated branch.
For a bare named type, that may be either the value or pointer form.

Example:

```gpp
catch ValidationError e {
    println(e.Field)
}
```

If both `ValidationError` and `*ValidationError` implement `error`, the catch
body is generated for both forms, and `e` has the corresponding concrete type
in each branch. Operations in the body must compile for both forms. Types for
which only the pointer implements `error` have only a pointer branch. Fields
and methods remain available when they work on every matched form.

---

# 17. Multi-Type Catch Variable

For:

```gpp
catch ValidationError, PermissionError e {
    ...
}
```

code may rely on ordinary `error` behavior:

```gpp
println(e.Error())
```

but cannot directly assume fields belonging only to `ValidationError`.

Invalid:

```gpp
catch ValidationError, PermissionError e {
    println(e.Field)
}
```

unless `Field` is present on the statically determined common type.

---

# 18. Narrowing Inside the Handler

If the application needs type-specific behavior inside a multi-type catch, normal type inspection/narrowing mechanisms may be used.

Conceptually:

```gpp
catch ValidationError, PermissionError e {
    if v := e.As[*ValidationError]() {
        ...
    }
}
```

Exact narrowing syntax follows the existing Go++ error/type system.

The multi-type catch itself does not introduce special pattern-matching syntax.

---

# 19. Duplicate Types

This should be rejected:

```gpp
catch Foo, Bar, Foo e {
}
```

because `Foo` appears more than once in the same type list.

The compiler should report a duplicate catch type.

---

# 20. Invalid Non-Error Types

Each listed type must be catchable under normal Go++ error rules.

Invalid:

```gpp
catch User, Order e {
}
```

if those classes/types are not valid throwable/error values.

The compiler validates each member independently.

---

# 21. Empty Type List

This is invalid:

```gpp
catch , e {
}
```

A typed catch must contain at least one type.

---

# 22. Trailing Comma

For multiline formatting, a trailing comma may be supported if the grammar permits:

```gpp
catch (
    Foo,
    Bar,
    Baz,
) e {
    ...
}
```

However, this parenthesized form is not required for v0.1.

The canonical v0.1 syntax remains:

```gpp
catch Foo, Bar, Baz e {
}
```

and should be kept on one line unless unusually long.

---

# 23. Long Type Lists

The formatter may wrap long type lists using a stable style.

Recommended:

```gpp
catch Foo,
    Bar,
    Baz,
    SomeVeryLongErrorName e {
    ...
}
```

or another formatter-owned equivalent.

The exact wrapping style is not user-configurable.

The unwrapped semantic syntax remains comma-separated types followed by the binding identifier.

---

# 24. Grammar

Conceptually:

```text
CatchClause
    = "catch" CatchTarget Block

CatchTarget
    = Identifier
    | TypeList Identifier

TypeList
    = Type
    | TypeList "," Type
```

This supports:

```gpp
catch e {
}
```

```gpp
catch Foo e {
}
```

```gpp
catch Foo, Bar, Baz e {
}
```

---

# 25. Parsing Ambiguity

The parser distinguishes catch-all from typed catch by whether a valid type list precedes the final identifier.

Examples:

```gpp
catch e {
}
```

contains only the binding identifier.

```gpp
catch Foo e {
}
```

contains type `Foo` followed by binding `e`.

```gpp
catch Foo, Bar e {
}
```

contains two types followed by binding `e`.

---

# 26. Compiler Representation

A catch clause may be represented conceptually as:

```go
type CatchClause struct {
    Types []TypeRef
    Name  string
    Body  *Block
}
```

For a catch-all:

```text
Types == empty
```

For:

```gpp
catch Foo e
```

the list contains one type.

For:

```gpp
catch Foo, Bar, Baz e
```

the list contains three types.

---

# 27. Semantic Validation

The compiler must validate:

```text
all listed types exist
all listed types are catchable
no duplicate types occur
binding name is valid
catch ordering is valid
catch-all placement is valid
```

---

# 28. Catch-All Placement

A catch-all should normally be last.

Invalid:

```gpp
try {
    ...
} catch e {
    ...
} catch Foo e {
    ...
}
```

because the typed clause is unreachable.

The compiler should reject this or report an unreachable catch clause.

---

# 29. Unreachable Multi-Type Catch

The compiler should detect obvious unreachable catches.

Example:

```gpp
catch BaseError e {
    ...
} catch ChildError, OtherChildError e {
    ...
}
```

If both listed child types are already matched by `BaseError`, the later catch is unreachable.

At minimum, obvious same-type duplication across preceding catches should be diagnosed.

More advanced hierarchy analysis may be added progressively.

---

# 30. Partially Shadowed Multi-Type Catch

Example:

```gpp
catch Foo e {
    ...
} catch Foo, Bar e {
    ...
}
```

`Foo` is shadowed but `Bar` is still reachable.

The compiler may:

```text
warn that Foo is unreachable in the second clause
```

or reject the redundant member.

Recommended v0.1 behavior:

> report a warning for individually unreachable alternatives while preserving reachable alternatives.

---

# 31. Exact Duplicate Across Catch Clauses

Example:

```gpp
catch Foo, Bar e {
    ...
} catch Bar, Baz e {
    ...
}
```

`Bar` in the second clause can never match.

The compiler should warn or reject the unreachable `Bar` alternative.

---

# 32. `finally`

Multi-type catch does not alter `finally`.

Example:

```gpp
try {
    ...
} catch Foo, Bar e {
    ...
} finally {
    cleanup()
}
```

`finally` runs according to the existing Go++ exception semantics.

---

# 33. Error Propagation From Handler

If a multi-type catch handler itself propagates another error:

```gpp
try {
    ...
} catch Foo, Bar e {
    DoSomethingThatFails()
}
```

the new propagated error leaves the handler according to ordinary Go++ error propagation.

The same catch clause does not recursively catch errors thrown from its own body.

---

# 34. Nested `try`

Normal nesting remains valid:

```gpp
try {
    try {
        ...
    } catch Foo, Bar e {
        ...
    }
} catch e {
    ...
}
```

Inner clauses get first opportunity to handle propagated errors.

---

# 35. Raw Go Panic

Multi-type catch does not change the existing rule:

> Raw Go `panic` is not automatically treated as a Go++ propagated error.

Therefore:

```gpp
catch Foo, Bar e {
}
```

matches Go++ error propagation, not arbitrary panics.

---

# 36. Go Error Interoperability

Native Go errors remain catchable when they participate in the existing Go++ typed-catch mechanism.

For example, where appropriate:

```gpp
catch *os.PathError, *url.Error e {
    ...
}
```

The exact matching behavior follows existing native-Go error interop.

---

# 37. Custom Go++ Errors

Example:

```gpp
class NotFoundError {
    Message string

    func Error() string {
        return Message
    }
}

class PermissionError {
    Message string

    func Error() string {
        return Message
    }
}
```

Then:

```gpp
try {
    ...
} catch NotFoundError, PermissionError e {
    ...
}
```

is valid.

---

# 38. Inheritance

Given:

```gpp
class AppError {
    func Error() string
}

class ValidationError : AppError
class PermissionError : AppError
```

this is valid:

```gpp
catch ValidationError, PermissionError e {
    ...
}
```

A preceding:

```gpp
catch AppError e {
}
```

would make both alternatives unreachable.

---

# 39. Error Identity

Multi-type catch should preserve the original error object.

The handler receives the same propagated error value.

The runtime must not wrap the error merely because several alternatives appear in the catch clause.

---

# 40. No Synthetic Group Error

The compiler must not construct something like:

```text
MultiCatchError<Foo, Bar, Baz>
```

at runtime.

The type list is only matching metadata.

---

# 41. Lowering Strategy

Conceptually:

```gpp
catch Foo, Bar, Baz e {
    Handle(e)
}
```

may lower to logic equivalent to:

```go
if matchesFoo(err) ||
   matchesBar(err) ||
   matchesBaz(err) {

    e := err
    Handle(e)
}
```

The exact generated Go is implementation-specific.

---

# 42. Match Once Per Alternative

The runtime/compiler should avoid repeated or side-effecting conversions beyond what ordinary type/error matching requires.

Each listed alternative is conceptually tested until one succeeds.

---

# 43. Left-to-Right Alternative Order

Within a multi-type catch:

```gpp
catch Foo, Bar, Baz e {
}
```

alternatives are considered left-to-right where matching implementation order is observable.

In ordinary cases, the alternatives are semantically OR-equivalent.

---

# 44. Overlapping Alternatives

If one alternative subsumes another:

```gpp
catch BaseError, ChildError e {
}
```

the `ChildError` entry is redundant if `ChildError : BaseError`.

The compiler should ideally diagnose this.

Recommended:

```text
warning: ChildError is already matched by BaseError in this catch clause
```

---

# 45. Formatter Support

`gpp fmt` must understand multi-type catches.

Input:

```gpp
catch Foo,Bar,Baz e{
    ...
}
```

becomes:

```gpp
catch Foo, Bar, Baz e {
    ...
}
```

Comments between types must be preserved.

---

# 46. Comments in Type Lists

If grammar permits comments:

```gpp
catch Foo,
    // network failures
    Bar,
    Baz e {
    ...
}
```

`gpp fmt` must not discard or relocate the comment incorrectly.

This follows the formatter's general comment-conservation requirement.

---

# 47. LSP Support

`gpp lsp` must understand each type reference independently.

For:

```gpp
catch Foo, Bar, Baz e {
}
```

the language server should support:

```text
hover on Foo/Bar/Baz
go-to-definition
rename
references where appropriate
diagnostics
```

---

# 48. Completion

After:

```gpp
catch Foo,
```

completion may suggest valid catchable error types.

It should prefer types satisfying the Go++ error/throwable rules.

---

# 49. Diagnostics

Examples:

```text
unknown catch type FooError
```

```text
User is not catchable
```

```text
duplicate catch type ValidationError
```

```text
PermissionError is unreachable because AppError is matched earlier
```

All diagnostics must point to the specific offending type in the list.

---

# 50. `gpp doc`

No new standalone language type is introduced, but the language reference for `catch` should document all three forms:

```gpp
catch Foo e {
}
```

```gpp
catch Foo, Bar, Baz e {
}
```

```gpp
catch e {
}
```

---

# 51. Example: Shared Client Error Handler

```gpp
try {
    user := LoadUser(id)
    return user
} catch NotFoundError, ValidationError e {
    return ClientError(e)
} catch e {
    return ServerError(e)
}
```

---

# 52. Example: Authentication

```gpp
try {
    identity := Authenticate(request)
} catch InvalidCredentialsError,
        AccountDisabledError,
        AccountLockedError e {

    return LoginFailed(e)
}
```

The formatter owns the final wrapping style.

---

# 53. Example: Filesystem/Network Handling

```gpp
try {
    data := FetchAndLoad(url)
} catch url.Error, os.PathError e {
    return TemporaryFailure(e)
}
```

The bare named types match their eligible value and pointer forms under Go's
`error` method-set rules. The catch variable is `error` here because the clause
lists multiple types.

---

# 54. Example: Specific Before Broad

```gpp
try {
    ...
} catch ValidationError e {
    HandleValidation(e)
} catch PermissionError, NotFoundError e {
    HandleClientFailure(e)
} catch e {
    HandleUnexpected(e)
}
```

Catch ordering remains explicit and readable.

---

# 55. Interaction With Deprecated Types

If a listed error type is deprecated:

```gpp
catch OldError, NewError e {
}
```

normal deprecation diagnostics apply to `OldError`.

The catch feature does not suppress deprecation warnings.

---

# 56. Interaction With Extension Methods

Inside a multi-type handler, extensions available on the static type of `e` may be used.

If `e` is statically `error`:

```gpp
catch Foo, Bar e {
    if e.Is(target) {
        ...
    }
}
```

works through normal `error` extensions.

---

# 57. Interaction With `error.As`

Type-specific recovery can remain explicit:

```gpp
catch Foo, Bar e {
    foo := e.As[*Foo]()

    if foo != nil {
        ...
    }
}
```

No special catch-only narrowing API is necessary.

---

# 58. V0.1 Required Surface

The complete catch syntax becomes:

```gpp
try {
    ...
} catch Foo e {
    ...
} catch Bar, Baz e {
    ...
} catch e {
    ...
} finally {
    ...
}
```

No additional multi-catch syntax is needed.

---

# 59. Non-Goals

This add-on does not introduce:

```text
union types
pattern matching
exception filters
catch guards
destructuring
catch expressions
typed catch aliases
synthetic grouped exception types
```

It only allows multiple alternative error types in one catch clause.

---

# 60. Design Summary

Canonical multi-type catch:

```gpp
try {
    ...
} catch Foo, Bar, Baz e {
    ...
}
```

The semantics are:

```text
Foo OR Bar OR Baz
```

The overall rules are:

> A catch clause may list one or more comma-separated error types.

> The first matching catch clause wins.

> A multi-type catch shares one handler body for all listed alternatives.

> Multi-type catch does not introduce union types.

> For v0.1, the catch variable should generally be typed as `error` for multi-type catches.

> Single-type catches retain their precise error type.

> Catch-all remains `catch e`.

> `finally` behavior is unchanged.

> Raw Go panic behavior is unchanged.

> The compiler should diagnose duplicate and unreachable catch alternatives where practical.

> `gpp fmt`, `gpp lsp`, diagnostics, and documentation must treat every listed catch type as a real source-level symbol.
