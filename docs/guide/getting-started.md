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

Try the command:

```bash
gpp version
```

If your terminal says it can't find `gpp`, add Go's install directory to your
`PATH` using the steps below.

::: details Troubleshooting: `gpp` command not found

`go install` puts the `gpp` executable in `GOBIN` when that variable is set;
otherwise it uses `GOPATH/bin`. With Go's default settings, this is `~/go/bin`
on macOS and Linux, and `%USERPROFILE%\go\bin` on Windows. Check the actual
location with `go env GOBIN GOPATH`.

**Windows:** In PowerShell, print the directory Go uses:

```powershell
$goBin = (go env GOBIN)
if (-not $goBin) { $goBin = Join-Path (go env GOPATH) 'bin' }
$goBin
```

Copy the printed path. Open **Edit environment variables for your account** from
the Start menu, select **Path** under **User variables**, choose **Edit** →
**New**, and paste the path. Open a new terminal for the change to take effect.

**macOS and Linux:** Add these lines to your shell startup file, then open a new
terminal (or source the file):

```bash
goBin="$(go env GOBIN)"
[ -n "$goBin" ] || goBin="$(go env GOPATH)/bin"
export PATH="$goBin:$PATH"
```

Use `~/.zshrc` for the default shell on macOS, or `~/.bashrc` for Bash on Linux
(some login shells use `~/.bash_profile` instead).

:::

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
