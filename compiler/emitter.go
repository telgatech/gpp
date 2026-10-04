package compiler

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	htmltemplate "html/template"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func Emit(file *File) ([]byte, error) {
	return EmitWithOptions(file, CompileOptions{})
}

func EmitWithOptions(file *File, options CompileOptions) ([]byte, error) {
	atExit, err := validateAtExit(&Program{Files: []*File{file}})
	if err != nil {
		return nil, err
	}
	model, err := ResolveProgram(&Program{Files: []*File{file}})
	if err != nil {
		return nil, err
	}
	addDefaultObjectMethods(model)
	model, err = ResolveProgram(&Program{Files: []*File{file}})
	if err != nil {
		return nil, err
	}
	scope, err := annotationScopeForFile(file, model, "")
	if err != nil {
		return nil, err
	}
	if err := validateFileAnnotations(file, model.Packages[file.Package], scope, false); err != nil {
		return nil, err
	}

	context := localConstructorContext(model.Packages[file.Package].Classes)
	context.Enums = model.Packages[file.Package].Enums
	context.Annotations = model.Packages[file.Package].Annotations
	context.Package = file.Package
	context.AtExit = atExit
	context.Development = options.Development
	context.Templates = model.Packages[file.Package].Templates
	context.Records = newRecordContext()
	context.AvailableImports = map[string]string{}
	context.GlobalValueTypes = globalValueTypesForFile(file)
	if imports, importErr := goImports(file); importErr == nil {
		for _, spec := range imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				continue
			}
			alias := path.Base(importPath)
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias != "_" && alias != "." {
				context.AvailableImports[alias] = importPath
			}
		}
	}
	context.Introspection.Enabled = fileUsesIntrospection(file)
	context.Extensions = extensionMethodsForDeclarations(model.Packages[file.Package].Extensions, "")
	if !options.NoPrelude {
		if err := configurePrelude(&context, true); err != nil {
			return nil, err
		}
	}
	configureNativeExtensionMethods(&context, file)
	if err := addFunctionOverloads(&context, file); err != nil {
		return nil, err
	}
	functionSignatures, err := functionSignaturesForFile(file)
	if err != nil {
		return nil, err
	}
	context.FunctionSignatures = functionSignatures
	configureEnumSignatures(&context)

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
	ClassFieldTypes  map[string]map[string]string
	LocalTypes       map[string]string
	CurrentClass     string
}

type constructorContext struct {
	Targets                    map[string]constructorTarget
	Enums                      map[string]*EnumDecl
	Overloads                  overloadContext
	FunctionSignatures         map[string][]callableSignature
	MethodSignatures           map[string][]callableSignature
	ClassMethodSignatures      map[string]map[string][]callableSignature
	StaticMethodSignatures     map[string]map[string][]callableSignature
	Records                    *recordContext
	Introspection              *introspectionContext
	Extensions                 []extensionMethod
	PreludeExtensions          []extensionMethod
	PreludeExtensionsNeeded    map[string]bool
	PreludeImports             []string
	EmitPrelude                bool
	EmitPreludeAll             bool
	Annotations                map[string]*AnnotationDecl
	Package                    string
	AtExit                     bool
	ModulePath                 string
	Development                bool
	AvailableImports           map[string]string
	GlobalValueTypes           map[string]string
	Embeds                     []compiledEmbed
	Templates                  map[string]*TemplateDecl
	IntrospectionRuntimeImport string
	Exceptions                 *exceptionContext
	ImportedTypes              map[string]map[string]bool
	NativeMethods              map[string]map[string]bool
	CurrentClass               string
	CurrentMethod              string
	CurrentResultAST           TypeNode
	CurrentExtensionReceiver   string
	CurrentParameterTypes      map[string]string
	CurrentParameterAST        map[string]TypeNode
	CurrentIntrospectionKinds  map[string]int
	RecordValueTypes           map[string]string
	RecordOnlyLowering         bool
	InterpolationName          string
}

func collectGlobalValueTypesFromAST(declarations []ast.Decl, output map[string]string) {
	for _, declaration := range declarations {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.VAR {
			continue
		}
		for _, spec := range group.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || value.Type == nil {
				continue
			}
			typeName := strings.TrimSpace(safeASTTypeName(value.Type))
			if typeName == "" {
				continue
			}
			for _, name := range value.Names {
				output[name.Name] = typeName
			}
		}
	}
}

