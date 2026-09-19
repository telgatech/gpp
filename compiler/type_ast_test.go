package compiler

import (
	"strings"
	"testing"
)

func TestParseTypeBuildsStructuredContainers(t *testing.T) {
	tokens, err := LexSource("type.gpp", `map[string][]*Person`)
	if err != nil {
		t.Fatal(err)
	}
	typeNode, err := ParseTypeTokens(tokens)
	if err != nil {
		t.Fatal(err)
	}
	mapType, ok := typeNode.(*MapType)
	if !ok {
		t.Fatalf("expected map type, got %#v", typeNode)
	}
	if _, ok := mapType.Key.(*NamedType); !ok {
		t.Fatalf("expected named map key, got %#v", mapType.Key)
	}
	slice, ok := mapType.Value.(*SliceType)
	if !ok {
		t.Fatalf("expected slice map value, got %#v", mapType.Value)
	}
	pointer, ok := slice.Element.(*PointerType)
	if !ok {
		t.Fatalf("expected pointer slice element, got %#v", slice.Element)
	}
	person, ok := pointer.Element.(*NamedType)
	if !ok || len(person.Parts) != 1 || person.Parts[0] != "Person" {
		t.Fatalf("unexpected person type: %#v", pointer.Element)
	}
}

func TestParseTypeBuildsEllipsisArray(t *testing.T) {
	typeNode, err := ParseTypeTokens(expressionTokens(t, `[...]Person`))
	if err != nil {
		t.Fatal(err)
	}
	array, ok := typeNode.(*ArrayType)
	if !ok || !array.Ellipsis || array.Element == nil {
		t.Fatalf("expected ellipsis array type, got %#v", typeNode)
	}
	if source, err := typeNodeSource(array); err != nil || source != "[...]Person" {
		t.Fatalf("ellipsis array did not round-trip: %q (%v)", source, err)
	}
}

func TestParseTypeBuildsChannelDirections(t *testing.T) {
	for _, test := range []struct {
		source    string
		direction string
	}{
		{source: "chan int", direction: "both"},
		{source: "chan<- int", direction: "send"},
		{source: "<-chan int", direction: "receive"},
	} {
		t.Run(test.source, func(t *testing.T) {
			typeNode, err := ParseTypeTokens(expressionTokens(t, test.source))
			if err != nil {
				t.Fatal(err)
			}
			channel, ok := typeNode.(*ChannelType)
			if !ok || channel.Direction != test.direction {
				t.Fatalf("expected %s channel, got %#v", test.direction, typeNode)
			}
			if source, err := typeNodeSource(channel); err != nil || source != test.source {
				t.Fatalf("channel type did not round-trip: %q (%v)", source, err)
			}
		})
	}
}

func TestParseTypeBuildsStructuredFunctionType(t *testing.T) {
	tokens, err := LexSource("function-type.gpp", `func(value int, rest ...string) (bool, error)`)
	if err != nil {
		t.Fatal(err)
	}
	typeNode, err := ParseTypeTokens(tokens)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := typeNode.(*FunctionType)
	if !ok || len(function.Parameters) != 2 || len(function.Results) != 2 {
		t.Fatalf("expected structured function type, got %#v", typeNode)
	}
	if function.Parameters[0].Name != "value" || function.Parameters[1].Type == nil {
		t.Fatalf("function parameters were not structured: %#v", function.Parameters)
	}
	if _, ok := function.Parameters[1].Type.(*VariadicType); !ok {
		t.Fatalf("variadic function parameter was not structured: %#v", function.Parameters[1].Type)
	}
	if source, err := typeNodeSource(function); err != nil || source != "func(value int, rest ...string) (bool, error)" {
		t.Fatalf("function type did not round-trip through AST: %q (%v)", source, err)
	}
}

