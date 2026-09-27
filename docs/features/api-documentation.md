# OpenAPI, Swagger, and OAuth

Go++ can generate API documentation from the routes that serve requests and
host Swagger UI for interactive exploration. OAuth and OIDC providers can be
configured on the same server, reducing drift between a running API and its
published description.

## Compare separate API docs with route-driven docs

Without generated docs, route behavior and API schemas need parallel updates.
Go++ can enable OpenAPI and Swagger on the server class:

::: code-group

```go [Separate setup]
// Register routes in the HTTP server.
// Maintain an OpenAPI document separately.
// Host Swagger UI that loads that document.
```

```go [Go++ server annotations]
class App : http.Server @{
    http.Prefix("/api"),
    http.OpenAPI,
    http.Swagger,
} {}
```

:::

Route annotations provide the input for the generated document:

```go
class App : http.Server @{http.OpenAPI, http.Swagger} {
    func Health(ctx *http.Context) error
        @{http.GET("/health")} {
        return ctx.JSON(record(ok: true))
    }
}
```

OAuth can be enabled with provider configuration on the same server. The HTTP
integration supports provider setup and callback handling. See [OpenAPI and Swagger](/reference/specifications/openapi-swagger)
and [OAuth](/reference/specifications/http.oauth) for configuration details.
