# Go++ Expression-Level Catch Operator (`??`) Specification

## Goal

Add a compact expression-level recovery operator for cases where a thrown `error` should be replaced by a fallback value.

Syntax:

```go
expression ?? fallback
```

Meaning:

> Evaluate the left expression. If it completes successfully, use its value. If evaluating it throws a Go++ error, evaluate and use the right expression instead.

This is equivalent to a small `try/catch` expression.

---

# Basic Example

```go
user := LoadUser(id) ?? GuestUser()
```

Conceptually:

```go
var user User

try {
    user = LoadUser(id)
} catch {
    user = GuestUser()
}
```

The fallback expression is evaluated only if the left expression throws.

---

# Relationship to Automatic Error Promotion

Given:

```go
func LoadUser(id int64) (User, error)
```

this:

```go
user := LoadUser(id)
```

automatically throws if the returned error is non-nil.

Therefore:

```go
user := LoadUser(id) ?? GuestUser()
```

catches that automatically promoted error locally and returns `GuestUser()` instead.

No explicit `try` block is required.

---

# Expression Semantics

For:

```go
A ?? B
```

evaluation proceeds as follows:

```text
evaluate A

if A completes normally:
    result = A

if A throws a Go++ error:
    evaluate B
    result = B
```

`B` is not evaluated unless `A` throws.

---

# Catch Scope

`??` catches any Go++ thrown error produced while evaluating the complete left operand.

Example:

```go
result := Load().
    Validate().
    Save() ?? fallback
```

If any part of:

```text
Load()
Validate()
Save()
```

throws while evaluating the left expression, evaluation stops and `fallback` is evaluated.

`??` does not apply only to the outermost function call.

---

# Ordinary Go Panics

`??` catches Go++ thrown errors and handles nil from an explicit safe access
when the complete left operand is a safe member-access chain.

It must not catch arbitrary Go panics.

Example:

```go
value := BuggyFunction() ?? fallback
```

If `BuggyFunction()` executes:

```go
panic("bug")
```

the panic continues normally.

It is not converted into the fallback.

This follows the same distinction as normal Go++ `catch`.

---

# Thrown Values

The caught value is always a value satisfying Go's:

```go
error
```

interface.

`??` discards the caught error.

If the programmer needs to inspect, log, classify, or rethrow the error, normal `try/catch` should be used instead.

Example:

```go
try {
    user := LoadUser(id)
} catch e {
    log.Println(e)
    user = GuestUser()
}
```

---

# No Error Variable

The simple `??` operator does not bind the caught error.

Do not support syntax such as:

```go
LoadUser(id) ?? e => ...
```

in v1.

Keep the operator intentionally small.

Use `try/catch` when access to the error is required.

---

# Type Compatibility

The successful left value and fallback value must be assignment-compatible.

Example:

```go
user := LoadUser(id) ?? GuestUser()
```

is valid if both sides produce `User`.

Likewise:

```go
port := ParsePort(value) ?? 8080
```

is valid if `ParsePort` effectively produces `int`.

Invalid:

```go
user := LoadUser(id) ?? 42
```

unless ordinary Go++ type rules make the two expressions compatible.

---

# Type of the Expression

The type of:

```go
A ?? B
```

is determined using ordinary Go++ expression compatibility rules.

Typically both sides have the same type.

Example:

```go
func ParsePort(s string) (int, error)
```

then:

```go
port := ParsePort(s) ?? 8080
```

has type:

```text
int
```

The trailing error from `ParsePort` is not part of the resulting expression type.

---

# Short-Circuit Evaluation

`??` is short-circuiting.

Example:

```go
value := Load() ?? ExpensiveFallback()
```

If `Load()` succeeds, `ExpensiveFallback()` is never evaluated.

This is mandatory semantics.

---

# Fallback May Also Throw

The fallback expression is evaluated normally.

Therefore it may itself throw.

Example:

