package compiler

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

type recordFieldType struct {
	Name string
	Type string
}

type recordShape struct {
	Key    string
	GoName string
	Fields []recordFieldType
}

type recordContext struct {
	Shapes          map[string]*recordShape
	FunctionResults map[string]string
	MethodResults   map[string]map[string]string
	Emitted         map[string]bool
}

func newRecordContext() *recordContext {
	return &recordContext{
		Shapes:          map[string]*recordShape{},
		FunctionResults: map[string]string{},
		MethodResults:   map[string]map[string]string{},
		Emitted:         map[string]bool{},
	}
}

func cloneRecordContext(source *recordContext) *recordContext {
	result := newRecordContext()
	if source == nil {
		return result
	}
	for key, shape := range source.Shapes {
		fields := append([]recordFieldType(nil), shape.Fields...)
		result.Shapes[key] = &recordShape{Key: shape.Key, GoName: shape.GoName, Fields: fields}
	}
	for name, typeName := range source.FunctionResults {
		result.FunctionResults[name] = typeName
	}
	for className, methods := range source.MethodResults {
		result.MethodResults[className] = map[string]string{}
		for name, typeName := range methods {
			result.MethodResults[className][name] = typeName
		}
	}
	for name, emitted := range source.Emitted {
		result.Emitted[name] = emitted
	}
	return result
}

