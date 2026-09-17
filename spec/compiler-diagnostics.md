# Go++ Source-Mapped Diagnostics Specification

## 1. Overview

Go++ compiler and backend diagnostics should point to original `.gpp` source locations rather than generated `.go` files wherever possible.

Current undesirable behavior:

```text
# generated
./string_interpolation.go:73:51: invalid operation: name * 2 (mismatched types string and untyped int)
go run failed: exit status 1
exit status 1
```

Desired behavior:

```text
examples/string_interpolation.gpp:18:24:
invalid operation: name * 2
mismatched types string and int

    "Value: {{name * 2}}"
               ^^^^^^^^
```

Generated Go source should remain available for compiler debugging, but ordinary Go++ users should see diagnostics in terms of their `.gpp` source.

---

# 2. Design Goal

The compiler should maintain this invariant:

> Every generated Go construct originating from Go++ source should retain a mapping to the narrowest meaningful original `.gpp` source span.

This mapping should survive:

```text
parsing
semantic analysis
lowering
Go AST generation
formatting
backend compilation
```

and be usable to rewrite backend Go diagnostics.

---

# 3. Diagnostic Sources

Go++ diagnostics come from two broad sources.

## Go++ compiler diagnostics

These are produced directly by:

```text
parser
semantic analyzer
type checker
overload resolver
inheritance resolver
template validator
record validator
annotation validator
```

These should already point directly to `.gpp` source.

Example:

```text
examples/app.gpp:42:12:
unknown field User.Nmae

did you mean User.Name?
```

These diagnostics should never mention generated Go files.

## Backend Go diagnostics

These originate from:

```text
go build
go run
go test
go/types
Go compiler
```

Examples include:

```text
invalid operation
cannot use X as Y
undefined symbol
interface mismatch
generic constraint error
native Go interop error
```

These require source remapping.

---

# 4. Source Span Model

Every relevant Go++ AST node should carry a source span.

Conceptually:

```go
type SourceSpan struct {
    File string

    StartLine int
    StartCol  int

    EndLine int
    EndCol  int
}
```

The span should identify the narrowest useful region in the original source.

Example:

```gpp
"Value: {{name * 2}}"
```

The interpolation expression:

```gpp
name * 2
```

should have its own `SourceSpan`.

The containing string literal may have a broader span.

---

# 5. Source Span Preservation

Source spans should be preserved through semantic and lowering phases.

Conceptually:

```text
Go++ AST node
    ↓
semantic node
    ↓
lowered node
    ↓
generated Go AST node
```

At each stage, generated nodes should retain an origin reference.

A generated node may originate from:

```text
one source node
multiple source nodes
compiler-generated synthetic code
```

Where possible, attach the most specific meaningful origin.

---

# 6. Generated Node Origin Table

The compiler should maintain an origin table associating generated Go nodes with `.gpp` spans.

Conceptually:

```go
map[ast.Node]SourceSpan
```

or a richer structure:

```go
type Origin struct {
    Source SourceSpan

    Kind OriginKind
}
```

where:

```go
type OriginKind int

const (
    OriginExact OriginKind = iota
    OriginDeclaration
    OriginSynthetic
)
```

Exact representation is implementation-defined.

---

# 7. Narrowest-Origin Rule

When generated Go code corresponds to a specific source expression, map to that expression rather than its enclosing declaration.

Example:

```gpp
result := "{{name * 2}}"
```

The generated Go expression corresponding to:

```gpp
name * 2
```

should map to the span of:

```gpp
name * 2
```

not merely to the entire assignment or function.

This allows precise diagnostics.

---

# 8. Declaration-Level Fallback

If a generated construct cannot be mapped to a specific expression, map it to the nearest meaningful enclosing declaration.

For example:

```text
generated interface wrapper
generated overload dispatcher
generated class method bridge
```

may map to:

```text
class declaration
method declaration
extension declaration
template declaration
record-returning function
```

rather than an arbitrary generated line.

---

# 9. Synthetic Code

Some generated Go constructs may have no direct user source equivalent.

