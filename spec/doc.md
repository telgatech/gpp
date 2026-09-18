# Go++ `gpp doc` Specification

## 1. Overview

`gpp doc` displays documentation for Go++ packages, types, classes, functions, methods, fields, extensions, annotations, templates, enums, and imported native Go symbols.

It is the Go++ equivalent of:

```bash
go doc
```

but understands the Go++ source model rather than exposing generated Go implementation details.

Examples:

```bash
gpp doc
gpp doc User
gpp doc User.Save
gpp doc gpp/http
gpp doc gpp/http.Server
gpp doc string.TrimSpace
gpp doc tpl.Execute
```

The central rule is:

> `gpp doc` documents the program the developer wrote, not the Go code the compiler generated.

---

# 2. Goals

`gpp doc` should:

* make source APIs quickly discoverable;
* preserve familiar `go doc` ergonomics;
* understand Go++ language features;
* include native Go documentation where relevant;
* hide compiler-generated implementation details;
* work before a project is fully buildable where practical;
* provide both human-readable and machine-readable output;
* become a foundation for IDE hover/completion documentation.

---

# 3. Basic Usage

```bash
gpp doc
```

displays documentation for the current package.

```bash
gpp doc Symbol
```

displays documentation for a symbol in the current package.

```bash
gpp doc Type.Member
```

displays documentation for a member.

```bash
gpp doc package/path
```

displays package documentation.

```bash
gpp doc package/path.Symbol
```

displays documentation for a package symbol.

Examples:

```bash
gpp doc User
gpp doc User.Save
gpp doc Order.Status
gpp doc gpp/http.Server
gpp doc gpp/tpl.Execute
gpp doc string.TrimSpace
```

---

# 4. Default Package

Without an explicit package:

```bash
gpp doc User
```

uses the current Go++ package.

If the current directory contains:

```gpp
package shop
```

then:

```bash
gpp doc User
```

means:

```text
shop.User
```

If there is no package declaration, the package is:

```text
main
```

consistent with normal Go++ rules.

---

# 5. Current Package Overview

Running:

```bash
gpp doc
```

should display a concise package overview.

Example:

```text
package shop

Package shop provides the storefront application domain.

Classes:
    Customer
    Order
    Product

Enums:
    OrderStatus

Functions:
    CalculateTax
    FindProduct

Templates:
    ProductPage
    CheckoutPage
```

Only public symbols should appear by default.

---

# 6. Package Documentation

Package documentation comes from ordinary documentation comments.

Example:

```gpp
// Package shop contains the application's storefront domain.
package shop
```

Then:

```bash
gpp doc
```

may display:

```text
package shop

Package shop contains the application's storefront domain.
```

Go++ should follow ordinary Go documentation-comment conventions where practical.

---

# 7. Symbol Documentation

Example:

```gpp
// Customer represents a storefront customer.
class Customer {
    Name string
    Email string
}
```

Then:

```bash
gpp doc Customer
```

should display:

```text
class Customer

Customer represents a storefront customer.

Fields:
    Name string
    Email string
```

---

# 8. Documentation Comments

Documentation comments immediately preceding declarations are associated with those declarations.

Example:

```gpp
// Save persists the customer.
func Save() error {
    ...
}
```

The comment should be shown by:

```bash
gpp doc Customer.Save
```

Normal comment style:

```gpp
// Save persists the customer.
```

is preferred.

Block comments may also be supported:

```gpp
/*
Save persists the customer.
*/
func Save() error
```

---

# 9. Classes

For:

```gpp
// User represents an application user.
class User : Entity {
    ID int
    Name string

    func Save() error
    func Delete() error

    static func Guest() User
}
```

`gpp doc User` should produce something conceptually like:

```text
class User : Entity

User represents an application user.

Fields:
    ID int
    Name string

Methods:
    func Save() error
    func Delete() error

Static Methods:
    static func Guest() User
```

---

# 10. Inheritance

Class documentation should show direct parents.

Example:

```text
class Employee : Person, Auditable
```

Inherited members should not overwhelm the default output.

Default:

```text
Methods:
    func Work()
    func Save()

Inherited:
    Person
    Auditable
```

