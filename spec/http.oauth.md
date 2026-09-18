# Go++ HTTP OAuth / OpenID Connect Client Specification

## 1. Overview

`gpp/http.Server` provides built-in support for external OAuth 2.0 and OpenID Connect providers.

Typical use with a well-known provider:

```gpp
class App : http.Server @{
    http.OAuth(http.OAuthProvider.Google)
}
```

Typical use with a custom provider:

```gpp
class App : http.Server @{
    http.OAuth(
        "corp",
        "https://identity.example.com",
        "CORP_CLIENT_ID",
        "CORP_CLIENT_SECRET",
        "openid",
        "email",
        "profile",
    )
}
```

The framework automatically handles:

```text
authorization redirects
callback routes
state
PKCE
OIDC nonce
authorization-code exchange
token validation
ID-token verification
provider discovery
identity normalization
```

The application only handles what should happen after authentication succeeds or fails.

---

# 2. Design Principle

The public OAuth configuration surface should remain very small.

There are only two canonical forms:

```gpp
http.OAuth(
    provider http.OAuthProvider,
    scopes ...string,
)
```

and:

```gpp
http.OAuth(
    name string,
    url string,
    clientIDEnv string,
    clientSecretEnv string,
    scopes ...string,
)
```

The first is convenience for well-known providers.

The second is the general primitive.

---

# 3. Well-Known Provider Form

Example:

```gpp
@{
    http.OAuth(http.OAuthProvider.Google)
}
```

The selected `OAuthProvider` supplies:

```text
provider name
provider issuer / metadata URL
default client ID environment-variable name
default client secret environment-variable name
default identity scopes
provider protocol behavior
```

The application need not repeat those values.

---

# 4. Built-In Provider Enum

Conceptually:

```gpp
enum OAuthProvider string {
    Google    = "google"
    Facebook  = "facebook"
    GitHub    = "github"
    Microsoft = "microsoft"
    Apple     = "apple"
}
```

Usage:

```gpp
@{
    http.OAuth(http.OAuthProvider.Google)
    http.OAuth(http.OAuthProvider.GitHub)
}
```

Using an enum provides:

```text
compile-time checking
autocomplete
discoverability
refactoring safety
stable provider identifiers
```

---

# 5. Explicit Provider Form

The general form is:

```gpp
http.OAuth(
    name,
    url,
    clientIDEnv,
    clientSecretEnv,
    scopes...,
)
```

Example:

```gpp
@{
    http.OAuth(
        "corp",
        "https://identity.example.com",
        "CORP_CLIENT_ID",
        "CORP_CLIENT_SECRET",
        "openid",
        "email",
        "profile",
    )
}
```

The arguments mean:

```text
name
    stable provider identifier

url
    provider issuer / metadata base URL

clientIDEnv
    environment-variable name containing the client ID

clientSecretEnv
    environment-variable name containing the client secret

scopes
    authorization scopes requested from the provider
```

---

# 6. Explicit Form Is the Canonical Primitive

Conceptually:

```gpp
http.OAuth(http.OAuthProvider.Google)
```

is convenience over something equivalent to:

```gpp
http.OAuth(
    "google",
    <Google issuer URL>,
    "GOOGLE_CLIENT_ID",
    "GOOGLE_CLIENT_SECRET",
    <Google default scopes>,
)
```

The exact provider URL and defaults are owned by `gpp/http`.

Both forms ultimately feed the same OAuth engine.

---

# 7. Scopes Belong to the Annotation

Scopes are part of the authorization request.

Therefore they belong in `http.OAuth(...)` rather than being configured later.

Example:

```gpp
@{
    http.OAuth(
        http.OAuthProvider.Google,
        "openid",
        "email",
        "profile",
        "https://www.googleapis.com/auth/calendar.readonly",
    )
}
```

These scopes are known when:

```text
GET /auth/google
```

constructs the provider authorization request.

---

# 8. Default Scopes

If scopes are omitted:

```gpp
@{
    http.OAuth(http.OAuthProvider.Google)
}
```

