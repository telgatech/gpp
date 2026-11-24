# Annotations and introspection

Annotations let a declaration carry structured, type-checked metadata.
Introspection lets code inspect that metadata together with class fields,
methods, and inheritance at runtime. These features are useful when a library
needs to work from a model definition instead of a second handwritten
description: an ORM can find table and column names, a validator can discover
required fields, and a serializer can apply field rules.

GPP keeps the two parts separate. An annotation declares information and a
target; a library or compiler feature decides what that information means.
Introspection provides the descriptors that let such tools inspect and act
on it.

## Compare struct tags with annotations

Go commonly stores metadata in string tags. GPP annotations are named,
parameterized declarations whose arguments and permitted targets are checked:

::: code-group
```go [GPP]
annotation Column(name string) on field

class Employee {
    Name string @{Column("employee_name")}
}
```

```go [Go]
type Employee struct {
    Name string `json:"name" db:"employee_name"`
}
```
:::

The target rule prevents applying `Column` to a class, and the string argument
is checked against the declared parameter type. Annotations can be declared
once in a package and reused from other packages with qualification.
GPP provides shared annotations such as `std.Deprecated` from `gpp/std`.
See the [`gpp/std` guide](/guide/standard-library/std).

## Keep annotations beside their declaration

GPP writes annotations on the declaration they describe, rather than on a
separate preceding line. For example, `@{Table("employees")}` stays with the
class header, and `@{Column("email")}` stays with its field. This makes the
relationship clear when reading code and keeps an annotation from being left
behind, commented out, or accidentally attached to a different declaration
when code is edited or copied.

When a declaration needs several annotations, put them together in a block
beside that declaration:

```go
import http "gpp/http"

class App @{
    http.IP("127.0.0.1"),
    http.Swagger,
} {}
```

## Declare and apply annotations

An annotation can have no arguments, one argument, or several typed arguments.
Use `on` to restrict where it may be applied:

```go
annotation (
    Table(name string) on class
    Column(name string) on field
    Required on field, parameter
    Route(method string, path string) on method
)

class Employee @{Table("employees")} {
    Email string @{Column("email"), Required}

    func Show(id int) string
        @{Route("GET", "/employees/{id}")} {
        return this.Email
    }
}

func Register(email string @{Required}) {}
```

Annotation declarations can be grouped with `annotation (...)` or written
individually. If `on` is omitted, the annotation can be applied to any
annotation-capable declaration. Targets include classes, fields, methods,
functions, parameters, types, packages, and template declarations. Annotation
arguments use normal GPP values and types. A no-argument annotation is written
without empty parentheses, such as `@{Required}`.

Declaring an annotation does not itself run code or change the target. It
defines metadata. Consumers such as `gpp/orm` or `gpp/encoding` interpret the
annotations they know about.

## Bundled annotations

The GPP standard packages provide annotations for serialization, ORM models,
HTTP servers, tests, and templates. Import the package that owns an annotation
and qualify it at the use site.

### `gpp/encoding`

`Serializable` opts a class into generated JSON, YAML, and GOB helpers.
`Name`, `Ignore`, and `OmitEmpty` control individual encoded fields:

```go
import "gpp/encoding"

class User @{encoding.Serializable} {
    Id int @{encoding.Name("id")}
    Name string @{encoding.Name("name")}
    Password string @{encoding.Ignore}
    Nickname string @{encoding.OmitEmpty}
}
```

### `gpp/orm`

`Table` names a model's table, `Column` maps a field to a column, and `PK`
marks a primary key:

```go
import orm "gpp/orm"

class Employee : orm.Model @{orm.Table("employees")} {
    Id int64 @{orm.Column("id"), orm.PK}
    Name string @{orm.Column("name")}
}
```

### `gpp/http`

Server annotations configure the listener, route prefix, static files,
documentation, and authentication. Method annotations declare HTTP routes and
authorization metadata.

