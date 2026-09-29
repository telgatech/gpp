package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"strconv"
	"strings"
)

type SemanticModel struct {
	Packages map[string]*PackageSymbols
}

type PackageSymbols struct {
	Name            string
	Classes         map[string]*ClassDecl
	Enums           map[string]*EnumDecl
	Embeds          map[string]EmbedEntry
	Templates       map[string]*TemplateDecl
	Imports         map[string]string
	ImportedClasses map[string]*ClassDecl
	ImportedEnums   map[string]*EnumDecl
	Types           map[string]bool
	Values          map[string]bool
	Extensions      []*ExtendDecl
	Annotations     map[string]*AnnotationDecl
	Functions       map[string]bool
}

func classLocation(class *ClassDecl) string {
	if class.SourceLine > 0 {
		return fmt.Sprintf("%s:%d", class.SourceFile, class.SourceLine)
	}
	return class.SourceFile
}

func rawTypeNamesAST(declaration ast.Decl) map[string]bool {
	result := map[string]bool{}
	group, ok := declaration.(*ast.GenDecl)
	if !ok || group.Tok != token.TYPE {
		return result
	}
	for _, spec := range group.Specs {
		if typeSpec, ok := spec.(*ast.TypeSpec); ok {
			result[typeSpec.Name.Name] = true
		}
	}
	return result
}

