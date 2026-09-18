# Go++ `gpp fmt` Command Specification

## 1. Overview

`gpp fmt` formats Go++ source into the canonical Go++ style.

```bash
gpp fmt
gpp fmt app.gpp
gpp fmt ./...
gpp fmt --check
```

The formatter is:

* deterministic;
* idempotent;
* comment-preserving;
* AST/token aware;
* shared with `gpp lsp`;
* Go-like wherever Go syntax is being formatted.

The formatter MUST NOT discard, rewrite, or accidentally relocate user comments.

---

# 2. Core Principle

Go++ should have one canonical formatting style.

The goal follows the same general philosophy as `gofmt`:

> Formatting is a tool decision, not a project-level style decision.

Therefore v0.1 does not expose configurable:

```text
indent width
brace style
line width
annotation style
import style
```

---

# 3. Commands

Canonical forms:

```bash
gpp fmt
gpp fmt app.gpp
gpp fmt a.gpp b.gpp
gpp fmt ./...
gpp fmt --check
gpp fmt --stdout app.gpp
```

`gpp fmt` with no explicit path formats the current Go++ project.

---

# 4. In-Place Behavior

Normal formatting rewrites files in place.

```bash
gpp fmt app.gpp
```

A file should only be rewritten if its formatted contents differ.

Successful formatting is normally silent.

---

# 5. Check Mode

```bash
gpp fmt --check
```

does not modify source.

It returns:

```text
0
    all applicable files are formatted

non-zero
    one or more files require formatting
    or a formatting/parsing error occurred
```

Typical CI output:

```text
Needs formatting:
    app.gpp
    models/user.gpp
```

---

# 6. Idempotency

Formatting must satisfy:

```text
Format(Format(source)) == Format(source)
```

Formatter instability is a correctness bug.

---

# 7. Semantic Preservation

Formatting may modify:

```text
spacing
indentation
line breaks
canonical import layout
trailing commas
```

It must not modify program semantics.

It must not reorder:

```text
declarations
fields
methods
annotations
enum members
embed entries
statements
expressions
```

unless a particular construct explicitly defines ordering as semantically irrelevant and the formatter specification permits it.

---

# 8. Comments Are Source Content

Comments are not disposable lexer trivia.

Given:

```gpp
// User represents an application account.
class User {
    // Name shown publicly.
    Name string

    Age int // in years
}
```

`gpp fmt` MUST preserve all three comments.

Formatting must never produce:

```gpp
class User {
    Name string
    Age int
}
```

from that source.

Comment loss is a formatter correctness failure and a v0.1 release blocker.

---

# 9. Parser/Lexer Requirement

The compiler frontend MUST retain comments.

A lexer implementation that merely does:

```text
encounter comment
    ↓
skip it
```

is insufficient for `gpp fmt`.

Every source comment must survive lexical/parsing processing with enough information to reconstruct its intended position.

---

# 10. Comment Representation

The exact internal representation is implementation-defined.

One acceptable model is:

```go
type Comment struct {
    Text string
    Span SourceSpan
}

type File struct {
    Decls    []Decl
    Comments []Comment
}
```

AST nodes also retain their source spans.

For example:

```go
type Field struct {
    Name string
    Type Type
    Span SourceSpan
}
```

This allows the formatter to determine where comments occur relative to declarations and expressions.

---

# 11. Comment Attachment

Where practical, semantic attachment may additionally classify comments as:

```text
leading
trailing
detached
inner
documentation
```

Example:

```gpp
// documentation
class User {
}
```

is logically attached to `User`.

Example:

```gpp
Age int // years
```

contains a trailing comment associated with the field.

---

# 12. Comments Between Syntax Nodes

Comments inside lists and blocks must also survive.

Example:

```gpp
foo(
    a,

    // compatibility mode
    LegacyMode,

    b,
)
```

The formatter must preserve the comment between `a` and `LegacyMode`.

