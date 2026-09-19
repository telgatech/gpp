# Go++ Full Compiler AST

## Status

This specification defines the end state of the Go++ frontend. String-valued
executable bodies such as `Method.Body` are migration artifacts and MUST NOT
remain the compiler's long-term representation of program syntax. Mixed
declarations retain tokens, spans, and parsed Go metadata; they do not carry a
duplicate executable source string.

The compiler may preserve source text for comments, raw strings, template
content, embedded files, and exact diagnostics. It MUST NOT preserve executable
Go++ syntax only as an opaque string in order to bypass parsing or AST
transformation.

Current migration invariants already enforced by the frontend are:

* declaration signatures use structured type and parameter nodes;
* method and function bodies use `BlockStmt` trees with typed statements;
* unsupported statements use token-preserving `TokenStmt`, never a raw
  declaration or a wrapper containing a duplicate typed node;
* annotation arguments retain token groups and expression nodes; and
* overload/callable metadata retains parameter and result type nodes; and
* exception lowering builds its `try/catch/finally` runtime wrapper from Go
  AST nodes, normalizing nested constructor expressions before parsing the
  generated wrapper;
* exception catch clauses carry Go-AST block bodies at the generated-Go
  boundary, and retain catch types as `TypeNode` values until Go lowering, so
  catch dispatch does not retain or reparse executable body or type strings;
  and
* exception ABI result metadata retains result types as `TypeNode` values and
  prints them only while constructing the generated boundary wrapper; and
* compatibility declarations do not duplicate singular Go/function nodes
  beside their structured slices; and
* parser-only statement drafts are discarded after clause assembly, so the
  public body tree does not retain both a typed statement and a second
  structured copy of it; and
* semantic prelude and callable discovery consumes declaration tokens or AST
  metadata rather than reparsing executable compatibility source; and
* safe-access lowering consumes typed selector and call expressions from
  `BlockStmt` bodies, including the parameter metadata of top-level
  functions, rather than scanning source characters; and
* enum lowering consumes typed selectors, enum-valued locals, and range
  element bindings from `BlockStmt` trees, without a source-rewrite fallback;
* lambda lowering consumes `LambdaExpr` nodes, including nested lambdas and
  captured parameter types, without a source scanner or overlap fallback.
* callable/default-argument lowering consumes typed call expressions from
  structured method and top-level function bodies, including indexed generic
  callees and function-literal arguments, without a character scanner;
* block function and lambda literals retain their source spans when rendered
  as call arguments, so statement boundaries are not reconstructed from a
  whitespace-free expression string; and
* generated import discovery, alias selection, import rewriting, and source
  directive placement consume the lossless token stream rather than reparsing
  generated source with `go/parser`; and
* ordinary Go declarations are emitted from their stored `go/ast` nodes; the
  owning source buffer is not used to recover executable declaration text at
  the emission boundary; and
* Go++ value declarations are emitted from their keyword, names, type, and
  initializer nodes; token-preserving expression fallbacks retain structural
  separators needed by multiline literals; and
* unsupported top-level syntax is rejected with a source diagnostic instead of
  entering a raw source-rewrite fallback; and
* top-level function parsing is strict at the source-frontend boundary:
  malformed function bodies cannot silently downgrade to `MixedDecl`, while
  receiver methods and function-type declarations remain ordinary structured
  Go declarations; and

Constructor lowering consumes typed call expressions, including calls found in
top-level function bodies and token-preserving compatibility statements. It
does not have a legacy source-scanning fallback; a constructor expression that
cannot be represented by the body AST is reported as a compiler error.

Legacy source lowerers may still materialize text at their final Go-emission
boundary, but that text must not be stored as the source AST or used as a
replacement for parsing.

## 1. Goals

The frontend MUST provide a lossless, source-oriented AST for:

* packages and imports;
* classes, inheritance, fields, methods, and constructors;
* functions, parameters, results, and default values;
* enums and enum members;
* records and record construction;
* extensions and extension constraints;
* annotations and annotation arguments;
* embeds and templates, including layout metadata;
* statements and expressions inside every function or method body;
* Go++ operators, interpolation, safe access, lambdas, exceptions, and
  multi-type catches;
* ordinary Go syntax accepted by Go++.

The AST MUST retain source spans and comment/trivia ownership sufficiently for
diagnostics, formatting, LSP features, and source mapping.

