# Function and method overloading

Overloading lets related operations share a name. Go++ chooses the applicable
function or method during compilation, so a call remains an ordinary direct
call in the generated Go code.

## Compare naming conventions

In Go, related operations with different input types usually need separate
names or a type switch. Go++ can keep one name and select from static types:

::: code-group

```go [Go]
func FormatInt(value int) string { return strconv.Itoa(value) }
func FormatString(value string) string { return value }
```

```go [Go++]
func Format(value int) string { return strconv.Itoa(value) }
func Format(value string) string { return value }

Format(42)
Format("42")
```

:::

## Select by static type

Overloads can have the same number of parameters when their parameter types
differ:

```go
func Describe(value int) string {
    return "integer"
}

func Describe(value string) string {
    return "text"
}

fmt.Println(Describe(42))
fmt.Println(Describe("42"))
```

The compiler uses the argument's static type. It does not inspect a value at
runtime or convert the overload into a reflection-based dispatcher.

## Select by arity

Overloads may also offer different parameter counts for the same operation:

```go
func Join(left string, right string) string {
    return left + right
}

func Join(left string, middle string, right string) string {
    return left + middle + right
}

pair := Join("go", "++")
triple := Join("go", "+", "+")
```

Methods can use the same overload rules, keeping a type's related operations
together:

```go
class Formatter {
    func Format(value int) string { return strconv.Itoa(value) }
    func Format(value string) string { return value }
}
```

## Clear call sites

Named arguments and defaults complement overloading when a function has
optional configuration. Prefer a single function with defaults when the
operation is the same and only an option changes. Prefer overloads when
different input types need different behavior. Calls that remain ambiguous
produce a compile-time diagnostic.

See [function and call syntax](/guide/language#functions-and-calls) and the
[base language specification](/reference/specifications/base) for resolution
details.
