# Go++ `tpl.Execute` Unified Resolution Specification

## 1. Purpose

`gpp/tpl` provides one dynamic execution API:

```gpp id="f3mx4r"
tpl.Execute(
    w io.Writer,
    key string,
    args ...any,
) error
```

`Execute` may execute:

1. a registered template by name;
2. a registered template selected by path;
3. ad-hoc template source supplied directly as the `key` string.

This removes the need for a separate:

```text id="uoq4iw"
tpl.ExecuteTemplate(...)
```

API.

The guiding principle is:

> One operation executes template output; resolution determines where the template comes from.

---

# 2. Execution Modes

The three execution modes are:

```text id="fd8tl3"
typed compile-time execution
    tpl.BlogPost(w, post)

dynamic registered execution
    tpl.Execute(w, "BlogPost", post)

dynamic path execution
    tpl.Execute(w, r.URL.Path, post)

ad-hoc source execution
    tpl.Execute(w, `<h1>{{.Title}}</h1>`, post)
```

All modes ultimately render to an `io.Writer`.

---

# 3. Canonical Signature

```gpp id="247o0v"
tpl.Execute(
    w io.Writer,
    key string,
    args ...any,
) error
```

`w` may be any `io.Writer`.

`key` is interpreted according to the resolution rules defined below.

`args` contain the template data arguments.

---

# 4. Resolution Order

`tpl.Execute` resolves the supplied `key` in a deterministic order.

Canonical resolution:

```text id="rwum67"
1. path candidate?
       attempt registered path lookup

2. registered template name exists?
       execute registered template

3. otherwise
       treat key as template source
       parse and execute it
```

More precisely:

```text id="sbw0mw"
if key is path-shaped:
    if a Path(...) registration matches:
        execute matching registered template
    otherwise:
        continue to source fallback

else if key exactly matches a registered template name:
    execute that registered template

otherwise:
    parse key as inline html/template source
    execute the parsed template
```

Registered templates therefore take precedence over source fallback.

---

# 5. Registered Name Execution

Given:

```gpp id="k6a814"
template BlogPost(post Post) {
    <h1>{{.Title}}</h1>
}
```

this:

```gpp id="e9hqma"
tpl.Execute(w, "BlogPost", post)
```

executes the registered `BlogPost` template.

The string is not interpreted as literal source because a registered template with that name exists.

---

# 6. Path Execution

Given:

```gpp id="wychq5"
template BlogPost(post Post) @{
    tpl.Path("/blog/{id}")
} {
    <h1>{{.Title}}</h1>
}
```

this:

```gpp id="6msgrt"
tpl.Execute(w, "/blog/42", post)
```

may resolve `/blog/42` against the registered `Path` metadata and execute `BlogPost`.

This enables:

```gpp id="69srxj"
tpl.Execute(w, r.URL.Path, post)
```

without requiring a separate path-specific API.

---

# 7. Inline Source Fallback

If no registered template name or path resolves the supplied string, `Execute` treats the string itself as template source.

Example:

```gpp id="6eaq2j"
tpl.Execute(
    w,
    `<h1>{{.Title}}</h1>`,
    post,
)
```

The string is parsed with Go's standard `html/template` parser and executed immediately.

Another valid example:

```gpp id="l76r5i"
tpl.Execute(
    w,
    `
    <!doctype html>
    <html>
        <body>
            <h1>{{.Message}}</h1>
        </body>
    </html>
    `,
    data,
)
```

---

# 8. Plain Text Is Valid Source

Because ordinary literal text is valid `html/template` input:

```gpp id="jiv8bd"
tpl.Execute(w, "Hello world", nil)
```

is valid.

If no registered template named:

```text id="dh5ztp"
Hello world
```

exists, the supplied string is treated as source and writes:

```text id="xv3gqq"
Hello world
```

This behavior is intentional.

---

# 9. Missing Name Semantics

Because source fallback exists:

```gpp id="a8s7pi"
tpl.Execute(w, "MissingTemplate", data)
```

does **not necessarily mean**:

> execute a registered template named `MissingTemplate` or fail.

If no such registered template exists, the string:

```text id="qs8971"
MissingTemplate
```

is valid inline template source and will be executed as literal output.

This is part of the unified execution model.

Applications that specifically need to test registry membership should use registry inspection.

For example:

```gpp id="7kfb46"
tpl.Has("BlogPost")
```

if such inspection is provided.

The execution API itself does not distinguish caller intent beyond resolution precedence.

---

# 10. Why Registered Templates Win

Suppose a template exists:

```gpp id="xvpc0h"
template Error(data ErrorData) {
    ...
}
```

Then:

```gpp id="hs3omr"
tpl.Execute(w, "Error", data)
```

must execute the registered template rather than output the literal string:

```text id="dj6ufg"
Error
```

