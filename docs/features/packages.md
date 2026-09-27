# Go packages and compatibility

Go++ is designed to fit beside ordinary Go source. It uses Go imports and
types, compiles into Go, and lets you adopt its syntax in the parts of a project
where it helps. Existing packages, libraries, and deployment workflows stay
available.

## The same Go foundation

A Go++ file can import and call a Go package directly. Its functions and values
remain Go values:

::: code-group

```go [Go]
package main

import "fmt"

func main() {
    fmt.Println("Hello, Go")
}
```

```go [Go++]
import "fmt"

func main() {
    fmt.Println("Hello, Go++")
}
```

:::

Go++ also works with existing handwritten Go files in the same project:

```go
// native.go
package main

func greeting(name string) string { return "Hello, " + name }
```

```go
// main.gpp
import "fmt"

func main() {
    fmt.Println(greeting("Ada"))
}
```

## Keep package organization practical

A missing package declaration defaults to `main`. Go++ source folders do not
have to mirror Go package names, which can help organize language source while
the compiler prepares a Go build workspace.

Go code can also consume a generated Go++ package. Use Go++ where its classes,
overloads, or structured errors make a module clearer, and leave stable
packages in Go. See [getting started](/guide/getting-started#working-with-existing-go)
and the [compatibility rules](/reference/specifications/compat).
