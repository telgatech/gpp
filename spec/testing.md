# Go++ Testing and `gpp test` Specification

## Goal

Provide a first-class testing experience for Go++ that improves ergonomics over raw Go testing while preserving complete compatibility with existing Go tests.

The design principle is:

> Go++ testing should be a library and tooling improvement, not a separate testing language.

Testing is built from:

```text
Go++ classes
    +
gpp/test
    +
gpp test
    +
ordinary go test underneath
```

No special `test { ... }` syntax is required.

---

# Core Model

A Go++ test suite is an ordinary class inheriting from:

```go
test.Suite
```

from:

```go
import "gpp/test"
```

Example:

```go
import "gpp/test"

class UserTest : test.Suite {
    func Create() {
        user := CreateUser("Bob")

        Equal("Bob", user.Name)
        True(user.Active)
    }

    func Delete() {
        user := CreateUser("Bob")

        user.Delete()

        True(user.Deleted)
    }
}
```

Running:

```bash
gpp test
```

discovers suites throughout the current module and executes their test cases together with ordinary Go tests.

---

# Terminology

Use these terms consistently:

```text
suite
    a concrete class deriving from test.Suite

test case
    an eligible public instance method on a suite

fixture
    shared suite state plus Setup/Teardown behavior

current test
    the underlying Go testing context for one case

package scope
    packages searched for tests

suite scope
    selected test.Suite subclasses

metadata scope
    tags and priority filters
```

---

# No Special Test Grammar

Do not introduce core language syntax such as:

```go
test "creates user" {
}
```

or:

```go
expect ...
```

Testing should use ordinary Go++ features:

* classes
* inheritance
* methods
* lambdas
* generics
* errors
* annotations
* enums
* reflection

Testing behavior belongs in:

```text
gpp/test
```

rather than in the core grammar.

---

# Test Suites

Any concrete class deriving directly or indirectly from:

```go
test.Suite
```

is a test suite.

Example:

```go
class UserTest : test.Suite {
}
```

Indirect inheritance also qualifies:

```go
class DatabaseSuite : test.Suite {
}

class UserTest : DatabaseSuite {
}
```

---

# Suite Discovery

`gpp test` discovers suites using semantic class hierarchy information.

Discovery must not depend solely on filenames.

Suites may appear in:

```text
user.gpp
user_test.gpp
tests.gpp
```

or any other source file included in the package.

---

# Test Case Discovery

Public instance methods on a concrete suite are test cases when they:

* are not lifecycle methods
* are not inherited framework helpers
* take no explicit parameters
* return no required application value

Example:

```go
class UserTest : test.Suite {
    func Create() {
        ...
    }

    func Delete() {
        ...
    }

    func helper() {
        ...
    }
}
```

Test cases:

```text
Create
Delete
```

Helper:

```text
helper
```

Lowercase/private methods are not automatically executed.

---

# Reserved Suite Methods

Framework methods are excluded from test discovery.

At minimum:

```go
Setup()
Teardown()
```

are lifecycle methods rather than test cases.

Likewise inherited helpers such as:

```text
Equal
True
Throws
Log
TempDir
Parallel
Cleanup
```

are never test cases.

---

# Inherited Test Cases

Inherited public test methods participate in derived suites unless overridden.

Example:

```go
class CRUDSuite : test.Suite {
    func Create() {
        ...
    }

    func Delete() {
        ...
    }
}

class UserTest : CRUDSuite {
}
```

`UserTest` includes both inherited cases.

Normal Go++ inheritance and override rules apply.

Do not introduce testing-specific method resolution.

---

# Reusable Base Suites

Inheritance should make shared fixtures straightforward.

Example:

```go
class DatabaseSuite : test.Suite {
    DB *sql.DB

    func Setup() {
        DB = OpenTestDB()
    }

    func Teardown() {
        DB.Close()
    }

    func createUser(name string) User {
        return CreateUser(DB, name)
    }
}
```

Then:

```go
class UserTest : DatabaseSuite {
    func Create() {
        user := createUser("Bob")

        Equal("Bob", user.Name)
    }
}
```

---

# Non-Runnable Base Suites

A suite base class containing no runnable test cases should not create an empty test execution entry.

If Go++ later supports abstract classes, abstract suites must never be instantiated directly.

---

# Lifecycle

`test.Suite` provides overridable:

```go
func Setup()
func Teardown()
```

