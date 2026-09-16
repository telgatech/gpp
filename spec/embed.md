# Go++ Embedded Resource Declaration Specification

## Goal

Provide a simple source-level way to embed files and directories into a Go++ binary without requiring:

* `//go:embed` comments
* explicit `embed.FS` declarations
* repeated build flags
* external runtime files

The declaration should be part of the program source so builds remain reproducible and self-describing.

---

# Basic Syntax

Go++ adds an `embed` declaration form:

```go
embed (
    staticFiles "static/"
    templates   "templates/"
    logo        "assets/logo.svg"
)
```

Each entry declares:

```text
symbol-name    source-path
```

The source path is a compile-time constant string.

---

# Directory Versus File

A trailing `/` explicitly indicates a directory.

Example:

```go
embed (
    staticFiles "static/"
)
```

means:

> recursively embed the `static` directory.

A path without a trailing `/` indicates a file:

```go
embed (
    logo "assets/logo.svg"
)
```

means:

> embed exactly `assets/logo.svg`.

The compiler should not infer file-versus-directory semantics merely by inspecting the filesystem.

The source declaration expresses the programmer's intent.

---

# Directory Validation

Given:

```go
embed (
    assets "assets/"
)
```

the compiler must verify that:

```text
assets
```

exists and is a directory.

If it is missing:

```text
embed path not found: assets/
```

If it is a file:

```text
embed path expected a directory: assets/
```

---

# File Validation

Given:

```go
embed (
    logo "assets/logo.svg"
)
```

the compiler must verify that the path exists and is a regular embeddable file.

If it is missing:

```text
embed file not found: assets/logo.svg
```

If it is a directory:

```text
embed path expected a file: assets/logo.svg
```

---

# Embedded Directory Type

An embedded directory should expose a standard Go filesystem-compatible value.

Its effective type should be:

```go
fs.FS
```

from:

```go
"io/fs"
```

or a compatible concrete type assignable to `fs.FS`.

Example:

```go
embed (
    staticFiles "static/"
)
```

allows:

```go
data := fs.ReadFile(staticFiles, "css/app.css")
```

and:

```go
http.FileServer(http.FS(staticFiles))
```

No Go++-specific filesystem API should be required.

---

# Embedded Directory Root

An embedded directory is rooted at the declared directory.

Given:

```text
static/
    css/
        app.css
    js/
        app.js
```

and:

```go
embed (
    staticFiles "static/"
)
```

the embedded filesystem should expose:

```text
css/app.css
js/app.js
```

not:

```text
static/css/app.css
static/js/app.js
```

Therefore:

```go
fs.ReadFile(staticFiles, "css/app.css")
```

is correct.

This avoids repeating the source directory prefix after embedding.

---

# Embedded File Type

A single embedded file should initially expose:

```go
[]byte
```

Example:

```go
embed (
    logo "assets/logo.svg"
)
```

then:

```go
http.ServeContent(...)
```

or other APIs may consume:

```go
logo
```

directly as bytes.

A future string-specific form may be considered if necessary, but v1 should keep one predictable representation.

---

# Multiple Entries

Any number of resources may be declared:

```go
embed (
    staticFiles "static/"
    templates   "templates/"
    migrations  "migrations/"
    logo        "assets/logo.svg"
    favicon     "assets/favicon.ico"
)
```

Each declaration creates an independent symbol.

---

# Single Declaration Form

A single-item form may optionally be supported:

```go
embed logo "assets/logo.svg"
```

but grouped declarations are the preferred form.

The grouped syntax should match other Go++ declaration groups such as:

```go
import (
    ...
)

annotation (
    ...
)

enum (
    ...
)
```

---

# Paths Are Compile-Time Constants

Embed paths must be literal compile-time strings.

Valid:

```go
embed (
    staticFiles "static/"
)
```

Invalid:

```go
path := "static/"

embed (
    staticFiles path
)
```

Embedding is determined at build time and should not depend on runtime values.

---

# Path Base

Embed paths should be resolved relative to the Go++ project/module root, not the shell's current working directory.

Given:

```text
project/
    go.mod
    app.gpp
    static/
```

and:

```go
embed (
    staticFiles "static/"
)
```

these commands should embed the same directory:

```bash
cd project
gpp build
```

and:

```bash
cd project/subdir
gpp build
```

provided both resolve to the same project/module.

---

# Deterministic Builds