Examples:

```text
runtime support
compiler-generated helper functions
vtable plumbing
temporary wrappers
internal record helpers
```

Such nodes should be marked as compiler-generated.

If a backend error occurs entirely inside synthetic code, the diagnostic should say so clearly.

Example:

```text
internal compiler-generated code failed to compile

this likely indicates a Go++ compiler bug
```

The compiler may also display the associated nearest source declaration if useful.

---

# 10. Go `//line` Directives

Go++ should use Go's standard `//line` directives where practical.

Example generated Go:

```go
//line examples/string_interpolation.gpp:18
_ = fmt.Sprint(name * 2)
```

This causes the Go compiler to report diagnostics using the `.gpp` filename and line number.

This provides a low-cost first layer of source remapping.

---

# 11. Purpose of `//line`

`//line` directives should be used to improve:

```text
backend filename mapping
backend line mapping
Go compiler error locations
go/types diagnostics
```

They should not be relied on as the sole diagnostic system.

They generally do not provide enough precision for:

```text
exact source spans
caret rendering
generated-name translation
multi-origin constructs
rich diagnostic context
```

Therefore they should be combined with a Go++ source map.

---

# 12. Hybrid Diagnostic Strategy

The preferred architecture is:

```text
generated Go
    +
Go //line directives
    +
Go++ generated-range → source-span map
```

The responsibilities are:

```text
//line
    gives backend .gpp filename/line automatically

source map
    provides precise column/span
    source excerpt
    semantic cleanup
    generated-name rewriting
```

This allows incremental implementation.

---

# 13. Initial Implementation Stage

A minimal first version may:

1. preserve source spans in the Go++ AST;
2. emit `//line` directives before generated constructs;
3. run the Go backend normally;
4. suppress generated-source framing;
5. print backend diagnostics using the `.gpp` locations produced by Go.

This alone should replace output like:

```text
./string_interpolation.go:73:51:
```

with:

```text
examples/string_interpolation.gpp:18:
```

---

# 14. Full Implementation Stage

A more complete implementation should additionally:

1. build generated-position mappings;
2. parse backend diagnostic locations;
3. locate the narrowest source span;
4. rewrite the diagnostic;
5. print source excerpts and carets;
6. translate generated internal names where possible.

---

# 15. Generated Position Mapping

Because `go/format` may change layout, mappings should reflect the final formatted Go source.

Do not assume an AST node's original `token.Pos` values automatically correspond to final emitted line/column positions.

The mapping must be derived from the final generated source.

---

# 16. Marker-Based Mapping

One robust implementation approach is to temporarily emit source-origin markers into generated Go.

Conceptually:

```go
/*gpp-origin:42*/
fmt.Sprint(name * 2)
```

where:

```text
42
```

indexes a source-origin table.

After `go/format`, the compiler can scan the formatted output and determine the final generated location of the marked construct.

The markers may then be stripped before backend compilation.

---

# 17. Comment-Based Markers

Origin markers may be emitted as Go comments attached to AST nodes.

Example:

```go
//gpp-origin:42
fmt.Sprint(name * 2)
```

Because `go/format` preserves comments, this can provide deterministic anchors in formatted output.

The compiler may then construct mappings such as:

```text
generated.go:73:20-73:51
    →
examples/string_interpolation.gpp:18:14-18:22
```

The exact marker format is internal and not part of the language contract.

---

# 18. Marker Removal

Compiler-internal mapping comments should not normally remain in final generated files passed to Go unless useful.

The compiler may:

```text
format with markers
build mapping
remove markers
compile clean generated Go
```

or retain them if harmless and hidden in temporary compiler output.

Normal user-visible generated Go from:

```bash
gpp build --emit-go
```

should preferably remain readable.

---

# 19. Backend Diagnostic Parsing

The compiler should parse backend diagnostics that contain locations such as:

```text
file.go:73:51: message
```

or:

```text
file.go:73: message
```

The parser should extract:

```text
file
line
column if present
message
```

and attempt remapping.

---

# 20. Diagnostic Remapping

