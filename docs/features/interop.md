# Go and GPP interop

GPP and Go source can live in the same module. GPP code can call native Go
functions, and Go code can import packages generated from GPP.

## Call Go from GPP

Keep both files in the same Go package. GPP can call the Go functions directly,
including functions that return an error; GPP promotes an unhandled error to an
exception that can be caught locally.

`main.gpp`:

```gpp
package main

import "fmt"

func main() {
    greeting := NativeGreeting("Ada")
    fmt.Println(greeting)

    try {
        NativeGreeting(" ")
    } catch err {
        fmt.Println("GPP caught native Go error:", err)
    }
}
```

`native.go`:

```go
package main

import (
    "errors"
    "strings"
)

func NativeGreeting(name string) (string, error) {
    name = strings.TrimSpace(name)
    if name == "" {
        return "", errors.New("name is required")
    }
    return "Hello, " + name + " from native Go", nil
}
```

Run the project directory. `main.gpp` is the entry point, and GPP compiles
`native.go` alongside it:

```sh
gpp run examples/interop/go_from_gpp
```

## Call GPP from Go

Go imports a GPP package by its module import path, just like any other Go
package. In the example, `main.go` imports
`example.com/gpp-go-imports-gpp/greeting`. The `go.mod` in that project gives
GPP the module path automatically.

The exported GPP `New` function returns `*Greeting`. Go receives the generated
class as a regular Go type and can access its exported field and method:

`main.go`:

```go
package main

import (
    "fmt"

    "example.com/gpp-go-imports-gpp/greeting"
)

func main() {
    person, err := greeting.New("Grace")
    if err != nil {
        fmt.Println("Go handled returned error:", err)
        return
    }
    fmt.Println(person.Name, person.Message())
}
```

The same example calls `greeting.Hello`, where GPP catches the error from `New`
as an exception. Go can also call `New` directly and handle its returned error
with ordinary Go error handling.

::: warning Limited interop coverage
Using GPP-generated class constructors and enums from Go may be unstable
because these paths do not yet have sufficient test coverage. This warning
applies to Go consuming GPP; calling Go code from GPP is stable.
:::

Run the directory to compile `greeting.gpp` and execute its Go `main.go`:

```sh
gpp run examples/interop/go_imports_gpp
```

See the complete [interop examples](/examples/) and the
[compatibility contract](/reference/specifications/compat).