the provider adapter uses its standard identity/login scopes.

For an OpenID Connect provider, these would normally include the provider's appropriate identity scopes, commonly corresponding to:

```text
openid
email
profile
```

The exact defaults are provider metadata.

---

# 9. Explicit Scopes

If scopes are supplied:

```gpp
@{
    http.OAuth(
        http.OAuthProvider.Google,
        "openid",
        "email",
        "profile",
    )
}
```

the authorization request uses those scopes.

Protocol-mandatory scopes may still be added internally where required.

For example, an OIDC login flow must not accidentally stop being OIDC merely because `openid` was omitted from an otherwise custom scope list.

---

# 10. Custom Provider Scopes

The same rule applies to explicit providers:

```gpp
@{
    http.OAuth(
        "corp",
        "https://login.example.com",
        "CORP_CLIENT_ID",
        "CORP_CLIENT_SECRET",
        "openid",
        "email",
    )
}
```

If no scopes are supplied, the framework may use protocol/provider defaults discoverable from the provider configuration.

Where no sensible defaults exist, provider setup should fail clearly rather than invent arbitrary permissions.

---

# 11. Provider URL

For explicit providers:

```gpp
http.OAuth(
    "corp",
    "https://identity.example.com",
    ...
)
```

the URL represents the provider's issuer or metadata base URL.

It is not merely an authorization endpoint.

The framework should use standardized discovery where available.

---

# 12. OIDC Discovery

For an OpenID Connect provider, `gpp/http` may discover metadata via the provider's standardized configuration.

This may provide:

```text
authorization endpoint
token endpoint
userinfo endpoint
JWKS endpoint
issuer
supported signing algorithms
supported scopes
```

The application does not manually provide these endpoints.

---

# 13. OAuth Authorization-Server Metadata

Where the provider supports standardized OAuth authorization-server metadata rather than OIDC discovery, the framework may use that metadata.

Again, the application supplies the provider URL, not each individual endpoint.

---

# 14. Non-Discoverable Providers

If a provider cannot be configured from its issuer/metadata URL, it is outside the simplest annotation-only path.

A separate provider-registration mechanism may later support explicit endpoint configuration.

Do not bloat `http.OAuth(...)` with:

```text
authorizationURL
tokenURL
userinfoURL
jwksURL
revocationURL
```

arguments.

The annotation should stay small.

---

# 15. Credentials

The annotation receives environment-variable names, never credentials directly.

Example:

```gpp
http.OAuth(
    "corp",
    "https://identity.example.com",
    "CORP_CLIENT_ID",
    "CORP_CLIENT_SECRET",
)
```

At runtime:

```text
CORP_CLIENT_ID
CORP_CLIENT_SECRET
```

are resolved from the environment/configuration system.

---

# 16. Well-Known Provider Environment Names

Built-in providers use fixed conventional environment-variable names.

Recommended defaults:

```text
Google

    GOOGLE_CLIENT_ID
    GOOGLE_CLIENT_SECRET


Facebook

    FACEBOOK_CLIENT_ID
    FACEBOOK_CLIENT_SECRET


GitHub

    GITHUB_CLIENT_ID
    GITHUB_CLIENT_SECRET


Microsoft

    MICROSOFT_CLIENT_ID
    MICROSOFT_CLIENT_SECRET


Apple

    APPLE_CLIENT_ID
    APPLE_CLIENT_SECRET
```

The well-known provider overload deliberately favors convention.

If an application requires custom env names, it may use the explicit form instead.

---

# 17. Explicit Configuration as Escape Hatch

Instead of adding more overloads such as:

```gpp
http.OAuth(
    OAuthProvider.Google,
    "MY_ID",
    "MY_SECRET",
)
```

an application can simply use:

```gpp
http.OAuth(
    "google",
    <Google issuer URL>,
    "MY_ID",
    "MY_SECRET",
)
```

This keeps the API surface smaller.

The enum overload exists purely for convenience.

---

# 18. Missing Credentials

