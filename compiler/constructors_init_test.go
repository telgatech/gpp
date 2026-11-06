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
			name: "parameters",
			src:  `class Sample { func init(value int) {} }`,
			want: "initializer `init` cannot have parameters",
		},
		{
			name: "static",
			src:  `class Sample { static func init() {} }`,
			want: "initializer `init` cannot be static",
		},
		{
			name: "invalid result",
			src:  `class Sample { func init() int { return 1 } }`,
			want: "must return no values or a trailing error",
		},
		{
			name: "duplicate",
			src:  `class Sample { func init() {} func init() error { return nil } }`,
			want: "may declare only one initializer",
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

func TestResolveProgramAcceptsClassErrorInitializerResult(t *testing.T) {
	file, err := ParseFile("initializer.gpp", `class InitError {
	func Error() string { return "init failed" }
}
class Sample {
	func init() *InitError { return nil }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProgram(&Program{Files: []*File{file}}); err != nil {
		t.Fatalf("expected an error-compatible class result to be accepted: %v", err)
	}
}

func TestConstructorInitializerLoweringPreservesErrorResult(t *testing.T) {
	base := &ClassDecl{
		Name:   "Base",
		Fields: []Field{{Name: "Name", TypeAST: parseTypeText("string")}},
		Methods: []Method{{
			Name:      "init",
			ResultAST: parseTypeText("error"),
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
	for _, expected := range []string{"func() (*Base, error)", "__gpp_instance := &Base{Name: \"value\"}", "GppInitialize()", "return nil, __gpp_initErr"} {
		if !strings.Contains(transformed, expected) {
			t.Fatalf("expected lowered constructor to contain %q, got:\n%s", expected, transformed)
		}
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

	func init() {
		fmt.Println("base", this.Name)
		this.Name += "-base"
	}
}

class Middle: Base {
	func init() error {
		fmt.Println("middle", this.Name)
		if this.Name == "bad-base" {
			return errors.New("middle rejected value")
		}
		return nil
	}
}

class Child: Middle {
	func init() {
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

func NewChild(name string) (*Child, error) {
	return Child(name)
}

func main() {
	good, err := Child("good")
	fmt.Println("good", good.Name, err == nil)

	returned, err := NewChild("returned")
	fmt.Println("returned", returned.Name, err == nil)

	bad, err := Child("bad")
	fmt.Println("bad", bad == nil, err)

	try {
		Child("bad")
	} catch error e {
		fmt.Println("caught", e)
	}

	_ = Combined()
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
		"",
	}, "\n")
	if string(output) != want {
		generated, _ := os.ReadFile(filepath.Join(outputDir, "main.go"))
		t.Fatalf("unexpected initializer output:\n got: %q\nwant: %q\ngenerated:\n%s", output, want, generated)
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
