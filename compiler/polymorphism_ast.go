package compiler

import (
	"sort"
	"strings"
)

// transformPolymorphicDeclarationsAST lowers dispatch-sensitive declarations
// and calls from the structured Go++ body tree. The legacy implementation in
// emitter.go remains below as a compatibility fallback for source fragments
// that the body parser cannot represent yet.
func transformPolymorphicDeclarationsAST(src string, context constructorContext) (string, bool, error) {
	if len(context.Targets) == 0 {
		return src, true, nil
	}
	blocks := []*BlockStmt{}
	functions := parseTopLevelFunctions("polymorphism", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function == nil || function.Method.BodyAST == nil {
				return src, false, nil
			}
			blocks = append(blocks, function.Method.BodyAST)
		}
	} else {
		tokens, err := LexSource("polymorphism", src)
		if err != nil {
			return src, false, nil
		}
		block, err := ParseBodyAST(tokens)
		if err != nil || block == nil {
			return src, false, nil
		}
		blocks = append(blocks, block)
	}

	edits := []polymorphismASTEdit{}
	addEdit := func(start, end int, text string) {
		if start < 0 || end < start || end > len(src) {
			return
		}
		for _, existing := range edits {
			if existing.start == start && existing.end == end && existing.text == text {
				return
			}
		}
		edits = append(edits, polymorphismASTEdit{start: start, end: end, text: text})
	}
	addPointer := func(expression ExprNode) {
		if expression == nil {
			return
		}
		if unary, ok := expression.(*UnaryExpr); ok && unary.Operator == "&" {
			return
		}
		span := expression.Span()
		addEdit(span.Start, span.Start, "&")
	}

	if len(functions) > 0 {
		for _, function := range functions {
			methodContext := context
			methodContext.CurrentParameterTypes = polymorphismParameterTypes(function.Method.ParameterAST, context.CurrentParameterTypes)
			valueTypes := polymorphismValueTypesAST(function.Method.BodyAST, methodContext)
			for _, parameter := range function.Method.ParameterAST {
				base, ok := dispatchTargetForTypeNode(parameter.Type, context)
				if !ok {
					continue
				}
				start, end, found := parameterTypeSpan(src, function.Method, parameter)
				if found {
					addEdit(start, end, dispatchInterfaceType(base))
				}
			}
			polymorphismFunctionResultEdits(function.Method, function.Method.BodyAST, valueTypes, methodContext, addEdit, addPointer)
			polymorphismBodyEdits(function.Method.BodyAST, valueTypes, methodContext, addEdit, addPointer)
		}
	} else {
		valueTypes := polymorphismValueTypesAST(blocks[0], context)
		polymorphismBodyResultEdits(blocks[0], valueTypes, context, addPointer)
		polymorphismBodyEdits(blocks[0], valueTypes, context, addEdit, addPointer)
	}

	sort.SliceStable(edits, func(left, right int) bool {
		if edits[left].start == edits[right].start {
			return edits[left].end > edits[right].end
		}
		return edits[left].start > edits[right].start
	})
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

type polymorphismASTEdit struct {
	start int
	end   int
	text  string
}

func polymorphismParameterTypes(parameters []ParameterNode, inherited map[string]string) map[string]string {
	result := map[string]string{}
	for name, typeName := range inherited {
		result[name] = typeName
	}
	for _, parameter := range parameters {
		if parameter.Name == "" || parameter.Type == nil {
			continue
		}
		if typeName, err := typeNodeSource(parameter.Type); err == nil {
			result[parameter.Name] = strings.TrimSpace(typeName)
		}
	}
	return result
}