If a configured provider lacks required credentials, server startup should fail.

Example:

```text
OAuth provider google is configured but GOOGLE_CLIENT_ID is not set
```

Do not wait until the first login attempt to discover obvious configuration problems.

---

# 19. Provider Name

The provider `name` is a stable lowercase identifier.

Example:

```gpp
http.OAuth(
    "corp",
    ...
)
```

generates:

```text
/auth/corp
/auth/corp/callback
```

Recommended characters:

```text
a-z
0-9
-
```

Invalid route-facing provider names should be rejected.

---

# 20. Generated Routes

Each OAuth provider automatically gets:

```text
GET /auth/{name}
GET /auth/{name}/callback
```

For:

```gpp
http.OAuth(http.OAuthProvider.Google)
```

this becomes:

```text
GET /auth/google
GET /auth/google/callback
```

For:

```gpp
http.OAuth(
    "corp",
    ...
)
```

this becomes:

```text
GET /auth/corp
GET /auth/corp/callback
```

---

# 21. Route Ownership

OAuth routes are framework-generated HTTP routes.

They participate in normal route conflict checking.

This is invalid:

```gpp
class App : http.Server @{
    http.OAuth(http.OAuthProvider.Google)
} {

    func Login() @{
        http.GET("/auth/google")
    }

}
```

The compiler should report the conflict.

---

# 22. Login Route

Requesting:

```text
GET /auth/google
```

starts the provider flow.

The framework handles:

```text
secure state generation
PKCE generation
OIDC nonce where applicable
temporary flow storage
authorization URL construction
redirect to provider
```

---

# 23. Callback Route

The provider callback:

```text
GET /auth/google/callback
```

causes the framework to perform:

```text
state validation
PKCE validation
nonce validation
authorization code exchange
token validation
ID-token validation
profile/UserInfo resolution
identity normalization
application callback
```

---

# 24. Authorization Code Flow

Browser login uses authorization-code flow.

Implicit-token flows should not be the default.

The framework should use current secure OAuth/OIDC practices appropriate to each provider.

---

# 25. State

The framework must generate and validate secure OAuth state automatically.

Application code should not implement state handling.

Invalid or expired state terminates the login attempt.

---

# 26. PKCE

PKCE should be used where supported or required.

The framework owns:

```text
verifier generation
challenge generation
flow storage
callback verification
```

---

# 27. OIDC Nonce

OIDC providers should use nonce handling where appropriate.

The framework generates, stores, and validates the nonce.

---

# 28. Redirect URI

Callback URLs are derived automatically from:

```text
server public base URL
+
generated callback route
```

Example:

```text
https://app.example.com
+
/auth/google/callback

=
https://app.example.com/auth/google/callback
```

---

# 29. Public Base URL

OAuth must use the server's externally visible base URL.

Example:

```text
internal listener:
    127.0.0.1:9000

external URL:
    https://app.example.com
```

The OAuth callback is:

```text
https://app.example.com/auth/google/callback
```

not the internal listener address.

---

# 30. Development Default

In local development, the server may derive:

```text
http://localhost:9000
```

when no explicit public URL is configured.

For Google:

```text
http://localhost:9000/auth/google/callback
```

would then be the generated callback URI.

Provider-side redirect registration is still required.

---

# 31. Normalized Identity

Successful authentication produces:

```gpp
class OAuthIdentity {
    Provider string
    ID string

    Email string
    Name string
    Picture string

    EmailVerified bool

    Claims map[string]any
}
```

The same application type is used regardless of provider.

---

# 32. Provider Identity Key

Applications should identify external accounts by:

```text
Provider + ID
```

Example:

```text
google + 123456789
```

Email alone should not be treated as the canonical external account identifier.

---

# 33. Common Identity Fields

The framework should normalize common provider information into:

```text
Provider
ID
Email
Name
Picture
EmailVerified
```

Not every provider guarantees every value.

Normal Go++ zero/nil semantics apply for unavailable fields.

---

# 34. Raw Claims

