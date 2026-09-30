# Program Exit Hook

Go++ supports an optional package-level `atExit` function in the `main` package. When present, the compiler arranges for it to run automatically as the program's generated `main` function exits.

## Declaration

Declare exactly one top-level function with no parameters and no return values:

```go
func atExit() {
    flushLogs()
    closeResources()
}
```

The function may appear in any source file in the `main` package. Other packages may declare a function with the same name; it is an ordinary function there and is not invoked automatically.

## Invocation

The generated entry point defers the call before it begins executing the user's `main` body:

```go
func main() {
    defer atExit()
    // user's main body
}
```

This means `atExit` runs once when `main` returns, including when it returns after handling an exception. It also runs while a panic unwinds through `main`. Since the hook's defer is registered before any defers in the user's body, those body defers run first and `atExit` runs afterward.

Use `atExit` for process-wide cleanup that should not clutter `main`, such as stopping background workers or closing shared resources. Hooks that need a result or error must report it through their own logging or state; `atExit` has no return values.

## Limits

`atExit` follows Go's deferred-call behavior. `os.Exit(code)` terminates the process immediately and does not run deferred functions, including `atExit`. A fatal runtime termination or external forced kill also prevents the hook from running. It is not a substitute for handling operating-system signals or for graceful shutdown protocols that require a context or deadline.

The compiler rejects an `atExit` declaration in package `main` if it has parameters or results.
