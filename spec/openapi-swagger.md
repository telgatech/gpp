# Go++ HTTP OpenAPI and Swagger Specification

## 1. Overview

`gpp/http.Server` may automatically expose an OpenAPI description for the server's routes.

The feature is enabled declaratively using a server annotation:

```gpp
class App : http.Server @{
    http.OpenAPI
}
```

or with an explicit path:

```gpp
class App : http.Server @{
    http.OpenAPI("/openapi.json")
}
```

No explicit route handler is required.

When `http.OpenAPI` is present, the HTTP runtime automatically registers a GET endpoint serving the generated OpenAPI document.

Optional Swagger UI support may be enabled separately:

```gpp
class App : http.Server @{
    http.OpenAPI("/openapi.json")
    http.Swagger("/docs")
}
```

The OpenAPI document is the machine-readable API description.

Swagger is an optional interactive UI consuming that OpenAPI document.

---

# 2. Design Principle

The API description must be derived from the same metadata used to implement the HTTP server.

There must not be a separate API-definition file that developers have to keep synchronized manually.

The model is:

```text
Go++ handler declarations
        ↓
HTTP semantic metadata
        ↓
runtime routing
        +
OpenAPI generation
        ↓
/openapi.json
```

The route metadata is the source of truth.

---

# 3. `http.OpenAPI`

Declare:

```gpp
class App : http.Server @{
    http.OpenAPI
}
```

to enable the default OpenAPI endpoint:

```text
GET /openapi.json
```

Equivalent explicit declaration:

```gpp
class App : http.Server @{
    http.OpenAPI("/openapi.json")
}
```

---

# 4. Annotation Declaration

Conceptually:

```gpp
annotation (
    OpenAPI(path string = "/openapi.json") on class
)
```

The annotation is valid only on classes derived from:

```gpp
http.Server
```

Example:

```gpp
class App : http.Server @{
    http.OpenAPI
}
```

---

# 5. Automatic Route Registration

When:

```gpp
@{http.OpenAPI("/openapi.json")}
```

is present, the server automatically exposes:

```text
GET /openapi.json
```

No source-level method is required.

The route behaves like a normal server route for:

```text
routing
middleware
request lifecycle hooks
logging
metrics
shutdown behavior
```

unless explicitly excluded by the HTTP runtime specification.

---

# 6. Response

The OpenAPI endpoint should return:

```text
HTTP 200
Content-Type: application/json
```

with a valid OpenAPI document.

JSON should be the canonical automatically exposed representation.

YAML output is not required for v1.

---

# 7. No Annotation, No Endpoint

If the server does not declare:

```gpp
@{http.OpenAPI}
```

then no OpenAPI route is automatically exposed.

OpenAPI generation is opt-in.

---

# 8. Canonical HTTP Path Syntax

Go++ HTTP routes use brace parameters.

Example:

```gpp
func User(id int) User @{
    http.GET("/users/{id}")
}
```

The canonical syntax is:

```text
/users/{id}
```

not:

```text
/users/:id
```

All HTTP documentation, route metadata, Swagger output, and OpenAPI generation should use `{name}` consistently.

---

# 9. Path Parameter Inference

Given:

```gpp
func User(id int) User @{
    http.GET("/users/{id}")
}
```

the compiler knows:

```text
HTTP method:
    GET

path:
    /users/{id}

path parameter:
    id

type:
    int

response:
    User
```

Therefore the generated OpenAPI operation should include an `id` path parameter automatically.

Conceptually:

```yaml
/users/{id}:
  get:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: integer
```

No duplicate path-parameter annotation should be necessary.

---

# 10. Path Parameter Validation

Every route path variable must correspond to a handler-bound value.

Example:

```gpp
func User(id int) User @{
    http.GET("/users/{id}")
}
```

is valid.

This should be rejected:

```gpp
func User() User @{
    http.GET("/users/{id}")
}
```

unless another explicit binding mechanism supplies `id`.

OpenAPI generation should use the already validated HTTP route model rather than independently guessing parameter bindings.

---

# 11. Route Example

Given:

```gpp
class App : http.Server @{
    http.OpenAPI
} {

    func User(id int) User @{
        http.GET("/users/{id}")
    }

}
```

the server exposes:

```text
GET /users/{id}
GET /openapi.json
```

The generated OpenAPI document contains the `/users/{id}` operation.

---

# 12. Method Mapping

HTTP route annotations map directly to OpenAPI operations.

For example:

