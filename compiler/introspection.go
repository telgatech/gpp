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
	return `type GppType struct {
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
	Name string
	Static bool
	Annotations GppAnnotations
}

type GppClass struct {
	Name        string
	Fields      []GppField
	Methods     []GppMethod
	Annotations GppAnnotations
}

`
}

func introspectionRuntimeAliases() string {
	return `type GppType = gppRuntime.GppType
type GppAnnotationType = gppRuntime.GppAnnotationType
type GppAnnotation = gppRuntime.GppAnnotation
type GppAnnotations = gppRuntime.GppAnnotations
type GppField = gppRuntime.GppField
type GppMethod = gppRuntime.GppMethod
type GppClass = gppRuntime.GppClass

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

	fmt.Fprintf(out, "var Gpp%sClass = &GppClass{Name: %q, Annotations: %s, Methods: []GppMethod{\n", class.Name, class.Name, annotationUsesLiteral(class.Annotations, context))
	methods, err := interfaceMethods(class, classes, map[string]bool{})
	if err != nil {
		return err
	}
	for _, method := range methods {
		fmt.Fprintf(out, "\t{Name: %q, Annotations: %s},\n", method.Name, annotationUsesLiteral(method.Annotations, context))
	}
	staticMethods, err := staticMethodsForClass(class, classes, map[string]bool{})
	if err != nil {
		return err
	}
	for _, method := range staticMethods {
		fmt.Fprintf(out, "\t{Name: %q, Static: true, Annotations: %s},\n", method.Name, annotationUsesLiteral(method.Annotations, context))
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
		if !isLocalClassName(classes, owner) {
			continue
		}
		fmt.Fprintf(out, "\tGpp%sClass.Fields[%d].Owner = Gpp%sClass\n", class.Name, index, owner)
	}
	out.WriteString("}\n\n")
	return nil
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
			}
			return true
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		kind := introspectionExpressionKind(selector.X, metadataTypes)
		if replacement, ok := introspectionSelectorNames[selector.Sel.Name]; ok {
			valid := (kind == introspectionClass && (selector.Sel.Name == "name" || selector.Sel.Name == "fields" || selector.Sel.Name == "annotations" || selector.Sel.Name == "methods")) ||
				(kind == introspectionField && (selector.Sel.Name == "name" || selector.Sel.Name == "owner" || selector.Sel.Name == "type" || selector.Sel.Name == "get" || selector.Sel.Name == "set" || selector.Sel.Name == "addr" || selector.Sel.Name == "annotations")) ||
				(kind == introspectionMethod && (selector.Sel.Name == "name" || selector.Sel.Name == "annotations" || selector.Sel.Name == "static")) ||
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
		case introspectionAnnotations:
			return introspectionAnnotation
		}
	case *ast.IndexListExpr:
		switch introspectionExpressionKind(value.X, variables) {
		case introspectionFields:
			return introspectionField
		case introspectionMethods:
			return introspectionMethod
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
		case "annotations", "Annotations":
			if base == introspectionClass || base == introspectionField || base == introspectionMethod {
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
			if base == introspectionField {
				return introspectionType
			}
		case "owner", "Owner":
			if base == introspectionField {
				return introspectionClass
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
