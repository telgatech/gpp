<script setup>
const sections = [
  {
    title: 'The changes that matter most',
    description: 'Go++ adds a practical object model and stronger ways to structure application code, while still compiling to ordinary Go.',
    features: [
      { title: 'Classes', description: 'Keep related state and behavior together, with familiar fields, methods, and concise construction.', code: `class Person {\n    Name string\n\n    func Greet() string {\n        return "Hello " + this.Name\n    }\n}\n\nperson := Person(Name: "Ada")`, link: '/features/classes', linkText: 'Explore classes' },
      { title: 'Multiple inheritance', description: 'Build a shape from reusable state and behavior, then call methods inherited from both parents.', code: `class Positioned {\n    X float64\n    Y float64\n    func Move(dx, dy float64) {\n        this.X += dx\n        this.Y += dy\n    }\n}\nclass Colored {\n    Color string\n    func SetColor(color string) {\n        this.Color = color\n    }\n}\nclass Circle : Positioned, Colored { Radius float64 }\n\ncircle.Move(5, 2)\ncircle.SetColor("blue")`, link: '/features/multiple-inheritance', linkText: 'Explore multiple inheritance' },
      { title: 'Polymorphism', description: 'Call behavior through a base type and let the concrete class provide the implementation.', code: `class Person {\n    func Describe() string { return "person" }\n}\n\nclass Employee : Person {\n    func Describe() string { return "employee" }\n}\n\nfunc DescribePerson(person Person) string {\n    return person.Describe()\n}\n\nmessage := DescribePerson(Employee())`, link: '/features/polymorphism', linkText: 'Explore polymorphism' },
      { title: 'Compile-time overloading', description: 'Give related operations one clear name; the compiler selects by argument count and static types.', code: `func Format(value int) string { return "number" }\nfunc Format(value string) string { return "text" }\n\nFormat(42)\nFormat("42")`, link: '/features/overloading', linkText: 'Explore overloading' },
      { title: 'Exception handling', description: 'Keep the success path readable with try, typed catches, throw, and finally.', code: `try {\n    data := os.ReadFile(path)\n    use(data)\n} catch *os.PathError e {\n    log(e.Path)\n}`, link: '/features/exceptions', linkText: 'Explore exceptions' },
      { title: 'Extension methods', description: 'Add compile-time method syntax to Go and Go++ types without changing those types.', code: `extend string {\n    func IsBlank() bool {\n        return this.TrimSpace() == ""\n    }\n}\n\nif name.IsBlank() { ... }`, link: '/features/extensions', linkText: 'Explore extension methods' },
      { title: 'Serialization', description: 'Generate JSON, YAML, and GOB helpers for classes you choose to serialize.', code: `class User @{encoding.Serializable} {\n    Id int\n    Name string\n}\n\nencoded := user.ToJSON()\ncopy := User.FromJSON(encoded)`, link: '/features/serialization', linkText: 'Explore serialization' },
    ]
  },
  {
    title: 'Less ceremony in everyday code',
    description: 'Small language features compose with normal Go types and functions.',
    features: [
      { title: 'Packages and Go compatibility', description: 'Keep Go imports, functions, and types. A missing package declaration defaults to main, and source folders need not mirror package names.', code: `package demo.people\n\nimport "fmt"\n\nfunc Hello(name string) {\n    fmt.Println("Hello", name)\n}`, link: '/features/packages', linkText: 'Explore Go compatibility' },
      { title: 'Compact construction', description: 'Create values positionally or name fields, with compile-time field and arity checks.', code: `positional := Person("Ada", 36)\nnamed := Person(Name: "Ada", Age: 36)`, link: '/features/classes#construct-values', linkText: 'Explore construction' },
      { title: 'Class constructor hook', description: 'Planned init() lifecycle hook to normalize or validate a class after constructor fields are assigned.', code: `class Person {\n    Name string\n\n    func init() {\n        this.Name = strings.TrimSpace(this.Name)\n    }\n}\n\nperson := Person(" Ada ")`, tag: 'Planned', link: '/features/classes#constructor-hook-init-planned', linkText: 'Explore planned constructors' },
      { title: 'Static methods and factories', description: 'Put class-qualified constructors and helpers next to the type they create.', code: `class User {\n    Name string\n\n    static func Guest() User {\n        return User(Name: "Guest")\n    }\n}\n\nguest := User.Guest()`, link: '/features/static-methods', linkText: 'Explore static methods' },
      { title: 'Named arguments and defaults', description: 'Make calls self-documenting and keep common options at their declaration.', code: `func Greet(name string, punctuation string = "!") string {\n    return name + punctuation\n}\n\nGreet(name: "Ada")\nGreet(punctuation: "...", name: "Ada")`, link: '/features/named-arguments', linkText: 'Explore named calls' },
      { title: 'Structural records', description: 'Use inferred, anonymous data shapes without declaring another named class.', code: `user := record(\n    Name: "Ada",\n    Active: true,\n    Profile: record(Role: "admin"),\n)\n\nfmt.Println(user.Profile.Role)`, link: '/features/records', linkText: 'Explore records' },
      { title: 'Go assignment operators', description: 'Keep familiar compound assignments and increment/decrement statements in Go++ code.', code: `count := 0\ncount += 2\ncount++\ncount--`, link: '/guide/language#go-compatible-assignment-operators', linkText: 'See language syntax' },
      { title: 'Enums with metadata', description: 'Represent finite choices as validated scalar values with ordered members and names.', code: `enum Status string {\n    Draft\n    Published\n}\n\nstatus := Status.Published\nfmt.Println(status.value, status.name)`, link: '/features/enums', linkText: 'Explore enums' },
      { title: 'String interpolation', description: 'Place expressions directly in strings and use formatting verbs when needed.', code: `message := "Hello {{user.Name}}"\nprice := "Total: {{price:.2f}}"`, link: '/features/interpolation', linkText: 'Explore interpolation' },
      { title: 'Lambdas', description: 'Write contextual expression or block lambdas that lower to Go function literals.', code: `activeNames := users\n    .Filter(user => user.Active)\n    .Map(user => user.Name)`, link: '/features/lambdas', linkText: 'Explore lambdas' },
      { title: 'Regular expressions', description: 'Compile patterns explicitly and use Go regexp methods, with ordinary Go error handling for invalid patterns.', code: `pattern := "^[a-z]+$"\nre := pattern.CompileRegex()\nmatched := re.MatchString(username)`, link: '/features/regular-expressions', linkText: 'Explore regular expressions' },
      { title: 'Explicit safe access', description: 'Opt in to zero-valued results when a nullable class pointer is nil.', code: `var user *Person\n\nname := user?.Name\ngreeting := user?.Greet()`, link: '/features/safe-access', linkText: 'Explore safe access' },
      { title: 'Lazy error fallback', description: 'Use ?? with error-returning calls or nullable values; evaluate the fallback only when needed.', code: `port := strconv.Atoi(rawPort) ?? 8080\nuser := FindUser(id) ?? GuestUser()`, link: '/features/error-fallback', linkText: 'Explore lazy fallback' },
      { title: 'Annotations and introspection', description: 'Declare typed metadata, validate targets, and inspect generated class descriptors at runtime.', code: `annotation Table(name string) on class\n\nclass Employee @{Table("employees")} {\n    Name string\n}\n\nfmt.Println(Employee.class.name)`, link: '/features/metadata', linkText: 'Explore metadata' }
    ]
  },
  {
    title: 'Build complete applications',
    description: 'Bundled Go++ packages connect language features to familiar Go libraries.',
    features: [
      { title: 'Typed templates', description: 'Compile html/template declarations into typed execution functions, with layouts and dynamic execution.', code: `template Page(title string) {\n    <h1>{{.}}</h1>\n}\n\ntpl.Page(&output, "Hello, Ada")`, link: '/features/templates', linkText: 'Explore templates' },
      { title: 'Embedded assets', description: 'Embed directories as rooted fs.FS values or files as []byte using Go embed directives.', code: `embed (\n    Assets "public/"\n    Schema "schema.sql"\n)\n\nindex, err := fs.ReadFile(Assets, "index.html")`, link: '/features/embedded-assets', linkText: 'Explore embedding' },
      { title: 'HTTP servers and routes', description: 'Build handlers with route annotations, request context, lifecycle hooks, and typed responses.', code: `class App : http.Server {\n    func Health(ctx *http.Context) error\n        @{http.GET("/health")} {\n        return ctx.JSON(record(ok: true))\n    }\n}`, link: '/features/http', linkText: 'Explore HTTP' },
      { title: 'OpenAPI, Swagger, and OAuth', description: 'Generate API docs and serve Swagger UI; configure OAuth/OIDC providers on the server.', code: `class App : http.Server @{\n    http.Prefix("/api"), http.OpenAPI,\n    http.Swagger, http.OAuth(http.OAuthProvider.Google),\n} {}`, link: '/features/api-documentation', linkText: 'Explore API documentation' },
      { title: 'ORM and SQL', description: 'Map annotated models to SQL with CRUD operations, transactions, and lifecycle hooks.', code: `class Employee : orm.Model @{orm.Table("employees")} {\n    Name string @{orm.Column("name")}\n}\n\ndb.Insert(&employee)\ndb.Get(&found, "id = $1", employee.Id)`, link: '/features/orm', linkText: 'Explore the ORM' },
      { title: 'Suite-based tests', description: 'Organize tests into suites with setup, assertions, tags, and priority filters.', code: `class UserTest : test.Suite @{test.Tag("crud")} {\n    func Create() {\n        Equal("Ada", user.Name)\n    }\n}\n\ngpp test --tag crud .`, link: '/features/testing', linkText: 'Explore testing' },
      { title: 'External template reloads', description: 'Keep templates in .gpp.tpl files and reload valid edits during gpp run with gpp/http.Server.', code: `// page.gpp.tpl\ntemplate Page(name string) {\n    <h1>Hello, {{.}}</h1>\n}\n\ngpp run .`, link: '/features/template-reload', linkText: 'Explore template reloads' }
    ]
  },
  {
    title: 'Stay in the Go toolchain',
    description: 'Go++ compiles to Go and brings project, editor, and source-level tools in one CLI.',
    features: [
      { title: 'Mixed Go and Go++ builds', description: 'Go++ can call handwritten Go, and Go packages can import generated Go++ packages.', code: `// Go++ calls a native.go function\ngpp run ./app\n\n// Go imports the generated package\ngo run ./consumer`, link: '/features/mixed-go', linkText: 'Explore Go interoperability' },
      { title: 'One project CLI', description: 'Create, build, run, test, and clean projects while generated Go stays in a hidden workspace.', code: `gpp init hello\ncd hello\ngpp run .\ngpp build -o ./hello .`, link: '/features/project-cli', linkText: 'Explore the CLI' },
      { title: 'Canonical formatter', description: 'Format source consistently, preserve comments, or use check mode in CI.', code: `gpp fmt ./...\ngpp fmt --check ./...\ngpp fmt --stdout main.gpp`, link: '/features/formatter', linkText: 'Explore formatting' },
      { title: 'Source documentation', description: 'Search Go++ declarations and inspect source-level docs without opening generated Go.', code: `gpp doc gpp/http.Server\ngpp doc string.TrimSpace\ngpp doc --search template`, link: '/features/source-docs', linkText: 'Explore source docs' },
      { title: 'Language server and diagnostics', description: 'Get diagnostics, completion, hover, navigation, rename, symbols, formatting, and signature help in your editor.', code: `gpp lsp\n\n// feedback uses open .gpp source;\n// no Go backend build on every edit`, link: '/features/language-server', linkText: 'Explore editor support' }
    ]
  }
]
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
          <pre><code>{{ feature.code }}</code></pre>
          <a v-if="feature.link" :href="feature.link">{{ feature.linkText || 'Learn more' }} <span aria-hidden="true">→</span></a>
        </article>
      </div>
    </section>

    <footer class="catalog-footer">
      <p>Go++ source becomes ordinary Go. Keep your existing packages, tools, and deployment flow.</p>
      <a href="/guide/getting-started">Build your first Go++ project <span aria-hidden="true">→</span></a>
    </footer>
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
.feature-card a, .catalog-footer a { align-self: flex-start; color: var(--vp-c-brand-1); font-size: 0.92rem; font-weight: 600; text-decoration: none; }
.feature-card a:hover, .catalog-footer a:hover { text-decoration: underline; text-underline-offset: 3px; }
.catalog-footer { display: flex; align-items: center; justify-content: space-between; gap: 24px; margin-top: 72px; padding: 26px 30px; border: 1px solid var(--vp-c-divider); border-radius: 14px; background: var(--vp-c-bg-soft); }
.catalog-footer p { margin: 0; font-weight: 500; }
@media (max-width: 760px) {
  .feature-catalog { padding: 40px 20px 56px; }
  .catalog-heading { margin-bottom: 48px; text-align: left; }
  .section-heading { display: block; }
  .section-heading p { margin-top: 8px; }
  .feature-grid { grid-template-columns: 1fr; gap: 14px; }
  .feature-copy p { min-height: 0; }
  .catalog-footer { align-items: flex-start; flex-direction: column; gap: 12px; margin-top: 48px; padding: 22px; }
}
@media (prefers-reduced-motion: reduce) { .feature-card { transition: none; } }
</style>
