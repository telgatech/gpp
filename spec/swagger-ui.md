# Go++ HTTP Swagger UI Specification

## 1. Overview

`gpp/http.Server` may expose a complete interactive Swagger UI for the server's generated OpenAPI document.

Swagger UI is enabled declaratively:

```gpp
class App : http.Server @{
    http.OpenAPI
    http.Swagger
}
```

This automatically exposes:

```text
GET /openapi.json
GET /swagger
GET /swagger/*
```

No application handler, frontend setup, npm dependency, external CDN, or manual static-file configuration is required.

The central rule is:

> `http.Swagger` exposes the complete Swagger UI application backed by the server's generated OpenAPI document.

---

# 2. Relationship to OpenAPI

OpenAPI is the machine-readable API contract.

Swagger UI is an interactive browser for that contract.

The relationship is:

```text
typed Go++ HTTP routes
        ↓
HTTP semantic metadata
        ↓
generated OpenAPI
        ↓
/openapi.json
        ↓
Swagger UI
        ↓
/swagger
```

Swagger does not independently define routes, schemas, parameters, or authentication.

It consumes the generated OpenAPI document.

---

# 3. Enabling Swagger

The simplest configuration is:

```gpp
class App : http.Server @{
    http.OpenAPI
    http.Swagger
}
```

Equivalent explicit configuration:

```gpp
class App : http.Server @{
    http.OpenAPI("/openapi.json")
    http.Swagger("/swagger")
}
```

---

# 4. Default Routes

With:

```gpp
@{
    http.OpenAPI
    http.Swagger
}
```

the server automatically exposes:

```text
GET /openapi.json
GET /swagger
GET /swagger/*
```

The application remains free to use:

```text
/docs
/help
/manual
```

for its own documentation.

---

# 5. Default Swagger Path

The canonical default Swagger path is:

```text
/swagger
```

not:

```text
/docs
```

This avoids reserving a generic application path.

Conceptually:

```gpp
annotation (
    Swagger(path string = "/swagger") on class
)
```

---

# 6. Custom Swagger Path

Applications may override the default:

```gpp
class App : http.Server @{
    http.OpenAPI("/api/openapi.json")
    http.Swagger("/api/swagger")
}
```

This automatically exposes:

```text
GET /api/openapi.json
GET /api/swagger
GET /api/swagger/*
```

The Swagger UI should automatically use:

```text
/api/openapi.json
```

as its API-description source.

No second configuration is required.

---

# 7. Swagger Requires OpenAPI

`http.Swagger` requires `http.OpenAPI` on the same server.

Invalid:

```gpp
class App : http.Server @{
    http.Swagger
}
```

The compiler should report:

```text
http.Swagger requires http.OpenAPI on the same server
```

Go++ should not silently enable OpenAPI.

The application explicitly controls whether the API description is exposed.

---

# 8. Full Swagger UI

`http.Swagger` must serve the actual interactive Swagger UI application.

It must not merely provide:

* raw OpenAPI JSON;
* a prettified JSON page;
* a small custom endpoint list;
* a hand-built HTML table;
* a minimal API viewer.

The expected experience is the normal Swagger-style interactive API explorer.

---

# 9. Required UI Capabilities

Where supported by the generated OpenAPI document, `/swagger` should provide:

* endpoint grouping;
* HTTP method display;
* route paths;
* operation descriptions;
* parameter documentation;
* path parameters;
* query parameters;
* headers;
* request-body schemas;
* editable request bodies;
* response status information;
* response schemas;
* reusable schema/model browsing;
* authentication controls;
* endpoint expansion/collapse;
* Try it out;
* Execute;
* request URL display;
* request headers;
* response headers;
* response body;
* request examples;
* curl examples where provided by Swagger UI.

Go++ should not reimplement these facilities.

It should expose the standard Swagger UI behavior.

---

# 10. Try It Out

