# Getting started

Go++ is distributed as a Go CLI. The compiler generates ordinary Go in a
per-project workspace under the system cache, then delegates dependency
resolution, building, and execution to the Go toolchain.

## Requirements

- Go 1.26 or newer
- Node.js 20 or newer only when developing this documentation site

Install the CLI from the repository module:

```bash
go install github.com/telgatech/gpp@latest
```

## Your first program

Create `hello.gpp`:

```go
import "fmt"

func main() {
    name := "Go++"
    fmt.Println("Hello {{name}}")
}
```

Run it directly:

```bash
gpp run hello.gpp
```

The same source can be compiled as a native executable:

```bash
gpp build hello.gpp -o ./hello
./hello
```

## Project builds

For multiple files, pass a directory or the files that belong to the build:

```bash
gpp run .
gpp build . -o ./app
```

Generated Go is kept outside the project in the system cache by default. Set
`GPP_CACHE` to choose the cache root, use `-output` to choose a workspace
directly, and use `-module` when local Go imports need a real module path:

```bash
gpp run -output build/gpp -module example.com/myapp .
```

Useful maintenance commands:

```bash
gpp clean
gpp doctor
gpp version
```

## Working with existing Go

Go++ accepts ordinary Go imports and can be mixed with `.go` files. The
repository includes examples in `examples/mixed/` showing both directions:

```bash
gpp run examples/mixed/go_from_gpp
gpp run examples/mixed/go_imports_gpp
```

This makes adoption incremental: keep stable Go packages as they are and use
Go++ where its higher-level constructs are useful.

## Next steps

- [Language tour](/guide/language)
- [Tooling](/guide/tooling)
- [Specifications](/reference/specifications)