Given a backend position:

```text
generated.go:73:51
```

the compiler should:

1. locate generated mapping ranges containing the position;
2. choose the narrowest matching generated range;
3. map to its source span;
4. replace the generated filename/position;
5. preserve or improve the backend message.

---

# 21. Range Selection

If multiple mappings contain the backend position, choose the most specific one.

Priority:

```text
exact expression
    before
statement
    before
declaration
    before
file-level fallback
```

This prevents broad declaration mappings from hiding a more useful expression span.

---

# 22. Source Excerpts

Where practical, remapped diagnostics should include a source excerpt.

Example:

```text
examples/string_interpolation.gpp:18:15:
invalid operation: name * 2
mismatched types string and int

    "Value: {{name * 2}}"
               ^^^^^^^^
```

The source excerpt should come from the original `.gpp` file.

---

# 23. Caret Rendering

For single-line spans:

```text
    "Value: {{name * 2}}"
               ^^^^^^^^
```

For a single token:

```text
    user.Nmae
         ^^^^
```

For multiline spans, highlight the most relevant backend-reported subexpression where possible.

---

# 24. Multiline Diagnostics

Example:

```gpp
value := {{
    subtotal +
    tax +
    shipping
}}
```

A diagnostic affecting `shipping` should ideally appear as:

```text
example.gpp:24:5:
cannot use string as numeric operand

    shipping
    ^^^^^^^^
```

rather than pointing only to the start of the interpolation.

---

# 25. Preserve Backend Message

Go backend messages are often useful and should generally be retained.

Example backend:

```text
invalid operation: name * 2 (mismatched types string and untyped int)
```

Go++ may reformat this as:

```text
invalid operation: name * 2
mismatched types string and int
```

but should preserve the important semantic information.

---

# 26. Cleanup of Go-Specific Noise

Go++ may simplify backend wording where the Go representation leaks implementation details.

Examples:

```text
untyped int
generated helper identifiers
internal record names
lowered class implementation names
```

Where safe, rewrite them into Go++ terminology.

---

# 27. Untyped Constant Wording

Example Go message:

```text
mismatched types string and untyped int
```

may be displayed as:

```text
mismatched types string and int
```

if doing so does not lose relevant meaning.

This is a presentation improvement, not a semantic change.

---

# 28. Generated Record Names

Backend diagnostics may mention names such as:

```text
__gpp_record_a81f09c2
```

These should be translated where possible.

Example:

```text
cannot use __gpp_record_a81f09c2 as __gpp_record_77fd92aa
```

may become:

```text
cannot use:

    record{Name string, Age int}

as:

    record{Name string, Age string}
```

The compiler already has structural record metadata and can use it for presentation.

---

# 29. Generated Class Names

If lowering creates internal class/runtime names, diagnostics should prefer source-level class names.

Prefer:

```text
cannot use User as Admin
```

rather than:

```text
cannot use *__gpp_User_impl as *__gpp_Admin_root
```

where the mapping is unambiguous.

---

# 30. Extension Method Diagnostics

If an extension call:

```gpp
name.TrimSpace()
```

lowers to a generated helper, diagnostics should still point to:

```gpp
name.TrimSpace()
```

not to the generated helper call.

The extension call expression should own the source mapping.

---

# 31. Automatic Error Promotion Diagnostics

If:

```gpp
data := os.ReadFile(path)
```

lowers into temporary variables and thrown-error checks, backend errors involving the promoted value should map back to the original call:

```gpp
os.ReadFile(path)
```

Compiler-generated error-promotion plumbing should not leak into normal diagnostics.

---

# 32. Interpolation Diagnostics

Interpolated expressions require fine-grained origin mapping.

Example:

```gpp
message := "Value: {{name * 2}}"
```

Generated formatting code should map the binary expression:

```go
name * 2
```

back to:

```gpp
name * 2
```

not merely to the whole generated `fmt` call.

This should be a priority case because interpolation lowering often introduces significant generated code.

---

# 33. Template Diagnostics

