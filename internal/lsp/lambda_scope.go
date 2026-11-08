package lsp

import "github.com/telgatech/gpp/compiler"

type lambdaParameter struct {
	declaration compiler.Span
	scope       compiler.Span
	arrow       compiler.Token
}

func lambdaParameterAt(state *fileState, method *compiler.Method, name string, offset int) (lambdaParameter, bool) {
	if state == nil || state.File == nil || method == nil || method.BodyAST == nil || name == "" {
		return lambdaParameter{}, false
	}
	var selected lambdaParameter
	selectedSize := int(^uint(0) >> 1)
	var visitExpression func(compiler.ExprNode)
	visitExpression = func(expression compiler.ExprNode) {
		if expression == nil {
			return
		}
		switch value := expression.(type) {
		case *compiler.LambdaExpr:
			span := value.SpanValue
			if offset >= span.Start && offset <= span.End {
				for _, parameter := range value.Parameters {
					if parameter.Kind != compiler.TokenIdentifier || parameter.Text != name {
						continue
					}
					size := span.End - span.Start
					if size >= selectedSize {
						continue
					}
					var arrow compiler.Token
					for _, token := range state.File.Tokens {
						if token.Text == "=>" && token.Span.Start >= parameter.Span.End && token.Span.Start < span.End {
							arrow = token
							break
						}
					}
					if arrow.Span.End <= arrow.Span.Start {
						continue
					}
					selected = lambdaParameter{declaration: parameter.Span, scope: span, arrow: arrow}
					selectedSize = size
				}
			}
			visitExpression(value.Body)
			compiler.WalkBlockExpressions(value.BlockBody, visitExpression)
		case *compiler.UnaryExpr:
			visitExpression(value.Operand)
		case *compiler.BinaryExpr:
			visitExpression(value.Left)
			visitExpression(value.Right)
		case *compiler.AssignmentExpr:
			for _, child := range append(append([]compiler.ExprNode{}, value.Left...), value.Right...) {
				visitExpression(child)
			}
		case *compiler.SelectorExpr:
			visitExpression(value.Receiver)
		case *compiler.IndexExpr:
			visitExpression(value.Receiver)
			visitExpression(value.Index)
		case *compiler.IndexListExpr:
			visitExpression(value.Receiver)
			for _, child := range value.Indices {
				visitExpression(child)
			}
		case *compiler.SliceExpr:
			visitExpression(value.Receiver)
			visitExpression(value.Low)
			visitExpression(value.High)
			visitExpression(value.Max)
		case *compiler.TypeAssertExpr:
			visitExpression(value.Expression)
		case *compiler.PostfixExpr:
			visitExpression(value.Expression)
		case *compiler.SpreadExpr:
			visitExpression(value.Expression)
		case *compiler.SendExpr:
			visitExpression(value.Channel)
			visitExpression(value.Value)
		case *compiler.FunctionLiteralExpr:
			compiler.WalkBlockExpressions(value.Body, visitExpression)
		case *compiler.CallExpr:
			visitExpression(value.Callee)
			for _, argument := range value.Arguments {
				visitExpression(argument.Value)
			}
		case *compiler.ParenthesizedExpr:
			visitExpression(value.Inner)
		case *compiler.CompositeLiteralExpr:
			for _, element := range value.Elements {
				visitExpression(element.Key)
				visitExpression(element.Value)
			}
		case *compiler.InterpolatedStringExpr:
			for _, segment := range value.Segments {
				visitExpression(segment.Expression)
			}
		}
	}
	compiler.WalkBlockExpressions(method.BodyAST, visitExpression)
	if selected.declaration.End <= selected.declaration.Start {
		return lambdaParameter{}, false
	}
	return selected, true
}
