# Standard library

Go++ ships with a small set of packages that build on Go's runtime and ecosystem. The implicit prelude supplies common collection, string, map, error, and I/O helpers; explicit packages cover scheduled jobs, HTTP services, SQL persistence, encoding, templates, and tests.

> **Evolution notice:** These packages are still evolving rapidly. APIs, behavior, and examples may change as Go++ develops, so expect updates and check the latest documentation before relying on them.

Each entry now has its own guide with an introduction, the problem it addresses, a Go comparison, and examples you can adapt. Start with the package that matches the work at hand:

| Entry | What it helps with |
| --- | --- |
| [Prelude](/guide/standard-library/prelude) | Everyday operations available without an import |
| [Cron](/guide/standard-library/cron) | Schedule annotated functions with explicit startup and shutdown |
| [ORM](/guide/standard-library/orm) | Map annotated Go++ models onto `database/sql` |
| [HTTP](/guide/standard-library/http) | Build servers and routes from classes and annotations |
| [Encoding](/guide/standard-library/encoding/) | Encode and decode JSON, YAML, and GOB |
| [Templates](/guide/standard-library/templates) | Render typed Go++ templates and HTTP pages |
| [Testing](/guide/standard-library/testing) | Organize tests as suites, methods, and annotations |

These packages are conveniences around familiar Go capabilities. They are designed to work alongside the standard library and existing Go packages, not to hide them.
