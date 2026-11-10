package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDocIndexUsesGoPlusSourceDeclarations(t *testing.T) {
	source := `// Package docs describes the documentation fixture.
package docs

// User is a documented class.
class User {
	// Name is the display name.
	Name string

	// Save persists the user.
	func Save() error { return nil }
}

// Status describes a lifecycle state.
enum Status string {
	Pending
	Active
}

// TrimSpace is a string extension.
extend string {
	func TrimSpace() string { return this }
}

// Load loads a user.
func Load(id int) (User, error) { return User(), nil }
`
	file, err := ParseFile("docs.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	index := BuildDocIndex([]*File{file})
	if got := index.Packages["docs"].Doc; got != "Package docs describes the documentation fixture." {
		t.Fatalf("unexpected package documentation: %q", got)
	}
	user := index.Find("User", false)
	if len(user) != 1 || user[0].Doc != "User is a documented class." || len(user[0].Fields) != 1 || user[0].Fields[0].Doc != "Name is the display name." {
		t.Fatalf("unexpected class documentation: %#v", user)
	}
	if methods := index.Find("User.Save", false); len(methods) != 1 || methods[0].Doc != "Save persists the user." {
		t.Fatalf("unexpected method documentation: %#v", methods)
	}
	if values := index.Find("Status", false); len(values) != 1 || len(values[0].Values) != 2 {
		t.Fatalf("unexpected enum documentation: %#v", values)
	}
	if extensions := index.Find("string.TrimSpace", false); len(extensions) != 1 || extensions[0].Kind != DocExtension {
		t.Fatalf("unexpected extension documentation: %#v", extensions)
	}
	if functions := index.Find("Load", false); len(functions) != 1 || functions[0].Signature != "func Load(id int) (User, error)" {
		t.Fatalf("unexpected function documentation: %#v", functions)
	}
}

func TestBuildDocIndexIncludesGoTypesAndValues(t *testing.T) {
	file, err := ParseFile("native_symbols.gpp", `package docs

// MaxItems is the default number of items to return.
const MaxItems = 64

// MaxBatch is the maximum batch size.
const (
	MaxBatch = 16
)

// UserID is the stable identifier used by the Go API.
type UserID int64
`)
	if err != nil {
		t.Fatal(err)
	}
	index := BuildDocIndex([]*File{file})
	constant := index.Find("MaxItems", true)
	if len(constant) != 1 || constant[0].Kind != DocValue || constant[0].Signature != "const MaxItems = 64" || !strings.Contains(constant[0].Doc, "default number") {
		t.Fatalf("Go constant should be indexed with signature and docs: %#v", constant)
	}
	groupedConstant := index.Find("MaxBatch", true)
	if len(groupedConstant) != 1 || groupedConstant[0].Kind != DocValue || !strings.Contains(groupedConstant[0].Signature, "MaxBatch = 16") || !strings.Contains(groupedConstant[0].Doc, "maximum batch") {
		t.Fatalf("grouped Go constant should be indexed with signature and docs: %#v", groupedConstant)
	}
	typeSymbol := index.Find("UserID", true)
	if len(typeSymbol) != 1 || typeSymbol[0].Kind != DocType || typeSymbol[0].Signature != "type UserID int64" || !strings.Contains(typeSymbol[0].Doc, "stable identifier") {
		t.Fatalf("Go type should be indexed with signature and docs: %#v", typeSymbol)
	}
}

func TestCompileFilesSupportsTemplateReload(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	templatePath := filepath.Join(inputDir, "message.gpp.tpl")
	if err := os.WriteFile(templatePath, []byte(`template Message(value string) {Hello}`), 0644); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := fmt.Sprintf(`
import (
    "bytes"
    "fmt"
    "gpp/tpl"
    "os"
)

func main() {
    if err := tpl.LoadFile(%q); err != nil { panic(err) }
    var output bytes.Buffer
    if err := tpl.Execute(&output, "Message", "first"); err != nil { panic(err) }
    fmt.Println(output.String())
    if err := os.WriteFile(%q, []byte(%q), 0644); err != nil { panic(err) }
    if err := tpl.Reload(); err != nil { panic(err) }
    output.Reset()
    if err := tpl.Execute(&output, "Message", "second"); err != nil { panic(err) }
    fmt.Println(output.String())
    if err := os.WriteFile(%q, []byte(%q), 0644); err != nil { panic(err) }
    if err := tpl.Reload(); err == nil { panic("expected invalid template reload to fail") }
    output.Reset()
    if err := tpl.Execute(&output, "Message", "third"); err != nil { panic(err) }
    if output.String() != "Goodbye" { panic("template reload was not atomic") }
}
	`, templatePath, templatePath, `template Message(value string) {Goodbye}`, templatePath, `template Message(value string) {Broken`)
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated template reload program did not run: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Hello") || !strings.Contains(string(output), "Goodbye") {
		t.Fatalf("unexpected template reload output:\n%s", output)
	}
}

func TestCompileFilesMapsBackendDiagnosticsToGoPlusSource(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `import "fmt"

func main() {
	name := "Ada"
	fmt.Println("Value: {{name * 2}}")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "build", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("expected generated Go to fail type checking")
	}
	relative, err := filepath.Rel(outputDir, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	location := filepath.ToSlash(relative) + ":5:"
	absoluteLocation := filepath.ToSlash(sourcePath) + ":5:"
	if !strings.Contains(string(output), location) && !strings.Contains(string(output), absoluteLocation) {
		t.Fatalf("backend diagnostic did not use the Go++ source location %q:\n%s", location, output)
	}
	if strings.Contains(string(output), "main.go:") {
		t.Fatalf("backend diagnostic leaked the generated file:\n%s", output)
	}
}

func TestCompileFilesManagedTemplateReloadUpdatesTypedExecution(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	templatePath := filepath.Join(inputDir, "message.gpp.tpl")
	if err := os.WriteFile(templatePath, []byte(`template Message(value string) {Hello}`), 0644); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := fmt.Sprintf(`
import (
    "bytes"
    "fmt"
    "gpp/tpl"
    "os"
)

func main() {
    var output bytes.Buffer
    if err := tpl.Message(&output, "first"); err != nil { panic(err) }
    fmt.Println(output.String())
    if err := os.WriteFile(%q, []byte(%q), 0644); err != nil { panic(err) }
    if err := tpl.Reload(); err != nil { panic(err) }
    output.Reset()
    if err := tpl.Message(&output, "second"); err != nil { panic(err) }
    fmt.Println(output.String())
}
`, templatePath, `template Message(value string) {Goodbye}`)
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath, templatePath}, outputDir, CompileOptions{ModulePath: "generated", Development: true}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("managed template reload program did not run: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Hello") || !strings.Contains(string(output), "Goodbye") {
		t.Fatalf("unexpected managed template reload output:\n%s", output)
	}
}

func TestCompileFilesSeparatesSameBasenameSourceAndTemplate(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "page.gpp")
	templatePath := filepath.Join(inputDir, "page.gpp.tpl")
	if err := os.WriteFile(sourcePath, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(templatePath, []byte("package main\n\ntemplate Page(value string) {<p>{{.}}</p>}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{templatePath, sourcePath},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "page.go")); err != nil {
		t.Fatalf("source output missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "page_tpl.go")); err != nil {
		t.Fatalf("template output missing: %v", err)
	}
}

func TestCompileFilesFindsAtExitInAnotherPackageFile(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	mainPath := filepath.Join(inputDir, "main.gpp")
	exitPath := filepath.Join(inputDir, "exit.gpp")
	if err := os.WriteFile(mainPath, []byte("package main\nfunc main() { work() }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exitPath, []byte("package main\nfunc atExit() { cleanup() }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions(
		[]string{mainPath, exitPath},
		outputDir,
		CompileOptions{NoStdlib: true, NoPrelude: true},
	); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	deferIndex := strings.Index(text, "defer atExit()")
	workIndex := strings.Index(text, "work()")
	if deferIndex < 0 || workIndex < 0 || deferIndex > workIndex {
		t.Fatalf("main did not defer the hook declared in another file:\n%s", text)
	}
}

func TestCompileFilesPrunesImportUsedOnlyByTemplateAnnotation(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `package main
import tpl "gpp/tpl"

template Home(name string) @{tpl.Path("/")} {
	<p>{{.}}</p>
}

func main() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(generated), `import tpl "generated/gpp/tpl"`) {
		t.Fatalf("annotation-only tpl alias remained in generated Go:\n%s", generated)
	}
}

