package compiler

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

type parameterInfo struct {
	Name          string
	TypeAST       TypeNode
	DefaultAST    ExprNode
	DefaultTokens []Token
	HasDefault    bool
}

func (parameter parameterInfo) typeText() string {
	if parameter.TypeAST == nil {
		return ""
	}
	text, _ := typeNodeSource(parameter.TypeAST)
	return text
}

func (parameter parameterInfo) defaultText() string {
	if len(parameter.DefaultTokens) > 0 {
		return expressionTokensSource(parameter.DefaultTokens)
	}
	if parameter.DefaultAST == nil {
		return ""
	}
	text, _ := expressionNodeSource(parameter.DefaultAST)
	return text
}

type callableSignature struct {
	Name       string
	GoName     string
	Parameters []parameterInfo
	ResultAST  TypeNode
}

func (signature callableSignature) resultText() string {
	if signature.ResultAST == nil {
		return ""
	}
	text, _ := typeNodeSource(signature.ResultAST)
	return text
}

func parameterInfosFromNodes(nodes []ParameterNode) ([]parameterInfo, error) {
	result := make([]parameterInfo, 0, len(nodes))
	for _, node := range nodes {
		if node.Type == nil {
			return nil, fmt.Errorf("parameter %s has no type", node.Name)
		}
		var err error
		defaultText := ""
		if node.HasDefault {
			defaultText = expressionTokensSource(node.DefaultTokens)
			if strings.TrimSpace(defaultText) == "" && node.Default != nil {
				defaultText, err = expressionNodeSource(node.Default)
				if err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(defaultText) == "" {
				return nil, fmt.Errorf("parameter %s has an empty default value", node.Name)
			}
		}
		result = append(result, parameterInfo{
			Name:          node.Name,
			TypeAST:       node.Type,
			DefaultAST:    node.Default,
			DefaultTokens: append([]Token(nil), node.DefaultTokens...),
			HasDefault:    node.HasDefault,
		})
	}
	return result, nil
}

// parameterInfosForMethod keeps method signatures on the source-AST path.
func parameterInfosForMethod(method Method) ([]parameterInfo, error) {
	if method.Owner != nil || len(method.ParameterAST) > 0 {
		return parameterInfosFromNodes(method.ParameterAST)
	}
	return []parameterInfo{}, nil
}

func parameterSignatureKey(parameters []parameterInfo) string {
	parts := make([]string, len(parameters))
	for index, parameter := range parameters {
		parts[index] = strings.Join(strings.Fields(parameter.typeText()), " ")
	}
	return strings.Join(parts, ",")
}

func parameterTypeMap(params string) map[string]string {
	result := map[string]string{}
	parameters, err := parseParameterInfos(params)
	if err != nil {
		return result
	}
	for _, parameter := range parameters {
		if parameter.Name != "" {
			result[parameter.Name] = strings.Join(strings.Fields(parameter.typeText()), " ")
		}
	}
	return result
}

func parameterTypeMapFromNodes(parameters []ParameterNode) map[string]string {
	result := map[string]string{}
	for _, parameter := range parameters {
		if parameter.Name == "" || parameter.Type == nil {
			continue
		}
		if typeName, err := typeNodeSource(parameter.Type); err == nil {
			result[parameter.Name] = strings.Join(strings.Fields(typeName), " ")
		}
	}
	return result
}

func parameterTypeNodeMapFromNodes(parameters []ParameterNode) map[string]TypeNode {
	result := map[string]TypeNode{}
	for _, parameter := range parameters {
		if parameter.Name != "" && parameter.Type != nil {
			result[parameter.Name] = parameter.Type
		}
	}
	return result
}

func astParameterSignatureKey(functionType *ast.FuncType) string {
	if functionType == nil || functionType.Params == nil {
		return ""
	}
	parts := []string{}
	for _, field := range functionType.Params.List {
		typeName, err := astTypeSignatureKeyWithFallback(field.Type)
		if err != nil {
			continue
		}
		for range field.Names {
			parts = append(parts, strings.Join(strings.Fields(typeName), " "))
		}
		if len(field.Names) == 0 {
			parts = append(parts, strings.Join(strings.Fields(typeName), " "))
		}
	}
	return strings.Join(parts, ",")
}

func astExpressionTypeKey(expr ast.Expr) string {
	return astExpressionTypeKeyWithEnv(expr, nil)
}

func astExpressionTypeKeyWithEnv(expr ast.Expr, types map[string]string) string {
	switch value := expr.(type) {
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
	case *ast.CompositeLit:
		name, _ := astTypeName(value)
		if name != "" {
			return name
		}
		if typeName, err := astTypeSignatureKeyWithFallback(value.Type); err == nil {
			return typeName
		}
	case *ast.UnaryExpr:
		if value.Op == token.AND {
			name := astExpressionTypeKey(value.X)
			if name != "" {
				return "*" + name
			}
		}
	case *ast.Ident:
		if value.Name == "true" || value.Name == "false" {
			return "bool"
		}
		if value.Name == "nil" {
			return "nil"
		}
		if types != nil {
			if typeName := types[value.Name]; typeName != "" {
				return typeName
			}
		}
		return value.Name
	case *ast.FuncLit:
		return astFunctionTypeKey(value.Type)
	}
	return ""
}

func astFunctionTypeKey(function *ast.FuncType) string {
	if function == nil {
		return ""
	}
	parameters := []string{}
	for _, field := range function.Params.List {
		typeName, err := astTypeSignatureKeyWithFallback(field.Type)
		if err != nil {
			return ""
		}
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			parameters = append(parameters, strings.Join(strings.Fields(typeName), " "))
		}
	}
	result := ""
	if function.Results != nil {
		parts := []string{}
		for _, field := range function.Results.List {
			typeName, err := astTypeSignatureKeyWithFallback(field.Type)
			if err != nil {
				return ""
			}
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for range count {
				parts = append(parts, strings.Join(strings.Fields(typeName), " "))
			}
		}
		if len(parts) == 1 {
			result = parts[0]
		} else {
			result = "(" + strings.Join(parts, ", ") + ")"
		}
	}
	return "func(" + strings.Join(parameters, ", ") + ")" + funcResultSuffix(result)
}

func astTypeSignatureKeyWithFallback(expression ast.Expr) (string, error) {
	if expression == nil {
		return "", nil
	}
	if typeNode, ok := typeNodeFromGoExpr(expression); ok {
		return typeNodeSignatureKey(typeNode), nil
	}
	// Keep a narrow interoperability fallback for Go type syntax not yet
	// represented by TypeNode. This spelling is metadata only; it is never
	// reparsed into compiler syntax.
	text, err := formatNode(expression)
	if err != nil {
		return "", err
	}
	return strings.Join(strings.Fields(text), " "), nil
}

func funcResultSuffix(result string) string {
	if result == "" {
		return ""
	}
	return " " + result
}

func astArgumentSignatureKey(call *ast.CallExpr, types map[string]string) string {
	parts := make([]string, len(call.Args))
	for index, argument := range call.Args {
		parts[index] = astExpressionTypeKeyWithEnv(argument, types)
	}
	return strings.Join(parts, ",")
}

