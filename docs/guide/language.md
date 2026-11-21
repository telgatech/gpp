# Language guide

GPP adds application-level constructs while preserving Go’s types, packages,
imports, and generated-code model.

## Classes and construction

Classes compile to Go structs and receiver methods. Construction can use
positional or named fields:

```go
class Person {
    Name string
    Age int
}

person := Person("Bob", 42)
other := Person{
    Name: "Ada",
    Age: 36,
}
```

These forms have deliberately different roles:

- `Person("Bob", 42)` is GPP constructor-style syntax. The compiler checks
  positional order, inherited fields, and arity, then lowers it to a Go
  composite literal.
- `Person(Name: "Bob", Age: 42)` is the named form of the same syntax. Named
  arguments can be supplied in any order, but positional and named arguments
  cannot be mixed.
- `Person{...}` is an ordinary Go composite literal and remains available
  unchanged and does not run class construction hooks.

Classes can define one instance `init()` method for setup and validation after
constructor fields are assigned. Constructor-style calls run it after
initializing fields; ordinary `User{...}` composite literals bypass it. The
initializer can return an error, which construction returns or promotes using
the normal trailing-error rules:

```go
class User {
    Name string

    static func Guest() User {
        return User(Name: "Guest")
    }
}

guest := User.Guest()
```

Inheritance and polymorphism are implemented through generated Go structures
and interfaces, so classes can still interoperate with ordinary Go code.

## Typed slices and arrays

Put the element type before a bracketed list to create a slice. The type also
provides the element type for nested literals:

```gpp
numbers := int[1, 2, 3]                 // []int
matrix := []int[[1, 2], [3, 4]]        // [][]int
fixed := [3]int[10, 20, 30]            // [3]int
empty := int[]
```

Class elements use the same constructor rules as ordinary `Type(...)` calls.
The parenthesized group supplies the arguments for one element:

```gpp
boxes := Box[int][(value: 1), (value: 2)] // []*Box[int]
```

Elements can also be ordinary expressions, so a parent-typed slice can hold
child class instances: `animals := Animal[Dog(), Cat()]`.

Use Go brace literals when you need keyed or indexed elements, such as
`[]int{2: 10}`. See the [typed slice and array literal specification](/reference/specifications/slice-array-literals)
for the full syntax and compatibility rules.

Multiple parents are allowed. If parents expose the same member, qualify the
member through the parent name instead of relying on an implicit winner. A
derived value passed through a base-typed parameter keeps the concrete method
dispatch expected by GPP.

## Functions and calls

GPP calls can use named arguments and defaults:

```go
func Greet(name string, punctuation string = "!") string {
    return name + punctuation
}

Greet(name: "Bob")
Greet(punctuation: "...", name: "Bob")
```

Functions and methods can also be overloaded by arity or exact static
parameter types:

```go
func Format(value int) string { return "int" }
func Format(value string) string { return "string" }

Format(42)
Format("value")
```

Resolution happens at compile time. An ambiguous call is a diagnostic; it is
not deferred to runtime reflection.

## Conditional expressions

Choose between two values inline with `condition ? whenTrue : whenFalse`:

```gpp
label := enabled ? "on" : "off"
```

The condition must be boolean, both branches must have compatible Go types,
and only the selected branch is evaluated. Nested conditionals associate from
right to left.

Class methods can also overload a non-generic method with a generic one. An
explicit type argument selects the generic overload, while a call without one
selects the non-generic overload:

```gpp
class Client {
    func Get(path string) string { return "raw response" }
    func Get[T any](path string) T { var value T; return value }
}

client := Client()
raw := client.Get("/users/1")
user := client.Get[User]("/users/1")
```

## Operators

GPP includes Go's standard operators, compound assignments, and postfix
increment/decrement statements, plus `||=` for conditional defaults and `??`
for error fallback. See the [Operators feature guide](/features/operators) for
the supported forms and examples.

## Anonymous structs and enums

Anonymous structs are concise anonymous data values:

```go
health := struct(ok: true, service: "catalog")
```

Enums provide named scalar values with conversion and metadata:

```go
enum Status {
    Draft
    Published
}
```

## Less ceremony in expressions

String interpolation, lambdas, safe access, and lazy error fallback keep
common application code compact:

```go
message := "Hello {{user.Name}}"
active := users.Any(user => user.Active)
label := user?.Profile?.DisplayName
name := user?.Name
price := strconv.ParseFloat(rawPrice, 64) ?? 0.0
```

The `??` operator is lazy: the fallback is evaluated only when the preceding
operation raises a GPP error. Successful values, including nil, pass through
unchanged.

Ordinary `.` and explicit Go error handling keep their normal meaning. Safe
access and fallback are opt-in, which makes the compatibility boundary easy to
see in a code review.

## Exceptions

GPP keeps ordinary Go errors valid and adds structured exception flow for
code where repeated propagation obscures the main path:

```go
try {
    data := os.ReadFile("config.json")
    fmt.Println(len(data))
} catch os.PathError e {
    fmt.Println("missing:", e.Path)
} catch e {
    fmt.Println("failed:", e)
} finally {
    fmt.Println("finished")
}
```

Uncaptured trailing `error` results propagate automatically as exceptions.
`try`/`catch` adds local recovery, `throw` raises a GPP error value, catch
clauses may name one type, several types, or a catch-all, and `finally` runs
during normal completion and unwinding. At Go boundaries, the compiler still
emits ordinary error values and Go-compatible code.

## Annotations and metadata

Annotations are typed declarations with target validation:

```go
annotation Label(value string) on class, field

class User @{Label("account")} {
    Name string @{Label("display name")}
}
```

The compiler stores runtime metadata for introspection. Class descriptors can
expose names, fields, methods, annotations, owners, and field accessors. This
is the mechanism used by the ORM, OpenAPI generation, and serialization
helpers; the language itself does not hard-code application models.

## Templates and embedding

Template declarations use standard `html/template` actions and can inherit a
layout through `: Layout`. External `.gpp.tpl` files are compiled alongside
the application and can be watched during development.

`embed` turns a rooted directory into `fs.FS` or a file into `[]byte`, using
the same filesystem interfaces as ordinary Go. These features are described in
detail in the [standard library guide](/guide/standard-library).

## Extensions

Extension blocks add methods to existing types without modifying the original
type:

```go
extend string {
    func IsBlank() bool {
        return this.TrimSpace() == ""
    }
}
```

The compiler lowers extensions to ordinary package-level Go functions and
resolves method-style calls at compile time.

Extensions are general rather than a built-in list of special cases: they can
target GPP classes, named Go types, pointers, and multiple compatible types.
They do not modify the target type or require runtime registration.

## HTTP and templates

The standard HTTP package combines route annotations, lifecycle hooks,
templates, OpenAPI metadata, and Swagger UI. Template declarations use
standard `html/template` syntax and can also live in external `.gpp.tpl`
files.

```go
template Page(title string) {
    <h1>{{.}}</h1>
}

class App : http.Server @{http.GET("/hello/{name}")} {
    func Hello(ctx *http.Context) error {
        return ctx.HTML(200, ctx.Param("name"), "Page")
    }
}
```

## Compatibility by default

GPP deliberately keeps ordinary Go syntax valid. When a feature is not
needed, use the Go you already know. See [the compatibility specification](/reference/specifications/compat) for the precise contract.
