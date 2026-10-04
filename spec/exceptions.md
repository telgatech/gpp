# Go++ Exception and Automatic Error Promotion Specification

## Goal

Add exception-style error handling to Go++ while preserving direct compatibility with ordinary Go functions that return `error`.

The central design rule is:

> When a function returns a trailing `error`, Go++ automatically throws a non-nil error if that error result is omitted by the caller.

If the caller explicitly captures the error result, ordinary Go error-value semantics apply.

Go++ does **not** introduce a separate `Exception` base class.

The existing Go `error` interface is the universal throwable contract.

---

# Core Rule

Given:

```go
func Read(name string) ([]byte, error)
```

Go++ allows:

```go
data := Read(name)
```

This means:

```text
call Read(name)

if error != nil
    throw error

otherwise
    assign []byte result to data
```

The error return is omitted at the source level and therefore becomes exception propagation.

---

# Explicit Error Capture

If the caller captures the error explicitly:

```go
data, err := Read(name)
```

then no automatic exception promotion occurs.

This retains ordinary Go semantics.

The rule is:

```text
trailing error captured
    → error is a normal value

trailing error omitted
    → non-nil error is thrown
```

---

# Error-Only Functions

Given:

```go
func Save() error
```

this:

```go
Save()
```

means:

```text
call Save()

if returned error != nil
    throw it
```

While:

```go
err := Save()
```

captures the error normally.

---

# Multiple Return Values

Given:

```go
func Dimensions() (int, int, error)
```

this is valid:

```go
width, height := Dimensions()
```

The omitted trailing error is automatically promoted.

Equivalent conceptual behavior:

```text
width, height, err := Dimensions()

if err != nil
    throw err
```

This remains valid ordinary Go-style handling:

```go
width, height, err := Dimensions()
```

---

# Error Must Be Trailing

Automatic error promotion applies only to the final return value.

Example:

```go
func Foo() (int, error)
```

supports promotion.

A function with an `error` in some non-final position does not receive this special treatment.

This preserves compatibility with normal Go conventions.

---

# Error-Compatible Return Types

The final return type may be:

```go
error
```

or another type assignable to Go's `error` interface.

Example:

```go
func Validate() *ValidationError
```

may participate in automatic promotion if `*ValidationError` implements:

```go
Error() string
```

The compiler should use ordinary Go assignability rules.

---

# Universal Throwable Contract

Go++ defines no separate exception hierarchy or mandatory exception base type.

Any value assignable to Go's:

```go
error
```

interface may be thrown.

Examples:

```go
throw fmt.Errorf("invalid user")
throw sql.ErrNoRows
throw validationErr
```

A Go++ class may become throwable simply by implementing:

```go
func Error() string
```

Example:

```go
class ValidationError {
    Field string
    Message string

    func Error() string {
        return this.Field + ": " + this.Message
    }
}
```

Then:

```go
throw ValidationError(
    Field: "email",
    Message: "invalid",
)
```

is valid if the generated Go++ class representation satisfies `error`.

---

# Throw

Go++ supports:

```go
throw err
```

The thrown value must satisfy Go's `error` interface.

Invalid:

```go
throw "something went wrong"
```

unless the value's type implements `error`.

Suggested diagnostic:

```text
cannot throw string; thrown value must implement error
```

---

# Try / Catch

`try` establishes an exception-handling scope.

Example:

```go
try {
    data := os.ReadFile(path)
    process(data)
} catch e {
    fmt.Println(e)
}
```

`os.ReadFile` returns:

```go
([]byte, error)
```

The omitted error automatically becomes a thrown error.

---

# Typed Catch

Catch clauses may match specific error types.

Example:

```go
try {
    user := LoadUser(id)
} catch ValidationError e {
    ...
} catch *os.PathError e {
    ...
} catch e {
    ...
}
```

Catch clauses are evaluated in source order.

The first compatible catch handles the error.

Typed catch matching should use normal Go/Go++ type assignability semantics.

## Error-value catches

An imported package error value may be caught directly without binding an error
variable and writing a separate `errors.Is` check:

```go
try {
    user := LoadUser(id)
} catch sql.ErrNoRows {
    return ctx.JSON(404, record(Error: "user not found"))
}
```

