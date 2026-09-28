# Regular expressions

The regex extension gives strings convenient compile and match operations
while keeping Go's `regexp` package as the engine. Pattern compilation remains
explicit, and invalid patterns propagate automatically when their error is
not captured.

## Go package calls and method-style calls

Both forms use the same Go regexp implementation:

::: code-group
```go [Go++]
re := "^[a-z]+$".CompileRegex()
matched := re.MatchString(username)
```

```go [Go]
re, err := regexp.Compile("^[a-z]+$")
if err != nil {
    return err
}
matched := re.MatchString(username)
```
:::

Dynamic patterns use the same extension. An invalid pattern automatically
propagates its error; no explicit error check is needed:

```go
pattern := config.UsernamePattern
re := pattern.CompileRegex()
if !re.MatchString(username) {
    return errors.New("username has an invalid format")
}
```

The extension does not change regexp matching rules or introduce a separate
pattern language. See [regex extensions](/reference/specifications/std.extensions.regex).
