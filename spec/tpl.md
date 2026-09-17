# Go++ Template System Specification

## 1. Overview

Go++ provides a first-class template system built around the official package:

```gpp
import "gpp/tpl"
```

The design goals are:

* preserve standard Go `html/template` behavior and syntax;
* make templates first-class Go++ declarations;
* provide typed template parameters;
* render directly to `io.Writer`;
* support templates declared inside normal `.gpp` source;
* support external `.gpp.tpl` files;
* hot-reload external templates during development;
* automatically embed external templates in production builds;
* provide typed access to compile-time-known templates;
* allow additional templates to be loaded dynamically at runtime;
* support optional path-based template lookup through annotations;
* avoid requiring users to manage embedded template files manually;
* avoid inventing a new template language where Go already provides one.

The compiler provides only the language integration necessary for template declarations, typing, discovery, code generation, validation, and embedding.

Runtime template behavior lives primarily in `gpp/tpl`.

The underlying template parser and execution model should remain Go's standard `html/template` implementation wherever possible.

---

# 2. Core Design Principle

Go++ does **not** implement a new templating language.

The boundary is:

```text
Go++ owns:
    template declarations
    typed parameters
    annotations
    generated tpl.Name(...) bindings
    dynamic registry
    path lookup
    source discovery
    hot reload
    production embedding
    compile-time validation where possible

Go html/template owns:
    {{if}}
    {{else}}
    {{range}}
    {{with}}
    {{end}}
    pipelines
    dot semantics
    variables
    template functions
    whitespace trimming
    contextual HTML escaping
    parsing of template actions
    runtime template execution
```

Guiding rule:

> If standard `html/template` already expresses something well, Go++ should not replace it.

Go++ primarily fixes the stringly, boilerplate-heavy parts around declaration, typing, registration, composition, loading, lookup, and deployment.

---

# 3. Template Declarations

A template is declared using the `template` keyword:

```gpp
template BlogPost(post Post) {
    <article>
        <h1>{{.Title}}</h1>
        <div>{{.Body}}</div>
    </article>
}
```

Templates may appear in either:

```text
.gpp
.gpp.tpl
```

files.

For example:

```gpp
package blog

class Post {
    Title string
    Body  string
}

template BlogPost(post Post) {
    <article>
        <h1>{{.Title}}</h1>
        <div>{{.Body}}</div>
    </article>
}
```

A `template` declaration is a normal package-level Go++ declaration.

---

# 4. `.gpp.tpl` Files

External template source uses the extension:

```text
.gpp.tpl
```

For example:

```text
blog/
    post.gpp
    blog.gpp.tpl
    admin.gpp.tpl
```

A `.gpp.tpl` file belongs to the Go++ source system and is not merely an arbitrary asset.

This distinction is important:

```text
.gpp
    ordinary Go++ source, optionally containing templates

.gpp.tpl
    Go++ template-oriented source

.go
    native Go source
```

`.gpp.tpl` files may contain one or more template declarations.

Example:

```gpp
template Blog(posts []Post) {
    ...
}

template BlogPost(post Post) {
    ...
}
```

---

# 5. Parser Architecture

The template system should reuse Go's standard template parser rather than implementing a second template parser.

The intended pipeline is:

```text
.gpp / .gpp.tpl
        ↓
Go++ outer parser
        ↓
template declaration metadata + raw template body
        ↓
Go html/template parser
        ↓
Go template AST
        ↓
Go++ semantic validation / registration
        ↓
html/template execution
```

The Go++ parser is responsible for the outer declaration:

```gpp
template BlogPost(post Post) @{
    tpl.Path("/blog/{id}")
} {
    ...raw template body...
}
```

It extracts:

```text
name
parameters
parameter types
annotations
layout metadata
source location
raw template body
```

The body itself is then parsed using the standard Go template parser.

---

# 6. Raw Template Body Mode

