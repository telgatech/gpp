package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
)

type coalesceCandidate struct {
	expression *ast.BinaryExpr
	start      int
	end        int
	depth      int
}

// transformErrorCoalescing lowers A ?? B to a generic helper call. The
// helper's fallback closure is invoked only after a Go++ thrown error, so its
// evaluation remains lazy and errors from the fallback are not swallowed by
// the same operator.
func transformErrorCoalescing(src string, context constructorContext) (string, error) {
	for strings.Contains(src, "??") {
		positions := coalesceOperatorPositions(src)
		if len(positions) == 0 {
			return src, nil
		}

		marked := src
		for index := len(positions) - 1; index >= 0; index-- {
			position := positions[index]
			marked = marked[:position] + "||" + marked[position+2:]
		}
		parsed, fileSet, prefixLength, err := parseExceptionSource(marked, context)
		if err != nil {
			return src, nil
		}

		positionSet := map[int]bool{}
		for _, position := range positions {
			positionSet[position] = true
		}
		candidates := []coalesceCandidate{}
		ast.Inspect(parsed, func(node ast.Node) bool {
			binary, ok := node.(*ast.BinaryExpr)
			if !ok || binary.Op != token.LOR {
				return true
			}
			operatorPosition := fileSet.Position(binary.OpPos).Offset - prefixLength
			if !positionSet[operatorPosition] {
				return true
			}
			start := fileSet.Position(binary.Pos()).Offset - prefixLength
			end := fileSet.Position(binary.End()).Offset - prefixLength
			candidates = append(candidates, coalesceCandidate{expression: binary, start: start, end: end})
			return true
		})
		if len(candidates) == 0 {
			return src, nil
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
		// Transform the outermost operator first. This keeps a chain's left
		// operand inside the protected closure of the next operator.
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].depth != candidates[j].depth {
				return candidates[i].depth < candidates[j].depth
			}
			return candidates[i].start < candidates[j].start
		})
		candidate := candidates[0]
		left := strings.TrimSpace(sourceNodeText(candidate.expression.X, fileSet, prefixLength, src))
		right := strings.TrimSpace(sourceNodeText(candidate.expression.Y, fileSet, prefixLength, src))
		if left == "" || right == "" {
			return "", fmt.Errorf("?? requires a left expression and a fallback expression")
		}
		valueTypes := polymorphicValueTypes(parsed, context)
		leftType := coalesceExpressionType(candidate.expression.X, src, fileSet, prefixLength, context, valueTypes)
		rightType := coalesceExpressionType(candidate.expression.Y, src, fileSet, prefixLength, context, valueTypes)
		if leftType == "" {
			return src, nil
		}
		if rightType == "" {
			return src, nil
		}
		if leftType == "error" {
			return "", fmt.Errorf("?? requires a value-producing left operand")
		}
		replacement := fmt.Sprintf(
			"__gppCoalesce[%s](func() %s { return %s }, func() %s { return %s })",
			leftType,
			leftType,
			left,
			leftType,
			right,
		)
		if candidate.start < 0 || candidate.end > len(src) || candidate.start > candidate.end {
			return src, nil
		}
		src = src[:candidate.start] + replacement + src[candidate.end:]
	}
	return src, nil
}

func coalesceOperatorPositions(src string) []int {
	positions := []int{}
	for index := 0; index < len(src); {
		var ignored strings.Builder
		if end, ok, err := copyIgnoredSource(src, index, &ignored); err == nil && ok {
			index = end
			continue
		}
		if index+1 < len(src) && src[index] == '?' && src[index+1] == '?' {
			positions = append(positions, index)
			index += 2
			continue
		}
		index++
	}
	return positions
}

func coalesceExpressionType(expr ast.Expr, original string, fileSet *token.FileSet, prefixLength int, context constructorContext, valueTypes map[string]string) string {
	if binary, ok := expr.(*ast.BinaryExpr); ok && binary.Op == token.LOR {
		start := fileSet.Position(binary.Pos()).Offset - prefixLength
		end := fileSet.Position(binary.End()).Offset - prefixLength
		if start >= 0 && end <= len(original) && strings.Contains(original[start:end], "??") {
			return coalesceExpressionType(binary.X, original, fileSet, prefixLength, context, valueTypes)
		}
	}
	typeName := strings.TrimSpace(expressionStaticType(expr, context, valueTypes))
	parts := enumResultTypes(typeName)
	if len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}
	return typeName
}
