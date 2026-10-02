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

type safeCoalesceLowering struct {
	resultType TypeNode
	isNil      ExprNode
	access     ExprNode
}

// safeCoalescePlan recognizes an explicit safe member access on the left of
// ?? and builds the nil condition separately from the value expression. This
// keeps standalone ?. zero-value semantics while letting the paired ?? use
// the fallback only when a safe receiver was nil.
func safeCoalescePlan(expression ExprNode, context constructorContext, valueTypes map[string]string) (*safeCoalesceLowering, bool, error) {
	for {
		parenthesized, ok := expression.(*ParenthesizedExpr)
		if !ok {
			break
		}
		expression = parenthesized.Inner
	}
	var call *CallExpr
	if candidate, ok := expression.(*CallExpr); ok {
		if selector, ok := candidate.Callee.(*SelectorExpr); ok && selector.Safe {
			call = candidate
			expression = selector
		}
	}
	selector, ok := expression.(*SelectorExpr)
	if !ok || !selector.Safe {
		return nil, false, nil
	}
	if !safeCoalesceChainRoot(selector.Receiver) {
		return nil, false, fmt.Errorf("safe access coalescing requires an identifier-based safe access chain")
	}
	var guards []ExprNode
	if err := collectSafeCoalesceGuards(selector.Receiver, context, valueTypes, &guards); err != nil {
		return nil, false, err
	}
	receiverType := coalesceExpressionTypeAST(selector.Receiver, context, valueTypes)
	typeName := coalesceTypeName(receiverType)
	target, found := context.Targets[typeName]
	if !found {
		return nil, false, fmt.Errorf("safe access receiver has no known class type")
	}
	receiverText, err := typeNodeSource(receiverType)
	if err != nil {
		return nil, false, err
	}
	if !safeReceiverCanBeNil(receiverText, target) {
		return nil, false, fmt.Errorf("safe access receiver must be a pointer or interface")
	}
	memberType, isMethod, err := safeMemberTypeNode(target.Class, target.Classes, selector.Name, map[string]bool{})
	if err != nil {
		return nil, false, err
	}
	if memberType == nil {
		return nil, false, fmt.Errorf("class %s has no member %s", target.Class.Name, selector.Name)
	}
	if isMethod != (call != nil) {
		if isMethod {
			return nil, false, fmt.Errorf("safe method access %s?.%s requires a call", expressionNodeSelectorReceiver(selector), selector.Name)
		}
		return nil, false, fmt.Errorf("safe field access %s?.%s is not callable", expressionNodeSelectorReceiver(selector), selector.Name)
	}
	guards = append(guards, &BinaryExpr{Left: safeOrdinaryExpr(selector.Receiver), Operator: "==", Right: &LiteralExpr{Text: "nil", Kind: TokenKeyword}})
	var isNil ExprNode
	for _, guard := range guards {
		if isNil == nil {
			isNil = guard
		} else {
			isNil = &BinaryExpr{Left: isNil, Operator: "||", Right: guard}
		}
	}
	var access ExprNode = &SelectorExpr{Receiver: safeOrdinaryExpr(selector.Receiver), Name: selector.Name}
	var resultType TypeNode = memberType
	if call != nil {
		args := make([]CallArg, len(call.Arguments))
		for index, argument := range call.Arguments {
			if argument.Name != "" {
				return nil, false, fmt.Errorf("safe method access does not support named arguments in this context")
			}
			args[index] = CallArg{Value: argument.Value}
		}
		access = &CallExpr{Callee: access, Arguments: args}
	}
	return &safeCoalesceLowering{resultType: resultType, isNil: isNil, access: access}, true, nil
}

func safeCoalesceChainRoot(expression ExprNode) bool {
	switch value := expression.(type) {
	case *NameExpr:
		return true
	case *ParenthesizedExpr:
		return safeCoalesceChainRoot(value.Inner)
	case *SelectorExpr:
		return value.Safe && safeCoalesceChainRoot(value.Receiver)
	default:
		return false
	}
}

