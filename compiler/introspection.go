package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
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
			for _, method := range value.Methods {
				if strings.Contains(method.Body, ".class") {
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
	return `type GoppType struct {
	Name string
}

type GoppField struct {
	Name  string
	Owner *GoppClass
	Type  *GoppType
	Get   func(any) any
	Set   func(any, any)
	Addr  func(any) any
}

type GoppClass struct {
	Name   string
	Fields []GoppField
}

`
}

func emitClassDescriptor(out *strings.Builder, class *ClassDecl, classes map[string]*ClassDecl, context constructorContext) error {
	fields, err := constructorFields(class, classes, nil, map[string]bool{})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "var Gopp%sClass = &GoppClass{Name: %q, Fields: []GoppField{\n", class.Name, class.Name)
	for _, field := range fields {
		path := append(append([]string(nil), field.Path...), field.Name)
		access := strings.Join(path, ".")
		owner := field.Owner
		if owner == "" {
			owner = class.Name
		}

		fmt.Fprintf(out, "\t{Name: %q, Owner: nil, Type: &GoppType{Name: %q},\n", field.Name, field.Type)
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
		fmt.Fprintf(out, "\tGopp%sClass.Fields[%d].Owner = Gopp%sClass\n", class.Name, index, owner)
	}
	out.WriteString("}\n\n")
	return nil
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
)

var introspectionSelectorNames = map[string]string{
	"name":   "Name",
	"fields": "Fields",
	"owner":  "Owner",
	"type":   "Type",
	"get":    "Get",
	"set":    "Set",
	"addr":   "Addr",
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
			!strings.Contains(src, ".owner") {
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
		addEdit(selector, receiver+".GoppRuntimeClass()")
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

	functionPrefix := prefix + "func __gopp_scope()"
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
	if strings.HasPrefix(typeName, "__gopp_") {
		name := strings.TrimPrefix(typeName, "__gopp_")
		if _, ok := context.Targets[name]; ok {
			return name
		}
	}
	for key, target := range context.Targets {
		if strings.HasSuffix(typeName, ".Gopp"+target.Class.Name) ||
			strings.HasSuffix(typeName, ".__gopp_"+target.Class.Name) ||
			(typeName == "Gopp"+target.Class.Name && key == target.Class.Name) {
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
			}
			return true
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		kind := introspectionExpressionKind(selector.X, metadataTypes)
		if replacement, ok := introspectionSelectorNames[selector.Sel.Name]; ok {
			valid := (kind == introspectionClass && (selector.Sel.Name == "name" || selector.Sel.Name == "fields")) ||
				(kind == introspectionField && (selector.Sel.Name == "name" || selector.Sel.Name == "owner" || selector.Sel.Name == "type" || selector.Sel.Name == "get" || selector.Sel.Name == "set" || selector.Sel.Name == "addr")) ||
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
		return src, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, change := range edits {
		src = src[:change.start] + change.text + src[change.end:]
	}
	return src, nil
}

func introspectionExpressionKind(expr ast.Expr, variables map[string]int) introspectionExprKind {
	switch value := expr.(type) {
	case *ast.Ident:
		if kind, ok := variables[value.Name]; ok {
			return introspectionExprKind(kind)
		}
		if strings.HasPrefix(value.Name, "Gopp") && strings.HasSuffix(value.Name, "Class") {
			return introspectionClass
		}
	case *ast.ParenExpr:
		return introspectionExpressionKind(value.X, variables)
	case *ast.CallExpr:
		if selector, ok := value.Fun.(*ast.SelectorExpr); ok &&
			(selector.Sel.Name == "__gopp_class" || selector.Sel.Name == "GoppRuntimeClass") {
			return introspectionClass
		}
	case *ast.IndexExpr:
		if introspectionExpressionKind(value.X, variables) == introspectionFields {
			return introspectionField
		}
	case *ast.SelectorExpr:
		if strings.HasPrefix(value.Sel.Name, "Gopp") && strings.HasSuffix(value.Sel.Name, "Class") {
			return introspectionClass
		}
		base := introspectionExpressionKind(value.X, variables)
		switch value.Sel.Name {
		case "fields", "Fields":
			if base == introspectionClass {
				return introspectionFields
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
	}
	return introspectionUnknown
}