The expression must resolve to an imported package variable assignable to
`error`. Import Go's `errors` package; Go++ matches the value with
`errors.Is`, so wrapped sentinel errors are also handled. A value catch does
not catch other errors; unmatched errors continue to the next clause or
propagate out of the `try`.

---

# Catch Without Variable

If the caught value is not needed:

```go
catch ValidationError {
    ...
}
```

should be allowed.

No dummy variable is required.

---

# Catch-All

An untyped catch:

```go
catch e {
    ...
}
```

catches any thrown Go++ error.

The variable `e` has type:

```go
error
```

It does not catch arbitrary Go panics.

---

# Bare Rethrow

Inside a catch block:

```go
throw
```

rethrows the currently handled error.

Example:

```go
catch ValidationError e {
    log.Println(e)
    throw
}
```

Using bare `throw` outside a catch block is a compile-time error.

---

# Finally

Go++ supports:

```go
try {
    ...
} catch e {
    ...
} finally {
    ...
}
```

`finally` runs whether the try block:

* succeeds
* throws
* returns
* exits through a handled error

It is intended primarily for cleanup.

---

# Propagation

If a thrown error is not handled by the current `try`, it propagates upward.

Example:

```go
func A() {
    B()
}

func B() {
    C()
}

func C() {
    os.ReadFile("/missing")
}
```

`os.ReadFile` returns a non-nil error.

Because that trailing error is omitted, Go++ throws it.

If neither `C`, `B`, nor `A` catches it, the error reaches the top-level goroutine and terminates execution.

---

# Uncaught Errors

An uncaught thrown error terminates the current execution path.

At the top-level goroutine, this normally terminates the process.

The runtime should print a useful diagnostic containing at least:

```text
error type
error message
stack trace where practical
```

Exact formatting is implementation-defined.

---

# No Implicit Error Discard

Go++ should not silently discard omitted errors.

Given:

```go
func Foo() error
```

this:

```go
Foo()
```

means:

```text
throw if error != nil
```

not:

```text
ignore returned error
```

This is intentional.

---

# Explicit Error-Value Semantics

If the programmer wants to inspect an error as a normal value, they capture it explicitly.

Example:

```go
err := os.Remove(path)

if errors.Is(err, os.ErrNotExist) {
    ...
}
```

For multi-result calls:

```go
data, err := os.ReadFile(path)
```

the compiler must not auto-promote the explicitly captured error.

---

# No `try foo()` Expression

Do not support:

```go
value := try foo()
```

Automatic trailing-error promotion already provides:

```go
value := foo()
```

with exception semantics.

`try` should have one meaning only:

> establish a catch/finally scope.

---

# No `?` Propagation Operator

Do not add:

```go
foo()?
```

for error propagation.

It is unnecessary because:

```go
foo()
```

already propagates a non-nil trailing error automatically when the error result is omitted.

---

# Expressions

Automatic error promotion should apply where the non-error results are expected.

Example:

```go
size := FileSize(path)
```

where:

```go
func FileSize(path string) (int64, error)
```

means:

```text
call FileSize
throw if error != nil
use int64 result
```

Nested usage may also be supported:

```go
printName(LoadUser(id))
```

if:

```text
LoadUser(id) -> (User, error)
printName(User)
```

The effective Go++ expression type is `User` after automatic error promotion.

The compiler should model this during semantic analysis.

---

# Multiple Non-Error Results

Automatic error promotion does not create tuple values.

Given:

```go
func Coordinates() (int, int, error)
```

this is valid:

```go
x, y := Coordinates()
```

but this remains invalid unless Go++ separately introduces tuple semantics:

```go
point := Coordinates()
```

---

# Try Scope Example

```go
try {
    file := os.Open(path)

    data := io.ReadAll(file)

    process(data)
} catch *os.PathError e {
    fmt.Println(
        "path error:",
        e.Path,
    )
} catch e {
    fmt.Println(
        "failed:",
        e,
    )
}
```

Each omitted trailing error automatically propagates to the enclosing `try`.

---

# Go Panics Are Separate

Go++ thrown errors must not turn arbitrary Go panics into catchable errors.

Example:

