# JSON encoding

JSON is the usual choice for APIs, browser clients, and data exchanged across languages. The `gpp/encoding` package offers generic wrappers around Go's JSON encoder and decoder.

## Side by side: marshal and unmarshal

In Go, decoding requires declaring a destination and passing its address. Go++ can use a serializable class as the schema and infer the result from the generic type argument. Encoding errors propagate automatically when left uncaptured:

::: code-group

```go [Go++]
class User @{encoding.Serializable} {
	ID int `json:"id"`
	Name string `json:"name"`
}

func DecodeUser(data []byte) User {
	user := User.FromJSON(data)
	encoded := user.ToJSON()
	fmt.Println(string(encoded))
	return user
}
```

```go [Go]
var user User
if err := json.Unmarshal(data, &user); err != nil {
	return err
}

encoded, err := json.Marshal(user)
if err != nil {
	return err
}
fmt.Println(string(encoded))
```

:::

## Records, lists, and boundary checks

Records work well for small response shapes that do not need a named class:

```go
payload := encoding.ToJSON(record(ok: true, count: len(users)))
```

Generic decoding works for slices as well as individual classes:

```go
users := encoding.FromJSON[[]User](payload)
for _, user := range users {
	fmt.Println(user.Name)
}
```

At an API boundary, decode first, then validate application-specific rules:

```go
class CreateUserRequest @{encoding.Serializable} {
	Name string `json:"name"`
}

request := CreateUserRequest.FromJSON(body)
if request.Name == "" { return errors.New("name is required") }
```

The helpers return errors from the underlying JSON implementation, and Go++ propagates uncaptured errors automatically. Use explicit handling when a malformed payload needs a custom response. For custom JSON behavior, use Go's standard `encoding/json` interfaces as you normally would.

See the [serialization specification](/reference/specifications/serialization) for supported types and behavior.
