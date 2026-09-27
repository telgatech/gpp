# Named arguments and defaults

Named calls put meaning beside values. Default parameters keep common calls
short while allowing callers to override options explicitly.

## Compare positional and named calls

Go functions normally require arguments in declaration order. Go++ can name
arguments, so a call with several similar values is easier to review:

::: code-group

```go [Go positional call]
func Resize(width int, height int) {}

Resize(640, 480)
```

```go [Go++ named call]
func Resize(width int, height int) {}

Resize(height: 480, width: 640)
```

:::

The compiler checks names and types. Named arguments can be supplied in a
different order, but a call cannot mix named and positional arguments.

## Set useful defaults

Default values belong on trailing parameters and are applied at the call site:

```go
func Greet(name string, punctuation string = "!") string {
    return name + punctuation
}

friendly := Greet(name: "Ada")
excited := Greet(name: "Ada", punctuation: "!")
quiet := Greet(name: "Ada", punctuation: ".")
```

This is useful when one or two options are common and the rest are occasional.
Use [overloading](/features/overloading) when input types represent genuinely
different operations. More details are in the [call syntax specification](/reference/specifications/constructors).
