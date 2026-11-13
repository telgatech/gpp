package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testPersonFile(t *testing.T) *File {
	t.Helper()

	file, err := ParseFile("test.gpp", `
class Person {
    Name string
    Age int
}
`)
	if err != nil {
		t.Fatal(err)
	}

	return file
}

func appendMixedSourceDecl(file *File, source string) {
	start := len(file.Source)
	if start > 0 && file.Source[start-1] != '\n' {
		file.Source += "\n"
		start++
	}
	file.Source += source
	end := len(file.Source)
	file.Source += "\n"
	tokens, _ := LexSource(file.Name, file.Source[start:end])
	tokens = rebaseTokens(tokens, file.Source, start)
	line, column := sourcePosition(file.Source, start)
	functions := parseTopLevelFunctions(file.Name, file.Package, source, start, file.Source, "", start)
	for _, function := range functions {
		attachFunctionOwner(file, function)
	}
	goASTDecls, goASTFileSet := parseGoDeclarations(file.Name, file.Package, source)
	file.Decls = append(file.Decls, &MixedDecl{
		GoASTDecls:   goASTDecls,
		GoASTFileSet: goASTFileSet,
		Functions:    functions,
		Tokens:       tokens,
		Owner:        file,
		SourceSpan:   Span{Start: start, End: end, Line: line, Column: column},
		SourceFile:   file.Name,
		SourceLine:   line,
	})
}

func TestEmitPositionalConstructor(t *testing.T) {
	file := testPersonFile(t)
	appendMixedSourceDecl(file, `func main() {
    p := Person("Bob", 42)
    _ = p
}`)

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `p := &Person{Name: "Bob", Age: 42}`) {
		t.Fatalf("generated code did not contain compact construction:\n%s", code)
	}
}

func TestEmitNamedConstructor(t *testing.T) {
	file := testPersonFile(t)
	appendMixedSourceDecl(file, `func main() {
    p := Person(
    Name: "Bob",
    Age: 42,
    )
    _ = p
}`)

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `p := &Person{Name: "Bob", Age: 42}`) {
		t.Fatalf("generated code did not contain named construction:\n%s", code)
	}
}