Likewise:

```gpp
@{
    http.GET("/users")

    // Admins only.
    auth.Role(Role.Admin)
}
```

must retain the comment in the annotation list.

---

# 13. Statement Comments

Given:

```gpp
if ready {
    prepare()

    // Must happen after prepare.
    execute()
}
```

the formatter must not move the comment above `prepare()` or outside the block.

Source-position relationships are part of formatting correctness.

---

# 14. Comment Text

The formatter must not rewrite prose inside comments.

It may change indentation surrounding a comment.

It must preserve the actual comment text.

For example:

```gpp
// don't "fix"   spacing in my prose
```

remains semantically and textually the same comment.

---

# 15. Documentation Comments

Documentation comments are especially important because `gpp doc` and `gpp lsp` may consume them.

Formatting must preserve the association between:

```gpp
// Save persists the user.
func Save() {
}
```

and `Save`.

---

# 16. Raw Strings and Template Content

The same preservation principle applies to opaque user-authored content.

The formatter must not arbitrarily rewrite:

```text
raw backtick contents
HTML bodies
template text
embedded SQL
other opaque literal contents
```

unless the syntax explicitly defines an independently formatable sublanguage.

---

# 17. Formatter Architecture

Preferred architecture:

```text
source
  ↓
lexer/parser
  ↓
Go++ syntax tree + comments + source positions
  ↓
formatter/printer
  ↓
canonical Go++ source
```

This should not be implemented as regex replacement over source text.

---

# 18. Reusing Go Formatting

Go++ should reuse Go's formatter infrastructure wherever it genuinely applies.

Useful Go packages include:

```go
go/ast
go/parser
go/token
go/printer
go/format
```

However, `gofmt` cannot directly parse Go++ syntax such as:

```gpp
class User {
}

extend string {
}

@{
    Deprecated
}

try {
} catch e {
}

record(name: "Bob")

template Page() {
}
```

Therefore `gpp fmt` cannot simply invoke `gofmt` on a `.gpp` file.

---

# 19. Hybrid Formatting Model

Recommended architecture:

```text
                 Go++ parser
                     ↓
                 Go++ AST
                ↙          ↘
      Go++ constructs      Go-compatible constructs
             ↓                     ↓
      Go++ printer          go/format / go/printer
                ↘          ↙
             canonical .gpp
```

Go++ owns formatting for syntax that Go does not understand.

Go's formatter machinery may handle ordinary Go-shaped syntax.

---

# 20. Candidate Go-Owned Syntax

Where lowering or AST representation permits it cleanly, Go's printer may be reused for:

```text
expressions
binary expressions
unary expressions
calls
selectors
indexing
Go-compatible type expressions
ordinary if statements
ordinary for statements
switches
returns
assignments
composite literals
```

For example:

```gpp
a+b*c
```

should naturally become something equivalent to Go formatting:

```gpp
a + b*c
```

according to ordinary Go formatting rules.

The goal is not to duplicate `gofmt`'s decisions unnecessarily.

---

# 21. Go++-Owned Syntax

The Go++ printer must handle constructs such as:

```text
class
inheritance
init
annotations
annotation declarations
extend
records
enums
templates
try/catch/finally
??
||
||=
Go++ interpolation
embed
other Go++-specific declarations
```

These cannot simply be handed to `go/format`.

---

# 22. Do Not Format Generated Go and Reverse It

The formatter MUST NOT use:

```text
.gpp
 ↓
lower to generated Go
 ↓
gofmt
 ↓
attempt to reconstruct Go++
```

Lowering loses source-level distinctions and cannot reliably reconstruct:

```text
classes
annotations
records
extension methods
Go++ error syntax
comments
source-level layout intent
```

Generated Go is not the formatting IR.

---

# 23. Go AST Fragments

A useful implementation technique is to represent Go-compatible Go++ expressions using `go/ast` nodes.

For example, a Go++ binary expression may internally be printable through:

```go
format.Node(...)
```

or:

```go
printer.Fprint(...)
```

Then the surrounding Go++ printer emits the language-specific syntax.

---

# 24. Comments and Go's Printer

Where Go-compatible fragments are represented using Go ASTs, comments may also use Go's comment infrastructure where practical.

However, Go++ remains responsible for preserving comments around Go++-specific syntax.

The architecture must never depend on `go/printer` magically preserving comments it was never given.

---

# 25. Source Fidelity

The frontend must retain enough information to distinguish:

```text
syntax
comments
opaque literal contents
source boundaries
```

The formatter may discard arbitrary whitespace because whitespace is what it canonicalizes.

It may not discard meaningful source content.

---

# 26. Indentation

Canonical indentation is:

```text
4 spaces
```

Go++ source formatting does not use emitted tab indentation merely because generated Go may use tabs.

Example:

```gpp
class User {
    Name string

    func Save() {
        if Valid() {
            Persist()
        }
    }
}
```

---

# 27. Braces

Canonical:

```gpp
class User {
}
```

```gpp
func Save() {
}
```

```gpp
if valid {
}
```

```gpp
try {
} catch e {
} finally {
}
```

Opening braces remain with the construct introducing them.

---

# 28. Function Parameters

Short:

```gpp
func Find(id int, active bool) User {
}
```

Long:

```gpp
func Create(
    name string,
    email string,
    role Role,
    active bool,
) User {
}
```

Multiline lists use trailing commas where grammar permits.

---

# 29. Calls

Short:

```gpp
Send(user, message)
```

Long:

```gpp
Send(
    user,
    message,
    options,
)
```

Avoid mixed layouts such as:

```gpp
Send(user,
    message, options)
```

---

# 30. Classes

Canonical:

```gpp
class User : Model {
    ID string
    Name string

    func Save() {
        ...
    }
}
```

---

# 31. Multiple Inheritance

Short:

```gpp
class Admin : User, Auditable, Serializable {
}
```

Long inheritance lists may wrap canonically.

The exact line-breaking policy remains formatter-controlled.

---

# 32. Inline/Postfix Annotations

Go++ annotation placement MUST remain postfix/inlined.

Correct:

```gpp
func OldAPI() @{
    Deprecated("Use NewAPI")
} {
    ...
}
```

The formatter must NEVER transform this into decorator-style placement above the declaration.

---

# 33. Short Annotations

Short annotation sets may remain inline:

```gpp
ID string @{PK}
```

```gpp
Name string @{Column("name")}
```

---

# 34. Multiline Annotations

Multiple or complex annotations use:

```gpp
func User(id int) @{
    http.GET("/users/{id}")
    auth.Permission("users.read")
    Deprecated("Use FindUser")
} {
    ...
}
```

Annotation order is preserved.

---

# 35. Annotation Arguments

Long calls format like ordinary multiline calls:

```gpp
class App : http.Server @{
    http.OAuth(
        "corp",
        "https://identity.example.com",
        "CORP_CLIENT_ID",
        "CORP_CLIENT_SECRET",
        "openid",
        "email",
        "profile",
    )
} {
}
```

---

# 36. Extensions

Canonical:

```gpp
extend string {
    func CompileRegex() (*regexp.Regexp, error) {
        return regexp.Compile(this)
    }
}
```

Multiple receivers:

```gpp
extend *sql.DB, *sql.Tx {
    ...
}
```

---

# 37. Records

Short:

```gpp
result := record(name: "Bob", age: 42)
```

Long:

```gpp
result := record(
    Name: user.Name,
    Email: user.Email,
    CreatedAt: user.CreatedAt,
)
```

---

# 38. Enums

Canonical:

```gpp
enum Status string {
    Pending
    Active
    Disabled
}
```

Explicit values:

```gpp
enum Role string {
    User = "user"
    Admin = "admin"
}
```

---

