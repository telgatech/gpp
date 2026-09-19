package compiler

import (
	"strings"
	"testing"
)

func TestGeneratedImportHelpersUseLosslessTokens(t *testing.T) {
	body := "import (\n\t\"fmt\"\n\talias \"gpp/http\"\n)\n\nfunc main() { alias.Server() }\n"
	imports, end, ok := generatedImports(body)
	if !ok || len(imports) != 2 || end <= 0 {
		t.Fatalf("generated imports were not recognized from tokens: %#v, %d, %v", imports, end, ok)
	}
	if imports[1].Alias != "alias" || imports[1].Path != "gpp/http" {
		t.Fatalf("unexpected generated import metadata: %#v", imports[1])
	}
	if got := generatedImportAlias(body, "gppFmt"); got != "gppFmt" {
		t.Fatalf("unexpected free import alias: %q", got)
	}
	if got := generatedImportAlias(body, "alias"); got != "alias2" {
		t.Fatalf("generated import alias collision was not resolved: %q", got)
	}
	if !containsPackageSelector(body, "alias") {
		t.Fatal("token selector scan did not find an imported package selector")
	}

	inserted := insertAfterImports(body, "import \"os\"\n")
	if strings.Index(inserted, "import \"os\"") > strings.Index(inserted, "func main") {
		t.Fatalf("insertion was not placed after the import group: %q", inserted)
	}
	rewritten := rewriteOfficialImports(body, "example.test/app")
	if !strings.Contains(rewritten, `"example.test/app/gpp/http"`) {
		t.Fatalf("official import was not rewritten from token spans: %q", rewritten)
	}
}

func TestEnsureGeneratedImportsUsesTokenSelectors(t *testing.T) {
	body := "func main() { fmt.Println(\"ok\") }\n"
	context := constructorContext{AvailableImports: map[string]string{"fmt": "fmt"}}
	result := ensureGeneratedImports(body, context)
	if !strings.HasPrefix(result, "import (\n") || !strings.Contains(result, "\t\"fmt\"") {
		t.Fatalf("missing generated import was not added: %q", result)
	}
	if strings.Count(result, `"fmt"`) != 1 {
		t.Fatalf("generated import was duplicated: %q", result)
	}
}

func TestEmitStructuredGoDeclsUsesGoAST(t *testing.T) {
	file, err := ParseFile("structured-decl.gpp", "package main\n\ntype Person struct {\n\tName string\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	declaration, ok := file.Decls[0].(*GoDecl)
	if !ok {
		t.Fatalf("expected structured Go declaration, got %T", file.Decls[0])
	}

	// The emitter must be able to render the declaration from its AST even if
	// the lossless source buffer is unavailable at this boundary.
	file.Source = ""
	rendered, err := emitStructuredGoDecls(file, declaration, constructorContext{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "type Person struct") || !strings.Contains(rendered, "Name string") {
		t.Fatalf("structured Go AST was not emitted: %q", rendered)
	}
}

func TestValueDeclEmissionUsesStructuredFields(t *testing.T) {
	file, err := ParseFile("structured-value.gpp", "package main\n\nvar fallback = value ?? \"unknown\"\n")
	if err != nil {
		t.Fatal(err)
	}
	declaration, ok := file.Decls[0].(*ValueDecl)
	if !ok {
		t.Fatalf("expected structured value declaration, got %T", file.Decls[0])
	}

	file.Source = ""
	source, err := valueDeclASTSource(declaration)
	if err != nil {
		t.Fatal(err)
	}
	if source != "var fallback = value ?? \"unknown\"\n" {
		t.Fatalf("structured value declaration was rendered incorrectly: %q", source)
	}
}

func TestTokenExpressionSourcePreservesAnonymousStructFields(t *testing.T) {
	tokens, err := LexSource("anonymous-struct.gpp", "struct {\n    sync.RWMutex\n    byName map[string]any\n}{byName: map[string]any{}}")
	if err != nil {
		t.Fatal(err)
	}
	expression, err := ParseExpressionTokens(tokens)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := expressionNodeSource(expression)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "sync.RWMutex; byName map[string]any") {
		t.Fatalf("token expression lost anonymous struct field separator: %q", rendered)
	}
}

func TestEmitRejectsUnstructuredMixedDeclaration(t *testing.T) {
	file := &File{
		Name:       "unstructured.gpp",
		SourcePath: "unstructured.gpp",
		Source:     "magic value\n",
		Package:    "main",
		Decls: []Decl{&MixedDecl{
			SourceFile: "unstructured.gpp",
			SourceLine: 1,
		}},
	}
	if _, err := emitDecls(file, constructorContext{}, ""); err == nil || !strings.Contains(err.Error(), "no structured AST representation") {
		t.Fatalf("unstructured mixed declaration was not rejected: %v", err)
	}
}
