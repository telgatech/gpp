package compiler

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/types"
	"path"
	"sort"
	"strconv"
	"strings"
)

type extensionMethod struct {
	Target            string
	TargetConstraints map[string]TypeNode
	ReceiverType      string
	Qualifier         string
	GoName            string
	SourceFile        string
	SourceLine        int
	Method            Method
	Prelude           bool
}

func extensionMethodsForDeclarations(declarations []*ExtendDecl, qualifier string) []extensionMethod {
	methods := []extensionMethod{}
	for _, declaration := range declarations {
		for _, rawTarget := range extensionTargetNames(declaration) {
			target := normalizeExtensionTarget(rawTarget)
			for _, method := range declaration.Methods {
				methods = append(methods, extensionMethod{
					Target:            target,
					TargetConstraints: cloneTypeNodeMap(declaration.TargetConstraints),
					ReceiverType:      extensionReceiverType(target, nil),
					Qualifier:         qualifier,
					GoName:            extensionGoName(target, method),
					Method:            method,
				})
			}
		}
	}
	return methods
}

func extensionReceiverType(target string, classes map[string]*ClassDecl) string {
	name := strings.TrimSpace(target)
	if strings.HasPrefix(name, "*") {
		return name
	}
	// Interfaces must remain interfaces when emitted as extension receivers;
	// taking a pointer to one would make ordinary interface values ineligible.
	// These are the standard-library interface targets provided by the prelude.
	if name == "error" || name == "io.Reader" || name == "io.Writer" {
		return name
	}
	if classes != nil {
		if _, ok := classes[name]; ok {
			return "*" + name
		}
	}
	if extensionValueType(name) {
		return name
	}
	return "*" + name
}

func extensionValueType(name string) bool {
	name = strings.TrimSpace(name)
	if name == "string" || name == "bool" || name == "byte" || name == "rune" ||
		name == "any" || name == "interface{}" {
		return true
	}
	for _, prefix := range []string{"int", "uint", "float", "complex"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return strings.HasPrefix(name, "[]") || strings.HasPrefix(name, "map[") ||
		strings.HasPrefix(name, "chan ") || strings.HasPrefix(name, "func(") ||
		strings.HasPrefix(name, "interface{")
}

func extensionGoName(target string, method Method) string {
	base := strings.NewReplacer(".", "_", "*", "ptr_", "[", "_", "]", "_").Replace(strings.TrimSpace(target))
	base = sanitizeExtensionName(base)
	hash := sha256.Sum256([]byte(strings.TrimSpace(target) + "\x00" + method.Name + "\x00" + methodTypeParamsSource(method) + "\x00" + methodParametersSource(method)))
	return fmt.Sprintf("GppExt_%s_%s_%x", base, method.Name, hash[:4])
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneTypeNodeMap(values map[string]TypeNode) map[string]TypeNode {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]TypeNode, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func sanitizeExtensionName(name string) string {
	var output strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' {
			output.WriteRune(r)
		} else {
			output.WriteByte('_')
		}
	}
	if output.Len() == 0 {
		return "type"
	}
	return output.String()
}

func transformExtensions(src string, context constructorContext) (string, error) {
	return transformExtensionsWithTypes(src, context, nil)
}

func transformExtensionsWithTypes(src string, context constructorContext, inheritedTypes map[string]string) (string, error) {
	if len(context.Extensions) == 0 {
		return src, nil
	}
	if inheritedTypes == nil {
		inheritedTypes = context.CurrentParameterTypes
	}
	if transformed, handled, err := transformExtensionsAST(src, context, inheritedTypes); handled {
		return transformed, err
	}
	return src, nil
}

type extensionASTEdit struct {
	start int
	end   int
	text  string
}

type extensionASTBody struct {
	block *BlockStmt
	types map[string]string
}

