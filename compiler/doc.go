package compiler

// The documentation model deliberately describes Go++ declarations instead
// of generated Go declarations. It is small enough for the CLI to render but
// structured enough for future IDE and static-site consumers to reuse.

import (
	"bytes"
	"fmt"
	"go/ast"
	gofmt "go/format"
	gotoken "go/token"
	"sort"
	"strings"
	"unicode"
)

type DocKind string

const (
	DocPackage    DocKind = "package"
	DocClass      DocKind = "class"
	DocEnum       DocKind = "enum"
	DocFunction   DocKind = "function"
	DocMethod     DocKind = "method"
	DocField      DocKind = "field"
	DocExtension  DocKind = "extension"
	DocAnnotation DocKind = "annotation"
	DocTemplate   DocKind = "template"
	DocValue      DocKind = "value"
)

type DocFieldInfo struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Doc         string   `json:"doc,omitempty"`
	Annotations []string `json:"annotations,omitempty"`
}

type DocValueInfo struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Doc   string `json:"doc,omitempty"`
}

type DocSymbol struct {
	Kind        DocKind        `json:"kind"`
	Name        string         `json:"name"`
	FullName    string         `json:"fullName"`
	Package     string         `json:"package"`
	Parent      string         `json:"parent,omitempty"`
	Target      string         `json:"target,omitempty"`
	Doc         string         `json:"doc,omitempty"`
	Signature   string         `json:"signature,omitempty"`
	Source      string         `json:"source,omitempty"`
	SourceFile  string         `json:"sourceFile,omitempty"`
	SourceLine  int            `json:"sourceLine,omitempty"`
	Exported    bool           `json:"exported"`
	Generated   bool           `json:"generated,omitempty"`
	Parents     []string       `json:"parents,omitempty"`
	Fields      []DocFieldInfo `json:"fields,omitempty"`
	Methods     []DocSymbol    `json:"methods,omitempty"`
	Static      []DocSymbol    `json:"staticMethods,omitempty"`
	Values      []DocValueInfo `json:"values,omitempty"`
	Annotations []string       `json:"annotations,omitempty"`
}

type DocPackageInfo struct {
	Name    string      `json:"name"`
	Path    string      `json:"path"`
	Doc     string      `json:"doc,omitempty"`
	Symbols []DocSymbol `json:"symbols"`
}

type DocIndex struct {
	Packages map[string]*DocPackageInfo
	Symbols  map[string][]*DocSymbol
}

func BuildDocIndex(files []*File) *DocIndex {
	index := &DocIndex{Packages: map[string]*DocPackageInfo{}, Symbols: map[string][]*DocSymbol{}}
	for _, file := range files {
		if file == nil {
			continue
		}
		pkg := index.Packages[file.Package]
		if pkg == nil {
			pkg = &DocPackageInfo{Name: file.Package, Path: documentationPackagePath(file.Package)}
			index.Packages[file.Package] = pkg
		}
		if pkg.Doc == "" && file.Doc != "" {
			pkg.Doc = file.Doc
		}
		for _, declaration := range file.Decls {
			for _, symbol := range documentationSymbols(file.Package, declaration) {
				pkg.Symbols = append(pkg.Symbols, symbol)
				index.add(symbol)
			}
		}
	}
	for _, pkg := range index.Packages {
		sort.SliceStable(pkg.Symbols, func(left, right int) bool {
			return pkg.Symbols[left].Name < pkg.Symbols[right].Name
		})
	}
	return index
}

