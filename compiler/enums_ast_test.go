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

func TestEnumMemberRetainsExactSourceNameSpan(t *testing.T) {
	source := "package main\nenum Status string {\n    Ready = \"ready\"\n    Other = \"other\"\n}\n"
	file, err := ParseFile("enum.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	var status *EnumDecl
	for _, declaration := range file.Decls {
		if enum, ok := declaration.(*EnumDecl); ok && enum.Name == "Status" {
			status = enum
			break
		}
	}
	if status == nil || len(status.Members) != 2 {
		t.Fatalf("expected parsed Status members, got %#v", status)
	}
	for index, name := range []string{"Ready", "Other"} {
		member := status.Members[index]
		if member.NameSpan.End <= member.NameSpan.Start || source[member.NameSpan.Start:member.NameSpan.End] != name {
			t.Errorf("enum member %s source span = %#v (%q)", name, member.NameSpan, source[member.NameSpan.Start:member.NameSpan.End])
		}
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
