# Source documentation lookup

`gpp doc` searches Go++ declarations and displays their documentation without
requiring you to open generated Go. It helps keep API discovery close to the
source language, including for bundled packages.

## Compare source lookup commands

Go developers commonly use `go doc` for exported APIs. Go++ provides a
parallel command that understands `.gpp` declarations:

::: code-group
```sh [Go++]
gpp doc gpp/http.Server
```

```sh [Go]
go doc net/http.Server
```
:::

Search by name or topic when you do not know the declaration path:

```sh
gpp doc string.TrimSpace
gpp doc --search template
gpp doc --json gpp/http.Server
```

Lookup uses Go++ source documentation, so generated implementation names
remain an internal compiler detail. See [source documentation in the tooling guide](/guide/tooling#source-documentation).
