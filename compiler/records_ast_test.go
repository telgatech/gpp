package compiler

import (
	"strings"
	"testing"
)

func TestCollectRecordValueTypesUsesBodyAST(t *testing.T) {
	typeName := "__gpp_record_deadbeef"
	source := `func Build(input ` + typeName + `) {
    current := input
    if current != nil {
        nested := current
        _ = nested
    }
}`

	valueTypes, handled := collectRecordValueTypesAST(source, constructorContext{})
	if !handled {
		t.Fatal("expected record value collection to use the structured body AST")
	}
	for _, name := range []string{"input", "current", "nested"} {
		if valueTypes[name] != typeName {
			t.Fatalf("expected %s to have type %s, got %q", name, typeName, valueTypes[name])
		}
	}
}

func TestCollectMapExpectedTypesFromTypedDeclaration(t *testing.T) {
	source := `func main() {
    var empty map[string]int = map()
}`
	valueTypes := collectRecordValueTypes(source, constructorContext{})
	expected := collectMapExpectedTypes(source, valueTypes, constructorContext{})
	if len(expected) != 1 {
		t.Fatalf("expected typed map declaration to provide an empty map literal type, got %#v", expected)
	}
	for _, typeName := range expected {
		if typeName != "map[string]int" {
			t.Fatalf("expected map[string]int context, got %q", typeName)
		}
	}
}

