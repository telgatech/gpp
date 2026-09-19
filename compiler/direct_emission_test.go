package compiler

import (
	"strings"
	"testing"
)

func TestDirectFunctionEmissionUsesStructuredBody(t *testing.T) {
	file, err := ParseFile("direct.gpp", `
func Add(value int) int {
    next := value + 1
    return next
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := file.Decls[0].(*FunctionDecl)
	if !ok {
		t.Fatalf("expected function declaration, got %T", file.Decls[0])
	}
	output, handled, err := directFunctionSource(function, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected direct AST function emission")
	}
	if !strings.Contains(output, "func Add(value int) int") || !strings.Contains(output, "next := value + 1") {
		t.Fatalf("unexpected direct function output: %s", output)
	}
}

func TestDirectValueDeclarationEmissionUsesStructuredAST(t *testing.T) {
	file, err := ParseFile("direct-value.gpp", "let answer = 42\n")
	if err != nil {
		t.Fatal(err)
	}
	declaration, ok := file.Decls[0].(*ValueDecl)
	if !ok {
		t.Fatalf("expected structured value declaration, got %T", file.Decls[0])
	}
	output, handled, err := directValueDeclSource(declaration, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || output != "var answer = 42" {
		t.Fatalf("unexpected direct value declaration output: handled=%v output=%q", handled, output)
	}
}

func TestMethodSignatureKeyUsesTypedParameters(t *testing.T) {
	method := Method{
		Owner: &File{},
		Name:  "Lookup",
		ParameterAST: []ParameterNode{
			{Name: "id", Type: &PointerType{Element: &NamedType{Parts: []string{"User"}}}},
			{Name: "tags", Type: &VariadicType{Element: &NamedType{Parts: []string{"string"}}}},
		},
	}
	if got, want := methodSignatureKey(method), "Lookup/*User,...string"; got != want {
		t.Fatalf("unexpected typed method signature key: got %q want %q", got, want)
	}
}

func TestDirectValueDeclarationFallsBackWhenTypeLoweringNeedsContext(t *testing.T) {
	file, err := ParseFile("direct-value-fallback.gpp", "var fallback = value ?? \"unknown\"\n")
	if err != nil {
		t.Fatal(err)
	}
	declaration, ok := file.Decls[0].(*ValueDecl)
	if !ok {
		t.Fatalf("expected structured value declaration, got %T", file.Decls[0])
	}
	output, handled, err := directValueDeclSource(declaration, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if handled || output != "" {
		t.Fatalf("expected compatibility fallback, handled=%v output=%q", handled, output)
	}
}

func TestDirectFunctionEmissionKeepsLabelsOnGoASTPath(t *testing.T) {
	file, err := ParseFile("direct-label.gpp", `
func Loop() {
again:
    goto again
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := file.Decls[0].(*FunctionDecl)
	if !ok {
		t.Fatalf("expected function declaration, got %T", file.Decls[0])
	}
	output, handled, err := directFunctionSource(function, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(output, "again:") || !strings.Contains(output, "goto again") {
		t.Fatalf("labelled function did not use structured Go AST emission: handled=%v output=%s", handled, output)
	}
}

func TestDirectFunctionEmissionIsNotDisabledPackageWideByIntrospection(t *testing.T) {
	file, err := ParseFile("direct-introspection-context.gpp", `
func Add(value int) int {
	return value + 1
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	output, handled, err := directFunctionSource(function, constructorContext{
		Introspection: &introspectionContext{Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(output, "func Add(value int) int") {
		t.Fatalf("ordinary function was unnecessarily routed through source lowering: handled=%v output=%s", handled, output)
	}
}

func TestDirectFunctionEmissionDefersIntrospectionSelectors(t *testing.T) {
	file, err := ParseFile("direct-introspection-selector.gpp", `
func Name(value Person) string {
	return value.class.name
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	_, handled, err := directFunctionSource(function, constructorContext{
		Introspection: &introspectionContext{Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("introspection selector was emitted without its metadata lowering")
	}
}

func TestDirectFunctionEmissionLowersKnownIntrospectionSelectors(t *testing.T) {
	file, err := ParseFile("direct-introspection-known.gpp", `
class Person {
    Name string
}

func Name(value Person) string {
    return value.class.name
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[1].(*FunctionDecl)
	person := file.Decls[0].(*ClassDecl)
	output, handled, err := directFunctionSource(function, constructorContext{
		Targets: map[string]constructorTarget{
			"Person": {Class: person, InterfaceName: "GppPerson"},
		},
		Introspection: &introspectionContext{
			Classes: map[string]*ClassDecl{"Person": person},
			Enabled: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(output, "return value.GppRuntimeClass().Name") {
		t.Fatalf("known introspection selector was not lowered structurally: handled=%v output=%s", handled, output)
	}
}

func TestDirectFunctionEmissionLowersEnumSelectors(t *testing.T) {
	file, err := ParseFile("direct-enum.gpp", `
enum Status string {
    Active = "active"
    Disabled = "disabled"
}

func Name(value Status) string {
    return value.name
}

func Meta() string {
    return Status.Active.name
}
`)
	if err != nil {
		t.Fatal(err)
	}
	status := file.Decls[0].(*EnumDecl)
	context := constructorContext{Enums: map[string]*EnumDecl{"Status": status}}
	for _, index := range []int{1, 2} {
		function := file.Decls[index].(*FunctionDecl)
		output, handled, directErr := directFunctionSource(function, context)
		if directErr != nil {
			t.Fatal(directErr)
		}
		if !handled {
			t.Fatalf("enum function %s did not use direct AST emission", function.Name)
		}
		if function.Name == "Name" && !strings.Contains(output, "GppEnum_StatusName(value)") {
			t.Fatalf("enum value helper was not lowered: %s", output)
		}
		if function.Name == "Meta" && !strings.Contains(output, "GppEnum_Status_ActiveMeta.Name") {
			t.Fatalf("enum metadata selector was not lowered: %s", output)
		}
	}
}

func TestDirectFunctionEmissionPreservesEnumDefaultsAndChannelTypes(t *testing.T) {
	file, err := ParseFile("direct-enum-default.gpp", `
enum Status string {
    Active = "active"
}

func Choose(value Status) chan int {
    switch value {
    case Status.Active:
        return make(chan int, 1)
    default:
        return make(chan int)
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	status := file.Decls[0].(*EnumDecl)
	function := file.Decls[1].(*FunctionDecl)
	output, handled, err := directFunctionSource(function, constructorContext{Enums: map[string]*EnumDecl{"Status": status}})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(output, "default:") || !strings.Contains(output, "make(chan int, 1)") {
		t.Fatalf("enum default or channel type was not emitted structurally: handled=%v output=%s", handled, output)
	}
}

func TestDirectFunctionEmissionLowersRecordCalls(t *testing.T) {
	file, err := ParseFile("direct-record.gpp", `
func Make() any {
	return record(name: "Bob", age: 42)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	context := constructorContext{Records: newRecordContext()}
	output, handled, err := directFunctionSource(function, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || strings.Contains(output, "record(") || !strings.Contains(output, "__gpp_record_") {
		t.Fatalf("record call was not lowered structurally: handled=%v output=%s", handled, output)
	}
}

func TestDirectFunctionEmissionLowersAnonymousStructCollections(t *testing.T) {
	file, err := ParseFile("direct-anonymous-struct.gpp", `
func Configs() any {
    return []struct { Name string; Values []any }{{Name: "demo", Values: []any{}}}
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	output, handled, err := directFunctionSource(function, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || strings.Contains(output, "TokenExpr") || !strings.Contains(output, "[]struct {") {
		t.Fatalf("anonymous struct collection was not lowered structurally: handled=%v output=%s", handled, output)
	}
}

func TestDirectFunctionEmissionSupportsArrayTypesAndSpreadCalls(t *testing.T) {
	file, err := ParseFile("direct-array-spread.gpp", `
func Collect(values ...int) int { return len(values) }

func Build(values []int) int {
    var fixed [2]int
    return Collect(fixed[:]...)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := file.Decls[1].(*FunctionDecl)
	if !ok {
		t.Fatalf("expected structured function, got %T", file.Decls[1])
	}
	output, handled, err := directFunctionSource(function, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(output, "var fixed [2]int") || !strings.Contains(output, "Collect(fixed[:]...)") {
		t.Fatalf("array or spread call was not lowered structurally: handled=%v output=%s", handled, output)
	}
}

func TestDirectFunctionEmissionLowersInitializerAssignments(t *testing.T) {
	file, err := ParseFile("direct-initializers.gpp", `
func Sum(limit int) int {
    total := 0
    for index := 0; index < limit; index++ {
        total += index
    }
    if value := total; value > 0 {
        return value
    }
    return 0
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	output, handled, err := directFunctionSource(function, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(output, "for index := 0;") || !strings.Contains(output, "if value := total;") {
		t.Fatalf("initializer assignments were not lowered structurally: handled=%v output=%s", handled, output)
	}
}

func TestDirectFunctionEmissionLowersSelectToSelectAST(t *testing.T) {
	file, err := ParseFile("direct-select.gpp", `
func Receive(ch chan int) int {
	select {
	case value := <-ch:
		return value
	default:
		return 0
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	output, handled, err := directFunctionSource(function, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(output, "select {") || strings.Contains(output, "switch {") {
		t.Fatalf("select was not emitted as a select statement: handled=%v output=%s", handled, output)
	}
}

func TestDirectFunctionEmissionLowersExceptionBodyToGoAST(t *testing.T) {
	file, err := ParseFile("direct-exception.gpp", `
import "errors"

func Run(shouldReturn bool) int {
    try {
        if shouldReturn {
            return 7
        }
        throw errors.New("boom")
    } catch error {
        return 9
    } finally {
        println("cleanup")
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := file.Decls[1].(*FunctionDecl)
	if !ok {
		t.Fatalf("expected function declaration, got %T", file.Decls[1])
	}
	output, handled, err := directFunctionSource(function, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected direct AST exception emission")
	}
	if strings.Contains(output, "try") || strings.Contains(output, "catch") || !strings.Contains(output, "__gppExceptionReturn") {
		t.Fatalf("exception body was not lowered structurally: %s", output)
	}
}

func TestDirectVoidExceptionBoundary(t *testing.T) {
	file, err := ParseFile("direct-void-exception.gpp", `
func Run(shouldReturn bool) {
    try {
        if shouldReturn {
            return
        }
    } finally {
        println("cleanup")
    }
    println("after")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	output, handled, err := directFunctionSource(function, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected direct AST void exception emission")
	}
	if !strings.Contains(output, "defer func()") || strings.Contains(output, "try") {
		t.Fatalf("void exception boundary was not lowered structurally: %s", output)
	}
}

func TestDirectFunctionEmissionLowersCoalescingFromAST(t *testing.T) {
	file, err := ParseFile("direct-coalesce.gpp", `
func ParsePort(value string) (int, error) { return 0, nil }

func Port(value string) int {
    return ParsePort(value) ?? 8080
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := file.Decls[1].(*FunctionDecl)
	if !ok {
		t.Fatalf("expected function declaration, got %T", file.Decls[1])
	}
	context := constructorContext{FunctionSignatures: map[string][]callableSignature{
		"ParsePort": {{
			Parameters: []parameterInfo{{Name: "value", TypeAST: parseTypeText("string")}},
			ResultAST:  parseTypeText("(int, error)"),
		}},
	}}
	output, handled, err := directFunctionSource(function, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected direct AST coalescing emission")
	}
	if strings.Contains(output, "??") || !strings.Contains(output, "__gppCoalesce[int]") {
		t.Fatalf("coalescing was not lowered structurally: %s", output)
	}
}

func TestDirectFunctionEmissionLowersConstructorFromAST(t *testing.T) {
	person := &ClassDecl{
		Name: "Person",
		Fields: []Field{
			{Name: "Name", TypeAST: parseTypeText("string")},
			{Name: "Age", TypeAST: parseTypeText("int")},
		},
	}
	file, err := ParseFile("direct-constructor.gpp", `
func MakePerson() Person {
    return Person("Bob", 42)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	context := constructorContext{Targets: map[string]constructorTarget{
		"Person": {Class: person, Classes: map[string]*ClassDecl{"Person": person}},
	}}
	output, handled, err := directFunctionSource(function, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected direct AST constructor emission")
	}
	if strings.Contains(output, "Person(\"Bob\", 42)") || !strings.Contains(output, "Person{Name: \"Bob\", Age: 42}") {
		t.Fatalf("constructor was not lowered structurally: %s", output)
	}
}

func TestDirectFunctionEmissionLowersInterpolationFromAST(t *testing.T) {
	file, err := ParseFile("direct-interpolation.gpp", `
func Greeting(name string) string {
    return "Hello {{name}}"
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	context := constructorContext{InterpolationName: "fmt"}
	output, handled, err := directFunctionSource(function, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected direct AST interpolation emission")
	}
	if !strings.Contains(output, `fmt.Sprintf("Hello %v", name)`) {
		t.Fatalf("interpolation was not lowered structurally: %s", output)
	}
}

func TestDirectFunctionEmissionLowersContextualLambdaFromAST(t *testing.T) {
	file, err := ParseFile("direct-lambda.gpp", `
func Apply(value int, transform func(int) int) int { return transform(value) }

func Double(value int) int {
    return Apply(value, item => item * 2)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[1].(*FunctionDecl)
	context := constructorContext{FunctionSignatures: map[string][]callableSignature{
		"Apply": {{
			Parameters: []parameterInfo{
				{Name: "value", TypeAST: parseTypeText("int")},
				{Name: "transform", TypeAST: parseTypeText("func(int) int")},
			},
			ResultAST: parseTypeText("int"),
		}},
	}}
	output, handled, err := directFunctionSource(function, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected direct AST lambda emission")
	}
	if !strings.Contains(output, "func(item int) int") || strings.Contains(output, "=>") {
		t.Fatalf("lambda was not lowered structurally: %s", output)
	}
}

func TestDirectFunctionEmissionLowersNamedAndDefaultArgumentsFromAST(t *testing.T) {
	file, err := ParseFile("direct-call-arguments.gpp", `
func Greeting(name string, punctuation string = "!") string { return name + punctuation }

func Message() string {
    return Greeting(punctuation: "?", name: "Ada")
}

`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[1].(*FunctionDecl)
	context := constructorContext{FunctionSignatures: map[string][]callableSignature{
		"Greeting": {{
			Parameters: []parameterInfo{
				{Name: "name", TypeAST: parseTypeText("string")},
				{Name: "punctuation", TypeAST: parseTypeText("string"), HasDefault: true, DefaultAST: &LiteralExpr{Text: `"!"`, Kind: TokenString}},
			},
			ResultAST: parseTypeText("string"),
		}},
	}}
	output, handled, err := directFunctionSource(function, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected direct AST call argument lowering")
	}
	if !strings.Contains(output, `Greeting("Ada", "?")`) || strings.Contains(output, "punctuation:") {
		t.Fatalf("named/default arguments were not lowered structurally: %s", output)
	}
}

func TestDirectFunctionEmissionLowersSafeAccessFromAST(t *testing.T) {
	person := &ClassDecl{
		Name:   "Person",
		Fields: []Field{{Name: "Name", TypeAST: parseTypeText("string")}},
	}
	file, err := ParseFile("direct-safe-access.gpp", `
class Person {
    Name string
}

func Read(person *Person) string {
    return person?.Name
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := file.Decls[1].(*FunctionDecl)
	if !ok {
		t.Fatalf("expected function declaration, got %T", file.Decls[1])
	}
	context := constructorContext{Targets: map[string]constructorTarget{
		"Person": {Class: person, Classes: map[string]*ClassDecl{"Person": person}},
	}}
	output, handled, err := directFunctionSource(function, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected direct AST safe-access emission")
	}
	if !strings.Contains(output, "__gpp_safe(person == nil") || !strings.Contains(output, "return person.Name") || strings.Contains(output, "?.") {
		t.Fatalf("safe access was not lowered structurally: %s", output)
	}
}

func TestDirectExtensionEmissionUsesStructuredBody(t *testing.T) {
	file, err := ParseFile("direct-extension.gpp", `
extend string {
    func Empty() bool {
        return len(this) == 0
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	extension, ok := file.Decls[0].(*ExtendDecl)
	if !ok || len(extension.Methods) != 1 {
		t.Fatalf("expected one extension method, got %#v", file.Decls[0])
	}
	method := extension.Methods[0]
	output, handled, err := directExtensionMethodSource(extensionMethod{
		GoName:       "__gpp_ext_string_Empty",
		ReceiverType: "string",
		Method:       method,
	}, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected direct AST extension emission")
	}
	if !strings.Contains(output, "func __gpp_ext_string_Empty(this string)") || !strings.Contains(output, "return len(this) == 0") {
		t.Fatalf("extension body was not emitted from AST: %s", output)
	}
}

func TestDirectFunctionEmissionLowersOverloadCallsFromAST(t *testing.T) {
	call := &CallExpr{
		Callee:    &NameExpr{Name: "Choose"},
		Arguments: []CallArg{{Value: &LiteralExpr{Text: "1", Kind: TokenNumber}}},
	}
	context := constructorContext{Overloads: overloadContext{
		Functions: map[string]map[int]string{"Choose": {1: "Choose__gpp_1"}},
		FunctionTypes: map[string]map[string]string{
			"Choose": {"int": "Choose__gpp_1_int"},
		},
	}}
	lowered, handled, err := lowerOverloadCallNode(call, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected AST overload lowering")
	}
	loweredCall, ok := lowered.(*CallExpr)
	if !ok {
		t.Fatalf("expected lowered call, got %T", lowered)
	}
	callee, ok := loweredCall.Callee.(*NameExpr)
	if !ok || callee.Name != "Choose__gpp_1_int" {
		t.Fatalf("overload callee was not lowered structurally: %#v", loweredCall.Callee)
	}
}
