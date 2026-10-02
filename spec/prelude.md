# Go++ Feature Spec: Implicit prelude.gpp

## Goal

Provide a small, automatically available Go++ prelude containing universally useful extensions and helpers so ordinary programs can be productive immediately without repetitive imports.

The prelude must remain:

- small
- general-purpose
- predictable
- implemented in ordinary Go++ where possible
- layered on top of native Go types
- free of domain-specific frameworks

Do NOT put ORM, web routing, validation frameworks, database abstractions, or application-specific behavior in the prelude.

## 1. File name

The built-in prelude source is:

prelude.gpp

It ships with the Go++ compiler/distribution.

Users do not need to import it.

## 2. Loading behavior

Every Go++ package automatically has access to symbols and extension methods declared by prelude.gpp.

Conceptually:

package usercode

implicitly sees:

import/preload Go++ prelude

Do NOT textually concatenate prelude.gpp into source files.

Recommended implementation:

1. Parse/type-check prelude.gpp once.
2. Cache its semantic representation.
3. Make exported prelude symbols/extensions visible in every Go++ package.
4. Compile required generated Go alongside user code.

## 3. Go standard library remains separate

The prelude does NOT replace Go's standard library.

Users still write:

import (
    "fmt"
    "net/http"
    "database/sql"
)

when they need those packages.

The model is:

Go stdlib
    native Go packages

Go++ prelude
    implicit convenience helpers/extensions

Optional Go++ libraries
    explicitly imported packages such as model/web/validate

## 4. Design rule

Only include functionality that is broadly useful across nearly all application domains.

Good candidates:

- slice helpers
- map helpers
- string helpers
- small collection algorithms
- sorting conveniences
- min/max/clamp where useful
- generic utility helpers

Bad candidates:

- ORM Model
- database CRUD
- HTTP routing
- authentication
- form validation rules
- JSON framework wrappers
- logging framework
- configuration framework
- dependency injection

Those should live in separate libraries.

## 5. Native Go types only

Do not invent replacement collection types.

Use normal Go:

[]T
map[K]V
string

Enhance them with extension methods.

Example:

users.Sort(...)
items.Any(...)
name.Empty()

The underlying values remain ordinary Go values.

## 6. Initial slice extension set

Provide a useful baseline for slices.

Conceptual API:

extend []T {
    func Each(fn func(T) error) error

    func Any(fn func(T) bool) bool
    func All(fn func(T) bool) bool

    func Find(fn func(T) bool) (T, bool)

    func Filter(fn func(T) bool) []T

    func Contains(value T) bool
        where T is comparable

    func Index(value T) int
        where T is comparable

    func Reverse()

    func Sort(less func(T, T) bool)
}

`Each` visits values in order, returns immediately when its callback returns an error, and otherwise returns `nil` after the final value. The caller can return or otherwise handle that error.

Exact generic constraint syntax should follow whatever Go++ currently supports.

If generic extension constraints are not yet implemented, split implementations using available Go generic mechanisms or defer constrained methods.

## 7. Any

Example:

if users.Any(func(u User) bool {
    return u.Active
}) {
    ...
}

Semantics:

return true if predicate returns true for at least one element.

Short-circuit on first match.

Equivalent conceptual implementation:

for _, v := range this {
    if fn(v) {
        return true
    }
}

return false

## 8. All

Example:

if users.All(func(u User) bool {
    return u.Active
}) {
    ...
}

Return true only if predicate is true for all elements.

Empty slice returns true.

Short-circuit on first false.

## 9. Find

Example:

user, ok := users.Find(func(u User) bool {
    return u.Id == id
})

Recommended return:

(T, bool)

rather than pointer-to-element.

Reason:

- works for all element types
- mirrors common Go idiom
- avoids pointer/address lifetime complications
- preserves ordinary value semantics

First matching element wins.

If absent:

zero(T), false

## 10. Filter

Example:

active := users.Filter(func(u User) bool {
    return u.Active
})

Return a new []T containing matching elements in original order.

Do not mutate original slice.

## 11. Contains

For comparable T:

if names.Contains("Bob") {
    ...
}

Use ordinary equality semantics.

Could delegate to slices.Contains where available.

## 12. Index

For comparable T:

i := names.Index("Bob")

Return:

index >= 0 if found
-1 otherwise

Could delegate to slices.Index where available.

## 13. Reverse

Example:

users.Reverse()

Recommended semantics:

mutates the slice in place.

Reason:

this maps naturally to slices.Reverse.

If a copying variant is desired later, add:

Reversed()

Do not make Reverse silently allocate.

