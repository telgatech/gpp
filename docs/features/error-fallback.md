# Lazy error fallback

The `??` operator supplies a fallback only when the left side raises a GPP
error. Successful values, including nil, pass through unchanged. Its right
side is evaluated only after an error. It does not recover arbitrary Go
panics.

## Compare explicit error handling

For a simple default, `??` can replace a small error branch:

::: code-group
```go [GPP]
port := strconv.Atoi(rawPort) ?? 8080
```

```go [Go]
port, err := strconv.Atoi(rawPort)
if err != nil {
    port = 8080
}
```
:::

The operator is best when the fallback is a straightforward default. If the
caller needs to inspect or report the error, capture it explicitly or use
[`try/catch`](/features/exceptions).

## Compute an error fallback only when needed

The fallback expression is lazy, so an expensive or stateful alternative does
not run when the primary value exists:

```go
user := FindUser(id) ?? GuestUser()
```

If `FindUser` succeeds and returns a nil user with no error, `user` remains nil;
`??` does not treat a successful nil result as an error.

## Handle multiple return values

When a call returns values followed by an error, provide a matching fallback
value for each result. A successful call passes through all its values:

```go
width, height := Dimensions() ?? 0, 1
cachedWidth, cachedHeight := Dimensions() ?? CachedDimensions() ?? 0, 1
```

This concise form is intended for simple fallback paths. Use explicit
conditionals when the fallback requires several steps. See the [`??`
specification](/reference/specifications/expr-catch) for supported result types
and evaluation rules.
