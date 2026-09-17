package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

type lambdaParameter struct {
	Name string
	Type string
}

type lambdaSource struct {
	Placeholder string
	Params      []lambdaParameter
	Body        string
	Block       bool
}

type lambdaFunctionType struct {
	Parameters []string
	Result     string
}

type lambdaSpan struct {
	Start int
	End   int
	Value lambdaSource
}

func transformLambdas(src string, context constructorContext) (string, error) {
	spans, err := collectLambdaSpans(src)
	if err != nil {
		return "", err
	}
	if len(spans) == 0 {
		return src, nil
	}

	var placeholderSource strings.Builder
	last := 0
	for index := range spans {
		placeholderSource.WriteString(src[last:spans[index].Start])
		spans[index].Value.Placeholder = fmt.Sprintf("__gpp_lambda_%d", index)
		placeholderSource.WriteString(spans[index].Value.Placeholder)
		last = spans[index].End
	}
	placeholderSource.WriteString(src[last:])

	parsed, err := parseLambdaSource(placeholderSource.String())
	if err != nil {
		return src, nil
	}
	valueTypes := polymorphicValueTypes(parsed, context)
	if context.CurrentClass != "" {
		valueTypes["this"] = "*" + context.CurrentClass
	} else if context.CurrentExtensionReceiver != "" {
		valueTypes["this"] = context.CurrentExtensionReceiver
	}
	declaredTypes := lambdaDeclaredTypes(parsed)

	for index := range spans {
		lambda := &spans[index].Value
		candidates := lambdaExpectedTypes(parsed, lambda.Placeholder, context, valueTypes)
		if declared := lambdaAssignmentType(parsed, lambda.Placeholder, declaredTypes); declared != "" {
			candidates = append(candidates, declared)
		}

		var rendered string
		if len(candidates) == 0 {
			var renderErr error
			rendered, renderErr = renderStandaloneLambda(*lambda, context)
			if renderErr != nil {
				return "", renderErr
			}
		} else {
			var renderErr error
			rendered, renderErr = renderContextualLambda(*lambda, candidates, context)
			if renderErr != nil {
				return "", renderErr
			}
		}
		lambda.Body = rendered
	}

	for index := len(spans) - 1; index >= 0; index-- {
		span := spans[index]
		src = src[:span.Start] + span.Value.Body + src[span.End:]
	}
	return src, nil
}

func collectLambdaSpans(src string) ([]lambdaSpan, error) {
	spans := []lambdaSpan{}
	for index := 0; index < len(src)-1; index++ {
		if end, ok, err := copyIgnoredSource(src, index, &strings.Builder{}); err != nil {
			return nil, err
		} else if ok {
			index = end - 1
			continue
		}
		if src[index] != '=' || src[index+1] != '>' {
			continue
		}
		start, params, err := lambdaStart(src, index)
		if err != nil {
			return nil, err
		}
		if start < 0 {
			continue
		}
		bodyStart := skipSpace(src, index+2)
		if bodyStart >= len(src) {
			return nil, fmt.Errorf("lambda requires a body")
		}
		bodyEnd := bodyStart
		block := src[bodyStart] == '{'
		if block {
			bodyEnd, err = findMatchingBrace(src, bodyStart)
			if err != nil {
				return nil, err
			}
			bodyEnd++
		} else {
			bodyEnd = lambdaExpressionEnd(src, bodyStart)
		}
		if bodyEnd <= bodyStart {
			return nil, fmt.Errorf("lambda requires a body")
		}
		spans = append(spans, lambdaSpan{
			Start: start,
			End:   bodyEnd,
			Value: lambdaSource{
				Params: params,
				Body:   src[bodyStart:bodyEnd],
				Block:  block,
			},
		})
		index = bodyEnd - 1
	}
	return spans, nil
}

