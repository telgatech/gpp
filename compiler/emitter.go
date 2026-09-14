package compiler

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var interpolationRE = regexp.MustCompile(
	`\{\{\s*(.*?)\s*\}\}`,
)

func Emit(file *File) ([]byte, error) {
	model, err := ResolveProgram(&Program{Files: []*File{file}})
	if err != nil {
		return nil, err
	}

	context := localConstructorContext(model.Packages[file.Package].Classes)
	context.Records = newRecordContext()
	context.Introspection.Enabled = fileUsesIntrospection(file)
	context.Extensions = extensionMethodsForDeclarations(model.Packages[file.Package].Extensions, "")
	if err := addFunctionOverloads(&context, file); err != nil {
		return nil, err
	}
	functionSignatures, err := functionSignaturesForFile(file)
	if err != nil {
		return nil, err
	}
	context.FunctionSignatures = functionSignatures

	return emitFile(file, context)
}

type constructorTarget struct {
	Class         *ClassDecl
	Classes       map[string]*ClassDecl
	Qualifier     string
	InterfaceName string
}

type overloadContext struct {
	Functions        map[string]map[int]string
	FunctionTypes    map[string]map[string]string
	Methods          map[string]map[int]string
	MethodTypes      map[string]map[string]string
	ClassMethods     map[string]map[string]map[int]string
	ClassMethodTypes map[string]map[string]map[string]string
	LocalTypes       map[string]string
}

type constructorContext struct {
	Targets               map[string]constructorTarget
	Overloads             overloadContext
	FunctionSignatures    map[string][]callableSignature
	MethodSignatures      map[string][]callableSignature
	ClassMethodSignatures map[string]map[string][]callableSignature
	Records               *recordContext
	Introspection         *introspectionContext
	Extensions            []extensionMethod
	CurrentClass          string
	CurrentMethod         string
	CurrentResult         string
}

func localConstructorContext(classes map[string]*ClassDecl) constructorContext {
	targets := map[string]constructorTarget{}
	for name, class := range classes {
		targets[name] = constructorTarget{
			Class:         class,
			Classes:       classes,
			InterfaceName: "__gopp_" + name,
		}
	}

	context := constructorContext{
		Targets: targets,
		Overloads: overloadContext{
			Functions:        map[string]map[int]string{},
			FunctionTypes:    map[string]map[string]string{},
			Methods:          map[string]map[int]string{},
			MethodTypes:      map[string]map[string]string{},
			ClassMethods:     map[string]map[string]map[int]string{},
			ClassMethodTypes: map[string]map[string]map[string]string{},
		},
		FunctionSignatures:    map[string][]callableSignature{},
		MethodSignatures:      map[string][]callableSignature{},
		ClassMethodSignatures: map[string]map[string][]callableSignature{},
		Introspection:         newIntrospectionContext(classes),
	}

	for _, class := range classes {
		methodSet := methodOverloadsForClass(
			class,
			classes,
			map[string]bool{},
		)
		context.Overloads.ClassMethods[class.Name] = methodSet
		context.Overloads.ClassMethods["__gopp_"+class.Name] = methodSet
		typeSet := methodOverloadTypesForClass(class, classes, map[string]bool{})
		context.Overloads.ClassMethodTypes[class.Name] = typeSet
		context.Overloads.ClassMethodTypes["__gopp_"+class.Name] = typeSet
		context.ClassMethodSignatures[class.Name] = methodSignaturesForClass(
			class,
			classes,
			map[string]bool{},
		)
		for _, method := range class.Methods {
			if method.GoName == "" {
				continue
			}
			arity, err := parameterCount(method.Parameters)
			if err != nil {
				continue
			}
			if context.Overloads.Methods[method.Name] == nil {
				context.Overloads.Methods[method.Name] = map[int]string{}
			}
			context.Overloads.Methods[method.Name][arity] = method.GoName
		}
	}

	return context
}

func emitFile(file *File, context constructorContext) ([]byte, error) {
	var out strings.Builder

	fmt.Fprintf(&out, "package %s\n\n", goPackageName(file.Package))

	interpolationName, needsFmtImport, err := interpolationImport(file)
	if err != nil {
		return nil, err
	}

	body, err := emitDecls(file, context, interpolationName)
	if err != nil {
		return nil, err
	}

	declarationsPrefix := ""
	if needsFmtImport {
		declarationsPrefix += "import \"fmt\"\n\n"
	}
	if fileHasSafeAccess(file) {
		declarationsPrefix += "func __gopp_safe[R any](isNil bool, access func() R) R {\n"
		declarationsPrefix += "\tif isNil { var zero R; return zero }\n"
		declarationsPrefix += "\treturn access()\n"
		declarationsPrefix += "}\n\n"
	}
	if context.Records != nil {
		declarationsPrefix += context.Records.definitions()
	}
	if context.Introspection != nil && len(context.Introspection.Classes) > 0 &&
		!context.Introspection.RuntimeEmitted {
		declarationsPrefix += introspectionRuntimeDefinitions()
		context.Introspection.RuntimeEmitted = true
	}

	out.WriteString(insertAfterImports(body, declarationsPrefix))

	result, err := format.Source([]byte(out.String()))

	if err != nil {
		return []byte(out.String()), fmt.Errorf(
			"generated invalid Go: %w",
			err,
		)
	}

	return result, nil
}

func insertAfterImports(body, insertion string) string {
	if insertion == "" {
		return body
	}
	const prefix = "package main\n\n"
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "generated.go", prefix+body, 0)
	if err != nil || len(parsed.Imports) == 0 {
		return insertion + body
	}
	lastImport := parsed.Decls[0]
	for _, declaration := range parsed.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok.String() != "import" {
			break
		}
		lastImport = declaration
	}
	offset := fileSet.Position(lastImport.End()).Offset - len(prefix)
	if offset < 0 || offset > len(body) {
		return insertion + body
	}
	return body[:offset] + "\n\n" + insertion + body[offset:]
}

