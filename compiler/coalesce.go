package compiler

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"
)

type coalesceASTCandidate struct {
	typed *BinaryExpr
	block *BlockStmt
	start int
	end   int
	depth int
}

// transformErrorCoalescing lowers A ?? B to a generic helper call. The
// helper's fallback closure is invoked only after a Go++ thrown error, so its
// evaluation remains lazy and errors from the fallback are not swallowed by
// the same operator.
func transformErrorCoalescing(src string, context constructorContext) (string, error) {
	for strings.Contains(src, "??") {
		transformed, handled, err := transformErrorCoalescingAST(src, context)
		if handled {
			if err != nil {
				return "", err
			}
			if transformed == src {
				return src, nil
			}
			src = transformed
			continue
		}
		return "", fmt.Errorf("coalescing expression could not be represented by the body AST")
	}
	return src, nil
}

// transformErrorCoalescingAST lowers coalescing operators discovered in the
// structured body AST. This path intentionally does not reconstruct a Go AST;
// source spans, operands, and type hints all come from Go++ nodes.
func transformErrorCoalescingAST(src string, context constructorContext) (string, bool, error) {
	tokens, err := LexSource("coalescing", src)
	if err != nil {
		return src, false, nil
	}
	blocks := []*BlockStmt{}
	// Prefer top-level function bodies when the input is a declaration region.
	// ParseBodyAST intentionally accepts unknown declaration-shaped text as a
	// token fallback, which would otherwise hide the structured function AST.
	functions := parseTopLevelFunctions("coalescing", "main", src, 0, src, "", 0)
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
		return src, false, nil
	}
	candidates := []coalesceASTCandidate{}
	for _, block := range blocks {
		typed := []*BinaryExpr{}
		collectCoalesceBodyExpressions(block, func(expression ExprNode) {
			collectCoalesceExprs(expression, &typed)
		})
		for _, expression := range typed {
			if expression == nil || expression.Operator != "??" {
				continue
			}
			span := expression.Span()
			candidates = append(candidates, coalesceASTCandidate{
				typed: expression,
				block: block,
				start: span.Start,
				end:   span.End,
			})
		}
	}
	if len(candidates) == 0 {
		return src, false, nil
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
		// Nested operators are lowered while rendering their containing
		// expression. This lets a chain be handled in one AST pass and avoids
		// reparsing the generated generic closure syntax on the next iteration.
		if candidate.depth != 0 {
			continue
		}
		valueTypes := coalesceValueTypes(candidate.block, context)
		replacement, lowerErr := lowerCoalesceExpression(candidate.typed, context, valueTypes)
		if lowerErr != nil {
			if strings.Contains(lowerErr.Error(), "could not infer") {
				return src, false, nil
			}
			return "", true, lowerErr
		}
		if candidate.start < 0 || candidate.end > len(src) || candidate.start > candidate.end {
			return src, false, nil
		}
		edits = append(edits, edit{start: candidate.start, end: candidate.end, text: replacement})
	}
	if len(edits) == 0 {
		return src, false, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

func lowerCoalesceExpression(expression ExprNode, context constructorContext, valueTypes map[string]string) (string, error) {
	binary, ok := expression.(*BinaryExpr)
	if !ok || binary.Operator != "??" {
		return expressionNodeSource(expression)
	}
	left, err := lowerCoalesceExpression(binary.Left, context, valueTypes)
	if err != nil {
		return "", err
	}
	right, err := lowerCoalesceExpression(binary.Right, context, valueTypes)
	if err != nil {
		return "", err
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return "", fmt.Errorf("?? requires a left expression and a fallback expression")
	}
	leftType := coalesceExpressionTypeNode(binary.Left, context, valueTypes)
	rightType := coalesceExpressionTypeNode(binary.Right, context, valueTypes)
	if leftType == "" || rightType == "" {
		return "", fmt.Errorf("could not infer the value type for ??")
	}
	if leftType == "error" {
		return "", fmt.Errorf("?? requires a value-producing left operand")
	}
	// The operands become return expressions inside generated closures. Run
	// implicit error promotion on those structured fragments before emitting
	// the closures, otherwise a multi-result call such as ParsePort() would
	// escape into a single-value `func() T { return ... }` context.
	left, err = promoteCoalesceOperand(left, leftType, context)
	if err != nil {
		return "", err
	}
	right, err = promoteCoalesceOperand(right, rightType, context)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"__gppCoalesce[%s](func() %s { return %s }, func() %s { return %s })",
		leftType, leftType, left, leftType, right,
	), nil
}