func methodSignaturesForClass(class *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) map[string][]callableSignature {
	if visiting[class.Name] {
		return map[string][]callableSignature{}
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	result := map[string][]callableSignature{}
	for _, method := range class.Methods {
		if method.IsStatic {
			continue
		}
		parameters, err := parameterInfosForMethod(method)
		if err != nil {
			continue
		}
		result[method.Name] = append(result[method.Name], callableSignature{
			Name:       method.Name,
			Parameters: parameters,
			ResultAST:  methodResultTypeNode(method),
		})
	}
	for _, parentName := range classParentNames(class) {
		parent, ok := classes[parentName]
		if !ok {
			continue
		}
		for name, signatures := range methodSignaturesForClass(parent, classes, visiting) {
			result[name] = append(result[name], signatures...)
		}
	}
	return result
}

func staticMethodSignaturesForClass(class *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) map[string][]callableSignature {
	if visiting[class.Name] {
		return map[string][]callableSignature{}
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	result := map[string][]callableSignature{}
	localNames := map[string]bool{}
	for _, method := range class.Methods {
		if !method.IsStatic {
			continue
		}
		localNames[method.Name] = true
		parameters, err := parameterInfosForMethod(method)
		if err != nil {
			continue
		}
		result[method.Name] = append(result[method.Name], callableSignature{
			Name:       method.Name,
			GoName:     staticMethodGoName(class, method),
			Parameters: parameters,
			ResultAST:  methodResultTypeNode(method),
		})
	}
	for _, parentName := range classParentNames(class) {
		parent, ok := classes[parentName]
		if !ok {
			continue
		}
		for name, signatures := range staticMethodSignaturesForClass(parent, classes, visiting) {
			if localNames[name] {
				continue
			}
			result[name] = append(result[name], signatures...)
		}
	}
	return result
}

func staticMethodsForClass(class *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) ([]Method, error) {
	if visiting[class.Name] {
		return nil, fmt.Errorf("inheritance cycle involving class %s", class.Name)
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	methods := []Method{}
	localNames := map[string]bool{}
	for _, method := range class.Methods {
		if method.IsStatic {
			methods = append(methods, method)
			localNames[method.Name] = true
		}
	}
	for _, parentName := range classParentNames(class) {
		parent, ok := classes[parentName]
		if !ok {
			continue
		}
		inherited, err := staticMethodsForClass(parent, classes, visiting)
		if err != nil {
			return nil, err
		}
		for _, method := range inherited {
			if !localNames[method.Name] {
				methods = append(methods, method)
			}
		}
	}
	return methods, nil
}

func functionSignaturesForFile(file *File) (map[string][]callableSignature, error) {
	result := map[string][]callableSignature{}
	for _, decl := range file.Decls {
		if function, ok := decl.(*FunctionDecl); ok {
			signature, err := functionDeclSignature(function)
			if err != nil {
				return nil, err
			}
			result[signature.Name] = append(result[signature.Name], signature)
			continue
		}
		raw, ok := decl.(*MixedDecl)
		if !ok {
			continue
		}
		if len(raw.Functions) > 0 {
			for _, function := range raw.Functions {
				signature, err := functionDeclSignature(function)
				if err != nil {
					return nil, err
				}
				result[signature.Name] = append(result[signature.Name], signature)
			}
			continue
		}
		if len(raw.GoASTDecls) > 0 {
			for _, declaration := range raw.GoASTDecls {
				if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil {
					result[function.Name.Name] = append(result[function.Name.Name], goCallableSignature(function))
				}
			}
			continue
		}
		// A compatibility declaration without a structured function or Go AST
		// has no callable metadata. Do not reparse its executable source here;
		// the parser must be the owner of function structure.
	}
	return result, nil
}

func functionDeclSignature(function *FunctionDecl) (callableSignature, error) {
	parameters, err := parameterInfosForMethod(function.Method)
	if err != nil {
		return callableSignature{}, err
	}
	return callableSignature{
		Name:       function.Name,
		Parameters: parameters,
		ResultAST:  methodResultTypeNode(function.Method),
	}, nil
}

func goCallableSignature(function *ast.FuncDecl) callableSignature {
	signature := callableSignature{Name: function.Name.Name}
	if function.Type == nil || function.Type.Params == nil {
		return signature
	}
	for _, field := range function.Type.Params.List {
		typeNode, ok := typeNodeFromGoExpr(field.Type)
		if !ok {
			typeName, err := formatNode(field.Type)
			if err != nil {
				continue
			}
			typeNode = parseTypeText(typeName)
		}
		if typeNode == nil {
			continue
		}
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for index := 0; index < count; index++ {
			name := ""
			if len(field.Names) > index {
				name = field.Names[index].Name
			}
			signature.Parameters = append(signature.Parameters, parameterInfo{Name: name, TypeAST: typeNode})
		}
	}
	if function.Type.Results != nil {
		for _, field := range function.Type.Results.List {
			typeNode, ok := typeNodeFromGoExpr(field.Type)
			if !ok {
				typeName, err := formatNode(field.Type)
				if err != nil {
					continue
				}
				typeNode = parseTypeText(typeName)
			}
			if typeNode == nil {
				continue
			}
			for range field.Names {
				if signature.ResultAST == nil {
					signature.ResultAST = typeNode
				} else if tuple, ok := signature.ResultAST.(*TupleType); ok {
					tuple.Elements = append(tuple.Elements, typeNode)
				} else {
					signature.ResultAST = &TupleType{Elements: []TypeNode{signature.ResultAST, typeNode}}
				}
			}
			if len(field.Names) == 0 {
				if signature.ResultAST == nil {
					signature.ResultAST = typeNode
				} else if tuple, ok := signature.ResultAST.(*TupleType); ok {
					tuple.Elements = append(tuple.Elements, typeNode)
				} else {
					signature.ResultAST = &TupleType{Elements: []TypeNode{signature.ResultAST, typeNode}}
				}
			}
		}
	}
	return signature
}

func functionSignaturesForProgram(program *Program) (map[string]map[string][]callableSignature, error) {
	result := map[string]map[string][]callableSignature{}
	for _, file := range program.Files {
		if result[file.Package] == nil {
			result[file.Package] = map[string][]callableSignature{}
		}
		signatures, err := functionSignaturesForFile(file)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.Name, err)
		}
		for name, values := range signatures {
			result[file.Package][name] = append(result[file.Package][name], values...)
		}
	}
	return result, nil
}

func configureFunctionOverloads(overloads *overloadContext, signatures map[string][]callableSignature) error {
	if overloads.Functions == nil {
		overloads.Functions = map[string]map[int]string{}
	}
	if overloads.FunctionTypes == nil {
		overloads.FunctionTypes = map[string]map[string]string{}
	}

	for name, candidates := range signatures {
		if len(candidates) < 2 {
			continue
		}
		seen := map[string]bool{}
		arityCounts := map[int]int{}
		for index := range candidates {
			key := parameterSignatureKey(candidates[index].Parameters)
			if seen[key] {
				return fmt.Errorf("function %s is declared more than once with parameter types %s", name, key)
			}
			seen[key] = true
			arityCounts[len(candidates[index].Parameters)]++
		}
		for index := range candidates {
			arity := len(candidates[index].Parameters)
			goName := overloadedName(name, arity)
			key := parameterSignatureKey(candidates[index].Parameters)
			if arityCounts[arity] > 1 {
				goName = overloadedTypedName(name, arity, key)
			}
			candidates[index].GoName = goName
			if overloads.FunctionTypes[name] == nil {
				overloads.FunctionTypes[name] = map[string]string{}
			}
			overloads.FunctionTypes[name][key] = goName
			if arityCounts[arity] == 1 {
				if overloads.Functions[name] == nil {
					overloads.Functions[name] = map[int]string{}
				}
				overloads.Functions[name][arity] = goName
			}
		}
		signatures[name] = candidates
	}
	return nil
}

func parseParameterInfos(params string) ([]parameterInfo, error) {
	if strings.TrimSpace(params) == "" {
		return nil, nil
	}
	parts, err := splitTopLevel(params, ',')
	if err != nil {
		return nil, err
	}

	result := []parameterInfo{}
	seenDefault := false
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		defaultAt := topLevelEquals(part)
		left := part
		defaultValue := ""
		hasDefault := defaultAt >= 0
		if seenDefault && !hasDefault {
			return nil, fmt.Errorf("required parameters must come before default parameters")
		}
		if hasDefault {
			seenDefault = true
			left = strings.TrimSpace(part[:defaultAt])
			defaultValue = strings.TrimSpace(part[defaultAt+1:])
			if defaultValue == "" {
				return nil, fmt.Errorf("parameter %d has an empty default value", index+1)
			}
		}

		tokens, err := LexSource("parameter", left)
		if err != nil {
			return nil, err
		}
		filtered := significantSyntaxTokens(tokens)
		if len(filtered) == 0 {
			return nil, fmt.Errorf("invalid parameter %q", part)
		}

		name := ""
		typeTokens := filtered
		if parameterNameCandidate(filtered) {
			candidate, candidateErr := ParseTypeTokens(filtered[1:])
			if candidateErr == nil && candidate != nil {
				name = filtered[0].Text
				typeTokens = filtered[1:]
			}
		}
		typeNode, err := ParseTypeTokens(typeTokens)
		if err != nil || typeNode == nil {
			if err != nil {
				return nil, fmt.Errorf("invalid parameter %q: %w", part, err)
			}
			return nil, fmt.Errorf("invalid parameter %q", part)
		}
		if hasDefault && name == "" {
			return nil, fmt.Errorf("defaults require a parameter name in %q", part)
		}
		var defaultAST ExprNode
		var defaultTokens []Token
		if hasDefault {
			defaultTokens, err = LexSource("parameter default", defaultValue)
			if err != nil {
				return nil, err
			}
			defaultAST, err = ParseExpressionTokens(defaultTokens)
			if err != nil || defaultAST == nil {
				if err == nil {
					err = fmt.Errorf("empty default expression")
				}
				return nil, fmt.Errorf("invalid default for parameter %q: %w", name, err)
			}
		}
		result = append(result, parameterInfo{
			Name:          name,
			TypeAST:       typeNode,
			DefaultAST:    defaultAST,
			DefaultTokens: defaultTokens,
			HasDefault:    hasDefault,
		})
	}

	return result, nil
}