func parameterTypeSpan(src string, method Method, parameter ParameterNode) (int, int, bool) {
	if method.ParametersSpan.Start < 0 || method.ParametersSpan.End > len(src) || method.ParametersSpan.Start > method.ParametersSpan.End {
		return 0, 0, false
	}
	parameterSource := src[method.ParametersSpan.Start:method.ParametersSpan.End]
	parts, err := splitTopLevel(parameterSource, ',')
	if err != nil {
		return 0, 0, false
	}
	parameterIndex := 0
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if parameterIndex >= len(method.ParameterAST) {
			return 0, 0, false
		}
		candidate := method.ParameterAST[parameterIndex]
		parameterIndex++
		if candidate.Name != parameter.Name {
			continue
		}
		typeText, typeErr := typeNodeSource(parameter.Type)
		if typeErr != nil || typeText == "" {
			return 0, 0, false
		}
		partStart := strings.Index(parameterSource, part)
		if partStart < 0 {
			return 0, 0, false
		}
		search := trimmed
		if parameter.Name != "" {
			nameAt := strings.Index(search, parameter.Name)
			if nameAt >= 0 {
				search = search[nameAt+len(parameter.Name):]
			}
		}
		typeAt := strings.Index(search, typeText)
		if typeAt < 0 {
			return 0, 0, false
		}
		trimmedOffset := strings.Index(part, trimmed)
		start := method.ParametersSpan.Start + partStart + trimmedOffset + (len(trimmed) - len(search)) + typeAt
		return start, start + len(typeText), true
	}
	return 0, 0, false
}

func dispatchTargetForTypeNode(typeNode TypeNode, context constructorContext) (constructorTarget, bool) {
	if typeNode == nil {
		return constructorTarget{}, false
	}
	typeName, err := typeNodeSource(typeNode)
	if err != nil {
		return constructorTarget{}, false
	}
	return dispatchTargetForTypeName(strings.TrimSpace(typeName), context)
}

func polymorphismFunctionResultEdits(method Method, body *BlockStmt, valueTypes map[string]string, context constructorContext, addEdit func(int, int, string), addPointer func(ExprNode)) {
	if method.ResultAST == nil {
		return
	}
	resultText, err := typeNodeSource(method.ResultAST)
	if err != nil || strings.TrimSpace(resultText) == "" {
		return
	}
	base, dispatchResult := dispatchTargetForTypeNode(method.ResultAST, context)
	pointerResult := transformPolymorphicResultType(resultText, context)
	if pointerResult != resultText || dispatchResult {
		if dispatchResult {
			addEdit(method.ResultSpan.Start, method.ResultSpan.End, dispatchInterfaceType(base))
		} else if pointerResult != resultText {
			addEdit(method.ResultSpan.Start, method.ResultSpan.End, pointerResult)
		}
	}
	if body == nil {
		return
	}
	collectPolymorphismReturns(body, func(statement *ReturnStmt) {
		if statement == nil || len(statement.Values) != 1 {
			return
		}
		value := statement.Values[0]
		derivedName := polymorphismExpressionClassName(value, context, valueTypes)
		if derivedName == "" {
			return
		}
		derived, ok := context.Targets[derivedName]
		if dispatchResult && (!ok || !sameConstructorPackage(base, derived) ||
			(derived.Class != base.Class && !classInherits(derived.Class, base.Class, derived.Classes, map[string]bool{}))) {
			return
		}
		if dispatchResult || pointerResult != resultText {
			if !strings.HasPrefix(strings.TrimSpace(polymorphismExpressionType(value, context, valueTypes)), "*") {
				addPointer(value)
			}
		}
	})
}

func polymorphismBodyEdits(block *BlockStmt, valueTypes map[string]string, context constructorContext, addEdit func(int, int, string), addPointer func(ExprNode)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		polymorphismStatementEdits(statement, valueTypes, context, addEdit, addPointer)
	}
}