func emitDecls(file *File, context constructorContext, interpolationName string) (string, error) {
	var out strings.Builder

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *RawDecl:
			code := transformInterpolationWithName(d.Code, interpolationName)
			code, err := transformSafeAccess(code, context)
			if err != nil {
				return "", err
			}
			code, err = stripDefaultParameterValues(code)
			if err != nil {
				return "", err
			}
			code, err = transformCallableCalls(code, context)
			if err != nil {
				return "", err
			}
			code, err = transformConstructors(code, context)
			if err != nil {
				return "", err
			}
			code, err = transformRecords(code, context)
			if err != nil {
				return "", err
			}
			code, err = transformPolymorphicDeclarations(code, context)
			if err != nil {
				return "", err
			}
			code, err = transformIntrospection(code, context)
			if err != nil {
				return "", err
			}
			code, err = transformExtensions(code, context)
			if err != nil {
				return "", err
			}
			code, err = transformOverloads(code, context.Overloads)
			if err != nil {
				return "", err
			}

			out.WriteString(code)

			if !strings.HasSuffix(code, "\n") {
				out.WriteByte('\n')
			}

		case *ClassDecl:
			if err := emitClass(&out, d, context, interpolationName); err != nil {
				return "", err
			}
		}
	}

	return out.String(), nil
}

func emitClass(out *strings.Builder, class *ClassDecl, context constructorContext, interpolationName string) error {
	fmt.Fprintf(out, "type %s struct {\n", class.Name)

	for _, parent := range class.Parents {
		fmt.Fprintf(out, "\t%s\n", parent)
	}

	for _, field := range class.Fields {
		fieldType := transformPolymorphicType(field.Type, context)
		fmt.Fprintf(
			out,
			"\t%s %s\n",
			field.Name,
			fieldType,
		)
	}
	if context.Introspection != nil && context.Introspection.Enabled {
		out.WriteString("\tGoppDynamicClass *GoppClass\n")
	}

	out.WriteString("}\n\n")

	for methodIndex := range class.Methods {
		method := &class.Methods[methodIndex]
		methodName := methodOutputName(*method)
		body := transformInterpolationWithName(method.Body, interpolationName)
		methodResult, body, err := transformRecordMethodResult(method.Result, body, contextForMethod(context, class, method.Name))
		if err != nil {
			return err
		}
		method.Result = methodResult
		parameters, err := transformParameterList(method.Parameters, context)
		if err != nil {
			return err
		}
		fmt.Fprintf(
			out,
			"func (this *%s) %s(%s)",
			class.Name,
			methodName,
			parameters,
		)

		result := transformPolymorphicResultType(strings.TrimSpace(method.Result), context)
		if result != "" {
			fmt.Fprintf(out, " %s", result)
		}

		out.WriteString(" {\n")

		methodContext := context
		methodContext.CurrentClass = class.Name
		methodContext.CurrentMethod = method.Name
		methodContext.CurrentResult = strings.TrimSpace(method.Result)
		methodContext.MethodSignatures = context.ClassMethodSignatures[class.Name]
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
		methodContext = context
		methodContext.Overloads.Methods = methodOverloadsForClass(
			class,
			classesForClass(context, class),
			map[string]bool{},
		)
		methodContext.Overloads.MethodTypes = methodOverloadTypesForClass(
			class,
			classesForClass(context, class),
			map[string]bool{},
		)
		methodContext.Overloads.LocalTypes = parameterTypeMap(method.Parameters)
		body, err = transformOverloads(body, methodContext.Overloads)
		if err != nil {
			return err
		}

		out.WriteString(body)

		out.WriteString("\n}\n\n")
	}

	classes := map[string]*ClassDecl{}
	if target, ok := context.Targets[class.Name]; ok {
		classes = target.Classes
	}

	methods, err := interfaceMethods(class, classes, map[string]bool{})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "type __gopp_%s interface {\n", class.Name)
	out.WriteString("\tGoppRuntimeClass() *GoppClass\n")
	for _, method := range methods {
		parameters, err := transformParameterList(method.Parameters, context)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "\t%s(%s)", methodOutputName(method), parameters)
		result := transformPolymorphicResultType(strings.TrimSpace(method.Result), context)
		if result != "" {
			fmt.Fprintf(out, " %s", result)
		}
		out.WriteByte('\n')
	}
	out.WriteString("}\n\n")
	fmt.Fprintf(out, "type Gopp%s = __gopp_%s\n\n", class.Name, class.Name)
	fmt.Fprintf(out, "func (this %s) GoppRuntimeClass() *GoppClass {\n", class.Name)
	if context.Introspection != nil && context.Introspection.Enabled {
		out.WriteString("\tif this.GoppDynamicClass != nil { return this.GoppDynamicClass }\n")
	}
	fmt.Fprintf(out, "\treturn Gopp%sClass\n}\n\n", class.Name)
	if err := emitClassDescriptor(out, class, classes, context); err != nil {
		return err
	}

	return nil
}

func transformParameterList(params string, context constructorContext) (string, error) {
	parameters, err := parseParameterInfos(params)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		typeName := transformPolymorphicType(parameter.Type, context)
		if parameter.Name == "" {
			parts = append(parts, typeName)
		} else {
			parts = append(parts, parameter.Name+" "+typeName)
		}
	}
	return strings.Join(parts, ", "), nil
}

func interfaceMethods(class *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) ([]Method, error) {
	if visiting[class.Name] {
		return nil, fmt.Errorf("inheritance cycle involving class %s", class.Name)
	}

	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	methods := []Method{}
	seen := map[string]bool{}
	for _, method := range class.Methods {
		methods = append(methods, method)
		arity, err := parameterCount(method.Parameters)
		if err != nil {
			return nil, err
		}
		seen[method.Name+fmt.Sprintf("/%d", arity)] = true
	}

	for _, parentName := range class.Parents {
		parent, ok := classes[parentName]
		if !ok {
			return nil, fmt.Errorf(
				"class %s has unresolved parent %s",
				class.Name,
				parentName,
			)
		}

		parentMethods, err := interfaceMethods(parent, classes, visiting)
		if err != nil {
			return nil, err
		}
		for _, method := range parentMethods {
			arity, err := parameterCount(method.Parameters)
			if err != nil {
				return nil, err
			}
			key := method.Name + fmt.Sprintf("/%d", arity)
			if !seen[key] {
				methods = append(methods, method)
				seen[key] = true
			}
		}
	}

	return methods, nil
}