func parameterNameCandidate(tokens []Token) bool {
	if len(tokens) < 2 || (tokens[0].Kind != TokenIdentifier && tokens[0].Kind != TokenKeyword) {
		return false
	}
	switch tokens[0].Text {
	case "chan", "func", "interface", "map", "struct":
		return false
	default:
		return true
	}
}

func stripParameterDefaults(params string) (string, error) {
	parts, err := splitTopLevel(params, ',')
	if err != nil {
		return "", err
	}
	for index, part := range parts {
		if equal := topLevelEquals(part); equal >= 0 {
			parts[index] = part[:equal]
		}
	}
	return strings.Join(parts, ","), nil
}

func formatNode(node ast.Node) (string, error) {
	var output strings.Builder
	if err := format.Node(&output, token.NewFileSet(), node); err != nil {
		return "", err
	}
	return output.String(), nil
}

func topLevelEquals(src string) int {
	parenDepth := 0
	braceDepth := 0
	bracketDepth := 0

	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '"', '\'':
			end, err := skipQuoted(src, i, src[i])
			if err != nil {
				return -1
			}
			i = end
		case '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return -1
			}
			i += end + 1
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		case '{':
			braceDepth++
		case '}':
			braceDepth--
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
		case '=':
			if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
				return i
			}
		}
	}

	return -1
}

type functionParameterRange struct {
	Open  int
	Close int
}

func namedFunctionParameterRanges(src string) ([]functionParameterRange, error) {
	ranges := []functionParameterRange{}
	for i := 0; i < len(src); {
		switch src[i] {
		case '"', '\'':
			end, err := skipQuoted(src, i, src[i])
			if err != nil {
				return nil, err
			}
			i = end + 1
			continue
		case '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return nil, fmt.Errorf("unterminated raw string")
			}
			i += end + 2
			continue
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				i += 2
				for i < len(src) && src[i] != '\n' {
					i++
				}
				continue
			}
			if i+1 < len(src) && src[i+1] == '*' {
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return nil, fmt.Errorf("unterminated comment")
				}
				i += end + 4
				continue
			}
		}

		if !keywordAt(src, i, "func") {
			i++
			continue
		}
		pos := skipSpace(src, i+len("func"))
		if _, n := readIdent(src[pos:]); n == 0 {
			i += len("func")
			continue
		}
		pos += len(readIdentValue(src[pos:]))
		pos = skipSpace(src, pos)
		if pos >= len(src) || src[pos] != '(' {
			i++
			continue
		}
		close, err := findMatchingParen(src, pos)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, functionParameterRange{Open: pos, Close: close})
		i = close + 1
	}
	return ranges, nil
}

func readIdentValue(src string) string {
	name, _ := readIdent(src)
	return name
}

func stripDefaultParameterValues(src string) (string, error) {
	ranges, err := namedFunctionParameterRanges(src)
	if err != nil {
		return "", err
	}
	edits := []struct {
		start int
		end   int
	}{}
	for _, parameterRange := range ranges {
		parts, err := splitTopLevel(src[parameterRange.Open+1:parameterRange.Close], ',')
		if err != nil {
			return "", err
		}
		base := parameterRange.Open + 1
		for _, part := range parts {
			equal := topLevelEquals(part)
			if equal < 0 {
				base += len(part) + 1
				continue
			}
			start := base + equal
			for start > base && (src[start-1] == ' ' || src[start-1] == '\t') {
				start--
			}
			end := base + len(part)
			edits = append(edits, struct{ start, end int }{start: start, end: end})
			base += len(part) + 1
		}
	}
	for i := len(edits) - 1; i >= 0; i-- {
		src = src[:edits[i].start] + src[edits[i].end:]
	}
	return src, nil
}