func promoteCoalesceOperand(source, resultType string, context constructorContext) (string, error) {
	operandContext := context
	operandContext.CurrentResultAST = parseTypeText(resultType)
	transformed, handled, err := transformImplicitErrorPromotionAST("return "+source, operandContext)
	if !handled {
		return source, nil
	}
	if err != nil {
		return "", err
	}
	transformed = strings.TrimSpace(transformed)
	if strings.HasPrefix(transformed, "return ") {
		return strings.TrimSpace(strings.TrimPrefix(transformed, "return ")), nil
	}
	return transformed, nil
}

func coalesceValueTypes(block *BlockStmt, context constructorContext) map[string]string {
	result := map[string]string{}
	for name, typeName := range context.CurrentParameterTypes {
		result[name] = typeName
	}
	var visitBlock func(*BlockStmt)
	var visitStatement func(Stmt)
	visitBlock = func(current *BlockStmt) {
		if current == nil {
			return
		}
		for _, statement := range current.Statements {
			visitStatement(statement)
		}
	}
	visitStatement = func(statement Stmt) {
		if statement == nil {
			return
		}
		switch value := statement.(type) {
		case *TokenStmt:
			visitBlock(value.Body)
			for _, child := range value.Children {
				visitStatement(child)
			}
		case *DeclarationStmt:
			declared := ""
			if value.Type != nil {
				declared, _ = typeNodeSource(value.Type)
			}
			for index, name := range value.Names {
				inferred := declared
				if inferred == "" && index < len(value.Values) {
					inferred = coalesceExpressionTypeNode(value.Values[index], context, result)
				}
				if inferred != "" {
					result[name.Text] = inferred
				}
			}
		case *AssignmentStmt:
			for index, left := range value.Left {
				name, ok := left.(*NameExpr)
				if !ok || index >= len(value.Right) {
					continue
				}
				if inferred := coalesceExpressionTypeNode(value.Right[index], context, result); inferred != "" {
					result[name.Name] = inferred
				}
			}
		case *IfStmt:
			visitBlock(value.Body)
			visitBlock(value.Else)
			if value.ElseIf != nil {
				visitStatement(value.ElseIf)
			}
		case *ForStmt:
			visitBlock(value.Body)
		case *SwitchStmt:
			visitBlock(value.Body)
		case *CaseStmt:
			visitBlock(value.Clause.Body)
		case *TryStmt:
			visitBlock(value.Body)
			for _, clause := range value.Catches {
				visitBlock(clause.Body)
			}
			visitBlock(value.Finally)
		case *BlockStmt:
			visitBlock(value)
		}
	}
	visitBlock(block)
	return result
}

