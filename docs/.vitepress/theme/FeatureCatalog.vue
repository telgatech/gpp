<script setup>
import { onMounted, ref } from 'vue'

const sections = [
  {
    title: 'A whirlwind tour of what\'s possible',
    description: 'GPP adds a practical object model and stronger ways to structure application code, while still compiling to ordinary Go.',
    features: [
      { title: 'Classes', description: 'Keep related state and behavior together, with familiar fields, methods, and concise construction.', code: `class Person {\n    Name string\n\n    func Greet() string {\n        return "Hello " + this.Name\n    }\n}\n\nperson := Person(Name: "Ada")`, link: '/features/classes', linkText: 'Explore classes' },
      { title: 'Generic classes', description: 'Define a class once and use it with different value types through Go’s generic type system.', code: `class Box[T] {\n    Value T\n\n    func Get() T { return this.Value }\n}\n\nbox := Box[int](Value: 42)\nfmt.Println(box.Get())`, link: '/features/classes#generics', linkText: 'Explore generic classes' },
      { title: 'Generic methods', description: 'Give a class method its own type parameter so it can work with different value types.', code: `class Echo {\n    func Identity[T](value T) T {\n        return value\n    }\n}\n\necho := Echo()\nfmt.Println(echo.Identity[string]("hello"))`, link: '/features/classes#generics', linkText: 'Explore generic methods' },
      { title: 'Multiple inheritance', description: 'Inherit fields and methods from multiple base classes to compose reusable state and behavior into one type.', code: `class Greeter {\n    func Greet() string { return "hello" }\n}\nclass Logger {\n    func Log() string { return "logged" }\n}\nclass App : Greeter, Logger {}\n\napp := App()\nprintln(app.Greet(), app.Log())`, link: '/features/multiple-inheritance', linkText: 'Explore multiple inheritance' },
      { title: 'Polymorphism', description: 'Call behavior through a base type and let the concrete class provide the implementation.', code: `class Animal {\n    func Speak() string { return "unknown" }\n}\n\nclass Cat: Animal {\n    func Speak() string { return "meow" }\n}\n\nvar animal Animal = Cat()\nfmt.Println(animal.Speak()) // meow`, link: '/features/polymorphism', linkText: 'Explore polymorphism' },
      { title: 'Error fallback', description: 'Handle a function’s returned error at the call site with default values or another function call, including for multiple results.', code: `import "fmt"\n\nfunc Dimensions() (int, int, error) {\n    return 0, 0, fmt.Errorf("dimensions unavailable")\n}\n\nwidth, height := Dimensions() ?? 0, 1`, link: '/features/error-fallback', linkText: 'Explore error fallback' },
      { title: 'Exception handling', description: 'GPP turns unhandled errors into exceptions automatically so you can keep the success path readable.', code: `try {\n    data := os.ReadFile(path)\n    println(len(data))\n} catch os.PathError, os.SyscallError err {\n    println(err)\n} finally {\n    println("load finished")\n}`, link: '/features/exceptions', linkText: 'Explore exceptions' },
      { title: 'Overloaded methods', description: 'Give related operations one clear name; the compiler selects by argument count and static types.', code: `func Format(value int) string { return "number" }\nfunc Format(value string) string { return "text" }\n\nFormat(42)\nFormat("42")`, link: '/features/overloading', linkText: 'Explore overloading' },
      { title: 'Named arguments and defaults', description: 'Make calls self-documenting and keep common options at their declaration.', code: `func Greet(name string, punctuation string = "!") string {\n    return name + punctuation\n}\n\nGreet(name: "Ada")\nGreet(punctuation: "...", name: "Ada")`, link: '/features/named-arguments', linkText: 'Explore named calls' },
      { title: 'Extension methods', description: 'Add methods to any type without cluttering your code with free floating functions!', code: `extend string, []byte {\n    func IsEmpty() bool {\n        return len(this) == 0\n    }\n}\n\nif name.IsEmpty() { ... }`, link: '/features/extensions', linkText: 'Explore extension methods' },
      { title: 'Serialization', description: 'One annotation gives your classes typed JSON, YAML, and GOB encoders and decoders.', code: `class User @{encoding.Serializable} {\n    Id int\n    Name string\n}\n\njson := user.ToJSON()\nfromJSON := User.FromJSON(json)\nyaml := user.ToYAML()\nfromYAML := User.FromYAML(yaml)\ngob := user.ToGOB()\nfromGOB := User.FromGOB(gob)`, link: '/features/serialization', linkText: 'Explore serialization' },
      { title: 'Packages', description: 'Keep Go imports, functions, and types. A missing package declaration defaults to main, and source folders need not mirror package names.', code: `package demo.people\n\nimport "fmt"\n\nfunc Hello(name string) {\n    fmt.Println("Hello", name)\n}`, link: '/features/packages', linkText: 'Explore Go compatibility' },
      { title: 'Constructor methods', description: 'Overload init methods to control construction; positional and named arguments match initializer parameters.', code: `class User {\n    Name string\n    Age int\n\n    func init(name string) {\n        this.Name = name\n    }\n\n    func init(name string, age int) {\n        this.Name = name\n        this.Age = age\n    }\n}\n\nguest := User("Guest")\nada := User(age: 36, name: "Ada")`, link: '/features/classes#constructors-init', linkText: 'Explore constructors' },
      { title: 'Object construction', description: 'Easier instances of classes and named structs positionally or by field name, with compile-time field and arity checks.', code: `class Person { Name string; Age int }\nstruct User { Name string; Age int }\n\nperson := Person("Ada", 36)\nuser := User(Name: "Ada", Age: 36)`, link: '/features/classes#construct-values', linkText: 'Explore construction' },
      { title: 'Static methods', description: 'Put class-qualified constructors and helpers next to the type they create.', code: `class User {\n    Name string\n\n    static func Guest() *User {\n        return User(Name: "Guest")\n    }\n}\n\nguest := User.Guest()`, link: '/features/static-methods', linkText: 'Explore static methods' },
      { title: 'Anonymous structs', description: 'Freely return and receive anonymous data shapes without declaring another named struct or class.', code: `user := struct(\n    Name: "Ada",\n    Active: true,\n    Profile: struct(Role: "admin"),\n)\n\nfmt.Println(user.Profile.Role)`, link: '/features/structs', linkText: 'Explore structs' },
      { title: 'Typed slices and arrays', description: 'Build slices, nested slices, fixed arrays, and class collections with their types up front.', code: `numbers := int[1, 2, 3]\nmatrix := []int[[1, 2], [3, 4]]\nfixed := [3]int[10, 20, 30]\n\nclass Label {\n    Text string\n    func init(text string) { this.Text = text }\n}\nlabels := Label[("first"), ("second")]\n\nmore := numbers.Append(4)\nfiltered := more.Filter(value => value > 1)`, link: '/guide/language#typed-slices-and-arrays', linkText: 'Explore slices and arrays' },
      { title: 'Enums with metadata', description: 'Represent finite choices as validated scalar values with ordered members and names.', code: `enum Status string {\n    Draft\n    Published\n}\n\nstatus := Status.Published\nfmt.Println(status.value, status.name)`, link: '/features/enums', linkText: 'Explore enums' },
      { title: 'Lambdas', description: 'Write contextual expression or block lambdas that lower to Go function literals.', code: `activeNames := users\n    .Filter(user => user.Active)\n    .Map(user => user.Name)`, link: '/features/lambdas', linkText: 'Explore lambdas' },
      { title: 'Annotations and introspection', description: 'Abstract away repeating patterns to reduce code duplication with a touch of metaprogramming using annotations!', code: `annotation Table(name string) on class\n\nclass Employee @{Table("employees")} {\n    Name string\n}\n\ntable := Employee.class.annotations.find(Table)\nfmt.Println(table.args[0]) // employees`, link: '/features/annotations', linkText: 'Explore annotations' },
      { title: 'Regular expressions', description: 'Use the regexp(pattern) builtin to match regular expressions in values and switch cases.', code: `path := "/users/42"\nmatched := regexp(\`^/users/[0-9]+$\`).MatchString(path)\n\nswitch path {\ncase regexp(\`^/users/[0-9]+$\`):\n    println("user path")\n}`, link: '/features/regular-expressions', linkText: 'Explore regular expressions' },
      { title: 'String interpolation', description: 'Place expressions directly in strings and use formatting verbs when needed.', code: `message := "Hello {{user.Name}}"\nprice := "Total: {{price:.2f}}"`, link: '/features/interpolation', linkText: 'Explore interpolation' },
      { title: 'Explicit safe access', description: 'Opt in to zero-valued results when a nullable class pointer is nil.', code: `var user *Person\n\nname := user?.Name\ngreeting := user?.Greet()`, link: '/features/safe-access', linkText: 'Explore safe access' },
      { title: 'Comprehensive Operators', description: 'Use compound assignments, increment/decrement, bitwise operations, conditional assignment, and ternary expressions.', code: `count, mask, bits := 10, 1, 0\ncount += 2 // addition assignment\ncount -= 1 // subtraction assignment\ncount *= 2 // multiplication assignment\ncount %= 4 // remainder assignment\ncount++ // increment\ncount-- // decrement\nbits |= mask // bitwise OR\nbits <<= 1 // shift left\nbits &^= mask // clear bits\n\nclass Feature {}\nvar feature *Feature\nfeature ||= Feature() // assign only if nil\n\n// ternary expression\nreturn feature != nil ? "ready" : "missing"`, link: '/features/operators', linkText: 'Explore operators' }
    ]
  },
  {
    title: 'Build complete applications',
    description: 'Bundled GPP packages connect language features to familiar Go libraries.',
    features: [
      { title: 'Typed SQL queries', description: 'Get strongly typed results from ad hoc SQL using inline struct shapes, without defining a model for every query.', code: `users := db.Select[struct(ID: int64, Name: string)](\n    "SELECT id, name FROM users WHERE active = $1",\n    true,\n)\n\nuser := db.Get[struct(ID: int64, Name: string)](\n    "SELECT id, name FROM users WHERE id = $1",\n    42,\n)\n\ncount := db.Get[int64]("SELECT COUNT(*) FROM users")`, link: '/features/sql', linkText: 'Explore typed queries' },
      { title: 'ORM and SQL', description: 'Go beyond inline structs with models mapped to tables, complete with CRUD operations, transactions, and lifecycle hooks.', code: `class Employee : orm.Model @{orm.Table("employees")} {\n    Id int64 @{orm.Column("id"), orm.PK}\n    Name string @{orm.Column("name")}\n}\n\nemployee := Employee(42, "Ada")\nemployee.Insert(db)\nemployee = Employee.Find(db, employee.Id)`, link: '/features/orm', linkText: 'Explore the ORM' },
      { title: 'HTTP servers and routes', description: 'Build handlers with route annotations, request context, lifecycle hooks, and typed responses.', code: `class App : http.Server {\n    func Health(ctx *http.Context) error\n        @{http.GET("/health")} {\n        return ctx.JSON(struct(ok: true))\n    }\n}\n\nfunc main() {\n    App().Listen(8080)\n}`, link: '/features/http', linkText: 'Explore HTTP' },
      { title: 'Websockets', description: 'Adding a WebSocket endpoint is as simple as adding an http.WebSocket annotation to a handler.', code: `class App : http.Server {\n    func Echo(ctx *http.Context) error\n        @{http.WebSocket("/echo")} {\n        for {\n            var message string\n            ctx.Conn.Read(&message)\n            ctx.Conn.Write("echo: " + message)\n        }\n    }\n}`, link: '/guide/standard-library/http#websocket-handlers', linkText: 'Explore WebSockets' },
      { title: 'Scheduled jobs', description: 'Attach an interval or calendar schedule to a function, then start the scheduler when your application is ready.', code: `func Refresh(ctx context.Context) error\n    @{cron.Every(5 * time.Minute)} {\n    fmt.Println("refreshing cache")\n    return nil\n}\n\nfunc main() {\n    cron.Start()\n}`, link: '/guide/standard-library/cron', linkText: 'Explore cron' },
      { title: 'Durable work queue', tag: 'Experimental', description: 'Build queue-based apps with a simple push-and-drain API; message classes define the work, and the queue handles execution.', code: `class Report : queue.Message {\n    Build string\n\n    func _execute() error {\n        fmt.Println("publishing report for", this.Build)\n        return nil\n    }\n}\n\nq := queue.New()\nq.Push(Report(Priority: 10, Build: "v2.4.0"))\nq.Drain(30 * time.Second)`, link: '/guide/standard-library/exp-queue', linkText: 'Explore the queue' },
      { title: 'OpenAPI, Swagger, and OAuth', description: 'Generate API docs and serve Swagger UI; configure OAuth/OIDC providers on the server.', code: `class App : http.Server @{\n    http.Prefix("/api"), http.OpenAPI,\n    http.Swagger, http.OAuth(http.OAuthProvider.Google),\n} {}`, link: '/features/api-documentation', linkText: 'Explore API documentation' },
      { title: 'Suite-based tests', description: 'Organize tests into suites with setup, assertions, tags, and priority filters.', code: `class UserTest : test.Suite @{test.Tag("crud")} {\n    func Create() {\n        Equal("Ada", user.Name)\n    }\n}\n\ngpp test --tag crud .`, link: '/features/testing', linkText: 'Explore testing' },
      { title: 'Embedded assets', description: 'Embed directories as rooted fs.FS values or files as []byte using Go embed directives.', code: `embed (\n    Assets "public/"\n    Schema "schema.sql"\n)\n\nindex, err := fs.ReadFile(Assets, "index.html")`, link: '/features/embedded-assets', linkText: 'Explore embedding' },
      { title: 'Typed templates', description: 'Compile html/template declarations into typed execution functions, with layouts and dynamic execution.', code: `template Page(title string) {\n    <h1>{{.}}</h1>\n}\n\ntpl.Page(&output, "Hello, Ada")`, link: '/features/templates', linkText: 'Explore templates' },
      { title: 'External template reloads', description: 'Keep templates in .gpp.tpl files and reload valid edits during gpp run with gpp/http.Server.', code: `// page.gpp.tpl\ntemplate Page(name string) {\n    <h1>Hello, {{.}}</h1>\n}\n\ngpp run .`, link: '/features/template-reload', linkText: 'Explore template reloads' }
    ]
  },
  {
    title: 'Stay in the Go toolchain',
    description: 'GPP compiles to Go and brings project, editor, and source-level tools in one CLI.',
    features: [
      { title: 'Mixed Go and GPP builds', description: 'GPP can call handwritten Go, and Go packages can import generated GPP packages.', code: `// GPP calls a native.go function\ngpp run ./app\n\n// Go imports the generated package\ngo run ./consumer`, link: '/features/mixed-go', linkText: 'Explore Go interoperability' },
      { title: 'One project CLI', description: 'Create, build, run, test, and clean projects while generated Go stays in a hidden workspace.', code: `gpp init hello\ncd hello\ngpp run .\ngpp build -o ./hello .`, link: '/features/project-cli', linkText: 'Explore the CLI' },
      { title: 'Canonical formatter', description: 'Format source consistently, preserve comments, or use check mode in CI.', code: `gpp fmt ./...\ngpp fmt --check ./...\ngpp fmt --stdout main.gpp`, link: '/features/formatter', linkText: 'Explore formatting' },
      { title: 'Source documentation', description: 'Search GPP declarations and inspect source-level docs without opening generated Go.', code: `gpp doc gpp/http.Server\ngpp doc string.TrimSpace\ngpp doc --search template`, link: '/features/source-docs', linkText: 'Explore source docs' },
      { title: 'Language server and diagnostics', description: 'Get diagnostics, completion, hover, navigation, rename, symbols, formatting, and signature help in your editor.', code: `gpp lsp\n\n// feedback uses open .gpp source;\n// no Go backend build on every edit`, link: '/features/language-server', linkText: 'Explore editor support' }
    ]
  }
]