Errors originating from compile-time `.gpp.tpl` parsing or validation should point directly to the `.gpp.tpl` source.

Example:

```text
views/blog.gpp.tpl:18:14:
unknown field Post.Ttile

did you mean Post.Title?
```

Backend generated Go used to support template wrappers should not leak into ordinary template diagnostics.

---

# 34. Record Diagnostics

Record lowering should preserve source mapping for:

```text
record literals
record field expressions
record assignments
record return expressions
```

Example:

```gpp
record(
    Name: value,
    Age: "42",
)
```

should point to the offending field expression where possible.

---

# 35. Overload Diagnostics

Generated overload dispatch/wrappers may trigger backend errors.

These should map back to:

```text
the call site
or
the overload declaration
```

depending on which is more meaningful.

Generated dispatcher names should not normally appear.

---

# 36. Inheritance Diagnostics

Generated class/vtable lowering may produce backend type errors.

These should map to:

```text
class declaration
method declaration
inheritance clause
call site
```

rather than internal vtable structures.

---

# 37. Cross-Package Diagnostics

Mappings must preserve original package-relative or project-relative `.gpp` paths.

Example:

```text
users/model.gpp:42:16:
...
```

not:

```text
/tmp/gpp-build-392810/generated/users/model.go:173:22
```

Temporary build paths should not leak into normal diagnostics.

---

# 38. Path Presentation

Prefer project-relative source paths when possible.

Example:

```text
examples/string_interpolation.gpp
```

rather than:

```text
/Users/a/dev/gopp/examples/string_interpolation.gpp
```

Absolute paths may be used where required by tooling or explicit debug output.

---

# 39. Backend Invocation Output

Normal Go backend framing should be suppressed where it adds no value.

Do not show:

```text
# generated
go run failed: exit status 1
exit status 1
```

for ordinary compile failures.

Prefer only the actual diagnostic.

---

# 40. Process Failure Message

If the backend process fails without a useful compiler diagnostic, Go++ may report:

```text
Go backend failed: exit status 1
```

but only when no more specific error is available.

Avoid printing duplicate exit-status messages.

---

# 41. Compiler Debug Mode

A debug mode should allow developers working on the compiler to see raw backend diagnostics.

Possible forms:

```bash
gpp run --debug
```

or:

```bash
gpp run --show-go-errors
```

Debug output may include:

```text
raw Go diagnostic
generated file path
generated line/column
mapped .gpp span
origin identifier
```

Normal users should not need this.

---

# 42. `--emit-go`

When:

```bash
gpp build --emit-go
```

is used, generated Go should remain inspectable.

It may include useful comments such as:

```go
// source: examples/foo.gpp:18
```

if desired.

Internal marker noise should not unnecessarily reduce readability.

---

# 43. Diagnostic Severity

The diagnostic framework should support:

```text
error
warning
note
help
```

even if v1 mostly emits errors.

Example:

```text
error:
invalid operation: name * 2

note:
name has type string

help:
convert the value explicitly before multiplication
```

This enables richer compiler diagnostics later.

---

# 44. Diagnostic Structure

Internally, prefer structured diagnostics.

Conceptually:

```go
type Diagnostic struct {
    Severity Severity

    Message string

    Primary SourceSpan

    Secondary []LabeledSpan

    Notes []string
    Help  []string
}
```

Backend diagnostics should be converted into this representation after source mapping.

---

# 45. Primary Span

Each diagnostic should have one primary source span where possible.

Example:

```gpp
name * 2
```

The primary span should identify the expression causing the error.

---

# 46. Secondary Spans

Future diagnostics may include secondary source references.

Example:

```text
cannot override method with incompatible signature

    child.gpp:22
        func Save(id string)
        ^^^^^^^^^^^^^^^^^^^^

    base.gpp:8
        func Save(id int)
        ^^^^^^^^^^^^^^^^^
```

The mapping infrastructure should not prevent this richer model.

---

# 47. Source File Availability

The compiler should retain source text for all `.gpp` files participating in a build.

This allows diagnostics to display excerpts without reopening or rediscovering files later.

