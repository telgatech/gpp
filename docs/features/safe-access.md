# Explicit safe access

Safe access handles a nullable class pointer without a separate nil branch at
every field or method read. It is opt-in: ordinary `.` access keeps Go's
normal behavior.

## Compare an explicit nil check

When a nil receiver should produce the member's zero value, `?.` says that
directly:

::: code-group

```go [Go nil check]
var name string
if user != nil {
    name = user.Name
}
```

```go [Go++ safe access]
var user *User
name := user?.Name
```

:::

The safe expression returns the field's zero value when the receiver is nil.
For a method call, it returns the method result's zero value:

```go
var user *User
name := user?.Name
greeting := user?.Greeting()
```

Safe access is limited to supported nullable class pointers and generated
class interfaces. It does not change ordinary dereference semantics or infer
that every access should be safe. See the [safe access rules](/reference/specifications/base).
