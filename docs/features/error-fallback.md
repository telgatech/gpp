# Lazy error and nil fallback

The `??` operator supplies a fallback when an operation returns an error or a
nullable value is absent. Its right side is evaluated only when the left side
needs a fallback, which keeps defaults close to the operation that needs them.

## Compare explicit error handling

For a simple default, `??` can replace a small error branch:

::: code-group

```go [Go explicit handling]
port, err := strconv.Atoi(rawPort)
if err != nil {
    port = 8080
}
```

```go [Go++ fallback]
port := strconv.Atoi(rawPort) ?? 8080
```

:::

The operator is best when the fallback is a straightforward default. If the
caller needs to inspect or report the error, capture it explicitly or use
[`try/catch`](/features/exceptions).

## Compute a nullable fallback only when needed

The fallback expression is lazy, so an expensive or stateful alternative does
not run when the primary value exists:

```go
user := FindUser(id) ?? GuestUser()
```

This concise form is intended for simple fallback paths. Use explicit
conditionals when the fallback requires several steps. See the [`??`
specification](/reference/specifications/expr-catch) for supported result types
and evaluation rules.