func lambdaStart(src string, arrow int) (int, []lambdaParameter, error) {
	end := arrow - 1
	for end >= 0 && isLambdaSpace(src[end]) {
		end--
	}
	if end < 0 {
		return -1, nil, nil
	}
	start := end
	if isIdentPart(src[end]) {
		for start >= 0 && isIdentPart(src[start]) {
			start--
		}
		start++
		name := strings.TrimSpace(src[start : end+1])
		if name == "" || !isIdentifier(name) {
			return -1, nil, nil
		}
		return start, []lambdaParameter{{Name: name}}, nil
	}
	if src[end] != ')' {
		return -1, nil, nil
	}
	open := lambdaOpenParen(src, end)
	if open < 0 {
		return -1, nil, fmt.Errorf("could not find lambda parameter list")
	}
	params, err := parseLambdaParameters(src[open+1 : end])
	if err != nil {
		return -1, nil, err
	}
	return open, params, nil
}

func lambdaOpenParen(src string, close int) int {
	depth := 0
	for index := close; index >= 0; index-- {
		switch src[index] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func parseLambdaParameters(src string) ([]lambdaParameter, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	parts, err := splitTopLevel(src, ',')
	if err != nil {
		return nil, err
	}
	result := make([]lambdaParameter, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("lambda parameter cannot be empty")
		}
		if isIdentifier(part) {
			result = append(result, lambdaParameter{Name: part})
			continue
		}
		parameters, err := parseParameterInfos(part)
		if err != nil || len(parameters) != 1 || parameters[0].Name == "" {
			if err != nil {
				return nil, fmt.Errorf("invalid lambda parameter %q: %w", part, err)
			}
			return nil, fmt.Errorf("invalid lambda parameter %q", part)
		}
		result = append(result, lambdaParameter{
			Name: parameters[0].Name,
			Type: parameters[0].Type,
		})
	}
	return result, nil
}

func lambdaExpressionEnd(src string, start int) int {
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for index := start; index < len(src); index++ {
		if end, ok, _ := copyIgnoredSource(src, index, &strings.Builder{}); ok {
			index = end - 1
			continue
		}
		switch src[index] {
		case '(':
			parenDepth++
		case ')':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return index
			}
			parenDepth--
		case '[':
			bracketDepth++
		case ']':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return index
			}
			bracketDepth--
		case '{':
			braceDepth++
		case '}':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return index
			}
			braceDepth--
		case ',', ';':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return index
			}
		case '\n':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return index
			}
		}
	}
	return len(src)
}

func isLambdaSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r'
}

func parseLambdaSource(src string) (ast.Node, error) {
	const filePrefix = "package main\n\n"
	parsed, err := parser.ParseFile(token.NewFileSet(), "lambda.go", filePrefix+src, 0)
	if err == nil {
		return parsed, nil
	}
	const functionPrefix = "package main\n\nfunc __gpp_lambda_scope() {\n"
	parsed, err = parser.ParseFile(token.NewFileSet(), "lambda.go", functionPrefix+src+"\n}\n", 0)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func lambdaDeclaredTypes(root ast.Node) map[string]string {
	result := map[string]string{}
	ast.Inspect(root, func(node ast.Node) bool {
		declaration, ok := node.(*ast.ValueSpec)
		if !ok || declaration.Type == nil {
			return true
		}
		typeName, err := formatNode(declaration.Type)
		if err != nil {
			return true
		}
		for _, name := range declaration.Names {
			result[name.Name] = typeName
		}
		return true
	})
	return result
}

func lambdaAssignmentType(root ast.Node, placeholder string, declared map[string]string) string {
	var result string
	ast.Inspect(root, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for index, value := range assignment.Rhs {
			ident, ok := value.(*ast.Ident)
			if !ok || ident.Name != placeholder || index >= len(assignment.Lhs) {
				continue
			}
			left, ok := assignment.Lhs[index].(*ast.Ident)
			if ok {
				result = declared[left.Name]
			}
		}
		return true
	})
	return result
}