func transformExtensionsAST(src string, context constructorContext, inheritedTypes map[string]string) (string, bool, error) {
	tokens, err := LexSource("extensions", src)
	if err != nil {
		return src, false, nil
	}
	bodies := []extensionASTBody{}
	functions := parseTopLevelFunctions("extensions", "main", src, 0, src, "", 0)
	for _, function := range functions {
		if function == nil || function.Method.BodyAST == nil {
			continue
		}
		types := cloneStringMap(context.GlobalValueTypes)
		if types == nil {
			types = map[string]string{}
		}
		for name, typeName := range inheritedTypes {
			types[name] = typeName
		}
		for _, parameter := range function.Method.ParameterAST {
			if parameter.Type == nil {
				continue
			}
			if typeName, typeErr := typeNodeSource(parameter.Type); typeErr == nil {
				types[parameter.Name] = strings.TrimSpace(typeName)
			}
		}
		bodies = append(bodies, extensionASTBody{block: function.Method.BodyAST, types: types})
	}
	if len(bodies) == 0 {
		if extensionSourceHasTopLevelDeclaration(tokens) {
			return src, false, nil
		}
		block, parseErr := ParseBodyAST(tokens)
		if parseErr != nil || block == nil {
			return src, false, nil
		}
		types := cloneStringMap(context.GlobalValueTypes)
		if types == nil {
			types = map[string]string{}
		}
		for name, typeName := range inheritedTypes {
			types[name] = typeName
		}
		bodies = append(bodies, extensionASTBody{block: block, types: types})
	}

	edits := []extensionASTEdit{}
	for _, body := range bodies {
		collectOverloadStatementTypes(body.block, body.types)
		collectExtensionStatementTypes(body.block, body.types, context)
		var resolutionErr error
		seenCalls := map[string]bool{}
		seenResults := map[string]bool{}
		visit := func(call *CallExpr) bool {
			if resolutionErr != nil {
				return false
			}
			span := call.Span()
			key := strconv.Itoa(span.Start) + ":" + strconv.Itoa(span.End)
			if seenCalls[key] {
				return seenResults[key]
			}
			seenCalls[key] = true
			rendered, lowered, renderErr := renderExtensionASTCall(call, context, body.types)
			if renderErr != nil {
				resolutionErr = renderErr
				return false
			}
			if !lowered {
				seenResults[key] = true
				return true
			}
			seenResults[key] = false
			if span.End > span.Start && span.Start >= 0 && span.End <= len(src) {
				edits = append(edits, extensionASTEdit{start: span.Start, end: span.End, text: rendered})
			}
			return false
		}
		collectExtensionASTCalls(body.block, visit)
		collectExtensionTokenCalls(tokens, body.block.Span(), visit)
		if resolutionErr != nil {
			return src, true, resolutionErr
		}
	}
	if len(edits) == 0 {
		return src, true, nil
	}
	sort.SliceStable(edits, func(left, right int) bool {
		return edits[left].start > edits[right].start
	})
	for index, edit := range edits {
		if index > 0 && edit.start == edits[index-1].start && edit.end == edits[index-1].end {
			continue
		}
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

func collectExtensionStatementTypes(block *BlockStmt, types map[string]string, context constructorContext) {
	if block == nil || types == nil {
		return
	}
	for _, statement := range block.Statements {
		collectExtensionStatementType(statement, types, context)
	}
}

func collectExtensionStatementType(statement Stmt, types map[string]string, context constructorContext) {
	if statement == nil {
		return
	}
	switch value := statement.(type) {
	case *TokenStmt:
		collectExtensionStatementTypes(value.Body, types, context)
		for _, child := range value.Children {
			collectExtensionStatementType(child, types, context)
		}
	case *DeclarationStmt:
		declared := ""
		if value.Type != nil {
			declared, _ = typeNodeSource(value.Type)
		}
		for index, name := range value.Names {
			inferred := strings.TrimSpace(declared)
			if inferred == "" && index < len(value.Values) {
				inferred = extensionASTStaticType(value.Values[index], context, types)
			}
			if inferred != "" {
				types[name.Text] = inferred
			}
		}
	case *AssignmentStmt:
		for index, left := range value.Left {
			name, ok := left.(*NameExpr)
			if !ok || index >= len(value.Right) {
				continue
			}
			if types[name.Name] == "" {
				if inferred := extensionASTStaticType(value.Right[index], context, types); inferred != "" {
					types[name.Name] = inferred
				}
			}
		}
	case *IfStmt:
		collectExtensionStatementTypes(value.Body, types, context)
		collectExtensionStatementTypes(value.Else, types, context)
		if value.ElseIf != nil {
			collectExtensionStatementType(value.ElseIf, types, context)
		}
	case *ForStmt:
		collectExtensionStatementTypes(value.Body, types, context)
	case *SwitchStmt:
		collectExtensionStatementTypes(value.Body, types, context)
	case *CaseStmt:
		collectExtensionStatementTypes(value.Clause.Body, types, context)
	case *TryStmt:
		collectExtensionStatementTypes(value.Body, types, context)
		for _, clause := range value.Catches {
			collectExtensionStatementTypes(clause.Body, types, context)
		}
		collectExtensionStatementTypes(value.Finally, types, context)
	}
}

func extensionSourceHasTopLevelDeclaration(tokens []Token) bool {
	depth := 0
	for index, token := range tokens {
		if token.Kind == TokenEOF || token.Kind == TokenComment || token.Kind == TokenNewline {
			continue
		}
		if token.Text == "{" {
			depth++
			continue
		}
		if token.Text == "}" {
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth != 0 {
			continue
		}
		switch token.Text {
		case "package", "import", "class", "extend", "enum", "record", "annotation", "type", "var", "const":
			return true
		case "func":
			if index+1 < len(tokens) && (tokens[index+1].Kind == TokenIdentifier || tokens[index+1].Kind == TokenKeyword) {
				return true
			}
		}
	}
	return false
}

func collectExtensionASTCalls(block *BlockStmt, visit func(*CallExpr) bool) {
	if block == nil || visit == nil {
		return
	}
	for _, statement := range block.Statements {
		walkStmtExpressions(statement, func(expression ExprNode) {
			collectExtensionASTExpression(expression, visit)
		})
		if tokenStatement, ok := statement.(*TokenStmt); ok {
			collectExtensionTokenCalls(tokenStatement.Tokens, tokenStatement.Span(), visit)
		}
	}
}

// collectExtensionTokenCalls covers syntax that the body parser has retained
// losslessly but has not yet promoted to a typed expression node. It still
// constructs CallExpr values with the original token spans; no executable
// source is stored or reparsed through go/parser.
func collectExtensionTokenCalls(tokens []Token, bounds Span, visit func(*CallExpr) bool) {
	if len(tokens) == 0 || visit == nil {
		return
	}
	type candidate struct {
		call *CallExpr
		span Span
	}
	candidates := []candidate{}
	for index := 0; index+3 < len(tokens); index++ {
		if tokens[index].Text != "." ||
			(tokens[index+1].Kind != TokenIdentifier && tokens[index+1].Kind != TokenKeyword) ||
			tokens[index+2].Text != "(" {
			continue
		}
		close := matchingToken(tokens, index+2, "(", ")")
		if close < 0 {
			continue
		}
		start := extensionTokenExpressionStart(tokens, index-1)
		if start < 0 || start >= index || tokens[start].Span.Start < bounds.Start || tokens[close].Span.End > bounds.End {
			continue
		}
		expression, err := ParseExpressionTokens(tokens[start : close+1])
		if err != nil || expression == nil {
			continue
		}
		call, ok := expression.(*CallExpr)
		if !ok {
			continue
		}
		if _, _, _, ok := extensionASTCallParts(call); !ok {
			continue
		}
		candidates = append(candidates, candidate{call: call, span: call.Span()})
	}
	// Visit enclosing calls first. If one is lowered, its renderer recursively
	// handles nested calls and the nested candidate must not add an overlapping
	// edit of its own.
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].span.Start != candidates[right].span.Start {
			return candidates[left].span.Start < candidates[right].span.Start
		}
		return candidates[left].span.End > candidates[right].span.End
	})
	blocked := []Span{}
	for _, candidate := range candidates {
		skip := false
		for _, span := range blocked {
			if candidate.span.Start >= span.Start && candidate.span.End <= span.End {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if !visit(candidate.call) {
			blocked = append(blocked, candidate.span)
		}
	}
}

func extensionTokenExpressionStart(tokens []Token, end int) int {
	if end < 0 || end >= len(tokens) {
		return -1
	}
	switch tokens[end].Text {
	case ")":
		open := matchingTokenBackward(tokens, end, "(", ")")
		if open < 0 {
			return -1
		}
		end = extensionTokenExpressionStart(tokens, open-1)
	case "]":
		open := matchingTokenBackward(tokens, end, "[", "]")
		if open < 0 {
			return -1
		}
		end = extensionTokenExpressionStart(tokens, open-1)
	case "}":
		open := matchingTokenBackward(tokens, end, "{", "}")
		if open < 0 {
			return -1
		}
		end = extensionTokenExpressionStart(tokens, open-1)
	}
	if end < 0 {
		return -1
	}
	if end >= 1 && tokens[end-1].Text == "." {
		return extensionTokenExpressionStart(tokens, end-2)
	}
	return end
}

func matchingTokenBackward(tokens []Token, close int, opening, closing string) int {
	depth := 0
	for index := close; index >= 0; index-- {
		switch tokens[index].Text {
		case closing:
			depth++
		case opening:
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func collectExtensionASTExpression(expression ExprNode, visit func(*CallExpr) bool) {
	if expression == nil {
		return
	}
	switch value := expression.(type) {
	case *CallExpr:
		if !visit(value) {
			return
		}
		collectExtensionASTExpression(value.Callee, visit)
		for _, argument := range value.Arguments {
			collectExtensionASTExpression(argument.Value, visit)
		}
	case *UnaryExpr:
		collectExtensionASTExpression(value.Operand, visit)
	case *BinaryExpr:
		collectExtensionASTExpression(value.Left, visit)
		collectExtensionASTExpression(value.Right, visit)
	case *AssignmentExpr:
		for _, expression := range value.Left {
			collectExtensionASTExpression(expression, visit)
		}
		for _, expression := range value.Right {
			collectExtensionASTExpression(expression, visit)
		}
	case *SelectorExpr:
		collectExtensionASTExpression(value.Receiver, visit)
	case *IndexExpr:
		collectExtensionASTExpression(value.Receiver, visit)
		collectExtensionASTExpression(value.Index, visit)
	case *IndexListExpr:
		collectExtensionASTExpression(value.Receiver, visit)
		for _, index := range value.Indices {
			collectExtensionASTExpression(index, visit)
		}
	case *ParenthesizedExpr:
		collectExtensionASTExpression(value.Inner, visit)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectExtensionASTExpression(element.Key, visit)
			collectExtensionASTExpression(element.Value, visit)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectExtensionASTExpression(segment.Expression, visit)
		}
	case *LambdaExpr:
		collectExtensionASTExpression(value.Body, visit)
		collectExtensionASTCalls(value.BlockBody, visit)
	case *FunctionLiteralExpr:
		collectExtensionASTCalls(value.Body, visit)
	}
}

func extensionASTCallParts(call *CallExpr) (ExprNode, string, []TypeNode, bool) {
	if call == nil || call.Callee == nil {
		return nil, "", nil, false
	}
	switch callee := call.Callee.(type) {
	case *SelectorExpr:
		return callee.Receiver, callee.Name, nil, true
	case *IndexExpr:
		selector, ok := callee.Receiver.(*SelectorExpr)
		if !ok {
			return nil, "", nil, false
		}
		return selector.Receiver, selector.Name, []TypeNode{extensionASTTypeArgument(callee.Index)}, true
	case *IndexListExpr:
		selector, ok := callee.Receiver.(*SelectorExpr)
		if !ok {
			return nil, "", nil, false
		}
		arguments := make([]TypeNode, 0, len(callee.Indices))
		for _, index := range callee.Indices {
			arguments = append(arguments, extensionASTTypeArgument(index))
		}
		return selector.Receiver, selector.Name, arguments, true
	default:
		return nil, "", nil, false
	}
}

func extensionASTTypeArgument(expression ExprNode) TypeNode {
	if typeExpr, ok := expression.(*TypeExpr); ok {
		return typeExpr.Type
	}
	text, err := expressionNodeSource(expression)
	if err != nil {
		return nil
	}
	return parseTypeText(text)
}

func renderExtensionASTCall(call *CallExpr, context constructorContext, valueTypes map[string]string) (string, bool, error) {
	// Prefer the structural extension lowerer even in this compatibility
	// source-rewrite pass. The final result still has to be formatted into the
	// original source span, but the call and its arguments remain AST nodes.
	structuralContext := context
	structuralContext.CurrentParameterTypes = cloneStringMap(valueTypes)
	if lowered, handled, err := lowerExceptionExtensionCallNode(call, structuralContext); handled {
		if err != nil {
			return "", true, err
		}
		if lowered != nil {
			goExpression, goErr := goExprNode(lowered)
			if goErr != nil {
				return "", false, nil
			}
			formatted, formatErr := formatNode(goExpression)
			return formatted, true, formatErr
		}
	}
	receiver, methodName, typeArguments, ok := extensionASTCallParts(call)
	if ok {
		actualType := extensionASTStaticType(receiver, context, valueTypes)
		candidates := applicableExtensionASTs(methodName, actualType, call.Arguments, typeArguments, valueTypes, context)
		if len(candidates) > 0 && !realMethodAppliesAST(actualType, methodName, call.Arguments, context) {
			if len(candidates) > 1 {
				return "", false, fmt.Errorf("ambiguous extension method %s for %s", methodName, actualType)
			}
			candidate := candidates[0]
			receiverText, err := renderExtensionASTExpression(receiver, context, valueTypes)
			if err != nil {
				return "", false, err
			}
			if extensionNeedsAddress(candidate.Method.ReceiverType, actualType) {
				receiverText = "&" + receiverText
			}
			arguments := []string{receiverText}
			arguments = append(arguments, candidate.Arguments...)
			functionName := candidate.Method.GoName
			if candidate.Method.Qualifier != "" {
				functionName = candidate.Method.Qualifier + "." + functionName
			}
			if len(typeArguments) > 0 {
				formatted := make([]string, len(typeArguments))
				for index, argument := range typeArguments {
					text, typeErr := typeNodeSource(argument)
					if typeErr != nil || text == "" {
						return "", false, nil
					}
					formatted[index] = text
				}
				functionName += "[" + strings.Join(formatted, ", ") + "]"
			}
			return functionName + "(" + strings.Join(arguments, ", ") + ")", true, nil
		}
	}
	return "", false, nil
}

func renderExtensionASTExpression(expression ExprNode, context constructorContext, valueTypes map[string]string) (string, error) {
	call, ok := expression.(*CallExpr)
	if !ok {
		return expressionNodeSource(expression)
	}
	if rendered, lowered, err := renderExtensionASTCall(call, context, valueTypes); err != nil {
		return "", err
	} else if lowered {
		return rendered, nil
	}
	callee, err := renderExtensionASTExpression(call.Callee, context, valueTypes)
	if err != nil {
		return "", err
	}
	arguments := make([]string, len(call.Arguments))
	for index, argument := range call.Arguments {
		text, argumentErr := renderExtensionASTExpression(argument.Value, context, valueTypes)
		if argumentErr != nil {
			return "", argumentErr
		}
		if argument.Name != "" {
			text = argument.Name + ": " + text
		}
		arguments[index] = text
	}
	return callee + "(" + strings.Join(arguments, ", ") + ")", nil
}

type applicableExtensionAST struct {
	Method    extensionMethod
	Arguments []string
}

func applicableExtensionASTs(name, actualType string, args []CallArg, typeArguments []TypeNode, valueTypes map[string]string, context constructorContext) []applicableExtensionAST {
	result := []applicableExtensionAST{}
	for _, extension := range context.Extensions {
		if extension.Method.Name != name || !extensionTargetMatches(extension.Target, extension.ReceiverType, actualType, context) {
			continue
		}
		if len(typeArguments) > 0 && extensionTypeParameterCount(methodTypeParamsSource(extension.Method)) != len(typeArguments) {
			continue
		}
		arguments, ok := resolveExtensionArgumentsAST(extension, actualType, args, valueTypes, context)
		if ok {
			result = append(result, applicableExtensionAST{Method: extension, Arguments: arguments})
		}
	}
	userExtensions := result[:0]
	for _, candidate := range result {
		if !candidate.Method.Prelude {
			userExtensions = append(userExtensions, candidate)
		}
	}
	if len(userExtensions) > 0 {
		return userExtensions
	}
	return result
}

func resolveExtensionArgumentsAST(extension extensionMethod, actualType string, args []CallArg, valueTypes map[string]string, context constructorContext) ([]string, bool) {
	parameters, err := parameterInfosForMethod(extension.Method)
	if err != nil {
		return nil, false
	}
	bindings := extensionTargetBindings(extension.Target, actualType)
	for index := range parameters {
		parameters[index].TypeAST = parseTypeText(substituteLambdaType(parameters[index].typeText(), bindings))
	}
	argumentText := make([]string, len(args))
	for index, argument := range args {
		text, sourceErr := expressionNodeSource(argument.Value)
		if sourceErr != nil {
			return nil, false
		}
		if argument.Name != "" {
			text = argument.Name + ": " + text
		}
		argumentText[index] = text
		actual := extensionASTStaticType(argument.Value, context, valueTypes)
		parameterIndex := index
		if parameterIndex >= len(parameters) {
			if len(parameters) == 0 || !strings.HasPrefix(strings.TrimSpace(parameters[len(parameters)-1].typeText()), "...") {
				return nil, false
			}
			parameterIndex = len(parameters) - 1
		}
		expected := parameters[parameterIndex].typeText()
		if strings.HasPrefix(strings.TrimSpace(expected), "...") {
			expected = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expected), "..."))
		}
		generic := extensionTypeParameterNames(methodTypeParamsSource(extension.Method))
		if actual != "" && !generic[expected] && !extensionArgumentMatches(actual, expected, text, context) && !genericExtensionArgumentMatches(actual, expected, generic) {
			return nil, false
		}
	}
	variadic := len(parameters) > 0 && strings.HasPrefix(strings.TrimSpace(parameters[len(parameters)-1].typeText()), "...")
	if variadic && len(args) < len(parameters)-1 {
		return nil, false
	}
	if !variadic {
		required := 0
		for _, parameter := range parameters {
			if !parameter.HasDefault {
				required++
			}
		}
		if len(args) < required || len(args) > len(parameters) {
			return nil, false
		}
	}
	resolved, _, resolveErr := resolveCallableCall(extension.Method.Name, argumentText, []callableSignature{{Name: extension.Method.Name, Parameters: parameters}})
	if resolveErr != nil {
		return nil, false
	}
	return resolved, true
}

func extensionASTStaticType(expression ExprNode, context constructorContext, valueTypes map[string]string) string {
	call, ok := expression.(*CallExpr)
	if !ok {
		return staticExpressionTypeNode(expression, context, valueTypes)
	}
	receiver, name, typeArguments, ok := extensionASTCallParts(call)
	if !ok {
		return staticExpressionTypeNode(expression, context, valueTypes)
	}
	actualType := extensionASTStaticType(receiver, context, valueTypes)
	candidates := applicableExtensionASTs(name, actualType, call.Arguments, typeArguments, valueTypes, context)
	if len(candidates) != 1 || realMethodAppliesAST(actualType, name, call.Arguments, context) {
		return staticExpressionTypeNode(expression, context, valueTypes)
	}
	result := strings.TrimSpace(methodResultSource(candidates[0].Method.Method))
	if result == "" {
		return ""
	}
	return substituteLambdaType(result, extensionTargetBindings(candidates[0].Method.Target, actualType))
}

func realMethodAppliesAST(actualType, methodName string, args []CallArg, context constructorContext) bool {
	name := strings.TrimPrefix(strings.TrimSpace(actualType), "*")
	if _, ok := context.Targets[name]; ok {
		for _, signature := range context.ClassMethodSignatures[name][methodName] {
			if len(args) >= requiredParameterCount(signature) && len(args) <= len(signature.Parameters) {
				return true
			}
		}
	}
	return context.NativeMethods[actualType][methodName] || context.NativeMethods[name][methodName]
}

func extensionCallParts(call *ast.CallExpr) (ast.Expr, string, []ast.Expr, bool) {
	switch function := call.Fun.(type) {
	case *ast.SelectorExpr:
		return function.X, function.Sel.Name, nil, true
	case *ast.IndexExpr:
		selector, ok := function.X.(*ast.SelectorExpr)
		if !ok {
			return nil, "", nil, false
		}
		return selector.X, selector.Sel.Name, []ast.Expr{function.Index}, true
	case *ast.IndexListExpr:
		selector, ok := function.X.(*ast.SelectorExpr)
		if !ok {
			return nil, "", nil, false
		}
		return selector.X, selector.Sel.Name, function.Indices, true
	default:
		return nil, "", nil, false
	}
}

type applicableExtension struct {
	Method    extensionMethod
	Arguments []string
}

func applicableExtensions(name, actualType string, args []ast.Expr, ellipsis bool, typeArguments []ast.Expr, valueTypes map[string]string, context constructorContext) []applicableExtension {
	result := []applicableExtension{}
	for _, extension := range context.Extensions {
		if extension.Method.Name != name || !extensionTargetMatches(extension.Target, extension.ReceiverType, actualType, context) {
			continue
		}
		if len(typeArguments) > 0 && extensionTypeParameterCount(methodTypeParamsSource(extension.Method)) != len(typeArguments) {
			continue
		}
		resolved, ok := resolveExtensionArguments(extension, actualType, args, ellipsis, valueTypes, context)
		if !ok {
			continue
		}
		result = append(result, applicableExtension{Method: extension, Arguments: resolved})
	}
	userExtensions := result[:0]
	for _, candidate := range result {
		if !candidate.Method.Prelude {
			userExtensions = append(userExtensions, candidate)
		}
	}
	if len(userExtensions) > 0 {
		return userExtensions
	}
	return result
}

func resolveExtensionArguments(extension extensionMethod, actualType string, args []ast.Expr, ellipsis bool, valueTypes map[string]string, context constructorContext) ([]string, bool) {
	parameters, err := parameterInfosForMethod(extension.Method)
	if err != nil {
		return nil, false
	}
	bindings := extensionTargetBindings(extension.Target, actualType)
	for index := range parameters {
		parameters[index].TypeAST = parseTypeText(substituteLambdaType(parameters[index].typeText(), bindings))
	}
	argumentText := make([]string, len(args))
	for index, argument := range args {
		argumentText[index], err = formatNode(argument)
		if err != nil {
			return nil, false
		}
		if ellipsis && index == len(args)-1 {
			argumentText[index] += "..."
		}
	}
	signature := callableSignature{Name: extension.Method.Name, Parameters: parameters}
	variadic := len(parameters) > 0 && strings.HasPrefix(strings.TrimSpace(parameters[len(parameters)-1].typeText()), "...")
	var resolved []string
	var changed bool
	if variadic {
		fixedCount := len(parameters) - 1
		if len(args) < fixedCount {
			return nil, false
		}
		resolved = argumentText
		changed = true
	} else {
		resolved, changed, err = resolveCallableCall(extension.Method.Name, argumentText, []callableSignature{signature})
		if err != nil {
			return nil, false
		}
		if !changed {
			if len(args) != len(parameters) {
				return nil, false
			}
			resolved = argumentText
		}
	}
	for index, argument := range resolved {
		argumentForType := strings.TrimSpace(argument)
		if strings.HasSuffix(argumentForType, "...") {
			argumentForType = strings.TrimSpace(strings.TrimSuffix(argumentForType, "..."))
		}
		parsed, err := parseStaticArgumentNode(argumentForType)
		if err != nil {
			return nil, false
		}
		actual := staticExpressionTypeNode(parsed, context, valueTypes)
		generic := extensionTypeParameterNames(methodTypeParamsSource(extension.Method))
		for name := range extensionTargetTypeParameterNames(extension.Target) {
			generic[name] = true
		}
		parameterIndex := index
		if variadic && parameterIndex >= len(parameters)-1 {
			parameterIndex = len(parameters) - 1
		}
		if parameterIndex >= len(parameters) {
			return nil, false
		}
		expected := parameters[parameterIndex].typeText()
		if strings.HasPrefix(strings.TrimSpace(expected), "...") {
			expected = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expected), "..."))
		}
		if actual != "" && !generic[expected] &&
			!extensionArgumentMatches(actual, expected, argument, context) &&
			!genericExtensionArgumentMatches(actual, expected, generic) {
			return nil, false
		}
	}
	return resolved, true
}

