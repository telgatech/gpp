package compiler

import (
	"strings"
	"testing"
)

func TestTransformPolymorphicDeclarationsUsesBodyAST(t *testing.T) {
	file, err := ParseFile("polymorphism-debug.gpp", `
class Person { func Use(other Person) string { return other.String() } }
class Employee: Person {}
func main() { person := Person(); _ = person.Use(Employee()) }
`)
	if err != nil {
		t.Fatal(err)
	}
	model, err := ResolveProgram(&Program{Files: []*File{file}})
	if err != nil {
		t.Fatal(err)
	}
	context := localConstructorContext(model.Packages[file.Package].Classes)
	context.FunctionSignatures, err = functionSignaturesForFile(file)
	if err != nil {
		t.Fatal(err)
	}
	main := file.Decls[2].(*FunctionDecl)
	transformed, handled, err := transformPolymorphicDeclarationsAST(functionSource(main), context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(transformed, "person.Use(&Employee())") {
		t.Fatalf("expected structured polymorphic lowering, handled=%v output=%s", handled, transformed)
	}
}
