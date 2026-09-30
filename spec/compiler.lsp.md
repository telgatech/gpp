# Go++ `gpp lsp` Command Specification

## 1. Overview

`gpp lsp` starts the Go++ Language Server Protocol service used by editors and IDEs.

Typical invocation:

```bash
gpp lsp
```

The process remains running for the lifetime of the editor workspace/session.

It communicates with the editor over standard input and standard output using the Language Server Protocol.

Typical architecture:

```text
VS Code / editor
      ↕
JSON-RPC / LSP
      ↕
gpp lsp
      ↓
Go++ workspace
      ↓
parser
semantic analysis
symbol index
formatter
documentation metadata
```

`gpp lsp` is not a separate compiler implementation.

It reuses the same compiler and semantic infrastructure as:

```text
gpp build
gpp run
gpp test
gpp fmt
gpp doc
```

---

# 2. Design Goal

The purpose of `gpp lsp` is to make Go++ feel like a normal modern language inside editors.

The server should provide:

```text
live diagnostics
completion
hover information
go-to-definition
find references
rename
document symbols
workspace symbols
formatting
deprecated-symbol information
```

without spawning a new compiler process for every edit or query.

---

# 3. Process Model

An editor normally starts one `gpp lsp` process for a workspace.

Example:

```text
VS Code window
      ↓
spawn
      ↓
gpp lsp
      ↓
persistent process
```

The process stays alive until the editor shuts down the language server.

It is not restarted on every source change.

---

# 4. Workspace Model

A typical workspace owns:

```text
source files
open editor buffers
package graph
parsed ASTs
semantic information
symbol index
type information
class hierarchy
extension declarations
annotation metadata
diagnostics
```

Unsaved editor contents take precedence over files on disk.

---

# 5. One Server Per Workspace

The normal model is:

```text
one editor workspace
      ↓
one gpp lsp process
```

Two separate VS Code windows may therefore run two independent `gpp lsp` processes.

Example:

```text
project-a
    → gpp lsp process A

project-b
    → gpp lsp process B
```

There is no global Go++ daemon in v0.1.

---

# 6. Multi-Root Workspaces

A single `gpp lsp` process may support multiple workspace roots supplied by the editor.

Example:

```text
workspace
    apps/web
    libs/core
    libs/data
```

The server should maintain package/project boundaries for each root while exposing a unified editor experience.

A separate process per workspace root is not required for v0.1.

---

# 7. Transport

The default transport is:

```text
stdin
stdout
```

using standard LSP framing and JSON-RPC messages.

No TCP port is required.

Typical process relationship:

```text
editor process
    │
    ├── stdin  → gpp lsp
    └── stdout ← gpp lsp
```

---

# 8. Standard Output Discipline

Because stdout carries the LSP protocol, `gpp lsp` must never write ordinary logs or debugging output to stdout.

Logs should go to:

```text
stderr
```

or an optional log file.

This is mandatory because stray stdout text would corrupt the LSP stream.

---

# 9. Optional Debug Logging

Recommended options:

```bash
gpp lsp --log
gpp lsp --log=/tmp/gpp-lsp.log
```

The exact CLI shape may follow the general `gpp` command conventions.

Logging should include:

```text
server startup
workspace roots
file open/change/close
analysis duration
internal failures
protocol errors
```

Sensitive source contents should not be logged by default.

---

# 10. No TCP Server in v0.1

Normal operation should not require:

```bash
gpp lsp --tcp ...
```

A network transport may be useful for debugging later but is not part of the required v0.1 surface.

---

# 11. Initialization

The server must support normal LSP initialization.

At startup the editor sends:

```text
initialize
```

The server receives information such as:

```text
workspace roots
client capabilities
locale
process ID
```

and returns its supported capabilities.

---

# 12. Initialization Sequence

Conceptually:

```text
editor
   ↓
initialize
   ↓
gpp lsp
   ↓
load workspace
   ↓
return capabilities
   ↓
initialized
```

Workspace loading should not require generating Go code or running `go build`.

---

# 13. Shutdown

The server must support:

```text
shutdown
exit
```

according to standard LSP behavior.

