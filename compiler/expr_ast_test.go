package compiler

import (
	"strings"
	"testing"
)

func expressionTokens(t *testing.T, source string) []Token {
	t.Helper()
	tokens, err := LexSource("expression.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func TestParseExpressionBuildsGoPlusNodes(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `user?.Name.TrimSpace() ?? "unknown"`))
	if err != nil {
		t.Fatal(err)
	}
	coalesce, ok := expression.(*BinaryExpr)
	if !ok || coalesce.Operator != "??" {
		t.Fatalf("expected coalescing expression, got %#v", expression)
	}
	call, ok := coalesce.Left.(*CallExpr)
	if !ok || len(call.Arguments) != 0 {
		t.Fatalf("expected zero-argument call on left side, got %#v", coalesce.Left)
	}
	selector, ok := call.Callee.(*SelectorExpr)
	if !ok || selector.Name != "TrimSpace" {
		t.Fatalf("expected TrimSpace selector, got %#v", call.Callee)
	}
	safeSelector, ok := selector.Receiver.(*SelectorExpr)
	if !ok || !safeSelector.Safe || safeSelector.Name != "Name" {
		t.Fatalf("expected safe Name selector, got %#v", selector.Receiver)
	}
}

func TestParseExpressionBuildsNamedArguments(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `Person(Name: "Bob", Age: 42)`))
	if err != nil {
		t.Fatal(err)
	}
	call, ok := expression.(*CallExpr)
	if !ok || len(call.Arguments) != 2 {
		t.Fatalf("expected named-argument call, got %#v", expression)
	}
	if call.Arguments[0].Name != "Name" || call.Arguments[1].Name != "Age" {
		t.Fatalf("unexpected argument names: %#v", call.Arguments)
	}
}

func TestParseExpressionBuildsQualifiedNamedArguments(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `Person(Parent.Name: "Bob")`))
	if err != nil {
		t.Fatal(err)
	}
	call, ok := expression.(*CallExpr)
	if !ok || len(call.Arguments) != 1 {
		t.Fatalf("expected one qualified named argument, got %#v", expression)
	}
	if call.Arguments[0].Name != "Parent.Name" {
		t.Fatalf("unexpected qualified argument name %q", call.Arguments[0].Name)
	}
}

func TestParseExpressionBuildsVariadicExpansion(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `append(values, extra...)`))
	if err != nil {
		t.Fatal(err)
	}
	call, ok := expression.(*CallExpr)
	if !ok || len(call.Arguments) != 2 {
		t.Fatalf("expected variadic call, got %#v", expression)
	}
	spread, ok := call.Arguments[1].Value.(*SpreadExpr)
	if !ok || spread.Expression == nil {
		t.Fatalf("expected explicit variadic expansion, got %#v", call.Arguments[1].Value)
	}
	printed, err := expressionNodeSource(expression)
	if err != nil || printed != "append(values, extra...)" {
		t.Fatalf("unexpected variadic source: %q (err=%v)", printed, err)
	}
}

func TestParseExpressionBuildsTypeArguments(t *testing.T) {
	for _, source := range []string{`make(chan int)`, `make([]int, 0)`, `make(map[string]int)`} {
		t.Run(source, func(t *testing.T) {
			expression, err := ParseExpressionTokens(expressionTokens(t, source))
			if err != nil {
				t.Fatal(err)
			}
			call, ok := expression.(*CallExpr)
			if !ok || len(call.Arguments) == 0 {
				t.Fatalf("expected call with type argument, got %#v", expression)
			}
			if _, ok := call.Arguments[0].Value.(*TypeExpr); !ok {
				t.Fatalf("expected explicit type expression, got %#v", call.Arguments[0].Value)
			}
		})
	}
}

func TestParseExpressionBuildsGenericIndexList(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `Factory[String, int](value)`))
	if err != nil {
		t.Fatal(err)
	}
	call, ok := expression.(*CallExpr)
	if !ok {
		t.Fatalf("expected generic call, got %#v", expression)
	}
	indices, ok := call.Callee.(*IndexListExpr)
	if !ok || len(indices.Indices) != 2 {
		t.Fatalf("expected explicit generic index list, got %#v", call.Callee)
	}
	printed, err := expressionNodeSource(expression)
	if err != nil || printed != "Factory[String, int](value)" {
		t.Fatalf("generic call did not round-trip: %q (%v)", printed, err)
	}
}

func TestParseExpressionBuildsChannelSend(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `channel <- value`))
	if err != nil {
		t.Fatal(err)
	}
	send, ok := expression.(*SendExpr)
	if !ok || send.Channel == nil || send.Value == nil {
		t.Fatalf("expected explicit send expression, got %#v", expression)
	}
	if source, err := expressionNodeSource(send); err != nil || source != "channel <- value" {
		t.Fatalf("send expression did not round-trip: %q (%v)", source, err)
	}
}

