package compiler

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"strings"
	"testing"
)

func TestTransformExceptionsUsesBodyAST(t *testing.T) {
	source := `try {
    throw errors.New("boom")
} catch {
    return
}`
	transformed, handled, err := transformExceptionRegionAST(source, constructorContext{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected the structured body AST to handle try/catch")
	}
	if strings.Contains(transformed, "try") || strings.Contains(transformed, "catch") {
		t.Fatalf("exception syntax remained after AST lowering: %s", transformed)
	}
	if !strings.Contains(transformed, "__gppRun(func()") || !strings.Contains(transformed, "__gppThrow(errors.New(\"boom\"))") {
		t.Fatalf("unexpected AST exception lowering: %s", transformed)
	}
}

func TestTransformExceptionsLowersSimpleBodiesDirectlyToGoAST(t *testing.T) {
	source := `try {
    throw err
} catch error e {}
`
	transformed, handled, err := transformExceptionRegionAST(source, constructorContext{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(transformed, "__gppRun(func()") {
		t.Fatalf("expected direct AST exception lowering, handled=%v output=%s", handled, transformed)
	}
}

func TestTransformExceptionsDirectlyLowersOrdinaryGoStatements(t *testing.T) {
	source := `try {
    println("before")
    value := 1
    value = value + 1
} catch error {}
`
	transformed, handled, err := transformExceptionRegionAST(source, constructorContext{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(transformed, "value := 1") || !strings.Contains(transformed, "value = value + 1") {
		t.Fatalf("expected ordinary statements to lower through Go AST, handled=%v output=%s", handled, transformed)
	}
}

func TestTransformExceptionsDirectlyLowersNestedTryAST(t *testing.T) {
	source := `try {
    try {
        throw err
    } catch error inner {}
} catch error outer {}
`
	transformed, handled, err := transformExceptionRegionAST(source, constructorContext{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !handled || strings.Count(transformed, "__gppRun(func()") != 2 {
		t.Fatalf("expected nested try blocks to lower through AST, handled=%v output=%s", handled, transformed)
	}
}

func TestTransformExceptionsCompatibilityFallbackStaysNodeBased(t *testing.T) {
	tokens, err := LexSource("compatibility-exception.gpp", `try {
	throw err
} catch error caught {
	println(caught)
}`)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ParseBodyAST(tokens)
	if err != nil {
		t.Fatal(err)
	}
	tryStatement, ok := block.Statements[0].(*TryStmt)
	if !ok {
		t.Fatalf("expected try statement, got %T", block.Statements[0])
	}
	// Introspection disables the direct-emission fast path. The fallback must
	// still lower the already-parsed tree without requiring a source slice to
	// be lexed and parsed again.
	context := constructorContext{Introspection: &introspectionContext{Enabled: true}}
	lowered, err := lowerASTTryCompatibilityNode(tryStatement, context, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lowered.(*ast.ExprStmt); !ok {
		t.Fatalf("expected Go AST exception wrapper, got %T", lowered)
	}
}

func TestTransformExceptionsDirectlyLowersCoalesceAST(t *testing.T) {
	context := constructorContext{
		FunctionSignatures: map[string][]callableSignature{
			"ParseFloat": {{
				Name: "ParseFloat",
				Parameters: []parameterInfo{
					{Name: "value", TypeAST: parseTypeText("string")},
					{Name: "bitSize", TypeAST: parseTypeText("int")},
				},
				ResultAST: &TupleType{Elements: []TypeNode{
					parseTypeText("float64"),
					parseTypeText("error"),
				}},
			}},
		},
	}
	source := `try {
    value := ParseFloat("1.5", 64) ?? -1.0
} catch error {}
`
	transformed, handled, err := transformExceptionRegionAST(source, context, "")
	if err != nil {
		t.Fatal(err)
	}
	if !handled || strings.Contains(transformed, "??") || !strings.Contains(transformed, "__gppCoalesce[float64]") {
		t.Fatalf("expected coalescing to lower through AST, handled=%v output=%s", handled, transformed)
	}
}

func TestPromotedCallRecognizesSelectorExtensionWithoutReceiverArgument(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `pattern.CompileRegex()`))
	if err != nil {
		t.Fatal(err)
	}
	call, ok := expression.(*CallExpr)
	if !ok {
		t.Fatalf("expected call expression, got %T", expression)
	}
	context := constructorContext{
		CurrentParameterTypes: map[string]string{"pattern": "string"},
		Extensions: []extensionMethod{{
			Target:       "string",
			ReceiverType: "string",
			Method: Method{
				Name:      "CompileRegex",
				ResultAST: &TupleType{Elements: []TypeNode{parseTypeText("*regexp.Regexp"), parseTypeText("error")}},
			},
		}},
	}
	result, found := promotedCallForExpr(call, context, context.CurrentParameterTypes)
	if !found || !result.trailingError || len(result.types) != 2 {
		t.Fatalf("selector extension was not recognized for implicit promotion: found=%v result=%#v", found, result)
	}
}

func TestLowerTryUsesGoASTWrapper(t *testing.T) {
	catchTokens, err := LexSource("catch-body", `println(err)`)
	if err != nil {
		t.Fatal(err)
	}
	catchBodyAST, err := ParseBodyAST(catchTokens)
	if err != nil {
		t.Fatal(err)
	}
	catchBody, err := lowerExceptionGoBlockNode(catchBodyAST, constructorContext{}, "__gppRecovered")
	if err != nil {
		t.Fatal(err)
	}
	tryTokens, err := LexSource("try-body", `__gppThrow(errors.New("boom"))`)
	if err != nil {
		t.Fatal(err)
	}
	tryBodyAST, err := ParseBodyAST(tryTokens)
	if err != nil {
		t.Fatal(err)
	}
	tryBody, err := lowerExceptionGoBlockNode(tryBodyAST, constructorContext{}, "")
	if err != nil {
		t.Fatal(err)
	}
	finallyTokens, err := LexSource("finally-body", `println("cleanup")`)
	if err != nil {
		t.Fatal(err)
	}
	finallyAST, err := ParseBodyAST(finallyTokens)
	if err != nil {
		t.Fatal(err)
	}
	finallyBody, err := lowerExceptionGoBlockNode(finallyAST, constructorContext{}, "")
	if err != nil {
		t.Fatal(err)
	}
	run, err := lowerTryASTNode(
		tryBody,
		[]catchClause{{typeNodes: []TypeNode{parseTypeText("error")}, variable: "err", body: catchBody}},
		finallyBody,
		true,
		constructorContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var generatedBuffer bytes.Buffer
	if err := format.Node(&generatedBuffer, token.NewFileSet(), run); err != nil {
		t.Fatal(err)
	}
	generated := generatedBuffer.String()
	for _, fragment := range []string{
		"__gppRun(func()",
		"__gppRecovered := recover()",
		"switch __gppCaught := __gppThrown.err.(type)",
		"defer func()",
	} {
		if !strings.Contains(generated, fragment) {
			t.Fatalf("AST exception wrapper omitted %q: %s", fragment, generated)
		}
	}
}

func TestExceptionReturnRewritingUsesBodyAST(t *testing.T) {
	transformed, handled, err := rewriteExceptionReturnsAST(`
	return value
`)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(transformed, "__gppExceptionReturn{values: []any{value}}") {
		t.Fatalf("return was not rewritten from the body AST: handled=%v output=%s", handled, transformed)
	}
}

func TestFinallyControlTransfersUseBodyAST(t *testing.T) {
	returnTokens, err := LexSource("finally-return", "return\n")
	if err != nil {
		t.Fatal(err)
	}
	returnBlock, err := ParseBodyAST(returnTokens)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFinallyControlTransfersBlock(returnBlock); err == nil || !strings.Contains(err.Error(), "return") {
		t.Fatalf("expected AST finally validation error, err=%v", err)
	}
	cleanupTokens, err := LexSource("finally-cleanup", "cleanup()\n")
	if err != nil {
		t.Fatal(err)
	}
	cleanupBlock, err := ParseBodyAST(cleanupTokens)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFinallyControlTransfersBlock(cleanupBlock); err != nil {
		t.Fatalf("unexpected AST finally validation result: %v", err)
	}
}