After shutdown, workspace resources may be released and the process exits cleanly.

---

# 14. Open Documents

The server supports:

```text
textDocument/didOpen
```

When a `.gpp` file is opened, its editor contents become the authoritative source for that document.

The buffer may differ from the file currently saved on disk.

---

# 15. Changed Documents

The server supports:

```text
textDocument/didChange
```

Changes update the in-memory representation of the document.

For v0.1, full-document synchronization is acceptable.

Example model:

```text
didChange
    ↓
replace in-memory source
    ↓
parse changed file
    ↓
reanalyze package
    ↓
publish diagnostics
```

Incremental text-edit application may be supported later.

---

# 16. Unsaved Files

All semantic functionality must operate on current in-memory editor contents.

This includes:

```text
diagnostics
hover
completion
go-to-definition
references
rename
formatting
```

A file does not need to be saved before editor tooling works.

---

# 17. Closed Documents

The server supports:

```text
textDocument/didClose
```

After a file is closed, the server may discard its editor overlay and return to the version on disk.

---

# 18. Save Notifications

The server may support:

```text
textDocument/didSave
```

but correctness must not depend on save events.

Analysis is driven primarily by open/change events and workspace state.

---

# 19. File Watching

The editor may notify the server of changes to files not currently open.

Examples:

```text
new .gpp files
deleted files
renamed files
go.mod changes
dependency changes
.gpp.tpl changes
```

The workspace model should update accordingly.

---

# 20. Workspace State

Conceptually:

```gpp
class Workspace {
    Roots ...
    Files ...
    Packages ...
    Symbols ...
}
```

The exact implementation is internal.

The important requirement is that workspace state remains alive between requests.

---

# 21. Shared Compiler Infrastructure

The LSP must reuse the same components as the compiler.

Preferred architecture:

```text
                  gpp build
                     ↑
                     │
parser → semantic model → gpp doc
                     │
                     ├── gpp fmt
                     │
                     └── gpp lsp
```

There must not be a second parser or separate editor-only type system.

---

# 22. Parsing Strategy

For v0.1:

```text
changed file
    ↓
reparse changed file
```

is sufficient.

The parser must tolerate incomplete source commonly seen while typing.

Where possible, a partially valid AST should still be produced.

---

# 23. Semantic Analysis Strategy

For v0.1, after a file changes:

```text
reparse file
    ↓
reanalyze containing package
```

is acceptable.

Fine-grained incremental semantic analysis is not required.

---

# 24. Dependency Reanalysis

When a declaration changes in a way that affects other packages, dependent packages may need reanalysis.

For v0.1, conservative invalidation is acceptable.

Example:

```text
public class/API changed
    ↓
invalidate dependent package semantic state
```

Correctness is more important than perfect incrementality.

---

# 25. No Backend Compilation Per Keystroke

`gpp lsp` must not normally run:

```text
Go code generation
go build
go test
```

on every editor change.

Most editor functionality should come directly from Go++ semantic analysis.

Backend compilation may be used only for exceptional diagnostics that cannot otherwise be determined.

---

# 26. Diagnostics

The server should publish compiler diagnostics directly to the editor.

Examples:

```text
syntax errors
unknown symbols
type errors
invalid overload resolution
annotation errors
inheritance errors
duplicate declarations
deprecated usage
route conflicts detectable statically
```

Diagnostics must refer to `.gpp` source positions.

---

# 27. Diagnostic Example

Given:

```gpp
user.DoesNotExist()
```

the server may publish:

```text
User has no method DoesNotExist
```

with the exact source range:

```text
DoesNotExist
^^^^^^^^^^^^
```

---

# 28. Source-Mapped Diagnostics

The LSP uses the same source-location infrastructure as normal compiler diagnostics.

Generated Go filenames and locations should not appear in normal editor diagnostics.

If a backend Go diagnostic is used, it must be remapped to the original Go++ source before being published.

---

# 29. Diagnostic Severity

The server should map Go++ diagnostic categories to LSP severity levels:

```text
compiler error
    → Error

deprecation
    → Warning

non-fatal advisory
    → Information or Hint
```

