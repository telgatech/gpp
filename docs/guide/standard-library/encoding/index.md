# `gpp/encoding`

`gpp/encoding` provides generic helpers for JSON, YAML, and GOB. It works with Go++ classes, records, and other values supported by the underlying Go encoders.

## Why one package?

Applications commonly exchange JSON and YAML with people and services, and may use GOB for compact Go-to-Go communication. A consistent generic API avoids repeating encoder setup while leaving the format choice explicit at each call site.

## Side by side: serialize a value

With Go's standard library, the JSON operation is commonly written as:

```go
data, err := json.Marshal(user)
if err != nil { return err }
var decoded User
if err := json.Unmarshal(data, &decoded); err != nil { return err }
```

Go++'s encoding helpers make the target type explicit through a type argument:

```go
data, err := encoding.ToJSON(user)
if err != nil { return err }
decoded, err := encoding.FromJSON[User](data)
```

Choose a format based on the boundary: JSON for broad interoperability, YAML for human-edited configuration, and GOB for Go-oriented binary exchange. Each format has a separate guide: [JSON](/guide/standard-library/encoding/json), [YAML](/guide/standard-library/encoding/yaml), and [GOB](/guide/standard-library/encoding/gob).

## Further reading

- [Serialization specification](/reference/specifications/serialization)
- [GOB specification](/reference/specifications/serialization.gob)
