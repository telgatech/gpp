# Go++ String Regex Compilation Add-on Specification

## 1. Overview

Go++ adds one small regex-oriented extension to `string`:

```gpp id="i0qg4x"
extend string {
    func CompileRegex() (*regexp.Regexp, error)
}
```

Its purpose is to turn regex source text into Go's native:

```go id="j35a62"
*regexp.Regexp
```

while preserving Go++'s normal automatic error propagation.

Example:

```gpp id="j02ywm"
re := "^[a-z]+$".CompileRegex()

if re.MatchString(username) {
    ...
}
```

Direct chaining is also valid:

```gpp id="cv5jhl"
if "^[a-z]+$".CompileRegex().MatchString(username) {
    ...
}
```

---

# 2. Design Goal

The goal is to provide regex ergonomics without duplicating Go's `regexp` API across `string`.

Instead of adding extensions such as:

```gpp id="ej25dm"
pattern.MatchString(value)
pattern.FindString(value)
pattern.FindAllString(value, -1)
pattern.ReplaceAllString(value, "#")
```

Go++ provides one conversion-oriented operation:

```gpp id="5dythg"
pattern.CompileRegex()
```

which returns the ordinary native Go regex object.

The existing standard-type extension specification already favors thin ergonomic projections over wrapper types or replacement standard libraries.

---

# 3. Canonical API

The canonical extension is:

```gpp id="wr4d8e"
extend string {
    func CompileRegex() (*regexp.Regexp, error)
}
```

Conceptually it is equivalent to:

```go id="vl4zik"
regexp.Compile(pattern)
```

where `pattern` is the receiver.

---

# 4. Basic Usage

```gpp id="nf981u"
pattern := "^[a-z]+$"

re := pattern.CompileRegex()

if re.MatchString(username) {
    ...
}
```

Because the returned value is the native `*regexp.Regexp`, all normal Go regex methods remain available.

---

# 5. Direct Chaining

Compilation may be chained directly into regex use:

```gpp id="sw6oik"
matched := "^[0-9]+$".CompileRegex().MatchString(value)
```

This is intentionally valid.

Automatic trailing-error promotion means an invalid pattern propagates according to ordinary Go++ error behavior.

---

# 6. Error Promotion

`CompileRegex()` preserves the trailing `error` result.

Therefore:

```gpp id="llnp2f"
re := pattern.CompileRegex()
```

uses Go++'s normal automatic error promotion.

Conceptually this replaces:

```go id="2ki3ak"
re, err := regexp.Compile(pattern)
if err != nil {
    return err
}
```

without changing the underlying error semantics.

This follows the existing standard-extension rule that wrappers returning trailing errors retain those signatures so Go++ error promotion can apply naturally.

---

# 7. Explicit Error Handling

Explicit Go-style handling remains available:

```gpp id="8yb16c"
re, err := pattern.CompileRegex()

if err != nil {
    ...
}
```

The extension does not force exception-style behavior.

---

# 8. Returned Type

The return type is exactly:

```go id="5if8mc"
*regexp.Regexp
```

There is no Go++ regex wrapper type.

This preserves the standard-type extension principle that conveniences do not introduce replacement runtime types.

---

# 9. Native Regex API

Once compiled, users use ordinary Go regexp methods.

Example:

```gpp id="qtc6a8"
re := "[0-9]+".CompileRegex()

re.MatchString(value)
re.FindString(value)
re.FindAllString(value, -1)
re.ReplaceAllString(value, "#")
re.Split(value, -1)
re.FindStringSubmatch(value)
```

Go++ does not duplicate these methods onto `string`.

---

# 10. Preserve Go Vocabulary

The returned object keeps Go's native method names.

Prefer:

```gpp id="ae0ht3"
pattern.CompileRegex().MatchString(value)
```

not:

```gpp id="0xj2y4"
pattern.CompileRegex().Match(value)
```

Go++ should not rename native regex operations merely to create a different style.

This follows the standard-extension rule to preserve Go terminology where an equivalent standard-library concept already exists.

---

# 11. Why `CompileRegex`

The extension name should make both operations clear:

```text id="0p0ij2"
Compile
    this operation may fail

Regex
    the string is interpreted as a regular expression
```

A shorter name such as:

```gpp id="fv15q2"
pattern.Regex()
```

is less explicit about compilation and error behavior.

The existing specification had previously discouraged generic `s.Regex()` helpers because they blur the transition into another domain.

`CompileRegex()` addresses that concern by making the conversion explicit.

---

# 12. Receiver Meaning

For:

```gpp id="qv7yc5"
pattern.CompileRegex()
```

the receiver is always interpreted as regex source.

Example:

```gpp id="ezhwhu"
"^foo.*bar$".CompileRegex()
```

means:

```go id="2vmpcg"
regexp.Compile("^foo.*bar$")
```

---

# 13. Repeated Use

For repeated matching, compile once:

```gpp id="8lp7gt"
re := "^[a-z]+$".CompileRegex()

for value in values {
    if re.MatchString(value) {
        ...
    }
}
```

This is preferable to:

```gpp id="0zwor8"
for value in values {
    if "^[a-z]+$".CompileRegex().MatchString(value) {
        ...
    }
}
```

