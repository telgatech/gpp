# External template reloads

External template files let authors work in `.gpp.tpl` files with normal
editor support. During development, the GPP HTTP server can watch those files
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

Put `.gpp.tpl` files in the project source tree so the compiler can discover
their template declarations. When the app runs with `gpp run`, those files are
registered with the development watcher. Saving a valid edit reloads it into
the running server without restarting the app:

```sh
gpp run
```

The watcher installs a new version only after it parses successfully, so a
temporary syntax error leaves the last valid template active. Production builds
use the compiled template sources and do not watch for file changes. This
development workflow applies to compiler-managed `.gpp.tpl` files; see the
[template watch specification](/reference/specifications/tpl.watch) for other
reloadable sources.