func collectSafeCoalesceGuards(expression ExprNode, context constructorContext, valueTypes map[string]string, guards *[]ExprNode) error {
	switch value := expression.(type) {
	case *ParenthesizedExpr:
		return collectSafeCoalesceGuards(value.Inner, context, valueTypes, guards)
	case *SelectorExpr:
		if !value.Safe {
			return nil
		}
		if err := collectSafeCoalesceGuards(value.Receiver, context, valueTypes, guards); err != nil {
			return err
		}
		receiverType := coalesceExpressionTypeAST(value.Receiver, context, valueTypes)
		typeName := coalesceTypeName(receiverType)
		target, found := context.Targets[typeName]
		if !found {
			return fmt.Errorf("safe access receiver has no known class type")
		}
		receiverText, err := typeNodeSource(receiverType)
		if err != nil {
			return err
		}
		if !safeReceiverCanBeNil(receiverText, target) {
			return fmt.Errorf("safe access receiver must be a pointer or interface")
		}
		*guards = append(*guards, &BinaryExpr{Left: safeOrdinaryExpr(value.Receiver), Operator: "==", Right: &LiteralExpr{Text: "nil", Kind: TokenKeyword}})
	}
	return nil
}

func safeOrdinaryExpr(expression ExprNode) ExprNode {
	switch value := expression.(type) {
	case *ParenthesizedExpr:
		value.Inner = safeOrdinaryExpr(value.Inner)
	case *SelectorExpr:
		value.Receiver = safeOrdinaryExpr(value.Receiver)
		value.Safe = false
	case *CallExpr:
		value.Callee = safeOrdinaryExpr(value.Callee)
		for index := range value.Arguments {
			value.Arguments[index].Value = safeOrdinaryExpr(value.Arguments[index].Value)
		}
	}
	return expression
}

func expressionNodeSelectorReceiver(selector *SelectorExpr) string {
	if selector == nil {
		return "receiver"
	}
	text, err := expressionNodeSource(selector.Receiver)
	if err != nil || text == "" {
		return "receiver"
	}
	return text
}