func polymorphismStatementEdits(statement Stmt, valueTypes map[string]string, context constructorContext, addEdit func(int, int, string), addPointer func(ExprNode)) {
	if statement == nil {
		return
	}
	switch value := statement.(type) {
	case *TokenStmt:
		polymorphismExpressionEdits(value.Exprs, valueTypes, context, addEdit, addPointer)
		polymorphismBodyEdits(value.Body, valueTypes, context, addEdit, addPointer)
		for _, child := range value.Children {
			polymorphismStatementEdits(child, valueTypes, context, addEdit, addPointer)
		}
	case *DeclarationStmt:
		if len(value.Names) == 1 && len(value.Values) == 1 && value.Type != nil {
			if base, ok := dispatchTargetForTypeNode(value.Type, context); ok {
				derivedName := polymorphismExpressionClassName(value.Values[0], context, valueTypes)
				if polymorphismCompatibleDerived(derivedName, base, context) {
					if typeName := dispatchInterfaceType(base); typeName != "" {
						span := value.Type.Span()
						addEdit(span.Start, span.End, typeName)
					}
					if !strings.HasPrefix(strings.TrimSpace(polymorphismExpressionType(value.Values[0], context, valueTypes)), "*") {
						addPointer(value.Values[0])
					}
				}
			}
		}
		polymorphismExpressionEdits(value.Values, valueTypes, context, addEdit, addPointer)
	case *AssignmentStmt:
		polymorphismExpressionEdits(value.Left, valueTypes, context, addEdit, addPointer)
		polymorphismExpressionEdits(value.Right, valueTypes, context, addEdit, addPointer)
	case *ReturnStmt:
		polymorphismExpressionEdits(value.Values, valueTypes, context, addEdit, addPointer)
	case *ExpressionStmt:
		polymorphismExpressionEdits([]ExprNode{value.Expression}, valueTypes, context, addEdit, addPointer)
	case *IfStmt:
		polymorphismExpressionEdits([]ExprNode{value.Init, value.Condition}, valueTypes, context, addEdit, addPointer)
		polymorphismBodyEdits(value.Body, valueTypes, context, addEdit, addPointer)
		polymorphismBodyEdits(value.Else, valueTypes, context, addEdit, addPointer)
		if value.ElseIf != nil {
			polymorphismStatementEdits(value.ElseIf, valueTypes, context, addEdit, addPointer)
		}
	case *ForStmt:
		polymorphismExpressionEdits([]ExprNode{value.Init, value.Condition, value.Post, value.RangeExpr}, valueTypes, context, addEdit, addPointer)
		polymorphismBodyEdits(value.Body, valueTypes, context, addEdit, addPointer)
	case *SwitchStmt:
		polymorphismExpressionEdits([]ExprNode{value.Init, value.Tag}, valueTypes, context, addEdit, addPointer)
		polymorphismBodyEdits(value.Body, valueTypes, context, addEdit, addPointer)
	case *CaseStmt:
		polymorphismExpressionEdits(value.Clause.Expressions, valueTypes, context, addEdit, addPointer)
		polymorphismBodyEdits(value.Clause.Body, valueTypes, context, addEdit, addPointer)
	case *TryStmt:
		polymorphismBodyEdits(value.Body, valueTypes, context, addEdit, addPointer)
		for _, clause := range value.Catches {
			polymorphismBodyEdits(clause.Body, valueTypes, context, addEdit, addPointer)
		}
		polymorphismBodyEdits(value.Finally, valueTypes, context, addEdit, addPointer)
	case *ThrowStmt:
		polymorphismExpressionEdits([]ExprNode{value.Value}, valueTypes, context, addEdit, addPointer)
	case *DeferStmt:
		polymorphismExpressionEdits([]ExprNode{value.Expression}, valueTypes, context, addEdit, addPointer)
	case *GoStmt:
		polymorphismExpressionEdits([]ExprNode{value.Expression}, valueTypes, context, addEdit, addPointer)
	case *SendStmt:
		polymorphismExpressionEdits([]ExprNode{value.Channel, value.Value}, valueTypes, context, addEdit, addPointer)
	}
}

func polymorphismBodyResultEdits(block *BlockStmt, valueTypes map[string]string, context constructorContext, addPointer func(ExprNode)) {
	if block == nil || context.CurrentResultAST == nil {
		return
	}
	resultText, err := typeNodeSource(context.CurrentResultAST)
	if err != nil || strings.TrimSpace(resultText) == "" {
		return
	}
	pointerResult := transformPolymorphicResultType(resultText, context)
	if pointerResult == resultText {
		return
	}
	collectPolymorphismReturns(block, func(statement *ReturnStmt) {
		if statement == nil || len(statement.Values) != 1 {
			return
		}
		value := statement.Values[0]
		if polymorphismExpressionClassName(value, context, valueTypes) == "" {
			return
		}
		if !strings.HasPrefix(strings.TrimSpace(polymorphismExpressionType(value, context, valueTypes)), "*") {
			addPointer(value)
		}
	})
}