func (context constructorContext) currentResultText() string {
	if context.CurrentResultAST == nil {
		return ""
	}
	text, err := typeNodeSource(context.CurrentResultAST)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

func localConstructorContext(classes map[string]*ClassDecl) constructorContext {
	targets := map[string]constructorTarget{}
	for name, class := range classes {
		targets[name] = constructorTarget{
			Class:         class,
			Classes:       classes,
			InterfaceName: "__gpp_" + name,
		}
	}

	context := constructorContext{
		Targets: targets,
		Enums:   map[string]*EnumDecl{},
		Overloads: overloadContext{
			Functions:        map[string]map[int]string{},
			FunctionTypes:    map[string]map[string]string{},
			Methods:          map[string]map[int]string{},
			MethodTypes:      map[string]map[string]string{},
			ClassMethods:     map[string]map[string]map[int]string{},
			ClassMethodTypes: map[string]map[string]map[string]string{},
			ClassFieldTypes:  map[string]map[string]string{},
		},
		FunctionSignatures:     map[string][]callableSignature{},
		MethodSignatures:       map[string][]callableSignature{},
		ClassMethodSignatures:  map[string]map[string][]callableSignature{},
		StaticMethodSignatures: map[string]map[string][]callableSignature{},
		Introspection:          newIntrospectionContext(classes),
		Exceptions:             &exceptionContext{},
		Annotations:            map[string]*AnnotationDecl{},
	}

	for _, class := range classes {
		methodSet := methodOverloadsForClass(
			class,
			classes,
			map[string]bool{},
		)
		context.Overloads.ClassMethods[class.Name] = methodSet
		context.Overloads.ClassMethods["__gpp_"+class.Name] = methodSet
		typeSet := methodOverloadTypesForClass(class, classes, map[string]bool{})
		context.Overloads.ClassMethodTypes[class.Name] = typeSet
		context.Overloads.ClassMethodTypes["__gpp_"+class.Name] = typeSet
		fieldTypes := classFieldTypesForClass(class, classes, map[string]bool{})
		context.Overloads.ClassFieldTypes[class.Name] = fieldTypes
		context.Overloads.ClassFieldTypes["__gpp_"+class.Name] = fieldTypes
		context.ClassMethodSignatures[class.Name] = methodSignaturesForClass(
			class,
			classes,
			map[string]bool{},
		)
		context.StaticMethodSignatures[class.Name] = staticMethodSignaturesForClass(
			class,
			classes,
			map[string]bool{},
		)
		for _, method := range class.Methods {
			if method.GoName == "" || method.IsStatic {
				continue
			}
			arity, err := parameterCount(methodParametersSource(method))
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
	context.InterpolationName = interpolationName
	if tokenSequence(file.Tokens, "record") || tokenSequence(file.Tokens, "let") {
		if err := prepareRecordContextAST(file, context); err != nil {
			return nil, err
		}
	}

	body, err := emitDecls(file, context, interpolationName)
	if err != nil {
		return nil, err
	}
	embedImports, embedDefinitions := emitEmbedDeclarations(context.Embeds, body)
	body = insertAfterImports(body, embedImports)
	templateImports, templateDefinitions, err := emitTemplateDeclarations(file, context, body)
	if err != nil {
		return nil, err
	}
	body = insertAfterImports(body, templateImports)
	body = rewriteLogicalPackageImports(body, file.Imports, context.ModulePath)
	body = rewriteOfficialImports(body, context.ModulePath)
	body = ensureGeneratedImports(body, context)
	enumErrorAlias, enumNeedsErrorImport := enumErrorImport(file)
	enumJSONAlias, enumNeedsJSONImport := enumJSONImport(file)
	enumDriverAlias, enumNeedsDriverImport := enumDriverImport(file)
	if hasEnumDeclarations(file) && enumNeedsErrorImport {
		body = insertAfterImports(body, fmt.Sprintf("import %s %q\n\n", enumErrorAlias, "errors"))
	}
	if hasEnumDeclarations(file) && enumNeedsJSONImport {
		body = insertAfterImports(body, fmt.Sprintf("import %s %q\n\n", enumJSONAlias, "encoding/json"))
	}
	if hasEnumDeclarations(file) && enumNeedsDriverImport {
		body = insertAfterImports(body, fmt.Sprintf("import %s %q\n\n", enumDriverAlias, "database/sql/driver"))
	}
	if context.Introspection != nil &&
		(len(context.Introspection.Classes) > 0 || len(context.Annotations) > 0) &&
		(context.IntrospectionRuntimeImport != "" &&
			(!context.Introspection.RuntimeEmitted || strings.Contains(body, "gppRuntime."))) {
		body = ensureIntrospectionRuntimeImport(body, context.IntrospectionRuntimeImport)
	}
	if context.EmitPrelude {
		preludeBody, err := emitPreludeExtensions(context, body)
		if err != nil {
			return nil, err
		}
		preludeBody = rewritePreludeImportAliases(preludeBody, body, context.PreludeImports)
		body = prependPreludeImports(body, preludeImportsForBody(preludeBody, context.PreludeImports))
		body = insertAfterImports(body, preludeBody)
	}

	localRuntimeDefinitions := ""
	localRuntimeImports := ""
	if context.Introspection != nil && context.IntrospectionRuntimeImport == "" &&
		(len(context.Introspection.Classes) > 0 || len(context.Annotations) > 0) &&
		!context.Introspection.RuntimeEmitted {
		runtimeImportBody := body
		if needsFmtImport {
			runtimeImportBody = "import \"fmt\"\n\n" + runtimeImportBody
		}
		fmtAlias, reflectAlias, strconvAlias, stringsAlias, imports := defaultObjectRuntimeImports(runtimeImportBody)
		localRuntimeImports = imports
		localRuntimeDefinitions = introspectionRuntimeDefinitionsWithAliases(
			fmtAlias,
			reflectAlias,
			strconvAlias,
			stringsAlias,
		)
	}

	declarationsPrefix := localRuntimeImports + embedDefinitions + templateDefinitions
	if context.Exceptions != nil && !context.Exceptions.RuntimeEmitted {
		declarationsPrefix += exceptionRuntimeDefinitions()
		context.Exceptions.RuntimeEmitted = true
	}
	if needsFmtImport {
		declarationsPrefix += "import \"fmt\"\n\n"
	}
	if fileHasSafeAccess(file) {
		declarationsPrefix += "func __gpp_safe[R any](isNil bool, access func() R) R {\n"
		declarationsPrefix += "\tif isNil { var zero R; return zero }\n"
		declarationsPrefix += "\treturn access()\n"
		declarationsPrefix += "}\n\n"
	}
	if context.Records != nil {
		declarationsPrefix += context.Records.definitions()
	}
	if context.Introspection != nil && (len(context.Introspection.Classes) > 0 || len(context.Annotations) > 0) &&
		!context.Introspection.RuntimeEmitted {
		if context.IntrospectionRuntimeImport == "" {
			declarationsPrefix += localRuntimeDefinitions
		} else {
			declarationsPrefix += introspectionRuntimeAliases()
		}
		context.Introspection.RuntimeEmitted = true
	}
	var annotationDescriptors strings.Builder
	emitAnnotationDescriptors(&annotationDescriptors, file, context)
	declarationsPrefix += annotationDescriptors.String()

	out.WriteString(insertAfterImports(body, declarationsPrefix))

	generated := out.String()
	generated, err = pruneUnusedImports(generated)
	if err != nil {
		return []byte(generated), err
	}
	result, err := format.Source([]byte(generated))

	if err != nil {
		return []byte(generated), fmt.Errorf(
			"generated invalid Go: %w",
			err,
		)
	}

	return result, nil
}

func emitEmbedDeclarations(embeds []compiledEmbed, body string) (string, string) {
	if len(embeds) == 0 {
		return "", ""
	}
	embedAlias := generatedImportAlias(body, "gppEmbed")
	needsFS := false
	for _, embed := range embeds {
		if embed.Directory {
			needsFS = true
			break
		}
	}
	fsAlias := ""
	if needsFS {
		fsAlias = generatedImportAlias(body, "gppFS")
	}

	var imports strings.Builder
	imports.WriteString("import ")
	if embedAlias == "embed" {
		fmt.Fprintf(&imports, "%q", "embed")
	} else {
		fmt.Fprintf(&imports, "%s %q", embedAlias, "embed")
	}
	if needsFS {
		imports.WriteString("\nimport ")
		fmt.Fprintf(&imports, "%s %q", fsAlias, "io/fs")
	}
	imports.WriteString("\n\n")

	var definitions strings.Builder
	fmt.Fprintf(&definitions, "var _ = %s.FS{}\n\n", embedAlias)
	for _, embed := range embeds {
		if !embed.Directory {
			fmt.Fprintf(&definitions, "//go:embed %s\nvar %s []byte\n\n", embed.Path, embed.Name)
			continue
		}
		internal := "__gppEmbed_" + embed.Name
		fmt.Fprintf(&definitions, "//go:embed %s\nvar %s %s.FS\n\n", embed.Path, internal, embedAlias)
		fmt.Fprintf(&definitions, "var %s %s.FS = func() %s.FS {\n", embed.Name, fsAlias, fsAlias)
		fmt.Fprintf(&definitions, "\tresult, err := %s.Sub(%s, %q)\n", fsAlias, internal, embed.Path)
		definitions.WriteString("\tif err != nil { panic(err) }\n\treturn result\n}()\n\n")
	}
	return imports.String(), definitions.String()
}

func templateParameterInfos(template *TemplateDecl) ([]parameterInfo, error) {
	if template != nil {
		return parameterInfosFromNodes(template.ParameterAST)
	}
	return nil, nil
}

func emitTemplateDeclarations(file *File, context constructorContext, body string) (string, string, error) {
	var templates []*TemplateDecl
	for _, declaration := range file.Decls {
		template, ok := declaration.(*TemplateDecl)
		if ok {
			templates = append(templates, template)
		}
	}
	if len(templates) == 0 {
		return "", "", nil
	}
	allNames := make([]string, 0, len(context.Templates))
	for name := range context.Templates {
		allNames = append(allNames, name)
	}
	sort.Strings(allNames)
	for _, template := range templates {
		if err := validateTemplateBody(template, allNames, context); err != nil {
			return "", "", err
		}
	}

	htmlAlias := generatedImportAlias(body, "gppHTML")
	ioAlias := generatedImportAlias(body, "gppIO")
	bytesAlias := generatedImportAlias(body, "gppBytes")
	fmtAlias := generatedImportAlias(body, "gppFmt")
	tplAlias := generatedImportAlias(body, "gppTpl")
	runtimeImport := "gpp/tpl"
	if context.ModulePath != "" {
		runtimeImport = context.ModulePath + "/gpp/tpl"
	}
	imports := fmt.Sprintf(
		"import %s %q\nimport %s %q\nimport %s %q\nimport %s %q\nimport %s %q\n\n",
		htmlAlias, "html/template", ioAlias, "io", bytesAlias, "bytes", fmtAlias, "fmt", tplAlias, runtimeImport,
	)

	var definitions strings.Builder

	for _, template := range templates {
		parameters, err := templateParameterInfos(template)
		if err != nil {
			return "", "", err
		}
		parameterText := templateParameterText(parameters, context)
		dataExpression := templateDataExpression(parameters)
		managedDevelopment := context.Development && strings.HasSuffix(file.Name, ".gpp.tpl") && file.SourcePath != ""
		fmt.Fprintf(&definitions, "func __gpp_tpl_static_funcs_%s() %s.FuncMap {\n", template.Name, htmlAlias)
		fmt.Fprintf(&definitions, "\tfunctions := %s.FuncMap{}\n", htmlAlias)
		fmt.Fprintf(&definitions, "\tfor name, function := range %s.Funcs.Snapshot() { functions[name] = function }\n", tplAlias)
		fmt.Fprintf(&definitions, "\tfunctions[\"param\"] = %s.Param\n", tplAlias)
		fmt.Fprintf(&definitions, "\tfunctions[\"body\"] = func(args ...any) (%s.HTML, error) { return \"\", nil }\n", htmlAlias)
		for _, name := range allNames {
			fmt.Fprintf(&definitions, "\tfunctions[%q] = __gpp_tpl_call_%s\n", name, name)
		}
		definitions.WriteString("\treturn functions\n}\n\n")
		fmt.Fprintf(&definitions, "func __gpp_tpl_funcs_%s(params map[string]string) %s.FuncMap {\n", template.Name, htmlAlias)
		fmt.Fprintf(&definitions, "\tfunctions := %s.FuncMap{}\n", htmlAlias)
		fmt.Fprintf(&definitions, "\tfor name, function := range %s.Funcs.Snapshot() { functions[name] = function }\n", tplAlias)
		fmt.Fprintf(&definitions, "\tfunctions[\"param\"] = func(name string) string { return params[name] }\n")
		fmt.Fprintf(&definitions, "\tfunctions[\"body\"] = func(args ...any) (%s.HTML, error) { return \"\", nil }\n", htmlAlias)
		for _, name := range allNames {
			fmt.Fprintf(&definitions, "\tcurrent_%s := %q\n", name, name)
			fmt.Fprintf(&definitions, "\tfunctions[current_%s] = func(args ...any) (%s.HTML, error) { var buffer %s.Buffer; if err := __gpp_tpl_render_%s(&buffer, params, args...); err != nil { return \"\", err }; return %s.HTML(buffer.String()), nil }\n", name, htmlAlias, bytesAlias, name, htmlAlias)
		}
		definitions.WriteString("\treturn functions\n}\n\n")
		fmt.Fprintf(&definitions, "var __gpp_tpl_%s_template *%s.Template\n\n", template.Name, htmlAlias)
		if managedDevelopment {
			fmt.Fprintf(&definitions, "const __gpp_tpl_development_%s = true\n\n", template.Name)
		}
		fmt.Fprintf(&definitions, "func __gpp_tpl_%s(w %s.Writer%s) error {\n", template.Name, ioAlias, parameterText)
		if managedDevelopment && template.Layout == "" {
			fmt.Fprintf(&definitions, "\tif __gpp_tpl_development_%s { return %s.Execute(w, %q", template.Name, tplAlias, template.Name)
			for _, parameter := range parameters {
				fmt.Fprintf(&definitions, ", %s", parameter.Name)
			}
			definitions.WriteString(") }\n")
		}
		if template.Layout != "" {
			fmt.Fprintf(&definitions, "\treturn __gpp_tpl_render_%s(w, map[string]string{}", template.Name)
			for _, parameter := range parameters {
				fmt.Fprintf(&definitions, ", %s", parameter.Name)
			}
			definitions.WriteString(")\n}\n\n")
		} else {
			fmt.Fprintf(&definitions, "\treturn __gpp_tpl_%s_template.Execute(%s, %s)\n}\n\n", template.Name, "w", dataExpression)
		}
		fmt.Fprintf(&definitions, "func __gpp_tpl_execute_%s(w %s.Writer, params map[string]string%s) error {\n", template.Name, ioAlias, parameterText)
		fmt.Fprintf(&definitions, "\tparsed, err := %s.New(%q).Funcs(__gpp_tpl_funcs_%s(params)).Parse(%s)\n", htmlAlias, template.Name, template.Name, strconv.Quote(template.Body))
		definitions.WriteString("\tif err != nil { return err }\n")
		fmt.Fprintf(&definitions, "\treturn parsed.Execute(w, %s)\n}\n\n", dataExpression)
		if templateUsedAsLayout(template.Name, context.Templates) {
			fmt.Fprintf(&definitions, "func __gpp_tpl_execute_%s_with_body(w %s.Writer, params map[string]string, body func(...any) (%s.HTML, error)) error {\n", template.Name, ioAlias, htmlAlias)
			fmt.Fprintf(&definitions, "\tfunctions := __gpp_tpl_funcs_%s(params)\n", template.Name)
			definitions.WriteString("\tfunctions[\"body\"] = body\n")
			fmt.Fprintf(&definitions, "\tparsed, err := %s.New(%q).Funcs(functions).Parse(%s)\n", htmlAlias, template.Name, strconv.Quote(template.Body))
			definitions.WriteString("\tif err != nil { return err }\n")
			fmt.Fprintf(&definitions, "\treturn parsed.Execute(w, %s)\n}\n\n", dataExpression)
		}
		fmt.Fprintf(&definitions, "func __gpp_tpl_render_%s_body(w %s.Writer, params map[string]string, args ...any) error {\n", template.Name, ioAlias)
		if err := emitTemplateArguments(&definitions, template, parameters, context, fmtAlias, true); err != nil {
			return "", "", err
		}
		definitions.WriteString("\treturn __gpp_tpl_execute_")
		definitions.WriteString(template.Name)
		definitions.WriteString("(w, params")
		for _, parameter := range parameters {
			definitions.WriteString(", ")
			definitions.WriteString(parameter.Name)
		}
		definitions.WriteString(")\n")
		definitions.WriteString("}\n\n")
		fmt.Fprintf(&definitions, "func __gpp_tpl_render_%s(w %s.Writer, params map[string]string, args ...any) error {\n", template.Name, ioAlias)
		if template.Layout == "" {
			fmt.Fprintf(&definitions, "\treturn __gpp_tpl_render_%s_body(w, params, args...)\n", template.Name)
		} else {
			fmt.Fprintf(&definitions, "\tvar content %s.Buffer\n", bytesAlias)
			fmt.Fprintf(&definitions, "\tif err := __gpp_tpl_render_%s_body(&content, params, args...); err != nil { return err }\n", template.Name)
			fmt.Fprintf(&definitions, "\treturn __gpp_tpl_execute_%s_with_body(w, params, func(args ...any) (%s.HTML, error) { return %s.HTML(content.String()), nil })\n", template.Layout, htmlAlias, htmlAlias)
		}
		definitions.WriteString("}\n\n")
		fmt.Fprintf(&definitions, "func __gpp_tpl_call_%s(args ...any) (%s.HTML, error) {\n", template.Name, htmlAlias)
		if err := emitTemplateArguments(&definitions, template, parameters, context, fmtAlias, false); err != nil {
			return "", "", err
		}
		definitions.WriteString("\tvar buffer ")
		definitions.WriteString(bytesAlias)
		definitions.WriteString(".Buffer\n")
		definitions.WriteString("\tif err := __gpp_tpl_")
		definitions.WriteString(template.Name)
		definitions.WriteString("(&buffer")
		for _, parameter := range parameters {
			definitions.WriteString(", ")
			definitions.WriteString(parameter.Name)
		}
		definitions.WriteString("); err != nil { return \"\", err }\n")
		fmt.Fprintf(&definitions, "\treturn %s.HTML(buffer.String()), nil\n}\n\n", htmlAlias)
		if managedDevelopment {
			fmt.Fprintf(&definitions, "func init() {\n\tparsed, err := %s.New(%q).Funcs(__gpp_tpl_static_funcs_%s()).Parse(%s)\n\tif err != nil { panic(err) }\n\t__gpp_tpl_%s_template = parsed\n\tif err := %s.RegisterManaged(%q, %q, %q, __gpp_tpl_render_%s); err != nil { panic(err) }\n}\n\n", htmlAlias, template.Name, template.Name, strconv.Quote(template.Body), template.Name, tplAlias, template.Name, templatePath(template), file.SourcePath, template.Name)
		} else {
			fmt.Fprintf(&definitions, "func init() {\n\tparsed, err := %s.New(%q).Funcs(__gpp_tpl_static_funcs_%s()).Parse(%s)\n\tif err != nil { panic(err) }\n\t__gpp_tpl_%s_template = parsed\n\tif err := %s.Register(%q, %q, __gpp_tpl_render_%s); err != nil { panic(err) }\n}\n\n", htmlAlias, template.Name, template.Name, strconv.Quote(template.Body), template.Name, tplAlias, template.Name, templatePath(template), template.Name)
		}
	}
	return imports, definitions.String(), nil
}

func validateTemplateBody(template *TemplateDecl, names []string, context constructorContext) error {
	functions := htmltemplate.FuncMap{
		"param": func(string) string { return "" },
		"body":  func(...any) (string, error) { return "", nil },
	}
	for _, name := range names {
		functions[name] = func(...any) (string, error) { return "", nil }
	}
	parsed, err := htmltemplate.New(template.Name).Funcs(functions).Parse(template.Body)
	if err != nil {
		return templateBodyParseError(template, err)
	}
	return validateTypedTemplateBody(template, parsed.Tree.Root, context)
}

func templateBodyParseError(template *TemplateDecl, err error) error {
	message := err.Error()
	line := 0
	for _, prefix := range []string{
		"template: " + template.Name + ":",
		"html/template: " + template.Name + ":",
	} {
		if !strings.HasPrefix(message, prefix) {
			continue
		}
		remainder := strings.TrimSpace(strings.TrimPrefix(message, prefix))
		lineText, detail, ok := strings.Cut(remainder, ":")
		if !ok {
			break
		}
		if parsedLine, parseErr := strconv.Atoi(strings.TrimSpace(lineText)); parseErr == nil {
			line = parsedLine
			message = strings.TrimSpace(detail)
		}
		break
	}
	if line > 0 {
		line = template.BodySpan.Line + line - 1
	} else {
		line = template.SourceLine
	}
	return sourceLineError(Span{Line: line}, fmt.Errorf("invalid template body: %s", message))
}

func templateUsedAsLayout(name string, templates map[string]*TemplateDecl) bool {
	for _, template := range templates {
		if template.Layout == name {
			return true
		}
	}
	return false
}

func templateParameterText(parameters []parameterInfo, context constructorContext) string {
	if len(parameters) == 0 {
		return ""
	}
	parts := []string{}
	for _, parameter := range parameters {
		parts = append(parts, parameter.Name+" "+transformPolymorphicType(parameter.typeText(), context))
	}
	return ", " + strings.Join(parts, ", ")
}

func templateDataExpression(parameters []parameterInfo) string {
	if len(parameters) == 0 {
		return "nil"
	}
	if len(parameters) == 1 {
		return parameters[0].Name
	}
	var data strings.Builder
	data.WriteString("map[string]any{")
	for _, parameter := range parameters {
		fmt.Fprintf(&data, "%q: %s, ", parameter.Name, parameter.Name)
	}
	data.WriteString("}")
	return data.String()
}

func emitTemplateArguments(out *strings.Builder, template *TemplateDecl, parameters []parameterInfo, context constructorContext, fmtAlias string, render bool) error {
	if render {
		fmt.Fprintf(out, "\tif len(args) != %d { return %s.Errorf(%q) }\n", len(parameters), fmtAlias, "template "+template.Name+" received the wrong number of arguments")
	} else {
		fmt.Fprintf(out, "\tif len(args) != %d { return \"\", %s.Errorf(%q) }\n", len(parameters), fmtAlias, "template "+template.Name+" received the wrong number of arguments")
	}
	for index, parameter := range parameters {
		parameterType := transformPolymorphicType(parameter.typeText(), context)
		if render {
			fmt.Fprintf(out, "\t%s, ok := args[%d].(%s)\n\tif !ok { return %s.Errorf(%q, args[%d]) }\n", parameter.Name, index, parameterType, fmtAlias, "template "+template.Name+" argument "+parameter.Name+" has incompatible type %T", index)
		} else {
			fmt.Fprintf(out, "\t%s, ok := args[%d].(%s)\n\tif !ok { return \"\", %s.Errorf(%q, args[%d]) }\n", parameter.Name, index, parameterType, fmtAlias, "template "+template.Name+" argument "+parameter.Name+" has incompatible type %T", index)
		}
	}
	return nil
}

func templatePath(template *TemplateDecl) string {
	for _, annotation := range template.Annotations {
		if annotation.Name != "Path" && !strings.HasSuffix(annotation.Name, ".Path") {
			continue
		}
		args := annotationArgumentTexts(annotation)
		if len(args) == 1 {
			value, err := strconv.Unquote(strings.TrimSpace(args[0]))
			if err == nil {
				return value
			}
		}
	}
	return ""
}

func transformStaticTemplateCalls(src string, context constructorContext) (string, error) {
	if len(context.Templates) == 0 {
		return src, nil
	}
	if transformed, handled, err := transformStaticTemplateCallsAST(src, context); handled {
		return transformed, err
	}
	return src, nil
}

type generatedImport struct {
	Alias string
	Path  string
	Start int
	End   int
}

func generatedImports(body string) ([]generatedImport, int, bool) {
	tokens, err := LexSource("generated Go", body)
	if err != nil {
		return nil, 0, false
	}
	imports := []generatedImport{}
	lastEnd := -1
	for index := 0; index < len(tokens); index++ {
		if tokens[index].Text != "import" {
			continue
		}
		start := tokens[index].Span.Start
		next := nextSignificantToken(tokens, index+1)
		if next >= len(tokens) {
			return nil, 0, false
		}
		if tokens[next].Text == "(" {
			close := matchingToken(tokens, next, "(", ")")
			if close < 0 {
				return nil, 0, false
			}
			for cursor := nextSignificantToken(tokens, next+1); cursor < close; {
				name, pathIndex, after, ok := importTokenSpec(tokens, cursor)
				if !ok || after > close {
					return nil, 0, false
				}
				importPath, err := strconv.Unquote(tokens[pathIndex].Text)
				if err != nil {
					return nil, 0, false
				}
				alias := path.Base(importPath)
				if name != nil {
					alias = name.Name
				}
				imports = append(imports, generatedImport{Alias: alias, Path: importPath, Start: start, End: tokens[close].Span.End})
				cursor = nextSignificantToken(tokens, after)
			}
			lastEnd = tokens[close].Span.End
			index = close
			continue
		}
		name, pathIndex, after, ok := importTokenSpec(tokens, next)
		if !ok {
			return nil, 0, false
		}
		importPath, err := strconv.Unquote(tokens[pathIndex].Text)
		if err != nil {
			return nil, 0, false
		}
		alias := path.Base(importPath)
		if name != nil {
			alias = name.Name
		}
		imports = append(imports, generatedImport{Alias: alias, Path: importPath, Start: start, End: tokens[pathIndex].Span.End})
		lastEnd = tokens[pathIndex].Span.End
		index = after - 1
	}
	return imports, lastEnd, true
}

func generatedImportAlias(body, preferred string) string {
	imports, _, ok := generatedImports(body)
	if !ok {
		return preferred
	}
	used := map[string]bool{}
	for _, declaration := range imports {
		used[declaration.Alias] = true
	}
	if !used[preferred] {
		return preferred
	}
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s%d", preferred, index)
		if !used[candidate] {
			return candidate
		}
	}
}

func insertAfterImports(body, insertion string) string {
	if insertion == "" {
		return body
	}
	_, lastEnd, ok := generatedImports(body)
	if !ok || lastEnd < 0 {
		return insertion + body
	}
	return body[:lastEnd] + "\n\n" + insertion + body[lastEnd:]
}

func defaultObjectRuntimeImports(body string) (string, string, string, string, string) {
	imports, _, ok := generatedImports(body)
	if !ok {
		return "gppFmt", "gppReflect", "gppStrconv", "gppStrings", "import (\n\tgppFmt \"fmt\"\n\tgppReflect \"reflect\"\n\tgppStrconv \"strconv\"\n\tgppStrings \"strings\"\n)\n\n"
	}

	existingNames := map[string]bool{}
	aliases := map[string]string{}
	for _, spec := range imports {
		existingNames[spec.Alias] = true
		if spec.Alias != "_" && spec.Alias != "." {
			aliases[spec.Path] = spec.Alias
		}
	}

	choose := func(importPath, preferred string) string {
		if alias := aliases[importPath]; alias != "" {
			return alias
		}
		alias := preferred
		for index := 2; existingNames[alias]; index++ {
			alias = fmt.Sprintf("%s%d", preferred, index)
		}
		existingNames[alias] = true
		return alias
	}

	fmtAlias := choose("fmt", "gppFmt")
	reflectAlias := choose("reflect", "gppReflect")
	strconvAlias := choose("strconv", "gppStrconv")
	stringsAlias := choose("strings", "gppStrings")

	runtimeImportPaths := []struct {
		path  string
		alias string
	}{
		{"fmt", fmtAlias},
		{"reflect", reflectAlias},
		{"strconv", strconvAlias},
		{"strings", stringsAlias},
	}
	var output strings.Builder
	for _, item := range runtimeImportPaths {
		if aliases[item.path] != "" {
			continue
		}
		fmt.Fprintf(&output, "\t%s %q\n", item.alias, item.path)
	}
	if output.Len() == 0 {
		return fmtAlias, reflectAlias, strconvAlias, stringsAlias, ""
	}
	return fmtAlias, reflectAlias, strconvAlias, stringsAlias, "import (\n" + output.String() + ")\n\n"
}

func rewriteLogicalPackageImports(body string, imports []ImportDecl, modulePath string) string {
	if modulePath == "" {
		return body
	}
	logicalPaths := map[string]bool{}
	for _, declaration := range imports {
		if declaration.LogicalPackage {
			logicalPaths[declaration.Path] = true
		}
	}
	if len(logicalPaths) == 0 {
		return body
	}
	specs, _, ok := generatedImports(body)
	if !ok {
		return body
	}
	type edit struct {
		start, end int
		text       string
	}
	edits := []edit{}
	for _, spec := range specs {
		if !logicalPaths[spec.Path] {
			continue
		}
		pathText := strconv.Quote(spec.Path)
		offset := strings.Index(body[spec.Start:spec.End], pathText)
		if offset < 0 {
			continue
		}
		start := spec.Start + offset
		logicalPath := strings.ReplaceAll(spec.Path, ".", "/")
		edits = append(edits, edit{start, start + len(pathText), strconv.Quote(modulePath + "/" + logicalPath)})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, change := range edits {
		body = body[:change.start] + change.text + body[change.end:]
	}
	return body
}

func rewriteOfficialImports(body, modulePath string) string {
	if modulePath == "" {
		return body
	}
	imports, _, ok := generatedImports(body)
	if !ok {
		return body
	}
	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	for _, spec := range imports {
		if !strings.HasPrefix(spec.Path, "gpp/") {
			continue
		}
		pathStart := strings.Index(body[spec.Start:spec.End], strconv.Quote(spec.Path))
		if pathStart < 0 {
			continue
		}
		start := spec.Start + pathStart
		end := start + len(strconv.Quote(spec.Path))
		edits = append(edits, edit{start: start, end: end, text: strconv.Quote(modulePath + "/" + spec.Path)})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, change := range edits {
		body = body[:change.start] + change.text + body[change.end:]
	}
	return body
}

func ensureGeneratedImports(body string, context constructorContext) string {
	if len(context.AvailableImports) == 0 {
		return body
	}
	imports, _, ok := generatedImports(body)
	if !ok {
		return body
	}
	existing := map[string]bool{}
	for _, spec := range imports {
		existing[spec.Alias] = true
	}

	aliases := make([]string, 0)
	for alias := range context.AvailableImports {
		if existing[alias] || !containsPackageSelector(body, alias) {
			continue
		}
		aliases = append(aliases, alias)
	}
	if len(aliases) == 0 {
		return body
	}
	sort.Strings(aliases)
	var importBlock strings.Builder
	importBlock.WriteString("import (\n")
	for _, alias := range aliases {
		importPath := context.AvailableImports[alias]
		if alias == path.Base(importPath) {
			fmt.Fprintf(&importBlock, "\t%q\n", importPath)
		} else {
			fmt.Fprintf(&importBlock, "\t%s %q\n", alias, importPath)
		}
	}
	importBlock.WriteString(")\n\n")
	return importBlock.String() + body
}

func containsPackageSelector(body, alias string) bool {
	tokens, err := LexSource("generated Go", body)
	if err != nil {
		return false
	}
	for index := 0; index+2 < len(tokens); index++ {
		if tokens[index].Text == alias && tokens[index+1].Text == "." &&
			(tokens[index+2].Kind == TokenIdentifier || tokens[index+2].Kind == TokenKeyword) {
			return true
		}
	}
	return false
}

// emitSourceDirective attaches a generated declaration to its Go++ source
// location. Go's compiler understands these directives and will report
// backend errors against the original source file instead of the generated
// workspace.
func emitSourceDirective(out *strings.Builder, sourcePath string, line int) {
	if sourcePath == "" || line <= 0 {
		return
	}
	sourcePath = filepath.ToSlash(sourcePath)
	fmt.Fprintf(out, "//line %s:%d\n", sourcePath, line)
}

func sourceDirectivePath(file *File) string {
	if file == nil {
		return ""
	}
	sourcePath := file.SourcePath
	if sourcePath == "" {
		sourcePath = file.Name
	}
	if absolute, err := filepath.Abs(sourcePath); err == nil {
		sourcePath = absolute
	}
	return filepath.ToSlash(sourcePath)
}

func emitDecls(file *File, context constructorContext, interpolationName string) (string, error) {
	var out strings.Builder
	enumErrorAlias, _ := enumErrorImport(file)
	enumJSONAlias, _ := enumJSONImport(file)
	enumDriverAlias, _ := enumDriverImport(file)

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *EnumDecl:
			emitSourceDirective(&out, sourceDirectivePath(file), d.SourceLine)
			emitEnum(&out, d, enumErrorAlias, enumJSONAlias, enumDriverAlias)

		case *MixedDecl:
			if structured, ok, err := emitStructuredMixedFunctions(file, d, context, interpolationName); err != nil {
				return "", err
			} else if ok {
				out.WriteString(structured)
				continue
			}
			if structured, ok, err := emitStructuredMixedGoDecls(file, d, context, interpolationName); err != nil {
				return "", err
			} else if ok {
				out.WriteString(structured)
				continue
			}
			return "", fmt.Errorf("%s:%d: declaration has no structured AST representation", d.SourceFile, d.SourceLine)

		case *FunctionDecl:
			code, err := transformTopLevelDeclSource(d, context, interpolationName)
			if err != nil {
				return "", err
			}
			if d.Name == "main" && file.Package == "main" && context.AtExit {
				code, err = injectAtExitDefer(code)
				if err != nil {
					return "", err
				}
			}
			cronRegistration, err := cronFunctionRegistrationSource(d, context)
			if err != nil {
				return "", err
			}
			emitSourceDirective(&out, sourceDirectivePath(file), d.SourceLine)
			out.WriteString(code)
			if !strings.HasSuffix(code, "\n") {
				out.WriteByte('\n')
			}
			out.WriteString(cronRegistration)

		case *GoDecl:
			structured, err := emitStructuredGoDecls(file, d, context, interpolationName)
			if err != nil {
				return "", err
			}
			out.WriteString(structured)

		case *ValueDecl:
			if direct, handled, err := directValueDeclSource(d, context); handled {
				if err != nil {
					return "", err
				}
				emitSourceDirective(&out, sourceDirectivePath(file), d.SourceLine)
				out.WriteString(direct)
				if !strings.HasSuffix(direct, "\n") {
					out.WriteByte('\n')
				}
				continue
			}
			source, err := valueDeclASTSource(d)
			if err != nil {
				return "", err
			}
			code, err := transformGoDeclSource(source, context, interpolationName)
			if err != nil {
				return "", err
			}
			emitSourceDirective(&out, sourceDirectivePath(file), d.SourceLine)
			out.WriteString(code)
			if !strings.HasSuffix(code, "\n") {
				out.WriteByte('\n')
			}

		case *ClassDecl:
			if err := emitClass(&out, file, d, context, interpolationName); err != nil {
				return "", err
			}

		case *ExtendDecl:
			if err := emitExtension(&out, file, d, context, interpolationName); err != nil {
				return "", err
			}
		}
	}

	return out.String(), nil
}

// emitStructuredGoDecls emits ordinary Go declarations from their parsed
// syntax trees. GoDecl retains the AST as the source representation; the
// owning File.Source buffer is only needed for lossless diagnostics and source
// mapping, not to recover executable declaration text during emission.
func emitStructuredGoDecls(file *File, declaration *GoDecl, context constructorContext, interpolationName string) (string, error) {
	if declaration == nil || len(declaration.Declarations) == 0 {
		return "", nil
	}
	fileSet := declaration.FileSet
	if fileSet == nil {
		fileSet = token.NewFileSet()
	}
	var output strings.Builder
	for _, node := range declaration.Declarations {
		if node == nil {
			continue
		}
		var source strings.Builder
		if err := format.Node(&source, fileSet, node); err != nil {
			return "", err
		}
		code := source.String()
		if goDeclNeedsLowering(declaration, node, context) {
			transformed, err := transformGoDeclSource(code, context, interpolationName)
			if err != nil {
				return "", err
			}
			code = transformed
		}
		line := declaration.SourceLine
		if position := fileSet.Position(node.Pos()); position.IsValid() && position.Line > 1 {
			line += position.Line - 2
		}
		emitSourceDirective(&output, sourceDirectivePath(file), line)
		output.WriteString(code)
		if !strings.HasSuffix(code, "\n") {
			output.WriteByte('\n')
		}
	}
	return output.String(), nil
}

// emitStructuredMixedGoDecls handles compatibility declarations whose Go
// portion is already represented by go/ast. Formatting those nodes directly
// avoids recovering the declaration from File.Source merely because the
// parser retained a MixedDecl container for annotation/trivia compatibility.
func emitStructuredMixedGoDecls(file *File, declaration *MixedDecl, context constructorContext, interpolationName string) (string, bool, error) {
	if declaration == nil || len(declaration.Functions) > 0 || len(declaration.GoASTDecls) == 0 {
		return "", false, nil
	}
	fileSet := declaration.GoASTFileSet
	if fileSet == nil {
		fileSet = token.NewFileSet()
	}
	var output strings.Builder
	for _, node := range declaration.GoASTDecls {
		if node == nil {
			continue
		}
		var source strings.Builder
		if err := format.Node(&source, fileSet, node); err != nil {
			return "", false, err
		}
		code := source.String()
		if goASTNodeNeedsLowering(node, declaration.AnnotationPlacements, context) {
			transformed, err := transformGoDeclSource(code, context, interpolationName)
			if err != nil {
				return "", false, err
			}
			code = transformed
		}
		emitSourceDirective(&output, sourceDirectivePath(file), declaration.SourceLine)
		output.WriteString(code)
		if !strings.HasSuffix(code, "\n") {
			output.WriteByte('\n')
		}
	}
	return output.String(), true, nil
}

// goDeclNeedsLowering keeps ordinary Go declarations on their existing Go AST
// all the way to the formatting boundary. A GoDecl can still contain
// Go++-compatible syntax that Go's parser accepts as ordinary identifiers or
// calls, so only those concrete markers opt it into the compatibility passes.
func goDeclNeedsLowering(declaration *GoDecl, node ast.Decl, context constructorContext) bool {
	if declaration != nil && len(declaration.AnnotationPlacements) > 0 {
		return true
	}
	if declaration != nil {
		for _, token := range declaration.Tokens {
			switch token.Text {
			case "record", "let", "try", "throw", "??", "?.", "=>":
				return true
			}
		}
		if tokensNeedInterpolationLowering(declaration.Tokens) {
			return true
		}
	}
	needs := false
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			return !needs
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if ok && context.Targets[identifier.Name].Class != nil {
			needs = true
			return false
		}
		return !needs
	})
	return needs
}

