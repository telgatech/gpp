package compiler

import (
	"fmt"
	"sort"
	"strings"
)

type ternaryCandidate struct {
	expression *CallExpr
	start      int
	end        int
	depth      int
}

type ternaryContext struct {
	RuntimeEmitted bool
}

// transformTernaryExpressions preserves ternary expressions as typed, lazy
// lambdas until the lambda lowering pass can infer their result types.
func transformTernaryExpressions(source string) (string, error) {
	tokens, err := LexSource("ternary", source)
	if err != nil || !tokenSequence(tokens, "?") {
		return source, nil
	}
	blocks := []*BlockStmt{}
	functions := parseTopLevelFunctions("ternary", "main", source, 0, source, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function != nil && function.Method.BodyAST != nil {
				blocks = append(blocks, function.Method.BodyAST)
			}
		}
	} else if block, parseErr := ParseBodyAST(tokens); parseErr == nil {
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return source, nil
	}

	candidates := []ternaryCandidate{}
	for _, block := range blocks {
		WalkBlockExpressions(block, func(expression ExprNode) {
			collectTernaryExpressions(expression, &candidates)
		})
	}
	if len(candidates) == 0 {
		return source, nil
	}
	for index := range candidates {
		for otherIndex := range candidates {
			if index == otherIndex {
				continue
			}
			other := candidates[otherIndex]
			if other.start <= candidates[index].start && other.end >= candidates[index].end {
				candidates[index].depth++
			}
		}
	}
	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	for _, candidate := range candidates {
		if candidate.depth != 0 {
			continue
		}
		replacement, lowerErr := ternaryCallSource(candidate.expression)
		if lowerErr != nil {
			return "", lowerErr
		}
		if candidate.start < 0 || candidate.end > len(source) || candidate.start > candidate.end {
			return source, nil
		}
		edits = append(edits, edit{start: candidate.start, end: candidate.end, text: replacement})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		source = source[:edit.start] + edit.text + source[edit.end:]
	}
	return source, nil
}

func collectTernaryExpressions(expression ExprNode, result *[]ternaryCandidate) {
	if expression == nil {
		return
	}
	if call, ok := expression.(*CallExpr); ok && isTernaryCall(call) {
		span := call.Span()
		*result = append(*result, ternaryCandidate{expression: call, start: span.Start, end: span.End})
	}
	visit := func(child ExprNode) { collectTernaryExpressions(child, result) }
	switch value := expression.(type) {
	case *UnaryExpr:
		visit(value.Operand)
	case *BinaryExpr:
		visit(value.Left)
		visit(value.Right)
	case *AssignmentExpr:
		for _, child := range append(append([]ExprNode{}, value.Left...), value.Right...) {
			visit(child)
		}
	case *SelectorExpr:
		visit(value.Receiver)
	case *IndexExpr:
		visit(value.Receiver)
		visit(value.Index)
	case *IndexListExpr:
		visit(value.Receiver)
		for _, child := range value.Indices {
			visit(child)
		}
	case *SliceExpr:
		for _, child := range []ExprNode{value.Receiver, value.Low, value.High, value.Max} {
			visit(child)
		}
	case *TypeAssertExpr:
		visit(value.Expression)
	case *PostfixExpr:
		visit(value.Expression)
	case *SpreadExpr:
		visit(value.Expression)
	case *SendExpr:
		visit(value.Channel)
		visit(value.Value)
	case *CallExpr:
		visit(value.Callee)
		for _, argument := range value.Arguments {
			visit(argument.Value)
		}
	case *ParenthesizedExpr:
		visit(value.Inner)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			visit(element.Key)
			visit(element.Value)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			visit(segment.Expression)
		}
	case *LambdaExpr:
		visit(value.Body)
		WalkBlockExpressions(value.BlockBody, visit)
	case *FunctionLiteralExpr:
		WalkBlockExpressions(value.Body, visit)
	}
}

func isTernaryCall(call *CallExpr) bool {
	if call == nil || len(call.Arguments) != 3 {
		return false
	}
	name, ok := call.Callee.(*NameExpr)
	return ok && name.Name == "__gppTernary"
}

func ternaryCallSource(call *CallExpr) (string, error) {
	if !isTernaryCall(call) {
		return "", fmt.Errorf("invalid ternary expression")
	}
	condition, err := expressionNodeSource(call.Arguments[0].Value)
	if err != nil {
		return "", err
	}
	whenTrue := ternaryBranchValue(call.Arguments[1].Value)
	whenFalse := ternaryBranchValue(call.Arguments[2].Value)
	trueSource, err := expressionNodeSource(whenTrue)
	if err != nil {
		return "", err
	}
	falseSource, err := expressionNodeSource(whenFalse)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("__gppTernary(%s, () => %s, () => %s)", strings.TrimSpace(condition), strings.TrimSpace(trueSource), strings.TrimSpace(falseSource)), nil
}