func (index *DocIndex) add(symbol DocSymbol) {
	copy := symbol
	keys := []string{symbol.FullName, symbol.Name}
	if symbol.Package != "" {
		keys = append(keys, documentationPackagePath(symbol.Package)+"."+symbol.Name)
	}
	if symbol.Parent != "" {
		keys = append(keys, symbol.Parent+"."+symbol.Name)
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		index.Symbols[key] = append(index.Symbols[key], &copy)
	}
	for _, method := range append(append([]DocSymbol{}, symbol.Methods...), symbol.Static...) {
		index.add(method)
	}
	for _, field := range symbol.Fields {
		index.add(DocSymbol{
			Kind:        DocField,
			Name:        field.Name,
			FullName:    symbol.FullName + "." + field.Name,
			Package:     symbol.Package,
			Parent:      symbol.Name,
			Doc:         field.Doc,
			Signature:   field.Name + " " + field.Type,
			Exported:    exportedDocName(field.Name),
			SourceFile:  symbol.SourceFile,
			SourceLine:  symbol.SourceLine,
			Annotations: field.Annotations,
		})
	}
	for _, value := range symbol.Values {
		index.add(DocSymbol{
			Kind:       DocValue,
			Name:       value.Name,
			FullName:   symbol.FullName + "." + value.Name,
			Package:    symbol.Package,
			Parent:     symbol.Name,
			Doc:        value.Doc,
			Signature:  symbol.Name + "." + value.Name + " = " + value.Value,
			Exported:   exportedDocName(value.Name),
			SourceFile: symbol.SourceFile,
			SourceLine: symbol.SourceLine,
		})
	}
}

func (index *DocIndex) Find(query string, includePrivate bool) []*DocSymbol {
	var result []*DocSymbol
	seen := map[string]bool{}
	for _, symbol := range index.Symbols[query] {
		if !includePrivate && !symbol.Exported {
			continue
		}
		key := symbol.FullName + "\x00" + string(symbol.Kind) + "\x00" + symbol.Signature
		if !seen[key] {
			seen[key] = true
			result = append(result, symbol)
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		return result[left].FullName+result[left].Signature < result[right].FullName+result[right].Signature
	})
	return result
}

func (index *DocIndex) Search(term string, includePrivate bool) []*DocSymbol {
	term = strings.ToLower(term)
	result := []*DocSymbol{}
	seen := map[string]bool{}
	for _, symbols := range index.Symbols {
		for _, symbol := range symbols {
			if !includePrivate && !symbol.Exported {
				continue
			}
			if !strings.Contains(strings.ToLower(symbol.FullName), term) {
				continue
			}
			key := symbol.FullName + "\x00" + string(symbol.Kind) + "\x00" + symbol.Signature
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, symbol)
		}
	}
	sort.SliceStable(result, func(left, right int) bool { return result[left].FullName < result[right].FullName })
	return result
}

