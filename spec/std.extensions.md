# GPP Standard Type Ergonomic Extensions Specification

## 1. Overview

GPP provides a small set of ergonomic extension methods for commonly used standard Go types.

The goal is to improve everyday readability without introducing new runtime types or duplicating large parts of the standard library.

Examples:

```gpp
name.TrimSpace().ToLower()

users.Filter(u => u.Active)

err.Is(sql.ErrNoRows)

body := resp.Body.ReadAll()
```

These are ordinary GPP extension methods over existing Go types.

They are not compiler intrinsics unless an implementation requirement makes that unavoidable.

---

# 2. Design Principle

The inclusion rule is:

> Add an extension when it represents a common operation naturally associated with the receiver and mainly improves the ergonomics of an existing Go standard-library operation.

Good example:

```gpp
name.TrimSpace()
```

instead of:

```go
strings.TrimSpace(name)
```

Good example:

```gpp
err.Is(target)
```

instead of:

```go
errors.Is(err, target)
```

Avoid adding convenience merely because an operation can technically be expressed as a method.

The prelude should remain small, predictable, and Go-like.

---

# 3. Implementation Model

The preferred implementation uses normal GPP extension declarations.

Conceptually:

```gpp
extend string {
    func TrimSpace() string {
        return strings.TrimSpace(this)
    }
}
```

and:

```gpp
extend error {
    func Is(target error) bool {
        return errors.Is(this, target)
    }
}
```

These extensions should compile to ordinary Go functions using the normal GPP extension-method lowering rules.

No wrapper types are introduced.

---

# 4. Native Go Types Remain Native

A string remains:

```go
string
```

A slice remains:

```go
[]T
```

A map remains:

```go
map[K]V
```

An error remains:

```go
error
```

The extensions are compile-time method-call sugar only.

For example:

```gpp
name.TrimSpace()
```

may lower conceptually to:

```go
strings.TrimSpace(name)
```

or to a generated extension helper that calls it.

---

# 5. Prelude Integration

The core ergonomic extensions should be available automatically through the GPP prelude.

Users should not need to write:

```gpp
import "gpp/strings"
```

for basic operations such as:

```gpp
s.TrimSpace()
s.Contains("foo")
```

The prelude remains compiler-shipped GPP source.

A future:

```bash
gpp build --no-prelude
```

mode may disable these automatic conveniences.

---

# 6. String Extensions

The highest-value extensions are direct projections of the standard `strings` package.

Recommended v1 set:

```gpp
extend string {
    func Reverse() string
    func Trim(cutset string) string
    func TrimLeft(cutset string) string
    func TrimRight(cutset string) string
    func TrimPrefix(prefix string) string
    func TrimSuffix(suffix string) string
    func TrimSpace() string
    func TrimFunc(predicate func(rune) bool) string
    func TrimLeftFunc(predicate func(rune) bool) string
    func TrimRightFunc(predicate func(rune) bool) string

    func ToLower() string
    func ToUpper() string
    func ToTitle() string
    func ToValidUTF8(replacement string) string

    func Contains(substr string) bool
    func ContainsAny(characters string) bool
    func ContainsRune(character rune) bool
    func ContainsFunc(predicate func(rune) bool) bool
    func HasPrefix(prefix string) bool
    func HasSuffix(suffix string) bool

    func Replace(old string, new string, n int) string
    func ReplaceAll(old string, new string) string
    func Cut(separator string) (string, string, bool)
    func CutLast(separator string) (string, string, bool)
    func CutPrefix(prefix string) (string, bool)
    func CutSuffix(suffix string) (string, bool)

    func Split(sep string) []string
    func SplitN(separator string, count int) []string
    func SplitAfter(separator string) []string
    func SplitAfterN(separator string, count int) []string
    func Fields() []string

    func Index(substr string) int
    func LastIndex(substr string) int
    func IndexAny(characters string) int
    func LastIndexAny(characters string) int
    func IndexByte(character byte) int
    func LastIndexByte(character byte) int
    func IndexRune(character rune) int
    func IndexFunc(predicate func(rune) bool) int
    func LastIndexFunc(predicate func(rune) bool) int

    func Count(substr string) int
    func Repeat(count int) string
    func Map(mapping func(rune) rune) string

    func EqualFold(other string) bool
}

extend []string {
    func Join(separator string) string
}
```

These should preserve the semantics of their corresponding functions in Go's `strings` package.
`Reverse()` is an addition beyond `strings`; it reverses Unicode code points,
not user-perceived grapheme clusters.