func extensionArgumentMatches(actual, expected, argument string, context constructorContext) bool {
	if expected == "any" || expected == "interface{}" {
		return true
	}
	if strings.HasSuffix(strings.TrimSpace(argument), "...") {
		return strings.TrimSpace(actual) == "[]"+strings.TrimSpace(expected)
	}
	return actual == expected || isAssignableStaticType(actual, expected, context)
}

func genericExtensionArgumentMatches(actual, expected string, generic map[string]bool) bool {
	actualFunction, actualErr := parseLambdaFunctionType(actual)
	expectedFunction, expectedErr := parseLambdaFunctionType(expected)
	if actualErr == nil && expectedErr == nil {
		if len(actualFunction.Parameters) != len(expectedFunction.Parameters) {
			return false
		}
		for index := range actualFunction.Parameters {
			actualType, _ := typeNodeSource(actualFunction.Parameters[index])
			expectedType, _ := typeNodeSource(expectedFunction.Parameters[index])
			if !genericTypeMatches(actualType, expectedType, generic) {
				return false
			}
		}
		return genericTypeMatches(actualFunction.resultText(), expectedFunction.resultText(), generic)
	}
	return genericTypeMatches(actual, expected, generic)
}

func genericTypeMatches(actual, expected string, generic map[string]bool) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if actual == expected || expected == "" {
		return actual == expected
	}
	if generic[expected] {
		return true
	}
	if strings.HasPrefix(actual, "[]") && strings.HasPrefix(expected, "[]") {
		return genericTypeMatches(actual[2:], expected[2:], generic)
	}
	return false
}