func (context *recordContext) register(fields []recordFieldType) *recordShape {
	sorted := append([]recordFieldType(nil), fields...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	parts := make([]string, len(sorted))
	for index, field := range sorted {
		parts[index] = field.Name + ":" + field.Type
	}
	key := strings.Join(parts, ";")
	if shape, ok := context.Shapes[key]; ok {
		return shape
	}
	hash := sha256.Sum256([]byte(key))
	shape := &recordShape{
		Key:    key,
		GoName: fmt.Sprintf("__gopp_record_%x", hash[:4]),
		Fields: sorted,
	}
	context.Shapes[key] = shape
	return shape
}

func (context *recordContext) definitions() string {
	if context == nil {
		return ""
	}
	shapes := []*recordShape{}
	for _, shape := range context.Shapes {
		if context.Emitted[shape.GoName] {
			continue
		}
		shapes = append(shapes, shape)
	}
	sort.Slice(shapes, func(i, j int) bool { return shapes[i].GoName < shapes[j].GoName })
	var output strings.Builder
	for _, shape := range shapes {
		fmt.Fprintf(&output, "type %s struct {\n", shape.GoName)
		for _, field := range shape.Fields {
			fmt.Fprintf(&output, "\t%s %s\n", field.Name, field.Type)
		}
		output.WriteString("}\n\n")
		context.Emitted[shape.GoName] = true
	}
	return output.String()
}

func transformRecords(src string, context constructorContext) (string, error) {
	if context.Records == nil || !strings.Contains(src, "record") && !strings.Contains(src, "let") {
		return src, nil
	}

	src = rewriteLetSyntax(src)
	trial := cloneRecordContext(context.Records)
	trialContext := context
	trialContext.Records = trial

	env := collectRecordValueTypes(src, trialContext)
	speculative, err := transformRecordLiterals(src, env, trialContext, false)
	if err != nil {
		return "", err
	}
	speculative, err = rewriteRecordFunctionResults(speculative, trialContext)
	if err != nil {
		return "", err
	}
	speculative, err = rewriteRecordCollectionLiterals(speculative, trialContext)
	if err != nil {
		return "", err
	}
	speculative, err = rewriteRecordFunctionParameters(speculative, trialContext)
	if err != nil {
		return "", err
	}

	finalRecords := cloneRecordContext(context.Records)
	for name, typeName := range trial.FunctionResults {
		finalRecords.FunctionResults[name] = typeName
	}
	for className, methods := range trial.MethodResults {
		if finalRecords.MethodResults[className] == nil {
			finalRecords.MethodResults[className] = map[string]string{}
		}
		for name, typeName := range methods {
			finalRecords.MethodResults[className][name] = typeName
		}
	}
	finalContext := context
	finalContext.Records = finalRecords
	finalEnv := collectRecordValueTypes(speculative, finalContext)
	transformed, err := transformRecordLiterals(src, finalEnv, finalContext, true)
	if err != nil {
		return "", err
	}
	transformed, err = rewriteRecordFunctionParameters(transformed, finalContext)
	if err != nil {
		return "", err
	}
	transformed, err = rewriteRecordFunctionResults(transformed, finalContext)
	if err != nil {
		return "", err
	}
	transformed, err = rewriteRecordCollectionLiterals(transformed, finalContext)
	if err != nil {
		return "", err
	}

	*context.Records = *finalRecords
	return transformed, nil
}

func rewriteLetSyntax(src string) string {
	var output strings.Builder
	for index := 0; index < len(src); {
		if end, ok, _ := copyIgnoredSource(src, index, &output); ok {
			index = end
			continue
		}
		if !keywordAt(src, index, "let") {
			output.WriteByte(src[index])
			index++
			continue
		}
		nameStart := skipSpace(src, index+len("let"))
		name, nameLength := readIdent(src[nameStart:])
		if nameLength == 0 {
			output.WriteString(src[index : index+len("let")])
			index += len("let")
			continue
		}
		equals := skipSpace(src, nameStart+nameLength)
		if equals >= len(src) || src[equals] != '=' {
			output.WriteString(src[index : index+len("let")])
			index += len("let")
			continue
		}
		output.WriteString(name)
		output.WriteString(" :=")
		index = equals + 1
	}
	return output.String()
}

func transformRecordLiterals(src string, valueTypes map[string]string, context constructorContext, strict bool) (string, error) {
	var output strings.Builder
	for index := 0; index < len(src); {
		if end, ok, err := copyIgnoredSource(src, index, &output); err != nil {
			return "", err
		} else if ok {
			index = end
			continue
		}

		name, length := readIdent(src[index:])
		if length == 0 {
			output.WriteByte(src[index])
			index++
			continue
		}
		if name != "record" || (index > 0 && (isIdentPart(src[index-1]) || src[index-1] == '.')) {
			output.WriteString(src[index : index+length])
			index += length
			continue
		}

		open := skipSpace(src, index+length)
		if open >= len(src) || src[open] != '(' {
			output.WriteString(src[index : index+length])
			index += length
			continue
		}
		close, err := findMatchingParen(src, open)
		if err != nil {
			return "", fmt.Errorf("record literal: %w", err)
		}
		literal, err := transformRecordLiteral(src[open+1:close], valueTypes, context, strict)
		if err != nil {
			return "", err
		}
		output.WriteString(literal)
		index = close + 1
	}
	return output.String(), nil
}

func transformRecordLiteral(argsSource string, valueTypes map[string]string, context constructorContext, strict bool) (string, error) {
	args, err := splitTopLevel(argsSource, ',')
	if err != nil {
		return "", fmt.Errorf("record literal: %w", err)
	}
	fields := []recordFieldType{}
	values := map[string]string{}
	order := []string{}
	for index, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		colon := topLevelColon(arg)
		if colon < 0 {
			return "", fmt.Errorf("record literal argument %d must be named", index+1)
		}
		name := strings.TrimSpace(arg[:colon])
		if !isIdentifier(name) {
			return "", fmt.Errorf("record literal field %q is not a valid identifier", name)
		}
		if _, exists := values[name]; exists {
			return "", fmt.Errorf("record literal repeats field %s", name)
		}
		valueSource := strings.TrimSpace(arg[colon+1:])
		if valueSource == "" {
			return "", fmt.Errorf("record literal field %s is missing a value", name)
		}
		value, err := transformRecordLiterals(valueSource, valueTypes, context, strict)
		if err != nil {
			return "", err
		}
		fieldType := inferRecordExpressionType(value, valueTypes, context)
		if fieldType == "" {
			if strict {
				return "", fmt.Errorf("cannot infer type of record field %s from %s", name, valueSource)
			}
			fieldType = "any"
		}
		values[name] = value
		order = append(order, name)
		fields = append(fields, recordFieldType{Name: name, Type: fieldType})
	}

	shape := context.Records.register(fields)
	var output strings.Builder
	fmt.Fprintf(&output, "%s{", shape.GoName)
	for index, name := range order {
		if index > 0 {
			output.WriteString(", ")
		}
		fmt.Fprintf(&output, "%s: %s", name, values[name])
	}
	output.WriteByte('}')
	return output.String(), nil
}