func coalesceExpressionTypeNode(expression ExprNode, context constructorContext, valueTypes map[string]string) string {
	if expression == nil {
		return ""
	}
	var typeName string
	switch value := expression.(type) {
	case *LiteralExpr:
		switch value.Kind {
		case TokenString, TokenRawString:
			typeName = "string"
		case TokenNumber:
			if strings.ContainsAny(value.Text, ".eE") {
				typeName = "float64"
			} else {
				typeName = "int"
			}
		case TokenRune:
			typeName = "rune"
		default:
			if value.Text == "true" || value.Text == "false" {
				typeName = "bool"
			}
		}
	case *NameExpr:
		typeName = valueTypes[value.Name]
		if typeName == "" && (value.Name == "true" || value.Name == "false") {
			typeName = "bool"
		}
	case *InterpolatedStringExpr:
		typeName = "string"
	case *ParenthesizedExpr:
		typeName = coalesceExpressionTypeNode(value.Inner, context, valueTypes)
	case *SliceExpr:
		typeName = coalesceExpressionTypeNode(value.Receiver, context, valueTypes)
		if strings.HasPrefix(typeName, "[]") {
			typeName = strings.TrimPrefix(typeName, "[]")
		} else if typeName == "string" {
			typeName = "string"
		}
	case *TypeAssertExpr:
		if !value.TypeSwitch && value.Type != nil {
			typeName, _ = typeNodeSource(value.Type)
		}
	case *PostfixExpr:
		typeName = coalesceExpressionTypeNode(value.Expression, context, valueTypes)
	case *SpreadExpr:
		typeName = coalesceExpressionTypeNode(value.Expression, context, valueTypes)
	case *TypeExpr:
		if value.Type != nil {
			typeName, _ = typeNodeSource(value.Type)
		}
	case *SendExpr:
		typeName = coalesceExpressionTypeNode(value.Value, context, valueTypes)
	case *FunctionLiteralExpr:
		if value.Type != nil {
			typeName, _ = typeNodeSource(value.Type)
		}
	case *UnaryExpr:
		typeName = coalesceExpressionTypeNode(value.Operand, context, valueTypes)
		if value.Operator == "&" && typeName != "" && !strings.HasPrefix(typeName, "*") {
			typeName = "*" + typeName
		}
		if value.Operator == "!" {
			typeName = "bool"
		}
	case *BinaryExpr:
		if value.Operator == "&&" || value.Operator == "||" || value.Operator == "==" || value.Operator == "!=" || value.Operator == "<" || value.Operator == "<=" || value.Operator == ">" || value.Operator == ">=" {
			return "bool"
		}
		typeName = coalesceExpressionTypeNode(value.Left, context, valueTypes)
	case *CompositeLiteralExpr:
		typeName, _ = typeNodeSource(value.Type)
	case *SelectorExpr:
		receiverType := strings.TrimPrefix(strings.TrimSpace(coalesceExpressionTypeNode(value.Receiver, context, valueTypes)), "*")
		if target, ok := context.Targets[receiverType]; ok {
			for _, field := range target.Class.Fields {
				if field.Name == value.Name {
					return fieldTypeSource(field)
				}
			}
			for _, signature := range context.ClassMethodSignatures[receiverType][value.Name] {
				if signature.resultText() != "" {
					return firstCoalesceResultType(signature.resultText())
				}
			}
		}
	case *CallExpr:
		typeName = coalesceCallResultType(value, context, valueTypes)
	}
	return firstCoalesceResultType(typeName)
}

func coalesceCallResultType(call *CallExpr, context constructorContext, valueTypes map[string]string) string {
	if call == nil {
		return ""
	}
	var candidates []callableSignature
	switch callee := call.Callee.(type) {
	case *NameExpr:
		candidates = context.FunctionSignatures[callee.Name]
	case *SelectorExpr:
		receiverType := strings.TrimPrefix(strings.TrimSpace(coalesceExpressionTypeNode(callee.Receiver, context, valueTypes)), "*")
		if _, isClass := context.Targets[receiverType]; isClass {
			candidates = context.StaticMethodSignatures[receiverType][callee.Name]
		} else {
			candidates = context.ClassMethodSignatures[receiverType][callee.Name]
		}
		if len(candidates) == 0 && receiverType != "" {
			for _, extension := range context.Extensions {
				if extension.Method.Name == callee.Name && extensionTargetMatches(extension.Target, extension.ReceiverType, receiverType, context) {
					return methodResultSource(extension.Method)
				}
			}
		}
	}
	for _, candidate := range candidates {
		if len(call.Arguments) >= requiredParameterCount(candidate) && len(call.Arguments) <= len(candidate.Parameters) {
			return candidate.resultText()
		}
	}
	if selector, ok := call.Callee.(*SelectorExpr); ok {
		if receiver, ok := selector.Receiver.(*NameExpr); ok {
			// Native package calls are resolved through go/types without
			// reparsing the expression source. The small Go AST adapter is only
			// used for the existing importer API.
			nativeCall := &ast.CallExpr{Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: receiver.Name},
				Sel: &ast.Ident{Name: selector.Name},
			}}
			if resultTypes, ok := nativePackageResultTypes(nativeCall, context); ok && len(resultTypes) > 0 {
				return resultTypes[0]
			}
		}
	}
	return ""
}