```go
panic("programming bug")
```

must not be caught by:

```go
catch e {
}
```

The exception mechanism catches only Go++-promoted/thrown errors.

---

# Runtime Representation

A thrown Go++ error may lower internally to a private panic wrapper:

```go
type __gppThrownError struct {
    err error
}
```

Conceptually:

```go
throw err
```

lowers to:

```go
panic(__gppThrownError{
    err: err,
})
```

The exact representation is implementation-defined.

---

# Catch Lowering

`try/catch` may use Go's:

```go
defer
recover
```

internally.

Recovered values must be checked.

Conceptually:

```text
recover value

if value is __gppThrownError
    perform typed catch dispatch

otherwise
    panic(value)
```

Ordinary Go panics must be re-panicked unchanged.

---

# Typed Catch Lowering

Example:

```go
catch ValidationError e {
    ...
}
```

performs a type assertion/type switch against the wrapped `error`. A bare
named catch type matches both its value and pointer forms when each implements
`error`. For example, `catch os.PathError` catches a `*os.PathError`, even
though only the pointer form implements `error`.

The thrown value is preserved. The compiler generates a match branch for each
eligible form, and the catch variable has that branch's concrete Go type. The
catch body must therefore compile for every form matched by the clause. Writing
`catch *os.PathError` does not narrow the match to pointers; it still catches
the error type regardless of representation.

---

# Native Go Functions

Native Go APIs require no wrappers at ordinary call sites.

Example:

```go
data := os.ReadFile(path)
```

The compiler sees:

```go
func ReadFile(name string) ([]byte, error)
```

and generates automatic error promotion.

This is a core interoperability goal.

---

# Go++ Functions

The same rule applies to Go++ functions.

Example:

```go
func FindUser(id int64) (User, error) {
    ...
}

func ShowUser(id int64) User {
    return FindUser(id)
}
```

`FindUser(id)` automatically throws if its error result is non-nil.

The `User` result becomes the effective expression value.

---

# Functions Declared With `error`

Go++ functions may continue to declare ordinary Go-compatible signatures:

```go
func Save(user User) error
```

or:

```go
func Load(id int64) (User, error)
```

This remains important for:

* Go interoperability
* interfaces
* callbacks
* generated Go APIs
* native Go expectations

The exception feature does not remove `error` from signatures.

---

# Functions That Throw Without Returning `error`

A Go++ function may also omit an `error` return and throw directly:

```go
func Load(id int64) User {
    if id == 0 {
        throw ValidationError(...)
    }

    ...
}
```

This is valid Go++.

However, ordinary Go callers cannot receive that thrown value through an `error` return because none exists in the function signature.

If such an error escapes into plain Go code, the generated boundary may panic.

This is acceptable and should be documented.

For APIs intended for direct Go consumption, declaring a trailing `error` remains preferable.

---

# Go ABI Boundary

When a Go++ function declares a trailing `error` and is exposed to ordinary Go code, its generated Go-facing implementation should preserve that Go signature.

Example:

```go
func Load() (User, error)
```

If internal Go++ code throws an error while executing `Load`, the generated Go-facing boundary should convert that escaping thrown error into:

```text
zero value for non-error results
returned error
```

rather than exposing the private panic wrapper.

---

# ABI Boundary Example

Go++:

```go
func Load() (User, error) {
    data := os.ReadFile("user.json")
    ...
}
```

If `ReadFile` fails, Go++ internally throws.

An ordinary Go caller:

```go
user, err := Load()
```

should observe:

```text
err != nil
```

rather than an internal Go++ panic wrapper.

---

# Only Thrown Errors Are Converted

Go-facing boundary adapters must catch only the private Go++ thrown-error wrapper.

An ordinary Go panic must remain a panic.

Do not transform arbitrary bugs into returned errors.

---

# Interfaces and Callbacks

A Go++ method implementing a native Go interface with an `error` return must preserve that interface contract.

Example:

```go
type Worker interface {
    Run() error
}
```

A Go++ implementation may internally use automatic promotion, but the generated Go-facing method must convert escaping thrown errors back into its declared `error` result.

---

# `defer`

A plain deferred call:

```go
defer file.Close()
```

