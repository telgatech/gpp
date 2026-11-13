# GPP String Interpolation and Formatting Specification

## 1. Overview

GPP interpolates expressions in double-quoted strings. Backtick raw strings
preserve their contents literally, following Go's raw-string behavior.

Interpolation uses:

```text
{{ expression }}
```

and may optionally include a Go `fmt` format specifier:

```text
{{ expression : format }}
```

Examples:

```gpp
name := "Bob"

message := "Hello {{name}}"

priceText := "Price: {{price:%.2f}}"

html := `<h1>{{title}}</h1>`
```

The goals are:

* remove common `fmt.Sprintf` boilerplate;
* preserve Go formatting conventions;
* support arbitrary GPP expressions;
* follow Go's string-literal behavior for raw strings;
* support multiline expressions inside interpolated strings;
* avoid introducing a new formatting mini-language.

---

# 2. Supported String Forms

Interpolation applies to double-quoted Go string literals. Backtick raw strings
are literal and do not interpolate.

## Double-quoted strings

```gpp
"Hello {{name}}"
```

Double-quoted strings preserve normal Go interpreted-string behavior.

For example:

```gpp
"Hello\n{{name}}"
```

contains an interpreted newline escape.

## Backtick raw strings

```gpp
`Hello {{name}}`
```

This string contains the literal text `{{name}}`. Raw strings preserve their
contents exactly and are useful for:

* HTML fragments;
* SQL;
* multiline messages;
* generated configuration;
* shell snippets;
* structured text.

---

# 3. Core Interpolation Syntax

Basic interpolation:

```gpp
"Hello {{name}}"
```

The expression between `{{` and `}}` is evaluated and converted to text.

Examples:

```gpp
"Hello {{user.Name}}"

"Total: {{price * quantity}}"

"Active: {{user.Active}}"

"User: {{GetUser(id)}}"
```

Interpolation is not restricted to identifiers.

---

# 4. Full GPP Expressions

The interpolation body may contain any valid GPP expression that produces a value.

Examples:

```gpp
"{{user.Name}}"

"{{price * quantity}}"

"{{user.Name.ToUpper()}}"

"{{items.Filter(x => x.Active).len}}"

"{{value || fallback}}"

"{{LoadPort() ?? 8080}}"
```

Normal GPP expression semantics apply.

Interpolation does not introduce a separate expression grammar.

---

# 5. Formatted Interpolation

An interpolation expression may specify an optional Go `fmt` formatting directive.

Syntax:

```text
{{ expression : format }}
```

Example:

```gpp
"Price: {{price:%.2f}}"
```

Conceptually equivalent to:

```go
fmt.Sprintf("Price: %.2f", price)
```

The format syntax is standard Go `fmt` syntax.

---

# 6. `%` Is Required

The formatting portion uses the normal Go `%` notation.

Preferred:

```gpp
"{{price:%.2f}}"
```

Not:

```text
{{price:.2f}}
```

GPP must not invent a second format syntax.

This allows developers already familiar with `fmt.Printf` and `fmt.Sprintf` to use the same formatting knowledge.

---

# 7. Formatting Examples

Floating point:

```gpp
"{{price:%.2f}}"
```

Integer:

```gpp
"{{count:%d}}"
```

Zero padding:

```gpp
"{{id:%08d}}"
```

Hexadecimal:

```gpp
"{{value:%x}}"
```

Go-syntax hexadecimal:

```gpp
"{{value:%#x}}"
```

Quoted string:

```gpp
"{{name:%q}}"
```

Width:

```gpp
"{{amount:%10.2f}}"
```

Left alignment:

```gpp
"{{name:%-20s}}"
```

Type:

```gpp
"{{value:%T}}"
```

Detailed/debug representation:

```gpp
"{{value:%#v}}"
```

---

# 8. Formatting Full Expressions

The expression before the format separator is not restricted to a simple variable.

Valid:

```gpp
"Total: {{price * quantity:%.2f}}"
```

```gpp
"Average: {{sum / count:%.3f}}"
```

```gpp
"Age: {{user.Age:%03d}}"
```

```gpp
"Result: {{Calculate():%#v}}"
```

Conceptually:

```gpp
"{{expression:format}}"
```

means:

```go
fmt.Sprintf(format, expression)
```

for that interpolation segment.

---

# 9. Default Formatting

If no explicit format is supplied:

```gpp
"Hello {{value}}"
```

the value is converted using normal GPP string interpolation semantics.

The default behavior should correspond closely to:

```go
fmt.Sprint(value)
```