## 2. Non-Goals

The AST does not need to preserve arbitrary insignificant whitespace as a
semantic property. It does need to preserve it while reporting exact source
locations and while retaining opaque payloads.

The AST is not the generated Go AST. Lowering to Go is a separate phase and
must not be used as a way to reconstruct Go++ source.

## 3. Source Model

Every parsed file has a source object:

```go
type SourceFile struct {
    Name       string
    Text       []byte
    LineStarts []int
    Tokens     []Token
    Comments   []Comment
    Package    *PackageDecl
    Decls      []Decl
}
```

Offsets are UTF-8 byte offsets into `Text`. Lines are one-based in public
diagnostics. Tokens and nodes retain half-open `[Start, End)` spans.

The existing public `compiler.File` may remain as a compatibility façade while
the migration is in progress, but it must eventually reference the structured
source model rather than duplicate raw executable text. Ordinary Go
declarations use `GoDecl` with `go/ast` nodes, and annotated ordinary Go
declarations retain their annotation placements on that node instead of
falling back to a mixed source container.

## 4. Lossless Tokens

The lexer MUST emit tokens for identifiers, keywords, literals, operators,
punctuation, newlines where layout requires them, comments, and EOF.

Each token contains:

```go
type Token struct {
    Kind TokenKind
    Text string
    Span Span
}

type Span struct {
    Start int
    End   int
    Line  int
    Column int
}
```

Comments are tokens or attached trivia, but they MUST remain available to the
formatter and documentation tooling. The lexer MUST distinguish:

* line comments;
* block comments;
* interpreted strings;
* raw strings;
* rune literals;
* template text and template actions;
* interpolation delimiters and interpolation expressions.

Unknown characters MUST produce a source diagnostic instead of silently being
discarded.

## 5. AST Node Contract

Every executable or declarative node implements:

```go
type Node interface {
    node()
    Span() Span
}
```

Nodes retain leading, trailing, and detached comments where attachment is
unambiguous. All nodes are immutable after parsing; later compiler phases
produce transformed nodes or explicit lowering IR.

## 6. Declarations

The declaration layer contains structured nodes equivalent to:

```go
type Decl interface { Node; decl() }

type FuncDecl struct {
    Name       string
    TypeParams []*TypeParam
    Params     []*Param
    Results    []*Type
    Body       *BlockStmt
    Annotations []*AnnotationUse
}

type ClassDecl struct {
    Name        string
    Parents     []*Type
    Fields      []*FieldDecl
    Methods     []*MethodDecl
    Annotations []*AnnotationUse
}
```

The existing class, enum, extension, annotation, embed, and template structs
may be retained where already structured, but their string-valued executable
members MUST be replaced with nodes.

## 7. Types

Types MUST be represented structurally rather than as strings. The type model
must cover:

* named and qualified types;
* pointers, slices, arrays, maps, channels, and function types;
* interfaces and Go-compatible composite types;
* generic type applications and constraints;
* nullable/class references used by safe access;
* record types;
* enum types;
* `any` and `error`.

The printer may retain the original spelling as trivia, but semantic passes
must use the type nodes.

## 8. Statements

Function and method bodies are `*BlockStmt` nodes. Statements include at least:

```text
block, declaration, assignment, expression, return, if, for, range,
switch, select, defer, go, break, continue, goto, label, try, catch,
finally, throw, and embedded Go statements.
```

`try/catch/finally` is represented directly:

```go
type TryStmt struct {
    Body    *BlockStmt
    Catches []*CatchClause
    Finally *BlockStmt
}

type CatchClause struct {
    Types    []*Type
    Binding  string
    Body     *BlockStmt
}
```

There is no string-based exception rewrite in the final architecture.

The parser may use private draft state while assembling a statement header and
its clauses, but that state MUST NOT escape into `BlockStmt.Statements`.
Recognized statements are emitted as typed nodes. Syntax that is not yet
covered by a dedicated node is retained as a `TokenStmt` containing tokens,
expressions, and nested blocks; it must not contain a duplicate typed node or
an executable source string. Exception lowering consumes the typed `TryStmt`
and `CatchClause` nodes and constructs the generated runtime wrapper as Go
AST. If a transformed body cannot be parsed as Go for that lowering, the
compiler reports an error rather than falling back to string assembly.