func TestCompileFilesSupportsAdHocTemplatesAndFunctions(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
import (
	"bytes"
	"errors"
	"fmt"
	"gpp/tpl"
	"io"
	"strings"
)

template Card(value string) {
    <i>{{.}}</i>
}

template PathCard(value string) @{tpl.Path("/posts/{id}")} {
    <p data-id="{{param "id"}}">{{.}}</p>
}

template LegacyPathCard(value string) @{tpl.Path("/legacy/:id")} {
    <p data-id="{{param "id"}}">{{.}}</p>
}

func upper(value string) string {
    return strings.ToUpper(value)
}

func isDash(character rune) bool { return character == '-' }
func isE(character rune) bool { return character == 'é' }
func isB(character rune) bool { return character == 'b' }
func uppercaseASCII(character rune) rune { return character - 32 }

func main() {
    var output bytes.Buffer
	if err := tpl.Execute(&output, "<h1>{{{{.}}}}</h1>", "unsafe &"); err != nil { panic(err) }
    fmt.Println(output.String())
    if err := tpl.Funcs.Add("upper", upper); err != nil { panic(err) }
    output.Reset()
	if err := tpl.Execute(&output, "<b>{{{{upper .}}}}</b>", "go"); err != nil { panic(err) }
    fmt.Println(output.String())
    output.Reset()
	if err := tpl.Execute(&output, "<section>{{{{Card .}}}}</section>", "body"); err != nil { panic(err) }
    fmt.Println(output.String())
    output.Reset()
    if err := tpl.Execute(&output, "/posts/42", "body"); err != nil { panic(err) }
    fmt.Println(output.String())
	output.Reset()
	if err := tpl.Execute(&output, "/legacy/7", "old"); err != nil { panic(err) }
	fmt.Println(output.String())
	output.Reset()
	if err := tpl.Execute(&output, "/missing/42", "unused"); err == nil || !strings.Contains(err.Error(), "no template matches path") {
		panic("expected an unmatched template path to return an error")
	}

	if "  hello  ".TrimSpace().ToUpper() != "HELLO" ||
		!"hello".Contains("ell") ||
		"hello".ReplaceAll("l", "L") != "heLLo" ||
		strings.Join("a,b".Split(","), "") != "ab" ||
		!"go".EqualFold("GO") ||
		"a\r\nb".Lines()[1] != "b" {
		panic("string prelude extension failed")
	}
	if "café".Reverse() != "éfac" ||
		"/api/users".TrimPrefix("/api") != "/users" ||
		"report.json".TrimSuffix(".json") != "report" ||
		"--title--".TrimFunc(isDash) != "title" ||
		"-title-".TrimLeftFunc(isDash) != "title-" ||
		"-title-".TrimRightFunc(isDash) != "-title" ||
		"hello WORLD".ToTitle() != "HELLO WORLD" ||
		"a,b,c".SplitN(",", 2)[1] != "b,c" ||
		"a,b,".SplitAfter(",")[1] != "b," ||
		"a,b,c".SplitAfterN(",", 2)[1] != "b,c" ||
		[]string{"api", "users"}.Join("/") != "api/users" {
		panic("string trim, split, reverse, or join extension failed")
	}
	before, after, found := "a/b/c".Cut("/")
	if before != "a" || after != "b/c" || !found {
		panic("string Cut extension failed")
	}
	before, after, found = "a/b/c".CutLast("/")
	if before != "a/b" || after != "c" || !found {
		panic("string CutLast extension failed")
	}
	withoutPrefix, found := "https://example.com".CutPrefix("https://")
	if withoutPrefix != "example.com" || !found {
		panic("string CutPrefix extension failed")
	}
	withoutSuffix, found := "report.json".CutSuffix(".json")
	if withoutSuffix != "report" || !found {
		panic("string CutSuffix extension failed")
	}
	if !"hello".ContainsAny("aeiou") || !"café".ContainsRune('é') ||
		!"café".ContainsFunc(isE) ||
		"abc".IndexAny("xz") != -1 || "abc".IndexByte(byte('b')) != 1 ||
		"café".IndexRune('é') != 3 || "abc".IndexFunc(isB) != 1 ||
		"abca".LastIndexAny("ac") != 3 || "abc".LastIndexByte(byte('b')) != 1 ||
		"abc".LastIndexFunc(isB) != 1 {
		panic("string search extension failed")
	}
	if "abc".Map(uppercaseASCII) != "ABC" ||
		"\xff".ToValidUTF8("?") != "?" {
		panic("string rune transform extension failed")
	}
	if "42".ToInt() != 42 || "bad".ToInt() != 0 || "bad".ToInt(fallback: -1) != -1 ||
		"42".ToInt64() != 42 || "42".ToUint64() != 42 ||
		"1.5".ToFloat32() != 1.5 || "1.5".ToFloat64() != 1.5 ||
		!"true".ToBool() || "invalid".ToBool() || "invalid".ToBool(fallback: true) != true {
		panic("string conversion extension failed")
	}
	intValue := -42
	var int64Value int64 = 43
	var uintValue uint64 = 44
	var float32Value float32 = 1.5
	var float64Value float64 = 2.5
	boolValue := true
	if intValue.String() != "-42" || int64Value.String() != "43" || uintValue.String() != "44" ||
		float32Value.String() != "1.5" || float64Value.String() != "2.5" || boolValue.String() != "true" {
		panic("scalar String extension failed")
	}
	values := []int{1, 2, 3}
	doubled := values.Map(func(value int) int { return value * 2 })
	if len(doubled) != 3 || doubled.First() != 2 || doubled.Last() != 6 || !values.NotEmpty() || values.Take(2).Last() != 2 || values.Drop(2).First() != 3 {
		panic("slice prelude extension failed")
	}
	settings := map[string]string{"mode": "test"}
	copy := settings.Clone()
	mode, modeFound := settings.Get("mode")
	missing, missingFound := settings.Get("missing")
	if !settings.Has("mode") || !modeFound || mode != "test" || missingFound || missing != "" ||
		settings.Get("missing", "default") != "default" || copy.Get("mode", "") != "test" || settings.IsEmpty() {
		panic("map prelude extension failed")
	}
	if !settings.Delete("mode") || settings.Has("mode") || settings.Delete("missing") {
		panic("map Delete extension failed")
	}
	prices := map(coffee: 2.5, tea: 1.75)
	priceKeys := prices.Keys()
	priceValues := prices.Values()
	if len(priceKeys) != 2 || len(priceValues) != 2 {
		panic("inferred map Keys or Values extension failed")
	}
	prices.Each((name, price) => println(name, price))
	iterationErr := prices.Each((name, price) => errors.New("stop map iteration"))
	if iterationErr == nil {
		panic("map Each did not return the callback error")
	}
	var coffeePrice float64 = prices["coffee"]
	prices["espresso"] = 3.0
	form := map(name: "Ada", level: 3)
	form["active"] = true
	headers := map("X-Request-ID": "req-42")
	var requestID string = headers["X-Request-ID"]
	var flexible map[string]any = map(name: "Grace")
	flexible["level"] = 37
	if coffeePrice != 2.5 || prices["espresso"] != 3.0 || fmt.Sprintf("%T", form["level"]) != "int" {
		panic("inferred map literal typing failed")
	}
	if requestID != "req-42" || fmt.Sprintf("%T", flexible["level"]) != "int" {
		panic("map literal keys or contextual value type failed")
	}
	var emptyCounts map[string]int = map()
	emptyCounts["items"] = 2
	if emptyCounts["items"] != 2 {
		panic("context-typed empty map literal failed")
	}
	mapContainer := struct(prices: map(coffee: 2.5))
	nestedPrices := map(prices: map(coffee: 2.5))
	if mapContainer.prices["coffee"] != 2.5 || nestedPrices["prices"]["coffee"] != 2.5 {
		panic("nested map literal typing failed")
	}
	sentinel := errors.New("sentinel")
	wrapped := fmt.Errorf("wrapped: %w", sentinel)
	if !wrapped.Is(sentinel) || wrapped.Unwrap() == nil {
		panic("error prelude extension failed")
	}
	var reader io.Reader = strings.NewReader("reader")
	data, err := reader.ReadAll()
	if err != nil || string(data) != "reader" {
		panic("reader prelude extension failed")
	}
	var outputWriter bytes.Buffer
	var writer io.Writer = &outputWriter
	_, err = writer.WriteString("writer")
	if err != nil || outputWriter.String() != "writer" {
		panic("writer prelude extension failed")
	}
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated ad-hoc template program did not run: %v\n%s", err, output)
	}
	result := string(output)
	if !strings.Contains(result, "<h1>unsafe &amp;</h1>") ||
		!strings.Contains(result, "<b>GO</b>") ||
		!strings.Contains(result, "<section>") ||
		!strings.Contains(result, "<i>body</i>") ||
		!strings.Contains(result, `<p data-id="42">body</p>`) ||
		!strings.Contains(result, `<p data-id="7">old</p>`) {
		t.Fatalf("unexpected ad-hoc template output:\n%s", output)
	}
}

