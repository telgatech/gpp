# Go++ CLI Packaging and Release Specification

## Goal

Distribute Go++ as a single executable named:

```text
gpp
```

The executable should contain the full Go++ command-line interface and dispatch all development tasks through subcommands.

The preferred installation command is:

```bash
go install github.com/telgatech/gpp@latest
```

After installation, the user should have:

```bash
gpp
```

available from:

```text
$(go env GOPATH)/bin
```

typically:

```text
~/go/bin
```

provided that directory is on `PATH`.

---

# Single Binary

Go++ should use one executable:

```text
gpp
```

Do not create separate binaries such as:

```text
gpp-build
gpp-run
gpp-init
gpp-clean
```

All functionality should be exposed as subcommands:

```text
gpp init
gpp compile
gpp build
gpp run
gpp clean
gpp fmt
gpp test
gpp env
gpp doctor
gpp version
gpp doc
gpp lsp
```

This keeps:

* installation simple
* versioning consistent
* compiler/runtime compatibility predictable
* standard library resources bundled with one tool
* future binary releases straightforward

---

# Module Root

To support:

```bash
go install github.com/telgatech/gpp@latest
```

the Go module root should itself be an installable:

```go
package main
```

The CLI entry point should therefore live at the repository root.

Recommended:

```text
gpp/
├── go.mod
├── go.sum
├── main.go
├── internal/
├── compiler/
├── parser/
├── ast/
├── sema/
├── codegen/
├── runtime/
├── stdlib/
└── examples/
```

The root `main.go` should remain very small.

Example:

```go
package main

import (
    "os"

    "github.com/telgatech/gpp/internal/command"
)

func main() {
    os.Exit(command.Run(os.Args[1:]))
}
```

Actual CLI behavior should live outside `main.go`.

---

# Migration From `cmd/gpp`

If the current entry point is:

```text
cmd/gpp/main.go
```

move or replace it with:

```text
/main.go
```

at the module root.

The existing command implementation code does not need to move into `main.go`.

Only the executable entry point needs to be available from the module root.

After this change:

```bash
go install github.com/telgatech/gpp@latest
```

should install:

```text
gpp
```

directly.

---

# Internal Command Structure

Recommended structure:

```text
internal/
└── command/
    ├── command.go
    ├── init.go
    ├── build.go
    ├── run.go
    ├── clean.go
    ├── fmt.go
    ├── test.go
    ├── env.go
    ├── doctor.go
    └── version.go
```

The top-level dispatcher may conceptually look like:

```go
func Run(args []string) int {
    if len(args) == 0 {
        return help()
    }

    switch args[0] {
    case "init":
        return runInit(args[1:])
    case "build":
        return runBuild(args[1:])
    case "run":
        return runRun(args[1:])
    case "clean":
        return runClean(args[1:])
    case "fmt":
        return runFmt(args[1:])
    case "test":
        return runTest(args[1:])
    case "env":
        return runEnv(args[1:])
    case "doctor":
        return runDoctor(args[1:])
    case "version":
        return runVersion(args[1:])
    default:
        return unknownCommand(args[0])
    }
}
```

Exact implementation is flexible.

The important rule is:

> `main.go` should dispatch; command packages should implement behavior.

---

# Required Initial Commands

The first public release should support at least:

```text
gpp init
gpp build
gpp run
gpp clean
gpp fmt
gpp test
gpp env
gpp doctor
gpp version
```

---

# `gpp init`

Creates a new Go++ project with a starter source file, root Go module, and Git
history. The default module path is `example.com/<directory-name>` with the
directory name normalized for use as a module component. Pass `-module` to
choose a different path.

Examples:

```bash
gpp init hello
```

```bash
gpp init -module github.com/acme/hello hello
```

```bash
gpp init .
```

`gpp init` refuses to overwrite an existing `main.gpp` or `go.mod`. It creates
`main.gpp` and `.gitignore` entries for `.gpp/` and the default `main` build
output, runs `go mod init`, then runs `go mod tidy` in the project root. The initial module contains no Go
packages yet, so Go may print its normal “matched no packages” warning.