func polymorphismExpressionEdits(expressions []ExprNode, valueTypes map[string]string, context constructorContext, addEdit func(int, int, string), addPointer func(ExprNode)) {
	for _, expression := range expressions {
		collectPolymorphismExpressions(expression, func(candidate ExprNode) {
			call, ok := candidate.(*CallExpr)
			if !ok {
				return
			}
			parameterTypes := polymorphismCallParameterTypes(call, context, valueTypes)
			for index, argument := range call.Arguments {
				if index < len(parameterTypes) && polymorphismShouldPointerCoerce(parameterTypes[index], argument.Value, context, valueTypes) {
					addPointer(argument.Value)
				}
			}
		})
	}
}

func collectPolymorphismExpressions(expression ExprNode, visit func(ExprNode)) {
	if expression == nil || visit == nil {
		return
	}
	visit(expression)
	switch value := expression.(type) {
	case *UnaryExpr:
		collectPolymorphismExpressions(value.Operand, visit)
	case *BinaryExpr:
		collectPolymorphismExpressions(value.Left, visit)
		collectPolymorphismExpressions(value.Right, visit)
	case *AssignmentExpr:
		for _, expression := range value.Left {
			collectPolymorphismExpressions(expression, visit)
		}
		for _, expression := range value.Right {
			collectPolymorphismExpressions(expression, visit)
		}
	case *SelectorExpr:
		collectPolymorphismExpressions(value.Receiver, visit)
	case *IndexExpr:
		collectPolymorphismExpressions(value.Receiver, visit)
		collectPolymorphismExpressions(value.Index, visit)
	case *IndexListExpr:
		collectPolymorphismExpressions(value.Receiver, visit)
		for _, index := range value.Indices {
			collectPolymorphismExpressions(index, visit)
		}
	case *SliceExpr:
		collectPolymorphismExpressions(value.Receiver, visit)
		collectPolymorphismExpressions(value.Low, visit)
		collectPolymorphismExpressions(value.High, visit)
		collectPolymorphismExpressions(value.Max, visit)
	case *TypeAssertExpr:
		collectPolymorphismExpressions(value.Expression, visit)
	case *PostfixExpr:
		collectPolymorphismExpressions(value.Expression, visit)
	case *SpreadExpr:
		collectPolymorphismExpressions(value.Expression, visit)
	case *SendExpr:
		collectPolymorphismExpressions(value.Channel, visit)
		collectPolymorphismExpressions(value.Value, visit)
	case *CallExpr:
		collectPolymorphismExpressions(value.Callee, visit)
		for _, argument := range value.Arguments {
			collectPolymorphismExpressions(argument.Value, visit)
		}
	case *ParenthesizedExpr:
		collectPolymorphismExpressions(value.Inner, visit)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectPolymorphismExpressions(element.Key, visit)
			collectPolymorphismExpressions(element.Value, visit)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectPolymorphismExpressions(segment.Expression, visit)
		}
	case *LambdaExpr:
		collectPolymorphismExpressions(value.Body, visit)
		collectPolymorphismBlock(value.BlockBody, visit)
	case *FunctionLiteralExpr:
		collectPolymorphismBlock(value.Body, visit)
	}
}

func collectPolymorphismBlock(block *BlockStmt, visit func(ExprNode)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		walkStmtExpressions(statement, func(expression ExprNode) {
			collectPolymorphismExpressions(expression, visit)
		})
	}
}