Provider-specific claims remain available through:

```gpp
identity.Claims
```

Example:

```gpp
locale := identity.Claims["locale"]
```

The common identity type should not grow provider-specific fields.

---

# 35. OIDC ID Token Validation

For OIDC providers, identity must come only from validated protocol data.

Applicable validation includes:

```text
signature
issuer
audience
expiry
nonce
```

The framework must never pass an application identity derived from an unvalidated ID token.

---

# 36. UserInfo

Where appropriate, provider adapters may query UserInfo/profile APIs after successful token exchange.

Whether normalized fields come from:

```text
ID token
OIDC UserInfo
provider profile API
```

is an adapter implementation detail.

---

# 37. OAuth Login Hook

Applications handle successful login through a virtual server method.

Recommended:

```gpp
func OAuthLogin(
    ctx *http.Context,
    identity http.OAuthIdentity,
) {
    ...
}
```

Example:

```gpp
class App : http.Server @{
    http.OAuth(http.OAuthProvider.Google)
} {

    func OAuthLogin(
        ctx *http.Context,
        identity http.OAuthIdentity,
    ) {

        user := DB.FindOrCreate(
            identity.Provider,
            identity.ID,
            identity.Email,
        )

        ctx.Session.Set("user_id", user.ID)
        ctx.Redirect("/")
    }
}
```

---

# 38. Application Responsibility

After protocol authentication succeeds, the application decides:

```text
whether account exists
whether to create account
whether to link identity
how to create session
whether to issue JWT
where to redirect
what authorization role the user receives
```

`gpp/http` should not silently make these domain decisions.

---

# 39. Token-Aware Hook

Applications needing provider API access may use an overloaded hook:

```gpp
func OAuthLogin(
    ctx *http.Context,
    identity http.OAuthIdentity,
    token http.OAuthToken,
) {
    ...
}
```

---

# 40. OAuth Token Model

Conceptually:

```gpp
class OAuthToken {
    AccessToken string
    RefreshToken string
    TokenType string

    Expiry time.Time

    Scopes []string
}
```

The provider token is separate from identity information.

---

# 41. Least Exposure

Ordinary social-login applications should use:

```gpp
OAuthLogin(ctx, identity)
```

and never receive provider tokens.

Only applications requiring provider API access should implement the token-bearing overload.

---

# 42. Token Persistence

`gpp/http` does not automatically persist:

```text
access tokens
refresh tokens
```

The application decides whether persistence is required.

Refresh tokens in particular must be treated as sensitive credentials.

---

# 43. Token Logging

The framework must not log by default:

```text
authorization code
access token
refresh token
ID token
client secret
PKCE verifier
```

---

# 44. OAuth Failure Hook

Applications may override:

```gpp
func OAuthError(
    ctx *http.Context,
    provider string,
    err error,
) {
    ...
}
```

The provider is exposed as its stable route/configuration name.

Example:

```text
google
corp
github
```

This naturally supports both built-in and custom providers.

---

# 45. Default Failure Handling

Without an override, the framework should:

```text
log safe diagnostic information
avoid leaking token/secret material
return an appropriate user-facing error
```

Provider cancellation should be distinguishable from internal protocol failure.

---

# 46. Provider Cancellation

If a user selects "Cancel" or denies authorization at the provider, `gpp/http` should identify this as a provider/user cancellation rather than an internal server fault.

Applications may redirect back to a login page.

---

# 47. Provider Errors

OAuth/OIDC errors should be normalized where practical.

Examples include:

```text
access_denied
invalid_scope
invalid_request
temporarily_unavailable
server_error
```

Raw provider responses must not be exposed directly to end users by default.

---

# 48. No Automatic Users Table

OAuth support must not automatically create application persistence.

It does not create:

```text
users
oauth_accounts
sessions
```

tables.

The application owns its own data model.

---

# 49. No Automatic Account Linking

Two identities sharing an email are not automatically considered the same user.

For example:

```text
Google: alice@example.com
GitHub: alice@example.com
```