func collectFunctionSignatures(src string) ([]callableSignature, error) {
	ranges, err := namedFunctionParameterRanges(src)
	if err != nil {
		return nil, err
	}
	result := []callableSignature{}
	for _, parameterRange := range ranges {
		nameStart := parameterRange.Open - 1
		for nameStart >= 0 && (src[nameStart] == ' ' || src[nameStart] == '\t' || src[nameStart] == '\n' || src[nameStart] == '\r') {
			nameStart--
		}
		end := nameStart + 1
		for nameStart >= 0 && isIdentPart(src[nameStart]) {
			nameStart--
		}
		name := src[nameStart+1 : end]
		if name == "" {
			continue
		}
		parameters, err := parseParameterInfos(src[parameterRange.Open+1 : parameterRange.Close])
		if err != nil {
			return nil, fmt.Errorf("function %s: %w", name, err)
		}
		resultStart := parameterRange.Close + 1
		for resultStart < len(src) && (src[resultStart] == ' ' || src[resultStart] == '\t' || src[resultStart] == '\n' || src[resultStart] == '\r') {
			resultStart++
		}
		resultEnd := resultStart
		for resultEnd < len(src) && src[resultEnd] != '{' {
			resultEnd++
		}
		result = append(result, callableSignature{
			Name:       name,
			Parameters: parameters,
			ResultAST:  parseTypeText(strings.TrimSpace(src[resultStart:resultEnd])),
		})
	}
	return result, nil
}

func callableHasDefaults(signature callableSignature) bool {
	for _, parameter := range signature.Parameters {
		if parameter.HasDefault {
			return true
		}
	}
	return false
}

func resolveCallableCall(name string, args []string, signatures []callableSignature) ([]string, bool, error) {
	named := false
	for _, arg := range args {
		if topLevelColon(arg) >= 0 {
			named = true
			break
		}
	}
	if !named {
		for _, signature := range signatures {
			if len(args) > len(signature.Parameters) || len(args) < requiredParameterCount(signature) {
				continue
			}
			if len(args) == len(signature.Parameters) || callableHasDefaults(signature) {
				result := append([]string{}, args...)
				for index := len(args); index < len(signature.Parameters); index++ {
					if !signature.Parameters[index].HasDefault {
						result = nil
						break
					}
					result = append(result, signature.Parameters[index].defaultText())
				}
				if result != nil {
					return result, true, nil
				}
			}
		}
		return args, false, nil
	}

	for _, signature := range signatures {
		values := make([]string, len(signature.Parameters))
		provided := make([]bool, len(signature.Parameters))
		matched := true
		for _, arg := range args {
			colon := topLevelColon(arg)
			if colon < 0 {
				return args, false, fmt.Errorf("%s call mixes positional and named arguments", name)
			}
			parameterName := strings.TrimSpace(arg[:colon])
			value := strings.TrimSpace(arg[colon+1:])
			found := -1
			for index, parameter := range signature.Parameters {
				if parameter.Name == parameterName {
					found = index
					break
				}
			}
			if found < 0 {
				matched = false
				break
			}
			if provided[found] {
				return args, false, fmt.Errorf("%s call repeats named argument %s", name, parameterName)
			}
			provided[found] = true
			values[found] = value
		}
		if !matched {
			continue
		}
		for index, parameter := range signature.Parameters {
			if provided[index] {
				continue
			}
			if !parameter.HasDefault {
				matched = false
				break
			}
			values[index] = parameter.defaultText()
		}
		if matched {
			return values, true, nil
		}
	}

	return args, false, fmt.Errorf("no matching %s signature for named arguments", name)
}

func lowerCallableCallNode(call *CallExpr, context constructorContext) (ExprNode, bool, error) {
	if call == nil || call.Callee == nil {
		return nil, false, nil
	}
	var signatures []callableSignature
	staticCall := false
	switch function := call.Callee.(type) {
	case *NameExpr:
		signatures = append(signatures, context.FunctionSignatures[function.Name]...)
		if len(signatures) == 0 && context.CurrentClass != "" {
			signatures = append(signatures, context.MethodSignatures[function.Name]...)
			if len(signatures) == 0 {
				signatures = append(signatures, context.ClassMethodSignatures[context.CurrentClass][function.Name]...)
			}
		}
	case *SelectorExpr:
		if receiver, ok := function.Receiver.(*NameExpr); ok && receiver.Name == "this" {
			signatures = append(signatures, context.MethodSignatures[function.Name]...)
			if len(signatures) == 0 {
				signatures = append(signatures, context.ClassMethodSignatures[context.CurrentClass][function.Name]...)
			}
		} else {
			// Imported static calls use a qualified receiver such as
			// `model.User`, which is represented by a SelectorExpr. Resolve
			// the complete receiver structurally instead of leaving it as a
			// native Go selector.
			receiverKey := staticMethodReceiverKey(function.Receiver, function.Name, context)
			staticCall = receiverKey != ""
			signatures = append(signatures, context.StaticMethodSignatures[receiverKey][function.Name]...)
		}
	case *IndexExpr:
		selector, ok := function.Receiver.(*SelectorExpr)
		if !ok {
			return nil, false, nil
		}
		clone := *call
		clone.Callee = selector
		lowered, handled, err := lowerCallableCallNode(&clone, context)
		if err != nil || !handled {
			return lowered, handled, err
		}
		loweredCall, ok := lowered.(*CallExpr)
		if !ok {
			return lowered, true, nil
		}
		return &CallExpr{
			Callee:    &IndexExpr{Receiver: loweredCall.Callee, Index: function.Index, SpanValue: function.SpanValue},
			Arguments: loweredCall.Arguments,
			SpanValue: call.SpanValue,
		}, true, nil
	case *IndexListExpr:
		selector, ok := function.Receiver.(*SelectorExpr)
		if !ok {
			return nil, false, nil
		}
		clone := *call
		clone.Callee = selector
		lowered, handled, err := lowerCallableCallNode(&clone, context)
		if err != nil || !handled {
			return lowered, handled, err
		}
		loweredCall, ok := lowered.(*CallExpr)
		if !ok {
			return lowered, true, nil
		}
		return &CallExpr{
			Callee:    &IndexListExpr{Receiver: loweredCall.Callee, Indices: function.Indices, SpanValue: function.SpanValue},
			Arguments: loweredCall.Arguments,
			SpanValue: call.SpanValue,
		}, true, nil
	default:
		return nil, false, nil
	}
	if len(signatures) != 1 {
		return nil, false, nil
	}
	signature := signatures[0]
	if function, ok := call.Callee.(*NameExpr); ok && !staticCall && context.CurrentClass != "" &&
		len(context.FunctionSignatures[function.Name]) == 0 && len(context.ClassMethodSignatures[context.CurrentClass][function.Name]) == 1 {
		call.Callee = &SelectorExpr{Receiver: &NameExpr{Name: "this"}, Name: function.Name, SpanValue: function.Span()}
		return call, true, nil
	}
	named := false
	for _, argument := range call.Arguments {
		if argument.Name != "" {
			named = true
			break
		}
	}
	if !named && len(call.Arguments) == len(signature.Parameters) {
		if !staticCall || signature.GoName == "" {
			return call, false, nil
		}
		return &CallExpr{
			Callee:    staticCallCallee(call.Callee, signature.GoName),
			Arguments: append([]CallArg(nil), call.Arguments...),
			SpanValue: call.SpanValue,
		}, true, nil
	}
	if !named && len(call.Arguments) > len(signature.Parameters) {
		return nil, false, nil
	}
	if !named && len(call.Arguments) < requiredParameterCount(signature) {
		return nil, false, nil
	}
	values := make([]ExprNode, len(signature.Parameters))
	if named {
		provided := make([]bool, len(signature.Parameters))
		for _, argument := range call.Arguments {
			if argument.Name == "" {
				return nil, true, fmt.Errorf("%s call mixes positional and named arguments", callableName(call))
			}
			index := -1
			for parameterIndex, parameter := range signature.Parameters {
				if parameter.Name == argument.Name {
					index = parameterIndex
					break
				}
			}
			if index < 0 {
				return nil, true, fmt.Errorf("%s call has unknown named argument %s", callableName(call), argument.Name)
			}
			if provided[index] {
				return nil, true, fmt.Errorf("%s call repeats named argument %s", callableName(call), argument.Name)
			}
			provided[index] = true
			values[index] = argument.Value
		}
		for index, parameter := range signature.Parameters {
			if provided[index] {
				continue
			}
			if !parameter.HasDefault || parameter.DefaultAST == nil {
				return nil, false, nil
			}
			values[index] = parameter.DefaultAST
		}
	} else {
		for index, argument := range call.Arguments {
			if argument.Name != "" || argument.Value == nil {
				return nil, false, nil
			}
			values[index] = argument.Value
		}
		for index := len(call.Arguments); index < len(signature.Parameters); index++ {
			if !signature.Parameters[index].HasDefault || signature.Parameters[index].DefaultAST == nil {
				return nil, false, nil
			}
			values[index] = signature.Parameters[index].DefaultAST
		}
	}
	arguments := make([]CallArg, 0, len(values))
	for _, value := range values {
		if value == nil {
			return nil, false, nil
		}
		arguments = append(arguments, CallArg{Value: value})
	}
	callee := call.Callee
	if staticCall && signature.GoName != "" {
		callee = staticCallCallee(call.Callee, signature.GoName)
	}
	return &CallExpr{Callee: callee, Arguments: arguments, SpanValue: call.SpanValue}, true, nil
}