func ResolveProgram(program *Program) (*SemanticModel, error) {
	model := &SemanticModel{
		Packages: map[string]*PackageSymbols{},
	}

	for _, file := range program.Files {
		pkg, ok := model.Packages[file.Package]
		if !ok {
			pkg = &PackageSymbols{
				Name:            file.Package,
				Classes:         map[string]*ClassDecl{},
				Enums:           map[string]*EnumDecl{},
				Embeds:          map[string]EmbedEntry{},
				Templates:       map[string]*TemplateDecl{},
				Imports:         map[string]string{},
				ImportedClasses: map[string]*ClassDecl{},
				ImportedEnums:   map[string]*EnumDecl{},
				Types:           map[string]bool{},
				Values:          map[string]bool{},
				Extensions:      []*ExtendDecl{},
				Annotations:     map[string]*AnnotationDecl{},
				Functions:       map[string]bool{},
			}
			model.Packages[file.Package] = pkg
		}

		for _, decl := range file.Decls {
			if annotation, ok := decl.(*AnnotationDecl); ok {
				annotation.Package = file.Package
				if previous, exists := pkg.Annotations[annotation.Name]; exists {
					return nil, fmt.Errorf(
						"%s: duplicate annotation %s in package %s; first declared in %s",
						annotationLocation(annotation), annotation.Name, file.Package, annotationLocation(previous),
					)
				}
				pkg.Annotations[annotation.Name] = annotation
				continue
			}
			if raw, ok := decl.(*MixedDecl); ok {
				tokens := raw.Tokens
				types := rawTypeNamesTokens(tokens)
				if len(raw.GoASTDecls) > 0 {
					types = map[string]bool{}
					for _, declaration := range raw.GoASTDecls {
						for name := range rawTypeNamesAST(declaration) {
							types[name] = true
						}
					}
				}
				for name := range types {
					pkg.Types[name] = true
				}
				values := rawPackageNamesTokens(tokens)
				if len(raw.GoASTDecls) > 0 {
					values = map[string]bool{}
					for _, declaration := range raw.GoASTDecls {
						for name := range rawPackageNamesAST(declaration) {
							values[name] = true
						}
					}
				}
				for name := range values {
					pkg.Values[name] = true
				}
			}
			if goDecl, ok := decl.(*GoDecl); ok {
				for _, declaration := range goDecl.Declarations {
					for name := range rawTypeNamesAST(declaration) {
						pkg.Types[name] = true
					}
					for name := range rawPackageNamesAST(declaration) {
						pkg.Values[name] = true
					}
				}
			}
			if valueDecl, ok := decl.(*ValueDecl); ok {
				for _, name := range valueDecl.Names {
					pkg.Values[name.Text] = true
				}
			}
			if embed, ok := decl.(*EmbedDecl); ok {
				for _, entry := range embed.Entries {
					if previous, exists := pkg.Embeds[entry.Name]; exists {
						return nil, fmt.Errorf(
							"%s: duplicate embed symbol %s in package %s; first declared in %s",
							embedLocation(embed), entry.Name, file.Package, embedEntryLocation(previous),
						)
					}
					pkg.Embeds[entry.Name] = entry
				}
				continue
			}
			if template, ok := decl.(*TemplateDecl); ok {
				if previous, exists := pkg.Templates[template.Name]; exists {
					return nil, fmt.Errorf(
						"%s: duplicate template %s in package %s; first declared in %s:%d",
						templateLocation(template), template.Name, file.Package, previous.SourceFile, previous.SourceLine,
					)
				}
				pkg.Templates[template.Name] = template
				continue
			}
			if extension, ok := decl.(*ExtendDecl); ok {
				pkg.Extensions = append(pkg.Extensions, extension)
				continue
			}
			if enum, ok := decl.(*EnumDecl); ok {
				if _, exists := pkg.Enums[enum.Name]; exists || pkg.Classes[enum.Name] != nil {
					return nil, fmt.Errorf(
						"%s: duplicate type %s in package %s",
						classLocation(&ClassDecl{SourceFile: enum.SourceFile, SourceLine: enum.SourceLine}),
						enum.Name,
						file.Package,
					)
				}
				pkg.Enums[enum.Name] = enum
				pkg.Types[enum.Name] = true
				continue
			}
			class, ok := decl.(*ClassDecl)
			if !ok {
				continue
			}

			if _, exists := pkg.Enums[class.Name]; exists {
				return nil, fmt.Errorf(
					"%s: duplicate type %s in package %s",
					classLocation(class),
					class.Name,
					file.Package,
				)
			}

			if previous, exists := pkg.Classes[class.Name]; exists {
				return nil, fmt.Errorf(
					"%s: duplicate class %s in package %s; first declared in %s",
					classLocation(class),
					class.Name,
					file.Package,
					classLocation(previous),
				)
			}

			pkg.Classes[class.Name] = class
		}
		if imports, err := goImports(file); err == nil {
			for _, spec := range imports {
				importPath, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					continue
				}
				alias := path.Base(importPath)
				if spec.Name != nil {
					alias = spec.Name.Name
				}
				if alias != "_" && alias != "." {
					pkg.Imports[alias] = importPath
				}
			}
		}
		for _, file := range program.Files {
			if file.Package != pkg.Name {
				continue
			}
			for _, decl := range file.Decls {
				if function, ok := decl.(*FunctionDecl); ok {
					pkg.Functions[function.Name] = true
					continue
				}
				if raw, ok := decl.(*MixedDecl); ok {
					tokens := raw.Tokens
					functions := []string{}
					if len(raw.Functions) > 0 {
						functions = make([]string, 0, len(raw.Functions))
						for _, function := range raw.Functions {
							functions = append(functions, function.Name)
						}
					} else {
						if len(raw.GoASTDecls) > 0 {
							for _, declaration := range raw.GoASTDecls {
								functions = append(functions, rawFunctionNamesAST(declaration)...)
							}
						} else {
							functions = rawFunctionNamesTokens(tokens)
						}
					}
					for _, signature := range functions {
						pkg.Functions[signature] = true
					}
				}
			}
		}
	}
	for _, pkg := range model.Packages {
		for alias, importPath := range pkg.Imports {
			logical, ok := officialLogicalPackage(importPath)
			if !ok {
				continue
			}
			imported := model.Packages[logical]
			if imported == nil {
				continue
			}
			for className, class := range imported.Classes {
				pkg.ImportedClasses[alias+"."+className] = class
			}
			for enumName, enum := range imported.Enums {
				pkg.ImportedEnums[alias+"."+enumName] = enum
			}
		}
	}

	for _, pkg := range model.Packages {
		for name, template := range pkg.Templates {
			_, embedExists := pkg.Embeds[name]
			if pkg.Classes[name] != nil || pkg.Enums[name] != nil || pkg.Types[name] || pkg.Values[name] || embedExists {
				return nil, fmt.Errorf("%s: duplicate template %s in package %s", templateLocation(template), name, pkg.Name)
			}
			if _, imported := pkg.Imports[name]; imported {
				return nil, fmt.Errorf("%s: template %s conflicts with import in package %s", templateLocation(template), name, pkg.Name)
			}
			if template.Layout != "" {
				layout, exists := pkg.Templates[template.Layout]
				if !exists {
					return nil, fmt.Errorf("%s: layout template %s does not exist in package %s", templateLocation(template), template.Layout, pkg.Name)
				}
				if layout.Name == template.Name {
					return nil, fmt.Errorf("%s: template cannot use itself as a layout", templateLocation(template))
				}
				if len(layout.ParameterAST) > 0 {
					return nil, fmt.Errorf("%s: layout template %s must not declare parameters", templateLocation(template), template.Layout)
				}
			}
		}
		for name, entry := range pkg.Embeds {
			_, templateExists := pkg.Templates[name]
			if pkg.Classes[name] != nil || pkg.Enums[name] != nil || pkg.Types[name] || pkg.Values[name] || templateExists {
				return nil, fmt.Errorf("%s: duplicate embed symbol %s in package %s", embedEntryLocation(entry), name, pkg.Name)
			}
			if _, imported := pkg.Imports[name]; imported {
				return nil, fmt.Errorf("%s: embed symbol %s conflicts with import in package %s", embedEntryLocation(entry), name, pkg.Name)
			}
		}
		for _, class := range pkg.Classes {
			if err := validateClass(pkg, class); err != nil {
				return nil, err
			}
		}

		if err := validateInheritanceCycles(pkg); err != nil {
			return nil, err
		}

		for _, class := range pkg.Classes {
			if err := validateAmbiguousMemberAccess(pkg, class); err != nil {
				return nil, err
			}
		}

		if err := validateExtensions(pkg); err != nil {
			return nil, err
		}
		if err := validateAnnotationDeclarations(pkg); err != nil {
			return nil, err
		}
		for _, file := range program.Files {
			if file.Package != pkg.Name {
				continue
			}
			if err := validateFileAnnotations(file, pkg, pkg.Annotations, true); err != nil {
				return nil, err
			}
		}
	}
	if err := validateHTTPDocumentation(program, model); err != nil {
		return nil, err
	}

	return model, nil
}

func embedLocation(embed *EmbedDecl) string {
	if embed.SourceLine > 0 {
		return fmt.Sprintf("%s:%d", embed.SourceFile, embed.SourceLine)
	}
	return embed.SourceFile
}

func embedEntryLocation(entry EmbedEntry) string {
	if entry.SourceLine > 0 {
		return fmt.Sprintf("%s:%d", entry.SourceFile, entry.SourceLine)
	}
	if entry.SourceFile != "" {
		return entry.SourceFile
	}
	return "embed"
}

func templateLocation(template *TemplateDecl) string {
	if template.SourceLine > 0 {
		return fmt.Sprintf("%s:%d", template.SourceFile, template.SourceLine)
	}
	return template.SourceFile
}

func annotationLocation(annotation *AnnotationDecl) string {
	if annotation.SourceLine > 0 {
		return fmt.Sprintf("%s:%d", annotation.SourceFile, annotation.SourceLine)
	}
	return annotation.SourceFile
}

func rawFunctionNamesAST(declaration ast.Decl) []string {
	function, ok := declaration.(*ast.FuncDecl)
	if !ok || function.Recv != nil {
		return nil
	}
	return []string{function.Name.Name}
}