// goASTNodeNeedsLowering applies the narrow feature check directly to a single
// Go AST declaration inside a MixedDecl. MixedDecl.Tokens cover the whole
// chunk and may include Go++ syntax from a neighboring function; using them
// here would needlessly route otherwise ordinary Go declarations through every
// source compatibility pass. Walking the AST also avoids formatting the node
// merely to lex it again.
func goASTNodeNeedsLowering(node ast.Decl, placements []AnnotationPlacement, context constructorContext) bool {
	if node == nil {
		return false
	}
	if len(placements) > 0 {
		return true
	}
	needs := false
	ast.Inspect(node, func(current ast.Node) bool {
		if needs {
			return false
		}
		switch value := current.(type) {
		case *ast.BasicLit:
			if value.Kind == token.STRING && len(value.Value) >= 2 {
				content := value.Value[1 : len(value.Value)-1]
				needs = interpolationLiteralHasExpression(content) || strings.Contains(content, "{{{{") || strings.Contains(content, "}}}}")
			}
		case *ast.CallExpr:
			identifier, ok := value.Fun.(*ast.Ident)
			if ok && context.Targets[identifier.Name].Class != nil {
				needs = true
			}
		}
		return !needs
	})
	return needs
}

// emitStructuredMixedFunctions keeps compatibility declarations from sending
// already-parsed top-level functions back through the grouped source rewriter.
// A MixedDecl may still contain a non-function remainder (for example an
// unsupported grouped declaration), so only the exact function spans are
// routed through the structured FunctionDecl path.
func emitStructuredMixedFunctions(file *File, declaration *MixedDecl, context constructorContext, interpolationName string) (string, bool, error) {
	if file == nil || declaration == nil || declaration.Owner != file || len(declaration.Functions) == 0 {
		return "", false, nil
	}
	// parseGoDeclarations succeeds only when the complete mixed chunk is valid
	// ordinary Go after annotation syntax is masked. In that case every
	// non-function declaration already has a real go/ast node, so do not recover
	// it from File.Source. This is especially important for a Go++ function next
	// to Go variables/imports: the function body is lowered structurally while
	// the surrounding Go declarations stay structurally owned by go/ast.
	if len(declaration.GoASTDecls) > 0 && declaration.GoASTFileSet != nil {
		type item struct {
			line     int
			order    int
			function *FunctionDecl
			goDecl   ast.Decl
		}
		items := make([]item, 0, len(declaration.Functions)+len(declaration.GoASTDecls))
		order := 0
		for _, function := range declaration.Functions {
			if function == nil {
				continue
			}
			items = append(items, item{line: function.SourceLine, order: order, function: function})
			order++
		}
		for _, node := range declaration.GoASTDecls {
			if node == nil {
				continue
			}
			if _, isFunction := node.(*ast.FuncDecl); isFunction {
				continue
			}
			line := declaration.SourceLine
			if position := declaration.GoASTFileSet.Position(node.Pos()); position.IsValid() && position.Line > 1 {
				line += position.Line - 2
			}
			items = append(items, item{line: line, order: order, goDecl: node})
			order++
		}
		if len(items) > 0 {
			sort.SliceStable(items, func(left, right int) bool {
				if items[left].line != items[right].line {
					return items[left].line < items[right].line
				}
				return items[left].order < items[right].order
			})
			var output strings.Builder
			for _, current := range items {
				if current.function != nil {
					code, err := transformTopLevelDeclSource(current.function, context, interpolationName)
					if err != nil {
						return "", false, err
					}
					emitSourceDirective(&output, sourceDirectivePath(file), current.function.SourceLine)
					output.WriteString(code)
					if !strings.HasSuffix(code, "\n") {
						output.WriteByte('\n')
					}
					continue
				}
				var source strings.Builder
				if err := format.Node(&source, declaration.GoASTFileSet, current.goDecl); err != nil {
					return "", false, err
				}
				code := source.String()
				if goASTNodeNeedsLowering(current.goDecl, declaration.AnnotationPlacements, context) {
					transformed, err := transformGoDeclSource(code, context, interpolationName)
					if err != nil {
						return "", false, err
					}
					code = transformed
				}
				emitSourceDirective(&output, sourceDirectivePath(file), current.line)
				output.WriteString(code)
				if !strings.HasSuffix(code, "\n") {
					output.WriteByte('\n')
				}
			}
			return output.String(), true, nil
		}
	}

	// If the Go parser could not represent the complete mixed chunk, retain the
	// lossless source-span fallback for the unsupported remainder. This branch
	// is compatibility behavior, not the normal AST emission path.
	start := declaration.SourceSpan.Start
	end := declaration.SourceSpan.End
	if start < 0 || end < start || end > len(file.Source) {
		return "", false, nil
	}

	var output strings.Builder
	cursor := start
	for _, function := range declaration.Functions {
		if function == nil || function.SourceSpan.Start < cursor || function.SourceSpan.End < function.SourceSpan.Start || function.SourceSpan.End > end {
			return "", false, nil
		}
		if cursor < function.SourceSpan.Start && strings.TrimSpace(file.Source[cursor:function.SourceSpan.Start]) != "" {
			segment, err := transformGoDeclSource(file.Source[cursor:function.SourceSpan.Start], context, interpolationName)
			if err != nil {
				return "", false, err
			}
			emitSourceDirective(&output, sourceDirectivePath(file), sourceLine(file.Source, cursor))
			output.WriteString(segment)
			if !strings.HasSuffix(segment, "\n") {
				output.WriteByte('\n')
			}
		}

		functionSourceText, err := transformTopLevelDeclSource(function, context, interpolationName)
		if err != nil {
			return "", false, err
		}
		emitSourceDirective(&output, sourceDirectivePath(file), function.SourceLine)
		output.WriteString(functionSourceText)
		if !strings.HasSuffix(functionSourceText, "\n") {
			output.WriteByte('\n')
		}
		cursor = function.SourceSpan.End
	}

	if cursor < end && strings.TrimSpace(file.Source[cursor:end]) != "" {
		segment, err := transformGoDeclSource(file.Source[cursor:end], context, interpolationName)
		if err != nil {
			return "", false, err
		}
		emitSourceDirective(&output, sourceDirectivePath(file), sourceLine(file.Source, cursor))
		output.WriteString(segment)
		if !strings.HasSuffix(segment, "\n") {
			output.WriteByte('\n')
		}
	}
	return output.String(), true, nil
}

func transformGoDeclSource(source string, context constructorContext, interpolationName string) (string, error) {
	code, err := transformInterpolationWithNameChecked(source, interpolationName)
	if err != nil {
		return "", err
	}
	code = stripAnnotationSyntaxPreserve(code)
	code, err = transformEnums(code, context)
	if err != nil {
		return "", err
	}
	code, err = transformImplicitErrorPromotion(code, context)
	if err != nil {
		return "", err
	}
	code, err = transformExceptions(code, context)
	if err != nil {
		return "", err
	}
	code, err = transformLambdas(code, context)
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
	code, err = transformErrorCoalescing(code, context)
	if err != nil {
		return "", err
	}
	code, err = transformSafeAccess(code, context)
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
	code, err = transformStaticTemplateCalls(code, context)
	if err != nil {
		return "", err
	}
	return transformExceptionABIBoundaries(code, context)
}

// transformTopLevelDeclSource is the transitional lowering path for a
// structured FunctionDecl. The declaration and body are already represented
// by FunctionDecl.Method and its BodyAST; this helper is kept isolated so the
// source-rewrite stages can be replaced by AST lowering without reintroducing
// MixedDecl storage.
func transformTopLevelDeclSource(function *FunctionDecl, context constructorContext, interpolationName string) (string, error) {
	if direct, handled, err := directFunctionSource(function, context); handled {
		return direct, err
	}
	source := functionSource(function)
	if err := prepareRecordContextForFunction(function, context); err != nil {
		return "", err
	}
	code, err := transformInterpolationInFunctionSource(source, function, interpolationName)
	if err != nil {
		return "", err
	}
	code = stripAnnotationSyntaxPreserve(code)
	code, err = transformEnums(code, context)
	if err != nil {
		return "", err
	}
	code, err = transformImplicitErrorPromotion(code, context)
	if err != nil {
		return "", err
	}
	if transformed, handled, exceptionErr := transformExceptionMethodBodyAST(code, &function.Method, context); handled {
		if exceptionErr != nil {
			return "", exceptionErr
		}
		code = transformed
	} else {
		code, err = transformExceptions(code, context)
		if err != nil {
			return "", err
		}
	}
	code, err = transformLambdas(code, context)
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
	code, err = transformErrorCoalescing(code, context)
	if err != nil {
		return "", err
	}
	code, err = transformSafeAccessInFunctionSource(code, function, context)
	if err != nil {
		return "", err
	}
	code, err = transformSafeAccess(code, context)
	if err != nil {
		return "", err
	}
	code, err = transformPolymorphicDeclarations(code, context)
	if err != nil {
		return "", err
	}
	code, err = transformEnums(code, context)
	if err != nil {
		return "", err
	}
	code, err = transformIntrospectionInFunctionSource(code, context)
	if err != nil {
		return "", err
	}
	code, err = transformExtensions(code, context)
	if err != nil {
		return "", err
	}
	// Overload resolution in a top-level function needs the function's
	// parameter types to distinguish Go++ class receivers from unrelated Go
	// selectors (for example, ctx.Text versus ResponseWriter.Write).
	overloadContext := context.Overloads
	overloadContext.LocalTypes = parameterTypeMapFromNodes(function.Method.ParameterAST)
	code, err = transformOverloads(code, overloadContext)
	if err != nil {
		return "", err
	}
	code, err = transformStaticTemplateCalls(code, context)
	if err != nil {
		return "", err
	}
	return transformExceptionABIBoundaries(code, context)
}