func staticCallCallee(original ExprNode, goName string) ExprNode {
	selector, ok := original.(*SelectorExpr)
	if !ok || goName == "" {
		return &NameExpr{Name: goName}
	}
	receiverPath, ok := directSelectorPath(selector.Receiver)
	if !ok {
		return &NameExpr{Name: goName, SpanValue: original.Span()}
	}
	qualifier := ""
	if dot := strings.LastIndex(receiverPath, "."); dot >= 0 {
		qualifier = receiverPath[:dot]
	}
	if qualifier == "" {
		return &NameExpr{Name: goName, SpanValue: original.Span()}
	}
	parts := strings.Split(qualifier, ".")
	result := ExprNode(&NameExpr{Name: parts[0], SpanValue: original.Span()})
	for _, part := range parts[1:] {
		result = &SelectorExpr{Receiver: result, Name: part, SpanValue: original.Span()}
	}
	return &SelectorExpr{Receiver: result, Name: goName, SpanValue: original.Span()}
}

func staticMethodReceiverKey(receiver ExprNode, methodName string, context constructorContext) string {
	if receiver == nil {
		return ""
	}
	if name, ok := receiver.(*NameExpr); ok {
		if len(context.StaticMethodSignatures[name.Name][methodName]) > 0 {
			return name.Name
		}
	}
	qualified, ok := directSelectorPath(receiver)
	if !ok {
		return ""
	}
	if len(context.StaticMethodSignatures[qualified][methodName]) > 0 {
		return qualified
	}
	return ""
}

// lowerOverloadCallNode applies the semantic Go name chosen for an overloaded
// call without using a source-span edit. The overload resolver already works
// from typed call arguments and local type information; this adapter only
// replaces the callee node and leaves argument lowering to the normal AST walk.
func lowerOverloadCallNode(call *CallExpr, context constructorContext) (ExprNode, bool, error) {
	if call == nil || call.Callee == nil {
		return nil, false, nil
	}
	name, found, err := overloadCallName(call, context.CurrentParameterTypes, context.Overloads)
	if err != nil {
		return nil, true, err
	}
	if !found || name == "" {
		return nil, false, nil
	}
	switch callee := call.Callee.(type) {
	case *NameExpr:
		if callee.Name == name {
			return call, false, nil
		}
		return &CallExpr{
			Callee:    &NameExpr{Name: name, SpanValue: callee.SpanValue},
			Arguments: append([]CallArg(nil), call.Arguments...),
			SpanValue: call.SpanValue,
		}, true, nil
	case *SelectorExpr:
		if callee.Name == name {
			return call, false, nil
		}
		return &CallExpr{
			Callee: &SelectorExpr{
				Receiver:  callee.Receiver,
				Name:      name,
				SpanValue: callee.SpanValue,
			},
			Arguments: append([]CallArg(nil), call.Arguments...),
			SpanValue: call.SpanValue,
		}, true, nil
	default:
		return nil, false, nil
	}
}

func callableName(call *CallExpr) string {
	if call == nil {
		return "call"
	}
	if name, ok := directSelectorPath(call.Callee); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return "call"
}

func requiredParameterCount(signature callableSignature) int {
	count := 0
	for _, parameter := range signature.Parameters {
		if !parameter.HasDefault {
			count++
		}
	}
	return count
}

func transformCallableCalls(src string, context constructorContext) (string, error) {
	if transformed, handled, err := transformCallableCallsAST(src, context); handled {
		return transformed, err
	}
	return src, nil
}

type callableCallRewrite struct {
	call        *CallExpr
	start       int
	end         int
	replacement string
}

func transformCallableCallsAST(src string, context constructorContext) (string, bool, error) {
	tokens, err := LexSource("calls", src)
	if err != nil {
		return src, false, nil
	}
	blocks := []*BlockStmt{}
	// A complete top-level function is represented as a TokenStmt when parsed
	// as a generic body. Prefer its structured FunctionDecl body so callable
	// lowering never falls back to scanning declaration text.
	functions := parseTopLevelFunctions("calls", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function != nil && function.Method.BodyAST != nil {
				blocks = append(blocks, function.Method.BodyAST)
			}
		}
	} else if block, parseErr := ParseBodyAST(tokens); parseErr == nil && block != nil {
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return src, false, nil
	}
	candidates := []callableCallRewrite{}
	for _, block := range blocks {
		collectCallableBodyExpressions(block, func(expression ExprNode) {
			collectCallableCallRewrites(expression, src, context, &candidates)
		})
	}
	if len(candidates) == 0 {
		return src, false, nil
	}
	outermost := make([]callableCallRewrite, 0, len(candidates))
	for index, candidate := range candidates {
		nested := false
		for otherIndex, other := range candidates {
			if index != otherIndex && other.start <= candidate.start && other.end >= candidate.end &&
				(other.start < candidate.start || other.end > candidate.end) {
				nested = true
				break
			}
		}
		if !nested {
			outermost = append(outermost, candidate)
		}
	}
	sort.Slice(outermost, func(i, j int) bool { return outermost[i].start > outermost[j].start })
	for _, candidate := range outermost {
		if candidate.start < 0 || candidate.end > len(src) || candidate.start >= candidate.end {
			return src, false, nil
		}
		src = src[:candidate.start] + candidate.replacement + src[candidate.end:]
	}
	return src, true, nil
}