func rawPackageNamesAST(declaration ast.Decl) map[string]bool {
	result := map[string]bool{}
	switch declaration := declaration.(type) {
	case *ast.FuncDecl:
		if declaration.Recv == nil {
			result[declaration.Name.Name] = true
		}
	case *ast.GenDecl:
		for _, spec := range declaration.Specs {
			switch spec := spec.(type) {
			case *ast.TypeSpec:
				result[spec.Name.Name] = true
			case *ast.ValueSpec:
				for _, name := range spec.Names {
					result[name.Name] = true
				}
			}
		}
	}
	return result
}

func rawFunctionNamesTokens(tokens []Token) []string {
	result := []string{}
	braceDepth := 0
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token.Kind == TokenComment || token.Kind == TokenNewline || token.Kind == TokenEOF {
			continue
		}
		if token.Text == "{" {
			braceDepth++
			continue
		}
		if token.Text == "}" {
			if braceDepth > 0 {
				braceDepth--
			}
			continue
		}
		if braceDepth != 0 || token.Text != "func" {
			continue
		}
		index = nextSemanticToken(tokens, index+1)
		if index >= len(tokens) || tokens[index].Text == "(" {
			// A leading parenthesized token is a method receiver, not a
			// package-level function declaration.
			continue
		}
		if tokens[index].Kind == TokenIdentifier || tokens[index].Kind == TokenKeyword {
			result = append(result, tokens[index].Text)
		}
	}
	return result
}

// rawTypeNamesTokens discovers type declarations without reparsing a mixed
// Go++ declaration as Go. The token stream is lossless and already scoped to
// this MixedDecl, so a top-level type name is unambiguous even when a nearby
// function has Go++ syntax such as defaults or lambdas.
func rawTypeNamesTokens(tokens []Token) map[string]bool {
	result := map[string]bool{}
	braceDepth := 0
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token.Kind == TokenComment || token.Kind == TokenNewline || token.Kind == TokenEOF {
			continue
		}
		if token.Text == "{" {
			braceDepth++
			continue
		}
		if token.Text == "}" {
			if braceDepth > 0 {
				braceDepth--
			}
			continue
		}
		if braceDepth != 0 || token.Text != "type" {
			continue
		}
		index++
		for index < len(tokens) && (tokens[index].Kind == TokenComment || tokens[index].Kind == TokenNewline) {
			index++
		}
		if index < len(tokens) && tokens[index].Text == "(" {
			index++
			for index < len(tokens) {
				if tokens[index].Text == ")" {
					break
				}
				if tokens[index].Kind == TokenIdentifier || tokens[index].Kind == TokenKeyword {
					result[tokens[index].Text] = true
					for index < len(tokens) && tokens[index].Text != "\n" && tokens[index].Text != ";" {
						index++
					}
				}
				index++
			}
			continue
		}
		if index < len(tokens) && (tokens[index].Kind == TokenIdentifier || tokens[index].Kind == TokenKeyword) {
			result[tokens[index].Text] = true
		}
	}
	return result
}

// rawPackageNamesTokens provides the small amount of top-level symbol
// discovery needed by semantic resolution for mixed declarations. It does not
// interpret executable expressions; it only records declaration names.
func rawPackageNamesTokens(tokens []Token) map[string]bool {
	result := map[string]bool{}
	braceDepth := 0
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token.Kind == TokenComment || token.Kind == TokenEOF {
			continue
		}
		if token.Text == "{" {
			braceDepth++
			continue
		}
		if token.Text == "}" {
			if braceDepth > 0 {
				braceDepth--
			}
			continue
		}
		if braceDepth != 0 {
			continue
		}
		switch token.Text {
		case "func":
			index = nextSemanticToken(tokens, index+1)
			if index < len(tokens) && tokens[index].Text == "(" {
				index = skipSemanticBalanced(tokens, index, "(", ")")
				index = nextSemanticToken(tokens, index+1)
			}
			if index < len(tokens) && (tokens[index].Kind == TokenIdentifier || tokens[index].Kind == TokenKeyword) {
				result[tokens[index].Text] = true
			}
		case "type":
			index = nextSemanticToken(tokens, index+1)
			if index < len(tokens) && (tokens[index].Kind == TokenIdentifier || tokens[index].Kind == TokenKeyword) {
				result[tokens[index].Text] = true
			}
		case "var", "const":
			index = nextSemanticToken(tokens, index+1)
			if index >= len(tokens) {
				continue
			}
			if tokens[index].Text == "(" {
				end := skipSemanticBalanced(tokens, index, "(", ")")
				for cursor := index + 1; cursor < end; cursor++ {
					if cursor == index+1 || tokens[cursor-1].Kind == TokenNewline || tokens[cursor-1].Text == ";" {
						cursor = nextSemanticToken(tokens, cursor)
						if cursor < end && (tokens[cursor].Kind == TokenIdentifier || tokens[cursor].Kind == TokenKeyword) {
							result[tokens[cursor].Text] = true
						}
					}
				}
				index = end
				continue
			}
			if tokens[index].Kind == TokenIdentifier || tokens[index].Kind == TokenKeyword {
				result[tokens[index].Text] = true
			}
		}
	}
	return result
}

func nextSemanticToken(tokens []Token, index int) int {
	for index < len(tokens) && tokens[index].Kind == TokenComment {
		index++
	}
	return index
}