func methodOutputName(method Method) string {
	if method.GoName != "" {
		return method.GoName
	}
	return method.Name
}

func contextForMethod(context constructorContext, class *ClassDecl, methodName string) constructorContext {
	methodContext := context
	methodContext.CurrentClass = class.Name
	methodContext.CurrentMethod = methodName
	methodContext.CurrentResult = ""
	methodContext.MethodSignatures = context.ClassMethodSignatures[class.Name]
	return methodContext
}

func classesForClass(context constructorContext, class *ClassDecl) map[string]*ClassDecl {
	if target, ok := context.Targets[class.Name]; ok {
		return target.Classes
	}
	return map[string]*ClassDecl{class.Name: class}
}

func methodOverloadsForClass(class *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) map[string]map[int]string {
	if visiting[class.Name] {
		return map[string]map[int]string{}
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	overloads := map[string]map[int]string{}
	for _, method := range class.Methods {
		if method.GoName == "" {
			continue
		}
		arity, err := parameterCount(method.Parameters)
		if err != nil {
			continue
		}
		if overloads[method.Name] == nil {
			overloads[method.Name] = map[int]string{}
		}
		overloads[method.Name][arity] = method.GoName
	}

	for _, parentName := range class.Parents {
		parent, ok := classes[parentName]
		if !ok {
			continue
		}
		for name, methods := range methodOverloadsForClass(parent, classes, visiting) {
			if overloads[name] == nil {
				overloads[name] = map[int]string{}
			}
			for arity, goName := range methods {
				if _, exists := overloads[name][arity]; !exists {
					overloads[name][arity] = goName
				}
			}
		}
	}

	return overloads
}

func methodOverloadTypesForClass(class *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) map[string]map[string]string {
	if visiting[class.Name] {
		return map[string]map[string]string{}
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	overloads := map[string]map[string]string{}
	for _, method := range class.Methods {
		if method.GoName == "" {
			continue
		}
		parameters, err := parseParameterInfos(method.Parameters)
		if err != nil {
			continue
		}
		if overloads[method.Name] == nil {
			overloads[method.Name] = map[string]string{}
		}
		overloads[method.Name][parameterSignatureKey(parameters)] = method.GoName
	}
	for _, parentName := range class.Parents {
		parent, ok := classes[parentName]
		if !ok {
			continue
		}
		for name, methods := range methodOverloadTypesForClass(parent, classes, visiting) {
			if overloads[name] == nil {
				overloads[name] = map[string]string{}
			}
			for typeKey, goName := range methods {
				if _, exists := overloads[name][typeKey]; !exists {
					overloads[name][typeKey] = goName
				}
			}
		}
	}
	return overloads
}

func transformPolymorphicDeclarations(src string, context constructorContext) (string, error) {
	if len(context.Targets) == 0 {
		return src, nil
	}

	const filePrefix = "package main\n\n"
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "", filePrefix+src, 0)
	prefixLength := len(filePrefix)
	if err != nil {
		functionPrefix := "package main\n\nfunc __gopp_scope()"
		if strings.TrimSpace(context.CurrentResult) != "" {
			functionPrefix += " " + strings.TrimSpace(context.CurrentResult)
		}
		functionPrefix += " {\n"
		functionSet := token.NewFileSet()
		parsed, err = parser.ParseFile(
			functionSet,
			"",
			functionPrefix+src+"\n}",
			0,
		)
		if err != nil {
			return src, nil
		}
		fileSet = functionSet
		prefixLength = len(functionPrefix)
	}

	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	addTypeEdit := func(expr ast.Expr, replacement string) {
		start := fileSet.Position(expr.Pos()).Offset - prefixLength
		end := fileSet.Position(expr.End()).Offset - prefixLength
		if start >= 0 && end <= len(src) && start <= end {
			edits = append(edits, edit{start: start, end: end, text: replacement})
		}
	}
	addPointerEdit := func(expr ast.Expr) {
		start := fileSet.Position(expr.Pos()).Offset - prefixLength
		if start >= 0 && start <= len(src) {
			edits = append(edits, edit{start: start, end: start, text: "&"})
		}
	}

	ast.Inspect(parsed, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.ValueSpec:
			if declaration.Type == nil || len(declaration.Names) != 1 || len(declaration.Values) != 1 {
				return true
			}

			base, ok := dispatchTargetForType(declaration.Type, context)
			if !ok {
				return true
			}
			derivedName := expressionClassName(declaration.Values[0], context, polymorphicValueTypes(parsed, context))
			if derivedName == "" {
				return true
			}
			derived, ok := context.Targets[derivedName]
			if !ok || !sameConstructorPackage(base, derived) ||
				(derived.Class != base.Class &&
					!classInherits(derived.Class, base.Class, derived.Classes, map[string]bool{})) {
				return true
			}

			addTypeEdit(declaration.Type, dispatchInterfaceType(base))
			addPointerEdit(declaration.Values[0])

		case *ast.FuncDecl:
			if declaration.Type.Params != nil {
				for _, field := range declaration.Type.Params.List {
					if base, ok := dispatchTargetForType(field.Type, context); ok {
						addTypeEdit(field.Type, dispatchInterfaceType(base))
					}
				}
			}
			if declaration.Type.Results == nil || len(declaration.Type.Results.List) != 1 || declaration.Body == nil {
				return true
			}

			resultType, resultErr := formatNode(declaration.Type.Results.List[0].Type)
			if resultErr != nil {
				return true
			}
			base, isDispatchResult := dispatchTargetForType(declaration.Type.Results.List[0].Type, context)
			if isDispatchResult {
				addTypeEdit(declaration.Type.Results.List[0].Type, dispatchInterfaceType(base))
			} else if pointerResult := transformPolymorphicResultType(resultType, context); pointerResult != resultType {
				addTypeEdit(declaration.Type.Results.List[0].Type, pointerResult)
			} else {
				return true
			}

			valueTypes := polymorphicValueTypes(parsed, context)
			pointerResult := transformPolymorphicResultType(resultType, context)

			ast.Inspect(declaration.Body, func(child ast.Node) bool {
				returnStmt, ok := child.(*ast.ReturnStmt)
				if !ok || len(returnStmt.Results) != 1 {
					return true
				}

				derivedName := expressionClassName(returnStmt.Results[0], context, valueTypes)
				if derivedName == "" {
					return true
				}
				derived, ok := context.Targets[derivedName]
				if isDispatchResult && (!ok || !sameConstructorPackage(base, derived) ||
					(derived.Class != base.Class && !classInherits(derived.Class, base.Class, derived.Classes, map[string]bool{}))) {
					return true
				}

				if isDispatchResult || pointerResult != resultType {
					if !strings.HasPrefix(strings.TrimSpace(expressionStaticType(returnStmt.Results[0], context, valueTypes)), "*") {
						addPointerEdit(returnStmt.Results[0])
					}
				}
				return true
			})

		case *ast.CallExpr:
			parameterTypes := callParameterTypes(declaration, context, polymorphicValueTypes(parsed, context))
			for index, argument := range declaration.Args {
				if index >= len(parameterTypes) {
					break
				}
				if shouldPointerCoerce(parameterTypes[index], argument, context) {
					addPointerEdit(argument)
				}
			}
		}
		return true
	})

	sort.Slice(edits, func(i, j int) bool {
		return edits[i].start > edits[j].start
	})
	for _, change := range edits {
		src = src[:change.start] + change.text + src[change.end:]
	}

	return src, nil
}