A detailed mode may include inherited members.

---

# 11. Inherited Members

Use:

```bash
gpp doc --all Employee
```

to include inherited methods and fields.

Example:

```text
Methods:
    Employee.Work()
    Person.Name()
    Auditable.CreatedAt()
```

Owner information should be shown when necessary.

---

# 12. Method Documentation

Given:

```gpp
// Save persists the user.
func Save() error {
    ...
}
```

running:

```bash
gpp doc User.Save
```

should show:

```text
func (User) Save() error

Save persists the user.
```

Even if Go++ source does not explicitly spell the receiver that way, the output may use a concise familiar presentation.

---

# 13. Static Methods

For:

```gpp
static func Guest() User
```

display:

```text
static func User.Guest() User
```

or:

```text
User.Guest

    static func Guest() User
```

The documentation must make static dispatch clear.

---

# 14. Fields

Given:

```gpp
class User {
    // Name is the display name.
    Name string
}
```

then:

```bash
gpp doc User.Name
```

should display:

```text
field User.Name string

Name is the display name.
```

---

# 15. Constructors

Compact constructors:

```gpp
class User {
    Name string
    Age int

    init(Name, Age)
}
```

should appear in class documentation.

Example:

```text
Constructor:
    User(Name string, Age int)
```

The user should not need to understand the compiler's generated constructor implementation.

---

# 16. Extension Methods

Extension methods are first-class documentation symbols.

Example:

```gpp
extend string {
    // Blank reports whether the string contains only whitespace.
    func Blank() bool
}
```

Then:

```bash
gpp doc string.Blank
```

should display:

```text
extension func string.Blank() bool

Blank reports whether the string contains only whitespace.
```

---

# 17. Extension Discovery

Running:

```bash
gpp doc string
```

should include applicable Go++ extensions.

Example:

```text
Extensions:
    func Blank() bool
    func Contains(substr string) bool
    func HasPrefix(prefix string) bool
    func Split(sep string) []string
    func ToLower() string
    func TrimSpace() string
```

Native Go methods, where applicable, should remain distinguishable from extensions.

---

# 18. Generic Extensions

For:

```gpp
extend []T {
    func Filter(fn func(T) bool) []T
}
```

documentation may display:

```text
extension func []T.Filter(fn func(T) bool) []T
```

Generic parameters should remain visible.

---

# 19. Prelude Extensions

Prelude extensions should be discoverable normally.

Examples:

```bash
gpp doc string.TrimSpace
gpp doc error.Is
gpp doc io.Reader.ReadAll
```

These should not require the developer to know which internal prelude file defines them.

---

# 20. Native Go Symbols

`gpp doc` should also support native Go symbols.

Example:

```bash
gpp doc strings.TrimSpace
```

should show the ordinary Go documentation for:

```go
strings.TrimSpace
```

The source may come from the installed Go package metadata/tooling.

---

# 21. Go++ Extensions on Native Types

For:

```bash
gpp doc string
```

the output may distinguish:

```text
Built-in type:
    string

Go++ Extensions:
    TrimSpace
    Contains
    Split
    ...
```

Do not pretend these extension methods are actual methods in the Go runtime type.

---

# 22. Functions

Given:

```gpp
// CalculateTax computes tax for an order.
func CalculateTax(order Order) decimal.Decimal
```

then:

```bash
gpp doc CalculateTax
```

displays:

```text
func CalculateTax(order Order) decimal.Decimal

CalculateTax computes tax for an order.
```

---

# 23. Function Overloads

For:

```gpp
func Load(id int) User
func Load(email string) User
```

then:

```bash
gpp doc Load
```

should show all overloads:

```text
func Load(id int) User
func Load(email string) User
```

Documentation comments belonging to individual overloads should appear with the corresponding signature where needed.

---

# 24. Overloaded Methods

Likewise:

```gpp
class User {
    func Find(id int) User
    func Find(email string) User
}
```

should display all overloads under:

```bash
gpp doc User.Find
```

---

# 25. Records

Anonymous records do not normally have standalone declaration names.

However, signatures containing records should display their source-level shape.

Example:

```text
func Stats() record{
    Users int
    Orders int
}
```

Generated internal names such as:

```text
__gpp_record_83acf001
```

must never appear in normal documentation.

---

# 26. Named Context Around Records

If a function returns an inferred anonymous record:

```gpp
func Stats() record {
    return record(
        Users: users,
        Orders: orders,
    )
}
```

`gpp doc Stats` should display the inferred structural return type.

Example:

```text
func Stats() record{
    Users int
    Orders int
}
```

---

# 27. Enums

Given:

```gpp
// OrderStatus describes the lifecycle of an order.
enum OrderStatus string {
    Pending
    Paid
    Shipped
}
```

then:

```bash
gpp doc OrderStatus
```

should display:

```text
enum OrderStatus string

OrderStatus describes the lifecycle of an order.

Values:
    Pending = "Pending"
    Paid    = "Paid"
    Shipped = "Shipped"
```

---

# 28. Enum Members

Running:

```bash
gpp doc OrderStatus.Pending
```

may display:

```text
OrderStatus.Pending = "Pending"
```

plus any member documentation if supported.

---

# 29. Annotations

Annotation declarations are documentation symbols.

For:

```gpp
annotation (
    // Table maps a class to a database table.
    Table(name string) on class
)
```

then:

```bash
gpp doc Table
```

may display:

```text
annotation Table(name string) on class

Table maps a class to a database table.
```

---

# 30. Qualified Annotations

Package-qualified annotations should work:

```bash
gpp doc orm.Table
gpp doc http.GET
gpp doc tpl.Path
```

The documentation should include:

```text
arguments
allowed targets
package
description
```

---

# 31. Annotation Usage on Symbols

Documentation for a declaration should show relevant annotations.

Example:

```gpp
class User @{
    orm.Table("users")
}
```

`gpp doc User` may include:

```text
Annotations:
    @orm.Table("users")
```

---

# 32. Templates

Templates are first-class documented declarations.

For:

```gpp
// BlogPost renders a blog post.
template BlogPost(post Post) {
    ...
}
```

then:

```bash
gpp doc BlogPost
```

should display:

```text
template BlogPost(post Post)

BlogPost renders a blog post.
```

---

# 33. Template Metadata

For:

```gpp
template BlogPost(post Post) @{
    tpl.Path("/blog/{id}")
} {
    ...
}
```

documentation may display:

```text
template BlogPost(post Post)

Annotations:
    @tpl.Path("/blog/{id}")
```

The HTML/template body should not normally be printed in concise documentation.

---

# 34. Template Source

A source flag may show the declaration body:

```bash
gpp doc --source BlogPost
```

This should display the original `.gpp` or `.gpp.tpl` source, not generated Go.

---

# 35. Template Call Signature

Because compiled templates have typed generated call bindings, documentation should show their source-level call signature.

Example:

```text
template BlogPost(post Post)

Typed call:
    tpl.BlogPost(w io.Writer, post Post) error
```

This is especially useful for discoverability.

---

# 36. Packages

Running:

```bash
gpp doc gpp/http
```

should provide something like:

```text
package http

HTTP server and request handling support for Go++.

Classes:
    Server
    Context

Annotations:
    GET
    POST
    Port
    IP

Functions:
    StatusText
```

The exact surface should reflect actual exported declarations.

---

# 37. Official Go++ Packages

`gpp doc` should work particularly well for compiler-shipped packages:

```text
gpp/http
gpp/orm
gpp/tpl
gpp/encoding
gpp/test
gpp/collections
```

Examples:

```bash
gpp doc gpp/http.Server
gpp doc gpp/tpl.Execute
gpp doc gpp/orm.Table
```

---

# 38. Package Aliases

If source imports:

```gpp
import web "gpp/http"
```

then:

```bash
gpp doc web.Server
```

may resolve using the current package's import aliases.

Canonical output should still identify the actual package:

```text
gpp/http.Server
```

---

# 39. Search

A search mode should help find symbols.

```bash
gpp doc --search trim
```

Example:

```text
string.Trim
string.TrimLeft
string.TrimRight
string.TrimSpace
strings.Trim
strings.TrimFunc
```

Search should include:

```text
current package
imports
prelude
official gpp packages
```

with broader module search optionally available.

---

# 40. Short Search Alias

A shorter form may be supported:

```bash
gpp doc -s trim
```

---

# 41. Exact Symbol Resolution

Without search mode:

```bash
gpp doc User.Save
```

performs symbol resolution rather than fuzzy search.

If ambiguous, report candidates.

Example:

```text
Save is ambiguous.

Candidates:
    User.Save
    Order.Save

Use:
    gpp doc User.Save
```

---

# 42. Case Sensitivity

Symbol resolution should follow Go/Go++ identifier case semantics.

However, `--search` may perform case-insensitive matching for usability.

---

# 43. Public vs Private Symbols

By default, `gpp doc` should show public symbols.

Use:

```bash
gpp doc --all
```

to include package-private declarations.

Example:

```bash
gpp doc --all User
```

includes lowercase/private members.

---

# 44. Generated Members

Generated source-level behaviors should be documented if they are part of the Go++ programming model.

For example:

```text
enum From(...)
serialization methods
annotation-generated methods
class metadata
```

may appear when genuinely callable by the programmer.

Compiler implementation helpers should not.

---

# 45. Generated Member Marking

Where a method is generated from source metadata, mark it.

Example:

```text
Generated:
    static func From(value string) (OrderStatus, error)
```

or:

```text
Generated from @encoding.Serializable:
    func JSON() ([]byte, error)
```

This explains why the method exists without exposing compiler internals.

---

# 46. Suppression by User Definitions

If generated behavior is suppressed because the user supplied a compatible method, documentation should show only the effective API.

Do not show both hypothetical generated and actual user methods.

---

# 47. `.class` Metadata

Class documentation may mention introspection support.

Example:

```text
Metadata:
    User.class
```

Detailed reflection metadata itself need not flood normal output.

A future:

```bash
gpp doc --metadata User
```

may show:

```text
fields
methods
parents
annotations
```

---

# 48. Error Model in Signatures

Functions that return trailing `error` should show the actual signature.

Example:

```text
func LoadUser(id int) (User, error)
```

Even though Go++ permits:

```gpp
user := LoadUser(id)
```

through automatic error promotion.

Documentation should describe the real semantic signature, not hide the `error`.

---

# 49. Exception-Like Behavior

Where useful, function documentation may add:

```text
Trailing error participates in automatic Go++ error promotion.
```

But this need not be repeated on every function in concise output.

The actual `(T, error)` signature is sufficient for experienced users.

---

# 50. Throws

Go++ does not need a Java-style declared `throws` section.

Errors remain Go `error` values and trailing-error promotion is a language behavior.

Therefore documentation should not invent:

```text
throws FooError
```

unless the source language later gains explicit typed throw declarations.

---

# 51. Source Locations

A location flag should show source location.

```bash
gpp doc --location User.Save
```

Example:

```text
src/user.gpp:42
```

For templates:

```text
views/blog.gpp.tpl:18
```

Native Go symbols may show their Go source location when available.

---

# 52. Source Display

```bash
gpp doc --source User.Save
```

prints the original declaration.

Example:

```gpp
// Save persists the user.
func Save() error {
    ...
}
```

Generated Go should not be shown.

---

# 53. Generated Go Inspection

If generated Go needs inspection, use compiler tooling such as:

```bash
gpp build --emit-go
```

`gpp doc` is not intended as a generated-code browser.

---

# 54. JSON Output

Machine-readable documentation should be available.

```bash
gpp doc --json User
```

Conceptually:

```json
{
  "kind": "class",
  "name": "User",
  "package": "shop",
  "doc": "User represents an application user.",
  "fields": [],
  "methods": [],
  "parents": []
}
```

The exact schema should be versioned/stable enough for tooling.

---

# 55. IDE Reuse

The documentation engine should be reusable for:

```text
hover
completion detail
signature help
go-to-definition metadata
documentation browsers
```

Avoid implementing `gpp doc` as a CLI-only text scraper.

It should consume compiler semantic metadata.

---

# 56. Semantic Index

A package documentation index should derive from the compiler's semantic model.

Conceptually:

```text
Package
    Symbols
        Class
        Function
        Enum
        Annotation
        Template
        Extension
        Type
```

This is preferable to reparsing source independently for documentation.

---

# 57. Documentation Resolution Pipeline

Conceptually:

```text
query
    ↓
package/module resolution
    ↓
Go++ semantic symbol table
    ↓
native Go package metadata if needed
    ↓
effective source-level symbol
    ↓
documentation renderer
```

---

# 58. Build Independence

`gpp doc` should not require a successful full application build when the requested package can be parsed and semantically indexed.

For example, documentation should ideally still work if an unrelated function body currently contains a compile error.

This makes documentation useful during active development.

---

# 59. Parsing Requirements

At minimum, documentation requires declarations and enough semantic analysis to resolve:

```text
packages
types
members
extensions
annotations
inheritance
overloads
```

Full code generation should not be necessary.

---

# 60. Dependency Resolution

If documentation targets an imported native Go package, Go module resolution may be used normally.

Example:

```bash
gpp doc github.com/jmoiron/sqlx.DB
```

should be able to use Go package metadata if that dependency is available.

---

# 61. Versioned Imports

If a Go++ project uses:

```gpp
import "github.com/foo/bar@v1.2.3"
```

documentation should resolve the version selected by the current module environment.

User-facing output should normally show:

```text
github.com/foo/bar
```

with version metadata optionally shown under verbose output.

---

# 62. Verbose Mode

Use:

```bash
gpp doc --verbose User
```

or:

```bash
gpp doc -v User
```

to include more metadata.

Possible details:

```text
package
source file
visibility
parents
annotations
generated members
inherited members
extension origin
module version
```

---

# 63. Concise Default

Default output should remain concise.

For example:

```bash
gpp doc User
```

should not dump:

```text
every inherited Object method
every annotation metadata field
compiler runtime methods
generated bridge methods
hidden identity fields
```

unless specifically requested.

---

# 64. Root `Object`

A Go++ class implicitly inherits from `Object`.

Default documentation should not clutter every class with:

```text
: Object
```

unless useful.

For a simple class:

```gpp
class User
```

display:

```text
class User
```

not necessarily:

```text
class User : Object
```

Verbose mode may expose the implicit root.

---

# 65. Hidden Runtime Internals

Never document internal fields such as:

```text
__gpp_root
__gpp_vtable
__gpp_identity
compiler temporary fields
generated record hash names
bridge functions
```

These are implementation details.

---

# 66. Multiple Inheritance Ownership

When displaying inherited ambiguous names, show owners.

Example:

```text
Methods:
    A.Save()
    B.Save()
```

This mirrors the language's requirement that ambiguous inherited members must be qualified.

---

# 67. Overrides

For an overridden method:

```gpp
class Employee : Person {
    func Name() string
}
```

documentation may show:

```text
func Name() string
    overrides Person.Name
```

under verbose mode.

---

# 68. `super`

`super` is a call mechanism, not a standalone documented member.

Do not list synthetic `super` methods.

---

# 69. Native Method vs Extension

If a native method and extension share a name, documentation should reflect actual resolution precedence.

Example:

```text
Methods:
    NativeMethod()

Extensions:
    OtherExtension()
```

A shadowed/inapplicable extension does not need to appear as an effective member unless verbose diagnostics are requested.

---

# 70. Documentation for `any`

Built-in language types should be queryable.

Examples:

```bash
gpp doc any
gpp doc Object
gpp doc record
```

The output should explain their Go++ semantics concisely.

---

# 71. Language Keywords

`gpp doc` may also document language constructs.

Examples:

```bash
gpp doc class
gpp doc extend
gpp doc record
gpp doc try
gpp doc throw
gpp doc template
gpp doc annotation
```

This makes the CLI useful as a language reference.

---

# 72. Language Topic Resolution

If a query matches a language keyword rather than a symbol:

```bash
gpp doc try
```

display the built-in language documentation topic.

Example:

```text
try / catch / finally

try {
    ...
} catch SomeError e {
    ...
} catch e {
    ...
} finally {
    ...
}
```

---

# 73. Operator Documentation

Operators may also be documented.