because the latter recompiles the expression repeatedly unless the compiler/runtime can prove and optimize otherwise.

Go++ should not silently introduce hidden regex caching in v1.

---

# 14. One-Shot Use

For occasional one-shot operations, direct chaining is encouraged:

```gpp id="t4hyse"
if "^admin-[0-9]+$".CompileRegex().MatchString(role) {
    ...
}
```

The choice between compile-once and one-shot use remains visible in source.

---

# 15. No Hidden Caching

`CompileRegex()` should initially behave like:

```go id="ujnqnw"
regexp.Compile
```

on every invocation.

It should not maintain:

```text id="4ni6gm"
global regex caches
implicit LRU caches
per-string caches
runtime memoization
```

because those would introduce hidden memory and concurrency behavior.

Caching can be explicit at the application level if needed.

---

# 16. No `MustCompileRegex` in V1

V1 should not add:

```gpp id="kldhbl"
pattern.MustCompileRegex()
```

Go++ already makes the safe form concise:

```gpp id="lsmhep"
re := pattern.CompileRegex()
```

Automatic error promotion gives almost the same convenience without converting invalid regex syntax into panic behavior.

If dogfooding later demonstrates a strong compile-time/static initialization case, `MustCompileRegex` may be reconsidered.

---

# 17. Invalid Pattern

Given:

```gpp id="hrrqu7"
re := "[abc".CompileRegex()
```

`regexp.Compile` returns an error.

That error follows ordinary Go++ propagation rules.

The extension must not:

```text id="1y6q7g"
return nil silently
return false
repair the pattern
swallow the syntax error
panic
```

---

# 18. `??` Compatibility

Because compilation participates in Go++'s normal error model, expression fallback may be used where appropriate:

```gpp id="y8fr7d"
re := userPattern.CompileRegex() ?? defaultPattern.CompileRegex()
```

Normal `??` semantics apply.

The regex extension introduces no special error behavior.

---

# 19. `try/catch` Compatibility

Regex compilation errors are ordinary Go errors and therefore participate in normal Go++ error handling:

```gpp id="76wo91"
try {
    re := pattern.CompileRegex()
    ...
} catch e {
    ...
}
```

No regex-specific exception hierarchy is introduced.

---

# 20. Implementation

Preferred prelude implementation:

```gpp id="kt726b"
extend string {
    func CompileRegex() (*regexp.Regexp, error) {
        return regexp.Compile(this)
    }
}
```

This should remain ordinary Go++ source where practical.

---

# 21. Lowering

Conceptually:

```gpp id="zvztck"
pattern.CompileRegex()
```

lowers to behavior equivalent to:

```go id="1nbw8s"
regexp.Compile(pattern)
```

using the ordinary extension-method lowering mechanism.

No special compiler intrinsic is required.

The standard extension specification already establishes ordinary extension declarations as the preferred implementation mechanism.

---

# 22. Prelude Availability

`CompileRegex()` belongs in the standard Go++ ergonomic prelude.

Users should not need a special Go++ package import merely to write:

```gpp id="ok2c9m"
pattern.CompileRegex()
```

The compiler/prelude implementation may import Go's `regexp` package internally.

---

# 23. Native Methods Still Win

Normal extension lookup rules remain unchanged.

If a future receiver type has a real native method called:

```text id="8md58c"
CompileRegex
```

the native method takes precedence according to ordinary Go++ extension resolution.

The feature does not alter normal dispatch rules.

---

# 24. Tooling

Autocomplete on a string should expose:

```text id="iak5sw"
CompileRegex
```

alongside other applicable string extensions.

Hover/documentation should identify it as:

```text id="avt702"
string.CompileRegex() (*regexp.Regexp, error)

Convenience extension over regexp.Compile.
```

---

# 25. `gpp doc`

Example:

```bash id="1e899a"
gpp doc string.CompileRegex
```

should display information conceptually equivalent to:

```text id="xgy0nu"
func string.CompileRegex() (*regexp.Regexp, error)

Compiles the receiver as a Go regular expression.

Equivalent to regexp.Compile(receiver).

The returned value is the native *regexp.Regexp.
```

---

# 26. Standard Library Interoperability

Because the return value is native:

```go id="dv6qtd"
*regexp.Regexp
```

it can be passed directly to Go APIs.

Example:

```gpp id="vy37vf"
re := pattern.CompileRegex()

SomeGoFunction(re)
```

No conversion or wrapper unboxing is necessary.

---

# 27. No Regex Methods on `string`

The prelude should not initially add:

```gpp id="ovya75"
string.MatchString(...)
string.FindString(...)
string.FindAllString(...)
string.ReplaceAllString(...)
string.FindStringSubmatch(...)
```

Those operations already exist naturally on the compiled regex value.

Adding them directly to `string` would duplicate a significant part of Go's `regexp.Regexp` method surface.

---

# 28. No `string.Regex()`

Do not add:

```gpp id="1qygyo"
pattern.Regex()
```

in v1.

`CompileRegex()` is preferred because it communicates:

```text id="a519cz"
domain conversion
possible compilation failure
relationship to regexp.Compile
```