---

# 7. String Convenience Extensions

GPP may additionally provide a few small conveniences that are not direct one-to-one wrappers.

Recommended:

```gpp
extend string {
    func Empty() bool
    func Blank() bool
    func Lines() []string
    func Words() []string
}
```

Semantics:

```gpp
"".Empty()          // true
" ".Empty()         // false

"".Blank()          // true
"   ".Blank()       // true
"\n\t".Blank()      // true
"hello".Blank()     // false
```

Canonical behavior:

```text
Empty
    len(s) == 0

Blank
    TrimSpace(s) == ""
```

`Words()` may be equivalent to `strings.Fields`.

`Lines()` should split text into logical lines using a documented newline policy.

String parsing and scalar formatting are also available through the prelude:

```gpp
extend string {
    func ToInt(fallback int = 0) int
    func ToInt64(fallback int64 = 0) int64
    func ToUint64(fallback uint64 = 0) uint64
    func ToFloat32(fallback float32 = 0) float32
    func ToFloat64(fallback float64 = 0) float64
    func ToBool(fallback bool = false) bool
}

extend int { func String() string }
extend float64 { func String() string }
extend bool { func String() string }
```

The parsing methods return the fallback on invalid input; omitted fallbacks use
the destination type's zero value. Integer parsing uses base 10. `String()` on
integer and unsigned integer values uses base 10, while floating-point values
use Go's shortest round-trippable representation.

---

# 8. Fluent String Example

Instead of:

```go
name := strings.TrimSpace(strings.ToLower(input))
```

GPP may use:

```gpp
name := input.ToLower().TrimSpace()
```

This is one of the primary motivations for standard-type extensions.

---

# 9. Slice Extensions

Generic slice extensions should provide common collection operations.

Recommended v1 set:

```gpp
extend []T {
    func Each(fn func(T) error) error

    func Any(fn func(T) bool) bool
    func All(fn func(T) bool) bool

    func Find(fn func(T) bool) T
    func Filter(fn func(T) bool) []T

    func Contains(value T) bool
    func Index(value T) int

    func First() T
    func Last() T

    func Reverse() []T

    func IsEmpty() bool
    func NotEmpty() bool

    func Take(n int) []T
    func Drop(n int) []T
}
```

Additional operations may require constraints or comparator functions.

---

# 10. Slice `Any`

```gpp
users.Any(u => u.Active)
```

returns `true` if at least one element satisfies the predicate.

Equivalent conceptual behavior:

```go
for _, v := range users {
    if fn(v) {
        return true
    }
}
return false
```

---

# 11. Slice `All`

```gpp
users.All(u => u.Active)
```

returns `true` if every element satisfies the predicate.

For an empty slice, `All` should return `true`, following ordinary universal-quantification semantics.

---

# 12. Slice `Find`

```gpp
user := users.Find(u => u.ID == id)
```

returns the first matching value.

The exact no-match behavior must remain consistent with GPP's nil/zero-value philosophy.

For v1, `Find` should return the element type's zero value if no match exists.

If later dogfooding shows this is too ambiguous, a separate richer API may be introduced.

---

# 13. Slice `Filter`

```gpp
active := users.Filter(u => u.Active)
```

returns a new slice containing matching elements in original order.

The original slice is not mutated.

---

# 14. Slice `Contains`

For comparable element types:

```gpp
users.Contains(user)
```

returns whether an equal element exists.

`Contains` should only be available when the element type supports equality.

Invalid uses should be compile-time errors.

---

# 15. Slice `Index`

For comparable element types:

```gpp
i := users.Index(user)
```

returns the first matching index.

If not found:

```text
-1
```

matching common Go/string indexing conventions.

---

# 16. Slice `First` and `Last`

```gpp
users.First()
users.Last()
```

return the first and last elements respectively.

For an empty slice, they return the zero value of `T`.

No exception is thrown merely because the slice is empty.

This follows GPP's general preference for non-exceptional nil/zero handling.

---

# 17. Slice `Reverse`

```gpp
reversed := users.Reverse()
```

returns a reversed slice.

For v1, `Reverse` should return a new slice rather than mutating the receiver.

Mutation-specific variants should not be added unless needed.

---

# 18. Slice `IsEmpty`

```gpp
users.IsEmpty()
```

is equivalent to:

```gpp
len(users) == 0
```

Likewise:

```gpp
users.NotEmpty()
```

is equivalent to:

```gpp
len(users) != 0
```

---

# 19. Slice `Take`

```gpp
users.Take(10)
```

returns at most the first ten values.

Recommended boundary semantics:

```text
n <= 0
    empty slice

n >= len(slice)
    complete slice contents
```

The result should not panic because `n` exceeds length.

---

# 20. Slice `Drop`

```gpp
users.Drop(10)
```

returns all elements after the first ten.

Recommended semantics:

```text
n <= 0
    complete slice

n >= len(slice)
    empty slice
```

---

# 21. Slice `Map`

Mapping is valuable enough to include even though the return element type differs.

Conceptual API:

```gpp
users.Map(u => u.Name)
```

produces:

```gpp
[]string
```

The compiler already has generic/lambda support, so this should be expressed through ordinary generic library functionality if possible.

Conceptually:

```gpp
func Map[T, R](
    values []T,
    fn func(T) R,
) []R
```

made available ergonomically as:

```gpp
values.Map(fn)
```

---

# 22. Sorting Extensions

Sorting should remain relatively small.

Recommended:

```gpp
users.Sort((a, b) => a.Name < b.Name)

users.SortDesc((a, b) => a.Name < b.Name)
```

The exact mutating versus non-mutating semantics must be explicit.

Recommended v1:

```text
Sort
SortDesc
    return sorted copies
```

to avoid surprising mutation.

If in-place sorting becomes important later, separate names may be introduced.

---

# 23. `Min` and `Max`

For ordered values:

```gpp
numbers.Min()
numbers.Max()
```

For objects, selector/comparator forms may be supported:

```gpp
users.Min(u => u.Age)
users.Max(u => u.Age)
```

These are useful but require generic ordering constraints.

They may be implemented after the core slice extensions if compiler support is not yet sufficient.

---

# 24. `Unique`

For comparable values:

```gpp
ids.Unique()
```

returns unique values while preserving first-seen order.

Potential keyed form:

```gpp
users.UniqueBy(u => u.ID)
```

may also be provided.

`UniqueBy` is useful but not required for the smallest v1.

---

# 25. Map Extensions

Recommended map extensions:

```gpp
extend map[K]V {
    func Each(fn func(K, V))
    func Each(fn func(K, V) error) error

    func Has(key K) bool
    func Delete(key K) bool
    func Get(key K) (V, bool)
    func Get(key K, fallback V) V

    func Keys() []K
    func Values() []V

    func IsEmpty() bool
    func NotEmpty() bool

    func Clone() map[K]V
}
```

`Each` visits each key/value pair in unspecified order. Its error-returning
overload stops at the first callback error and returns it.

---

# 26. Map `Has`

```gpp
settings.Has("theme")
```

is equivalent to:

```go
_, ok := settings["theme"]
```

and returns the boolean result.

---

# 27. Map `Get`

```gpp
theme, found := settings.Get("theme")
fallbackTheme := settings.Get("theme", "light")
```

With one argument after the receiver, `Get` returns the value and whether the
key exists, like Go's two-result map lookup. With a fallback argument, it
returns the value when present and the fallback otherwise:

```gpp
value, found := settings.Get("theme")
theme := settings.Get("theme", "light")
```

The boolean reports key presence, even when the stored value is the zero value.

Thus if a key exists with:

```text
""
0
false
nil
```

that existing value is still returned.

---

# 28. Map `Delete`

```gpp
removed := settings.Delete("theme")
```

Deletes the key if it exists and returns whether it was present. The map is
modified in place; deleting a missing key returns `false`.

---

# 29. Map `Keys`

```gpp
keys := m.Keys()
```

returns the map keys.

Go maps do not define iteration order, so `Keys()` does not promise stable ordering.

Users requiring ordering should explicitly sort the result.

---

# 30. Map `Values`

```gpp
values := m.Values()
```

returns values using normal Go map iteration semantics.

No ordering guarantee is provided.

---

# 31. Map `Clone`

```gpp
copy := m.Clone()
```

returns a shallow copy.

Nested pointers, slices, maps, classes, or reference-containing values are not recursively duplicated.

---

# 32. Error Extensions

Go's `errors` package maps naturally onto extensions on `error`.

Recommended:

```gpp
extend error {
    func Is(target error) bool
    func Unwrap() error
}
```

and where generic lowering permits:

```gpp
func As[T]() T
```

or equivalent typed form.

---

# 33. `error.Is`

Instead of:

```go
errors.Is(err, sql.ErrNoRows)
```

