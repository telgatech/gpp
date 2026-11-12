package lsp

import (
	"sort"

	"github.com/telgatech/gpp/compiler"
)

type lambdaParameter struct {
	name        string
	declaration compiler.Span
	scope       compiler.Span
	arrow       compiler.Token
}

func lambdaParameterAt(state *fileState, method *compiler.Method, name string, offset int) (lambdaParameter, bool) {
	if state == nil || state.File == nil || method == nil || method.BodyAST == nil || name == "" {
		return lambdaParameter{}, false
	}
	if state.lambdaParameters == nil {
		state.lambdaParameters = make(map[*compiler.Method][]lambdaParameter)
	}
	parameters, cached := state.lambdaParameters[method]
	if !cached {
		parameters = collectLambdaParameters(state, method)
		state.lambdaParameters[method] = parameters
	}
	var selected lambdaParameter
	selectedSize := int(^uint(0) >> 1)
	for _, parameter := range parameters {
		if parameter.declaration.End <= parameter.declaration.Start || offset < parameter.scope.Start || offset > parameter.scope.End || parameter.name != name {
			continue
		}
		size := parameter.scope.End - parameter.scope.Start
		if size < selectedSize {
			selected, selectedSize = parameter, size
		}
	}
	if selected.declaration.End <= selected.declaration.Start {
		return lambdaParameter{}, false
	}
	return selected, true
}

func collectLambdaParameters(state *fileState, method *compiler.Method) []lambdaParameter {
	var result []lambdaParameter
	var visitExpression func(compiler.ExprNode)
	visitExpression = func(expression compiler.ExprNode) {
		if expression == nil {
			return
		}
		switch value := expression.(type) {
		case *compiler.LambdaExpr:
			span := value.SpanValue
			arrowIndex := sort.Search(len(state.File.Tokens), func(index int) bool { return state.File.Tokens[index].Span.Start >= span.Start })
			var arrow compiler.Token
			for index := arrowIndex; index < len(state.File.Tokens) && state.File.Tokens[index].Span.Start < span.End; index++ {
				if state.File.Tokens[index].Text == "=>" {
					arrow = state.File.Tokens[index]
					break
				}
			}
			if arrow.Span.End > arrow.Span.Start {
				for _, parameter := range value.Parameters {
					if parameter.Kind == compiler.TokenIdentifier {
						result = append(result, lambdaParameter{name: parameter.Text, declaration: parameter.Span, scope: span, arrow: arrow})
					}
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
			for _, child := range value.Left {
				visitExpression(child)
			}
			for _, child := range value.Right {
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
	return result
}