# 39. Lambdas

Expression lambda:

```gpp
users.Filter(u => u.Active)
```

Multiple parameters:

```gpp
users.Sort((a, b) => a.Name < b.Name)
```

Block:

```gpp
users.Map(user => {
    name := user.Name.TrimSpace()
    return name.ToLower()
})
```

---

# 40. Operators

Canonical spacing follows Go conventions where applicable:

```gpp
a + b
a == b
value ?? fallback
value || fallback
value ||= defaultValue
```

Selectors remain tight:

```gpp
user.Name.TrimSpace()
```

Generics remain tight:

```gpp
err.As[*MyError]()
```

---

# 41. String Interpolation

The formatter may format Go++ expressions inside interpolation.

Example:

```gpp
"Total: {{price*qty:%.2f}}"
```

may become:

```gpp
"Total: {{price * qty:%.2f}}"
```

The surrounding text and format specification must not change.

---

# 42. Raw Strings

Raw string contents are preserved byte-for-byte except for any unavoidable representation rules explicitly defined by the language.

The formatter does not reflow arbitrary multiline raw strings.

---

# 43. `try/catch/finally`

Canonical:

```gpp
try {
    ...
} catch NotFoundError e {
    ...
} catch e {
    ...
} finally {
    ...
}
```

---

# 44. Imports

Imports may be sorted deterministically.

Example:

```gpp
import (
    "database/sql"
    "regexp"

    "github.com/example/foo"
)
```

Import aliases and versioned imports are preserved.

---

# 45. Formatter Does Not Fix Semantics

`gpp fmt` does not remove unused imports.

It does not:

```text
rename symbols
rewrite deprecated APIs
remove unreachable code
fix type errors
reorganize declarations
```

Those belong to linting, refactoring, or future `gpp fix` functionality.

---

# 46. Templates

For:

```gpp
template UserPage(user User) {
    <h1>{{.Name}}</h1>
}
```

the formatter canonicalizes the Go++ declaration syntax.

The template body should remain largely preserved in v0.1.

HTML/template pretty-printing is not required.

---

# 47. Parse Errors

`gpp fmt` normally requires syntactically valid Go++.

If parsing fails:

```text
report source diagnostic
leave original file untouched
```

Do not partially rewrite malformed source.

---

# 48. Atomic Writes

In-place formatting should use safe replacement:

```text
read
 ↓
parse
 ↓
format
 ↓
successfully produce complete output
 ↓
write temporary
 ↓
replace original
```

A formatter failure must not destroy the source file.

---

# 49. LSP Integration

`gpp lsp` uses the formatter library directly.

It does NOT run:

```bash
gpp fmt
```

as a child process.

Architecture:

```text
              formatter
              ↙      ↘
         gpp fmt    gpp lsp
```

---

# 50. Unsaved Buffers

LSP formatting must operate on current editor buffer contents.

The file does not need to exist in formatted form on disk.

---

# 51. Formatter API

Conceptually:

```go
func FormatSource(
    filename string,
    source []byte,
) ([]byte, error)
```

and potentially:

```go
func FormatFile(file *ast.File) ([]byte, error)
```

The internal API should also retain access to comments/source metadata.

---

# 52. Possible Internal Architecture

A useful structure is:

```text
parser/
    AST
    source spans
    comments

format/
    printer
    Go-fragment bridge
    comment placement
    canonical layout

cmd/
    fmt

lsp/
    formatting handler
```

---

# 53. Golden Tests

Formatter behavior should be tested with input/golden pairs:

```text
testdata/
    classes.input.gpp
    classes.golden.gpp

    annotations.input.gpp
    annotations.golden.gpp

    comments.input.gpp
    comments.golden.gpp
```

---

# 54. Mandatory Comment Tests

The formatter test suite MUST include:

```text
file-leading comments
documentation comments
field-leading comments
method-leading comments
inline trailing comments
comments between arguments
comments between annotations
comments between statements
comments inside expressions where legal
comments around class members
comments around try/catch blocks
comments around imports
comments around templates
```