Inside a `.gpp` file, the contents of a template body are not parsed as ordinary Go++ statements.

Once the compiler encounters:

```gpp
template Name(...) {
```

the parser enters raw-template-body mode.

For example:

```gpp
template Welcome(user User) {
    <section class="hero">
        {{if .Active}}
            <h1>{{.Name}}</h1>
        {{end}}
    </section>
}
```

The HTML and `{{...}}` actions are captured as template source rather than being lexed as ordinary Go++ code.

The compiler must reliably detect the end of the template declaration and then return to normal Go++ parsing.

The exact internal scanner implementation is compiler-defined.

---

# 7. Template Body Language

The body of a Go++ template follows standard Go `html/template` semantics as closely as possible.

Literal text remains literal:

```gpp
template Welcome(user User) {
    <h1>Welcome</h1>
}
```

Go template actions remain available unchanged:

```gpp
template Blog(data BlogData) {
    {{if .User}}
        <h1>Hello {{.User.Name}}</h1>
    {{else}}
        <h1>Hello</h1>
    {{end}}

    {{range .Posts}}
        <article>
            <h2>{{.Title}}</h2>
        </article>
    {{end}}
}
```

Standard constructs include:

```text
{{if ...}}
{{else}}
{{end}}

{{range ...}}
{{with ...}}

pipelines
variables
template functions
whitespace trimming
HTML escaping
```

Go++ does not introduce replacement syntax for control flow.

---

# 8. Template Calls Inside Templates

Calls to other Go++ templates use standard Go-template command syntax.

No parentheses are required.

Example:

```gpp
template Post(post Post) {
    <article>
        <h2>{{.Title}}</h2>
    </article>
}

template Blog(posts []Post) {
    {{range .}}
        {{Post .}}
    {{end}}
}
```

Inside the `range`, `.` is the current `Post`.

General forms:

```gotemplate
{{Header}}
{{Post .}}
{{Post .Featured}}
{{Card .User true}}
```

A zero-argument template call is:

```gotemplate
{{Header}}
```

A one-argument call is:

```gotemplate
{{Post .}}
```

A multiple-argument call is:

```gotemplate
{{Card .User true}}
```

Go++ should not invent function-style template syntax such as:

```text
{{Post(.)}}
```

for this purpose.

---

# 9. How Template Calls Are Implemented

Standard Go template syntax already treats:

```gotemplate
{{Post .}}
```

as a call to a template function named `Post`.

Go++ should take advantage of this rather than inventing a new parser rule.

For compile-time-known templates, the compiler/runtime may register generated callable adapters in the template function map.

Conceptually:

```gpp
template Post(post Post) {
    ...
}
```

makes a template-call adapter named:

```text
Post
```

available to the underlying Go template parser/runtime.

Therefore:

```gotemplate
{{Post .}}
```

can remain valid standard Go-template command syntax.

Go++ may then add semantic validation over the parsed template AST to ensure that known template calls are compatible with declared signatures.

---

# 10. Compile-Time Template Call Validation

Where enough type information is statically available, Go++ should validate calls between compile-time-known templates.

For example:

```gpp
template Post(post Post) {
    ...
}

template Blog(users []User) {
    {{range .}}
        {{Post .}}
    {{end}}
}
```

should produce a compile-time diagnostic because `.` is a `User` while `Post` expects `Post`.

Example:

```text
blog.gpp.tpl:12:
template Post expects Post
current value has type User
```

This validation is an additional Go++ layer over the standard template AST.

It should not require replacing Go's parser.

---

# 11. Typed Parameters

Template declarations may have typed parameters:

```gpp
template BlogPost(post Post) {
    ...
}
```

The declared parameters form the compile-time contract of the template.

For compile-time-known templates, Go++ validates arguments passed from Go++ code.

For example:

```gpp
tpl.BlogPost(w, post)
```

must pass a value compatible with `Post`.

A template may have multiple parameters:

```gpp
template UserPage(user User, posts []Post) {
    ...
}
```

with:

```gpp
tpl.UserPage(w, user, posts)
```

---

# 12. Template Data Model

For a one-parameter template:

```gpp
template BlogPost(post Post) {
    <h1>{{.Title}}</h1>
}
```

the standard Go template dot:

```text
.
```

refers directly to `post`.

Therefore:

```gotemplate
{{.Title}}
```

accesses `post.Title`.

For multiple parameters, the compiler/runtime must provide a deterministic carrier value compatible with standard Go-template dot semantics.

For:

```gpp
template UserPage(user User, posts []Post) {
    ...
}
```

the body may conceptually access:

```gotemplate
{{.user.Name}}

{{range .posts}}
    ...
{{end}}
```

The exact generated carrier type is implementation-defined.

Single-parameter templates should always bind `.` directly to that parameter value.

---

# 13. Rendering Model

Every template renders directly to an `io.Writer`.

Conceptually:

```gpp
template BlogPost(post Post) {
    ...
}
```

behaves like:

```go
func BlogPost(w io.Writer, post Post) error
```

The writer is supplied by the caller:

```gpp
tpl.BlogPost(w, post)
```

This works naturally with:

```text
http.ResponseWriter
bytes.Buffer
files
gzip writers
network streams
custom io.Writer implementations
```

There is no required intermediate string or `[]byte`.

For example:

```gpp
func ShowPost(w http.ResponseWriter, id int64) {
    post := LoadPost(id)

    tpl.BlogPost(w, post)
}
```

Because the template renderer returns `error`, normal Go++ automatic error promotion applies.

Thus:

```gpp
tpl.BlogPost(w, post)
```

automatically throws a non-nil rendering error.

Explicit handling remains possible:

```gpp
err := tpl.BlogPost(w, post)
```

---

# 14. `tpl` Namespace

Compile-time-known templates are exposed through the package-level `tpl` namespace.

For example:

```gpp
template Blog(posts []Post) {
    ...
}

template BlogPost(post Post) {
    ...
}
```

produce typed calls such as:

```gpp
tpl.Blog(w, posts)
tpl.BlogPost(w, post)
```

`tpl` acts as both:

1. the official template runtime package;
2. the generated namespace for statically known templates.

Inside template bodies, the shorter Go-template form is used:

```gotemplate
{{BlogPost .}}
```

---

# 15. Static and Dynamic Rendering

Every compile-time-known template may be rendered in two ways.

Typed:

```gpp
tpl.BlogPost(w, post)
```

Dynamic:

```gpp
tpl.Execute(w, "BlogPost", post)
```

The typed form is preferred when the template is known at compile time.

It provides:

* compile-time template-name checking;
* compile-time argument-count checking;
* compile-time argument-type checking;
* better tooling and completion.

The dynamic form is useful when the template name is determined at runtime.

---

# 16. `tpl.Execute`

The core dynamic rendering API is:

```gpp
tpl.Execute(
    w io.Writer,
    key string,
    args ...any,
) error
```

`key` may identify a template either by name or by path.

The lookup rule is:

```text
key contains "/"
    -> path lookup

otherwise
    -> template-name lookup
```

Therefore:

```gpp
tpl.Execute(w, "BlogPost", post)
```

performs exact template-name lookup.

While:

```gpp
tpl.Execute(w, r.URL.Path, post)
```

performs template path lookup.

Example:

```gpp
tpl.Execute(w, "/blog/42", post)
```

may match:

```gpp
template BlogPost(post Post) @{
    tpl.Path("/blog/{id}")
} {
    ...
}
```

---

# 17. Template Names

Template declaration names must be valid Go++ identifiers.

For example:

```text
Blog
BlogPost
UserProfile
AdminDashboard
```

A template name may not contain `/`.

This makes `tpl.Execute` name-vs-path resolution unambiguous.

Thus:

```text
"BlogPost"
```

always means a template name.

While:

```text
"/blog/42"
```

always means a path.

---

# 18. Path Annotation

`gpp/tpl` provides:

```gpp
annotation (
    Path(pattern string) on template
)
```

Example:

```gpp
template BlogPost(post Post) @{
    tpl.Path("/blog/{id}")
} {
    ...
}
```

The annotation adds lookup metadata only.

It does **not** create an HTTP route.

HTTP routing remains the responsibility of `gpp/http` or user code.

For example:

```gpp
func ShowPost(w http.ResponseWriter, r *http.Request) {
    post := LoadPost(...)

    tpl.Execute(w, r.URL.Path, post)
}
```

---

# 19. Path Patterns

Version 1 supports:

```text
literal paths
named path segments
```

Examples:

```text
/blog
/blog/archive
/blog/{id}
/users/{user}
/users/{user}/posts/{post}
```

A named segment uses `{name}`.

The legacy `:name` spelling is accepted as a compatibility alias, but new
templates should use `{name}` so template paths and `gpp/http` routes share
the same notation.

For example:

```text
/blog/{id}
```

matches:

```text
/blog/1
/blog/42
/blog/hello
```

but not:

```text
/blog
/blog/42/comments
```

---

# 20. Path Matching Precedence

Path matching must be deterministic.

More specific static paths beat parameterized paths.

For example:

```gpp
@{tpl.Path("/blog/archive")}
```

takes precedence over:

```gpp
@{tpl.Path("/blog/{id}")}
```

for:

```text
/blog/archive
```

General precedence:

```text
static segment
    before
named parameter segment
```

Wildcard matching is not part of v1.

---

# 21. Ambiguous Paths

Ambiguous patterns must be rejected.

For example:

```gpp
@{tpl.Path("/blog/{id}")}
```

and:

```gpp
@{tpl.Path("/blog/{slug}")}
```

represent the same match pattern and must produce a compiler or runtime-load error.

Ambiguity must not depend on registration order.

---

# 22. Path Parameters

When a template is selected through path matching, captured path parameters are made available to the template runtime.

For:

```text
/blog/{id}
```

and:

```text
/blog/42
```

the captured parameter set is:

```text
id = "42"
```

Path parameters must not alter or wrap the declared template arguments.

A template helper provides access to them:

```gotemplate
{{param "id"}}
```

Example:

```gpp
template BlogPost(post Post) @{
    tpl.Path("/blog/{id}")
} {
    <div data-post-id="{{param "id"}}">
        <h1>{{.Title}}</h1>
    </div>
}
```

If the named parameter does not exist, `param` returns an empty string.

---

# 23. Compile-Time Templates

Templates discovered while compiling the application are compile-time templates.

These include:

* templates declared in `.gpp`;
* templates declared in `.gpp.tpl` files belonging to the application.

Compile-time templates receive generated typed bindings:

```gpp
tpl.Blog(w, posts)
tpl.BlogPost(w, post)
```

They are also registered for dynamic rendering:

```gpp
tpl.Execute(w, "BlogPost", post)
```

and path rendering:

```gpp
tpl.Execute(w, "/blog/42", post)
```

They are also callable from other template bodies:

```gotemplate
{{BlogPost .}}
```

---

# 24. Runtime-Loaded Templates

Additional `.gpp.tpl` sources may be loaded after the application has started.

Runtime loading uses an overloaded API.

Canonical forms:

```gpp
tpl.LoadFile(path string) error
tpl.LoadFile(files fs.FS, name string) error

tpl.LoadDir(path string) error
tpl.LoadDir(files fs.FS) error
```

Examples:

```gpp
tpl.LoadFile("themes/dark.gpp.tpl")
```

```gpp
tpl.LoadDir("customer-templates/")
```

```gpp
tpl.LoadDir(themeFS)
```

