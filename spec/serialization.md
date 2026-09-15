# gpp/encoding Serializable Specification

## Goal

Provide opt-in serialization support for Go++ classes through:

```go
@{encoding.Serializable}
```

A serializable class should automatically gain convenient generated instance and static methods for supported formats.

Initial formats:

* JSON
* YAML
* GOB

The feature should be implemented through ordinary Go++ annotations plus compile-time code generation.

---

# Package

Use:

```go
import "gpp/encoding"
```

The package owns the serialization annotations and supporting implementation.

---

# Basic Usage

```go
class User @{encoding.Serializable} {
    Id int
    Name string
}
```

This automatically adds:

```go
func ToJSON() ([]byte, error)
func ToYAML() ([]byte, error)

static func FromJSON(data []byte) (User, error)
static func FromYAML(data []byte) (User, error)

func ToGOB() ([]byte, error)

static func FromGOB(data []byte) (User, error)
```

Usage:

```go
user := User(
    Id: 1,
    Name: "Ada",
)

jsonData, err := user.ToJSON()

copy, err := User.FromJSON(jsonData)

yamlData, err := user.ToYAML()

copy2, err := User.FromYAML(yamlData)
```

---

# Serializable Annotation

Declare:

```go
annotation Serializable on class
```

Usage:

```go
class User @{encoding.Serializable} {
}
```

The annotation opts the class into generated encoding behavior.

Classes without the annotation do not automatically gain these methods.

---

# Generated Methods

For v1, `Serializable` generates:

```go
func ToJSON() ([]byte, error)
func ToYAML() ([]byte, error)
func ToGOB() ([]byte, error)

static func FromJSON(data []byte) (T, error)
static func FromYAML(data []byte) (T, error)
static func FromGOB(data []byte) (T, error)
```

where `T` is the annotated concrete class.

For:

```go
class User @{encoding.Serializable} {
}
```

generate logically:

```go
func ToJSON() ([]byte, error)

func ToYAML() ([]byte, error)

static func FromJSON(data []byte) (User, error)

static func FromYAML(data []byte) (User, error)
```

---

# JSON

Generated:

```go
user.ToJSON()
```

should use Go's normal JSON encoding machinery where practical:

```go
encoding/json
```

Likewise:

```go
User.FromJSON(data)
```

should decode using normal Go JSON semantics.

Conceptually:

```go
func ToJSON() ([]byte, error) {
    return json.Marshal(this)
}

static func FromJSON(data []byte) (User, error) {
    value := User()
    err := json.Unmarshal(data, &value)
    return value, err
}
```

Exact lowering may differ due to Go++ object layout.

---

# YAML

Generated:

```go
user.ToYAML()
User.FromYAML(data)
```

should use the chosen official YAML backend.

Because Go does not provide YAML in the standard library, `gpp/encoding` may depend on a selected external Go YAML package.

That dependency belongs to `gpp/encoding`, not to the compiler core.

---

# GOB

GOB uses Go's standard `encoding/gob` backend and is intended for internal
Go/Go++ communication, local persistence, caches, queues, and snapshots.

```go
func ToGOB() ([]byte, error)

static func FromGOB(data []byte) (T, error)
```

---

# Field Metadata

Provide field annotations for customization.

Recommended initial declarations:

```go
annotation (
    Name(name string) on field
    Ignore on field
    OmitEmpty on field
)
```

Example:

```go
class User @{encoding.Serializable} {
    Id int @{encoding.Name("id")}

    Name string @{
        encoding.Name("name")
    }

    Password string @{
        encoding.Ignore
    }

    Nickname string @{
        encoding.OmitEmpty
    }
}
```

---

# Name

`Name` changes the serialized field name.

Example:

```go
FirstName string @{
    encoding.Name("first_name")
}
```

JSON:

```json
{
  "first_name": "Ada"
}
```

YAML should use the same logical name unless format-specific annotations are introduced later.

---

# Ignore

Example:

```go
Password string @{
    encoding.Ignore
}
```

The field must not be serialized.

It should also be ignored during deserialization.

---

# OmitEmpty

Example:

```go
Nickname string @{
    encoding.OmitEmpty
}
```

When the value is empty according to the underlying encoder's semantics, omit the field when encoding.

For JSON this should behave consistently with Go's `omitempty` where possible.

---

# Compile-Time Generation

Prefer compile-time generated encoding metadata/code over custom runtime reflection.

For formats whose Go implementation uses struct tags, the compiler may lower annotations to generated tags.

Example:

```go
class User @{encoding.Serializable} {
    Id int @{
        encoding.Name("id")
    }

    Password string @{
        encoding.Ignore
    }
}
```

may lower conceptually to fields equivalent to:

```go
Id       int    `json:"id" yaml:"id"`
Password string `json:"-" yaml:"-"`
```

Exact generated representation is implementation-defined.

---

# User-Facing Abstraction

Users should not need to write Go struct tags directly for ordinary Go++ classes.

Preferred:

```go
Name string @{
    encoding.Name("name")
}
```

