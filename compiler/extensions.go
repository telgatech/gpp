package compiler

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"sort"
	"strconv"
	"strings"
)

type extensionMethod struct {
	Target            string
	TargetConstraints map[string]string
	ReceiverType      string
	Qualifier         string
	GoName            string
	Method            Method
	Prelude           bool
}

func extensionMethodsForDeclarations(declarations []*ExtendDecl, qualifier string) []extensionMethod {
	methods := []extensionMethod{}
	for _, declaration := range declarations {
		for _, rawTarget := range declaration.Targets {
			target := normalizeExtensionTarget(rawTarget)
			for _, method := range declaration.Methods {
				methods = append(methods, extensionMethod{
					Target:            target,
					TargetConstraints: cloneStringMap(declaration.TargetConstraints),
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
	hash := sha256.Sum256([]byte(strings.TrimSpace(target) + "\x00" + method.Name + "\x00" + method.TypeParams + "\x00" + method.Parameters))
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
	if len(context.Extensions) == 0 {
		return src, nil
	}
	var err error
	src, err = normalizeExtensionArguments(src, context)
	if err != nil {
		return "", err
	}
	parsed, fileSet, prefixLength, err := parseExtensionSource(src)
	if err != nil {
		return src, nil
	}
	valueTypes := polymorphicValueTypes(parsed, context)
	if context.CurrentClass != "" {
		valueTypes["this"] = "*" + context.CurrentClass
	} else if context.CurrentExtensionReceiver != "" {
		valueTypes["this"] = context.CurrentExtensionReceiver
	}

	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	var resolutionErr error
	addEdit := func(node ast.Node, text string) {
		start := fileSet.Position(node.Pos()).Offset - prefixLength
		end := fileSet.Position(node.End()).Offset - prefixLength
		if start >= 0 && end <= len(src) && start <= end {
			edits = append(edits, edit{start: start, end: end, text: text})
		}
	}

	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		receiver, methodName, typeArguments, ok := extensionCallParts(call)
		if !ok {
			return true
		}
		actualType := expressionStaticType(receiver, context, valueTypes)
		candidates := applicableExtensions(methodName, actualType, call.Args, typeArguments, valueTypes, context)
		if len(candidates) == 0 {
			return true
		}
		if realMethodApplies(actualType, methodName, call.Args, context) {
			return true
		}
		if len(candidates) > 1 {
			resolutionErr = fmt.Errorf("ambiguous extension method %s for %s", methodName, actualType)
			return false
		}

		candidate := candidates[0].Method
		receiverText, err := formatNode(receiver)
		if err != nil {
			return true
		}
		if extensionNeedsAddress(candidate.ReceiverType, actualType) {
			receiverText = "&" + receiverText
		}
		arguments := []string{receiverText}
		arguments = append(arguments, candidates[0].Arguments...)
		functionName := candidate.GoName
		if candidate.Qualifier != "" {
			functionName = candidate.Qualifier + "." + functionName
		}
		if len(typeArguments) > 0 {
			formatted := make([]string, len(typeArguments))
			for index, argument := range typeArguments {
				formatted[index], err = formatNode(argument)
				if err != nil {
					return true
				}
			}
			functionName += "[" + strings.Join(formatted, ", ") + "]"
		}
		addEdit(call, functionName+"("+strings.Join(arguments, ", ")+")")
		return false
	})

	if resolutionErr != nil {
		return "", resolutionErr
	}
	if len(edits) == 0 {
		return src, nil
	}
	for index := len(edits) - 1; index >= 0; index-- {
		for other := index - 1; other >= 0; other-- {
			if edits[other].start == edits[index].start && edits[other].end == edits[index].end {
				edits = append(edits[:other], edits[other+1:]...)
				index--
				break
			}
		}
	}
	// Rebuild from right to left so nested calls are not invalidated by edits
	// to earlier source positions.
	for i := 0; i < len(edits); i++ {
		for j := i + 1; j < len(edits); j++ {
			if edits[j].start > edits[i].start {
				edits[i], edits[j] = edits[j], edits[i]
			}
		}
	}
	for _, change := range edits {
		src = src[:change.start] + change.text + src[change.end:]
	}
	return src, nil
}

func normalizeExtensionArguments(src string, context constructorContext) (string, error) {
	knownNames := map[string]bool{}
	signatures := map[string][]callableSignature{}
	for _, extension := range context.Extensions {
		name := extension.Method.Name
		knownNames[name] = true
		parameters, err := parseParameterInfos(extension.Method.Parameters)
		if err != nil {
			continue
		}
		signatures[name] = append(signatures[name], callableSignature{Name: name, Parameters: parameters})
	}
	for index := 0; index < len(src); {
		if end, ok, err := copyIgnoredSource(src, index, &strings.Builder{}); err != nil {
			return "", err
		} else if ok {
			index = end
			continue
		}
		name, length := readIdent(src[index:])
		if length == 0 || !knownNames[name] || (index > 0 && isIdentPart(src[index-1])) {
			index++
			continue
		}
		previous := index - 1
		for previous >= 0 && (src[previous] == ' ' || src[previous] == '\t' || src[previous] == '\n' || src[previous] == '\r') {
			previous--
		}
		if previous < 0 || src[previous] != '.' {
			index += length
			continue
		}
		open := skipSpace(src, index+length)
		if open < len(src) && src[open] == '[' {
			end, err := findMatchingBracket(src, open)
			if err != nil {
				return "", err
			}
			open = skipSpace(src, end+1)
		}
		if open >= len(src) || src[open] != '(' {
			index += length
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
		hasNamed := false
		for _, argument := range args {
			if topLevelColon(argument) >= 0 {
				hasNamed = true
				break
			}
		}
		if !hasNamed {
			index = close + 1
			continue
		}
		resolved, _, err := resolveCallableCall(name, args, signatures[name])
		if err != nil {
			return "", err
		}
		src = src[:open+1] + strings.Join(resolved, ", ") + src[close:]
		index = open + 1 + len(strings.Join(resolved, ", ")) + 1
	}
	return src, nil
}

func parseExtensionSource(src string) (ast.Node, *token.FileSet, int, error) {
	const prefix = "package main\n\n"
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "", prefix+src, 0)
	if err == nil {
		return parsed, fileSet, len(prefix), nil
	}
	functionPrefix := prefix + "func __gpp_scope() {\n"
	functionSet := token.NewFileSet()
	parsed, err = parser.ParseFile(functionSet, "", functionPrefix+src+"\n}\n", 0)
	if err != nil {
		return nil, nil, 0, err
	}
	return parsed, functionSet, len(functionPrefix), nil
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

func applicableExtensions(name, actualType string, args []ast.Expr, typeArguments []ast.Expr, valueTypes map[string]string, context constructorContext) []applicableExtension {
	result := []applicableExtension{}
	for _, extension := range context.Extensions {
		if extension.Method.Name != name || !extensionTargetMatches(extension.Target, extension.ReceiverType, actualType) {
			continue
		}
		if len(typeArguments) > 0 && extensionTypeParameterCount(extension.Method.TypeParams) != len(typeArguments) {
			continue
		}
		resolved, ok := resolveExtensionArguments(extension, actualType, args, valueTypes, context)
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

func resolveExtensionArguments(extension extensionMethod, actualType string, args []ast.Expr, valueTypes map[string]string, context constructorContext) ([]string, bool) {
	parameters, err := parseParameterInfos(extension.Method.Parameters)
	if err != nil {
		return nil, false
	}
	bindings := extensionTargetBindings(extension.Target, actualType)
	for index := range parameters {
		parameters[index].Type = substituteLambdaType(parameters[index].Type, bindings)
	}
	argumentText := make([]string, len(args))
	for index, argument := range args {
		argumentText[index], err = formatNode(argument)
		if err != nil {
			return nil, false
		}
	}
	signature := callableSignature{Name: extension.Method.Name, Parameters: parameters}
	variadic := len(parameters) > 0 && strings.HasPrefix(strings.TrimSpace(parameters[len(parameters)-1].Type), "...")
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
		parsed, err := parser.ParseExpr(argument)
		if err != nil {
			return nil, false
		}
		actual := expressionStaticType(parsed, context, valueTypes)
		generic := extensionTypeParameterNames(extension.Method.TypeParams)
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
		expected := parameters[parameterIndex].Type
		if strings.HasPrefix(strings.TrimSpace(expected), "...") {
			expected = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expected), "..."))
		}
		if actual != "" && !generic[expected] && !extensionArgumentMatches(actual, expected, argument, context) {
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

func extensionTargetMatches(target, receiverType, actual string) bool {
	target = normalizeExtensionTarget(target)
	actual = normalizeExtensionTarget(actual)
	if actual == "" {
		return false
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
		pkg, err := importer.Default().Import(importPath)
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
		pkg, err := importer.Default().Import(importPath)
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

func extensionTargetTypeParameters(target string, constraints map[string]string) string {
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
		constraint := strings.TrimSpace(constraints[name])
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
	methodParameters := extensionTypeParameters(extension.Method.TypeParams)
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

func emitExtension(out *strings.Builder, declaration *ExtendDecl, context constructorContext, interpolationName string) error {
	for _, target := range declaration.Targets {
		target = normalizeExtensionTarget(target)
		for _, method := range declaration.Methods {
			extension := extensionMethod{
				Target:            target,
				TargetConstraints: cloneStringMap(declaration.TargetConstraints),
				ReceiverType:      extensionReceiverType(target, context.Introspection.Classes),
				GoName:            extensionGoName(target, method),
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
	parameters, err := stripParameterDefaults(extension.Method.Parameters)
	if err != nil {
		return err
	}
	parameters, err = transformExtensionParameterList(parameters, context)
	if err != nil {
		return err
	}
	name := extension.GoName
	fmt.Fprintf(out, "func %s%s(this %s", name, extensionFunctionTypeParameters(extension), extension.ReceiverType)
	if parameters != "" {
		fmt.Fprintf(out, ", %s", parameters)
	}
	out.WriteByte(')')
	result := transformPolymorphicResultType(strings.TrimSpace(extension.Method.Result), context)
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
				methodContext.CurrentParameterTypes[parameter.Name] = parameter.Type
			}
		}
	}
	body := transformInterpolationWithName(extension.Method.Body, interpolationName)
	body, err = transformLambdas(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformSafeAccess(body, methodContext)
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
	body, err = transformOverloads(body, context.Overloads)
	if err != nil {
		return err
	}
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
		typeName := transformPolymorphicType(parameter.Type, context)
		if typeName == parameter.Type {
			name := strings.TrimSpace(parameter.Type)
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
