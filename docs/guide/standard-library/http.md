# `gpp/http`

Starting an HTTP server in Go is easy. Turning it into an application means making a series of connected decisions: route registration, shared request behavior, authentication, static assets and templates, API documentation, TLS, error responses, and graceful termination. Those concerns tend to spread across handlers, middleware stacks, setup functions, and signal code.

`gpp/http` brings much of that application shape into one Go++ server class. Annotations declare routes and server options; lifecycle hooks provide places for app-wide behavior; the package wires the declarations into Go's `net/http` server. You can keep using ordinary Go handlers, middleware, listeners, and libraries when a deployment needs something more specific.

## Why use it

The goal is to make the routine shape of a service easy to see and maintain. A route lives beside its handler, a class-level annotation configures the server, and built-in integrations such as OAuth/OIDC and Swagger do not need hand-written infrastructure routes. The result is less setup code between the application and Go's HTTP primitives.

## Side by side: wire a service

A conventional Go service composes its mux, handler middleware, server, and shutdown path explicitly:

::: code-group
```go [Go++]
import http "gpp/http"

class App : http.Server @{
	http.IP("0.0.0.0"),
	http.Port(8080),
	http.Prefix("/api"),
	http.Files("/static/", "./public"),
	http.OpenAPI,
	http.Swagger
} {
	func User(ctx *http.Context) error @{http.GET("/users/{id}")} {
		user := loadUser(ctx.Param("id"))
		return ctx.JSON(user)
	}

	func CreateUser(ctx *http.Context) error @{http.POST("/users")} {
		return ctx.JSON(201, record(ok: true))
	}
}

func main() {
	app := App()
	app.Listen()
}
```

```go [Go]
mux := http.NewServeMux()
mux.Handle("GET /users/{id}", logging(authenticate(
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(loadUser(r.PathValue("id")))
	}),
)))

server := &http.Server{Addr: ":8080", Handler: mux}
go waitForSignalAndShutdown(server)
return server.ListenAndServe()
```
:::

`Listen()` creates the listener from the class configuration and runs the server. The annotations set the bind address, URL prefix, static-file mount, OpenAPI document, and Swagger UI. `GET`, `POST`, `PUT`, `PATCH`, and `DELETE` annotations attach routes to methods; the handler still receives a normal request context and can return ordinary Go errors.

**The two API-documentation annotations do a lot of work.** `http.OpenAPI` generates an OpenAPI 3.0.3 document for the declared routes at `/openapi.json`; `http.Swagger` serves the bundled Swagger UI at `/swagger` and connects it to that document. Open the UI, choose an operation, and use its **Try it out** control to send a request to the running API and inspect the response. You write no OpenAPI file, docs handler, Swagger page, JavaScript setup, or UI asset wiring. The same route annotations already used to run the service power this live API explorer; maintaining that infrastructure by hand can grow to hundreds or thousands of lines as an API expands.

## HTTP annotation catalog

These are the annotations declared by `gpp/http`. The first group is read by the current server runtime and replaces listener setup, mux registration, static-file wiring, or API-documentation plumbing with metadata beside the server and its methods.

| Annotation | Applies to | What it configures | Ceremony it removes |
| --- | --- | --- | --- |
| `http.IP(addr)` | Server class | TCP bind address | Avoids setting up a listener just to choose the interface. |
| `http.Port(port)` | Server class | TCP port | Avoids passing a separate address into listener setup. |
| `http.Unix(path)` | Server class | Unix-domain socket path | Selects a local socket without writing custom `net.Listen` setup. |
| `http.Prefix(path)` | Server class | Shared prefix for app routes | Keeps a common `/api` prefix out of every route declaration. |
| `http.GET(path)` | Handler method | GET route | Replaces explicit mux registration and keeps the route next to its handler. |
| `http.POST(path)` | Handler method | POST route | Same route-to-method declaration for writes and submissions. |
| `http.PUT(path)` | Handler method | PUT route | Same route-to-method declaration for full updates. |
| `http.PATCH(path)` | Handler method | PATCH route | Same route-to-method declaration for partial updates. |
| `http.DELETE(path)` | Handler method | DELETE route | Same route-to-method declaration for deletes. |
| `http.Files(path, dir)` | Server class | Static-file mount | Replaces manual `FileServer` and `StripPrefix` registration. |
| `http.OpenAPI(path)` | Server class | Generated OpenAPI endpoint; defaults to `/openapi.json` | Avoids maintaining a separate API description and serving handler. |
| `http.Swagger(path)` | Server class | Bundled Swagger UI; defaults to `/swagger` | Avoids adding Swagger assets, page setup, and spec-loading code. Requires `http.OpenAPI`. |
| `http.OAuth(options...)` | Server class | OAuth/OIDC provider login and callback routes | Replaces hand-written protocol routes and state/token-exchange plumbing; the app still chooses its user and session behavior. |