```gpp
tpl.LoadFile(themeFS, "dark.gpp.tpl")
```

All forms register discovered templates into the same runtime registry used by `tpl.Execute`.

---

# 25. Why Loading Uses Overloading

The operation is conceptually the same regardless of where the template source comes from.

Therefore Go++ overloading should express the distinction rather than encoding source type into function names.

Preferred:

```gpp
tpl.LoadDir("themes/")
tpl.LoadDir(themeFS)
```

rather than:

```gpp
tpl.LoadDir("themes/")
tpl.LoadFS(themeFS)
```

Likewise:

```gpp
tpl.LoadFile("theme.gpp.tpl")
tpl.LoadFile(themeFS, "theme.gpp.tpl")
```

This follows the general Go++ principle:

> same semantic operation + different argument types = overload where appropriate.

---

# 26. `tpl.LoadFile`

`LoadFile` loads one `.gpp.tpl` source.

Filesystem path:

```gpp
tpl.LoadFile("theme.gpp.tpl")
```

`fs.FS` source:

```gpp
tpl.LoadFile(themeFS, "theme.gpp.tpl")
```

The file may itself contain multiple template declarations.

All declarations are parsed and validated before registration.

Registration should be atomic where practical.

If one declaration fails:

```text
the load fails
the existing registry remains unchanged
```

rather than leaving partially registered templates.

---

# 27. `tpl.LoadDir`

`LoadDir` discovers and loads every `.gpp.tpl` file beneath a source tree.

Filesystem path:

```gpp
tpl.LoadDir("themes/")
```

Filesystem abstraction:

```gpp
tpl.LoadDir(themeFS)
```

Both forms recursively discover:

```text
*.gpp.tpl
```

Other files are ignored.

Discovery order must not affect semantics.

Duplicate names or path conflicts are errors regardless of filesystem traversal order.

---

# 28. `fs.FS` Loading

The overloaded `fs.FS` variants allow template sources to come from:

* Go embedded filesystems;
* application resource filesystems;
* ZIP-backed filesystems;
* custom `fs.FS` implementations;
* plugin-owned filesystems;
* remote-backed filesystem abstractions.

Example:

```gpp
embed (
    themes "themes/"
)

tpl.LoadDir(themes)
```

Because an embedded directory already lowers to a rooted `fs.FS`, no special template-specific filesystem API is needed.

---

# 29. Runtime Templates Do Not Gain Typed Members

Runtime loading occurs after compilation.

Therefore a runtime-loaded template cannot generate a new statically typed member such as:

```gpp
tpl.CustomerHome(...)
```

because that symbol did not exist when the application was compiled.

Thus:

```text
compile-time template
    tpl.Blog(...)
    tpl.Execute(..., "Blog", ...)

runtime template
    tpl.Execute(..., "CustomerHome", ...)
```

Typed members are generated only for templates known during compilation.

---

# 30. Runtime Template Type Resolution

Runtime-loaded templates may reference application types already known to the running application.

For example:

```gpp
template CustomerPost(post Post) {
    ...
}
```

is valid if `Post` is available to the template environment.

A runtime template may not introduce arbitrary new Go++ application types or native code requiring compilation.

Runtime template loading is template parsing and registration, not runtime Go++ compilation or dynamic linking.

---

# 31. Production Embedding

Compile-time `.gpp.tpl` files are automatically embedded by:

```bash
gpp build
```

No explicit user declaration is required.

The compiler may generate something conceptually equivalent to:

```go
//go:embed ...
var __gppTemplatesFS embed.FS
```

but this filesystem is compiler-private.

Users interact with templates through:

```gpp
tpl.Blog(...)
tpl.Execute(...)
```

rather than reading compiler-generated embedded files directly.

---

# 32. Relationship to `embed (...)`

The Go++ resource declaration:

```gpp
embed (
    staticFiles "static/"
    logo        "assets/logo.svg"
)
```

is separate from template source handling.

