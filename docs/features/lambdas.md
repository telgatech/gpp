# Lambda expressions

Lambdas are short function values whose parameter types can come from the
place they are used. They make collection transformations and callbacks more
direct while still compiling to ordinary Go function literals.

## Compare a named callback with a lambda

For a one-use predicate, a lambda can keep the operation close to the call:

::: code-group
```go [Go++]
activeUsers := users.Filter(user => user.Active)
```

```go [Go]
activeUsers := Filter(users, func(user User) bool {
    return user.Active
})
```
:::

The surrounding method supplies the context needed to infer the lambda
parameter type. A block lambda is available when the body needs more than one
expression:

```go
names := users.Map(user => {
    label := user.Name.TrimSpace()
    return label
})
```

Use a named function when behavior is reused or deserves a name. See the
[lambda specification](/reference/specifications/lambdas) for inference and
block rules.
