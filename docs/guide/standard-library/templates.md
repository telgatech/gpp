# `gpp/tpl`

`gpp/tpl` renders Go++ templates using typed arguments and Go's template execution model. Templates can be declared in source or loaded from `.gpp.tpl` files, composed through layouts, and served by the HTTP package.

## Why use it

HTML often mixes presentation with application logic. Templates provide an explicit presentation layer, while typed template parameters make the data expected by a page visible in the source. Go++ templates retain Go template execution semantics and can be used without adopting a separate web framework.

## Side by side: render a page

Go++ connects a typed template declaration to its invocation. Select the Go tab to compare it with explicit `html/template` setup:

::: code-group

```go [Go++]
class Post { Title string }

template Page(post Post) {
	<h1>{{.Title}}</h1>
}

var output bytes.Buffer
tpl.Page(&output, Post(Title: "Hello"))
```

```go [Go]
tmpl := template.Must(template.New("page").Parse(`<h1>{{.Title}}</h1>`))
err := tmpl.Execute(&output, post)
```

:::

## The `tpl` namespace and generated functions

`template` is a recognized Go++ declaration keyword, not a function call or a
string passed to a registration API. This lets a template be written in the
same source file as the code that uses it, with a function-like signature that
makes its inputs and purpose clear:

```go
template Page(post Post) {
	<h1>{{.Title}}</h1>
}

tpl.Page(&output, post)
```

The body accepts HTML or XML markup directly; you do not wrap it in quotes or
escape it into a string. That gives a similar inline-markup convenience to
JSX, while keeping Go++ template bodies in Go's `html/template` syntax and
using its standard actions to access template data.

Here `Page` declares a renderer that expects a `Post`; the template body uses
that value as `.`. The declaration can live in a `.gpp` file or in a dedicated
`.gpp.tpl` file.

`tpl` is Go++'s special template namespace. You do not construct a `tpl`
value or register each source template by hand: every template known at
compile time is exposed as a statically typed function under its name. For
example, `template Page(post Post)` provides `tpl.Page(...)`. Go++ resolves
that name to a generated renderer whose Go signature includes the declared
parameters, so a direct call is checked for the template name, argument count,
and argument types during compilation.

Every generated template function takes an `io.Writer` as its first argument,
followed by the values declared in the template signature. The source-level
binding has a signature equivalent to:

```go
func Page(w io.Writer, post Post) error
```

That writer-first rule applies even to templates with no data parameters. It
lets a template render directly to a buffer, file, or HTTP response without
building an intermediate string. The renderer returns an error; Go++
propagates it automatically when the call does not capture it:

```go
var output bytes.Buffer
tpl.Page(&output, Post(Title: "Hello"))
```

Use `tpl.Execute(writer, name, args...)` when the template name is only known
at runtime. Runtime-loaded templates do not receive statically generated
`tpl.Name` members.

::: warning Template bodies are not statically type-checked

The compiler validates the template body's `html/template` syntax, and direct
calls to generated renderers get ordinary compile-time argument checking. It
does **not** yet check expressions inside the body against the declared
parameter types—for example, whether `.Title` exists on `Post`.
`html/template` reports those data-access errors when the template executes.
Static checking of template-body expressions against the declared types is
planned.

:::

## Layouts, paths, and functions

```go
template Layout() {
	<html><body><main>{{body}}</main></body></html>
}

template Listing(title string): Layout {
	<h1>{{.}}</h1>
}

tpl.Funcs.Add("upper", strings.ToUpper)
tpl.Listing(&output, "Bicycles")
```

## Bind a template to a URL path

Use the `tpl.Path` annotation when a template should be selected by the
incoming URL path. The annotation records a route-shaped pattern on the
template; it does not register an HTTP handler by itself. For example, the
template below is associated with any `/posts/{id}` request:

```go
template PostPage(post Post) @{tpl.Path("/posts/{id}")} {
	<h1>{{.Title}}</h1>
}
```

The HTTP handler can pass the request path to `ctx.Template`. The helper looks
up the matching path annotation, supplies the matched path parameters to the
template, and writes the result as an HTML response:

```go
class App : http.Server {
	func ShowPost(ctx *http.Context) error @{http.GET("/posts/{id}")} {
		post := LoadPost(ctx.Param("id"))
		return ctx.Template(ctx.Request.URL.Path, post)
	}
}
```

`ctx.Template` also looks up a template by its declared name. Use that form
when the handler already knows which page it wants to render:

```go
return ctx.Template("PostPage", post)
```

The helper defaults to status 200; its status overload accepts the status
first, as in `ctx.Template(404, "NotFound", data)`. The corresponding
non-HTTP API is `tpl.Execute(writer, key, args...)`, where `key` can be a
template name, a URL path matched by `tpl.Path`, or inline template source.
Static calls such as `tpl.PostPage(writer, post)` remain available when the
template is known at compile time. For path matching rules and parameter
access, see the [template specification](/reference/specifications/tpl).

External template files can be watched during development when using the
watch tooling.

See the [template specification](/reference/specifications/tpl), [HTTP integration](/reference/specifications/tpl.http), [watch support](/reference/specifications/tpl.watch), and [ad hoc templates](/reference/specifications/tpl.adhoc).
