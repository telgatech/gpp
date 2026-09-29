package compiler

import (
	"strings"
	"testing"
)

func TestParseRejectsUseDeclaration(t *testing.T) {
	_, err := ParseFile("legacy.gpp", "use fmt\n")
	if err == nil || !strings.Contains(err.Error(), "use declarations are no longer supported") {
		t.Fatalf("expected use declaration error, got %v", err)
	}
}

func TestParseReportsSourceLineForExtensionErrors(t *testing.T) {
	_, err := ParseFile("broken.gpp", "\n\nclass Broken\n")
	if err == nil || !strings.Contains(err.Error(), "broken.gpp:3:") {
		t.Fatalf("expected source line in parser error, got %v", err)
	}
}

func TestParseSplitsDocumentedTopLevelFunctionsIntoASTDeclarations(t *testing.T) {
	file, err := ParseFile("documented-functions.gpp", `
func first() {
    println("first")
}

// second is documented between two top-level functions.
func second() {
    println("second")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 2 {
		t.Fatalf("expected two declarations, got %#v", file.Decls)
	}
	first, firstOK := file.Decls[0].(*FunctionDecl)
	second, secondOK := file.Decls[1].(*FunctionDecl)
	if !firstOK || !secondOK || first.Name != "first" || second.Name != "second" {
		t.Fatalf("top-level functions were not split into AST declarations: %#v", file.Decls)
	}
	if second.Doc != "second is documented between two top-level functions." {
		t.Fatalf("documentation comment was not attached to second function: %q", second.Doc)
	}
	if source := functionSource(second); !strings.Contains(source, "// second is documented") {
		t.Fatalf("function source lost its documentation comment: %q", source)
	}
}

func TestParseRejectsDuplicatePackageDeclaration(t *testing.T) {
	_, err := ParseFile("duplicate.gpp", "package one\npackage two\n")
	if err == nil || !strings.Contains(err.Error(), "duplicate package declaration") {
		t.Fatalf("expected duplicate package error, got %v", err)
	}
}

func TestParseEmbedDeclarations(t *testing.T) {
	file, err := ParseFile("embed.gpp", `
embed (
    assets "static/"
    schema "schema.sql"
)
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 1 {
		t.Fatalf("expected one embed declaration, got %d", len(file.Decls))
	}
	embed, ok := file.Decls[0].(*EmbedDecl)
	if !ok || len(embed.Entries) != 2 {
		t.Fatalf("unexpected embed declaration: %#v", file.Decls[0])
	}
	if embed.Entries[0].Name != "assets" || embed.Entries[0].Path != "static/" || !embed.Entries[0].Directory {
		t.Fatalf("unexpected directory embed entry: %#v", embed.Entries[0])
	}
	if embed.Entries[1].Name != "schema" || embed.Entries[1].Path != "schema.sql" || embed.Entries[1].Directory {
		t.Fatalf("unexpected file embed entry: %#v", embed.Entries[1])
	}
}

func TestParseTemplateDeclaration(t *testing.T) {
	file, err := ParseFile("page.gpp", `
template Page(post Post): Layout @{tpl.Path("/posts/{id}")} {
    <article data-id="{{param "id"}}">
	<h1>{{.Title}}</h1>
	</article>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 1 {
		t.Fatalf("expected one template declaration, got %d", len(file.Decls))
	}
	template, ok := file.Decls[0].(*TemplateDecl)
	if !ok {
		t.Fatalf("expected TemplateDecl, got %#v", file.Decls[0])
	}
	if template.Name != "Page" || templateParametersSource(template) != "post Post" || template.Layout != "Layout" || len(template.Annotations) != 1 {
		t.Fatalf("unexpected template metadata: %#v", template)
	}
	if !strings.Contains(template.Body, `{{param "id"}}`) {
		t.Fatalf("template body was not preserved: %q", template.Body)
	}
}

func TestParseDoesNotTreatFunctionTypeAsTopLevelFunction(t *testing.T) {
	file, err := ParseFile("function-type-decl.gpp", `
type Handler func(string) error
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 1 {
		t.Fatalf("expected one declaration, got %d", len(file.Decls))
	}
	goDecl, ok := file.Decls[0].(*GoDecl)
	if !ok || len(goDecl.Declarations) != 1 {
		t.Fatalf("function type was not retained as a structured Go declaration: %#v", file.Decls[0])
	}
}

func TestParseTopLevelFunctionValidationDoesNotCountMethods(t *testing.T) {
	file, err := ParseFile("method.gpp", `
type worker struct{}

func (w *worker) Run() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 2 {
		t.Fatalf("method-only source was not retained as structured Go declarations: %#v", file.Decls)
	}
	for _, declaration := range file.Decls {
		if _, ok := declaration.(*GoDecl); !ok {
			t.Fatalf("method-only source used an unexpected declaration fallback: %#v", declaration)
		}
	}
}

func TestParseRejectsUnstructuredTopLevelFunctionBody(t *testing.T) {
	_, err := ParseFile("broken-function.gpp", `
func broken() {
    if true {
`)
	if err == nil || !strings.Contains(err.Error(), "invalid function body") {
		t.Fatalf("expected structured function-body diagnostic, got %v", err)
	}
}

func TestParseRejectsUnstructuredTopLevelSyntax(t *testing.T) {
	_, err := ParseFile("unsupported-top-level.gpp", "magic value\n")
	if err == nil || !strings.Contains(err.Error(), "no structured AST representation") {
		t.Fatalf("expected structured top-level syntax diagnostic, got %v", err)
	}
}

func TestParseStructuredAnnotatedTopLevelFunction(t *testing.T) {
	file, err := ParseFile("annotated-function.gpp", `
annotation Trace on function

func Helper(value string) string @{Trace} {
    return value
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := file.Decls[1].(*FunctionDecl)
	if !ok || len(function.Annotations) != 1 || function.Annotations[0].Name != "Trace" {
		t.Fatalf("annotated function was not represented structurally: %#v", file.Decls)
	}
}

func TestParseStructuredLeadingAnnotatedTopLevelFunction(t *testing.T) {
	file, err := ParseFile("leading-annotated-function.gpp", `
annotation Route(path string) on function

@{Route("/users")}
func Users() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := file.Decls[1].(*FunctionDecl)
	if !ok || len(function.Annotations) != 1 || function.Annotations[0].Name != "Route" {
		t.Fatalf("leading annotation was not promoted to FunctionDecl: %#v", file.Decls)
	}
	if function.SourceSpan.Start != strings.Index(file.Source, "@{Route") {
		t.Fatalf("function span did not include leading annotation: %#v", function.SourceSpan)
	}
}

func TestParseEnumsSupportsSingleAndGroupedDeclarations(t *testing.T) {
	file, err := ParseFile("enums.gpp", `
enum Status int {
    Pending
    Active = 4
    Done
}

enum (
    Role string {
        User
        Admin = "admin"
    }
)
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 2 {
		t.Fatalf("expected two enum declarations, got %d", len(file.Decls))
	}
	status := file.Decls[0].(*EnumDecl)
	if enumBackingType(status) != "int" || enumMemberValueSource(status.Members[2]) != "5" {
		t.Fatalf("unexpected implicit enum values: %#v", status)
	}
	role := file.Decls[1].(*EnumDecl)
	if enumBackingType(role) != "string" || enumMemberValueSource(role.Members[0]) != `"User"` || enumMemberValueSource(role.Members[1]) != `"admin"` {
		t.Fatalf("unexpected string enum values: %#v", role)
	}
}

func TestParseEnumsRejectsDuplicateValues(t *testing.T) {
	_, err := ParseFile("duplicate-enum.gpp", `
enum Status int {
    Pending = 1
    Active = 1
}
`)
	if err == nil || !strings.Contains(err.Error(), "duplicate enum value") {
		t.Fatalf("expected duplicate enum value error, got %v", err)
	}
}

func TestParseValidatesDottedPackageNames(t *testing.T) {
	file, err := ParseFile("nested.gpp", "package telga.db.models\n")
	if err != nil {
		t.Fatal(err)
	}
	if file.Package != "telga.db.models" {
		t.Fatalf("unexpected logical package: %q", file.Package)
	}

	_, err = ParseFile("invalid.gpp", "package telga..models\n")
	if err == nil || !strings.Contains(err.Error(), "invalid package declaration") {
		t.Fatalf("expected invalid package error, got %v", err)
	}
}

func TestParseSupportsLogicalPackageImportsAndRejectsGeneratedPaths(t *testing.T) {
	file, err := ParseFile("logical-import.gpp", `package main

import foo.bar

func main() { bar.Greet() }
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Imports) != 1 || file.Imports[0].Path != "foo.bar" ||
		file.Imports[0].Alias != "bar" || !file.Imports[0].LogicalPackage {
		t.Fatalf("logical import was not structured correctly: %#v", file.Imports)
	}

	_, err = ParseFile("generated-import.gpp", `package main
import bar "generated/foo/bar"
`)
	if err == nil || !strings.Contains(err.Error(), "generated import paths are internal") {
		t.Fatalf("expected generated path import to be rejected, got %v", err)
	}
}

func TestParseIgnoresExtensionsInsideGoBodiesAndStrings(t *testing.T) {
	file, err := ParseFile("lexical_boundaries.gpp", "func main() {\n"+
		"    _ = `class NotAClass {}`\n"+
		"    // package not a declaration\n"+
		"    _ = 1\n"+
		"}\n\n"+
		"class RealClass {}\n")
	if err != nil {
		t.Fatal(err)
	}

	if len(file.Decls) != 2 {
		t.Fatalf("expected one compatibility declaration and one class, got %d", len(file.Decls))
	}
	if class, ok := file.Decls[1].(*ClassDecl); !ok || class.Name != "RealClass" {
		t.Fatalf("expected RealClass after Go body, got %#v", file.Decls[1])
	}
}

func TestParseMultiTargetExtension(t *testing.T) {
	file, err := ParseFile("multi_extension.gpp", `
extend
    string,
    []byte,
    map[string]int
{
    func Empty() bool {
        return len(this) == 0
    }
}

`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 1 {
		t.Fatalf("expected one declaration, got %d", len(file.Decls))
	}
	extension, ok := file.Decls[0].(*ExtendDecl)
	if !ok {
		t.Fatalf("expected extension declaration, got %#v", file.Decls[0])
	}
	expected := []string{"string", "[]byte", "map[string]int"}
	if strings.Join(extensionTargetNames(extension), "|") != strings.Join(expected, "|") {
		t.Fatalf("unexpected extension targets: %#v", extensionTargetNames(extension))
	}
}

func TestParseGenericExtensionTargetConstraint(t *testing.T) {
	file, err := ParseFile("generic_extension.gpp", `
extend []T where T cmp.Ordered {
    func Min() (T, bool) { return this[0], true }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	extension, ok := file.Decls[0].(*ExtendDecl)
	if !ok {
		t.Fatalf("expected extension declaration, got %#v", file.Decls[0])
	}
	if len(extensionTargetNames(extension)) != 1 || extensionTargetNames(extension)[0] != "[]T" {
		t.Fatalf("unexpected generic target: %#v", extensionTargetNames(extension))
	}
	constraint, constraintErr := typeNodeSource(extension.TargetConstraints["T"])
	if constraintErr != nil || constraint != "cmp.Ordered" {
		t.Fatalf("unexpected target constraints: %#v", extension.TargetConstraints)
	}
}