func skipSemanticBalanced(tokens []Token, open int, opening, closing string) int {
	depth := 0
	for index := open; index < len(tokens); index++ {
		switch tokens[index].Text {
		case opening:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return len(tokens)
}

func validateExtensions(pkg *PackageSymbols) error {
	seen := map[string]bool{}
	for _, extension := range pkg.Extensions {
		targetNames := extensionTargetNames(extension)
		if len(targetNames) == 0 {
			return fmt.Errorf("%s: extension target cannot be empty", extension.SourceFile)
		}
		targets := map[string]bool{}
		for _, rawTarget := range targetNames {
			target := strings.TrimSpace(rawTarget)
			if target == "" {
				return fmt.Errorf("%s: extension target cannot be empty", extension.SourceFile)
			}
			normalized := normalizeExtensionTarget(target)
			if targets[normalized] {
				return fmt.Errorf("%s: duplicate extension target %s", extension.SourceFile, target)
			}
			targets[normalized] = true
		}
		for _, method := range extension.Methods {
			parameters, err := parameterInfosForMethod(method)
			if err != nil {
				return fmt.Errorf("%s: extension %s.%s has invalid parameters: %w", extension.SourceFile, strings.Join(targetNames, ", "), method.Name, err)
			}
			for _, rawTarget := range targetNames {
				target := strings.TrimSpace(rawTarget)
				key := normalizeExtensionTarget(target) + "/" + method.Name + "/" + parameterSignatureKey(parameters)
				if seen[key] {
					return fmt.Errorf("%s: extension method %s for %s is declared more than once with parameter types %s", extension.SourceFile, method.Name, target, parameterSignatureKey(parameters))
				}
				seen[key] = true
			}
		}
	}
	return nil
}

func normalizeExtensionTarget(target string) string {
	target = strings.TrimSpace(target)
	if tokens, err := LexSource("extension target", target); err == nil {
		if typeNode, err := ParseTypeTokens(tokens); err == nil && typeNode != nil {
			if formatted, err := typeNodeSource(typeNode); err == nil {
				return strings.TrimSpace(formatted)
			}
		}
	}
	return strings.Join(strings.Fields(target), " ")
}

func validateClass(pkg *PackageSymbols, class *ClassDecl) error {
	for _, reserved := range []string{"GppClass", "GppField", "GppType"} {
		if class.Name == reserved {
			return fmt.Errorf(
				"%s: class name %s is reserved for Go++ introspection metadata",
				classLocation(class),
				class.Name,
			)
		}
	}
	fields := map[string]bool{}
	for _, field := range class.Fields {
		if field.Name == "GppDynamicClass" || field.Name == "GppDynamicObject" {
			return fmt.Errorf(
				"%s: class %s field %s is reserved for Go++ runtime metadata",
				classLocation(class),
				class.Name,
				field.Name,
			)
		}
		if fields[field.Name] {
			return fmt.Errorf(
				"%s: class %s declares field %s more than once",
				classLocation(class),
				class.Name,
				field.Name,
			)
		}
		fields[field.Name] = true
	}

	type methodEntry struct {
		index     int
		arity     int
		typeKey   string
		parameter []parameterInfo
	}
	methods := map[string][]methodEntry{}
	for index, method := range class.Methods {
		if method.IsStatic && tokensContainIdentifier(methodBodyTokens(method), "this") {
			return fmt.Errorf(
				"%s: static method %s has no this",
				classLocation(class),
				method.Name,
			)
		}
		if method.Name == "GppRuntimeClass" {
			return fmt.Errorf(
				"%s: class %s method %s is reserved for Go++ runtime metadata",
				classLocation(class),
				class.Name,
				method.Name,
			)
		}
		if fields[method.Name] {
			return fmt.Errorf(
				"%s: class %s declares field and method %s with the same name",
				classLocation(class),
				class.Name,
				method.Name,
			)
		}
		arity, err := parameterCount(methodParametersSource(method))
		if err != nil {
			return fmt.Errorf(
				"%s: class %s method %s has invalid parameters: %w",
				classLocation(class),
				class.Name,
				method.Name,
				err,
			)
		}

		parameters, err := parameterInfosForMethod(method)
		if err != nil {
			return fmt.Errorf(
				"%s: class %s method %s has invalid parameters: %w",
				classLocation(class),
				class.Name,
				method.Name,
				err,
			)
		}
		typeKey := parameterSignatureKey(parameters)
		methodKey := method.Name
		if method.IsStatic {
			methodKey += "\x00static"
		}
		for _, previous := range methods[methodKey] {
			if previous.typeKey == typeKey {
				return fmt.Errorf(
					"%s: class %s declares method %s more than once with parameter types %s",
					classLocation(class),
					class.Name,
					method.Name,
					typeKey,
				)
			}
		}
		methods[methodKey] = append(methods[methodKey], methodEntry{
			index:     index,
			arity:     arity,
			typeKey:   typeKey,
			parameter: parameters,
		})
	}
	for _, classMethods := range methods {
		if len(classMethods) < 2 {
			continue
		}
		arityCounts := map[int]int{}
		for _, method := range classMethods {
			arityCounts[method.arity]++
		}
		for _, method := range classMethods {
			name := class.Methods[method.index].Name
			goName := overloadedName(name, method.arity)
			if arityCounts[method.arity] > 1 {
				goName = overloadedTypedName(name, method.arity, method.typeKey)
			}
			class.Methods[method.index].GoName = goName
		}
	}

	parents := map[string]bool{}
	for _, parentName := range classParentNames(class) {
		if parents[parentName] {
			return fmt.Errorf(
				"%s: class %s lists parent %s more than once",
				classLocation(class),
				class.Name,
				parentName,
			)
		}
		parents[parentName] = true

		if _, ok := pkg.Classes[parentName]; !ok {
			if _, imported := pkg.ImportedClasses[parentName]; imported {
				continue
			}
			return fmt.Errorf(
				"%s: class %s has unresolved parent %s in package %s",
				classLocation(class),
				class.Name,
				parentName,
				pkg.Name,
			)
		}
	}

	return nil
}

func sourceContainsIdentifier(source, wanted string) bool {
	for index := 0; index < len(source); {
		if end, ok, _ := copyIgnoredSource(source, index, &strings.Builder{}); ok {
			index = end
			continue
		}
		name, length := readIdent(source[index:])
		if length > 0 {
			if name == wanted {
				return true
			}
			index += length
			continue
		}
		index++
	}
	return false
}

func tokensContainIdentifier(tokens []Token, wanted string) bool {
	for _, token := range tokens {
		if (token.Kind == TokenIdentifier || token.Kind == TokenKeyword) && token.Text == wanted {
			return true
		}
	}
	return false
}

func parameterCount(params string) (int, error) {
	parameters, err := parseParameterInfos(params)
	if err != nil {
		return 0, err
	}
	return len(parameters), nil
}

func overloadedName(name string, arity int) string {
	return fmt.Sprintf("%s__gpp_%d", name, arity)
}

func overloadedTypedName(name string, arity int, typeKey string) string {
	return fmt.Sprintf("%s__gpp_%d_%s", name, arity, mangleTypeKey(typeKey))
}

func mangleTypeKey(typeKey string) string {
	result := strings.Builder{}
	for _, r := range typeKey {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			result.WriteRune(r)
			continue
		}
		result.WriteByte('_')
	}
	if result.Len() == 0 {
		return "value"
	}
	return result.String()
}

func validateInheritanceCycles(pkg *PackageSymbols) error {
	state := map[string]uint8{}
	stack := []string{}

	var visit func(string) error
	visit = func(className string) error {
		switch state[className] {
		case 1:
			start := 0
			for index, name := range stack {
				if name == className {
					start = index
					break
				}
			}

			cycle := append(append([]string(nil), stack[start:]...), className)
			return fmt.Errorf(
				"%s: inheritance cycle in package %s: %s",
				classLocation(pkg.Classes[className]),
				pkg.Name,
				joinNames(cycle),
			)
		case 2:
			return nil
		}

		state[className] = 1
		stack = append(stack, className)

		for _, parentName := range classParentNames(pkg.Classes[className]) {
			if _, local := pkg.Classes[parentName]; local {
				if err := visit(parentName); err != nil {
					return err
				}
			}
		}

		stack = stack[:len(stack)-1]
		state[className] = 2
		return nil
	}

	for className := range pkg.Classes {
		if err := visit(className); err != nil {
			return err
		}
	}

	return nil
}

func joinNames(names []string) string {
	result := ""
	for index, name := range names {
		if index > 0 {
			result += " -> "
		}
		result += name
	}
	return result
}

func constructorContextForFile(file *File, model *SemanticModel, modulePath string) (constructorContext, error) {
	localClasses := model.Packages[file.Package].Classes
	context := localConstructorContext(localClasses)
	context.Package = file.Package
	context.Annotations, _ = annotationScopeForFile(file, model, modulePath)
	context.Extensions = extensionMethodsForDeclarations(model.Packages[file.Package].Extensions, "")
	context.ImportedTypes = map[string]map[string]bool{}

	if modulePath == "" {
		return context, nil
	}

	imports, err := goImports(file)
	if err != nil {
		return context, nil
	}

	for _, spec := range imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}

		logicalPackage, ok := logicalPackageForImport(
			importPath,
			modulePath,
			model,
		)
		if !ok {
			continue
		}

		pkg := model.Packages[logicalPackage]
		alias := path.Base(importPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}

		if alias == "_" {
			continue
		}
		qualifier := alias
		if alias == "." {
			qualifier = ""
		}
		if alias != "_" {
			context.ImportedTypes[alias] = pkg.Types
		}

		for className, class := range pkg.Classes {
			key := alias + "." + className
			if alias == "." {
				key = className
			}

			context.Targets[key] = constructorTarget{
				Class:         class,
				Classes:       pkg.Classes,
				Qualifier:     qualifier,
				InterfaceName: "Gpp" + className,
			}
			methodKey := key
			if alias == "." {
				methodKey = className
			}
			context.ClassMethodSignatures[methodKey] = methodSignaturesForClass(
				class,
				pkg.Classes,
				map[string]bool{},
			)
			context.StaticMethodSignatures[methodKey] = staticMethodSignaturesForClass(
				class,
				pkg.Classes,
				map[string]bool{},
			)
			context.Overloads.ClassMethods[methodKey] = methodOverloadsForClass(
				class,
				pkg.Classes,
				map[string]bool{},
			)
			context.Overloads.ClassMethodTypes[methodKey] = methodOverloadTypesForClass(
				class,
				pkg.Classes,
				map[string]bool{},
			)
			addMethodOverload(&context.Overloads, class, key)
		}
		importedExtensions := extensionMethodsForDeclarations(pkg.Extensions, alias)
		if qualifier != "" {
			importedTypes := map[string]map[string]bool{qualifier: pkg.Types}
			for index := range importedExtensions {
				// These are copied into the importing package and then qualified;
				// detach source-backed spans before replacing their typed nodes.
				method := &importedExtensions[index].Method
				structured := len(method.ParameterAST) > 0 || method.ResultAST != nil
				parameters := ""
				result := ""
				if !structured {
					parameters = methodParametersSource(*method)
					result = methodResultSource(*method)
				}
				if structured {
					parameters := append([]ParameterNode(nil), method.ParameterAST...)
					for parameterIndex := range parameters {
						parameters[parameterIndex].Type = qualifyImportedTypeNode(parameters[parameterIndex].Type, qualifier+"._", pkg.Classes, importedTypes)
					}
					method.ParameterAST = parameters
					method.ResultAST = qualifyImportedTypeNode(method.ResultAST, qualifier+"._", pkg.Classes, importedTypes)
					resultFields := append([]ParameterNode(nil), method.ResultFieldsAST...)
					for resultIndex := range resultFields {
						resultFields[resultIndex].Type = qualifyImportedTypeNode(resultFields[resultIndex].Type, qualifier+"._", pkg.Classes, importedTypes)
					}
					method.ResultFieldsAST = resultFields
					typeParameters := append([]TypeParameterNode(nil), method.TypeParamsAST...)
					for typeParameterIndex := range typeParameters {
						typeParameters[typeParameterIndex].Constraint = qualifyImportedTypeNode(typeParameters[typeParameterIndex].Constraint, qualifier+"._", pkg.Classes, importedTypes)
					}
					method.TypeParamsAST = typeParameters
				}
				// Detach source-backed spans after replacing their typed nodes.
				importedExtensions[index].Method.Owner = nil
				importedExtensions[index].Method.ParametersSpan = Span{}
				importedExtensions[index].Method.ResultSpan = Span{}
				if !structured {
					qualifiedParameters := qualifyImportedTypeNames(parameters, qualifier+"._", pkg.Classes, importedTypes)
					qualifiedResult := qualifyImportedTypeNames(result, qualifier+"._", pkg.Classes, importedTypes)
					importedExtensions[index].Method.ParameterAST = parseParameterNodes(qualifiedParameters)
					importedExtensions[index].Method.ResultAST = parseTypeText(qualifiedResult)
				}
			}
			for index := range importedExtensions {
				baseTarget := strings.TrimPrefix(importedExtensions[index].Target, "*")
				if _, ok := pkg.Classes[baseTarget]; !ok {
					continue
				}
				importedExtensions[index].Target = qualifier + "." + importedExtensions[index].Target
				if strings.HasPrefix(importedExtensions[index].ReceiverType, "*") {
					importedExtensions[index].ReceiverType = "*" + qualifier + "." + strings.TrimPrefix(importedExtensions[index].ReceiverType, "*")
				} else {
					importedExtensions[index].ReceiverType = qualifier + "." + importedExtensions[index].ReceiverType
				}
			}
		}
		context.Extensions = append(context.Extensions, importedExtensions...)
	}

	for name, class := range localClasses {
		context.ClassMethodSignatures[name] = methodSignaturesForClass(
			class,
			classesForClass(context, class),
			map[string]bool{},
		)
		context.StaticMethodSignatures[name] = staticMethodSignaturesForClass(
			class,
			classesForClass(context, class),
			map[string]bool{},
		)
	}

	return context, nil
}