func TestParseTypeBuildsStructuredCompositeTypes(t *testing.T) {
	structNode, err := ParseTypeTokens(expressionTokens(t, "struct { Name string; Age int }"))
	if err != nil {
		t.Fatal(err)
	}
	structure, ok := structNode.(*StructType)
	if !ok || len(structure.Fields) != 2 || structure.Fields[0].Names[0] != "Name" {
		t.Fatalf("expected structured struct type, got %#v", structNode)
	}
	if source, err := typeNodeSource(structure); err != nil || source != "struct { Name string; Age int }" {
		t.Fatalf("struct type did not round-trip: %q (%v)", source, err)
	}

	interfaceNode, err := ParseTypeTokens(expressionTokens(t, "interface { Read([]byte) (int, error); io.Closer }"))
	if err != nil {
		t.Fatal(err)
	}
	interfaceType, ok := interfaceNode.(*InterfaceType)
	if !ok || len(interfaceType.Methods) != 1 || len(interfaceType.Embeds) != 1 {
		t.Fatalf("expected structured interface type, got %#v", interfaceNode)
	}
	if source, err := typeNodeSource(interfaceType); err != nil || source != "interface { io.Closer; Read([]byte) (int, error) }" {
		t.Fatalf("interface type did not round-trip: %q (%v)", source, err)
	}
}

func TestParseTypeParametersBuildsConstraints(t *testing.T) {
	parameters := parseTypeParameterNodes("[T any, N ~int | ~string]")
	if len(parameters) != 2 || parameters[0].Name != "T" || parameters[0].Constraint == nil || parameters[1].Constraint == nil {
		t.Fatalf("expected structured type parameter constraints, got %#v", parameters)
	}
	source, err := typeParameterNodesSource(parameters)
	if err != nil || source != "[T any, N ~int|~string]" {
		t.Fatalf("type parameters did not round-trip: %q (%v)", source, err)
	}
	if _, ok := parameters[1].Constraint.(*UnionType); !ok {
		t.Fatalf("union constraint was not structured: %#v", parameters[1].Constraint)
	}
}

func TestDeclarationsExposeTypeAST(t *testing.T) {
	file, err := ParseFile("typed-declarations.gpp", `
class User {
    Friends map[string][]*Person
    func Rename(value string = "default") []string {
        return []string{value}
    }
}

`)
	if err != nil {
		t.Fatal(err)
	}
	class := file.Decls[0].(*ClassDecl)
	if class.Fields[0].TypeAST == nil {
		t.Fatal("field did not receive a type AST")
	}
	if class.Fields[0].Owner != file || fieldTypeSource(class.Fields[0]) != "map[string][]*Person" {
		t.Fatalf("field type source was not AST-backed: owner=%p type=%q", class.Fields[0].Owner, fieldTypeSource(class.Fields[0]))
	}
	method := class.Methods[0]
	if len(method.ParameterAST) == 0 || method.ResultAST == nil {
		t.Fatalf("parsed method lost structured signature: %#v", method)
	}
	if methodParametersSource(method) != "value string = \"default\"" || methodResultSource(method) != "[]string" {
		t.Fatalf("method signature source was not span-backed: parameters=%q result=%q", methodParametersSource(method), methodResultSource(method))
	}
	if method.ResultAST == nil || len(method.ParameterAST) != 1 || method.ParameterAST[0].Type == nil {
		t.Fatalf("method types were not structured: %#v", method)
	}
	if method.ParameterAST[0].Default == nil {
		t.Fatalf("parameter default was not parsed: %#v", method.ParameterAST[0])
	}
}