const highlightedCode = ref(new Map())

onMounted(async () => {
  try {
    const { createHighlighter } = await import('shiki')
    const highlighter = await createHighlighter({
      themes: ['github-light', 'github-dark'],
      langs: ['go'],
    })
    highlightedCode.value = new Map(
      sections.flatMap(section => section.features.map(feature => [
        feature.code,
        highlighter.codeToHtml(feature.code, {
          lang: 'go',
          themes: { light: 'github-light', dark: 'github-dark' },
          defaultColor: false,
        }),
      ])),
    )
  } catch (error) {
    console.warn('Could not apply Go syntax highlighting to feature cards.', error)
  }
})
</script>

<template>
  <main class="feature-catalog">

    <section v-for="section in sections" :key="section.title" class="feature-section" :aria-label="section.title">
      <div class="section-heading">
        <h3>{{ section.title }}</h3>
        <p>{{ section.description }}</p>
      </div>
      <div class="feature-grid">
        <article v-for="feature in section.features" :key="feature.title" class="feature-card">
          <div class="feature-copy">
            <div class="feature-title-row">
              <h4>{{ feature.title }}</h4>
              <span v-if="feature.tag" class="feature-status">{{ feature.tag }}</span>
            </div>
            <p>{{ feature.description }}</p>
          </div>
          <div v-if="highlightedCode.has(feature.code)" class="feature-code" v-html="highlightedCode.get(feature.code)"></div>
          <pre v-else><code>{{ feature.code }}</code></pre>
          <a v-if="feature.link" :href="feature.link">{{ feature.linkText || 'Learn more' }} <span aria-hidden="true">→</span></a>
        </article>
      </div>
    </section>

  </main>
