# Go++ HTTP Error Templates and Context Response Helpers Addendum

## 1. Overview

This addendum defines:

1. `http.Server.ErrorTemplate()`;
2. default server error-page handling;
3. status-aware `Context.JSON`;
4. status-aware `Context.Text`;
5. `Context.Template`;
6. interaction with `gpp/tpl.Execute`.

The responsibility boundary is:

```text
gpp/http.Server
    routing
    HTTP error policy
    default error template
    error response status

gpp/http.Context
    response helpers
    response status
    headers
    writer

gpp/tpl
    template lookup
    template parsing
    template execution
```

Templates remain ordinary package-level templates.

No receiver-bound or class-level template semantics are introduced.

---

# 2. `Server.ErrorTemplate`

`http.Server` provides an overridable virtual method:

```gpp
func ErrorTemplate() string
```

The default implementation returns built-in template source owned by `gpp/http`.

Conceptually:

```gpp
class Server {
    func ErrorTemplate() string {
        return defaultErrorTemplate
    }
}
```

Because Go++ methods are polymorphic by default, a derived server may override it:

```gpp
class App : http.Server {
    func ErrorTemplate() string {
        return "ErrorPage"
    }
}
```

No special template override mechanism is required.

---

# 3. What `ErrorTemplate()` Returns

`ErrorTemplate()` returns the value passed to:

```gpp
tpl.Execute(...)
```

Therefore it may return either:

```text
a registered template name
or
literal html/template source
```

Example registered template:

```gpp
func ErrorTemplate() string {
    return "ErrorPage"
}
```

Example inline source:

```gpp
func ErrorTemplate() string {
    return `
        <h1>{{.Code}} {{.Message}}</h1>
    `
}
```

The server does not need to distinguish between the two.

It simply executes:

```gpp
tpl.Execute(w, this.ErrorTemplate(), data)
```

---

# 4. Default Error Template

`gpp/http` should provide a simple built-in error template.

Conceptually:

```gotemplate
<!doctype html>
<html>
<head>
    <title>{{.Code}} {{.Message}}</title>
</head>
<body>
    {{if eq .Code 404}}
        <h1>Page not found</h1>
    {{else if eq .Code 403}}
        <h1>Forbidden</h1>
    {{else}}
        <h1>{{.Code}} {{.Message}}</h1>
    {{end}}
</body>
</html>
```

The built-in template is framework-owned source.

It does not need to be registered in the application's template namespace.

---

# 5. Error Template Data

The HTTP response status and template data are separate concepts.

For example:

```gpp
ctx.Template(
    404,
    this.ErrorTemplate(),
    data,
)
```

means:

```text
404
    HTTP response status

data
    template input exposed as `.`
```

The template data may be any suitable Go++ value.

There is no required `http.ErrorData` class.

---

# 6. Default Error Data

The server may construct an ordinary record for its default error template.

For example:

```gpp
data := record(
    Code:    code,
    Message: http.StatusText(code),
    Path:    ctx.Request.URL.Path,
)
```

and execute:

```gpp
ctx.Template(
    code,
    this.ErrorTemplate(),
    data,
)
```

Because record fields preserve normal Go capitalization/export semantics, exported fields such as `Code`, `Message`, and `Path` are naturally accessible to `html/template`.

Thus:

```gotemplate
{{.Code}}
{{.Message}}
{{.Path}}
```

works normally.

---

# 7. Record Field Visibility

Error-page records should use exported field names when template access is intended.

Preferred:

```gpp
record(
    Code: 404,
    Message: "Page not found",
    Path: "/missing",
)
```

rather than:

```gpp
record(
    code: 404,
    message: "Page not found",
)
```

Record fields follow normal Go++ / Go capitalization semantics.

Uppercase fields are exported.

Lowercase fields remain package-private.

---

# 8. Richer Error Data

Applications are not restricted to:

```gpp
record(Code: code)
```

They may pass richer data:

```gpp
record(
    Code:    code,
    Message: http.StatusText(code),
    Path:    ctx.Request.URL.Path,
    Request: ctx.Request,
)
```

or their own class/record:

```gpp
data := record(
    Code:       404,
    Message:    "Page not found",
    Path:       ctx.Request.URL.Path,
    RequestID:  ctx.Get("requestID"),
    SupportURL: "/support",
)
```

The template contract is determined by whatever data the server/application chooses to pass.

`gpp/http` should not force an unnecessary named DTO merely for error pages.

---

