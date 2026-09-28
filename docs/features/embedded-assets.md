# Embedded assets

Embedding places files inside the compiled program. Go++ provides a compact
declaration for exposing a directory as `fs.FS` or a file as bytes, using Go's
embed mechanism underneath.

## Compare Go directives and Go++ declarations

Go directives remain available. Go++ groups related embedded values into one
source declaration:

::: code-group
```go [Go++]
embed (
    Assets "public/"
    Schema "schema.sql"
)
```

```go [Go]
import "embed"

//go:embed public
var assets embed.FS

//go:embed schema.sql
var schema []byte
```
:::

Read an embedded directory through standard filesystem interfaces:

```go
index := fs.ReadFile(Assets, "index.html")
fmt.Println(string(index))
```

Directories become rooted `fs.FS` values and individual files become `[]byte`.
Read failures propagate automatically unless the caller captures the returned
error.
See the [embed specification](/reference/specifications/embed) for path and
validation details.