or an equivalent optimized lowering.

Therefore:

```gpp
"count={{count}}"
```

works naturally for integers.

Likewise:

```gpp
"enabled={{enabled}}"
```

works naturally for booleans.

---

# 10. `String()` Integration

If a value implements:

```go
fmt.Stringer
```

or a GPP class provides an appropriate:

```gpp
func String() string
```

default interpolation should use that representation according to normal Go formatting behavior.

Example:

```gpp
class User {
    Name string

    func String() string {
        return Name
    }
}
```

Then:

```gpp
"User: {{user}}"
```

may render the user's `String()` value.

This naturally integrates with the GPP root `Object.String()` model.

---

# 11. Explicit Formatting Overrides `String()`

An explicit format directive follows normal `fmt` behavior.

For example:

```gpp
"{{user:%#v}}"
```

requests `%#v` formatting rather than relying on the ordinary default interpolation representation.

GPP should not invent special precedence rules beyond standard Go formatting behavior.

---

# 12. Double-Quoted String Semantics

Double-quoted interpolation strings retain normal Go escape processing.

Example:

```gpp
message := "Hello\n{{name}}\n"
```

The `\n` sequences are interpreted normally.

Interpolation occurs as part of the GPP compilation process.

---

# 13. Raw Backtick String Semantics

Backtick strings retain normal Go raw-string behavior and do not interpolate.

```gpp
source := `<h1>{{.Name}}</h1>`
path := `C:\users\{{name}}`
```

Both values preserve their contents exactly. Use a double-quoted string when
GPP interpolation is needed. For a dynamic multiline string, use a standard Go
formatter such as `fmt.Sprintf`.

---

# 14. Multiline String Construction

Backtick strings provide readable multiline literals. Since raw strings do not
interpolate, use `fmt.Sprintf` when dynamic values are needed:

```gpp
message := fmt.Sprintf(`
Total: %.2f
`, subtotal + tax + shipping)
```

GPP interpolation remains available in double-quoted strings.

---

# 15. Format Separator Parsing

The colon separating the expression from its format directive is recognized only at the top level of the interpolation expression.

Example:

```gpp
"{{condition ? a : b:%d}}"
```

must not incorrectly interpret the inner expression colon as the formatting separator if GPP later supports syntax containing colons.

Likewise colons inside:

```text
strings
nested calls
composite syntax
```

must not prematurely terminate the expression.

The parser must identify the formatting separator structurally, not through a naive first-colon split.

---

# 16. Recommended Grammar Model

Conceptually:

```text
Interpolation
    := "{{" Expression [ ":" FormatSpecifier ] "}}"
```

where:

```text
Expression
    normal GPP expression

FormatSpecifier
    Go fmt-compatible format string beginning with %
```

The exact parser implementation is compiler-defined.

---

# 17. Literal `{{` and `}}`

Backtick strings are literal, so Go template actions can be written directly:

```gpp
source := `<h1>{{.Name}}</h1>`
```

Inside a double-quoted interpolated string, a backslash before the opener emits
it literally:

```gpp
source := "<h1>\{{.Name}}</h1>"
```

The existing doubled-delimiter form remains supported in double-quoted strings:

```text
{{{{  -> literal {{
}}}}  -> literal }}
```

For example, `"Use {{{{name}}}} in the configuration"` produces
`Use {{name}} in the configuration`.

---

# 18. Raw Strings and Templates

Raw strings are the natural way to write literal Go template source because
GPP does not interpret their delimiters. Template actions are then evaluated
later by `html/template`, when the parsed template executes.

---

# 19. Empty Interpolation

This is invalid:

```gpp
"{{}}"
```

The compiler should report:

```text
empty interpolation expression
```

Whitespace-only interpolation is likewise invalid:

```gpp
"{{   }}"
```

---

# 20. Invalid Format

Malformed format syntax should produce a useful compiler diagnostic where statically detectable.

Example:

```gpp
"{{price:.2f}}"
```

may produce:

```text
invalid interpolation format: expected Go fmt format beginning with %
```

Likewise:

```gpp
"{{price:%}}"
```

should be diagnosed where practical.

GPP need not fully reimplement all `fmt` validation if normal Go compilation can provide the final diagnostic, but source mapping must refer back to the original `.gpp` source.

---

# 21. Type/Format Checking

Where practical, the compiler should diagnose obviously incompatible constant formatting combinations.

For example:

```gpp
name := "Bob"

"{{name:%d}}"
```

may produce a compile-time warning or error if the type is statically known.

