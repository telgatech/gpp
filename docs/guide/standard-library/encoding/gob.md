# GOB encoding

GOB is Go's binary encoding format for exchanging Go values. It is a good fit for trusted Go-to-Go data flows where compact binary data is useful; it is not intended as a human-readable or cross-language interchange format.

## Side by side: encode and decode

Using Go's `encoding/gob` directly requires creating an encoder or decoder around an I/O stream. Go++ can declare the message as a serializable class and infer the decoded type; unhandled errors propagate automatically:

::: code-group

```go [Go++]
class Message @{encoding.Serializable} {
	ID int
	Text string
}

func RoundTrip(message Message) Message {
	data := message.ToGOB()
	return Message.FromGOB(data)
}
```

```go [Go]
var data bytes.Buffer
if err := gob.NewEncoder(&data).Encode(message); err != nil {
	return err
}

var decoded Message
if err := gob.NewDecoder(&data).Decode(&decoded); err != nil {
	return err
}
```

:::

When you already have a destination value, decode into it directly:

```go
var decoded Message
encoding.DecodeGOB(data, &decoded)
```

## Use GOB for internal snapshots

The same helpers work for slices and nested values, provided both ends agree on compatible Go types:

```go
snapshot := []Message{first, second}
data := encoding.ToGOB(snapshot)

restored := encoding.FromGOB[[]Message](data)
fmt.Println("restored", len(restored), "messages")
```

For a stream or an existing destination, use the standard `encoding/gob` package directly or call `DecodeGOB` with the destination pointer. The helper API is aimed at convenient in-memory byte slices.

The GOB helpers preserve encoder and decoder errors, which Go++ propagates automatically unless you capture them. Prefer JSON or YAML when data needs to be inspected manually or consumed by non-Go systems.

See the [GOB specification](/reference/specifications/serialization.gob) for supported values and details.