func lambdaExpectedTypes(root ast.Node, placeholder string, context constructorContext, valueTypes map[string]string) []string {
	result := []string{}
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	ast.Inspect(root, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		for index, argument := range call.Args {
			ident, ok := argument.(*ast.Ident)
			if !ok || ident.Name != placeholder {
				continue
			}
			for _, expected := range lambdaExpectedTypesForCall(call, index, context, valueTypes) {
				add(expected)
			}
		}
		return true
	})
	return result
}

func lambdaExpectedTypesForCall(call *ast.CallExpr, index int, context constructorContext, valueTypes map[string]string) []string {
	result := []string{}
	switch function := call.Fun.(type) {
	case *ast.Ident:
		for _, signature := range context.FunctionSignatures[function.Name] {
			if index < len(signature.Parameters) {
				result = append(result, signature.Parameters[index].Type)
			}
		}
	case *ast.SelectorExpr:
		actualType := expressionStaticType(function.X, context, valueTypes)
		className := strings.TrimPrefix(strings.TrimSpace(actualType), "*")
		for _, signature := range context.ClassMethodSignatures[className][function.Sel.Name] {
			if index < len(signature.Parameters) {
				result = append(result, signature.Parameters[index].Type)
			}
		}
		for _, extension := range context.Extensions {
			if extension.Method.Name != function.Sel.Name || !extensionTargetMatches(extension.Target, extension.ReceiverType, actualType, context) {
				continue
			}
			parameters, err := parseParameterInfos(extension.Method.Parameters)
			if err != nil || index >= len(parameters) {
				continue
			}
			bindings := extensionTargetBindings(extension.Target, actualType)
			result = append(result, substituteLambdaType(parameters[index].Type, bindings))
		}
	}
	return result
}

func extensionTargetBindings(target, actual string) map[string]string {
	bindings := map[string]string{}
	target = strings.TrimSpace(target)
	actual = strings.TrimSpace(actual)
	if strings.HasPrefix(target, "[]") && strings.HasPrefix(actual, "[]") {
		name := strings.TrimSpace(target[2:])
		if isTypeParameterName(name) {
			bindings[name] = strings.TrimSpace(actual[2:])
		}
		return bindings
	}
	if strings.HasPrefix(target, "map[") && strings.HasPrefix(actual, "map[") {
		targetClose := strings.IndexByte(target, ']')
		actualClose := strings.IndexByte(actual, ']')
		if targetClose < 0 || actualClose < 0 {
			return bindings
		}
		targetKey := strings.TrimSpace(target[len("map["):targetClose])
		actualKey := strings.TrimSpace(actual[len("map["):actualClose])
		targetValue := strings.TrimSpace(target[targetClose+1:])
		actualValue := strings.TrimSpace(actual[actualClose+1:])
		if isTypeParameterName(targetKey) {
			bindings[targetKey] = actualKey
		}
		if isTypeParameterName(targetValue) {
			bindings[targetValue] = actualValue
		}
	}
	return bindings
}

func substituteLambdaType(source string, bindings map[string]string) string {
	for name, value := range bindings {
		parts := strings.FieldsFunc(source, func(char rune) bool {
			return !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '_'
		})
		if len(parts) == 0 {
			continue
		}
		var out strings.Builder
		for index := 0; index < len(source); {
			if index+len(name) <= len(source) && source[index:index+len(name)] == name &&
				(index == 0 || !isIdentPart(source[index-1])) &&
				(index+len(name) == len(source) || !isIdentPart(source[index+len(name)])) {
				out.WriteString(value)
				index += len(name)
				continue
			}
			out.WriteByte(source[index])
			index++
		}
		source = out.String()
	}
	return source
}