func TestCompileFilesDoesNotWidenInferredMapValueType(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `package main

func main() {
    values := map(ratio: 1.23)
    values["label"] = "not a float"
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("expected a later incompatible map value to fail type checking; output: %s", output)
	}
	if !strings.Contains(string(output), "cannot use") {
		t.Fatalf("expected Go's map value type error, got:\n%s", output)
	}
}

func TestCompileFilesResolvesNativeSelectorChains(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
import (
    "fmt"
    "io"
    "os"
    "strings"
    stdhttp "net/http"
)

func main() {
    request, err := stdhttp.NewRequest("GET", "http://example.com/path", nil)
    if err != nil { panic(err) }
    title := request.FormValue("title").TrimSpace()
    if !title.Empty() { panic("expected empty title") }
    env := os.Getenv("GPP_TYPE_RESOLVER_MISSING").TrimSpace().ToUpper()
    response := &stdhttp.Response{Request: request, Body: io.NopCloser(strings.NewReader("body"))}
    path := response.Request.URL.Path
    body, err := response.Body.ReadAll()
    if err != nil { panic(err) }
    fmt.Println(title, env, path, string(body))
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated native selector program did not run: %v\n%s", err, output)
	}
	if string(output) != "  /path body\n" {
		t.Fatalf("unexpected native selector output: %s", output)
	}
}

func TestCompileFilesRunsBundledHTTPServer(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
package main

import (
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"
	"time"
	http "gpp/http"
)

var events []string

class App : http.Server @{http.IP("127.0.0.1")} {
    func BeforeListen() error {
        events = append(events, "before-listen")
        return nil
    }

    func AfterListen() error {
        events = append(events, "after-listen")
        return nil
    }

    func BeforeRequest(ctx *http.Context) error {
        events = append(events, "before-request")
        ctx.Set("marker", "shared")
        return nil
    }

    func Handle(ctx *http.Context) error {
        events = append(events, "handle")
        ctx.Set("front-controller", "shared")
        if ctx.Request.URL.Path == "/handled" {
            return ctx.Text("handled")
        }
        if ctx.Request.URL.Path == "/handle-error" {
            return fmt.Errorf("front controller failed")
        }
        return nil
    }

    func Hello(ctx *http.Context) error @{http.GET("/hello/{name}")} {
        events = append(events, "route")
        return ctx.Text(fmt.Sprintf("%s %s %s", ctx.Get("marker"), ctx.Get("front-controller"), ctx.Param("name")))
    }

    func AfterRequest(ctx *http.Context) error {
        events = append(events, "after-request")
        return nil
    }

    func Error(ctx *http.Context, code int, cause error) error {
        events = append(events, "error")
        return ctx.Text(code, fmt.Sprint(code))
    }

    func BeforeShutdown() error {
        events = append(events, "before-shutdown")
        return nil
    }

    func AfterShutdown() error {
        events = append(events, "after-shutdown")
        return nil
    }
}

func main() {
    app := App()
    done := make(chan error, 1)
    go func() { done <- app.Listen(0) }()

    var response *stdhttp.Response
    var err error
    for response == nil {
        select {
        case err := <-done:
            if err != nil { fmt.Println("SKIP:", err); return }
        default:
        }
        if app.HTTPServer != nil {
            response, err = stdhttp.Get("http://" + app.HTTPServer.Addr + "/hello/Go")
        }
        if response == nil { time.Sleep(time.Millisecond) }
    }
    body, err := io.ReadAll(response.Body)
    if err != nil { panic(err) }
    response.Body.Close()
    fmt.Println(response.StatusCode, string(body))

    response, err = stdhttp.Get("http://" + app.HTTPServer.Addr + "/handled")
    if err != nil { panic(err) }
    body, err = io.ReadAll(response.Body)
    if err != nil { panic(err) }
    response.Body.Close()
    fmt.Println(response.StatusCode, string(body))

    response, err = stdhttp.Get("http://" + app.HTTPServer.Addr + "/handle-error")
    if err != nil { panic(err) }
    body, err = io.ReadAll(response.Body)
    if err != nil { panic(err) }
    response.Body.Close()
    fmt.Println(response.StatusCode, string(body))

    if err := app.Shutdown(context.Background()); err != nil { panic(err) }
    if err := <-done; err != nil && err != stdhttp.ErrServerClosed { panic(err) }
    fmt.Println(strings.Join(events, ","))
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated HTTP server did not run: %v\n%s", err, output)
	}
	if strings.HasPrefix(string(output), "SKIP:") {
		t.Skipf("loopback sockets unavailable: %s", output)
	}
	result := string(output)
	if !strings.Contains(result, "gpp/http: listening on 127.0.0.1:") {
		t.Fatalf("server did not report its actual listener address:\n%s", output)
	}
	if !strings.Contains(result, "200 shared shared Go\n200 handled\n500 500\nbefore-listen,after-listen,before-request,handle,route,after-request,before-request,handle,after-request,before-request,handle,after-request,error,before-shutdown,after-shutdown\n") {
		t.Fatalf("unexpected HTTP response:\n%s", output)
	}
}

func TestCompileFilesBindsHTTPIntoGenericTypes(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
	"fmt"
	"net/http/httptest"
	"strings"
	http "gpp/http"
)

class ClassInput {
	Name string
	Age int
}

class App : http.Server {
	func Ping(ctx *http.Context) error @{http.GET("/ping")} {
		return ctx.Text("pong")
	}
}

type GoInput struct {
	Name string
	Age int
}

func newContext(method string, target string, contentType string, body string) *http.Context {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	return &http.Context{
		Response: response,
		Request: request,
		Values: map[string]any{},
	}
}

func main() {
	classContext := newContext("POST", "/class", "application/json", "{\"name\":\"Ada\",\"age\":37,\"extra\":true}")
	classValue := classContext.Bind[ClassInput]()
	if classValue.Name != "Ada" || classValue.Age != 37 { panic("class JSON binding failed") }

	pointerContext := newContext("POST", "/class-pointer", "application/json", "{\"name\":\"Lin\",\"age\":42}")
	pointerValue := pointerContext.Bind[*ClassInput]()
	if pointerValue.Name != "Lin" || pointerValue.Age != 42 { panic("pointer class JSON binding failed") }

	structContext := newContext("POST", "/struct", "application/json", "{\"name\":\"Grace\",\"age\":85}")
	structValue := structContext.Bind[GoInput]()
	if structValue.Name != "Grace" || structValue.Age != 85 { panic("Go struct JSON binding failed") }

	recordContext := newContext("POST", "/record", "application/json", "{\"name\":\"Margaret\",\"age\":86,\"ignored\":\"field\"}")
	recordValue := recordContext.Bind[record(name: string, age: int)]()
	if recordValue.name != "Margaret" || recordValue.age != 86 { panic("record JSON binding failed") }

	pointerRecordContext := newContext("POST", "/record-pointer", "application/json", "{\"name\":\"Lin\",\"age\":42}")
	pointerRecord := pointerRecordContext.Bind[*record(name: string, age: int)]()
	if pointerRecord.name != "Lin" || pointerRecord.age != 42 { panic("pointer record JSON binding failed") }

	queryContext := newContext("GET", "/query?name=Katherine&age=101", "", "")
	queryValue := queryContext.Bind[record(name: string, age: int)]()
	if queryValue.name != "Katherine" || queryValue.age != 101 { panic("record query binding failed") }

	formContext := newContext("POST", "/form", "application/x-www-form-urlencoded", "name=Frances&age=68")
	formValue := formContext.Bind[ClassInput]()
	if formValue.Name != "Frances" || formValue.Age != 68 { panic("class form binding failed") }

	badContext := newContext("POST", "/bad", "application/json", "{\"name\":\"bad\",\"age\":\"not an integer\"}")
	_, bindErr := badContext.Bind[record(name: string, age: int)]()
	if bindErr == nil { panic("expected invalid record field to return an error") }

	fmt.Println("generic HTTP binding passed")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generic HTTP binding program did not run: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "generic HTTP binding passed") {
		t.Fatalf("generic HTTP binding checks did not finish:\n%s", output)
	}
}

func TestCompileFilesRunsHTTPClientHelpers(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	http "gpp/exp/http"
)

class Reply {
	Cookie string
	Name string
	Filename string
	Content string
	Note string
}

class CreateRequest {
	Name string
}

func ensure(ok bool, message string) error {
	if !ok {
		return fmt.Errorf("%s", message)
	}
	return nil
}

func main() {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(response stdhttp.ResponseWriter, request *stdhttp.Request) {
		switch strings.TrimPrefix(request.URL.Path, "/api") {
		case "/login":
			stdhttp.SetCookie(response, &stdhttp.Cookie{Name: "session", Value: "active", Path: "/"})
			json.NewEncoder(response).Encode(Reply(Cookie: "set"))
		case "/cookie":
			cookie, err := request.Cookie("session")
			if err != nil {
				stdhttp.Error(response, "cookie missing", stdhttp.StatusUnauthorized)
				return
			}
			json.NewEncoder(response).Encode(Reply(Cookie: cookie.Value))
		case "/json":
			var input CreateRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				stdhttp.Error(response, err.Error(), stdhttp.StatusBadRequest)
				return
			}
			json.NewEncoder(response).Encode(Reply(Name: input.Name))
		case "/json-record":
			var input map[string]any
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				stdhttp.Error(response, err.Error(), stdhttp.StatusBadRequest)
				return
			}
			json.NewEncoder(response).Encode(input)
		case "/anonymous":
			response.Header().Set("Content-Type", "application/json")
			response.Write([]byte("{\"name\":\"Ada\",\"level\":37}"))
		case "/post":
			if request.Method != stdhttp.MethodPost || request.Header.Get("Content-Type") != "text/plain" {
				stdhttp.Error(response, "unexpected POST request", stdhttp.StatusBadRequest)
				return
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				stdhttp.Error(response, err.Error(), stdhttp.StatusBadRequest)
				return
			}
			response.Write(body)
		case "/form", "/simple-form":
			if err := request.ParseForm(); err != nil {
				stdhttp.Error(response, err.Error(), stdhttp.StatusBadRequest)
				return
			}
			json.NewEncoder(response).Encode(Reply(Name: request.FormValue("name"), Note: request.FormValue("level")))
		case "/upload", "/simple-upload":
			file, header, err := request.FormFile("attachment")
			if err != nil {
				stdhttp.Error(response, err.Error(), stdhttp.StatusBadRequest)
				return
			}
			defer file.Close()
			content, err := io.ReadAll(file)
			if err != nil {
				stdhttp.Error(response, err.Error(), stdhttp.StatusBadRequest)
				return
			}
			json.NewEncoder(response).Encode(Reply(Filename: header.Filename, Content: string(content), Note: request.FormValue("note")))
		case "/missing":
			stdhttp.Error(response, "missing item", stdhttp.StatusNotFound)
		default:
			stdhttp.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := http.Client(BaseURL: server.URL+"/api", Timeout: 10*time.Second)
	ctx := context.Background()
	client.Get("/login", ctx).Body.Close()
	cookieReply := client.Get[Reply]("/cookie")
	ensure(cookieReply.Cookie == "active", "cookie jar did not retain the session")

	jsonReply := client.Post[Reply]("/json", CreateRequest(Name: "Ada"), ctx)
	ensure(jsonReply.Name == "Ada", "typed JSON POST failed")
	postResponse := client.Post("/post", "text/plain", strings.NewReader("raw body"))
	postBody, err := io.ReadAll(postResponse.Body)
	postResponse.Body.Close()
	ensure(err == nil && string(postBody) == "raw body", "raw POST response failed")

	formResponse := client.PostForm("/form", url.Values{"name": []string{"Grace"}})
	var formReply Reply
	json.NewDecoder(formResponse.Body).Decode(&formReply)
	formResponse.Body.Close()
	ensure(formReply.Name == "Grace", "form POST failed")
	simpleFormResponse := client.PostForm("/simple-form", struct(name: "Ada", level: 3))
	var simpleFormReply Reply
	json.NewDecoder(simpleFormResponse.Body).Decode(&simpleFormReply)
	simpleFormResponse.Body.Close()
	ensure(simpleFormReply.Name == "Ada" && simpleFormReply.Note == "3", "anonymous struct form POST failed")
	postRecord := client.Post[struct(name: string, level: int)]("/json-record", struct(name: "Ada", level: 37))
	ensure(postRecord.name == "Ada" && postRecord.level == 37, "anonymous struct JSON POST failed")
	getRecord := client.Get[struct(name: string, level: int)]("/anonymous")
	ensure(getRecord.name == "Ada" && getRecord.level == 37, "anonymous struct JSON GET failed")

	temporaryFile := os.CreateTemp("", "gpp-http-upload-*")
	defer os.Remove(temporaryFile.Name())
	temporaryFile.WriteString("file contents")
	temporaryFile.Close()
	uploadResponse := client.UploadFiles("/upload", url.Values{"note": []string{"metadata"}}, []*http.UploadFile{http.UploadFile(Field: "attachment", Path: temporaryFile.Name())})
	var uploadReply Reply
	json.NewDecoder(uploadResponse.Body).Decode(&uploadReply)
	uploadResponse.Body.Close()
	ensure(uploadReply.Filename == filepath.Base(temporaryFile.Name()), "multipart filename default failed")
	ensure(uploadReply.Content == "file contents" && uploadReply.Note == "metadata", "multipart upload failed")

	simpleUploadResponse := client.Upload(
		"/simple-upload",
		struct(note: 42),
		map[string]string{"attachment": temporaryFile.Name()},
	)
	var simpleUploadReply Reply
	json.NewDecoder(simpleUploadResponse.Body).Decode(&simpleUploadReply)
	simpleUploadResponse.Body.Close()
	ensure(simpleUploadReply.Filename == filepath.Base(temporaryFile.Name()), "anonymous struct multipart filename default failed")
	ensure(simpleUploadReply.Content == "file contents" && simpleUploadReply.Note == "42", "anonymous struct multipart upload failed")

	_, statusError := client.Get[Reply]("/missing")
	ensure(statusError != nil && strings.Contains(statusError.Error(), "404 Not Found"), "non-success status was not returned as an error")
	fmt.Println("HTTP client passed")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("HTTP client program did not run: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "HTTP client passed") {
		t.Fatalf("HTTP client checks did not finish:\n%s", output)
	}
}

func TestCompileFilesSchedulesAnnotatedCronFunctions(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	jobsPath := filepath.Join(inputDir, "jobs.gpp")
	jobsSource := `package jobs

import (
	"context"
	"time"
	cron "gpp/cron"
)

var Runs int

func Pulse(ctx context.Context) error @{cron.Every(20 * time.Millisecond)} {
	Runs++
	return nil
}

func Nightly(ctx context.Context) error @{cron.Cron("0 2 * * *")} {
	return nil
}
`
	source := `package main

import (
	"context"
	"fmt"
	"time"
	cron "gpp/cron"
	jobs
)

class Maintenance : cron.Cron {
	Runs int

	func PulseInstance(ctx context.Context) error @{cron.Every(20 * time.Millisecond)} {
		this.Runs++
		return nil
	}
}

func main() {
	time.Sleep(50 * time.Millisecond)
	if jobs.Runs != 0 { panic("cron job ran before Start") }
	if err := cron.Start(); err != nil { panic(err) }
	maintenance := Maintenance()
	if err := maintenance.Start(); err != nil { panic(err) }
	if err := maintenance.Start(); err != nil { panic(err) }
	secondMaintenance := Maintenance()
	if err := secondMaintenance.Start(); err != nil { panic(err) }
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := cron.StopWithContext(ctx); err != nil { panic(err) }
	if err := maintenance.StopWithContext(ctx); err != nil { panic(err) }
	if err := secondMaintenance.StopWithContext(ctx); err != nil { panic(err) }
	fmt.Println(jobs.Runs > 0 && maintenance.Runs > 0 && secondMaintenance.Runs > 0)
}
`
	if err := os.WriteFile(jobsPath, []byte(jobsSource), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{jobsPath, sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	cronTests := `package cron

import (
	"testing"
	"time"
)

func TestCompiledCronFieldMatching(t *testing.T) {
	schedule, err := parseCronSchedule("*/15 9-17 * * 1-5")
	if err != nil { t.Fatal(err) }
	if !schedule.matches(time.Date(2026, time.September, 28, 9, 15, 0, 0, time.Local)) {
		t.Fatal("expected a matching weekday time")
	}
	if schedule.matches(time.Date(2026, time.September, 28, 9, 10, 0, 0, time.Local)) {
		t.Fatal("unexpected match outside the minute step")
	}

	dayOrWeekday, err := parseCronSchedule("0 0 1 * 1")
	if err != nil { t.Fatal(err) }
	if !dayOrWeekday.matches(time.Date(2026, time.June, 8, 0, 0, 0, 0, time.Local)) {
		t.Fatal("expected a match when only the weekday field matches")
	}
	if dayOrWeekday.matches(time.Date(2026, time.June, 2, 0, 0, 0, 0, time.Local)) {
		t.Fatal("unexpected match when neither day field matches")
	}
}

func TestCompiledCronRejectsMalformedSchedules(t *testing.T) {
	for _, expression := range []string{"", "60 * * * *", "@every 0s", "0 0 * * 8"} {
		if _, err := parseCronSchedule(expression); err == nil {
			t.Errorf("expected %q to be rejected", expression)
		}
	}
}
`
	cronTestPath := filepath.Join(outputDir, "gpp", "cron", "cron_test.go")
	if err := os.WriteFile(cronTestPath, []byte(cronTests), 0644); err != nil {
		t.Fatal(err)
	}
	cronTestCommand := exec.Command("go", "test", "./gpp/cron")
	cronTestCommand.Dir = outputDir
	cronTestCommand.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := cronTestCommand.CombinedOutput(); err != nil {
		t.Fatalf("generated cron package tests failed: %v\n%s", err, output)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated cron program did not run: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "true" {
		t.Fatalf("annotated cron function was not run:\n%s", output)
	}
}

func TestCompileAllowsAnnotatedMethodsOutsideCronSchedulerClass(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `package main

import (
	"context"
	"time"
	cron "gpp/cron"
)

class UnmanagedJobs {
	func Prune(ctx context.Context) error @{cron.Every(time.Minute)} {
		return nil
	}
}

func main() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatalf("generic method annotations should not be restricted to cron.Cron subclasses: %v", err)
	}
}

func TestCompileFilesServesOpenAPIAndSwaggerRoutes(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
	"context"
	"fmt"
	stdjson "encoding/json"
	"io"
	stdhttp "net/http"
	"strings"
	"time"
	http "gpp/http"
)

class HealthStatus {
	OK bool
}

class App : http.Server @{http.IP("127.0.0.1"), http.OpenAPI, http.Swagger("/swagger")} {
	func Hello(ctx *http.Context) error @{
		http.GET("/hello/{name}"),
		http.Summary("Say hello"),
		http.Description("Returns a greeting in plain text."),
		http.Tags("Greetings"),
		http.ResponseBody(200, "Greeting returned", "text/plain", "hello")
	} {
		return ctx.Text("hello")
	}

	func Create(ctx *http.Context) error @{
		http.POST("/swagger"),
		http.Summary("Create a health status"),
		http.RequestBody("application/json", "Health status to create", record(OK: true)),
		http.ResponseBody(201, "Health status created", "application/json", record(OK: true)),
		http.ResponseBody(400, "The request is invalid", "", nil)
	} {
		status := HealthStatus()
		if err := stdjson.NewDecoder(ctx.Request.Body).Decode(&status); err != nil { return err }
		return ctx.JSON(201, status)
	}

	func Page(ctx *http.Context) error @{
		http.GET("/page"),
		http.Summary("Render a page"),
		http.ResponseBody(200, "HTML page", "text/html", "<h1>Page</h1>")
	} {
		return ctx.Text("page")
	}
}

