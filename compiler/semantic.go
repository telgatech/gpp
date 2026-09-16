package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
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

func rawTypeNames(source string) map[string]bool {
	result := map[string]bool{}
	parsed, err := parser.ParseFile(token.NewFileSet(), "raw.gpp", "package main\n"+source, 0)
	if err != nil {
		return result
	}
	for _, declaration := range parsed.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.TYPE {
			continue
		}
		for _, spec := range group.Specs {
			if typeSpec, ok := spec.(*ast.TypeSpec); ok {
				result[typeSpec.Name.Name] = true
			}
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
			if raw, ok := decl.(*RawDecl); ok {
				for name := range rawTypeNames(raw.Code) {
					pkg.Types[name] = true
				}
				for name := range rawPackageNames(raw.Code) {
					pkg.Values[name] = true
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
				if raw, ok := decl.(*RawDecl); ok {
					for _, signature := range rawFunctionNames(raw.Code) {
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

func rawFunctionNames(src string) []string {
	parsed, err := parser.ParseFile(token.NewFileSet(), "raw.go", "package main\n\n"+stripAnnotationSyntaxPreserve(src), 0)
	if err != nil {
		return nil
	}
	result := []string{}
	for _, declaration := range parsed.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil {
			result = append(result, function.Name.Name)
		}
	}
	return result
}

func rawPackageNames(src string) map[string]bool {
	parsed, err := parser.ParseFile(token.NewFileSet(), "raw.go", "package main\n\n"+stripAnnotationSyntaxPreserve(src), 0)
	if err != nil {
		return nil
	}
	result := map[string]bool{}
	for _, declaration := range parsed.Decls {
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
	}
	return result
}

func validateExtensions(pkg *PackageSymbols) error {
	seen := map[string]bool{}
	for _, extension := range pkg.Extensions {
		if len(extension.Targets) == 0 {
			return fmt.Errorf("%s: extension target cannot be empty", extension.SourceFile)
		}
		targets := map[string]bool{}
		for _, rawTarget := range extension.Targets {
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
			parameters, err := parseParameterInfos(method.Parameters)
			if err != nil {
				return fmt.Errorf("%s: extension %s.%s has invalid parameters: %w", extension.SourceFile, strings.Join(extension.Targets, ", "), method.Name, err)
			}
			for _, rawTarget := range extension.Targets {
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
	if parsed, err := parser.ParseExpr(target); err == nil {
		if formatted, err := formatNode(parsed); err == nil {
			return strings.TrimSpace(formatted)
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
		if method.IsStatic && sourceContainsIdentifier(method.Body, "this") {
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
		arity, err := parameterCount(method.Parameters)
		if err != nil {
			return fmt.Errorf(
				"%s: class %s method %s has invalid parameters: %w",
				classLocation(class),
				class.Name,
				method.Name,
				err,
			)
		}

		parameters, err := parseParameterInfos(method.Parameters)
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
	for _, parentName := range class.Parents {
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

func parameterCount(params string) (int, error) {
	if strings.TrimSpace(params) == "" {
		return 0, nil
	}
	params, err := stripParameterDefaults(params)
	if err != nil {
		return 0, err
	}

	parsed, err := parser.ParseFile(
		token.NewFileSet(),
		"parameters.go",
		"package main\nfunc __gpp_parameters("+params+") {}\n",
		0,
	)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, field := range parsed.Decls[0].(*ast.FuncDecl).Type.Params.List {
		if len(field.Names) == 0 {
			count++
		} else {
			count += len(field.Names)
		}
	}
	return count, nil
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

		for _, parentName := range pkg.Classes[className].Parents {
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
				importedExtensions[index].Method.Parameters = qualifyImportedTypeNames(
					importedExtensions[index].Method.Parameters,
					qualifier+"._",
					pkg.Classes,
					importedTypes,
				)
				importedExtensions[index].Method.Result = qualifyImportedTypeNames(
					importedExtensions[index].Method.Result,
					qualifier+"._",
					pkg.Classes,
					importedTypes,
				)
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
		arity, err := parameterCount(method.Parameters)
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
	var raw strings.Builder
	for _, decl := range file.Decls {
		if code, ok := decl.(*RawDecl); ok {
			raw.WriteString(stripAnnotationSyntaxPreserve(code.Code))
		}
	}

	parsed, err := parser.ParseFile(
		token.NewFileSet(),
		file.Name,
		"package main\n\n"+raw.String(),
		parser.ImportsOnly,
	)
	if err != nil {
		return nil, err
	}

	return parsed.Imports, nil
}

func logicalPackageForImport(importPath, modulePath string, model *SemanticModel) (string, bool) {
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
		member := findAmbiguousThisMember(method.Body, ambiguous)
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

	for _, parentName := range class.Parents {
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
