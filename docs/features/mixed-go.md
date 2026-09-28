# Mixed Go and Go++ builds

Go++ supports gradual adoption. A Go++ package can call handwritten Go, and a
Go package can import a generated Go++ package. Teams can keep trusted Go code
in place and introduce Go++ where its language features help.

## Call Go from Go++

Keep native code in an ordinary `.go` file and call its functions from `.gpp`
source:

::: code-group
```go [Go++]
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

Run both through the Go++ project command:

```sh
gpp run .
```

## Call generated Go++ from Go

Go++ compiles source into Go packages. A normal Go consumer can import the
generated package through the configured module path and use its exported
surface. The repository examples show both interoperability directions in
`examples/mixed/`.

This boundary keeps Go's types, libraries, and build tools central. See
[working with existing Go](/guide/getting-started#working-with-existing-go)
and the [compatibility contract](/reference/specifications/compat).
