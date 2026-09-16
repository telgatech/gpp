# Go++ Enum Specification

## Goal

Add real enums to Go++ as first-class typed scalar values with:

* explicit backing types
* concise member declarations
* optional explicit member values
* closed known value sets
* type-safe comparisons
* generated conversion
* introspection metadata
* natural JSON/SQL/HTTP/serialization behavior
* clean lowering to ordinary Go

Enums are deliberately **not** sum types or tagged unions.

They carry no payloads and do not require pattern matching.

---

# Basic Syntax

An enum declares:

```go
enum Status int {
    Pending
    Active
    Suspended
}
```

or:

```go
enum Role string {
    User
    Admin
    Moderator
}
```

The backing type is required.

Member values may be omitted.

---

# Grouped Enum Declarations

Enums may be grouped:

```go
enum (
    Status int {
        Pending
        Active
        Suspended
    }

    Role string {
        User
        Admin
    }

    Priority int {
        Low
        Medium
        High
    }
)
```

Grouping is purely syntactic.

Each enum remains an independent type with its own members, backing type, metadata, and generated methods.

---

# Backing Types

The backing type must be explicitly declared.

Initial supported backing types should be scalar types suitable for constants and external representation.

At minimum:

```text
string
int
int8
int16
int32
int64
uint
uint8
uint16
uint32
uint64
```

Support for additional scalar types may be added later if useful.

Examples:

```go
enum Status int {
    Pending
    Active
}
```

```go
enum Code int32 {
    OK       = 200
    NotFound = 404
}
```

```go
enum Role string {
    User
    Admin
}
```

---

# Integer Member Defaults

For integer-backed enums, omitted values are automatically assigned sequentially.

Example:

```go
enum Status int {
    Pending
    Active
    Suspended
}
```

means:

```text
Pending   = 0
Active    = 1
Suspended = 2
```

An explicit value resets the sequence:

```go
enum Priority int {
    Low
    Medium = 10
    High
}
```

means:

```text
Low    = 0
Medium = 10
High   = 11
```

The behavior should be deterministic and analogous to the useful part of Go's `iota` pattern without requiring `iota` syntax.

---

# String Member Defaults

For string-backed enums, an omitted value defaults to the member name.

Example:

```go
enum Role string {
    User
    Admin
    Moderator
}
```

means:

```text
User      = "User"
Admin     = "Admin"
Moderator = "Moderator"
```

Explicit values may override this:

```go
enum Role string {
    User      = "user"
    Admin     = "admin"
    Moderator = "moderator"
}
```

Mixed explicit and implicit string members are allowed:

```go
enum Role string {
    User = "user"
    Admin
}
```

means:

```text
User  = "user"
Admin = "Admin"
```

---

# Member Values Must Match the Backing Type

All explicit values must be assignable to the declared backing type.

Invalid:

```go
enum Status int {
    Pending = 0
    Active  = "active"
}
```

Compile-time error:

```text
enum member Active has value of type string;
expected int
```

---

# Member Access

Enum members are namespaced under the enum type:

```go
Status.Pending
Status.Active
Role.Admin
```

Do not require package-level names such as:

```go
StatusPending
StatusActive
```

at the Go++ source level.

The compiler may use such names internally when lowering to Go.

---

# Enum Type Safety

Enums are distinct named types.

Example:

```go
enum Status int {
    Pending
    Active
}

enum Priority int {
    Low
    High
}
```

These are not interchangeable even though both use `int`.

Invalid:

```go
var status Status = Priority.High
```

Likewise, arbitrary backing values should not silently become enum values:

```go
var status Status = 999
```

should not be accepted through ordinary implicit assignment.

Use:

```go
status := Status.From(999)
```

if conversion and validation are desired.

---

# Closed Known Value Set

The compiler knows the complete declared set of enum values.

Example:

```go
enum Status int {
    Pending
    Active
    Suspended
}
```

defines a known set:

```text
0
1
2
```

Values outside that set are not valid enum members.

This knowledge is used for:

* conversion validation
* serialization validation
* reflection
* HTTP binding
* database decoding
* generated tooling

---

# `From`

Every enum receives one generated static conversion method:

```go
EnumType.From(value)
```

The argument type is the enum's backing type.

Examples:

```go
status := Status.From(1)
role := Role.From("admin")
```

Conceptual generated signatures:

```go
static func From(value int) (Status, error)
```

```go
static func From(value string) (Role, error)
```

`From` validates that the backing value corresponds to a declared enum member.

---

# Invalid `From`

Given:

```go
enum Role string {
    User  = "user"
    Admin = "admin"
}
```

this:

```go
role := Role.From("manager")
```

returns an error.

