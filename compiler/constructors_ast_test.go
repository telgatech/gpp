package compiler

import (
	"strings"
	"testing"
)

func TestTransformConstructorsUsesBodyAST(t *testing.T) {
	person := &ClassDecl{
		Name: "Person",
		Fields: []Field{
			{Name: "Name", TypeAST: parseTypeText("string")},
			{Name: "Age", TypeAST: parseTypeText("int")},
		},
	}
	context := localConstructorContext(map[string]*ClassDecl{"Person": person})

	transformed, handled, err := transformConstructorsAST(`return Person(Name: "Bob", Age: 42)`, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected the structured body AST to handle constructor lowering")
	}
	if strings.Contains(transformed, "Person(") {
		t.Fatalf("constructor call remained after AST lowering: %s", transformed)
	}
	if !strings.Contains(transformed, `Name: "Bob"`) || !strings.Contains(transformed, "Age: 42") {
		t.Fatalf("named constructor fields were not preserved: %s", transformed)
	}
}

func TestTransformConstructorsSupportsExplicitGenericClassArguments(t *testing.T) {
	box := &ClassDecl{
		Name:          "Box",
		TypeParamsAST: parseTypeParameterNodes("[T any]"),
		Fields:        []Field{{Name: "Value", TypeAST: parseTypeText("T")}},
	}
	context := localConstructorContext(map[string]*ClassDecl{"Box": box})
	transformed, handled, err := transformConstructorsAST(`func main() {
	box := Box[int](Value: 42)
	_ = box
}`, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected the generic constructor call to be lowered")
	}
	if !strings.Contains(transformed, `Box[int]{Value: 42}`) {
		t.Fatalf("expected an instantiated generic composite literal, got:\n%s", transformed)
	}
}

func TestTransformConstructorsUsesTopLevelFunctionBodies(t *testing.T) {
	person := &ClassDecl{
		Name: "Person",
		Fields: []Field{
			{Name: "Name", TypeAST: parseTypeText("string")},
			{Name: "Age", TypeAST: parseTypeText("int")},
		},
	}
	context := localConstructorContext(map[string]*ClassDecl{"Person": person})

	source := `func main() {
	p := Person("Bob", 42)
	_ = p
}`
	transformed, handled, err := transformConstructorsAST(source, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected a top-level function body to be handled by the constructor AST")
	}
	if strings.Contains(transformed, `Person("Bob", 42)`) {
		t.Fatalf("constructor call remained after top-level AST lowering: %s", transformed)
	}
	if !strings.Contains(transformed, `Person{Name: "Bob", Age: 42}`) {
		t.Fatalf("unexpected top-level constructor lowering: %s", transformed)
	}
}