However, GPP should not build an entirely separate `fmt` type system merely for interpolation.

The implementation may rely on Go formatting semantics where necessary.

---

# 22. Lowering Without Formatting

Example:

```gpp
message := "Hello {{name}}"
```

may lower conceptually to:

```go
message := "Hello " + fmt.Sprint(name)
```

or:

```go
message := fmt.Sprintf("Hello %v", name)
```

or a more efficient generated builder.

The lowering strategy is implementation-defined.

Visible semantics are what matter.

---

# 23. Lowering With Formatting

Example:

```gpp
message := "Price {{price:%.2f}}"
```

may lower conceptually to:

```go
message := "Price " + fmt.Sprintf("%.2f", price)
```

or to one combined generated formatting operation.

The compiler is free to optimize.

---

# 24. Efficient Multi-Part Lowering

This:

```gpp
message := "User {{user.Name}}, age {{user.Age}}, balance {{balance:%.2f}}"
```

does not need to lower to multiple nested `fmt.Sprintf` calls.

The compiler may generate:

```text
strings.Builder
single fmt.Sprintf
direct concatenation
strconv operations
```

or another efficient implementation.

The language specification defines output semantics, not the exact allocation strategy.

---

# 25. No Mandatory `fmt` Import

Users should not need:

```gpp
import "fmt"
```

merely because interpolation is used.

Example:

```gpp
message := "Price {{price:%.2f}}"
```

must work without an explicit `fmt` import.

The compiler may inject/use `fmt` internally as part of lowering.

---

# 26. Nested Calls

Normal function/method calls are valid inside interpolation.

Examples:

```gpp
"{{strings.TrimSpace(name)}}"
```

```gpp
"{{name.TrimSpace()}}"
```

```gpp
"{{user.DisplayName()}}"
```

```gpp
"{{CalculateTotal(order):%.2f}}"
```

---

# 27. Nil Coalescing

Normal GPP nil coalescing is valid:

```gpp
"Name: {{user.Name || "Unknown"}}"
```

The interpolation system does not need a separate fallback syntax.

---

# 28. Error Fallback

Normal GPP expression-level error fallback is valid:

```gpp
"Port: {{ParsePort(value) ?? 8080}}"
```

Again, interpolation reuses the existing expression language.

---

# 29. Error Promotion

If an interpolation expression invokes a function returning a trailing `error` and the error is omitted, normal GPP automatic error promotion applies.

Example:

```gpp
message := "User: {{LoadUser(id)}}"
```

If:

```gpp
LoadUser(id)
```

returns:

```gpp
(User, error)
```

the `error` is automatically promoted in the same way it would be outside interpolation.

Interpolation must not silently swallow errors.

---

# 30. Function Context

Because interpolation may throw through automatic error promotion, it participates in the enclosing function's normal GPP error behavior.

For example:

```gpp
func Message(id int) string {
    return "User: {{LoadUser(id)}}"
}
```

must obey the same rules that would apply if `LoadUser(id)` were evaluated normally in that function.

The compiler must not create a hidden error-swallowing boundary around interpolation.

---

# 31. Side Effects

Interpolation expressions are evaluated exactly once and in left-to-right string order.

Example:

```gpp
"{{Next()}} {{Next()}}"
```

calls `Next()` twice.

The first interpolation is evaluated before the second.

The compiler must preserve evaluation order during lowering.

---

# 32. Formatted Expression Evaluated Once

This:

```gpp
"{{Expensive():%.2f}}"
```

must evaluate:

```gpp
Expensive()
```

exactly once.

The compiler must not duplicate the expression while generating formatting code.

---

# 33. Constant Strings

Strings containing no interpolation remain ordinary Go strings.

Example:

```gpp
"hello world"
```

requires no interpolation machinery.

Likewise:

```gpp
`hello world`
```

remains an ordinary raw Go string.

---

# 34. Compile-Time Constant Semantics

A string containing interpolation is generally not a Go compile-time constant because expressions must be evaluated.

Example:

```gpp
const name = "Bob"
```

does not automatically imply:

```gpp
const greeting = "Hello {{name}}"
```

can remain a Go constant.

The compiler may constant-fold interpolation where all operands are compile-time constants, but this is an optimization and not required for v1.

---

# 35. HTML Is Not Special

String interpolation does not perform HTML escaping.

Example:

```gpp
html := "<h1>{{name}}</h1>"
```

is ordinary string construction.

This is different from:

```gpp
template Page(user User) {
    <h1>{{.Name}}</h1>
}
```