Therefore exact registry matches always take precedence over inline-source interpretation.

---

# 11. Path-Shaped Strings

A string containing `/` may represent either:

```text id="25fg45"
a registered path
or
literal template source
```

Example registered path:

```gpp id="fqq6ms"
tpl.Execute(w, "/blog/42", post)
```

Example literal source:

```gpp id="9hjqj5"
tpl.Execute(w, `<a href="/blog">Blog</a>`, nil)
```

Therefore merely containing `/` must not permanently classify a string as a path.

The correct behavior is:

```text id="k4w4or"
attempt path match first
if no registered path matches
fall back to inline source
```

This avoids making ordinary HTML strings invalid.

---

# 12. Path Matching Still Uses Registry Rules

Path matching continues to use the existing rules for:

```text id="1bxpez"
literal paths
named parameters
static-path precedence
ambiguity rejection
```

For example:

```gpp id="iv1ymf"
@{tpl.Path("/blog/archive")}
```

beats:

```gpp id="19p0z7"
@{tpl.Path("/blog/{id}")}
```

for:

```text id="ims2zn"
/blog/archive
```

If no registered path matches, inline source fallback is attempted.

---

# 13. Inline Source Uses `html/template`

Inline fallback uses:

```go id="mx78wv"
html/template
```

not a custom Go++ template parser.

Therefore standard syntax works:

```gotemplate id="e55mak"
{{.Name}}

{{if .Admin}}
    ...
{{end}}

{{range .Items}}
    ...
{{end}}

{{with .User}}
    ...
{{end}}
```

The same rule applies as elsewhere:

> Go++ does not reinvent the template body language.

---

# 14. HTML Escaping

Inline source execution uses standard `html/template` contextual escaping.

Example:

```gpp id="a6yloe"
tpl.Execute(
    w,
    `<div>{{.Body}}</div>`,
    page,
)
```

must HTML-escape unsafe content in `page.Body`.

Inline source fallback must not silently use `text/template`.

---

# 15. `tpl.Funcs`

Inline source execution uses the same function registry as registered templates.

Example:

```gpp id="3w98sk"
tpl.Funcs.Add("money", FormatMoney)
```

then:

```gpp id="hxxn4k"
tpl.Execute(
    w,
    `<strong>{{money .Price}}</strong>`,
    product,
)
```

works normally.

The same function environment applies to:

```text id="y08sdt"
compile-time templates
runtime-loaded templates
reloaded templates
inline source execution
```

---

# 16. Calling Registered Templates from Inline Source

Inline source should use the same generated template-call adapters available to ordinary template bodies.

Therefore, if:

```gpp id="0ai2nj"
template BlogPost(post Post) {
    <article>
        <h1>{{.Title}}</h1>
    </article>
}
```

is registered, this should work:

```gpp id="cyadkt"
tpl.Execute(
    w,
    `
    <section>
        {{BlogPost .}}
    </section>
    `,
    post,
)
```

The inline template can therefore compose with registered templates.

---

# 17. No Registry Mutation from Inline Source

When `Execute` falls back to source parsing:

```gpp id="d8v4an"
tpl.Execute(w, source, data)
```

the parsed inline source is not added to the template registry.

It does not gain a name.

It does not gain `Path` metadata.

It does not become available through later registry lookups.

Inline execution is ephemeral.

---

# 18. No Reload Participation for Inline Source

Inline source does not create a source descriptor.

Therefore it does not participate in:

```gpp id="ryvixd"
tpl.Reload()
```

`Reload` applies only to registered reloadable sources such as those introduced through:

```gpp id="f4w035"
tpl.LoadFile(...)
tpl.LoadDir(...)
```

or compiler-managed development `.gpp.tpl` files.

---

# 19. Caching Inline Source

Repeated parsing of identical source strings may be expensive.

`gpp/tpl` may internally cache parsed inline templates.

Conceptually:

```text id="9aylzl"
source string
    ↓
cache lookup
    ↓
parsed html/template
    ↓
execute
```

This cache is an implementation detail.

It does not make the source a registered application template.

---

# 20. Inline Cache Key

If inline parsing is cached, the cache key must include all parse-affecting state.

At minimum:

```text id="sylh38"
source string
tpl.Funcs generation
relevant template options
registered template-call adapter generation where required
```

This prevents stale parsed templates from using outdated helper registrations.

---

# 21. `tpl.Funcs` Changes

If:

```gpp id="59xr10"
tpl.Funcs.Add(...)
tpl.Funcs.Remove(...)
```

changes the function environment, cached inline parses must either:

```text id="cyafwi"
be invalidated
or
be versioned
```

A simple implementation may maintain a function-registry generation counter.

---

# 22. Registry Changes and Inline Composition

If inline source may call registered templates:

```gotemplate id="wcpzmw"
{{BlogPost .}}
```