must not be linked automatically unless application policy says so.

---

# 50. Email Verification

`EmailVerified` is true only where supported by trusted provider metadata.

The presence of an email address alone does not imply verification.

---

# 51. Session Independence

OAuth integration does not prescribe the application's login/session strategy.

The success hook may:

```text
set a session cookie
store a server-side session
issue a JWT
link an identity only
redirect elsewhere
```

---

# 52. Generated Routes as Infrastructure Routes

OAuth login and callback routes are framework infrastructure routes.

They should normally:

```text
participate in request logging
participate in tracing
participate in metrics
bypass application-login requirements
```

This prevents circular authentication such as requiring login to access `/auth/google`.

---

# 53. Server Hooks

Generated OAuth routes may still participate in:

```gpp
BeforeRequest
AfterRequest
```

unless explicitly excluded by server policy.

---

# 54. OpenAPI Integration

OAuth infrastructure routes do not need to appear as normal business API routes in OpenAPI.

However, OAuth-related security capabilities may contribute security metadata where relevant.

---

# 55. Swagger Integration

Swagger security configuration should only use the OAuth provider flow if it actually corresponds to the API authentication scheme.

Do not automatically equate:

```text
website login with Google
```

with:

```text
OAuth authorization used by external clients to access this API
```

Those are different concerns.

---

# 56. Duplicate Provider Configuration

This should normally be invalid:

```gpp
@{
    http.OAuth(http.OAuthProvider.Google)
    http.OAuth(http.OAuthProvider.Google)
}
```

Likewise duplicate explicit names:

```gpp
@{
    http.OAuth("corp", "https://one.example.com", "A", "B")
    http.OAuth("corp", "https://two.example.com", "C", "D")
}
```

should fail.

One provider name corresponds to one OAuth configuration per server.

---

# 57. Compiler Validation

The compiler should validate:

```text
annotation target
provider enum
explicit argument count/types
provider name syntax
duplicate provider names
generated route conflicts
success/failure hook signatures
```

Runtime configuration validates:

```text
environment values
provider discovery
provider metadata
network connectivity
```

---

# 58. Built-In Provider Metadata

For:

```gpp
http.OAuth(http.OAuthProvider.Google)
```

`gpp/http` owns:

```text
name
issuer / metadata URL
default credential env names
default scopes
adapter behavior
```

The provider enum is therefore more than a string constant: it identifies known framework configuration.

---

# 59. Provider Updates

Well-known provider endpoint/configuration updates belong to `gpp/http` releases.

Applications using:

```gpp
http.OAuth(http.OAuthProvider.Google)
```

should not need source changes merely because Google changes discovery or protocol details compatible with the abstraction.

---

# 60. Explicit Providers Remain Stable

Applications that want complete control over which issuer they target can use:

```gpp
http.OAuth(
    "google",
    "https://accounts.google.com",
    "GOOGLE_CLIENT_ID",
    "GOOGLE_CLIENT_SECRET",
    ...
)
```

instead of the enum convenience.

The explicit form remains the general escape hatch.

---

# 61. Example: Minimal Google Login

```gpp
class App : http.Server @{
    http.OAuth(http.OAuthProvider.Google)
} {

    func OAuthLogin(
        ctx *http.Context,
        identity http.OAuthIdentity,
    ) {
        user := DB.FindOrCreate(
            identity.Provider,
            identity.ID,
            identity.Email,
        )

        ctx.Session.Set("user_id", user.ID)
        ctx.Redirect("/")
    }
}
```

Runtime configuration:

```text
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
```

---

# 62. Example: Google With Additional Scope

```gpp
class App : http.Server @{
    http.OAuth(
        http.OAuthProvider.Google,
        "openid",
        "email",
        "profile",
        "https://www.googleapis.com/auth/calendar.readonly",
    )
}
```

No URL or credential environment names need to be specified.

---

# 63. Example: Custom Corporate Identity Provider