func emitClass(out *strings.Builder, file *File, class *ClassDecl, context constructorContext, interpolationName string) error {
	sourcePath := sourceDirectivePath(file)
	emitSourceDirective(out, sourcePath, class.SourceLine)
	classTypeParams, err := goTypeParameterNodesSource(class.TypeParamsAST)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "type %s%s struct {\n", class.Name, classTypeParams)

	for _, parent := range classParentNames(class) {
		fmt.Fprintf(out, "\t%s\n", parent)
	}

	for _, field := range class.Fields {
		fieldType := transformPolymorphicType(fieldTypeSource(field), context)
		fmt.Fprintf(
			out,
			"\t%s %s%s\n",
			field.Name,
			fieldType,
			serializationFieldTag(field.Annotations),
		)
	}
	if context.Introspection != nil && context.Introspection.Enabled {
		out.WriteString("\tGppDynamicClass *GppClass `json:\"-\" yaml:\"-\"`\n")
		out.WriteString("\tGppDynamicObject any `json:\"-\" yaml:\"-\"`\n")
	}

	out.WriteString("}\n\n")
	if len(class.TypeParamsAST) == 0 {
		if err := emitGeneratedGobSupport(out, class, context); err != nil {
			return err
		}
	}

	classes := classesForClass(context, class)
	methods, err := interfaceMethods(class, classes, map[string]bool{})
	if err != nil {
		return err
	}

	for _, method := range class.Methods {
		if !method.IsStatic {
			continue
		}
		if err := emitStaticMethod(out, sourcePath, class, method, context, interpolationName); err != nil {
			return err
		}
	}

	for methodIndex := range methods {
		method := methods[methodIndex]
		if method.Generated && isDefaultObjectMethod(method) {
			if len(class.TypeParamsAST) != 0 {
				continue
			}
			emitDefaultObjectMethod(out, class, method, context)
			continue
		}
		if parentName, ok := importedParentForMethod(class, method, classes); ok {
			if err := emitImportedInheritedMethod(out, class, parentName, method, context); err != nil {
				return err
			}
			continue
		}
		emitSourceDirective(out, sourcePath, class.SourceLine)
		methodName := methodOutputName(method)
		exceptionContext := contextForMethod(context, class, method.Name)
		if direct, handled, err := directMethodSource(class, method, exceptionContext); handled {
			if err != nil {
				return err
			}
			out.WriteString(direct)
			out.WriteByte('\n')
			continue
		}
		body, err := transformMethodInterpolation(method, interpolationName)
		if err != nil {
			return err
		}
		body, err = transformEnums(body, context)
		if err != nil {
			return err
		}
		body, err = transformImplicitErrorPromotion(body, context)
		if err != nil {
			return err
		}
		if transformed, handled, exceptionErr := transformExceptionMethodBodyAST(body, &method, exceptionContext); handled {
			if exceptionErr != nil {
				return exceptionErr
			}
			body = transformed
		} else {
			body, err = transformExceptions(body, context)
			if err != nil {
				return err
			}
		}
		methodResult, body, err := transformRecordMethodResult(methodResultSource(method), body, contextForMethod(context, class, method.Name))
		if err != nil {
			return err
		}
		parameters, err := transformParameterList(methodParametersSource(method), context)
		if err != nil {
			return err
		}
		fmt.Fprintf(
			out,
			"func (this %s) %s(%s)",
			methodReceiverType(class, method),
			methodName,
			parameters,
		)

		result := transformPolymorphicResultType(strings.TrimSpace(methodResult), context)
		if result != "" {
			fmt.Fprintf(out, " %s", result)
		}

		out.WriteString(" {\n")

		methodContext := context
		methodContext.CurrentClass = class.Name
		methodContext.CurrentMethod = method.Name
		methodContext.CurrentResultAST = methodResultTypeNode(method)
		methodContext.MethodSignatures = context.ClassMethodSignatures[class.Name]
		methodContext.CurrentParameterTypes = parameterTypeMapFromNodes(method.ParameterAST)
		methodContext.CurrentParameterTypes["this"] = "*" + class.Name + typeParameterNames(class.TypeParamsAST)
		body, err = transformLambdas(body, methodContext)
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
		body, err = transformErrorCoalescing(body, context)
		if err != nil {
			return err
		}
		body, err = transformSafeAccess(body, methodContext)
		if err != nil {
			return err
		}
		body, err = transformPolymorphicDeclarations(body, methodContext)
		if err != nil {
			return err
		}
		body, err = transformEnums(body, methodContext)
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
		body, err = transformErrorCoalescing(body, methodContext)
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
		methodContext.Overloads.CurrentClass = class.Name
		methodContext.Overloads.LocalTypes = parameterTypeMapFromNodes(method.ParameterAST)
		body, err = transformOverloads(body, methodContext.Overloads)
		if err != nil {
			return err
		}
		body, _ = wrapExceptionBoundaryBody(body, methodResult, methodContext)
		body = qualifyImplicitClassFields(body, class, classesForClass(context, class), method)

		out.WriteString(body)

		out.WriteString("\n}\n\n")
	}

	// Runtime class descriptors describe concrete field and method types. A
	// generic declaration has no concrete Go type until instantiated.
	if len(class.TypeParamsAST) != 0 {
		return nil
	}

	fmt.Fprintf(out, "type __gpp_%s interface {\n", class.Name)
	out.WriteString("\tGppRuntimeClass() *GppClass\n")
	for _, method := range methods {
		parameterSource := methodParametersSource(method)
		resultSource := methodResultSource(method)
		if parentName, imported := importedParentForMethod(class, method, classes); imported {
			parameterSource = qualifyImportedTypeNames(parameterSource, parentName, classes, context.ImportedTypes)
			resultSource = qualifyImportedTypeNames(resultSource, parentName, classes, context.ImportedTypes)
		}
		parameters, err := transformParameterList(parameterSource, context)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "\t%s(%s)", methodOutputName(method), parameters)
		result := transformPolymorphicResultType(strings.TrimSpace(resultSource), context)
		if result != "" {
			fmt.Fprintf(out, " %s", result)
		}
		out.WriteByte('\n')
	}
	out.WriteString("}\n\n")
	fmt.Fprintf(out, "type Gpp%s = __gpp_%s\n\n", class.Name, class.Name)
	fmt.Fprintf(out, "func (this %s) GppRuntimeClass() *GppClass {\n", class.Name)
	if context.Introspection != nil && context.Introspection.Enabled {
		out.WriteString("\tif this.GppDynamicClass != nil { return this.GppDynamicClass }\n")
	}
	fmt.Fprintf(out, "\treturn Gpp%sClass\n}\n\n", class.Name)
	if err := emitClassDescriptor(out, class, classes, context); err != nil {
		return err
	}

	return nil
}

func isDefaultObjectMethod(method Method) bool {
	return !method.IsStatic && methodParametersSource(method) == "" && methodResultSource(method) == "string" &&
		(method.Name == "String" || method.Name == "Dump")
}

func methodReceiverType(class *ClassDecl, method Method) string {
	classType := class.Name + typeParameterNames(class.TypeParamsAST)
	if method.Name == "Error" && !method.IsStatic && methodParametersSource(method) == "" && strings.TrimSpace(methodResultSource(method)) == "string" {
		return classType
	}
	return "*" + classType
}

func typeParameterNames(parameters []TypeParameterNode) string {
	if len(parameters) == 0 {
		return ""
	}
	names := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		names = append(names, parameter.Name)
	}
	return "[" + strings.Join(names, ", ") + "]"
}

func emitDefaultObjectMethod(out *strings.Builder, class *ClassDecl, method Method, context constructorContext) {
	pretty := method.Name == "Dump"
	formatter := "__gppFormatObject"
	if context.IntrospectionRuntimeImport != "" {
		formatter = "gppRuntime.FormatObject"
	}
	fmt.Fprintf(out, "func (this %s) %s() string {\n", class.Name+typeParameterNames(class.TypeParamsAST), method.Name)
	fmt.Fprintf(out, "\treturn %s(Gpp%sClass, this, %t)\n", formatter, class.Name, pretty)
	out.WriteString("}\n\n")
}

// importedParentForMethod identifies inherited methods whose implementation
// belongs to another generated package. Their bodies must remain in that
// package so references to package-local helpers and imports stay valid.
func importedParentForMethod(class *ClassDecl, method Method, classes map[string]*ClassDecl) (string, bool) {
	if classDeclaresMethod(class, method) {
		return "", false
	}
	for _, parentName := range classParentNames(class) {
		if !strings.Contains(parentName, ".") {
			continue
		}
		parent := classes[parentName]
		if parent == nil {
			continue
		}
		parentMethods, err := interfaceMethods(parent, classes, map[string]bool{})
		if err != nil {
			continue
		}
		for _, parentMethod := range parentMethods {
			if methodSignatureKey(parentMethod) == methodSignatureKey(method) {
				return parentName, true
			}
		}
	}
	return "", false
}

func classDeclaresMethod(class *ClassDecl, method Method) bool {
	key := methodSignatureKey(method)
	for _, declared := range class.Methods {
		if methodSignatureKey(declared) == key {
			return true
		}
	}
	return false
}

func methodSignatureKey(method Method) string {
	if method.Owner != nil {
		parts := make([]string, 0, len(method.ParameterAST))
		for _, parameter := range method.ParameterAST {
			parts = append(parts, strings.Join(strings.Fields(typeNodeSignatureKey(parameter.Type)), " "))
		}
		return method.Name + "/" + strings.Join(parts, ",")
	}
	// Compatibility callers may still construct a Method without an owner.
	// Parsed methods always take the typed branch above.
	return method.Name + "/" + parameterSignatureKeyFromSource(methodParametersSource(method))
}

func parameterSignatureKeyFromSource(params string) string {
	parameters, err := parseParameterInfos(params)
	if err != nil {
		return strings.TrimSpace(params)
	}
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		parts = append(parts, strings.TrimSpace(parameter.typeText()))
	}
	return strings.Join(parts, ",")
}

func emitImportedInheritedMethod(out *strings.Builder, class *ClassDecl, parentName string, method Method, context constructorContext) error {
	parameters, err := parameterInfosForMethod(method)
	if err != nil {
		return err
	}
	parameterParts := make([]string, 0, len(parameters))
	arguments := make([]string, 0, len(parameters))
	for index, parameter := range parameters {
		name := parameter.Name
		if name == "" {
			name = fmt.Sprintf("arg%d", index)
		}
		parameterType := qualifyImportedTypeNames(parameter.typeText(), parentName, classesForClass(context, class), context.ImportedTypes)
		parameterParts = append(parameterParts, name+" "+transformPolymorphicType(parameterType, context))
		argument := name
		if strings.HasPrefix(parameter.typeText(), "...") {
			argument += "..."
		}
		arguments = append(arguments, argument)
	}

	methodName := methodOutputName(method)
	resultType := qualifyImportedTypeNames(methodResultSource(method), parentName, classesForClass(context, class), context.ImportedTypes)
	result := transformPolymorphicResultType(strings.TrimSpace(resultType), context)
	fieldName := classParentFieldName(parentName)
	fmt.Fprintf(out, "func (this *%s%s) %s(%s)", class.Name, typeParameterNames(class.TypeParamsAST), methodName, strings.Join(parameterParts, ", "))
	if result != "" {
		fmt.Fprintf(out, " %s", result)
	}
	out.WriteString(" {\n")
	fmt.Fprintf(out, "\tthis.%s.GppDynamicObject = this\n", fieldName)
	call := fmt.Sprintf("this.%s.%s(%s)", fieldName, methodName, strings.Join(arguments, ", "))
	if result != "" {
		fmt.Fprintf(out, "\treturn %s\n", call)
	} else {
		fmt.Fprintf(out, "\t%s\n", call)
	}
	out.WriteString("}\n\n")
	return nil
}

func qualifyImportedTypeNames(typeName, parentName string, classes map[string]*ClassDecl, importedTypes map[string]map[string]bool) string {
	dot := strings.Index(parentName, ".")
	if dot <= 0 {
		return typeName
	}
	qualifier := parentName[:dot]
	for name := range classes {
		if name == "" {
			continue
		}
		typeName = qualifyImportedTypeIdentifier(typeName, name, qualifier+"."+name)
	}
	for name := range importedTypes[qualifier] {
		typeName = qualifyImportedTypeIdentifier(typeName, name, qualifier+"."+name)
	}
	return typeName
}

func qualifyImportedTypeNode(typeNode TypeNode, parentName string, classes map[string]*ClassDecl, importedTypes map[string]map[string]bool) TypeNode {
	if typeNode == nil {
		return nil
	}
	dot := strings.Index(parentName, ".")
	if dot <= 0 {
		return typeNode
	}
	qualifier := parentName[:dot]
	qualifyName := func(name *NamedType) *NamedType {
		if name == nil {
			return nil
		}
		parts := append([]string(nil), name.Parts...)
		if len(parts) == 1 {
			_, local := classes[parts[0]]
			_, imported := importedTypes[qualifier][parts[0]]
			if local || imported {
				parts = []string{qualifier, parts[0]}
			}
		}
		arguments := make([]TypeNode, len(name.Arguments))
		for index, argument := range name.Arguments {
			arguments[index] = qualifyImportedTypeNode(argument, parentName, classes, importedTypes)
		}
		return &NamedType{Parts: parts, Arguments: arguments, SpanValue: name.SpanValue}
	}
	qualifyList := func(values []TypeNode) []TypeNode {
		result := make([]TypeNode, len(values))
		for index, value := range values {
			result[index] = qualifyImportedTypeNode(value, parentName, classes, importedTypes)
		}
		return result
	}
	switch value := typeNode.(type) {
	case *NamedType:
		return qualifyName(value)
	case *PointerType:
		return &PointerType{Element: qualifyImportedTypeNode(value.Element, parentName, classes, importedTypes), SpanValue: value.SpanValue}
	case *SliceType:
		return &SliceType{Element: qualifyImportedTypeNode(value.Element, parentName, classes, importedTypes), SpanValue: value.SpanValue}
	case *ArrayType:
		return &ArrayType{Length: value.Length, Element: qualifyImportedTypeNode(value.Element, parentName, classes, importedTypes), Ellipsis: value.Ellipsis, SpanValue: value.SpanValue}
	case *MapType:
		return &MapType{Key: qualifyImportedTypeNode(value.Key, parentName, classes, importedTypes), Value: qualifyImportedTypeNode(value.Value, parentName, classes, importedTypes), SpanValue: value.SpanValue}
	case *ChannelType:
		return &ChannelType{Direction: value.Direction, Element: qualifyImportedTypeNode(value.Element, parentName, classes, importedTypes), SpanValue: value.SpanValue}
	case *VariadicType:
		return &VariadicType{Element: qualifyImportedTypeNode(value.Element, parentName, classes, importedTypes), SpanValue: value.SpanValue}
	case *FunctionType:
		parameters := append([]ParameterNode(nil), value.Parameters...)
		for index := range parameters {
			parameters[index].Type = qualifyImportedTypeNode(parameters[index].Type, parentName, classes, importedTypes)
		}
		return &FunctionType{Parameters: parameters, Results: qualifyList(value.Results), SpanValue: value.SpanValue}
	case *StructType:
		fields := append([]StructFieldNode(nil), value.Fields...)
		for index := range fields {
			fields[index].Type = qualifyImportedTypeNode(fields[index].Type, parentName, classes, importedTypes)
		}
		return &StructType{Fields: fields, SpanValue: value.SpanValue}
	case *InterfaceType:
		interfaceNode := &InterfaceType{Embeds: qualifyList(value.Embeds), Methods: append([]InterfaceMethodNode(nil), value.Methods...), SpanValue: value.SpanValue}
		for index := range interfaceNode.Methods {
			interfaceNode.Methods[index].Signature = qualifyImportedTypeNode(interfaceNode.Methods[index].Signature, parentName, classes, importedTypes)
		}
		return interfaceNode
	case *TupleType:
		return &TupleType{Elements: qualifyList(value.Elements), SpanValue: value.SpanValue}
	case *UnderlyingType:
		return &UnderlyingType{Element: qualifyImportedTypeNode(value.Element, parentName, classes, importedTypes), SpanValue: value.SpanValue}
	case *UnionType:
		return &UnionType{Terms: qualifyList(value.Terms), SpanValue: value.SpanValue}
	default:
		// TokenType is intentionally opaque. The source compatibility path
		// remains responsible for qualifying syntax not represented yet.
		return typeNode
	}
}

func qualifyImportedTypeIdentifier(source, name, replacement string) string {
	var out strings.Builder
	for index := 0; index < len(source); {
		if !isASCIIIdentStart(source[index]) {
			out.WriteByte(source[index])
			index++
			continue
		}
		end := index + 1
		for end < len(source) && isIdentPart(source[end]) {
			end++
		}
		word := source[index:end]
		if word == name && (index == 0 || source[index-1] != '.') {
			out.WriteString(replacement)
		} else {
			out.WriteString(word)
		}
		index = end
	}
	return out.String()
}