func polymorphismValueTypesAST(block *BlockStmt, context constructorContext) map[string]string {
	result := polymorphismParameterTypes(nil, context.CurrentParameterTypes)
	if block == nil {
		return result
	}
	var visit func(*BlockStmt)
	visit = func(current *BlockStmt) {
		if current == nil {
			return
		}
		for _, statement := range current.Statements {
			switch value := statement.(type) {
			case *DeclarationStmt:
				declared := ""
				if value.Type != nil {
					declared, _ = typeNodeSource(value.Type)
					if base, ok := dispatchTargetForTypeNode(value.Type, context); ok {
						declared = dispatchInterfaceType(base)
					}
				}
				for index, name := range value.Names {
					inferred := declared
					if inferred == "" && index < len(value.Values) {
						inferred = polymorphismExpressionType(value.Values[index], context, result)
						if call, ok := value.Values[index].(*CallExpr); ok {
							if _, _, isConstructor := constructorTargetForCallee(call.Callee, context); isConstructor && inferred != "" && !strings.HasPrefix(inferred, "*") {
								inferred = "*" + inferred
							}
						}
					}
					if inferred != "" {
						result[name.Text] = inferred
					}
				}
			case *AssignmentStmt:
				for index, left := range value.Left {
					name, ok := left.(*NameExpr)
					if ok && index < len(value.Right) {
						if inferred := polymorphismExpressionType(value.Right[index], context, result); inferred != "" {
							result[name.Name] = inferred
						}
					}
				}
			case *IfStmt:
				visit(value.Body)
				visit(value.Else)
				if value.ElseIf != nil {
					visit(&BlockStmt{Statements: []Stmt{value.ElseIf}})
				}
			case *ForStmt:
				visit(value.Body)
			case *SwitchStmt:
				visit(value.Body)
			case *CaseStmt:
				visit(value.Clause.Body)
			case *TryStmt:
				visit(value.Body)
				for _, clause := range value.Catches {
					visit(clause.Body)
				}
				visit(value.Finally)
			}
		}
	}
	visit(block)
	return result
}

func polymorphismExpressionClassName(expression ExprNode, context constructorContext, valueTypes map[string]string) string {
	typeName := strings.TrimPrefix(strings.TrimSpace(polymorphismExpressionType(expression, context, valueTypes)), "*")
	if _, ok := context.Targets[typeName]; ok {
		return typeName
	}
	for key, target := range context.Targets {
		if target.Class != nil && target.Class.Name == typeName {
			return key
		}
	}
	return ""
}

func polymorphismExpressionType(expression ExprNode, context constructorContext, valueTypes map[string]string) string {
	if expression == nil {
		return ""
	}
	switch value := expression.(type) {
	case *NameExpr:
		return valueTypes[value.Name]
	case *LiteralExpr:
		switch value.Kind {
		case TokenString, TokenRawString:
			return "string"
		case TokenNumber:
			if strings.ContainsAny(value.Text, ".eEpP") {
				return "float64"
			}
			return "int"
		case TokenRune:
			return "rune"
		}
	case *InterpolatedStringExpr:
		return "string"
	case *ParenthesizedExpr:
		return polymorphismExpressionType(value.Inner, context, valueTypes)
	case *UnaryExpr:
		inner := polymorphismExpressionType(value.Operand, context, valueTypes)
		if value.Operator == "&" && inner != "" && !strings.HasPrefix(inner, "*") {
			return "*" + inner
		}
		return inner
	case *BinaryExpr:
		if value.Operator == "&&" || value.Operator == "||" || value.Operator == "==" || value.Operator == "!=" || value.Operator == "<" || value.Operator == "<=" || value.Operator == ">" || value.Operator == ">=" {
			return "bool"
		}
		return polymorphismExpressionType(value.Left, context, valueTypes)
	case *PostfixExpr:
		return polymorphismExpressionType(value.Expression, context, valueTypes)
	case *SpreadExpr:
		return polymorphismExpressionType(value.Expression, context, valueTypes)
	case *TypeAssertExpr:
		if !value.TypeSwitch && value.Type != nil {
			result, _ := typeNodeSource(value.Type)
			return result
		}
	case *CompositeLiteralExpr:
		result, _ := typeNodeSource(value.Type)
		return result
	case *SelectorExpr:
		base := strings.TrimPrefix(strings.TrimSpace(polymorphismExpressionType(value.Receiver, context, valueTypes)), "*")
		if target, ok := context.Targets[base]; ok && target.Class != nil {
			for _, field := range target.Class.Fields {
				if field.Name == value.Name {
					return transformPolymorphicType(fieldTypeSource(field), context)
				}
			}
			for _, signature := range context.ClassMethodSignatures[base][value.Name] {
				if signature.resultText() != "" {
					return signature.resultText()
				}
			}
		}
	case *CallExpr:
		if constructorName, _, isConstructor := constructorTargetForCallee(value.Callee, context); isConstructor {
			return constructorName
		}
		return polymorphismCallResultType(value, context, valueTypes)
	}
	return ""
}