should retain normal Go defer semantics.

Do not automatically promote the returned `error` from a deferred call.

Otherwise a cleanup error could unexpectedly replace an already-propagating error during stack unwinding.

If deferred cleanup errors need handling, capture them explicitly:

```go
defer func() {
    err := file.Close()

    if err != nil {
        ...
    }
}()
```

---

# Defer During Propagation

Normal Go deferred functions must execute while a thrown error propagates.

Example:

```go
func Work() {
    defer cleanup()

    os.ReadFile("/missing")
}
```

`cleanup()` runs before the thrown error leaves `Work()`.

Using panic/recover lowering naturally provides this behavior.

---

# Goroutines

Thrown errors do not propagate across goroutine boundaries.

Each goroutine has its own propagation stack.

For v1, if a direct goroutine call would require automatic trailing-error promotion, reject it.

Example:

```go
go Worker()
```

where:

```go
func Worker() error
```

should produce a diagnostic such as:

```text
cannot implicitly propagate error from goroutine call;
handle the error inside the goroutine
```

Use:

```go
go func() {
    try {
        Worker()
    } catch e {
        log.Println(e)
    }
}()
```

or explicitly capture the returned error.

---

# Finally Semantics

`finally` always executes after entering its associated `try`.

Example:

```go
try {
    Work()
} finally {
    Cleanup()
}
```

If `Work()` throws, `Cleanup()` executes before propagation continues.

---

# Errors Thrown From Finally

If `finally` itself throws while another error is already propagating, the error thrown from `finally` becomes the active propagated error in v1.

A future suppressed/cause mechanism may retain both.

Do not add that complexity initially.

---

# Control Transfer From Finally

For v1, disallow:

```text
return
break
continue
goto
```

from inside `finally`.

Suggested diagnostic:

```text
control transfer from finally is not allowed
```

This avoids unintentionally suppressing propagation and simplifies lowering.

---

# Return Through Finally

A `return` inside `try` or `catch` still causes `finally` to execute before returning.

Example:

```go
try {
    return value
} finally {
    cleanup()
}
```

The compiler must preserve this behavior.

---

# Catch and Return

Returning from a `catch` block is allowed.

If a `finally` block exists, it still executes before the return completes.

---

# Custom Go++ Error Types

Go++ classes can define structured errors by implementing:

```go
Error() string
```

Example:

```go
class ValidationError {
    Field string
    Message string

    func Error() string {
        return this.Field + ": " + this.Message
    }
}
```

No inheritance from any special exception class is required.

Usage:

```go
throw ValidationError(
    Field: "email",
    Message: "invalid",
)
```

and:

```go
catch ValidationError e {
    fmt.Println(e.Field)
}
```

---

# Native Go Error Types

Typed catch may also match native Go error types.

Example:

```go
try {
    file := os.Open(path)
} catch *os.PathError e {
    fmt.Println(e.Path)
}
```

---

# `errors.Is` / `errors.As`

Normal Go error utilities remain fully available.

Explicit error-value handling:

```go
_, err := os.Open(path)

if errors.Is(err, os.ErrNotExist) {
    ...
}
```

Inside a catch:

```go
catch e {
    if errors.Is(e, sql.ErrNoRows) {
        ...
    }
}
```

also works normally.

---

# Wrapped Errors

Automatic error promotion does not alter Go error wrapping.

Example:

```go
return fmt.Errorf(
    "load user: %w",
    err,
)
```

continues to work.

If that error result is later omitted by a Go++ caller, the wrapped error is thrown automatically.

---

# Top-Level Main

Example:

```go
func main() {
    Run()
}
```

If `Run()` indirectly throws an uncaught error, the program terminates with a useful diagnostic.

Users do not need a mandatory top-level `try`.

---

# Example: Concise Go Interop

```go
func LoadConfig(path string) Config {
    data := os.ReadFile(path)

    config := Config.FromJSON(data)

    return config
}
```

If either operation returns a non-nil trailing error, it propagates automatically.

No repetitive:

```go
if err != nil
```

boilerplate is required.

---

# Example: Explicit Handling

