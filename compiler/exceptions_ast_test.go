package compiler

import (
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

func TestLowerTryUsesGoASTWrapper(t *testing.T) {
	catchBody, err := parseLoweredBlock(`println(err)`)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := lowerTryAST(
		`__gppThrow(errors.New("boom"))`,
		[]catchClause{{typeNodes: []TypeNode{parseTypeText("error")}, variable: "err", body: catchBody}},
		`println("cleanup")`,
		constructorContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
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
	if err, handled := validateFinallyControlTransfersAST("return\n"); !handled || err == nil || !strings.Contains(err.Error(), "return") {
		t.Fatalf("expected AST finally validation error, handled=%v err=%v", handled, err)
	}
	if err, handled := validateFinallyControlTransfersAST("cleanup()\n"); !handled || err != nil {
		t.Fatalf("unexpected AST finally validation result, handled=%v err=%v", handled, err)
	}
}