A source manager may maintain:

```text
file ID
path
contents
line offsets
```

---

# 48. Source Manager

A central source manager is recommended.

Conceptually:

```go
type SourceManager struct {
    Files map[FileID]*SourceFile
}
```

where each source file contains:

```go
type SourceFile struct {
    Path string
    Text string

    LineOffsets []int
}
```

This supports fast conversion between:

```text
byte offsets
line/column
source excerpts
```

---

# 49. Span Storage

AST nodes may store compact source positions rather than repeated strings.

For example:

```go
type Pos struct {
    File FileID
    Offset int
}
```

and:

```go
type Span struct {
    Start Pos
    End   Pos
}
```

Line/column can then be derived through the source manager.

This may be simpler and more efficient than storing line/column everywhere.

---

# 50. Mapping Granularity

The compiler does not need to map every emitted token.

Prioritize meaningful constructs:

```text
expressions
calls
selectors
binary operations
assignments
returns
type expressions
method declarations
template calls
record literals
extension calls
```

Generated punctuation and boilerplate need not have independent mappings.

---

# 51. Exact vs Approximate Mapping

Mappings may be classified as:

```text
exact
approximate
synthetic
```

Exact:

```text
generated expression directly corresponds to source expression
```

Approximate:

```text
generated helper corresponds to a source declaration
```

Synthetic:

```text
no meaningful source equivalent
```

Diagnostic rendering may prefer exact mappings over approximate ones.

---

# 52. Formatting and Mapping

If using `go/format`, origin markers or a post-format mapping pass must ensure final generated positions are known.

Do not use pre-format line numbers for backend remapping.

The final compilation unit is authoritative.

---

# 53. Multiple Generated Nodes from One Source Node

One source construct may lower to several Go statements.

Example:

```gpp
data := os.ReadFile(path)
```

may lower to:

```text
temporary assignment
error check
throw wrapper
value extraction
```

All generated pieces may map back to the same source call span.

This is valid.

---

# 54. One Generated Node from Multiple Source Nodes

Occasionally one generated construct may aggregate several source constructs.

The compiler should map it to:

```text
the most relevant primary source span
```

and optionally attach additional related spans if the diagnostic system supports them.

---

# 55. Backend Diagnostics Already Using `.gpp` Paths

If `//line` causes Go to report:

```text
example.gpp:18:...
```

the Go++ compiler should still process the diagnostic.

It may:

```text
add exact column mapping
add source excerpt
rewrite generated type names
clean message formatting
```

`//line` output should not bypass the Go++ diagnostic formatter.

---

# 56. Template Source Paths

`.gpp.tpl` source files should participate in the same source manager and diagnostic infrastructure.

Examples:

```text
views/blog.gpp.tpl
views/admin.gpp.tpl
```

Template diagnostics should use the same formatting style as `.gpp` diagnostics.

---

# 57. Mixed `.gpp` and `.go` Packages

If an error genuinely originates in handwritten `.go` source, preserve the `.go` location.

Example:

```text
native/helper.go:44:12:
...
```

Do not remap handwritten Go errors to unrelated `.gpp` source.

---

# 58. Native Go Interop Errors

If a `.gpp` call into native Go causes a type mismatch, map the diagnostic to the `.gpp` call site.

Example:

```gpp
native.Func(value)
```

should point to that call expression even though the signature comes from native Go.

---

# 59. Compiler Bug Diagnostics

If backend compilation fails in synthetic generated code and no meaningful source mapping exists, report:

```text
Go++ compiler generated invalid Go code.

internal location:
    <generated path>:line:column

nearest source:
    app.gpp:42
```

and suggest:

```text
rerun with --emit-go or --debug
```

This distinguishes user mistakes from compiler defects.

---

# 60. Example: Interpolation Error

Source:

```gpp
func main() {
    name := "Bob"

    println("Value: {{name * 2}}")
}
```

Desired diagnostic:

```text
examples/string_interpolation.gpp:4:23:
invalid operation: name * 2
mismatched types string and int

    println("Value: {{name * 2}}")
                       ^^^^^^^^
```