GPP may write:

```gpp
err.Is(sql.ErrNoRows)
```

Semantics remain exactly those of:

```go
errors.Is
```

---

# 34. `error.Unwrap`

```gpp
cause := err.Unwrap()
```

is equivalent to:

```go
errors.Unwrap(err)
```

and returns `nil` where the error has no unwrap chain.

---

# 35. `error.As`

The desired ergonomic form is:

```gpp
target := err.As[*MyError]()
```

The implementation must preserve the semantics of `errors.As`.

If direct generic extension lowering is awkward in v1, this helper may be deferred rather than introducing compiler-specific behavior solely for it.

---

# 36. `io.Reader` Extensions

A very high-value extension is:

```gpp
extend io.Reader {
    func ReadAll() ([]byte, error)
}
```

Then:

```gpp
body := resp.Body.ReadAll()
```

replaces:

```go
body, err := io.ReadAll(resp.Body)
```

Because GPP automatically promotes omitted trailing errors:

```gpp
body := resp.Body.ReadAll()
```

naturally throws on a non-nil read error.

Explicit handling remains:

```gpp
body, err := resp.Body.ReadAll()
```

---

# 37. `io.Writer` Extensions

A possible useful extension:

```gpp
extend io.Writer {
    func WriteString(value string) (int, error)
}
```

allowing:

```gpp
w.WriteString("hello")
```

where the receiver otherwise only provides `Write([]byte)`.

This should map to standard Go behavior such as `io.WriteString`.

---

# 38. Numeric Extensions

Numeric extensions should remain deliberately restrained.

Potential v1 conveniences:

```gpp
n.Abs()
n.Min(other)
n.Max(other)
n.Clamp(min, max)
```

These should only be introduced if they can be implemented cleanly over the supported numeric constraints.

Do not grow primitive numeric types into large utility APIs.

---

# 39. Existing Native Methods Win

Normal GPP extension resolution rules apply.

If a native Go type already has a method with the requested name and compatible call:

```text
native method wins
```

before extension lookup.

Extensions must not shadow or alter native Go methods.

---

# 40. No Runtime Type Modification

Adding:

```gpp
s.TrimSpace()
```

does not actually add a method to Go's built-in `string` type.

It is compile-time lowering.

Similarly:

```gpp
err.Is(target)
```

does not modify the Go `error` interface.

This preserves full Go interoperability.

---

# 41. Extension Discovery

Prelude extensions should be discoverable by GPP tooling.

For example, autocomplete on:

```gpp
name.
```

should include:

```text
Trim
TrimSpace
ToLower
ToUpper
Contains
HasPrefix
HasSuffix
Split
...
```

alongside applicable native members.

Extension methods should clearly appear as extensions in tooling where useful.

---

# 42. Documentation

Each prelude extension should document the corresponding Go standard-library behavior where applicable.

For example:

```text
string.TrimSpace
    convenience extension over strings.TrimSpace

error.Is
    convenience extension over errors.Is

io.Reader.ReadAll
    convenience extension over io.ReadAll
```

This helps Go developers understand that semantics remain familiar.

---

# 43. Names Should Follow Go Vocabulary

Where an extension directly projects a Go standard-library function, preserve the Go function name.

Prefer:

```gpp
s.TrimSpace()
s.HasPrefix(...)
s.EqualFold(...)
```

not renamed alternatives such as:

```text
strip
startsWith
equalsIgnoreCase
```

The feature should feel like ergonomic Go, not a foreign standard library layered over Go.

---

# 44. Avoid Excessive Aliases

Do not provide multiple synonymous names such as:

```text
Empty
IsEmpty

StartsWith
HasPrefix

Lower
ToLower
```

Choose one canonical form.

Prefer existing Go vocabulary unless Go provides no suitable term.

---

# 45. Domain-Specific Extensions Stay Out of Prelude

The implicit prelude should not absorb domain-specific conveniences.

For example, HTTP-specific behavior belongs in:

```text
gpp/http
```

Database behavior belongs in:

```text
gpp/orm
```

Encoding behavior belongs in:

```text
gpp/encoding
```

Examples that should not automatically belong to the base prelude:

```gpp
request.JSON(...)
request.Query(...)

db.Find(...)

data.JSON(...)
```

unless later usage proves a more general abstraction belongs in core.

---

# 46. Path Helpers

Functions from:

```go
path/filepath
```

should not automatically become string methods merely because paths are represented as strings.

For example:

```gpp
path.Ext()
path.Base()
```