func addMethodOverload(overloads *overloadContext, class *ClassDecl, className string) {
	if overloads.ClassMethods == nil {
		overloads.ClassMethods = map[string]map[string]map[int]string{}
	}
	if overloads.ClassMethods[className] == nil {
		overloads.ClassMethods[className] = map[string]map[int]string{}
	}
	interfaceName := className
	if dot := strings.LastIndex(interfaceName, "."); dot >= 0 {
		interfaceName = interfaceName[:dot+1] + "Gpp" + interfaceName[dot+1:]
	} else {
		interfaceName = "Gpp" + interfaceName
	}
	if overloads.ClassMethods[interfaceName] == nil {
		overloads.ClassMethods[interfaceName] = map[string]map[int]string{}
	}

	for _, method := range class.Methods {
		if method.GoName == "" || method.IsStatic {
			continue
		}
		arity, err := parameterCount(methodParametersSource(method))
		if err != nil {
			continue
		}
		if overloads.Methods[method.Name] == nil {
			overloads.Methods[method.Name] = map[int]string{}
		}
		overloads.Methods[method.Name][arity] = method.GoName
		if overloads.ClassMethods[className][method.Name] == nil {
			overloads.ClassMethods[className][method.Name] = map[int]string{}
		}
		overloads.ClassMethods[className][method.Name][arity] = method.GoName
		if overloads.ClassMethods[interfaceName][method.Name] == nil {
			overloads.ClassMethods[interfaceName][method.Name] = map[int]string{}
		}
		overloads.ClassMethods[interfaceName][method.Name][arity] = method.GoName
	}
}