Exact categorization may evolve.

Published Go++ diagnostics carry stable string codes so extensions can offer
reliable code actions:

| Code | Meaning |
| --- | --- |
| `GPP1000` | Go++ syntax error |
| `GPP1001` | Unterminated string or rune literal |
| `GPP1002` | Unterminated block comment |
| `GPP2000` | Go++ semantic or resolution error |

The literal and block-comment diagnostics include structured `data` describing
the delimiter fix. `textDocument/codeAction` returns a quick fix that inserts
the closing delimiter at the literal or comment end. Clients should use the
diagnostic code and fix data rather than matching human-readable messages.

---

# 30. Deprecated Symbols

Declarations using:

```gpp
@{
    Deprecated
}
```

or:

```gpp
@{
    Deprecated("Use NewAPI instead")
}
```

should be surfaced through LSP deprecation metadata where supported.

This allows editors to show:

```text
strikethrough
warning markers
hover message
completion annotation
```

---

# 31. Completion

The server supports:

```text
textDocument/completion
```

Examples:

```gpp
user.
```

may suggest:

```text
Name
Email
Save
Delete
class
extension methods applicable to User
```

---

# 32. Extension Method Completion

Extension methods must appear in completion when applicable to the receiver type.

Example:

```gpp
name.
```

may suggest:

```text
TrimSpace
ToLower
Contains
CompileRegex
...
```

alongside any applicable native members.

---

# 33. Completion Sources

Completion may include:

```text
local variables
parameters
fields
methods
static methods
classes
functions
enums
enum members
annotations
packages
extension methods
templates
language keywords
```

---

# 34. Completion Should Be Semantic

Completion should prefer type-aware results rather than dumping all globally known symbols.

Example:

```gpp
re.
```

where `re` is:

```go
*regexp.Regexp
```

should primarily return members valid on that native Go type.

The implementation resolves lexical parameters and declarations in the active
function or method, class members, receiver members, and applicable extension
methods. Receiver lookup understands explicit and safe selectors, inferred
class and string values, lambda parameters where their collection element type
is known, and fields declared in anonymous records. Dotted Go++ imports expose
their logical package members and package path segments. Annotation completion
filters declarations by the target currently being annotated.

---

# 35. Annotation Completion

Inside:

```gpp
func Save() @{
    ...
}
```

completion should offer valid annotations for the declaration target.

For example, if an annotation is restricted to classes, it should not be offered on a method.

---

# 36. Route Annotation Completion

For an HTTP server method:

```gpp
func User(id int) @{
    http.
}
```

completion should include applicable annotations such as:

```text
GET
POST
PUT
DELETE
PATCH
```

based on package metadata.

---

# 37. Hover

The server supports:

```text
textDocument/hover
```

Hover should show source-level Go++ information.

Example:

```gpp
user.Save()
```

hovering `Save` might show:

```text
func User.Save() error
```

plus documentation where available.

---

# 38. Hover for Classes

Hovering a class may show:

```text
class User : Model

Fields:
    ID string
    Email string

Methods:
    Save() error
```

The exact rendering may be Markdown.

---

# 39. Hover for Extensions

Extension methods should be identified clearly.

Example:

```text
extension func string.TrimSpace() string

Convenience extension over strings.TrimSpace.
```

---

# 40. Hover for Deprecated Symbols

Hover must surface deprecation information.

Example:

```text
func OldAPI()

Deprecated: Use NewAPI instead.
```

---

# 41. Hover and `gpp doc`

Hover documentation should reuse the same semantic documentation metadata used by:

```bash
gpp doc
```

This avoids separate documentation systems.

---

# 42. Go-to-Definition

The server supports:

```text
textDocument/definition
```

For:

```gpp
user.Save()
```

the editor should navigate to the resolved `Save` declaration.

---

# 43. Definition Resolution

Definition lookup must understand:

```text
normal functions
methods
inherited methods
extension methods
static methods
classes
fields
enum members
annotations
templates
overloads
```

---

# 44. Native Go Definition

For native Go symbols, the server should navigate to source where Go tooling makes source location available.

Example:

```gpp
regexp.Compile(...)
```

may navigate into the Go standard library source.

---

# 45. Find References

The server supports:

```text
textDocument/references
```

References should be resolved semantically rather than simple text matches.

Example:

```text
User.Save
```

must not be confused with an unrelated `Save` method on another type.

---

# 46. Rename

The server supports:

```text
textDocument/rename
```

Rename must operate on the resolved symbol.

Example:

```text
User.Save
    ↓ rename to Persist
```

should update legitimate references while leaving unrelated `Save` symbols untouched.

---

# 47. Rename Safety

Rename should reject or warn about changes that would introduce semantic conflicts.

Examples:

```text
duplicate method
overload collision
field collision
package symbol collision
```

The exact user interaction depends on editor capabilities.

---

# 48. Document Symbols

The server supports:

```text
textDocument/documentSymbol
```

A `.gpp` file should expose its structure to the editor outline.

Example:

```text
App
    Home
    User
    OAuthLogin

User
    ID
    Name
    Save

Status
    Active
    Disabled
```

---

# 49. Workspace Symbols

The server should support:

```text
workspace/symbol
```

allowing users to search across the workspace for:

```text
classes
functions
methods
enums
annotations
templates
```

---

# 50. Formatting

The server supports:

```text
textDocument/formatting
```

using the same formatter as:

```bash
gpp fmt
```

The editor must not implement a separate Go++ formatter.

---

# 51. Format-on-Save

Editors may configure format-on-save.

The expected flow is:

```text
editor
    ↓
textDocument/formatting
    ↓
gpp lsp
    ↓
shared formatter
    ↓
text edits
    ↓
editor applies changes
```

---

# 52. Range Formatting

Range formatting may be added later.

For v0.1, whole-document formatting is sufficient.

---

# 53. Formatting Stability

Repeated formatting should be stable:

```text
format(format(source)) == format(source)
```

This is a requirement of both `gpp fmt` and LSP formatting.

---

# 54. Signature Help

The server should support:

```text
textDocument/signatureHelp
```

where practical.

Example:

```gpp
http.OAuth(
```

may show:

```text
OAuth(provider OAuthProvider, scopes ...string)

OAuth(
    name string,
    url string,
    clientIDEnv string,
    clientSecretEnv string,
    scopes ...string,
)
```

---

# 55. Overload Awareness

Signature help should understand Go++ overloads.

As arguments are typed, applicable overloads may be prioritized.

---

# 56. Code Actions

Basic code-action support is desirable but not mandatory for the smallest v0.1.

Possible future examples:

```text
add missing import
remove unused import
replace deprecated API
implement required method
generate constructor
```

These should not block the initial LSP release.

---

# 57. Semantic Highlighting

Semantic tokens are desirable because Go++ has concepts that generic syntax highlighting cannot fully understand:

```text
class
annotation
enum
template
extension method
static method
deprecated symbol
record field
```

The language server provides full-document semantic tokens with a stable
legend. Token ranges use UTF-16 line and character positions, are single-line,
sorted, and non-overlapping. The ordered token types are:

```text
namespace, type, class, enum, interface, struct, typeParameter, parameter,
variable, property, enumMember, function, method, macro, keyword, modifier,
comment, string, number, operator, decorator
```

The ordered modifiers are `declaration`, `readonly`, `static`, `deprecated`,
and `defaultLibrary`. Go++ declarations, annotation uses, enum members, methods,
lambda and catch parameters, operators such as `?.` and `??`, and opaque
template bodies receive semantic classifications in addition to lexical
keywords, comments, and literals.

---

# 58. Syntax Highlighting Is Separate

The VS Code extension should still provide a TextMate grammar or equivalent basic syntax highlighting.

This works even before the language server has completed semantic analysis.

The LSP augments it with semantic tokens after parsing the current buffer.

---

# 59. `.gpp.tpl` Support

The server should recognize:

```text
.gpp.tpl
```

files where Go++ template declarations are supported.

Capabilities may include:

```text
diagnostics
symbols
template references
completion where practical
formatting where supported
```

Template semantics should come from the same compiler/template parser.

