---
name: Go++
filename: learngpp.gpp
contributors:
  - ["Go++ contributors", "https://github.com/telgatech/gpp"]
---

Go++ builds on Go and assumes you already know Go. It adds language and
application features while keeping Go packages, libraries, and tools. Go++
source compiles to Go, so teams can adopt the features gradually.

Install Go++ with `go install github.com/telgatech/gpp@main`. Save this example
as `learngpp.gpp` and run it with `gpp run learngpp.gpp`.

    // Go++ source looks like Go. Packages, imports, functions, control flow,
    // and Go libraries work as you already know them.
    package main

    import (
        "fmt"
        "os"
        "strconv"

        "gpp/encoding"
    )

    // Go++ classes group fields and methods. `this` is the current instance.
    class Person {
        Name string
        Age int

        func Greeting() string {
            return "Hello, " + this.Name
        }
    }

    // Construct with positional values in field order, or name them in any
    // order. A class constructor returns a pointer to the instance.
    func NewPerson() *Person {
        return Person(Age: 37, Name: "Ada")
    }

    // Static methods live on a class and need no instance. `self` is the
    // calling class type, while `this` is an instance receiver.
    class EntityFactory {
        static func New() *self {
            return new(self)
        }
    }

    class Account : EntityFactory {
        Email string
    }

    // Inheritance can compose fields and behavior from multiple classes.
    class Named {
        Name string

        func Label() string {
            return this.Name
        }
    }

    class Identified {
        ID int
    }

    class Customer : Named, Identified {
        func Label() string {
            return fmt.Sprintf("%s (#%d)", this.Name, this.ID)
        }
    }

    // A derived instance can be used through a base type; overridden methods
    // dispatch to the implementation on the actual value.
    func Describe(value Named) string {
        return value.Label()
    }

    // Functions and methods can be overloaded by argument count or types.
    func Format(value int) string {
        return fmt.Sprintf("integer: %d", value)
    }

    func Format(value string) string {
        return "text: " + value
    }

    // Named arguments clarify call sites. Trailing parameters can have
    // defaults, and a call can use either all named or all positional values.
    func Greet(name string, punctuation string = "!") string {
        return name + punctuation
    }

    // Generic functions and classes use Go-style type parameters.
    func First[T](values []T) T {
        return values[0]
    }

    class Stack[T] {
        Items []T

        func Push(value T) {
            this.Items = append(this.Items, value)
        }
    }

    // Type parameters can use Go constraints such as `comparable`.
    class Index[K comparable, V] {
        Entries map[K]V

        func Put(key K, value V) {
            if this.Entries == nil {
                this.Entries = make(map[K]V)
            }
            this.Entries[key] = value
        }
    }

    // Anonymous structs are statically checked data shapes, not maps.
    func LoadUser() struct {
        return struct(Name: "Ada", Age: 37)
    }

    // Enums define named values with a specific underlying Go type.
    enum Status int {
        Pending
        Active
        Disabled
    }

    // Define an extension method for an existing type. Extension methods are
    // resolved at compile time; they do not modify the original Go type.
    extend string {
        func IsBlank() bool {
            return this.TrimSpace() == ""
        }
    }

    // A lambda is a short function value. The collection operation provides
    // the context used to infer its argument type.
    func DoubleAll(values []int) []int {
        positive := values.Filter(value => value > 0)
        return positive.Map(value => value * 2)
    }

    // Safe access returns a member's zero value for a nil class pointer.
    class Contact {
        Name string
    }

    // `??` lazily supplies a fallback for an error result or safe nil access.
    func ConfiguredPort(raw string) int {
        return strconv.Atoi(raw) ?? 8080
    }

    // Go++ keeps Go's error values and function signatures. An uncaptured,
    // non-nil trailing error is raised and propagates to a matching catch.
    func ReadGreeting(path string) string {
        // This call raises *os.PathError if reading fails.
        data := os.ReadFile(path)
        return string(data)
    }

    // Capture the error to handle it as an ordinary Go value instead.
    func ReadGreetingGoStyle(path string) (string, error) {
        data, err := os.ReadFile(path)
        if err != nil {
            return "", err
        }
        return string(data), nil
    }

    // `throw` raises an error value explicitly.
    func RequireName(name string) {
        if name == "" {
            throw fmt.Errorf("name is required")
        }
    }

    // An annotation declares typed metadata. A library or compiler feature
    // decides what that metadata means.
    annotation Table(name string) on class

    class SchemaExample @{Table("customers")} {
        Name string
    }

    // Serialization helpers are generated for classes marked Serializable.
    class Profile @{encoding.Serializable} {
        Name string
        Age int
    }

    func main() {
        // Double braces interpolate values in strings.
        name := "Go++"
        fmt.Println("Welcome to {{name}}")
        fmt.Println("Total: {{42.5:.2f}}")

        person := NewPerson()
        fmt.Println(person.Greeting())
        fmt.Println(Greet(name: "Ada"))
        fmt.Println(Greet(punctuation: ".", name: "Hello"))
        fmt.Println(Format(42), Format("example"))

        // A single-inheritance method can be reused through a base class.
        customer := Customer(
            Named.Name: "Ada",
            Identified.ID: 7,
        )
        fmt.Println(Describe(customer))

        // `self` lets a base-class factory create the calling class.
        account := Account.New()
        account.Email = "ada@example.com"

        numbers := Stack[int](Items: []int{})
        numbers.Push(10)
        numbers.Push(20)
        fmt.Println(First[int](numbers.Items), DoubleAll(numbers.Items))
        fmt.Println(numbers.Items.Any(value => value == 20))

        index := Index[string, int](Entries: map[string]int{})
        index.Put("users", 1)

        user := LoadUser()
        fmt.Println(user.Name, user.Age)

        status := Status.Active
        fmt.Println(status.name, status.value)

        var contact *Contact
        contactName := contact?.Name ?? "Guest"
        fmt.Println(contactName, "   ".IsBlank())

        // A matching typed catch handles a specific error. Other errors keep
        // propagating. `finally` runs after success or a handled exception.
        try {
            fmt.Println(ReadGreeting("greeting.txt"))
        } catch *os.PathError e {
            fmt.Println("could not read", e.Path, e)
        } catch e {
            fmt.Println("unexpected error:", e)
        } finally {
            fmt.Println("read attempt finished")
        }

        fmt.Println("port:", ConfiguredPort("not a port"))

        // Inspect class annotations and their arguments at runtime.
        table := SchemaExample.class.annotations.find(Table)
        fmt.Println("table:", table.args[0])

        // Generated methods use Go encoders and return ordinary typed values.
        profile := Profile(Name: "Ada", Age: 37)
        json := profile.ToJSON()
        decoded := Profile.FromJSON(json)
        fmt.Println(string(json), decoded.Name)
    }

    // A regex string extension compiles with Go's regexp package underneath.
    func ValidUsername(name string) bool {
        pattern := "^[a-z][a-z0-9_]+$".CompileRegex()
        return pattern.MatchString(name)
    }

    // `atExit()` runs once when main returns. Use it for process-wide cleanup.
    func atExit() {
        fmt.Println("shutting down")
    }

    // A Go++ project can mix `.go` and `.gpp` files. Functions in native Go
    // files are available to Go++ source in the same package:
    //
    // // native.go
    // func greeting(name string) string { return "Hello, " + name }
    //
    // // main.gpp
    // fmt.Println(greeting("Ada"))
    //
    // HTTP classes declare routes with annotations. The same package supports
    // request binding, middleware, WebSockets, OpenAPI, Swagger, and OAuth:
    //
    // class App : http.Server @{http.OpenAPI, http.Swagger} {
    //     func Hello(ctx *http.Context) error @{http.GET("/hello")} {
    //         return ctx.JSON(struct(Greeting: "Hello"))
    //     }
    // }
    //
    // ORM models use annotations for tables and columns. Their instance and
    // class methods provide insert, update, delete, and model-based queries:
    //
    // employee := Employee.Find(db, employeeID)
    // employee.Update(db)
    // employees := Employee.Select(db, "name LIKE $1", "A%")
    //
    // Generic SQL queries return scalars, records, models, or slices:
    // count := db.Get[int64]("SELECT COUNT(*) FROM employees")
    // events := db.Select[struct(ID: int64, Name: string)](
    //     "SELECT id, name FROM events",
    // )
    //
    // Templates compile into typed renderers and use html/template escaping:
    // template Welcome(name string) {
    //     <h1>Hello, {{.}}</h1>
    // }
    // tpl.Welcome(&output, "Ada")
    //
    // Embed files and directories using Go's filesystem interfaces:
    // embed ( Assets "public/" )
    // index := fs.ReadFile(Assets, "index.html")
    //
    // Schedule jobs with annotations, or test suites with gpp/test:
    // func Refresh(ctx context.Context) error @{cron.Every(time.Minute)} {
    //     return nil
    // }
    //
    // class UserTest : test.Suite {
    //     func Create() { Equal("Ada", User(Name: "Ada").Name) }
    // }

## Further Reading

Go++ includes application libraries for HTTP servers, routing, WebSockets,
OpenAPI, OAuth, ORM and typed SQL, templates, embedded assets, cron jobs,
serialization, and suite-based testing. Its tools include project build and
run commands, dependency and import management, a formatter, source
documentation, and a language server. During development, `gpp run` can watch
external `.gpp.tpl` templates and reload them after edits.

The [feature index](https://github.com/telgatech/gpp/tree/main/docs/features)
links to guides for language features, application libraries, and tools. Use
`gpp build`, `gpp run`, `gpp test`, and `gpp fmt` to build, run, test, and
format a project. `gpp doc`, `gpp fiximports`, `gpp doctor`, and `gpp lsp`
help explore APIs, maintain imports, inspect the environment, and connect an
editor. See the
[examples](https://github.com/telgatech/gpp/tree/main/examples) for runnable
programs, and the
[specifications](https://github.com/telgatech/gpp/tree/main/spec) for the
language rules.