func addFunctionOverloads(context *constructorContext, file *File) error {
	signatures, err := functionSignaturesForFile(file)
	if err != nil {
		return err
	}
	context.FunctionSignatures = signatures
	return configureFunctionOverloads(&context.Overloads, signatures)
}

func astParameterCount(functionType *ast.FuncType) (int, error) {
	if functionType.Params == nil {
		return 0, nil
	}

	count := 0
	for _, field := range functionType.Params.List {
		if len(field.Names) == 0 {
			count++
		} else {
			count += len(field.Names)
		}
	}
	return count, nil
}

func goImports(file *File) ([]*ast.ImportSpec, error) {
	imports := []*ast.ImportSpec{}
	for _, decl := range file.Decls {
		if goDecl, ok := decl.(*GoDecl); ok {
			for _, declaration := range goDecl.Declarations {
				if group, ok := declaration.(*ast.GenDecl); ok && group.Tok == token.IMPORT {
					for _, spec := range group.Specs {
						if importSpec, ok := spec.(*ast.ImportSpec); ok {
							imports = append(imports, importSpec)
						}
					}
				}
			}
			continue
		}
		raw, ok := decl.(*MixedDecl)
		if !ok {
			continue
		}
		// Ordinary Go declarations, including imports, are represented by their
		// Go AST. Other compatibility declarations must not force us to
		// concatenate and reparse executable source just to discover imports.
		for _, declaration := range raw.GoASTDecls {
			if group, ok := declaration.(*ast.GenDecl); ok && group.Tok == token.IMPORT {
				for _, spec := range group.Specs {
					if importSpec, ok := spec.(*ast.ImportSpec); ok {
						imports = append(imports, importSpec)
					}
				}
			}
		}
		if len(raw.GoASTDecls) == 0 {
			tokenImports, err := importSpecsFromTokens(raw.Tokens)
			if err != nil {
				return nil, err
			}
			imports = append(imports, tokenImports...)
		}
	}
	return imports, nil
}

