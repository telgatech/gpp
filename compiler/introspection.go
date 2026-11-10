package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

// introspectionContext contains package-wide state for generated class
// metadata. It is shared by all generated files in a logical package so the
// runtime declarations are emitted exactly once.
type introspectionContext struct {
	Classes        map[string]*ClassDecl
	RuntimeEmitted bool
	Enabled        bool
}

func newIntrospectionContext(classes map[string]*ClassDecl) *introspectionContext {
	return &introspectionContext{Classes: classes}
}

func fileUsesIntrospection(file *File) bool {
	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *MixedDecl:
			if tokenSequence(mixedDeclTokens(value), ".", "class") {
				return true
			}
		case *GoDecl:
			if tokenSequence(goDeclTokens(value), ".", "class") {
				return true
			}
		case *ValueDecl:
			if tokenSequence(valueDeclTokens(value), ".", "class") {
				return true
			}
		case *ClassDecl:
			if len(classParentNames(value)) > 0 {
				return true
			}
			if len(value.Annotations) > 0 {
				return true
			}
			for _, field := range value.Fields {
				if len(field.Annotations) > 0 {
					return true
				}
			}
			for _, method := range value.Methods {
				if len(method.Annotations) > 0 || len(method.ParameterAnnotations) > 0 || tokenSequence(methodBodyTokens(method), ".", "class") {
					return true
				}
			}
		case *ExtendDecl:
			for _, method := range value.Methods {
				if len(method.Annotations) > 0 || len(method.ParameterAnnotations) > 0 {
					return true
				}
			}
		case *FunctionDecl:
			if len(value.Annotations) > 0 || len(value.Method.ParameterAnnotations) > 0 || tokenSequence(methodBodyTokens(value.Method), ".", "class") {
				return true
			}
		}
	}
	return false
}

func programUsesIntrospection(program *Program) bool {
	for _, file := range program.Files {
		if fileUsesIntrospection(file) {
			return true
		}
	}
	return false
}

func introspectionRuntimeDefinitions() string {
	return introspectionRuntimeDefinitionsWithAliases("gppFmt", "gppReflect", "gppStrconv", "gppStrings")
}

