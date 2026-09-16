## Record field visibility

Record fields follow normal Go++ / Go capitalization-based visibility rules.

```gpp
record(
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

Records lower to compiler-generated Go structs while preserving field names exactly.

Conceptually:

```go
type __gpp_record_<hash> struct {
    Name     string
    Age      int
    internal string
}
```

Therefore standard Go export rules apply naturally.

This rule is part of the record's structural type. Field names are case-sensitive, so:

```gpp
record(Name: "Bob")
```

and:

```gpp
record(name: "Bob")
```

are different record shapes.

Reflection-based consumers such as `html/template`, `encoding/json`, and handwritten Go code can access exported record fields according to normal Go rules.

For data intended to cross package boundaries or be consumed by templates/serialization, exported field names should normally be used:

```gpp
record(
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

Lowercase fields remain valid and useful for package-internal record data.