func request(base string, path string) {
	response, err := stdhttp.Get(base + path)
	if err != nil { panic(err) }
	body, err := io.ReadAll(response.Body)
	if err != nil { panic(err) }
	response.Body.Close()
	text := string(body)
	if response.StatusCode != stdhttp.StatusOK { panic(fmt.Sprintf("%s returned %d", path, response.StatusCode)) }
	if path == "/openapi.json" && (strings.Contains(text, "/hello/{name}") || strings.Contains(text, "/page")) {
		panic("OpenAPI should describe JSON endpoints, not plain-text or HTML routes")
	}
	fmt.Println(path, response.StatusCode,
		strings.Contains(response.Header.Get("Content-Type"), "application/json"),
		strings.Contains(response.Header.Get("Content-Type"), "text/html"),
		strings.Contains(text, "3.0.3"),
		strings.Contains(text, "/hello/{name}"),
		strings.Contains(text, "Swagger UI"),
		strings.Contains(text, "\"summary\": \"Say hello\""),
		strings.Contains(text, "\"description\": \"Returns a greeting in plain text.\""),
		strings.Contains(text, "\"tags\": ["),
		strings.Contains(text, "\"201\": {"),
		strings.Contains(text, "\"400\": {"),
		strings.Contains(text, "\"OK\""),
		strings.Contains(text, "\"example\": {"),
		strings.Contains(text, "\"requestBody\": {"),
		strings.Contains(text, "Health status to create"),
	)
}

func requestAsset(base string, path string, contentType string, marker string) {
	response, err := stdhttp.Get(base + path)
	if err != nil { panic(err) }
	body, err := io.ReadAll(response.Body)
	if err != nil { panic(err) }
	response.Body.Close()
	if response.StatusCode != stdhttp.StatusOK { panic(fmt.Sprintf("%s returned %d", path, response.StatusCode)) }
	if !strings.Contains(response.Header.Get("Content-Type"), contentType) { panic(fmt.Sprintf("%s content type %q", path, response.Header.Get("Content-Type"))) }
	if marker != "" && !strings.Contains(string(body), marker) { panic(fmt.Sprintf("%s missing marker", path)) }
}

func main() {
	app := App()
	done := make(chan error, 1)
	go func() { done <- app.Listen(0) }()
	for app.HTTPServer == nil {
		select {
		case err := <-done:
			if err != nil { fmt.Println("SKIP:", err); return }
		default:
		}
		time.Sleep(time.Millisecond)
	}
	base := "http://" + app.HTTPServer.Addr
	request(base, "/openapi.json")
	request(base, "/swagger")
	request(base, "/swagger/")
	requestAsset(base, "/swagger/swagger-ui.css", "text/css", ".swagger-ui")
	requestAsset(base, "/swagger/swagger-ui-bundle.js", "text/javascript", "SwaggerUIBundle")
	requestAsset(base, "/swagger/swagger-ui-standalone-preset.js", "text/javascript", "StandaloneLayout")
	if err := app.Shutdown(context.Background()); err != nil { panic(err) }
	if err := <-done; err != nil && err != stdhttp.ErrServerClosed { panic(err) }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated OpenAPI program did not run: %v\n%s", err, output)
	}
	if strings.HasPrefix(string(output), "SKIP:") {
		t.Skipf("loopback sockets unavailable: %s", output)
	}
	result := string(output)
	if !strings.Contains(result, "/openapi.json 200 true false true false false false false false true true true true true true") {
		t.Fatalf("unexpected OpenAPI response:\n%s", output)
	}
	if !strings.Contains(result, "/swagger 200 false true false false true") {
		t.Fatalf("unexpected Swagger response:\n%s", output)
	}
}

func TestCompileFilesValidatesSwaggerConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		message string
	}{
		{
			name: "swagger requires openapi",
			source: `package main

import http "gpp/http"

class App : http.Server @{http.Swagger} {}
`,
			message: "http.Swagger requires http.OpenAPI",
		},
		{
			name: "swagger subtree conflict",
			source: `package main

import http "gpp/http"

class App : http.Server @{http.OpenAPI, http.Swagger} {
	func Asset(ctx *http.Context) error @{http.GET("/swagger/custom")} { return nil }
}
`,
			message: "conflicts with http.Swagger mount",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inputDir := t.TempDir()
			outputDir := t.TempDir()
			sourcePath := filepath.Join(inputDir, "main.gpp")
			if err := os.WriteFile(sourcePath, []byte(test.source), 0644); err != nil {
				t.Fatal(err)
			}
			err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"})
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected %q, got %v", test.message, err)
			}
		})
	}
}

func TestCompileFilesValidatesOAuthConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		message string
	}{
		{
			name: "oauth requires a provider",
			source: `package main

import http "gpp/http"

class App : http.Server @{http.OAuth} {}
`,
			message: "http.OAuth requires a provider",
		},
		{
			name: "oauth providers must be unique",
			source: `package main

import http "gpp/http"

class App : http.Server @{http.OAuth(http.OAuthProvider.Google), http.OAuth(http.OAuthProvider.Google)} {}
`,
			message: "duplicate OAuth provider google",
		},
		{
			name: "oauth routes cannot conflict",
			source: `package main

import http "gpp/http"

class App : http.Server @{http.OAuth(http.OAuthProvider.Google)} {
	func Login(ctx *http.Context) error @{http.GET("/auth/google")} { return nil }
}
`,
			message: "conflicts with generated OAuth route",
		},
		{
			name: "oauth hook signature",
			source: `package main

import http "gpp/http"

class App : http.Server @{http.OAuth(http.OAuthProvider.Google)} {
	func OAuthLogin(ctx *http.Context, identity string) {}
}
`,
			message: "OAuthLogin has invalid signature",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inputDir := t.TempDir()
			outputDir := t.TempDir()
			sourcePath := filepath.Join(inputDir, "main.gpp")
			if err := os.WriteFile(sourcePath, []byte(test.source), 0644); err != nil {
				t.Fatal(err)
			}
			err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"})
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected %q, got %v", test.message, err)
			}
		})
	}
}

func TestCompileFilesScopesTemplatesAcrossPackages(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	pagesPath := filepath.Join(inputDir, "pages.gpp")
	mainPath := filepath.Join(inputDir, "main.gpp")

	pagesSource := `package demo.pages

import (
    "bytes"
    "gpp/tpl"
)

template Card(value string) {
    <i>page:{{.}}</i>
}

func Render() string {
    var output bytes.Buffer
    if err := tpl.Execute(&output, "Card", "package"); err != nil { panic(err) }
    return output.String()
}
`
	mainSource := `package main

import (
    "bytes"
    "fmt"
    "gpp/tpl"
    demo.pages
)

template Card(value string) {
    <b>main:{{.}}</b>
}

func main() {
    var output bytes.Buffer
    if err := tpl.Execute(&output, "Card", "package"); err != nil { panic(err) }
    fmt.Println(output.String())
    fmt.Println(pages.Render())
}
`
	if err := os.WriteFile(pagesPath, []byte(pagesSource), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(mainSource), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{pagesPath, mainPath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated cross-package template program did not run: %v\n%s", err, output)
	}
	got := string(output)
	for _, want := range []string{"<b>main:package</b>", "<i>page:package</i>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("cross-package template output missing %q:\n%s", want, got)
		}
	}
}

func TestCompileFilesSupportsHTTPResponseHelpersAndErrorTemplates(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "context"
    "fmt"
    "io"
    stdhttp "net/http"
    "strings"
    "time"
    http "gpp/http"
    "gpp/tpl"
)

template Page(value string) {
    <p>{{.}}</p>
}

class Post {
    Title string
}

template PathPage(post *Post) @{tpl.Path("/path-view/{id}")} {
    <h1>{{.Title}}</h1>
}

template RootPage(post *Post) @{tpl.Path("/")} {
    <h1>{{.Title}}</h1>
}

template ErrorPage(data any) {
    <h1>custom {{.Code}} {{.Message}}</h1>
}

class App : http.Server @{http.IP("127.0.0.1")} {
    func ErrorTemplate() string {
        return "ErrorPage"
    }

    func Root(ctx *http.Context) error @{http.GET("/")} {
        post := Post(Title: "selected by root path")
        return ctx.HTML(200, post)
    }

    func Created(ctx *http.Context) error @{http.GET("/created")} {
        return ctx.Text(201, "created")
    }

    func Accepted(ctx *http.Context) error @{http.GET("/accepted")} {
        return ctx.JSON(202, record(OK: true))
    }

    func View(ctx *http.Context) error @{http.GET("/view")} {
        return ctx.Template(202, "hello", "Page")
    }

    func PathView(ctx *http.Context) error @{http.GET("/path-view/{id}")} {
        post := Post(Title: "selected by request path")
        // Keep the obsolete alias covered while callers migrate to HTML.
        return ctx.Template(200, post)
    }

    func MissingTemplate(ctx *http.Context) error @{http.GET("/no-template")} {
        return ctx.HTML(201)
    }

    func Broken(ctx *http.Context) error @{http.GET("/broken")} {
        return fmt.Errorf("boom")
    }
}

func request(base string, path string) {
    response, err := stdhttp.Get(base + path)
    if err != nil { panic(err) }
    body, err := io.ReadAll(response.Body)
    if err != nil { panic(err) }
    response.Body.Close()
    text := string(body)
    fmt.Println(path, response.StatusCode,
        strings.HasPrefix(response.Header.Get("Content-Type"), "text/plain"),
        strings.HasPrefix(response.Header.Get("Content-Type"), "application/json"),
        strings.HasPrefix(response.Header.Get("Content-Type"), "text/html"),
        strings.Contains(text, "Page not found"),
        strings.Contains(text, "custom 404 Not Found"),
        strings.Contains(text, "500 Internal Server Error"),
        text,
    )
}

func main() {
    if err := tpl.Execute(io.Discard, "Page", "hello"); err != nil { panic(err) }
    app := App()
    done := make(chan error, 1)
    go func() { done <- app.Listen(0) }()
    for app.HTTPServer == nil {
        select {
        case err := <-done:
            if err != nil { fmt.Println("SKIP:", err); return }
        default:
        }
        time.Sleep(time.Millisecond)
    }
    base := "http://" + app.HTTPServer.Addr
    request(base, "/")
    request(base, "/created")
    request(base, "/accepted")
    request(base, "/view")
    request(base, "/path-view/42")
    request(base, "/no-template")
    request(base, "/missing")
    request(base, "/broken")
    if err := app.Shutdown(context.Background()); err != nil { panic(err) }
    if err := <-done; err != nil && err != stdhttp.ErrServerClosed { panic(err) }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated HTTP response helper program did not run: %v\n%s", err, output)
	}
	if strings.HasPrefix(string(output), "SKIP:") {
		t.Skipf("loopback sockets unavailable: %s", output)
	}
	result := string(output)
	for _, expected := range []string{
		"/ 200 false false true",
		"/created 201 true false false",
		"/accepted 202 false true false",
		"/view 202 false false true",
		"/path-view/42 200 false false true",
		"/no-template 500 false false true false false true",
		"/missing 404 false false true false true false",
		"/broken 500 false false true false false true",
		"<p>hello</p>",
		"<h1>selected by root path</h1>",
		"<h1>selected by request path</h1>",
		`{"OK":true}`,
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("HTTP response helper output missing %q:\n%s", expected, output)
		}
	}
}

func TestCompileFilesEmbedsFilesAndDirectories(t *testing.T) {
	inputDir := t.TempDir()
	resourceDir := filepath.Join(inputDir, "static")
	if err := os.MkdirAll(resourceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourceDir, "index.txt"), []byte("embedded directory\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "schema.sql"), []byte("embedded file\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
import (
    "fmt"
    "io/fs"
)

embed (
    Assets "static/"
    Schema "schema.sql"
)

func main() {
    contents, err := fs.ReadFile(Assets, "index.txt")
    if err != nil { panic(err) }
    fmt.Print(string(contents))
    fmt.Print(string(Schema))
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	outputDir := t.TempDir()
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated", ProjectRoot: inputDir}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("embedded resources did not run: %v\n%s", err, output)
	}
	if string(output) != "embedded directory\nembedded file\n" {
		t.Fatalf("unexpected embedded resource output: %s", output)
	}
}

func TestCompileFilesGeneratesDefaultStringAndDump(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "fmt"
)

class Person {
    Name string
    Age int
}

class Employee : Person {
    Id int
}

class Custom {
    Name string

    func String() string {
        return "custom:" + this.Name
    }
}

func main() {
    employee := Employee("Ada", 42, 7)
    fmt.Println(employee)
    fmt.Println(employee.Dump())

	custom := Custom("Secret")
	fmt.Println(custom.String())
	fmt.Println(custom.Dump())
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated default object methods did not run: %v\n%s", err, output)
	}
	expected := "Employee{Name: \"Ada\", Age: 42, Id: 7}\nEmployee {\n    Name: \"Ada\"\n    Age: 42\n    Id: 7\n}\ncustom:Secret\nCustom {\n    Name: \"Secret\"\n}\n"
	if string(output) != expected {
		t.Fatalf("unexpected default object formatting:\n%s", output)
	}
}