where `html/template` performs contextual escaping.

Developers should use `gpp/tpl` for HTML templating where escaping matters.

---

# 36. SQL Is Not Special

Likewise:

```gpp
query := `
    SELECT *
    FROM users
    WHERE id = {{id}}
`
```

is ordinary string interpolation.

It does not create parameterized SQL.

Applications should continue to use database parameters for untrusted values:

```gpp
db.Query(
    "SELECT * FROM users WHERE id = $1",
    id,
)
```

Interpolation does not imply SQL escaping or safety.

---

# 37. Relationship to Templates

GPP interpolation syntax and Go `html/template` syntax both use:

```text
{{ ... }}
```

but they operate in different contexts.

Inside a double-quoted GPP string literal:

```gpp
"Hello {{name}}"
```

GPP interpolation applies. Backtick strings are literal, including any
`{{...}}` sequences.

Inside a template declaration:

```gpp
template Page(user User) {
    Hello {{.Name}}
}
```

Go `html/template` syntax applies.

The parser distinguishes double-quoted GPP strings, literal raw strings, and raw template declaration bodies.

---

# 38. Raw String and Template Distinction

Backtick strings preserve template source literally:

```gpp
source := `
    <h1>{{.Name}}</h1>
`
```

The action remains in the string until it is parsed and executed by
`html/template`.

# 39. Example: Dynamic Template Source

When GPP interpolation is needed, use a double-quoted string. For dynamic
multiline output, use a standard Go formatter such as `fmt.Sprintf` and keep
template actions in a raw string:

```gpp
name := "Ada"
source := `<h1>{{.Name}}</h1>`
html := fmt.Sprintf("<p>Hello %s</p>", name)
```

# 40. Example: Mixed Construction

A template can be kept literal while data is supplied separately:

```gpp
source := `<h1>{{.Name}}</h1>`
tpl.Execute(w, source, user)
```

For immediate GPP interpolation, use a double-quoted string:

```gpp
greeting := "Hello {{user.Name}}"
```

# 41. Interpolation in Struct Fields

Interpolation works wherever a normal string expression is valid.

Example:

```gpp
data := struct(
    Title: "Order {{order.ID}}",
    Total: "{{order.Total:%.2f}}",
)
```

No special struct behavior is required.

---

# 42. Interpolation in Annotations

Interpolation should not automatically be allowed in annotation arguments that require compile-time constants.

For example:

```gpp
@{tpl.Path("/users/{{id}}")}
```

should not become runtime interpolation unless that annotation explicitly accepts nonconstant expressions.

Normal annotation constant-expression rules apply.

---

# 43. Dynamic Multiline Content

Use `fmt.Sprintf` when constructing a multiline string with dynamic values:

```gpp
email := fmt.Sprintf(`
Hello %s,

Your order %d has shipped.

Total: %.2f

Thanks,
%s
`, user.Name, order.ID, order.Total, company.Name)
```

The raw format string remains literal; Go's formatter inserts the values.

---

# 44. Example: Logging

Instead of:

```go
log.Printf(
    "user %s logged in from %s",
    user.Name,
    ip,
)
```

GPP may write:

```gpp
log.Println(
    "user {{user.Name}} logged in from {{ip}}",
)
```

or simply:

```gpp
message := "user {{user.Name}} logged in from {{ip}}"
```

depending on the logging API.

---

# 45. Example: Numeric Formatting

Instead of:

```go
text := fmt.Sprintf(
    "subtotal %.2f, tax %.2f, total %.2f",
    subtotal,
    tax,
    total,
)
```

GPP:

```gpp
text := "subtotal {{subtotal:%.2f}}, tax {{tax:%.2f}}, total {{total:%.2f}}"
```

---

# 46. Example: Tabular Formatting

Go formatting width works unchanged:

```gpp
line := "{{name:%-20s}} {{quantity:%5d}} {{price:%10.2f}}"
```

This makes interpolation useful for:

```text
CLI tables
logs
reports
fixed-width output
```

without introducing a new alignment API.

---

# 47. Example: Debugging

```gpp
println("user={{user:%#v}}")
```

or:

```gpp
println("type={{user:%T}} value={{user:%+v}}")
```

uses familiar Go formatting directly.

---

# 48. Compiler Diagnostics

Interpolation diagnostics should point to the interpolation expression in the original `.gpp` source.

Example:

```gpp
"Hello {{user.Nmae}}"
```

should report something such as:

```text
unknown field User.Nmae
did you mean User.Name?
```

at the interpolation source location.