func extensionCallResultType(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) string {
	receiver, methodName, typeArguments, ok := extensionCallParts(call)
	if !ok {
		return ""
	}
	actualType := expressionStaticType(receiver, context, valueTypes)
	candidates := applicableExtensions(methodName, actualType, call.Args, call.Ellipsis.IsValid(), typeArguments, valueTypes, context)
	if len(candidates) != 1 || realMethodApplies(actualType, methodName, call.Args, context) {
		return ""
	}
	result := strings.TrimSpace(methodResultSource(candidates[0].Method.Method))
	if result == "" {
		return ""
	}
	return substituteLambdaType(result, extensionTargetBindings(candidates[0].Method.Target, actualType))
}

func extensionTypeParameterNames(typeParams string) map[string]bool {
	result := map[string]bool{}
	typeParams = strings.TrimSpace(typeParams)
	if len(typeParams) < 2 || typeParams[0] != '[' || typeParams[len(typeParams)-1] != ']' {
		return result
	}
	parts, err := splitTopLevel(typeParams[1:len(typeParams)-1], ',')
	if err != nil {
		return result
	}
	for _, part := range parts {
		fields := strings.Fields(part)
		if len(fields) > 0 {
			result[fields[0]] = true
		}
	}
	return result
}

func extensionTargetMatches(target, receiverType, actual string, context constructorContext) bool {
	target = normalizeExtensionTarget(target)
	actual = normalizeExtensionTarget(actual)
	if actual == "" {
		return false
	}
	if nativeTypesAssignable(actual, target, context) {
		return true
	}
	if extensionGenericTargetMatches(target, actual) {
		return true
	}
	if strings.HasPrefix(target, "*") {
		return actual == target || actual == strings.TrimPrefix(target, "*")
	}
	if extensionValueType(target) {
		return actual == target
	}
	return actual == target || actual == "*"+target || actual == receiverType
}

