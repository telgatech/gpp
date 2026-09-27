# `gpp/tpl`

`gpp/tpl` renders Go++ templates using typed arguments and Go's template execution model. Templates can be declared in source or loaded from `.gpp.tpl` files, composed through layouts, and served by the HTTP package.

## Why use it

HTML often mixes presentation with application logic. Templates provide an explicit presentation layer, while typed template parameters make the data expected by a page visible in the source. Go++ templates retain Go template execution semantics and can be used without adopting a separate web framework.

## Side by side: render a page

With `html/template`, callers create and execute a template explicitly:

```go
tmpl := template.Must(template.New("page").Parse(`<h1>{{.Title}}</h1>`))
err := tmpl.Execute(&output, post)
```

With Go++ embedded templates, declaration and invocation are connected:

```go
class Post { Title string }

template Page(post Post) {
	<h1>{{.Title}}</h1>
}

var output bytes.Buffer
tpl.Page(&output, Post(Title: "Hello"))
```

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

Templates can be associated with paths using `tpl.Path`, then executed by path with `tpl.Execute`. This is useful for routing page rendering from an HTTP handler. The `ctx.Template` response helper connects HTTP routes to named templates. External template files can be watched during development when using the watch tooling.

See the [template specification](/reference/specifications/tpl), [HTTP integration](/reference/specifications/tpl.http), [watch support](/reference/specifications/tpl.watch), and [ad hoc templates](/reference/specifications/tpl.adhoc).