func rewriteRecordCollectionLiterals(src string, context constructorContext) (string, error) {
	var output strings.Builder
	for index := 0; index < len(src); {
		if end, ok, err := copyIgnoredSource(src, index, &output); err != nil {
			return "", err
		} else if ok {
			index = end
			continue
		}
		if !keywordAt(src, index, "record") || (index > 0 && isIdentPart(src[index-1])) {
			output.WriteByte(src[index])
			index++
			continue
		}
		previous := index - 1
		for previous >= 0 && (src[previous] == ' ' || src[previous] == '\t' || src[previous] == '\n' || src[previous] == '\r') {
			previous--
		}
		if previous < 0 || src[previous] != ']' {
			output.WriteByte(src[index])
			index++
			continue
		}
		open := skipSpace(src, index+len("record"))
		if open >= len(src) || src[open] != '{' {
			output.WriteString(src[index : index+len("record")])
			index += len("record")
			continue
		}
		close, err := findMatchingBrace(src, open)
		if err != nil {
			return "", fmt.Errorf("record collection literal: %w", err)
		}
		shapeName := firstRecordTypeName(src[open+1 : close])
		if shapeName == "" {
			return "", fmt.Errorf("cannot infer record collection element type")
		}
		output.WriteString(shapeName)
		index = open
	}
	return output.String(), nil
}

func firstRecordTypeName(src string) string {
	for index := 0; index < len(src); index++ {
		if strings.HasPrefix(src[index:], "__gopp_record_") &&
			(index == 0 || !isIdentPart(src[index-1])) {
			end := index + len("__gopp_record_")
			for end < len(src) && isIdentPart(src[end]) {
				end++
			}
			return src[index:end]
		}
	}
	return ""
}

func inferRecordExpressionType(source string, valueTypes map[string]string, context constructorContext) string {
	parsed, err := parser.ParseExpr(strings.TrimSpace(source))
	if err != nil {
		return ""
	}
	return inferRecordASTType(parsed, valueTypes, context)
}

func inferRecordASTType(expr ast.Expr, valueTypes map[string]string, context constructorContext) string {
	switch value := expr.(type) {
	case *ast.Ident:
		if valueTypes[value.Name] != "" {
			return valueTypes[value.Name]
		}
		switch value.Name {
		case "true", "false":
			return "bool"
		}
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
		if recordType := formatRecordTypeExpr(value.Type); recordType != "" {
			return recordType
		}
		typeName, err := formatNode(value.Type)
		if err == nil {
			return strings.Join(strings.Fields(typeName), " ")
		}
	case *ast.ParenExpr:
		return inferRecordASTType(value.X, valueTypes, context)
	case *ast.UnaryExpr:
		inner := inferRecordASTType(value.X, valueTypes, context)
		if inner != "" && value.Op.String() == "&" {
			return "*" + inner
		}
		return inner
	case *ast.BinaryExpr:
		left := inferRecordASTType(value.X, valueTypes, context)
		right := inferRecordASTType(value.Y, valueTypes, context)
		switch value.Op.String() {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			return "bool"
		case "+":
			if left == "string" || right == "string" {
				return "string"
			}
			if left == "int" && right == "int" {
				return "int"
			}
		}
	case *ast.CallExpr:
		return inferRecordCallResult(value, valueTypes, context)
	case *ast.SelectorExpr:
		base := inferRecordASTType(value.X, valueTypes, context)
		if shape := context.Records.recordByGoName(strings.TrimPrefix(base, "*")); shape != nil {
			for _, field := range shape.Fields {
				if field.Name == value.Sel.Name {
					return field.Type
				}
			}
		}
		if classTarget, ok := context.Targets[strings.TrimPrefix(base, "*")]; ok {
			for _, field := range classTarget.Class.Fields {
				if field.Name == value.Sel.Name {
					return field.Type
				}
			}
		}
	}
	return ""
}

func (context *recordContext) recordByGoName(name string) *recordShape {
	if context == nil {
		return nil
	}
	for _, shape := range context.Shapes {
		if shape.GoName == name {
			return shape
		}
	}
	return nil
}

func formatRecordTypeExpr(expr ast.Expr) string {
	if ident, ok := expr.(*ast.Ident); ok && strings.HasPrefix(ident.Name, "__gopp_record_") {
		return ident.Name
	}
	return ""
}

