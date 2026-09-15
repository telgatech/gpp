# GOB Extension

Add GOB support to `encoding.Serializable` alongside JSON and YAML.

## Generated Methods

For every class annotated with:

```go
class User @{encoding.Serializable} {
}
```

also generate:

```go
func ToGOB() ([]byte, error)

static func FromGOB(data []byte) (User, error)
```

For a generic class `T`, the conceptual generated surface is:

```go
func ToGOB() ([]byte, error)

static func FromGOB(data []byte) (T, error)
```

## Usage

```go
data, err := user.ToGOB()
if err != nil {
    return err
}

copy, err := User.FromGOB(data)
if err != nil {
    return err
}
```

The intended symmetry is:

```text
instance → binary bytes
    user.ToGOB()

binary bytes → instance
    User.FromGOB(data)
```

## Backend

Use Go's standard library:

```go
encoding/gob
bytes
```

No external dependency is required.

Conceptually, instance encoding behaves like:

```go
func ToGOB() ([]byte, error) {
    var buffer bytes.Buffer

    err := gob.NewEncoder(&buffer).Encode(this)
    if err != nil {
        return nil, err
    }

    return buffer.Bytes(), nil
}
```

Exact generated code may differ because of Go++ class lowering.

## Static Deserialization

Conceptually:

```go
static func FromGOB(data []byte) (User, error) {
    value := User()

    err := gob.NewDecoder(
        bytes.NewReader(data),
    ).Decode(&value)

    return value, err
}
```

Return the underlying decoder error unchanged where practical.

Malformed or incompatible data must return an error rather than panic.

## Serializable Generated Surface

With GOB included, the v1 generated API becomes:

```go
func ToJSON() ([]byte, error)
static func FromJSON(data []byte) (T, error)

func ToYAML() ([]byte, error)
static func FromYAML(data []byte) (T, error)

func ToGOB() ([]byte, error)
static func FromGOB(data []byte) (T, error)
```

Example:

```go
class User @{encoding.Serializable} {
    Id int
    Name string
}
```

supports:

```go
jsonData, err := user.ToJSON()
fromJSON, err := User.FromJSON(jsonData)

yamlData, err := user.ToYAML()
fromYAML, err := User.FromYAML(yamlData)

gobData, err := user.ToGOB()
fromGOB, err := User.FromGOB(gobData)
```

## Purpose

GOB should be treated differently from JSON and YAML.

JSON and YAML are primarily interchange formats.

GOB is primarily suitable for:

* Go/Go++ internal communication
* local persistence
* caches
* queues
* snapshots
* trusted internal binary transport

Do not present GOB as a stable cross-language interchange format.

## Field Behavior

`encoding.Ignore` should apply to GOB as well if the generated representation can enforce it consistently.

Example:

```go
class User @{encoding.Serializable} {
    Id int
    Name string

    Password string @{
        encoding.Ignore
    }
}
```

`Password` should not appear in:

```text
JSON
YAML
GOB
```

Similarly:

```go
encoding.Name(...)
```

is primarily a textual-format naming concern.

It does not need to affect GOB's internal field representation unless the implementation uses a generated transport struct shared by all formats.

`encoding.OmitEmpty` may be meaningful for JSON/YAML but should not be forced onto GOB if the underlying GOB representation does not naturally support equivalent semantics.

The package should document format-specific applicability of field annotations.

## Preferred Implementation Strategy

If the compiler already generates an encoding transport representation for a class, GOB may encode that same transport representation.

For example:

```text
Go++ class
    ↓
generated serializable transport representation
    ↓
JSON / YAML / GOB
```

This has the advantage that:

```text
Ignore
visibility rules
inherited field flattening
```

can behave consistently across formats.

If direct GOB encoding of the generated class representation is already correct and stable enough, a separate transport representation is not mandatory.

## Inherited Fields

For:

```go
class Person {
    Name string
}

class Employee : Person @{encoding.Serializable} {
    Id int
}
```

GOB encoding of `Employee` must include serializable inherited fields such as:

```text
Name
Id
```

according to the same class-field visibility rules used by JSON/YAML serialization.

## Nested Serializable Classes

Nested values should encode naturally.

Example:

```go
class Address @{encoding.Serializable} {
    City string
}

class User @{encoding.Serializable} {
    Name string
    Address Address
}
```

