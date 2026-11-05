# `gpp/std`

`gpp/std` provides annotations shared by Go++ packages.

## `Deprecated`

Use `std.Deprecated` to mark a declaration as deprecated in editor metadata:

```gpp
import std "gpp/std"

func OldAPI() @{std.Deprecated("Use NewAPI instead")} {
    // Compatibility implementation.
}
```

The annotation can be used on any declaration that supports annotations. The
language server marks deprecated symbols in completion, hover, and symbol
views. The compiler does not currently emit warnings for deprecated uses.