more clearly.

---

# 29. No Automatic Pattern Interpretation

Ordinary string methods continue to treat arguments literally.

Example:

```gpp id="ohtcry"
value.Contains("[0-9]+")
```

means literal substring containment.

It does not invoke regex behavior.

Regex behavior only begins explicitly with:

```gpp id="bfrtzk"
"[0-9]+".CompileRegex()
```

This prevents ambiguity between string and regex semantics.

---

# 30. No Change to Existing String Operations

Existing methods retain current semantics:

```gpp id="l8uyvc"
s.Contains(...)
s.Replace(...)
s.Split(...)
s.Index(...)
```

They remain projections of Go's `strings` behavior.

The regex extension does not reinterpret or overload them.

The current string extension set is explicitly based primarily on direct `strings` package projections.

---

# 31. Fluent Example

```gpp id="t7kj2o"
emailPattern :=
    `^[^@\s]+@[^@\s]+\.[^@\s]+$`.
    CompileRegex()

if emailPattern.MatchString(email.TrimSpace()) {
    ...
}
```

The string preprocessing remains ordinary string behavior.

The regex matching remains ordinary native Go regexp behavior.

---

# 32. Inline Example

```gpp id="2zcqbt"
if `^/users/[0-9]+$`.
    CompileRegex().
    MatchString(path) {

    ...
}
```

This form is useful for short one-off checks.

---

# 33. Extracting Matches

```gpp id="56ljip"
re := `[0-9]+`.CompileRegex()

first := re.FindString("order 123 costs 45")
all := re.FindAllString("order 123 costs 45", -1)
```

Expected behavior is exactly that of Go's regexp package.

---

# 34. Replacement

```gpp id="jq4axm"
re := `[0-9]+`.CompileRegex()

clean := re.ReplaceAllString(
    "order 123 costs 45",
    "#",
)
```

No Go++-specific replacement syntax is introduced.

---

# 35. Splitting

Because the result is a native regex, regex splitting is naturally available:

```gpp id="dr1r6v"
re := `\s*,\s*`.CompileRegex()

parts := re.Split(
    "one, two ,three",
    -1,
)
```

This avoids overloading the existing literal:

```gpp id="t77la8"
value.Split(",")
```

with regex semantics.

---

# 36. Performance Principle

The extension should be as thin as practical.

`CompileRegex()` should add no meaningful overhead beyond:

```go id="8sa155"
regexp.Compile
```

and ordinary extension-call lowering.

This follows the standard-extension policy that ergonomic methods should remain thin wrappers without unnecessary allocations, reflection, or hidden runtime machinery.

---

# 37. Revised Regex Policy

The standard-type ergonomic extensions specification should revise its previous regex guidance.

Instead of broadly excluding regex-related string ergonomics, the rule becomes:

> Go++ does not mirror the regex API onto `string`. It provides `string.CompileRegex()` as an explicit bridge from regex source text to Go's native `*regexp.Regexp`.

This preserves the original concern about turning `string` into a domain-heavy mega type while still removing repeated `regexp.Compile(...)` ceremony.

---

# 38. Revised Non-Goal

The previous non-goal:

```text id="gqpt9n"
implicit URL/regex parsing
```

should be interpreted more narrowly.

Go++ still avoids implicit regex interpretation.

This remains invalid conceptually:

```text id="0g0nhw"
ordinary string operation
    magically treated as regex
```

But this explicit conversion is supported:

```gpp id="tx6hl4"
pattern.CompileRegex()
```

because the programmer has explicitly selected regex semantics.

---

# 39. Design Principle

The distinction is:

```text id="p21nc5"
bad:
    silently treat a string as a regex

good:
    explicitly compile a string into *regexp.Regexp
```

The method name itself establishes the semantic boundary.

---

# 40. V1 Surface

The complete regex addition to the standard `string` extension surface is therefore only:

```gpp id="1185dx"
extend string {
    func CompileRegex() (*regexp.Regexp, error)
}
```

No additional regex-specific `string` methods are required for v1.

---

# 41. Design Summary

Typical usage:

```gpp id="dnm1do"
re := "^[a-z]+$".CompileRegex()

if re.MatchString(username) {
    ...
}
```

or:

```gpp id="g7h3d3"
if "^[a-z]+$".CompileRegex().MatchString(username) {
    ...
}
```

The model is:

```text id="sh1jbc"
string pattern
      ↓
CompileRegex()
      ↓
*regexp.Regexp
      ↓
native Go regex API
```

The core rules are:

> `CompileRegex()` is the only regex-specific `string` extension required for v1.

> It is a thin ergonomic projection over `regexp.Compile`.

> It returns the native `*regexp.Regexp`, not a Go++ wrapper.

> Invalid patterns preserve the ordinary trailing `error`.

> Go++ automatic error promotion makes the safe compile path concise.

> Native Go regexp method names and semantics remain unchanged.

> Repeated regex use should compile once and reuse the returned regex.

> Go++ does not silently interpret ordinary strings or string methods as regular expressions.

> The feature improves ergonomics while preserving Go's regexp package as the underlying abstraction.