func introspectionRuntimeDefinitionsWithAliases(fmtAlias, reflectAlias, strconvAlias, stringsAlias string) string {
	definitions := `type GppType struct {
	Name string
}

func GppInvocationError(message string) error { return gppFmt.Errorf("%s", message) }
type GppAnnotationType struct {
	Name     string
	FullName string
}

func (annotation *GppAnnotationType) GppAnnotationFullName() string {
	if annotation == nil { return "" }
	return annotation.FullName
}

type GppAnnotation struct {
	Name     string
	FullName string
	Args     []any
	Type     any
}

type GppAnnotations []GppAnnotation

func (annotations GppAnnotations) Has(annotation any) bool {
	return annotations.find(annotation) >= 0
}

func (annotations GppAnnotations) Find(annotation any) *GppAnnotation {
	index := annotations.find(annotation)
	if index < 0 { return nil }
	return &annotations[index]
}

func (annotations GppAnnotations) All(annotation any) []GppAnnotation {
	result := []GppAnnotation{}
	for _, value := range annotations {
		if annotationMatches(value, annotation) { result = append(result, value) }
	}
	return result
}

func (annotations GppAnnotations) find(annotation any) int {
	for index, value := range annotations {
		if annotationMatches(value, annotation) { return index }
	}
	return -1
}

func annotationMatches(value GppAnnotation, annotation any) bool {
	switch key := annotation.(type) {
	case interface{ GppAnnotationFullName() string }:
		return value.FullName == key.GppAnnotationFullName()
	case string:
		return value.Name == key || value.FullName == key
	default:
		return false
	}
}

type GppField struct {
	Name  string
	Owner *GppClass
	Type  *GppType
	Get   func(any) any
	Set   func(any, any)
	Addr  func(any) any
	Annotations GppAnnotations
}

type GppMethod struct {
	Name        string
	Owner       *GppClass
	Parameters  []GppParameter
	Result      *GppType
	Static      bool
	Annotations GppAnnotations
	Invoke      func(any, []any) ([]any, error)
}

type GppFunction struct {
	Name        string
	Parameters  []GppParameter
	Result      *GppType
	Annotations GppAnnotations
	Invoke      func([]any) ([]any, error)
}

var gppRegisteredFunctions []GppFunction

func RegisterFunction(function GppFunction) {
	gppRegisteredFunctions = append(gppRegisteredFunctions, function)
}

func RegisteredFunctions() []GppFunction {
	return append([]GppFunction(nil), gppRegisteredFunctions...)
}

func __gppInvocationError(message string) error { return GppInvocationError(message) }
func __gppRegisterFunction(function GppFunction) { RegisterFunction(function) }
func __gppRegisteredFunctions() []GppFunction { return RegisteredFunctions() }

type GppParameter struct {
	Name        string
	Type        *GppType
	Annotations GppAnnotations
}

type GppClass struct {
	Name        string
	Package     string
	Parents     []*GppClass
	Fields      []GppField
	Methods     []GppMethod
	Annotations GppAnnotations
}

type gppFormatVisit struct {
	typeOf gppReflect.Type
	ptr    uintptr
}

func FormatObject(class *GppClass, object any, pretty bool) string {
	return gppFormatObject(class, object, pretty, map[gppFormatVisit]bool{}, 0)
}

func gppFormatObject(class *GppClass, object any, pretty bool, active map[gppFormatVisit]bool, depth int) string {
	if class == nil || object == nil {
		return "nil"
	}
	value := gppReflect.ValueOf(object)
	for value.IsValid() && value.Kind() == gppReflect.Interface {
		if value.IsNil() { return "nil" }
		value = value.Elem()
	}
	if !value.IsValid() { return "nil" }
	if value.Kind() == gppReflect.Pointer {
		if value.IsNil() { return "nil" }
		key := gppFormatVisit{typeOf: value.Type(), ptr: value.Pointer()}
		if active[key] { return "<cycle " + class.Name + ">" }
		active[key] = true
		defer delete(active, key)
	}

	counts := map[string]int{}
	for _, field := range class.Fields { counts[field.Name]++ }
	parts := make([]string, 0, len(class.Fields))
	for _, field := range class.Fields {
		name := field.Name
		if counts[name] > 1 && field.Owner != nil {
			name = field.Owner.Name + "." + name
		}
		parts = append(parts, name+": "+gppFormatValue(field.Get(object), pretty, active, depth+1))
	}
	if !pretty {
		return class.Name + "{" + gppStrings.Join(parts, ", ") + "}"
	}
	if len(parts) == 0 { return class.Name + " {}" }
	indent := gppStrings.Repeat("    ", depth)
	childIndent := gppStrings.Repeat("    ", depth+1)
	for index := range parts { parts[index] = childIndent + parts[index] }
	return class.Name + " {\n" + gppStrings.Join(parts, "\n") + "\n" + indent + "}"
}

func gppFormatValue(object any, pretty bool, active map[gppFormatVisit]bool, depth int) string {
	if object == nil { return "nil" }
	if provider, ok := object.(interface{ GppRuntimeClass() *GppClass }); ok {
		if class := provider.GppRuntimeClass(); class != nil {
			return gppFormatObject(class, object, pretty, active, depth)
		}
	}
	value := gppReflect.ValueOf(object)
	for value.IsValid() && value.Kind() == gppReflect.Interface {
		if value.IsNil() { return "nil" }
		value = value.Elem()
	}
	if !value.IsValid() { return "nil" }
	switch value.Kind() {
	case gppReflect.Pointer:
		if value.IsNil() { return "nil" }
		return gppFormatValue(value.Elem().Interface(), pretty, active, depth)
	case gppReflect.String:
		return gppStrconv.Quote(value.String())
	case gppReflect.Bool:
		return gppFmt.Sprint(value.Bool())
	case gppReflect.Int, gppReflect.Int8, gppReflect.Int16, gppReflect.Int32, gppReflect.Int64:
		return gppFmt.Sprint(value.Int())
	case gppReflect.Uint, gppReflect.Uint8, gppReflect.Uint16, gppReflect.Uint32, gppReflect.Uint64, gppReflect.Uintptr:
		return gppFmt.Sprint(value.Uint())
	case gppReflect.Float32, gppReflect.Float64:
		return gppFmt.Sprint(value.Float())
	case gppReflect.Slice, gppReflect.Array:
		items := make([]string, value.Len())
		for index := 0; index < value.Len(); index++ {
			items[index] = gppFormatValue(value.Index(index).Interface(), pretty, active, depth+1)
		}
		return "[" + gppStrings.Join(items, ", ") + "]"
	default:
		return gppFmt.Sprint(object)
	}
}

func __gppFormatObject(class *GppClass, object any, pretty bool) string {
	return FormatObject(class, object, pretty)
}

	`
	definitions = strings.ReplaceAll(definitions, "gppFmt", fmtAlias)
	definitions = strings.ReplaceAll(definitions, "gppReflect", reflectAlias)
	definitions = strings.ReplaceAll(definitions, "gppStrconv", strconvAlias)
	definitions = strings.ReplaceAll(definitions, "gppStrings", stringsAlias)
	return definitions
}

