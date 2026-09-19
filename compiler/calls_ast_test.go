package compiler

import (
	"strings"
	"testing"
)

func TestTransformCallableCallsUsesBodyAST(t *testing.T) {
	context := constructorContext{
		FunctionSignatures: map[string][]callableSignature{
			"greet": {{
				Name: "greet",
				Parameters: []parameterInfo{{
					Name:          "name",
					TypeAST:       parseTypeText("string"),
					DefaultAST:    &LiteralExpr{Text: `"world"`, Kind: TokenString},
					DefaultTokens: []Token{{Kind: TokenString, Text: `"world"`}},
					HasDefault:    true,
				}},
			}},
		},
	}

	transformed, handled, err := transformCallableCallsAST("return greet()", context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatal("expected the structured body AST to handle default call arguments")
	}
	if !strings.Contains(transformed, `greet("world")`) {
		t.Fatalf("default argument was not inserted: %s", transformed)
	}
}

func TestTransformCallableCallsUsesTopLevelFunctionBodyAST(t *testing.T) {
	context := constructorContext{
		Targets: map[string]constructorTarget{"User": {}},
		StaticMethodSignatures: map[string]map[string][]callableSignature{
			"User": {
				"Guest": {{Name: "Guest", GoName: "GppStatic_User_Guest"}},
			},
		},
	}

	transformed, handled, err := transformCallableCallsAST(`func main() {
	guest := User.Guest()
	_ = guest
}`, context)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(transformed, `guest := GppStatic_User_Guest()`) {
		t.Fatalf("top-level static call was not lowered from its body AST: handled=%v source=%s", handled, transformed)
	}
}
