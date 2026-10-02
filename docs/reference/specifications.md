# Specifications

The specifications are the source of truth for Go++ language and standard
library behavior. They are synchronized from the repository’s `spec/` folder
before each docs development or production build.

## Compiler and language

- [Base language](./specifications/base)
- [Program exit hook](./specifications/at-exit)
- [Compatibility](./specifications/compat)
- [Full compiler AST](./specifications/compiler.ast)
- [Compiler diagnostics](./specifications/compiler-diagnostics)
- [Language server](./specifications/compiler.lsp)
- [Formatting](./specifications/fmt)
- [Type resolver](./specifications/type-resolver)

## Core features

- [Annotations](./specifications/annotations)
- [Annotation inheritance](./specifications/annotation-inheritance)
- [Object model](./specifications/base)
- [Enums](./specifications/enums)
- [Exceptions](./specifications/exceptions)
- [Expression catch](./specifications/expr-catch)
- [Lambdas](./specifications/lambdas)
- [Records](./specifications/records)
- [Serialization](./specifications/serialization)
- [String interpolation](./specifications/string-interpolation)

## Application libraries

- [HTTP](./specifications/std.http)
- [HTTP lifecycle](./specifications/std.http.lifecycle)
- [OAuth](./specifications/http.oauth)
- [ORM](./specifications/std.orm)
- [OpenAPI and Swagger](./specifications/openapi-swagger)
- [Swagger UI](./specifications/swagger-ui)
- [Templates](./specifications/tpl)
- [HTTP templates](./specifications/tpl.http)
- [Template watch mode](./specifications/tpl.watch)
- [Ad-hoc templates](./specifications/tpl.adhoc)
- [Standard extensions](./specifications/std.extensions)
- [Regex extensions](./specifications/std.extensions.regex)

## Build and integration

- [Embedding](./specifications/embed)
- [Modules](./specifications/modules)
- [Packaging](./specifications/packaging)
- [Testing](./specifications/testing)
- [Static methods](./specifications/static-methods)