func introspectionRuntimeAliases() string {
	return `type GppType = gppRuntime.GppType
type GppAnnotationType = gppRuntime.GppAnnotationType
type GppAnnotation = gppRuntime.GppAnnotation
type GppAnnotations = gppRuntime.GppAnnotations
type GppField = gppRuntime.GppField
type GppMethod = gppRuntime.GppMethod
type GppFunction = gppRuntime.GppFunction
type GppParameter = gppRuntime.GppParameter
type GppClass = gppRuntime.GppClass

func __gppInvocationError(message string) error { return gppRuntime.GppInvocationError(message) }
func __gppRegisterFunction(function GppFunction) { gppRuntime.RegisterFunction(function) }
func __gppRegisteredFunctions() []GppFunction { return gppRuntime.RegisteredFunctions() }

func __gppFormatObject(class *GppClass, object any, pretty bool) string {
	return gppRuntime.FormatObject(class, object, pretty)
}
`
}

func annotatedFunctionRegistrationSource(function *FunctionDecl, context constructorContext) (string, error) {
	if function == nil || len(function.Annotations) == 0 {
		return "", nil
	}
	name := function.Name
	if overloaded := overloadFunctionName(function, context.Overloads); overloaded != "" {
		name = overloaded
	}
	invoker := "nil"
	if len(function.Method.TypeParamsAST) == 0 && !methodHasVariadicParameter(function.Method) {
		var err error
		invoker, err = functionInvocationLiteral(function.Method, name, context)
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("\nfunc init() { __gppRegisterFunction(GppFunction{Name: %q, Parameters: %s, Result: %s, Annotations: %s, Invoke: %s}) }\n",
		context.Package+"."+name,
		methodParametersLiteral(function.Method, context),
		methodResultLiteral(function.Method),
		annotationUsesLiteral(function.Annotations, context),
		invoker), nil
}

func methodHasVariadicParameter(method Method) bool {
	parameters, err := parameterInfosForMethod(method)
	if err != nil {
		return true
	}
	for _, parameter := range parameters {
		if strings.HasPrefix(strings.TrimSpace(parameter.typeText()), "...") {
			return true
		}
	}
	return false
}

func functionInvocationLiteral(method Method, name string, context constructorContext) (string, error) {
	parameters, err := parameterInfosForMethod(method)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("func(args []any) ([]any, error) {\n")
	arguments := make([]string, 0, len(parameters))
	for index, parameter := range parameters {
		argName := fmt.Sprintf("arg%d", index)
		arguments = append(arguments, argName)
		fmt.Fprintf(&out, "if len(args) <= %d { return nil, __gppInvocationError(%q) }; %s, ok := args[%d].(%s); if !ok { return nil, __gppInvocationError(%q) }\n", index, fmt.Sprintf("%s expects %d arguments", name, len(parameters)), argName, index, transformPolymorphicType(parameter.typeText(), context), fmt.Sprintf("argument %d has the wrong type", index+1))
	}
	fmt.Fprintf(&out, "if len(args) != %d { return nil, __gppInvocationError(%q) }\n", len(parameters), fmt.Sprintf("%s expects %d arguments", name, len(parameters)))
	call := name + "(" + strings.Join(arguments, ", ") + ")"
	result := transformPolymorphicResultType(strings.TrimSpace(methodResultSource(method)), context)
	if result == "" {
		fmt.Fprintf(&out, "%s; return nil, nil\n", call)
	} else {
		resultTypes := methodResultTypes(method)
		if len(resultTypes) <= 1 {
			fmt.Fprintf(&out, "result := %s; return []any{result}, nil\n", call)
		} else {
			resultNames := make([]string, len(resultTypes))
			for index := range resultNames {
				resultNames[index] = fmt.Sprintf("result%d", index)
			}
			fmt.Fprintf(&out, "%s := %s; return []any{%s}, nil\n", strings.Join(resultNames, ", "), call, strings.Join(resultNames, ", "))
		}
	}
	out.WriteString("}")
	return out.String(), nil
}

func ensureIntrospectionRuntimeImport(body, importPath string) string {
	if importPath == "" {
		return body
	}
	return "import gppRuntime " + strconv.Quote(importPath) + "\n\n" + body
}

func emitClassDescriptor(out *strings.Builder, class *ClassDecl, classes map[string]*ClassDecl, context constructorContext) error {
	fields, err := constructorFields(class, classes, nil, map[*ClassDecl]bool{})
	if err != nil {
		return err
	}
	fields = filterInaccessibleInheritedFields(class, fields)

	parentNames := classParentNames(class)
	parentReferences := make([]string, 0, len(parentNames))
	for _, parent := range parentNames {
		if reference := classDescriptorReference(parent, classes, context); reference != "" {
			parentReferences = append(parentReferences, reference)
		}
	}
	parents := "nil"
	if len(parentReferences) > 0 {
		parents = "[]*GppClass{" + strings.Join(parentReferences, ", ") + "}"
	}
	fmt.Fprintf(out, "var Gpp%sClass = &GppClass{Name: %q, Package: %q, Parents: %s, Annotations: %s, Methods: []GppMethod{\n", class.Name, class.Name, templatePackageScope(context.ModulePath, context.Package), parents, annotationUsesLiteral(class.Annotations, context))
	methods, err := effectiveMethodDescriptors(class, classes, map[*ClassDecl]bool{})
	if err != nil {
		return err
	}
	for _, descriptor := range methods {
		invoker := "nil"
		if descriptor.Owner == class.Name && len(descriptor.Method.Annotations) > 0 && len(descriptor.Method.TypeParamsAST) == 0 && !methodHasVariadicParameter(descriptor.Method) {
			invoker, err = methodInvocationLiteral(descriptor.Method, context)
			if err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "\t{Name: %q, Parameters: %s, Result: %s, Annotations: %s, Invoke: %s},\n", descriptor.Method.Name, methodParametersLiteral(descriptor.Method, context), methodResultLiteral(descriptor.Method), annotationUsesLiteral(descriptor.Method.Annotations, context), invoker)
	}
	staticMethods, err := effectiveStaticMethodDescriptors(class, classes, map[*ClassDecl]bool{})
	if err != nil {
		return err
	}
	for _, descriptor := range staticMethods {
		fmt.Fprintf(out, "\t{Name: %q, Parameters: %s, Result: %s, Static: true, Annotations: %s},\n", descriptor.Method.Name, methodParametersLiteral(descriptor.Method, context), methodResultLiteral(descriptor.Method), annotationUsesLiteral(descriptor.Method.Annotations, context))
	}
	out.WriteString("}, Fields: []GppField{\n")
	for _, field := range fields {
		path := append(append([]string(nil), field.Path...), generatedMemberName(field.Name))
		access := strings.Join(path, ".")
		owner := field.Owner
		if owner == "" {
			owner = class.Name
		}

		fieldType := field.Type
		fmt.Fprintf(out, "\t{Name: %q, Owner: nil, Type: &GppType{Name: %q}, Annotations: %s,\n", field.Name, fieldType, annotationUsesLiteral(field.Annotations, context))
		fmt.Fprintf(out, "\t\tGet: func(root any) any {\n")
		fmt.Fprintf(out, "\t\t\tswitch value := root.(type) {\n")
		fmt.Fprintf(out, "\t\t\tcase *%s:\n\t\t\t\treturn value.%s\n", class.Name, access)
		fmt.Fprintf(out, "\t\t\tcase %s:\n\t\t\t\treturn value.%s\n", class.Name, access)
		fmt.Fprintf(out, "\t\t\tdefault:\n\t\t\t\treturn nil\n\t\t\t}\n\t\t},\n")

		fmt.Fprintf(out, "\t\tSet: func(root any, input any) {\n")
		fmt.Fprintf(out, "\t\t\tvalue, ok := root.(*%s)\n\t\t\tif !ok { return }\n", class.Name)
		if isNilableGoType(fieldType) {
			fmt.Fprintf(out, "\t\t\tif input == nil { value.%s = nil; return }\n", access)
		}
		runtimeType := transformPolymorphicType(fieldType, context)
		fmt.Fprintf(out, "\t\t\tconverted, ok := input.(%s)\n\t\t\tif !ok { return }\n", runtimeType)
		fmt.Fprintf(out, "\t\t\tvalue.%s = converted\n\t\t},\n", access)

		fmt.Fprintf(out, "\t\tAddr: func(root any) any {\n")
		fmt.Fprintf(out, "\t\t\tvalue, ok := root.(*%s)\n\t\t\tif !ok { return nil }\n", class.Name)
		fmt.Fprintf(out, "\t\t\treturn &value.%s\n\t\t},\n", access)
		out.WriteString("\t},\n")
	}
	out.WriteString("},\n}\n\n")
	out.WriteString("func init() {\n")
	for index, field := range fields {
		owner := field.Owner
		if owner == "" {
			owner = class.Name
		}
		if reference := classDescriptorReference(owner, classes, context); reference != "" {
			fmt.Fprintf(out, "\tGpp%sClass.Fields[%d].Owner = %s\n", class.Name, index, reference)
		}
	}
	methodIndex := 0
	for _, descriptor := range methods {
		if reference := classDescriptorReference(descriptor.Owner, classes, context); reference != "" {
			fmt.Fprintf(out, "\tGpp%sClass.Methods[%d].Owner = %s\n", class.Name, methodIndex, reference)
		}
		methodIndex++
	}
	for _, descriptor := range staticMethods {
		if reference := classDescriptorReference(descriptor.Owner, classes, context); reference != "" {
			fmt.Fprintf(out, "\tGpp%sClass.Methods[%d].Owner = %s\n", class.Name, methodIndex, reference)
		}
		methodIndex++
	}
	out.WriteString("}\n\n")
	return nil
}

// methodInvocationLiteral emits a typed adapter for annotated instance methods.
// The adapter keeps runtime annotation consumers independent of method names
// and avoids reflection when invoking them.
func methodInvocationLiteral(method Method, context constructorContext) (string, error) {
	parameters, err := parameterInfosForMethod(method)
	if err != nil {
		return "", err
	}
	if method.IsStatic {
		return "nil", nil
	}
	var out strings.Builder
	out.WriteString("func(receiver any, args []any) ([]any, error) {\n")
	interfaceName := "invoker"
	parameterTypes := make([]string, 0, len(parameters))
	arguments := make([]string, 0, len(parameters))
	for index, parameter := range parameters {
		typeName := transformPolymorphicType(parameter.typeText(), context)
		parameterTypes = append(parameterTypes, fmt.Sprintf("%s %s", methodOutputNameForParameter(parameter.Name, index), typeName))
		arguments = append(arguments, methodOutputNameForParameter(parameter.Name, index))
	}
	result := transformPolymorphicResultType(strings.TrimSpace(methodResultSource(method)), context)
	fmt.Fprintf(&out, "type %s interface { %s(%s)", interfaceName, methodOutputName(method), strings.Join(parameterTypes, ", "))
	if result != "" {
		fmt.Fprintf(&out, " %s", result)
	}
	out.WriteString(" }\n")
	fmt.Fprintf(&out, "target, ok := receiver.(%s); if !ok { return nil, __gppInvocationError(%q) }\n", interfaceName, "receiver does not implement "+methodOutputName(method))
	fmt.Fprintf(&out, "if len(args) != %d { return nil, __gppInvocationError(%q) }\n", len(parameters), fmt.Sprintf("%s expects %d arguments", methodOutputName(method), len(parameters)))
	for index, parameter := range parameters {
		name := methodOutputNameForParameter(parameter.Name, index)
		fmt.Fprintf(&out, "%s, ok := args[%d].(%s); if !ok { return nil, __gppInvocationError(%q) }\n", name, index, transformPolymorphicType(parameter.typeText(), context), fmt.Sprintf("argument %d has the wrong type", index+1))
	}
	call := "target." + methodOutputName(method) + "(" + strings.Join(arguments, ", ") + ")"
	if result == "" {
		fmt.Fprintf(&out, "%s; return nil, nil\n", call)
	} else {
		resultTypes := methodResultTypes(method)
		if len(resultTypes) <= 1 {
			fmt.Fprintf(&out, "result := %s; return []any{result}, nil\n", call)
		} else {
			resultNames := make([]string, len(resultTypes))
			for index := range resultNames {
				resultNames[index] = fmt.Sprintf("result%d", index)
			}
			fmt.Fprintf(&out, "%s := %s; return []any{%s}, nil\n", strings.Join(resultNames, ", "), call, strings.Join(resultNames, ", "))
		}
	}
	out.WriteString("}")
	return out.String(), nil
}

func methodOutputNameForParameter(name string, index int) string {
	if name == "" || name == "_" {
		return fmt.Sprintf("arg%d", index)
	}
	return "arg" + strconv.Itoa(index)
}

func methodResultTypes(method Method) []TypeNode {
	if method.ResultAST == nil {
		return nil
	}
	if tuple, ok := method.ResultAST.(*TupleType); ok {
		return tuple.Elements
	}
	return []TypeNode{method.ResultAST}
}

func filterInaccessibleInheritedFields(class *ClassDecl, fields []constructorField) []constructorField {
	if class == nil {
		return fields
	}
	accessible := fields[:0]
	for _, field := range fields {
		if len(field.Path) > 0 && !memberVisibleToSubclass(field.Name) {
			continue
		}
		accessible = append(accessible, field)
	}
	return accessible
}

type methodDescriptor struct {
	Method Method
	Owner  string
}

func effectiveMethodDescriptors(class *ClassDecl, classes map[string]*ClassDecl, visiting map[*ClassDecl]bool) ([]methodDescriptor, error) {
	if visiting[class] {
		return nil, fmt.Errorf("inheritance cycle involving class %s", class.Name)
	}
	visiting[class] = true
	defer delete(visiting, class)

	result := []methodDescriptor{}
	seen := map[string]bool{}
	for _, method := range class.Methods {
		if method.IsStatic {
			continue
		}
		result = append(result, methodDescriptor{Method: method, Owner: class.Name})
		arity, err := parameterCount(methodParametersSource(method))
		if err != nil {
			return nil, err
		}
		seen[method.Name+fmt.Sprintf("/%d", arity)] = true
	}
	for _, parentName := range classParentNames(class) {
		parent, ok := classes[parentName]
		if !ok {
			return nil, fmt.Errorf("class %s has unresolved parent %s", class.Name, parentName)
		}
		inherited, err := effectiveMethodDescriptors(parent, classes, visiting)
		if err != nil {
			return nil, err
		}
		for _, descriptor := range inherited {
			arity, err := parameterCount(methodParametersSource(descriptor.Method))
			if err != nil {
				return nil, err
			}
			key := descriptor.Method.Name + fmt.Sprintf("/%d", arity)
			if seen[key] {
				continue
			}
			if strings.Contains(parentName, ".") && !strings.Contains(descriptor.Owner, ".") {
				descriptor.Owner = parentName[:strings.Index(parentName, ".")] + "." + descriptor.Owner
			}
			result = append(result, descriptor)
			seen[key] = true
		}
	}
	return result, nil
}

func effectiveStaticMethodDescriptors(class *ClassDecl, classes map[string]*ClassDecl, visiting map[*ClassDecl]bool) ([]methodDescriptor, error) {
	if visiting[class] {
		return nil, fmt.Errorf("inheritance cycle involving class %s", class.Name)
	}
	visiting[class] = true
	defer delete(visiting, class)

	result := []methodDescriptor{}
	localNames := map[string]bool{}
	for _, method := range class.Methods {
		if !method.IsStatic {
			continue
		}
		result = append(result, methodDescriptor{Method: method, Owner: class.Name})
		localNames[method.Name] = true
	}
	for _, parentName := range classParentNames(class) {
		parent, ok := classes[parentName]
		if !ok {
			continue
		}
		inherited, err := effectiveStaticMethodDescriptors(parent, classes, visiting)
		if err != nil {
			return nil, err
		}
		for _, descriptor := range inherited {
			if localNames[descriptor.Method.Name] {
				continue
			}
			if strings.Contains(parentName, ".") && !strings.Contains(descriptor.Owner, ".") {
				descriptor.Owner = parentName[:strings.Index(parentName, ".")] + "." + descriptor.Owner
			}
			result = append(result, descriptor)
			localNames[descriptor.Method.Name] = true
		}
	}
	return result, nil
}

func methodParametersLiteral(method Method, context constructorContext) string {
	parameters, err := parameterInfosForMethod(method)
	if err != nil || len(parameters) == 0 {
		return "nil"
	}
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		parts = append(parts, fmt.Sprintf("GppParameter{Name: %q, Type: &GppType{Name: %q}, Annotations: %s}", parameter.Name, parameter.typeText(), annotationUsesLiteral(method.ParameterAnnotations[parameter.Name], context)))
	}
	return "[]GppParameter{" + strings.Join(parts, ", ") + "}"
}