rather than:

```go
Name string `json:"name" yaml:"name"`
```

Native Go tags may remain available where interoperability requires them.

---

# Generated Method Conflicts

If a user explicitly defines one of the generated methods:

```go
class User @{encoding.Serializable} {
    func ToJSON() ([]byte, error) {
        ...
    }
}
```

do not silently generate another method with the same signature.

Preferred behavior:

Use the explicitly supplied method as the class implementation.

The annotation generates only methods that are absent.

This allows custom behavior while retaining the convenience of generated methods.

Example:

```go
class User @{encoding.Serializable} {
    func ToJSON() ([]byte, error) {
        return customJSON(this)
    }
}
```

Still generate:

```go
ToYAML()
FromJSON(...)
FromYAML(...)
```

if they are absent.

---

# Incompatible Conflicts

If the user defines:

```go
func ToJSON() string
```

while `Serializable` expects:

```go
func ToJSON() ([]byte, error)
```

report a compile-time conflict.

Suggested diagnostic:

```text
encoding.Serializable requires User.ToJSON() ([]byte, error),
but User defines an incompatible ToJSON method
```

Same rule for generated static methods.

---

# Inheritance

Serializable behavior should apply to the concrete annotated class.

Example:

```go
class Person {
    Name string
}

class Employee : Person @{
    encoding.Serializable
} {
    Id int
}
```

`Employee.ToJSON()` should serialize the persisted/visible fields of the complete `Employee` object according to Go++ class field semantics, including inherited fields.

---

# Annotation Inheritance

Do not automatically inherit the `Serializable` annotation itself unless the language establishes that annotation inheritance generally works that way.

Preferred rule:

```go
class Person @{encoding.Serializable} {
}

class Employee : Person {
}
```

does not automatically imply that `Employee` gets generated serialization methods solely because `Person` is annotated.

If users want serialization on `Employee`, they should explicitly declare:

```go
class Employee : Person @{
    encoding.Serializable
} {
}
```

This keeps code generation explicit.

---

# Runtime Type

Generated instance encoding operates on the concrete object.

If the implementation goes through inherited class machinery, it must preserve the actual runtime object correctly.

However, generated methods belong to the concrete annotated class and should normally be compiled directly for that class.

---

# Static Deserialization

`FromJSON` and `FromYAML` are static methods because they create new instances.

Example:

```go
user, err := User.FromJSON(data)
```

This is preferred over requiring:

```go
user := User()
err := user.FromJSON(data)
```

Deserialization therefore becomes a class factory.

---

# Instance Serialization

Encoding uses instance methods:

```go
data, err := user.ToJSON()
```

because the operation serializes an existing object.

This gives a natural symmetry:

```text
instance → bytes
    user.ToJSON()

bytes → instance
    User.FromJSON(data)
```

---

# Package-Level Functions

`gpp/encoding` may also expose package functions internally or publicly:

```go
encoding.ToJSON(value)
encoding.FromJSON(data, target)
```

but the primary DX for `Serializable` classes should be the generated class API.

Package-level functions remain useful for:

* native Go values
* records
* generic infrastructure
* HTTP internals
* values that cannot receive generated methods

---

# Records

Records should be serializable through package functions without requiring an annotation.

Example:

```go
value := record(
    ok: true,
    count: 3,
)

data, err := encoding.ToJSON(value)
```

Because anonymous records do not have a user-declared class namespace, do not require generated methods such as:

```go
value.ToJSON()
```

for v1.

---

# Native Go Types

Native Go values may continue to use package-level encoding helpers or the native Go encoding packages.

Do not attempt to inject methods into arbitrary imported Go types as part of `Serializable`.

---

# HTTP Integration

`gpp/http` may use `gpp/encoding` for request/response binding.

Example route:

```go
func Create(
    user User @{http.Body},
) User @{
    http.POST("/users")
} {
    ...
}
```

Conceptually:

```text
request body
↓
User.FromJSON(...)
↓
route
↓
returned User
↓
user.ToJSON()
↓
HTTP response
```

The HTTP package should not duplicate serialization rules.

---

# Content Type

Initial mapping may include:

```text
application/json
    JSON

application/yaml
text/yaml
application/x-yaml
    YAML
```

Exact HTTP behavior belongs to `gpp/http`, not to the compiler.

---

# Encoding Errors

Generated methods should return underlying encoding errors directly unless additional context is useful.

Example:

```go
user, err := User.FromJSON(data)
```

invalid JSON should return the JSON decoder's error.

Do not panic for normal malformed input.

---

# Unsupported Fields

If a field type cannot be encoded by the selected backend, behavior should follow the underlying encoder where practical.

If the compiler can detect a definitely unsupported type statically, it may provide an earlier diagnostic.

Do not require an exhaustive compiler-side serialization type system for v1.

---

# Cycles

Cycle behavior should follow the underlying encoding backend.

For example, JSON may reject cyclic object graphs.

Do not build custom graph serialization solely for this feature.

---

# Visibility