The package also declares the following policy and transport annotations, but the current router does not read them yet:

| Annotation | Intended purpose | Current behavior |
| --- | --- | --- |
| `http.WebSocket(path)` | Mark a method as a WebSocket endpoint. | Not registered by the current route dispatcher; it does not create a WebSocket route today. |
| `http.Auth` | Mark a server or method as requiring authentication. | Metadata only; it does not enforce authentication. |
| `http.NoAuth` | Mark a method as exempt from inherited authentication policy. | Metadata only; it does not change routing or auth behavior. |
| `http.Role(name)` | Declare a required role for a server or method. | Metadata only; it does not enforce role checks. |

Do not rely on `Auth`, `NoAuth`, or `Role` to protect a route yet. Use implemented OAuth/OIDC sign-in plus application-owned sessions and authorization middleware or checks. Likewise, WebSocket routes need a Go HTTP integration rather than this annotation for now.

## Routing and request context

Go++ builds on `http.ServeMux` and uses standard method-and-path behavior. Path parameters are declared in the route and read by name. The context also exposes query values and the underlying Go request and response writer:

```go
class App : http.Server @{http.Prefix("/api")} {
	func Search(ctx *http.Context) error @{http.GET("/search")} {
		term := ctx.Query("q")
		return ctx.JSON(record(term: term, results: search(term)))
	}

	func Hello(ctx *http.Context) error @{http.GET("/hello/{name}")} {
		return ctx.Text("Hello " + ctx.Param("name"))
	}
}
```

The context provides `Text`, `JSON`, `Template`, and `Redirect` response helpers. It retains `Request` and `Response` for code that needs the full `net/http` API. `Files(path, directory)` mounts a static directory, while `Prefix(path)` applies a common path prefix to application routes.

## Bind a request body into a class

Use a Go++ class as the typed shape for a JSON request. Its fields can use ordinary Go JSON tags, and `gpp/encoding` decodes the body into an instance:

```go
import (
	encoding "gpp/encoding"
	http "gpp/http"
)

class CreateUserRequest @{encoding.Serializable} {
	Name string `json:"name"`
	Email string `json:"email"`
}

class App : http.Server {
	func CreateUser(ctx *http.Context) error @{http.POST("/users")} {
		body := ctx.Request.Body.ReadAll()
		input := CreateUserRequest.FromJSON(body)
		if input.Name.Blank() {
			return ctx.JSON(400, record(message: "name is required"))
		}

		return ctx.JSON(201, record(name: input.Name, email: input.Email))
	}
}
```

This is the same basic flow as `json.NewDecoder(r.Body).Decode(&input)` in Go, with the request shape named as a Go++ class and decoding expressed as a typed function call. Read and decode errors propagate automatically; application validation remains explicit. Binding is explicit today: handlers receive `*http.Context`, and `gpp/http` currently declares no `Body`, `Query`, or `Path` argument annotation and does not inject extra method parameters. The OpenAPI generator can inspect extra method parameters as documentation metadata, but that does not bind them at runtime; keep route handlers to the context parameter until the dispatcher supports binding. Apply a request-size limit before reading bodies from untrusted clients, and validate the decoded value before using it.

## Shared request behavior and middleware

For common app-wide work, the server calls `BeforeRequest(ctx)` before a matched route and `AfterRequest(ctx)` after it. These hooks are useful for request-scoped values, metrics, auditing, and application checks:

```go
class App : http.Server {
	func BeforeRequest(ctx *http.Context) error {
		ctx.Set("request-id", newRequestID())
		return nil
	}

	func AfterRequest(ctx *http.Context) error {
		logRequest(ctx.Get("request-id"), ctx.Status)
		return nil
	}
}
```

Existing Go middleware remains usable. `BeforeListen()` runs after the underlying `HTTPServer` and its handler have been prepared, so it can wrap the handler with a standard middleware stack:

```go
class App : http.Server {
	func BeforeListen() error {
		this.HTTPServer.Handler = Logging(Recover(this.HTTPServer.Handler))
		return nil
	}
}
```

There is no special middleware annotation: use the hooks for the simple lifecycle cases and ordinary `net/http` middleware when you need composable or third-party behavior.

## OAuth and OIDC

OAuth 2.0 lets an application delegate authorization to an identity provider. OpenID Connect (OIDC) adds a standard identity layer to OAuth, so an application can receive a verified user identity as part of sign-in. `http.OAuth(...)` declares the provider once; `gpp/http` then creates the login and callback routes and runs the protocol flow.

For a built-in provider, the annotation supplies the provider's issuer and defaults:

```go
class App : http.Server @{
	http.OAuth(http.OAuthProvider.Google)
} {
	func OAuthLogin(ctx *http.Context, identity http.OAuthIdentity) {
		user := findOrCreateUser(identity.Provider, identity.ID, identity.Email)
		session := createSession(user)
		setSessionCookie(ctx.Response, session)
		ctx.Redirect("/", 302)
	}

	func OAuthError(ctx *http.Context, provider string, err error) {
		logOAuthFailure(provider, err)
		ctx.Redirect("/login?failed=1", 302)
	}
}
```

The provider configuration automatically reserves these routes:

| Route | What happens |
| --- | --- |
| `GET /auth/google` | Starts login, creates temporary state and PKCE values, adds an OIDC nonce when needed, and redirects the browser to Google. |
| `GET /auth/google/callback` | Checks the returned state, exchanges the authorization code, validates tokens and identity data, then calls `OAuthLogin`. |

The runtime also handles provider metadata discovery and callback URL construction. Its state cookie is temporary and protected with `HttpOnly`, `SameSite=Lax`, and `Secure` when the request is HTTPS. Set `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` in the service environment; the credentials stay outside source code.

You can request additional scopes or configure an issuer that is not one of the built-in providers:

```go
class App : http.Server @{
	http.OAuth(
		"company",
		"https://identity.example.com",
		"COMPANY_CLIENT_ID",
		"COMPANY_CLIENT_SECRET",
		"openid",
		"email",
	)
} {}
```

The `OAuthLogin` hook receives the normalized identity (and has a token-aware overload when the app needs provider tokens); `OAuthError` is the place to log or customize a failed login response. The library handles the protocol plumbing, while the application still decides whether the person maps to a local account, whether to create or link that account, how to establish its session, and what permissions the user receives. OAuth sign-in alone does not protect unrelated routes; apply application authorization through your session middleware or handler checks.

## Templates and error pages

Templates can be declared in Go++ or loaded from template files. A handler can render one with typed application data:

```go
template ProfilePage(user User) {
	<h1>{{.Name}}</h1>
	<p>{{.Email}}</p>
}

class App : http.Server {
	func Profile(ctx *http.Context) error @{http.GET("/profile/{id}")} {
		user := loadUser(ctx.Param("id"))
		return ctx.Template("ProfilePage", user)
	}
}
```

`ctx.Template(key, args...)` renders a named template such as `ctx.Template("ProfilePage", user)`. It can also look up a template by URL path when that template declares a `tpl.Path` annotation; pass `ctx.Request.URL.Path` as the key to select it from the current request. The helper writes an HTML response with status 200 by default, or accepts an explicit status first, as in `ctx.Template(404, "NotFound", data)`. `ErrorTemplate()` selects the template used for server-generated error pages. For URL-bound templates and the generated `tpl.Name` functions, see the [template guide](/guide/standard-library/templates). When managed template source files are present, the server lifecycle starts a watcher that reloads them after edits.