func inferRecordCallResult(call *ast.CallExpr, valueTypes map[string]string, context constructorContext) string {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		if result := context.Records.FunctionResults[function.Name]; result != "" {
			return result
		}
		if result := commonGoFunctionResult(function.Name); result != "" {
			return result
		}
		for _, signature := range context.FunctionSignatures[function.Name] {
			if signature.Result != "" {
				return strings.TrimSpace(signature.Result)
			}
		}
	case *ast.SelectorExpr:
		if receiver, ok := function.X.(*ast.Ident); ok {
			className := strings.TrimPrefix(valueTypes[receiver.Name], "*")
			if className == "" {
				className = context.CurrentClass
			}
			if result := context.Records.MethodResults[className][function.Sel.Name]; result != "" {
				return result
			}
			for _, signature := range context.ClassMethodSignatures[className][function.Sel.Name] {
				if signature.Result != "" {
					return strings.TrimSpace(signature.Result)
				}
			}
			if function.Sel.Name == "Sprintf" || function.Sel.Name == "Itoa" || function.Sel.Name == "ToUpper" || function.Sel.Name == "ToLower" || function.Sel.Name == "TrimSpace" || function.Sel.Name == "Join" {
				return "string"
			}
		}
	}
	return ""
}

func commonGoFunctionResult(name string) string {
	switch name {
	case "len", "cap":
		return "int"
	case "string":
		return "string"
	case "bool":
		return "bool"
	case "byte", "uint8":
		return "uint8"
	case "rune", "int32":
		return "rune"
	case "int", "int8", "int16", "int64":
		return name
	case "float32", "float64":
		return name
	}
	return ""
}

func collectRecordValueTypes(src string, context constructorContext) map[string]string {
	result := map[string]string{}
	parsed, err := parser.ParseFile(token.NewFileSet(), "records.go", "package main\n\n"+src, 0)
	if err != nil {
		parsed, err = parser.ParseFile(token.NewFileSet(), "records.go", "package main\n\nfunc __gopp_scope() {\n"+src+"\n}\n", 0)
	}
	if err != nil {
		return result
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.FuncDecl:
			if declaration.Type.Params != nil {
				for _, field := range declaration.Type.Params.List {
					typeName, err := formatNode(field.Type)
					if err != nil {
						continue
					}
					for _, name := range field.Names {
						result[name.Name] = strings.Join(strings.Fields(typeName), " ")
					}
				}
			}
		case *ast.ValueSpec:
			declared := ""
			if declaration.Type != nil {
				declared, _ = formatNode(declaration.Type)
			}
			for index, name := range declaration.Names {
				typeName := declared
				if typeName == "" && index < len(declaration.Values) {
					typeName = inferRecordASTType(declaration.Values[index], result, context)
				}
				if typeName != "" {
					result[name.Name] = strings.Join(strings.Fields(typeName), " ")
				}
			}
		case *ast.AssignStmt:
			for index, left := range declaration.Lhs {
				name, ok := left.(*ast.Ident)
				if !ok || index >= len(declaration.Rhs) {
					continue
				}
				if typeName := inferRecordASTType(declaration.Rhs[index], result, context); typeName != "" {
					result[name.Name] = strings.Join(strings.Fields(typeName), " ")
				}
			}
		}
		return true
	})
	return result
}