```gpp
class App : http.Server @{
    http.OAuth(
        "corp",
        "https://identity.company.com",
        "CORP_CLIENT_ID",
        "CORP_CLIENT_SECRET",
        "openid",
        "profile",
        "email",
    )
}
```

Automatic routes:

```text
GET /auth/corp
GET /auth/corp/callback
```

---

# 64. Example: Multiple Providers

```gpp
class App : http.Server @{
    http.OAuth(http.OAuthProvider.Google)

    http.OAuth(
        "employees",
        "https://login.company.com",
        "EMPLOYEE_OAUTH_ID",
        "EMPLOYEE_OAUTH_SECRET",
        "openid",
        "email",
        "profile",
    )
}
```

Automatic routes:

```text
/auth/google
/auth/google/callback

/auth/employees
/auth/employees/callback
```

Both produce the same:

```gpp
http.OAuthIdentity
```

application abstraction.

---

# 65. Provider Engine

Both annotation forms lower into the same conceptual configuration:

```text
OAuthConfig

    Name
    URL
    ClientIDEnv
    ClientSecretEnv
    Scopes
    ProviderAdapter
```

The remainder of the OAuth runtime is shared.

---

# 66. Compiler vs Runtime Responsibility

Compiler:

```text
parse annotations
resolve OAuthProvider enum
expand well-known provider metadata
validate static configuration
reserve routes
validate hooks
emit provider metadata
```

Runtime:

```text
resolve environment values
perform discovery
create state
perform PKCE
create nonce
redirect
exchange authorization code
validate tokens
retrieve identity
invoke hooks
```

---

# 67. Library Ownership

OAuth implementation belongs in:

```text
gpp/http
```

not as deep compiler magic.

The compiler understands the annotation and emits metadata.

The official HTTP library performs the protocol.

---

# 68. V1 Public Surface

The complete annotation surface should remain:

```gpp
OAuth(
    provider OAuthProvider,
    scopes ...string,
)

OAuth(
    name string,
    url string,
    clientIDEnv string,
    clientSecretEnv string,
    scopes ...string,
)
```

No additional OAuth annotation is necessary for the ordinary client/login case.

---

# 69. V1 Built-In Providers

Recommended initial provider enum:

```gpp
http.OAuthProvider.Google
http.OAuthProvider.Facebook
http.OAuthProvider.GitHub
http.OAuthProvider.Microsoft
http.OAuthProvider.Apple
```

Additional providers can be added later.

---

# 70. Non-Goals

This specification does not include:

```text
Go++ acting as an OAuth authorization server
Go++ acting as an OpenID Provider
dynamic client registration
client credentials flow
device authorization flow
service-account authentication
automatic account persistence
automatic account linking
automatic refresh-token storage
```

Provider-side OAuth/OpenID support belongs in a separate specification.

---

# 71. Design Summary

The normal well-known-provider experience is:

```gpp
class App : http.Server @{
    http.OAuth(http.OAuthProvider.Google)
}
```

The fully explicit form is:

```gpp
class App : http.Server @{
    http.OAuth(
        "corp",
        "https://identity.example.com",
        "CORP_CLIENT_ID",
        "CORP_CLIENT_SECRET",
        "openid",
        "email",
        "profile",
    )
}
```

The architecture is:

```text
OAuthProvider enum
        ↓
known URL + env names + defaults
        ↓
        OAuthConfig
             ↑
explicit name + URL + env names + scopes
             ↓
      shared OAuth/OIDC engine
             ↓
       OAuthIdentity
             ↓
       App.OAuthLogin
```

The core rules are:

> The well-known-provider overload is convenience only.

> The explicit `name, url, id, secret, scopes...` form is the canonical general-purpose primitive.

> Scopes belong directly in the OAuth annotation because they are part of the provider authorization request.

> The `url` identifies the provider issuer/metadata base rather than forcing applications to specify individual protocol endpoints.

> Credential arguments name environment variables rather than containing credentials.

> `gpp/http` handles the OAuth/OIDC protocol; the application handles users, sessions, persistence, account linking, and post-login behavior.
