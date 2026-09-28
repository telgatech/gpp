# `gpp/encoding`

`gpp/encoding` provides generic helpers for JSON, YAML, and GOB, including for records and other values without a named class. A class marked `encoding.Serializable` also gets instance encoders such as `user.ToJSON()` and typed class decoders such as `User.FromJSON(data)`.

## Why one package?

Applications commonly exchange JSON and YAML with people and services, and may use GOB for compact Go-to-Go communication. A consistent generic API avoids repeating encoder setup while leaving the format choice explicit at each call site.

## Side by side: serialize a value

The `Serializable` annotation generates an instance encoder and a typed class decoder. Unhandled errors propagate automatically, so the successful path stays compact. Select the Go tab to compare with the standard library:

::: code-group

```go [Go++]
class User @{encoding.Serializable} {
	ID int `json:"id"`
	Name string `json:"name"`
}

func RoundTrip(user User) User {
	data := user.ToJSON()
	return User.FromJSON(data)
}
```

```go [Go]
data, err := json.Marshal(user)
if err != nil { return err }
var decoded User
if err := json.Unmarshal(data, &decoded); err != nil { return err }
```

:::

Choose a format based on the boundary: JSON for broad interoperability, YAML for human-edited configuration, and GOB for Go-oriented binary exchange. Each format has a separate guide: [JSON](/guide/standard-library/encoding/json), [YAML](/guide/standard-library/encoding/yaml), and [GOB](/guide/standard-library/encoding/gob).

## Further reading

- [Serialization specification](/reference/specifications/serialization)
- [GOB specification](/reference/specifications/serialization.gob)
