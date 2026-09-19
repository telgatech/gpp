package compiler

import (
	"strings"
	"testing"
)

func TestLexSourceRetainsCommentsLiteralsAndGoPlusOperators(t *testing.T) {
	source := "// docs\nfunc main() {\n" +
		"value := \"hello {world}\" // trailing\n" +
		"raw := `raw {world}`\n" +
		"if value?.TrimSpace() ?? \"fallback\" {\n}\n}\n"
	tokens, err := LexSource("tokens.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) == 0 || tokens[len(tokens)-1].Kind != TokenEOF {
		t.Fatal("lexer did not emit EOF")
	}
	comments := []string{}
	operators := map[string]bool{}
	stringsFound := []string{}
	for _, token := range tokens {
		switch token.Kind {
		case TokenComment:
			comments = append(comments, token.Text)
		case TokenOperator:
			operators[token.Text] = true
		case TokenString, TokenRawString:
			stringsFound = append(stringsFound, token.Text)
		}
	}
	if len(comments) != 2 || comments[0] != "// docs" || comments[1] != "// trailing" {
		t.Fatalf("comments were not retained: %#v", comments)
	}
	for _, operator := range []string{":=", "?.", "??"} {
		if !operators[operator] {
			t.Fatalf("operator %q was not tokenized", operator)
		}
	}
	if len(stringsFound) != 3 || stringsFound[0] != `"hello {world}"` || stringsFound[1] != "`raw {world}`" || stringsFound[2] != `"fallback"` {
		t.Fatalf("literal contents were not retained: %#v", stringsFound)
	}
	if tokens[0].Span.Line != 1 || tokens[0].Span.Column != 1 || tokens[0].Span.Start != 0 {
		t.Fatalf("unexpected first token span: %#v", tokens[0].Span)
	}
}

func TestParseFilePopulatesSourceAndBodyTokens(t *testing.T) {
	source := `package main

func main() {
    // body comment
    println("hello")
}
`
	file, err := ParseFile("body.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	if file.Source != source || len(file.Tokens) == 0 || len(file.Comments) != 1 {
		t.Fatalf("source metadata was not populated: source=%t tokens=%d comments=%d", file.Source == source, len(file.Tokens), len(file.Comments))
	}
	function, ok := file.Decls[0].(*FunctionDecl)
	if !ok || len(function.Method.BodyTokens) == 0 {
		t.Fatalf("top-level function did not receive structured tokens: %#v", file.Decls)
	}
	if function.GoAST == nil || function.GoASTFileSet == nil {
		t.Fatalf("ordinary Go declaration was not parsed into Go AST: %#v", function)
	}
	if function.Owner != file {
		t.Fatalf("ordinary declaration lost owner: owner=%p file=%p", function.Owner, file)
	}
	if function.GoASTFileSet.Position(function.GoAST.Pos()).Line != 2 {
		t.Fatalf("structured Go AST lost source position: %#v", function.GoASTFileSet.Position(function.GoAST.Pos()))
	}
	if function.Method.BodyAST == nil {
		t.Fatalf("top-level function was not represented by structured metadata: %#v", function)
	}
}