func polymorphismCallResultType(call *CallExpr, context constructorContext, valueTypes map[string]string) string {
	if call == nil {
		return ""
	}
	var candidates []callableSignature
	switch callee := call.Callee.(type) {
	case *NameExpr:
		if target, ok := context.Targets[callee.Name]; ok && target.Class != nil {
			return callee.Name
		}
		candidates = context.FunctionSignatures[callee.Name]
	case *SelectorExpr:
		receiverType := strings.TrimPrefix(strings.TrimSpace(polymorphismExpressionType(callee.Receiver, context, valueTypes)), "*")
		if receiverName, isName := callee.Receiver.(*NameExpr); isName {
			if _, isStaticClass := context.Targets[receiverName.Name]; isStaticClass && valueTypes[receiverName.Name] == "" {
				receiverType = receiverName.Name
				candidates = context.StaticMethodSignatures[receiverType][callee.Name]
			} else {
				candidates = context.ClassMethodSignatures[receiverType][callee.Name]
			}
		} else {
			candidates = context.ClassMethodSignatures[receiverType][callee.Name]
		}
	}
	for _, candidate := range candidates {
		if len(call.Arguments) < requiredParameterCount(candidate) || len(call.Arguments) > len(candidate.Parameters) {
			continue
		}
		return transformPolymorphicResultType(candidate.resultText(), context)
	}
	return ""
}

func polymorphismCallParameterTypes(call *CallExpr, context constructorContext, valueTypes map[string]string) []string {
	if call == nil {
		return nil
	}
	var candidates []callableSignature
	switch callee := call.Callee.(type) {
	case *NameExpr:
		candidates = context.FunctionSignatures[callee.Name]
	case *SelectorExpr:
		receiver := strings.TrimPrefix(strings.TrimSpace(polymorphismExpressionType(callee.Receiver, context, valueTypes)), "*")
		if name, ok := callee.Receiver.(*NameExpr); ok && name.Name == "this" && context.CurrentClass != "" {
			receiver = context.CurrentClass
		}
		if name, isName := callee.Receiver.(*NameExpr); isName && name.Name != "this" && valueTypes[name.Name] == "" {
			if _, isClass := context.Targets[name.Name]; isClass {
				receiver = name.Name
				candidates = context.StaticMethodSignatures[receiver][callee.Name]
			} else {
				candidates = context.ClassMethodSignatures[receiver][callee.Name]
			}
		} else {
			candidates = context.ClassMethodSignatures[receiver][callee.Name]
		}
	}
	for _, candidate := range candidates {
		if len(candidate.Parameters) != len(call.Arguments) {
			continue
		}
		matches := true
		result := make([]string, len(candidate.Parameters))
		for index, parameter := range candidate.Parameters {
			result[index] = parameter.typeText()
			actual := polymorphismExpressionType(call.Arguments[index].Value, context, valueTypes)
			if actual != "" && actual != result[index] && !polymorphismAssignable(actual, result[index], context) {
				matches = false
				break
			}
		}
		if matches {
			return result
		}
	}
	return nil
}