func isASCIIIdentStart(value byte) bool {
	return value == '_' || (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')
}

func transformParameterList(params string, context constructorContext) (string, error) {
	parameters, err := parseParameterInfos(params)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		typeName := transformPolymorphicType(parameter.typeText(), context)
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
		if method.IsStatic {
			continue
		}
		methods = append(methods, method)
		arity, err := parameterCount(methodParametersSource(method))
		if err != nil {
			return nil, err
		}
		seen[method.Name+fmt.Sprintf("/%d", arity)] = true
	}

	for _, parentName := range classParentNames(class) {
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
			arity, err := parameterCount(methodParametersSource(method))
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

func staticMethodGoName(class *ClassDecl, method Method) string {
	prefix := "__gpp_static_"
	if isExportedGoPlusName(method.Name) {
		prefix = "GppStatic_"
	}
	return prefix + class.Name + "_" + methodOutputName(method)
}

func isExportedGoPlusName(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func emitStaticMethod(out *strings.Builder, sourcePath string, class *ClassDecl, method Method, context constructorContext, interpolationName string) error {
	if direct, handled, err := directStaticMethodSource(class, method, context); handled {
		if err != nil {
			return err
		}
		emitSourceDirective(out, sourcePath, class.SourceLine)
		out.WriteString(direct)
		out.WriteByte('\n')
		return nil
	}
	body, err := transformMethodInterpolation(method, interpolationName)
	if err != nil {
		return err
	}
	body, err = transformEnums(body, context)
	if err != nil {
		return err
	}
	body, err = transformImplicitErrorPromotion(body, context)
	if err != nil {
		return err
	}
	if transformed, handled, exceptionErr := transformExceptionMethodBodyAST(body, &method, context); handled {
		if exceptionErr != nil {
			return exceptionErr
		}
		body = transformed
	} else {
		body, err = transformExceptions(body, context)
		if err != nil {
			return err
		}
	}
	methodResult, body, err := transformRecordMethodResult(
		methodResultSource(method),
		body,
		context,
	)
	if err != nil {
		return err
	}
	parameters, err := transformParameterList(methodParametersSource(method), context)
	if err != nil {
		return err
	}

	emitSourceDirective(out, sourcePath, class.SourceLine)
	methodTypeParams, err := goTypeParameterNodesSource(method.TypeParamsAST)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "func %s%s(%s)", staticMethodGoName(class, method), methodTypeParams, parameters)
	result := strings.TrimSpace(methodResult)
	if !method.Generated {
		result = transformPolymorphicResultType(result, context)
	}
	if result != "" {
		fmt.Fprintf(out, " %s", result)
	}
	out.WriteString(" {\n")

	methodContext := context
	methodContext.CurrentClass = ""
	methodContext.CurrentMethod = method.Name
	methodContext.CurrentResultAST = methodResultTypeNode(method)
	methodContext.MethodSignatures = nil
	methodContext.CurrentParameterTypes = parameterTypeMapFromNodes(method.ParameterAST)
	body, err = transformLambdas(body, methodContext)
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
	body, err = transformErrorCoalescing(body, context)
	if err != nil {
		return err
	}
	body, err = transformSafeAccess(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformPolymorphicDeclarations(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformEnums(body, methodContext)
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
	body, err = transformErrorCoalescing(body, methodContext)
	if err != nil {
		return err
	}
	body, err = transformOverloads(body, context.Overloads)
	if err != nil {
		return err
	}
	body, _ = wrapExceptionBoundaryBody(body, methodResult, methodContext)
	out.WriteString(body)
	out.WriteString("\n}\n\n")
	return nil
}

func contextForMethod(context constructorContext, class *ClassDecl, methodName string) constructorContext {
	methodContext := context
	methodContext.CurrentClass = class.Name
	methodContext.CurrentMethod = methodName
	methodContext.CurrentResultAST = nil
	methodContext.MethodSignatures = context.ClassMethodSignatures[class.Name]
	methodContext.Overloads.Methods = methodOverloadsForClass(class, classesForClass(context, class), map[string]bool{})
	methodContext.Overloads.MethodTypes = methodOverloadTypesForClass(class, classesForClass(context, class), map[string]bool{})
	methodContext.Overloads.CurrentClass = class.Name
	return methodContext
}

func classesForClass(context constructorContext, class *ClassDecl) map[string]*ClassDecl {
	classes := map[string]*ClassDecl{}
	if target, ok := context.Targets[class.Name]; ok {
		for name, value := range target.Classes {
			classes[name] = value
		}
	} else {
		classes[class.Name] = class
	}
	for name, target := range context.Targets {
		if target.Qualifier == "" {
			continue
		}
		classes[name] = target.Class
		for className, imported := range target.Classes {
			qualified := target.Qualifier + "." + className
			classes[qualified] = imported
			if _, exists := classes[className]; !exists {
				classes[className] = imported
			}
		}
	}
	return classes
}

func methodOverloadsForClass(class *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) map[string]map[int]string {
	if visiting[class.Name] {
		return map[string]map[int]string{}
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	overloads := map[string]map[int]string{}
	for _, method := range class.Methods {
		if method.GoName == "" || method.IsStatic {
			continue
		}
		arity, err := parameterCount(methodParametersSource(method))
		if err != nil {
			continue
		}
		if overloads[method.Name] == nil {
			overloads[method.Name] = map[int]string{}
		}
		overloads[method.Name][arity] = method.GoName
	}

	for _, parentName := range classParentNames(class) {
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
		if method.GoName == "" || method.IsStatic {
			continue
		}
		parameters, err := parameterInfosForMethod(method)
		if err != nil {
			continue
		}
		if overloads[method.Name] == nil {
			overloads[method.Name] = map[string]string{}
		}
		overloads[method.Name][parameterSignatureKey(parameters)] = method.GoName
	}
	for _, parentName := range classParentNames(class) {
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

func classFieldTypesForClass(class *ClassDecl, classes map[string]*ClassDecl, visiting map[string]bool) map[string]string {
	if class == nil || visiting[class.Name] {
		return map[string]string{}
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	fields := map[string]string{}
	for _, parentName := range classParentNames(class) {
		if parent, ok := classes[parentName]; ok {
			for name, typeName := range classFieldTypesForClass(parent, classes, visiting) {
				fields[name] = typeName
			}
		}
	}
	for _, field := range class.Fields {
		if typeName := strings.TrimSpace(fieldTypeSource(field)); typeName != "" {
			fields[field.Name] = typeName
		}
	}
	return fields
}

func transformPolymorphicDeclarations(src string, context constructorContext) (string, error) {
	if len(context.Targets) == 0 {
		return src, nil
	}
	if transformed, handled, err := transformPolymorphicDeclarationsAST(src, context); handled {
		return transformed, err
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
			if actual != "" && actual != parameter.typeText() &&
				!shouldPointerCoerce(parameter.typeText(), call.Args[index], context) {
				matches = false
				break
			}
		}
		if matches {
			result := make([]string, len(candidate.Parameters))
			for index, parameter := range candidate.Parameters {
				result[index] = parameter.typeText()
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
					typeName, err := astTypeSignatureKeyWithFallback(field.Type)
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
			if typeName == "" && len(declaration.Values) == 1 && len(declaration.Names) > 1 {
				if call, ok := declaration.Values[0].(*ast.CallExpr); ok {
					results := enumResultTypes(callResultType(call, context, result))
					if len(results) == len(declaration.Names) {
						for index, name := range declaration.Names {
							result[name.Name] = results[index]
						}
						break
					}
				}
			}
			for index, name := range declaration.Names {
				inferred := typeName
				if inferred == "" && index < len(declaration.Values) {
					inferred = expressionStaticType(declaration.Values[index], context, result)
					if len(declaration.Names) == 1 {
						parts := enumResultTypes(inferred)
						if len(parts) > 0 {
							inferred = parts[0]
						}
					}
				}
				if inferred != "" {
					result[name.Name] = inferred
				}
			}
		case *ast.RangeStmt:
			if value, ok := declaration.Value.(*ast.Ident); ok {
				if key, enum, exists := enumValuesReference(declaration.X, context); exists {
					result[value.Name] = enumReferencePrefix(key) + enumMetaTypeName(enum)
				}
			}
		case *ast.AssignStmt:
			if len(declaration.Rhs) == 1 && len(declaration.Lhs) > 1 {
				if call, ok := declaration.Rhs[0].(*ast.CallExpr); ok {
					results := enumResultTypes(callResultType(call, context, result))
					if len(results) == len(declaration.Lhs) {
						for index, left := range declaration.Lhs {
							if name, ok := left.(*ast.Ident); ok {
								result[name.Name] = results[index]
							}
						}
						break
					}
				}
			}
			for index, left := range declaration.Lhs {
				name, ok := left.(*ast.Ident)
				if !ok || index >= len(declaration.Rhs) {
					continue
				}
				if inferred := expressionStaticType(declaration.Rhs[index], context, result); inferred != "" {
					if len(declaration.Lhs) == 1 {
						parts := enumResultTypes(inferred)
						if len(parts) > 0 {
							inferred = parts[0]
						}
					}
					result[name.Name] = inferred
				}
			}
		}
		return true
	})
	for name, typeName := range context.CurrentParameterTypes {
		result[name] = typeName
	}
	return result
}

func expressionStaticType(expr ast.Expr, context constructorContext, valueTypes map[string]string) string {
	switch value := expr.(type) {
	case *ast.TypeAssertExpr:
		if typeName, err := astTypeSignatureKeyWithFallback(value.Type); err == nil {
			return strings.TrimSpace(typeName)
		}
	case *ast.UnaryExpr:
		if value.Op == token.AND {
			name := expressionStaticType(value.X, context, valueTypes)
			if name != "" && !strings.HasPrefix(name, "*") {
				return "*" + name
			}
			return name
		}
		if value.Op == token.ADD || value.Op == token.SUB || value.Op == token.XOR || value.Op == token.NOT {
			return expressionStaticType(value.X, context, valueTypes)
		}
	case *ast.ParenExpr:
		return expressionStaticType(value.X, context, valueTypes)
	case *ast.BinaryExpr:
		switch value.Op {
		case token.LAND, token.LOR, token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return "bool"
		default:
			return expressionStaticType(value.X, context, valueTypes)
		}
	case *ast.CallExpr:
		if result := extensionCallResultType(value, context, valueTypes); result != "" {
			return result
		}
		if result := callResultType(value, context, valueTypes); result != "" {
			return result
		}
		if resultTypes, ok := nativePackageResultTypes(value, context); ok && len(resultTypes) > 0 {
			return resultTypes[0]
		}
		if resultTypes, ok := nativeMethodResultTypes(value, context, valueTypes); ok && len(resultTypes) > 0 {
			return resultTypes[0]
		}
		if native, ok := nativePackageFunction(value, context); ok && len(native.types) > 0 {
			return native.types[0]
		}
		if native, ok := nativeMethodFunction(value, context, valueTypes); ok && len(native.types) > 0 {
			return native.types[0]
		}
	case *ast.SelectorExpr:
		if key, enum, ok := enumReference(value.X, context); ok {
			if _, exists := enumMember(enum, value.Sel.Name); exists {
				return enumReferenceType(key, enum)
			}
		}
		baseType := strings.TrimPrefix(strings.TrimSpace(expressionStaticType(value.X, context, valueTypes)), "*")
		if target, ok := context.Targets[baseType]; ok {
			for _, field := range target.Class.Fields {
				if field.Name == value.Sel.Name {
					return fieldTypeSource(field)
				}
			}
			for _, signature := range context.ClassMethodSignatures[baseType][value.Sel.Name] {
				if signature.resultText() != "" {
					return signature.resultText()
				}
			}
		}
		if nativeType := nativeSelectorFieldType(value, context, valueTypes); nativeType != "" {
			return nativeType
		}
	case *ast.Ident:
		if valueTypes != nil && valueTypes[value.Name] != "" {
			return valueTypes[value.Name]
		}
	}
	return astExpressionTypeKeyWithEnv(expr, valueTypes)
}

func nativeSelectorFieldType(selector *ast.SelectorExpr, context constructorContext, valueTypes map[string]string) string {
	baseType := expressionStaticType(selector.X, context, valueTypes)
	named, _, ok := nativeNamedType(baseType, context)
	if !ok {
		return ""
	}
	structure, ok := named.Underlying().(*types.Struct)
	if !ok {
		return ""
	}
	for index := 0; index < structure.NumFields(); index++ {
		field := structure.Field(index)
		if field.Name() != selector.Sel.Name || !field.Exported() {
			continue
		}
		return types.TypeString(field.Type(), nativeTypeQualifier(context))
	}
	return ""
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
		if function.Sel.Name == "From" {
			if key, enum, exists := enumReference(function.X, context); exists {
				return "(" + enumReferenceType(key, enum) + ", error)"
			}
		}
		if receiver, ok := function.X.(*ast.Ident); ok {
			if _, isClass := context.Targets[receiver.Name]; isClass {
				candidates = context.StaticMethodSignatures[receiver.Name][function.Sel.Name]
			} else {
				className := context.CurrentClass
				if receiver.Name != "this" {
					className = strings.TrimPrefix(valueTypes[receiver.Name], "*")
				}
				candidates = context.ClassMethodSignatures[className][function.Sel.Name]
			}
		}
	}

	for _, candidate := range candidates {
		if len(call.Args) < requiredParameterCount(candidate) || len(call.Args) > len(candidate.Parameters) {
			continue
		}
		matches := true
		for index, argument := range call.Args {
			actual := expressionStaticType(argument, context, valueTypes)
			expected := strings.Join(strings.Fields(candidate.Parameters[index].typeText()), " ")
			if actual != "" && actual != expected && !isAssignableStaticType(actual, expected, context) {
				matches = false
				break
			}
		}
		if matches {
			if function, ok := call.Fun.(*ast.SelectorExpr); ok {
				if receiver, ok := function.X.(*ast.Ident); ok {
					if _, isClass := context.Targets[receiver.Name]; isClass {
						return candidate.resultText()
					}
				}
			}
			return transformPolymorphicResultType(candidate.resultText(), context)
		}
	}
	if native, ok := nativePackageFunction(call, context); ok && len(native.types) > 0 {
		return native.types[0]
	}
	if native, ok := nativeMethodFunction(call, context, valueTypes); ok && len(native.types) > 0 {
		return native.types[0]
	}
	return ""
}

func isAssignableStaticType(actual, expected string, context constructorContext) bool {
	actualName := strings.TrimPrefix(strings.TrimSpace(actual), "*")
	expectedTarget, expectedIsClass := context.Targets[strings.TrimPrefix(strings.TrimSpace(expected), "*")]
	actualTarget, actualIsClass := context.Targets[actualName]
	if !expectedIsClass || !actualIsClass {
		return false
	}
	return classInheritsTarget(actualTarget, expectedTarget, context)
}

// classInheritsTarget follows both local and imported parents. An application
// class can therefore satisfy an interface-like class imported from a bundled
// package (for example, Employee : orm.Model).
func classInheritsTarget(actual, expected constructorTarget, context constructorContext) bool {
	visited := map[*ClassDecl]bool{}
	var visit func(constructorTarget) bool
	visit = func(current constructorTarget) bool {
		if current.Class == nil || visited[current.Class] {
			return false
		}
		visited[current.Class] = true
		if current.Class == expected.Class {
			return true
		}

		for _, parentName := range classParentNames(current.Class) {
			parent := constructorTarget{Classes: current.Classes, Qualifier: current.Qualifier}
			if local, ok := current.Classes[parentName]; ok {
				parent.Class = local
			} else if imported, ok := context.Targets[parentName]; ok {
				parent = imported
			} else if current.Qualifier != "" {
				parent, _ = context.Targets[current.Qualifier+"."+parentName]
			}
			if parent.Class != nil && visit(parent) {
				return true
			}
		}
		return false
	}

	return visit(actual)
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
	return len(classParentNames(class)) > 0 || classHasDerived(class, classes)
}

func dispatchInterfaceType(target constructorTarget) string {
	if target.Qualifier == "" {
		return target.InterfaceName
	}
	return target.Qualifier + ".Gpp" + target.Class.Name
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
	transformed, handled, err := transformOverloadsAST(src, overloads)
	if err != nil {
		return src, err
	}
	if handled {
		return transformed, nil
	}
	return src, nil
}

func overloadSourceHasTypedFunctionSignature(source string) bool {
	tokens, err := LexSource("overloads", source)
	if err != nil {
		return false
	}
	for index, token := range tokens {
		if token.Text != "func" || index+1 >= len(tokens) {
			continue
		}
		open := index + 1
		if tokens[open].Kind == TokenIdentifier {
			open++
		}
		if open >= len(tokens) || tokens[open].Text != "(" {
			continue
		}
		depth := 0
		close := -1
		for cursor := open; cursor < len(tokens); cursor++ {
			switch tokens[cursor].Text {
			case "(":
				depth++
			case ")":
				depth--
				if depth == 0 {
					close = cursor
				}
			}
			if close >= 0 {
				break
			}
		}
		if close <= open+1 {
			continue
		}
		for _, parameter := range splitExpressionTokens(tokens[open+1 : close]) {
			if len(parameter) >= 2 &&
				(parameter[0].Kind == TokenIdentifier || parameter[0].Kind == TokenKeyword) {
				return true
			}
		}
	}
	return false
}

func overloadSourceHasUnresolvedCall(source string, overloads overloadContext) bool {
	if overloads.CurrentClass != "" {
		methods := overloads.ClassMethods[overloads.CurrentClass]
		for name, candidates := range methods {
			if len(candidates) > 0 && strings.Contains(source, "."+name+"(") {
				return true
			}
		}
		return false
	}
	for name, candidates := range overloads.Functions {
		if len(candidates) > 0 && strings.Contains(source, name+"(") {
			return true
		}
	}
	for name, candidates := range overloads.FunctionTypes {
		if len(candidates) > 0 && strings.Contains(source, name+"(") {
			return true
		}
	}
	for name, candidates := range overloads.Methods {
		if len(candidates) > 0 && strings.Contains(source, "."+name+"(") {
			return true
		}
	}
	for name, candidates := range overloads.MethodTypes {
		if len(candidates) > 0 && strings.Contains(source, "."+name+"(") {
			return true
		}
	}
	return false
}

type overloadASTBody struct {
	block *BlockStmt
	types map[string]string
}

func transformOverloadsAST(src string, overloads overloadContext) (string, bool, error) {
	tokens, err := LexSource("overloads", src)
	if err != nil {
		return src, false, nil
	}
	bodies := []overloadASTBody{}
	edits := []struct {
		start int
		end   int
		text  string
	}{}
	functions := parseTopLevelFunctions("overloads", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function == nil || function.Method.BodyAST == nil {
				continue
			}
			types := cloneStringMap(overloads.LocalTypes)
			if types == nil {
				types = map[string]string{}
			}
			for _, parameter := range function.Method.ParameterAST {
				if parameter.Type == nil {
					continue
				}
				if typeName, typeErr := typeNodeSource(parameter.Type); typeErr == nil {
					types[parameter.Name] = strings.TrimSpace(typeName)
				}
			}
			bodies = append(bodies, overloadASTBody{block: function.Method.BodyAST, types: types})
			if renamed := overloadFunctionName(function, overloads); renamed != "" && renamed != function.Name {
				span := function.Method.NameSpan
				if span.End > span.Start && span.Start >= 0 && span.End <= len(src) {
					edits = append(edits, struct {
						start int
						end   int
						text  string
					}{start: span.Start, end: span.End, text: renamed})
				}
			}
		}
	} else if block, parseErr := ParseBodyAST(tokens); parseErr == nil && block != nil {
		types := cloneStringMap(overloads.LocalTypes)
		if types == nil {
			types = map[string]string{}
		}
		bodies = append(bodies, overloadASTBody{block: block, types: types})
	}
	if len(bodies) == 0 {
		return src, false, nil
	}
	for _, body := range bodies {
		collectOverloadStatementTypes(body.block, body.types)
		var resolutionErr error
		collectOverloadBodyCalls(body.block, body.types, func(call *CallExpr, callTypes map[string]string) {
			if resolutionErr != nil {
				return
			}
			name, ok, err := overloadCallName(call, callTypes, overloads)
			if err != nil {
				resolutionErr = err
				return
			}
			if !ok {
				return
			}
			span := overloadCallCalleeSpan(call)
			if span.End > span.Start && span.Start >= 0 && span.End <= len(src) {
				edits = append(edits, struct {
					start int
					end   int
					text  string
				}{start: span.Start, end: span.End, text: name})
			}
		})
		if resolutionErr != nil {
			return src, true, resolutionErr
		}
		tokenScopes := collectOverloadFunctionLiteralTypeScopes(tokens, body.block)
		collectOverloadTokenCalls(src, tokens, body.block, body.types, tokenScopes, overloads, &edits)
	}
	sort.SliceStable(edits, func(left, right int) bool { return edits[left].start > edits[right].start })
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

type overloadTokenTypeScope struct {
	start int
	end   int
	types map[string]string
}

// collectOverloadFunctionLiteralTypeScopes recovers receiver types from typed
// function literals whose expression AST is incomplete, while preserving
// their lexical scope during token-backed call resolution.
func collectOverloadFunctionLiteralTypeScopes(tokens []Token, block *BlockStmt) []overloadTokenTypeScope {
	if block == nil {
		return nil
	}
	start, end := block.Span().Start, block.Span().End
	scopes := []overloadTokenTypeScope{}
	for index := 0; index+1 < len(tokens); index++ {
		if tokens[index].Span.Start < start || tokens[index].Span.End > end || tokens[index].Text != "func" || tokens[index+1].Text != "(" {
			continue
		}
		close := matchingToken(tokens, index+1, "(", ")")
		if close < 0 {
			continue
		}
		parameters, err := parseParameterInfos(expressionTokensSource(tokens[index+2 : close]))
		if err != nil {
			continue
		}
		bodyOpen := -1
		for cursor := close + 1; cursor < len(tokens) && tokens[cursor].Span.Start < end; cursor++ {
			if tokens[cursor].Text == "{" {
				bodyOpen = cursor
				break
			}
			if tokens[cursor].Text == ";" || tokens[cursor].Text == "=>" {
				break
			}
		}
		if bodyOpen < 0 {
			continue
		}
		bodyClose := matchingToken(tokens, bodyOpen, "{", "}")
		if bodyClose < 0 {
			continue
		}
		literalTypes := map[string]string{}
		for _, parameter := range parameters {
			if parameter.Name == "" || parameter.TypeAST == nil {
				continue
			}
			if typeName, typeErr := typeNodeSource(parameter.TypeAST); typeErr == nil {
				literalTypes[parameter.Name] = strings.TrimSpace(typeName)
			}
		}
		scopes = append(scopes, overloadTokenTypeScope{
			start: tokens[bodyOpen].Span.Start,
			end:   tokens[bodyClose].Span.End,
			types: literalTypes,
		})
	}
	return scopes
}

func collectOverloadTokenCalls(source string, tokens []Token, block *BlockStmt, types map[string]string, scopes []overloadTokenTypeScope, overloads overloadContext, edits *[]struct {
	start int
	end   int
	text  string
}) {
	if block == nil || edits == nil {
		return
	}
	start, end := block.Span().Start, block.Span().End
	for index := 0; index+3 < len(tokens); index++ {
		if tokens[index].Span.Start < start || tokens[index].Span.End > end ||
			(tokens[index].Kind != TokenIdentifier && tokens[index].Kind != TokenKeyword) ||
			tokens[index+1].Text != "." ||
			(tokens[index+2].Kind != TokenIdentifier && tokens[index+2].Kind != TokenKeyword) ||
			tokens[index+3].Text != "(" {
			continue
		}
		close := matchingToken(tokens, index+3, "(", ")")
		if close < 0 {
			continue
		}
		arguments := []CallArg{}
		for _, part := range splitExpressionTokens(tokens[index+4 : close]) {
			expression, err := ParseExpressionTokens(part)
			if err != nil || expression == nil {
				arguments = nil
				break
			}
			arguments = append(arguments, CallArg{Value: expression})
		}
		call := &CallExpr{
			Callee: &SelectorExpr{
				Receiver:  &NameExpr{Name: tokens[index].Text, SpanValue: tokens[index].Span},
				Name:      tokens[index+2].Text,
				SpanValue: Span{Start: tokens[index].Span.Start, End: tokens[index+2].Span.End, Line: tokens[index].Span.Line, Column: tokens[index].Span.Column},
			},
			Arguments: arguments,
			SpanValue: Span{Start: tokens[index].Span.Start, End: tokens[close].Span.End, Line: tokens[index].Span.Line, Column: tokens[index].Span.Column},
		}
		callTypes := cloneStringMap(types)
		if callTypes == nil {
			callTypes = map[string]string{}
		}
		for _, scope := range scopes {
			if tokens[index].Span.Start >= scope.start && tokens[index].Span.Start < scope.end {
				for name, typeName := range scope.types {
					callTypes[name] = typeName
				}
			}
		}
		name, ok, _ := overloadCallName(call, callTypes, overloads)
		if !ok || name == "" || name == tokens[index+2].Text {
			continue
		}
		duplicate := false
		for _, edit := range *edits {
			if edit.start == tokens[index+2].Span.Start && edit.end == tokens[index+2].Span.End {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		*edits = append(*edits, struct {
			start int
			end   int
			text  string
		}{start: tokens[index+2].Span.Start, end: tokens[index+2].Span.End, text: name})
		_ = source
	}
}

func matchingToken(tokens []Token, open int, opening, closing string) int {
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
	return -1
}

func overloadFunctionName(function *FunctionDecl, overloads overloadContext) string {
	if function == nil {
		return ""
	}
	parameters, err := parameterInfosForMethod(function.Method)
	if err != nil {
		return ""
	}
	if renamed := overloads.FunctionTypes[function.Name][parameterSignatureKey(parameters)]; renamed != "" {
		return renamed
	}
	return overloads.Functions[function.Name][len(parameters)]
}

func collectOverloadStatementTypes(block *BlockStmt, types map[string]string) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectOverloadStatementType(statement, types)
	}
}

func collectOverloadStatementType(statement Stmt, types map[string]string) {
	if statement == nil {
		return
	}
	switch value := statement.(type) {
	case *TokenStmt:
		collectOverloadStatementTypes(value.Body, types)
		for _, child := range value.Children {
			collectOverloadStatementType(child, types)
		}
	case *DeclarationStmt:
		declared := ""
		if value.Type != nil {
			declared, _ = typeNodeSource(value.Type)
		}
		for index, name := range value.Names {
			inferred := strings.TrimSpace(declared)
			if inferred == "" && index < len(value.Values) {
				inferred = overloadExpressionTypeKey(value.Values[index], types)
			}
			if inferred != "" {
				types[name.Text] = inferred
			}
		}
	case *AssignmentStmt:
		for index, left := range value.Left {
			name, ok := left.(*NameExpr)
			if !ok || index >= len(value.Right) {
				continue
			}
			if inferred := overloadExpressionTypeKey(value.Right[index], types); inferred != "" {
				types[name.Name] = inferred
			}
		}
	case *IfStmt:
		collectOverloadStatementTypes(value.Body, types)
		collectOverloadStatementTypes(value.Else, types)
		if value.ElseIf != nil {
			collectOverloadStatementType(value.ElseIf, types)
		}
	case *ForStmt:
		collectOverloadStatementTypes(value.Body, types)
	case *SwitchStmt:
		collectOverloadStatementTypes(value.Body, types)
	case *CaseStmt:
		collectOverloadStatementTypes(value.Clause.Body, types)
	case *TryStmt:
		collectOverloadStatementTypes(value.Body, types)
		for _, clause := range value.Catches {
			collectOverloadStatementTypes(clause.Body, types)
		}
		collectOverloadStatementTypes(value.Finally, types)
	}
}

func collectOverloadBodyCalls(block *BlockStmt, types map[string]string, visit func(*CallExpr, map[string]string)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		walkStmtExpressions(statement, func(expression ExprNode) {
			collectOverloadCalls(expression, types, visit)
		})
	}
}

func collectOverloadCalls(expression ExprNode, types map[string]string, visit func(*CallExpr, map[string]string)) {
	if expression == nil {
		return
	}
	switch value := expression.(type) {
	case *CallExpr:
		visit(value, types)
		collectOverloadCalls(value.Callee, types, visit)
		for _, argument := range value.Arguments {
			collectOverloadCalls(argument.Value, types, visit)
		}
	case *UnaryExpr:
		collectOverloadCalls(value.Operand, types, visit)
	case *BinaryExpr:
		collectOverloadCalls(value.Left, types, visit)
		collectOverloadCalls(value.Right, types, visit)
	case *AssignmentExpr:
		for _, expression := range value.Left {
			collectOverloadCalls(expression, types, visit)
		}
		for _, expression := range value.Right {
			collectOverloadCalls(expression, types, visit)
		}
	case *SelectorExpr:
		collectOverloadCalls(value.Receiver, types, visit)
	case *IndexExpr:
		collectOverloadCalls(value.Receiver, types, visit)
		collectOverloadCalls(value.Index, types, visit)
	case *IndexListExpr:
		collectOverloadCalls(value.Receiver, types, visit)
		for _, index := range value.Indices {
			collectOverloadCalls(index, types, visit)
		}
	case *ParenthesizedExpr:
		collectOverloadCalls(value.Inner, types, visit)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectOverloadCalls(element.Key, types, visit)
			collectOverloadCalls(element.Value, types, visit)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectOverloadCalls(segment.Expression, types, visit)
		}
	case *LambdaExpr:
		collectOverloadCalls(value.Body, types, visit)
		collectOverloadBodyCalls(value.BlockBody, types, visit)
	case *FunctionLiteralExpr:
		literalTypes := cloneStringMap(types)
		if literalTypes == nil {
			literalTypes = map[string]string{}
		}
		if value.Type != nil {
			for _, parameter := range value.Type.Parameters {
				if parameter.Name == "" || parameter.Type == nil {
					continue
				}
				if typeName, err := typeNodeSource(parameter.Type); err == nil {
					literalTypes[parameter.Name] = strings.TrimSpace(typeName)
				}
			}
		}
		collectOverloadBodyCalls(value.Body, literalTypes, visit)
	}
}

func overloadExpressionTypeKey(expression ExprNode, types map[string]string) string {
	switch value := expression.(type) {
	case *TokenExpr:
		text := strings.TrimSpace(expressionTokensSource(value.Tokens))
		if _, err := strconv.ParseInt(text, 0, 64); err == nil {
			return "int"
		}
		if _, err := strconv.ParseFloat(text, 64); err == nil {
			return "float64"
		}
		if len(value.Tokens) == 1 {
			token := value.Tokens[0]
			if token.Text != "" && token.Text[0] >= '0' && token.Text[0] <= '9' {
				if strings.ContainsAny(token.Text, ".eE") {
					return "float64"
				}
				return "int"
			}
			switch token.Kind {
			case TokenNumber:
				if strings.ContainsAny(token.Text, ".eE") {
					return "float64"
				}
				return "int"
			case TokenString, TokenRawString:
				return "string"
			case TokenRune:
				return "rune"
			}
		}
	case *LiteralExpr:
		switch value.Kind {
		case TokenString, TokenRawString:
			return "string"
		case TokenNumber:
			if strings.ContainsAny(value.Text, ".eE") {
				return "float64"
			}
			return "int"
		case TokenRune:
			return "rune"
		}
	case *NameExpr:
		if value.Name == "true" || value.Name == "false" {
			return "bool"
		}
		if value.Name == "nil" {
			return "nil"
		}
		if _, err := strconv.ParseInt(value.Name, 0, 64); err == nil {
			return "int"
		}
		if _, err := strconv.ParseFloat(value.Name, 64); err == nil {
			return "float64"
		}
		return types[value.Name]
	case *UnaryExpr:
		if value.Operator == "&" {
			if name := overloadExpressionTypeKey(value.Operand, types); name != "" {
				return "*" + name
			}
		}
	case *CompositeLiteralExpr:
		name, _ := typeNodeSource(value.Type)
		return strings.TrimSpace(name)
	case *FunctionLiteralExpr:
		return overloadTypeSignatureKey(value.Type)
	case *LambdaExpr:
		if value.Body != nil {
			return "func"
		}
	case *ParenthesizedExpr:
		return overloadExpressionTypeKey(value.Inner, types)
	case *BinaryExpr:
		if value.Operator == "==" || value.Operator == "!=" || value.Operator == "<" || value.Operator == ">" || value.Operator == "<=" || value.Operator == ">=" {
			return "bool"
		}
		return overloadExpressionTypeKey(value.Left, types)
	}
	return ""
}

func overloadTypeSignatureKey(typeNode TypeNode) string {
	function, ok := typeNode.(*FunctionType)
	if !ok || function == nil {
		name, _ := typeNodeSource(typeNode)
		return strings.TrimSpace(name)
	}
	parameters, err := parameterSignatureKeyFromNodes(function.Parameters)
	if err != nil {
		return ""
	}
	results := make([]string, 0, len(function.Results))
	for _, result := range function.Results {
		text, resultErr := typeNodeSource(result)
		if resultErr != nil {
			return ""
		}
		results = append(results, strings.TrimSpace(text))
	}
	if len(results) == 0 {
		return "func(" + parameters + ")"
	}
	if len(results) == 1 {
		return "func(" + parameters + ") " + results[0]
	}
	return "func(" + parameters + ") (" + strings.Join(results, ",") + ")"
}

func parameterSignatureKeyFromNodes(parameters []ParameterNode) (string, error) {
	parts := make([]string, len(parameters))
	for index, parameter := range parameters {
		text, err := typeNodeSource(parameter.Type)
		if err != nil {
			return "", err
		}
		parts[index] = strings.Join(strings.Fields(text), " ")
	}
	return strings.Join(parts, ","), nil
}

func overloadArgumentSignatureKey(call *CallExpr, types map[string]string) string {
	parts := make([]string, len(call.Arguments))
	for index, argument := range call.Arguments {
		parts[index] = overloadExpressionTypeKey(argument.Value, types)
	}
	return strings.Join(parts, ",")
}

func overloadCallName(call *CallExpr, types map[string]string, overloads overloadContext) (string, bool, error) {
	if call == nil {
		return "", false, nil
	}
	arity := len(call.Arguments)
	typeKey := overloadArgumentSignatureKey(call, types)
	switch function := call.Callee.(type) {
	case *NameExpr:
		set := overloads.FunctionTypes[function.Name]
		name := set[typeKey]
		if name == "" {
			name = overloads.Functions[function.Name][arity]
		}
		if name == "" && len(set) > 0 {
			return "", false, fmt.Errorf("cannot resolve overloaded function %s with argument types %s", function.Name, typeKey)
		}
		return name, name != "", nil
	case *SelectorExpr:
		methodSet := overloads.Methods
		methodTypes := overloads.MethodTypes
		if receiver, ok := function.Receiver.(*NameExpr); !ok || receiver.Name != "this" {
			typeName := overloadReceiverTypeKey(function.Receiver, types, overloads)
			if typeName == "" {
				return "", false, nil
			}
			typeName = strings.TrimPrefix(strings.TrimSpace(typeName), "*")
			methodSet = overloads.ClassMethods[typeName]
			methodTypes = overloads.ClassMethodTypes[typeName]
			if methodSet == nil && methodTypes == nil {
				return "", false, nil
			}
		}
		set := methodTypes[function.Name]
		name := set[typeKey]
		if name == "" {
			name = methodSet[function.Name][arity]
		}
		if name == "" && len(set) > 0 {
			return "", false, fmt.Errorf("cannot resolve overloaded method %s with argument types %s", function.Name, typeKey)
		}
		return name, name != "", nil
	}
	return "", false, nil
}

func overloadReceiverTypeKey(expression ExprNode, types map[string]string, overloads overloadContext) string {
	switch value := expression.(type) {
	case *NameExpr:
		if value.Name == "this" && overloads.CurrentClass != "" {
			return "*" + overloads.CurrentClass
		}
		return strings.TrimSpace(types[value.Name])
	case *SelectorExpr:
		receiverType := overloadReceiverTypeKey(value.Receiver, types, overloads)
		typeName := strings.TrimPrefix(strings.TrimSpace(receiverType), "*")
		if typeName == "" {
			return ""
		}
		return strings.TrimSpace(overloads.ClassFieldTypes[typeName][value.Name])
	case *CompositeLiteralExpr:
		name, _ := typeNodeSource(value.Type)
		if name != "" {
			return "*" + strings.TrimSpace(name)
		}
	case *CallExpr:
		if name, ok := value.Callee.(*NameExpr); ok {
			if overloads.ClassMethods[name.Name] != nil || overloads.ClassMethodTypes[name.Name] != nil {
				return "*" + name.Name
			}
		}
	case *ParenthesizedExpr:
		return overloadReceiverTypeKey(value.Inner, types, overloads)
	}
	return ""
}

func overloadCallCalleeSpan(call *CallExpr) Span {
	if call == nil || call.Callee == nil {
		return Span{}
	}
	if selector, ok := call.Callee.(*SelectorExpr); ok {
		span := selector.Span()
		nameStart := span.End - len(selector.Name)
		return Span{Start: nameStart, End: span.End, Line: span.Line, Column: span.Column}
	}
	return call.Callee.Span()
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

	for _, parentName := range classParentNames(class) {
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
	if !fileHasInterpolationExpression(file) {
		// Escaped template braces still need the interpolation pass to turn
		// `{{{{`/`}}}}` into literal `{{`/`}}`, but they do not require fmt.
		return "fmt", false, nil
	}

	if imports, err := goImports(file); err == nil {
		for _, spec := range imports {
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
		case *MixedDecl:
			if tokensNeedInterpolationLowering(mixedDeclTokens(d)) {
				return true
			}
		case *FunctionDecl:
			if tokensNeedInterpolationLowering(methodBodyTokens(d.Method)) {
				return true
			}
		case *GoDecl:
			if tokensNeedInterpolationLowering(goDeclTokens(d)) {
				return true
			}
		case *ValueDecl:
			if tokensNeedInterpolationLowering(valueDeclTokens(d)) {
				return true
			}
		case *ClassDecl:
			for _, method := range d.Methods {
				if tokensNeedInterpolationLowering(methodBodyTokens(method)) {
					return true
				}
			}
		case *ExtendDecl:
			for _, method := range d.Methods {
				if tokensNeedInterpolationLowering(methodBodyTokens(method)) {
					return true
				}
			}
		}
	}

	return false
}

func fileHasInterpolationExpression(file *File) bool {
	for _, declaration := range file.Decls {
		var tokens []Token
		switch value := declaration.(type) {
		case *MixedDecl:
			tokens = mixedDeclTokens(value)
		case *FunctionDecl:
			tokens = methodBodyTokens(value.Method)
		case *GoDecl:
			tokens = goDeclTokens(value)
		case *ValueDecl:
			tokens = valueDeclTokens(value)
		case *ClassDecl:
			for _, method := range value.Methods {
				if tokensHaveInterpolation(methodBodyTokens(method)) {
					return true
				}
			}
			continue
		case *ExtendDecl:
			for _, method := range value.Methods {
				if tokensHaveInterpolation(methodBodyTokens(method)) {
					return true
				}
			}
			continue
		default:
			continue
		}
		if tokensHaveInterpolation(tokens) {
			return true
		}
	}
	return false
}

func tokensNeedInterpolationLowering(tokens []Token) bool {
	if tokensHaveInterpolation(tokens) {
		return true
	}
	for _, token := range tokens {
		if token.Kind != TokenString && token.Kind != TokenRawString {
			continue
		}
		content := token.Text[1 : len(token.Text)-1]
		if strings.Contains(content, "{{{{") || strings.Contains(content, "}}}}") {
			return true
		}
	}
	return false
}

func tokensHaveInterpolation(tokens []Token) bool {
	for _, token := range tokens {
		if token.Kind != TokenString && token.Kind != TokenRawString {
			continue
		}
		if len(token.Text) < 2 {
			continue
		}
		if interpolationLiteralHasExpression(token.Text[1 : len(token.Text)-1]) {
			return true
		}
	}
	return false
}

func sourceHasInterpolation(src string) bool {
	for i := 0; i < len(src); {
		if src[i] == '/' && i+1 < len(src) && src[i+1] == '/' {
			end := strings.IndexByte(src[i+2:], '\n')
			if end < 0 {
				return false
			}
			i += end + 2
			continue
		}
		if src[i] == '/' && i+1 < len(src) && src[i+1] == '*' {
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return false
			}
			i += end + 4
			continue
		}
		if src[i] == '"' || src[i] == '\'' {
			end, err := skipQuoted(src, i, src[i])
			if err != nil {
				return false
			}
			if interpolationLiteralHasExpression(src[i+1 : end]) {
				return true
			}
			i = end + 1
			continue
		}
		if src[i] == '`' {
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return false
			}
			if interpolationLiteralHasExpression(src[i+1 : i+1+end]) {
				return true
			}
			i += end + 2
			continue
		}
		i++
	}
	return false
}

func interpolationLiteralHasExpression(content string) bool {
	for i := 0; i+1 < len(content); {
		if content[i] != '{' || content[i+1] != '{' {
			i++
			continue
		}
		if i+3 < len(content) && content[i:i+4] == "{{{{" {
			i += 4
			continue
		}
		return true
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
	if transformed, handled, err := transformConstructorsAST(src, context); handled {
		return transformed, err
	}
	return src, nil
}

type constructorCallNode struct {
	call   *CallExpr
	name   string
	target constructorTarget
	start  int
	end    int
}

func transformConstructorsAST(src string, context constructorContext) (string, bool, error) {
	tokens, err := LexSource("constructors", src)
	if err != nil {
		return src, false, nil
	}
	blocks := []*BlockStmt{}
	functions := parseTopLevelFunctions("constructors", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function != nil && function.Method.BodyAST != nil {
				blocks = append(blocks, function.Method.BodyAST)
			}
		}
	} else if block, parseErr := ParseBodyAST(tokens); parseErr == nil && block != nil {
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return src, false, nil
	}
	calls := []constructorCallNode{}
	for _, block := range blocks {
		collectConstructorBodyExpressions(block, func(expression ExprNode) {
			collectConstructorCalls(expression, context, &calls)
		})
	}
	if len(calls) == 0 {
		return src, false, nil
	}

	// Lower only outermost constructor calls. The structured expression
	// lowerer handles nested constructor expressions while this pass avoids
	// overlapping source edits.
	outermost := make([]constructorCallNode, 0, len(calls))
	for index, candidate := range calls {
		nested := false
		for otherIndex, other := range calls {
			if index == otherIndex {
				continue
			}
			if other.start <= candidate.start && other.end >= candidate.end &&
				(other.start < candidate.start || other.end > candidate.end) {
				nested = true
				break
			}
		}
		if !nested {
			outermost = append(outermost, candidate)
		}
	}
	if len(outermost) == 0 {
		return src, false, nil
	}

	type edit struct {
		start int
		end   int
		text  string
	}
	edits := make([]edit, 0, len(outermost))
	for _, candidate := range outermost {
		lowered, loweredHandled, lowerErr := constructorExprNode(candidate.call, context)
		if lowerErr != nil {
			return "", true, lowerErr
		}
		if !loweredHandled {
			return src, false, nil
		}
		literal, emitErr := goExprNode(lowered)
		if emitErr != nil {
			return src, false, nil
		}
		literalSource, formatErr := formatNode(literal)
		if formatErr != nil {
			return "", true, formatErr
		}
		edits = append(edits, edit{start: candidate.start, end: candidate.end, text: literalSource})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		if edit.start < 0 || edit.end > len(src) || edit.start >= edit.end {
			return src, false, nil
		}
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

func collectConstructorBodyExpressions(block *BlockStmt, visit func(ExprNode)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectConstructorStmtExpressions(statement, visit)
	}
}

func collectConstructorStmtExpressions(statement Stmt, visit func(ExprNode)) {
	walkStmtExpressions(statement, visit)
}

func collectConstructorStructuredHeader(statement Stmt, visit func(ExprNode)) {
	switch value := statement.(type) {
	case *IfStmt:
		visit(value.Condition)
	case *ForStmt:
		visit(value.Init)
		visit(value.Condition)
		visit(value.Post)
		visit(value.RangeExpr)
	case *SwitchStmt:
		visit(value.Tag)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			visit(expression)
		}
	}
}

func collectConstructorCalls(expression ExprNode, context constructorContext, result *[]constructorCallNode) {
	if expression == nil {
		return
	}
	switch value := expression.(type) {
	case *CallExpr:
		if name, target, ok := constructorTargetForCallee(value.Callee, context); ok {
			span := value.Span()
			*result = append(*result, constructorCallNode{call: value, name: name, target: target, start: span.Start, end: span.End})
		}
		collectConstructorCalls(value.Callee, context, result)
		for _, argument := range value.Arguments {
			collectConstructorCalls(argument.Value, context, result)
		}
	case *UnaryExpr:
		collectConstructorCalls(value.Operand, context, result)
	case *BinaryExpr:
		collectConstructorCalls(value.Left, context, result)
		collectConstructorCalls(value.Right, context, result)
	case *AssignmentExpr:
		for _, expression := range value.Left {
			collectConstructorCalls(expression, context, result)
		}
		for _, expression := range value.Right {
			collectConstructorCalls(expression, context, result)
		}
	case *SelectorExpr:
		collectConstructorCalls(value.Receiver, context, result)
	case *IndexExpr:
		collectConstructorCalls(value.Receiver, context, result)
		collectConstructorCalls(value.Index, context, result)
	case *IndexListExpr:
		collectConstructorCalls(value.Receiver, context, result)
		for _, index := range value.Indices {
			collectConstructorCalls(index, context, result)
		}
	case *SliceExpr:
		collectConstructorCalls(value.Receiver, context, result)
		collectConstructorCalls(value.Low, context, result)
		collectConstructorCalls(value.High, context, result)
		collectConstructorCalls(value.Max, context, result)
	case *TypeAssertExpr:
		collectConstructorCalls(value.Expression, context, result)
	case *PostfixExpr:
		collectConstructorCalls(value.Expression, context, result)
	case *SpreadExpr:
		collectConstructorCalls(value.Expression, context, result)
	case *TypeExpr:
		// Type expressions do not contain constructor calls.
	case *SendExpr:
		collectConstructorCalls(value.Channel, context, result)
		collectConstructorCalls(value.Value, context, result)
	case *ParenthesizedExpr:
		collectConstructorCalls(value.Inner, context, result)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectConstructorCalls(element.Key, context, result)
			collectConstructorCalls(element.Value, context, result)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectConstructorCalls(segment.Expression, context, result)
		}
	case *LambdaExpr:
		collectConstructorCalls(value.Body, context, result)
		collectConstructorBodyExpressions(value.BlockBody, func(expression ExprNode) {
			collectConstructorCalls(expression, context, result)
		})
	case *FunctionLiteralExpr:
		collectConstructorBodyExpressions(value.Body, func(expression ExprNode) {
			collectConstructorCalls(expression, context, result)
		})
	case *TokenExpr:
		collectConstructorCallsFromTokens(value.Tokens, context, result)
	}
}

// collectConstructorCallsFromTokens handles a syntax-preserving expression
// fallback without converting it back into source text. This is needed for a
// small class of mixed Go/Go++ statements whose header contains more than one
// expression; the token stream still gives us exact call boundaries.
func collectConstructorCallsFromTokens(tokens []Token, context constructorContext, result *[]constructorCallNode) {
	clean := significantSyntaxTokens(tokens)
	for index := 0; index < len(clean); index++ {
		if clean[index].Kind != TokenIdentifier && clean[index].Kind != TokenKeyword {
			continue
		}
		nameEnd := index + 1
		for nameEnd+1 < len(clean) && clean[nameEnd].Text == "." &&
			(clean[nameEnd+1].Kind == TokenIdentifier || clean[nameEnd+1].Kind == TokenKeyword) {
			nameEnd += 2
		}
		name := clean[index].Text
		if nameEnd > index+1 {
			parts := []string{name}
			for part := index + 2; part < nameEnd; part += 2 {
				parts = append(parts, clean[part].Text)
			}
			name = strings.Join(parts, ".")
		}
		if _, exists := context.Targets[name]; !exists || nameEnd >= len(clean) || clean[nameEnd].Text != "(" {
			continue
		}
		close := matchingTokenParen(clean, nameEnd)
		if close < 0 {
			continue
		}
		expression, err := ParseExpressionTokens(clean[index : close+1])
		call, ok := expression.(*CallExpr)
		if err != nil || !ok {
			continue
		}
		collectConstructorCalls(call, context, result)
		index = close
	}
}

func matchingTokenParen(tokens []Token, open int) int {
	depth := 0
	for index := open; index < len(tokens); index++ {
		switch tokens[index].Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func constructorTargetForCallee(callee ExprNode, context constructorContext) (string, constructorTarget, bool) {
	base := callee
	var typeArguments []string
	switch value := callee.(type) {
	case *IndexExpr:
		base = value.Receiver
		argument, ok := genericTypeArgumentSource(value.Index)
		if !ok {
			return "", constructorTarget{}, false
		}
		typeArguments = []string{argument}
	case *IndexListExpr:
		base = value.Receiver
		for _, index := range value.Indices {
			argument, ok := genericTypeArgumentSource(index)
			if !ok {
				return "", constructorTarget{}, false
			}
			typeArguments = append(typeArguments, argument)
		}
	}
	name, ok := directSelectorPath(base)
	if !ok {
		return "", constructorTarget{}, false
	}
	name = strings.TrimSpace(name)
	target, ok := context.Targets[name]
	if ok && len(typeArguments) != 0 {
		name += "[" + strings.Join(typeArguments, ", ") + "]"
	}
	return name, target, ok
}

func genericTypeArgumentSource(expression ExprNode) (string, bool) {
	switch value := expression.(type) {
	case *TypeExpr:
		if value.Type != nil {
			text, err := typeNodeSource(value.Type)
			return text, err == nil && text != ""
		}
	case *NameExpr:
		return value.Name, value.Name != ""
	case *SelectorExpr:
		text, ok := directSelectorPath(value)
		return text, ok
	}
	return "", false
}

type constructorField struct {
	Name        string
	Type        string
	Path        []string
	Owner       string
	File        *File
	Annotations []AnnotationUse
}

func constructorFields(class *ClassDecl, classes map[string]*ClassDecl, prefix []string, visiting map[string]bool) ([]constructorField, error) {
	if visiting[class.Name] {
		return nil, fmt.Errorf("inheritance cycle involving class %s", class.Name)
	}

	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	fields := []constructorField{}
	for _, parentName := range classParentNames(class) {
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
			append(prefix, classParentFieldName(parentName)),
			visiting,
		)
		if err != nil {
			return nil, err
		}
		fields = append(fields, parentFields...)
	}

	for _, field := range class.Fields {
		fields = append(fields, constructorField{
			Name:        field.Name,
			Type:        fieldTypeSource(field),
			Path:        append([]string(nil), prefix...),
			Owner:       class.Name,
			File:        field.Owner,
			Annotations: field.Annotations,
		})
	}

	return fields, nil
}

func classParentFieldName(parentName string) string {
	if dot := strings.LastIndex(parentName, "."); dot >= 0 {
		return parentName[dot+1:]
	}
	return parentName
}

func expressionTypeName(expression ExprNode) (string, bool) {
	switch value := expression.(type) {
	case *NameExpr:
		return value.Name, true
	case *SelectorExpr:
		prefix, ok := expressionTypeName(value.Receiver)
		if !ok {
			return "", false
		}
		return prefix + "." + value.Name, true
	case *CompositeLiteralExpr:
		typeName, err := typeNodeSource(value.Type)
		return typeName, err == nil && typeName != ""
	case *IndexListExpr:
		return expressionTypeName(value.Receiver)
	case *UnaryExpr:
		if value.Operator == "&" {
			return expressionTypeName(value.Operand)
		}
	case *TypeAssertExpr:
		if !value.TypeSwitch && value.Type != nil {
			if typeName, err := typeNodeSource(value.Type); err == nil {
				return typeName, true
			}
		}
	case *PostfixExpr:
		return expressionTypeName(value.Expression)
	case *SpreadExpr:
		return expressionTypeName(value.Expression)
	case *TypeExpr:
		if value.Type != nil {
			if typeName, err := typeNodeSource(value.Type); err == nil {
				return typeName, true
			}
		}
	case *SendExpr:
		return expressionTypeName(value.Value)
	case *FunctionLiteralExpr:
		if value.Type != nil {
			if typeName, err := typeNodeSource(value.Type); err == nil {
				return typeName, true
			}
		}
	}
	return "", false
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

func descriptorName(qualifier, className string) string {
	if qualifier == "" {
		return "Gpp" + className + "Class"
	}
	return qualifier + ".Gpp" + className + "Class"
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
	transformed, _ := transformInterpolationWithNameChecked(src, fmtName)
	return transformed
}

func transformInterpolationWithNameChecked(src, fmtName string) (string, error) {
	tokens, err := LexSource("interpolation", src)
	if err != nil {
		return src, err
	}
	fmtCall := fmtName + ".Sprintf"
	if fmtName == "Sprintf" {
		fmtCall = fmtName
	}
	type replacement struct {
		start int
		end   int
		text  string
	}
	replacements := []replacement{}
	for _, token := range tokens {
		if token.Kind != TokenString && token.Kind != TokenRawString {
			continue
		}
		expression, parseErr := parseInterpolatedString(token)
		if parseErr != nil {
			return src, parseErr
		}
		if expression == nil {
			continue
		}
		interpolated, ok := expression.(*InterpolatedStringExpr)
		if !ok {
			return src, fmt.Errorf("internal error: interpolation did not produce an interpolated string AST")
		}
		lowered, renderErr := renderInterpolatedStringExpr(interpolated, fmtCall)
		if renderErr != nil {
			return src, renderErr
		}
		replacements = append(replacements, replacement{start: token.Span.Start, end: token.Span.End, text: lowered})
	}
	for index := len(replacements) - 1; index >= 0; index-- {
		change := replacements[index]
		if change.start < 0 || change.end > len(src) || change.start > change.end {
			return src, fmt.Errorf("invalid interpolation source span [%d,%d)", change.start, change.end)
		}
		src = src[:change.start] + change.text + src[change.end:]
	}
	return src, nil
}

// transformMethodInterpolation lowers interpolation from the parsed body AST.
// Structured methods must not silently fall back to scanning executable source.
func transformMethodInterpolation(method Method, fmtName string) (string, error) {
	source := methodBodySource(method)
	if method.BodyAST == nil {
		return source, fmt.Errorf("method %s has no structured body AST for interpolation lowering", method.Name)
	}
	if method.Owner == nil {
		body, err := goBlockNode(method.BodyAST, "")
		if err != nil {
			return source, fmt.Errorf("method %s cannot be emitted from its structured body AST: %w", method.Name, err)
		}
		var output bytes.Buffer
		if err := format.Node(&output, token.NewFileSet(), body); err != nil {
			return source, err
		}
		rendered := strings.TrimSpace(output.String())
		if len(rendered) >= 2 && strings.HasPrefix(rendered, "{") && strings.HasSuffix(rendered, "}") {
			return strings.TrimSpace(rendered[1 : len(rendered)-1]), nil
		}
		return rendered, nil
	}
	transformed, handled, err := transformInterpolationAST(source, method.BodyAST, method.BodySpan.Start, fmtName)
	if err != nil {
		return source, err
	}
	if !handled {
		// The token-level interpolation parser is still AST-based; it is only
		// needed when a surrounding TokenStmt could not expose the literal as a
		// typed body expression.
		return transformInterpolationWithNameChecked(source, fmtName)
	}
	return transformed, nil
}

func transformInterpolationInFunctionSource(source string, function *FunctionDecl, fmtName string) (string, error) {
	if function == nil {
		return source, nil
	}
	method := function.Method
	if method.BodyAST == nil || function.Owner == nil {
		return source, fmt.Errorf("function %s has no structured body AST for interpolation lowering", function.Name)
	}
	body := methodBodySource(method)
	transformed, handled, err := transformInterpolationAST(body, method.BodyAST, method.BodySpan.Start, fmtName)
	if err != nil {
		return source, err
	}
	if !handled {
		return transformInterpolationWithNameChecked(body, fmtName)
	}
	if transformed == body {
		transformed, err = transformEscapedInterpolationChecked(transformed)
		if err != nil {
			return source, err
		}
		if transformed == body {
			return source, nil
		}
	} else {
		transformed, err = transformEscapedInterpolationChecked(transformed)
		if err != nil {
			return source, err
		}
	}
	start := method.BodySpan.Start - function.SourceSpan.Start
	end := method.BodySpan.End - function.SourceSpan.Start
	if start < 0 || end > len(source) || start >= end {
		return source, fmt.Errorf("invalid function body source span [%d,%d)", start, end)
	}
	return source[:start] + transformed + source[end:], nil
}

// transformEscapedInterpolationChecked handles escaped template braces that
// contain no Go++ expression. A body may already have had ordinary
// interpolations lowered by the AST pass; this narrow follow-up is safe because
// it only rewrites tokens that still contain doubled braces.
func transformEscapedInterpolationChecked(src string) (string, error) {
	tokens, err := LexSource("escaped interpolation", src)
	if err != nil {
		return src, err
	}
	type replacement struct {
		start int
		end   int
		text  string
	}
	replacements := []replacement{}
	for _, token := range tokens {
		if token.Kind != TokenString && token.Kind != TokenRawString || len(token.Text) < 2 {
			continue
		}
		content := token.Text[1 : len(token.Text)-1]
		if !strings.Contains(content, "{{{{") && !strings.Contains(content, "}}}}") {
			continue
		}
		expression, parseErr := parseInterpolatedString(token)
		if parseErr != nil {
			return src, parseErr
		}
		interpolated, ok := expression.(*InterpolatedStringExpr)
		if !ok {
			continue
		}
		containsExpression := false
		for _, segment := range interpolated.Segments {
			if segment.Expression != nil {
				containsExpression = true
				break
			}
		}
		if containsExpression {
			continue
		}
		text, renderErr := renderInterpolatedStringExpr(interpolated, "fmt.Sprintf")
		if renderErr != nil {
			return src, renderErr
		}
		replacements = append(replacements, replacement{start: token.Span.Start, end: token.Span.End, text: text})
	}
	for index := len(replacements) - 1; index >= 0; index-- {
		change := replacements[index]
		if change.start < 0 || change.end > len(src) || change.start > change.end {
			return src, fmt.Errorf("invalid escaped interpolation source span [%d,%d)", change.start, change.end)
		}
		src = src[:change.start] + change.text + src[change.end:]
	}
	return src, nil
}

func transformInterpolationAST(source string, block *BlockStmt, sourceBase int, fmtName string) (string, bool, error) {
	if block == nil {
		return source, false, nil
	}
	expressions := []*InterpolatedStringExpr{}
	collectInterpolationBlockExpressions(block, &expressions)
	if len(expressions) == 0 {
		if tokens, lexErr := LexSource("interpolation", source); lexErr == nil && tokensHaveInterpolation(tokens) {
			// Preserve the handled=false signal so the token-level interpolation
			// parser can produce the precise literal diagnostic or lower a valid
			// literal retained inside a TokenStmt. Plain composite literals such
			// as `[]T{{...}}` are not interpolation syntax.
			return source, false, nil
		}
		return source, true, nil
	}
	type replacement struct {
		start int
		end   int
		text  string
	}
	replacements := make([]replacement, 0, len(expressions))
	fmtCall := fmtName + ".Sprintf"
	if fmtName == "Sprintf" {
		fmtCall = fmtName
	}
	for _, expression := range expressions {
		if expression == nil {
			continue
		}
		span := expression.Span()
		start := span.Start - sourceBase
		end := span.End - sourceBase
		if start < 0 || end > len(source) || start >= end {
			return source, true, fmt.Errorf("invalid interpolation source span [%d,%d)", start, end)
		}
		text, err := renderInterpolatedStringExpr(expression, fmtCall)
		if err != nil {
			return source, true, err
		}
		replacements = append(replacements, replacement{start: start, end: end, text: text})
	}
	for index := len(replacements) - 1; index >= 0; index-- {
		change := replacements[index]
		source = source[:change.start] + change.text + source[change.end:]
	}
	return source, true, nil
}

func collectInterpolationBlockExpressions(block *BlockStmt, result *[]*InterpolatedStringExpr) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectInterpolationStmtExpressions(statement, result)
	}
}

func collectInterpolationStmtExpressions(statement Stmt, result *[]*InterpolatedStringExpr) {
	walkStmtExpressions(statement, func(expression ExprNode) {
		collectInterpolatedExprs(expression, result)
	})
}

func collectInterpolationStructuredHeader(statement Stmt, result *[]*InterpolatedStringExpr) {
	if statement == nil {
		return
	}
	switch value := statement.(type) {
	case *IfStmt:
		collectInterpolatedExprs(value.Init, result)
		collectInterpolatedExprs(value.Condition, result)
	case *ForStmt:
		collectInterpolatedExprs(value.Init, result)
		collectInterpolatedExprs(value.Condition, result)
		collectInterpolatedExprs(value.Post, result)
		collectInterpolatedExprs(value.RangeExpr, result)
	case *SwitchStmt:
		collectInterpolatedExprs(value.Init, result)
		collectInterpolatedExprs(value.Tag, result)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			collectInterpolatedExprs(expression, result)
		}
	}
}

func collectInterpolatedExprs(expression ExprNode, result *[]*InterpolatedStringExpr) {
	if expression == nil {
		return
	}
	switch value := expression.(type) {
	case *InterpolatedStringExpr:
		*result = append(*result, value)
		for _, segment := range value.Segments {
			collectInterpolatedExprs(segment.Expression, result)
		}
	case *UnaryExpr:
		collectInterpolatedExprs(value.Operand, result)
	case *BinaryExpr:
		collectInterpolatedExprs(value.Left, result)
		collectInterpolatedExprs(value.Right, result)
	case *AssignmentExpr:
		for _, expression := range value.Left {
			collectInterpolatedExprs(expression, result)
		}
		for _, expression := range value.Right {
			collectInterpolatedExprs(expression, result)
		}
	case *SelectorExpr:
		collectInterpolatedExprs(value.Receiver, result)
	case *IndexExpr:
		collectInterpolatedExprs(value.Receiver, result)
		collectInterpolatedExprs(value.Index, result)
	case *IndexListExpr:
		collectInterpolatedExprs(value.Receiver, result)
		for _, index := range value.Indices {
			collectInterpolatedExprs(index, result)
		}
	case *SliceExpr:
		collectInterpolatedExprs(value.Receiver, result)
		collectInterpolatedExprs(value.Low, result)
		collectInterpolatedExprs(value.High, result)
		collectInterpolatedExprs(value.Max, result)
	case *TypeAssertExpr:
		collectInterpolatedExprs(value.Expression, result)
	case *PostfixExpr:
		collectInterpolatedExprs(value.Expression, result)
	case *SpreadExpr:
		collectInterpolatedExprs(value.Expression, result)
	case *TypeExpr:
		// Type expressions do not contain interpolation.
	case *SendExpr:
		collectInterpolatedExprs(value.Channel, result)
		collectInterpolatedExprs(value.Value, result)
	case *CallExpr:
		collectInterpolatedExprs(value.Callee, result)
		for _, argument := range value.Arguments {
			collectInterpolatedExprs(argument.Value, result)
		}
	case *ParenthesizedExpr:
		collectInterpolatedExprs(value.Inner, result)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectInterpolatedExprs(element.Key, result)
			collectInterpolatedExprs(element.Value, result)
		}
	case *LambdaExpr:
		collectInterpolatedExprs(value.Body, result)
		collectInterpolationBlockExpressions(value.BlockBody, result)
	case *FunctionLiteralExpr:
		collectInterpolationBlockExpressions(value.Body, result)
	}
}

