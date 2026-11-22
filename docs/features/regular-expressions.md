# Regular expressions

GPP provides `regexp(pattern)` for direct Go `*regexp.Regexp` construction and
a `CompileRegex()` string extension for error-returning compilation. Both use
Go's `regexp` package. `CompileRegex()` errors propagate automatically when
they are not captured; `regexp(pattern)` follows `regexp.MustCompile` behavior
for invalid dynamic patterns.

## Go package calls and method-style calls

`"^[a-z]+$".CompileRegex()` calls `CompileRegex()` as an extension method on
`string`. The same syntax works on a string variable:

Both forms use the same Go regexp implementation:

::: code-group
```go [GPP]
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

## `regexp(pattern)` and switch cases

Use `regexp(pattern)` when you want a compiled matcher directly. It returns
Go's native `*regexp.Regexp`, so methods such as `MatchString` are available:

```go
pattern := `^/users/[0-9]+$`
path := "/users/42"
re := regexp(pattern)
fmt.Println(re.MatchString(path))
```

You can also use regular expressions in a string `switch`. Regex cases can be
mixed with exact string cases, and the switch value is evaluated once:

```go
switch path {
case "/", regexp(pattern):
    fmt.Println("home or user path")
case regexp(`^/health$`):
    fmt.Println("health check")
default:
    fmt.Println("no path matched")
}
```

Literal patterns in `case regexp(...)` are checked during compilation and
compiled once for reuse. Dynamic patterns are compiled when their case is
checked; an invalid dynamic pattern follows Go's `regexp.MustCompile` behavior
and panics.

The compiler injects the special variable `match` into each regex case body.
It is local to that case and is unavailable outside it. `match.Items` contains
the full match at index `0` followed by capture groups in pattern order; every
item is a string. An optional group that did not participate is an empty
string. Named groups in a literal pattern also become string fields on `match`:

```go
switch path {
case regexp(`^/users/(?P<userID>[0-9]+)$`), regexp(`^/members/(?P<userID>[0-9]+)$`):
    fmt.Println(match.userID)  // named capture
    fmt.Println(match.Items[1]) // same capture by index
case regexp(`^/files/([^/]+)/([^/]+)$`):
    fmt.Println(match.Items[1], match.Items[2]) // unnamed captures
}
```

Dynamic patterns expose captures through `match.Items`; their names are not
known at compile time. For comma-separated regex alternatives, `Items` comes
from the first matching pattern, and named fields are available only when all
alternatives define that name. A case that uses `match` must contain only regex
patterns; cases without capture access can still mix regex and exact string
alternatives. Each case has its own local `match` shape. See the [regex
specification](/reference/specifications/std.extensions.regex) for the case
scope and capture rules.

`match` is a compiler-provided variable for regex switch cases. Calling
`regexp(pattern)` elsewhere just returns Go's `*regexp.Regexp`; use its methods
such as `FindStringSubmatch` to retrieve captures there.

The extension does not change regexp matching rules or introduce a separate
pattern language. See [regex extensions](/reference/specifications/std.extensions.regex).
