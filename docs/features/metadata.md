# Annotations and introspection

Annotations let a declaration carry structured, type-checked metadata.
Introspection lets code inspect that metadata together with class fields,
methods, and inheritance at runtime. These features are useful when a library
needs to work from a model definition instead of a second handwritten
description: an ORM can find table and column names, a validator can discover
required fields, and a serializer can apply field rules.

Go++ keeps the two parts separate. An annotation declares information and a
target; a library or compiler feature decides what that information means.
Introspection provides the descriptors that let such tools inspect and act
on it.

## Compare struct tags with annotations

Go commonly stores metadata in string tags. Go++ annotations are named,
parameterized declarations whose arguments and permitted targets are checked:

::: code-group

```go [Go struct tag]
type Employee struct {
    Name string `json:"name" db:"employee_name"`
}
```

```go [Go++ annotation]
annotation Column(name string) on field

class Employee {
    Name string @{Column("employee_name")}
}
```

:::

The target rule prevents applying `Column` to a class, and the string argument
is checked against the declared parameter type. Annotations can be declared
once in a package and reused from other packages with qualification.

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
arguments use normal Go++ values and types. A no-argument annotation is written
without empty parentheses, such as `@{Required}`.

Declaring an annotation does not itself run code or change the target. It
defines metadata. Consumers such as `gpp/orm` or `gpp/encoding` interpret the
annotations they know about.

## Bundled annotations

The Go++ standard packages provide annotations for serialization, ORM models,
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
| `Port(port)` | Class | `@{http.Port(8080)}` |
| `Unix(path)` | Class | `@{http.Unix("/var/run/app.sock")}` |
| `Prefix(path)` | Class | `@{http.Prefix("/api")}` |
| `Files(path, dir)` | Class | `@{http.Files("/static/", "./public")}` |
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
    http.Port(8080),
    http.Prefix("/api"),
    http.OpenAPI,
    http.Swagger,
} {
    func Health(ctx *http.Context) error
        @{http.GET("/health")} {
        return ctx.JSON(record(ok: true))
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
Use either `IP` with `Port`, or `Unix`, to choose the listener address.

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

The `.class` selector returns metadata for a class or a Go++ class instance.
Inside an inherited method, `this.class` identifies the most-derived runtime
class, which makes reusable base-class tooling useful for child classes too:

```go
class Model {
    func Debug() {
        fmt.Println("concrete type:", this.class.name)
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
| Class | `name`, `parents`, `fields`, `methods`, `annotations` |
| Field | `name`, `owner`, `type`, `get(object)`, `set(object, value)`, `addr(object)`, `annotations` |
| Method | `name`, `owner`, `parameters`, `result`, `static`, `annotations` |
| Parameter | `name`, `type`, `annotations` |
| Type | `name` |
| Annotation collection | `has(Annotation)`, `get(Annotation)`, `all(Annotation)` |
| Annotation value | `name`, `fullName`, `args` |

Class fields include inherited fields in deterministic parent-then-local order.
`owner` identifies where a field or method was declared. A field type's `name`
is available without exposing a complete Go reflection type system.

## Inspect fields and annotations

Annotation collections can be queried with the annotation declaration itself.
Use `has` for a yes/no check and `get` to read a particular annotation's
arguments:

```go
descriptor := Employee.class
table := descriptor.annotations.get(orm.Table)
if table != nil {
    fmt.Println("table:", table.args[0])
}

for field := range descriptor.fields {
    fmt.Printf("%s.%s (%s)\n", field.owner.name, field.name, field.type.name)
    if field.annotations.has(orm.PK) {
        fmt.Println("primary key:", field.name)
    }
}
```

`all` returns every matching annotation when a target may carry more than one
use of the same annotation. Annotation values expose both the short `name`
and qualified `fullName`, plus the typed argument values in `args`:

```go
for annotation := range Employee.class.annotations.all(orm.Table) {
    fmt.Println(annotation.name, annotation.fullName, annotation.args[0])
}
```

Methods and parameters can be inspected in the same way:

```go
for method := range Employee.class.methods {
    fmt.Println(method.owner.name, method.name, method.static)
    if method.result != nil {
        fmt.Println("returns", method.result.name)
    }
    if method.annotations.has(Route) {
        route := method.annotations.get(Route)
        fmt.Println("route:", route.args)
    }
    for parameter := range method.parameters {
        fmt.Println(parameter.name, parameter.type.name)
        if parameter.annotations.has(Required) {
            fmt.Println("required parameter:", parameter.name)
        }
    }
}
```

Use `parents` to discover inherited classes and inspect their metadata:

```go
for parent := range Employee.class.parents {
    fmt.Println("inherits:", parent.name)
}
```

## Use introspection for metaprogramming

Introspection turns annotations into reusable library behavior. A validator can
walk the class's effective fields, look for a `Required` annotation, and check
each value. A database helper can use `field.addr(employee)` to build the
destination slice for `database/sql` without hardcoding every column:

```go
func ScanEmployee(row *sql.Row, employee *Employee) error {
    destinations := []any{}
    for field := range employee.class.fields {
        destinations = append(destinations, field.addr(employee))
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
    for field := range Signup.class.fields {
        if field.annotations.has(Required) && field.get(value) == "" {
            return fmt.Errorf("%s is required", field.name)
        }
    }
    return nil
}
```

A generic formatter or debugger can use `field.get(employee)` to inspect
values, while a mapper can use `field.set(&employee, value)` to populate them.
For example, find a named field and update it through the generated setter:

```go
for field := range Employee.class.fields {
    if field.name == "Name" {
        field.set(&employee, "Grace")
    }
}
```

`field.get(employee)` reads a value; `field.addr(&employee)` returns its address
for APIs that require pointers.
The compiler generates these accessors with the declared field types in mind;
they do not require application code to use Go's unrestricted `reflect`
package.

This is the foundation for model-driven behavior: declarations hold the
metadata, and small reusable tools interpret it. For exact declaration and
target rules, see [annotations](/reference/specifications/annotations). For
descriptor members and inheritance behavior, see
[introspection](/reference/specifications/introspection).
