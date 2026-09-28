# Prelude

The prelude is Go++'s implicit standard library: its common helpers are available without an import. It adds useful operations to familiar Go values through extension methods, keeping call sites readable while leaving the underlying slices, strings, maps, errors, and readers as ordinary Go values.

Many helpers retain their Go error results. At a Go++ call site, an uncaptured
error propagates automatically; capture it explicitly only when the code needs
to inspect it or recover locally.

## Why it exists

Small everyday operations often become repeated loops or helper functions. A prelude gives common transformations a consistent home without introducing wrapper collection types or changing Go's data model. The methods are still explicit calls, so their behavior is visible where they are used.

## Side by side: filtering a collection

In Go, filtering is usually written as a loop:

::: code-group
```go [Go++]
adultSummaries := people
	.Filter(person => person.Age >= 18)
	.Map(person => record(name: person.Name, age: person.Age))
```

```go [Go]
var adults []Person
for _, person := range people {
	if person.Age >= 18 {
		adults = append(adults, person)
	}
}
```
:::

The lambda keeps the predicate next to the operation. The input and output are still `[]Person`; `Filter` simply packages the common loop.

## Collection examples

```go
activeNames := people.Filter(person => person.Active).Map(person => person.Name)
hasAdults := people.Any(person => person.Age >= 18)
allNamed := people.All(person => person.Name != "")
first, found := people.Find(person => person.Active)
recent := events.Take(10)
older := events.Drop(10)
```

When a transformation needs a few steps, use a block lambda:

```go
labels := people.Map(person => {
	name := person.Name.TrimSpace()
	return name.ToUpper()
})
```

The parameter type is inferred from the collection element type, so these lambdas do not need explicit parameter types.

## Function reference

### Slice extensions

#### `Any(predicate) bool`

Returns `true` as soon as the predicate matches an element. Returns `false` for an empty slice.

```go
hasAdults := people.Any(person => person.Age >= 18)
```

#### `All(predicate) bool`

Returns `true` only when every element matches. An empty slice returns `true` because there is no counterexample.

```go
allNamed := people.All(person => person.Name != "")
```

#### `Find(predicate) (T, bool)`

Returns the first matching value and `true`; if none matches, returns the element type's zero value and `false`.

```go
person, found := people.Find(person => person.Active)
```

#### `Filter(predicate) []T`

Returns a new slice containing matching values in their original order. The source slice is unchanged.

```go
active := people.Filter(person => person.Active)
```

#### `First() T`

Returns the first element, or the zero value of the element type when the slice is empty. Use a length check when an empty slice must be distinguished from one whose first value is zero.

```go
first := people.First()
```

#### `Last() T`

Returns the last element, or the zero value of the element type when the slice is empty.

```go
latest := events.Last()
```

#### `IsEmpty() bool`

Reports whether the slice has no elements.

```go
if queue.IsEmpty() { /* nothing to process */ }
```

#### `NotEmpty() bool`

Reports whether the slice contains at least one element; it is the inverse of `IsEmpty()`.

```go
if results.NotEmpty() { /* show results */ }
```

#### `Take(n int) []T`

Returns a copy of the first `n` elements. A non-positive `n` gives an empty slice; a value larger than the length gives a copy of the whole slice.

```go
topFive := scores.Take(5)
```

#### `Drop(n int) []T`

Returns a copy of the elements after the first `n`. A non-positive `n` copies the whole slice; a value at least as large as the length gives an empty slice.

```go
remaining := tasks.Drop(1)
```

#### `Map[R any](transform func(T) R) []R`

Applies the transform to every element and returns a new slice of the result type. It preserves element order and does not mutate the source.

```go
names := people.Map(person => person.Name)
```

#### `Reverse()`

Reverses the slice **in place**. It does not allocate or return a new slice.

```go
events.Reverse()
```

#### `Sort(less func(T, T) bool)`

Sorts the slice **in place** using the supplied “comes before” comparison. Use this overload for custom types or sort orders.

```go
people.Sort((left, right) => left.Name < right.Name)
```

### Comparable slice extensions

These methods are available when the slice element type is comparable.

#### `Contains(value T) bool`

Returns `true` if an equal value appears in the slice.

```go
hasAdmin := roles.Contains("admin")
```

#### `Index(value T) int`

Returns the index of the first equal value, or `-1` if it is absent.

```go
position := roles.Index("editor")
```

### Ordered slice extensions

These methods are available when the element type has Go's ordered comparison operators.

#### `Sort()`

Sorts the slice in ascending order **in place**.

```go
scores.Sort()
```

#### `SortDesc()`

Sorts the slice in descending order **in place**.

```go
scores.SortDesc()
```

#### `Min() (T, bool)`

Returns the smallest element and `true`. For an empty slice, returns the zero value and `false`.

```go
lowest, hasScores := scores.Min()
```

#### `Max() (T, bool)`

Returns the largest element and `true`. For an empty slice, returns the zero value and `false`.

```go
highest, hasScores := scores.Max()
```

### String extensions

#### `CompileRegex() (*regexp.Regexp, error)`

Compiles the string as a regular expression and returns `(*regexp.Regexp, error)`. Invalid patterns are reported through the error result.

```go
pattern := "^user-[0-9]+$".CompileRegex()
```

#### `Trim(cutset string) string`

Removes all leading and trailing Unicode code points contained in `cutset`.

```go
clean := "--title--".Trim("-")
```

#### `TrimLeft(cutset string) string`

Removes leading code points from `cutset`, leaving the right side unchanged.

```go
path := "///api".TrimLeft("/")
```

