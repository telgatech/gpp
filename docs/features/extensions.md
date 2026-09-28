# Extension methods

Extension methods let a package add method-style operations to an existing Go
or Go++ type. They are useful when a type is owned by another package or when
you want a local, domain-focused helper without wrapping or subclassing the
original type.

## Compare a helper with an extension method

A package-level helper works in Go and remains a good choice. An extension
lets the same operation read like a method on the value:

::: code-group
```go [Go++]
extend string {
    func IsBlank() bool {
        return this.TrimSpace() == ""
    }
}

if userInput.IsBlank() { /* ... */ }
```

```go [Go]
func IsBlank(value string) bool {
    return strings.TrimSpace(value) == ""
}

if IsBlank(userInput) { /* ... */ }
```
:::

## Extend an existing type

An extension block names the target type. Inside a method, `this` refers to
the receiver:

```go
extend string {
    func IsBlank() bool {
        return this.TrimSpace() == ""
    }
}

if userInput.IsBlank() {
    fmt.Println("a value is required")
}
```

The same approach works with imported Go types and Go++ classes. An extension
can also target several compatible types when the same operation applies to
each:

```go
extend string, []byte {
    func Size() int {
        return len(this)
    }
}
```

## Compile-time method syntax

The compiler resolves extension calls while compiling and emits ordinary Go
functions. The original type is not modified, and no runtime registration or
monkey-patching is involved. This keeps the extension visible to the package
that imports it and preserves the original library's behavior.

Real methods on a type keep their normal precedence. Use an extension for
convenience operations, adapters, and project-specific vocabulary; define a
real method on a type you own when the behavior is part of that type's public
contract.

Read the [extension method specification](/reference/specifications/extension)
for generic targets, receiver behavior, and resolution rules.