Swagger UI's interactive request execution must work against the currently running Go++ server.

For example:

```gpp
func User(id int) User @{
    http.GET("/users/{id}")
}
```

should appear in Swagger as:

```text
GET /users/{id}
```

The user should be able to:

1. expand the operation;
2. select Try it out;
3. enter `id`;
4. execute the request;
5. inspect the response.

---

# 11. Authentication

Swagger authentication controls should derive from generated OpenAPI security metadata.

For example, if the API uses bearer authentication, OpenAPI should define the corresponding security scheme.

Swagger UI will then automatically provide its normal:

```text
Authorize
```

interface.

Swagger-specific authentication configuration should not duplicate `gpp/http` authentication metadata.

---

# 12. Swagger Assets

The Swagger UI frontend requires static assets such as:

```text
HTML
JavaScript
CSS
icons
supporting resources
```

These should be distributed with `gpp/http`.

They should be embedded into the application using Go++'s existing `embed` feature.

---

# 13. Go++ `embed` Integration

Go++ already provides native resource embedding:

```gpp
embed (
    swaggerFiles "swagger/"
)
```

or the corresponding compiler-owned declaration in the `gpp/http` implementation.

Swagger UI assets should use this Go++ feature.

The implementation should not require application developers to write raw Go:

```text
//go:embed
```

Go++ source remains the canonical source.

---

# 14. Compiler-Owned Swagger Assets

The Swagger UI resources belong to the official:

```text
gpp/http
```

package.

Conceptually, its implementation may contain:

```gpp
embed (
    swaggerFiles "assets/swagger/"
)
```

The actual resource path is implementation-specific.

The important semantic result is that the Swagger frontend is available as an embedded `fs.FS`-style resource tree to the HTTP runtime.

---

# 15. Lowering of `embed`

Go++'s normal `embed` lowering rules apply.

The Go++ source:

```gpp
embed (
    swaggerFiles "assets/swagger/"
)
```

may ultimately lower to whatever ordinary Go implementation is necessary.

That generated Go mechanism is an implementation detail.

The `gpp/http` source and specification should use Go++'s `embed` declaration rather than exposing raw backend embedding syntax.

---

# 16. Application Developers Do Not Embed Swagger

Applications should not need:

```gpp
embed (
    swaggerFiles ...
)
```

merely to enable Swagger.

This:

```gpp
class App : http.Server @{
    http.OpenAPI
    http.Swagger
}
```

is sufficient.

The Swagger resources are supplied by `gpp/http`.

---

# 17. Single-Binary Deployment

Swagger support must preserve Go++'s single-binary deployment model.

A built application should contain everything necessary to serve:

```text
/openapi.json
/swagger
/swagger/*
```

without requiring:

* Node.js;
* npm;
* a frontend build;
* a separate static directory;
* downloaded runtime assets;
* an external documentation service.

---

# 18. No CDN Dependency

Swagger UI should not depend on a public CDN by default.

The generated application should continue to provide its documentation UI when deployed:

* on localhost;
* on an internal network;
* in an air-gapped environment;
* without outbound internet access.

All required Swagger UI resources should be locally available through embedded resources.

---

# 19. Root Swagger Page

Requesting:

```text
GET /swagger
```

should produce the Swagger UI entry point.

The implementation may either serve the page directly or redirect canonically to:

```text
/swagger/
```

if required for relative asset resolution.

From the application's perspective:

```text
/swagger
```

is the canonical public URL.

---

# 20. Asset Paths

Swagger UI assets may be exposed below the configured Swagger root.

For the default:

```text
/swagger
```

examples include:

```text
/swagger/swagger-ui.css
/swagger/swagger-ui-bundle.js
/swagger/swagger-ui-standalone-preset.js
/swagger/favicon-32x32.png
```

The exact filenames depend on the bundled Swagger UI distribution and are not part of Go++'s stable API.

---

# 21. Custom Root Asset Paths

