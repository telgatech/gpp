# GOB encoding

GOB is Go's binary encoding format for exchanging Go values. It is a good fit for trusted Go-to-Go data flows where compact binary data is useful; it is not intended as a human-readable or cross-language interchange format.

## Side by side: encode and decode

Using Go's `encoding/gob` directly requires creating an encoder or decoder around an I/O stream. The Go++ helpers handle the in-memory buffer and let the destination type be explicit:

```go
data, err := encoding.ToGOB(message)
if err != nil { return err }

decoded, err := encoding.FromGOB[Message](data)
if err != nil { return err }
```

When you already have a destination value, decode into it directly:

```go
var decoded Message
if err := encoding.DecodeGOB(data, &decoded); err != nil {
	return err
}
```

## Use GOB for internal snapshots

The same helpers work for slices and nested values, provided both ends agree on compatible Go types:

```go
snapshot := []Message{first, second}
data, err := encoding.ToGOB(snapshot)
if err != nil { return err }

restored, err := encoding.FromGOB[[]Message](data)
if err != nil { return err }
fmt.Println("restored", len(restored), "messages")
```

For a stream or an existing destination, use the standard `encoding/gob` package directly or call `DecodeGOB` with the destination pointer. The helper API is aimed at convenient in-memory byte slices.

The GOB helpers return encoder and decoder errors unchanged. Prefer JSON or YAML when data needs to be inspected manually or consumed by non-Go systems.

See the [GOB specification](/reference/specifications/serialization.gob) for supported values and details.