```go
func LoadConfig(path string) Config {
    try {
        data := os.ReadFile(path)
        return Config.FromJSON(data)
    } catch *os.PathError e {
        return DefaultConfig()
    }
}
```

---

# Example: Error as Value

```go
data, err := os.ReadFile(path)

if errors.Is(err, os.ErrNotExist) {
    return DefaultConfig()
}

if err != nil {
    return Config{}, err
}

return Config.FromJSON(data)
```

Explicit capture prevents automatic promotion.

---

# Example: Chained Propagation

```go
func Register(user User) User {
    Validate(user)

    db.Insert(&user)

    SendWelcomeEmail(user)

    return user
}
```

If each operation returns `error`, each call means:

```text
call
if error != nil
    throw
continue otherwise
```

This is one of the primary DX goals.

---

# Tooling

Because automatic error promotion introduces implicit control flow, tooling should expose it clearly.

Recommended IDE/LSP hover:

```text
LoadUser(id) User
implicit trailing-error promotion
may throw error
```

This is a tooling recommendation, not a semantic requirement.

---

# Compiler Semantic Model

A call with an omitted trailing error should have an effective result set excluding that error.

Given:

```go
func Foo() (A, B, error)
```

then:

```go
a, b := Foo()
```

has effective Go++ result types:

```text
A
B
```

plus an implicit exceptional control-flow edge.

This should be represented during semantic analysis.

Do not implement it solely as a late textual rewrite.

---

# Lowering

Conceptually:

```go
a, b := Foo()
```

may lower to:

```go
a, b, __err := Foo()

if __err != nil {
    __gppThrow(__err)
}
```

Exact generated structure is implementation-defined.

Generated temporary names must avoid collisions.

---

# Statement Call Lowering

Go++:

```go
Save()
```

where:

```go
func Save() error
```

may lower conceptually to:

```go
if __err := Save(); __err != nil {
    __gppThrow(__err)
}
```

---

# Explicit Capture Lowering

Go++:

```go
value, err := Foo()
```

lowers essentially unchanged.

No automatic throw logic is generated for that call.

---

# Runtime Throw Helper

A small runtime helper may centralize propagation:

```go
func __gppThrow(err error) {
    if err != nil {
        panic(__gppThrownError{
            err: err,
        })
    }
}
```

This is an implementation detail.

---

# Compile-Time Validation

The compiler should detect:

* thrown values that do not implement `error`
* bare `throw` outside catch
* invalid typed catch types
* unreachable typed catches where practical
* unsupported implicit propagation in goroutine calls
* illegal control transfer from `finally`
* normal arity mismatches unrelated to trailing-error elision

---

# Catch Ordering Warning

Example:

```go
catch error e {
    ...
} catch ValidationError e {
    ...
}
```

The second catch is unreachable.

Likewise, if one custom error type is assignable to an earlier catch type, diagnose it.

Suggested diagnostic:

```text
unreachable catch: ValidationError is already matched by an earlier catch
```

---

# No Checked Exceptions

Do not add:

```text
throws FooError
```

declarations.

Functions do not declare exception sets.

---

# No Separate Exception Hierarchy

Go++ must not introduce a mandatory parallel hierarchy such as:

```text
Exception
RuntimeException
CheckedException
```

The existing Go:

```go
error
```

interface is sufficient.

This is a core design principle.

---

# Compatibility Principle

Every ordinary Go API returning a trailing `error` should be immediately usable in Go++ without wrappers.

Example:

```go
data := os.ReadFile(path)
conn := net.Dial("tcp", addr)
rows := db.Query(query)
```

all receive exception-style behavior automatically when the trailing error is omitted.

---

# Design Principle

Go++ should make Go's existing explicit error model usable in two styles without changing the underlying API.

## Exception Style

```go
data := os.ReadFile(path)
Save(data)
```

Errors propagate automatically.

## Go Style

```go
data, err := os.ReadFile(path)

if err != nil {
    ...
}
```

Errors remain ordinary values.

The caller chooses naturally by whether the trailing error is captured.

The intended mental model is:

```text
Go function:
    (value..., error)

Go++ caller captures error:
    normal Go semantics

Go++ caller omits error:
    automatic thrown-error semantics
```

Go's `error` remains the one universal error abstraction throughout the system.