---

# 60. Template Definitions

For:

```gpp
template UserPage(user User) {
    ...
}
```

the server should index:

```text
UserPage
parameter user
type User
```

and allow navigation from typed calls such as:

```gpp
tpl.UserPage(w, user)
```

---

# 61. Template Diagnostics

Errors in template declarations should point to the `.gpp.tpl` source.

Examples:

```text
unknown field
unknown template
invalid typed argument
invalid declaration
```

---

# 62. Native Go Interop

The LSP must understand imported native Go packages.

Example:

```gpp
import "regexp"

re := regexp.Compile(...)
```

Completion, hover, and definitions should expose relevant native Go information.

---

# 63. Go Package Information

Where practical, Go++ tooling should reuse Go package/type metadata rather than parsing Go source independently.

Useful infrastructure includes Go package loading/type information.

The exact implementation remains internal.

---

# 64. Imported Go Methods

Native Go methods participate in semantic lookup before Go++ extension methods according to the normal extension resolution rules.

Editor completion and hover must reflect the same resolution.

---

# 65. Packages

The LSP must understand Go++ package rules:

```text
one package declaration per file
no package declaration means main
dotted package declarations allowed
source directory need not mirror package syntax directly
```

Navigation must use the compiler's actual package-lowering model.

---

# 66. Imports

Completion should assist with imported package symbols.

Automatic import insertion may be added later.

For v0.1, manually written imports plus semantic completion is acceptable.

---

# 67. Versioned Imports

If Go++ supports imports such as:

```gpp
import "github.com/example/foo@v1.2.3"
```

the language server should parse and represent them correctly.

Dependency fetching should not occur aggressively on every editor query.

---

# 68. Workspace Loading

At startup the LSP should discover relevant project files.

Typical inputs include:

```text
.gpp files
.gpp.tpl files
go.mod
Go source dependencies where needed
Go++ compiler/project configuration
```

Generated build artifacts should normally be excluded.

---

# 69. Large Workspaces

The LSP should avoid eagerly performing expensive backend compilation for the entire workspace.

A practical v0.1 strategy is:

```text
scan files
parse relevant packages
build semantic index
analyze opened/related packages first
```

Optimization can evolve with usage.

---

# 70. Source Manager

A shared source manager is required.

It should provide conversions between:

```text
byte offset
Go++ source span
line
column
LSP position
```

The same source manager should support compiler diagnostics and LSP features.

---

# 71. UTF-16 Position Handling

LSP positions use editor-oriented character positions that commonly require UTF-16 code-unit handling.

Go++ source management must correctly convert between:

```text
UTF-8 byte offsets used internally
↕
LSP line + UTF-16 character position
```

This must be implemented centrally.

Do not scatter ad-hoc position conversion logic throughout handlers.

---

# 72. Position Accuracy

All features depending on source positions must use the same conversion system:

```text
diagnostics
hover
definition
references
rename
completion
formatting
semantic tokens
```

This avoids inconsistent editor behavior.

---

# 73. Error Recovery

Incomplete source is normal while typing.

Examples:

```gpp
class User {
    func Save(
```

or:

```gpp
@{
    http.GET(
```

The language server should remain alive and return partial useful results when possible.

A parse error must not crash the server.

---

# 74. Crash Isolation

An internal compiler or analyzer failure should not terminate the editor if avoidable.

The server should:

```text
recover request boundary where safe
log internal error
return protocol error or diagnostic
continue serving later requests
```

Fatal workspace corruption may require process termination, but this should be exceptional.

---

# 75. Concurrency

The language server may process some independent requests concurrently.

However, workspace mutations such as:

```text
didOpen
didChange
didClose
```

must be synchronized with semantic queries.

The implementation must prevent races between source updates and semantic lookup.

---

# 76. Cancellation

Long-running requests should honor LSP cancellation where practical.

Examples:

```text
workspace symbol search
large reference search
large rename
initial workspace indexing
```

Simple hover/completion requests should already be fast enough that cancellation is rarely relevant.

---

# 77. Caching

The persistent process may cache:

```text
ASTs
semantic package state
symbol indexes
Go package metadata
documentation
dependency information
```

Caches should be invalidated based on source/package changes.

---

# 78. No Global Cache Requirement

A disk-backed global semantic cache is not required for v0.1.

In-memory caching for the lifetime of the LSP process is sufficient.

---

# 79. `gpp doc` Integration

The semantic index used by `gpp doc` should be reusable by the LSP.

This allows consistent representation of:

```text
signatures
documentation
annotations
deprecation
extension methods
records
enums
templates
```

---

# 80. `gpp fmt` Integration

Formatting logic is owned by the formatter library shared by:

```text
gpp fmt
gpp lsp
```

No formatting behavior should exist only in the VS Code extension.

---

# 81. `gpp build` Integration

The LSP uses compiler parsing and semantic analysis but normally does not invoke the entire `gpp build` pipeline.

The relationship is:

```text
shared front end
    ├── gpp build → lowering → Go → go build
    └── gpp lsp   → editor services
```

---

# 82. CLI

Canonical command:

```bash
gpp lsp
```

The command starts the language server and waits for protocol input.

Running it manually in a shell is primarily useful for debugging.

Ordinary users should not need to run it themselves; the editor extension launches it.

---

# 83. Exit Behavior

If stdin closes unexpectedly because the editor terminates, the server should exit cleanly.

It should not remain running as an orphan daemon.

---

# 84. Version Reporting

During initialization, the server should identify itself with:

```text
name: gpp
version: compiler version
```

The LSP version should normally track the installed `gpp` compiler version.

---

# 85. Compiler/Extension Compatibility

The VS Code extension should avoid embedding a second language implementation.

Its compatibility model should be based primarily on the installed:

```text
gpp
```

binary.

This allows compiler upgrades to improve tooling without requiring equivalent logic updates in the extension.

---

# 86. VS Code Extension Responsibilities

The VS Code extension should remain thin.

Its responsibilities are approximately:

```text
register .gpp language
register .gpp.tpl language
provide basic syntax grammar
find gpp executable
start gpp lsp
connect LSP client
forward editor lifecycle
configure formatting
```

Language semantics belong in `gpp lsp`.

---

# 87. VS Code Startup

Typical extension startup:

```text
extension activates
      ↓
locate gpp
      ↓
spawn `gpp lsp`
      ↓
create LanguageClient
      ↓
LSP initialize
      ↓
workspace ready
```

---

# 88. Missing `gpp`

If the VS Code extension cannot find the `gpp` executable, it should display a clear configuration error.

It should not silently disable language services.

Example:

```text
Go++ compiler `gpp` was not found in PATH.
```

A configurable explicit executable path may be supported.

---

# 89. Minimal v0.1 Feature Set

The required initial LSP feature set should be:

```text
initialize/shutdown
document open/change/close
diagnostics
completion
hover
go-to-definition
document symbols
formatting
```

This is enough to make Go++ productive in an editor.

---

# 90. Strong v0.1 Feature Set

Preferably v0.1 also includes:

```text
find references
rename
workspace symbols
signature help
deprecated-symbol metadata
```

These build naturally on the semantic index.

---

# 91. Features That May Wait

The following do not need to block v0.1:

```text
semantic tokens
code actions
code lens
inlay hints
call hierarchy
type hierarchy UI
range formatting
advanced refactors
global disk caches
remote LSP transport
```

They may be added once the core server is stable.

---

# 92. Example Editor Session

Given:

```gpp
class User {
    Name string

    func Save() error {
        ...
    }
}
```

and:

```gpp
user := User(Name: "Bob")
user.
```

the sequence is conceptually:

```text
editor sends didChange
      ↓
gpp lsp reparses source
      ↓
semantic model resolves user as User
      ↓
editor sends completion request
      ↓
gpp lsp returns:
    Name
    Save
    class
    applicable extensions
```

---

# 93. Example Diagnostic Session

User types:

```gpp
user.DeleteEverything()
```

The editor sends the changed source.

`gpp lsp` analyzes it and publishes:

```text
User has no method DeleteEverything
```

with a range pointing precisely at:

```text
DeleteEverything
```

