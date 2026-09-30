# Program exit hook

Define `atExit()` once in the `main` package to run process-wide cleanup when
the program's `main` function finishes. The compiler arranges the call, so
resource cleanup does not need to be collected as `defer` statements in `main`.

## Compare cleanup placement

In Go, cleanup is commonly deferred from `main`:

::: code-group
```go [Go]
func main() {
    openDatabase()
    defer closeDatabase()
    startWorker()
    defer stopWorker()
    serve()
}
```

```go [Go++]
func atExit() {
    stopWorker()
    closeDatabase()
}

func main() {
    openDatabase()
    startWorker()
    serve()
}
```
:::

The Go++ version puts process-wide cleanup in one function. `atExit` must be a
top-level function in package `main` with no parameters or return values. It
runs once when `main` returns, including after a handled exception, and while
a panic unwinds. The compiler registers it before `main` begins, so defers
declared inside `main` run first and `atExit` runs afterward.

## `os.Exit` skips `atExit`

`os.Exit(code)` terminates the process immediately. It does **not** run deferred
functions, including the compiler-inserted `atExit()` call. Use ordinary
returns or a graceful shutdown path when cleanup must run. A forced kill or
fatal runtime termination also cannot be handled by `atExit`.

See the [program exit hook specification](/reference/specifications/at-exit)
for the exact rules.