# 9. Application Error Template

An application may define:

```gpp
template ErrorPage(data record) {
    {{if eq .Code 404}}
        <h1>Sorry, that page does not exist.</h1>
    {{else if eq .Code 403}}
        <h1>You do not have access.</h1>
    {{else}}
        <h1>Something went wrong.</h1>
    {{end}}
}
```

and:

```gpp
class App : http.Server {
    func ErrorTemplate() string {
        return "ErrorPage"
    }
}
```

The exact record shape is inferred from the value supplied by the server/application according to normal Go++ record rules.

A named class may also be used if preferred.

---

# 10. Why `ErrorTemplate()` Takes No Status

The canonical signature remains:

```gpp
func ErrorTemplate() string
```

not:

```gpp
func ErrorTemplate(code int) string
```

The status code is normally part of the supplied template data:

```gpp
record(
    Code: code,
    ...
)
```

Therefore one template can handle all status codes:

```gotemplate
{{if eq .Code 404}}
    ...
{{else if eq .Code 403}}
    ...
{{else}}
    ...
{{end}}
```

This keeps the server override contract minimal.

---

# 11. Route Not Found

If no route matches a request, `Server` should produce a 404 using the normal error-template path.

Conceptually:

```gpp
route := FindRoute(r)

if route == nil {
    this.Error(ctx, 404, nil)
    return
}
```

The error handler constructs suitable data and executes:

```gpp
ctx.Template(
    404,
    this.ErrorTemplate(),
    data,
)
```

No separate `NotFoundTemplate` API is required.

---

# 12. Uncaught Handler Error

An uncaught Go++ error during request execution should default to HTTP 500.

Conceptually:

```gpp
try {
    route.Call(...)
} catch e {
    this.Error(ctx, 500, e)
}
```

The underlying error may be used for:

```text
logging
development diagnostics
metrics
```

It does not have to be exposed to the production template.

---

# 13. Production Safety

The default production error record should not expose sensitive internals unnecessarily.

A production response might use:

```gpp
record(
    Code:    code,
    Message: http.StatusText(code),
    Path:    ctx.Request.URL.Path,
)
```

Development mode may construct richer diagnostic data.

For example:

```gpp
record(
    Code:    code,
    Message: err.Error(),
    Path:    ctx.Request.URL.Path,
    File:    diagnostic.File,
    Line:    diagnostic.Line,
    Source:  diagnostic.Source,
)
```

The decision belongs to `gpp/http.Server`, not `gpp/tpl`.

---

# 14. Error Template Failure

Error rendering must not recurse indefinitely.

If:

```gpp
ctx.Template(
    code,
    this.ErrorTemplate(),
    data,
)
```

fails, the server must not invoke `ErrorTemplate()` again.

Instead it falls back to a minimal hardcoded response:

```text
500 Internal Server Error
```

Canonical chain:

```text
request error
    ↓
ErrorTemplate()
    ↓
template execution
    ↓
fails?
    ↓
minimal hardcoded 500
```

---

# 15. Render Before Committing Error Response

Error templates should preferably execute into a buffer before committing the HTTP response.

Conceptually:

```gpp
var buf bytes.Buffer

tpl.Execute(
    &buf,
    this.ErrorTemplate(),
    data,
)

ctx.Writer.WriteHeader(code)
buf.WriteTo(ctx.Writer)
```

If execution fails, the server still has an opportunity to send the minimal fallback response.

---

# 16. `http.Context` Response Helpers

`http.Context` should expose consistent helpers:

```gpp
ctx.JSON(...)
ctx.Text(...)
ctx.Template(...)
```

Each supports:

```text
default 200 response
or
explicit status code
```

---

# 17. `Context.JSON`

Canonical overloads:

```gpp
func JSON(value any) error

func JSON(
    status int,
    value any,
) error
```

Examples:

```gpp
ctx.JSON(user)
```

equivalent to:

```gpp
ctx.JSON(200, user)
```

and:

```gpp
ctx.JSON(201, user)
```

Behavior:

```text
status:
    supplied status, or 200

Content-Type:
    application/json

body:
    JSON encoding of value
```

---

# 18. `Context.Text`

Canonical overloads:

```gpp
func Text(value string) error

func Text(
    status int,
    value string,
) error
```

Examples:

```gpp
ctx.Text("ok")
```

equivalent to:

```gpp
ctx.Text(200, "ok")
```

Explicit status:

```gpp
ctx.Text(404, "not found")
```