func polymorphismAssignable(actual, expected string, context constructorContext) bool {
	actualName := strings.TrimPrefix(strings.TrimSpace(actual), "*")
	expectedName := strings.TrimPrefix(strings.TrimSpace(expected), "*")
	actualTarget, actualOK := context.Targets[actualName]
	expectedTarget, expectedOK := context.Targets[expectedName]
	if !actualOK || !expectedOK {
		return false
	}
	return classInheritsTarget(actualTarget, expectedTarget, context)
}

func polymorphismShouldPointerCoerce(expected string, argument ExprNode, context constructorContext, valueTypes map[string]string) bool {
	base, ok := dispatchTargetForTypeName(expected, context)
	if !ok {
		return false
	}
	actualType := polymorphismExpressionType(argument, context, valueTypes)
	if strings.HasPrefix(strings.TrimSpace(actualType), "*") {
		return false
	}
	actualName := strings.TrimPrefix(strings.TrimSpace(actualType), "*")
	actual, ok := context.Targets[actualName]
	if !ok || !sameConstructorPackage(base, actual) {
		return false
	}
	return actual.Class == base.Class || classInherits(actual.Class, base.Class, actual.Classes, map[string]bool{})
}

func polymorphismCompatibleDerived(name string, base constructorTarget, context constructorContext) bool {
	derived, ok := context.Targets[name]
	if !ok || !sameConstructorPackage(base, derived) {
		return false
	}
	return derived.Class == base.Class || classInherits(derived.Class, base.Class, derived.Classes, map[string]bool{})
}

// lowerPolymorphismBlockNode applies the dispatch adjustments that were
// historically performed by source edits. It mutates a fresh body tree owned
// by a lowering pass: interface-typed locals are rewritten to their generated
// dispatch interface, derived values are address-taken where required, and
// polymorphic call arguments receive the same coercion.
func lowerPolymorphismBlockNode(block *BlockStmt, context constructorContext) error {
	if block == nil || len(context.Targets) == 0 {
		return nil
	}
	valueTypes := polymorphismValueTypesAST(block, context)
	resultText := ""
	if context.CurrentResultAST != nil {
		resultText, _ = typeNodeSource(context.CurrentResultAST)
	}
	resultText = strings.TrimSpace(resultText)
	resultBase, dispatchResult := dispatchTargetForTypeNode(context.CurrentResultAST, context)
	pointerResult := transformPolymorphicResultType(resultText, context)
	resultNeedsPointer := pointerResult != resultText || dispatchResult

	var lowerStatement func(Stmt) error
	var lowerBlock func(*BlockStmt) error
	lowerExpression := func(expression ExprNode) {
		collectPolymorphismExpressions(expression, func(candidate ExprNode) {
			call, ok := candidate.(*CallExpr)
			if !ok {
				return
			}
			parameterTypes := polymorphismCallParameterTypes(call, context, valueTypes)
			for index := range call.Arguments {
				if index >= len(parameterTypes) || !polymorphismShouldPointerCoerce(parameterTypes[index], call.Arguments[index].Value, context, valueTypes) {
					continue
				}
				call.Arguments[index].Value = polymorphismAddressOf(call.Arguments[index].Value)
			}
		})
	}
	lowerDeclaration := func(statement *DeclarationStmt) {
		if statement == nil || len(statement.Names) != 1 || len(statement.Values) != 1 || statement.Type == nil {
			return
		}
		base, ok := dispatchTargetForTypeNode(statement.Type, context)
		if !ok {
			return
		}
		derived := polymorphismExpressionClassName(statement.Values[0], context, valueTypes)
		if !polymorphismCompatibleDerived(derived, base, context) {
			return
		}
		if interfaceName := dispatchInterfaceType(base); interfaceName != "" {
			statement.Type = &NamedType{Parts: strings.Split(interfaceName, ".")}
			// Keep the local type environment in sync with the declaration
			// rewrite. Subsequent calls must see `person` as the dispatch
			// interface, not as the original class value; otherwise the call
			// lowering incorrectly emits `&person` (a pointer to an interface).
			valueTypes[statement.Names[0].Text] = interfaceName
		}
		if !strings.HasPrefix(strings.TrimSpace(polymorphismExpressionType(statement.Values[0], context, valueTypes)), "*") {
			statement.Values[0] = polymorphismAddressOf(statement.Values[0])
		}
	}
	lowerReturns := func(statement Stmt) {
		returnStatement, ok := statement.(*ReturnStmt)
		if !ok || !resultNeedsPointer || len(returnStatement.Values) != 1 {
			return
		}
		value := returnStatement.Values[0]
		derived := polymorphismExpressionClassName(value, context, valueTypes)
		if derived == "" {
			return
		}
		if dispatchResult && !polymorphismCompatibleDerived(derived, resultBase, context) {
			return
		}
		returnStatement.Values[0] = polymorphismAddressOf(value)
	}
	lowerStatement = func(statement Stmt) error {
		if statement == nil || isNilStmt(statement) {
			return nil
		}
		walkStmtExpressions(statement, lowerExpression)
		lowerReturns(statement)
		switch value := statement.(type) {
		case *DeclarationStmt:
			lowerDeclaration(value)
		case *TokenStmt:
			if err := lowerBlock(value.Body); err != nil {
				return err
			}
			for _, child := range value.Children {
				if err := lowerStatement(child); err != nil {
					return err
				}
			}
		case *IfStmt:
			if err := lowerBlock(value.Body); err != nil {
				return err
			}
			if err := lowerBlock(value.Else); err != nil {
				return err
			}
			if err := lowerStatement(value.ElseIf); err != nil {
				return err
			}
		case *ForStmt:
			if err := lowerBlock(value.Body); err != nil {
				return err
			}
		case *SwitchStmt:
			if err := lowerBlock(value.Body); err != nil {
				return err
			}
		case *CaseStmt:
			if err := lowerBlock(value.Clause.Body); err != nil {
				return err
			}
		case *TryStmt:
			if err := lowerBlock(value.Body); err != nil {
				return err
			}
			for _, clause := range value.Catches {
				if err := lowerBlock(clause.Body); err != nil {
					return err
				}
			}
			if err := lowerBlock(value.Finally); err != nil {
				return err
			}
		case *BlockStmt:
			return lowerBlock(value)
		}
		return nil
	}
	lowerBlock = func(current *BlockStmt) error {
		if current == nil {
			return nil
		}
		for _, statement := range current.Statements {
			if err := lowerStatement(statement); err != nil {
				return err
			}
		}
		return nil
	}
	return lowerBlock(block)
}