// importSpecsFromTokens extracts ordinary Go import declarations without
// reparsing a compatibility declaration's executable source. A MixedDecl can
// contain Go++ syntax (for example default parameters) that Go's parser must
// reject, while its import tokens remain fully unambiguous.
func importSpecsFromTokens(tokens []Token) ([]*ast.ImportSpec, error) {
	imports := []*ast.ImportSpec{}
	for index := 0; index < len(tokens); index++ {
		if tokens[index].Text != "import" {
			continue
		}
		index++
		index = nextSignificantToken(tokens, index)
		if index >= len(tokens) || tokens[index].Text != "(" {
			name, importPath, next, _, ok := sourceImportSpec(tokens, index)
			if !ok {
				return nil, fmt.Errorf("invalid import declaration")
			}
			imports = append(imports, newImportSpec(name, importPath))
			index = next - 1
			continue
		}

		index++
		for {
			index = nextSignificantToken(tokens, index)
			if index >= len(tokens) {
				return nil, fmt.Errorf("unterminated import group")
			}
			if tokens[index].Text == ")" {
				break
			}
			name, importPath, next, _, ok := sourceImportSpec(tokens, index)
			if !ok {
				return nil, fmt.Errorf("invalid import declaration")
			}
			imports = append(imports, newImportSpec(name, importPath))
			index = next
		}
	}
	return imports, nil
}

func importNodesFromTokens(tokens []Token) ([]ImportDecl, error) {
	imports := []ImportDecl{}
	for index := 0; index < len(tokens); index++ {
		if tokens[index].Text != "import" {
			continue
		}
		startToken := tokens[index]
		index = nextSignificantToken(tokens, index+1)
		if index >= len(tokens) {
			return nil, fmt.Errorf("import declaration is missing a path")
		}
		if tokens[index].Text != "(" {
			name, importPath, next, logical, ok := sourceImportSpec(tokens, index)
			if !ok {
				return nil, fmt.Errorf("invalid import declaration")
			}
			declaration, err := tokenImportNode(name, importPath, logical, startToken, tokens[next-1])
			if err != nil {
				return nil, err
			}
			imports = append(imports, declaration)
			index = next - 1
			continue
		}

		index++
		for {
			index = nextSignificantToken(tokens, index)
			if index >= len(tokens) {
				return nil, fmt.Errorf("unterminated import group")
			}
			if tokens[index].Text == ")" {
				break
			}
			name, importPath, next, logical, ok := sourceImportSpec(tokens, index)
			if !ok {
				return nil, fmt.Errorf("invalid import declaration")
			}
			declaration, err := tokenImportNode(name, importPath, logical, startToken, tokens[next-1])
			if err != nil {
				return nil, err
			}
			imports = append(imports, declaration)
			index = next
		}
	}
	return imports, nil
}

func tokenImportNode(name *ast.Ident, importPath string, logical bool, startToken, endToken Token) (ImportDecl, error) {
	if importPath == "" {
		return ImportDecl{}, fmt.Errorf("import path cannot be empty")
	}
	if strings.HasPrefix(importPath, "generated/") {
		return ImportDecl{}, fmt.Errorf("generated import paths are internal and cannot be imported directly; use the logical package name")
	}
	alias := ""
	if name != nil {
		alias = name.Name
	}
	return ImportDecl{Alias: alias, Path: importPath, LogicalPackage: logical, SpanValue: Span{
		Start:  startToken.Span.Start,
		End:    endToken.Span.End,
		Line:   startToken.Span.Line,
		Column: startToken.Span.Column,
	}}, nil
}

// sourceImportSpec accepts regular Go import specs and Go++ logical imports
// such as `import foo.bar`. Logical imports are normalized to a quoted path
// for semantic analysis and later lowered to the module's generated path.
func sourceImportSpec(tokens []Token, index int) (*ast.Ident, string, int, bool, bool) {
	name, pathIndex, next, ok := importTokenSpec(tokens, index)
	if ok {
		importPath, err := strconv.Unquote(tokens[pathIndex].Text)
		if err != nil {
			return nil, "", next, false, false
		}
		return name, importPath, next, false, true
	}

	index = nextSignificantToken(tokens, index)
	if index >= len(tokens) || tokens[index].Kind != TokenIdentifier {
		return nil, "", index, false, false
	}
	parts := []string{tokens[index].Text}
	index++
	for {
		dot := nextSignificantToken(tokens, index)
		if dot >= len(tokens) || tokens[dot].Text != "." {
			break
		}
		part := nextSignificantToken(tokens, dot+1)
		if part >= len(tokens) || tokens[part].Kind != TokenIdentifier {
			return nil, "", part, false, false
		}
		parts = append(parts, tokens[part].Text)
		index = part + 1
	}
	logical := strings.Join(parts, ".")
	return &ast.Ident{Name: parts[len(parts)-1]}, logical, index, true, true
}

func newImportSpec(name *ast.Ident, importPath string) *ast.ImportSpec {
	return &ast.ImportSpec{Name: name, Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(importPath)}}
}

func nextSignificantToken(tokens []Token, index int) int {
	for index < len(tokens) {
		if tokens[index].Kind != TokenComment && tokens[index].Kind != TokenNewline && tokens[index].Kind != TokenEOF {
			return index
		}
		index++
	}
	return index
}

func importTokenSpec(tokens []Token, index int) (*ast.Ident, int, int, bool) {
	index = nextSignificantToken(tokens, index)
	if index >= len(tokens) {
		return nil, 0, index, false
	}
	if tokens[index].Kind == TokenString {
		return nil, index, index + 1, true
	}
	name := tokens[index].Text
	if tokens[index].Kind != TokenIdentifier && name != "." && name != "_" {
		return nil, 0, index, false
	}
	pathIndex := nextSignificantToken(tokens, index+1)
	if pathIndex >= len(tokens) || tokens[pathIndex].Kind != TokenString {
		return nil, 0, pathIndex, false
	}
	return &ast.Ident{Name: name}, pathIndex, pathIndex + 1, true
}

