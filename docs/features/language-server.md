# Language server and diagnostics

The GPP language server gives editors source-aware feedback while you work in
`.gpp` files. It reports errors at their source location and offers completion,
hover, navigation, references, rename, formatting, symbols, and signature help.

## Get feedback on GPP source

The server analyzes open GPP documents directly. It keeps unsaved buffers in
memory, so editor feedback does not require a backend Go build on every edit.
For example, while editing an imported package call such as `os.Mkdir`, the
editor can offer package members; at a typed value such as `user.`, it can
offer the fields and methods visible on that type:

::: code-group

```text [Editor workflow]
Open app.gpp
Type `os.` or `user.`
Choose a completion or inspect a diagnostic at its GPP source location
```

```sh [Start the server]
gpp lsp
```

:::

When an error comes from generated Go, the compiler maps it back to the closest
available GPP location.

## Connect an editor

Configure the editor's language client to launch `gpp lsp` over standard input
and output. For troubleshooting, send logs to a file:

```sh
gpp lsp --log=/tmp/gpp-lsp.log
```

See the [tooling guide](/guide/tooling#language-server) and [language server specification](/reference/specifications/compiler.lsp).