Default implementations do nothing.

Each test case runs as:

```text
create fresh suite instance
        ↓
attach current *testing.T
        ↓
Setup()
        ↓
test case
        ↓
Teardown()
```

`Teardown()` should run even when the test fails or throws, where safely possible.

---

# Fresh Suite Instance Per Test Case

Each test case gets a fresh suite object.

State does not leak across test cases.

Example:

```go
class CounterTest : test.Suite {
    Value int

    func Setup() {
        Value = 0
    }

    func First() {
        Value++
        Equal(1, Value)
    }

    func Second() {
        Equal(0, Value)
    }
}
```

`Second` must begin with `Value == 0`.

---

# Setup Failure

If `Setup()` fails:

* the test body does not run
* the case fails
* `Teardown()` should still run where practical
* setup failure remains the primary failure

---

# Teardown Failure

If the test succeeds but `Teardown()` fails, the test fails.

If both test and teardown fail:

* preserve the test failure as primary
* report teardown failure additionally where practical

---

# No Mandatory Suite-Level Lifecycle

Do not require:

```text
SetupAll
TeardownAll
BeforeAll
AfterAll
```

in v1.

Initial lifecycle remains intentionally small:

```go
Setup()
Teardown()
```

---

# Test Metadata

`gpp/test` should define metadata annotations for test organization and scheduling.

Recommended initial annotations:

```go
annotation (
    Priority(level Priority) on class, method
    Tag(name string) on class, method
)
```

with:

```go
enum Priority int {
    High
    Medium
    Low
}
```

These annotations are library metadata, not core language semantics.

---

# Priority

A suite or test case may declare:

```go
@test.Priority(test.High)
```

using normal Go++ annotation syntax:

```go
class UserTest : test.Suite @{
    test.Priority(test.High)
} {
}
```

or:

```go
func Login() @{
    test.Priority(test.High)
} {
}
```

Exact annotation syntax follows the general Go++ annotation specification.

---

# Priority Defaults

Every runnable test case has an effective priority.

Default:

```text
Medium
```

Therefore:

```text
High
Medium
Low
```

are the only normal scheduling buckets.

Unannotated tests are treated as:

```text
Medium
```

---

# Suite Priority

A class-level priority provides the default priority for cases in that suite.

Example:

```go
class UserTest : test.Suite @{
    test.Priority(test.Medium)
} {
    func Create() {
        ...
    }

    func Login() @{
        test.Priority(test.High)
    } {
        ...
    }
}
```

Effective priorities:

```text
UserTest.Create → Medium
UserTest.Login  → High
```

Method-level priority overrides suite-level priority.

---

# Priority Is Ordering, Not Selection

By default:

```bash
gpp test
```

runs all tests.

Priority affects scheduling order:

```text
High
    ↓
Medium
    ↓
Low
```

Tests must not disappear merely because they have lower priority.

Priority filtering is explicit.

---

# Priority Filtering

Allow:

```bash
gpp test --priority high
```

and:

```bash
gpp test --priority high,medium
```

These select only matching priority buckets.

Names should be case-insensitive.

Examples:

```bash
gpp test --priority high
gpp test --priority high,medium
```

---

# Priority Scheduling

Priority ordering should be runner-controlled.

Do not rely on source declaration order or generated test function order.

`gpp test` should conceptually schedule:

```text
phase 1:
    High

phase 2:
    Medium

phase 3:
    Low
```

This matters because Go does not guarantee global test execution order from source order.

---

# Cross-Package Priority

If the command covers multiple packages, priority semantics should remain meaningful across the selected scope.

Conceptually:

```text
all High tests in selected scope
        ↓
all Medium tests
        ↓
all Low tests
```

The implementation may orchestrate multiple `go test` runs/phases to preserve this behavior.

Do not pretend source ordering alone provides this guarantee.

---

# Tags

Suites and methods may carry one or more:

```go
test.Tag("...")
```

annotations.

Example:

```go
class UserTest : test.Suite @{
    test.Tag("users")
} {
    func Create() @{
        test.Tag("crud")
    } {
        ...
    }

    func PasswordReset() @{
        test.Tag("auth"),
        test.Tag("password")
    } {
        ...
    }
}
```

---

# Effective Tags

Class-level suite tags apply to contained test cases for selection purposes.

Example:

```go
class UserTest : test.Suite @{
    test.Tag("users")
} {
    func Create() @{
        test.Tag("crud")
    } {
    }
}
```