Examples:

```bash
gpp doc ??
gpp doc ||
gpp doc ||=
```

This is particularly useful because Go++ adds semantics beyond Go.

Example:

```bash
gpp doc ??
```

could explain expression-level error fallback.

---

# 74. Documentation Precedence

When a query may refer to both a project symbol and a language topic:

```text
project symbol wins
```

unless an explicit topic mode is used.

Possible explicit mode:

```bash
gpp doc --lang record
```

---

# 75. `gpp doc` and `go doc`

The intended relationship is:

```text
go doc
    documents Go source API

gpp doc
    documents Go++ source API
    and delegates to Go metadata where appropriate
```

Go++ should reuse Go documentation data rather than duplicate standard-library documentation manually.

---

# 76. Output Style

Output should remain terminal-friendly and plain by default.

Example:

```text
class User : Entity

User represents an application user.

Fields:
    ID   int
    Name string

Methods:
    func Save() error
    func Delete() error

Static Methods:
    static func Guest() User
```

No heavy table formatting is required.

---

# 77. Color

Color may be used when stdout is a terminal.

Possible highlighting:

```text
symbol names
types
section headings
source locations
```

Support:

```bash
gpp doc --color=auto
gpp doc --color=always
gpp doc --color=never
```

This can follow the rest of `gpp` CLI behavior.

---

# 78. Pager

Long documentation may automatically use a pager when running interactively.

This is optional for v1.

It should never interfere with piped output.

---

# 79. Piping

This should work cleanly:

```bash
gpp doc User > user.txt
```

and:

```bash
gpp doc --json User | jq
```

No interactive decoration should appear when output is not a terminal.

---

# 80. Exit Codes

Recommended:

```text
0
    documentation found

1
    symbol/package not found

2
    invalid command usage

other
    dependency/compiler/tooling failure
```

---

# 81. Unknown Symbol

Example:

```bash
gpp doc Usre
```

may report:

```text
symbol not found: Usre

Did you mean:
    User
```

Use the compiler symbol table for suggestions.

---

# 82. Ambiguous Symbol

Example:

```text
symbol "Save" is ambiguous

Candidates:
    User.Save
    Order.Save
    Product.Save
```

The tool should not silently select one.

---

# 83. Search Scope

Default symbol lookup should remain narrow and deterministic.

Search mode may be broader.

Recommended search scopes:

```text
current package
current module
imports
prelude
official packages
```

A flag may specify:

```bash
gpp doc --search --module save
```

for module-wide search.

---

# 84. Module-Wide Search

Example:

```bash
gpp doc --search --module customer
```

may return:

```text
shop.Customer
billing.CustomerAccount
admin.CustomerPage
```

---

# 85. Documentation Generation Foundation

Although `gpp doc` is initially a terminal command, its semantic documentation model should later support static documentation generation.

Potential future command:

```bash
gpp docs
```

or:

```bash
gpp doc --html
```

This is not required for `gpp doc` v1.

---

# 86. V1 Recommended Command Surface

```text
gpp doc
gpp doc <symbol>
gpp doc <package>
gpp doc <package.symbol>

gpp doc --all <symbol>
gpp doc --source <symbol>
gpp doc --location <symbol>
gpp doc --search <term>
gpp doc --json <symbol>
gpp doc --verbose <symbol>
```

Short aliases may include:

```text
-a
-s
-v
```

where unambiguous.

---

# 87. Example: Class

Command:

```bash
gpp doc User
```

Output:

```text
class User : Entity

User represents an application user.

Fields:
    ID    int
    Name  string
    Email string

Methods:
    func Save() error
    func Delete() error

Static Methods:
    static func Guest() User
```

---

# 88. Example: Extension

Command:

```bash
gpp doc string.TrimSpace
```

Output:

```text
extension func string.TrimSpace() string

TrimSpace removes leading and trailing whitespace.

Equivalent to strings.TrimSpace.
```

---

# 89. Example: Enum

Command:

```bash
gpp doc OrderStatus
```

Output:

```text
enum OrderStatus string

OrderStatus describes the lifecycle of an order.

Values:
    Pending = "Pending"
    Paid    = "Paid"
    Shipped = "Shipped"

Generated:
    static func From(value string) (OrderStatus, error)
```