func TestBodyStatementsExposeExpressions(t *testing.T) {
	file, err := ParseFile("expression-body.gpp", `
class User {
    func NameOrUnknown() string {
        value := this?.Name ?? "unknown"
        return value.TrimSpace()
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	method := file.Decls[0].(*ClassDecl).Methods[0]
	statements := method.BodyAST.Statements
	if len(statements) != 2 {
		t.Fatalf("expected two statements, got %#v", statements)
	}
	declaration := statements[0].(*DeclarationStmt)
	if len(declaration.Values) != 1 {
		t.Fatalf("expected declaration initializer expression, got %#v", declaration.Values)
	}
	if _, ok := declaration.Values[0].(*BinaryExpr); !ok {
		t.Fatalf("expected typed coalescing initializer, got %#v", declaration.Values[0])
	}
	returnStatement := statements[1].(*ReturnStmt)
	if len(returnStatement.Values) != 1 {
		t.Fatalf("expected return expression, got %#v", returnStatement.Values)
	}
	if _, ok := returnStatement.Values[0].(*CallExpr); !ok {
		t.Fatalf("expected typed return call, got %#v", returnStatement.Values[0])
	}
}

func TestUnsupportedExpressionRemainsTokenStructured(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `value ? "yes" : "no"`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := expression.(*TokenExpr); !ok {
		t.Fatalf("expected transitional token expression, got %#v", expression)
	}
}

func TestGoExpressionFallbackBuildsTypedNodes(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `&struct { Name string }{Name: "Ada"}`))
	if err != nil {
		t.Fatal(err)
	}
	unary, ok := expression.(*UnaryExpr)
	if !ok || unary.Operator != "&" {
		t.Fatalf("expected typed unary expression, got %#v", expression)
	}
	literal, ok := unary.Operand.(*CompositeLiteralExpr)
	if !ok || len(literal.Elements) != 1 {
		t.Fatalf("expected typed composite literal, got %#v", unary.Operand)
	}
	if _, ok := literal.Type.(*StructType); !ok {
		t.Fatalf("expected anonymous struct type, got %#v", literal.Type)
	}
}

func TestParseExpressionBuildsSliceTypeConversion(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `[]byte(value)`))
	if err != nil {
		t.Fatal(err)
	}
	call, ok := expression.(*CallExpr)
	if !ok {
		t.Fatalf("expected conversion call, got %T", expression)
	}
	if _, ok := call.Callee.(*TypeExpr); !ok {
		t.Fatalf("expected typed conversion callee, got %T", call.Callee)
	}
}

func TestParseExpressionBuildsLambdaAndCompositeLiteral(t *testing.T) {
	lambda, err := ParseExpressionTokens(expressionTokens(t, `x => x + 1`))
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := lambda.(*LambdaExpr); !ok || len(value.Parameters) != 1 {
		t.Fatalf("expected expression lambda, got %#v", lambda)
	}

	composite, err := ParseExpressionTokens(expressionTokens(t, `Person{Name: "Bob", Age: 42}`))
	if err != nil {
		t.Fatal(err)
	}
	literal, ok := composite.(*CompositeLiteralExpr)
	if !ok || len(literal.Elements) != 2 {
		t.Fatalf("expected keyed composite literal, got %#v", composite)
	}
	if _, ok := literal.Elements[0].Key.(*NameExpr); !ok {
		t.Fatalf("expected first composite key, got %#v", literal.Elements[0].Key)
	}

	blockLambda, err := ParseExpressionTokens(expressionTokens(t, `value => { return value + 1 }`))
	if err != nil {
		t.Fatal(err)
	}
	block, ok := blockLambda.(*LambdaExpr)
	if !ok || block.BlockBody == nil || block.Body == nil && len(block.BodyTokens) == 0 {
		t.Fatalf("expected structured block lambda, got %#v", blockLambda)
	}
}

func TestParseExpressionBuildsGoExpressionNodes(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   any
	}{
		{name: "function literal", source: `func(value int) string { return value }`, want: &FunctionLiteralExpr{}},
		{name: "type assertion", source: `value.(string)`, want: &TypeAssertExpr{}},
		{name: "type switch assertion", source: `value.(type)`, want: &TypeAssertExpr{}},
		{name: "slice", source: `items[1:3]`, want: &SliceExpr{}},
		{name: "three index slice", source: `items[:limit:capacity]`, want: &SliceExpr{}},
		{name: "postfix", source: `counter++`, want: &PostfixExpr{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expression, err := ParseExpressionTokens(expressionTokens(t, test.source))
			if err != nil {
				t.Fatal(err)
			}
			switch test.want.(type) {
			case *FunctionLiteralExpr:
				value, ok := expression.(*FunctionLiteralExpr)
				if !ok || value.Type == nil || value.Body == nil {
					t.Fatalf("expected structured function literal, got %#v", expression)
				}
			case *TypeAssertExpr:
				value, ok := expression.(*TypeAssertExpr)
				if !ok {
					t.Fatalf("expected structured type assertion, got %#v", expression)
				}
				if test.name == "type switch assertion" && !value.TypeSwitch {
					t.Fatalf("expected type-switch assertion, got %#v", value)
				}
			case *SliceExpr:
				value, ok := expression.(*SliceExpr)
				if !ok || value.High == nil {
					t.Fatalf("expected structured slice expression, got %#v", expression)
				}
			case *PostfixExpr:
				value, ok := expression.(*PostfixExpr)
				if !ok || value.Operator != "++" {
					t.Fatalf("expected structured postfix expression, got %#v", expression)
				}
			}
			printed, err := expressionNodeSource(expression)
			if err != nil {
				t.Fatal(err)
			}
			if printed == "" {
				t.Fatalf("expected source printer output for %T", expression)
			}
		})
	}
}

func TestParseExpressionDistinguishesChannelReceiveFromSend(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `err := <-done`))
	if err != nil {
		t.Fatal(err)
	}
	assignment, ok := expression.(*BinaryExpr)
	if !ok {
		t.Fatalf("expected assignment containing receive expression, got %#v", expression)
	}
	if _, ok := assignment.Right.(*UnaryExpr); !ok {
		t.Fatalf("expected unary receive operand, got %#v", assignment.Right)
	}

	send, err := ParseExpressionTokens(expressionTokens(t, `done <- value`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := send.(*SendExpr); !ok {
		t.Fatalf("expected typed channel send expression, got %#v", send)
	}
}

func TestParseExpressionBuildsNestedLiterals(t *testing.T) {
	tests := []struct {
		name   string
		source string
		check  func(t *testing.T, expression ExprNode)
	}{
		{
			name:   "address of composite",
			source: `&Person{Name: "Bob"}`,
			check: func(t *testing.T, expression ExprNode) {
				unary, ok := expression.(*UnaryExpr)
				if !ok {
					t.Fatalf("expected unary expression, got %#v", expression)
				}
				if _, ok := unary.Operand.(*CompositeLiteralExpr); !ok {
					t.Fatalf("expected composite literal operand, got %#v", unary.Operand)
				}
			},
		},
		{
			name:   "literal argument",
			source: `consume(Person{Name: "Bob"})`,
			check: func(t *testing.T, expression ExprNode) {
				call, ok := expression.(*CallExpr)
				if !ok || len(call.Arguments) != 1 {
					t.Fatalf("expected one-argument call, got %#v", expression)
				}
				if _, ok := call.Arguments[0].Value.(*CompositeLiteralExpr); !ok {
					t.Fatalf("expected composite literal argument, got %#v", call.Arguments[0].Value)
				}
			},
		},
		{
			name:   "literal selector",
			source: `Person{Name: "Bob"}.Name`,
			check: func(t *testing.T, expression ExprNode) {
				selector, ok := expression.(*SelectorExpr)
				if !ok {
					t.Fatalf("expected selector expression, got %#v", expression)
				}
				if _, ok := selector.Receiver.(*CompositeLiteralExpr); !ok {
					t.Fatalf("expected composite literal receiver, got %#v", selector.Receiver)
				}
			},
		},
		{
			name:   "function literal argument",
			source: `consume(func(value int) string { return "ok" })`,
			check: func(t *testing.T, expression ExprNode) {
				call, ok := expression.(*CallExpr)
				if !ok || len(call.Arguments) != 1 {
					t.Fatalf("expected function-literal call, got %#v", expression)
				}
				if _, ok := call.Arguments[0].Value.(*FunctionLiteralExpr); !ok {
					t.Fatalf("expected function literal argument, got %#v", call.Arguments[0].Value)
				}
			},
		},
		{
			name:   "anonymous struct literal",
			source: `struct{Name string}{Name: "Bob"}`,
			check: func(t *testing.T, expression ExprNode) {
				literal, ok := expression.(*CompositeLiteralExpr)
				if !ok || literal.Type == nil || len(literal.Elements) != 1 {
					t.Fatalf("expected anonymous struct literal, got %#v", expression)
				}
				if _, ok := literal.Type.(*StructType); !ok {
					t.Fatalf("expected structured anonymous struct type, got %#v", literal.Type)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expression, err := ParseExpressionTokens(expressionTokens(t, test.source))
			if err != nil {
				t.Fatal(err)
			}
			test.check(t, expression)
		})
	}
}

func TestParseExpressionKeepsFunctionLiteralAfterEarlierCallArgument(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `handle("/", func(value string) { use(Context(Value: value)) })`))
	if err != nil {
		t.Fatal(err)
	}
	call, ok := expression.(*CallExpr)
	if !ok || len(call.Arguments) != 2 {
		t.Fatalf("expected two call arguments, got %#v", expression)
	}
	if _, ok := call.Arguments[1].Value.(*FunctionLiteralExpr); !ok {
		t.Fatalf("expected the second argument to remain a function literal, got %T", call.Arguments[1].Value)
	}
}

func TestParseExpressionPreservesMultilineNestedFunctionLiteralBodies(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `register(func(ctx *Context) error {
	if err := run(ctx); err != nil {
		log.Printf("failed: %v", err)
		if hookErr := recoverHook(ctx, err); hookErr != nil { err = hookErr }
	}
	return nil
})`))
	if err != nil {
		t.Fatal(err)
	}
	call, ok := expression.(*CallExpr)
	if !ok || len(call.Arguments) != 1 {
		t.Fatalf("expected callback call, got %#v", expression)
	}
	literal, ok := call.Arguments[0].Value.(*FunctionLiteralExpr)
	if !ok {
		t.Fatalf("expected function literal callback, got %T", call.Arguments[0].Value)
	}
	if len(literal.Body.Statements) != 2 {
		t.Fatalf("expected outer if and return statements, got %d", len(literal.Body.Statements))
	}
	conditional, ok := literal.Body.Statements[0].(*IfStmt)
	if !ok || len(conditional.Body.Statements) != 2 {
		t.Fatalf("expected structured callback if body, got %#v", literal.Body.Statements[0])
	}
	if _, ok := conditional.Body.Statements[1].(*IfStmt); !ok {
		t.Fatalf("expected nested if statement, got %T", conditional.Body.Statements[1])
	}
}

func TestParseExpressionBuildsInterpolatedString(t *testing.T) {
	expression, err := ParseExpressionTokens(expressionTokens(t, `"Hello {{name}}, total {{amount:%.2f}}"`))
	if err != nil {
		t.Fatal(err)
	}
	interpolated, ok := expression.(*InterpolatedStringExpr)
	if !ok || len(interpolated.Segments) != 4 {
		t.Fatalf("expected interpolated string segments, got %#v", expression)
	}
	if interpolated.Segments[0].Text != "Hello " || interpolated.Segments[1].Expression == nil || len(interpolated.Segments[1].ExpressionTokens) == 0 || interpolated.Segments[3].Format != "%.2f" {
		t.Fatalf("unexpected interpolation segments: %#v", interpolated.Segments)
	}
}

func TestInterpolationRendererUsesExpressionTokens(t *testing.T) {
	tokens := expressionTokens(t, `"value={{amount - -1}}"`)
	expression, err := ParseExpressionTokens(tokens)
	if err != nil {
		t.Fatal(err)
	}
	interpolated, ok := expression.(*InterpolatedStringExpr)
	if !ok {
		t.Fatalf("expected interpolated string AST, got %#v", expression)
	}
	lowered, err := renderInterpolatedStringExpr(interpolated, "fmt.Sprintf")
	if err != nil {
		t.Fatal(err)
	}
	if lowered != `fmt.Sprintf("value=%v", amount- -1)` {
		t.Fatalf("unexpected AST interpolation lowering: %s", lowered)
	}
}

func TestMethodInterpolationLowersFromBodyAST(t *testing.T) {
	file, err := ParseFile("interpolation-method.gpp", `
class Greeter {
    func Message(name string) string {
        return "Hello {{name}}"
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	method := file.Decls[0].(*ClassDecl).Methods[0]
	transformed, err := transformMethodInterpolation(method, "fmt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(transformed, `fmt.Sprintf("Hello %v", name)`) {
		t.Fatalf("method interpolation was not lowered from the body AST: %q", transformed)
	}
}

func TestTopLevelInterpolationLowersFromFunctionAST(t *testing.T) {
	file, err := ParseFile("interpolation-function.gpp", `
func message(name string) string {
    return "Hello {{name}}"
}
`)
	if err != nil {
		t.Fatal(err)
	}
	function := file.Decls[0].(*FunctionDecl)
	transformed, err := transformInterpolationInFunctionSource(functionSource(function), function, "fmt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(transformed, `fmt.Sprintf("Hello %v", name)`) {
		t.Fatalf("top-level interpolation was not lowered from the function AST: %q", transformed)
	}
}