func documentationSymbols(packageName string, declaration Decl) []DocSymbol {
	qualified := func(name string) string { return packageName + "." + name }
	switch value := declaration.(type) {
	case *ClassDecl:
		symbol := DocSymbol{
			Kind: DocClass, Name: value.Name, FullName: qualified(value.Name), Package: packageName,
			Doc: value.Doc, Source: classSource(value), SourceFile: value.SourceFile, SourceLine: value.SourceLine,
			Exported: exportedDocName(value.Name), Parents: classParentNames(value),
			Annotations: formatAnnotationUses(value.Annotations),
		}
		for _, field := range value.Fields {
			symbol.Fields = append(symbol.Fields, DocFieldInfo{Name: field.Name, Type: fieldTypeSource(field), Doc: field.Doc, Annotations: formatAnnotationUses(field.Annotations)})
		}
		for _, method := range value.Methods {
			member := documentationMethod(packageName, value.Name, method, value.SourceFile, value.SourceLine)
			if method.IsStatic {
				symbol.Static = append(symbol.Static, member)
			} else {
				symbol.Methods = append(symbol.Methods, member)
			}
		}
		return []DocSymbol{symbol}
	case *EnumDecl:
		symbol := DocSymbol{
			Kind: DocEnum, Name: value.Name, FullName: qualified(value.Name), Package: packageName,
			Doc: value.Doc, Signature: "enum " + value.Name + " " + enumBackingType(value),
			Source: "enum " + value.Name + " " + enumBackingType(value), SourceFile: value.SourceFile,
			SourceLine: value.SourceLine, Exported: exportedDocName(value.Name),
		}
		for _, member := range value.Members {
			symbol.Values = append(symbol.Values, DocValueInfo{Name: member.Name, Value: enumMemberValueSource(member), Doc: member.Doc})
		}
		return []DocSymbol{symbol}
	case *ExtendDecl:
		result := []DocSymbol{}
		for _, target := range extensionTargetNames(value) {
			for _, method := range value.Methods {
				fullName := packageName + "." + target + "." + method.Name
				result = append(result, DocSymbol{
					Kind: DocExtension, Name: method.Name, FullName: fullName, Package: packageName,
					Target: target, Parent: target, Doc: method.Doc,
					Signature: extensionSignature(target, method), Source: extensionSignature(target, method),
					SourceFile: value.SourceFile, SourceLine: value.SourceLine,
					Exported: exportedDocName(method.Name), Annotations: formatAnnotationUses(method.Annotations),
				})
			}
		}
		return result
	case *AnnotationDecl:
		targets := make([]string, len(value.Targets))
		for index, target := range value.Targets {
			targets[index] = string(target)
		}
		return []DocSymbol{{
			Kind: DocAnnotation, Name: value.Name, FullName: qualified(value.Name), Package: packageName,
			Doc: value.Doc, Signature: "annotation " + value.Name + "(" + annotationParameterSource(value) + ") on " + strings.Join(targets, ", "),
			Source: "annotation " + value.Name + "(" + annotationParameterSource(value) + ")", SourceFile: value.SourceFile,
			SourceLine: value.SourceLine, Exported: value.Exported,
		}}
	case *TemplateDecl:
		signature := "template " + value.Name + "(" + templateParametersSource(value) + ")"
		if value.Layout != "" {
			signature += " : " + value.Layout
		}
		return []DocSymbol{{
			Kind: DocTemplate, Name: value.Name, FullName: qualified(value.Name), Package: packageName,
			Doc: value.Doc, Signature: signature, Source: signature, SourceFile: value.SourceFile,
			SourceLine: value.SourceLine, Exported: exportedDocName(value.Name), Annotations: formatAnnotationUses(value.Annotations),
		}}
	case *MixedDecl:
		return documentationMixedFunctions(packageName, value)
	case *FunctionDecl:
		return []DocSymbol{{
			Kind: DocFunction, Name: value.Name, FullName: packageName + "." + value.Name,
			Package: packageName, Parent: "", Doc: value.Doc,
			Signature: functionDeclDocumentationSignature(value), Source: functionDeclDocumentationSignature(value),
			SourceFile: value.SourceFile, SourceLine: value.SourceLine,
			Exported: exportedDocName(value.Name), Annotations: formatAnnotationUses(value.Annotations),
		}}
	default:
		return nil
	}
}

func functionDeclDocumentationSignature(function *FunctionDecl) string {
	if function == nil {
		return ""
	}
	method := function.Method
	return "func " + function.Name + methodTypeParamsSource(method) + "(" + methodParametersSource(method) + ")" + resultSuffix(methodResultSource(method))
}

func documentationMethod(packageName, parent string, method Method, sourceFile string, sourceLine int) DocSymbol {
	kind := DocMethod
	fullName := packageName + "." + parent + "." + method.Name
	if method.IsStatic {
		fullName = packageName + "." + parent + "." + method.Name
	}
	signature := methodSignature(parent, method)
	return DocSymbol{Kind: kind, Name: method.Name, FullName: fullName, Package: packageName, Parent: parent,
		Doc: method.Doc, Signature: signature, Source: signature, SourceFile: sourceFile, SourceLine: sourceLine,
		Exported: exportedDocName(method.Name), Generated: method.Generated, Annotations: formatAnnotationUses(method.Annotations)}
}

func documentationMixedFunctions(packageName string, raw *MixedDecl) []DocSymbol {
	if len(raw.Functions) > 0 {
		result := make([]DocSymbol, 0, len(raw.Functions))
		for _, function := range raw.Functions {
			signature := functionDeclDocumentationSignature(function)
			result = append(result, DocSymbol{
				Kind: DocFunction, Name: function.Name, FullName: packageName + "." + function.Name,
				Package: packageName, Doc: function.Doc, Signature: signature, Source: signature,
				SourceFile: function.SourceFile, SourceLine: function.SourceLine,
				Exported: exportedDocName(function.Name), Annotations: formatAnnotationUses(function.Annotations),
			})
		}
		return result
	}
	if len(raw.GoASTDecls) > 0 {
		result := []DocSymbol{}
		for _, declaration := range raw.GoASTDecls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			doc := ""
			if function.Doc != nil {
				doc = strings.TrimSpace(function.Doc.Text())
			}
			line := raw.SourceLine
			if raw.GoASTFileSet != nil {
				line += raw.GoASTFileSet.Position(function.Pos()).Line - 2
			}
			name := function.Name.Name
			signature := goFunctionSignature(function)
			result = append(result, DocSymbol{Kind: DocFunction, Name: name, FullName: packageName + "." + name,
				Package: packageName, Doc: doc, Signature: signature, Source: signature,
				SourceFile: raw.SourceFile, SourceLine: line, Exported: exportedDocName(name)})
		}
		return result
	}
	// Parsed mixed declarations without a Go AST are represented by
	// MixedDecl.Functions. If neither structured metadata nor a Go declaration
	// exists, there is no executable function node for documentation to emit;
	// do not reparse opaque source text here.
	return nil
}