```gpp
@{http.GET("/users")}
@{http.POST("/users")}
@{http.PUT("/users/{id}")}
@{http.PATCH("/users/{id}")}
@{http.DELETE("/users/{id}")}
```

map to:

```text
get
post
put
patch
delete
```

under the corresponding OpenAPI paths.

---

# 13. Typed Parameters

Handler parameter types should drive OpenAPI parameter schemas.

Examples:

```gpp
func User(id int)
```

becomes an integer parameter.

```gpp
func Search(q string)
```

becomes a string parameter.

```gpp
func Enabled(active bool)
```

becomes a boolean parameter.

The HTTP binding model determines whether each value comes from:

```text
path
query
header
cookie
request body
```

OpenAPI should reuse that binding information.

---

# 14. Request Bodies

Typed request-body binding should automatically produce an OpenAPI request schema.

Example:

```gpp
func CreateUser(input CreateUser) User @{
    http.POST("/users")
}
```

where the HTTP binding model identifies `input` as the request body.

OpenAPI should derive its request schema from:

```gpp
CreateUser
```

rather than requiring the schema to be separately declared.

---

# 15. Response Types

Handler return types should contribute response schemas.

Example:

```gpp
func User(id int) User @{
    http.GET("/users/{id}")
}
```

implies a successful response whose body schema is derived from:

```gpp
User
```

---

# 16. Classes as Schemas

Given:

```gpp
class User {
    ID int
    Name string
    Email string
}
```

OpenAPI generation should produce a corresponding object schema.

Conceptually:

```yaml
User:
  type: object
  properties:
    ID:
      type: integer
    Name:
      type: string
    Email:
      type: string
```

Visibility and serialization annotations must be respected.

---

# 17. Record Schemas

Anonymous records used in HTTP-visible signatures should also generate schemas.

Example:

```gpp
func Status() record {
    return record(
        Status: "ok",
        Version: version,
    )
}
```

may expose a structural OpenAPI schema equivalent to:

```text
record{
    Status string
    Version string
}
```

Internal generated record type names must not appear in OpenAPI.

---

# 18. Enum Schemas

Given:

```gpp
enum Status string {
    Pending
    Active
    Disabled
}
```

OpenAPI should represent the enum using its backing type and values.

Conceptually:

```yaml
type: string
enum:
  - Pending
  - Active
  - Disabled
```

Explicit enum backing values should be preserved.

---

# 19. Collections

Standard collection mappings should include:

```text
[]T
    OpenAPI array of T

map[string]T
    OpenAPI object with additionalProperties T
```

Nested combinations should work recursively.

---

# 20. Primitive Mapping

At minimum, map common Go/Go++ primitive types:

```text
string
    string

bool
    boolean

int
int8
int16
int32
int64
uint...
    integer

float32
float64
    number
```

More specific OpenAPI formats may be used where clearly appropriate.

---

# 21. Standard Library Types

Common standard types should receive useful OpenAPI schemas.

Examples may include:

```text
time.Time
    string / date-time

[]byte
    encoded binary/string representation as appropriate
```

The mapping table should remain explicit and deterministic.

---

# 22. Serialization Metadata

OpenAPI property names must match actual HTTP serialization behavior.

If:

```gpp
class User @{
    encoding.Serializable
} {
    Name string @{
        encoding.Name("name")
    }
}
```

serializes as:

```json
{
  "name": "..."
}
```

then OpenAPI must also describe the field as:

```text
name
```

not:

```text
Name
```

The API contract must match actual wire behavior.

---

# 23. Ignored Fields

Fields excluded from serialization should also be excluded from generated API schemas.

For example:

```gpp
PasswordHash string @{
    encoding.Ignore
}
```

must not appear in the OpenAPI schema.

---

# 24. Required Fields

Where the Go++ type/binding model can determine requiredness, OpenAPI should represent it.

Do not mark every property required merely because it exists as a field.

Requiredness should follow actual request/serialization semantics.

---

# 25. Status Codes

OpenAPI response status codes should come from the HTTP route's response semantics.

Where a handler has a canonical success status, describe it automatically.

Examples:

```text
GET
    normally 200

POST creating a resource
    may be 201 if declared by HTTP metadata

DELETE
    may be 204 if declared
```

Do not infer status semantics merely from the HTTP verb if the runtime does not enforce them.

---

# 26. Explicit HTTP Response Metadata

If Go++ HTTP annotations later or already provide response metadata, OpenAPI should use it directly.

Conceptually:

```gpp
@{
    http.POST("/users")
    http.Status(201)
}
```

would cause OpenAPI to document:

```text
201
```