func TestAnnotationArgumentsExposeExpressionAST(t *testing.T) {
	file, err := ParseFile("annotation-expression.gpp", `
@{route("/users", method: "GET")}
func list() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := file.Decls[0].(*FunctionDecl)
	if !ok || len(function.Annotations) != 1 {
		t.Fatalf("expected structured annotated function declaration, got %#v", file.Decls[0])
	}
	use := function.Annotations[0]
	if len(use.ArgumentsAST) != 2 {
		t.Fatalf("expected two annotation argument expressions, got %#v", use.ArgumentsAST)
	}
	if _, ok := use.ArgumentsAST[0].(*LiteralExpr); !ok {
		t.Fatalf("expected first annotation argument literal, got %#v", use.ArgumentsAST[0])
	}
}

func TestAnnotationValidationUsesGoPlusExpressionAST(t *testing.T) {
	program := parseProgram(t, `
annotation Label(value string) on field
class User {
    Name string @{Label("hello {{name}}")}
}
`)
	if _, err := ResolveProgram(program); err != nil {
		t.Fatalf("interpolated annotation argument should validate through the Go++ AST: %v", err)
	}
}

func TestTemplateAndAnnotationParametersExposeTypeAST(t *testing.T) {
	file, err := ParseFile("parameter-declarations.gpp", `
annotation Route(path string, secure bool = false) on template
template Page(post Post, title string) : Layout @{Route("/posts")} {
    <h1>{{.Title}}</h1>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 2 {
		t.Fatalf("expected annotation and template declarations, got %#v", file.Decls)
	}
	annotation, ok := file.Decls[0].(*AnnotationDecl)
	if !ok {
		t.Fatalf("expected AnnotationDecl, got %#v", file.Decls[0])
	}
	if len(annotation.ParameterAST) != 2 || annotation.ParameterAST[0].Type == nil || annotation.ParameterAST[1].Default == nil {
		t.Fatalf("annotation parameters were not structured: %#v", annotation.ParameterAST)
	}
	template, ok := file.Decls[1].(*TemplateDecl)
	if !ok {
		t.Fatalf("expected TemplateDecl, got %#v", file.Decls[1])
	}
	if len(template.ParameterAST) != 2 || template.ParameterAST[0].Type == nil || template.ParameterAST[1].Type == nil {
		t.Fatalf("template parameters were not structured: %#v", template.ParameterAST)
	}
}

func TestDeclarationRelationshipsExposeTypeAST(t *testing.T) {
	file, err := ParseFile("relationship-types.gpp", `
class Child : Parent, base.Base {
}

extend *Child, string {
    func Label() string { return "" }
}
enum Status int {
    Ready = 1
}
`)
	if err != nil {
		t.Fatal(err)
	}
	class := file.Decls[0].(*ClassDecl)
	extend := file.Decls[1].(*ExtendDecl)
	enum := file.Decls[2].(*EnumDecl)
	if len(class.ParentAST) != 2 || class.ParentAST[0] == nil || class.ParentAST[1] == nil {
		t.Fatalf("class parent types were not structured: %#v", class.ParentAST)
	}
	if len(extend.TargetAST) != 2 || extend.TargetAST[0] == nil || extend.TargetAST[1] == nil {
		t.Fatalf("extension target types were not structured: %#v", extend.TargetAST)
	}
	if enum.BackingTypeAST == nil {
		t.Fatalf("enum backing type was not structured: %#v", enum)
	}
}

func TestExtensionTargetNormalizationUsesTypeAST(t *testing.T) {
	if got := normalizeExtensionTarget(" * map [ string ] [ ] *Person "); got != "*map[string][]*Person" {
		t.Fatalf("unexpected normalized extension target: %q", got)
	}
}

func TestDeclarationsExposeSourceSpans(t *testing.T) {
	source := `package main

annotation Trace on class
class User {
}

enum Status int {
    Ready = 1
}
extend User {
}
embed schema "schema.sql"
template Page() {
    <h1>Hi</h1>
}
func main() {
}
`
	file, err := ParseFile("spans.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 7 {
		t.Fatalf("unexpected declaration count: %#v", file.Decls)
	}
	for _, declaration := range file.Decls {
		span := declaration.Span()
		if span.Start < 0 || span.End <= span.Start || span.End > len(source) || span.Line <= 0 || span.Column <= 0 {
			t.Fatalf("invalid declaration source span: %T %#v", declaration, span)
		}
	}
	if file.Decls[1].Span().Start != strings.Index(source, "class User") {
		t.Fatalf("class span does not point to its declaration: %#v", file.Decls[1].Span())
	}
}

func TestMembersExposeSourceSpans(t *testing.T) {
	file, err := ParseFile("member-spans.gpp", `class User {
    Name string
    func Rename(value string) string {
        return value
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	class := file.Decls[0].(*ClassDecl)
	if fieldSpan := class.Fields[0].Span(); fieldSpan.Start >= fieldSpan.End || fieldSpan.End > len(file.Source) {
		t.Fatalf("invalid field span: %#v", fieldSpan)
	}
	if methodSpan := class.Methods[0].Span(); methodSpan.Start >= methodSpan.End || methodSpan.End > len(file.Source) {
		t.Fatalf("invalid method span: %#v", methodSpan)
	}
}