## 14. Sort with comparator

Example:

users.Sort(func(a, b User) bool {
    return a.Name < b.Name
})

Semantics:

sort slice in place.

Comparator returns true when a should sort before b.

Lower using slices.SortFunc or sort.Slice depending on generated Go/version.

If using slices.SortFunc, adapt bool comparator to cmp-style result as needed.

## 15. Ordered slice helpers

For ordered element types, provide:

numbers.Sort()
numbers.SortDesc()
numbers.Min()
numbers.Max()

Examples:

names.Sort()

scores.SortDesc()

smallest := scores.Min()
largest := scores.Max()

Use Go's cmp.Ordered/slices helpers where practical.

If overloading allows:

Sort()

and:

Sort(func(T,T) bool)

both may coexist naturally.

## 16. SortDesc

For ordered T:

values.SortDesc()

Mutates slice in place.

Equivalent to descending natural ordering.

## 17. Min / Max

For ordered T:

min := values.Min()
max := values.Max()

Need a defined empty-slice behavior.

Recommended:

panic/exception on empty input is undesirable.

Prefer:

func Min() (T, bool)
func Max() (T, bool)

Example:

min, ok := values.Min()

This is more Go-like and avoids hidden exceptional behavior.

## 18. String helpers

Provide only genuinely useful tiny helpers.

Initial candidates:

extend string {
    func Empty() bool
    func Blank() bool
}

`Empty`:

return len(this) == 0

`Blank`:

true if string is empty or contains only Unicode whitespace.

Use strings.TrimSpace or equivalent.

Example:

if name.Blank() {
    ...
}

Avoid adding dozens of Rails-style string methods initially.

## 19. Map helpers

Useful map helpers may include:

extend map[K]V {
    func Keys() []K
    func Values() []V
    func Has(key K) bool
}

Examples:

keys := users.Keys()
values := users.Values()

if users.Has(id) {
    ...
}

`Has` is convenience over:

_, ok := m[key]

Keys/Values ordering remains unspecified, matching Go map iteration semantics.

## 20. GetOr

Potentially useful map helper:

extend map[K]V {
    func GetOr(key K, fallback V) V
}

Example:

port := config.GetOr("port", "8080")

Must return fallback only if key is absent.

Do not confuse absent with zero-valued stored values.

Implementation:

v, ok := this[key]
if !ok {
    return fallback
}
return v

## 21. Generic numeric helpers

Potentially include:

Min(a, b)
Max(a, b)
Clamp(value, min, max)

only if they are not already ergonomically available from Go packages.

Because the prelude should remain small, these may be deferred.

## 22. No implicit `it`

Do NOT require implicit `it` or lambda shorthand for prelude APIs.

Use normal functions initially:

users.Filter(func(u User) bool {
    return u.Active
})

If Go++ later gains concise function syntax, these APIs automatically become nicer.

Do not make the prelude depend on speculative language features.

## 23. Implementation language

Prefer implementing prelude.gpp in Go++ itself.

Example:

extend []T {
    func Any(fn func(T) bool) bool {
        for _, v := range this {
            if fn(v) {
                return true
            }
        }
        return false
    }
}

This serves two purposes:

1. useful standard conveniences
2. a real-world stress test/demo of Go++ features

Use native Go interop where useful.

## 24. Prelude dependencies

prelude.gpp may import selected Go stdlib packages internally, such as:

"slices"
"strings"
"cmp"

These dependencies should be compiler-managed.

Users should not need to import those packages merely because the prelude implementation uses them.

## 25. Name collision rules

Real/native methods always beat prelude extensions according to normal extension resolution.

If user code defines a conflicting extension with equal precedence, follow normal extension ambiguity rules.

Do not silently let the prelude override user-defined behavior.

Recommended precedence:

1. real/native methods
2. Go++ class methods
3. explicitly imported/user extension methods
4. prelude extension methods

This gives user code an opportunity to supply a more specific extension without being trapped by the prelude.

If current extension resolution does not distinguish explicit vs prelude extensions, add origin metadata.

## 26. Shadowing ordinary functions

Prelude package-level functions should enter normal symbol resolution carefully.

Avoid generic names likely to collide heavily.

Prefer extension methods where possible.

Example:

values.Sort()

is better than globally injecting:

Sort(values)

The prelude should not pollute package namespaces unnecessarily.

## 27. Disable option

Provide a compiler option for testing/minimal builds:

gpp build --no-prelude

gpp run --no-prelude

When disabled:

- no prelude symbols
- no prelude extensions
- normal Go++ language remains available

This is useful for:

- compiler tests
- bootstrapping
- diagnosing conflicts
- minimal generated output

## 28. Versioning

The prelude is versioned with the Go++ compiler.

Do not fetch a remote prelude during compilation.

Compiler distribution contains the canonical prelude.gpp.

Programs therefore get deterministic prelude behavior for a given Go++ compiler version.

## 29. Source availability

Ship prelude.gpp as readable source.

Users should be able to inspect it.

It should demonstrate idiomatic Go++.

Avoid hiding most of the implementation in compiler intrinsics unless required for correctness/performance.

## 30. Intrinsics

Some operations may require compiler support, but keep intrinsics minimal.

For example:

- language operators
- reflection descriptors
- atomic ++/--
- class runtime machinery

should remain compiler features.

Collection algorithms should not become compiler intrinsics merely for convenience.

## 31. Suggested initial prelude.gpp

Conceptually:

package prelude

import (
    "slices"
    "strings"
)

extend []T {
    func Any(fn func(T) bool) bool {
        for _, v := range this {
            if fn(v) {
                return true
            }
        }
        return false
    }

    func All(fn func(T) bool) bool {
        for _, v := range this {
            if !fn(v) {
                return false
            }
        }
        return true
    }

    func Find(fn func(T) bool) (T, bool) {
        for _, v := range this {
            if fn(v) {
                return v, true
            }
        }

        var zero T
        return zero, false
    }

    func Filter(fn func(T) bool) []T {
        out := []T{}

        for _, v := range this {
            if fn(v) {
                out = append(out, v)
            }
        }

        return out
    }

    func Reverse() {
        slices.Reverse(this)
    }

    func Sort(less func(T, T) bool) {
        slices.SortFunc(this, func(a, b T) int {
            if less(a, b) {
                return -1
            }
            if less(b, a) {
                return 1
            }
            return 0
        })
    }
}

extend string {
    func Empty() bool {
        return len(this) == 0
    }

    func Blank() bool {
        return len(strings.TrimSpace(this)) == 0
    }
}

The exact syntax may need adjustment to current generic-extension support.

## 32. Additional ordered extensions

Where constraints are supported:

extend []T where T cmp.Ordered {
    func Sort() {
        slices.Sort(this)
    }

    func SortDesc() {
        slices.Sort(this)
        slices.Reverse(this)
    }

    func Min() (T, bool) {
        if len(this) == 0 {
            var zero T
            return zero, false
        }

        return slices.Min(this), true
    }

    func Max() (T, bool) {
        if len(this) == 0 {
            var zero T
            return zero, false
        }

        return slices.Max(this), true
    }
}

If Go++ does not yet support this constraint syntax, implement later rather than adding ad-hoc compiler behavior.

## 33. Suggested v1 contents

Ship v1 with approximately:

Slices:
    Any
    All
    Find
    Filter
    Contains
    Index
    Reverse
    Sort(comparator)

Ordered slices:
    Sort()
    SortDesc()
    Min()
    Max()

Strings:
    Empty
    Blank

Maps:
    Keys
    Values
    Has
    GetOr

Keep v1 intentionally small.

## 34. Features NOT in v1 prelude

Do not include:

Reduce
GroupBy
Chunk
Zip
DistinctBy
ParallelMap
Retry
Memoize
ORM helpers
HTTP helpers
validation rules

These may live in optional libraries if demand appears.

The prelude should not turn into a kitchen-sink utility framework.

## 35. Tests

Implicit availability:

package main

func main() {
    xs := []int{1,2,3}

    assert(xs.Any(func(x int) bool {
        return x == 2
    }))
}

must compile without importing prelude.

Find:

v, ok := xs.Find(...)

must return first match.

Filter:

must preserve order.

Reverse:

must mutate in place.

Sort:

must mutate in place and honor comparator.

Empty:

"".Empty() == true
"x".Empty() == false

Blank:

"   ".Blank() == true
"\n\t".Blank() == true
"x".Blank() == false

Maps:

m.Has(k)
m.Keys()
m.Values()
m.GetOr(k, fallback)

must behave according to normal Go map semantics.

No-prelude:

gpp build --no-prelude

must make prelude extension calls unresolved.

## 36. Design principle

prelude.gpp should make Go++ pleasant immediately without creating a second standard library.

Use native Go types.
Use native Go packages underneath.
Add only small universal conveniences.

If a feature can be implemented as ordinary Go++ extension code, prefer that over compiler magic.

The ideal experience is:

users.Sort(...)
users.Filter(...)
users.Any(...)
name.Blank()
config.GetOr(...)

with zero imports and zero loss of Go interoperability.
