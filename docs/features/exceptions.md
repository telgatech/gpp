# Exception handling

Error handling has been one of the most discussed parts of Go throughout the
language's life. Developers often land in different camps: some value Go's
explicit error returns, while others prefer the familiar control flow of
exceptions. Go++ aims to resolve that tension by keeping traditional Go error
handling intact and adding exceptions as a modern, well-understood way to
handle errors in application code. Teams can use either style where it fits,
including in the same codebase.

Go++ exceptions work with Go's existing `error` values. There is no separate
exception base type, and Go APIs do not need to change. Structured error flow
is useful when repeated propagation makes the main path hard to read.

## Compare the two styles

Suppose an existing Go function loads a profile and returns its result with an
ordinary trailing error:

```go
func LoadProfile(path string) (Profile, error)
```

Both versions below call that same function and handle file errors separately
from other failures. The first checks the returned error explicitly. In the
second, the call inside `try` omits the trailing error result, so Go++ sends a
non-nil error to the matching `catch` clause.

::: code-group

```go [Traditional Go]
func ShowProfile(path string) {
    profile, err := LoadProfile(path)
    if err != nil {
        var pathErr *os.PathError
        if errors.As(err, &pathErr) {
            fmt.Printf("could not read %s: %v\n", pathErr.Path, err)
            return
        }

        fmt.Printf("could not load profile: %v\n", err)
        return
    }

    Render(profile)
}
```

```go [Go++ with exceptions]
func ShowProfile(path string) {
    try {
        profile := LoadProfile(path)
        Render(profile)
    } catch *os.PathError e {
        fmt.Printf("could not read %s: %v\n", e.Path, e)
    } catch e {
        fmt.Printf("could not load profile: %v\n", e)
    }
}
```

:::

The traditional form makes error propagation explicit at each call site. The
exception form keeps the successful path together and moves recovery into
handlers. Both styles use Go's `error` type and can call the same Go functions;
the choice belongs to the code that handles the failure.

## Promote trailing errors inside `try`

When a call inside a `try` omits a function's trailing `error` result, a
non-nil error enters the catch flow:

```go
try {
    data := os.ReadFile("settings.json")
    config := ParseConfig(data)
    Start(config)
} catch *os.PathError e {
    fmt.Println("could not read", e.Path)
} catch e {
    fmt.Println("configuration failed:", e)
}
```

Each operation can keep its normal Go signature. `try` groups the success path,
and catch clauses handle the failures where the caller has enough context to
respond.

## Keep explicit error values when useful

If a call captures its trailing error, it remains an ordinary Go error value:

```go
data, err := os.ReadFile("settings.json")
if err != nil {
    return err
}
```

This lets a package use conventional Go error handling wherever it is clearest
and structured catches where they improve readability.

## Throw, catch several types, and clean up

Use `throw` to propagate an error value intentionally. A catch can match one or
more types, or use a catch-all. `finally` runs when the protected flow finishes
or unwinds:

```go
try {
    if invalid {
        throw errors.New("invalid input")
    }
    Save()
} catch *os.PathError, *os.SyscallError e {
    log.Printf("filesystem error: %v", e)
} catch e {
    log.Printf("operation failed: %v", e)
} finally {
    releaseLock()
}
```

Keep explicit Go `if err != nil` handling at simple boundaries and use
`try/catch` when several operations share meaningful recovery or cleanup logic.
Read the [exception specification](/reference/specifications/exceptions) for
propagation and catch matching details.
