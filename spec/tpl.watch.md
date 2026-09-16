# Go++ Template System Addendum

This addendum updates the `gpp/tpl` specification in three areas:

1. rename dynamic rendering from `Render` to `Execute`;
2. add registry-wide `tpl.Reload()`;
3. define how `gpp/http.Server` may use template reload during development.

---

# 1. Rename `tpl.Render` to `tpl.Execute`

The dynamic template execution API is renamed:

```gpp id="g5n8q1"
tpl.Render(...)
```

becomes:

```gpp id="m3p7wd"
tpl.Execute(...)
```

The reason is consistency with Go's standard template terminology.

Go uses:

```go id="af1gm5"
tmpl.Execute(...)
tmpl.ExecuteTemplate(...)
```

Go++ therefore uses:

```gpp id="6u0qqh"
tpl.Execute(w, key, args...)
```

rather than introducing the separate term `Render`.

The operation is still conceptually rendering output to an `io.Writer`; only the public API name changes.

---

# 2. `tpl.Execute`

Canonical signature:

```gpp id="cpilbn"
tpl.Execute(
    w io.Writer,
    key string,
    args ...any,
) error
```

The lookup rule remains:

```text id="9byym5"
key contains "/"
    -> path lookup

otherwise
    -> template-name lookup
```

Examples:

```gpp id="veogxp"
tpl.Execute(w, "BlogPost", post)
```

performs exact template-name lookup.

```gpp id="1b2nag"
tpl.Execute(w, r.URL.Path, post)
```

performs path lookup against `tpl.Path(...)` metadata.

Example:

```gpp id="d17tp6"
template BlogPost(post Post) @{
    tpl.Path("/blog/:id")
} {
    <h1>{{.Title}}</h1>
}
```

may be executed by:

```gpp id="fh1ek6"
tpl.Execute(w, "/blog/42", post)
```

---

# 3. Typed Template Execution Is Unchanged

Compile-time-known templates continue to expose typed generated members:

```gpp id="tnru19"
tpl.BlogPost(w, post)
tpl.Blog(w, posts)
```

These are preferable when the template is known statically.

Therefore the two execution forms are:

```gpp id="h738ff"
tpl.BlogPost(w, post)
```

for statically known typed execution, and:

```gpp id="9vin44"
tpl.Execute(w, name, post)
```

for dynamic execution.

No generated method naming such as:

```text id="55rfhy"
ExecuteBlogPost
```

is introduced.

---

# 4. Template Execution Terminology

The canonical terminology becomes:

```text id="q300iq"
template declaration
    defines a template

tpl.BlogPost(...)
    typed execution

tpl.Execute(...)
    dynamic execution

tpl.LoadFile(...)
    load/register template source

tpl.LoadDir(...)
    load/register template source tree

tpl.Reload()
    reload registered template sources
```

The specification should use **execute** rather than **render** when referring specifically to the public runtime operation.

The word "render" may still be used descriptively when discussing produced output.

---

# 5. Add `tpl.Reload`

`gpp/tpl` provides:

```gpp id="zo8g0f"
tpl.Reload() error
```

`Reload` re-reads all reloadable template sources previously registered with the template system.

This includes sources registered through:

```gpp id="nrhkqb"
tpl.LoadFile(...)
tpl.LoadDir(...)
```

and compiler-managed `.gpp.tpl` sources registered during development.

---

# 6. Source Tracking

`gpp/tpl` must retain enough information to reload template sources after their initial registration.

Conceptually, every loaded source has a source descriptor.

Examples:

```text id="rhj4g1"
filesystem file
    path

filesystem directory
    root path

fs.FS file
    fs value + filename

fs.FS directory
    fs value
```

For example:

```gpp id="paobfg"
tpl.LoadFile("themes/dark.gpp.tpl")
```

registers both:

```text id="p4qg6q"
the parsed templates
the source descriptor for themes/dark.gpp.tpl
```

Likewise:

```gpp id="5tu9iz"
tpl.LoadDir("themes/")
```

retains the directory as a reloadable source.

---

# 7. Reload Semantics

Calling:

```gpp id="28gvnj"
tpl.Reload()
```

means:

1. re-read all registered reloadable sources;
2. rediscover `.gpp.tpl` files for registered directories;
3. parse template declarations;
4. parse template bodies through `html/template`;
5. perform Go++ template validation;
6. validate names and path registrations;
7. construct a replacement registry;
8. atomically publish the replacement only if the complete reload succeeds.

The registry must never become partially updated.

---

# 8. Atomic Reload

A reload is transactional from the application's perspective.

Before reload:

```text id="ckshmw"
registry A
```

After a successful reload:

```text id="05nxss"
registry B
```

A request must observe either A or B.

It must never observe:

```text id="86hop9"
some templates from A
some templates from B
```

