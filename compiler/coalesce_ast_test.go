package compiler

import (
	"strings"
	"testing"
)

func TestTransformErrorCoalescingUsesBodyAST(t *testing.T) {
	context := constructorContext{
		CurrentResultAST:      parseTypeText("int"),
		CurrentParameterTypes: map[string]string{"value": "int"},
	}

	transformed, handled, err := transformErrorCoalescingAST("return value ?? 42", context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected the structured body AST to handle coalescing")
	}
	if strings.Contains(transformed, "??") {
		t.Fatalf("coalescing operator remained after AST lowering: %s", transformed)
	}
	if !strings.Contains(transformed, "__gppCoalesce[int]") {
		t.Fatalf("expected typed coalescing helper, got: %s", transformed)
	}
}

func TestTransformErrorCoalescingUsesSafeAccessPresence(t *testing.T) {
	person := &ClassDecl{
		Name:   "Person",
		Fields: []Field{{Name: "Name", TypeAST: parseTypeText("string")}},
	}
	context := constructorContext{
		CurrentParameterTypes: map[string]string{"person": "*Person"},
		Targets: map[string]constructorTarget{
			"Person": {Class: person, Classes: map[string]*ClassDecl{"Person": person}},
		},
	}
	transformed, handled, err := transformErrorCoalescingAST(`
func Read(person *Person) string {
    return person?.Name ?? "Unknown"
}
`, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || strings.Contains(transformed, "??") || strings.Contains(transformed, "?.") {
		t.Fatalf("expected safe access coalescing to lower, handled=%v output=%s", handled, transformed)
	}
	if !strings.Contains(transformed, `__gppSafeCoalesce[string](person == nil`) {
		t.Fatalf("expected nil-aware coalescing helper, got: %s", transformed)
	}
}

func TestTransformErrorCoalescingInfersLocalTypesFromBodyAST(t *testing.T) {
	context := constructorContext{}
	transformed, handled, err := transformErrorCoalescingAST(`
	value := 1.5
	return value ?? 2.0
`, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(transformed, "__gppCoalesce[float64]") {
		t.Fatalf("expected local AST type inference, handled=%v output=%s", handled, transformed)
	}
}

func TestTransformErrorCoalescingLowersWholeFunctionASTInOnePass(t *testing.T) {
	context := constructorContext{FunctionSignatures: map[string][]callableSignature{
		"ParsePort": {{
			Parameters: []parameterInfo{{Name: "value", TypeAST: parseTypeText("string")}},
			ResultAST:  parseTypeText("int"),
		}},
	}}
	source := `func main() {
	first := ParsePort("") ?? 8080
	second := ParsePort("") ?? ParsePort("") ?? 3000
}`
	transformed, handled, err := transformErrorCoalescingAST(source, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || strings.Contains(transformed, "??") {
		t.Fatalf("expected every coalescing expression in the function AST to be lowered, handled=%v output=%s", handled, transformed)
	}
	if strings.Count(transformed, "__gppCoalesce[int]") != 3 {
		t.Fatalf("expected three typed coalescing helpers, output=%s", transformed)
	}
}
