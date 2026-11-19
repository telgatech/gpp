# The GPP formatter

`gpp fmt` formats GPP source according to one canonical style. It preserves
comments and can check formatting in CI, keeping diffs consistent as new
syntax is introduced.

## Compare source formatting commands

`gofmt` remains the formatter for Go files. Use `gpp fmt` for `.gpp` files:

::: code-group
```sh [GPP]
gpp fmt ./...
```

```sh [Go]
gofmt -w ./...
```
:::

`gpp fmt` sorts import paths within each import group, like `gofmt`, while
preserving aliases, comments, and separate groups.

## Use check mode and standard output

Check mode reports unformatted source without rewriting it, which is useful
in CI. Standard output mode helps editor integrations and scripts:

```sh
gpp fmt --check ./...
gpp fmt --stdout main.gpp
```

Formatting behavior is shared with the language server. See [formatting in the tooling guide](/guide/tooling#formatting) and the [formatter specification](/reference/specifications/fmt).

Use [`gpp fiximports`](/guide/tooling#fixing-imports) to add resolvable missing
imports, remove unused named imports, and resolve package-name clashes
separately from formatting. GPP packages take priority; colliding
standard-library packages use aliases such as `stdhttp`.