func polymorphismAddressOf(expression ExprNode) ExprNode {
	if expression == nil {
		return nil
	}
	if unary, ok := expression.(*UnaryExpr); ok && unary.Operator == "&" {
		return expression
	}
	return &UnaryExpr{Operator: "&", Operand: expression, SpanValue: expression.Span()}
}

func collectPolymorphismReturns(block *BlockStmt, visit func(*ReturnStmt)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		switch value := statement.(type) {
		case *ReturnStmt:
			visit(value)
		case *TokenStmt:
			collectPolymorphismReturns(value.Body, visit)
			for _, child := range value.Children {
				collectPolymorphismReturns(&BlockStmt{Statements: []Stmt{child}}, visit)
			}
		case *IfStmt:
			collectPolymorphismReturns(value.Body, visit)
			collectPolymorphismReturns(value.Else, visit)
			if value.ElseIf != nil {
				collectPolymorphismReturns(&BlockStmt{Statements: []Stmt{value.ElseIf}}, visit)
			}
		case *ForStmt:
			collectPolymorphismReturns(value.Body, visit)
		case *SwitchStmt:
			collectPolymorphismReturns(value.Body, visit)
		case *CaseStmt:
			collectPolymorphismReturns(value.Clause.Body, visit)
		case *TryStmt:
			collectPolymorphismReturns(value.Body, visit)
			for _, clause := range value.Catches {
				collectPolymorphismReturns(clause.Body, visit)
			}
			collectPolymorphismReturns(value.Finally, visit)
		}
	}
}