// transformErrorCoalescing lowers A ?? B to a generic helper call. Ordinary
// operands fall back after a Go++ thrown error; explicit safe-access operands
// also fall back after a nil receiver check. In both cases fallback evaluation
// remains lazy and errors from the fallback are not swallowed by the operator.
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
	if plan, found, planErr := safeCoalescePlan(binary.Left, context, valueTypes); planErr != nil {
		return "", planErr
	} else if found {
		resultType, typeErr := typeNodeSource(plan.resultType)
		if typeErr != nil {
			return "", typeErr
		}
		isNil, sourceErr := expressionNodeSource(plan.isNil)
		if sourceErr != nil {
			return "", sourceErr
		}
		access, sourceErr := expressionNodeSource(plan.access)
		if sourceErr != nil {
			return "", sourceErr
		}
		right, sourceErr := lowerCoalesceExpression(binary.Right, context, valueTypes)
		if sourceErr != nil {
			return "", sourceErr
		}
		access, sourceErr = promoteCoalesceOperand(access, resultType, context)
		if sourceErr != nil {
			return "", sourceErr
		}
		right, sourceErr = promoteCoalesceOperand(right, resultType, context)
		if sourceErr != nil {
			return "", sourceErr
		}
		return fmt.Sprintf("__gppSafeCoalesce[%s](%s, func() %s { return %s }, func() %s { return %s })", resultType, isNil, resultType, access, resultType, right), nil
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
	// This is the compatibility adapter for source fragments that cannot use
	// direct AST emission. Native package calls may still require the existing
	// importer-backed promotion resolver here.
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

// lowerCoalesceExpressionNode is shared by direct body emission and the
// compatibility source-edit boundary. The latter still needs a replacement
// string because it edits a larger source buffer, but the replacement itself
// is now produced from typed expression nodes rather than concatenated source
// fragments.
func lowerCoalesceExpressionNode(expression ExprNode, context constructorContext, valueTypes map[string]string) (ExprNode, error) {
	binary, ok := expression.(*BinaryExpr)
	if !ok || binary.Operator != "??" {
		return lowerExceptionExprNode(expression, context)
	}
	if plan, found, planErr := safeCoalescePlan(binary.Left, context, valueTypes); planErr != nil {
		return nil, planErr
	} else if found {
		rightType := coalesceExpressionTypeAST(binary.Right, context, valueTypes)
		if rightType == nil {
			return nil, fmt.Errorf("could not infer the value type for ??")
		}
		access, err := lowerCoalesceOperandNode(plan.access, context, valueTypes)
		if err != nil {
			return nil, err
		}
		fallback, err := lowerCoalesceExpressionNode(binary.Right, context, valueTypes)
		if err != nil {
			return nil, err
		}
		fallback, err = lowerCoalesceOperandNode(fallback, context, valueTypes)
		if err != nil {
			return nil, err
		}
		functionType := &FunctionType{Results: []TypeNode{plan.resultType}}
		callee := &IndexExpr{
			Receiver: &NameExpr{Name: "__gppSafeCoalesce"},
			Index:    &TypeExpr{Type: plan.resultType},
		}
		return &CallExpr{
			Callee: callee,
			Arguments: []CallArg{
				{Value: plan.isNil},
				{Value: &FunctionLiteralExpr{Type: functionType, Body: &BlockStmt{Statements: []Stmt{&ReturnStmt{Values: []ExprNode{access}}}}}},
				{Value: &FunctionLiteralExpr{Type: functionType, Body: &BlockStmt{Statements: []Stmt{&ReturnStmt{Values: []ExprNode{fallback}}}}}},
			},
		}, nil
	}
	left, err := lowerCoalesceExpressionNode(binary.Left, context, valueTypes)
	if err != nil {
		return nil, err
	}
	right, err := lowerCoalesceExpressionNode(binary.Right, context, valueTypes)
	if err != nil {
		return nil, err
	}
	leftType := coalesceExpressionTypeAST(left, context, valueTypes)
	rightType := coalesceExpressionTypeAST(right, context, valueTypes)
	if leftType == nil || rightType == nil {
		return nil, fmt.Errorf("could not infer the value type for ??")
	}
	if isErrorTypeNode(leftType) {
		return nil, fmt.Errorf("?? requires a value-producing left operand")
	}
	left, err = lowerCoalesceOperandNode(left, context, valueTypes)
	if err != nil {
		return nil, err
	}
	right, err = lowerCoalesceOperandNode(right, context, valueTypes)
	if err != nil {
		return nil, err
	}
	functionType := &FunctionType{Results: []TypeNode{leftType}}
	callee := &IndexExpr{
		Receiver: &NameExpr{Name: "__gppCoalesce"},
		Index:    &TypeExpr{Type: leftType},
	}
	leftFunction := &FunctionLiteralExpr{
		Type: functionType,
		Body: &BlockStmt{Statements: []Stmt{&ReturnStmt{Values: []ExprNode{left}}}},
	}
	rightFunction := &FunctionLiteralExpr{
		Type: functionType,
		Body: &BlockStmt{Statements: []Stmt{&ReturnStmt{Values: []ExprNode{right}}}},
	}
	return &CallExpr{
		Callee: callee,
		Arguments: []CallArg{
			{Value: leftFunction},
			{Value: rightFunction},
		},
	}, nil
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

func lowerCoalesceOperandNode(expression ExprNode, context constructorContext, valueTypes map[string]string) (ExprNode, error) {
	if expression == nil {
		return nil, nil
	}
	lower := func(value ExprNode) (ExprNode, error) {
		return lowerCoalesceOperandNode(value, context, valueTypes)
	}
	switch value := expression.(type) {
	case *BinaryExpr:
		var err error
		value.Left, err = lower(value.Left)
		if err != nil {
			return nil, err
		}
		value.Right, err = lower(value.Right)
		return value, err
	case *UnaryExpr:
		lowered, err := lower(value.Operand)
		value.Operand = lowered
		return value, err
	case *ParenthesizedExpr:
		lowered, err := lower(value.Inner)
		value.Inner = lowered
		return value, err
	case *SelectorExpr:
		lowered, err := lower(value.Receiver)
		value.Receiver = lowered
		return value, err
	case *IndexExpr:
		var err error
		value.Receiver, err = lower(value.Receiver)
		if err != nil {
			return nil, err
		}
		value.Index, err = lower(value.Index)
		return value, err
	case *IndexListExpr:
		lowered, err := lower(value.Receiver)
		if err != nil {
			return nil, err
		}
		value.Receiver = lowered
		for index := range value.Indices {
			value.Indices[index], err = lower(value.Indices[index])
			if err != nil {
				return nil, err
			}
		}
		return value, nil
	case *SliceExpr:
		var err error
		value.Receiver, err = lower(value.Receiver)
		if err != nil {
			return nil, err
		}
		value.Low, err = lower(value.Low)
		if err != nil {
			return nil, err
		}
		value.High, err = lower(value.High)
		if err != nil {
			return nil, err
		}
		value.Max, err = lower(value.Max)
		return value, err
	case *CallExpr:
		if result, found := promotedCallForExpr(value, context, valueTypes); found && result.trailingError {
			nonErrorCount := len(result.types) - 1
			if nonErrorCount == 1 {
				return &CallExpr{Callee: &NameExpr{Name: "__gppUnwrap"}, Arguments: []CallArg{{Value: value}}}, nil
			}
			return nil, fmt.Errorf("direct exception coalescing does not support %d-result error promotion", len(result.types))
		}
	}
	return expression, nil
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
		if index, ok := value.Callee.(*IndexExpr); ok {
			if receiver, ok := index.Receiver.(*NameExpr); ok && (receiver.Name == "__gppCoalesce" || receiver.Name == "__gppSafeCoalesce") {
				if typeExpression, ok := index.Index.(*TypeExpr); ok {
					typeName, _ = typeNodeSource(typeExpression.Type)
					return typeName
				}
			}
		}
		typeName = coalesceCallResultType(value, context, valueTypes)
	}
	return firstCoalesceResultType(typeName)
}

// coalesceExpressionTypeAST is used by direct exception lowering. It keeps
// the type of common ?? operands as a TypeNode instead of rendering the type
// and parsing that text back into a node. The older string resolver above is
// retained for source-rewrite compatibility and for semantic environments
// that still expose value types as strings.
func coalesceExpressionTypeAST(expression ExprNode, context constructorContext, valueTypes map[string]string) TypeNode {
	if expression == nil {
		return nil
	}
	switch value := expression.(type) {
	case *LiteralExpr:
		switch value.Kind {
		case TokenString, TokenRawString:
			return &NamedType{Parts: []string{"string"}}
		case TokenNumber:
			if strings.ContainsAny(value.Text, ".eE") {
				return &NamedType{Parts: []string{"float64"}}
			}
			return &NamedType{Parts: []string{"int"}}
		case TokenRune:
			return &NamedType{Parts: []string{"rune"}}
		default:
			if value.Text == "true" || value.Text == "false" {
				return &NamedType{Parts: []string{"bool"}}
			}
		}
	case *NameExpr:
		if typeNode := context.CurrentParameterAST[value.Name]; typeNode != nil {
			return typeNode
		}
		if typeText := strings.TrimSpace(valueTypes[value.Name]); typeText != "" {
			return parseTypeText(typeText)
		}
		if value.Name == "true" || value.Name == "false" {
			return &NamedType{Parts: []string{"bool"}}
		}
	case *InterpolatedStringExpr:
		return &NamedType{Parts: []string{"string"}}
	case *ParenthesizedExpr:
		return coalesceExpressionTypeAST(value.Inner, context, valueTypes)
	case *SliceExpr:
		base := coalesceExpressionTypeAST(value.Receiver, context, valueTypes)
		if named, ok := base.(*NamedType); ok && len(named.Parts) == 1 && named.Parts[0] == "string" {
			return base
		}
		if slice, ok := base.(*SliceType); ok {
			return slice.Element
		}
	case *IndexExpr:
		base := coalesceExpressionTypeAST(value.Receiver, context, valueTypes)
		switch typed := base.(type) {
		case *SliceType:
			return typed.Element
		case *ArrayType:
			return typed.Element
		case *MapType:
			return typed.Value
		}
	case *TypeAssertExpr:
		if !value.TypeSwitch {
			return value.Type
		}
	case *PostfixExpr:
		return coalesceExpressionTypeAST(value.Expression, context, valueTypes)
	case *SpreadExpr:
		return coalesceExpressionTypeAST(value.Expression, context, valueTypes)
	case *TypeExpr:
		return value.Type
	case *SendExpr:
		return coalesceExpressionTypeAST(value.Value, context, valueTypes)
	case *FunctionLiteralExpr:
		return value.Type
	case *UnaryExpr:
		if value.Operator == "!" {
			return &NamedType{Parts: []string{"bool"}}
		}
		operand := coalesceExpressionTypeAST(value.Operand, context, valueTypes)
		if value.Operator == "&" && operand != nil {
			if _, pointer := operand.(*PointerType); !pointer {
				return &PointerType{Element: operand}
			}
		}
		return operand
	case *BinaryExpr:
		switch value.Operator {
		case "??":
			return coalesceExpressionTypeAST(value.Left, context, valueTypes)
		case "&&", "||", "==", "!=", "<", "<=", ">", ">=":
			return &NamedType{Parts: []string{"bool"}}
		default:
			return coalesceExpressionTypeAST(value.Left, context, valueTypes)
		}
	case *CompositeLiteralExpr:
		return value.Type
	case *SelectorExpr:
		receiverType := coalesceExpressionTypeAST(value.Receiver, context, valueTypes)
		receiverName := coalesceTypeName(receiverType)
		if target, ok := context.Targets[receiverName]; ok && target.Class != nil {
			for _, field := range target.Class.Fields {
				if field.Name == value.Name {
					return field.TypeAST
				}
			}
			for _, signature := range context.ClassMethodSignatures[receiverName][value.Name] {
				if signature.ResultAST != nil {
					return firstCoalesceResultTypeAST(signature.ResultAST)
				}
			}
		}
	case *CallExpr:
		if index, ok := value.Callee.(*IndexExpr); ok {
			if receiver, ok := index.Receiver.(*NameExpr); ok && (receiver.Name == "__gppCoalesce" || receiver.Name == "__gppSafeCoalesce") {
				if typeExpression, ok := index.Index.(*TypeExpr); ok {
					return typeExpression.Type
				}
			}
		}
		if index, ok := value.Callee.(*IndexListExpr); ok {
			if receiver, ok := index.Receiver.(*NameExpr); ok && (receiver.Name == "__gppCoalesce" || receiver.Name == "__gppSafeCoalesce") && len(index.Indices) > 0 {
				if typeExpression, ok := index.Indices[0].(*TypeExpr); ok {
					return typeExpression.Type
				}
			}
		}
		return coalesceCallResultTypeAST(value, context, valueTypes)
	}
	return nil
}

func coalesceCallResultTypeAST(call *CallExpr, context constructorContext, valueTypes map[string]string) TypeNode {
	if call == nil {
		return nil
	}
	var candidates []callableSignature
	switch callee := call.Callee.(type) {
	case *NameExpr:
		candidates = context.FunctionSignatures[callee.Name]
	case *SelectorExpr:
		receiverName := coalesceTypeName(coalesceExpressionTypeAST(callee.Receiver, context, valueTypes))
		if _, isClass := context.Targets[receiverName]; isClass {
			candidates = context.StaticMethodSignatures[receiverName][callee.Name]
		} else {
			candidates = context.ClassMethodSignatures[receiverName][callee.Name]
		}
		if len(candidates) == 0 {
			for _, extension := range context.Extensions {
				if extension.Method.Name == callee.Name && extensionTargetMatches(extension.Target, extension.ReceiverType, receiverName, context) {
					return firstCoalesceResultTypeAST(methodResultTypeNode(extension.Method))
				}
			}
		}
	}
	for _, candidate := range candidates {
		if len(call.Arguments) >= requiredParameterCount(candidate) && len(call.Arguments) <= len(candidate.Parameters) {
			return firstCoalesceResultTypeAST(candidate.ResultAST)
		}
	}
	return nil
}

func firstCoalesceResultTypeAST(typeNode TypeNode) TypeNode {
	if tuple, ok := typeNode.(*TupleType); ok {
		if len(tuple.Elements) == 0 {
			return nil
		}
		return tuple.Elements[0]
	}
	return typeNode
}

func coalesceTypeName(typeNode TypeNode) string {
	for {
		switch value := typeNode.(type) {
		case *PointerType:
			typeNode = value.Element
			continue
		case *NamedType:
			return strings.Join(value.Parts, ".")
		}
		return ""
	}
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
	case *AssignmentExpr:
		for _, expression := range value.Left {
			collectCoalesceExprs(expression, result)
		}
		for _, expression := range value.Right {
			collectCoalesceExprs(expression, result)
		}
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