func collectCallableBodyExpressions(block *BlockStmt, visit func(ExprNode)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectCallableStmtExpressions(statement, visit)
	}
}

func collectCallableStmtExpressions(statement Stmt, visit func(ExprNode)) {
	walkStmtExpressions(statement, visit)
}

func collectCallableStructuredHeader(statement Stmt, visit func(ExprNode)) {
	switch value := statement.(type) {
	case *IfStmt:
		visit(value.Condition)
	case *ForStmt:
		visit(value.Init)
		visit(value.Condition)
		visit(value.Post)
		visit(value.RangeExpr)
	case *SwitchStmt:
		visit(value.Tag)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			visit(expression)
		}
	}
}

func collectCallableCallRewrites(expression ExprNode, src string, context constructorContext, result *[]callableCallRewrite) {
	if expression == nil {
		return
	}
	switch value := expression.(type) {
	case *CallExpr:
		if rewrite, ok, err := callableCallRewriteFor(value, src, context); err == nil && ok {
			*result = append(*result, rewrite)
		}
		collectCallableCallRewrites(value.Callee, src, context, result)
		for _, argument := range value.Arguments {
			collectCallableCallRewrites(argument.Value, src, context, result)
		}
	case *UnaryExpr:
		collectCallableCallRewrites(value.Operand, src, context, result)
	case *BinaryExpr:
		collectCallableCallRewrites(value.Left, src, context, result)
		collectCallableCallRewrites(value.Right, src, context, result)
	case *AssignmentExpr:
		for _, expression := range value.Left {
			collectCallableCallRewrites(expression, src, context, result)
		}
		for _, expression := range value.Right {
			collectCallableCallRewrites(expression, src, context, result)
		}
	case *SelectorExpr:
		collectCallableCallRewrites(value.Receiver, src, context, result)
	case *IndexExpr:
		collectCallableCallRewrites(value.Receiver, src, context, result)
		collectCallableCallRewrites(value.Index, src, context, result)
	case *IndexListExpr:
		collectCallableCallRewrites(value.Receiver, src, context, result)
		for _, index := range value.Indices {
			collectCallableCallRewrites(index, src, context, result)
		}
	case *SliceExpr:
		collectCallableCallRewrites(value.Receiver, src, context, result)
		collectCallableCallRewrites(value.Low, src, context, result)
		collectCallableCallRewrites(value.High, src, context, result)
		collectCallableCallRewrites(value.Max, src, context, result)
	case *TypeAssertExpr:
		collectCallableCallRewrites(value.Expression, src, context, result)
	case *PostfixExpr:
		collectCallableCallRewrites(value.Expression, src, context, result)
	case *SpreadExpr:
		collectCallableCallRewrites(value.Expression, src, context, result)
	case *TypeExpr:
		// Type expressions do not contain callable expressions.
	case *SendExpr:
		collectCallableCallRewrites(value.Channel, src, context, result)
		collectCallableCallRewrites(value.Value, src, context, result)
	case *ParenthesizedExpr:
		collectCallableCallRewrites(value.Inner, src, context, result)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectCallableCallRewrites(element.Key, src, context, result)
			collectCallableCallRewrites(element.Value, src, context, result)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectCallableCallRewrites(segment.Expression, src, context, result)
		}
	case *LambdaExpr:
		collectCallableCallRewrites(value.Body, src, context, result)
		collectCallableBodyExpressions(value.BlockBody, func(expression ExprNode) {
			collectCallableCallRewrites(expression, src, context, result)
		})
	case *FunctionLiteralExpr:
		collectCallableBodyExpressions(value.Body, func(expression ExprNode) {
			collectCallableCallRewrites(expression, src, context, result)
		})
	}
}

func expressionSourceForRewrite(expression ExprNode, src string) (string, error) {
	if expression != nil {
		span := expression.Span()
		switch expression.(type) {
		case *FunctionLiteralExpr, *LambdaExpr:
			if span.Start >= 0 && span.End <= len(src) && span.Start < span.End {
				return src[span.Start:span.End], nil
			}
		}
	}
	return expressionNodeSource(expression)
}