func methodResultLiteral(method Method) string {
	result := strings.TrimSpace(methodResultSource(method))
	if result == "" {
		return "nil"
	}
	return "&GppType{Name: " + strconv.Quote(result) + "}"
}

func classDescriptorReference(name string, classes map[string]*ClassDecl, context constructorContext) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if target, ok := context.Targets[name]; ok {
		if target.Qualifier == "" {
			return "Gpp" + target.Class.Name + "Class"
		}
		return target.Qualifier + ".Gpp" + target.Class.Name + "Class"
	}
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		return name[:dot] + ".Gpp" + name[dot+1:] + "Class"
	}
	if _, ok := classes[name]; ok {
		candidate := classes[name]
		for qualified, imported := range classes {
			if !strings.Contains(qualified, ".") || imported != candidate {
				continue
			}
			if target, ok := context.Targets[qualified]; ok {
				return target.Qualifier + ".Gpp" + target.Class.Name + "Class"
			}
		}
		return "Gpp" + name + "Class"
	}
	return ""
}

func isLocalClassName(classes map[string]*ClassDecl, name string) bool {
	class, ok := classes[name]
	if !ok {
		return false
	}
	for qualified, imported := range classes {
		if strings.Contains(qualified, ".") && imported == class {
			return false
		}
	}
	return true
}