func renderInterpolatedStringExpr(expression *InterpolatedStringExpr, fmtCall string) (string, error) {
	if expression == nil {
		return "", fmt.Errorf("nil interpolated string expression")
	}
	var format strings.Builder
	expressions := []string{}
	for _, segment := range expression.Segments {
		if segment.Expression == nil {
			text := segment.Text
			if !expression.Raw {
				decoded, err := strconv.Unquote(`"` + text + `"`)
				if err != nil {
					return "", fmt.Errorf("invalid interpolated string literal: %w", err)
				}
				text = decoded
			}
			format.WriteString(strings.ReplaceAll(text, "%", "%%"))
			continue
		}
		formatSpec := segment.Format
		if formatSpec == "" {
			formatSpec = "%v"
		}
		if err := validateInterpolationFormat(formatSpec); err != nil {
			return "", err
		}
		format.WriteString(formatSpec)
		expressions = append(expressions, expressionTokensSource(segment.ExpressionTokens))
	}
	if len(expressions) == 0 {
		if expression.Raw {
			return "`" + format.String() + "`", nil
		}
		return strconv.Quote(format.String()), nil
	}
	var result strings.Builder
	fmt.Fprintf(&result, "%s(%q", fmtCall, format.String())
	for _, value := range expressions {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("empty interpolation expression")
		}
		fmt.Fprintf(&result, ", %s", value)
	}
	result.WriteByte(')')
	return result.String(), nil
}