No subprocess needs to be started for this diagnostic.

---

# 94. Example Hover Session

User hovers:

```gpp
CompileRegex
```

in:

```gpp
pattern.CompileRegex()
```

The server may return:

```text
extension func string.CompileRegex() (*regexp.Regexp, error)

Compiles the receiver as a regular expression.
Equivalent to regexp.Compile(receiver).
```

---

# 95. Example Deprecated Symbol

Given:

```gpp
func OldAPI() @{
    Deprecated("Use NewAPI instead")
} {
    ...
}
```

usage:

```gpp
OldAPI()
```

may receive:

```text
warning: OldAPI is deprecated

Use NewAPI instead
```

and the editor may render the symbol with strikethrough.

---

# 96. Performance Goal

Interactive requests should feel immediate for normal projects.

Particularly:

```text
hover
completion
definition
```

should generally be answered from in-memory state rather than triggering full project rebuilds.

---

# 97. Correctness Before Incrementality

The v0.1 implementation should prioritize:

```text
correct semantic results
stable server behavior
accurate positions
shared compiler semantics
```

over sophisticated incremental analysis.

Reanalyzing an entire package after an edit is acceptable if performance remains reasonable.

---

# 98. Implementation Boundary

A useful internal API might resemble:

```go
type Workspace struct {
    // internal state
}

func (w *Workspace) Open(uri string, source []byte) error
func (w *Workspace) Change(uri string, source []byte) error
func (w *Workspace) Close(uri string) error

func (w *Workspace) Diagnostics(uri string) []Diagnostic
func (w *Workspace) Complete(uri string, pos Position) []Completion
func (w *Workspace) Hover(uri string, pos Position) *Hover
func (w *Workspace) Definition(uri string, pos Position) []Location
func (w *Workspace) References(uri string, pos Position) []Location
func (w *Workspace) Rename(uri string, pos Position, name string) []Edit
func (w *Workspace) Symbols(uri string) []Symbol
func (w *Workspace) Format(uri string) []Edit
```

These are conceptual interfaces, not required public APIs.

---

# 99. Protocol Layer

The protocol layer should mainly perform:

```text
LSP request
    ↓
URI/position conversion
    ↓
Workspace operation
    ↓
Go++ result
    ↓
LSP response conversion
```

It should contain as little language-semantic logic as possible.

---

# 100. Non-Goals

`gpp lsp` v0.1 is not intended to provide:

```text
a global machine-wide Go++ daemon
a network service
remote code execution
project builds on every keypress
a second compiler
a second formatter
editor-specific semantic implementations
perfect incremental compilation
full IDE refactoring infrastructure
```

---

# 101. Design Summary

Normal lifecycle:

```text
VS Code opens project
        ↓
starts `gpp lsp`
        ↓
server loads workspace
        ↓
server stays alive
        ↓
editor sends file changes and semantic queries
        ↓
server updates in-memory compiler state
        ↓
server returns diagnostics/completion/hover/etc.
        ↓
editor closes
        ↓
server exits
```

Core architecture:

```text
                    ┌─────────────┐
                    │  gpp build  │
                    └──────▲──────┘
                           │
                   shared compiler
                           │
      ┌────────────────────┼────────────────────┐
      │                    │                    │
   gpp doc              gpp fmt              gpp lsp
                                                │
                                                ▼
                                            editors
```

The core rules are:

> `gpp lsp` is one persistent process per editor workspace/session.

> The default transport is standard input/output using standard LSP JSON-RPC framing.

> File edits update an in-memory workspace; they do not restart the `gpp` process.

> Unsaved editor buffers are first-class compiler inputs.

> The server reuses the same parser, semantic model, source manager, formatter, and documentation metadata as the rest of the Go++ toolchain.

> v0.1 may reparse changed files and reanalyze whole packages rather than implementing sophisticated incremental compilation.

> Normal editor operations must not invoke Go code generation or `go build` on every change.

> The VS Code extension remains thin; language intelligence belongs in `gpp lsp`.

> Correctness, stable source positions, and shared compiler semantics matter more than advanced LSP features for the first release.
