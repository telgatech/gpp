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

## Visual Studio Code syntax highlighting

For Go++ syntax highlighting in Visual Studio Code, see the
[Go++ VS Code extension](https://github.com/telgatech/gpp-vscode).

## Diagnostics

When compilation fails, the compiler should point to the offending line in the
original `.gpp` source, including when the error comes from generated Go. You
should not have to track down the corresponding generated file yourself. If a
diagnostic points to the wrong line or only points into generated Go, please
raise a ticket with the source and full error output so it can be fixed.

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
