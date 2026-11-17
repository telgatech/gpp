# Explicit safe access

Safe access handles a nullable class pointer without a separate nil branch at
every field or method read. It is opt-in: ordinary `.` access keeps Go's
normal behavior.

## Compare an explicit nil check

When a nil receiver should produce the member's zero value, `?.` says that
directly:

::: code-group
```go [GPP]
var user *User
name := user?.Name
```

```go [Go]
var name string
if user != nil {
    name = user.Name
}
```
:::

The safe expression returns the field's zero value when the receiver is nil.
For a method call, it returns the method result's zero value:

```go
var user *User
name := user?.Name
greeting := user?.Greeting()
```

`??` handles errors from an operation; it does not use a fallback for a nil
receiver. A nil safe receiver produces the member's zero value. Use `||=` to
assign a default when a variable is nil, or use an explicit nil check when an
expression needs a different value.

Safe access starts from an identifier with a supported nullable class pointer
or generated class interface type. Safe access does not change ordinary dereference
semantics or infer that every access should be safe. See the [safe access
rules](/reference/specifications/base).