Behavior:

```text
Content-Type:
    text/plain; charset=utf-8
```

---

# 19. `Context.Template`

For consistency, `http.Context` provides:

```gpp
ctx.Template(...)
```

Canonical overloads:

```gpp
func Template(
    key string,
    args ...any,
) error

func Template(
    data any,
) error

func Template(
    status int,
    key string,
    args ...any,
) error
```

Default status:

```text
200 OK
```

---

# 20. Template Response Examples

Registered template:

```gpp
ctx.Template(
    "BlogPost",
    post,
)
```

Explicit status:

```gpp
ctx.Template(
    200,
    "BlogPost",
    post,
)
```

Path-based execution:

```gpp
ctx.Template(post)
```

The data-only overload looks up a template using the current request URL path.
The template must declare a matching `tpl.Path` annotation. Captured path
parameters are made available through the template's `param` helper.

Inline source:

```gpp
ctx.Template(
    200,
    `<h1>{{.Title}}</h1>`,
    post,
)
```

All forms delegate to the unified:

```gpp
tpl.Execute(...)
```

behavior.

---

# 21. `Context.Template` Delegation

Conceptually:

```gpp
func Template(data any) error {
    return this.renderTemplate(
        200,
        this.Request.URL.Path,
        data,
    )
}

func Template(
    status int,
    key string,
    args ...any,
) error {
    ...
    tpl.Execute(
        writer,
        key,
        args...,
    )
}
```

The context handles:

```text
HTTP status
Content-Type
response commitment
```

`gpp/tpl` handles:

```text
registered-name lookup
Path lookup
inline-source fallback
html/template execution
```

---

# 22. Template Content Type

`Context.Template` should default to:

```text
text/html; charset=utf-8
```

because `gpp/tpl` uses `html/template`.

This is HTTP behavior and belongs in `gpp/http.Context`.

It does not belong in template annotations.

---

# 23. Template Buffering

`Context.Template` should preferably execute into a temporary buffer before committing headers.

Conceptually:

```gpp
var buf bytes.Buffer

tpl.Execute(
    &buf,
    key,
    args...,
)

ctx.Writer.Header().Set(
    "Content-Type",
    "text/html; charset=utf-8",
)

ctx.Writer.WriteHeader(status)

buf.WriteTo(ctx.Writer)
```

This prevents a parsing/execution error from committing a partially generated response.

---

# 24. `Context.JSON` Buffering

The same principle may be used for JSON where practical:

```text
encode first
then commit status/headers
then write
```

This prevents serialization failures from leaving a partially committed response.

The exact implementation strategy is runtime-defined.

---

# 25. Status Tracking

`Context` should track the response status.

For example:

```gpp
ctx.Status
```

Canonical meaning:

```text
0
    no response status committed yet

otherwise
    committed HTTP status
```

Response helpers set `ctx.Status` when committing their response.

This is useful for:

```gpp
AfterRequest(ctx)
```

logging, metrics, and diagnostics.

---

# 26. Response Commitment

Response helpers must respect normal Go HTTP semantics.

Once headers have been committed:

```text
the status cannot meaningfully be replaced
```

`Context` should avoid accidentally calling `WriteHeader` repeatedly.

It may internally track:

```text
Status
Written
```

or equivalent state.

---

# 27. Consistent Response API

The resulting API is:

```gpp
ctx.JSON(value)
ctx.JSON(status, value)

ctx.Text(value)
ctx.Text(status, value)

ctx.Template(key, args...)
ctx.Template(status, key, args...)
```

Examples:

```gpp
ctx.JSON(user)
ctx.JSON(201, user)

ctx.Text("ok")
ctx.Text(202, "accepted")

ctx.Template("BlogPost", post)
ctx.Template(404, "ErrorPage", data)
```

This is the canonical response-helper pattern.

---

# 28. Default Status Rule

When the explicit status overload is not used:

```text
JSON      -> 200
Text      -> 200
Template  -> 200
```

No status inference is performed.

For example, returning a newly created object does not automatically imply 201.

The application supplies non-200 statuses explicitly.

---

# 29. Error Propagation

All three response helpers return `error`:

```gpp
ctx.JSON(...)
ctx.Text(...)
ctx.Template(...)
```

Therefore normal Go++ automatic trailing-error promotion applies.

Typical handler:

```gpp
func Create(ctx *http.Context) {
    user := CreateUser(...)

    ctx.JSON(201, user)
}
```