---

# 90. Example: Annotation

Command:

```bash
gpp doc tpl.Path
```

Output:

```text
annotation tpl.Path(pattern string) on template

Associates a path pattern with a template for lookup.

Example:

    template Blog(post Post) @{
        tpl.Path("/blog/{id}")
    } {
        ...
    }
```

---

# 91. Example: Template

Command:

```bash
gpp doc BlogPost
```

Output:

```text
template BlogPost(post Post)

BlogPost renders a blog post.

Annotations:
    @tpl.Path("/blog/{id}")

Typed call:
    tpl.BlogPost(w io.Writer, post Post) error
```

---

# 92. Example: Error-Producing Function

Command:

```bash
gpp doc LoadUser
```

Output:

```text
func LoadUser(id int) (User, error)

LoadUser loads a user by ID.
```

No special exception syntax is required because trailing `error` is already represented accurately.

---

# 93. Example: Language Topic

Command:

```bash
gpp doc ??
```

Output:

```text
operator ??

Expression-level error fallback.

    value := Load() ?? fallback

The left expression is evaluated first. If it completes successfully,
its value is returned. If it throws a Go++ error, the fallback
expression is evaluated.

Raw Go panics are not caught.
```

---

# 94. Example: `try`

Command:

```bash
gpp doc try
```

Output:

```text
try / catch / finally

    try {
        user := LoadUser(id)
    } catch NotFoundError e {
        ...
    } catch e {
        ...
    } finally {
        ...
    }

catch handles Go++ thrown errors.

Raw Go panics are not caught.
```

---

# 95. Example: Current Package

Command:

```bash
gpp doc
```

Output:

```text
package store

Store application domain.

Classes:
    Customer
    Order
    Product

Enums:
    OrderStatus

Functions:
    CalculateTax
    FindProduct

Templates:
    CheckoutPage
    ProductPage
```

---

# 96. Implementation Architecture

Recommended architecture:

```text
source files
    ↓
Go++ parser
    ↓
semantic package index
    ↓
documentation symbol model
    ↓
query resolver
    ↓
terminal / JSON renderer
```

Native Go symbols may enter through:

```text
go/packages
go/types
Go documentation metadata
```

---

# 97. Documentation Symbol Model

Conceptually:

```go
type DocSymbol struct {
    Kind       SymbolKind
    Name       string
    FullName   string
    Package    string
    Visibility Visibility

    Signature string
    Doc       string

    Source Span

    Children    []DocSymbol
    Annotations []AnnotationInfo
}
```

Additional typed fields may be preferable to a single generic structure.

---

# 98. Source-of-Truth Rule

`gpp doc` should use:

```text
Go++ AST
semantic model
Go package metadata
```

as sources of truth.

It should not infer public APIs from generated `.go` files.

Generated Go is an implementation artifact and may change without changing the Go++ API.

---

# 99. Diagnostic Integration

Errors from `gpp doc` should use the same source-mapped diagnostic infrastructure as the compiler.

For example:

```text
app.gpp:18:5:
duplicate documentation target
```

or semantic errors encountered during indexing should point to `.gpp` source.

---

# 100. Non-Goals

`gpp doc` v1 does not need to:

* generate a complete documentation website;
* expose generated Go internals;
* execute examples;
* run tests;
* evaluate template bodies;
* provide package popularity information;
* replace pkg.go.dev;
* maintain a second copy of Go standard-library documentation;
* require comments on every declaration.

---

# 101. Design Summary

The documentation flow is:

```text
Go++ source
    ↓
semantic symbol model
    ↓
source-level API
    ↓
gpp doc
```

rather than:

```text
Go++ source
    ↓
generated Go
    ↓
go doc
```

Examples:

```bash
gpp doc User
gpp doc User.Save
gpp doc string.TrimSpace
gpp doc tpl.Execute
gpp doc tpl.Path
gpp doc BlogPost
gpp doc ??
```

The core principle is:

> `gpp doc` should make the effective Go++ application API discoverable while hiding the compiler machinery used to implement it.