func isNilableGoType(typeName string) bool {
	name := strings.TrimSpace(typeName)
	return strings.HasPrefix(name, "*") || strings.HasPrefix(name, "[]") ||
		strings.HasPrefix(name, "map[") || strings.HasPrefix(name, "chan ") ||
		strings.HasPrefix(name, "func(") || strings.HasPrefix(name, "interface{") ||
		name == "any"
}

type introspectionExprKind int

const (
	introspectionUnknown introspectionExprKind = iota
	introspectionClass
	introspectionFields
	introspectionField
	introspectionType
	introspectionMethods
	introspectionMethod
	introspectionParents
	introspectionParameters
	introspectionParameter
	introspectionAnnotations
	introspectionAnnotation
)

var introspectionSelectorNames = map[string]string{
	"name":        "Name",
	"fields":      "Fields",
	"owner":       "Owner",
	"type":        "Type",
	"get":         "Get",
	"set":         "Set",
	"addr":        "Addr",
	"annotations": "Annotations",
	"methods":     "Methods",
	"parents":     "Parents",
	"parameters":  "Parameters",
	"result":      "Result",
	"static":      "Static",
	"has":         "Has",
	"find":        "Find",
	"all":         "All",
	"fullName":    "FullName",
	"args":        "Args",
}

func transformIntrospection(src string, context constructorContext) (string, error) {
	if transformed, handled, err := transformIntrospectionAST(src, context); handled {
		return transformed, err
	}
	return src, nil
}

func introspectionDescriptorName(target constructorTarget) string {
	if target.Qualifier == "" {
		return descriptorName("", target.Class.Name)
	}
	return descriptorName(target.Qualifier, target.Class.Name)
}
