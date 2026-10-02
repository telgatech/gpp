# Explicit safe access

Safe access handles a nullable class pointer without a separate nil branch at
every field or method read. It is opt-in: ordinary `.` access keeps Go's
normal behavior.

## Compare an explicit nil check

When a nil receiver should produce the member's zero value, `?.` says that
directly:

::: code-group
```go [Go++]
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

Add `??` when nil should select a specific fallback instead:

```go
var user *User
name := user?.Name ?? "Unknown"
```

The fallback is used only when a safe receiver is nil. A present user's empty
name remains empty. Safe field accesses can be chained before `??`; if any safe
receiver in the chain is nil, the fallback is evaluated:

```go
managerName := user?.Manager?.Name ?? "No manager"
```

The fallback is lazy. Ordinary `user.Name` remains an ordinary Go selector and
a nil dereference still panics; `??` does not recover that panic.

Safe access starts from an identifier with a supported nullable class pointer
or generated class interface type. A chained safe field path can be used with
`??` as shown above. Safe access does not change ordinary dereference
semantics or infer that every access should be safe. See the [safe access
rules](/reference/specifications/base).
