# Standard library guide

The Go++ standard library is written to make the language useful for complete
applications, not only isolated syntax examples. The packages compose with
ordinary Go packages and use familiar interfaces such as `database/sql`,
`html/template`, `io/fs`, and `net/http`.

## HTTP servers

`gpp/http` wraps the standard server lifecycle while keeping handlers close to
the model they operate on:

```go
import (
    "fmt"
    http "gpp/http"
)

class App : http.Server @{http.GET("/hello/{name}")} {
    func Hello(ctx *http.Context) error {
        return ctx.Text(fmt.Sprintf("hello %s", ctx.Param("name")))
    }
}

func main() {
    app := App()
    if err := app.Listen(); err != nil {
        panic(err)
    }
}
```

### Routes and responses

Routes are annotations on methods. Supported response helpers include:

```go
ctx.Text("plain text")
ctx.JSON(record(ok: true))
ctx.JSON(201, record(created: true))
ctx.Template("Page", data)
```

`Context` exposes request, response, path parameters, query/form values,
headers, cookies, and request-scoped values. Returning an error lets the
server’s error boundary select the configured error response.

### Lifecycle

Server hooks are optional methods on the server class:

```go
func BeforeListen() error { ... }
func BeforeRequest(ctx *http.Context) error { ... }
func AfterRequest(ctx *http.Context) error { ... }
func BeforeShutdown() error { ... }
func AfterShutdown() error { ... }
```

If no port is configured, the server listens on a dynamically assigned port.
The server also handles interrupt-driven shutdown and can expose a custom
error template.

Read [HTTP](/reference/specifications/std.http) and
[HTTP lifecycle](/reference/specifications/std.http.lifecycle).

## Templates

Templates use standard `html/template` actions but can be declared in Go++:

```go
template Layout() {
    <!doctype html>
    <html>
    <body>
        <header>Go++ classifieds</header>
        <main>&#123;&#123;body&#125;&#125;</main>
    </body>
    </html>
}

template Listing(title string): Layout {
    <h1>&#123;&#123;.&#125;&#125;</h1>
}
```

The `: Layout` declaration makes the child content available through the
layout’s implicit `body` slot. Templates may be embedded in `.gpp` files or
kept in external `.gpp.tpl` sources for faster editing.

Execution is typed and can be static or dynamic:

```go
tpl.Listing(&output, "Bicycle")
tpl.Execute(&output, "/listings/42", listing)
```

Template paths support named segments such as `/listings/{id}`. The same path
metadata can be used by HTTP routing and documentation generation.

Read [templates](/reference/specifications/tpl),
[HTTP templates](/reference/specifications/tpl.http),
[template watch mode](/reference/specifications/tpl.watch), and
[ad-hoc templates](/reference/specifications/tpl.adhoc).

## ORM and SQLite

`gpp/orm` uses annotations and introspection to map a model to SQL. Models must
derive from `orm.Model`:

```go
import (
    "database/sql"
    orm "gpp/orm"
)

class Employee : orm.Model @{orm.Table("employees")} {
    Name string @{orm.Column("name")}
    Salary float64 @{orm.Column("salary")}
}

db := openDatabase()
employee := Employee(Name: "Ada", Salary: 120000)
db.Insert(&employee)

var found Employee
db.Get(&found, "id = $1", employee.ID)

var employees []Employee
db.Select(&employees, "salary > $1", 100000)
```

The database and transaction APIs share the same operations:

```go
tx, err := db.Begin()
try {
    tx.Insert(&employee)
    tx.Update(&employee)
    tx.Delete(&employee)
    tx.Commit()
} catch e {
    tx.Rollback()
    throw e
}
```

Use numbered `$1`, `$2`, ... placeholders in Go++ source. The ORM rewrites
them for the active driver, so the same query works with SQLite and
PostgreSQL. Lifecycle hooks such as `BeforeCreate`, `AfterCreate`,
`BeforeUpdate`, `AfterUpdate`, `BeforeDelete`, `AfterDelete`, and `Validate`
run around operations. Hooks receive the active executor when they need to
write related rows in the same transaction.

Read [the ORM specification](/reference/specifications/std.orm) and the
[`orm.gpp`](https://github.com/telgatech/gpp/blob/main/examples/orm.gpp) example.

## Serialization

Serialization is opt-in through standard-style packages:

```go
encoded, err := json.ToJSON(user)
decoded, err := json.FromJSON[User](encoded)

binary, err := gob.ToGOB(user)
copy, err := gob.FromGOB[User](binary)
```

JSON, YAML, and GOB support generated class methods, field tags, ignored
fields, renamed fields, and omitted empty fields where the format supports
them. The result remains compatible with the underlying Go encoders.

See [serialization](/reference/specifications/serialization) and
[GOB serialization](/reference/specifications/serialization.gob).

## OpenAPI and Swagger UI

HTTP annotations can generate an OpenAPI document from the same routes that
serve the application:

```go
class App : http.Server @{http.Prefix("/api"), http.OpenAPI, http.Swagger} {
    func Health(ctx *http.Context) error @{http.GET("/health")} {
        return ctx.JSON(record(ok: true))
    }
}
```

The generated OpenAPI document is available under the server’s configured
prefix, while the self-contained interactive UI is served at `/swagger`.
The UI can execute requests against the running service and displays route,
parameter, response, and schema information.

Read [OpenAPI and Swagger](/reference/specifications/openapi-swagger) and
[Swagger UI](/reference/specifications/swagger-ui).

## OAuth and OIDC

`gpp/http` includes an OAuth/OIDC flow with provider configuration, state,
PKCE, callback handling, normalized identity, and login/error hooks:

```go
class App : http.Server @{http.OAuth(http.OAuthProvider.Google)} {
    func OAuthLogin(ctx *http.Context, identity http.OAuthIdentity) {
        fmt.Println("authenticated", identity.Email)
    }

    func OAuthError(ctx *http.Context, provider string, err error) {
        fmt.Println("OAuth failed", provider, err)
    }
}
```

Read [HTTP OAuth](/reference/specifications/http.oauth) and
[`oauth.gpp`](https://github.com/telgatech/gpp/blob/main/examples/oauth.gpp).

## Testing

The `gpp/test` package adds suite discovery and metadata on top of ordinary Go
test execution:

```go
suite UserSuite {
    test "creates a user" @test.Tag("crud") @test.Priority("high") {
        assert.Equal("Ada", user.Name)
    }
}
```

Run selected tests from the CLI:

```bash
gpp test examples/testing.gpp
gpp test --tag crud examples/testing.gpp
gpp test --priority high examples/testing.gpp
```

See [testing](/reference/specifications/testing) and
[`testing.gpp`](https://github.com/telgatech/gpp/blob/main/examples/testing.gpp).

## Extensions for standard types

The prelude and standard extension packages make common operations read like
domain code:

```go
title := ctx.Request.FormValue("title").TrimSpace()
tags := values.Map(tag => tag.TrimSpace()).Filter(tag => !tag.IsBlank())
matched := email.Matches(`^[^@]+@[^@]+$`)
```

These are compile-time extensions, not monkey patches. They resolve to normal
Go calls and remain explicit in generated code.

Read [standard extensions](/reference/specifications/std.extensions) and
[regex extensions](/reference/specifications/std.extensions.regex).
