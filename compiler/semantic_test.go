package compiler

import (
	"strings"
	"testing"
)

func parseProgram(t *testing.T, sources ...string) *Program {
	t.Helper()

	program := &Program{}
	for index, source := range sources {
		file, err := ParseFile("file"+string(rune('A'+index))+".gpp", source)
		if err != nil {
			t.Fatal(err)
		}
		program.Files = append(program.Files, file)
	}

	return program
}

func TestResolveProgramBuildsCrossFilePackageSymbols(t *testing.T) {
	program := parseProgram(t,
		"package telga.web\nclass Person {\n    Name string\n}\n",
		"package telga.web\nclass Employee: Person {}\n",
	)

	model, err := ResolveProgram(program)
	if err != nil {
		t.Fatal(err)
	}

	pkg := model.Packages["telga.web"]
	if pkg == nil || pkg.Classes["Person"] == nil || pkg.Classes["Employee"] == nil {
		t.Fatalf("cross-file package symbols were not built: %#v", model.Packages)
	}
}

func TestResolveProgramRejectsDuplicateClasses(t *testing.T) {
	program := parseProgram(t,
		"class Person {}\n",
		"class Person {}\n",
	)

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "duplicate class Person") {
		t.Fatalf("expected duplicate class error, got %v", err)
	}
}

func TestResolveProgramRejectsInvalidInheritance(t *testing.T) {
	program := parseProgram(t, "class Employee: Missing {}\n")

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "unresolved parent Missing") {
		t.Fatalf("expected unresolved parent error, got %v", err)
	}
}

func TestResolveProgramRejectsInheritanceCycles(t *testing.T) {
	program := parseProgram(t, `
class A: C {}
class B: A {}
class C: B {}
`)

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "inheritance cycle") {
		t.Fatalf("expected inheritance cycle error, got %v", err)
	}
}

func TestResolveProgramRejectsDuplicateMembers(t *testing.T) {
	program := parseProgram(t, `
class Person {
    Name string
    Name int

    func Speak() {}
    func Speak() {}
}
`)

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "field Name more than once") {
		t.Fatalf("expected duplicate field error, got %v", err)
	}
}

func TestResolveProgramAllowsTypedMethodOverloads(t *testing.T) {
	program := parseProgram(t, `
class Formatter {
    func Format(value int) string { return "int" }
    func Format(value string) string { return "string" }
}
`)

	if _, err := ResolveProgram(program); err != nil {
		t.Fatal(err)
	}
	methods := program.Files[0].Decls[0].(*ClassDecl).Methods
	if methods[0].GoName != "Format__gopp_1_int" || methods[1].GoName != "Format__gopp_1_string" {
		t.Fatalf("typed overloads were not assigned stable Go names: %#v", methods)
	}
}

func TestResolveProgramRejectsDuplicateTypedMethodOverloads(t *testing.T) {
	program := parseProgram(t, `
class Formatter {
	func Format(value int) string { return "one" }
	func Format(other int) string { return "two" }
}
`)

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "parameter types int") {
		t.Fatalf("expected duplicate typed overload error, got %v", err)
	}
}

func TestResolveProgramValidatesAnnotationPlacement(t *testing.T) {
	program := parseProgram(t, `
annotation PK on field
class Employee @{PK} {}
`)
	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "annotation PK cannot be applied to class") {
		t.Fatalf("expected annotation placement error, got %v", err)
	}
}

func TestResolveProgramValidatesAnnotationArguments(t *testing.T) {
	program := parseProgram(t, `
annotation Min(value int) on field
class Employee {
    Age int @{Min("18")}
}
`)
	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("expected annotation argument type error, got %v", err)
	}
}

func TestResolveProgramRejectsUnknownAnnotation(t *testing.T) {
	program := parseProgram(t, `
class Employee {
    Name string @{Required}
}
`)
	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "undefined annotation Required") {
		t.Fatalf("expected unknown annotation error, got %v", err)
	}
}