No generated Go filename should appear in normal mode.

---

# 61. Example: Native Call Error

Source:

```gpp
strings.Repeat(42, 3)
```

Desired diagnostic:

```text
example.gpp:12:16:
cannot use int as string

    strings.Repeat(42, 3)
                   ^^
```

---

# 62. Example: Record Error

Source:

```gpp
a := record(
    Name: "Bob",
    Age: 42,
)

b := record(
    Name: "Alice",
    Age: "42",
)

a = b
```

Desired diagnostic:

```text
example.gpp:11:5:
incompatible record types

expected:
    record{Name string, Age int}

got:
    record{Name string, Age string}

    a = b
        ^
```

The exact wording may come from Go++ semantic analysis rather than the backend.

---

# 63. Example: Synthetic Failure

If generated runtime code fails:

```text
Go++ compiler generated invalid Go code.

nearest Go++ source:
    app.gpp:18:1

run with:
    gpp build --emit-go --debug

to inspect generated backend output.
```

---

# 64. Performance

Source mapping should not materially affect runtime performance of compiled applications.

It is purely a compiler/build-time feature.

Build-time overhead should remain small enough for normal development use.

---

# 65. Mapping Persistence

Source maps only need to live for the duration of the build/run/test operation unless explicitly emitted for tooling.

A future debug-artifact format may persist mappings, but this is not required for v1.

---

# 66. `gpp run`

`gpp run` should:

```text
compile
capture backend diagnostics
remap
print Go++ diagnostics
```

It should not invoke `go run` in a way that bypasses diagnostic processing.

The compiler should own the child process output.

---

# 67. `gpp build`

`gpp build` should apply the same diagnostic remapping behavior.

Backend errors should always be presented as Go++ source diagnostics where possible.

---

# 68. `gpp test`

`gpp test` should use the same source mapping system.

This includes:

```text
test compilation errors
generated _test.go wrapper errors
suite lowering errors
native Go test compilation errors
```

Generated test wrapper failures should map back to the relevant `.gpp` test method where possible.

---

# 69. Tooling Reuse

The source manager and diagnostic structures should be reusable by:

```text
compiler
formatter
language server
IDE tooling
gpp test
template validator
generated-code debugger
```

Avoid building separate location systems for each subsystem.

---

# 70. Recommended Implementation Order

1. Add/standardize source spans on all Go++ AST nodes.
2. Introduce a central source manager.
3. Preserve origin spans during lowering.
4. Emit `//line` directives.
5. Capture backend diagnostics instead of printing raw process output.
6. Remove generated-path/exit-status noise.
7. Add final generated-position mappings.
8. Remap columns/spans precisely.
9. Add source excerpts and carets.
10. Translate generated internal type names.
11. Add debug/raw-backend mode.
12. Extend the same system to `gpp test` and `.gpp.tpl`.

---

# 71. V1 Minimum

The minimum acceptable v1 behavior is:

```text
backend compile errors point to .gpp filename + line
generated file paths are hidden
duplicate "exit status 1" output is removed
```

using `//line` directives and backend-output capture.

---

# 72. Target Experience

The target experience is:

```text
examples/string_interpolation.gpp:18:24:
invalid operation: name * 2
mismatched types string and int

    "Value: {{name * 2}}"
               ^^^^^^^^
```

rather than:

```text
# generated
./string_interpolation.go:73:51: invalid operation: name * 2 (mismatched types string and untyped int)
go run failed: exit status 1
exit status 1
```

---

# 73. Design Summary

The diagnostic architecture is:

```text
.gpp source
    ↓
source manager + spans
    ↓
Go++ AST
    ↓
lowering with origin metadata
    ↓
generated Go
    ↓
Go //line directives
    +
generated-range/source-span mapping
    ↓
Go backend diagnostic
    ↓
Go++ diagnostic formatter
    ↓
original .gpp location + source excerpt
```

The central rule is:

> Generated Go is an implementation detail. Normal diagnostics should speak in terms of the Go++ source the programmer actually wrote.
