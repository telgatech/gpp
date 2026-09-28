# Serialization

Go++ can generate serialization helpers for classes that opt in. This makes
common encoding and decoding calls convenient while keeping Go's familiar
JSON, YAML, and GOB formats underneath.

## Compare manual encoding with generated helpers

Go's encoders remain available for any value. The annotation adds convenient,
typed methods to the classes that opt in:

::: code-group
```go [Go++]
data := user.ToJSON()
restored := User.FromJSON(data)
```

```go [Go]
data, err := json.Marshal(user)
if err != nil {
    return err
}

var restored User
if err := json.Unmarshal(data, &restored); err != nil {
    return err
}
```
:::

## Opt a class in

Import `gpp/encoding` and annotate only the classes that should expose generated
helpers:

```go
import "gpp/encoding"

class User @{encoding.Serializable} {
    Id int
    Name string
    Email string
}
```

The annotation adds instance encoders and typed static decoders for supported
formats:

```go
user := User(Id: 7, Name: "Ada", Email: "ada@example.com")

jsonData := user.ToJSON()
restored := User.FromJSON(jsonData)
fmt.Println(restored.Name)
```

The generated API also provides `ToYAML` and `FromYAML`, plus `ToGOB` and
`FromGOB`. Choose the format based on the boundary: JSON is a common choice for
HTTP and external APIs, YAML is useful for configuration, and GOB is a Go
binary format.

## Control the encoded shape

Use field annotations to rename or omit fields according to the format's
supported options. This lets a Go++ model keep domain-oriented field names
while presenting a stable wire shape. Keep secrets and internal-only state
out of serialized data explicitly, and treat changes to public field names as
API changes for consumers of the encoded form.

## Interoperate with Go encoders

The generated methods use Go's established encoder packages. Their underlying
Go methods return the usual `([]byte, error)` results; Go++ propagates an
uncaptured error automatically. You can continue to use standard Go encoding
functions directly when you need lower-level control. Serialization is opt-in;
classes without the annotation do not gain generated methods.

See the [serialization guide](/guide/standard-library#serialization),
[Serializable specification](/reference/specifications/serialization), and
[GOB specification](/reference/specifications/serialization.gob) for field
rules and generated method details.