looks attractive but semantically treats every string as a filesystem path.

These should initially remain normal package functions:

```gpp
filepath.Ext(path)
filepath.Base(path)
```

or belong to a more explicitly typed/path-oriented helper layer later.

---

# 47. URL and Regex Helpers

Similarly, avoid extensions such as:

```gpp
s.ParseURL()
s.Regex()
```

in the implicit prelude.

They represent conversion into another domain rather than an operation intrinsic to a string.

Prefer ordinary constructors/functions:

```gpp
url.Parse(s)
regexp.Compile(s)
```

unless dogfooding demonstrates clear repeated friction.

---

# 48. Mutation Policy

Prelude extension methods should avoid hidden mutation where practical.

Recommended default:

```text
string operations
    immutable

Filter / Map / Reverse / Sort
    return new values

map Clone
    returns new map
```

Methods that mutate the receiver should have names/semantics that make mutation explicit.

This reduces surprises in fluent chains.

---

# 49. Nil Behavior

Extensions must follow normal GPP nil behavior.

For receiver types that may be nil:

```text
do not introduce arbitrary panics merely for convenience
```

However, native Go semantics and legitimate nil dereferences must not be silently hidden where that would change meaning.

Each extension should document meaningful nil behavior where applicable.

---

# 50. Error Behavior

Extensions wrapping functions returning trailing `error` should preserve that signature.

Example:

```gpp
io.Reader.ReadAll() ([]byte, error)
```

Then GPP automatic error promotion naturally applies.

Do not convert errors into sentinel zero values just to make an extension look simpler.

---

# 51. Performance

Prelude extensions should be thin wrappers.

They should not add significant allocations, reflection, or hidden runtime machinery beyond what the underlying Go operation requires.

For example:

```gpp
s.Contains("x")
```

should remain essentially equivalent to:

```go
strings.Contains(s, "x")
```

after optimization/inlining.

---

# 52. Initial Recommended Prelude Surface

The initial standard ergonomic extension set should focus on:

```text
string
    Trim
    TrimLeft
    TrimRight
    TrimSpace
    ToLower
    ToUpper
    Contains
    HasPrefix
    HasSuffix
    Replace
    ReplaceAll
    Split
    Fields
    Index
    LastIndex
    Count
    Repeat
    EqualFold
    Empty
    Blank
    Lines
    Words

[]T
    Any
    All
    Find
    Filter
    Map
    Contains
    Index
    First
    Last
    Reverse
    IsEmpty
    NotEmpty
    Take
    Drop

map[K]V
    Has
    Get
    Keys
    Values
    IsEmpty
    NotEmpty
    Clone

error
    Is
    Unwrap
    As[T] where practical

io.Reader
    ReadAll

io.Writer
    WriteString
```

Sorting, Min/Max, Unique, and UniqueBy may be added once the generic constraint implementation is ready.

---

# 53. Example

Without extensions:

```go
name := strings.TrimSpace(strings.ToLower(input))

if strings.HasPrefix(name, "admin") {
    ...
}

active := make([]User, 0)
for _, user := range users {
    if user.Active {
        active = append(active, user)
    }
}

body, err := io.ReadAll(resp.Body)
if err != nil {
    return err
}
```

With GPP:

```gpp
name := input.ToLower().TrimSpace()

if name.HasPrefix("admin") {
    ...
}

active := users.Filter(u => u.Active)

body := resp.Body.ReadAll()
```

The generated program still uses ordinary Go types and standard-library semantics.

---

# 54. Non-Goals

The v1 extension set should not attempt to provide:

* LINQ-scale collection APIs;
* Java/Kotlin-style mega standard classes;
* implicit filesystem semantics on all strings;
* implicit URL/regex parsing;
* deep cloning;
* hidden exception swallowing;
* lazy enumerable abstractions;
* stream APIs;
* a second collection hierarchy;
* wrapper classes around Go primitives;
* compiler magic for every convenience function.

---

# 55. Design Summary

The feature follows this model:

```text
existing Go type
    +
ordinary GPP extension declaration
    +
small compiler-shipped prelude
    =
more ergonomic GPP syntax
```

Examples:

```gpp
name.TrimSpace()

users.Any(u => u.Active)

settings.Get("theme", "light")

err.Is(sql.ErrNoRows)

body := resp.Body.ReadAll()
```

The core rule is:

> Improve call-site ergonomics while preserving Go's types, names, semantics, interoperability, and standard library as the underlying implementation.
