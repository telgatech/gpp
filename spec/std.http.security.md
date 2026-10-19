# Go++ Standard Library — Secure-by-Default HTTP Server

## Objective

Improve the existing `http.Server` class in the Go++ standard library so that applications automatically benefit from modern HTTP and browser security protections without requiring developers to understand, configure, or explicitly enable each protection.

**The guiding principle: application developers should write application logic, not HTTP security infrastructure.**

This is an enhancement to the existing `http.Server`, not a new framework, server abstraction, or separate security library.

## Step 1 — Inspect the existing implementation

Before making changes:

- Locate the existing `http.Server` class and its Go runtime implementation.
- Understand route registration, request dispatch, response generation, template rendering, static assets, sessions (if present), and error handling.
- Identify security protections already provided by the existing code or Go's underlying `net/http` package.
- Identify missing safeguards, insecure defaults, and potential compatibility problems.
- Review existing tests and public APIs.

Reuse existing facilities wherever possible. Avoid duplicating functionality.

## Step 2 — Implement secure defaults

Introduce a standard, opinionated security baseline covering the following areas.

### A. HTTP request and connection hardening

- Safe read-header, read, write, and idle timeouts.
- Maximum request header and body sizes.
- Connection and request concurrency protection.
- Slow-client and request-flood defenses.
- Safe handling of malformed requests.
- Safe HTTP error responses without internal stack traces.
- Bounded memory consumption for request-related state.

Consider streaming endpoints, large uploads, WebSockets, SSE, and long-running requests when selecting defaults.

### B. Browser security protections

Provide appropriate security headers automatically:

- `X-Content-Type-Options: nosniff`
- `Content-Security-Policy`
- `Referrer-Policy`
- `X-Frame-Options` or equivalent CSP `frame-ancestors` protection
- `Strict-Transport-Security` where HTTPS deployment is confirmed
- Other modern, broadly applicable browser protections where appropriate

Avoid obsolete headers, ineffective security theater, and unnecessarily restrictive defaults.

Make sure security headers also apply to relevant error responses and static-file responses.

### C. Cross-origin security

- Default to same-origin behavior.
- Do not automatically allow arbitrary CORS origins.
- Handle CORS preflight requests correctly when cross-origin access is explicitly enabled.
- Avoid unsafe wildcard/credential combinations.
- Support narrowly scoped, deliberate cross-origin exceptions.

Remember that CORS restricts browser access to responses; it is not authentication or general request authorization.

### D. Cross-site scripting defenses

- Ensure HTML and template output uses context-appropriate escaping by default.
- Avoid unsafe injection of user-controlled content into scripts, attributes, and URLs.
- Generate an appropriate CSP baseline wherever possible.
- Preserve legitimate application functionality without silently disabling security.

Do not claim that an HTTP server can universally prevent XSS in arbitrary raw HTML supplied by applications.

### E. CSRF protection

If the existing standard library provides cookie-based sessions or form handling, integrate appropriate CSRF protection with those facilities.

- Protect state-changing requests where relevant.
- Use secure token and/or origin-based validation as appropriate.
- Avoid breaking APIs that use non-cookie authentication.
- Account for reverse proxies and trusted origins.

If reliable automatic CSRF protection is not possible with the current APIs, document the dependency and propose the smallest necessary integration.

### F. Rate limiting and abuse prevention

Implement practical defaults for:

- Excessive requests from individual clients.
- Bursts of traffic.
- Excessive concurrent requests.
- Memory usage associated with rate-limit tracking.

Use bounded storage and concurrency-safe algorithms.

Handle reverse proxies carefully: never trust arbitrary forwarded IP headers supplied by an untrusted client.

Make limits adjustable for legitimate high-traffic applications. Document limitations for distributed deployments.

### G. HTTPS and cookies

- Apply secure TLS defaults when TLS is managed by the server.
- Enable HSTS only when the deployment can safely support it.
- Ensure framework-managed cookies use sensible `Secure`, `HttpOnly`, and `SameSite` defaults appropriate to their purpose.
- Avoid trusting arbitrary proxy headers to determine whether a request is secure.

Do not attempt to rewrite application-created cookies or change their semantics without evaluating compatibility.

## Step 3 — Design the public API

For ordinary applications, no security setup should be required.

The existing usage pattern should remain valid:

```go
server := http.Server(port: 8080)

// Register application routes.

server.start()
```

Do not introduce mandatory security annotations, middleware registration, configuration files, or new initialization steps.

Provide a small, coherent opt-out or customization interface for exceptional cases, following existing Go++ standard-library conventions.

Avoid exposing dozens of independent configuration options unless genuinely necessary.

## Step 4 — Compatibility and architecture

- Preserve existing public APIs wherever possible.
- Avoid introducing a new HTTP framework.
- Keep implementation complexity reasonable.
- Prefer standard-library dependencies and established algorithms.
- Ensure protections are applied consistently across all relevant request paths.
- Do not break development over plain localhost HTTP.
- Distinguish protections enforceable by the server from those requiring integration with templates, sessions, deployment, or application-specific logic.

If a default would break legitimate existing applications, evaluate a compatible alternative or clearly report the issue rather than silently weakening the security model.

## Step 5 — Tests

Add automated tests covering:

- Security headers on normal and error responses.
- Rejected oversized requests and headers.
- Slow requests and timeout behavior.
- Rate limiting and concurrent access.
- Memory bounds and limiter eviction.
- CORS defaults, preflight handling, and credentials.
- HTML escaping where supported.
- CSRF protection where applicable.
- HTTPS and HSTS behavior.
- Reverse-proxy spoofing attempts.
- Streaming and WebSocket compatibility.
- Existing application compatibility.

Include adversarial tests, not merely successful configuration tests.

Run existing tests, new tests, race detection where applicable, and relevant Go security/static-analysis tools.

## Step 6 — Deliverables

Provide:

1. A short summary of existing protections and identified gaps.
2. An implementation plan organized by priority.
3. Changes to the existing `http.Server` and associated runtime components.
4. Regression tests and documentation.
5. A concise description of all default behaviors and available overrides.
6. Known limitations and protections that cannot be guaranteed by the server alone.

## Working approach

First inspect the repository and present your findings and implementation strategy.

Then implement the agreed design in the existing codebase.

Prefer substantive, well-tested improvements over an extensive but fragile collection of security features.

**Success criterion: a developer using Go++ `http.Server` receives a strong, maintainable baseline of HTTP security protections without writing a single line of security configuration.**