`UserTest.Create` has effective selection tags:

```text
users
crud
```

This is a `gpp/test` policy interpretation.

The underlying annotation metadata remains attached to its original declarations according to normal annotation semantics.

---

# Tag Filtering

Allow:

```bash
gpp test --tag auth
```

Example:

```bash
gpp test --tag crud
gpp test --tag payments
```

Only matching test cases are selected.

---

# Multiple Tag Filters

Repeated tag filters should use OR semantics by default.

Example:

```bash
gpp test --tag auth --tag payments
```

means:

```text
tag == auth
OR
tag == payments
```

A case matching either tag runs.

This is friendlier for ordinary test selection.

---

# All-Tags Filtering

If intersection filtering is needed, support an explicit form such as:

```bash
gpp test --tags-all auth,password
```

This means:

```text
auth
AND
password
```

Do not introduce a full boolean tag-expression language in v1.

---

# Tags Are Semantic Grouping

Tags should be used for feature-oriented test groups such as:

```text
auth
payments
crud
email
database
integration
slow
api
```

This avoids encoding all organization into test names.

Example:

```bash
gpp test --tag auth
```

may run auth-related cases from multiple suites and packages.

---

# Assertions

`gpp/test` provides a compact Minitest-inspired assertion surface.

Initial helpers should include at least:

```go
Equal(expected, actual)
NotEqual(expected, actual)

True(value)
False(value)

Nil(value)
NotNil(value)

Same(expected, actual)
NotSame(expected, actual)

Includes(collection, value)
NotIncludes(collection, value)

Empty(value)
NotEmpty(value)

Match(pattern, value)
NotMatch(pattern, value)

InstanceOf[T](value)
KindOf[T](value)

InDelta(expected, actual, delta)

Fail(message)
Skip(message)

Throws(fn)
Throws[T](fn)
```

---

# Equal

Example:

```go
Equal("Bob", user.Name)
Equal(42, answer)
```

Failures should clearly show expected and actual values.

---

# NotEqual

Example:

```go
NotEqual("", user.Name)
```

---

# Boolean Assertions

Examples:

```go
True(user.Active)
False(user.Deleted)
```

---

# Nil Assertions

Examples:

```go
Nil(user.Manager)
NotNil(user.Id)
```

---

# Identity Assertions

Examples:

```go
Same(expected, actual)
NotSame(a, b)
```

Identity semantics should follow ordinary Go++ reference/object behavior.

---

# Collection Assertions

Examples:

```go
Includes(user.Roles, Role.Admin)
NotIncludes(user.Roles, Role.Guest)

Empty(users)
NotEmpty(users)
```

---

# Pattern Assertions

Examples:

```go
Match(`^[A-Z]`, user.Name)
NotMatch(`\s`, username)
```

Use Go regexp semantics where practical.

---

# Type Assertions

Examples:

```go
InstanceOf[ValidationError](err)
KindOf[BaseError](err)
```

---

# Numeric Delta

Example:

```go
InDelta(3.14, value, 0.001)
```

---

# Fail

Example:

```go
Fail("expected active user")
```

immediately fails the current test.

---

# Skip

Example:

```go
Skip("not supported on this platform")
```

uses the underlying Go testing skip mechanism.

---

# Unexpected Errors

Because Go++ automatically promotes omitted trailing errors:

```go
func Create() {
    user := CreateUser("Bob")
    user.Save()

    Equal("Bob", user.Name)
}
```

requires no `NoError`.

Unexpected errors automatically fail the test.

---

# Expected Errors

Use:

```go
Throws(() => {
    ValidateEmail("bad")
})
```

for any thrown Go++ error.

Typed form:

```go
Throws[ValidationError](() => {
    CreateUser("")
})
```

---

# Throws and Raw Panics

`Throws` catches only Go++ thrown errors.

Raw Go:

```go
panic(...)
```

must remain a normal panic.

---

# Explicitly Captured Errors

If an error is explicitly captured:

```go
user, err := CreateUser("")
```

it remains a normal value.

Assertions may then use:

```go
NotNil(err)
True(errors.Is(err, ErrInvalidUser))
```

---

# Native Go Test Context

Every suite instance should have hidden access to the current:

```go
*testing.T
```

without requiring a parameter on every test method.

---

# Native Test Helpers

`test.Suite` should expose useful wrappers such as:

```go
Log(args ...any)
Logf(format string, args ...any)

TempDir() string

Parallel()

Helper()

Cleanup(fn)

Name() string
```

---

# TempDir

Example:

```go
func SaveFile() {
    dir := TempDir()

    path := filepath.Join(dir, "data.json")
    Save(path)
}
```

---

# Parallel

Example:

```go
func Independent() {
    Parallel()

    ...
}
```

Tests are not automatically parallel.

---

# Cleanup

Example:

```go
Cleanup(() => {
    server.Close()
})
```

uses Go's native cleanup facility.

---

# Helper Methods

Lowercase/private methods are ordinary helpers:

```go
class UserTest : test.Suite {
    func Create() {
        user := createUser()
        Equal("Bob", user.Name)
    }

    func createUser() User {
        return CreateUser("Bob")
    }
}
```

---

# `gpp test`

The command:

```bash
gpp test
```

runs both:

```text
Go++ test.Suite tests
ordinary Go *_test.go tests
```

Go++ testing is additive.

It does not replace Go's testing system.

---

# Default Package Scope

With no explicit package argument:

```bash
gpp test
```

means:

> Test all packages in the current module.

This is intentionally broader than ordinary `go test` default behavior.

The command should locate the current module root and discover packages beneath it.

---

# Module Root Discovery

`gpp test` should locate the nearest enclosing project/module root.

Initially this may be based on:

```text
go.mod
```

and any Go++ module metadata if introduced later.

Running from a nested directory:

```text
project/
    go.mod
    users/
    orders/
```

with:

```bash
cd project/users
gpp test
```

should still mean:

```text
test the entire current module
```

unless an explicit package scope is supplied.

---

# Explicit Current Package

To test only the current package:

```bash
gpp test .
```

This distinction is intentional:

```text
gpp test
    all packages in current module

gpp test .
    current package only
```

---

# Package Selection

Support familiar package paths:

```bash
gpp test .
gpp test ./...
gpp test ./users
gpp test ./users ./orders
```

Package arguments define where discovery occurs.

---

# Multiple Packages

Allow:

```bash
gpp test ./users ./orders
```

to test both selected packages.

Filters such as suite, tag, priority, and run apply inside that package scope.

---

# Default and `./...`

From the module root:

```bash
gpp test
```

will often behave similarly to:

```bash
gpp test ./...
```

but they remain semantically distinct:

```text
no package
    current module

./...
    explicit recursive package pattern
```

---

# Existing `_test.go` Compatibility

Existing Go tests must remain unchanged and runnable.

Example:

```go
func TestDatabase(t *testing.T) {
    ...
}
```

in:

```text
database_test.go
```

must run under:

```bash
gpp test
```

---

# Mixed Test Projects

A project may contain:

```text
user.gpp
user_test.gpp
parser.go
parser_test.go
integration_test.go
```

and `gpp test` must run all relevant Go++ and Go tests.

---

# `_test.gpp`

The convention:

```text
*_test.gpp
```

is supported but not required.

Suites may live in ordinary `.gpp` files.

---

# Production Builds

`gpp build` must exclude:

* `test.Suite` subclasses intended for testing
* generated Go test wrappers
* `_test.gpp`-specific test code where applicable
* ordinary `_test.go` files

---

# Test Discovery Pipeline

Conceptually:

```text
resolve package scope
        ↓
parse .gpp sources
        ↓
resolve class hierarchy
        ↓
discover test.Suite subclasses
        ↓
discover effective test cases
        ↓
compute tags and priorities
        ↓
apply suite/tag/priority/run filters
        ↓
generate *_test.go wrappers
        ↓
include existing *_test.go
        ↓
schedule by priority
        ↓
invoke go test
```

---

# Suite Selection

Allow:

```bash
gpp test --suite UserTest
```

This selects all cases in that suite.

Multiple suites:

```bash
gpp test --suite UserTest --suite OrderTest
```

---

# Suite Names

Suite selection uses semantic class names.

If names are ambiguous across packages, allow qualified names such as:

```bash
gpp test --suite users.UserTest
```

using normal Go++ package qualification rules.

---

# Unknown Suite

If a requested suite does not exist, fail clearly.

Example:

```text
suite not found: UserTes

Did you mean:
    UserTest
```

Do not silently execute zero tests.

---

# Case Selection

Use:

```bash
gpp test --run UserTest.Create
```

for a specific test case.

Canonical case identity:

```text
SuiteName.MethodName
```

Examples:

```text
UserTest.Create
UserTest.Delete
OrderTest.Create
```

---

# `--run`

`--run` provides name-based filtering.

Examples:

```bash
gpp test --run Create
gpp test --run UserTest.Create
```

The implementation may translate this into generated Go test names internally.

Users should not need to know those generated names.

---

# Filtering Composition

Selectors should compose.

Examples:

```bash
gpp test ./users --suite UserTest
```

```bash
gpp test --tag auth
```

```bash
gpp test ./... --tag payments --priority high
```

```bash
gpp test --suite UserTest --tag auth
```

```bash
gpp test --suite UserTest --run PasswordReset
```

---

# Selection Order

Conceptually filters apply as:

```text
package scope
    ↓
suite filter
    ↓
tag filter
    ↓
priority filter
    ↓
run/name filter
    ↓
priority scheduling
```

Exact implementation may optimize this pipeline.

---

# Listing Tests

Support:

```bash
gpp test --list
```

Example output:

```text
users.UserTest
    Create              Medium   users,crud
    Login               High     users,auth
    PasswordReset       High     users,auth,password

orders.OrderTest
    Create              Medium   orders,crud
    Cancel              Low      orders

Go tests
    TestParser
    TestDatabase
```

---

# Listing One Suite

Allow:

```bash
gpp test --list --suite UserTest
```

to display only that suite and its effective metadata.

---

# Filtering Native Go Tests

Ordinary `_test.go` tests continue to use Go's normal test naming/filter mechanisms.

Metadata annotations apply only to Go++ suites unless a future explicit mechanism maps native Go tests into metadata.

Do not attempt to infer tags or priorities from ordinary Go test names.

---

# Suite Filters and Native Go Tests

When a `--suite` filter is supplied:

```bash
gpp test --suite UserTest
```

the intent is specifically to run selected Go++ suites.

Unrelated native Go tests need not run.

With no suite filter:

```bash
gpp test
```

all native Go tests run normally.

---

# Tag Filters and Native Go Tests

A tag filter:

```bash
gpp test --tag auth
```

selects Go++ cases carrying that metadata.

Native Go tests have no Go++ tags and therefore are not automatically selected by tag filtering.

This behavior should be documented clearly.

---

# Priority Filters and Native Go Tests

Native Go tests have no Go++ priority metadata.

When no priority filter is supplied, they run normally.

When an explicit Go++ priority filter is supplied, native test behavior should be predictable and documented.

Recommended v1 behavior:

```text
--priority applies to Go++ suite cases only
```

while native Go tests continue only when broader `--run`/package execution includes them.

Avoid inventing implicit priorities for native Go tests.

---

# Pass-Through Go Testing Features

`gpp test` should preserve access to:

```text
coverage
race detector
benchmarks
fuzzing
verbose mode
timeouts
profiles
Go test filtering
```

Do not rebuild these facilities unnecessarily.

---

# Coverage

Support:

```bash
gpp test -cover
```

and/or:

```bash
gpp test --cover
```

using Go coverage underneath.

Where practical, coverage should map back to `.gpp` source.

---

# Race Detector

Support:

```bash
gpp test -race
```

---

# Native Benchmarks

Existing:

```go
func BenchmarkFoo(b *testing.B) {
    ...
}
```

must continue to work.

---

# Native Fuzz Tests

Existing Go fuzz tests in `_test.go` must continue to work unchanged.

---

# Fail Fast

A useful runner option should be:

```bash
gpp test --fail-fast
```

This stops scheduling new test phases/cases after the first failure where practical.

Priority makes this particularly useful:

```bash
gpp test --priority high --fail-fast
```

---

# Generated Go Test Names

Generated names should be stable and readable.

Recommended:

```text
UserTest.Create
```

lowers to something like:

```text
TestUserTest_Create
```

This preserves compatibility with Go's test runner.

---

# Lowering

Example:

```go
class UserTest : test.Suite {
    func Create() {
        user := CreateUser("Bob")

        Equal("Bob", user.Name)
    }
}
```

may lower conceptually to:

```go
func TestUserTest_Create(t *testing.T) {
    suite := newUserTestForTesting(t)

    runGppSuiteCase(
        t,
        suite.Setup,
        suite.Create,
        suite.Teardown,
    )
}
```