func renderStandaloneLambda(lambda lambdaSource, context constructorContext) (string, error) {
	paramTypes := map[string]string{}
	parts := make([]string, len(lambda.Params))
	for index, parameter := range lambda.Params {
		if parameter.Type == "" {
			return "", fmt.Errorf("cannot infer type of lambda parameter `%s`; provide an explicit type or use the lambda in a typed context", parameter.Name)
		}
		paramTypes[parameter.Name] = parameter.Type
		parts[index] = parameter.Name + " " + parameter.Type
	}
	result := lambdaReturnType(lambda, paramTypes, context)
	return renderLambdaWithResult(lambda, parts, result), nil
}

func renderContextualLambda(lambda lambdaSource, candidates []string, context constructorContext) (string, error) {
	viable := []string{}
	for _, candidate := range candidates {
		function, err := parseLambdaFunctionType(candidate)
		if err != nil || len(function.Parameters) != len(lambda.Params) {
			continue
		}
		paramTypes := map[string]string{}
		parts := make([]string, len(lambda.Params))
		valid := true
		for index, parameter := range lambda.Params {
			typeName := parameter.Type
			if typeName == "" {
				typeName = function.Parameters[index]
			}
			if parameter.Type != "" && strings.TrimSpace(parameter.Type) != strings.TrimSpace(function.Parameters[index]) {
				valid = false
				break
			}
			paramTypes[parameter.Name] = typeName
			parts[index] = parameter.Name + " " + typeName
		}
		if !valid {
			continue
		}
		actualResult := lambdaReturnType(lambda, paramTypes, context)
		if lambda.Block && function.Result != "" && !lambdaHasValueReturn(lambda) {
			valid = false
			continue
		}
		if !lambdaResultMatches(actualResult, function.Result, context) {
			continue
		}
		viable = append(viable, renderLambdaWithResult(lambda, parts, function.Result))
	}
	if len(viable) == 0 {
		if len(candidates) == 0 && len(lambda.Params) == 1 && lambda.Params[0].Type == "" {
			return "", fmt.Errorf("cannot infer type of lambda parameter `%s`", lambda.Params[0].Name)
		}
		return "", fmt.Errorf("lambda does not match any expected function type")
	}
	if len(viable) > 1 {
		return "", fmt.Errorf("ambiguous lambda: it matches multiple function types")
	}
	return viable[0], nil
}

func lambdaHasValueReturn(lambda lambdaSource) bool {
	if !lambda.Block {
		return strings.TrimSpace(lambda.Body) != ""
	}
	parsed, err := parser.ParseFile(
		token.NewFileSet(),
		"lambda_body.go",
		"package main\n\nfunc __gpp_lambda_body() {\n"+lambda.Body+"\n}\n",
		0,
	)
	if err != nil {
		return false
	}
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		statement, ok := node.(*ast.ReturnStmt)
		if ok && len(statement.Results) > 0 {
			found = true
		}
		return !found
	})
	return found
}

func parseLambdaFunctionType(source string) (lambdaFunctionType, error) {
	expr, err := parser.ParseExpr(strings.TrimSpace(source))
	if err != nil {
		return lambdaFunctionType{}, err
	}
	function, ok := expr.(*ast.FuncType)
	if !ok || function.Params == nil {
		return lambdaFunctionType{}, fmt.Errorf("%s is not a function type", source)
	}
	result := lambdaFunctionType{}
	for _, field := range function.Params.List {
		typeName, err := formatNode(field.Type)
		if err != nil {
			return lambdaFunctionType{}, err
		}
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			result.Parameters = append(result.Parameters, typeName)
		}
	}
	if function.Results != nil {
		if len(function.Results.List) == 1 {
			result.Result, err = formatNode(function.Results.List[0].Type)
		} else {
			parts := []string{}
			for _, field := range function.Results.List {
				typeName, formatErr := formatNode(field.Type)
				if formatErr != nil {
					return lambdaFunctionType{}, formatErr
				}
				parts = append(parts, typeName)
			}
			result.Result = "(" + strings.Join(parts, ", ") + ")"
		}
	}
	return result, err
}