## 9. Expressions

Expressions include identifiers, literals, selectors, calls, indexing,
composite construction, unary/binary operators, assignments, lambdas,
interpolation, records, safe access, coalescing, and named/default arguments.

Go++-specific operators MUST have explicit nodes or an explicit operator enum;
they MUST NOT be hidden in text substitutions. Examples include `?.`, `??`,
`||`, and `||=`.

## 10. Annotations

Annotations are AST nodes with structured argument expressions:

```go
type AnnotationUse struct {
    Name      *QualifiedName
    Arguments []*Expr
    Span      Span
}
```

Annotation placement is represented by the containing declaration or parameter,
not inferred later by searching raw source text.

## 11. Templates and Opaque Payloads

Template declarations have structured headers, parameters, layout references,
annotations, and a template-body node. The template body may contain an opaque
HTML/template payload, but template actions and embedded Go++ expressions MUST
be parsed into template AST nodes.

Raw strings, embedded bytes, and user template text remain opaque payloads by
language definition. Their contents must not be treated as executable Go++
source by ordinary compiler transformations.

## 12. Parsing Pipeline

The compiler pipeline becomes:

```text
source bytes
  -> lossless lexer
  -> Go++ parser
  -> source AST
  -> name/type resolution
  -> typed AST
  -> Go++ lowering IR
  -> Go AST / generated Go
  -> go/format
```

The parser must report syntax errors before semantic resolution. Semantic
passes must never need to reparse arbitrary source strings to discover nodes.

## 13. Lowering

Lowering is explicit and one-way. Each Go++ node has a lowering operation that
produces Go AST or a small internal IR. Lowering owns:

* overload selection;
* constructors and named arguments;
* extension dispatch;
* inheritance and polymorphic dispatch;
* exception propagation and catch dispatch;
* interpolation and Go++ operators;
* records, enums, annotations, serialization, and templates.

Lowering MUST preserve source spans so generated diagnostics can map back to
Go++ nodes.

## 14. Comments and Formatting

The formatter consumes the source AST and token/trivia stream. It MUST be able
to print any valid AST without consulting generated Go. Go-compatible fragments
may be printed with `go/format` only after they have been isolated from Go++
syntax and their comments have been retained.

Formatting MUST be deterministic, idempotent, comment-preserving, and safe for
opaque payloads.

## 15. Diagnostics and LSP

Every syntax and semantic diagnostic points to a node or token span. LSP
completion, hover, definition, references, rename, symbols, and formatting use
the same source AST and token stream as the compiler.

No LSP feature may infer declarations by regular-expression scanning generated
Go or raw executable strings.

## 16. Compatibility

The migration must preserve valid existing Go++ and Go source behavior. The
public compiler APIs may remain source-compatible while internal declaration
types gain structured fields.

During migration, a mixed compatibility field may exist only when accompanied
by parsed tokens/nodes and only for lossless source reproduction. New compiler
features MUST NOT add another raw-string transformation pass. `MixedDecl` is a
last-resort container for source that still combines syntax families; it is
not the normal representation for Go declarations, Go++ functions, value
declarations, or annotated declarations.

## 17. Migration Plan

The implementation proceeds in dependency order:

1. Add lossless tokens, spans, comments, and line maps.
2. Populate tokens for every current declaration and method body.
3. Parse Go-compatible types and expressions into AST nodes.
4. Parse blocks and ordinary statements.
5. Parse Go++ statements and expressions, including exceptions and lambdas.
6. Replace raw method/function bodies with `BlockStmt`.
7. Keep structured lambda spans and callable metadata in AST form through
   resolution; move remaining transformations from source rewriting to typed
   AST lowering.
8. Move semantic resolution and LSP features to AST traversal.
9. Replace the conservative formatter with the AST printer.
10. Remove `MixedDecl`, `Method.Body`, string type fields, and reparsing
    helpers once all consumers have migrated.

## 18. Acceptance Criteria

The full AST migration is complete only when:

* no executable declaration or body is represented solely as a string;
* no compiler transformation reparses arbitrary executable source text;
* every valid Go++ construct has a source AST node;
* comments and source spans survive every compiler phase;
* `gpp fmt` can format expressions and statements without generated Go;
* `gpp lsp` and compiler diagnostics use the same AST;
* the existing compiler, examples, and regression suite pass unchanged.