as the corresponding response.

The precise status annotation syntax belongs to the HTTP annotation specification.

---

# 27. Error Responses

Known framework-generated responses may be included where appropriate.

For example:

```text
400
    request binding/validation failure

401
    authentication required

403
    permission denied

404
    route/resource behavior where declared

500
    server error
```

However, OpenAPI generation should not claim application-specific responses that cannot be inferred from the route metadata.

---

# 28. Authentication

HTTP authentication annotations should automatically contribute OpenAPI security metadata.

For example, if a route requires a bearer token, generated OpenAPI should describe that requirement.

The security description should come from the same metadata enforcing authentication at runtime.

---

# 29. Roles and Permissions

Role/permission annotations may contribute documentation extensions or operation descriptions where useful.

However, OpenAPI security semantics should not be distorted merely to encode Go++-specific authorization details.

Standard OpenAPI constructs should be preferred.

---

# 30. Operation IDs

Each operation should receive a stable operation ID.

By default, the source handler name is a natural choice.

Example:

```gpp
func GetUser(id int) User @{
    http.GET("/users/{id}")
}
```

may produce:

```text
operationId: GetUser
```

Operation IDs must be unique within the generated document.

---

# 31. Handler Documentation

Documentation comments should contribute OpenAPI descriptions.

Example:

```gpp
// GetUser returns a user by ID.
func GetUser(id int) User @{
    http.GET("/users/{id}")
}
```

may generate:

```text
summary/description:
    GetUser returns a user by ID.
```

The precise split between OpenAPI `summary` and `description` may be implementation-defined.

---

# 32. Class Documentation

Documentation comments on schema-visible classes and fields should contribute OpenAPI schema descriptions.

Example:

```gpp
// User represents an application account.
class User {

    // Name is the public display name.
    Name string
}
```

The generated schema should preserve those descriptions where practical.

---

# 33. OpenAPI Document Info

The generated document requires API metadata such as:

```text
title
version
description
```

Sensible defaults may come from:

```text
server class name
module metadata
package documentation
build/application metadata
```

Additional HTTP annotations may later customize these values.

Do not require extensive metadata merely to enable OpenAPI.

---

# 34. Default Title

If no explicit title exists:

```gpp
class Shop : http.Server @{
    http.OpenAPI
}
```

may generate a title such as:

```text
Shop API
```

The default should be deterministic.

---

# 35. Default Version

If project/module version information is available, it may be used.

Otherwise a neutral default such as:

```text
0.0.0
```

or another documented default may be supplied.

OpenAPI generation must not fail merely because application version metadata is absent.

---

# 36. OpenAPI Version

The initial implementation should emit one specific supported OpenAPI version consistently.

Prefer a modern OpenAPI 3.x format.

The exact supported version should be exposed as runtime/compiler metadata rather than varying opportunistically.

---

# 37. Endpoint Exclusion

The automatically generated OpenAPI endpoint itself should not normally appear in the generated API description.

Thus:

```text
/openapi.json
```

is infrastructure metadata, not an application API operation.

Likewise the Swagger UI route should normally be excluded.

---

# 38. Route Conflict

Given:

```gpp
class App : http.Server @{
    http.OpenAPI("/openapi.json")
} {

    func Custom() string @{
        http.GET("/openapi.json")
    }

}
```

the compiler should report a route conflict.

Example:

```text
duplicate HTTP route:

GET /openapi.json

automatically registered by:
    @http.OpenAPI

conflicts with:
    App.Custom
```

There must be no silent override or registration-order behavior.

---

# 39. Invalid OpenAPI Path

This should be invalid:

```gpp
@{http.OpenAPI("")}
```

unless an empty path is explicitly defined as meaning the default.

Prefer requiring either:

```gpp
@{http.OpenAPI}
```

or a valid absolute server path:

```gpp
@{http.OpenAPI("/api/openapi.json")}
```

---

# 40. Compile-Time Route Metadata

OpenAPI metadata should be generated primarily from compiler semantic information.

The compiler already knows:

```text
routes
handler signatures
bound parameters
return types
classes
records
enums
annotations
authentication
serialization metadata
```

Do not rediscover this information through runtime reflection if compile-time metadata is already available.

---

# 41. Runtime Generation

The runtime may construct the final document from compiler-generated metadata.

Conceptually:

```text
compiler
    ↓
static API metadata
    ↓
http.Server runtime
    ↓
OpenAPI JSON
```

This allows runtime server metadata such as configured host information to participate if needed without requiring reflection over handlers.

---