func nativeTypesAssignable(actual, expected string, context constructorContext) bool {
	actualType, actualOK := nativeGoType(actual, context)
	expectedType, expectedOK := nativeGoType(expected, context)
	if !actualOK || !expectedOK {
		return false
	}
	return types.AssignableTo(actualType, expectedType)
}

func nativeGoType(typeName string, context constructorContext) (types.Type, bool) {
	typeName = strings.TrimSpace(typeName)
	pointer := strings.HasPrefix(typeName, "*")
	named, _, ok := nativeNamedType(typeName, context)
	if !ok {
		return nil, false
	}
	if pointer {
		return types.NewPointer(named), true
	}
	return named, true
}

func extensionTargetTypeParameterNames(target string) map[string]bool {
	result := map[string]bool{}
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "[]") {
		name := strings.TrimSpace(target[2:])
		if isTypeParameterName(name) {
			result[name] = true
		}
		return result
	}
	if !strings.HasPrefix(target, "map[") {
		return result
	}
	close := strings.IndexByte(target, ']')
	if close < 0 || close+1 >= len(target) {
		return result
	}
	key := strings.TrimSpace(target[len("map["):close])
	value := strings.TrimSpace(target[close+1:])
	if isTypeParameterName(key) {
		result[key] = true
	}
	if isTypeParameterName(value) {
		result[value] = true
	}
	return result
}

