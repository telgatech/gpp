package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProgramValidatesClassInitializers(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "static",
			src:  `class Sample { static func init() {} }`,
			want: "initializer `init` cannot be static",
		},
		{
			name: "invalid result",
			src:  `class Sample { func init() int { return 1 } }`,
			want: "must return no values or error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file, err := ParseFile("initializer.gpp", test.src)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ResolveProgram(&Program{Files: []*File{file}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected initializer diagnostic containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestResolveProgramRejectsClassErrorInitializerResult(t *testing.T) {
	file, err := ParseFile("initializer.gpp", `class InitError {
	func Error() string { return "init failed" }
}
class Sample {
	func init() *InitError { return nil }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProgram(&Program{Files: []*File{file}}); err == nil || !strings.Contains(err.Error(), "must return no values or error") {
		t.Fatalf("expected class error results to be rejected in favor of the error interface, got %v", err)
	}
}

func TestConstructorInitializerLoweringPreservesErrorResult(t *testing.T) {
	base := &ClassDecl{
		Name:   "Base",
		Fields: []Field{{Name: "Name", TypeAST: parseTypeText("string")}},
		Methods: []Method{{
			Name:         "init",
			ParameterAST: []ParameterNode{{Name: "name", Type: parseTypeText("string")}},
			ResultAST:    parseTypeText("error"),
		}},
	}
	context := localConstructorContext(map[string]*ClassDecl{"Base": base})
	transformed, handled, err := transformConstructorsAST(`func makeBase() (*Base, error) {
	return Base("value")
}`, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected constructor call to be lowered")
	}
	for _, expected := range []string{"func() (*Base, error)", "__gpp_instance := &Base{}", "GppInitialize(\"value\")", "return nil, __gpp_initErr"} {
		if !strings.Contains(transformed, expected) {
			t.Fatalf("expected lowered constructor to contain %q, got:\n%s", expected, transformed)
		}
	}
}

func TestClassConstructionUsesMatchingInitOverload(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `package main

import "fmt"

class Sample {
	Value int

	func init(value int) {
		this.Value = value + 1
	}

	func init(value string) {
		this.Value = len(value)
	}

	func init(value int, bonus int) {
		this.Value = value + bonus
	}
}

class Plain {
	Value int
}

func main() {
	fromFactory := Sample(4)
	fromOverload := Sample("four")
	fromTwoArguments := Sample(4, 3)
	fromNamedOverload := Sample(value: "four")
	fromNamedTwoArguments := Sample(bonus: 4, value: 5)
	plain := Plain(12)
	fmt.Println(fromFactory.Value, fromOverload.Value, fromTwoArguments.Value, fromNamedOverload.Value, fromNamedTwoArguments.Value, plain.Value)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatalf("compile overloaded initializer program: %v", err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated initializer program did not run: %v\n%s", err, output)
	}
	if got, want := strings.TrimSpace(string(output)), "5 4 7 4 9 12"; got != want {
		t.Fatalf("unexpected factory construction output: got %q, want %q", got, want)
	}
}

func TestClassConstructionRequiresMatchingInitOverload(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `package main

class User {
	Name string

	func init(name string) {
		this.Name = name
	}

	func init(name string, age int) {
		this.Name = name
	}
}

func main() {
	_ = User()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err == nil || !strings.Contains(err.Error(), "no matching") {
		t.Fatalf("expected User() to require a matching init overload, got %v", err)
	}
}

func TestPromotedCallRecognizesInitializerErrors(t *testing.T) {
	file, err := ParseFile("initializer.gpp", `class Base {
	func init() error { return nil }
}
class Child: Base {}`)
	if err != nil {
		t.Fatal(err)
	}
	model, err := ResolveProgram(&Program{Files: []*File{file}})
	if err != nil {
		t.Fatal(err)
	}
	context := localConstructorContext(model.Packages[file.Package].Classes)
	tokens, err := LexSource("call", `Child()`)
	if err != nil {
		t.Fatal(err)
	}
	expression, err := ParseExpressionTokens(tokens)
	if err != nil {
		t.Fatal(err)
	}
	call, ok := expression.(*CallExpr)
	if !ok {
		t.Fatalf("expected constructor call, got %T", expression)
	}
	result, ok := promotedCallForExpr(call, context, nil)
	if !ok || !result.trailingError || strings.Join(result.types, ",") != "*Child,error" {
		t.Fatalf("expected Child constructor to promote its inherited initializer error, got %#v (found=%v)", result, ok)
	}
}

func TestCompileFilesRunsClassInitializersInOrderAndPropagatesErrors(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `package main

import (
	"errors"
	"fmt"
)

class Base {
	Name string

	func init(name string) {
		this.Name = name + "-base"
		fmt.Println("base", name)
	}
}

class Middle: Base {
	func init(name string) error {
		super(name: name)
		fmt.Println("middle", this.Name)
		if this.Name == "bad-base" {
			return errors.New("middle rejected value")
		}
		return nil
	}
}

class Child: Middle {
	func init(name string) {
		super(name: name)
		fmt.Println("child", this.Name)
	}
}

class Left {
	func init() { fmt.Println("left") }
}

class Right {
	func init() { fmt.Println("right") }
}

class Combined: Left, Right {
	func init() { fmt.Println("combined") }
}

class NamedLeft {
	Label string

	func init(label string) {
		this.Label = label
		fmt.Println("named left", label)
	}
}

class NamedRight {
	Code int

	func init(code int) {
		this.Code = code
		fmt.Println("named right", code)
	}
}

class NamedCombined: NamedLeft, NamedRight {
	func init(label string, code int) {
		super[NamedRight](code: code)
		super[NamedLeft](label: label)
	}
}

class Delegated {
	Value int

	func init(value int) {
		this(bonus: 1, value: value)
	}

	func init(value int, bonus int) {
		this.Value = value + bonus
	}
}

func NewChild(name string) (*Child, error) {
	return Child(name: name)
}

func main() {
	good, err := Child(name: "good")
	fmt.Println("good", good.Name, err == nil)

	returned, err := NewChild("returned")
	fmt.Println("returned", returned.Name, err == nil)

	bad, err := Child(name: "bad")
	fmt.Println("bad", bad == nil, err)

	try {
		Child("bad")
	} catch error e {
		fmt.Println("caught", e)
	}

	_ = Combined()
	namedCombined := NamedCombined(code: 42, label: "named")
	fmt.Println("named parents", namedCombined.Label, namedCombined.Code)
	delegated := Delegated(value: 4)
	fmt.Println("delegated", delegated.Value)
	_ = Child{Middle: Middle{Base: Base{Name: "literal"}}}
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatalf("compile initializer program: %v", err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated initializer program did not run: %v\n%s", err, output)
	}
	want := strings.Join([]string{
		"base good",
		"middle good-base",
		"child good-base",
		"good good-base true",
		"base returned",
		"middle returned-base",
		"child returned-base",
		"returned returned-base true",
		"base bad",
		"middle bad-base",
		"bad true middle rejected value",
		"base bad",
		"middle bad-base",
		"caught middle rejected value",
		"left",
		"right",
		"combined",
		"named left named",
		"named right 42",
		"named parents named 42",
		"delegated 5",
		"",
	}, "\n")
	if string(output) != want {
		generated, _ := os.ReadFile(filepath.Join(outputDir, "main.go"))
		t.Fatalf("unexpected initializer output:\n got: %q\nwant: %q\ngenerated:\n%s", output, want, generated)
	}
}

func TestCompileFilesRunsEachPackageInit(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	files := map[string]string{
		"first/first_a.gpp": `package demo.first
import "fmt"
func init() { fmt.Println("first init A") }
func Value() string { return "first" }
`,
		"first/first_b.gpp": `package demo.first
import "fmt"
func init() { fmt.Println("first init B") }
`,
		"second/second.gpp": `package demo.second
import "fmt"
func init() { fmt.Println("second init") }
func Value() string { return "second" }
`,
		"main_init.gpp": `package main
import "fmt"
func init() { fmt.Println("main init") }
`,
		"main.gpp": `package main
import (
	"fmt"
	demo.first
	demo.second
)
func main() { fmt.Println("main run", first.Value(), second.Value()) }
`,
	}
	paths := make([]string, 0, len(files))
	for name, source := range files {
		path := filepath.Join(inputDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if err := CompileFilesWithOptions(paths, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatalf("compile package initialization program: %v", err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated package initialization program did not run: %v\n%s", err, output)
	}
	for _, line := range []string{
		"first init A",
		"first init B",
		"second init",
		"main init",
		"main run first second",
	} {
		if count := strings.Count(string(output), line); count != 1 {
			t.Fatalf("expected package initializer output %q once, got %d times:\n%s", line, count, output)
		}
	}
	mainRun := strings.Index(string(output), "main run first second")
	for _, line := range []string{"first init A", "first init B", "second init", "main init"} {
		if strings.Index(string(output), line) > mainRun {
			t.Fatalf("package initializer %q ran after main:\n%s", line, output)
		}
	}
}

func TestCompileFilesLogicalOrAssignmentSupportsBooleansAndNil(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `package main
import "fmt"

var booleanDefaults int
var pointerDefaults int

func defaultBoolean() bool {
    booleanDefaults++
    return true
}

func defaultPointer() *int {
    pointerDefaults++
    value := 7
    return &value
}

class Sample {
    Ready bool
    Value *int

	func init(ready bool, value *int) {
		this.Ready = ready
		this.Value = value
        this.Value ||= defaultPointer()
    }

    func Configure(enabled bool) {
        enabled ||= defaultBoolean()
        this.Ready ||= defaultBoolean()
        this.Value ||= defaultPointer()
    }
}

func main() {
    existingValue := 3
	existing := Sample(true, &existingValue)
    existing.Configure(true)
    if booleanDefaults != 0 || pointerDefaults != 0 {
        fmt.Println("default evaluated eagerly")
        return
    }

	missing := Sample(false, nil)
    missing.Configure(false)
    fmt.Println(booleanDefaults, pointerDefaults, *missing.Value)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatalf("compile logical OR assignment program: %v", err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("logical OR assignment program did not run: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "2 1 7" {
		t.Fatalf("logical OR assignment evaluated incorrectly: %s", output)
	}
}

func TestCompileFilesLogicalOrAssignmentOnImportedBaseField(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `package main
import (
    stdlog "log"
    "gpp/http"
)

class Server : http.Server {
    func init() {
        this.Logger ||= stdlog.Default()
    }
}

func main() {
    _ = Server()
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatalf("compile logical OR assignment on imported base field: %v", err)
	}
}

func TestGeneratedInitializerHelpersDoNotUsePackageInitName(t *testing.T) {
	file, err := ParseFile("initializer.gpp", `class Sample {
	func init() {}
}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if strings.Contains(generated, "func (this *Sample) init()") {
		t.Fatalf("class initializer was emitted as a Go init function:\n%s", generated)
	}
	if !strings.Contains(generated, "func (this *Sample) __gppInit()") || !strings.Contains(generated, "GppInitialize() error") {
		t.Fatalf("generated initializer bridge is missing:\n%s", generated)
	}
	if !strings.Contains(generated, "this.__gppInit()") {
		t.Fatalf("generated initializer bridge does not invoke the source initializer:\n%s", generated)
	}
}