func TestCollectMapExpectedTypesFromFunctionArguments(t *testing.T) {
	source := `func consume(values map[string]any) {}
func main() {
    consume(map(name: "Ada", level: 3))
}`
	parameterType := &MapType{
		Key:   &NamedType{Parts: []string{"string"}},
		Value: &NamedType{Parts: []string{"any"}},
	}
	context := constructorContext{FunctionSignatures: map[string][]callableSignature{
		"consume": {{Name: "consume", Parameters: []parameterInfo{{Name: "values", TypeAST: parameterType}}}},
	}}
	valueTypes := collectRecordValueTypes(source, context)
	expected := collectMapExpectedTypes(source, valueTypes, context)
	if len(expected) != 1 {
		t.Fatalf("expected function parameter to provide a map literal type, got %#v", expected)
	}
	for _, typeName := range expected {
		if typeName != "map[string]any" {
			t.Fatalf("expected map[string]any context, got %q", typeName)
		}
	}
	transformed, err := transformMapLiterals(source, valueTypes, context, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(transformed, `consume(map[string]any{"name": "Ada", "level": 3})`) {
		t.Fatalf("function argument did not use its expected map type:\n%s", transformed)
	}
}

func TestTransformNestedEmptyMapWithExpectedType(t *testing.T) {
	source := `func main() {
    var groups map[string]map[string]int = map(team: map())
}`
	transformed, err := transformMapLiterals(source, map[string]string{}, constructorContext{}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := `map[string]map[string]int{"team": map[string]int{}}`
	if !strings.Contains(transformed, want) {
		t.Fatalf("nested empty map did not inherit its expected type %q:\n%s", want, transformed)
	}
}

func TestTransformMapLiteralInfersStrongAndMixedValueTypes(t *testing.T) {
	source := `func main() {
    prices := map(coffee: 1.23)
    form := map(name: "Ada", level: 3)
}`
	transformed, err := transformMapLiterals(source, map[string]string{}, constructorContext{}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`map[string]float64{"coffee": 1.23}`,
		`map[string]any{"name": "Ada", "level": 3}`,
	} {
		if !strings.Contains(transformed, expected) {
			t.Errorf("map literal did not lower to %q:\n%s", expected, transformed)
		}
	}
}

func TestCollectRecordValueTypesUsesImportedFunctionResults(t *testing.T) {
	source := `func main() {
    keyID := common.GetEnv("KEY_ID", "")
    _ = record(key: keyID)
}`
	signature := callableSignature{
		Name:      "GetEnv",
		ResultAST: &NamedType{Parts: []string{"string"}},
	}
	context := constructorContext{
		ImportedFunctionSignatures: map[string]map[string][]callableSignature{
			"common": {"GetEnv": {signature}},
		},
	}
	valueTypes, handled := collectRecordValueTypesAST(source, context)
	if !handled {
		t.Fatal("expected structured record value collection")
	}
	if valueTypes["keyID"] != "string" {
		t.Fatalf("expected imported GetEnv result to infer keyID as string, got %q", valueTypes["keyID"])
	}
}

func TestCollectRecordValueTypesPreservesDeclaredTypeAcrossAssignment(t *testing.T) {
	source := `func payment() {
    amount := int64(9900)
    amount = 4900
    _ = record(amount: amount)
}`

	valueTypes, handled := collectRecordValueTypesAST(source, constructorContext{})
	if !handled {
		t.Fatal("expected structured record value collection")
	}
	if got := valueTypes["amount"]; got != "int64" {
		t.Fatalf("assignment changed amount's inferred type: got %q, want int64", got)
	}
}

func TestCollectRecordValueTypesUnwrapsTryHandledTupleAssignment(t *testing.T) {
	source := `func main() {
    var user *SessionUser
    try {
        user = requireUser()
    } catch err {}
    _ = record(name: user.Name)
}`
	context := constructorContext{
		Targets: map[string]constructorTarget{
			"SessionUser": {Class: &ClassDecl{
				Name:   "SessionUser",
				Fields: []Field{{Name: "Name", TypeAST: &NamedType{Parts: []string{"string"}}}},
			}},
		},
		FunctionSignatures: map[string][]callableSignature{
			"requireUser": {{
				Name: "requireUser",
				ResultAST: &TupleType{Elements: []TypeNode{
					&PointerType{Element: &NamedType{Parts: []string{"SessionUser"}}},
					&NamedType{Parts: []string{"error"}},
				}},
			}},
		},
	}
	valueTypes, handled := collectRecordValueTypesAST(source, context)
	if !handled {
		t.Fatal("expected structured record value collection")
	}
	if got := valueTypes["user"]; got != "*SessionUser" {
		t.Fatalf("expected try assignment to bind the first call result, got %q", got)
	}
	tokens, err := LexSource("record-selector.gpp", "user.Name")
	if err != nil {
		t.Fatal(err)
	}
	expression, err := ParseExpressionTokens(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if got := inferRecordExprType(expression, valueTypes, context); got != "string" {
		t.Fatalf("expected SessionUser.Name to infer as string, got %q", got)
	}
}

func TestRecordCallInferenceErrorIncludesValueExpression(t *testing.T) {
	tokens, err := LexSource("record-error.gpp", `record(key: keyID)`)
	if err != nil {
		t.Fatal(err)
	}
	expression, err := ParseExpressionTokens(tokens)
	if err != nil {
		t.Fatal(err)
	}
	call, ok := expression.(*CallExpr)
	if !ok {
		t.Fatalf("expected record call, got %T", expression)
	}
	_, _, err = lowerRecordCallExprNode(call, constructorContext{Records: newRecordContext()})
	if err == nil || err.Error() != "1:13: cannot infer type of record field key from keyID" {
		t.Fatalf("expected source-located field and value diagnostic, got %v", err)
	}
}

func TestEmitInfersRecordFieldTypeFromClassSelector(t *testing.T) {
	file, err := ParseFile("record-selector.gpp", `class SessionUser {
    Name string
}

func main() {
    var user *SessionUser
    _ = record(name: user.Name)
}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(file); err != nil {
		t.Fatalf("expected user.Name to infer from SessionUser.Name: %v", err)
	}
}

func TestRewriteRecordCollectionLiteralsUsesBodyAST(t *testing.T) {
	source := `func Users() []record {
    return []record{__gpp_record_deadbeef{Name: "A"}}
}`
	transformed, handled, err := rewriteRecordCollectionLiteralsAST(source, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(transformed, "[]__gpp_record_deadbeef{") {
		t.Fatalf("expected structured collection type lowering, handled=%v output=%s", handled, transformed)
	}
}