Because Go++ automatically promotes omitted trailing errors, ordinary use effectively throws:

```go
role := Role.From("manager")
```

If explicit handling is desired:

```go
role, err := Role.From("manager")
```

retains normal error-value semantics.

No separate `.Parse` API is required.

---

# Enum Metadata

Enum values should expose:

```go
.name
.value
```

Example:

```go
Role.Admin.name
```

returns:

```text
"Admin"
```

while:

```go
Role.Admin.value
```

returns:

```text
"admin"
```

for:

```go
enum Role string {
    Admin = "admin"
}
```

For:

```go
enum Status int {
    Pending
    Active
}
```

then:

```go
Status.Active.name
```

is:

```text
"Active"
```

and:

```go
Status.Active.value
```

is:

```text
1
```

---

# Enum Value Collection

Each enum exposes its declared values:

```go
Status.values
Role.values
```

This should produce a deterministic collection in source declaration order.

Example:

```go
for status := range Status.values {
    fmt.Println(status.name)
}
```

The exact generated collection representation is implementation-defined.

It should behave as a read-only enum value collection.

---

# Ordering

`values` preserves declaration order.

Example:

```go
enum Status int {
    Pending
    Active
    Suspended
}
```

gives:

```text
Status.Pending
Status.Active
Status.Suspended
```

in that order.

Do not sort by backing value automatically.

---

# Duplicate Names

Duplicate enum member names are compile-time errors.

Invalid:

```go
enum Status int {
    Active
    Active
}
```

---

# Duplicate Backing Values

For v1, duplicate backing values should be rejected.

Invalid:

```go
enum Status int {
    Active  = 1
    Enabled = 1
}
```

Reason:

* `.From(1)` would otherwise be ambiguous
* `.name` would not have a canonical result
* serialization round trips become less predictable

Suggested diagnostic:

```text
duplicate enum value 1 for members Active and Enabled
```

Aliasing may be considered later if a clear canonical-member rule is introduced.

---

# Comparisons

Values of the same enum type may be compared normally:

```go
if user.Status == Status.Active {
    ...
}
```

Ordering comparisons may be allowed when the backing type supports them:

```go
if priority > Priority.Low {
    ...
}
```

This should follow ordinary Go rules for the generated underlying type.

---

# No Implicit Cross-Enum Conversion

Even when two enum types use the same backing type:

```go
enum Status int {
    Active
}

enum Priority int {
    High
}
```

this is invalid:

```go
status := Priority.High
```

Explicit conversion must go through the target enum's backing value and validation when needed.

---

# External Representation Principle

At serialization, persistence, and binding boundaries:

> Enums encode as their backing values and decode by validating the backing value against the enum's declared value set.

The enum member name is metadata.

The backing value is the external representation.

---

# JSON Encoding

Given:

```go
enum Status string {
    Pending = "pending"
    Active  = "active"
}
```

and:

```go
record(
    id: 123,
    status: Status.Active,
)
```

JSON should be:

```json
{
  "id": 123,
  "status": "active"
}
```

not:

```json
{
  "status": "Active"
}
```

Serialization uses:

```text
Status.Active.value
```

not:

```text
Status.Active.name
```

---

# Integer JSON Encoding

Given:

```go
enum Priority int {
    Low
    Medium
    High
}
```

then:

```go
record(
    priority: Priority.High,
)
```

serializes as:

```json
{
  "priority": 2
}
```

---

# JSON Decoding

Given:

```go
enum Status string {
    Pending = "pending"
    Active  = "active"
}
```

JSON:

```json
{
  "status": "active"
}
```

should decode to:

```go
Status.Active
```

Conceptually this is equivalent to:

```go
Status.From("active")
```

The implementation need not literally call that method.

---

# Invalid JSON Enum Values

JSON containing:

```json
{
  "status": "deleted"
}
```

when `"deleted"` is not a declared value must result in a decoding error.

Example:

```go
user := User.FromJSON(data)
```

will automatically propagate that error under normal Go++ error semantics.

Explicit handling remains possible:

```go
user, err := User.FromJSON(data)
```

---

# Serializable Classes

Enums should work naturally inside classes using `gpp/encoding`.

Example:

```go
enum Status string {
    Pending = "pending"
    Active  = "active"
}

class User @{encoding.Serializable} {
    Name string
    Status Status
}
```

Then:

```go
user := User(
    Name: "Bob",
    Status: Status.Active,
)

data := user.JSON()
```

produces conceptually:

```json
{
  "Name": "Bob",
  "Status": "active"
}
```

No enum-specific annotation should be required.

---

# Records

Enums should work naturally in anonymous records.

Example:

```go
r := record(
    id: 123,
    status: Status.Active,
    role: Role.Admin,
)
```