func TestEmitClassMethodUsesImplicitThisForFields(t *testing.T) {
	file, err := ParseFile("implicit-this.gpp", `class Person {
    Name string

    func Label() string {
        local := "local"
        return Name + ":" + local
    }
}`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if !strings.Contains(generated, `return this.Name + ":" + local`) {
		t.Fatalf("generated method did not qualify the field while preserving the local:\n%s", generated)
	}
}

func TestEmitMixedDeclKeepsOrdinaryGoDeclarationsOnASTPath(t *testing.T) {
	file := testPersonFile(t)
	appendMixedSourceDecl(file, `var ordinary = 7

func main() {
    person := Person("Bob", 42)
    _ = person
}`)

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if !strings.Contains(generated, "var ordinary = 7") {
		t.Fatalf("ordinary Go declaration was not emitted from its AST:\n%s", generated)
	}
	if !strings.Contains(generated, `person := &Person{Name: "Bob", Age: 42}`) {
		t.Fatalf("neighboring GPP function was not lowered:\n%s", generated)
	}
}

func TestEmitStaticMethods(t *testing.T) {
	file, err := ParseFile("static.gpp", `
import "strings"

class User {
    Name string

    static func Guest() User {
        return User(Name: "Guest")
    }

    static func Parse(name string) User {
        return User(Name: strings.TrimSpace(name))
    }

    static func Identity[T any](value T) T {
        return value
    }

    func Greeting() string {
        return "Hello " + this.Name
    }
}

func main() {
    guest := User.Guest()
    parsed := User.Parse(input)
    number := User.Identity[int](42)
    _ = guest.Greeting()
    _ = parsed.Greeting()
    _ = number
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	for _, expected := range []string{
		"func GppStatic_User_Guest() User",
		"func GppStatic_User_Parse(name string) User",
		"func GppStatic_User_Identity[T any](value T) T",
		"guest := GppStatic_User_Guest()",
		"parsed := GppStatic_User_Parse(input)",
		"number := GppStatic_User_Identity[int](42)",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("static method lowering missing %q:\n%s", expected, code)
		}
	}
	if strings.Contains(generated, "func GppStatic_User_Guest(this") {
		t.Fatalf("static method unexpectedly has a receiver:\n%s", code)
	}
}

func TestEmitInheritedAndShadowedStaticMethods(t *testing.T) {
	file, err := ParseFile("static_inheritance.gpp", `
class A {
    static func Version() string {
        return "A"
    }
}

class B: A {}

class C: A {
    static func Version() string {
        return "C"
    }
}

func main() {
    _ = A.Version()
    _ = B.Version()
    _ = C.Version()
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if strings.Count(generated, "GppStatic_A_Version()") < 2 {
		t.Fatalf("inherited static method did not resolve to A:\n%s", code)
	}
	if !strings.Contains(generated, "GppStatic_C_Version()") {
		t.Fatalf("shadowed static method did not resolve to C:\n%s", code)
	}
}

func TestEmitRejectsThisInStaticMethod(t *testing.T) {
	file, err := ParseFile("static_this.gpp", `
class User {
    Name string

    static func Guest() User {
        return User(Name: this.Name)
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "static method Guest has no this") {
		t.Fatalf("expected static this error, got %v", err)
	}
}

func TestEmitMarksStaticMethodsInMetadata(t *testing.T) {
	file, err := ParseFile("static_metadata.gpp", `
annotation Marker on method

class User {
    static func Guest() User @{Marker} {
        return User()
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(code), `{Name: "Guest",`) || !strings.Contains(string(code), `Static: true`) {
		t.Fatalf("static method metadata was not emitted:\n%s", code)
	}
}

func TestEmitConstructorHandlesNestedExpressions(t *testing.T) {
	file := testPersonFile(t)
	appendMixedSourceDecl(file, `func main() {
    p := Person(makeName("Bob, Jr."), add(20, 22))
    _ = p
}`)

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `p := &Person{Name: makeName("Bob, Jr."), Age: add(20, 22)}`) {
		t.Fatalf("generated code did not preserve nested expressions:\n%s", code)
	}
}

func TestEmitConstructorRejectsUnknownNamedField(t *testing.T) {
	file := testPersonFile(t)
	appendMixedSourceDecl(file, `func main() {
    p := Person(Name: "Bob", Height: 180)
    _ = p
}`)

	_, err := Emit(file)
	if err == nil || !strings.Contains(err.Error(), "unknown field Height") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
	position, ok := PositionOfDiagnostic(err)
	if !ok {
		t.Fatalf("constructor error did not retain a structured source position: %v", err)
	}
	fieldOffset := strings.Index(file.Source, "Height")
	wantLine, wantColumn := sourcePosition(file.Source, fieldOffset)
	if position.File != file.Name || position.Span.Line != wantLine || position.Span.Column != wantColumn {
		t.Fatalf("constructor error position = %s:%d:%d, want %s:%d:%d", position.File, position.Span.Line, position.Span.Column, file.Name, wantLine, wantColumn)
	}
}

func TestEmitConstructorRejectsWrongArity(t *testing.T) {
	file := testPersonFile(t)
	appendMixedSourceDecl(file, `func main() {
    p := Person("Bob")
    _ = p
}`)

	_, err := Emit(file)
	if err == nil || !strings.Contains(err.Error(), "expects 2 arguments, got 1") {
		t.Fatalf("expected arity error, got %v", err)
	}
}

func TestEmitPreservesGoImportSyntax(t *testing.T) {
	file, err := ParseFile("imports.gpp", `
import (
    "fmt"
    j "encoding/json"
)

class Person {
    Name string
}

func main() {
    p := Person("Bob")
    _, _ = fmt.Println(p.Name), j.Compact
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, `"fmt"`) ||
		!strings.Contains(generated, `j "encoding/json"`) {
		t.Fatalf("generated code did not preserve Go imports:\n%s", code)
	}
	if strings.Count(generated, `"fmt"`) != 1 {
		t.Fatalf("fmt was imported more than once:\n%s", code)
	}
}

func TestInterpolationUsesFmtAlias(t *testing.T) {
	file, err := ParseFile("alias.gpp", `
import f "fmt"

func main() {
    _ = "value {{1}}"
    _ = f.Println
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `f.Sprintf("value %v", 1)`) {
		t.Fatalf("interpolation did not use the fmt alias:\n%s", code)
	}
}

func TestInterpolationUsesFmtAliasInDottedPackage(t *testing.T) {
	file, err := ParseFile("dotted_alias.gpp", `
package telga.web

import f "fmt"

func Greeting() string {
    return "value {{1}}"
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "package web") ||
		!strings.Contains(generated, `f.Sprintf("value %v", 1)`) {
		t.Fatalf("dotted package interpolation did not preserve the fmt alias:\n%s", code)
	}
	if strings.Contains(generated, "import \"fmt\"") {
		t.Fatalf("dotted package interpolation synthesized a duplicate fmt import:\n%s", code)
	}
}

func TestCompileFilesSupportsFormattedInterpolationAndLiteralRawStrings(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := "" +
		"import \"fmt\"\n\n" +
		"func main() {\n" +
		"    name := \"Bob\"\n" +
		"    price := 12.5\n" +
		"    id := 7\n" +
		"    fmt.Println(\"Hello {{name}} {{price:%.2f}} {{id:%03d}}\")\n" +
		"    raw := `raw {{name}} path C:\\\\users\\{{name}}`\n" +
		"    fmt.Println(raw)\n" +
		"    fmt.Println(\"Use {{{{name}}}}\")\n" +
		"}\n"
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
		t.Fatalf("generated interpolation program did not run: %v\n%s", err, output)
	}
	expected := "Hello Bob 12.50 007\nraw {{name}} path C:\\\\users\\{{name}}\nUse {{name}}\n"
	if string(output) != expected {
		t.Fatalf("unexpected interpolation output:\n%s", output)
	}
}

func TestCompileFilesRunsGenericClass(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `class Box[T comparable] {
    Value T

    func Get() T {
        return this.Value
    }
}

func Read(box *Box[int]) int {
    return box.Get()
}

func main() {
    box := Box[int](Value: 42)
    println(Read(box))
}`
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
		t.Fatalf("generated generic class program did not run: %v\n%s", err, output)
	}
	if string(output) != "42\n" {
		t.Fatalf("unexpected generic class output: %q", output)
	}
}

func TestCompileFilesRunsGenericMethodDeclaredOnClass(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `class Client {
    func Echo[T any](value T) (T, error) {
        return value, nil
    }
}

class Child : Client {}

func main() {
    client := Client()
    explicit := client.Echo[string]("Ada")
    inferred := client.Echo(42)
    child := Child()
    inherited := child.Echo[float64](3.5)
    println(explicit, inferred, inherited)
}`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "go-cache"))
	output, err := command.CombinedOutput()
	if err != nil {
		generated, readErr := os.ReadFile(filepath.Join(outputDir, "main.go"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		t.Fatalf("generated generic method program did not run: %v\n%s\nGenerated source:\n%s", err, output, generated)
	}
	if string(output) != "Ada 42 3.5\n" {
		t.Fatalf("unexpected generic method output: %q", output)
	}
}

func TestCompileFilesResolvesGenericMethodOverloadByExplicitTypeArguments(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `class Client {
    func Get(url string, context string = "") string {
        return "raw"
    }

    func Get[T any](url string, context string = "") T {
        var decoded T
        return decoded
    }
}

func main() {
    client := Client()
    raw := client.Get("/users/1")
    decoded := client.Get[string]("/users/1")
    println(raw, decoded)
}`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "go-cache"))
	output, err := command.CombinedOutput()
	if err != nil {
		generated, readErr := os.ReadFile(filepath.Join(outputDir, "main.go"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		t.Fatalf("overloaded generic method program did not run: %v\n%s\nGenerated source:\n%s", err, output, generated)
	}
	if string(output) != "raw \n" {
		t.Fatalf("raw and generic overloads selected the wrong methods: %q", output)
	}
}

func TestCompileFilesRunsImportedInheritedGenericMethod(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	basePath := filepath.Join(inputDir, "api", "base.gpp")
	mainPath := filepath.Join(inputDir, "main.gpp")
	if err := os.MkdirAll(filepath.Dir(basePath), 0755); err != nil {
		t.Fatal(err)
	}
	baseSource := `package api

class Base {
    func Echo[T any](value T) (T, error) {
        return value, nil
    }
}`
	mainSource := `package main

import "fixture/api"

class Child : api.Base {}

func main() {
    child := Child()
    value := child.Echo[string]("inherited")
    println(value)
}`
	for path, source := range map[string]string{basePath: baseSource, mainPath: mainSource} {
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := CompileFilesWithOptions([]string{basePath, mainPath}, outputDir, CompileOptions{ModulePath: "fixture"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "go-cache"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated inherited generic method program did not run: %v\n%s", err, output)
	}
	if string(output) != "inherited\n" {
		t.Fatalf("unexpected inherited generic method output: %q", output)
	}
}

func TestEmitRejectsInvalidInterpolation(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		want   string
	}{
		{name: "empty", source: `func main() { _ = "{{}}" }`, want: "empty interpolation expression"},
		{name: "format", source: `func main() { _ = "{{1:.2f}}" }`, want: "expected Go fmt format"},
		{name: "unterminated", source: "func main() { _ = \"{{value\" }", want: "unterminated interpolation"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := ParseFile(test.name+".gpp", test.source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Emit(file); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected interpolation diagnostic containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestEmitUsesFinalDottedPackageSegment(t *testing.T) {
	file, err := ParseFile("web.gpp", `
package telga.web

class Page {}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(code), "package web\n") {
		t.Fatalf("generated package name was not the final segment:\n%s", code)
	}
}

func TestEmitInheritedPositionalConstructor(t *testing.T) {
	file, err := ParseFile("inheritance.gpp", `
class Person {
    Name string
}

class Employee: Person {
    Salary int
}

func main() {
    employee := Employee("Bob", 100)
    _ = employee
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `&Employee{Person: Person{Name: "Bob"}, Salary: 100}`) {
		t.Fatalf("generated code did not initialize inherited fields:\n%s", code)
	}
}

func TestEmitInheritedNamedConstructor(t *testing.T) {
	file, err := ParseFile("inheritance_named.gpp", `
class Person {
    Name string
}

class Employee: Person {
    Salary int
}

func main() {
    employee := Employee(
        Name: "Bob",
        Salary: 100,
    )
    _ = employee
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `&Employee{Person: Person{Name: "Bob"}, Salary: 100}`) {
		t.Fatalf("generated named construction did not initialize inherited fields:\n%s", code)
	}
}

func TestEmitInheritedMethodsOnDerivedReceiver(t *testing.T) {
	file, err := ParseFile("inherited_methods.gpp", `
class Model {
    func RuntimeName() string {
        return this.class.name
    }
}

class Employee: Model {}

func main() {
    employee := Employee()
    _ = employee.RuntimeName()
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if !strings.Contains(generated, "func (this *Employee) RuntimeName() string") ||
		!strings.Contains(generated, "return this.GppRuntimeClass().Name") {
		t.Fatalf("inherited method was not emitted for the derived receiver:\n%s", code)
	}
}

func TestEmitDoesNotPromotePrivateInheritedMethods(t *testing.T) {
	file, err := ParseFile("private_inherited_methods.gpp", `
class Parent {
    func helper() {}
    func Public() {}
}

class Child : Parent {}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if strings.Contains(generated, "func (this *Child) helper()") {
		t.Fatalf("private parent method was promoted to the child:\n%s", generated)
	}
	if !strings.Contains(generated, "func (this *Child) Public()") {
		t.Fatalf("exported parent method was not promoted to the child:\n%s", generated)
	}
}

func TestEmitProtectedMembersUseAccessibleGeneratedNames(t *testing.T) {
	file, err := ParseFile("protected_members.gpp", `class Parent {
    _state string
    func _reset() { this._state = "" }
    static func _factory() Parent { return Parent{} }
}
class Child : Parent {
    func Reset() { this._reset(); this._state = "ready" }
    static func New() Parent { return Parent._factory() }
}`)
	if err != nil {
		t.Fatal(err)
	}
	appendMixedSourceDecl(file, `func makeChild() Child { return Child(_state: "ready") }`)
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	for _, expected := range []string{
		"GppProtected_state string",
		"GppProtected_reset()",
		"this.GppProtected_reset()",
		"GppStatic_Parent_GppProtected_factory()",
		"GppProtected_state: \"ready\"",
	} {
		if !strings.Contains(generated, expected) {
			t.Errorf("generated Go did not include %q:\n%s", expected, generated)
		}
	}
}

func TestConstructorFieldsDoNotPromotePrivateInheritedFields(t *testing.T) {
	file, err := ParseFile("private_inherited_fields.gpp", `
class Parent {
    secret string
    _protected string
    Public string
}

class Child : Parent {}
`)
	if err != nil {
		t.Fatal(err)
	}
	classes := map[string]*ClassDecl{}
	for _, declaration := range file.Decls {
		if class, ok := declaration.(*ClassDecl); ok {
			classes[class.Name] = class
		}
	}
	fields, err := constructorFieldsVisibleToClass(classes["Child"], classes)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Name == "secret" {
			t.Fatalf("private parent field was promoted to the child constructor: %#v", fields)
		}
	}
	if len(fields) != 2 || fields[0].Name != "_protected" || fields[1].Name != "Public" {
		t.Fatalf("expected protected and public inherited fields, got %#v", fields)
	}
	parentFields, err := constructorFields(classes["Parent"], classes, nil, map[*ClassDecl]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if len(parentFields) != 3 || parentFields[0].Name != "secret" {
		t.Fatalf("private state should remain in the declaring class metadata, got %#v", parentFields)
	}
}

func TestEmitRequiresQualifiedAmbiguousInheritedField(t *testing.T) {
	file, err := ParseFile("ambiguous.gpp", `
class A {
    Name string
}

class B {
    Name string
}

class C: A, B {}

func main() {
    value := C(Name: "ambiguous")
    _ = value
}
`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "ambiguous named argument Name") {
		t.Fatalf("expected ambiguous field error, got %v", err)
	}
}

func TestEmitQualifiedAmbiguousInheritedFields(t *testing.T) {
	file, err := ParseFile("qualified_ambiguity.gpp", `
class A {
    Name string
}

class B {
    Name string
}

class C: A, B {}

func main() {
    value := C(A.Name: "a", B.Name: "b")
    _ = value
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `C{A: A{Name: "a"}, B: B{Name: "b"}}`) {
		t.Fatalf("qualified fields were not emitted into their parents:\n%s", code)
	}
}

func TestEmitRejectsUnqualifiedAmbiguousMemberAccess(t *testing.T) {
	file, err := ParseFile("ambiguous_access.gpp", `
class A {
    Name string
}

class B {
    Name string
}

class C: A, B {
    func GetName() string {
        return this.Name
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "ambiguous member Name") ||
		!strings.Contains(err.Error(), "this.A.Name") {
		t.Fatalf("expected qualified member access error, got %v", err)
	}
}

func TestEmitAllowsQualifiedAmbiguousMemberAccess(t *testing.T) {
	file, err := ParseFile("qualified_access.gpp", `
class A {
    Name string
}

class B {
    Name string
}

class C: A, B {
    func GetName() string {
        return this.A.Name
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Emit(file); err != nil {
		t.Fatal(err)
	}
}

func TestEmitLowersBaseTypedAssignmentForVirtualDispatch(t *testing.T) {
	file, err := ParseFile("polymorphism.gpp", `
class Person {
    func Speak() string {
        return "person"
    }
}

class Employee: Person {
    func Speak() string {
        return "employee"
    }
}

func main() {
    var person Person = Employee()
    _ = person.Speak()
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "type __gpp_Person interface") {
		t.Fatalf("base dispatch interface was not generated:\n%s", code)
	}
	if !strings.Contains(generated, "var person __gpp_Person = &Employee{}") {
		t.Fatalf("base-typed assignment was not lowered:\n%s", code)
	}
}

func TestEmitLowersPolymorphicFunctionParametersAndReturns(t *testing.T) {
	file, err := ParseFile("polymorphic_functions.gpp", `
class Person {
    func Speak() string {
        return "person"
    }
}

class Employee: Person {
    func Speak() string {
        return "employee"
    }
}

func SpeakFor(person Person) string {
    return person.Speak()
}

func MakePerson() Person {
    return Employee()
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func SpeakFor(person __gpp_Person) string",
		"func MakePerson() __gpp_Person",
		"return &Employee{}",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("generated code did not contain %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersDerivedArgumentsForBaseParameters(t *testing.T) {
	file, err := ParseFile("polymorphic_arguments.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

func SpeakFor(person Person) string {
    return person.Speak()
}

func main() {
    _ = SpeakFor(Employee())
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func SpeakFor(person __gpp_Person) string",
		"_ = SpeakFor(&Employee{})",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("derived argument was not coerced for a base parameter, missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersDerivedArgumentsForBaseMethodParameters(t *testing.T) {
	file, err := ParseFile("polymorphic_method_arguments.gpp", `
class Person {
    func Speak() string { return "person" }

    func Use(other Person) string {
        return other.Speak()
    }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

func main() {
    person := Person()
    _ = person.Use(Employee())
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func (this *Person) Use(other __gpp_Person) string",
		"_ = person.Use(&Employee{})",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("derived method argument was not coerced, missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersBaseConstructorToPointerForDispatch(t *testing.T) {
	file, err := ParseFile("polymorphic_base_constructor.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {}

func MakePerson() Person {
    return Person()
}

func main() {
    var person Person = Person()
    _ = person
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func MakePerson() __gpp_Person",
		"return &Person{}",
		"var person __gpp_Person = &Person{}",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("base constructor was not lowered to a pointer for dispatch, missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersPolymorphicFields(t *testing.T) {
	file, err := ParseFile("polymorphic_field.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

class Holder {
    Value Person
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	fieldWasLowered := false
	for _, line := range strings.Split(string(code), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Value ") && strings.Contains(trimmed, "__gpp_Person") {
			fieldWasLowered = true
			break
		}
	}
	if !fieldWasLowered {
		t.Fatalf("polymorphic field was not lowered to the dispatch interface:\n%s", code)
	}
}

func TestEmitLowersPolymorphicConstructorFieldValue(t *testing.T) {
	file, err := ParseFile("polymorphic_constructor_field.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

class Holder {
    Value Person
}

func main() {
    holder := Holder(Employee())
    _ = holder
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), "Holder{Value: &Employee{}}") {
		t.Fatalf("polymorphic constructor field was not lowered to a pointer:\n%s", code)
	}
}

func TestEmitResolvesMethodOverloadsByArity(t *testing.T) {
	file, err := ParseFile("method_overloads.gpp", `
class Printer {
    func Print() string {
        return "empty"
    }

    func Print(value string) string {
        return value
    }

    func Use() string {
        return this.Print() + this.Print("value")
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func (this *Printer) Print__gpp_0() string",
		"func (this *Printer) Print__gpp_1(value string) string",
		"this.Print__gpp_0()",
		"this.Print__gpp_1(\"value\")",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("method overload output missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitScopesMethodOverloadsToDeclaringClass(t *testing.T) {
	file, err := ParseFile("scoped_method_overloads.gpp", `
class A {
    func Print(value string) string {
        return value
    }

    func Use() string {
        return this.Print("a")
    }
}

class B {
    func Print() string {
        return "empty"
    }

    func Print(value string) string {
        return value
    }

    func Use() string {
        return this.Print("b")
    }
}

func main() {
    a := A{}
    b := B{}
    _ = a.Print("a")
    _ = b.Print("b")
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "func (this *A) Print(value string)") ||
		!strings.Contains(generated, "this.Print(\"a\")") {
		t.Fatalf("unrelated class method was incorrectly renamed:\n%s", code)
	}
	if !strings.Contains(generated, "func (this *B) Print__gpp_1(value string)") ||
		!strings.Contains(generated, "this.Print__gpp_1(\"b\")") ||
		!strings.Contains(generated, "a.Print(\"a\")") ||
		!strings.Contains(generated, "b.Print__gpp_1(\"b\")") {
		t.Fatalf("overloaded class method was not renamed:\n%s", code)
	}
}

func TestEmitResolvesFunctionOverloadsByArity(t *testing.T) {
	file, err := ParseFile("function_overloads.gpp", `
func Add(value int) int {
    return value
}

func Add(left int, right int) int {
    return left + right
}

func Use() int {
    return Add(1) + Add(1, 2)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func Add__gpp_1(value int) int",
		"func Add__gpp_2(left int, right int) int",
		"return Add__gpp_1(1) + Add__gpp_2(1, 2)",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("function overload output missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitSupportsNamedAndDefaultFunctionArguments(t *testing.T) {
	file, err := ParseFile("function_defaults.gpp", `
func Greeting(name string, punctuation string = "!") string {
    return name + punctuation
}

func main() {
    first := Greeting(name: "Bob")
    second := Greeting(punctuation: "?", name: "Ada")
    _, _ = first, second
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func Greeting(name string, punctuation string) string",
		`first := Greeting("Bob", "!")`,
		`second := Greeting("Ada", "?")`,
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("named/default function arguments missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitSupportsNamedAndDefaultMethodArguments(t *testing.T) {
	file, err := ParseFile("method_defaults.gpp", `
class Greeter {
    func Greeting(name string, punctuation string = "!") string {
        return name + punctuation
    }

    func Use() string {
        return this.Greeting(punctuation: "?", name: "Ada")
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatalf("%v\n%s", err, code)
	}

	generated := string(code)
	if !strings.Contains(generated, "func (this *Greeter) Greeting(name string, punctuation string) string") ||
		!strings.Contains(generated, `this.Greeting("Ada", "?")`) {
		t.Fatalf("named/default method arguments were not lowered:\n%s", code)
	}
}

func TestEmitSupportsSafeMemberAccess(t *testing.T) {
	file, err := ParseFile("safe_access.gpp", `
class Person {
    Name string

    func Speak() string {
        return "hello " + this.Name
    }
}

func main() {
    var person *Person
    name := person?.Name
    greeting := person?.Speak()
    _, _ = name, greeting
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func __gpp_safe[R any]",
		"name := __gpp_safe(person == nil, func() string {",
		"return person.Name",
		"greeting := __gpp_safe(person == nil, func() string {",
		"return person.Speak()",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("safe access output missing %q:\n%s", expected, code)
		}
	}
}

func TestSafeAccessLoweringConsumesTopLevelFunctionBodyAST(t *testing.T) {
	file, err := ParseFile("safe_access_ast.gpp", `
class Person {
    Name string
}

func main() {
    var person *Person
    name := person?.Name
    _ = name
}
`)
	if err != nil {
		t.Fatal(err)
	}
	model, err := ResolveProgram(&Program{Files: []*File{file}})
	if err != nil {
		t.Fatal(err)
	}
	context := localConstructorContext(model.Packages[file.Package].Classes)
	function := file.Decls[1].(*FunctionDecl)
	lowered, err := transformSafeAccessInFunctionSource(functionSource(function), function, context)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lowered, `__gpp_safe(person == nil, func() string { return person.Name })`) {
		t.Fatalf("top-level safe access was not lowered from its body AST:\n%s", lowered)
	}
	wholeFunction, err := transformSafeAccess(functionSource(function), context)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wholeFunction, `__gpp_safe(person == nil, func() string { return person.Name })`) {
		t.Fatalf("whole-function safe access was not lowered from its body AST:\n%s", wholeFunction)
	}
}

func TestSafeValueTypesUsesBodyASTForGPPDeclarations(t *testing.T) {
	types, ok := safeValueTypesAST(`
let person *Person
alias := person
`)
	if !ok {
		t.Fatal("expected GPP body AST type collection to succeed")
	}
	if types["person"] != "*Person" || types["alias"] != "*Person" {
		t.Fatalf("unexpected AST-inferred safe-access types: %#v", types)
	}
}

func TestEmitResolvesSameArityFunctionOverloadsByType(t *testing.T) {
	file, err := ParseFile("typed_function_overloads.gpp", `
func Format(value int) string {
    return "int"
}

func Format(value string) string {
    return "string"
}

func Use() string {
    return Format(1) + Format("value")
}

func UseVariable(value int) string {
    return Format(value)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func Format__gpp_1_int(value int) string",
		"func Format__gpp_1_string(value string) string",
		"return Format__gpp_1_int(1) + Format__gpp_1_string(\"value\")",
		"return Format__gpp_1_int(value)",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("typed function overload output missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitResolvesSameArityMethodOverloadsByType(t *testing.T) {
	file, err := ParseFile("typed_method_overloads.gpp", `
class Formatter {
    func Format(value int) string {
        return "int"
    }

    func Format(value string) string {
        return "string"
    }

    func Use() string {
        return this.Format(1) + this.Format("value")
    }

    func UseValue(value int) string {
        return this.Format(value)
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func (this *Formatter) Format__gpp_1_int(value int) string",
		"func (this *Formatter) Format__gpp_1_string(value string) string",
		"this.Format__gpp_1_int(1) + this.Format__gpp_1_string(\"value\")",
		"return this.Format__gpp_1_int(value)",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("typed method overload output missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitRejectsUnknownTypedOverloadCall(t *testing.T) {
	file, err := ParseFile("unknown_typed_overload.gpp", `
func Format(value int) string { return "int" }
func Format(value string) string { return "string" }

func Use(value bool) string {
    return Format(value)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "cannot resolve overloaded function Format") {
		t.Fatalf("expected unknown typed overload diagnostic, got %v", err)
	}
}

func TestEmitLowersKnownPolymorphicFunctionResults(t *testing.T) {
	file, err := ParseFile("polymorphic_results.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

func MakeEmployee() Employee {
    return Employee()
}

func SpeakFor(person Person) string {
    return person.Speak()
}

func main() {
    _ = SpeakFor(MakeEmployee())
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func MakeEmployee() *Employee",
		"return &Employee{}",
		"func SpeakFor(person __gpp_Person) string",
		"_ = SpeakFor(MakeEmployee())",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("known polymorphic function result was not lowered, missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersKnownPolymorphicMethodResults(t *testing.T) {
	file, err := ParseFile("polymorphic_method_results.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

class Factory {
    func Make() Employee {
        return Employee()
    }
}

func SpeakFor(person Person) string {
    return person.Speak()
}

func main() {
    factory := Factory()
    _ = SpeakFor(factory.Make())
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func (this *Factory) Make() *Employee",
		"return &Employee{}",
		"_ = SpeakFor(factory.Make())",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("known polymorphic method result was not lowered, missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitRejectsLegacyRecordConstructor(t *testing.T) {
	file, err := ParseFile("legacy_record.gpp", `
func main() {
    _ = record(name: "Bob")
}
`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Emit(file); err == nil {
		t.Fatal("legacy record(...) constructor should be rejected")
	}
}

func TestEmitSupportsAnonymousStructs(t *testing.T) {
	file, err := ParseFile("structs.gpp", `
func GetUser() struct {
    return struct(
        name: "Bob",
        age: 42,
        profile: struct(active: true),
    )
}

func main() {
    user := GetUser()
    _ = user.name
    _ = user.profile.active
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"type __gpp_record_",
		"func GetUser() __gpp_record_",
		"name: \"Bob\"",
		"age: 42",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("anonymous struct output missing %q:\n%s", expected, code)
		}
	}
	if strings.Contains(generated, "struct(name") {
		t.Fatalf("anonymous struct call was not lowered:\n%s", code)
	}
}

func TestEmitInfersAnonymousStructFieldsFromNamedStructSelector(t *testing.T) {
	file, err := ParseFile("named_struct_selector.gpp", `
struct User {
    Name string
    Age int
}

func makeSummary(user User) struct {
    return struct(name: user.Name, age: user.Age)
}

func printSummary(summary struct) string {
    return summary.name
}

func main() {
    summary := makeSummary(User(Name: "Ada", Age: 36))
    _ = summary.name
    _ = printSummary(makeSummary(User(Name: "Ada", Age: 36)))
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(file); err != nil {
		t.Fatalf("expected named struct fields to infer anonymous result fields: %v", err)
	}
}

func TestEmitLowersAnonymousStructGenericTypeArguments(t *testing.T) {
	file, err := ParseFile("struct_generic_type_argument.gpp", `
func Identity[T any](value T) T {
    return value
}

func main() {
    _ = Identity[struct(Name: string, Age: int)](struct(Name: "Ada", Age: 36))
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatalf("expected anonymous struct generic type argument to lower: %v", err)
	}
	generated := string(code)
	if !strings.Contains(generated, "Identity[__gpp_record_") {
		t.Fatalf("anonymous struct generic argument was not lowered to a generated type:\n%s", generated)
	}
}

func TestEmitSupportsShortStructDeclarations(t *testing.T) {
	file, err := ParseFile("short_structs.gpp", `
struct User {
	Name string
}
struct Box[T any] {
	Value T
}
type Profile struct { Name string }

func main() {
	user := User(Name: "Ada")
	_ = user.Name
	profile := Profile(Name: "Grace")
	_ = profile.Name
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	for _, expected := range []string{"type User struct", "type Box[T any] struct"} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("short struct declaration output missing %q:\n%s", expected, generated)
		}
	}
	if !strings.Contains(generated, "User{Name: \"Ada\"}") || !strings.Contains(generated, "Profile{Name: \"Grace\"}") || strings.Contains(generated, "User(Name:") {
		t.Fatalf("named struct constructor was not lowered to a keyed Go literal:\n%s", generated)
	}
}

func TestEmitInfersRecordParametersAfterFunctionResultInference(t *testing.T) {
	file, err := ParseFile("record_parameter_inference.gpp", `
func getUser() struct {
    return struct(Name: "Bob", Age: 42)
}

func userName(user struct) string {
    return user.Name
}

func main() {
    user := getUser()
    _ = userName(user)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "func userName(user __gpp_record_") {
		t.Fatalf("record parameter was not inferred after result inference:\n%s", generated)
	}
}

func TestEmitTreatsRecordFieldCaseAsSignificant(t *testing.T) {
	file, err := ParseFile("record_visibility.gpp", `
func main() {
    exported := struct(Name: "Bob")
    internal := struct(name: "Bob")
    _ = exported.Name
    _ = internal.name
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if strings.Count(generated, "type __gpp_record_") != 2 {
		t.Fatalf("case-sensitive record fields should produce distinct shapes:\n%s", code)
	}
	if !strings.Contains(generated, "\tName string") || !strings.Contains(generated, "\tname string") {
		t.Fatalf("record field capitalization was not preserved:\n%s", code)
	}
}

func TestEmitRejectsInconsistentRecordReturns(t *testing.T) {
	file, err := ParseFile("bad_records.gpp", `
func Broken(ok bool) struct {
    if ok {
        return struct(message: "yes")
    }
    return struct(message: 1)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "inconsistent record return types in Broken") {
		t.Fatalf("expected inconsistent record return diagnostic, got %v", err)
	}
}

func TestEmitSupportsAnonymousStructSlices(t *testing.T) {
	file, err := ParseFile("record_slice.gpp", `
func Users() any {
    return []struct { name string; age int }{
        {name: "A", age: 1},
        {name: "B", age: 2},
    }
}

func main() {
    _ = Users()
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "[]struct {") {
		t.Fatalf("anonymous struct slice was not preserved:\n%s", code)
	}
}

func TestEmitSupportsAnonymousStructMaps(t *testing.T) {
	file, err := ParseFile("record_map.gpp", `
func Users() any {
    return map[string]struct { name string }{
        "first": struct { name string }{name: "A"},
        "second": struct { name string }{name: "B"},
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), "map[string]struct {") {
		t.Fatalf("anonymous struct map was not preserved:\n%s", code)
	}
}

func TestEmitSupportsLocalAnonymousStructCollections(t *testing.T) {
	file, err := ParseFile("record_collections.gpp", `
func main() {
    users := []struct { name string }{
        {name: "A"},
        {name: "B"},
    }
    byName := map[string]struct { name string }{
        "first": struct { name string }{name: "A"},
    }
    _ = users[0].name
    _ = byName["first"].name
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if strings.Contains(generated, "[]record") || strings.Contains(generated, "map[string]record") || !strings.Contains(generated, "[]struct {") {
		t.Fatalf("local anonymous struct collections were not emitted natively:\n%s", code)
	}
}

func TestEmitInfersRecordFunctionParameters(t *testing.T) {
	file, err := ParseFile("record_parameter.gpp", `
func PrintUser(user struct) string {
    return user.name
}

func main() {
    _ = PrintUser(struct(name: "Bob"))
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "func PrintUser(user __gpp_record_") {
		t.Fatalf("record function parameter was not inferred:\n%s", code)
	}
}

func TestEmitGeneratesClassIntrospection(t *testing.T) {
	file, err := ParseFile("introspection.gpp", `
class Model {
    Id *string
}

class Employee: Model {
    Name string
    Age int

    func Label() string {
        return this.class.name
    }
}

func main() {
    employee := Employee("id", "Bob", 42)
    c := Employee.class
    for f := range employee.class.fields {
        _ = f.name
        _ = f.owner.name
        _ = f.type.name
        _ = f.get(employee)
        f.set(employee, "Alice")
        _ = f.addr(employee)
    }
    _ = c
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"type GppClass struct",
		"var GppEmployeeClass = &GppClass{Name: \"Employee\"",
		"func (this Employee) GppRuntimeClass() *GppClass",
		"return this.GppRuntimeClass().Name",
		"for _, f := range employee.GppRuntimeClass().Fields",
		"_ = f.Name",
		"_ = f.Owner.Name",
		"_ = f.Type.Name",
		"_ = f.Get(employee)",
		"f.Set(employee, \"Alice\")",
		"_ = f.Addr(employee)",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("introspection output missing %q:\n%s", expected, code)
		}
	}
	if strings.Index(generated, `Name: "Id"`) > strings.Index(generated, `Name: "Name"`) {
		t.Fatalf("inherited fields are not emitted before own fields:\n%s", code)
	}
}

func TestEmitRejectsNativeTypeIntrospection(t *testing.T) {
	file, err := ParseFile("native_introspection.gpp", `
import "net/http"

func main() {
    _ = http.Request.class
}

`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "http.Request.class") {
		t.Fatalf("expected native type introspection error, got %v", err)
	}
}

func TestEmitLowersExtensionMethods(t *testing.T) {
	file, err := ParseFile("extensions.gpp", `
extend string {
    func Empty() bool {
        return len(this) == 0
    }

    func Identity[T](value T) T {
        return value
    }

    func Surround(left string, right string = "!") string {
        return left + this + right
    }
}

func main() {
    value := ""
    _ = value.Empty()
    _ = "go".Surround(right: "?", left: "[")
    _ = "go".Identity[int](42)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	for _, expected := range []string{
		"func GppExt_string_Empty_",
		"func GppExt_string_Identity_",
		"GppExt_string_Empty_5037a682(value)",
		"GppExt_string_Identity_40a61015[int](\"go\", 42)",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("extension method output missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersGlobalExtensionCallsInClassTryBlocks(t *testing.T) {
	file, err := ParseFile("global_extension_try.gpp", `
class Box {
    Value int
}

var globalBox *Box

extend *Box {
    func ValuePlus(delta int) (int, error) {
        return this.Value + delta, nil
    }
}

class App {
    func Read() int {
        var result int
        try {
            result = globalBox.ValuePlus(1)
        } catch err {
            panic(err)
        }
        return result
    }
}

func main() {}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if strings.Contains(generated, "globalBox.ValuePlus") {
		t.Fatalf("global extension call was not lowered:\n%s", generated)
	}
	if !strings.Contains(generated, "GppExt_ptr_Box_ValuePlus_") || !strings.Contains(generated, "__gppUnwrap(GppExt_ptr_Box_ValuePlus_") {
		t.Fatalf("global extension call was not promoted inside try:\n%s", generated)
	}
}

func TestEmitPromotesGenericVariadicExtensionErrorWithoutTry(t *testing.T) {
	file, err := ParseFile("generic_extension_error.gpp", `
class DB {}

extend *DB {
    func Select[T any](query string, args ...any) ([]T, error) {
        return nil, nil
    }
}

func main() {
    db := DB()
    rows := db.Select[int]("SELECT 1")
    _ = rows
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if !strings.Contains(generated, "__gppUnwrap(GppExt_ptr_DB_Select_") {
		t.Fatalf("generic extension trailing error was not promoted without try:\n%s", generated)
	}
}

func TestEmitSliceHelpersForPointerElements(t *testing.T) {
	file, err := ParseFile("pointer_slice_helpers.gpp", `
class Model {
    static func Page() ([]*self, error) {
        return nil, nil
    }
}

class Employee : Model {}

func main() {
	var page []*Employee = Employee.Page()
    if page.NotEmpty() {
        last := page.Last()
        _ = last
    }
    page.Each(func(employee *Employee) error { return nil })
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	for _, name := range []string{"GppPreludeExt___T_NotEmpty_", "GppPreludeExt___T_Last_"} {
		if !strings.Contains(generated, name) {
			t.Fatalf("pointer slice helper %s was not lowered:\n%s", name, generated)
		}
	}
	if strings.Contains(generated, "page.Each(") || strings.Count(generated, "GppPreludeExt___T_Each_") < 2 {
		t.Fatalf("Each callback for a pointer slice from an inherited static Self result was not lowered:\n%s", generated)
	}
}

func TestInferSliceTypeFromImportedSelfMethod(t *testing.T) {
	ormFile, err := ParseFile("orm_model.gpp", `package orm
class Model {
    static func Page() ([]*self, error) { return nil, nil }
}`)
	if err != nil {
		t.Fatal(err)
	}
	appFile, err := ParseFile("app.gpp", `package main
class Employee : orm.Model {}
func main() {
    page := Employee.Page()
}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := bindStaticSelfTypes(&Program{Files: []*File{ormFile, appFile}}); err != nil {
		t.Fatal(err)
	}
	model := ormFile.Decls[0].(*ClassDecl)
	employee := appFile.Decls[0].(*ClassDecl)
	context := localConstructorContext(map[string]*ClassDecl{"Employee": employee})
	context.Targets["orm.Model"] = constructorTarget{Class: model, Classes: map[string]*ClassDecl{"Model": model}, Qualifier: "orm"}
	context.StaticMethodSignatures["orm.Model"] = staticMethodSignaturesForClass(model, map[string]*ClassDecl{"Model": model}, map[*ClassDecl]bool{})
	function := appFile.Decls[1].(*FunctionDecl)
	got := lambdaValueTypesAST([]*BlockStmt{function.Method.BodyAST}, context)["page"]
	if got != "[]*Employee" {
		call := function.Method.BodyAST.Statements[0].(*DeclarationStmt).Values[0].(*CallExpr)
		signature, receiver, found := staticMethodSignatureForCall(call.Callee.(*SelectorExpr).Receiver, "Page", context)
		t.Fatalf("inferred page type = %q, want []*Employee (signature=%#v receiver=%q found=%v result=%q)", got, signature, receiver, found, staticCallResultTypeNode(call, context, nil))
	}
}

func TestEmitAutomaticallyDefersAtExitFromMain(t *testing.T) {
	file, err := ParseFile("at_exit.gpp", `
func main() {
	work()
}

func atExit() {
	cleanup()
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	deferIndex := strings.Index(generated, "defer atExit()")
	workIndex := strings.Index(generated, "work()")
	if deferIndex < 0 || workIndex < 0 || deferIndex > workIndex {
		t.Fatalf("main must defer atExit before executing its body:\n%s", generated)
	}
}

func TestEmitRejectsInvalidAtExitSignature(t *testing.T) {
	file, err := ParseFile("invalid_at_exit.gpp", `
func main() {}
func atExit(code int) {}
`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "atExit must have no parameters and no return values") {
		t.Fatalf("expected invalid atExit signature error, got %v", err)
	}
}

func TestEmitLowersVariadicExtensionCalls(t *testing.T) {
	file, err := ParseFile("variadic_extensions.gpp", `
extend string {
    func Join(prefix string, values ...any) string {
        return prefix + this
    }
}

func main() {
    value := "go"
    _ = value.Join(":", 1, "two")
    values := []any{1, "two"}
    _ = value.Join(":", values...)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if !strings.Contains(generated, "func GppExt_string_Join_") ||
		!strings.Contains(generated, `value, ":", 1, "two")`) {
		t.Fatalf("variadic extension call was not lowered:\n%s", code)
	}
	if !strings.Contains(generated, `value, ":", values...)`) {
		t.Fatalf("variadic slice extension call was not lowered:\n%s", code)
	}
}

func TestEmitLowersIntrospectionOnExtensionParameters(t *testing.T) {
	file, err := ParseFile("extension_introspection.gpp", `
import "database/sql"

annotation Table(name string) on class

class Model {}

class Employee: Model @{Table("employees")} {
    Name string
}

extend *sql.DB {
    func ModelName(entity Model) string {
        return entity.class.name
    }
}

func main() {
    var db *sql.DB
    employee := Employee("Ada")
    _ = db.ModelName(employee)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if !strings.Contains(generated, "entity.GppRuntimeClass().Name") {
		t.Fatalf("extension parameter introspection was not lowered:\n%s", code)
	}
}

func TestEmitRejectsExtensionFields(t *testing.T) {
	_, err := ParseFile("extension_field.gpp", `
extend string {
    Value int
}
`)
	if err == nil || !strings.Contains(err.Error(), "methods only") {
		t.Fatalf("expected extension field error, got %v", err)
	}
}

func TestEmitLowersMultiTargetExtensionMethods(t *testing.T) {
	file, err := ParseFile("multi_extensions.gpp", `
extend string, []byte {
    func Empty() bool {
        return len(this) == 0
    }

    func Identity[T](value T) T {
        return value
    }
}

func main() {
    text := ""
    bytes := []byte{}
    _ = text.Empty()
    _ = bytes.Empty()
    _ = text.Identity[int](42)
    _ = bytes.Identity[string]("go")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if strings.Count(generated, "func GppExt_") != 4 {
		t.Fatalf("expected one generated function per target/method combination:\n%s", generated)
	}
	for _, expected := range []string{
		"func GppExt_string_Empty_",
		"func GppExt___byte_Empty_",
		"GppExt_string_Identity_40a61015[int](text, 42)",
		"GppExt___byte_Identity_143efffa[string](bytes, \"go\")",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("multi-target extension output missing %q:\n%s", expected, generated)
		}
	}
}

func TestEmitLowersMultiTargetExtensionCallsForEachReceiver(t *testing.T) {
	file, err := ParseFile("multi_target_extension_calls.gpp", `
class DB {}
class Tx {}

extend *DB {
	func Select() error { return nil }
}

extend *Tx {
	func Select() error { return nil }
}

extend *DB, *Tx {
	func Read() error { return this.Select() }
}

func main() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	dbRead := strings.Index(generated, "func GppExt_ptr_DB_Read_")
	txRead := strings.Index(generated, "func GppExt_ptr_Tx_Read_")
	if dbRead < 0 || txRead < 0 || dbRead >= txRead {
		t.Fatalf("expected DB and Tx extension methods in output:\n%s", generated)
	}
	dbBody := generated[dbRead:txRead]
	txBody := generated[txRead:]
	if !strings.Contains(dbBody, "GppExt_ptr_DB_Select_") {
		t.Fatalf("DB extension body did not call the DB Select extension:\n%s", dbBody)
	}
	if !strings.Contains(txBody, "GppExt_ptr_Tx_Select_") {
		t.Fatalf("Tx extension body did not call the Tx Select extension:\n%s", txBody)
	}
}

func TestEmitRejectsDuplicateMultiTargetExtension(t *testing.T) {
	file, err := ParseFile("duplicate_extension_target.gpp", `
extend string, string {
    func Empty() bool { return true }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "duplicate extension target string") {
		t.Fatalf("expected duplicate extension target error, got %v", err)
	}
}

func TestEmitAnnotationsAndIntrospection(t *testing.T) {
	file, err := ParseFile("annotations.gpp", `
annotation (
    Table(name string) on class
    PK on field
    Required on field, parameter
    Trace on method, function
)

class Employee @{Table("employees")} {
    Id string @{PK}
    Email string @{Required}

    func Lookup(id int @{Required}) string @{Trace} {
        return this.Id
    }
}

func Helper(value string @{Required}) string @{Trace} {
    return value
}

func main() {
    descriptor := Employee.class
    table := descriptor.annotations.find(Table)
    _ = table.name
    _ = table.fullName
    _ = table.args[0]
    for field := range descriptor.fields {
        _ = field.annotations.has(PK)
        _ = field.annotations.all(Required)
    }
    for method := range descriptor.methods {
        _ = method.annotations.has(Trace)
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	for _, expected := range []string{
		"var GppAnnotation_Table =",
		"Args: []any{\"employees\"}",
		"Annotations: GppAnnotations{",
		"GppAnnotation_Table",
		".Find(GppAnnotation_Table)",
		".Has(GppAnnotation_PK)",
		".All(GppAnnotation_Required)",
		".FullName",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("annotation output missing %q:\n%s", expected, generated)
		}
	}
}

func TestEmitImplicitPrelude(t *testing.T) {
	file, err := ParseFile("prelude.gpp", `
func main() {
    values := []int{3, 1, 2}
    _ = values.Each(func(value int) error { return nil })
    values.Each(value => println(value))
    values.SortBy(value => value)
    values.ReverseBy(value => value)
    values = values.DeleteBy(value => value == 2)
    values = values.Append(4)
    values = values.Prepend(0)
    _, values, _ = values.Pop()
    values.Shuffle()
    _ = values.Sample()
    _ = values.Sample(n: 2)
    _ = values.Unique()
    _ = values.Any(func(value int) bool { return value == 2 })
    _ = values.Contains(2)
    _ = values.Filter(func(value int) bool { return value > 1 })
    _, _ = values.Find(func(value int) bool { return value == 1 })
    values.Sort()
    values.SortDesc()
    _, _ = values.Min()
    _, _ = values.Max()
    values.Reverse()

    text := " value "
    _ = text.Empty()
    _ = text.Blank()
	_, _ = "^[a-z]+$".CompileRegex()
	_ = "^[a-z]+$".CompileRegex().MatchString("go")

	options := map[string]string{"mode": "test"}
    _ = options.Has("mode")
    _, _ = options.Get("mode")
    _ = options.Get("missing", "default")
    _ = options.Keys()
    _ = options.Values()
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	for _, expected := range []string{
		"func GppPreludeExt___T_Each_",
		"func GppPreludeExt___T_Any_",
		"func GppPreludeExt___T_Contains_",
		"func GppPreludeExt___T_Sort_",
		"func GppPreludeExt___T_SortDesc_",
		"func GppPreludeExt___T_Min_",
		"func GppPreludeExt___T_Max_",
		"func GppPreludeExt_string_Empty_",
		"func GppPreludeExt_string_Blank_",
		"func GppPreludeExt_string_CompileRegex_",
		"func GppPreludeExt_map_K_V_Has_",
		"import (",
		"\"cmp\"",
		"\"sort\"",
		"\"regexp\"",
		"\"strings\"",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("implicit prelude output missing %q:\n%s", expected, generated)
		}
	}
}

func TestEmitCanDisablePrelude(t *testing.T) {
	file, err := ParseFile("no_prelude.gpp", `
func main() {
    values := []int{1}
    _ = values.Any(func(value int) bool { return value == 1 })
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := EmitWithOptions(file, CompileOptions{NoPrelude: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(code), "GppExt_") {
		t.Fatalf("no-prelude output unexpectedly contains generated prelude code:\n%s", code)
	}
	if !strings.Contains(string(code), ".Any(") {
		t.Fatalf("no-prelude output unexpectedly lowered the prelude call:\n%s", code)
	}
}

func TestEmitLowersContextualLambdas(t *testing.T) {
	file, err := ParseFile("lambdas.gpp", `
class User {
    Name string
    Active bool
}

class Team {
    Users []User
    Enabled bool

    func Active() []User {
        return this.Users.Filter(user => this.Enabled && user.Active)
    }
}

func Apply(value int, fn func(int) int) int {
    return fn(value)
}

func Choose(fn func(User) bool) string {
    return "bool"
}

func Choose(fn func(User) string) string {
    return "string"
}

func Run(fn func() string) string {
    return fn()
}

func main() {
    users := []User{User("Ada", true)}
	team := Team(users, true)
	_ = team.Active()
    _ = users.Any(user => user.Active)
    _ = users.Filter(user => {
        return user.Active
    })
    _ = users.Sort((left, right) => left.Name < right.Name)
    _ = Apply(2, value => value * 2)
    _ = Choose(user => user.Active)
    _ = Run(() => "done")

    var predicate func(User) bool
    predicate = user => user.Active
    _ = predicate
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	for _, expected := range []string{
		"func(user User) bool {",
		"func(left User, right User) bool {",
		"func(value int) int { return value * 2 }",
		"Choose__gpp_1_func_User__bool(func(user User) bool {",
		"func() string { return \"done\" }",
		"predicate = func(user User) bool { return user.Active }",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("lambda output missing %q:\n%s", expected, generated)
		}
	}
}

func TestEmitRejectsUntypedStandaloneLambda(t *testing.T) {
	file, err := ParseFile("untyped_lambda.gpp", `
func main() {
    predicate := value => value
    _ = predicate
}
`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "cannot infer type of lambda parameter") {
		t.Fatalf("expected standalone lambda inference error, got %v", err)
	}
}

func TestEmitRejectsValueLambdaWithoutReturn(t *testing.T) {
	file, err := ParseFile("missing_lambda_return.gpp", `
func Consume(fn func(int) bool) {}

func main() {
    Consume(value => {
        _ = value
    })
}
`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "lambda does not match any expected function type") {
		t.Fatalf("expected missing lambda return error, got %v", err)
	}
}

func TestEmitPrecompilesLiteralRegexpSwitchPatterns(t *testing.T) {
	file, err := ParseFile("regexp_switch.gpp", `
func main() {
    path := "/health"
    switch path {
    case regexp("^/health$"):
        _ = path
    case regexp("^/health$"):
        _ = path
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(code)
	if count := strings.Count(generated, "MustCompile"); count != 1 {
		t.Fatalf("expected duplicate literal patterns to share one compiled regexp, got %d:\n%s", count, generated)
	}
	if strings.Contains(generated, "case gppRegexp.MustCompile") || strings.Contains(generated, "regexp(`") {
		t.Fatalf("regexp switch case was not lowered to a cached matcher:\n%s", generated)
	}
}