Exact generated structure is implementation-defined.

---

# Failure From Uncaught Go++ Errors

If a case throws:

```go
func Create() {
    CreateUser("")
}
```

the wrapper should convert that uncaught Go++ error into a normal test failure rather than crashing the complete test process.

Report where practical:

```text
suite
case
error type
error message
Go++ source location
```

Raw Go panics retain normal panic behavior.

---

# Assertion Diagnostics

Assertions should produce concise structured output.

Example:

```text
FAIL UserTest.Create

Expected:
    "Bob"

Actual:
    "Alice"

user_test.gpp:18
```

`Object.Dump()` and reflection may be used for useful structural output.

---

# Diff Support

`Equal` may provide useful diffs for:

```text
strings
records
classes
slices
maps
```

where appropriate.

---

# Optional Assertion Message

Allow an optional custom message:

```go
Equal(
    "Bob",
    user.Name,
    "created user should retain its name",
)
```

---

# Minitest Inspiration

`gpp/test` should borrow from Minitest:

```text
class-based suites
Setup / Teardown
small assertion vocabulary
low ceremony
clear failures
```

without copying Ruby behavior unnecessarily.

---

# No Core Mocking Framework

Do not add mocking/stubbing syntax to the core language or initial `gpp/test`.

Mocks may be supplied later as ordinary libraries.

---

# No Test-Specific Core Keywords

Avoid adding:

```text
test
expect
fixture
mock
before
after
```

as core language keywords solely for testing.

Existing Go++ features are sufficient.

---

# Example: Tagged and Prioritized Suite

```go
import "gpp/test"

class UserTest : test.Suite @{
    test.Tag("users"),
    test.Priority(test.Medium)
} {
    func Create() @{
        test.Tag("crud")
    } {
        user := CreateUser("Bob")

        Equal("Bob", user.Name)
    }

    func Login() @{
        test.Tag("auth"),
        test.Priority(test.High)
    } {
        user := Login("bob", "secret")

        NotNil(user)
    }

    func PasswordReset() @{
        test.Tag("auth"),
        test.Tag("password"),
        test.Priority(test.High)
    } {
        ResetPassword("bob")
    }

    func ArchiveOldAccounts() @{
        test.Tag("maintenance"),
        test.Priority(test.Low)
    } {
        ...
    }
}
```

Effective metadata:

```text
UserTest.Create
    Priority: Medium
    Tags: users, crud

UserTest.Login
    Priority: High
    Tags: users, auth

UserTest.PasswordReset
    Priority: High
    Tags: users, auth, password

UserTest.ArchiveOldAccounts
    Priority: Low
    Tags: users, maintenance
```

---

# Example Commands

Run the entire current module:

```bash
gpp test
```

Run current package only:

```bash
gpp test .
```

Run recursive package tree:

```bash
gpp test ./...
```

Run selected packages:

```bash
gpp test ./users ./orders
```

Run one suite:

```bash
gpp test --suite UserTest
```

Run multiple suites:

```bash
gpp test --suite UserTest --suite OrderTest
```

Run one semantic feature group:

```bash
gpp test --tag auth
```

Run either auth or payments tests:

```bash
gpp test --tag auth --tag payments
```

Require multiple tags:

```bash
gpp test --tags-all auth,password
```

Run only high-priority tests:

```bash
gpp test --priority high
```

Run high and medium:

```bash
gpp test --priority high,medium
```

Run high-priority payment tests across the module:

```bash
gpp test --tag payments --priority high
```

Run a specific case:

```bash
gpp test --run UserTest.PasswordReset
```

Run auth cases only within UserTest:

```bash
gpp test --suite UserTest --tag auth
```

List discovered tests:

```bash
gpp test --list
```

---

# Design Principle

`gpp test` should improve test authoring, organization, and execution while retaining Go's battle-tested testing infrastructure.

The model is:

```text
module/package selection
        ↓
Go++ test.Suite discovery
        ↓
suite/case metadata
        ↓
tags + priorities
        ↓
Minitest-like assertions and lifecycle
        ↓
generated *_test.go wrappers
        ↓
existing *_test.go support
        ↓
priority-aware orchestration
        ↓
go test
```

The intended experience is:

> Go testing with less ceremony, reusable class-based fixtures, semantic grouping, priority scheduling, better assertions, native Go++ error handling, and full compatibility with ordinary Go tests.
