package compiler

import (
	"strings"
	"testing"
)

func TestTransformEnumsUsesBodyAST(t *testing.T) {
	context := constructorContext{Enums: map[string]*EnumDecl{
		"Status": {Name: "Status", Members: []EnumMember{{Name: "Active"}}},
	}}

	transformed, handled, err := transformEnumsAST("return Status.Active", context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected the structured body AST to handle enum member lowering")
	}
	if strings.Contains(transformed, "Status.Active") {
		t.Fatalf("enum member remained after AST lowering: %s", transformed)
	}
	if !strings.Contains(transformed, "GppEnum_Status_Active") {
		t.Fatalf("unexpected enum AST lowering: %s", transformed)
	}
}

func TestTransformEnumsUsesASTTypesForRangeValues(t *testing.T) {
	context := constructorContext{Enums: map[string]*EnumDecl{
		"Status": {Name: "Status", Members: []EnumMember{{Name: "Active"}}},
	}}

	source := `for _, item := range Status.values {
	fmt.Println(item.name, item.value)
}`
	transformed, handled, err := transformEnumsAST(source, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected the structured range body to handle enum metadata lowering")
	}
	if strings.Contains(transformed, "item.name") || strings.Contains(transformed, "item.value") {
		t.Fatalf("enum range metadata remained after AST lowering: %s", transformed)
	}
	if !strings.Contains(transformed, "item.Name") || !strings.Contains(transformed, "item.Value") {
		t.Fatalf("unexpected enum range lowering: %s", transformed)
	}
}