func goFunctionSignature(function *ast.FuncDecl) string {
	var buffer bytes.Buffer
	if err := gofmt.Node(&buffer, gotoken.NewFileSet(), function.Type); err != nil {
		return "func " + function.Name.Name
	}
	return "func " + function.Name.Name + strings.TrimPrefix(buffer.String(), "func")
}

func classSource(class *ClassDecl) string {
	result := "class " + class.Name
	parents := classParentNames(class)
	if len(parents) > 0 {
		result += " : " + strings.Join(parents, ", ")
	}
	return result
}

func methodSignature(parent string, method Method) string {
	prefix := "func "
	if method.IsStatic {
		prefix = "static func " + parent + "."
	}
	return prefix + method.Name + methodTypeParamsSource(method) + "(" + methodParametersSource(method) + ")" + resultSuffix(methodResultSource(method))
}

func extensionSignature(target string, method Method) string {
	return "extension func " + target + "." + method.Name + methodTypeParamsSource(method) + "(" + methodParametersSource(method) + ")" + resultSuffix(methodResultSource(method))
}

func resultSuffix(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return ""
	}
	return " " + result
}

func formatAnnotationUses(uses []AnnotationUse) []string {
	result := []string{}
	for _, use := range uses {
		value := "@" + use.Name
		if use.HasArguments {
			value += "(" + strings.Join(annotationArgumentTexts(use), ", ") + ")"
		}
		result = append(result, value)
	}
	return result
}

func documentationPackagePath(packageName string) string {
	return strings.ReplaceAll(packageName, ".", "/")
}

func exportedDocName(name string) bool {
	for _, runeValue := range name {
		return unicode.IsUpper(runeValue)
	}
	return false
}

func documentationPackageSymbol(index *DocIndex, packageName string) *DocSymbol {
	pkg := index.Packages[packageName]
	if pkg == nil {
		return nil
	}
	return &DocSymbol{Kind: DocPackage, Name: pkg.Name, FullName: pkg.Path, Package: packageName, Doc: pkg.Doc, Exported: true}
}

func FormatDocPackage(index *DocIndex, packageName string, includePrivate bool) string {
	pkg := index.Packages[packageName]
	if pkg == nil {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "package %s\n", pkg.Path)
	if pkg.Doc != "" {
		out.WriteString("\n" + pkg.Doc + "\n")
	}
	groups := map[DocKind][]string{}
	seenNames := map[DocKind]map[string]bool{}
	for _, symbol := range pkg.Symbols {
		if !includePrivate && !symbol.Exported {
			continue
		}
		if seenNames[symbol.Kind] == nil {
			seenNames[symbol.Kind] = map[string]bool{}
		}
		if seenNames[symbol.Kind][symbol.Name] {
			continue
		}
		seenNames[symbol.Kind][symbol.Name] = true
		groups[symbol.Kind] = append(groups[symbol.Kind], symbol.Name)
	}
	for _, group := range []struct {
		kind  DocKind
		title string
	}{{DocClass, "Classes"}, {DocEnum, "Enums"}, {DocFunction, "Functions"}, {DocExtension, "Extensions"}, {DocAnnotation, "Annotations"}, {DocTemplate, "Templates"}} {
		if len(groups[group.kind]) == 0 {
			continue
		}
		sort.Strings(groups[group.kind])
		fmt.Fprintf(&out, "\n%s:\n", group.title)
		for _, name := range groups[group.kind] {
			fmt.Fprintf(&out, "    %s\n", name)
		}
	}
	return strings.TrimRight(out.String(), "\n")
}