The record retains the actual enum types internally.

Serialization emits backing values.

---

# Collections

Collections of enums serialize as collections of backing values.

Example:

```go
statuses := []Status{
    Status.Pending,
    Status.Active,
}
```

serializes to:

```json
[
  "pending",
  "active"
]
```

---

# Nested Structures

Enum handling should work recursively.

Example:

```go
record(
    user: record(
        status: Status.Active,
    ),
)
```

serializes the nested enum using its backing value.

No special handling should be required from the user.

---

# Maps

Enum values used as map values should serialize using normal backing-value rules.

Example:

```go
map[string]Status{
    "bob": Status.Active,
}
```

becomes conceptually:

```json
{
  "bob": "active"
}
```

Map-key behavior should follow the encoding backend's normal key rules.

Do not invent enum-specific JSON object-key semantics beyond what the backing type and encoder naturally support.

---

# SQL Persistence

Enums should behave as their backing values when bound to SQL parameters.

Given:

```go
enum Status string {
    Pending = "pending"
    Active  = "active"
}
```

then:

```go
db.Insert(&user)
```

for:

```go
user.Status = Status.Active
```

should bind:

```text
"active"
```

to the database.

For:

```go
enum Priority int {
    Low
    Medium
    High
}
```

`Priority.High` binds as:

```text
2
```

---

# SQL Decoding

Database values should be decoded through enum validation.

If a database column contains:

```text
"active"
```

it becomes:

```go
Status.Active
```

If it contains:

```text
"deleted"
```

and no such enum value exists, scanning/decoding should return an error.

Do not silently construct invalid enum values.

---

# HTTP Query Binding

Enums should bind naturally from request values.

Example:

```go
func Users(
    status Status @{http.Query}
) []User @{
    http.GET("/users")
}
```

Request:

```text
/users?status=active
```

should bind:

```go
status == Status.Active
```

Conceptually:

```go
Status.From("active")
```

for a string-backed enum.

Invalid values enter the ordinary HTTP binding error path.

---

# HTTP Path Binding

Enums should also work for path parameters:

```go
func Users(
    status Status
) []User @{
    http.GET("/users/{status}")
}
```

Request:

```text
/users/active
```

binds to:

```go
Status.Active
```

when the backing type is string.

Numeric path values may bind to integer-backed enums after normal scalar conversion.

---

# HTTP Body Binding

When an enum appears inside a request body model:

```go
class CreateUserRequest {
    Status Status
}
```

JSON:

```json
{
  "Status": "active"
}
```

should decode to:

```go
Status.Active
```

through the same encoding rules.

Do not duplicate enum parsing logic in `gpp/http`.

The HTTP package should reuse the encoding/type-conversion layer where practical.

---

# YAML and Other Text Formats

The same external representation rule applies:

```text
enum → backing value
backing value → validated enum
```

Example:

```go
Status.Active
```

for a string-backed enum should appear in YAML as:

```yaml
status: active
```

unless the encoding backend explicitly defines another representation.

---

# GOB

GOB may preserve the enum's generated Go named scalar type directly.

It should still round-trip as the same enum type.

Validation on decode should ensure the resulting backing value belongs to the declared enum set where practical.

Do not rely on arbitrary out-of-range backing values becoming valid enums merely because the underlying Go scalar can represent them.

---

# Enum Conversion at Boundaries

All boundary conversion should conceptually follow:

```text
external backing value
        ↓
Enum.From(value)
        ↓
typed enum
```

and:

```text
typed enum
        ↓
enum.value
        ↓
external representation
```

Libraries may optimize this and need not literally invoke `.From` or `.value`.

The semantics must remain equivalent.

---

# Go Lowering

An enum should lower cleanly to an ordinary named Go scalar type plus constants.

Example:

```go
enum Status int {
    Pending
    Active
    Suspended
}
```

may lower approximately to:

```go
type Status int

const (
    StatusPending Status = 0
    StatusActive  Status = 1
    StatusSuspended Status = 2
)
```

The exact generated identifiers are implementation-defined.

---

# String Enum Lowering

Example:

```go
enum Role string {
    User  = "user"
    Admin = "admin"
}
```

may lower approximately to:

```go
type Role string

const (
    RoleUser  Role = "user"
    RoleAdmin Role = "admin"
)
```

---

# Generated `From`

The compiler may generate code conceptually similar to:

```go
func RoleFrom(value string) (Role, error) {
    switch value {
    case "user":
        return RoleUser, nil
    case "admin":
        return RoleAdmin, nil
    default:
        return "", fmt.Errorf(
            "invalid Role value %q",
            value,
        )
    }
}
```