---

# 55. Comment Conservation Test

Formatter testing should enforce a comment-conservation invariant.

Conceptually:

```text
comments(input) == comments(formatted output)
```

subject only to source-position/layout changes.

No comment may disappear.

No comment text may silently change.

---

# 56. Idempotency Test

Every golden formatter test should also verify:

```text
Format(golden) == golden
```

---

# 57. Semantic Equivalence Test

Where practical:

```text
Parse(input)
```

and:

```text
Parse(Format(input))
```

must represent equivalent programs.

---

# 58. Real-World Corpus Test

As Go++ dogfooding grows, the formatter should be continuously run across the real Go++ source corpus.

A useful release check is:

```bash
gpp fmt --check ./...
```

against:

```text
compiler examples
stdlib
sample applications
tests
documentation examples where extractable
```

---

# 59. Why Reuse Go Formatting

Go already solves many difficult formatting questions exceptionally well:

```text
operator spacing
nested expressions
calls
selectors
composite expressions
Go types
control-flow layout
parentheses
precedence-sensitive printing
```

Go++ should not recreate those decisions unnecessarily.

---

# 60. Why We Still Need a Go++ Printer

Go formatting infrastructure has no grammar or AST representation for:

```text
class
annotation blocks
extend
record
enum
try/catch
template
Go++ interpolation semantics
Go++ error operators
```

So the correct goal is:

> reuse Go formatting machinery beneath the Go++ formatter, not replace the Go++ formatter with `gofmt`.

---

# 61. Preferred Implementation Strategy

The recommended order is:

```text
1. Preserve all comments/tokens/source spans in frontend.

2. Build a Go++ syntax printer.

3. Delegate Go-compatible expression/type/statement formatting to Go printer machinery where clean.

4. Print Go++-specific constructs around those formatted fragments.

5. Add golden tests.

6. Enforce comment conservation and idempotency.

7. Reuse exact formatter in gpp lsp.
```

---

# 62. V0.1 Correctness Requirements

A formatter bug is release-blocking if it:

```text
loses a comment
changes comment text
changes program semantics
changes a literal
corrupts interpolation
corrupts template contents
reorders annotations
reorders declarations
produces source the parser cannot read
produces unstable output
```

Cosmetic wrapping imperfections are not equivalent severity.

---

# 63. V0.1 Non-Goals

`gpp fmt` is not:

```text
a linter
a source migration tool
an optimizer
a configurable beautifier
an unused-import remover
an HTML formatter
a SQL formatter
a generated-Go formatter
```

Generated Go continues to use Go's own formatter.

---

# 64. Design Summary

Architecture:

```text
                       Go++ source
                           ↓
                   lexer / parser
                           ↓
           AST + tokens + comments + spans
                           ↓
                    Go++ formatter
                    ↙           ↘
          Go++ syntax       Go-like fragments
                ↓                 ↓
        Go++ printer       go/printer / go/format
                    ↘           ↙
                     canonical .gpp
                           ↓
                ┌──────────┴──────────┐
                ↓                     ↓
             gpp fmt               gpp lsp
```

The fundamental rules are:

> Comments are source content and MUST survive parsing and formatting.

> Comment loss is a formatter correctness failure.

> Go++ should reuse Go's formatter/printer for Go-compatible syntax where doing so is clean and semantics-preserving.

> `gofmt` itself cannot parse Go++ and therefore cannot be the top-level formatter.

> Generated Go must never be used as an intermediate representation from which formatted Go++ is reconstructed.

> Go++ owns its extended syntax while delegating ordinary Go-shaped syntax to proven Go formatting machinery where practical.

> Formatting is deterministic, idempotent, semantics-preserving, and shared by the CLI and LSP.

> V0.1 favors correctness and source preservation over sophisticated line wrapping.