If the target is outside an existing Git worktree, `gpp init` runs `git init`,
stages only the generated starter files (`main.gpp`, `go.mod`, `.gitignore`, and
`go.sum` when present), and creates the first commit with the message `init`.
It never stages unrelated files. If the target is already inside a Git
worktree, the command does not create a nested repository or stage or commit
any files. Git
must be installed and configured with an author name and email for the initial
commit; if the commit fails, the scaffold remains and its generated files stay
staged.

The default starter should remain intentionally minimal.

Recommended generated source:

```go
import "fmt"

func main() {
    fmt.Println("Hello from Go++")
}
```

No explicit:

```go
package main
```

should be required because Go++ defaults to `main`.

The command requires a supported Go toolchain because it creates and tidies the
project's module.

---

# `gpp build`

Compiles Go++ source into a native executable.

Examples:

```bash
gpp build hello.gpp
```

should produce:

```text
./hello
```

and:

```bash
gpp build .
```

should build the current project.

Recommended optional output override:

```bash
gpp build -o app
```

Generated intermediate Go source must not normally be placed beside the user's `.gpp` source.
By default, all CLI generation commands use a stable, per-project workspace
under the system cache directory. `GPP_CACHE` overrides the cache root, and
`-output` selects an explicit workspace. `gpp clean` removes the current
project's default cached workspace.

---

# `gpp run`

Compiles and immediately runs a Go++ program.

Examples:

```bash
gpp run hello.gpp
```

or:

```bash
gpp run .
```

A single-file program should not require:

```text
go.mod
package declaration
manual project setup
```

The command should create any temporary Go module/build environment required internally.

---

# `gpp clean`

Removes Go++-generated temporary or cache artifacts.

It should not delete user source.

Potential targets include:

```text
compiler cache
generated temporary Go files
temporary module directories
temporary binaries
```

The exact cache location may be platform-dependent.

---

# `gpp fmt`

Formats Go++ source.

Example:

```bash
gpp fmt .
```

or:

```bash
gpp fmt app.gpp
```

Formatting should operate on Go++ syntax rather than merely formatting generated Go.

The style should be deterministic.

---

# `gpp test`

Runs Go++ tests.

Example:

```bash
gpp test
```

or:

```bash
gpp test ./...
```

Exact testing syntax may evolve, but all testing should remain behind the same `gpp` executable.

The command may lower Go++ tests to Go and then invoke:

```bash
go test
```

internally.

---

# `gpp env`

Displays Go++ environment information, including the cache root used by
generation commands. `GPP_CACHE` may be set to override the system cache path.

Recommended fields include:

```text
GPP_VERSION
GPP_CACHE
GO
GO_VERSION
GOOS
GOARCH
GOPATH
GOROOT
```

Example:

```text
GPP_VERSION="0.1.0"
GPP_CACHE="/Users/me/Library/Caches/gpp"
GO="/usr/local/go/bin/go"
GO_VERSION="go1.xx.x"
GOOS="darwin"
GOARCH="arm64"
```

This command is primarily diagnostic.

---

# `gpp doctor`

Checks whether the local machine is capable of compiling Go++ programs.

It should verify at least:

```text
gpp installation
Go executable available
supported Go version
cache directory writable
compiler-owned resources available
basic temporary build succeeds
```

Possible output:

```text
Go++ 0.1.0

✓ Go found
✓ Go version supported
✓ cache writable
✓ embedded standard library available
✓ test compilation succeeded
```

Failures should include actionable diagnostics.

Example:

```text
✗ Go compiler not found

Install Go and ensure `go` is available on PATH.
```

---

# `gpp version`

Displays version information.

Minimum:

```text
gpp 0.1.0
```

Recommended:

```text
gpp 0.1.0
go go1.xx.x
darwin/arm64
```

Development builds may additionally include:

```text
commit
build date
dirty state
```

where available.

Do not require these fields for ordinary release builds.