func expressionTokensSource(tokens []Token) string {
	var result strings.Builder
	var previous *Token
	for index := range tokens {
		token := tokens[index]
		if token.Kind == TokenComment || token.Kind == TokenNewline || token.Kind == TokenEOF {
			continue
		}
		if previous != nil && expressionTokensNeedSpace(*previous, token) {
			result.WriteByte(' ')
		}
		result.WriteString(token.Text)
		copyToken := token
		previous = &copyToken
	}
	return result.String()
}

// tokenExpressionSource preserves statement-like separators inside a
// token-preserving expression fallback. Compact expression printing is
// sufficient for ordinary expressions, but multiline anonymous struct types
// use newlines as field separators; dropping them joins fields into invalid
// Go. Newlines inside calls and indexes remain ordinary whitespace.
func tokenExpressionSource(tokens []Token) string {
	var result strings.Builder
	var previous *Token
	braceDepth, parenDepth, bracketDepth := 0, 0, 0
	for index := range tokens {
		token := tokens[index]
		if token.Kind == TokenEOF {
			continue
		}
		if token.Kind == TokenNewline {
			if braceDepth > 0 && parenDepth == 0 && bracketDepth == 0 && previous != nil && previous.Text != "{" && previous.Text != ";" && previous.Text != "," {
				result.WriteByte(';')
			} else {
				result.WriteByte(' ')
			}
			previous = nil
			continue
		}
		if token.Kind == TokenComment {
			if previous != nil {
				result.WriteByte(' ')
			}
			result.WriteString(token.Text)
			previous = nil
			continue
		}
		if previous != nil && expressionTokensNeedSpace(*previous, token) {
			result.WriteByte(' ')
		}
		result.WriteString(token.Text)
		switch token.Text {
		case "{":
			braceDepth++
		case "}":
			if braceDepth > 0 {
				braceDepth--
			}
		case "(":
			parenDepth++
		case ")":
			if parenDepth > 0 {
				parenDepth--
			}
		case "[":
			bracketDepth++
		case "]":
			if bracketDepth > 0 {
				bracketDepth--
			}
		}
		copyToken := token
		previous = &copyToken
	}
	return strings.TrimSpace(result.String())
}

