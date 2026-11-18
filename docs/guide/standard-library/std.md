# `gpp/std`

`gpp/std` provides annotations shared by GPP packages.

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

## `Hook`

Use `std.Hook` for a method that a framework dispatches as a lifecycle
callback, by reflection, or through an internal interface, instead of
application code calling it as part of the normal API:

```gpp
import std "gpp/std"

class Model {
    func _beforeSave() error @{std.Hook} {
        return nil
    }
}
```

The language server omits annotated methods from completion suggestions,
including inherited member suggestions. The leading underscore makes this hook
protected, so the declaring class and subclasses can override it while ordinary
callers cannot invoke it. `std.Hook` marks it for framework dispatch and hides
it from completions; it does not change access by itself. Annotations belong to
the declaration, so annotate an override as well when it should also be hidden
from completions.