func logicalPackageForImport(importPath, modulePath string, model *SemanticModel) (string, bool) {
	if _, exists := model.Packages[importPath]; exists {
		return importPath, true
	}
	if logical, ok := officialLogicalPackage(importPath); ok {
		if _, exists := model.Packages[logical]; exists {
			return logical, true
		}
	}
	prefix := modulePath + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", false
	}

	relative := strings.TrimPrefix(importPath, prefix)
	logical := strings.ReplaceAll(relative, "/", ".")
	if _, ok := model.Packages[logical]; !ok {
		return "", false
	}

	return logical, true
}

func officialLogicalPackage(importPath string) (string, bool) {
	if !strings.HasPrefix(importPath, "gpp/") {
		return "", false
	}
	return "gpp." + strings.ReplaceAll(strings.TrimPrefix(importPath, "gpp/"), "/", "."), true
}

func validateAmbiguousMemberAccess(pkg *PackageSymbols, class *ClassDecl) error {
	ambiguous, err := ambiguousMembers(pkg, class, map[string]map[string][]string{}, map[string]bool{})
	if err != nil {
		return err
	}

	for _, method := range class.Methods {
		member := findAmbiguousThisMemberTokens(methodBodyTokens(method), ambiguous)
		if member == "" {
			continue
		}

		paths := ambiguous[member]
		qualifiers := []string{}
		seen := map[string]bool{}
		for _, memberPath := range paths {
			qualifier := strings.Split(memberPath, ".")[0]
			if !seen[qualifier] {
				seen[qualifier] = true
				qualifiers = append(qualifiers, qualifier)
			}
		}

		access := make([]string, 0, len(qualifiers))
		for _, qualifier := range qualifiers {
			access = append(access, "this."+qualifier+"."+member)
		}

		return fmt.Errorf(
			"%s: class %s method %s has ambiguous member %s; qualify access as %s",
			classLocation(class),
			class.Name,
			method.Name,
			member,
			strings.Join(access, " or "),
		)
	}

	return nil
}

func ambiguousMembers(pkg *PackageSymbols, class *ClassDecl, cache map[string]map[string][]string, visiting map[string]bool) (map[string][]string, error) {
	members, err := classMembers(pkg, class, cache, visiting)
	if err != nil {
		return nil, err
	}

	ambiguous := map[string][]string{}
	for name, paths := range members {
		if len(paths) > 1 {
			ambiguous[name] = paths
		}
	}

	return ambiguous, nil
}

func classMembers(pkg *PackageSymbols, class *ClassDecl, cache map[string]map[string][]string, visiting map[string]bool) (map[string][]string, error) {
	if members, ok := cache[class.Name]; ok {
		return members, nil
	}
	if visiting[class.Name] {
		return nil, fmt.Errorf(
			"%s: inheritance cycle involving class %s",
			classLocation(class),
			class.Name,
		)
	}

	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	members := map[string][]string{}
	direct := map[string]bool{}
	for _, field := range class.Fields {
		direct[field.Name] = true
		members[field.Name] = []string{field.Name}
	}
	for _, method := range class.Methods {
		direct[method.Name] = true
		members[method.Name] = []string{method.Name}
	}

	for _, parentName := range classParentNames(class) {
		parent := pkg.Classes[parentName]
		if parent == nil {
			parent = pkg.ImportedClasses[parentName]
		}
		if parent == nil {
			return nil, fmt.Errorf("class %s has unresolved parent %s", class.Name, parentName)
		}
		parentMembers, err := classMembers(pkg, parent, cache, visiting)
		if err != nil {
			return nil, err
		}

		for name, paths := range parentMembers {
			if direct[name] {
				continue
			}

			for _, memberPath := range paths {
				members[name] = append(
					members[name],
					parentName+"."+memberPath,
				)
			}
		}
	}

	cache[class.Name] = members
	return members, nil
}

func findAmbiguousThisMember(src string, ambiguous map[string][]string) string {
	for i := 0; i < len(src); {
		switch src[i] {
		case '"', '\'':
			end, err := skipQuoted(src, i, src[i])
			if err != nil {
				return ""
			}
			i = end + 1
			continue
		case '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return ""
			}
			i += end + 2
			continue
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				end := strings.IndexByte(src[i+2:], '\n')
				if end < 0 {
					return ""
				}
				i += end + 2
				continue
			}
			if i+1 < len(src) && src[i+1] == '*' {
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return ""
				}
				i += end + 4
				continue
			}
		}

		name, n := readIdent(src[i:])
		if n == 0 {
			i++
			continue
		}
		if name == "this" {
			dot := skipSpace(src, i+n)
			if dot < len(src) && src[dot] == '.' {
				member, memberLength := readIdent(src[dot+1:])
				if memberLength > 0 {
					if _, ok := ambiguous[member]; ok {
						return member
					}
				}
			}
		}
		i += n
	}

	return ""
}

func findAmbiguousThisMemberTokens(tokens []Token, ambiguous map[string][]string) string {
	for index := 0; index < len(tokens); index++ {
		if tokens[index].Text != "this" || (tokens[index].Kind != TokenIdentifier && tokens[index].Kind != TokenKeyword) {
			continue
		}
		memberIndex := index + 1
		for memberIndex < len(tokens) && (tokens[memberIndex].Kind == TokenComment || tokens[memberIndex].Kind == TokenNewline) {
			memberIndex++
		}
		if memberIndex >= len(tokens) || (tokens[memberIndex].Text != "." && tokens[memberIndex].Text != "?.") {
			continue
		}
		memberIndex++
		for memberIndex < len(tokens) && (tokens[memberIndex].Kind == TokenComment || tokens[memberIndex].Kind == TokenNewline) {
			memberIndex++
		}
		if memberIndex < len(tokens) {
			member := tokens[memberIndex].Text
			if _, ok := ambiguous[member]; ok && (tokens[memberIndex].Kind == TokenIdentifier || tokens[memberIndex].Kind == TokenKeyword) {
				return member
			}
		}
	}
	return ""
}
