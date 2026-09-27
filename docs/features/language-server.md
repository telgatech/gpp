# Language server and diagnostics

The Go++ language server gives editors source-aware feedback while you work in
`.gpp` files. It reports errors at their source location and offers completion,
hover, navigation, references, rename, formatting, symbols, and signature help.

## Get feedback on Go++ source

The server analyzes open Go++ documents directly. It keeps unsaved buffers in
memory, so editor feedback does not require a backend Go build on every edit:

::: code-group

```text [Editor workflow]
Open app.gpp
Edit a declaration
See Go++ diagnostics and completion in that file
```

```sh [Start the server]
gpp lsp
```

:::

When an error comes from generated Go, the compiler maps it back to the closest
available Go++ location.

## Connect an editor

Configure the editor's language client to launch `gpp lsp` over standard input
and output. For troubleshooting, send logs to a file:

```sh
gpp lsp --log=/tmp/gpp-lsp.log
```

See the [tooling guide](/guide/tooling#language-server) and [language server specification](/reference/specifications/compiler.lsp).
