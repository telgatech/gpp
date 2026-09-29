# The Go++ project CLI

The `gpp` command handles common source workflows and delegates dependency
resolution and final builds to the Go toolchain. Generated Go stays in a
hidden workspace so a project can remain organized around `.gpp` source.

## Compare project workflows

Go projects usually use Go commands directly. `gpp init` creates a starter
source file and module, tidies the module, and creates an initial Git commit
for a new repository. A Go++ project then uses `gpp` for steps that understand
`.gpp` files:

::: code-group
```sh [Go++]
gpp init hello
cd hello
gpp run .
gpp build -o ./hello .
```

```sh [Go]
go mod init example.com/hello
go mod tidy
git init
git add main.go go.mod go.sum
git commit -m init
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
