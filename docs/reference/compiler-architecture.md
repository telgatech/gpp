# Compiler architecture

Go++ uses a dedicated frontend because its syntax extends Go. The compiler
parses Go++ constructs into typed AST nodes, transforms those nodes into Go
AST where possible, and emits ordinary Go for the final toolchain.

## The pipeline

```text
source .gpp
    ↓
lossless lexer and Go++ parser
    ↓
typed Go++ AST with source spans
    ↓
semantic analysis and feature lowering
    ↓
Go AST / generated Go declarations
    ↓
go build, go test, or go run
```

The AST is not just an implementation detail. It powers diagnostics,
formatting, LSP features, overload resolution, source mapping, and generated
documentation.

## Why not patch the Go compiler?

Go’s compiler internals are not a stable extension API, and its parser rejects
Go++ syntax before an extension could transform it. A dedicated frontend lets
Go++ evolve its syntax while preserving the important interoperability point:
the output is ordinary Go.

## Source fidelity

Executable syntax is represented structurally. Source text is retained only
where exact text is meaningful, such as comments, raw strings, templates,
embedded files, and diagnostics. This keeps transformations composable and
lets tooling report locations in the original Go++ source.

Read the [full AST specification](./specifications/compiler.ast) for the
invariants that guide the implementation.