```go
config := LoadPrimary() ?? LoadBackup()
```

If:

```text
LoadPrimary()
```

throws, then:

```text
LoadBackup()
```

is evaluated.

If `LoadBackup()` also throws, that second error propagates normally.

`??` does not repeatedly swallow errors.

---

# Chaining

Because `??` is an expression operator, chaining is allowed.

Example:

```go
config := LoadPrimary() ?? LoadBackup() ?? DefaultConfig()
```

This should behave as:

```text
try LoadPrimary
if error:
    try LoadBackup
    if error:
        use DefaultConfig
```

The operator should therefore be left-associative unless grammar considerations require otherwise:

```go
(LoadPrimary() ?? LoadBackup()) ?? DefaultConfig()
```

This gives intuitive fallback chains.

---

# Example: Configuration

```go
port := strconv.Atoi(os.Getenv("PORT")) ?? 8080
```

If conversion succeeds, use the parsed port.

If conversion throws because of the omitted trailing `error`, use `8080`.

---

# Example: File Fallback

```go
data := os.ReadFile(primary) ?? os.ReadFile(backup)
```

If `primary` cannot be read, try `backup`.

If both fail, the backup error propagates.

---

# Example: Database Lookup

```go
user := FindUser(id) ?? GuestUser()
```

This catches any thrown error from `FindUser`.

If different error types require different behavior, use full `try/catch` instead.

For example:

```go
try {
    user := FindUser(id)
} catch NotFoundError {
    user = GuestUser()
} catch e {
    throw
}
```

`??` intentionally does not distinguish error types.

---

# Error-Only Expressions

For v1, `??` should be defined primarily for value-producing expressions.

Example:

```go
user := LoadUser(id) ?? GuestUser()
```

is clearly valid.

Avoid initially defining statement-style behavior such as:

```go
SendEmail(user) ?? LogFailure()
```

when both sides produce no value.

This can be revisited later if there is a compelling use case.

Keeping `??` expression-oriented simplifies typing and lowering.

---

# Interaction With Explicit Error Capture

Given:

```go
func Load() (User, error)
```

this:

```go
user, err := Load()
```

does not throw automatically.

Therefore:

```go
(user, err := Load()) ?? fallback
```

is not meaningful and should not be special-cased.

`??` operates on thrown-error semantics, not on ordinary non-nil `error` values.

If the error is explicitly captured, it remains a normal value.

---

# Interaction With `throw`

Explicitly thrown errors are caught as well.

Example:

```go
value := Compute() ?? fallback
```

where:

```go
func Compute() int {
    if invalid {
        throw fmt.Errorf("invalid")
    }

    return 42
}
```

uses `fallback` when `Compute()` throws.

---

# Interaction With `try/catch`

`??` is syntactic convenience for simple fallback cases.

Use:

```go
value := Foo() ?? fallback
```

instead of:

```go
try {
    value := Foo()
} catch {
    value = fallback
}
```

when:

* the error itself is irrelevant
* all error types have the same fallback behavior
* the result can be expressed as a single value

Use full `try/catch` when:

* the error needs inspection
* different error types need different handling
* logging or side effects are needed
* rethrowing is needed
* more than one statement is required

---

# Relationship to Safe Access

`??` also supplies a fallback when an explicit `?.` access encounters a nil
receiver. This is a compile-time nil check; `??` does not recover a nil-pointer
panic or change ordinary `.` access.

```go
var person *Person
name := person?.Name ?? "Unknown"
```

The fallback is used only when the safe receiver is nil. If `person` exists and
`person.Name` is the empty string, the result remains the empty string. A safe
access used without `??` keeps its existing behavior and returns the accessed
member's zero value when the receiver is nil.

Safe member accesses may be chained before `??`; if any safe receiver in the
chain is nil, the fallback is evaluated. The accessed value and fallback must
have compatible types.

```go
label := person?.Manager?.Name ?? "No manager"
```

