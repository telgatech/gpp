package compiler

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

type parameterInfo struct {
	Name       string
	Type       string
	Default    string
	HasDefault bool
}

type callableSignature struct {
	Name       string
	GoName     string
	Parameters []parameterInfo
	Result     string
}

func parameterSignatureKey(parameters []parameterInfo) string {
	parts := make([]string, len(parameters))
	for index, parameter := range parameters {
		parts[index] = strings.Join(strings.Fields(parameter.Type), " ")
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
			result[parameter.Name] = strings.Join(strings.Fields(parameter.Type), " ")
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
		typeName, err := formatNode(field.Type)
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
		if typeName, err := formatNode(value.Type); err == nil {
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
	if function == nil || function.Params == nil {
		return ""
	}
	parameters := []string{}
	for _, field := range function.Params.List {
		typeName, err := formatNode(field.Type)
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
			typeName, err := formatNode(field.Type)
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
		parameters, err := parseParameterInfos(method.Parameters)
		if err != nil {
			continue
		}
		result[method.Name] = append(result[method.Name], callableSignature{
			Name:       method.Name,
			Parameters: parameters,
			Result:     strings.TrimSpace(method.Result),
		})
	}
	for _, parentName := range class.Parents {
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

func functionSignaturesForFile(file *File) (map[string][]callableSignature, error) {
	result := map[string][]callableSignature{}
	for _, decl := range file.Decls {
		raw, ok := decl.(*RawDecl)
		if !ok {
			continue
		}
		signatures, err := collectFunctionSignatures(stripAnnotationSyntaxPreserve(raw.Code))
		if err != nil {
			return nil, err
		}
		for _, signature := range signatures {
			result[signature.Name] = append(result[signature.Name], signature)
		}
	}
	return result, nil
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

		parsed, err := parser.ParseFile(
			token.NewFileSet(),
			"parameters.go",
			"package main\nfunc __gpp_parameters("+left+") {}\n",
			0,
		)
		if err != nil {
			return nil, err
		}

		function, ok := parsed.Decls[0].(*ast.FuncDecl)
		if !ok || function.Type.Params == nil || len(function.Type.Params.List) != 1 {
			return nil, fmt.Errorf("invalid parameter %q", part)
		}
		field := function.Type.Params.List[0]
		if hasDefault && len(field.Names) != 1 {
			return nil, fmt.Errorf("defaults require exactly one parameter name in %q", part)
		}

		typeText, err := formatNode(field.Type)
		if err != nil {
			return nil, err
		}
		if len(field.Names) == 0 {
			result = append(result, parameterInfo{Type: typeText})
			continue
		}
		for nameIndex, name := range field.Names {
			info := parameterInfo{
				Name:       name.Name,
				Type:       typeText,
				Default:    defaultValue,
				HasDefault: hasDefault && nameIndex == 0,
			}
			result = append(result, info)
		}
	}

	return result, nil
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
			Result:     strings.TrimSpace(src[resultStart:resultEnd]),
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
					result = append(result, signature.Parameters[index].Default)
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
			values[index] = parameter.Default
		}
		if matched {
			return values, true, nil
		}
	}

	return args, false, fmt.Errorf("no matching %s signature for named arguments", name)
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
	return transformCallableCallsInRange(src, context, 0)
}

func transformCallableCallsInRange(src string, context constructorContext, _ int) (string, error) {
	var out strings.Builder
	for i := 0; i < len(src); {
		if end, ok, err := copyIgnoredSource(src, i, &out); err != nil {
			return "", err
		} else if ok {
			i = end
			continue
		}

		name, n := readIdent(src[i:])
		if n == 0 {
			out.WriteByte(src[i])
			i++
			continue
		}
		if name == "this" {
			dot := skipSpace(src, i+n)
			if dot < len(src) && src[dot] == '.' {
				methodName, methodLength := readIdent(src[dot+1:])
				open := skipSpace(src, dot+1+methodLength)
				signatures := context.MethodSignatures[methodName]
				if methodLength > 0 && open < len(src) && src[open] == '(' && len(signatures) > 0 {
					close, err := findMatchingParen(src, open)
					if err != nil {
						return "", err
					}
					args, err := splitTopLevel(src[open+1:close], ',')
					if err != nil {
						return "", err
					}
					for len(args) > 0 && strings.TrimSpace(args[len(args)-1]) == "" {
						args = args[:len(args)-1]
					}
					resolved, changed, err := resolveCallableCall(methodName, args, signatures)
					if err != nil {
						return "", err
					}
					if changed {
						for index := range resolved {
							resolved[index], err = transformCallableCallsInRange(strings.TrimSpace(resolved[index]), context, 0)
							if err != nil {
								return "", err
							}
						}
						out.WriteString(src[i : open+1])
						out.WriteString(strings.Join(resolved, ", "))
						out.WriteByte(')')
						i = close + 1
						continue
					}
				}
			}
		}
		if i > 0 && (src[i-1] == '.' || isIdentPart(src[i-1])) {
			out.WriteString(src[i : i+n])
			i += n
			continue
		}
		open := skipSpace(src, i+n)
		if open >= len(src) || src[open] != '(' || precededByFunc(src, i) {
			out.WriteString(src[i : i+n])
			i += n
			continue
		}
		signatures := context.FunctionSignatures[name]
		if len(signatures) == 0 {
			out.WriteString(src[i : i+n])
			i += n
			continue
		}
		close, err := findMatchingParen(src, open)
		if err != nil {
			return "", err
		}
		args, err := splitTopLevel(src[open+1:close], ',')
		if err != nil {
			return "", err
		}
		for len(args) > 0 && strings.TrimSpace(args[len(args)-1]) == "" {
			args = args[:len(args)-1]
		}
		resolved, changed, err := resolveCallableCall(name, args, signatures)
		if err != nil {
			return "", err
		}
		if !changed {
			out.WriteString(src[i : close+1])
			i = close + 1
			continue
		}
		for index := range resolved {
			resolved[index], err = transformCallableCallsInRange(strings.TrimSpace(resolved[index]), context, 0)
			if err != nil {
				return "", err
			}
		}
		out.WriteString(src[i : open+1])
		out.WriteString(strings.Join(resolved, ", "))
		out.WriteByte(')')
		i = close + 1
	}
	return out.String(), nil
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
