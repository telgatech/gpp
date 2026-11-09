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

## `Hook`

Use `std.Hook` for a method that a framework dispatches as a lifecycle
callback, by reflection, or through an internal interface, instead of
application code calling it as part of the normal API:

```gpp
import std "gpp/std"

class Model {
    func BeforeSave() error @{std.Hook} {
        return nil
    }
}
```

The language server omits annotated methods from completion suggestions,
including inherited member suggestions. A hook can be overridden, but ordinary
Go++ code can call it only from the class that first declares it; an override
does not transfer that permission to a child. This keeps lifecycle timing under
the declaring class's control. Framework dispatch through internal interfaces
or runtime hook lookup remains allowed, and normal method dispatch still selects
an override. Annotations belong to the declaration, so annotate an override as
well when it should also be hidden from completions.