For:

```gpp
@{http.Swagger("/api/swagger")}
```

all Swagger assets should also be served beneath that root.

Conceptually:

```text
/api/swagger
/api/swagger/swagger-ui.css
/api/swagger/swagger-ui-bundle.js
...
```

No hard-coded `/swagger/...` asset URLs should break custom mounting.

---

# 22. OpenAPI URL Configuration

The served Swagger UI entry page must be configured with the actual `http.OpenAPI` route.

Default:

```gpp
@{
    http.OpenAPI
    http.Swagger
}
```

uses:

```text
/openapi.json
```

Custom:

```gpp
@{
    http.OpenAPI("/api/spec.json")
    http.Swagger("/swagger")
}
```

uses:

```text
/api/spec.json
```

The developer should not repeat the OpenAPI URL in Swagger configuration.

---

# 23. No Duplicate Configuration

Avoid APIs such as:

```gpp
@{
    http.OpenAPI("/openapi.json")
    http.Swagger("/swagger", "/openapi.json")
}
```

because the OpenAPI location is already known.

Prefer:

```gpp
@{
    http.OpenAPI("/openapi.json")
    http.Swagger("/swagger")
}
```

---

# 24. Route Conflict Detection

Swagger-generated routes participate in normal route validation.

Example:

```gpp
class App : http.Server @{
    http.OpenAPI
    http.Swagger
} {

    func SwaggerPage() string @{
        http.GET("/swagger")
    }

}
```

must produce a compile-time route conflict.

There is no registration-order or last-wins behavior.

---

# 25. Asset Route Conflicts

The Swagger mount owns its route subtree.

For default:

```text
/swagger
```

the framework also controls:

```text
/swagger/*
```

Therefore an application route such as:

```gpp
@{http.GET("/swagger/custom")}
```

should conflict with the Swagger mount.

This prevents ambiguous asset routing.

---

# 26. OpenAPI Route Conflict

Likewise:

```gpp
@{http.OpenAPI("/openapi.json")}
```

reserves:

```text
GET /openapi.json
```

The OpenAPI and Swagger generated routes participate in the same compile-time HTTP route table as source-declared handlers.

---

# 27. Server Lifecycle

Swagger requests should participate in the normal `http.Server` lifecycle.

This includes:

```text
BeforeRequest
AfterRequest
request logging
metrics
tracing
```

unless a hook explicitly excludes infrastructure routes.

Swagger should not create a second HTTP server.

---

# 28. Middleware

Global server middleware may apply normally to Swagger requests.

Endpoint-specific middleware belonging to unrelated application routes does not.

The framework should avoid accidentally treating Swagger assets as application business endpoints.

---

# 29. Documentation Route Authentication

By default, Swagger UI should be publicly reachable if `http.Swagger` is enabled.

The same applies to `/openapi.json` unless application/server-wide security policy states otherwise.

Future HTTP metadata may allow explicitly protecting documentation endpoints.

Swagger should not automatically inherit authentication from arbitrary application routes.

---

# 30. OpenAPI Security and Swagger Authorization

There are two separate concepts:

```text
access to /swagger
```

and:

```text
authentication used by Swagger when calling protected APIs
```

The second is described through the OpenAPI security schemes.

Thus `/swagger` may be publicly viewable while Swagger requests to:

```text
/account
/admin
```

use an authorization token entered through Swagger's Authorize UI.

---

# 31. Content Types

The Swagger entry page should use:

```text
text/html; charset=utf-8
```

CSS assets should use their appropriate CSS content type.

JavaScript should use its appropriate JavaScript content type.

Images/icons should use their appropriate content types.

The OpenAPI endpoint remains:

```text
application/json
```

---

# 32. Caching

Embedded Swagger assets are immutable for a given application build.

The HTTP implementation may therefore use appropriate caching headers for static Swagger resources.

The entry page itself may use more conservative caching if its OpenAPI URL or server configuration is generated dynamically.