</template>

<style scoped>
.feature-catalog { max-width: 1152px; margin: 0 auto; padding: 5rem 0; }
.catalog-heading { max-width: 720px; margin: 0 auto 64px; text-align: center; }
.catalog-heading h2 { margin: 0; color: var(--vp-c-text-1); font-size: clamp(1.8rem, 4vw, 2.65rem); font-weight: 700; letter-spacing: -0.035em; line-height: 1.15; }
.catalog-heading p, .section-heading p, .feature-copy p, .catalog-footer p { color: var(--vp-c-text-2); line-height: 1.65; }
.catalog-heading p { margin: 16px 0 0; font-size: 1.08rem; }
.feature-section + .feature-section { margin-top: 72px; }
.section-heading { xdisplay: grid; grid-template-columns: minmax(220px, 0.7fr) minmax(280px, 1fr); align-items: baseline; gap: 24px; margin-bottom: 24px; padding-bottom: 16px; border-bottom: 1px solid var(--vp-c-divider); }
.section-heading h3 { margin: 0; color: var(--vp-c-text-1); font-size: 1.55rem; font-weight: 650; letter-spacing: -0.025em; }
.section-heading h3 { margin: 0; color: var(--vp-c-text-1); font-size: clamp(1.8rem, 4vw, 2.65rem); font-weight: 700; letter-spacing: -0.035em; line-height: 1.15; }
.section-heading p { margin: 0; }
.feature-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; }
.feature-card { display: flex; min-width: 0; flex-direction: column; padding: 24px; border: 1px solid var(--vp-c-divider); border-radius: 14px; background: var(--vp-c-bg); transition: border-color 160ms ease, box-shadow 160ms ease, transform 160ms ease; }
.feature-card:hover { transform: translateY(-2px); border-color: var(--vp-c-brand-1); box-shadow: 0 10px 28px rgb(20 36 72 / 8%); }
.feature-title-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.feature-copy h4 { margin: 0; color: var(--vp-c-text-1); font-size: 1.13rem; font-weight: 650; letter-spacing: -0.01em; }
.feature-status { flex: 0 0 auto; padding: 3px 8px; border: 1px solid var(--vp-c-brand-soft); border-radius: 999px; background: var(--vp-c-bg-soft); color: var(--vp-c-brand-1); font-size: 0.72rem; font-weight: 650; line-height: 1.2; }
.feature-copy p { min-height: 3.2em; margin: 9px 0 18px; font-size: 0.94rem; }
.feature-card pre { flex: 1; overflow-x: auto; margin: 0 0 16px; padding: 16px; border: 1px solid var(--vp-c-divider); border-radius: 9px; background: var(--vp-c-bg-soft); color: var(--vp-c-text-1); font-size: 0.82rem; line-height: 1.6; tab-size: 4; }
.feature-card code { font-family: var(--vp-font-family-mono); }
.feature-card .feature-code { display: flex; min-width: 0; flex: 1; }
.feature-card :deep(.feature-code pre) { width: 100%; overflow-x: auto; margin: 0 0 16px; padding: 16px; border: 1px solid var(--vp-c-divider); border-radius: 9px; background: var(--vp-c-bg-soft); color: var(--vp-c-text-1); font-size: 0.82rem; line-height: 1.6; tab-size: 4; }
.feature-card :deep(.feature-code code) { font-family: var(--vp-font-family-mono); }
.feature-card :deep(.feature-code .shiki) { background: var(--vp-c-bg-soft) !important; }
.feature-card :deep(.feature-code .shiki span) { color: var(--shiki-light); }
.feature-card a { align-self: flex-start; color: var(--vp-c-brand-1); font-size: 0.92rem; font-weight: 600; text-decoration: none; }
.feature-card a:hover { text-decoration: underline; text-underline-offset: 3px; }
@media (max-width: 760px) {
  .feature-catalog { padding: 40px 20px 56px; }
  .catalog-heading { margin-bottom: 48px; text-align: left; }
  .section-heading { display: block; }
  .section-heading p { margin-top: 8px; }
  .feature-grid { grid-template-columns: 1fr; gap: 14px; }
  .feature-copy p { min-height: 0; }
}
@media (prefers-reduced-motion: reduce) { .feature-card { transition: none; } }
</style>
