package compiler

import "testing"

func TestTransformStaticTemplateCallsUsesBodyAST(t *testing.T) {
	context := constructorContext{
		Templates:        map[string]*TemplateDecl{"Page": {Name: "Page"}},
		AvailableImports: map[string]string{"tpl": "gpp/tpl"},
	}
	transformed, handled, err := transformStaticTemplateCallsAST("return tpl.Page(value)", context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || transformed != "return __gpp_tpl_Page(value)" {
		t.Fatalf("unexpected template lowering: handled=%v source=%q", handled, transformed)
	}
}