Caching policy is runtime implementation detail unless explicitly configured.

---

# 33. Compression

Swagger static resources may participate in normal server compression behavior if supported.

No Swagger-specific compression mechanism is required.

---

# 34. Embedded Resource Serving

Swagger assets should use ordinary filesystem/resource abstractions provided by Go++ and Go.

Conceptually:

```text
Go++ embed declaration
        ↓
embedded fs.FS
        ↓
gpp/http static resource handler
        ↓
/swagger/*
```

Swagger should not require bespoke binary-resource machinery.

---

# 35. Resource Ownership

Swagger resources are compiler/library-owned, not application-owned.

Application resource declarations such as:

```gpp
embed (
    staticFiles "static/"
)
```

remain independent.

There should be no naming collision between compiler-owned Swagger resources and the application's embedded resource symbols.

---

# 36. Swagger UI Version

`gpp/http` should bundle a known Swagger UI version.

The version should move with releases of Go++ rather than downloading "latest" at application runtime.

This provides reproducible application builds.

---

# 37. Reproducibility

Given the same:

```text
Go++ compiler version
gpp/http version
application source
```

the generated Swagger UI resources should be deterministic.

Runtime internet state must not affect which Swagger UI code is served.

---

# 38. Upgrading Swagger UI

Swagger UI may be upgraded as part of normal Go++/`gpp/http` releases.

Applications should not need to change source merely because the bundled frontend version changes.

The stable application contract remains:

```gpp
@{http.Swagger}
```

---

# 39. Swagger UI Configuration

Go++ may configure sensible Swagger UI defaults internally.

Examples include:

```text
OpenAPI URL
application title
deep-link behavior
operation expansion defaults
request duration display
```

Avoid exposing a large Swagger-specific configuration surface in v1.

---

# 40. Avoid Swagger Configuration Explosion

Do not initially introduce annotations such as:

```text
SwaggerDeepLinks
SwaggerPersistAuthorization
SwaggerModelDepth
SwaggerDefaultExpansion
SwaggerFilter
SwaggerTheme
...
```

unless real application use demonstrates a need.

The primary promise is zero-configuration useful API documentation.

---

# 41. Application Title

Swagger UI naturally displays metadata from the generated OpenAPI document.

Therefore title/version/description should come from OpenAPI metadata rather than separate Swagger configuration.

Again:

```text
HTTP semantic metadata
        ↓
OpenAPI
        ↓
Swagger
```

rather than maintaining two documentation models.

---

# 42. Schemas

Swagger UI should expose the schemas generated from Go++:

```text
classes
records
enums
collections
request types
response types
```

No Swagger-specific schema generator is necessary.

It renders the OpenAPI schemas already produced by `http.OpenAPI`.

---

# 43. Documentation Comments

Handler and type documentation comments flow:

```text
Go++ source comments
        ↓
OpenAPI descriptions
        ↓
Swagger UI
```

Example:

```gpp
// User returns a user by ID.
func User(id int) User @{
    http.GET("/users/{id}")
}
```

should cause Swagger to display the route documentation automatically.

---

# 44. Brace Path Syntax

Swagger/OpenAPI paths use the canonical Go++ HTTP parameter syntax:

```gpp
@{http.GET("/users/{id}")}
```

There is no `:id` route syntax in the current HTTP design.

This gives direct alignment between:

```text
Go++:
/users/{id}

OpenAPI:
/users/{id}

Swagger:
/users/{id}
```

No path-variable syntax translation is required.

---

# 45. Example

```gpp
class App : http.Server @{
    http.Port(9000)
    http.OpenAPI
    http.Swagger
} {

    // ListUsers returns all users.
    func ListUsers() []User @{
        http.GET("/users")
    } {
        return DB.Users()
    }

    // User returns one user by ID.
    func User(id int) User @{
        http.GET("/users/{id}")
    } {
        return DB.User(id)
    }
}
```