func callParameterTypes(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) []string {
	var candidates []callableSignature
	switch function := call.Fun.(type) {
	case *ast.Ident:
		candidates = context.FunctionSignatures[function.Name]
	case *ast.SelectorExpr:
		if receiver, ok := function.X.(*ast.Ident); ok && receiver.Name == "this" && context.CurrentClass != "" {
			for _, signature := range context.ClassMethodSignatures[context.CurrentClass][function.Sel.Name] {
				candidates = append(candidates, signature)
			}
		} else if receiver, ok := function.X.(*ast.Ident); ok {
			for _, signature := range context.ClassMethodSignatures[valueTypes[receiver.Name]][function.Sel.Name] {
				candidates = append(candidates, signature)
			}
		}
	}
	for _, candidate := range candidates {
		if len(candidate.Parameters) != len(call.Args) {
			continue
		}
		matches := true
		for index, parameter := range candidate.Parameters {
			actual := expressionStaticType(call.Args[index], context, valueTypes)
			if actual != "" && actual != parameter.Type &&
				!shouldPointerCoerce(parameter.Type, call.Args[index], context) {
				matches = false
				break
			}
		}
		if matches {
			result := make([]string, len(candidate.Parameters))
			for index, parameter := range candidate.Parameters {
				result[index] = parameter.Type
			}
			return result
		}
	}
	return nil
}

func polymorphicValueTypes(root ast.Node, context constructorContext) map[string]string {
	result := map[string]string{}
	ast.Inspect(root, func(node ast.Node) bool {
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
			typeName := safeASTTypeName(declaration.Type)
			for index, name := range declaration.Names {
				inferred := typeName
				if inferred == "" && index < len(declaration.Values) {
					inferred = expressionStaticType(declaration.Values[index], context, result)
				}
				if inferred != "" {
					result[name.Name] = inferred
				}
			}
		case *ast.AssignStmt:
			for index, left := range declaration.Lhs {
				name, ok := left.(*ast.Ident)
				if !ok || index >= len(declaration.Rhs) {
					continue
				}
				if inferred := expressionStaticType(declaration.Rhs[index], context, result); inferred != "" {
					result[name.Name] = inferred
				}
			}
		}
		return true
	})
	return result
}

func expressionStaticType(expr ast.Expr, context constructorContext, valueTypes map[string]string) string {
	switch value := expr.(type) {
	case *ast.UnaryExpr:
		if value.Op == token.AND {
			name := expressionStaticType(value.X, context, valueTypes)
			if name != "" && !strings.HasPrefix(name, "*") {
				return "*" + name
			}
			return name
		}
	case *ast.CallExpr:
		if result := callResultType(value, context, valueTypes); result != "" {
			return result
		}
	case *ast.Ident:
		if valueTypes != nil && valueTypes[value.Name] != "" {
			return valueTypes[value.Name]
		}
	}
	return astExpressionTypeKeyWithEnv(expr, valueTypes)
}

func expressionClassName(expr ast.Expr, context constructorContext, valueTypes map[string]string) string {
	typeName := expressionStaticType(expr, context, valueTypes)
	typeName = strings.TrimPrefix(strings.TrimSpace(typeName), "*")
	if _, ok := context.Targets[typeName]; ok {
		return typeName
	}
	for key, target := range context.Targets {
		if target.Class.Name == typeName {
			return key
		}
	}
	return ""
}

func callResultType(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) string {
	var candidates []callableSignature
	switch function := call.Fun.(type) {
	case *ast.Ident:
		candidates = context.FunctionSignatures[function.Name]
	case *ast.SelectorExpr:
		if receiver, ok := function.X.(*ast.Ident); ok {
			className := context.CurrentClass
			if receiver.Name != "this" {
				className = strings.TrimPrefix(valueTypes[receiver.Name], "*")
			}
			candidates = context.ClassMethodSignatures[className][function.Sel.Name]
		}
	}

	for _, candidate := range candidates {
		if len(call.Args) < requiredParameterCount(candidate) || len(call.Args) > len(candidate.Parameters) {
			continue
		}
		matches := true
		for index, argument := range call.Args {
			actual := expressionStaticType(argument, context, valueTypes)
			expected := strings.Join(strings.Fields(candidate.Parameters[index].Type), " ")
			if actual != "" && actual != expected && !isAssignableStaticType(actual, expected, context) {
				matches = false
				break
			}
		}
		if matches {
			return transformPolymorphicResultType(candidate.Result, context)
		}
	}
	return ""
}

func isAssignableStaticType(actual, expected string, context constructorContext) bool {
	actualName := strings.TrimPrefix(strings.TrimSpace(actual), "*")
	expectedTarget, expectedIsClass := context.Targets[strings.TrimPrefix(strings.TrimSpace(expected), "*")]
	actualTarget, actualIsClass := context.Targets[actualName]
	if !expectedIsClass || !actualIsClass || !sameConstructorPackage(expectedTarget, actualTarget) {
		return false
	}
	return actualTarget.Class == expectedTarget.Class || classInherits(
		actualTarget.Class,
		expectedTarget.Class,
		actualTarget.Classes,
		map[string]bool{},
	)
}