func callableCallRewriteFor(call *CallExpr, src string, context constructorContext) (callableCallRewrite, bool, error) {
	if call == nil {
		return callableCallRewrite{}, false, nil
	}
	callee, err := expressionSourceForRewrite(call.Callee, src)
	if err != nil {
		return callableCallRewrite{}, false, nil
	}
	callee = strings.TrimSpace(callee)
	args := make([]string, 0, len(call.Arguments))
	for _, argument := range call.Arguments {
		value, valueErr := expressionSourceForRewrite(argument.Value, src)
		if valueErr != nil {
			return callableCallRewrite{}, false, nil
		}
		if argument.Name != "" {
			args = append(args, argument.Name+": "+value)
		} else {
			args = append(args, value)
		}
	}

	replacement := ""
	changed := false
	switch function := call.Callee.(type) {
	case *NameExpr:
		signatures := context.FunctionSignatures[function.Name]
		if len(signatures) > 0 {
			resolved, resolvedChanged, resolveErr := resolveCallableCall(function.Name, args, signatures)
			if resolveErr != nil {
				return callableCallRewrite{}, false, resolveErr
			}
			if resolvedChanged {
				resolved, resolveErr = transformCallableArgumentSources(resolved, context)
				if resolveErr != nil {
					return callableCallRewrite{}, false, resolveErr
				}
				replacement = function.Name + "(" + strings.Join(resolved, ", ") + ")"
				changed = true
			}
		}
		if !changed && context.CurrentClass != "" && len(context.FunctionSignatures[function.Name]) == 0 {
			signatures = context.MethodSignatures[function.Name]
			if len(signatures) == 0 {
				signatures = context.ClassMethodSignatures[context.CurrentClass][function.Name]
			}
			if len(signatures) > 0 {
				resolved, resolvedChanged, resolveErr := resolveCallableCall(function.Name, args, signatures)
				if resolveErr != nil {
					return callableCallRewrite{}, false, resolveErr
				}
				if resolvedChanged {
					resolved, resolveErr = transformCallableArgumentSources(resolved, context)
					if resolveErr != nil {
						return callableCallRewrite{}, false, resolveErr
					}
					replacement = "this." + function.Name + "(" + strings.Join(resolved, ", ") + ")"
					changed = true
				}
			}
		}
	case *IndexExpr:
		selector, ok := function.Receiver.(*SelectorExpr)
		if !ok {
			break
		}
		// Generic static calls are represented as an index expression whose
		// receiver is the selector. Reuse the typed selector resolver, then
		// restore the type arguments in the generated call.
		clone := *call
		clone.Callee = selector
		rewrite, ok, err := callableCallRewriteFor(&clone, src, context)
		if err != nil || !ok {
			return callableCallRewrite{}, ok, err
		}
		typeText, typeErr := expressionNodeSource(function.Index)
		if typeErr != nil {
			return callableCallRewrite{}, false, typeErr
		}
		if open := strings.IndexByte(rewrite.replacement, '('); open >= 0 {
			rewrite.replacement = rewrite.replacement[:open] + "[" + strings.TrimSpace(typeText) + "]" + rewrite.replacement[open:]
		}
		return rewrite, true, nil
	case *IndexListExpr:
		selector, ok := function.Receiver.(*SelectorExpr)
		if !ok {
			break
		}
		clone := *call
		clone.Callee = selector
		rewrite, ok, err := callableCallRewriteFor(&clone, src, context)
		if err != nil || !ok {
			return callableCallRewrite{}, ok, err
		}
		typeParts := make([]string, 0, len(function.Indices))
		for _, index := range function.Indices {
			typeText, typeErr := expressionNodeSource(index)
			if typeErr != nil {
				return callableCallRewrite{}, false, typeErr
			}
			typeParts = append(typeParts, strings.TrimSpace(typeText))
		}
		if open := strings.IndexByte(rewrite.replacement, '('); open >= 0 {
			rewrite.replacement = rewrite.replacement[:open] + "[" + strings.Join(typeParts, ", ") + "]" + rewrite.replacement[open:]
		}
		return rewrite, true, nil
	case *SelectorExpr:
		receiver, receiverErr := expressionSourceForRewrite(function.Receiver, src)
		if receiverErr != nil {
			return callableCallRewrite{}, false, nil
		}
		receiver = strings.TrimSpace(receiver)
		if receiver == "this" {
			signatures := context.MethodSignatures[function.Name]
			if len(signatures) == 0 {
				signatures = context.ClassMethodSignatures[context.CurrentClass][function.Name]
			}
			if len(signatures) > 0 {
				resolved, resolvedChanged, resolveErr := resolveCallableCall(function.Name, args, signatures)
				if resolveErr != nil {
					return callableCallRewrite{}, false, resolveErr
				}
				if resolvedChanged {
					resolved, resolveErr = transformCallableArgumentSources(resolved, context)
					if resolveErr != nil {
						return callableCallRewrite{}, false, resolveErr
					}
					replacement = "this." + function.Name + "(" + strings.Join(resolved, ", ") + ")"
					changed = true
				}
			}
		} else if target, ok := context.Targets[receiver]; ok {
			_ = target
			signatures := context.StaticMethodSignatures[receiver][function.Name]
			if len(signatures) > 0 {
				goName, resolved, resolveErr := resolveStaticMethodCall(receiver, function.Name, args, signatures, context)
				if resolveErr != nil {
					return callableCallRewrite{}, false, resolveErr
				}
				resolved, resolveErr = transformCallableArgumentSources(resolved, context)
				if resolveErr != nil {
					return callableCallRewrite{}, false, resolveErr
				}
				functionName := goName
				if dot := strings.LastIndex(receiver, "."); dot > 0 {
					functionName = receiver[:dot] + "." + goName
				}
				replacement = functionName + "(" + strings.Join(resolved, ", ") + ")"
				changed = true
			}
		}
	}
	if !changed {
		return callableCallRewrite{}, false, nil
	}
	span := call.Span()
	return callableCallRewrite{call: call, start: span.Start, end: span.End, replacement: replacement}, true, nil
}

func transformCallableArgumentSources(values []string, context constructorContext) ([]string, error) {
	result := make([]string, len(values))
	for index, value := range values {
		transformed, err := transformCallableCalls(strings.TrimSpace(value), context)
		if err != nil {
			return nil, err
		}
		result[index] = transformed
	}
	return result, nil
}

func resolveStaticMethodCall(
	className string,
	methodName string,
	args []string,
	signatures []callableSignature,
	context constructorContext,
) (string, []string, error) {
	var matches []struct {
		signature callableSignature
		args      []string
	}
	for _, signature := range signatures {
		resolved, _, err := resolveCallableCall(
			methodName,
			args,
			[]callableSignature{signature},
		)
		if err != nil {
			continue
		}
		if len(signatures) == 1 {
			return signature.GoName, resolved, nil
		}
		match := true
		for index, argument := range resolved {
			if index >= len(signature.Parameters) {
				match = false
				break
			}
			expression, err := parseStaticArgumentNode(argument)
			if err != nil {
				match = false
				break
			}
			actual := staticExpressionTypeNode(expression, context, nil)
			expected := signature.Parameters[index].typeText()
			if actual != "" && actual != expected &&
				!isAssignableStaticType(actual, expected, context) &&
				!isUnknownStaticArgumentNode(expression) {
				match = false
				break
			}
		}
		if match {
			matches = append(matches, struct {
				signature callableSignature
				args      []string
			}{signature: signature, args: resolved})
		}
	}
	if len(matches) == 1 {
		return matches[0].signature.GoName, matches[0].args, nil
	}
	if len(matches) > 1 {
		return "", nil, fmt.Errorf("ambiguous static method %s.%s", className, methodName)
	}
	return "", nil, fmt.Errorf("no matching static method %s.%s", className, methodName)
}

func parseStaticArgumentNode(source string) (ExprNode, error) {
	tokens, err := LexSource("static method argument", strings.TrimSpace(source))
	if err != nil {
		return nil, err
	}
	return ParseExpressionTokens(tokens)
}

func staticExpressionTypeNode(expression ExprNode, context constructorContext, valueTypes map[string]string) string {
	switch value := expression.(type) {
	case *LiteralExpr:
		switch value.Kind {
		case TokenString, TokenRawString:
			return "string"
		case TokenRune:
			return "rune"
		case TokenNumber:
			if strings.ContainsAny(value.Text, ".eEpP") {
				return "float64"
			}
			return "int"
		}
		switch value.Text {
		case "true", "false":
			return "bool"
		case "nil":
			return "nil"
		}
	case *NameExpr:
		if valueTypes != nil && valueTypes[value.Name] != "" {
			return valueTypes[value.Name]
		}
		if context.GlobalValueTypes != nil && context.GlobalValueTypes[value.Name] != "" {
			return context.GlobalValueTypes[value.Name]
		}
		return value.Name
	case *InterpolatedStringExpr:
		return "string"
	case *CompositeLiteralExpr:
		if typeName, err := typeNodeSource(value.Type); err == nil {
			return typeName
		}
	case *IndexListExpr:
		return staticExpressionTypeNode(value.Receiver, context, valueTypes)
	case *ParenthesizedExpr:
		return staticExpressionTypeNode(value.Inner, context, valueTypes)
	case *SliceExpr:
		receiverType := strings.TrimSpace(staticExpressionTypeNode(value.Receiver, context, valueTypes))
		if strings.HasPrefix(receiverType, "[]") {
			return strings.TrimPrefix(receiverType, "[]")
		}
		if receiverType == "string" {
			return "string"
		}
	case *TypeAssertExpr:
		if value.TypeSwitch {
			return ""
		}
		if value.Type != nil {
			if typeText, err := typeNodeSource(value.Type); err == nil {
				return typeText
			}
		}
	case *PostfixExpr:
		return staticExpressionTypeNode(value.Expression, context, valueTypes)
	case *SpreadExpr:
		return staticExpressionTypeNode(value.Expression, context, valueTypes)
	case *TypeExpr:
		if value.Type != nil {
			if typeText, err := typeNodeSource(value.Type); err == nil {
				return typeText
			}
		}
	case *SendExpr:
		return staticExpressionTypeNode(value.Value, context, valueTypes)
	case *FunctionLiteralExpr:
		if value.Type != nil {
			if typeText, err := typeNodeSource(value.Type); err == nil {
				return typeText
			}
		}
	case *UnaryExpr:
		inner := staticExpressionTypeNode(value.Operand, context, valueTypes)
		if value.Operator == "&" && inner != "" && !strings.HasPrefix(inner, "*") {
			return "*" + inner
		}
		return inner
	case *BinaryExpr:
		switch value.Operator {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			return "bool"
		default:
			return staticExpressionTypeNode(value.Left, context, valueTypes)
		}
	case *SelectorExpr:
		if qualified, ok := directSelectorPath(value); ok {
			if target, ok := context.Targets[qualified]; ok && target.Class != nil {
				return qualified
			}
		}
		if key, enum, ok := enumReferenceExpr(value.Receiver, context); ok {
			if _, exists := enumMember(enum, value.Name); exists {
				return enumReferenceType(key, enum)
			}
		}
		baseType := strings.TrimPrefix(strings.TrimSpace(staticExpressionTypeNode(value.Receiver, context, valueTypes)), "*")
		if target, ok := context.Targets[baseType]; ok {
			for _, field := range target.Class.Fields {
				if field.Name == value.Name {
					return fieldTypeSource(field)
				}
			}
			for _, signature := range context.ClassMethodSignatures[baseType][value.Name] {
				if signature.resultText() != "" {
					return signature.resultText()
				}
			}
		}
		if nativeType := nativeSelectorFieldTypeNode(value, context, valueTypes); nativeType != "" {
			return nativeType
		}
	case *CallExpr:
		if result := staticCallResultTypeNode(value, context, valueTypes); result != "" {
			return result
		}
	}
	return ""
}