Go++ exposes this as:

```go
Role.From(value)
```

through its static-method model.

Exact generated naming is implementation-defined.

---

# Generated Metadata

The compiler should generate static metadata sufficient to support:

```go
Role.values
Role.Admin.name
Role.Admin.value
```

This metadata should be:

* deterministic
* read-only
* shared
* generated at compile time
* available to reflection/introspection

Avoid runtime scanning or registration where unnecessary.

---

# Reflection

Enums should participate in Go++ introspection.

A type descriptor may expose enum-specific metadata such as:

```go
Role.enum
Role.values
```

or equivalent through the existing class/type metadata system.

Exact reflection API may be finalized separately.

At minimum, libraries must be able to determine:

* whether a type is an enum
* its backing type
* its declared members
* each member's name
* each member's backing value

---

# String Conversion

Do not automatically redefine the enum's external representation around `String()`.

If a generated or user-defined `String()` exists, it is a human-readable method.

Serialization still uses:

```text
.value
```

unless a specific encoder explicitly documents otherwise.

For example:

```go
Role.Admin.name   // "Admin"
Role.Admin.value  // "admin"
```

must remain distinct concepts.

---

# Methods

Enums may support ordinary methods if Go++'s type method model permits them.

Example:

```go
extend Status {
    func Terminal() bool {
        return this == Status.Suspended
    }
}
```

This is orthogonal to enum membership.

---

# Static Methods

Generated:

```go
Status.From(...)
```

is a static enum method.

Additional static methods may be added through normal Go++ extension syntax if supported:

```go
extend Status {
    static func Default() Status {
        return Status.Pending
    }
}
```

---

# Annotations

Enums and enum members may eventually support annotations if useful:

```go
enum Status string {
    Active
}
```

Annotation-target support is a separate decision.

Do not require enum annotations for basic serialization, persistence, or binding behavior.

---

# No Payload Enums

Do not support:

```go
enum Result {
    Ok(value)
    Error(err)
}
```

as part of this feature.

That is a sum type / tagged union feature and is outside the enum specification.

---

# No Pattern Matching Requirement

Enums do not require a new pattern matching construct.

Ordinary control flow remains sufficient:

```go
switch status {
case Status.Pending:
    ...
case Status.Active:
    ...
}
```

Go++ may improve exhaustiveness diagnostics later, but that is not required for v1.

---

# Exhaustiveness Diagnostics

Because the compiler knows the complete enum member set, it may optionally warn when a switch over an enum omits members.

Example:

```go
switch status {
case Status.Pending:
case Status.Active:
}
```

when `Status.Suspended` exists.

This is a tooling/compiler enhancement, not required core semantics.

---

# Zero Values

Integer-backed enums whose first member is `0` naturally have a valid Go zero value.

Example:

```go
enum Status int {
    Pending
    Active
}
```

zero value:

```go
Status.Pending
```

String-backed enums typically have Go zero value:

```text
""
```

which may not correspond to any declared member.

Go++ should distinguish:

```text
underlying Go zero representation
```

from:

```text
valid declared enum value
```

A zero-valued string enum is therefore potentially invalid until explicitly assigned.

Boundary decoding and `.From` must validate it normally.

The language should not silently invent an empty enum member.

---

# Invalid Raw Values

Generated Go or unsafe/native interop may theoretically construct an enum with an undeclared backing value.

Go++ code should not assume such a value is valid merely because the underlying scalar exists.

Operations such as:

```go
.name
```

should detect invalid enum values.

Possible behavior:

```text
error / invalid marker
```

should be defined consistently by the runtime metadata implementation.

Ordinary Go++ source should normally create enums only through declared members or `.From`.

---

# Go Interoperability

Generated enum types remain ordinary named Go scalar types.

This allows them to participate naturally in:

* function signatures
* structs
* interfaces where applicable
* SQL drivers
* JSON implementations
* Go maps and slices
* external Go APIs expecting compatible named/scalar values

Go++ should preserve ordinary Go ABI compatibility where possible.

---

# Design Principle

Enums should provide a real typed closed-set abstraction while still compiling to straightforward Go.

The intended source model is:

```go
enum Role string {
    User  = "user"
    Admin = "admin"
}
```

with:

```go
Role.Admin
Role.Admin.name
Role.Admin.value
Role.values
Role.From("admin")
```

and automatic cross-layer behavior:

```text
HTTP / JSON / SQL / YAML
          ↓
       "admin"
          ↓
     Role.Admin
          ↓
 application logic
          ↓
       "admin"
          ↓
HTTP / JSON / SQL / YAML
```

The enum's **member name** is for source-level identity and metadata.

The enum's **backing value** is its default external representation.