---

# Help

Running:

```bash
gpp
```

with no arguments should display concise help.

Example:

```text
Go++ compiler and toolchain

Usage:
    gpp <command> [arguments]

Commands:
    init       create a Go++ project
    build      build a Go++ program
    run        build and run a Go++ program
    clean      remove generated build artifacts
    fmt        format Go++ source
    test       run tests
    env        show environment information
    doctor     diagnose the toolchain
    version    show version information
```

Each command should also support:

```bash
gpp build --help
gpp run --help
```

---

# Standard Library Packaging

Compiler-owned Go++ resources should be embedded into the `gpp` executable.

This includes, where applicable:

```text
prelude
gpp/http
gpp/orm
gpp/encoding
runtime templates
compiler support files
generated runtime source templates
```

Use Go's embedding support where practical.

Conceptually:

```go
import "embed"

//go:embed stdlib/**
var stdlibFS embed.FS
```

Exact paths are implementation-defined.

---

# No External `GPP_HOME`

The first release should not require users to configure:

```text
GPP_HOME
GPP_STDLIB
GPP_RUNTIME
```

or similar directories.

The installation should be self-contained apart from the Go toolchain.

This should work:

```bash
go install github.com/telgatech/gpp@latest
gpp run hello.gpp
```

without further Go++ setup.

---

# Embedded Prelude

The Go++ prelude should be embedded in the compiler executable.

The compiler may load it logically for every compilation.

Users should not need to locate or install:

```text
prelude.gpp
```

manually.

If supported:

```bash
gpp build --no-prelude
```

may disable it for diagnostics or advanced use.

---

# Embedded Official `gpp/*` Libraries

Official libraries such as:

```text
gpp/http
gpp/orm
gpp/encoding
gpp/collections
gpp/test
```

should resolve from compiler-owned embedded resources.

Example:

```go
import "gpp/http"
```

should not require downloading:

```text
gpp/http
```

from the network.

Resolution order for the official namespace should treat:

```text
gpp/*
```

as compiler-distributed standard modules.

---

# Go Toolchain Dependency

Initially, `gpp` may depend on an installed Go toolchain for compilation.

Commands such as:

```text
gpp build
gpp run
gpp test
```

may invoke:

```bash
go build
go run
go test
```

or equivalent internal Go commands.

This is acceptable for early releases.

---

# Go Discovery

The compiler should locate Go using normal system mechanisms.

Preferred:

```go
exec.LookPath("go")
```

and/or:

```bash
go env
```

Do not hard-code installation paths.

---

# Missing Go Toolchain

If Go is unavailable:

```bash
gpp build app.gpp
```

should fail with a clear diagnostic.

Example:

```text
Go toolchain not found.

Go++ currently requires Go to build programs.
Install Go and ensure `go` is available on PATH.
```

Do not expose obscure `exec` errors where a clearer message is possible.

---

# Go Version Compatibility

The project should define a minimum supported Go version.

`gpp doctor` should check it.

Compilation should fail clearly when the installed version is unsupported.

Example:

```text
Go++ 0.1.0 requires Go 1.xx or newer.
Found Go 1.yy.
```

Exact version requirements should be defined by the implementation and release.

---

# Generated Build Environment

For single-file programs:

```bash
gpp run hello.gpp
```

Go++ may internally create a temporary module such as:

```text
/tmp/gpp-xxxx/
    go.mod
    generated.go
```

and invoke the Go compiler there.

These implementation details must remain invisible during normal successful builds.

---

# Generated Go Inspection

Support an explicit inspection option:

```bash
gpp build --emit-go
```

This should allow developers to inspect the generated Go source.

Possible forms include:

```bash
gpp build --emit-go app.gpp
```

or:

```bash
gpp build --emit-go=./generated app.gpp
```

Exact CLI syntax may be finalized separately.

Normal builds should not clutter source directories with generated Go.

---

# Mixed Go and Go++ Projects

The toolchain should support projects containing both:

```text
.gpp
.go
```