func firstCoalesceResultType(typeName string) string {
	parts := enumResultTypes(typeName)
	if len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}
	return strings.TrimSpace(typeName)
}

func collectCoalesceBodyExpressions(block *BlockStmt, visit func(ExprNode)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectCoalesceStmtExpressions(statement, visit)
	}
}

func collectCoalesceStmtExpressions(statement Stmt, visit func(ExprNode)) {
	walkStmtExpressions(statement, visit)
}

func collectCoalesceStructuredHeader(statement Stmt, visit func(ExprNode)) {
	switch value := statement.(type) {
	case *IfStmt:
		visit(value.Init)
		visit(value.Condition)
	case *ForStmt:
		visit(value.Init)
		visit(value.Condition)
		visit(value.Post)
		visit(value.RangeExpr)
	case *SwitchStmt:
		visit(value.Init)
		visit(value.Tag)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			visit(expression)
		}
	}
}

func collectCoalesceExprs(expression ExprNode, result *[]*BinaryExpr) {
	if expression == nil {
		return
	}
	switch value := expression.(type) {
	case *BinaryExpr:
		if value.Operator == "??" {
			*result = append(*result, value)
		}
		collectCoalesceExprs(value.Left, result)
		collectCoalesceExprs(value.Right, result)
	case *UnaryExpr:
		collectCoalesceExprs(value.Operand, result)
	case *SelectorExpr:
		collectCoalesceExprs(value.Receiver, result)
	case *IndexExpr:
		collectCoalesceExprs(value.Receiver, result)
		collectCoalesceExprs(value.Index, result)
	case *IndexListExpr:
		collectCoalesceExprs(value.Receiver, result)
		for _, index := range value.Indices {
			collectCoalesceExprs(index, result)
		}
	case *SliceExpr:
		collectCoalesceExprs(value.Receiver, result)
		collectCoalesceExprs(value.Low, result)
		collectCoalesceExprs(value.High, result)
		collectCoalesceExprs(value.Max, result)
	case *TypeAssertExpr:
		collectCoalesceExprs(value.Expression, result)
	case *PostfixExpr:
		collectCoalesceExprs(value.Expression, result)
	case *SpreadExpr:
		collectCoalesceExprs(value.Expression, result)
	case *TypeExpr:
		// Type expressions are not value-producing coalescing operands.
	case *SendExpr:
		collectCoalesceExprs(value.Channel, result)
		collectCoalesceExprs(value.Value, result)
	case *CallExpr:
		collectCoalesceExprs(value.Callee, result)
		for _, argument := range value.Arguments {
			collectCoalesceExprs(argument.Value, result)
		}
	case *ParenthesizedExpr:
		collectCoalesceExprs(value.Inner, result)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectCoalesceExprs(element.Key, result)
			collectCoalesceExprs(element.Value, result)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectCoalesceExprs(segment.Expression, result)
		}
	case *LambdaExpr:
		collectCoalesceExprs(value.Body, result)
		collectCoalesceBodyExpressions(value.BlockBody, func(expression ExprNode) {
			collectCoalesceExprs(expression, result)
		})
	case *FunctionLiteralExpr:
		collectCoalesceBodyExpressions(value.Body, func(expression ExprNode) {
			collectCoalesceExprs(expression, result)
		})
	}
}