then inline parse caching must also account for changes to the available registered template adapters.

For example:

```text id="f8brcj"
LoadFile
LoadDir
Reload
```

may change the callable template environment.

The implementation must ensure cached inline templates remain semantically valid.

---

# 23. Error Handling

`tpl.Execute` always returns ordinary `error`.

Possible failures include:

```text id="wemuje"
registered template execution error
path-match execution error
inline template parse error
unknown template function
template execution error
invalid field/method access
wrong dynamic argument shape
writer error
```

Normal Go++ automatic error promotion applies:

```gpp id="gdrp2w"
tpl.Execute(w, key, data)
```

Explicit capture remains possible:

```gpp id="prmwgl"
err := tpl.Execute(w, key, data)
```

---

# 24. Resolution Errors

Because inline source is the final fallback, failure to find a registered name or path is not itself an error.

For example:

```gpp id="3nqkft"
tpl.Execute(w, "Something", data)
```

falls through to inline parsing.

Only if parsing or execution of `"Something"` fails does `Execute` return an error.

Since plain text is valid source, many unmatched strings will execute successfully as literal output.

This is deliberate.

---

# 25. Server-Owned Internal Templates

The unified fallback is especially useful for `gpp/http.Server`.

The server can define:

```gpp id="k36ku3"
const defaultErrorTemplate = `
<!doctype html>
<html>
<body>
    <h1>Internal Server Error</h1>
    <p>{{.Message}}</p>
</body>
</html>
`
```

and execute it directly:

```gpp id="ccph6x"
tpl.Execute(w, defaultErrorTemplate, data)
```

No registration step is required.

No globally visible template name is created.

---

# 26. Registered Override Pattern

The same API also makes framework defaults easy to override dynamically.

For example:

```gpp id="lzjnbb"
template Error(data http.ErrorData) {
    ...
}
```

An HTTP server may hold:

```gpp id="9ck98j"
errorTemplate := "Error"
```

then:

```gpp id="8ot1j8"
tpl.Execute(w, errorTemplate, data)
```

If `Error` is registered, the application template executes.

If `errorTemplate` instead contains literal template source, that source executes.

The caller does not need separate code paths.

---

# 27. Dynamic Template Selection

This enables useful runtime patterns.

For example:

```gpp id="e54cu1"
template := server.ErrorTemplate(err)

tpl.Execute(w, template, data)
```

`template` may contain:

```text id="zutimh"
registered name
registered path
inline template source
```

and `Execute` handles resolution.

This makes framework extension points particularly simple.

---

# 28. Typed Templates Remain Preferred

Unified dynamic execution does not replace typed generated methods.

When a template is known statically:

```gpp id="wsakeo"
tpl.BlogPost(w, post)
```

remains preferred over:

```gpp id="0i2qs2"
tpl.Execute(w, "BlogPost", post)
```

because the typed form provides stronger compile-time checking and better tooling.

The distinction remains:

```text id="xjgl8z"
tpl.BlogPost(...)
    compile-time dispatch

tpl.Execute(...)
    runtime dispatch / source execution
```

---

# 29. Interaction with Multiple Arguments

For a registered typed template:

```gpp id="ms5rfc"
template UserPage(user User, posts []Post) {
    ...
}
```

dynamic execution may use:

```gpp id="qx5nfu"
tpl.Execute(w, "UserPage", user, posts)
```

The registry knows the declared signature and validates argument count/types at runtime where necessary.

For inline source, arguments must be transformed into the same deterministic template data model used elsewhere.

---

# 30. Single Inline Argument

For:

```gpp id="h8sclf"
tpl.Execute(
    w,
    `<h1>{{.Title}}</h1>`,
    post,
)
```

the template dot:

```text id="gqsorm"
.
```

refers directly to `post`.

This matches one-parameter declared-template behavior.

---

# 31. Multiple Inline Arguments

For:

```gpp id="ux9ii1"
tpl.Execute(
    w,
    source,
    user,
    posts,
)
```

the runtime must construct the same deterministic multi-argument carrier used for dynamic registered execution.

However, because inline source has no declared parameter names, positional names cannot be inferred naturally.

Therefore v1 should strongly prefer one data argument for inline source.

Canonical inline usage:

```gpp id="68yydg"
tpl.Execute(w, source, data)
```

where `data` is a struct, class, record, map, or other suitable aggregate.

The variadic signature remains necessary for registered typed templates.

---

# 32. Inline Source Arity Rule

For clarity, v1 should define:

```text id="y90jg0"
registered template:
    args matched against declared template signature

inline source:
    zero or one data argument preferred/supported directly
```

Possible canonical rules:

```gpp id="qntokj"
tpl.Execute(w, source)
tpl.Execute(w, source, data)
```

If more than one argument is supplied to inline source, the runtime may return an error unless an explicit carrier policy is later defined.