The implementation should therefore build and validate a replacement registry before swapping it into active use.

---

# 9. Reload Failure

If any source fails to parse or validate:

```gpp id="zj8au6"
tpl.Reload()
```

returns an error and the previous valid registry remains active.

Example:

```text id="w5x47r"
views/blog.gpp.tpl:21:
template Post expects Post
current value has type User
```

The failed reload must not:

* remove existing valid templates;
* partially update path metadata;
* leave the registry in an inconsistent state.

---

# 10. Reload and Go++ Error Promotion

Because `Reload` returns `error`, normal Go++ automatic error promotion applies.

Typical use:

```gpp id="e7lz9p"
tpl.Reload()
```

Explicit handling remains available:

```gpp id="498t3j"
err := tpl.Reload()
```

Or:

```gpp id="luvmrv"
try {
    tpl.Reload()
} catch e {
    log.Println(e)
}
```

---

# 11. Reloading Filesystem Paths

Sources registered through:

```gpp id="242du3"
tpl.LoadFile(path)
tpl.LoadDir(path)
```

are re-read from the filesystem when `Reload` is called.

A directory reload must rediscover its current `.gpp.tpl` contents.

Therefore files may be:

```text id="9ebdm9"
added
removed
modified
```

between reloads.

The resulting replacement registry reflects the current valid state of the source tree.

---

# 12. Reloading `fs.FS`

Sources registered through:

```gpp id="rzjdsi"
tpl.LoadFile(files, name)
tpl.LoadDir(files)
```

are re-read from the supplied `fs.FS`.

Whether the underlying data can actually change depends on the implementation of that `fs.FS`.

For example:

```text id="8vum80"
embed.FS
    effectively immutable

custom filesystem
    may change dynamically
```

`tpl.Reload()` does not need special cases.

It simply re-reads the registered source.

---

# 13. Compiler-Managed Development Sources

Under:

```bash id="m2yxi6"
gpp run
```

compiler-known `.gpp.tpl` files may be registered with `gpp/tpl` as reloadable source descriptors.

This allows:

```gpp id="l8w2fn"
tpl.Reload()
```

to refresh normal application templates during development without requiring user-written `LoadDir` calls.

Under:

```bash id="jgdy2s"
gpp build
```

the same compile-time templates are embedded into the production binary.

---

# 14. Production Reload Behavior

Compile-time production templates are backed by embedded source.

Calling:

```gpp id="gx53zf"
tpl.Reload()
```

against embedded compiler-owned template sources simply re-reads the embedded data and therefore normally produces no change.

This is valid and harmless.

Production applications may still gain meaningful reload behavior if they additionally load external sources:

```gpp id="jdoq8r"
tpl.LoadDir("/etc/myapp/templates")
```

Then:

```gpp id="wgl3td"
tpl.Reload()
```

will re-read those external templates.

---

# 15. `gpp/http.Server` Integration

`gpp/http.Server` may provide automatic development-time template watching.

The important architectural rule is:

> `gpp/http.Server` watches for changes; `gpp/tpl` performs the reload.

The server should not independently parse or mutate the template registry.

Conceptually:

```text id="hhra27"
filesystem watcher
        ↓
template source changed
        ↓
tpl.Reload()
```

This keeps template ownership inside `gpp/tpl`.

---

# 16. Why Watching Belongs to the Server

`tpl.LoadFile` and `tpl.LoadDir` are synchronous loading operations.

They should not unexpectedly:

* spawn background goroutines;
* start filesystem watchers;
* own application shutdown lifecycle;
* run indefinitely.

For example:

```gpp id="ipj86b"
tpl.LoadDir("themes/")
```

means:

> load/register this directory now.

It does not mean:

> load it and watch it forever.

The long-running HTTP server already owns application lifecycle, making it a natural place for development file watching.

---

# 17. Development Watch Flow

Under `gpp run`, a server may behave conceptually as:

```text id="zhfgd5"
start server
    ↓
watch compiler-known .gpp.tpl sources
    ↓
source change detected
    ↓
debounce/coalesce filesystem events
    ↓
tpl.Reload()
    ↓
success:
    continue with new registry

failure:
    log diagnostic
    continue with previous registry
```

The HTTP process remains running after a reload failure.

---

# 18. Server Does Not Need Template Internals

`gpp/http.Server` should not need to know:

* template declaration names;
* template signatures;
* path metadata;
* template ASTs;
* active registry structure;
* how templates are parsed.

It only needs to know that template source changed.

Then it calls:

```gpp id="gpdiys"
tpl.Reload()
```

This keeps coupling between `gpp/http` and `gpp/tpl` minimal.

---

# 19. Watch Scope

Automatic development watching should initially apply only to compiler-known `.gpp.tpl` application sources.

For example:

```text id="ou17se"
views/blog.gpp.tpl
views/users.gpp.tpl
```

registered by `gpp run`.

A runtime call such as:

```gpp id="2ucrbx"
tpl.LoadDir("/customer/templates")
```

does not automatically require `http.Server` to begin watching that external directory.

The source remains reloadable through:

```gpp id="s1imv2"
tpl.Reload()
```

but automatic watching of arbitrary runtime-added sources is outside the initial contract.

This behavior may be extended later if actual use cases require it.

---

# 20. Manual Reload

Because reload is a normal `gpp/tpl` API, applications may trigger it independently of HTTP file watching.

Examples include:

```text id="ct5g1w"
admin command
signal handler
development command
test
plugin refresh
configuration reload
```

For example:

```gpp id="uh9nhm"
func ReloadTemplates() {
    tpl.Reload()
}
```

The feature is therefore not coupled to HTTP.

---

# 21. Testing Reload Behavior

`tpl.Reload()` should make template reload straightforward to test.

Example conceptually:

```gpp id="f9mb0o"
tpl.LoadDir(tempDir)

tpl.Execute(&buf, "Page", data)

writeUpdatedTemplate()

tpl.Reload()

tpl.Execute(&buf, "Page", data)
```

Tests can verify:

* changed output;
* newly discovered templates;
* removed templates;
* invalid reload rollback;
* duplicate-name handling;
* path conflict handling.

---

# 22. Revised Minimal Public API

The `gpp/tpl` v1 public API becomes:

```gpp id="ecgygt"
tpl.Execute(
    w io.Writer,
    key string,
    args ...any,
) error

tpl.LoadFile(path string) error
tpl.LoadFile(files fs.FS, name string) error

tpl.LoadDir(path string) error
tpl.LoadDir(files fs.FS) error

tpl.Reload() error
```

Generated compile-time members remain:

```gpp id="r0q5jl"
tpl.<TemplateName>(
    w io.Writer,
    args...
) error
```

Template annotation:

```gpp id="zwxh5f"
tpl.Path(pattern string)
```

Template helper:

```text id="s1v8ri"
param
```

---

# 23. Revised Examples

Static execution:

```gpp id="320vd6"
tpl.BlogPost(w, post)
```

Dynamic name execution:

```gpp id="rhp6u6"
tpl.Execute(w, "BlogPost", post)
```

Dynamic path execution:

```gpp id="zz23sc"
tpl.Execute(w, r.URL.Path, post)
```

Load from disk:

```gpp id="wdd5md"
tpl.LoadFile("theme.gpp.tpl")
tpl.LoadDir("themes/")
```

Load from `fs.FS`:

```gpp id="zo50xq"
tpl.LoadFile(themeFS, "dark.gpp.tpl")
tpl.LoadDir(themeFS)
```

Reload:

```gpp id="0utq7c"
tpl.Reload()
```

---

# 24. Revised Runtime Model

The runtime model is:

```text id="7u24y4"
source descriptors
    ↓
LoadFile / LoadDir
    ↓
parse declarations
    ↓
html/template parser
    ↓
Go++ validation
    ↓
active registry
    ↓
tpl.Execute
```

Reload is:

```text id="igso3b"
existing source descriptors
    ↓
tpl.Reload
    ↓
re-read all sources
    ↓
parse + validate replacement registry
    ↓
atomic swap
```

Development watching is:

```text id="w5mjlv"
gpp/http.Server
    ↓
watch compiler-known .gpp.tpl files
    ↓
change detected
    ↓
tpl.Reload
```

---

# 25. Design Principle

The resulting separation is:

```text id="4j66vp"
Go++ compiler
    knows template declarations
    knows compile-time signatures
    discovers .gpp.tpl
    embeds production sources

gpp/tpl
    owns template sources
    owns registry
    owns Execute
    owns loading
    owns Reload

gpp/http.Server
    optionally watches development files
    calls tpl.Reload
```

This avoids coupling template parsing and lifecycle logic into the HTTP package.

It also avoids turning `gpp/tpl` loading operations into hidden background services.

---

# 26. Final API Summary

```gpp id="arxpx3"
import "gpp/tpl"

tpl.BlogPost(w, post)

tpl.Execute(w, "BlogPost", post)

tpl.Execute(w, r.URL.Path, post)

tpl.LoadFile("theme.gpp.tpl")
tpl.LoadFile(themeFS, "theme.gpp.tpl")

tpl.LoadDir("themes/")
tpl.LoadDir(themeFS)

tpl.Reload()
```

Inside templates:

```gotemplate id="ed6b4b"
{{BlogPost .}}
```

The underlying body language remains standard Go `html/template`.

The public terminology now follows Go conventions: templates are **executed**, sources are **loaded**, and registered sources may be **reloaded**.