`embed (...)` means:

> expose arbitrary application resources as ordinary embedded data.

`.gpp.tpl` handling means:

> compile, validate, register, and package Go++ templates.

Therefore users do not need:

```gpp
embed (
    templates "templates/"
)
```

merely to deploy compile-time templates.

Compile-time `.gpp.tpl` files are embedded automatically.

---

# 33. Explicitly Embedded Template Files

A user may explicitly embed a directory containing `.gpp.tpl` files:

```gpp
embed (
    customerTemplates "customer/"
)
```

Those files remain ordinary embedded resources.

They do not automatically become registered templates.

To load them:

```gpp
tpl.LoadDir(customerTemplates)
```

For one specific file:

```gpp
tpl.LoadFile(customerTemplates, "invoice.gpp.tpl")
```

This makes explicit embedding compose naturally with the overloaded loading API.

---

# 34. Development Hot Reload

`gpp run` treats application `.gpp.tpl` files as development-time external template sources.

The compiler initially:

1. discovers template declarations;
2. validates signatures;
3. registers metadata;
4. parses bodies using Go's standard template parser;
5. retains source locations.

During development, template body changes may be reloaded without rebuilding or restarting the application.

Example:

```text
edit blog.gpp.tpl
save
refresh browser
see new output
```

---

# 35. Hot Reload Contract

The compile-time template signature forms part of the program contract.

For example:

```gpp
template BlogPost(post Post) {
    ...
}
```

may have its body changed during hot reload.

Changing the name, argument count, argument types, or other compile-time signature information requires recompilation.

Therefore:

```text
body-only change
    may hot reload

signature change
    requires rebuild
```

---

# 36. Reload Failures

A malformed or invalid updated template must not crash the development server.

Instead:

1. the updated body is parsed;
2. Go++ validation runs;
3. the new version is rejected if invalid;
4. the previous valid version remains registered;
5. a useful development diagnostic is returned.

The process itself should remain alive.

---

# 37. Production Behavior

Production binaries use the copies embedded during `gpp build`.

No external template directory is required at runtime.

Deployment may remain:

```text
one executable
```

unless the application deliberately invokes:

```gpp
tpl.LoadFile(...)
tpl.LoadDir(...)
```

to introduce additional runtime templates.

---

# 38. Template Composition

Templates may call other templates using normal Go-template command syntax.

Example:

```gpp
template BlogPost(post Post) {
    <article>
        <h2>{{.Title}}</h2>
    </article>
}

template Blog(posts []Post) {
    {{range .}}
        {{BlogPost .}}
    {{end}}
}
```

This is the canonical invocation syntax.

The compiler may type-check known template references but should not replace the underlying Go template parser.

---

# 39. Zero-Argument Template Calls

A zero-argument template is invoked simply by naming it:

```gpp
template Header() {
    <header>...</header>
}

template Page() {
    {{Header}}
}
```

Argument-bearing calls use whitespace-separated Go-template syntax:

```gotemplate
{{BlogPost .}}
{{Card .User true}}
```

Parentheses are not part of Go++ template invocation syntax.

---

# 40. Layouts

Go++ supports template composition through a layout relationship.

Canonical syntax:

```gpp
template Blog(posts []Post): Layout {
    ...
}
```

The child body is supplied to the layout through an implicit nested template named:

```text
body
```

Layout templates currently declare no parameters. The child receives the
original template arguments, renders its body into the implicit `body`
function, and then renders the layout around that output.

Example:

```gpp
template Layout() {
    <!doctype html>
    <html>
        <body>
            {{Header}}
            {{body}}
            {{Footer}}
        </body>
    </html>
}
```

and:

```gpp
template Blog(posts []Post): Layout {
    {{range .}}
        {{BlogPost .}}
    {{end}}
}
```

The writer is passed through the complete composition chain.

---

# 41. HTML Escaping

Templates use `html/template` semantics by default.

