# String interpolation

Interpolation places an expression inside a string without building a format
argument list by hand. It is useful for messages, labels, and log text that
combine a few values.

## Compare formatting styles

Go's `fmt.Sprintf` remains a clear choice for complex formatting. Interpolation
keeps simple expressions close to the surrounding text:

::: v-pre

**Go formatting**

```go
message := fmt.Sprintf("Hello %s, order %d", user.Name, order.ID)
```

**Go++ interpolation**

```go
message := "Hello {{user.Name}}, order {{order.ID}}"
```

Expressions between `{{` and `}}` are Go++ expressions. The compiler lowers
the interpolation to formatting code, so it does not change the type or
behavior of the values being interpolated. Interpolation works in both quoted
strings and raw backtick strings.

## Format a value

Formatting verbs can be included when a value needs a specific presentation:

```go
price := "Total: {{amount:.2f}}"
summary := "{{item.Name}} x {{quantity}}"
multiline := `Hello, {{user.Name}}!
Your order {{order.ID}} is ready.`
```

For complex alignment or custom formatting, build the string with the normal
Go formatting packages. Read the [interpolation specification](/reference/specifications/string-interpolation)
for expression and format rules.

:::
