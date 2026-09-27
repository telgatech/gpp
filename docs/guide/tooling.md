# Tooling

The `gpp` CLI treats compiler tooling as part of the language experience.

## Formatting

Format one file, a project, or standard input/output:

```bash
gpp fmt app.gpp
gpp fmt --check .
gpp fmt --stdout app.gpp
```

Formatting is deterministic, comment-preserving, and shared with the language
server. `--check` is suitable for CI.

## Testing

Go++ tests use the bundled `gpp/test` library and ordinary Go test execution:

```bash
gpp test examples/testing.gpp
gpp test --tag crud examples/testing.gpp
gpp test --priority high examples/testing.gpp
```

## Source documentation

Inspect source-level declarations without browsing generated Go:

```bash
gpp doc gpp/http.Server
gpp doc string.TrimSpace
gpp doc --json gpp/http.Server
gpp doc --search template
```

## Language server

Start the LSP server over standard input/output:

```bash
gpp lsp
gpp lsp --log=/tmp/gpp-lsp.log
```

The server supports diagnostics, completion, hover, navigation, references,
rename, symbols, formatting, and signature help. Open-document overlays stay
in memory, so editor feedback does not require a Go backend build on every
change.

## Diagnostics

Compiler errors point back to the original `.gpp` source using source spans,
rather than asking users to edit generated Go. If a backend Go error remains,
the compiler maps it back to the closest available Go++ location.

## Developing this documentation site

Install the documentation dependencies and start VitePress:

```bash
npm install
npm run docs:dev
```

Build and preview the static site:

```bash
npm run docs:build
npm run docs:preview
```

The `docs:sync` step mirrors every Markdown file in `spec/` into the site’s
reference section before development and production builds.