Running the application gives:

```text
GET /users
GET /users/{id}

GET /openapi.json
GET /swagger
GET /swagger/*
```

Opening:

```text
http://localhost:9000/swagger
```

shows a complete interactive Swagger UI for the application.

---

# 46. Custom Example

```gpp
class App : http.Server @{
    http.OpenAPI("/api/spec.json")
    http.Swagger("/developer/swagger")
}
```

automatically provides:

```text
GET /api/spec.json
GET /developer/swagger
GET /developer/swagger/*
```

and the Swagger application automatically loads:

```text
/api/spec.json
```

---

# 47. Production Behavior

Swagger support should work identically in a production binary.

The production executable contains:

```text
HTTP application
OpenAPI metadata
Swagger HTML
Swagger JavaScript
Swagger CSS
Swagger supporting assets
```

within the application binary.

No source tree is required after compilation.

---

# 48. Development Behavior

Under:

```text
gpp run
```

Swagger uses the current application build's OpenAPI metadata.

When the application is rebuilt because routes or schemas change, reloading `/swagger` naturally displays the updated OpenAPI contract.

No Swagger-specific filesystem watcher is required.

---

# 49. Failure Behavior

Failure to serve an embedded Swagger resource should be treated as an internal framework problem.

It should not expose filesystem/compiler internals to the user.

If the Swagger entry page cannot be constructed, normal server error handling applies.

---

# 50. Missing OpenAPI Document

Because:

```gpp
@{http.Swagger}
```

requires:

```gpp
@{http.OpenAPI}
```

a normally compiled application should never have Swagger enabled without an OpenAPI source.

If runtime state somehow makes the OpenAPI endpoint unavailable, Swagger may display its normal loading error while the server logs the underlying framework failure.

---

# 51. V1 Annotation Surface

V1 requires only:

```gpp
annotation (
    OpenAPI(path string = "/openapi.json") on class
    Swagger(path string = "/swagger") on class
)
```

Example:

```gpp
class App : http.Server @{
    http.OpenAPI
    http.Swagger
}
```

No other Swagger annotations are required for the initial implementation.

---

# 52. V1 Implementation Model

Conceptually, `gpp/http` contains:

```gpp
embed (
    swaggerFiles "assets/swagger/"
)
```

and runtime code equivalent in responsibility to:

```text
serve Swagger entry page
serve embedded Swagger assets
inject configured OpenAPI URL
```

The exact implementation may evolve.

The public Go++ source model remains based on the Go++ `embed` declaration.

---

# 53. Non-Goals

V1 does not require:

* custom Swagger themes;
* npm integration;
* arbitrary Swagger plugins;
* runtime downloads;
* CDN-hosted assets;
* user-supplied JavaScript configuration;
* a Go++ reimplementation of Swagger UI;
* separate schema generation for Swagger;
* separate API annotations for Swagger;
* raw `//go:embed` declarations in Go++ source.

---

# 54. Design Summary

The complete model is:

```text
Go++ HTTP handlers
        ↓
typed HTTP metadata
        ↓
OpenAPI generator
        ↓
/openapi.json
        ↓
full embedded Swagger UI
        ↓
/swagger
```

Swagger frontend resources are packaged through Go++:

```gpp
embed (
    swaggerFiles "assets/swagger/"
)
```

rather than exposing backend Go embedding syntax.

Application configuration remains:

```gpp
class App : http.Server @{
    http.OpenAPI
    http.Swagger
}
```

The core principles are:

> `http.Swagger` means a complete interactive Swagger UI, not a simplified API page.

> Swagger assets are supplied by `gpp/http` and embedded through Go++'s existing `embed` feature.

> The resulting application remains a self-contained executable with no CDN, npm, or runtime asset dependency.

> Swagger consumes the same OpenAPI contract generated from the typed HTTP routes, so there is still only one source of truth.