func renderLambdaWithResult(lambda lambdaSource, parameters []string, result string) string {
	var out strings.Builder
	out.WriteString("func(")
	out.WriteString(strings.Join(parameters, ", "))
	out.WriteByte(')')
	if strings.TrimSpace(result) != "" {
		out.WriteByte(' ')
		out.WriteString(result)
	}
	if lambda.Block {
		out.WriteString(" ")
		out.WriteString(lambda.Body)
	} else if strings.TrimSpace(result) == "" {
		out.WriteString(" {\n")
		out.WriteString(lambda.Body)
		out.WriteString("\n}")
	} else {
		out.WriteString(" { return ")
		out.WriteString(lambda.Body)
		out.WriteString(" }")
	}
	return out.String()
}

func lambdaReturnType(lambda lambdaSource, parameters map[string]string, context constructorContext) string {
	if context.CurrentClass != "" {
		parameters["this"] = "*" + context.CurrentClass
	} else if context.CurrentExtensionReceiver != "" {
		parameters["this"] = context.CurrentExtensionReceiver
	}
	if lambda.Block {
		parsed, err := parser.ParseFile(token.NewFileSet(), "lambda_body.go", "package main\n\nfunc __gpp_lambda_body() {\n"+lambda.Body+"\n}\n", 0)
		if err != nil {
			return ""
		}
		result := ""
		ast.Inspect(parsed, func(node ast.Node) bool {
			statement, ok := node.(*ast.ReturnStmt)
			if !ok || len(statement.Results) == 0 || result != "" {
				return true
			}
			result = lambdaExpressionType(statement.Results[0], parameters, context)
			return true
		})
		return result
	}
	expr, err := parser.ParseExpr(strings.TrimSpace(lambda.Body))
	if err != nil {
		return ""
	}
	return lambdaExpressionType(expr, parameters, context)
}

func lambdaResultMatches(actual, expected string, context constructorContext) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return actual == ""
	}
	if actual == "" {
		return true
	}
	return actual == expected || isAssignableStaticType(actual, expected, context)
}

func lambdaExpressionType(expr ast.Expr, parameters map[string]string, context constructorContext) string {
	switch value := expr.(type) {
	case *ast.ParenExpr:
		return lambdaExpressionType(value.X, parameters, context)
	case *ast.BasicLit:
		switch value.Kind {
		case token.STRING:
			return "string"
		case token.INT:
			return "int"
		case token.FLOAT:
			return "float64"
		case token.CHAR:
			return "rune"
		}
	case *ast.Ident:
		if value.Name == "true" || value.Name == "false" {
			return "bool"
		}
		return parameters[value.Name]
	case *ast.SelectorExpr:
		base := lambdaExpressionType(value.X, parameters, context)
		className := strings.TrimPrefix(strings.TrimSpace(base), "*")
		if target, ok := context.Targets[className]; ok {
			for _, field := range target.Class.Fields {
				if field.Name == value.Sel.Name {
					return field.Type
				}
			}
		}
	case *ast.BinaryExpr:
		switch value.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ, token.LAND, token.LOR:
			return "bool"
		default:
			return lambdaExpressionType(value.X, parameters, context)
		}
	case *ast.UnaryExpr:
		if value.Op == token.NOT {
			return "bool"
		}
		return lambdaExpressionType(value.X, parameters, context)
	case *ast.CallExpr:
		if identifier, ok := value.Fun.(*ast.Ident); ok && identifier.Name == "len" {
			return "int"
		}
	case *ast.IndexExpr:
		base := lambdaExpressionType(value.X, parameters, context)
		if strings.HasPrefix(base, "[]") {
			return strings.TrimSpace(base[2:])
		}
		if strings.HasPrefix(base, "map[") {
			if close := strings.IndexByte(base, ']'); close >= 0 {
				return strings.TrimSpace(base[close+1:])
			}
		}
	}
	return ""
}