| Annotation | Target | Example |
| --- | --- | --- |
| `IP(addr)` | Class | `@{http.IP("127.0.0.1")}` |
| `Unix(path)` | Class | `@{http.Unix("/var/run/app.sock")}` |
| `Prefix(path)` | Class | `@{http.Prefix("/api")}` |
| `Files(path, dir, DirListing = false)` | Class | `@{http.Files("/static/", "./public")}` or `@{http.Files("/static/", "./public", DirListing: true)}` |
| `OpenAPI(path)` | Class | `@{http.OpenAPI("/openapi.json")}` or `@{http.OpenAPI}` |
| `Swagger(path)` | Class | `@{http.Swagger("/swagger")}` or `@{http.Swagger}` |
| `OAuth(options...)` | Class | `@{http.OAuth(http.OAuthProvider.Google)}` |
| `Auth` | Class or method | `@{http.Auth}` |
| `Role(name)` | Class or method | `@{http.Role("admin")}` |
| `GET(path)` | Method | `@{http.GET("/items/{id}")}` |
| `POST(path)` | Method | `@{http.POST("/items")}` |
| `PUT(path)` | Method | `@{http.PUT("/items/{id}")}` |
| `PATCH(path)` | Method | `@{http.PATCH("/items/{id}")}` |
| `DELETE(path)` | Method | `@{http.DELETE("/items/{id}")}` |
| `WebSocket(path)` | Method | `@{http.WebSocket("/events")}` |
| `NoAuth` | Method | `@{http.NoAuth}` |

For example, configure an API server and add a protected route:

```go
import http "gpp/http"

class App : http.Server @{
    http.IP("127.0.0.1"),
    http.Prefix("/api"),
    http.OpenAPI,
    http.Swagger,
} {
    func Health(ctx *http.Context) error
        @{http.GET("/health")} {
        return ctx.JSON(struct(ok: true))
    }

    func Admin(ctx *http.Context) error
        @{http.GET("/admin"), http.Auth, http.Role("admin")} {
        return ctx.Text("admin")
    }
}
```

`OpenAPI` defaults to `/openapi.json` and `Swagger` defaults to `/swagger`;
both accept an optional path. `OAuth` accepts provider options. `NoAuth` can
mark a method that should be exempt from class-level authentication policy.
Use `IP` and pass the port to `Listen(port)`, or use `Unix` to choose a Unix-domain socket.

### `gpp/test`

`Tag` labels a test suite or case. `Priority` assigns a `High`, `Medium`, or
`Low` scheduling level:

```go
import "gpp/test"

class UserTest : test.Suite @{
    test.Tag("crud"),
    test.Priority(test.High),
} {
    func CreatesUser() @{test.Tag("create")} {
        user := User(Name: "Ada")
        Equal("Ada", user.Name)
    }
}
```

### `gpp/tpl`

`Path` associates a URL pattern with a template declaration:

```go
import "gpp/tpl"

template UserPage(id string) @{tpl.Path("/users/{id}")} {
    <h1>{{.}}</h1>
}
```

These are the annotations declared by the bundled encoding, ORM, HTTP, test,
and template packages. Projects can define additional annotations for their
own libraries.

## Introspect class metadata

The `.Class` selector returns metadata for a class or a GPP class instance.
Inside an inherited method, `this.Class` identifies the most-derived runtime
class, which makes reusable base-class tooling useful for child classes too:

```go
class Model {
    func Debug() {
        fmt.Println("concrete type:", this.Class.Name)
    }
}

class Employee : Model {
    Name string
    Age int
}

employee := Employee(Name: "Ada", Age: 36)
employee.Debug() // concrete type: Employee
```

The class descriptor exposes these members:

| Descriptor | Members |
| --- | --- |
| Class | `Name`, `Parents`, `Fields`, `Methods`, `Annotations` |
| Field | `Name`, `Owner`, `Type`, `Get(object)`, `Set(object, value)`, `Addr(object)`, `Annotations` |
| Method | `Name`, `Owner`, `Parameters`, `Result`, `Static`, `Annotations` |
| Parameter | `Name`, `Type`, `Annotations` |
| Type | `Name` |
| Annotation collection | `Has(Annotation)`, `Find(Annotation)`, `All(Annotation)` |
| Annotation value | `Name`, `FullName`, `Args` |

