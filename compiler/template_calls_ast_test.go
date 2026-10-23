package compiler

import (
	"strings"
	"testing"
)

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

func TestTransformStaticTemplateCallsScopesDynamicExecute(t *testing.T) {
	context := constructorContext{
		ModulePath:       "generated",
		Package:          "main",
		Templates:        map[string]*TemplateDecl{"Card": {Name: "Card"}},
		AvailableImports: map[string]string{"tpl": "gpp/tpl"},
	}
	source := `func main() {
    tpl.Execute(&output, "<b>{{Card .}}</b>", "body")
}`
	transformed, handled, err := transformStaticTemplateCallsAST(source, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(transformed, `tpl.ExecuteIn("generated", &output`) {
		t.Fatalf("dynamic template execution did not receive its package scope: handled=%v source=%q", handled, transformed)
	}
}