func rewriteRecordFunctionResults(src string, context constructorContext) (string, error) {
	parsed, fileSet, prefixLength, err := parseRecordSource(src)
	if err != nil {
		return src, nil
	}
	valueTypes := collectRecordValueTypes(src, context)
	type replacements struct {
		start int
		end   int
		text  string
	}
	edits := []replacements{}
	type functionResult struct {
		name        string
		kind        string
		goType      string
		declaration *ast.FuncDecl
	}
	functions := []functionResult{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		declaration, ok := node.(*ast.FuncDecl)
		if !ok || declaration.Type.Results == nil || len(declaration.Type.Results.List) != 1 || declaration.Body == nil {
			return true
		}
		kind, ok := recordResultKind(declaration.Type.Results.List[0].Type)
		if !ok {
			return true
		}
		functions = append(functions, functionResult{name: declaration.Name.Name, kind: kind, declaration: declaration})
		return true
	})

	for pass := 0; pass <= len(functions); pass++ {
		changed := false
		for index := range functions {
			declaration := functions[index].declaration
			goType, err := inferRecordFunctionResult(declaration, functions[index].kind, valueTypes, context)
			if err != nil {
				return "", err
			}
			if goType == "" {
				continue
			}
			if functions[index].goType != goType {
				functions[index].goType = goType
				changed = true
			}
			if context.CurrentClass != "" && declaration.Name.Name == "__gopp_scope" {
				if context.Records.MethodResults[context.CurrentClass] == nil {
					context.Records.MethodResults[context.CurrentClass] = map[string]string{}
				}
				context.Records.MethodResults[context.CurrentClass][context.CurrentMethod] = goType
			} else {
				context.Records.FunctionResults[declaration.Name.Name] = goType
			}
		}
		if !changed {
			break
		}
		valueTypes = collectRecordValueTypes(src, context)
	}
	for _, function := range functions {
		if function.goType == "" {
			return "", fmt.Errorf("cannot infer record return type for %s", function.name)
		}
	}

	seenFunctions := 0
	ast.Inspect(parsed, func(node ast.Node) bool {
		declaration, ok := node.(*ast.FuncDecl)
		if !ok || declaration.Type.Results == nil || len(declaration.Type.Results.List) != 1 {
			return true
		}
		kind, ok := recordResultKind(declaration.Type.Results.List[0].Type)
		if !ok {
			return true
		}
		if seenFunctions >= len(functions) || functions[seenFunctions].name != declaration.Name.Name {
			return true
		}
		goType := functions[seenFunctions].goType
		seenFunctions++
		if goType == "" {
			return true
		}
		resultType := declaration.Type.Results.List[0].Type
		start := fileSet.Position(resultType.Pos()).Offset - prefixLength
		end := fileSet.Position(resultType.End()).Offset - prefixLength
		switch kind {
		case "slice":
			goType = "[]" + goType
		case "map":
			if mapping, ok := resultType.(*ast.MapType); ok {
				keyType, _ := formatNode(mapping.Key)
				goType = "map[" + strings.TrimSpace(keyType) + "]" + goType
			}
		}
		edits = append(edits, replacements{start: start, end: end, text: goType})
		return true
	})

	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		if edit.start >= 0 && edit.end <= len(src) && edit.start <= edit.end {
			src = src[:edit.start] + edit.text + src[edit.end:]
		}
	}
	return src, nil
}

func parseRecordSource(src string) (*ast.File, *token.FileSet, int, error) {
	const filePrefix = "package main\n\n"
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "records.go", filePrefix+src, 0)
	if err == nil {
		return parsed, fileSet, len(filePrefix), nil
	}
	functionPrefix := "package main\n\nfunc __gopp_scope() {\n"
	fileSet = token.NewFileSet()
	parsed, err = parser.ParseFile(fileSet, "records.go", functionPrefix+src+"\n}\n", 0)
	if err != nil {
		return nil, nil, 0, err
	}
	return parsed, fileSet, len(functionPrefix), nil
}

func rewriteRecordFunctionParameters(src string, context constructorContext) (string, error) {
	parsed, fileSet, prefixLength, err := parseRecordSource(src)
	if err != nil {
		return src, nil
	}
	valueTypes := collectRecordValueTypes(src, context)
	calls := []*ast.CallExpr{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			calls = append(calls, call)
		}
		return true
	})
	type replacement struct {
		start int
		end   int
		text  string
	}
	edits := []replacement{}
	var resolutionErr error
	ast.Inspect(parsed, func(node ast.Node) bool {
		if resolutionErr != nil {
			return false
		}
		declaration, ok := node.(*ast.FuncDecl)
		if !ok || declaration.Type.Params == nil {
			return true
		}
		parameterIndex := 0
		for _, field := range declaration.Type.Params.List {
			kind, ok := recordResultKind(field.Type)
			if !ok {
				parameterIndex += maxInt(1, len(field.Names))
				continue
			}
			provided := ""
			for _, call := range calls {
				function, ok := call.Fun.(*ast.Ident)
				if !ok || function.Name != declaration.Name.Name || parameterIndex >= len(call.Args) {
					continue
				}
				actual := inferRecordASTType(call.Args[parameterIndex], valueTypes, context)
				if !recordTypeMatchesKind(actual, kind) {
					continue
				}
				if provided != "" && provided != actual {
					resolutionErr = fmt.Errorf("inconsistent record argument types for %s parameter %d", declaration.Name.Name, parameterIndex+1)
					return false
				}
				provided = actual
			}
			if provided == "" {
				resolutionErr = fmt.Errorf("cannot infer record parameter %d of %s from call sites", parameterIndex+1, declaration.Name.Name)
				return false
			}
			start := fileSet.Position(field.Type.Pos()).Offset - prefixLength
			end := fileSet.Position(field.Type.End()).Offset - prefixLength
			edits = append(edits, replacement{start: start, end: end, text: provided})
			parameterIndex += maxInt(1, len(field.Names))
		}
		return true
	})
	if resolutionErr != nil {
		return "", resolutionErr
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		if edit.start >= 0 && edit.end <= len(src) && edit.start <= edit.end {
			src = src[:edit.start] + edit.text + src[edit.end:]
		}
	}
	return src, nil
}