func shouldPointerCoerce(expectedType string, argument ast.Expr, context constructorContext) bool {
	base, ok := dispatchTargetForTypeName(expectedType, context)
	if !ok {
		return false
	}
	actualType := expressionStaticType(argument, context, nil)
	if strings.HasPrefix(strings.TrimSpace(actualType), "*") {
		return false
	}
	actualName := strings.TrimPrefix(strings.TrimSpace(actualType), "*")
	if actualName == "" {
		return false
	}
	actual, ok := context.Targets[actualName]
	if !ok || !sameConstructorPackage(base, actual) {
		return false
	}
	return actual.Class == base.Class || classInherits(actual.Class, base.Class, actual.Classes, map[string]bool{})
}

func dispatchTargetForType(expr ast.Expr, context constructorContext) (constructorTarget, bool) {
	name, ok := astTypeName(expr)
	if !ok {
		return constructorTarget{}, false
	}

	target, ok := context.Targets[name]
	if !ok || !classHasDerived(target.Class, target.Classes) {
		return constructorTarget{}, false
	}

	return target, true
}

func transformPolymorphicType(typeName string, context constructorContext) string {
	name := strings.TrimSpace(typeName)
	target, ok := context.Targets[name]
	if !ok || !classHasDerived(target.Class, target.Classes) {
		return typeName
	}

	return dispatchInterfaceType(target)
}

func transformPolymorphicResultType(typeName string, context constructorContext) string {
	name := strings.TrimSpace(typeName)
	if name == "" {
		return name
	}
	if transformed := transformPolymorphicType(name, context); transformed != name {
		return transformed
	}

	prefix := ""
	baseName := name
	if strings.HasPrefix(baseName, "*") {
		prefix = "*"
		baseName = strings.TrimSpace(strings.TrimPrefix(baseName, "*"))
	}
	target, ok := context.Targets[baseName]
	if !ok || prefix == "*" || !classParticipatesInDispatch(target.Class, target.Classes) {
		return name
	}
	return "*" + baseName
}

func classParticipatesInDispatch(class *ClassDecl, classes map[string]*ClassDecl) bool {
	return len(class.Parents) > 0 || classHasDerived(class, classes)
}

func dispatchInterfaceType(target constructorTarget) string {
	if target.Qualifier == "" {
		return target.InterfaceName
	}
	return target.Qualifier + ".Gopp" + target.Class.Name
}

func sameConstructorPackage(base, derived constructorTarget) bool {
	return base.Qualifier == derived.Qualifier
}

func classHasDerived(base *ClassDecl, classes map[string]*ClassDecl) bool {
	for _, class := range classes {
		if class != base && classInherits(class, base, classes, map[string]bool{}) {
			return true
		}
	}
	return false
}

func transformOverloads(src string, overloads overloadContext) (string, error) {
	if len(overloads.Functions) == 0 && len(overloads.FunctionTypes) == 0 &&
		len(overloads.Methods) == 0 && len(overloads.MethodTypes) == 0 &&
		len(overloads.ClassMethods) == 0 {
		return src, nil
	}

	const filePrefix = "package main\n\n"
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "", filePrefix+src, 0)
	prefixLength := len(filePrefix)
	if err != nil {
		const functionPrefix = "package main\n\nfunc __gopp_scope() {\n"
		functionSet := token.NewFileSet()
		parsed, err = parser.ParseFile(functionSet, "", functionPrefix+src+"\n}", 0)
		if err != nil {
			return src, nil
		}
		fileSet = functionSet
		prefixLength = len(functionPrefix)
	}

	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	addEdit := func(node ast.Node, text string) {
		start := fileSet.Position(node.Pos()).Offset - prefixLength
		end := fileSet.Position(node.End()).Offset - prefixLength
		if start >= 0 && end <= len(src) && start <= end {
			edits = append(edits, edit{start: start, end: end, text: text})
		}
	}

	receiverTypes := map[string]string{}
	valueTypes := map[string]string{}
	var resolutionErr error
	for name, typeName := range overloads.LocalTypes {
		valueTypes[name] = typeName
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
						valueTypes[name.Name] = strings.Join(strings.Fields(typeName), " ")
					}
				}
			}
		case *ast.ValueSpec:
			declaredType, _ := astTypeName(declaration.Type)
			if declaredType == "" && declaration.Type != nil {
				declaredType, _ = formatNode(declaration.Type)
			}
			for index, name := range declaration.Names {
				typeName := declaredType
				if typeName == "" && index < len(declaration.Values) {
					typeName = astExpressionTypeKeyWithEnv(declaration.Values[index], valueTypes)
				}
				if typeName != "" {
					receiverTypes[name.Name] = typeName
					valueTypes[name.Name] = typeName
				}
			}
		case *ast.AssignStmt:
			if declaration.Tok != token.DEFINE && declaration.Tok != token.ASSIGN {
				return true
			}
			for index, left := range declaration.Lhs {
				name, ok := left.(*ast.Ident)
				if !ok || index >= len(declaration.Rhs) {
					continue
				}
				if typeName := constructorTypeName(declaration.Rhs[index]); typeName != "" {
					receiverTypes[name.Name] = typeName
					valueTypes[name.Name] = typeName
				} else if typeName := astExpressionTypeKeyWithEnv(declaration.Rhs[index], valueTypes); typeName != "" {
					valueTypes[name.Name] = typeName
				}
			}
		}
		return true
	})

	ast.Inspect(parsed, func(node ast.Node) bool {
		if resolutionErr != nil {
			return false
		}
		switch value := node.(type) {
		case *ast.FuncDecl:
			arity, err := astParameterCount(value.Type)
			if err != nil {
				return true
			}
			renamed, ok := overloads.FunctionTypes[value.Name.Name][astParameterSignatureKey(value.Type)]
			if !ok {
				renamed, ok = overloads.Functions[value.Name.Name][arity]
			}
			if ok {
				addEdit(value.Name, renamed)
			}

		case *ast.CallExpr:
			arity := len(value.Args)
			switch function := value.Fun.(type) {
			case *ast.Ident:
				typeKey := astArgumentSignatureKey(value, valueTypes)
				typeSet := overloads.FunctionTypes[function.Name]
				renamed, ok := typeSet[typeKey]
				if !ok {
					renamed, ok = overloads.Functions[function.Name][arity]
				}
				if !ok && len(typeSet) > 0 {
					resolutionErr = fmt.Errorf("cannot resolve overloaded function %s with argument types %s", function.Name, typeKey)
					return false
				}
				if ok {
					addEdit(function, renamed)
				}
			case *ast.SelectorExpr:
				methodSet := overloads.Methods
				methodTypeSet := overloads.MethodTypes
				if receiver, ok := function.X.(*ast.Ident); ok && receiver.Name != "this" {
					if typeName := receiverTypes[receiver.Name]; typeName != "" {
						methodSet = overloads.ClassMethods[typeName]
						methodTypeSet = overloads.ClassMethodTypes[typeName]
					}
				}
				typeKey := astArgumentSignatureKey(value, valueTypes)
				typeSet := methodTypeSet[function.Sel.Name]
				renamed, ok := typeSet[typeKey]
				if !ok {
					renamed, ok = methodSet[function.Sel.Name][arity]
				}
				if !ok && len(typeSet) > 0 {
					resolutionErr = fmt.Errorf("cannot resolve overloaded method %s with argument types %s", function.Sel.Name, typeKey)
					return false
				}
				if ok {
					addEdit(function.Sel, renamed)
				}
			}
		}
		return true
	})
	if resolutionErr != nil {
		return src, resolutionErr
	}

	sort.Slice(edits, func(i, j int) bool {
		return edits[i].start > edits[j].start
	})
	for _, change := range edits {
		src = src[:change.start] + change.text + src[change.end:]
	}

	return src, nil
}

