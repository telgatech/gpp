# Go++ Standard Type Ergonomic Extensions Specification

## 1. Overview

Go++ provides a small set of ergonomic extension methods for commonly used standard Go types.

The goal is to improve everyday readability without introducing new runtime types or duplicating large parts of the standard library.

Examples:

```gpp
name.TrimSpace().ToLower()

users.Filter(u => u.Active)

err.Is(sql.ErrNoRows)

body := resp.Body.ReadAll()
```

These are ordinary Go++ extension methods over existing Go types.

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

The preferred implementation uses normal Go++ extension declarations.

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

These extensions should compile to ordinary Go functions using the normal Go++ extension-method lowering rules.

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

The core ergonomic extensions should be available automatically through the Go++ prelude.

Users should not need to write:

```gpp
import "gpp/strings"
```

for basic operations such as:

```gpp
s.TrimSpace()
s.Contains("foo")
```

The prelude remains compiler-shipped Go++ source.

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
    func Trim(cutset string) string
    func TrimLeft(cutset string) string
    func TrimRight(cutset string) string
    func TrimSpace() string

    func ToLower() string
    func ToUpper() string

    func Contains(substr string) bool
    func HasPrefix(prefix string) bool
    func HasSuffix(suffix string) bool

    func Replace(old string, new string, n int) string
    func ReplaceAll(old string, new string) string

    func Split(sep string) []string
    func Fields() []string

    func Index(substr string) int
    func LastIndex(substr string) int

    func Count(substr string) int
    func Repeat(count int) string

    func EqualFold(other string) bool
}
```

These should preserve the semantics of their corresponding functions in Go's `strings` package.

---

# 7. String Convenience Extensions

Go++ may additionally provide a few small conveniences that are not direct one-to-one wrappers.

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

---

# 8. Fluent String Example

Instead of:

```go
name := strings.TrimSpace(strings.ToLower(input))
```

Go++ may use:

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

The exact no-match behavior must remain consistent with Go++'s nil/zero-value philosophy.

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

This follows Go++'s general preference for non-exceptional nil/zero handling.

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
    func Has(key K) bool
    func GetOr(key K, fallback V) V

    func Keys() []K
    func Values() []V

    func IsEmpty() bool
    func NotEmpty() bool

    func Clone() map[K]V
}
```

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

# 27. Map `GetOr`

```gpp
theme := settings.GetOr("theme", "light")
```

returns:

```text
map value
    if key exists

fallback
    otherwise
```

The distinction is key existence, not whether the stored value is zero.

Thus if a key exists with:

```text
""
0
false
nil
```

that existing value is still returned.

---

# 28. Map `Keys`

```gpp
keys := m.Keys()
```

returns the map keys.

Go maps do not define iteration order, so `Keys()` does not promise stable ordering.

Users requiring ordering should explicitly sort the result.

---

# 29. Map `Values`

```gpp
values := m.Values()
```

returns values using normal Go map iteration semantics.

No ordering guarantee is provided.

---

# 30. Map `Clone`

```gpp
copy := m.Clone()
```

returns a shallow copy.

Nested pointers, slices, maps, classes, or reference-containing values are not recursively duplicated.

---

# 31. Error Extensions

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

# 32. `error.Is`

Instead of:

```go
errors.Is(err, sql.ErrNoRows)
```

Go++ may write:

```gpp
err.Is(sql.ErrNoRows)
```

Semantics remain exactly those of:

```go
errors.Is
```

---

# 33. `error.Unwrap`

```gpp
cause := err.Unwrap()
```

is equivalent to:

```go
errors.Unwrap(err)
```

and returns `nil` where the error has no unwrap chain.

---

# 34. `error.As`

The desired ergonomic form is:

```gpp
target := err.As[*MyError]()
```

The implementation must preserve the semantics of `errors.As`.

If direct generic extension lowering is awkward in v1, this helper may be deferred rather than introducing compiler-specific behavior solely for it.

---

# 35. `io.Reader` Extensions

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

Because Go++ automatically promotes omitted trailing errors:

```gpp
body := resp.Body.ReadAll()
```

naturally throws on a non-nil read error.

Explicit handling remains:

```gpp
body, err := resp.Body.ReadAll()
```

---

# 36. `io.Writer` Extensions

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

# 37. Numeric Extensions

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

# 38. Existing Native Methods Win

Normal Go++ extension resolution rules apply.

If a native Go type already has a method with the requested name and compatible call:

```text
native method wins
```

before extension lookup.

Extensions must not shadow or alter native Go methods.

---

# 39. No Runtime Type Modification

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

# 40. Extension Discovery

Prelude extensions should be discoverable by Go++ tooling.

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

# 41. Documentation

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

# 42. Names Should Follow Go Vocabulary

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

# 43. Avoid Excessive Aliases

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

# 44. Domain-Specific Extensions Stay Out of Prelude

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

# 45. Path Helpers

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

# 46. URL and Regex Helpers

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

# 47. Mutation Policy

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

# 48. Nil Behavior

Extensions must follow normal Go++ nil behavior.

For receiver types that may be nil:

```text
do not introduce arbitrary panics merely for convenience
```

However, native Go semantics and legitimate nil dereferences must not be silently hidden where that would change meaning.

Each extension should document meaningful nil behavior where applicable.

---

# 49. Error Behavior

Extensions wrapping functions returning trailing `error` should preserve that signature.

Example:

```gpp
io.Reader.ReadAll() ([]byte, error)
```

Then Go++ automatic error promotion naturally applies.

Do not convert errors into sentinel zero values just to make an extension look simpler.

---

# 50. Performance

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

# 51. Initial Recommended Prelude Surface

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
    GetOr
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

# 52. Example

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

With Go++:

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

# 53. Non-Goals

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

# 54. Design Summary

The feature follows this model:

```text
existing Go type
    +
ordinary Go++ extension declaration
    +
small compiler-shipped prelude
    =
more ergonomic Go++ syntax
```

Examples:

```gpp
name.TrimSpace()

users.Any(u => u.Active)

settings.GetOr("theme", "light")

err.Is(sql.ErrNoRows)

body := resp.Body.ReadAll()
```

The core rule is:

> Improve call-site ergonomics while preserving Go's types, names, semantics, interoperability, and standard library as the underlying implementation.