func TestCompileFilesSupportsEnums(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "encoding/json"
    "fmt"
)

enum Status int {
    Pending
    Active = 7
    Disabled
}

enum Role string {
    User
    Admin = "administrator"
}

enum (
    Level uint8 {
        Low
        High
    }
)

func main() {
    status := Status.Active
    fmt.Println(status == Status.Active, status.name, status.value)
    autoStatus := Status.From(7)
    fmt.Println(autoStatus.name)
    for _, item := range Status.values {
        fmt.Println(item.name, item.value)
    }

    role, err := Role.From("administrator")
    if err != nil { panic(err) }
    fmt.Println(role.name, role.value)
    _, err = Role.From("unknown")
    fmt.Println(err != nil, Level.High.value)

    encoded, err := json.Marshal(Role.Admin)
    if err != nil { panic(err) }
    var decoded Role
    if err := json.Unmarshal(encoded, &decoded); err != nil { panic(err) }
    _, invalidJSONErr := json.Marshal(Role("unknown"))
    fmt.Println(string(encoded), decoded.name, invalidJSONErr != nil)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated enum program did not run: %v\n%s", err, output)
	}
	expected := "true Active 7\nActive\nPending 0\nActive 7\nDisabled 8\nAdmin administrator\ntrue 1\n\"administrator\" Admin true\n"
	if string(output) != expected {
		t.Fatalf("unexpected enum output:\n%s", output)
	}
}

func TestCompileFilesSupportsImportedEnums(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sharedPath := filepath.Join(inputDir, "shared.gpp")
	mainPath := filepath.Join(inputDir, "main.gpp")
	if err := os.WriteFile(sharedPath, []byte(`
package shared

enum Status string {
    Pending = "pending"
    Active = "active"
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(`
package main

import (
    "fmt"
    shared
)

func main() {
    status := shared.Status.Active
    fmt.Println(status.name, status.value, shared.Status.Pending.name)
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{sharedPath, mainPath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated imported enum program did not run: %v\n%s", err, output)
	}
	if string(output) != "Active active Pending\n" {
		t.Fatalf("unexpected imported enum output: %s", output)
	}
}

func TestCompileFilesResolvesModulePackagesAcrossLayouts(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "packages in one directory",
			files: map[string]string{
				"context.gpp": `package api
class Context {}
`,
				"server.gpp": `package api
class Server {
    func Handle(ctx *Context) error { return nil }
}
`,
				"common.gpp": `package common
import "fixture/api"
class Server: api.Server {}
`,
				"app.gpp": `package app
import "fixture/common"
class App: common.Server {}
`,
			},
		},
		{
			name: "packages in conventional directories",
			files: map[string]string{
				"api/context.gpp": `package api
class Context {}
`,
				"api/server.gpp": `package api
class Server {
    func Handle(ctx *Context) error { return nil }
}
`,
				"common/server.gpp": `package common
import "fixture/api"
class Server: api.Server {}
`,
				"app/app.gpp": `package app
import "fixture/common"
class App: common.Server {}
`,
			},
		},
		{
			name: "nested dotted packages split across files",
			files: map[string]string{
				"internal/api/context.gpp": `package internal.api
class Context {}
`,
				"internal/api/server.gpp": `package internal.api
class Server {
    func Handle(ctx *Context) error { return nil }
}
`,
				"internal/common/server.gpp": `package internal.common
import "fixture/internal/api"
class Server: api.Server {}
`,
				"internal/common/extra.gpp": `package internal.common
class Extra {}
`,
				"cmd/app/app.gpp": `package cmd.app
import "fixture/internal/common"
class App: common.Server {}
`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inputDir := t.TempDir()
			outputDir := t.TempDir()
			paths := make([]string, 0, len(test.files))
			for relative, source := range test.files {
				path := filepath.Join(inputDir, relative)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(source), 0644); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, path)
			}

			if err := CompileFilesWithOptions(paths, outputDir, CompileOptions{
				ModulePath: "fixture",
				NoStdlib:   true,
				NoPrelude:  true,
			}); err != nil {
				t.Fatalf("compiling module-local package references: %v", err)
			}

			command := exec.Command("go", "build", "./...")
			command.Dir = outputDir
			command.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "go-cache"), "GOPROXY=off")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("generated Go did not build: %v\n%s", err, output)
			}
		})
	}
}

func TestCompileFilesSupportsExpressionCatch(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "fmt"

var fallbackCalls int

func ParsePort(value string) (int, error) {
    if value == "" {
        return 0, fmt.Errorf("empty port")
    }
    return 8080, nil
}

func Fallback() int {
    fallbackCalls++
    return 42
}

func Broken() int {
    throw fmt.Errorf("broken")
    return 0
}

func main() {
    port := ParsePort("") ?? 8080
    configured := ParsePort("configured") ?? 8080
    offset := ParsePort("configured") + 1 ?? 9000
    chained := ParsePort("") ?? ParsePort("") ?? 3000
    recovered := Broken() ?? Fallback()
    fmt.Println(port, configured, offset, chained, recovered, fallbackCalls)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated expression-catch program did not run: %v\n%s", err, output)
	}
	if string(output) != "8080 8080 8081 3000 42 1\n" {
		t.Fatalf("unexpected expression-catch output: %s", output)
	}
}

func TestCompileFilesSupportsExpressionCatchInClassMethods(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "fmt"
    "strconv"
)

class App {
    func Price() float64 {
        return strconv.ParseFloat("invalid", 64) ?? -1
    }
}

func main() {
    app := App()
    fmt.Println(app.Price())
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated class expression-catch program did not run: %v\n%s", err, output)
	}
	if string(output) != "-1\n" {
		t.Fatalf("unexpected class expression-catch output: %s", output)
	}
}

func TestCompileFilesSupportsSafeAccessCoalescing(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "fmt"

class Manager {
    Name string
}

class Person {
    Name string
    Manager *Manager

    func Greeting() string {
        return "Hello " + this.Name
    }
}

var fallbackCalls int

func fallback() string {
    fallbackCalls++
    return "Unknown"
}

func main() {
    var person *Person
    fmt.Println(person?.Name ?? fallback())
    fmt.Println(person?.Greeting() ?? "Hello, stranger")

    person = &Person{Name: ""}
    fmt.Println(person?.Name ?? fallback())

    person = &Person{Name: "Ada"}
    fmt.Println(person?.Manager?.Name ?? "No manager")
    fmt.Println(fallbackCalls)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated nil-safe coalescing program did not run: %v\n%s", err, output)
	}
	if string(output) != "Unknown\nHello, stranger\n\nNo manager\n1\n" {
		t.Fatalf("unexpected nil-safe coalescing output: %q", output)
	}
}

func TestCompileFilesExpressionCatchPreservesOrdinaryPanics(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

func Buggy() int {
    panic("ordinary panic")
}

func main() {
    _ = Buggy() ?? 42
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "ordinary panic") || strings.Contains(string(output), "\n42\n") {
		t.Fatalf("ordinary panic was not preserved by ??; error=%v output=%s", err, output)
	}
}

func TestCompileFilesCoalesceDoesNotRecoverOrdinaryNilDereference(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "fmt"

class Person {
    Name string
}

func main() {
    var person *Person
    fmt.Println(person.Name ?? "Unknown")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "invalid memory address or nil pointer dereference") || strings.Contains(string(output), "Unknown") {
		t.Fatalf("ordinary nil dereference was not preserved by ??; error=%v output=%s", err, output)
	}
}

func TestCompileFilesSupportsExceptionsAndImplicitErrorPromotion(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "fmt"
    "os"
)

class ValidationError {
    Field string
    Message string

    func Error() string {
        return this.Field + ": " + this.Message
    }
}

func ReadName(path string) string {
    data := os.ReadFile(path)
    return string(data)
}

func Save() error {
    return fmt.Errorf("save failed")
}

func main() {
    try {
        ReadName("/definitely/missing/gpp-file")
    } catch os.PathError e {
        fmt.Println("path", e.Op)
    } finally {
        fmt.Println("cleanup")
    }

    try {
        Save()
    } catch e {
        fmt.Println(e)
    }

    try {
        throw ValidationError(Field: "email", Message: "invalid")
    } catch ValidationError e {
        fmt.Println(e.Field)
    }

    try {
        throw ValidationError{Field: "name", Message: "required"}
    } catch *ValidationError e {
        fmt.Println(e.Field)
    }

    _, err := os.ReadFile("/definitely/missing/gpp-file")
    fmt.Println(err != nil)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated exception program did not run: %v\n%s", err, output)
	}
	expected := "path open\ncleanup\nsave failed\nemail\nname\ntrue\n"
	if string(output) != expected {
		t.Fatalf("unexpected exception output:\n%s", output)
	}
}

func TestCompileFilesSupportsMultiTypeCatch(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "fmt"
    "os"
    "net/url"
)

class ValidationError {
    Message string

    func Error() string {
        return this.Message
    }
}

class PermissionError {
    Message string

    func Error() string {
        return this.Message
    }
}

func main() {
    try {
        throw ValidationError(Message: "invalid")
    } catch ValidationError, PermissionError e {
        fmt.Println("client", e.Error())
    } catch e {
        fmt.Println("unexpected", e)
    }

    try {
        throw PermissionError{Message: "denied"}
    } catch ValidationError, PermissionError e {
        fmt.Println("client", e.Error())
    }

    try {
        os.ReadFile("/definitely/missing/gpp-multicatch-file")
    } catch os.PathError, url.Error e {
        fmt.Println("native", e.Error() != "")
    }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated multi-catch program did not run: %v\n%s", err, output)
	}
	expected := "client invalid\nclient denied\nnative true\n"
	if string(output) != expected {
		t.Fatalf("unexpected multi-catch output:\n%s", output)
	}
}

func TestCompileFilesCatchesErrorValues(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
	"database/sql"
	"errors"
	"fmt"
)

func find() error {
	try {
		throw sql.ErrNoRows
	} catch sql.ErrNoRows {
		return nil
	}
	return fmt.Errorf("not caught: %w", errors.New("unexpected"))
}

func main() {
	fmt.Println(find() == nil)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated error-value catch program did not run: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "true" {
		t.Fatalf("error-value catch did not handle sql.ErrNoRows and return nil: %s", output)
	}
}

