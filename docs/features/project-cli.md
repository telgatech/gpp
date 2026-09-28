# The Go++ project CLI

The `gpp` command handles common source workflows and delegates dependency
resolution and final builds to the Go toolchain. Generated Go stays in a
hidden workspace so a project can remain organized around `.gpp` source.

## Compare project workflows

Go projects usually use Go commands directly. A Go++ project adds `gpp` for
steps that understand `.gpp` files:

::: code-group
```sh [Go++]
gpp init hello
cd hello
gpp run .
gpp build -o ./hello .
```

```sh [Go]
go mod init example.com/hello
go run .
go build -o ./hello .
```
:::

## Keep generated files out of the way

Project commands prepare generated Go in `.gpp/` by default. Choose a
different output directory or module path when needed:

```sh
gpp run -output build/gpp -module example.com/myapp .
gpp clean
gpp doctor
```

The resulting executable and deployment remain ordinary Go artifacts. See
[project builds](/guide/getting-started#project-builds) and the
[packaging specification](/reference/specifications/packaging).