## HTTPS and deployment boundaries

The `IP` and `Port` annotations configure a plain TCP listener; `gpp/http` does not currently provide a certificate or TLS annotation. Deployments commonly terminate TLS at a reverse proxy or load balancer. If the Go++ process must serve TLS itself, pass a TLS listener to `Serve`:

```go
cert := tls.LoadX509KeyPair("server.crt", "server.key")
listener := net.Listen("tcp", ":8443")

secureListener := tls.NewListener(listener, &tls.Config{
	Certificates: []tls.Certificate{cert},
})
App().Serve(secureListener)
```

This keeps certificate provisioning and rotation explicit while still letting the Go++ server assemble routes, middleware, templates, and lifecycle behavior on that listener.

## Graceful termination

The server installs SIGINT and SIGTERM handling and begins a bounded graceful shutdown when either signal arrives. It calls `BeforeShutdown`, drains requests through Go's `http.Server.Shutdown`, then calls `AfterShutdown`. Override the hooks for readiness changes, worker cleanup, or final logging; override `Shutdown(ctx)` only when the application needs a different shutdown trigger or policy.

```go
class App : http.Server {
	func BeforeShutdown() error {
		markNotReady()
		return nil
	}

	func AfterShutdown() error {
		flushTelemetry()
		return nil
	}
}
```

The default signal path uses a ten-second context. For tests or application-controlled termination, call `app.Shutdown(ctx)` directly.

## OpenAPI and Swagger UI

OpenAPI and Swagger solve related but different problems. OpenAPI is a machine-readable contract describing an API's paths, HTTP methods, inputs, and responses. It can be consumed by API clients, validators, documentation tools, and code generators. Swagger UI is a browser application that reads that contract and lets a developer explore and call the API.

Go++ derives the document from the same route annotations that register handlers. Enabling `http.OpenAPI` automatically mounts a `GET /openapi.json` endpoint containing an OpenAPI 3.0.3 document. The generator includes known route paths and methods, path parameters, and schemas it can infer from available type information; no separate JSON or YAML contract needs to be kept in sync.

Add `http.Swagger` to serve the bundled Swagger UI, configured to load that endpoint:

```go
class App : http.Server @{
	http.OpenAPI,
	http.Swagger
} {
	func Health(ctx *http.Context) error @{http.GET("/health")} {
		return ctx.JSON(record(ok: true))
	}
}
```

Starting this app automatically exposes:

| Endpoint | What it provides |
| --- | --- |
| `GET /openapi.json` | An OpenAPI 3.0.3 description generated from the server's declared routes and available type information |
| `/swagger` | The bundled Swagger UI, already configured to load this server's OpenAPI document |

Swagger UI turns the generated contract into an interactive **Try it out** page: select an operation, fill in its path/query/body inputs, submit it to the running service, and inspect the response. There is no handwritten JSON/YAML contract to keep synchronized, no manually registered documentation route, and no Swagger client code or assets to install. This makes a growing API explorable from the same annotations that define its running routes. Custom paths are available through `http.OpenAPI("/spec.json")` and `http.Swagger("/docs")`; Swagger requires OpenAPI to be enabled.

The UI is served locally from the Go++ package, so a service does not need to download its Swagger assets from a CDN at runtime. `Try it out` sends real requests to the running service; it does not mock handler behavior or validate that application logic matches every declared schema. Add the annotations once, and the route inventory, API description, and interactive explorer stay connected without handwritten spec endpoints, browser setup, or bundled UI code that could otherwise grow into hundreds or thousands of lines.

## Further reading

- [HTTP specification](/reference/specifications/std.http)
- [Server lifecycle, middleware, and shutdown](/reference/specifications/std.http.lifecycle)
- [OpenAPI and Swagger](/reference/specifications/openapi-swagger)
- [OAuth/OIDC](/reference/specifications/http.oauth)
- [HTTP template integration](/reference/specifications/tpl.http)
- [HTTP feature guide](/features/http)
