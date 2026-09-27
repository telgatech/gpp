# JSON encoding

JSON is the usual choice for APIs, browser clients, and data exchanged across languages. The `gpp/encoding` package offers generic wrappers around Go's JSON encoder and decoder.

## Side by side: marshal and unmarshal

In Go, decoding requires declaring a destination and passing its address:

```go
var user User
if err := json.Unmarshal(data, &user); err != nil { return err }
```

In Go++, the generic helper returns the decoded value:

```go
user, err := encoding.FromJSON[User](data)
if err != nil { return err }
```

Encoding uses the corresponding helper:

```go
data, err := encoding.ToJSON(user)
if err != nil { return err }
fmt.Println(string(data))
```

## Records, lists, and boundary checks

Records work well for small response shapes that do not need a named class:

```go
payload, err := encoding.ToJSON(record(ok: true, count: len(users)))
if err != nil { return err }
```

Generic decoding works for slices as well as individual classes:

```go
users, err := encoding.FromJSON[[]User](payload)
if err != nil { return err }
for _, user := range users {
	fmt.Println(user.Name)
}
```

At an API boundary, decode first, then validate application-specific rules:

```go
request, err := encoding.FromJSON[CreateUserRequest](body)
if err != nil { return fmt.Errorf("decode request: %w", err) }
if request.Name == "" { return errors.New("name is required") }
```

The helpers return errors from the underlying JSON implementation. Check them at the boundary where malformed or unsupported data can enter the application. For custom JSON behavior, use Go's standard `encoding/json` interfaces as you normally would.

See the [serialization specification](/reference/specifications/serialization) for supported types and behavior.