func staticCallResultTypeNode(call *CallExpr, context constructorContext, valueTypes map[string]string) string {
	if call == nil || call.Callee == nil {
		return ""
	}
	if receiver, name, typeArguments, ok := extensionASTCallParts(call); ok {
		actualType := extensionASTStaticType(receiver, context, valueTypes)
		candidates := applicableExtensionASTs(name, actualType, call.Arguments, typeArguments, valueTypes, context)
		if len(candidates) == 1 && !realMethodAppliesAST(actualType, name, call.Arguments, context) {
			result := strings.TrimSpace(methodResultSource(candidates[0].Method.Method))
			return substituteLambdaType(result, extensionTargetBindings(candidates[0].Method.Target, actualType))
		}
	}
	switch callee := call.Callee.(type) {
	case *NameExpr:
		if target, ok := context.Targets[callee.Name]; ok && target.Class != nil {
			return target.Class.Name
		}
		for _, signature := range context.FunctionSignatures[callee.Name] {
			if result := strings.TrimSpace(signature.resultText()); result != "" {
				return result
			}
		}
	case *SelectorExpr:
		if qualified, ok := directSelectorPath(callee); ok {
			if target, ok := context.Targets[qualified]; ok && target.Class != nil {
				return qualified
			}
		}
		if receiverKey := staticMethodReceiverKey(callee.Receiver, callee.Name, context); receiverKey != "" {
			for _, signature := range context.StaticMethodSignatures[receiverKey][callee.Name] {
				if result := strings.TrimSpace(signature.resultText()); result != "" {
					return result
				}
			}
		}
		if receiver, ok := callee.Receiver.(*NameExpr); ok {
			for _, signature := range context.ClassMethodSignatures[strings.TrimPrefix(valueTypes[receiver.Name], "*")][callee.Name] {
				if result := strings.TrimSpace(signature.resultText()); result != "" {
					return result
				}
			}
			if importPath := context.AvailableImports[receiver.Name]; importPath != "" {
				if pkg, err := importNativePackage(importPath); err == nil {
					if object, ok := pkg.Scope().Lookup(callee.Name).(*types.Func); ok {
						if signature, ok := object.Type().(*types.Signature); ok && signature.Results() != nil && signature.Results().Len() > 0 {
							return types.TypeString(signature.Results().At(0).Type(), nativeTypeQualifier(context))
						}
					}
				}
			}
		}
		typeName := staticExpressionTypeNode(callee.Receiver, context, valueTypes)
		if named, pkg, ok := nativeNamedType(typeName, context); ok {
			method := types.NewMethodSet(types.NewPointer(named)).Lookup(pkg, callee.Name)
			if method == nil {
				method = types.NewMethodSet(named).Lookup(pkg, callee.Name)
			}
			if method != nil {
				if signature, ok := method.Type().(*types.Signature); ok && signature.Results() != nil && signature.Results().Len() > 0 {
					return types.TypeString(signature.Results().At(0).Type(), nativeTypeQualifier(context))
				}
			}
		}
	}
	return ""
}

func nativeSelectorFieldTypeNode(selector *SelectorExpr, context constructorContext, valueTypes map[string]string) string {
	if selector == nil {
		return ""
	}
	baseType := staticExpressionTypeNode(selector.Receiver, context, valueTypes)
	named, _, ok := nativeNamedType(baseType, context)
	if !ok {
		return ""
	}
	structure, ok := named.Underlying().(*types.Struct)
	if !ok {
		return ""
	}
	for index := 0; index < structure.NumFields(); index++ {
		field := structure.Field(index)
		if field.Name() == selector.Name && field.Exported() {
			return types.TypeString(field.Type(), nativeTypeQualifier(context))
		}
	}
	return ""
}

func isUnknownStaticArgumentNode(expression ExprNode) bool {
	_, ok := expression.(*NameExpr)
	return ok
}

func copyIgnoredSource(src string, start int, out *strings.Builder) (int, bool, error) {
	if start >= len(src) {
		return start, false, nil
	}
	switch src[start] {
	case '"', '\'':
		end, err := skipQuoted(src, start, src[start])
		if err != nil {
			return 0, false, err
		}
		out.WriteString(src[start : end+1])
		return end + 1, true, nil
	case '`':
		end := strings.IndexByte(src[start+1:], '`')
		if end < 0 {
			return 0, false, fmt.Errorf("unterminated raw string")
		}
		end += start + 1
		out.WriteString(src[start : end+1])
		return end + 1, true, nil
	case '/':
		if start+1 < len(src) && src[start+1] == '/' {
			end := start + 2
			for end < len(src) && src[end] != '\n' {
				end++
			}
			out.WriteString(src[start:end])
			return end, true, nil
		}
		if start+1 < len(src) && src[start+1] == '*' {
			end := strings.Index(src[start+2:], "*/")
			if end < 0 {
				return 0, false, fmt.Errorf("unterminated comment")
			}
			end += start + 4
			out.WriteString(src[start:end])
			return end, true, nil
		}
	}
	return start, false, nil
}

func precededByFunc(src string, position int) bool {
	end := position
	for end > 0 && (src[end-1] == ' ' || src[end-1] == '\t' || src[end-1] == '\n' || src[end-1] == '\r') {
		end--
	}
	start := end
	for start > 0 && isIdentPart(src[start-1]) {
		start--
	}
	return src[start:end] == "func"
}