Generated Go source locations should not leak into ordinary diagnostics.

---

# 49. Unterminated Interpolation

Invalid:

```gpp
"Hello {{user.Name"
```

The compiler should report:

```text
unterminated interpolation
```

and identify the starting delimiter.

---

# 50. Unterminated Format

Invalid:

```gpp
"{{price:%.2f"
```

should produce an interpolation-specific parse diagnostic rather than an unrelated generated-Go error where possible.

---

# 51. Nested Braces

The interpolation parser must correctly handle braces belonging to GPP expressions.

For example, if future expression forms contain nested literals or structures, matching `}}` must respect nested syntax rather than relying on naive text search.

The implementation should use proper lexical/expression parsing inside interpolation regions.

---

# 52. No Recursive String Interpolation

If an interpolation expression evaluates to a string containing:

```text
{{ ... }}
```

that resulting string is not interpolated again.

Example:

```gpp
x := "{{name}}"

"Value: {{x}}"
```

produces:

```text
Value: {{name}}
```

not the value of `name`.

Interpolation occurs only during parsing of the original GPP string literal.

---

# 53. No Implicit Format Reuse

Each interpolation controls its own formatting.

Example:

```gpp
"{{a:%.2f}} {{b}}"
```

formats only `a` with `%.2f`.

Formatting does not carry over to later expressions.

---

# 54. Implementation Strategy

A useful compiler strategy is to tokenize each interpolated string into segments.

For example:

```gpp
"Hello {{user.Name}}, total {{total:%.2f}}"
```

becomes conceptually:

```text
Literal("Hello ")
Expr(user.Name, default)
Literal(", total ")
Expr(total, "%.2f")
```

The emitter then lowers the sequence efficiently.

This representation is used for double-quoted interpolated strings.

---

# 55. Suggested AST

Conceptually:

```go
type InterpolatedString struct {
    Raw      bool
    Segments []StringSegment
}
```

with segments such as:

```go
type LiteralSegment struct {
    Text string
}

type ExpressionSegment struct {
    Expr   Expr
    Format string
}
```

`Format` is empty when default formatting is requested.

The exact AST shape is implementation-defined.

---

# 56. Lexer Behavior

The lexer recognizes interpolation delimiters inside double-quoted string
literals. Backtick raw strings are preserved literally. It must not apply GPP
string interpolation while parsing a `template` declaration's raw body.

Template bodies belong to `html/template`, not the GPP string lexer.

---

# 57. Source Preservation

For raw backtick strings, literal source content should be preserved exactly,
including interpolation-like delimiters and backslashes. No whitespace
normalization should occur.

---

# 58. Compatibility

Existing non-interpolated Go string literals remain source compatible.

Existing double-quoted interpolation remains valid.

Backtick strings no longer interpolate; they now preserve Go raw-string
contents literally. Existing raw strings that relied on GPP interpolation
should use double-quoted interpolation or an explicit Go formatter such as
`fmt.Sprintf`.

---

# 59. Non-Goals

Version 1 does not need:

* `${expr}` syntax;
* `$name` interpolation;
* Python f-string syntax;
* a custom `.2f` formatting grammar;
* named formatting arguments;
* automatic locale formatting;
* automatic currency formatting;
* automatic HTML escaping;
* automatic SQL escaping;
* recursive interpolation;
* template-engine control flow inside strings.

---

# 60. Recommended V1 Syntax

Basic:

```gpp
"Hello {{name}}"
```

Raw multiline:

```gpp
`
Hello {{name}}
`
```

Expression:

```gpp
"{{price * quantity}}"
```

Formatted:

```gpp
"{{price * quantity:%.2f}}"
```

Nil fallback:

```gpp
"{{name || "Unknown"}}"
```

Error fallback:

```gpp
"{{ParsePort(value) ?? 8080}}"
```

Literal delimiter:

```gpp
"{{{{name}}}}"
```

produces:

```text
{{name}}
```

---

# 61. Design Summary

The interpolation model is:

```text
"..." and `...`
        ↓
literal segments + {{ expressions }}
        ↓
optional :%fmt specifier
        ↓
normal GPP expression semantics
        ↓
normal Go fmt formatting semantics
        ↓
ordinary Go string result
```

Examples:

```gpp
"Hello {{user.Name}}"

"Total: {{price * quantity:%.2f}}"

"Debug: {{value:%#v}}"
```

The core rule is:

> GPP interpolation adds expression insertion and Go-native formatting to double-quoted strings while backtick strings retain Go's literal raw-string behavior.
