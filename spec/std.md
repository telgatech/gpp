# GPP Standard Package

Import `gpp/std` for annotations shared across standard-library and user
packages:

```gpp
import std "gpp/std"
```

## `Deprecated`

```gpp
annotation Deprecated(message string)
```

`Deprecated` is valid on any declaration that supports annotations. It marks
the symbol as deprecated in language-server completion, hover, and symbol
metadata. It does not currently cause the compiler to emit a warning when code
uses the symbol.