func isTypeParameterName(name string) bool {
	switch name {
	case "T", "K", "V", "E":
		return true
	default:
		return false
	}
}

func extensionGenericTargetMatches(target, actual string) bool {
	parameters := extensionTargetTypeParameterNames(target)
	if len(parameters) == 0 {
		return false
	}
	target = strings.TrimSpace(target)
	actual = strings.TrimSpace(actual)
	if strings.HasPrefix(target, "[]") && strings.HasPrefix(actual, "[]") {
		return strings.TrimSpace(target[2:]) != "" && strings.TrimSpace(actual[2:]) != ""
	}
	if strings.HasPrefix(target, "map[") && strings.HasPrefix(actual, "map[") {
		targetClose := strings.IndexByte(target, ']')
		actualClose := strings.IndexByte(actual, ']')
		return targetClose > 4 && actualClose > 4 &&
			strings.TrimSpace(target[targetClose+1:]) != "" &&
			strings.TrimSpace(actual[actualClose+1:]) != ""
	}
	return false
}

func extensionNeedsAddress(receiverType, actualType string) bool {
	return strings.HasPrefix(strings.TrimSpace(receiverType), "*") &&
		!strings.HasPrefix(strings.TrimSpace(actualType), "*")
}

func realMethodApplies(actualType, methodName string, args []ast.Expr, context constructorContext) bool {
	name := strings.TrimPrefix(strings.TrimSpace(actualType), "*")
	if _, ok := context.Targets[name]; ok {
		for _, signature := range context.ClassMethodSignatures[name][methodName] {
			if len(args) >= requiredParameterCount(signature) && len(args) <= len(signature.Parameters) {
				return true
			}
		}
	}
	return context.NativeMethods[actualType][methodName] || context.NativeMethods[name][methodName]
}

