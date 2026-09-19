package compiler

import (
	"strings"
	"testing"
)

func TestTransformLambdasUsesBodyAST(t *testing.T) {
	source := `var predicate func(int) bool
predicate = value => value > 0`

	transformed, handled, err := transformLambdasAST(source, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected the structured body AST to handle lambda lowering")
	}
	if strings.Contains(transformed, "=>") {
		t.Fatalf("lambda operator remained after AST lowering: %s", transformed)
	}
	if !strings.Contains(transformed, "func(value int) bool") {
		t.Fatalf("unexpected AST lambda lowering: %s", transformed)
	}
}

func TestTransformLambdasUsesASTForNestedLambdas(t *testing.T) {
	source := `var build func(int) func(int) int
build = x => (y int) => x + y`

	transformed, handled, err := transformLambdasAST(source, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected nested lambdas to be handled by the structured AST")
	}
	if strings.Contains(transformed, "=>") {
		t.Fatalf("nested lambda operator remained after AST lowering: %s", transformed)
	}
	if !strings.Contains(transformed, "func(x int) func(int) int") ||
		(!strings.Contains(transformed, "func(y int) int") && !strings.Contains(transformed, "func(y int)int")) {
		t.Fatalf("unexpected nested lambda lowering: %s", transformed)
	}
}