func constructorTypeName(expr ast.Expr) string {
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		return constructorTypeName(unary.X)
	}
	returnName, _ := astTypeName(expr)
	return returnName
}

func astTypeName(expr ast.Expr) (string, bool) {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name, true
	case *ast.SelectorExpr:
		prefix, ok := value.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		return prefix.Name + "." + value.Sel.Name, true
	case *ast.CompositeLit:
		return astTypeName(value.Type)
	default:
		return "", false
	}
}

func classInherits(class, base *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) bool {
	if visiting[class.Name] {
		return false
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	for _, parentName := range class.Parents {
		parent, ok := classes[parentName]
		if !ok {
			continue
		}
		if parent == base || classInherits(parent, base, classes, visiting) {
			return true
		}
	}

	return false
}

// interpolationImport determines which fmt identifier interpolation should
// use. Existing Go imports are preserved as written; only a missing default
// fmt import is synthesized.
func interpolationImport(file *File) (string, bool, error) {
	if !fileHasInterpolation(file) {
		return "fmt", false, nil
	}

	var raw strings.Builder
	for _, decl := range file.Decls {
		if code, ok := decl.(*RawDecl); ok {
			raw.WriteString(code.Code)
		}
	}

	parsed, err := parser.ParseFile(
		token.NewFileSet(),
		file.Name,
		"package "+goPackageName(file.Package)+"\n\n"+raw.String(),
		parser.ImportsOnly,
	)
	if err == nil {
		for _, spec := range parsed.Imports {
			path, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil || path != "fmt" {
				continue
			}

			if spec.Name == nil {
				return "fmt", false, nil
			}

			switch spec.Name.Name {
			case ".":
				return "Sprintf", false, nil
			case "_":
				return "", false, fmt.Errorf(
					"string interpolation requires fmt to be imported with a usable name",
				)
			default:
				return spec.Name.Name, false, nil
			}
		}
	}

	return "fmt", true, nil
}

func fileHasInterpolation(file *File) bool {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *RawDecl:
			if strings.Contains(d.Code, "{{") {
				return true
			}
		case *ClassDecl:
			for _, method := range d.Methods {
				if strings.Contains(method.Body, "{{") {
					return true
				}
			}
		}
	}

	return false
}

func classesInFile(file *File) map[string]*ClassDecl {
	classes := map[string]*ClassDecl{}

	for _, decl := range file.Decls {
		if class, ok := decl.(*ClassDecl); ok {
			classes[class.Name] = class
		}
	}

	return classes
}

// transformConstructors lowers Go++ class calls into Go struct literals.
// Positional arguments use the class field order; named arguments use the
// field names written by the caller.
func transformConstructors(src string, context constructorContext) (string, error) {
	var out strings.Builder

	for i := 0; i < len(src); {
		switch src[i] {
		case '"':
			end, err := skipQuoted(src, i, '"')
			if err != nil {
				return "", err
			}

			out.WriteString(src[i : end+1])
			i = end + 1
			continue

		case '\'':
			end, err := skipQuoted(src, i, '\'')
			if err != nil {
				return "", err
			}

			out.WriteString(src[i : end+1])
			i = end + 1
			continue

		case '`':
			end := i + 1
			for end < len(src) && src[end] != '`' {
				end++
			}

			if end >= len(src) {
				return "", fmt.Errorf("unterminated raw string")
			}

			out.WriteString(src[i : end+1])
			i = end + 1
			continue

		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				end := i + 2
				for end < len(src) && src[end] != '\n' {
					end++
				}

				out.WriteString(src[i:end])
				i = end
				continue
			}

			if i+1 < len(src) && src[i+1] == '*' {
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return "", fmt.Errorf("unterminated comment")
				}

				end += i + 2
				out.WriteString(src[i : end+2])
				i = end + 2
				continue
			}
		}

		name, n := readIdent(src[i:])
		if n == 0 {
			out.WriteByte(src[i])
			i++
			continue
		}

		// A qualified call such as other.Person(...) may refer to an imported
		// Go++ class; unknown qualified calls remain ordinary Go code.
		qualified := i > 0 && (src[i-1] == '.' || isIdentPart(src[i-1]))
		if qualified {
			out.WriteString(src[i : i+n])
			i += n
			continue
		}

		constructorName := name
		constructorEnd := i + n
		target, ok := context.Targets[name]

		if !ok && constructorEnd < len(src) && src[constructorEnd] == '.' {
			member, memberLength := readIdent(src[constructorEnd+1:])
			if memberLength > 0 {
				qualifiedName := name + "." + member
				target, ok = context.Targets[qualifiedName]
				if ok {
					constructorName = src[i : constructorEnd+1+memberLength]
					constructorEnd += 1 + memberLength
				}
			}
		}

		if !ok {
			out.WriteString(src[i : i+n])
			i += n
			continue
		}

		open := skipSpace(src, constructorEnd)
		if open >= len(src) || src[open] != '(' {
			out.WriteString(src[i : i+n])
			i += n
			continue
		}

		close, err := findMatchingParen(src, open)
		if err != nil {
			return "", fmt.Errorf("%s constructor: %w", name, err)
		}

		literal, err := emitConstructor(
			constructorName,
			target,
			context,
			src[open+1:close],
		)
		if err != nil {
			return "", err
		}

		out.WriteString(literal)
		i = close + 1
	}

	return out.String(), nil
}