# 42. Deterministic Output

Given the same source program, the OpenAPI document should be deterministic.

Ordering should be stable where JSON generation allows it.

This improves:

```text
source control diffs
API contract tests
generated clients
caching
build reproducibility
```

---

# 43. `http.Swagger`

Swagger UI is optional and separate from OpenAPI generation.

Example:

```gpp
class App : http.Server @{
    http.OpenAPI("/openapi.json")
    http.Swagger("/docs")
}
```

This automatically exposes:

```text
GET /openapi.json
GET /docs
```

`/docs` serves an interactive Swagger UI configured to consume:

```text
/openapi.json
```

---

# 44. Annotation Declaration

Conceptually:

```gpp
annotation (
    Swagger(path string = "/docs") on class
)
```

It is valid only on:

```gpp
http.Server
```

derived classes.

---

# 45. `http.Swagger` Requires OpenAPI

This should be invalid:

```gpp
class App : http.Server @{
    http.Swagger("/docs")
}
```

without:

```gpp
http.OpenAPI
```

because Swagger UI requires an API description source.

The compiler should report:

```text
http.Swagger requires http.OpenAPI
```

rather than silently adding OpenAPI.

This preserves explicit behavior.

---

# 46. Swagger UI Source

The Swagger UI frontend may be:

```text
embedded in gpp/http
served from compiler/runtime resources
```

It should not require application developers to install Node.js, npm, or copy frontend assets manually.

---

# 47. No External CDN Requirement

The default Swagger UI implementation should preferably work without depending on a public CDN at runtime.

Required static resources may be embedded in the Go++ HTTP library/application binary.

This preserves Go++'s single-binary deployment model.

---

# 48. Swagger Route Conflict

This:

```gpp
@{http.Swagger("/docs")}
```

must conflict with any existing:

```text
GET /docs
```

application route.

The conflict should be diagnosed at compile time where possible.

---

# 49. Swagger Does Not Define the API

Swagger UI is a presentation layer.

The relationship is:

```text
Go++ HTTP metadata
        ↓
OpenAPI document
        ↓
Swagger UI
```

not:

```text
Swagger annotations
        ↓
application routing
```

Application routing remains defined entirely by Go++ HTTP routes.

---

# 50. Example Application

```gpp
class App : http.Server @{
    http.Port(8080)
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

    // CreateUser creates a user.
    func CreateUser(input CreateUser) User @{
        http.POST("/users")
    } {
        return DB.Create(input)
    }

}
```

This automatically provides:

```text
GET  /users
GET  /users/{id}
POST /users

GET  /openapi.json
GET  /docs
```

---

# 51. Example OpenAPI Structure

The server above may generate conceptually:

```yaml
openapi: 3.x.x

info:
  title: App API

paths:

  /users:

    get:
      operationId: ListUsers

    post:
      operationId: CreateUser

  /users/{id}:

    get:
      operationId: User

      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
```

Schema details should be generated from the actual source types.

---

# 52. No Duplicate API DSL

Go++ should not require code such as:

```gpp
openapi {
    path "/users/{id}" {
        ...
    }
}
```

when the route already exists as:

```gpp
func User(id int) User @{
    http.GET("/users/{id}")
}
```

Duplicating route declarations creates synchronization problems and contradicts the compiler-driven model.

---

# 53. Source of Truth

The source-of-truth hierarchy is:

```text
Go++ route declaration
        ↓
HTTP semantic model
        ↓
runtime routing

and

HTTP semantic model
        ↓
OpenAPI metadata
```

OpenAPI is a projection of the application, not an independent application definition.

---

# 54. User Overrides

Where OpenAPI cannot infer sufficient documentation, future annotations may add metadata such as:

```text
summary
description
tags
response descriptions
examples
deprecated
```

These should augment the generated model rather than redefine routing or types.

V1 does not require a large OpenAPI-specific annotation surface.

---

# 55. Avoid Annotation Explosion

Do not introduce dozens of annotations merely to mirror every field in the OpenAPI specification.

Go++ should infer the common case automatically.

Application code should normally look like:

```gpp
func User(id int) User @{
    http.GET("/users/{id}")
}
```

not:

```gpp
@{
    http.GET(...)
    openapi.Operation(...)
    openapi.Parameter(...)
    openapi.Response(...)
    openapi.Schema(...)
}
```

The latter defeats the purpose of having typed HTTP declarations.

---

# 56. OpenAPI Access From Code

The runtime may expose the generated OpenAPI document programmatically.

Conceptually:

```gpp
doc := server.OpenAPI()
```

