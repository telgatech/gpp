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
	Name       string
	Classes    map[string]*ClassDecl
	Extensions []*ExtendDecl
}

func classLocation(class *ClassDecl) string {
	if class.SourceLine > 0 {
		return fmt.Sprintf("%s:%d", class.SourceFile, class.SourceLine)
	}
	return class.SourceFile
}

func ResolveProgram(program *Program) (*SemanticModel, error) {
	model := &SemanticModel{
		Packages: map[string]*PackageSymbols{},
	}

	for _, file := range program.Files {
		pkg, ok := model.Packages[file.Package]
		if !ok {
			pkg = &PackageSymbols{
				Name:       file.Package,
				Classes:    map[string]*ClassDecl{},
				Extensions: []*ExtendDecl{},
			}
			model.Packages[file.Package] = pkg
		}

		for _, decl := range file.Decls {
			if extension, ok := decl.(*ExtendDecl); ok {
				pkg.Extensions = append(pkg.Extensions, extension)
				continue
			}
			class, ok := decl.(*ClassDecl)
			if !ok {
				continue
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
	}

	for _, pkg := range model.Packages {
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
	}

	return model, nil
}

func validateExtensions(pkg *PackageSymbols) error {
	seen := map[string]bool{}
	for _, extension := range pkg.Extensions {
		if strings.TrimSpace(extension.Target) == "" {
			return fmt.Errorf("%s: extension target cannot be empty", extension.SourceFile)
		}
		for _, method := range extension.Methods {
			parameters, err := parseParameterInfos(method.Parameters)
			if err != nil {
				return fmt.Errorf("%s: extension %s.%s has invalid parameters: %w", extension.SourceFile, extension.Target, method.Name, err)
			}
			key := strings.TrimSpace(extension.Target) + "/" + method.Name + "/" + parameterSignatureKey(parameters)
			if seen[key] {
				return fmt.Errorf("%s: extension method %s for %s is declared more than once with parameter types %s", extension.SourceFile, method.Name, extension.Target, parameterSignatureKey(parameters))
			}
			seen[key] = true
		}
	}
	return nil
}

func validateClass(pkg *PackageSymbols, class *ClassDecl) error {
	for _, reserved := range []string{"GoppClass", "GoppField", "GoppType"} {
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
		if field.Name == "GoppDynamicClass" {
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
		if method.Name == "GoppRuntimeClass" {
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
		for _, previous := range methods[method.Name] {
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
		methods[method.Name] = append(methods[method.Name], methodEntry{
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
		"package main\nfunc __gopp_parameters("+params+") {}\n",
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
	return fmt.Sprintf("%s__gopp_%d", name, arity)
}

func overloadedTypedName(name string, arity int, typeKey string) string {
	return fmt.Sprintf("%s__gopp_%d_%s", name, arity, mangleTypeKey(typeKey))
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
			if err := visit(parentName); err != nil {
				return err
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
	context.Extensions = extensionMethodsForDeclarations(model.Packages[file.Package].Extensions, "")

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

		for className, class := range pkg.Classes {
			key := alias + "." + className
			if alias == "." {
				key = className
			}

			context.Targets[key] = constructorTarget{
				Class:         class,
				Classes:       pkg.Classes,
				Qualifier:     qualifier,
				InterfaceName: "Gopp" + className,
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
		context.Extensions = append(context.Extensions, extensionMethodsForDeclarations(pkg.Extensions, alias)...)
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
		interfaceName = interfaceName[:dot+1] + "Gopp" + interfaceName[dot+1:]
	} else {
		interfaceName = "Gopp" + interfaceName
	}
	if overloads.ClassMethods[interfaceName] == nil {
		overloads.ClassMethods[interfaceName] = map[string]map[int]string{}
	}

	for _, method := range class.Methods {
		if method.GoName == "" {
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
			raw.WriteString(code.Code)
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
		parentMembers, err := classMembers(pkg, pkg.Classes[parentName], cache, visiting)
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
