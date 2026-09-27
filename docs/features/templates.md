# Typed templates

Go++ template declarations keep HTML templates near the application code that
uses them. They compile into typed execution functions and use Go's
`html/template` behavior for escaping and rendering.

## Compare string assembly with templates

Templates separate presentation from concatenation and escape dynamic HTML
values:

::: code-group

```go [Manual string assembly]
html := "<h1>" + html.EscapeString(title) + "</h1>"
```

```go [Go++ template]
template Page(title string) {
    <h1>{{.}}</h1>
}

tpl.Page(&output, title)
```

:::

## Compose a layout

Templates can inherit a layout through `: Layout`. The child body is supplied
to the layout's body slot:

```go
template Layout() {
    <!doctype html>
    <html><body><main>{{body}}</main></body></html>
}

template Welcome(name string): Layout {
    <h1>Hello, {{.}}</h1>
}

tpl.Welcome(&output, "Ada")
```

Use typed calls when the template is known at compile time. Dynamic execution
is also available when a name comes from routing or configuration. See
[template declarations](/reference/specifications/tpl) and the [template guide](/guide/standard-library#templates).
