# Go++ Classifieds

This is a small but deployable-style classified listings application. It
demonstrates:

- `gpp/orm` models with validation and lifecycle hooks;
- SQLite persistence with foreign keys and indexes;
- ORM `Get`, `Select`, `Insert`, `Update`, and `Delete` operations;
- `gpp/http` routes, status-aware responses, redirects, health checks, and
  custom error templates;
- generated OpenAPI JSON at `/openapi.json` and self-contained Swagger UI at
  `/swagger`;
- external `classifieds.gpp.tpl` templates using standard `html/template`
  syntax.

Run it from the repository root:

```bash
gpp run apps/classifieds
```

The server uses `gpp/http.Server.Listen()`, binds to loopback, and receives a
dynamically assigned port. The default database is `classifieds.db`.
Configure the database path with:

```bash
CLASSIFIEDS_DB=/var/lib/classifieds/classifieds.db \\
gpp run apps/classifieds
```

Build a native executable with:

```bash
gpp build apps/classifieds -o ./classifieds
```

The example seeds a demo seller, category, and listing on an empty database.
Authentication, authorization, CSRF protection, migrations, and production
observability would be the next application-layer additions before exposing
it to untrusted users.