The same source declaration:

```go
embed (
    staticFiles "static/"
)
```

must always describe the same project-relative resource tree.

No additional:

```bash
--embed
```

flag should be required.

This makes embedding part of the application's source contract.

---

# Build Commands

Embed declarations apply automatically to:

```bash
gpp build
gpp run
gpp test
```

No separate command-line option is required.

---

# Production Binary

Embedded resources become part of the generated native executable.

After:

```bash
gpp build
```

the application should not require the original source resource directory at runtime.

Example:

```text
app
static/
```

may become simply:

```text
app
```

for deployment if all required static assets were embedded.

---

# `gpp run`

Running:

```bash
gpp run
```

must compile and use the same embedded resources that:

```bash
gpp build
```

would include.

There should be no development-only alternate embedding semantics.

---

# `gpp test`

Tests should see the same embedded resources as production code.

Example:

```go
embed (
    fixtures "testdata/fixtures/"
)
```

may be used by test suites during:

```bash
gpp test
```

subject to normal production-build inclusion rules if the declaration itself lives in test-only source.

---

# Test-Only Embedded Resources

If an `embed` declaration appears only in:

```text
*_test.gpp
```

or otherwise test-only source, those resources should only participate in `gpp test`.

They should not be included in:

```bash
gpp build
```

This should mirror ordinary test-source exclusion semantics.

---

# Go Lowering

Go++ should lower embed declarations using Go's standard:

```go
embed
```

and:

```go
io/fs
```

mechanisms where practical.

For example:

```go
embed (
    staticFiles "static/"
)
```

may lower conceptually to:

```go
//go:embed static/**
var __gppStaticFiles embed.FS

var staticFiles fs.FS = mustSub(
    __gppStaticFiles,
    "static",
)
```

The exact generated implementation is not part of the language contract.

---

# File Lowering

Example:

```go
embed (
    logo "assets/logo.svg"
)
```

may lower approximately to:

```go
//go:embed assets/logo.svg
var logo []byte
```

---

# Directory Lowering

The compiler should generally use:

```go
fs.Sub(...)
```

or equivalent generated logic so the resulting filesystem is rooted at the declared directory.

The user should not need to call `fs.Sub` manually.

---

# Generated Build Location

Go's `//go:embed` patterns are relative to the Go source package containing them.

Because Go++ may generate temporary Go source elsewhere, the compiler must ensure embed paths remain valid.

Implementation strategies may include:

* generating relevant Go source in a package-local build location
* copying resource trees into the generated build workspace
* using another deterministic staging strategy

The implementation must preserve the source-level semantics.

Users should not need to know how this staging works.

---

# No Runtime Directory Access

A declaration such as:

```go
embed (
    staticFiles "static/"
)
```

must not lower to runtime disk access such as:

```go
http.Dir("static")
```

or:

```go
os.DirFS("static")
```

Those refer to external runtime files.

`embed` specifically means:

> include these resources in the binary at build time.

---

# Standard Library Compatibility

Embedded directories should work naturally with normal Go APIs expecting:

```go
fs.FS
```

Examples:

```go
data := fs.ReadFile(staticFiles, "index.html")
```

```go
entries := fs.ReadDir(staticFiles, ".")
```

```go
server := http.FileServer(http.FS(staticFiles))
```

The feature should avoid introducing unnecessary Go++ wrappers.

---

# Example: Static Web Assets

Project:

```text
app/
    app.gpp
    static/
        index.html
        css/
            app.css
        js/
            app.js
```

Source:

```go
import (
    "io/fs"
    "net/http"
)

embed (
    staticFiles "static/"
)

func main() {
    http.Handle(
        "/",
        http.FileServer(
            http.FS(staticFiles),
        ),
    )

    http.ListenAndServe(":8080", nil)
}
```

The resulting executable contains the entire `static/` tree.

---

# Example: Templates

```go
embed (
    templates "templates/"
)

func LoadTemplates() *template.Template {
    return template.Must(
        template.ParseFS(
            templates,
            "*.html",
        ),
    )
}
```

The embedded filesystem is already rooted at:

```text
templates/
```

so the caller does not repeat the prefix.

---

# Example: Single File

```go
embed (
    schema "schema.sql"
)
```

Then:

```go
db.Exec(string(schema))
```

---

# Duplicate Symbols

Duplicate embed symbol names are compile-time errors.

Invalid:

```go
embed (
    assets "static/"
    assets "images/"
)
```

Suggested diagnostic:

```text
duplicate embed symbol: assets
```

---

# Conflict With Other Declarations

Embed symbols participate in the normal package namespace.

Invalid:

```go
var assets string

embed (
    assets "static/"
)
```

should produce the normal duplicate declaration error.

---

# Empty Directories

If the declared directory is empty:

```go
embed (
    assets "assets/"
)
```

the compiler may either:

* permit an empty embedded filesystem, or
* reject it if Go's underlying embedding machinery cannot represent it cleanly

The chosen behavior must be deterministic and documented.

Prefer allowing empty directories if implementation permits.

---

# Hidden Files

Hidden-file behavior should follow a deterministic rule.

Recommended v1 behavior:

> Recursively embed the directory contents according to Go's embed rules.

Do not invent a separate Go++ hidden-file convention unless necessary.

If Go's underlying embed behavior excludes particular names by default, Go++ should document that behavior rather than silently pretending otherwise.

---

# Symlinks

Symlink behavior should follow Go embed safety restrictions.

Do not attempt to make Go++ embedding traverse arbitrary symlinks around the project tree.

If a resource cannot legally be embedded under Go's rules, emit a clear Go++ diagnostic.

---

# Unsupported Paths

Paths that escape the module/project root should be rejected.

For example:

```go
embed (
    secrets "../secrets/"
)
```

should fail.

Recommended principle:

> Embedded resources belong to the current project tree.

This improves reproducibility and avoids surprising filesystem access.

---

# No Wildcard Syntax in v1

Do not initially add:

```go
"static/*.css"
```

or arbitrary glob declarations as a Go++ source feature.

Directories already cover the common recursive case:

```go
"static/"
```

A glob form may be considered later if real use cases require it.

---

# No Exclusion Rules in v1

Do not initially add options such as:

```go
exclude "*.map"
exclude ".DS_Store"
```

Keep the declaration model small.

If selective embedding becomes necessary later, extend the syntax deliberately.

---

# No Virtual Mount Remapping in v1

Do not initially add:

```go
"static/" => "/public"
```

or other virtual mount syntax.

The embedded filesystem is simply rooted at the declared directory.

HTTP route mounting belongs to `gpp/http` or `net/http`, not to the embed declaration.

---

# Reflection / Metadata

Embed declarations do not require runtime reflection metadata in v1.

They are compile-time resource declarations that produce ordinary values.

Tooling may still discover them from the AST/compiler semantic model.

---

# Formatting

`gpp fmt` should format:

```go
embed (
    staticFiles "static/"
    templates   "templates/"
    logo        "assets/logo.svg"
)
```

consistently with grouped Go-style declarations.

---

# Diagnostics

Compiler diagnostics should refer to the Go++ declaration.

Example:

```text
app.gpp:12:17: embed directory not found: static/
```

Do not expose generated:

```text
__gpp_embed.go
```

errors when the compiler can provide a source-level message.

---

# Name Resolution

An embed symbol behaves like an ordinary package-level declaration.

Example:

```go
embed (
    staticFiles "static/"
)
```

then:

```go
func files() fs.FS {
    return staticFiles
}
```

is ordinary symbol resolution.

---

# Exported Embedded Resources

Capitalization should follow normal Go++ visibility rules.

Example:

```go
embed (
    Assets "assets/"
)
```

may be exported from the package.

Lowercase:

```go
embed (
    assets "assets/"
)
```

remains package-private.

Generated Go lowering should preserve this visibility where practical.

---

# Cross-Package Usage

Given package:

```go
package web.assets

embed (
    Files "static/"
)
```

another Go++ package may import it and use:

```go
assets.Files
```

if exported.

The generated representation should preserve ordinary Go package interoperability where practical.

---

# Design Principle

Embedding should feel like an ordinary declarative part of the program.

Canonical form:

```go
embed (
    staticFiles "static/"
    templates   "templates/"
    logo        "assets/logo.svg"
)
```

The visual rule is intentionally simple:

```text
"directory/"   → recursively embedded fs.FS
"file.ext"     → embedded []byte
```

The compiler handles:

```text
validation
resource collection
Go embed generation
directory rooting
temporary build staging
```

while user code receives ordinary Go-compatible values.

The feature should remove the boilerplate around Go's excellent `embed` facility without replacing the underlying standard-library model.