type constructorField struct {
	Name  string
	Type  string
	Path  []string
	Owner string
}

type constructorLiteral struct {
	Values   map[string]string
	Children map[string]*constructorLiteral
}

func emitConstructor(name string, target constructorTarget, context constructorContext, argsSource string) (string, error) {
	class := target.Class
	classes := target.Classes
	args, err := splitTopLevel(argsSource, ',')
	if err != nil {
		return "", fmt.Errorf("%s constructor: %w", name, err)
	}

	for len(args) > 0 && strings.TrimSpace(args[len(args)-1]) == "" {
		args = args[:len(args)-1]
	}

	if len(args) == 0 {
		args = nil
	}

	if len(args) == 0 {
		return emitConstructorLiteral(name, class, &constructorLiteral{
			Values:   map[string]string{},
			Children: map[string]*constructorLiteral{},
		}, classes, target.Qualifier, context.Introspection != nil && context.Introspection.Enabled), nil
	}

	fields, err := constructorFields(class, classes, nil, map[string]bool{})
	if err != nil {
		return "", err
	}

	named := strings.Contains(args[0], ":")
	if !named && len(args) != len(fields) {
		return "", fmt.Errorf(
			"%s constructor expects %d arguments, got %d",
			name,
			len(fields),
			len(args),
		)
	}

	root := &constructorLiteral{
		Values:   map[string]string{},
		Children: map[string]*constructorLiteral{},
	}

	if named {
		seen := map[string]bool{}

		for index, arg := range args {
			colon := topLevelColon(arg)
			if colon < 0 {
				return "", fmt.Errorf(
					"%s constructor mixes positional and named arguments at argument %d",
					name,
					index+1,
				)
			}

			fieldName := strings.TrimSpace(arg[:colon])
			value := strings.TrimSpace(arg[colon+1:])
			if value == "" {
				return "", fmt.Errorf(
					"%s constructor argument %s is missing a value",
					name,
					fieldName,
				)
			}

			ref, err := resolveConstructorField(fields, fieldName)
			if err != nil {
				return "", fmt.Errorf("%s constructor: %w", name, err)
			}

			key := strings.Join(append(ref.Path, ref.Name), ".")
			if seen[key] {
				return "", fmt.Errorf(
					"%s constructor repeats named argument %s",
					name,
					fieldName,
				)
			}

			value, err = transformConstructors(value, context)
			if err != nil {
				return "", err
			}
			value = transformPolymorphicValue(value, ref.Type, context)

			seen[key] = true
			setConstructorValue(root, ref.Path, ref.Name, value)
		}
	} else {
		for index, arg := range args {
			value, err := transformConstructors(strings.TrimSpace(arg), context)
			if err != nil {
				return "", err
			}
			value = transformPolymorphicValue(value, fields[index].Type, context)

			setConstructorValue(root, fields[index].Path, fields[index].Name, value)
		}
	}

	return emitConstructorLiteral(name, class, root, classes, target.Qualifier, context.Introspection != nil && context.Introspection.Enabled), nil
}

func constructorFields(class *ClassDecl, classes map[string]*ClassDecl, prefix []string, visiting map[string]bool) ([]constructorField, error) {
	if visiting[class.Name] {
		return nil, fmt.Errorf("inheritance cycle involving class %s", class.Name)
	}

	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	fields := []constructorField{}
	for _, parentName := range class.Parents {
		parent, ok := classes[parentName]
		if !ok {
			return nil, fmt.Errorf(
				"class %s has unresolved parent %s",
				class.Name,
				parentName,
			)
		}

		parentFields, err := constructorFields(
			parent,
			classes,
			append(prefix, parentName),
			visiting,
		)
		if err != nil {
			return nil, err
		}
		fields = append(fields, parentFields...)
	}

	for _, field := range class.Fields {
		fields = append(fields, constructorField{
			Name:  field.Name,
			Type:  field.Type,
			Path:  append([]string(nil), prefix...),
			Owner: class.Name,
		})
	}

	return fields, nil
}

func transformPolymorphicValue(value, fieldType string, context constructorContext) string {
	base, ok := dispatchTargetForTypeName(fieldType, context)
	if !ok {
		return value
	}

	parsed, err := parser.ParseExpr(strings.TrimSpace(value))
	if err != nil {
		return value
	}

	concreteName, ok := astTypeName(parsed)
	if !ok {
		return value
	}
	concrete, ok := context.Targets[concreteName]
	if !ok || !sameConstructorPackage(base, concrete) ||
		(concrete.Class != base.Class &&
			!classInherits(concrete.Class, base.Class, concrete.Classes, map[string]bool{})) {
		return value
	}

	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "&") {
		return value
	}
	return "&" + value
}

func dispatchTargetForTypeName(typeName string, context constructorContext) (constructorTarget, bool) {
	name := strings.TrimSpace(typeName)
	if target, ok := context.Targets[name]; ok && classHasDerived(target.Class, target.Classes) {
		return target, true
	}
	return constructorTarget{}, false
}

