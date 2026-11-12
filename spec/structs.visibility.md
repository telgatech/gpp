# GPP Struct Field Visibility Specification

## Struct field visibility

Struct fields follow normal GPP / Go capitalization-based visibility rules.

```gpp
struct(
    Name: "Bob",
    Age: 42,
    internal: "debug",
)
```

has fields with the following visibility:

```text
Name       exported/public
Age        exported/public
internal   package-private
```

Anonymous structs lower to compiler-generated Go structs while preserving field names exactly.

Conceptually:

```go
type __gpp_struct_<hash> struct {
    Name     string
    Age      int
    internal string
}
```

Therefore standard Go export rules apply naturally.

This rule is part of an anonymous struct's structural type. Field names are case-sensitive, so:

```gpp
struct(Name: "Bob")
```

and:

```gpp
struct(name: "Bob")
```

are different struct shapes.

Reflection-based consumers such as `html/template`, `encoding/json`, and handwritten Go code can access exported fields according to normal Go rules.

For data intended to cross package boundaries or be consumed by templates/serialization, exported field names should normally be used:

```gpp
struct(
    Code: 404,
    Message: "Page not found",
    Path: request.URL.Path,
)
```

allowing:

```gotemplate
{{.Code}}
{{.Message}}
{{.Path}}
```

Lowercase fields remain valid and useful for package-internal struct data.
