package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
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
		case *RawDecl:
			if strings.Contains(value.Code, ".class") {
				return true
			}
		case *ClassDecl:
			if len(value.Annotations) > 0 {
				return true
			}
			for _, field := range value.Fields {
				if len(field.Annotations) > 0 {
					return true
				}
			}
			for _, method := range value.Methods {
				if len(method.Annotations) > 0 || len(method.ParameterAnnotations) > 0 || strings.Contains(method.Body, ".class") {
					return true
				}
			}
		case *ExtendDecl:
			for _, method := range value.Methods {
				if len(method.Annotations) > 0 || len(method.ParameterAnnotations) > 0 {
					return true
				}
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

func (annotations GppAnnotations) Get(annotation any) *GppAnnotation {
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
}

type GppParameter struct {
	Name        string
	Type        *GppType
	Annotations GppAnnotations
}

type GppClass struct {
	Name        string
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
type GppParameter = gppRuntime.GppParameter
type GppClass = gppRuntime.GppClass

func __gppFormatObject(class *GppClass, object any, pretty bool) string {
	return gppRuntime.FormatObject(class, object, pretty)
}

`
}

func ensureIntrospectionRuntimeImport(body, importPath string) string {
	if importPath == "" {
		return body
	}
	return "import gppRuntime " + strconv.Quote(importPath) + "\n\n" + body
}

func emitClassDescriptor(out *strings.Builder, class *ClassDecl, classes map[string]*ClassDecl, context constructorContext) error {
	fields, err := constructorFields(class, classes, nil, map[string]bool{})
	if err != nil {
		return err
	}

	parentReferences := make([]string, 0, len(class.Parents))
	for _, parent := range class.Parents {
		if reference := classDescriptorReference(parent, classes, context); reference != "" {
			parentReferences = append(parentReferences, reference)
		}
	}
	parents := "nil"
	if len(parentReferences) > 0 {
		parents = "[]*GppClass{" + strings.Join(parentReferences, ", ") + "}"
	}
	fmt.Fprintf(out, "var Gpp%sClass = &GppClass{Name: %q, Parents: %s, Annotations: %s, Methods: []GppMethod{\n", class.Name, class.Name, parents, annotationUsesLiteral(class.Annotations, context))
	methods, err := effectiveMethodDescriptors(class, classes, map[string]bool{})
	if err != nil {
		return err
	}
	for _, descriptor := range methods {
		fmt.Fprintf(out, "\t{Name: %q, Parameters: %s, Result: %s, Annotations: %s},\n", descriptor.Method.Name, methodParametersLiteral(descriptor.Method, context), methodResultLiteral(descriptor.Method), annotationUsesLiteral(descriptor.Method.Annotations, context))
	}
	staticMethods, err := effectiveStaticMethodDescriptors(class, classes, map[string]bool{})
	if err != nil {
		return err
	}
	for _, descriptor := range staticMethods {
		fmt.Fprintf(out, "\t{Name: %q, Parameters: %s, Result: %s, Static: true, Annotations: %s},\n", descriptor.Method.Name, methodParametersLiteral(descriptor.Method, context), methodResultLiteral(descriptor.Method), annotationUsesLiteral(descriptor.Method.Annotations, context))
	}
	out.WriteString("}, Fields: []GppField{\n")
	for _, field := range fields {
		path := append(append([]string(nil), field.Path...), field.Name)
		access := strings.Join(path, ".")
		owner := field.Owner
		if owner == "" {
			owner = class.Name
		}

		fmt.Fprintf(out, "\t{Name: %q, Owner: nil, Type: &GppType{Name: %q}, Annotations: %s,\n", field.Name, field.Type, annotationUsesLiteral(field.Annotations, context))
		fmt.Fprintf(out, "\t\tGet: func(root any) any {\n")
		fmt.Fprintf(out, "\t\t\tswitch value := root.(type) {\n")
		fmt.Fprintf(out, "\t\t\tcase *%s:\n\t\t\t\treturn value.%s\n", class.Name, access)
		fmt.Fprintf(out, "\t\t\tcase %s:\n\t\t\t\treturn value.%s\n", class.Name, access)
		fmt.Fprintf(out, "\t\t\tdefault:\n\t\t\t\treturn nil\n\t\t\t}\n\t\t},\n")

		fmt.Fprintf(out, "\t\tSet: func(root any, input any) {\n")
		fmt.Fprintf(out, "\t\t\tvalue, ok := root.(*%s)\n\t\t\tif !ok { return }\n", class.Name)
		if isNilableGoType(field.Type) {
			fmt.Fprintf(out, "\t\t\tif input == nil { value.%s = nil; return }\n", access)
		}
		runtimeType := transformPolymorphicType(field.Type, context)
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

type methodDescriptor struct {
	Method Method
	Owner  string
}

func effectiveMethodDescriptors(class *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) ([]methodDescriptor, error) {
	if visiting[class.Name] {
		return nil, fmt.Errorf("inheritance cycle involving class %s", class.Name)
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	result := []methodDescriptor{}
	seen := map[string]bool{}
	for _, method := range class.Methods {
		if method.IsStatic {
			continue
		}
		result = append(result, methodDescriptor{Method: method, Owner: class.Name})
		arity, err := parameterCount(method.Parameters)
		if err != nil {
			return nil, err
		}
		seen[method.Name+fmt.Sprintf("/%d", arity)] = true
	}
	for _, parentName := range class.Parents {
		parent, ok := classes[parentName]
		if !ok {
			return nil, fmt.Errorf("class %s has unresolved parent %s", class.Name, parentName)
		}
		inherited, err := effectiveMethodDescriptors(parent, classes, visiting)
		if err != nil {
			return nil, err
		}
		for _, descriptor := range inherited {
			arity, err := parameterCount(descriptor.Method.Parameters)
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

func effectiveStaticMethodDescriptors(class *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) ([]methodDescriptor, error) {
	if visiting[class.Name] {
		return nil, fmt.Errorf("inheritance cycle involving class %s", class.Name)
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	result := []methodDescriptor{}
	localNames := map[string]bool{}
	for _, method := range class.Methods {
		if !method.IsStatic {
			continue
		}
		result = append(result, methodDescriptor{Method: method, Owner: class.Name})
		localNames[method.Name] = true
	}
	for _, parentName := range class.Parents {
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
	parameters, err := parseParameterInfos(method.Parameters)
	if err != nil || len(parameters) == 0 {
		return "nil"
	}
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		parts = append(parts, fmt.Sprintf("GppParameter{Name: %q, Type: &GppType{Name: %q}, Annotations: %s}", parameter.Name, parameter.Type, annotationUsesLiteral(method.ParameterAnnotations[parameter.Name], context)))
	}
	return "[]GppParameter{" + strings.Join(parts, ", ") + "}"
}

func methodResultLiteral(method Method) string {
	result := strings.TrimSpace(method.Result)
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
	"all":         "All",
	"fullName":    "FullName",
	"args":        "Args",
}

func transformIntrospection(src string, context constructorContext) (string, error) {
	src = rewriteIntrospectionTypeSelectors(src)
	if context.Introspection == nil || !strings.Contains(src, ".class") {
		// Metadata selectors may still follow a class descriptor introduced in a
		// previous transform, so only skip when there is no metadata-looking
		// syntax at all.
		if !strings.Contains(src, ".fields") && !strings.Contains(src, ".name") &&
			!strings.Contains(src, ".type") && !strings.Contains(src, ".get") &&
			!strings.Contains(src, ".set") && !strings.Contains(src, ".addr") &&
			!strings.Contains(src, ".owner") && !strings.Contains(src, ".annotations") &&
			!strings.Contains(src, ".methods") && !strings.Contains(src, ".has") &&
			!strings.Contains(src, ".parents") && !strings.Contains(src, ".parameters") &&
			!strings.Contains(src, ".result") &&
			!strings.Contains(src, ".get") && !strings.Contains(src, ".all") &&
			!strings.Contains(src, ".fullName") && !strings.Contains(src, ".args") {
			return src, nil
		}
	}

	parsed, fileSet, prefixLength, err := parseIntrospectionSource(src, context)
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
	invalidClassAccess := ""
	addEdit := func(node ast.Node, text string) {
		start := fileSet.Position(node.Pos()).Offset - prefixLength
		end := fileSet.Position(node.End()).Offset - prefixLength
		if start >= 0 && end <= len(src) && start <= end {
			edits = append(edits, edit{start: start, end: end, text: text})
		}
	}

	ast.Inspect(parsed, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "class" {
			return true
		}

		receiver, receiverErr := formatNode(selector.X)
		if receiverErr != nil {
			return true
		}
		if target, ok := introspectionStaticTarget(receiver, context); ok {
			addEdit(selector, introspectionDescriptorName(target))
			return true
		}

		className := introspectionClassName(selector.X, context, valueTypes)
		if className == "" {
			invalidClassAccess = receiver
			return true
		}
		addEdit(selector, receiver+".GppRuntimeClass()")
		return true
	})

	if invalidClassAccess != "" {
		return "", fmt.Errorf("%s.class is only available on Go++ class instances or class names", invalidClassAccess)
	}
	if len(edits) > 0 {
		sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
		for _, change := range edits {
			src = src[:change.start] + change.text + src[change.end:]
		}
	}

	return transformIntrospectionMetadataSelectors(src, context)
}

func rewriteIntrospectionTypeSelectors(src string) string {
	var output strings.Builder
	for index := 0; index < len(src); {
		if end, ok, _ := copyIgnoredSource(src, index, &output); ok {
			index = end
			continue
		}
		if src[index] != '.' {
			output.WriteByte(src[index])
			index++
			continue
		}
		nameStart := skipSpace(src, index+1)
		if !keywordAt(src, nameStart, "type") {
			output.WriteByte(src[index])
			index++
			continue
		}
		nameEnd := nameStart + len("type")
		if nameEnd < len(src) && isIdentPart(src[nameEnd]) {
			output.WriteByte(src[index])
			index++
			continue
		}
		output.WriteString(src[index:nameStart])
		output.WriteString("Type")
		index = nameEnd
	}
	return output.String()
}

func parseIntrospectionSource(src string, context constructorContext) (ast.Node, *token.FileSet, int, error) {
	const prefix = "package main\n\n"
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "", prefix+src, 0)
	if err == nil {
		return parsed, fileSet, len(prefix), nil
	}

	functionPrefix := prefix + "func __gpp_scope()"
	if strings.TrimSpace(context.CurrentResult) != "" {
		functionPrefix += " " + strings.TrimSpace(context.CurrentResult)
	}
	functionPrefix += " {\n"
	functionSet := token.NewFileSet()
	parsed, err = parser.ParseFile(functionSet, "", functionPrefix+src+"\n}\n", 0)
	if err != nil {
		return nil, nil, 0, err
	}
	return parsed, functionSet, len(functionPrefix), nil
}

func introspectionStaticTarget(receiver string, context constructorContext) (constructorTarget, bool) {
	target, ok := context.Targets[receiver]
	if ok {
		return target, true
	}
	return constructorTarget{}, false
}

func introspectionDescriptorName(target constructorTarget) string {
	if target.Qualifier == "" {
		return descriptorName("", target.Class.Name)
	}
	return descriptorName(target.Qualifier, target.Class.Name)
}

func introspectionClassName(expr ast.Expr, context constructorContext, valueTypes map[string]string) string {
	if ident, ok := expr.(*ast.Ident); ok && ident.Name == "this" && context.CurrentClass != "" {
		return context.CurrentClass
	}
	typeName := expressionStaticType(expr, context, valueTypes)
	typeName = strings.TrimPrefix(strings.TrimSpace(typeName), "*")
	if target, ok := context.Targets[typeName]; ok {
		return target.Class.Name
	}
	if strings.HasPrefix(typeName, "__gpp_") {
		name := strings.TrimPrefix(typeName, "__gpp_")
		if _, ok := context.Targets[name]; ok {
			return name
		}
	}
	for key, target := range context.Targets {
		if strings.HasSuffix(typeName, ".Gpp"+target.Class.Name) ||
			strings.HasSuffix(typeName, ".__gpp_"+target.Class.Name) ||
			(typeName == "Gpp"+target.Class.Name && key == target.Class.Name) {
			return target.Class.Name
		}
	}
	return ""
}

func transformIntrospectionMetadataSelectors(src string, context constructorContext) (string, error) {
	parsed, fileSet, prefixLength, err := parseIntrospectionSource(src, context)
	if err != nil {
		return src, nil
	}
	valueTypes := polymorphicValueTypes(parsed, context)
	if context.CurrentClass != "" {
		valueTypes["this"] = "*" + context.CurrentClass
	}
	metadataTypes := map[string]int{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.RangeStmt:
			if key, ok := statement.Key.(*ast.Ident); ok && key.Name != "_" &&
				introspectionExpressionKind(statement.X, metadataTypes) == introspectionFields {
				metadataTypes[key.Name] = int(introspectionField)
			} else if key, ok := statement.Key.(*ast.Ident); ok && key.Name != "_" &&
				introspectionExpressionKind(statement.X, metadataTypes) == introspectionMethods {
				metadataTypes[key.Name] = int(introspectionMethod)
			} else if key, ok := statement.Key.(*ast.Ident); ok && key.Name != "_" &&
				introspectionExpressionKind(statement.X, metadataTypes) == introspectionParents {
				metadataTypes[key.Name] = int(introspectionClass)
			} else if key, ok := statement.Key.(*ast.Ident); ok && key.Name != "_" &&
				introspectionExpressionKind(statement.X, metadataTypes) == introspectionParameters {
				metadataTypes[key.Name] = int(introspectionParameter)
			}
		case *ast.ValueSpec:
			for index, name := range statement.Names {
				if index < len(statement.Values) {
					if kind := introspectionExpressionKind(statement.Values[index], metadataTypes); kind != introspectionUnknown {
						metadataTypes[name.Name] = int(kind)
					}
				}
			}
		case *ast.AssignStmt:
			for index, left := range statement.Lhs {
				name, ok := left.(*ast.Ident)
				if ok && index < len(statement.Rhs) {
					if kind := introspectionExpressionKind(statement.Rhs[index], metadataTypes); kind != introspectionUnknown {
						metadataTypes[name.Name] = int(kind)
					}
				}
			}
		}
		return true
	})

	type edit struct {
		start, end int
		text       string
	}
	edits := []edit{}
	addEdit := func(node ast.Node, text string) {
		start := fileSet.Position(node.Pos()).Offset - prefixLength
		end := fileSet.Position(node.End()).Offset - prefixLength
		if start >= 0 && end <= len(src) && start <= end {
			edits = append(edits, edit{start: start, end: end, text: text})
		}
	}
	addPointer := func(expr ast.Expr) {
		start := fileSet.Position(expr.Pos()).Offset - prefixLength
		if start >= 0 && start <= len(src) {
			edits = append(edits, edit{start: start, end: start, text: "&"})
		}
	}

	ast.Inspect(parsed, func(node ast.Node) bool {
		if statement, ok := node.(*ast.RangeStmt); ok {
			if key, keyOK := statement.Key.(*ast.Ident); keyOK && key.Name != "_" &&
				introspectionExpressionKind(statement.X, metadataTypes) == introspectionFields {
				start := fileSet.Position(key.Pos()).Offset - prefixLength
				if start >= 0 && start <= len(src) {
					edits = append(edits, edit{start: start, end: start, text: "_, "})
				}
			} else if key, keyOK := statement.Key.(*ast.Ident); keyOK && key.Name != "_" &&
				introspectionExpressionKind(statement.X, metadataTypes) == introspectionMethods {
				start := fileSet.Position(key.Pos()).Offset - prefixLength
				if start >= 0 && start <= len(src) {
					edits = append(edits, edit{start: start, end: start, text: "_, "})
				}
			} else if key, keyOK := statement.Key.(*ast.Ident); keyOK && key.Name != "_" &&
				introspectionExpressionKind(statement.X, metadataTypes) == introspectionParents {
				start := fileSet.Position(key.Pos()).Offset - prefixLength
				if start >= 0 && start <= len(src) {
					edits = append(edits, edit{start: start, end: start, text: "_, "})
				}
			} else if key, keyOK := statement.Key.(*ast.Ident); keyOK && key.Name != "_" &&
				introspectionExpressionKind(statement.X, metadataTypes) == introspectionParameters {
				start := fileSet.Position(key.Pos()).Offset - prefixLength
				if start >= 0 && start <= len(src) {
					edits = append(edits, edit{start: start, end: start, text: "_, "})
				}
			}
			return true
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		kind := introspectionExpressionKind(selector.X, metadataTypes)
		if replacement, ok := introspectionSelectorNames[selector.Sel.Name]; ok {
			valid := (kind == introspectionClass && (selector.Sel.Name == "name" || selector.Sel.Name == "fields" || selector.Sel.Name == "annotations" || selector.Sel.Name == "methods" || selector.Sel.Name == "parents")) ||
				(kind == introspectionField && (selector.Sel.Name == "name" || selector.Sel.Name == "owner" || selector.Sel.Name == "type" || selector.Sel.Name == "get" || selector.Sel.Name == "set" || selector.Sel.Name == "addr" || selector.Sel.Name == "annotations")) ||
				(kind == introspectionMethod && (selector.Sel.Name == "name" || selector.Sel.Name == "owner" || selector.Sel.Name == "parameters" || selector.Sel.Name == "result" || selector.Sel.Name == "annotations" || selector.Sel.Name == "static")) ||
				(kind == introspectionParameter && (selector.Sel.Name == "name" || selector.Sel.Name == "type" || selector.Sel.Name == "annotations")) ||
				(kind == introspectionAnnotations && (selector.Sel.Name == "has" || selector.Sel.Name == "get" || selector.Sel.Name == "all")) ||
				(kind == introspectionAnnotation && (selector.Sel.Name == "name" || selector.Sel.Name == "fullName" || selector.Sel.Name == "args")) ||
				(kind == introspectionType && selector.Sel.Name == "name")
			if valid {
				addEdit(selector.Sel, replacement)
			}
		}
		return true
	})

	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		function, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || introspectionExpressionKind(function.X, metadataTypes) != introspectionField {
			return true
		}
		if function.Sel.Name != "set" && function.Sel.Name != "addr" {
			return true
		}
		actual := expressionStaticType(call.Args[0], context, valueTypes)
		if actual != "" && !strings.HasPrefix(strings.TrimSpace(actual), "*") && introspectionClassName(call.Args[0], context, valueTypes) != "" {
			addPointer(call.Args[0])
		}
		return true
	})

	if len(edits) == 0 {
		return transformAnnotationDescriptorArguments(src, context)
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, change := range edits {
		src = src[:change.start] + change.text + src[change.end:]
	}
	return transformAnnotationDescriptorArguments(src, context)
}

func transformAnnotationDescriptorArguments(src string, context constructorContext) (string, error) {
	if len(context.Annotations) == 0 {
		return src, nil
	}
	parsed, fileSet, prefixLength, err := parseIntrospectionSource(src, context)
	if err != nil {
		return src, nil
	}
	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "Has" && selector.Sel.Name != "Get" && selector.Sel.Name != "All") {
			return true
		}
		name, err := formatNode(call.Args[0])
		if err != nil {
			return true
		}
		declaration := context.Annotations[name]
		if declaration == nil {
			return true
		}
		start := fileSet.Position(call.Args[0].Pos()).Offset - prefixLength
		end := fileSet.Position(call.Args[0].End()).Offset - prefixLength
		if start >= 0 && end <= len(src) {
			edits = append(edits, edit{
				start: start,
				end:   end,
				text:  annotationDescriptorReference(AnnotationUse{Name: name}, declaration),
			})
		}
		return true
	})
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, nil
}

func introspectionExpressionKind(expr ast.Expr, variables map[string]int) introspectionExprKind {
	switch value := expr.(type) {
	case *ast.Ident:
		if kind, ok := variables[value.Name]; ok {
			return introspectionExprKind(kind)
		}
		if strings.HasPrefix(value.Name, "Gpp") && strings.HasSuffix(value.Name, "Class") {
			return introspectionClass
		}
	case *ast.ParenExpr:
		return introspectionExpressionKind(value.X, variables)
	case *ast.CallExpr:
		if selector, ok := value.Fun.(*ast.SelectorExpr); ok &&
			(selector.Sel.Name == "__gpp_class" || selector.Sel.Name == "GppRuntimeClass") {
			return introspectionClass
		}
		if selector, ok := value.Fun.(*ast.SelectorExpr); ok {
			switch selector.Sel.Name {
			case "Get", "get":
				if introspectionExpressionKind(selector.X, variables) == introspectionAnnotations {
					return introspectionAnnotation
				}
			case "All", "all":
				if introspectionExpressionKind(selector.X, variables) == introspectionAnnotations {
					return introspectionAnnotations
				}
			}
		}
	case *ast.IndexExpr:
		switch introspectionExpressionKind(value.X, variables) {
		case introspectionFields:
			return introspectionField
		case introspectionMethods:
			return introspectionMethod
		case introspectionParameters:
			return introspectionParameter
		case introspectionAnnotations:
			return introspectionAnnotation
		}
	case *ast.IndexListExpr:
		switch introspectionExpressionKind(value.X, variables) {
		case introspectionFields:
			return introspectionField
		case introspectionMethods:
			return introspectionMethod
		case introspectionParameters:
			return introspectionParameter
		case introspectionAnnotations:
			return introspectionAnnotation
		}
	case *ast.SelectorExpr:
		if strings.HasPrefix(value.Sel.Name, "Gpp") && strings.HasSuffix(value.Sel.Name, "Class") {
			return introspectionClass
		}
		base := introspectionExpressionKind(value.X, variables)
		switch value.Sel.Name {
		case "fields", "Fields":
			if base == introspectionClass {
				return introspectionFields
			}
		case "methods", "Methods":
			if base == introspectionClass {
				return introspectionMethods
			}
		case "parents", "Parents":
			if base == introspectionClass {
				return introspectionParents
			}
		case "parameters", "Parameters":
			if base == introspectionMethod {
				return introspectionParameters
			}
		case "annotations", "Annotations":
			if base == introspectionClass || base == introspectionField || base == introspectionMethod || base == introspectionParameter {
				return introspectionAnnotations
			}
		case "has", "Has", "get", "Get", "all", "All":
			if base == introspectionAnnotations {
				if value.Sel.Name == "get" || value.Sel.Name == "Get" {
					return introspectionAnnotation
				}
				return introspectionAnnotations
			}
		case "type", "Type":
			if base == introspectionField || base == introspectionParameter {
				return introspectionType
			}
		case "owner", "Owner":
			if base == introspectionField {
				return introspectionClass
			}
			if base == introspectionMethod {
				return introspectionClass
			}
		case "result", "Result":
			if base == introspectionMethod {
				return introspectionType
			}
		case "name", "Name":
			if base == introspectionParameter {
				return introspectionParameter
			}
		}
		if base == introspectionAnnotation {
			switch value.Sel.Name {
			case "name", "Name", "fullName", "FullName", "args", "Args":
				return introspectionAnnotation
			}
		}
	}
	return introspectionUnknown
}