However, such an API is optional.

The annotation-driven endpoint is the primary feature.

The presence of:

```gpp
@{http.OpenAPI}
```

must be sufficient by itself.

---

# 57. No Explicit Handler Required

Developers should not need:

```gpp
func OpenAPI(ctx *http.Context) @{
    http.GET("/openapi.json")
} {
    ctx.JSON(this.OpenAPI())
}
```

The annotation replaces all of that boilerplate.

---

# 58. Middleware Behavior

The generated OpenAPI and Swagger routes participate in the server lifecycle.

However, authentication middleware requires careful handling.

By default, API documentation routes should remain accessible unless explicitly protected by application/server configuration.

They should not accidentally inherit endpoint-specific authentication annotations.

Global server middleware may still apply.

---

# 59. Lifecycle Hooks

Generated documentation requests should participate in normal:

```gpp
BeforeRequest
AfterRequest
```

hooks.

This ensures:

```text
request logging
timing
metrics
tracing
```

remain consistent.

---

# 60. Development and Production

The feature should work identically under:

```text
gpp run
gpp build
production binary
```

OpenAPI generation should not depend on source files being present at runtime.

Required metadata must be compiled into the application.

---

# 61. Single-Binary Compatibility

Both:

```text
OpenAPI generation
Swagger UI assets
```

should be compatible with Go++'s single executable deployment model.

No external sidecar or documentation server should be required.

---

# 62. OpenAPI Endpoint Performance

The generated document may be constructed once and cached.

Because route/type metadata is normally static after server startup:

```text
first request
    generate + encode

later requests
    serve cached representation
```

is acceptable.

It may alternatively be generated during server initialization.

---

# 63. Development Reload

If `gpp run` supports recompilation/reload of route metadata, the OpenAPI document should naturally reflect the currently running application.

No special OpenAPI watcher is needed independently of the normal application rebuild/reload process.

---

# 64. Invalid API Metadata

If the compiler cannot represent an exported HTTP route faithfully in OpenAPI because of an unsupported type, it should report a useful diagnostic.

Example:

```text
cannot generate OpenAPI schema for response type Foo

route:
    GET /foo

handler:
    App.Foo
```

Do not silently emit a misleading schema.

---

# 65. Unsupported Internal Routes

Routes deliberately excluded from API documentation should only be excluded through explicit HTTP metadata if such a feature is introduced.

Do not guess that routes are "internal" based on their names.

---

# 66. Handwritten Go Handlers

If mixed Go/Go++ applications register HTTP routes dynamically through native Go code outside the Go++ server's route metadata, those routes may not be representable automatically.

`http.OpenAPI` guarantees documentation for routes known to the Go++ HTTP semantic model.

The compiler/runtime should not claim completeness for arbitrary runtime route registration it cannot inspect.

---

# 67. OpenAPI Errors

Failure to encode or serve a compiler-generated OpenAPI document should be considered a framework/runtime error.

It should route through normal server error handling where possible.

A malformed OpenAPI document generated from valid compiler metadata should be treated as a Go++ implementation defect.

---

# 68. Suggested Initial Annotation Surface

V1 requires only:

```gpp
annotation (
    OpenAPI(path string = "/openapi.json") on class
    Swagger(path string = "/docs") on class
)
```

More customization should be added only when real applications require it.

---

# 69. Recommended V1

Typical API server:

```gpp
class App : http.Server @{
    http.Port(8080)
    http.OpenAPI
    http.Swagger
}
```

produces:

```text
/openapi.json
/docs
```

with no explicit documentation handlers.

A production API that wants only the machine-readable contract may use:

```gpp
class App : http.Server @{
    http.OpenAPI
}
```

---

# 70. Design Summary

The model is:

```text
typed Go++ handlers
        ↓
HTTP route metadata
        ↓
┌─────────────────────┐
│                     │
runtime router    OpenAPI generator
                      ↓
                /openapi.json
                      ↓
                 Swagger UI
                    /docs
```

Example:

```gpp
class App : http.Server @{
    http.OpenAPI
    http.Swagger
} {

    func User(id int) User @{
        http.GET("/users/{id}")
    }

}
```

The core rules are:

> `http.OpenAPI` alone enables and automatically exposes the OpenAPI route.

> `http.Swagger` optionally exposes an interactive UI backed by that OpenAPI route.

> OpenAPI is generated from the same typed HTTP metadata that drives routing, so the application has one source of truth.

> Go++ HTTP paths use `{name}` syntax consistently, including `/users/{id}`.
