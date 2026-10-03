# Typed templates

Go++ template declarations keep HTML templates near the application code that
uses them. They compile into typed execution functions and use Go's
`html/template` behavior for escaping and rendering.

## Declare intent with the `template` keyword

`template` is a recognized Go++ keyword for a package-level declaration. The
function-like signature states which values the page expects, and a generated
renderer makes that template available through `tpl`:

```go
template Welcome(name string) {
    <h1>Hello, {{.}}</h1>
}

tpl.Welcome(&output, "Ada")
```

This makes the boundary between application data and presentation visible in
the source. Go++ checks direct calls to the generated renderer against the
declared argument count and types. It also checks template field and method
accesses when their types are known, including accesses inside `if`, `with`,
and `range` blocks. For example, if `post` has type `Post`, an access to
`.MissingField` is reported while compiling.

The checker follows known Go++ classes and imported Go types. Values typed as
`any` or an interface, and values returned by dynamically typed template
functions, remain runtime checks because their concrete types are not known at
compile time. Template parsing and rendering still use `html/template`.

## Compare string assembly with templates

Templates separate presentation from concatenation and escape dynamic HTML
values:

::: code-group
```go [Go++]
template Page(title string) {
    <h1>{{.}}</h1>
}

tpl.Page(&output, title)
```

```go [Go]
html := "<h1>" + html.EscapeString(title) + "</h1>"
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
[template declarations](/reference/specifications/tpl) and the [template guide](/guide/standard-library/templates).
