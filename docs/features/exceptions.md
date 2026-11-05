# Exception handling

Error handling has been one of the most debated parts of Go throughout the
language's life. Developers often land in different camps: some value Go's
explicit error returns, while others prefer the familiar control flow of
exceptions. Go++ aims to resolve that tension by keeping traditional Go error
handling intact and adding exceptions as a modern, well-understood way to
handle errors in application code. Teams can use either style where it fits,
including in the same codebase.

Go++ adopts the path of least resistence by simply treating unhandled errors and turning them into propogating exceptions automatically using nothing more than Go's existing `error` values. There is no separate
exception base type so to speak, and Go APIs do not need to change. Structured error flow
is useful when repeated propagation makes the main path hard to read.

## How exception handling works: a guided example

An exception is an error that interrupts the usual line-by-line flow and
jumps to a matching handler. Go++ uses the same Go `error` values you already
know: when a call leaves its trailing error result uncaptured, a non-nil error
propagates automatically. An enclosing `try` can catch it and recover.

First, a function can keep an ordinary Go signature and return an error in the
usual way:

```go
func ReadGreeting(path string) (string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return "", err
    }
    return string(data), nil
}
```

Now call it inside a `try` block:

```go
func ShowGreeting(path string) {
    try {
        greeting := ReadGreeting(path)
        fmt.Println(greeting)
    } catch os.PathError e {
        fmt.Printf("Could not read %s: %v\n", e.Path, e)
    } catch e {
        fmt.Println("Could not show greeting:", e)
    } finally {
        fmt.Println("Finished reading attempt")
    }
}
```

Read it in order:

1. If the file opens, `ReadGreeting` returns its text and `nil`; execution
   continues to `fmt.Println(greeting)`.
2. If file reading fails, Go++ sees the omitted trailing `error` and transfers
   control out of the rest of the `try` block.
3. The first compatible `catch` handles the failure. `catch os.PathError` matches
   the path error whether represented as `os.PathError` or `*os.PathError`; any
   other Go++-thrown error reaches the catch-all `catch e`.
4. `finally` runs after the `try` flow, whether it succeeded or an error was
   handled. It is a good place for cleanup that must happen either way.

If you instead wrote `text, err := ReadGreeting(path)`, the error would remain
an ordinary value and no exception transfer would happen. That is the same
explicit error-handling style Go has always used; Go++ lets the caller choose
which style makes the surrounding code clearer.

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
```go [Go++]
func ShowProfile(path string) {
    try {
        profile := LoadProfile(path)
        Render(profile)
    } catch os.PathError e {
        fmt.Printf("could not read %s: %v\n", e.Path, e)
    } catch e {
        fmt.Printf("could not load profile: %v\n", e)
    }
}
```

```go [Go]
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
:::

The traditional form makes error propagation explicit at each call site. The
exception form keeps the successful path together and moves recovery into
handlers. Both styles use Go's `error` type and can call the same Go functions;
the choice belongs to the code that handles the failure.

## Automatic trailing-error promotion

Go++ automatically raises an uncaptured trailing `error` as an exception. If
the current function does not handle it, the exception propagates to its caller.
Add `try`/`catch` where you want to handle it.

```go
try {
    data := os.ReadFile("settings.json")
    config := ParseConfig(data)
    Start(config)
} catch os.PathError e {
    fmt.Println("could not read", e.Path)
} catch e {
    fmt.Println("configuration failed:", e)
}
```

Each operation keeps its normal Go signature. The following functions let the
exception propagate through both calls before `main` catches it:

```go
func ReadGreeting(path string) string {
    // This call raises *os.PathError if the file can't be opened.
    data := os.ReadFile(path)
    return string(data)
}

func ShowGreeting(path string) {
    fmt.Println(ReadGreeting(path))
}

func main() {
    try {
        ShowGreeting("settings.json")
    } catch *os.PathError e {
        fmt.Println("could not read", e.Path, ":", e)
    } catch e {
        fmt.Println("could not show greeting:", e)
    }
}
```

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

## Catch a sentinel error directly

When a handler knows how to respond to a specific sentinel, catch that value
without adding a catch-all, rethrow branch, or separate `errors.Is` check:

```go
try {
    todo := Todo.Find(id)
    return ctx.JSON(todo)
} catch sql.ErrNoRows {
    return ctx.JSON(404, record(Error: "todo not found"))
}
```

Go++ matches the value with `errors.Is`, so wrapped sentinel errors still
match. Other errors remain unhandled and propagate normally.

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
} catch os.PathError, os.SyscallError e {
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

A named typed catch matches both the value and pointer forms of that error type
when each form implements `error`. The caught value keeps its original form,
so the handler must compile for every form it can receive. Writing `*Foo` in a
catch does not narrow it to pointers.