func expressionTokensNeedSpace(previous, current Token) bool {
	word := func(token Token) bool {
		return token.Kind == TokenIdentifier || token.Kind == TokenKeyword || token.Kind == TokenNumber
	}
	if word(previous) && word(current) {
		return true
	}
	if previous.Kind != TokenOperator && current.Kind != TokenOperator {
		return false
	}
	// Keep two adjacent operators from being fused into a different token
	// when the original expression contained whitespace, for example `x - -1`
	// becoming `x--1`.
	fused := previous.Text + current.Text
	if fused == "//" || fused == "/*" {
		return true
	}
	for _, operator := range goPlusOperators {
		if fused == operator {
			return true
		}
	}
	return false
}

func findInterpolationEnd(src string, start int) (int, error) {
	parenDepth, braceDepth, bracketDepth := 0, 0, 0
	for i := start; i < len(src); i++ {
		if src[i] == '"' || src[i] == '\'' {
			end, err := skipQuoted(src, i, src[i])
			if err != nil {
				return 0, err
			}
			i = end
			continue
		}
		if src[i] == '`' {
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return 0, fmt.Errorf("unterminated raw string in interpolation")
			}
			i += end + 1
			continue
		}
		if src[i] == '/' && i+1 < len(src) && src[i+1] == '/' {
			end := strings.IndexByte(src[i+2:], '\n')
			if end < 0 {
				return 0, fmt.Errorf("unterminated interpolation")
			}
			i += end + 2
			continue
		}
		if src[i] == '/' && i+1 < len(src) && src[i+1] == '*' {
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return 0, fmt.Errorf("unterminated block comment in interpolation")
			}
			i += end + 3
			continue
		}
		if parenDepth == 0 && braceDepth == 0 && bracketDepth == 0 && src[i] == '}' && i+1 < len(src) && src[i+1] == '}' {
			return i, nil
		}
		switch src[i] {
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
		}
	}
	return 0, fmt.Errorf("unterminated interpolation")
}

func interpolationFormatSeparator(body string) int {
	parenDepth, braceDepth, bracketDepth := 0, 0, 0
	separator := -1
	lastTopLevelColon := -1
	for i := 0; i < len(body); i++ {
		if body[i] == '"' || body[i] == '\'' {
			end, err := skipQuoted(body, i, body[i])
			if err != nil {
				return -1
			}
			i = end
			continue
		}
		if body[i] == '`' {
			end := strings.IndexByte(body[i+1:], '`')
			if end < 0 {
				return -1
			}
			i += end + 1
			continue
		}
		switch body[i] {
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
				lastTopLevelColon = i
				if strings.HasPrefix(strings.TrimSpace(body[i+1:]), "%") {
					separator = i
				}
			}
		}
	}
	if separator < 0 {
		return lastTopLevelColon
	}
	return separator
}

func validateInterpolationFormat(formatSpec string) error {
	if !strings.HasPrefix(formatSpec, "%") {
		return fmt.Errorf("invalid interpolation format: expected Go fmt format beginning with %%")
	}
	for _, character := range formatSpec[1:] {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') {
			return nil
		}
	}
	return fmt.Errorf("invalid interpolation format: missing fmt verb")
}