func configureNativeExtensionMethods(context *constructorContext, file *File) {
	context.NativeMethods = map[string]map[string]bool{}
	if len(context.Extensions) == 0 {
		return
	}
	imports, err := goImports(file)
	if err != nil {
		return
	}
	paths := map[string]string{}
	for _, spec := range imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		alias := path.Base(importPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias != "" && alias != "_" && alias != "." {
			paths[alias] = importPath
		}
	}
	for _, extension := range context.Extensions {
		target := strings.TrimPrefix(strings.TrimSpace(extension.Target), "*")
		dot := strings.LastIndex(target, ".")
		if dot <= 0 || dot == len(target)-1 {
			continue
		}
		alias := target[:dot]
		typeName := target[dot+1:]
		importPath := paths[alias]
		if importPath == "" {
			continue
		}
		pkg, err := importNativePackage(importPath)
		if err != nil {
			continue
		}
		object := pkg.Scope().Lookup(typeName)
		if object == nil {
			continue
		}
		named, ok := object.Type().(*types.Named)
		if !ok {
			continue
		}
		methods := types.NewMethodSet(types.NewPointer(named))
		for index := 0; index < methods.Len(); index++ {
			method := methods.At(index).Obj()
			if context.NativeMethods[target] == nil {
				context.NativeMethods[target] = map[string]bool{}
			}
			context.NativeMethods[target][method.Name()] = true
			if context.NativeMethods["*"+target] == nil {
				context.NativeMethods["*"+target] = map[string]bool{}
			}
			context.NativeMethods["*"+target][method.Name()] = true
		}
	}

	// Generic extensions must yield to native methods on imported named types
	// as well. This matters for APIs such as reflect.Value, whose native Index
	// method would otherwise be mistaken for the prelude slice extension.
	extensionNames := map[string]bool{}
	for _, extension := range context.Extensions {
		extensionNames[extension.Method.Name] = true
	}
	for alias, importPath := range paths {
		pkg, err := importNativePackage(importPath)
		if err != nil {
			continue
		}
		for _, name := range pkg.Scope().Names() {
			object := pkg.Scope().Lookup(name)
			typeName, ok := object.(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := typeName.Type().(*types.Named)
			if !ok {
				continue
			}
			methods := types.NewMethodSet(types.NewPointer(named))
			for index := 0; index < methods.Len(); index++ {
				method := methods.At(index).Obj()
				if !extensionNames[method.Name()] {
					continue
				}
				target := alias + "." + name
				if context.NativeMethods[target] == nil {
					context.NativeMethods[target] = map[string]bool{}
				}
				context.NativeMethods[target][method.Name()] = true
			}
		}
	}
}

func extensionTypeParameterCount(typeParams string) int {
	typeParams = strings.TrimSpace(typeParams)
	if len(typeParams) < 2 || typeParams[0] != '[' || typeParams[len(typeParams)-1] != ']' {
		return 0
	}
	parts, err := splitTopLevel(typeParams[1:len(typeParams)-1], ',')
	if err != nil {
		return 0
	}
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func extensionTypeParameters(typeParams string) string {
	typeParams = strings.TrimSpace(typeParams)
	if typeParams == "" {
		return ""
	}
	parts, err := splitTopLevel(typeParams[1:len(typeParams)-1], ',')
	if err != nil {
		return typeParams
	}
	for index, part := range parts {
		if len(strings.Fields(part)) == 1 {
			parts[index] = strings.TrimSpace(part) + " any"
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func extensionTargetTypeParameters(target string, constraints map[string]TypeNode) string {
	names := extensionTargetTypeParameterNames(target)
	if len(names) == 0 {
		return ""
	}
	ordered := []string{}
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	parts := make([]string, len(ordered))
	for index, name := range ordered {
		constraint := ""
		if constraintNode := constraints[name]; constraintNode != nil {
			constraint, _ = typeNodeSource(constraintNode)
			constraint = strings.TrimSpace(constraint)
		}
		if constraint == "" {
			if name == extensionMapKeyParameter(target) {
				constraint = "comparable"
			} else {
				constraint = "any"
			}
		}
		parts[index] = name + " " + constraint
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func extensionMapKeyParameter(target string) string {
	target = strings.TrimSpace(target)
	if !strings.HasPrefix(target, "map[") {
		return ""
	}
	close := strings.IndexByte(target, ']')
	if close < 0 {
		return ""
	}
	key := strings.TrimSpace(target[len("map["):close])
	if isTypeParameterName(key) {
		return key
	}
	return ""
}

func extensionFunctionTypeParameters(extension extensionMethod) string {
	targetParameters := extensionTargetTypeParameters(extension.Target, extension.TargetConstraints)
	methodParameters := extensionTypeParameters(methodTypeParamsSource(extension.Method))
	if targetParameters == "" {
		return methodParameters
	}
	if methodParameters == "" {
		return targetParameters
	}
	targetParts := strings.TrimSuffix(strings.TrimPrefix(targetParameters, "["), "]")
	methodParts := strings.TrimSuffix(strings.TrimPrefix(methodParameters, "["), "]")
	return "[" + targetParts + ", " + methodParts + "]"
}

func emitExtension(out *strings.Builder, file *File, declaration *ExtendDecl, context constructorContext, interpolationName string) error {
	sourcePath := sourceDirectivePath(file)
	for _, target := range extensionTargetNames(declaration) {
		target = normalizeExtensionTarget(target)
		for _, method := range declaration.Methods {
			extension := extensionMethod{
				Target:            target,
				TargetConstraints: cloneTypeNodeMap(declaration.TargetConstraints),
				ReceiverType:      extensionReceiverType(target, context.Introspection.Classes),
				GoName:            extensionGoName(target, method),
				SourceFile:        sourcePath,
				SourceLine:        declaration.SourceLine,
				Method:            method,
			}
			if err := emitExtensionMethod(out, extension, context, interpolationName); err != nil {
				return fmt.Errorf("extension method %s for target %s: %w", method.Name, target, err)
			}
		}
	}
	return nil
}

func emitExtensionMethod(out *strings.Builder, extension extensionMethod, context constructorContext, interpolationName string) error {
	if direct, handled, err := directExtensionMethodSource(extension, context); handled {
		if err != nil {
			return err
		}
		emitSourceDirective(out, extension.SourceFile, extension.SourceLine)
		out.WriteString(direct)
		out.WriteByte('\n')
		return nil
	}
	parameters, err := stripParameterDefaults(methodParametersSource(extension.Method))
	if err != nil {
		return err
	}
	parameters, err = transformExtensionParameterList(parameters, context)
	if err != nil {
		return err
	}
	name := extension.GoName
	emitSourceDirective(out, extension.SourceFile, extension.SourceLine)
	fmt.Fprintf(out, "func %s%s(this %s", name, extensionFunctionTypeParameters(extension), extension.ReceiverType)
	if parameters != "" {
		fmt.Fprintf(out, ", %s", parameters)
	}
	out.WriteByte(')')
	result := transformPolymorphicResultType(strings.TrimSpace(methodResultSource(extension.Method)), context)
	if result != "" {
		fmt.Fprintf(out, " %s", result)
	}
	out.WriteString(" {\n")

	methodContext := context
	methodContext.CurrentExtensionReceiver = extension.ReceiverType
	methodContext.CurrentParameterTypes = map[string]string{}
	if parameterInfos, parseErr := parseParameterInfos(parameters); parseErr == nil {
		for _, parameter := range parameterInfos {
			if parameter.Name != "" {
				methodContext.CurrentParameterTypes[parameter.Name] = parameter.typeText()
			}
		}
	}
	body, err := transformMethodInterpolation(extension.Method, interpolationName)
	if err != nil {
		return err
	}
	body, err = transformEnums(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformImplicitErrorPromotion(body, methodContext)
	if err != nil {
		return err
	}
	if transformed, handled, exceptionErr := transformExceptionMethodBodyAST(body, &extension.Method, methodContext); handled {
		if exceptionErr != nil {
			return exceptionErr
		}
		body = transformed
	} else {
		body, err = transformExceptions(body, methodContext)
		if err != nil {
			return err
		}
	}
	body, err = transformLambdas(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformCallableCalls(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformConstructors(body, context)
	if err != nil {
		return err
	}
	body, err = transformErrorCoalescing(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformSafeAccess(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformRecords(body, context)
	if err != nil {
		return err
	}
	body, err = transformPolymorphicDeclarations(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformIntrospection(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformExtensions(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformErrorCoalescing(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformOverloads(body, context.Overloads)
	if err != nil {
		return err
	}
	body, _ = wrapExceptionBoundaryBody(body, result, methodContext)
	out.WriteString(body)
	out.WriteString("\n}\n\n")
	return nil
}

// Extension parameters that name a Go++ class are exposed through the
// generated class interface. This lets an extension package accept derived
// classes without knowing which packages will define those derived classes.
func transformExtensionParameterList(params string, context constructorContext) (string, error) {
	parameters, err := parseParameterInfos(params)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		typeName := transformPolymorphicType(parameter.typeText(), context)
		if typeName == parameter.typeText() {
			name := strings.TrimSpace(parameter.typeText())
			if target, ok := context.Targets[name]; ok && target.Qualifier == "" {
				typeName = "Gpp" + target.Class.Name
			}
		}
		if parameter.Name == "" {
			parts = append(parts, typeName)
		} else {
			parts = append(parts, parameter.Name+" "+typeName)
		}
	}
	return strings.Join(parts, ", "), nil
}

func transformExtensionParameterNodes(parameters []ParameterNode, context constructorContext) (string, error) {
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		typeName, err := directTypeText(parameter.Type)
		if err != nil {
			return "", err
		}
		transformed := transformPolymorphicType(typeName, context)
		if transformed == typeName {
			name := strings.TrimSpace(typeName)
			if target, ok := context.Targets[name]; ok && target.Qualifier == "" && target.Class != nil {
				transformed = "Gpp" + target.Class.Name
			}
		}
		if parameter.Name == "" {
			parts = append(parts, transformed)
		} else {
			parts = append(parts, parameter.Name+" "+transformed)
		}
	}
	return strings.Join(parts, ", "), nil
}