#### `TrimRight(cutset string) string`

Removes trailing code points from `cutset`, leaving the left side unchanged.

```go
filename := "report...".TrimRight(".")
```

#### `TrimSpace() string`

Removes leading and trailing whitespace.

```go
name := rawName.TrimSpace()
```

#### `ToLower() string`

Returns the string converted to lowercase according to Go's Unicode-aware string functions.

```go
key := input.ToLower()
```

#### `ToUpper() string`

Returns the string converted to uppercase.

```go
label := status.ToUpper()
```

#### `Contains(substr string) bool`

Reports whether `substr` occurs anywhere in the string.

```go
isGo := title.Contains("Go")
```

#### `HasPrefix(prefix string) bool`

Reports whether the string begins with `prefix`.

```go
isAPI := path.HasPrefix("/api/")
```

#### `HasSuffix(suffix string) bool`

Reports whether the string ends with `suffix`.

```go
isJSON := filename.HasSuffix(".json")
```

#### `Replace(old, new string, n int) string`

Returns a copy with up to `n` non-overlapping occurrences replaced. A negative `n` replaces every occurrence; zero makes no changes.

```go
preview := text.Replace("old", "new", 1)
```

#### `ReplaceAll(old, new string) string`

Returns a copy with every non-overlapping occurrence of `old` replaced by `new`.

```go
slug := title.ReplaceAll(" ", "-")
```

#### `Split(sep string) []string`

Splits the string around each occurrence of `sep`. An empty separator splits between UTF-8 sequences.

```go
fields := csvLine.Split(",")
```

#### `Fields() []string`

Splits on runs of Unicode whitespace and omits leading, trailing, and repeated whitespace.

```go
words := sentence.Fields()
```

#### `Index(substr string) int`

Returns the byte index of the first occurrence of `substr`, or `-1` when absent.

```go
offset := source.Index("package ")
```

#### `LastIndex(substr string) int`

Returns the byte index of the last occurrence of `substr`, or `-1` when absent.

```go
lastSlash := path.LastIndex("/")
```

#### `Count(substr string) int`

Counts non-overlapping occurrences of `substr`. An empty `substr` has one match at each UTF-8 boundary.

```go
mentions := text.Count("Go++")
```

#### `Repeat(count int) string`

Returns the string repeated `count` times. Its behavior, including invalid counts, follows Go's `strings.Repeat`.

```go
divider := "-".Repeat(40)
```

#### `EqualFold(other string) bool`

Reports whether two strings are equal under Unicode case folding.

```go
sameUser := supplied.EqualFold(expected)
```

#### `Empty() bool`

Reports whether the string has zero bytes. Whitespace is not considered empty.

```go
if token.Empty() { /* reject missing token */ }
```

#### `Blank() bool`

Reports whether the string is empty or consists only of whitespace.

```go
if comment.Blank() { /* skip comment */ }
```

#### `Lines() []string`

Splits on line endings, treating `\r\n`, `\r`, and `\n` as separators. A trailing line ending produces a final empty element.

```go
lines := document.Lines()
```

#### `Words() []string`

Splits on Unicode whitespace, like `Fields()`. It is provided as a word-oriented name for the same operation.

```go
words := sentence.Words()
```

### Map extensions

#### `Keys() []K`

Returns a slice containing the map's keys. Map iteration order is unspecified, so the returned key order is unspecified too.

```go
names := users.Keys()
```

#### `Values() []V`

Returns a slice containing the map's values, in unspecified order.

```go
counts := inventory.Values()
```

#### `Has(key K) bool`

Reports whether the key exists, including when its associated value is the value type's zero value.

```go
configured := settings.Has("port")
```

#### `GetOr(key K, fallback V) V`

Returns the value for `key`, or `fallback` when the key is absent. Use `Has` when you need to distinguish an absent key from a present zero value.

```go
port := settings.GetOr("port", 8080)
```

#### `IsEmpty() bool`

Reports whether the map has no entries.

```go
if cache.IsEmpty() { /* warm cache */ }
```

#### `NotEmpty() bool`

Reports whether the map contains at least one entry.

```go
if headers.NotEmpty() { /* copy headers */ }
```

#### `Clone() map[K]V`

Returns a new map with the same keys and values. It is a shallow copy: referenced values are not cloned.

```go
updated := defaults.Clone()
```

### Error extensions

#### `Is(target error) bool`

Reports whether the error chain contains `target`, using Go's `errors.Is` behavior.

```go
if err.Is(context.Canceled) { /* request was cancelled */ }
```

#### `Unwrap() error`

Returns the error wrapped directly by this error, or `nil` when there is no direct wrapped error. Use `Is` to search an entire chain.

```go
cause := err.Unwrap()
```

#### `As[T any]() T`

Searches the error chain for a value assignable to `T` and returns it. If no matching error exists, it returns `T`'s zero value; use `Is` or an appropriate nil check when absence matters.

```go
var pathErr *os.PathError = err.As[*os.PathError]()
```

### Reader and writer extensions

#### `ReadAll() ([]byte, error)`

Reads an `io.Reader` completely and returns its bytes and any read error. It follows `io.ReadAll` behavior.

```go
body := response.Body.ReadAll()
```

#### `WriteString(value string) (int, error)`

Writes the string to an `io.Writer`, returning the number of bytes written and any write error.

```go
count := writer.WriteString("hello")
```

## Further reading

- [Prelude specification](/reference/specifications/prelude)
- [Extension methods specification](/reference/specifications/std.extensions)
- [Regex extensions specification](/reference/specifications/std.extensions.regex)