Metadata selectors use the same exported names as the Go descriptor fields.
Descriptor metadata is read-only. Use a field descriptor's `Set` method to
change a class instance's field value; the descriptor itself cannot be changed.

Class fields include inherited fields in deterministic parent-then-local order.
`Owner` identifies where a field or method was declared. A field type's `Name`
is available without exposing a complete Go reflection type system.

## Inspect fields and annotations

Annotation collections can be queried with the annotation declaration itself.
Use `Has` for a yes/no check and `Find` to read the first matching annotation's
arguments. `Find` returns `nil` when the annotation is absent:

```go
descriptor := Employee.Class
table := descriptor.Annotations.Find(orm.Table)
if table != nil {
    fmt.Println("table:", table.Args[0])
}

for field := range descriptor.Fields {
    fmt.Printf("%s.%s (%s)\n", field.Owner.Name, field.Name, field.Type.Name)
    if field.Annotations.Has(orm.PK) {
        fmt.Println("primary key:", field.Name)
    }
}
```

`All` returns every matching annotation when a target may carry more than one
use of the same annotation. Annotation values expose both the short `Name`
and qualified `FullName`, plus the typed argument values in `Args`:

```go
for annotation := range Employee.Class.Annotations.All(orm.Table) {
    fmt.Println(annotation.Name, annotation.FullName, annotation.Args[0])
}
```

Methods and parameters can be inspected in the same way:

```go
for method := range Employee.Class.Methods {
    fmt.Println(method.Owner.Name, method.Name, method.Static)
    if method.Result != nil {
        fmt.Println("returns", method.Result.Name)
    }
    if method.Annotations.Has(Route) {
        route := method.Annotations.Find(Route)
        fmt.Println("route:", route.Args)
    }
    for parameter := range method.Parameters {
        fmt.Println(parameter.Name, parameter.Type.Name)
        if parameter.Annotations.Has(Required) {
            fmt.Println("required parameter:", parameter.Name)
        }
    }
}
```

Use `Parents` to discover inherited classes and inspect their metadata:

```go
for parent := range Employee.Class.Parents {
    fmt.Println("inherits:", parent.Name)
}
```

## Use introspection for metaprogramming

Introspection turns annotations into reusable library behavior. A validator can
walk the class's effective fields, look for a `Required` annotation, and check
each value. A database helper can use `field.Addr(employee)` to build the
destination slice for `database/sql` without hardcoding every column:

```go
func ScanEmployee(row *sql.Row, employee *Employee) error {
    destinations := []any{}
    for field := range employee.Class.Fields {
        destinations = append(destinations, field.Addr(employee))
    }
    return row.Scan(destinations...)
}
```

The same pattern can drive validation. A reusable pass looks for a marker and
checks the corresponding field value:

```go
annotation Required on field

class Signup {
    Email string @{Required}
    DisplayName string
}

func ValidateSignup(value Signup) error {
    for field := range Signup.Class.Fields {
        if field.Annotations.Has(Required) && field.Get(value) == "" {
            return fmt.Errorf("%s is required", field.Name)
        }
    }
    return nil
}
```

A generic formatter or debugger can use `field.Get(employee)` to inspect
values, while a mapper can use `field.Set(&employee, value)` to populate them.
For example, find a named field and update it through the generated setter:

```go
for field := range Employee.Class.Fields {
    if field.Name == "Name" {
        field.Set(&employee, "Grace")
    }
}
```

`field.Get(employee)` reads a value; `field.Addr(&employee)` returns its address
for APIs that require pointers.
The compiler generates these accessors with the declared field types in mind;
they do not require application code to use Go's unrestricted `reflect`
package.

This is the foundation for model-driven behavior: declarations hold the
metadata, and small reusable tools interpret it. For exact declaration and
target rules, see [annotations](/reference/specifications/annotations). For
descriptor members and inheritance behavior, see
[introspection](/reference/specifications/introspection).
