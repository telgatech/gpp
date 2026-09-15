Go++ Compatibility Spec: Go as a Source Superset

Goal
Make ordinary valid Go source valid Go++ source with the same meaning whenever
the source does not intentionally use a Go++ extension.

Current status

This is a planned compatibility track, not a completed guarantee. Go++ currently
uses a handwritten extension parser and passes most ordinary Go declarations
through as raw source. The compiler does not yet run a complete Go conformance
suite.

Known compatibility hazards

- The top-level extension scanner recognizes `class` and `package` at line
  starts without tracking parenthesis depth. A valid Go identifier named
  `class` inside a top-level `const (...)` or `var (...)` group can therefore be
  mistaken for an extension.
- Package declarations are currently validated line-by-line and may reject
  otherwise valid trailing comments or explicit semicolons.
- String interpolation is triggered by `{{...}}` inside ordinary interpreted
  string literals. A valid Go string containing that sequence can therefore be
  rewritten as a Go++ interpolation.
- Extension transforms use lightweight lexical and AST analysis rather than
  the full Go type checker. Unusual but valid Go expressions may be preserved
  syntactically while still being outside the compiler's semantic guarantees.

Compatibility rules to preserve

1. Go imports, declarations, statements, types, generics, build-tag comments,
   and standard-library usage remain available unchanged.
2. Go++ extensions must be opt-in by syntax where practical. Ordinary `.`
   access, ordinary strings, and ordinary function calls must retain Go meaning.
3. Generated output must remain valid ordinary Go and must not require a Go++
   runtime for pass-through code.
4. A compatibility fix must include a regression test containing the original
   Go source and a generated-Go build check.

Planned implementation

1. Replace top-level extension scanning with Go-token-aware scanning that tracks
   parentheses, brackets, braces, comments, and strings.
2. Parse package clauses with the Go parser while preserving Go comments and
   semicolon forms.
3. Make interpolation unambiguously opt-in or distinguish it from ordinary Go
   strings before rewriting.
4. Add a corpus of valid Go fixtures covering declarations, generics, grouped
   declarations, labels, comments, raw/interpreted strings, directives, and
   unusual formatting.
5. Compile every fixture both as Go and as Go++, compare the generated program's
   build result, and add semantic output checks where transforms are involved.

Acceptance criteria

- Every fixture accepted by the selected Go toolchain is accepted by Go++.
- Generated Go builds with the same module and build constraints.
- Ordinary Go fixtures do not gain interpolation, constructor, overload,
  polymorphism, safe-access, or other Go++ rewrites unless they use the
  corresponding extension syntax.
- New Go language releases add compatibility fixtures before being declared
  supported by the project.

Out of scope for the initial compatibility pass

- Making Go++ source compatible with every future Go release automatically.
- Structural compatibility between Go++ records/classes and arbitrary native
  Go types.
- Changing intentionally different Go++ syntax such as `class`, compact
  construction, named/default arguments, `record(...)`, or `?.`.
