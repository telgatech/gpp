# Go++ Native Type Resolver

## Purpose

The compiler must be able to determine the static type of ordinary Go
expressions before applying Go++ transformations such as extension methods,
implicit error propagation, overload selection, and lambda inference.

This is a compiler capability, not a new runtime or type-system feature.
Generated Go++ programs continue to use the original Go types.

## Required behavior

The resolver must propagate types through expression chains. These forms must
be equivalent from the resolver's perspective:

```gpp
var title string = ctx.Request.FormValue("title")
title.TrimSpace()
```

and:

```gpp
ctx.Request.FormValue("title").TrimSpace()
```

The second form must resolve `FormValue` to `string`, then resolve
`TrimSpace` as a string extension.

## Resolution order

For an expression, resolution proceeds from the innermost receiver outward:

1. Go++ local variables, parameters, class fields, and class methods;
2. Go++ extension-call results;
3. imported Go++ package functions and methods;
4. imported native Go package functions and methods;
5. exported fields on imported native Go structs;
6. literals, composite literals, and other existing syntax rules.

Native methods and fields are inspected using Go type information. The
resolver must not contain one-off knowledge of `http.Request`, `os.Getenv`,
or any other particular library API.

## Native package functions

For an imported function such as:

```gpp
os.Getenv("PORT")
```

the resolver records its complete result list, including functions that do not
return an error. A function's result type is useful even when no implicit
error promotion applies.

## Native methods and fields

Given:

```gpp
func Read(response *http.Response) string {
    return response.Request.URL.Path
}
```

the resolver must walk `response.Request.URL.Path`, resolving each field from
the previous expression's native type. It must also resolve methods such as:

```gpp
response.Body.ReadAll()
```

when `ReadAll` is a Go++ extension on `io.Reader`.

Pointer and value method sets follow Go's normal rules. Exported fields and
methods are eligible; inaccessible native members are not exposed by the
resolver.

## Import aliases

Type lookup must respect source aliases:

```gpp
import stdhttp "net/http"

func Handle(request *stdhttp.Request) string {
    return request.URL.Path
}
```

The resolver maps the source qualifier to its import path and uses a matching
qualifier when formatting discovered result types. It must not assume that a
package is always referenced by its default package name.

## Extension integration

Extension lookup consumes the resolver's result directly. It must support:

```gpp
ctx.Request.FormValue("title").TrimSpace()
os.Getenv("NAME").ToLower()
response.Body.ReadAll()
```

Nested extension calls are lowered without requiring temporary variables.
Native methods take precedence over extensions according to the existing
extension-resolution rules.

## Unknown and ambiguous types

If a type cannot be resolved statically, the compiler should leave the
expression unchanged and allow normal Go compilation to report the resulting
error. It must not guess a type or silently select an unrelated extension.

If multiple native declarations are possible, resolution must follow Go's
normal import and method-set rules. Ambiguous Go++ extensions remain compiler
errors under the existing extension rules.

## Compatibility

This feature does not change:

- Go or Go++ runtime representations;
- Go import semantics;
- native method precedence;
- extension declaration syntax;
- `--no-prelude` behavior.

It only improves compile-time type information available to existing lowering
passes.

## Non-goals

The initial resolver does not attempt to become a complete replacement for
`go/types` in all compiler phases. It does not infer arbitrary generic type
arguments, perform whole-program data-flow analysis, or expose unexported Go
members.

## Acceptance examples

The compiler must build programs containing all of the following without
explicit temporary type annotations:

```gpp
title := ctx.Request.FormValue("title").TrimSpace()
name := os.Getenv("NAME").TrimSpace()
path := response.Request.URL.Path
body, err := response.Body.ReadAll()
```

Regression tests must cover direct calls, selector chains, import aliases,
extension chaining, and native methods whose result does not end in `error`.

