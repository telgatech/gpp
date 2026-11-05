# Changelog

Notable Go++ changes, with links to the documentation for each update.

## v0.2.0

- <time datetime="2026-11-05T13:54:46+05:30">2026-11-05 13:54 IST</time> — **Typed database queries.** Generic `Get[T]` and `Select[T]` queries now return model values, scalars, or inline structural records. [Details](/features/orm#typed-queries-with-structural-records)
- <time datetime="2026-11-05T13:54:46+05:30">2026-11-05 13:54 IST</time> — **Model query and persistence helpers.** ORM models inherit find, order, and cursor paging helpers; instances support `Insert`, `Update`, and `Destroy`. [Details](/features/orm#common-model-operations)
- <time datetime="2026-11-05T13:54:46+05:30">2026-11-05 13:54 IST</time> — **Exception handling fixes.** Fixed error promotion for generic calls and imported methods that return a trailing error. [Details](/features/exceptions)
- <time datetime="2026-11-05T13:54:46+05:30">2026-11-05 13:54 IST</time> — **Incremental builds and warning control.** Builds reuse generated output for unchanged packages, and compiler warnings can be enabled with `gpp build -warnings`. [Details](/features/project-cli)
- <time datetime="2026-11-05T13:54:46+05:30">2026-11-05 13:54 IST</time> — **Inherited static self types.** Static methods can use lowercase `self` to refer to the concrete class at the call site, including through inheritance. [Details](/features/static-methods)
