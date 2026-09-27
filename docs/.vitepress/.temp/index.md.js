import { ssrRenderAttrs, ssrRenderList, ssrRenderAttr, ssrInterpolate, ssrRenderComponent } from "vue/server-renderer";
import { mergeProps, useSSRContext } from "vue";
import { _ as _export_sfc } from "./plugin-vue_export-helper.1tPrXgE0.js";
const _sfc_main$1 = {
  __name: "FeatureCatalog",
  __ssrInlineRender: true,
  setup(__props) {
    const sections = [
      {
        title: "The changes that matter most",
        description: "Go++ adds a practical object model and stronger ways to structure application code, while still compiling to ordinary Go.",
        features: [
          { title: "Classes", description: "Keep related state and behavior together, with familiar fields, methods, and concise construction.", code: `class Person {
    Name string

    func Greet() string {
        return "Hello " + this.Name
    }
}

person := Person(Name: "Ada")`, link: "/features/classes", linkText: "Explore classes" },
          { title: "Polymorphism", description: "Call behavior through a base type and let the concrete class provide the implementation.", code: `class Person {
    func Describe() string { return "person" }
}

class Employee : Person {
    func Describe() string { return "employee" }
}

func DescribePerson(person Person) string {
    return person.Describe()
}

message := DescribePerson(Employee())`, link: "/features/polymorphism", linkText: "Explore polymorphism" },
          { title: "Compile-time overloading", description: "Give related operations one clear name; the compiler selects by argument count and static types.", code: `func Format(value int) string { return "number" }
func Format(value string) string { return "text" }

Format(42)
Format("42")`, link: "/features/overloading", linkText: "Explore overloading" },
          { title: "Multiple inheritance", description: "Compose a class from several parents and qualify a member when parent APIs overlap.", code: `class Named { Name string }
class Timestamped { CreatedAt time.Time }

class User : Named, Timestamped {}`, link: "/features/multiple-inheritance", linkText: "Explore multiple inheritance" },
          { title: "Exception handling", description: "Keep the success path readable with try, typed catches, throw, and finally.", code: `try {
    data := os.ReadFile(path)
    use(data)
} catch *os.PathError e {
    log(e.Path)
}`, link: "/features/exceptions", linkText: "Explore exceptions" },
          { title: "Extension methods", description: "Add compile-time method syntax to Go and Go++ types without changing those types.", code: `extend string {
    func IsBlank() bool {
        return this.TrimSpace() == ""
    }
}

if name.IsBlank() { ... }`, link: "/features/extensions", linkText: "Explore extension methods" },
          { title: "Serialization", description: "Generate JSON, YAML, and GOB helpers for classes you choose to serialize.", code: `class User @{encoding.Serializable} {
    Id int
    Name string
}

encoded := user.ToJSON()
copy := User.FromJSON(encoded)`, link: "/features/serialization", linkText: "Explore serialization" }
        ]
      },
      {
        title: "Less ceremony in everyday code",
        description: "Small language features compose with normal Go types and functions.",
        features: [
          { title: "Packages and Go compatibility", description: "Keep Go imports, functions, and types. A missing package declaration defaults to main, and source folders need not mirror package names.", code: `package demo.people

import "fmt"

func Hello(name string) {
    fmt.Println("Hello", name)
}`, link: "/features/packages", linkText: "Explore Go compatibility" },
          { title: "Compact construction", description: "Create values positionally or name fields, with compile-time field and arity checks.", code: `positional := Person("Ada", 36)
named := Person(Name: "Ada", Age: 36)`, link: "/features/construction", linkText: "Explore construction" },
          { title: "Static methods and factories", description: "Put class-qualified constructors and helpers next to the type they create.", code: `class User {
    Name string

    static func Guest() User {
        return User(Name: "Guest")
    }
}

guest := User.Guest()`, link: "/features/static-methods", linkText: "Explore static methods" },
          { title: "Named arguments and defaults", description: "Make calls self-documenting and keep common options at their declaration.", code: `func Greet(name string, punctuation string = "!") string {
    return name + punctuation
}

Greet(name: "Ada")
Greet(punctuation: "...", name: "Ada")`, link: "/features/named-arguments", linkText: "Explore named calls" },
          { title: "Structural records", description: "Use inferred, anonymous data shapes without declaring another named class.", code: `user := record(
    Name: "Ada",
    Active: true,
    Profile: record(Role: "admin"),
)

fmt.Println(user.Profile.Role)`, link: "/features/records", linkText: "Explore records" },
          { title: "Enums with metadata", description: "Represent finite choices as validated scalar values with ordered members and names.", code: `enum Status string {
    Draft
    Published
}

status := Status.Published
fmt.Println(status.value, status.name)`, link: "/features/enums", linkText: "Explore enums" },
          { title: "String interpolation", description: "Place expressions directly in strings and use formatting verbs when needed.", code: `message := "Hello {{user.Name}}"
price := "Total: {{price:.2f}}"`, link: "/features/interpolation", linkText: "Explore interpolation" },
          { title: "Lambdas", description: "Write contextual expression or block lambdas that lower to Go function literals.", code: `activeNames := users
    .Filter(user => user.Active)
    .Map(user => user.Name)`, link: "/features/lambdas", linkText: "Explore lambdas" },
          { title: "Regular expressions", description: "Compile patterns explicitly and use Go regexp methods, with ordinary Go error handling for invalid patterns.", code: `pattern := "^[a-z]+$"
re := pattern.CompileRegex()
matched := re.MatchString(username)`, link: "/features/regular-expressions", linkText: "Explore regular expressions" },
          { title: "Explicit safe access", description: "Opt in to zero-valued results when a nullable class pointer is nil.", code: `var user *Person

name := user?.Name
greeting := user?.Greet()`, link: "/features/safe-access", linkText: "Explore safe access" },
          { title: "Lazy error fallback", description: "Use ?? with error-returning calls or nullable values; evaluate the fallback only when needed.", code: `port := strconv.Atoi(rawPort) ?? 8080
user := FindUser(id) ?? GuestUser()`, link: "/features/error-fallback", linkText: "Explore lazy fallback" },
          { title: "Annotations and introspection", description: "Declare typed metadata, validate targets, and inspect generated class descriptors at runtime.", code: `annotation Table(name string) on class

class Employee @{Table("employees")} {
    Name string
}

fmt.Println(Employee.class.name)`, link: "/features/metadata", linkText: "Explore metadata" }
        ]
      },
      {
        title: "Build complete applications",
        description: "Bundled Go++ packages connect language features to familiar Go libraries.",
        features: [
          { title: "Typed templates", description: "Compile html/template declarations into typed execution functions, with layouts and dynamic execution.", code: `template Page(title string) {
    <h1>{{.}}</h1>
}

tpl.Page(&output, "Hello, Ada")`, link: "/features/templates", linkText: "Explore templates" },
          { title: "Embedded assets", description: "Embed directories as rooted fs.FS values or files as []byte using Go embed directives.", code: `embed (
    Assets "public/"
    Schema "schema.sql"
)

index, err := fs.ReadFile(Assets, "index.html")`, link: "/features/embedded-assets", linkText: "Explore embedding" },
          { title: "HTTP servers and routes", description: "Build handlers with route annotations, request context, lifecycle hooks, and typed responses.", code: `class App : http.Server {
    func Health(ctx *http.Context) error
        @{http.GET("/health")} {
        return ctx.JSON(record(ok: true))
    }
}`, link: "/features/http", linkText: "Explore HTTP" },
          { title: "OpenAPI, Swagger, and OAuth", description: "Generate API docs and serve Swagger UI; configure OAuth/OIDC providers on the server.", code: `class App : http.Server @{
    http.Prefix("/api"), http.OpenAPI,
    http.Swagger, http.OAuth(http.OAuthProvider.Google),
} {}`, link: "/features/api-documentation", linkText: "Explore API documentation" },
          { title: "ORM and SQL", description: "Map annotated models to SQL with CRUD operations, transactions, and lifecycle hooks.", code: `class Employee : orm.Model @{orm.Table("employees")} {
    Name string @{orm.Column("name")}
}

db.Insert(&employee)
db.Get(&found, "id = $1", employee.Id)`, link: "/features/orm", linkText: "Explore the ORM" },
          { title: "Suite-based tests", description: "Organize tests into suites with setup, assertions, tags, and priority filters.", code: `class UserTest : test.Suite @{test.Tag("crud")} {
    func Create() {
        Equal("Ada", user.Name)
    }
}

gpp test --tag crud .`, link: "/features/testing", linkText: "Explore testing" },
          { title: "External template reloads", description: "Keep templates in .gpp.tpl files and reload valid edits during gpp run with gpp/http.Server.", code: `// page.gpp.tpl
template Page(name string) {
    <h1>Hello, {{.}}</h1>
}

gpp run .`, link: "/features/template-reload", linkText: "Explore template reloads" }
        ]
      },
      {
        title: "Stay in the Go toolchain",
        description: "Go++ compiles to Go and brings project, editor, and source-level tools in one CLI.",
        features: [
          { title: "Mixed Go and Go++ builds", description: "Go++ can call handwritten Go, and Go packages can import generated Go++ packages.", code: `// Go++ calls a native.go function
gpp run ./app

// Go imports the generated package
go run ./consumer`, link: "/features/mixed-go", linkText: "Explore Go interoperability" },
          { title: "One project CLI", description: "Create, build, run, test, and clean projects while generated Go stays in a hidden workspace.", code: `gpp init hello
cd hello
gpp run .
gpp build -o ./hello .`, link: "/features/project-cli", linkText: "Explore the CLI" },
          { title: "Canonical formatter", description: "Format source consistently, preserve comments, or use check mode in CI.", code: `gpp fmt ./...
gpp fmt --check ./...
gpp fmt --stdout main.gpp`, link: "/features/formatter", linkText: "Explore formatting" },
          { title: "Source documentation", description: "Search Go++ declarations and inspect source-level docs without opening generated Go.", code: `gpp doc gpp/http.Server
gpp doc string.TrimSpace
gpp doc --search template`, link: "/features/source-docs", linkText: "Explore source docs" },
          { title: "Language server and diagnostics", description: "Get diagnostics, completion, hover, navigation, rename, symbols, formatting, and signature help in your editor.", code: `gpp lsp

// feedback uses open .gpp source;
// no Go backend build on every edit`, link: "/features/language-server", linkText: "Explore editor support" }
        ]
      }
    ];
    return (_ctx, _push, _parent, _attrs) => {
      _push(`<main${ssrRenderAttrs(mergeProps({ class: "feature-catalog" }, _attrs))} data-v-6637fd20><!--[-->`);
      ssrRenderList(sections, (section) => {
        _push(`<section class="feature-section"${ssrRenderAttr("aria-label", section.title)} data-v-6637fd20><div class="section-heading" data-v-6637fd20><h3 data-v-6637fd20>${ssrInterpolate(section.title)}</h3><p data-v-6637fd20>${ssrInterpolate(section.description)}</p></div><div class="feature-grid" data-v-6637fd20><!--[-->`);
        ssrRenderList(section.features, (feature) => {
          _push(`<article class="feature-card" data-v-6637fd20><div class="feature-copy" data-v-6637fd20><h4 data-v-6637fd20>${ssrInterpolate(feature.title)}</h4><p data-v-6637fd20>${ssrInterpolate(feature.description)}</p></div><pre data-v-6637fd20><code data-v-6637fd20>${ssrInterpolate(feature.code)}</code></pre>`);
          if (feature.link) {
            _push(`<a${ssrRenderAttr("href", feature.link)} data-v-6637fd20>${ssrInterpolate(feature.linkText || "Learn more")} <span aria-hidden="true" data-v-6637fd20>→</span></a>`);
          } else {
            _push(`<!---->`);
          }
          _push(`</article>`);
        });
        _push(`<!--]--></div></section>`);
      });
      _push(`<!--]--><footer class="catalog-footer" data-v-6637fd20><p data-v-6637fd20>Go++ source becomes ordinary Go. Keep your existing packages, tools, and deployment flow.</p><a href="/guide/getting-started" data-v-6637fd20>Build your first Go++ project <span aria-hidden="true" data-v-6637fd20>→</span></a></footer></main>`);
    };
  }
};
const _sfc_setup$1 = _sfc_main$1.setup;
_sfc_main$1.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add(".vitepress/theme/FeatureCatalog.vue");
  return _sfc_setup$1 ? _sfc_setup$1(props, ctx) : void 0;
};
const FeatureCatalog = /* @__PURE__ */ _export_sfc(_sfc_main$1, [["__scopeId", "data-v-6637fd20"]]);
const __pageData = JSON.parse(`{"title":"","description":"","frontmatter":{"layout":"home","hero":{"text":"Go. With superpowers!","tagline":"Everything you love about Go, only now with classes, polymorphism, overloading, and multiple inheritance, exception handling, extension methods, built-in serialization, annotations, records and more! Everything you've always wanted but were too afraid to ask!","image":{"src":"/go-gopher-superman.png","alt":"Go's blue gopher mascot flying upward with a red cape"},"actions":[{"theme":"brand","text":"Explore the core changes","link":"/features/classes"},{"theme":"alt","text":"Build your first project","link":"/guide/getting-started"}]}},"headers":[],"relativePath":"index.md","filePath":"index.md","lastUpdated":null}`);
const __default__ = { name: "index.md" };
const _sfc_main = /* @__PURE__ */ Object.assign(__default__, {
  __ssrInlineRender: true,
  setup(__props) {
    return (_ctx, _push, _parent, _attrs) => {
      _push(`<div${ssrRenderAttrs(_attrs)}>`);
      _push(ssrRenderComponent(FeatureCatalog, null, null, _parent));
      _push(`</div>`);
    };
  }
});
const _sfc_setup = _sfc_main.setup;
_sfc_main.setup = (props, ctx) => {
  const ssrContext = useSSRContext();
  (ssrContext.modules || (ssrContext.modules = /* @__PURE__ */ new Set())).add("index.md");
  return _sfc_setup ? _sfc_setup(props, ctx) : void 0;
};
export {
  __pageData,
  _sfc_main as default
};