func TestEmitRejectsInvalidMultiTypeCatch(t *testing.T) {
	duplicate, err := ParseFile("duplicate-multicatch.gpp", `
class ValidationError {
    func Error() string { return "invalid" }
}

func main() {
    try {
        throw ValidationError()
    } catch ValidationError, ValidationError e {
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate catch type ValidationError") {
		t.Fatalf("expected duplicate multi-catch diagnostic, got %v", err)
	}

	invalid, err := ParseFile("invalid-multicatch.gpp", `
class ValidationError {
    func Error() string { return "invalid" }
}

func main() {
    try {
        throw ValidationError()
    } catch string, ValidationError e {
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(invalid); err == nil || !strings.Contains(err.Error(), "invalid catch type string") {
		t.Fatalf("expected invalid multi-catch diagnostic, got %v", err)
	}
}

func TestEmitRejectsNonErrorThrow(t *testing.T) {
	file, err := ParseFile("throw.gpp", `
func main() {
    throw "not an error"
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(file); err == nil || !strings.Contains(err.Error(), "cannot throw string") {
		t.Fatalf("expected non-error throw diagnostic, got %v", err)
	}
}

func TestEmitPromotesNativeMethodErrors(t *testing.T) {
	file, err := ParseFile("method-error.gpp", `
import "os"

func Size(file *os.File) int64 {
    info := file.Stat()
    return info.Size()
}
`)
	if err != nil {
		t.Fatal(err)
	}
	output, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "__gppUnwrap(file.Stat())") {
		t.Fatalf("native method error was not promoted:\n%s", output)
	}
}

func TestEmitPreservesDiscardedGoroutineResults(t *testing.T) {
	file, err := ParseFile("goroutine-error.gpp", `
import "os"

func main() {
    go os.ReadFile("config.json")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	output, err := Emit(file)
	if err != nil {
		t.Fatalf("go statement should preserve Go's discarded-result behavior: %v", err)
	}
	if !strings.Contains(string(output), `go os.ReadFile("config.json")`) {
		t.Fatalf("goroutine call was unexpectedly rewritten:\n%s", output)
	}
}

func TestCompileFilesPromotesMoreThanThreeNonErrorResults(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "fmt"

func Four() (int, int, int, int, error) {
    return 1, 2, 3, 4, nil
}

func main() {
    a, b, c, d := Four()
    fmt.Println(a + b + c + d)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated multi-result program did not run: %v\n%s", err, output)
	}
	if string(output) != "10\n" {
		t.Fatalf("unexpected multi-result output: %s", output)
	}
}

func TestCompileFilesConvertsThrownErrorsAtGoABIBoundary(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "fmt"
    "os"
)

func ReadConfig(path string) (string, error) {
    data := os.ReadFile(path)
    return string(data), nil
}

func main() {
    _, err := ReadConfig("missing-config.json")
    fmt.Println(err != nil)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated ABI-boundary program did not run: %v\n%s", err, output)
	}
	if string(output) != "true\n" {
		t.Fatalf("unexpected ABI-boundary output: %s", output)
	}
}

func TestCompileFilesPreservesMultiResultNativeReturns(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "encoding/json"
    "fmt"
)

func Encode(value any) ([]byte, error) {
    return json.Marshal(value)
}

func main() {
    data, err := Encode(map[string]string{"name": "Ada"})
    if err != nil { panic(err) }
    fmt.Println(string(data))
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated native multi-result return program did not run: %v\n%s", err, output)
	}
	if string(output) != "{\"name\":\"Ada\"}\n" {
		t.Fatalf("unexpected native multi-result output: %s", output)
	}
}

func TestCompileFilesPreservesReturnThroughFinally(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "fmt"

func choose(early bool) int {
    try {
        if early {
            return 7
        }
    } finally {
        fmt.Println("cleanup")
    }
    return 9
}

func chooseCatch() int {
    try {
        throw fmt.Errorf("stop")
    } catch error {
        return 11
    } finally {
        fmt.Println("catch cleanup")
    }
    return 12
}

func namedReturn() (value int) {
    value = 13
    try {
        return
    } finally {
        fmt.Println("named cleanup")
    }
}

func earlyExit(shouldExit bool) {
    try {
        if shouldExit {
            return
        }
    } finally {
        fmt.Println("void cleanup")
    }
    fmt.Println("after void try")
}

func main() {
    fmt.Println(choose(true))
	fmt.Println(choose(false))
	fmt.Println(chooseCatch())
	fmt.Println(namedReturn())
	earlyExit(true)
	earlyExit(false)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated return/finally program did not run: %v\n%s", err, output)
	}
	if string(output) != "cleanup\n7\ncleanup\n9\ncatch cleanup\n11\nnamed cleanup\n13\nvoid cleanup\nvoid cleanup\nafter void try\n" {
		t.Fatalf("unexpected return/finally output: %s", output)
	}
}

func TestEmitRejectsUnreachableCatchAndFinallyControlTransfer(t *testing.T) {
	catchFile, err := ParseFile("catch-order.gpp", `
func main() {
    try {
        panic("ordinary panic")
    } catch error e {
    } catch error e {
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(catchFile); err == nil || !strings.Contains(err.Error(), "unreachable catch") {
		t.Fatalf("expected unreachable catch diagnostic, got %v", err)
	}

	finallyFile, err := ParseFile("finally-transfer.gpp", `
func main() {
    try {
    } finally {
        return
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(finallyFile); err == nil || !strings.Contains(err.Error(), "control transfer from finally") {
		t.Fatalf("expected finally control-transfer diagnostic, got %v", err)
	}
}

func TestCompileFilesGeneratesSerializationMethodsAndTags(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "gpp/encoding"

class User @{encoding.Serializable} {
    Id int @{encoding.Name("id")}
    Password string @{encoding.Ignore}
    Nickname string @{encoding.OmitEmpty}
}

func main() {
    user := User(Id: 1)
    data, _ := user.ToJSON()
    _, _ = User.FromJSON(data)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, expected := range []string{
		"Id               int       `json:\"id\" yaml:\"id\"`",
		"Password         string    `json:\"-\" yaml:\"-\"`",
		"Nickname         string    `json:\",omitempty\" yaml:\",omitempty\"`",
		"func (this *User) ToJSON() ([]byte, error)",
		"func (this *User) ToGOB() ([]byte, error)",
		"func GppStatic_User_FromJSON(data []byte) (User, error)",
		"return encoding.FromJSON[User](data)",
		"func (this User) GobEncode() ([]byte, error)",
		"func (this *User) GobDecode(data []byte) error",
		"func GppStatic_User_FromGOB(data []byte) (User, error)",
		"return encoding.FromGOB[User](data)",
		`{Name: "FromJSON", Parameters:`,
		`{Name: "FromGOB", Parameters:`,
		`FromJSON", Parameters: []GppParameter`,
		`FromGOB", Parameters: []GppParameter`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated serialization code is missing %q:\n%s", expected, text)
		}
	}
}

func TestCompileFilesRejectsIncompatibleSerializationMethod(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "gpp/encoding"

class User @{encoding.Serializable} {
    func ToJSON() string { return "custom" }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"})
	if err == nil || !strings.Contains(err.Error(), "encoding.Serializable requires User.ToJSON") {
		t.Fatalf("expected serialization method conflict, got %v", err)
	}
}

func TestCompileFilesPreservesAnnotationInheritanceMetadata(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "fmt"

annotation (
    Foo on class, field, method
    Bar on class
    Query on parameter
)

class Base @{Foo} {
    Id int @{Foo}

    func Search(q string @{Query}) string @{Foo} { return q }
    func Run() string @{Foo} { return "base" }
}

class Child : Base @{Bar} {
    func Run() string { return "child" }
}

func main() {
    fmt.Println(Child.class.annotations.has(Foo), Child.class.annotations.has(Bar))
    for parent := range Child.class.parents {
        fmt.Println(parent.name, parent.annotations.has(Foo))
    }
    for field := range Child.class.fields {
        fmt.Println(field.name, field.owner.name, field.annotations.has(Foo))
    }
    for method := range Child.class.methods {
        fmt.Println(method.name, method.owner.name, method.annotations.has(Foo))
        for parameter := range method.parameters {
            fmt.Println(parameter.name, parameter.annotations.has(Query))
        }
    }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("annotation inheritance example did not run: %v\n%s", err, output)
	}
	expected := "false true\nBase true\nId Base true\nRun Child false\nString Child false\nDump Child false\nSearch Base true\nq true\n"
	if string(output) != expected {
		t.Fatalf("unexpected annotation inheritance metadata:\n%s", output)
	}
}

func TestCompileFilesBuildsBundledORMModelBoundary(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
package main

import (
    orm "gpp/orm"
)

class Employee : orm.Model @{orm.Table("employees")} {
    Id int64 @{orm.Column("id"), orm.PK}
}

func useORM(exec orm.SQLExecutor) error {
    employee := Employee()
    return employee.Insert(exec)
}

func main() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated ORM model boundary did not build: %v\n%s", err, output)
	}
}

func TestCompileFilesResolvesClassesAcrossDottedPackageFiles(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()

	classFile := filepath.Join(inputDir, "person.gpp")
	userFile := filepath.Join(inputDir, "use_person.gpp")

	if err := os.WriteFile(classFile, []byte(`package telga.web

class Person {
    Name string
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, []byte(`package telga.web

func NewPerson() *Person {
    return Person("Bob")
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFiles([]string{classFile, userFile}, outputDir); err != nil {
		t.Fatal(err)
	}

	generatedPath := filepath.Join(outputDir, "telga", "web", "use_person.go")
	generated, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(generated), "package web") {
		t.Fatalf("generated file has incorrect package name:\n%s", generated)
	}
	if !strings.Contains(string(generated), `&Person{Name: "Bob"}`) {
		t.Fatalf("class resolution did not lower the constructor:\n%s", generated)
	}
}

func TestCompileFilesAllowsClassNameMatchingImportedParent(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import http "gpp/http"

class Server : http.Server {}

func main() {
	app := Server()
	_ = app
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatalf("class name matching imported parent should compile: %v", err)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated class with imported name collision did not build: %v\n%s", err, output)
	}
}

func TestCompileFilesIncludesTransitiveImportsForInheritedMethods(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sources := map[string]string{
		"core.gpp": `package demo.core

import (
    "context"
    "net"
    std "gpp/std"
)

class Server {
    func Serve(listener net.Listener) error @{std.Hook} { return nil }
    func Shutdown(ctx context.Context) error { return nil }
}
`,
		"common.gpp": `package demo.common

import (
    demo.core
)

class Server : core.Server {}
`,
		"apps.gpp": `package demo.apps

import (
    demo.common
)

class Server : common.Server {}
`,
		"main.gpp": `package main

import (
    demo.apps
)

func main() {
    _ = apps.Server()
}
`,
	}
	paths := make([]string, 0, len(sources))
	for name, source := range sources {
		path := filepath.Join(inputDir, name)
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if err := CompileFilesWithOptions(paths, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE="+t.TempDir(), "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("transitive imports used by inherited methods were not available in derived packages: %v\n%s", err, output)
	}
}

func TestCompileFilesWithOptionsCreatesAndChecksGoModule(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(sourcePath, []byte("func main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	options := CompileOptions{ModulePath: "example.com/acme/app"}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, options); err != nil {
		t.Fatal(err)
	}

	goMod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(goMod) != "module example.com/acme/app\n\ngo 1.27\n" {
		t.Fatalf("unexpected generated go.mod:\n%s", goMod)
	}

	err = CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "different.example/app"},
	)
	if err == nil || !strings.Contains(err.Error(), "generated module is") {
		t.Fatalf("expected module mismatch error, got %v", err)
	}
}

func TestCompileFilesWithOptionsUpgradesExistingGoModuleVersion(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	if err := os.WriteFile(sourcePath, []byte("func main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	goModPath := filepath.Join(outputDir, "go.mod")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}
	original := "module example.com/acme/app\n\ngo 1.26\n\nrequire example.com/dep v1.2.3\n"
	if err := os.WriteFile(goModPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "example.com/acme/app"}); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "go 1.27") || !strings.Contains(string(updated), "require example.com/dep v1.2.3") {
		t.Fatalf("existing generated go.mod was not upgraded while preserving dependencies:\n%s", updated)
	}
}

func TestCompileFilesWithOptionsUpdatesModuleForIncrementalOutput(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	if err := os.WriteFile(sourcePath, []byte("func main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{
		ModulePath:        "generated",
		IncrementalOutput: true,
	}); err != nil {
		t.Fatal(err)
	}
	const requestedModule = "github.com/example/project"
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{
		ModulePath:        requestedModule,
		IncrementalOutput: true,
	}); err != nil {
		t.Fatalf("incremental output should update its generated module path: %v", err)
	}
	goMod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goMod), "module "+requestedModule+"\n") {
		t.Fatalf("generated go.mod did not update its module path:\n%s", goMod)
	}
}

func TestCompileFilesWithOptionsCleansGeneratedOutput(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(sourcePath, []byte("func main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "stale.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "stale"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "stale", "nested.go"), []byte("package stale\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "generated", CleanOutput: true},
	); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "stale.go")); !os.IsNotExist(err) {
		t.Fatalf("stale generated file still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "stale")); !os.IsNotExist(err) {
		t.Fatalf("stale generated directory still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "go.mod")); err != nil {
		t.Fatalf("generated module was not created: %v", err)
	}
}

func TestCompileFilesEmitsPreludeOncePerPackage(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	first := filepath.Join(inputDir, "first.gpp")
	second := filepath.Join(inputDir, "second.gpp")

	if err := os.WriteFile(first, []byte(`package demo

func First() {}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte(`package demo

func Second() bool {
    values := []int{1, 2}
    return values.Any(func(value int) bool { return value == 2 })
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{first, second},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(outputDir, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(outputDir, "demo", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		count += strings.Count(string(data), "func GppPreludeExt___T_Any_")
	}
	if count != 1 {
		t.Fatalf("expected one generated prelude helper for the package, got %d", count)
	}
}

func TestCompileFilesLowersQualifiedCrossPackageConstructor(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()

	classFile := filepath.Join(inputDir, "person.gpp")
	mainFile := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(classFile, []byte(`package telga.web

class Person {
    Name string
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainFile, []byte(`package main

import telga.web

func main() {
    person := web.Person("Bob")
    _ = person
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{classFile, mainFile},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `&web.Person{Name: "Bob"}`) {
		t.Fatalf("qualified constructor was not lowered:\n%s", generated)
	}
}

func TestCompileFilesLowersCrossPackagePolymorphicTypes(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()

	classFile := filepath.Join(inputDir, "people.gpp")
	mainFile := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(classFile, []byte(`package telga.web

class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainFile, []byte(`package main

import telga.web

func SpeakFor(person web.Person) string {
    return person.Speak()
}

func MakePerson() web.Person {
    return web.Employee()
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{classFile, mainFile},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"func SpeakFor(person web.GppPerson) string",
		"func MakePerson() web.GppPerson",
		"return &web.Employee{}",
	} {
		if !strings.Contains(string(generated), expected) {
			t.Fatalf("cross-package polymorphism missing %q:\n%s", expected, generated)
		}
	}
}

func TestCompileFilesPreservesSubstitutabilityAcrossInheritanceBoundaries(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `import "fmt"

class Animal {
    Name string

    func Kind() string { return "animal" }
    func Describe() string { return this.Name + ":" + this.Kind() }
    func Rename(name string) { this.Name = name }
}

class Dog: Animal {
    func Kind() string { return "dog" }
}

class Cat: Animal {
    func Kind() string { return "cat" }
}

class Shelter {
    Resident Animal
}

func DescribeAnimal(animal Animal) string {
    return animal.Describe()
}

func MakeAnimal(name string) Animal {
    return Dog(Name: name)
}

class AnimalFactory {
    func Make() Animal {
        return Cat(Name: "Mittens")
    }
}

func main() {
    dog := Dog(Name: "Rex")
    fmt.Println(DescribeAnimal(dog))
    var animal Animal = dog
    fmt.Println(DescribeAnimal(animal))

    animal.Rename("Max")
    fmt.Println(dog.Name)
    fmt.Println(DescribeAnimal(MakeAnimal("Buddy")))

    factory := AnimalFactory()
    fmt.Println(DescribeAnimal(factory.Make()))

    shelter := Shelter(Resident: dog)
    fmt.Println(DescribeAnimal(shelter.Resident))

    animal = Cat(Name: "Mittens")
    fmt.Println(DescribeAnimal(animal))
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated inheritance substitutability program did not run: %v\n%s", err, output)
	}
	want := "Rex:dog\nRex:dog\nMax\nBuddy:dog\nMittens:cat\nMax:dog\nMittens:cat\n"
	if string(output) != want {
		t.Fatalf("unexpected polymorphic behavior:\n got: %q\nwant: %q", output, want)
	}
}

func TestCompileFilesPreservesMultipleInheritanceDispatch(t *testing.T) {
	outputDir := t.TempDir()
	sourcePath := filepath.Join("..", "examples", "inheritance.gpp")
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatalf("compile multiple inheritance example: %v", err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated multiple inheritance program did not run: %v\n%s", err, output)
	}
	want := "employee: Ada (engineer)\n125000\nemployee: Ada (engineer)\n125000\nnamed: Ada\n120000\n120000\n"
	if string(output) != want {
		t.Fatalf("unexpected multiple inheritance behavior:\n got: %q\nwant: %q", output, want)
	}
}

func TestCompileFilesResolvesFunctionOverloadsAcrossFiles(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	first := filepath.Join(inputDir, "add_one.gpp")
	second := filepath.Join(inputDir, "add_two.gpp")
	use := filepath.Join(inputDir, "use.gpp")

	for filename, source := range map[string]string{
		first: `func Add(value int) int { return value }
`,
		second: `func Add(left int, right int) int { return left + right }
`,
		use: `func Use() int { return Add(1) + Add(1, 2) }
`,
	} {
		if err := os.WriteFile(filename, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := CompileFiles([]string{first, second, use}, outputDir); err != nil {
		t.Fatal(err)
	}

	generated, err := os.ReadFile(filepath.Join(outputDir, "use.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "Add__gpp_1(1)") ||
		!strings.Contains(string(generated), "Add__gpp_2(1, 2)") {
		t.Fatalf("cross-file overload calls were not resolved:\n%s", generated)
	}
}

func TestCompileFilesGeneratedCodeBuildsWithPolymorphism(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
import "fmt"

class Person {
    Name string

    func Speak() string {
        return this.Name
    }
}

class Employee: Person {
    func Speak() string {
        return "employee: " + this.Name
    }
}

class Holder {
    Value Person
}

class Factory {
    func Make() Employee {
        return Employee("factory")
    }
}

func Announce(person Person) string {
    return person.Speak()
}

func MakePerson() Person {
    return Person("base")
}

func main() {
    var person Person = Employee("Bob")
    holder := Holder(Employee("Alice"))
    factory := Factory()
    fmt.Println(person.Speak(), holder.Value.Speak(), MakePerson().Speak())
    fmt.Println(Announce(factory.Make()))
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "test", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Go did not build: %v\n%s", err, output)
	}
}

func TestCompileFilesDeduplicatesRecordShapesAcrossFiles(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	first := filepath.Join(inputDir, "first.gpp")
	second := filepath.Join(inputDir, "second.gpp")

	for filename, source := range map[string]string{
		first:  "func First() record { return record(name: \"A\") }\n",
		second: "func Second() record { return record(name: \"B\") }\n",
	} {
		if err := os.WriteFile(filename, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := CompileFiles([]string{first, second}, outputDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(outputDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		count += strings.Count(string(data), "type __gpp_record_")
	}
	if count != 1 {
		t.Fatalf("expected one generated record type across package files, got %d", count)
	}
}

func TestCompileFilesSupportsExportedCrossPackageRecords(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	people := filepath.Join(inputDir, "people.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(people, []byte(`package demo.people

func GetUser() record {
    return record(Name: "Bob", Age: 42)
}

`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import (
    "fmt"
    demo.people
)

func main() {
    user := people.GetUser()
    fmt.Println(user.Name, user.Age)
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{people, main},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "test", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated cross-package record code did not build: %v\n%s", err, output)
	}
}

func TestCompileFilesPreservesRecordFieldVisibility(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "encoding/json"
    "fmt"
)

func GetUser() record {
    return record(
        Name: "Bob",
        internal: "debug",
    )
}

func main() {
    user := GetUser()
    encoded, err := json.Marshal(user)
    if err != nil {
        panic(err)
    }
    fmt.Println(user.Name, user.internal, string(encoded))
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated record visibility program did not run: %v\n%s", err, output)
	}
	if string(output) != `Bob debug {"Name":"Bob"}
` {
		t.Fatalf("unexpected record visibility output:\n%s", output)
	}
}

func TestCompileFilesGeneratedCodeBuildsWithIntrospection(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
import "fmt"

annotation Table(name string) on class

class Model {
    Id string

    func Describe() string {
        return this.class.name
    }
}

class Employee: Model @{Table("employees")} {
    Name string
}

class Manager: Employee {}

func main() {
    first := Employee("id", "Bob")
    second := Employee("id2", "Alice")
    field := Employee.class.fields[1]
    field.set(first, "Carol")
    fmt.Println(first.class.name, Employee.class.name, first.class == second.class)
    fmt.Println(field.name, field.get(first), field.addr(first) != nil)
    table := Employee.class.annotations.find(Table)
    fmt.Println(table.args[0])

    var model Model = Employee("id3", "Dan")
    fmt.Println(model.Describe())

    manager := Manager()
    fmt.Println(manager.Describe())
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated introspection Go did not run: %v\n%s", err, output)
	}
	expected := "Employee Employee true\nName Carol true\nemployees\nEmployee\nManager\n"
	if string(output) != expected {
		t.Fatalf("unexpected introspection output:\n%s\nwant:\n%s", output, expected)
	}
}

func TestCompileFilesSupportsCrossPackageClassIntrospection(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	people := filepath.Join(inputDir, "people.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(people, []byte(`package demo.people

class Person {
    Name string
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import (
    "fmt"
    demo.people
)

func main() {
    person := people.Person("Bob")
    fmt.Println(people.Person.class.name, person.class.name)
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{people, main},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated cross-package introspection Go did not run: %v\n%s", err, output)
	}
	if string(output) != "Person Person\n" {
		t.Fatalf("unexpected cross-package introspection output: %s", output)
	}
}

func TestCompileFilesPrefersNativeMethodsOverExtensions(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
import "database/sql"

extend *sql.DB {
    func Ping() bool {
        return true
    }
}

func Use(db *sql.DB) error {
    return db.Ping()
}

func main() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "test", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native method precedence did not produce buildable Go: %v\n%s", err, output)
	}
}

func TestCompileFilesSupportsMultiTargetImportedExtensions(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
import "database/sql"

extend *sql.DB, *sql.Tx {
    func ExecOne(query string, args ...any) error {
        _, err := this.Exec(query, args...)
        return err
    }
}

func main() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "test", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("multi-target imported extension did not produce buildable Go: %v\n%s", err, output)
	}
}

func TestCompileFilesResolvesExtensionsOnInheritedImportedFields(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	base := filepath.Join(inputDir, "base.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(base, []byte(`package demo.base

import "database/sql"

class Base {
    DB *sql.DB
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import (
    "database/sql"
    "fmt"
    demo.base
    gppsql "gpp/sql"
)

struct Person {
    Id int
    Name string
}

class Server: base.Base {
    func Rows() {
        rows := this.DB.Select[struct(ID: int)]("SELECT 1")
        _ = rows
    }
}

func Consume(db *sql.DB) {
    try {
        rows := db.Select[Person]("SELECT 1")
        rows.Each((value Person) => { fmt.Printf("%d %s\\n", value.Id, value.Name) })
    } catch err {
        println(err)
    }
}

func main() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{base, main}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "gppsql.GppExt_ptr_sql_DB_Select_") {
		t.Fatalf("extension call on inherited *sql.DB field was not lowered through gpp/sql:\n%s", generated)
	}
	if strings.Contains(string(generated), "rows.Each(") || !strings.Contains(string(generated), "GppPreludeExt___T_Each_") {
		t.Fatalf("Each callback inside try was not lowered alongside imported SQL extensions:\n%s", generated)
	}

	command := exec.Command("go", "build", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("imported extension on inherited field did not produce buildable Go: %v\n%s", err, output)
	}
}

func TestCompileSQLExampleLowersEachInsideTry(t *testing.T) {
	outputDir := t.TempDir()
	sourcePath := filepath.Join("..", "examples", "sql.gpp")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	functions := parseTopLevelFunctions(sourcePath, "main", string(source), 0, string(source), "", 0)
	for _, function := range functions {
		if function == nil || function.Method.BodyAST == nil {
			continue
		}
		collectExtensionASTCalls(function.Method.BodyAST, func(call *CallExpr) bool {
			if _, name, _, ok := extensionASTCallParts(call); ok && name == "Each" {
				if len(call.Arguments) != 1 || call.Arguments[0].Value == nil {
					t.Errorf("parsed Each call has incomplete argument AST: %#v", call.Arguments)
				}
			}
			return true
		})
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{
		ModulePath:  "example.com/gpp-example-smoke",
		Development: true,
	}); err != nil {
		t.Fatalf("SQL example with inline Each callback did not compile: %v", err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "sql.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(generated), "people.Each(") || !strings.Contains(string(generated), "GppPreludeExt___T_Each_") {
		t.Fatalf("inline Each call was not lowered in generated SQL example:\n%s", generated)
	}
}

func TestCompileFilesAppliesDefaultsToImportedChainedMethodCalls(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	base := filepath.Join(inputDir, "base.gpp")
	common := filepath.Join(inputDir, "common.gpp")
	server := filepath.Join(inputDir, "server.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(base, []byte(`package demo.http

class Server {
    func Listen(port int = 0) error { return nil }
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(common, []byte(`package demo.common

import demo.http

class Server : http.Server {}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(server, []byte(`package demo.server

import demo.common

class Server : common.Server {}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import demo.server

func main() {
    go server.Server().Listen()
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{base, common, server, main}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), ".Listen(0)") {
		t.Fatalf("default method argument was not added to a chained call:\n%s", generated)
	}

	command := exec.Command("go", "build", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("chained call with default argument did not produce buildable Go: %v\n%s", err, output)
	}
}

func TestCompileFilesResolvesPreludeExtensionsOnNestedAnonymousStructFields(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(main, []byte(`package main

func value() struct { return struct(c: struct(e: "ok")) }

func main() {
    blank := value().c.e.Trim(" ").IsBlank()
    _ = blank
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{main}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "GppPreludeExt_string_Trim_") || !strings.Contains(string(generated), "GppPreludeExt_string_IsBlank_") {
		t.Fatalf("string extensions on nested anonymous struct fields were not lowered:\n%s", generated)
	}

	command := exec.Command("go", "build", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("string extensions on nested anonymous struct fields did not produce buildable Go: %v\n%s", err, output)
	}
}

func TestCompileFilesLowersPreludeExtensionsInsideMapLambdas(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(main, []byte(`package main

func main() {
    values := []string{"x"}
    upper := values.Map(value => value.ToUpper())
    upperBlock := values.Map(value => { return value.ToUpper() })
    _ = upper
    _ = upperBlock
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{main}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "GppPreludeExt_string_ToUpper_") {
		t.Fatalf("string extension inside a Map lambda was not lowered:\n%s", generated)
	}

	command := exec.Command("go", "build", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache", "GOPROXY=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("string extension inside a Map lambda did not produce buildable Go: %v\n%s", err, output)
	}
}

func FuzzPreludeMapLambdaAcceptedProgramsBuild(f *testing.F) {
	f.Add([]byte{0})
	f.Add([]byte{1, 2, 0, 1})
	f.Add([]byte{0, 1, 2, 2})
	f.Fuzz(func(t *testing.T, choices []byte) {
		if len(choices) > 32 {
			t.Skip()
		}
		operations := []string{`.ToUpper()`, `.ToLower()`, `.Trim(" ")`}
		steps := 1
		if len(choices) > 0 {
			steps += int(choices[0] % 4)
		}
		body := "value"
		for i := 0; i < steps; i++ {
			choice := byte(i)
			if len(choices) > 0 {
				choice = choices[i%len(choices)]
			}
			body += operations[int(choice)%len(operations)]
		}
		block := len(choices) > 0 && choices[0]&1 != 0
		if block {
			body = "{ return " + body + " }"
		}
		inputDir := t.TempDir()
		outputDir := t.TempDir()
		main := filepath.Join(inputDir, "main.gpp")
		source := `package main

func main() {
    values := []string{"x"}
    mapped := values.Map(value => ` + body + `)
    _ = mapped
}
`
		if err := os.WriteFile(main, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		if err := CompileFilesWithOptions([]string{main}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
			return
		}
		command := exec.Command("go", "build", "./...")
		command.Dir = outputDir
		command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache", "GOPROXY=off")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("accepted Map lambda did not generate buildable Go: %v\n%s\nGo++ source:\n%s", err, output, source)
		}
	})
}

func FuzzInheritedDefaultMethodCallAcceptedProgramsBuild(f *testing.F) {
	f.Add(byte(0))
	f.Add(byte(1))
	f.Add(byte(2))
	f.Add(byte(3))
	f.Fuzz(func(t *testing.T, choice byte) {
		receiver := "server.Server()"
		prefix := ""
		if choice&4 != 0 {
			prefix = "    instance := server.Server()\n"
			receiver = "instance"
		}
		calls := []string{
			"go " + receiver + ".Listen()",
			receiver + ".Listen()",
			"go " + receiver + ".Listen(port: 8080)",
			receiver + ".Listen(8080)",
		}
		call := calls[int(choice)%len(calls)]
		inputDir := t.TempDir()
		outputDir := t.TempDir()
		base := filepath.Join(inputDir, "base.gpp")
		common := filepath.Join(inputDir, "common.gpp")
		server := filepath.Join(inputDir, "server.gpp")
		main := filepath.Join(inputDir, "main.gpp")
		sources := []struct {
			path   string
			source string
		}{
			{base, `package demo.http

class Server {
    func Listen(port int = 0) error { return nil }
}
`},
			{common, `package demo.common

import demo.http

class Server : http.Server {}
`},
			{server, `package demo.server

import demo.common

class Server : common.Server {}
`},
			{main, `package main

import demo.server

func main() {
` + prefix + `
    ` + call + `
}
`},
		}
		paths := make([]string, 0, len(sources))
		for _, file := range sources {
			if err := os.WriteFile(file.path, []byte(file.source), 0644); err != nil {
				t.Fatal(err)
			}
			paths = append(paths, file.path)
		}
		if err := CompileFilesWithOptions(paths, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
			return
		}
		command := exec.Command("go", "build", "./...")
		command.Dir = outputDir
		command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache", "GOPROXY=off")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("accepted inherited method call did not generate buildable Go: %v\n%s\nGo++ call: %s", err, output, call)
		}
	})
}

func TestCompileFilesSupportsImportedGoPlusExtensions(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	people := filepath.Join(inputDir, "people.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(people, []byte(`package demo.people

class Person {
    Name string
}

extend Person {
    func Greeting() string {
        return "Hello, " + this.Name
    }
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import (
    "fmt"
    demo.people
)

func main() {
    person := people.Person("Ada")
    fmt.Println(person.Greeting())
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{people, main}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("imported Go++ extension did not run: %v\n%s", err, output)
	}
	if string(output) != "Hello, Ada\n" {
		t.Fatalf("unexpected imported extension output: %s", output)
	}
}

func TestCompileFilesSupportsImportedStaticMethods(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	modelFile := filepath.Join(inputDir, "model.gpp")
	mainFile := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(modelFile, []byte(`package demo.model

class User {
    Name string

    static func Guest() *User {
        return User(Name: "Guest")
    }
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainFile, []byte(`package main

import (
    "fmt"
    demo.model
)

func main() {
    user := model.User.Guest()
    fmt.Println(user.Name)
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{modelFile, mainFile},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("imported static method did not run: %v\n%s", err, output)
	}
	if string(output) != "Guest\n" {
		t.Fatalf("unexpected imported static method output: %s", output)
	}
}

func TestCompileFilesBindsStaticSelfToCallingSubclass(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
class Base {
    static func Identity(value *self) *self {
        return value
    }
}

class Child : Base {
    Name string
}

func main() {
    child := Child.Identity(Child(Name: "Ada"))
    if child.Name != "Ada" {
        panic("static Self did not bind to Child")
    }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "GppStatic_Base_Identity[Child]") {
		t.Fatalf("inherited static Self call was not specialized to Child:\n%s", generated)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("static Self output did not compile and run: %v\n%s", err, output)
	}
}

func TestCompileFilesInheritedStaticSelfFactoryReturnsCallingClassPointer(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
class EntityFactory {
    static func New() *self {
        return new(self)
    }
}

class User : EntityFactory {
    Name string
}

func main() {
    user := User.New()
    user.Name = "Ada"
    if user.Name != "Ada" {
        panic("factory did not return a *User")
    }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatalf("documented inherited static self factory should compile: %v", err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "GppStatic_EntityFactory_New[User]") {
		t.Fatalf("factory call should specialize self to User:\n%s", generated)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("documented inherited static self factory did not run: %v\n%s", err, output)
	}
}

func TestCompileFilesLowersSliceMethodsOnInferredStaticSelfResults(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
class Base {
    static func Select() ([]*self, error) { return nil, nil }
}

class Child : Base {
    func Validate() error { return nil }
}

func main() {
    children := Child.Select()
    children.Each(func(child *Child) error { return nil })
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("slice method on inferred static Self result did not compile: %v\n%s", err, output)
	}
}

func TestCompileFilesInfersSliceTypeForStaticSelfResults(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
import "errors"

class Base {
    static func Select() ([]*self, error) { return nil, nil }
}

class Child : Base {
    func Validate() error { return errors.New("callback failed") }
}

func run() error {
    children := Child.Select()
    children = append(children, Child())
    children.Each(child => println("visited"))
    children.Each(child => child.Validate())
    return nil
}

func main() {
    err := run()
    if err == nil || err.Error() != "callback failed" {
        panic("Each did not propagate the callback error")
    }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("ignored Each error was not promoted to an exception: %v\n%s", err, output)
	} else if string(output) != "visited\n" {
		t.Fatalf("Each did not run the non-error callback for every element: %q", output)
	}
}

func TestCompileFilesGeneratesStaticORMModelQueries(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	modelPath := filepath.Join(inputDir, "model.gpp")
	sourcePath := filepath.Join(inputDir, "main.gpp")
	modelSource := `
package main

import orm "gpp/orm"

class Employee : orm.Model @{orm.Table("employees")} {
    Id int64 @{orm.Column("id"), orm.PK}
    Name string @{orm.Column("name")}
}
`
	source := `
package main

import (
    "database/sql"
    orm "gpp/orm"
)

func LoadEmployee(db *sql.DB, id int64) {
    employee := Employee.Get(db, "id = $1", id)
    _ = employee
}

func LoadEmployees(db *sql.DB, prefix string) {
    employees := Employee.Select(db, "name LIKE $1 ORDER BY name", prefix + "%")
    _ = employees
    employeesWithMultipleArgs := Employee.Select(db, "id = $1 AND name LIKE $2", int64(1), prefix + "%")
    _ = employeesWithMultipleArgs
}

func LoadByPrimaryKey(db *sql.DB, id int64) {
    employee := Employee.Find(db, id)
    _ = employee
    _ = Employee.All(db)
    _ = Employee.All(db, "name")
    _ = Employee.Page(db)
    _ = Employee.Page(db, 25)
    _ = Employee.Page(db, 50, id)
    _ = Employee.Page(exec: db, afterID: id)
    _ = Employee.First(db)
    _ = Employee.First(db, "name")
    _ = Employee.Last(db)
}

func DeleteModels(db *sql.DB, employee *Employee) {
    employee.Destroy(db)
    Employee.Truncate(db)
}

func PersistModel(db *sql.DB, employee *Employee) {
    employee.Insert(db)
    employee.Update(db)
}

func RawEmployee(db *sql.DB, id int64) {
	try {
		employee := orm.Get[Employee](db, "SELECT id, name FROM employees WHERE name = $1 AND id = $2", "Grace", id)
		_ = employee
	} catch err {
		_ = err
	}
}

func RawEmployeeWithExplicitError(db *sql.DB, id int64) {
	employee, err := orm.Get[Employee](db, "SELECT id, name FROM employees WHERE name = $1 AND id = $2", "Grace", id)
    _ = employee
	_ = err
}

func RawEmployees(db *sql.DB, prefix string) {
	try {
		employees := orm.Select[Employee](db, "SELECT id, name FROM employees WHERE name LIKE $1", prefix + "%")
		_ = employees
	} catch err {
		_ = err
	}
}

func RawEmployeesWithExplicitError(db *sql.DB, prefix string) {
	employees, err := orm.Select[Employee](db, "SELECT id, name FROM employees WHERE name LIKE $1", prefix + "%")
	_ = employees
	_ = err
}

func main() {}
`
	if err := os.WriteFile(modelPath, []byte(modelSource), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{modelPath, sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(generated), "employees := orm.GppStatic_Model_Select[Employee]") {
		t.Fatalf("static query error was not promoted:\n%s", generated)
	}
	if !strings.Contains(string(generated), "__gppUnwrap(orm.Get[Employee]") {
		t.Fatalf("generic imported ORM call inside try was not promoted:\n%s", generated)
	}
	if !strings.Contains(string(generated), "__gppUnwrap(orm.Select[Employee]") {
		t.Fatalf("generic imported ORM select inside try was not promoted:\n%s", generated)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("ORM static and full-query APIs did not compile: %v\n%s\nGenerated source:\n%s", err, output, generated)
	}
}

func TestCompileORMExamplePromotesGenericFullSQLQueries(t *testing.T) {
	examplePath := filepath.Join("..", "examples", "orm.gpp")
	outputDir := t.TempDir()
	if err := CompileFilesWithOptions([]string{examplePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "orm.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{
		"__gppUnwrap(orm.Get[Employee](db",
		"__gppUnwrap(orm.Select[Employee](db",
	} {
		if !strings.Contains(string(generated), call) {
			t.Errorf("ORM example call was not promoted: %s\nGenerated source:\n%s", call, generated)
		}
	}
}

func TestCompileFilesSupportsExportedImportedAnnotations(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	model := filepath.Join(inputDir, "model.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(model, []byte(`package demo.model

annotation Public on class
annotation private on class
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import demo.model

class Employee @{model.Public} {}

func main() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{model, main}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("imported annotation did not produce buildable Go: %v\n%s", err, output)
	}
}

func TestCompileFilesRejectsUnexportedImportedAnnotations(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	model := filepath.Join(inputDir, "model.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(model, []byte(`package demo.model

annotation private on class
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import demo.model

class Employee @{model.private} {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	err := CompileFilesWithOptions([]string{model, main}, outputDir, CompileOptions{ModulePath: "generated"})
	if err == nil || !strings.Contains(err.Error(), "undefined annotation model.private") {
		t.Fatalf("expected unexported annotation error, got %v", err)
	}
}