For any left operand that is not an explicit safe access, `??` retains its
Go++ thrown-error fallback behavior. Ordinary Go panics, including nil-pointer
panics from `person.Name`, continue normally.

---

# Precedence

`??` should have relatively low expression precedence.

Recommended behavior:

```go
value := Foo() + Bar() ?? fallback
```

means:

```go
value := (Foo() + Bar()) ?? fallback
```

rather than:

```go
Foo() + (Bar() ?? fallback)
```

unless parentheses specify otherwise.

It should bind more weakly than:

```text
member access
function calls
unary operators
arithmetic
comparison
```

but more strongly than assignment.

Exact precedence should be chosen consistently with the rest of the Go++ grammar.

---

# Associativity

`??` should be left-associative.

Example:

```go
a ?? b ?? c
```

means:

```go
(a ?? b) ?? c
```

This naturally creates fallback chains.

---

# Lowering

Conceptually:

```go
value := Foo() ?? fallback
```

may lower to generated Go roughly equivalent to:

```go
value := func() (result T) {
    defer func() {
        if r := recover(); r != nil {
            if thrown, ok := r.(__gppThrownError); ok {
                result = fallback
                return
            }

            panic(r)
        }
    }()

    result = Foo()
    return
}()
```

The exact lowering is implementation-defined.

The important semantics are:

* catch only Go++ thrown-error wrappers
* re-panic ordinary Go panics
* evaluate fallback only after a thrown error
* return the fallback result as the expression value

The compiler may use more efficient internal lowering.

---

# Nested Lowering

For:

```go
value := Load().Transform() ?? fallback
```

the protected region covers evaluation of the complete left expression:

```text
Load().Transform()
```

not merely the final call.

---

# Compiler Semantic Model

`??` introduces an error-handling boundary in the expression control-flow graph.

Given:

```go
A ?? B
```

the compiler should model:

```text
normal completion of A
        ↓
     result

exceptional completion of A
        ↓
   evaluate B
        ↓
     result
```

This should be represented semantically rather than implemented as a textual rewrite.

---

# Errors From the Fallback

The fallback expression is outside the catch scope of that specific `??`.

Conceptually:

```go
A ?? B
```

means:

```text
catch errors from A
evaluate B normally
```

not:

```text
catch errors from both A and B forever
```

Therefore:

```go
A ?? B
```

where `B` throws causes that new error to propagate.

For additional fallback:

```go
A ?? B ?? C
```

the next `??` catches the error from the preceding expression according to normal associativity.

---

# Side Effects

Side effects performed before the left expression throws are not rolled back.

Example:

```go
value := UpdateDatabaseAndLoad() ?? fallback
```

If `UpdateDatabaseAndLoad()` modifies state and then throws, those modifications remain unless the surrounding application uses transactions or its own rollback mechanisms.

`??` is error handling, not transactional execution.

---

# No Hidden Logging

`??` must not:

* print errors
* log errors
* wrap errors
* attach stack traces
* report telemetry

It simply catches and discards the thrown error, then evaluates the fallback.

Applications requiring observability should use `try/catch`.

---

# No Panic Recovery

`??` must never become a general-purpose panic recovery operator.

This:

```go
value := Foo() ?? fallback
```

must not hide:

```go
panic("index out of range")
```

or other ordinary Go panics.

Only the internal Go++ thrown-error representation is caught.

---

# Design Principle

`??` exists for the common case:

```text
try this expression;
if it fails with an error, use this other value
```

It should remain intentionally small.

Canonical examples:

```go
port := ParsePort(s) ?? 8080

user := LoadUser(id) ?? GuestUser()

config := LoadPrimary() ?? LoadBackup() ?? DefaultConfig()

data := os.ReadFile(path) ?? []byte{}
```

The language retains full `try/catch` for every case requiring richer error handling.

The mental model is simply:

```text
A ?? B

=
evaluate A
catch any Go++ thrown error from A
use B instead
```
