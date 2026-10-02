# External template reloads

External template files let authors work in `.gpp.tpl` files with normal
editor support. During development, the Go++ HTTP server can watch those files
and reload valid edits without restarting the application.

## Embedded source and external source

Templates can live in a `.gpp` file or a `.gpp.tpl` file. The external form
separates presentation from application code:

::: code-group

```go [Embedded template]
template Page(name string) {
    <h1>Hello, {{.}}</h1>
}
```

```go [page.gpp.tpl]
template Page(name string) {
    <h1>Hello, {{.}}</h1>
}
```

:::

Run the application with the development server to enable watching:

```sh
gpp run
```

The watcher installs a new version only after it parses successfully, so a
temporary syntax error does not replace the last valid template. Production
builds use compiled template sources. See [template watch mode](/reference/specifications/tpl.watch).