Interpolated values are contextually HTML-escaped.

Raw HTML requires an explicit opt-in representation compatible with standard Go template safety types.

Go++ must not silently disable standard escaping.

---

# 42. Standard Go Template Functions

Standard Go template facilities remain available.

The implementation may add functions provided by `gpp/tpl`, including:

```text
param
```

and generated adapters for known templates.

These additions should use the normal Go template function map where possible.

---

# 43. Path Metadata Is Not HTTP Metadata

Template metadata remains concerned with template lookup and rendering.

For example:

```gpp
@{tpl.Path("/blog/{id}")}
```

is template metadata.

These remain HTTP handler/server concerns:

```text
Content-Type
Cache-Control
authentication
authorization
rate limiting
timeouts
CORS
HTTP method
route registration
```

A template does not become an HTTP endpoint merely because it has a `Path`.

---

# 44. Annotation Target

The general Go++ annotation system must support:

```text
template
```

as a target.

Thus:

```gpp
annotation (
    Path(pattern string) on template
)
```

is legal.

---

# 45. Registry Conflicts

Template registration must reject conflicting names.

Runtime loading must also reject existing names unless an explicit replacement policy is added later.

Version 1 should prefer strict duplicate rejection.

Development hot reload is a special case for compiler-known templates and is not equivalent to arbitrary runtime replacement.

---

# 46. Package Scope

Template declarations belong to their Go++ package.

Templates within a package share that package's generated `tpl` namespace.

For example, package:

```text
blog
```

may expose:

```gpp
blog.tpl.BlogPost(...)
```

from another package, subject to normal visibility rules.

---

# 47. Generated Go

The exact lowering is implementation-defined.

A compile-time template might conceptually generate:

```go
func __gpp_tpl_BlogPost(
    w io.Writer,
    post Post,
) error
```

plus:

* `html/template` parsing;
* generated callable adapters;
* registry metadata;
* path metadata;
* embedded source registration.

The public Go++ surface remains:

```gpp
tpl.BlogPost(w, post)
```

---

# 48. Error Handling

All rendering and loading APIs return `error`.

Examples:

```gpp
tpl.BlogPost(w, post)

tpl.Execute(w, "BlogPost", post)

tpl.LoadFile("theme.gpp.tpl")
tpl.LoadFile(themeFS, "theme.gpp.tpl")

tpl.LoadDir("themes/")
tpl.LoadDir(themeFS)
```

Because of Go++ automatic trailing-error promotion, ordinary usage requires no explicit error boilerplate.

Explicit capture remains valid:

```gpp
err := tpl.LoadDir(themeFS)
```

---

# 49. `tpl.Execute` Errors

Dynamic rendering may fail for reasons including:

```text
unknown template name
no matching path
wrong argument count
incompatible argument type
template parse error
template execution error
writer error
```

Diagnostics should identify the template declaration and source location where practical.

---

# 50. Static Calls vs Dynamic Calls

Use:

```gpp
tpl.BlogPost(w, post)
```

when the template is known statically.

Use:

```gpp
tpl.Execute(w, name, post)
```

when the name is dynamic.

Use:

```gpp
tpl.Execute(w, r.URL.Path, post)
```

when selecting through `tpl.Path`.

Inside templates:

```gotemplate
{{BlogPost .}}
```

---

# 51. Minimal Public API

The initial `gpp/tpl` public API should remain deliberately small.

Dynamic rendering:

```gpp
tpl.Execute(w io.Writer, key string, args ...any) error
```

Runtime loading:

```gpp
tpl.LoadFile(path string) error
tpl.LoadFile(files fs.FS, name string) error

tpl.LoadDir(path string) error
tpl.LoadDir(files fs.FS) error
```

Generated typed members:

```gpp
tpl.<TemplateName>(w io.Writer, args...) error
```

Annotation:

```gpp
tpl.Path(pattern string)
```

Template helper:

```text
param
```

Underlying engine:

```text
html/template
```

Avoid adding speculative APIs until real use cases require them.

---

# 52. Non-Goals for Version 1

Version 1 does not need:

* a replacement for Go template control flow;
* a custom replacement for Go's template parser;
* a second template expression language;
* function-call parentheses inside templates;
* arbitrary Go++ compilation at runtime;
* dynamic generation of typed template methods;
* automatic HTTP route registration;
* template-level cache policy;
* template-level content-type policy;
* wildcard path routing;
* a public compiler-generated template filesystem;
* manual embedding declarations for normal compile-time templates;
* separate `LoadFS` naming.

---

# 53. Example Application

```gpp
package blog

import (
    "gpp/tpl"
    "net/http"
)

class Post {
    ID    int64
    Title string
    Body  string
}

template BlogPost(post Post) @{
    tpl.Path("/blog/{id}")
} {
    <article data-id="{{param "id"}}">
        <h1>{{.Title}}</h1>
        <div>{{.Body}}</div>
    </article>
}

template Blog(posts []Post) {
    {{range .}}
        {{BlogPost .}}
    {{end}}
}

func ShowPost(w http.ResponseWriter, r *http.Request) {
    post := LoadPost(r.URL.Path)

    tpl.Execute(w, r.URL.Path, post)
}
```

Static rendering:

```gpp
tpl.BlogPost(w, post)
```

Dynamic rendering:

```gpp
tpl.Execute(w, "BlogPost", post)
```

Runtime filesystem loading:

```gpp
tpl.LoadDir("themes/")
```

Embedded/custom filesystem loading:

```gpp
embed (
    themes "themes/"
)

tpl.LoadDir(themes)
```

Specific file from an `fs.FS`:

```gpp
tpl.LoadFile(themes, "dark.gpp.tpl")
```

---

# 54. Development and Production Lifecycle

Development:

```text
.gpp.tpl on disk
      ↓
gpp run
      ↓
Go++ parses declaration envelope
      ↓
html/template parses body
      ↓
Go++ validates/registers metadata
      ↓
watch/reload body changes
      ↓
tpl.Blog / tpl.Execute
```

Production:

```text
.gpp.tpl
      ↓
gpp build
      ↓
validate
      ↓
embed automatically
      ↓
single executable
      ↓
tpl.Blog / tpl.Execute
```

Runtime extension:

```text
path or fs.FS
      ↓
tpl.LoadFile / tpl.LoadDir
      ↓
parse .gpp.tpl declarations
      ↓
html/template parser
      ↓
dynamic registry
      ↓
tpl.Execute
```

---

# 55. Design Summary

The core model is:

```text
template declaration
    +
standard Go html/template body
    +
typed Go++ parameters
    +
io.Writer rendering
    +
generated tpl.Name() bindings
    +
standard Go-template invocation syntax
    +
dynamic tpl.Execute()
    +
optional tpl.Path()
    +
overloaded LoadFile / LoadDir
```

Compile-time templates provide:

```gpp
tpl.BlogPost(w, post)
```

Inside templates:

```gotemplate
{{BlogPost .}}
```

Dynamic templates provide:

```gpp
tpl.Execute(w, "CustomerPage", data)
```

Path-based lookup provides:

```gpp
tpl.Execute(w, r.URL.Path, data)
```

Runtime sources may be loaded naturally from either paths or `fs.FS`:

```gpp
tpl.LoadDir("themes/")
tpl.LoadDir(themeFS)

tpl.LoadFile("theme.gpp.tpl")
tpl.LoadFile(themeFS, "theme.gpp.tpl")
```

External `.gpp.tpl` files remain hot-reloadable during development and are automatically embedded into production binaries.

The compiler handles only the language-level integration required to make templates typed and first-class.

`gpp/tpl` handles registry, rendering, runtime loading, path matching, and integration with Go's standard `html/template`.