This avoids inventing unnamed fields such as:

```text id="so4kbq"
Arg0
Arg1
Arg2
```

without a compelling need.

---

# 33. Empty Source

An empty source string:

```gpp id="v9kakg"
tpl.Execute(w, "", nil)
```

is valid.

If no registered template has the empty name, the empty source parses and executes successfully, producing no output.

---

# 34. Source Diagnostics

Inline-source parse errors should identify themselves clearly.

For example:

```text id="6ssrye"
inline template:4:17:
unexpected {{end}}
```

Line and column information should be retained wherever the Go parser provides it.

Registered `.gpp.tpl` sources should continue to report their actual source filename.

---

# 35. Concurrency

`tpl.Execute` must be safe for concurrent use.

This applies to all resolution paths:

```text id="akgv62"
registered name execution
path execution
inline parse/cache execution
```

Applications should not require explicit locking around template execution.

---

# 36. No HTTP Knowledge

`tpl.Execute` does not know whether `key` came from:

```text id="9eaoae"
r.URL.Path
application configuration
database
framework default
literal source
user-selected theme
```

It simply performs template resolution and execution.

HTTP policy remains in `gpp/http.Server`.

---

# 37. Security Considerations

Inline template source must be treated as executable template configuration, not plain untrusted content.

Because Go templates may invoke registered functions:

```gotemplate id="b9pewu"
{{helper .}}
```

applications should not blindly pass arbitrary untrusted user-provided template source to `tpl.Execute` if registered functions expose sensitive behavior.

`html/template` provides HTML escaping, but escaping is not a sandbox.

This is especially relevant for CMS or customer-editable template features.

---

# 38. Revised Minimal Public API

The core `gpp/tpl` API becomes:

```gpp id="r97hf7"
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

Function registry:

```gpp id="qozqwo"
tpl.Funcs.Add(fn)
tpl.Funcs.Add(name, fn)

tpl.Funcs.Has(name)
tpl.Funcs.Remove(name)
```

Generated typed members:

```gpp id="48q5wl"
tpl.<TemplateName>(
    w io.Writer,
    args...
) error
```

Annotation:

```gpp id="fdf5yj"
tpl.Path(pattern string)
```

There is no separate:

```text id="90kl88"
tpl.ExecuteTemplate
```

in v1.

---

# 39. Resolution Summary

Given:

```gpp id="51jc5k"
tpl.Execute(w, key, data)
```

the runtime behaves conceptually as:

```text id="vzvlbo"
key
 ↓
registered path match?
 ├─ yes → execute registered path template
 │
 └─ no
      ↓
registered name match?
 ├─ yes → execute registered named template
 │
 └─ no
      ↓
parse key as html/template source
      ↓
execute inline source
```

The implementation may optimize classification and lookup internally, but visible behavior must remain deterministic.

---

# 40. Examples

## Registered name

```gpp id="7zre6f"
tpl.Execute(w, "BlogPost", post)
```

## Registered path

```gpp id="4w8lk7"
tpl.Execute(w, r.URL.Path, post)
```

## Inline HTML

```gpp id="kjfsvx"
tpl.Execute(
    w,
    `<h1>{{.Title}}</h1>`,
    post,
)
```

## Plain text

```gpp id="anjjhz"
tpl.Execute(w, "Hello world", nil)
```

## Framework error page

```gpp id="1qbgsy"
tpl.Execute(w, defaultErrorTemplate, errorData)
```

## Inline composition with registered template

```gpp id="px72rr"
tpl.Execute(
    w,
    `
    <main>
        {{BlogPost .}}
    </main>
    `,
    post,
)
```

---

# 41. Design Principle

The API now follows a single rule:

> If the caller has something that identifies or contains a template, pass it to `tpl.Execute`.

The runtime first tries to resolve it through known template metadata.

If no registered template applies, the value itself becomes template source.

This keeps the public surface small while supporting:

```text id="oh8myd"
typed application templates
dynamic template-name dispatch
URL/path dispatch
framework-owned internal templates
database/configuration template strings
small ad-hoc HTML fragments
```

without introducing separate execution functions.

---

# 42. Final Model

```text id="o9z6jg"
compile-time declaration
    template BlogPost(post Post) { ... }

typed execution
    tpl.BlogPost(w, post)

dynamic registered execution
    tpl.Execute(w, "BlogPost", post)

path execution
    tpl.Execute(w, "/blog/42", post)

inline source execution
    tpl.Execute(w, `<h1>{{.Title}}</h1>`, post)

runtime registration
    tpl.LoadFile(...)
    tpl.LoadDir(...)

reload
    tpl.Reload()

helpers
    tpl.Funcs.Add(...)
```

The underlying body language remains Go's `html/template`.

Go++ adds typed declarations, lookup, registration, loading, reload, and ergonomic execution around it.
