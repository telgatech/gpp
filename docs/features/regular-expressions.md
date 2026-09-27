# Regular expressions

The regex extension gives strings convenient compile and match operations
while keeping Go's `regexp` package as the engine. Pattern compilation remains
explicit, so invalid patterns continue to produce normal errors.

## Go package calls and method-style calls

Both forms use the same Go regexp implementation:

::: code-group

```go [Go regexp package]
re, err := regexp.Compile("^[a-z]+$")
if err != nil {
    return err
}
matched := re.MatchString(username)
```

```go [Go++ regex extension]
re := "^[a-z]+$".CompileRegex()
matched := re.MatchString(username)
```

:::

If the pattern is dynamic, handle the returned error as usual:

```go
pattern := config.UsernamePattern
re, err := pattern.CompileRegex()
if err != nil {
    return fmt.Errorf("invalid username pattern: %w", err)
}
if !re.MatchString(username) {
    return errors.New("username has an invalid format")
}
```

The extension does not change regexp matching rules or introduce a separate
pattern language. See [regex extensions](/reference/specifications/std.extensions.regex).