Then:

```go
data, err := user.ToGOB()
copy, err := User.FromGOB(data)
```

should round-trip the nested `Address`.

## Collections

Fields containing supported Go collection types should rely on normal `encoding/gob` behavior where possible:

```go
[]T
map[K]V
arrays
pointers
primitive values
```

Do not create a separate collection serialization system solely for GOB.

## Interface Fields

Go's `encoding/gob` may require concrete types stored behind interfaces to be registered.

If Go++ classes stored in interface-typed fields require registration, `gpp/encoding` may generate or perform the necessary registration.

For example, implementation may use:

```go
gob.Register(...)
```

for concrete serializable class representations where required.

Do not expose unnecessary registration boilerplate to normal users if the compiler has sufficient type information to generate it.

## Compatibility

GOB data should not be treated as permanently stable application storage by default.

Changes to class shape may affect compatibility.

The initial implementation does not guarantee:

* long-term schema evolution
* compatibility across arbitrary class changes
* compatibility with non-Go implementations
* canonical byte output

A future versioning/schema feature may address those separately.

## User Overrides

As with JSON and YAML, a user may explicitly provide:

```go
func ToGOB() ([]byte, error)
```

or:

```go
static func FromGOB(data []byte) (User, error)
```

When a compatible method is explicitly supplied, do not generate that method.

Generate the remaining missing serialization methods normally.

Example:

```go
class User @{encoding.Serializable} {
    func ToGOB() ([]byte, error) {
        return customGobEncode(this)
    }
}
```

Still generate:

```text
FromGOB
ToJSON
FromJSON
ToYAML
FromYAML
```

unless separately overridden.

## Incompatible Conflict

If the class defines an incompatible method:

```go
func ToGOB() string
```

report a compile-time error.

Suggested diagnostic:

```text
encoding.Serializable requires User.ToGOB() ([]byte, error),
but User defines an incompatible ToGOB method
```

Likewise for `FromGOB`.

## Introspection

Generated ToGOB methods should appear in class method metadata under the same rules as other generated serialization methods.

For example:

```go
User.class.methods
```

may expose:

```text
JSON
FromJSON
YAML
FromYAML
GOB
FromGOB
```

with static/instance metadata preserved.

## Records

Records may support GOB through the package-level API:

```go
value := record(
    id: 1,
    name: "Ada",
)

data, err := encoding.ToGOB(value)
```

Do not require instance methods on anonymous records in v1.

Package-level decoding of an anonymous record shape may be supported only where the target shape is statically known.

## HTTP

Do not automatically use GOB for ordinary HTTP APIs merely because a class is serializable.

JSON should remain the normal web default.

GOB may be used only when explicitly selected through an appropriate content type or application configuration.

Potential content type:

```text
application/gob
```

Exact HTTP negotiation belongs to `gpp/http`.

## Tests

### Round Trip

Verify:

```text
User
↓ GOB
[]byte
↓ FromGOB
User
```

preserves all supported fields.

### Nested Values

Verify nested serializable classes round-trip correctly.

### Collections

Verify slices/maps of supported values round-trip.

### Ignore

Verify `encoding.Ignore` fields are excluded when the chosen generated representation supports common filtering across formats.

### Error Handling

Invalid GOB input must return an error.

### Static Generation

Verify:

```go
User.FromGOB(data)
```

is generated as a static method.

### Instance Generation

Verify:

```go
user.ToGOB()
```

is generated as an instance method.

### Override

Verify explicit compatible `ToGOB` / `FromGOB` methods suppress only the corresponding generated method.

## Non-goals

Do not add as part of initial GOB support:

* schema migration
* canonical binary encoding
* cross-language compatibility
* custom wire protocol
* streaming protocol abstraction
* encryption
* compression
* version negotiation
* automatic persistent object storage

These are separate concerns.

## Design Principle

GOB support should feel identical to the other serialization formats from the user's perspective:

```go
jsonData, _ := user.ToJSON()
yamlData, _ := user.ToYAML()
gobData, _ := user.ToGOB()

a, _ := User.FromJSON(jsonData)
b, _ := User.FromYAML(yamlData)
c, _ := User.FromGOB(gobData)
```

The implementation should reuse Go's existing `encoding/gob` package rather than inventing a new binary serialization format.