files where practical.

Native Go source should remain usable directly.

Go++ should generate additional Go source and compile the combined package.

---

# Installation

Primary source installation:

```bash
go install github.com/telgatech/gpp@latest
```

Specific version:

```bash
go install github.com/telgatech/gpp@v0.1.0
```

Development checkout:

```bash
go install .
```

from the repository root.

---

# Binary Location

Go normally installs the executable into:

```text
$(go env GOBIN)
```

when `GOBIN` is configured.

Otherwise:

```text
$(go env GOPATH)/bin
```

commonly:

```text
~/go/bin
```

Documentation should tell users to ensure the appropriate directory is on `PATH`.

Do not hard-code `~/go/bin` as the only valid installation location.

---

# Release Versioning

Use semantic version tags.

Examples:

```text
v0.1.0
v0.2.0
v0.2.1
```

Initial development should remain within:

```text
v0.x
```

while language compatibility is still evolving.

Release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Then:

```bash
go install github.com/telgatech/gpp@v0.1.0
```

must install that release.

---

# Compiler and Standard Library Versioning

The embedded Go++ standard library should be versioned with the compiler.

For the initial design:

```text
gpp compiler 0.1.0
```

ships with the matching:

```text
prelude
gpp/http
gpp/orm
gpp/encoding
runtime
```

resources.

Do not independently fetch mismatched standard-library versions for ordinary builds.

This keeps compiler/runtime behavior deterministic.

---

# Future Binary Releases

Later releases may provide precompiled binaries through GitHub Releases.

Potential artifacts:

```text
gpp-darwin-arm64
gpp-darwin-amd64
gpp-linux-amd64
gpp-linux-arm64
gpp-windows-amd64.exe
```

These may be produced by GitHub Actions.

This is not required for the first release.

---

# Future Package Managers

Later, distribution may additionally support:

```text
Homebrew
Scoop
winget
apt repositories
Docker
```

but these should wrap the same single `gpp` executable.

Do not restructure the compiler around a particular package manager.

---

# Command Exit Codes

All subcommands should return meaningful process exit codes.

Convention:

```text
0   success
non-zero   failure
```

Compilation failures, invalid CLI usage, missing dependencies, failed tests, etc. should return non-zero.

Do not call:

```go
os.Exit(...)
```

deep inside compiler packages.

Return structured errors/statuses to the command layer where practical.

---

# Output Streams

Normal command output should go to:

```text
stdout
```

Diagnostics and errors should go to:

```text
stderr
```

This allows:

```bash
gpp env > env.txt
```

and normal shell scripting.

---

# Compiler Diagnostics

Errors should refer to Go++ source locations, not generated Go implementation details wherever possible.

Preferred:

```text
app.gpp:14:12: invalid enum value "deleted" for Status
```

Avoid exposing errors such as:

```text
__gpp_generated_239.go:842: ...
```

unless generated-Go details are explicitly requested.

---

# Public CLI Stability

Once released, command names should be treated as part of the public Go++ developer experience.

Prefer:

```text
gpp build
```

over introducing alternative aliases unnecessarily.

Keep the command surface small.

New commands should be added only when they represent a distinct workflow.

---

# Initial User Experience

The target first-run experience is:

```bash
go install github.com/telgatech/gpp@latest

gpp version

gpp init hello
cd hello

gpp run
```

The user should immediately see:

```text
Hello from Go++
```

No additional Go++ installation or configuration should be necessary.

---

# Design Principle

Go++ should feel like a normal Go toolchain extension rather than a large standalone SDK.

The release model is:

```text
Go toolchain
     +
single gpp executable
     +
embedded Go++ compiler/runtime/stdlib
```

The single executable owns the complete Go++ frontend experience:

```text
gpp init
gpp build
gpp run
gpp clean
gpp fmt
gpp test
gpp env
gpp doctor
gpp version
```

The preferred installation contract is:

```bash
go install github.com/telgatech/gpp@latest
```

and that should be sufficient to install the Go++ toolchain frontend.
