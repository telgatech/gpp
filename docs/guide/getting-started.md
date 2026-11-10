# Getting started

Go++ is distributed as a Go CLI. The compiler generates ordinary Go in a
per-project workspace under the system cache, then delegates dependency
resolution, building, and execution to the Go toolchain.

## Requirements

- Go 1.27 or newer
- Node.js 20 or newer only when developing this documentation site

<div class="install-options">
  <section class="install-card">
    <div class="install-card-heading">
      <img src="/homebrew.svg" width="32" height="32" alt="Homebrew icon">
      <h3>Homebrew</h3>
    </div>
    <ol>
      <li><a href="https://brew.sh/">Install Homebrew</a> if needed.</li>
      <li>Add the Go++ tap: <pre><code>brew tap telgatech/gpp</code></pre></li>
      <li>Install Go++ and verify it: <pre><code>brew install --HEAD telgatech/gpp/gpp
gpp version</code></pre></li>
      <li>Update to the latest commit on <code>main</code>: <pre><code>brew update
brew upgrade --fetch-HEAD telgatech/gpp/gpp</code></pre></li>
    </ol>
    <p>The formula currently tracks <code>main</code>, so <code>--HEAD</code> is required until a versioned release. Homebrew Core also has an unrelated formula named <code>gpp</code>; uninstall it first if present, since Homebrew cannot install both same-named formulae side by side.</p>
  </section>
  <section class="install-card">
    <div class="install-card-heading">
      <img src="/go.svg" width="32" height="32" alt="Go icon">
      <h3>Go install</h3>
    </div>
    <p>For Go developers who already have Go 1.27 or newer and want to try Go++ quickly:</p>
    <pre><code>go install github.com/telgatech/gpp@main</code></pre>
    <p>Check the installation with <code>gpp version</code>.</p>
  </section>
</div>

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

Start with a project directory:

```bash
gpp init hello
cd hello
```

`gpp init` creates a starter project with `main.gpp` and a Go module.

The generated `main.gpp` contains:

```go
import "fmt"

func main() {
	fmt.Println("Hello from Go++")
}
```

Run the project:

```bash
gpp run
```

Build and run a native executable:

```bash
gpp build
./hello
```

## Project builds

For multiple files, pass a directory or the files that belong to the build:

```bash
gpp run
gpp build                  # writes an executable named after the project directory
gpp build -o ./app         # override the executable path/name
```

When building a single source file, the default executable name comes from the
file name. When building a project directory, it comes from the directory name.
Use `-o` to choose a different path or name.

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