By default, serialization should follow the language's public/private field behavior chosen for Go++ classes.

Preferred initial rule:

Serialize fields that are eligible according to generated Go representation and encoding metadata.

If Go++ private field semantics make native encoders unsuitable directly, generated transport structures/code may be required.

Do not expose private implementation fields accidentally.

---

# AST / Semantic Expansion

`Serializable` should be processed during semantic/code-generation phases.

Conceptually:

```text
parse class
↓
resolve annotations
↓
detect encoding.Serializable
↓
validate field annotations
↓
synthesize missing generated methods
↓
normal method/type checking
↓
lower to Go
```

Generated methods should enter the class semantic model so later code can resolve:

```go
User.FromJSON(...)
user.ToJSON()
```

normally.

---

# Generated Method Metadata

Generated methods should appear in class metadata just like normal methods unless there is a strong reason to hide them.

For example:

```go
User.class.methods
```

may include:

```text
ToJSON
ToYAML
FromJSON
FromYAML
```

with the static/instance distinction preserved.

This keeps introspection consistent.

---

# Annotation Expansion Is Library-Driven

The compiler needs a general mechanism for annotations that generate class members, rather than hard-coding:

```text
encoding.Serializable
```

throughout the parser.

However, if the current compiler does not yet support library-driven annotation macros/codegen, an initial compiler-recognized implementation may be used as a stepping stone.

Long-term preference:

```text
general annotation expansion mechanism
        ↓
gpp/encoding defines Serializable behavior
```

rather than permanent serializer-specific compiler logic.

---

# Initial API

Recommended v1:

```go
annotation (
    Serializable on class

    Name(name string) on field
    Ignore on field
    OmitEmpty on field
)
```

Generated for each `Serializable` class:

```go
func ToJSON() ([]byte, error)

static func FromJSON(data []byte) (T, error)

func ToYAML() ([]byte, error)

static func FromYAML(data []byte) (T, error)

func ToGOB() ([]byte, error)

static func FromGOB(data []byte) (T, error)
```

Nothing more is required initially.

---

# Example

```go
import "gpp/encoding"

class User @{
    encoding.Serializable
} {
    Id int @{
        encoding.Name("id")
    }

    Name string @{
        encoding.Name("name")
    }

    Email string @{
        encoding.Name("email"),
        encoding.OmitEmpty
    }

    Password string @{
        encoding.Ignore
    }
}

func main() {
    user := User(
        Id: 1,
        Name: "Ada",
        Email: "ada@example.com",
        Password: "secret",
    )

    data, err := user.ToJSON()
    if err != nil {
        panic(err)
    }

    copy, err := User.FromJSON(data)
    if err != nil {
        panic(err)
    }

    fmt.Println(copy.Name)
}
```

Expected serialized JSON:

```json
{
  "id": 1,
  "name": "Ada",
  "email": "ada@example.com"
}
```

`Password` is excluded.

---

# Tests

## Method generation

Given:

```go
class User @{encoding.Serializable} {
}
```

verify the class resolves:

```go
user.ToJSON()
user.ToYAML()

User.FromJSON(...)
User.FromYAML(...)
```

---

## Static method generation

Verify `FromJSON` / `FromYAML` are static and callable through the class.

They must not require an instance.

---

## Round trip

Verify:

```text
User
↓ JSON
bytes
↓ FromJSON
User
```

preserves supported fields.

Do the same for YAML.

---

## Ignore

Verify `encoding.Ignore` fields are neither encoded nor populated during decoding.

---

## Name

Verify renamed fields use the configured serialized name.

---

## OmitEmpty

Verify empty values are omitted where supported.

---

## User override

Define a custom:

```go
func ToJSON() ([]byte, error)
```

and verify it replaces generation for that method only.

---

## Conflict

Define an incompatible:

```go
func ToJSON() string
```

and verify compilation fails.

---

## Inherited fields

Verify an annotated derived class serializes inherited fields correctly.

---

## Runtime method metadata

Verify generated methods appear consistently in class method metadata if method introspection includes synthesized methods.

---

# Non-goals

Do not add in v1:

* XML
* MessagePack
* BSON
* Protobuf
* custom schema language
* versioned serialization
* graph identity preservation
* migration between serialized schema versions
* field-level custom encoder functions
* automatic database persistence
* arbitrary serialization callbacks
* implicit serialization for every class

These may be considered later.

---

# Design Principle

`encoding.Serializable` should turn this:

```go
class User @{encoding.Serializable} {
    Id int
    Name string
}
```

into a class that naturally supports:

```go
data, err := user.ToJSON()
user2, err := User.FromJSON(data)

yaml, err := user.ToYAML()
user3, err := User.FromYAML(yaml)

gob, err := user.ToGOB()
user4, err := User.FromGOB(gob)
```

The annotation provides explicit opt-in.

Static methods provide natural factory/deserialization syntax.

Instance methods provide natural serialization syntax.

The underlying implementation should still leverage the existing Go encoding ecosystem whenever practical.
