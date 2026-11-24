# Go and GPP interop

GPP supports gradual adoption. A GPP package can call handwritten Go, and a
Go package can import a generated GPP package. Teams can keep trusted Go code
in place and introduce GPP where its language features help.

::: warning Limited interop coverage
Using GPP-generated objects including classes and enums from Go may be unstable
because these paths do not yet have sufficient test coverage. This warning
does not apply to calling Go code from GPP.
:::

## Call Go from GPP

Keep native code in an ordinary `.go` file and call its functions from `.gpp`
source:

::: code-group
```go [GPP]
import "fmt"

func main() {
    fmt.Println(greeting("Ada"))
}
```

```go [Go]
package main

func greeting(name string) string {
    return "Hello, " + name
}
```
:::

Run the directory through GPP. `main.gpp` is the entry point, and `native.go`
is compiled alongside it:

```sh
gpp run examples/interop/go_from_gpp
```

## Call generated GPP from Go

GPP compiles source into Go packages. A normal Go consumer can import the
generated package through the configured module path and use its exported
surface. When a mixed source directory has a `go.mod`, `gpp run` detects its
module path automatically. The repository examples show both interoperability
directions in `examples/interop/`.

In the runnable example, GPP compiles `greeting.gpp` and then runs the Go
`main.go` entry point. `New` returns `*Greeting`, so Go can read its exported
`Name` field and call its `Message` method directly. `Hello` also shows GPP
catching the error returned by `New`:

```sh
gpp run examples/interop/go_imports_gpp
```

This boundary keeps Go's types, libraries, and build tools central. See
[working with existing Go](/guide/getting-started#working-with-existing-go)
and the [compatibility contract](/reference/specifications/compat).
