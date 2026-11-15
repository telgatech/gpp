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

func TestTransformCallableCallsLowersImportedFunctionDefaults(t *testing.T) {
	context := constructorContext{
		ImportedFunctionSignatures: map[string]map[string][]callableSignature{
			"queue": {
				"New": {{
					Name: "New",
					Parameters: []parameterInfo{
						{Name: "file", TypeAST: parseTypeText("string"), DefaultAST: &LiteralExpr{Text: `"app.queue"`, Kind: TokenString}, HasDefault: true},
						{Name: "retries", TypeAST: parseTypeText("int"), DefaultAST: &LiteralExpr{Text: "3", Kind: TokenNumber}, HasDefault: true},
					},
				}},
			},
		},
	}

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "all defaults omitted",
			source: `func main() { q := queue.New(); _ = q }`,
			want:   `queue.New("app.queue", 3)`,
		},
		{
			name:   "trailing default omitted",
			source: `func main() { q := queue.New(file: "local.queue"); _ = q }`,
			want:   `queue.New("local.queue", 3)`,
		},
		{
			name:   "leading default omitted",
			source: `func main() { q := queue.New(retries: 5); _ = q }`,
			want:   `queue.New("app.queue", 5)`,
		},
		{
			name:   "all arguments supplied by name",
			source: `func main() { q := queue.New(retries: 5, file: "local.queue"); _ = q }`,
			want:   `queue.New("local.queue", 5)`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transformed, handled, err := transformCallableCallsAST(test.source, context)
			if err != nil {
				t.Fatal(err)
			}
			if !handled || !strings.Contains(transformed, test.want) {
				t.Fatalf("imported function defaults were not lowered: handled=%v source=%s; want %s", handled, transformed, test.want)
			}
		})
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
