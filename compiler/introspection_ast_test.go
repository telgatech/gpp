package compiler

import (
	"strings"
	"testing"
)

func TestTransformIntrospectionUsesBodyAST(t *testing.T) {
	class := &ClassDecl{Name: "Employee"}
	context := constructorContext{
		Targets: map[string]constructorTarget{
			"Employee": {Class: class, InterfaceName: "GppEmployee"},
		},
		Introspection: newIntrospectionContext(map[string]*ClassDecl{"Employee": class}),
		CurrentClass:  "Employee",
		CurrentParameterTypes: map[string]string{
			"this": "*Employee",
		},
	}

	transformed, handled, err := transformIntrospectionAST("return this.class.name", context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || transformed != "return this.GppRuntimeClass().Name" {
		t.Fatalf("unexpected AST introspection lowering: handled=%v source=%q", handled, transformed)
	}
}

func TestTransformIntrospectionASTTracksMetadataRanges(t *testing.T) {
	class := &ClassDecl{Name: "Employee"}
	context := constructorContext{
		Targets: map[string]constructorTarget{
			"Employee": {Class: class, InterfaceName: "GppEmployee"},
		},
		Introspection: newIntrospectionContext(map[string]*ClassDecl{"Employee": class}),
	}

	source := `descriptor := Employee.class
for field := range descriptor.fields {
	_ = field.name
}`
	transformed, handled, err := transformIntrospectionAST(source, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected metadata AST lowering to handle the body")
	}
	for _, expected := range []string{
		"descriptor := GppEmployeeClass",
		"for _, field := range descriptor.Fields",
		"_ = field.Name",
	} {
		if !strings.Contains(transformed, expected) {
			t.Fatalf("metadata lowering missing %q:\n%s", expected, transformed)
		}
	}
}