func recordTypeMatchesKind(typeName, kind string) bool {
	if kind == "record" {
		return strings.HasPrefix(typeName, "__gopp_record_")
	}
	if kind == "slice" {
		return strings.HasPrefix(typeName, "[]__gopp_record_")
	}
	if kind == "map" {
		return strings.Contains(typeName, "]__gopp_record_")
	}
	return false
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func recordResultKind(expr ast.Expr) (string, bool) {
	if ident, ok := expr.(*ast.Ident); ok && ident.Name == "record" {
		return "record", true
	}
	if array, ok := expr.(*ast.ArrayType); ok {
		if ident, ok := array.Elt.(*ast.Ident); ok && ident.Name == "record" {
			return "slice", true
		}
	}
	if mapping, ok := expr.(*ast.MapType); ok {
		if ident, ok := mapping.Value.(*ast.Ident); ok && ident.Name == "record" {
			return "map", true
		}
	}
	return "", false
}

func inferRecordFunctionResult(declaration *ast.FuncDecl, kind string, valueTypes map[string]string, context constructorContext) (string, error) {
	results := []string{}
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		returnStmt, ok := node.(*ast.ReturnStmt)
		if !ok || len(returnStmt.Results) != 1 {
			return true
		}
		if literal, ok := returnStmt.Results[0].(*ast.CompositeLit); ok && (kind == "slice" || kind == "map") {
			for _, element := range literal.Elts {
				value := ast.Expr(element)
				if kind == "map" {
					pair, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					value = pair.Value
				}
				typeName := inferRecordASTType(value, valueTypes, context)
				if strings.HasPrefix(typeName, "__gopp_record_") {
					results = append(results, typeName)
				}
			}
		} else {
			typeName := inferRecordASTType(returnStmt.Results[0], valueTypes, context)
			if strings.HasPrefix(typeName, "__gopp_record_") {
				results = append(results, typeName)
			}
		}
		return true
	})
	if len(results) == 0 {
		return "", nil
	}
	for _, result := range results[1:] {
		if result != results[0] {
			return "", fmt.Errorf(
				"inconsistent record return types in %s: expected %s, got %s",
				declaration.Name.Name,
				recordShapeDescription(results[0], context.Records),
				recordShapeDescription(result, context.Records),
			)
		}
	}
	return results[0], nil
}

func recordShapeDescription(goName string, context *recordContext) string {
	shape := context.recordByGoName(goName)
	if shape == nil {
		return goName
	}
	parts := make([]string, len(shape.Fields))
	for index, field := range shape.Fields {
		parts[index] = field.Name + " " + field.Type
	}
	return "record{" + strings.Join(parts, ", ") + "}"
}

func transformRecordMethodResult(result, body string, context constructorContext) (string, string, error) {
	if context.Records == nil || (!strings.Contains(result, "record") && !strings.Contains(body, "record") && !strings.Contains(body, "let")) {
		return result, body, nil
	}
	wrapped := "func __gopp_scope()"
	if strings.TrimSpace(result) != "" {
		wrapped += " " + strings.TrimSpace(result)
	}
	wrapped += " {\n" + body + "\n}\n"
	transformed, err := transformRecords(wrapped, context)
	if err != nil {
		return "", "", err
	}
	parsed, _, _, err := parseRecordSource(transformed)
	if err != nil || len(parsed.Decls) == 0 {
		return result, body, nil
	}
	declaration, ok := parsed.Decls[0].(*ast.FuncDecl)
	if !ok {
		return result, body, nil
	}
	newResult := result
	if declaration.Type.Results != nil && len(declaration.Type.Results.List) == 1 {
		newResult, _ = formatNode(declaration.Type.Results.List[0].Type)
	}
	open := strings.Index(transformed, "{")
	if open < 0 || len(transformed) == 0 || transformed[len(transformed)-1] != '\n' {
		return newResult, body, nil
	}
	close := strings.LastIndex(transformed, "}")
	if close <= open {
		return newResult, body, nil
	}
	return newResult, transformed[open+1 : close], nil
}