func TestParseFileTracksMultipleTopLevelFunctionASTs(t *testing.T) {
	file, err := ParseFile("functions.gpp", `
func first(value string) string {
    return value
}

func second() {
    println("second")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 2 {
		t.Fatalf("expected two structured function declarations, got %d", len(file.Decls))
	}
	first, firstOK := file.Decls[0].(*FunctionDecl)
	second, secondOK := file.Decls[1].(*FunctionDecl)
	if !firstOK || !secondOK || first.Name != "first" || second.Name != "second" {
		t.Fatalf("top-level functions were not split into structured declarations: %#v", file.Decls)
	}
	if methodParametersSource(first.Method) != `value string` || second.Method.BodyAST == nil {
		t.Fatalf("structured function signatures/bodies were incomplete: %#v %#v", first, second)
	}
}

func TestTokenInterpolationDetectionIgnoresComments(t *testing.T) {
	tokens, err := LexSource("interpolation.gpp", "// \"{{comment}}\"\nvalue := \"{{name}}\"\nraw := `literal {{not_an_expression}}`\n")
	if err != nil {
		t.Fatal(err)
	}
	if !tokensHaveInterpolation(tokens) {
		t.Fatal("interpolation literal was not detected")
	}
	commentOnly, err := LexSource("comment.gpp", `// "{{comment}}"`)
	if err != nil {
		t.Fatal(err)
	}
	if tokensHaveInterpolation(commentOnly) {
		t.Fatal("comment text incorrectly triggered interpolation")
	}
}

func TestGoImportsUsesLosslessTokensForMixedGoPlusDeclarations(t *testing.T) {
	file, err := ParseFile("mixed-imports.gpp", `
import f "fmt"

func first(value string = "default") string {
    return value
}
`)
	if err != nil {
		t.Fatal(err)
	}
	imports, err := goImports(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) != 1 || imports[0].Name == nil || imports[0].Name.Name != "f" || imports[0].Path.Value != `"fmt"` {
		t.Fatalf("mixed Go++ import was not extracted from tokens: %#v", imports)
	}
}

func TestFileExposesPackageAndImportAST(t *testing.T) {
	file, err := ParseFile("source-declarations.gpp", `package example.web

import (
    "fmt"
    db "database/sql"
)
`)
	if err != nil {
		t.Fatal(err)
	}
	if file.PackageAST == nil || file.PackageAST.Name != "example.web" {
		t.Fatalf("package declaration was not structured: %#v", file.PackageAST)
	}
	if len(file.Imports) != 2 || file.Imports[0].Path != "fmt" || file.Imports[1].Alias != "db" || file.Imports[1].Path != "database/sql" {
		t.Fatalf("import declarations were not structured: %#v", file.Imports)
	}
}

func TestFunctionSourceCanRenderStructuredGoAST(t *testing.T) {
	file, err := ParseFile("structured-source.gpp", "func greet() { println(\"hi\") }\n")
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	if source := functionSource(function); source == "" || !strings.Contains(source, "func greet") {
		t.Fatalf("structured Go AST did not provide compatibility source: %q", source)
	}
}

func TestParsedCompatibilityDeclDoesNotDuplicateExecutableSource(t *testing.T) {
	file, err := ParseFile("structured-value.gpp", `package main

var fallback = value ?? "unknown"
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 1 {
		t.Fatalf("expected one compatibility declaration, got %d", len(file.Decls))
	}
	value, ok := file.Decls[0].(*ValueDecl)
	if !ok {
		t.Fatalf("expected structured ValueDecl, got %T", file.Decls[0])
	}
	if len(value.Values) != 1 || value.Keyword != "var" {
		t.Fatalf("value declaration was not structured: %#v", value)
	}
	if value.SourceSpan.Start < 0 || value.SourceSpan.End <= value.SourceSpan.Start {
		t.Fatalf("structured declaration lost its source span: %#v", value.SourceSpan)
	}
}

func TestAnnotatedGoDeclarationUsesGoASTContainer(t *testing.T) {
	file, err := ParseFile("annotated-type.gpp", `package main

annotation Marker on type

type User struct {
	Name string
} @{Marker}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 2 {
		t.Fatalf("expected annotation and type declarations, got %d", len(file.Decls))
	}
	declaration, ok := file.Decls[1].(*GoDecl)
	if !ok {
		t.Fatalf("expected annotated Go declaration to use GoDecl, got %T", file.Decls[1])
	}
	if len(declaration.AnnotationPlacements) != 1 || declaration.AnnotationPlacements[0].Target != AnnotationTargetType {
		t.Fatalf("annotation placement was not preserved: %#v", declaration.AnnotationPlacements)
	}
}