No explicit error boilerplate is needed unless the handler wants to catch the failure.

---

# 30. JSON Handler Example

```gpp
func CreateUser(ctx *http.Context) {
    user := Create(...)

    ctx.JSON(
        201,
        user,
    )
}
```

---

# 31. Text Handler Example

```gpp
func Health(ctx *http.Context) {
    ctx.Text("ok")
}
```

---

# 32. Template Handler Example

```gpp
func ShowPost(
    ctx *http.Context,
    id int64,
) {
    post := LoadPost(id)

    ctx.Template(
        "BlogPost",
        post,
    )
}
```

---

# 33. Rich Error Record Example

An application error page may receive:

```gpp
data := record(
    Code:    404,
    Message: "Page not found",
    Path:    ctx.Request.URL.Path,
)
```

and:

```gpp
ctx.Template(
    404,
    this.ErrorTemplate(),
    data,
)
```

Template:

```gotemplate
<!doctype html>
<html>
<body>
    <h1>{{.Code}} {{.Message}}</h1>
    <p>{{.Path}}</p>
</body>
</html>
```

---

# 34. Minimal Error Record Example

Applications may also keep it minimal:

```gpp
ctx.Template(
    404,
    this.ErrorTemplate(),
    record(
        Code: 404,
    ),
)
```

or even:

```gpp
ctx.Template(
    404,
    this.ErrorTemplate(),
    404,
)
```

if the custom template expects a plain integer:

```gotemplate
{{if eq . 404}}
    <h1>Not found</h1>
{{end}}
```

`gpp/http` does not impose a universal template-data class.

---

# 35. Server Error Helper

`Server` should internally centralize error response handling.

Conceptually:

```gpp
func Error(
    ctx *Context,
    code int,
    err error,
) {
    data := record(
        Code:    code,
        Message: http.StatusText(code),
        Path:    ctx.Request.URL.Path,
    )

    ctx.Template(
        code,
        this.ErrorTemplate(),
        data,
    )
}
```

The exact method visibility/name is implementation-defined unless exposed publicly.

---

# 36. Request Flow

Conceptually:

```text
incoming request
    ↓
find route
    ↓
route missing?
    ├─ yes
    │    ↓
    │   Error(ctx, 404, nil)
    │
    └─ no
         ↓
       execute route
         ↓
       uncaught Go++ error?
         ├─ no → normal response
         └─ yes
              ↓
             Error(ctx, 500, err)
```

---

# 37. Error Response Flow

```text
Error(ctx, code, err)
    ↓
construct template data
    ↓
this.ErrorTemplate()
    ↓
ctx.Template(code, keyOrSource, data)
    ↓
tpl.Execute(...)
    ↓
success
    → commit requested status

failure
    → hardcoded minimal 500
```

---

# 38. No Special Error Template Type

The design intentionally does not introduce:

```text
http.ErrorData mandatory DTO
special error template declarations
class-bound templates
framework template inheritance
special ErrorPage override syntax
```

Customization uses mechanisms Go++ already has:

```text
normal virtual method override
normal template declarations
normal records/classes
tpl.Execute
```

---

# 39. Responsibility Boundary

Final ownership is:

```text
Server.ErrorTemplate()
    chooses the error template source/name

Server
    determines when an HTTP error response is needed

Context
    owns status, headers, writer, response helpers

tpl.Execute
    resolves and executes the template

record/class supplied as data
    defines what the template sees as `.`
```

---

# 40. Minimal API Summary

`http.Server`:

```gpp
func ErrorTemplate() string
```

`http.Context`:

```gpp
JSON(value any) error
JSON(status int, value any) error

Text(value string) error
Text(status int, value string) error

Template(
    key string,
    args ...any,
) error

Template(
    status int,
    key string,
    args ...any,
) error
```

Example error-page override:

```gpp
class App : http.Server {
    func ErrorTemplate() string {
        return "ErrorPage"
    }
}
```

Example template:

```gpp
template ErrorPage(data record) {
    {{if eq .Code 404}}
        <h1>Page not found</h1>
    {{else}}
        <h1>{{.Code}} {{.Message}}</h1>
    {{end}}
}
```

Example response:

```gpp
ctx.Template(
    404,
    this.ErrorTemplate(),
    record(
        Code:    404,
        Message: "Page not found",
        Path:    ctx.Request.URL.Path,
    ),
)
```

The core rule is:

> HTTP status is response metadata; template data is an independent value chosen by the server/application.