func resolveConstructorField(fields []constructorField, name string) (constructorField, error) {
	parts := strings.Split(name, ".")
	if len(parts) == 0 {
		return constructorField{}, fmt.Errorf("invalid named argument %q", name)
	}

	for _, part := range parts {
		if !isIdentifier(part) {
			return constructorField{}, fmt.Errorf("invalid named argument %q", name)
		}
	}

	matches := []constructorField{}
	for _, field := range fields {
		fieldParts := append(append([]string(nil), field.Path...), field.Name)
		if strings.Join(fieldParts, ".") == name {
			matches = append(matches, field)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return constructorField{}, fmt.Errorf(
			"ambiguous named argument %s; qualify it with its parent class",
			name,
		)
	}

	// A direct child field shadows promoted parent fields, matching Go's
	// selector behavior. Otherwise an unqualified inherited name is allowed
	// only when exactly one field has that name.
	if len(parts) == 1 {
		direct := []constructorField{}
		for _, field := range fields {
			if field.Name == name && len(field.Path) == 0 {
				direct = append(direct, field)
			}
		}
		if len(direct) == 1 {
			return direct[0], nil
		}

		unqualified := []constructorField{}
		for _, field := range fields {
			if field.Name == name {
				unqualified = append(unqualified, field)
			}
		}
		if len(unqualified) == 1 {
			return unqualified[0], nil
		}
		if len(unqualified) > 1 {
			return constructorField{}, fmt.Errorf(
				"ambiguous named argument %s; qualify it with its parent class",
				name,
			)
		}
	}

	return constructorField{}, fmt.Errorf("unknown field %s", name)
}

func setConstructorValue(root *constructorLiteral, path []string, fieldName, value string) {
	node := root
	for _, segment := range path {
		child, ok := node.Children[segment]
		if !ok {
			child = &constructorLiteral{
				Values:   map[string]string{},
				Children: map[string]*constructorLiteral{},
			}
			node.Children[segment] = child
		}
		node = child
	}
	node.Values[fieldName] = value
}

func emitConstructorLiteral(name string, class *ClassDecl, literal *constructorLiteral, classes map[string]*ClassDecl, qualifier string, withDynamicClass bool) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s{", name)
	writeConstructorMembers(&out, class, literal, classes, qualifier, descriptorName(qualifier, class.Name), withDynamicClass)
	out.WriteByte('}')
	return out.String()
}

func writeConstructorMembers(out *strings.Builder, class *ClassDecl, literal *constructorLiteral, classes map[string]*ClassDecl, qualifier, dynamicDescriptor string, withDynamicClass bool) {
	first := true
	writeMember := func(name, value string) {
		if !first {
			out.WriteString(", ")
		}
		first = false
		fmt.Fprintf(out, "%s: %s", name, value)
	}

	for _, parentName := range class.Parents {
		child, ok := literal.Children[parentName]
		if !ok && !withDynamicClass {
			continue
		}
		if !ok {
			child = &constructorLiteral{
				Values:   map[string]string{},
				Children: map[string]*constructorLiteral{},
			}
		}

		parent := classes[parentName]
		var nested strings.Builder
		fmt.Fprintf(&nested, "%s{", qualifyTypeName(parentName, qualifier))
		writeConstructorMembers(&nested, parent, child, classes, qualifier, dynamicDescriptor, withDynamicClass)
		nested.WriteByte('}')
		writeMember(parentName, nested.String())
	}

	for _, field := range class.Fields {
		if value, ok := literal.Values[field.Name]; ok {
			writeMember(field.Name, value)
		}
	}
	if withDynamicClass {
		writeMember("GoppDynamicClass", dynamicDescriptor)
	}
}

func descriptorName(qualifier, className string) string {
	if qualifier == "" {
		return "Gopp" + className + "Class"
	}
	return qualifier + ".Gopp" + className + "Class"
}

func qualifyTypeName(name, qualifier string) string {
	if qualifier == "" {
		return name
	}
	return qualifier + "." + name
}

func splitTopLevel(src string, separator byte) ([]string, error) {
	parts := []string{}
	start := 0
	parenDepth := 0
	braceDepth := 0
	bracketDepth := 0

	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '"', '\'':
			end, err := skipQuoted(src, i, src[i])
			if err != nil {
				return nil, err
			}
			i = end

		case '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return nil, fmt.Errorf("unterminated raw string")
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
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				end := strings.IndexByte(src[i+2:], '\n')
				if end < 0 {
					return nil, fmt.Errorf("comment after argument")
				}
				i += end + 2
			} else if i+1 < len(src) && src[i+1] == '*' {
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return nil, fmt.Errorf("unterminated comment")
				}
				i += end + 3
			}
		}

		if src[i] == separator &&
			parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
			parts = append(parts, src[start:i])
			start = i + 1
		}
	}

	parts = append(parts, src[start:])
	return parts, nil
}

func topLevelColon(src string) int {
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
		case ':':
			if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 {
				return i
			}
		}
	}

	return -1
}

func isIdentifier(value string) bool {
	name, n := readIdent(value)
	return value != "" && n == len(value) && name == value
}

func isIdentPart(value byte) bool {
	return value >= 'A' && value <= 'Z' ||
		value >= 'a' && value <= 'z' ||
		value >= '0' && value <= '9' ||
		value == '_'
}

func transformInterpolation(src string) string {
	return transformInterpolationWithName(src, "fmt")
}

func transformInterpolationWithName(src, fmtName string) string {
	var out strings.Builder
	fmtCall := fmtName + ".Sprintf"
	if fmtName == "Sprintf" {
		fmtCall = fmtName
	}

	for i := 0; i < len(src); {
		if src[i] != '"' {
			out.WriteByte(src[i])
			i++
			continue
		}

		end, err := skipQuoted(src, i, '"')

		if err != nil {
			out.WriteString(src[i:])
			break
		}

		literal := src[i+1 : end]

		if !strings.Contains(literal, "{{") {
			out.WriteString(src[i : end+1])
			i = end + 1
			continue
		}

		matches := interpolationRE.FindAllStringSubmatch(
			literal,
			-1,
		)

		if len(matches) == 0 {
			out.WriteString(src[i : end+1])
			i = end + 1
			continue
		}

		formatString := interpolationRE.ReplaceAllString(
			literal,
			"%v",
		)

		fmt.Fprintf(
			&out,
			"%s(%q",
			fmtCall,
			formatString,
		)

		for _, match := range matches {
			fmt.Fprintf(
				&out,
				", %s",
				strings.TrimSpace(match[1]),
			)
		}

		out.WriteByte(')')

		i = end + 1
	}

	return out.String()
}
