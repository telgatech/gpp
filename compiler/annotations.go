package compiler

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

func validateAnnotationDeclarations(pkg *PackageSymbols) error {
	for _, declaration := range pkg.Annotations {
		if _, err := annotationParameterInfos(declaration); err != nil {
			return fmt.Errorf("%s: annotation %s has invalid parameters: %w", annotationLocation(declaration), declaration.Name, err)
		}
	}
	return nil
}

func validateFileAnnotations(file *File, pkg *PackageSymbols, scope map[string]*AnnotationDecl, allowUnresolvedQualified bool) error {
	validate := func(uses []AnnotationUse, target AnnotationTarget) error {
		return validateAnnotationUses(uses, target, pkg, scope, allowUnresolvedQualified)
	}
	if err := validate(file.Annotations, AnnotationTargetPackage); err != nil {
		return err
	}
	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *ClassDecl:
			if err := validate(value.Annotations, AnnotationTargetClass); err != nil {
				return err
			}
			for _, field := range value.Fields {
				if err := validate(field.Annotations, AnnotationTargetField); err != nil {
					return err
				}
			}
			for _, method := range value.Methods {
				if err := validate(method.Annotations, AnnotationTargetMethod); err != nil {
					return err
				}
				if err := validateParameterAnnotations(method.ParameterAnnotations, pkg, scope, allowUnresolvedQualified); err != nil {
					return err
				}
			}
		case *ExtendDecl:
			for _, method := range value.Methods {
				if err := validate(method.Annotations, AnnotationTargetMethod); err != nil {
					return err
				}
				if err := validateParameterAnnotations(method.ParameterAnnotations, pkg, scope, allowUnresolvedQualified); err != nil {
					return err
				}
			}
		case *TemplateDecl:
			if err := validate(value.Annotations, AnnotationTargetTemplate); err != nil {
				return err
			}
		case *FunctionDecl:
			if err := validate(value.Annotations, AnnotationTargetFunction); err != nil {
				return err
			}
			if err := validateParameterAnnotations(value.Method.ParameterAnnotations, pkg, scope, allowUnresolvedQualified); err != nil {
				return err
			}
		case *GoDecl:
			for _, placement := range value.AnnotationPlacements {
				if err := validate([]AnnotationUse{placement.Use}, placement.Target); err != nil {
					return err
				}
			}
		case *MixedDecl:
			for _, placement := range value.AnnotationPlacements {
				if err := validate([]AnnotationUse{placement.Use}, placement.Target); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateParameterAnnotations(annotations map[string][]AnnotationUse, pkg *PackageSymbols, scope map[string]*AnnotationDecl, allowUnresolvedQualified bool) error {
	for _, uses := range annotations {
		if err := validateAnnotationUses(uses, AnnotationTargetParameter, pkg, scope, allowUnresolvedQualified); err != nil {
			return err
		}
	}
	return nil
}

func validateAnnotationUses(uses []AnnotationUse, target AnnotationTarget, pkg *PackageSymbols, scope map[string]*AnnotationDecl, allowUnresolvedQualified bool) error {
	for index := range uses {
		use := &uses[index]
		declaration := scope[use.Name]
		if declaration == nil {
			if allowUnresolvedQualified && strings.Contains(use.Name, ".") {
				continue
			}
			if strings.Contains(use.Name, ".") {
				return fmt.Errorf("%s: undefined annotation %s", annotationUseLocation(*use), use.Name)
			}
			if pkg.Functions[use.Name] {
				return fmt.Errorf("%s: %s is a function, not an annotation", annotationUseLocation(*use), use.Name)
			}
			return fmt.Errorf("%s: undefined annotation %s", annotationUseLocation(*use), use.Name)
		}
		use.Declaration = declaration
		if len(declaration.Targets) > 0 && !containsAnnotationTarget(declaration.Targets, target) {
			return fmt.Errorf(
				"%s: annotation %s cannot be applied to %s; allowed targets: %s",
				annotationUseLocation(*use), use.Name, target, strings.Join(annotationTargetNames(declaration.Targets), ", "),
			)
		}
		if err := validateAnnotationArguments(*use, declaration); err != nil {
			return fmt.Errorf("%s: %w", annotationUseLocation(*use), err)
		}
	}
	return nil
}

func containsAnnotationTarget(targets []AnnotationTarget, target AnnotationTarget) bool {
	for _, candidate := range targets {
		if candidate == target {
			return true
		}
	}
	return false
}

func annotationTargetNames(targets []AnnotationTarget) []string {
	result := make([]string, len(targets))
	for index, target := range targets {
		result[index] = string(target)
	}
	return result
}

func annotationUseLocation(use AnnotationUse) string {
	if use.SourceLine > 0 {
		return fmt.Sprintf("%s:%d", use.SourceFile, use.SourceLine)
	}
	return use.SourceFile
}

func validateAnnotationArguments(use AnnotationUse, declaration *AnnotationDecl) error {
	parameters, err := annotationParameterInfos(declaration)
	if err != nil {
		return err
	}
	args := []string{}
	if use.HasArguments {
		args = annotationArgumentTexts(use)
		if len(args) == 1 && strings.TrimSpace(args[0]) == "" {
			args = nil
		}
	}
	signature := callableSignature{Name: declaration.Name, Parameters: parameters}
	if len(parameters) == 1 && strings.HasPrefix(strings.TrimSpace(parameters[0].typeText()), "...") {
		if !use.HasArguments || len(args) == 0 {
			return nil
		}
		variadicType := strings.TrimPrefix(strings.TrimSpace(parameters[0].typeText()), "...")
		if variadicType == "any" {
			return nil
		}
		argumentNodes, err := annotationArgumentNodes(use, args)
		if err != nil {
			return fmt.Errorf("invalid annotation arguments for %s: %w", declaration.Name, err)
		}
		for index, argument := range argumentNodes {
			actual := annotationExpressionTypeKey(argument)
			if actual != variadicType {
				return fmt.Errorf("argument %d to annotation %s: expected %s, got %s", index+1, declaration.Name, variadicType, actual)
			}
		}
		return nil
	}
	resolved, changed, resolveErr := resolveCallableCall(declaration.Name, args, []callableSignature{signature})
	if resolveErr != nil || !changed {
		return fmt.Errorf("annotation %s expects %d argument(s), got %d", declaration.Name, len(parameters), len(args))
	}
	for index, argument := range resolved {
		if index >= len(parameters) {
			break
		}
		parsed, err := parseAnnotationArgumentNode(argument)
		if err != nil {
			return fmt.Errorf("invalid argument %d to annotation %s: %w", index+1, declaration.Name, err)
		}
		actual := annotationExpressionTypeKey(parsed)
		expected := strings.Join(strings.Fields(parameters[index].typeText()), " ")
		if actual == "" {
			if declaration.Package == "gpp.cron" && declaration.Name == "Every" && expected == "time.Duration" {
				// Duration expressions commonly combine imported constants such
				// as time.Second. Their exact types are resolved by Go when the
				// generated RegisterEvery call is compiled.
				continue
			}
			if isSelectorAnnotationExpression(parsed) && isNamedAnnotationType(expected) {
				// Imported enum members are selectors (for example
				// test.High), but their declared enum type is resolved in the
				// imported package rather than by the small expression typer.
				continue
			}
		}
		if actual == "" || actual == "nil" && !isNilableGoType(expected) {
			return fmt.Errorf("argument %d to annotation %s: expected %s", index+1, declaration.Name, expected)
		}
		if actual != expected && expected != "any" && !annotationNumericCompatible(actual, expected) {
			return fmt.Errorf("argument %d to annotation %s: expected %s, got %s", index+1, declaration.Name, expected, actual)
		}
	}
	return nil
}

// annotationArgumentTexts renders argument token groups only at the point
// where an existing semantic helper still consumes source-shaped values. The
// annotation AST itself retains tokens and parsed expressions, not a
// duplicate comma-separated source string.
func annotationArgumentTexts(use AnnotationUse) []string {
	return use.ArgumentTexts()
}

// annotationArgumentNodes keeps annotation validation on the Go++ expression
// AST. The legacy validator used go/parser.ParseExpr here, which meant that a
// valid Go++ argument could be rejected or silently interpreted using Go's
// syntax instead of the language frontend's syntax.
func annotationArgumentNodes(use AnnotationUse, args []string) ([]ExprNode, error) {
	if len(use.ArgumentsAST) == len(args) && !annotationArgumentsHaveNames(args) {
		return use.ArgumentsAST, nil
	}
	result := make([]ExprNode, 0, len(args))
	for index, argument := range args {
		node, err := parseAnnotationArgumentNode(argument)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", index+1, err)
		}
		result = append(result, node)
	}
	return result, nil
}

func annotationArgumentsHaveNames(args []string) bool {
	for _, argument := range args {
		tokens, err := LexSource("annotation argument", argument)
		if err != nil {
			continue
		}
		filtered := significantSyntaxTokens(tokens)
		if len(filtered) >= 2 &&
			(filtered[0].Kind == TokenIdentifier || filtered[0].Kind == TokenKeyword) &&
			filtered[1].Text == ":" {
			return true
		}
	}
	return false
}

func parseAnnotationArgumentNode(argument string) (ExprNode, error) {
	tokens, err := LexSource("annotation argument", strings.TrimSpace(argument))
	if err != nil {
		return nil, err
	}
	node, err := ParseExpressionTokens(tokens)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, fmt.Errorf("expected expression")
	}
	return node, nil
}

func annotationExpressionTypeKey(expression ExprNode) string {
	switch value := expression.(type) {
	case *LiteralExpr:
		switch value.Kind {
		case TokenString, TokenRawString:
			return "string"
		case TokenRune:
			return "rune"
		case TokenNumber:
			if strings.ContainsAny(value.Text, ".eEpP") {
				return "float64"
			}
			return "int"
		}
		switch value.Text {
		case "true", "false":
			return "bool"
		case "nil":
			return "nil"
		}
	case *InterpolatedStringExpr:
		return "string"
	case *CompositeLiteralExpr:
		typeName, err := typeNodeSource(value.Type)
		if err == nil {
			return typeName
		}
	case *UnaryExpr:
		if value.Operator == "&" {
			if operand := annotationExpressionTypeKey(value.Operand); operand != "" {
				return "*" + operand
			}
		}
	case *NameExpr:
		return value.Name
	case *ParenthesizedExpr:
		return annotationExpressionTypeKey(value.Inner)
	case *IndexListExpr:
		return ""
	case *SliceExpr:
		return annotationExpressionTypeKey(value.Receiver)
	case *TypeAssertExpr:
		if !value.TypeSwitch && value.Type != nil {
			if typeName, err := typeNodeSource(value.Type); err == nil {
				return typeName
			}
		}
	case *PostfixExpr:
		return annotationExpressionTypeKey(value.Expression)
	case *SpreadExpr:
		return annotationExpressionTypeKey(value.Expression)
	case *TypeExpr:
		if value.Type != nil {
			if typeName, err := typeNodeSource(value.Type); err == nil {
				return typeName
			}
		}
	case *SendExpr:
		return annotationExpressionTypeKey(value.Value)
	case *FunctionLiteralExpr:
		if value.Type != nil {
			if typeName, err := typeNodeSource(value.Type); err == nil {
				return typeName
			}
		}
	}
	return ""
}

func isSelectorAnnotationExpression(expression ExprNode) bool {
	for {
		parenthesized, ok := expression.(*ParenthesizedExpr)
		if !ok {
			break
		}
		expression = parenthesized.Inner
	}
	_, ok := expression.(*SelectorExpr)
	return ok
}

func isNamedAnnotationType(typeName string) bool {
	if strings.ContainsAny(typeName, "[]{}()*") {
		return false
	}
	switch typeName {
	case "any", "bool", "string", "byte", "rune", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128", "error":
		return false
	default:
		return typeName != ""
	}
}

func annotationNumericCompatible(actual, expected string) bool {
	if actual == "int" && (expected == "int64" || expected == "float64") {
		return true
	}
	return actual == "float64" && expected == "complex128"
}

func annotationUsesLiteral(uses []AnnotationUse, context constructorContext) string {
	if len(uses) == 0 {
		return "GppAnnotations{}"
	}
	parts := make([]string, 0, len(uses))
	for _, use := range uses {
		declaration := use.Declaration
		if declaration == nil {
			declaration = context.Annotations[use.Name]
		}
		if declaration == nil {
			continue
		}
		arguments := "nil"
		args := []string{}
		if use.HasArguments {
			args = annotationArgumentTexts(use)
		}
		resolved, changed, resolveErr := resolveCallableCall(
			declaration.Name,
			args,
			[]callableSignature{{Name: declaration.Name, Parameters: mustAnnotationParameterInfos(declaration)}},
		)
		if resolveErr == nil && changed {
			args = resolved
		}
		for index := range args {
			trimmed := strings.TrimSpace(args[index])
			if enum, ok := context.Enums[trimmed]; ok {
				if dot := strings.LastIndex(trimmed, "."); dot >= 0 {
					if member, exists := enumMember(enum, trimmed[dot+1:]); exists {
						args[index] = enumGeneratedMember(trimmed, enum, member.Name)
					}
				}
			}
			if transformed, err := transformEnums(args[index], context); err == nil {
				args[index] = transformed
			}
		}
		if len(args) > 0 {
			arguments = "[]any{" + strings.Join(args, ", ") + "}"
		}
		parts = append(parts, fmt.Sprintf(
			"GppAnnotation{Name: %q, FullName: %q, Args: %s, Type: %s}",
			declaration.Name,
			annotationFullName(declaration),
			arguments,
			annotationDescriptorReferenceForContext(use, declaration, context),
		))
	}
	return "GppAnnotations{" + strings.Join(parts, ", ") + "}"
}

func annotationDescriptorReferenceForContext(use AnnotationUse, declaration *AnnotationDecl, context constructorContext) string {
	if dot := strings.LastIndex(use.Name, "."); dot >= 0 {
		return use.Name[:dot] + ".GppAnnotation_" + declaration.Name
	}
	if declaration.Package == "" || declaration.Package == "main" || declaration.Package == context.Package {
		return "GppAnnotation_" + declaration.Name
	}
	for alias, importPath := range context.AvailableImports {
		logical, ok := officialLogicalPackage(importPath)
		if !ok && context.ModulePath != "" && strings.HasPrefix(importPath, context.ModulePath+"/") {
			logical = strings.ReplaceAll(strings.TrimPrefix(importPath, context.ModulePath+"/"), "/", ".")
			ok = true
		}
		if ok && logical == declaration.Package {
			if alias == "." {
				return "GppAnnotation_" + declaration.Name
			}
			return alias + ".GppAnnotation_" + declaration.Name
		}
	}
	return "GppAnnotation_" + declaration.Name
}

func annotationParameterInfos(declaration *AnnotationDecl) ([]parameterInfo, error) {
	if declaration != nil {
		return parameterInfosFromNodes(declaration.ParameterAST)
	}
	return nil, nil
}

func annotationParameterSource(declaration *AnnotationDecl) string {
	if declaration == nil {
		return ""
	}
	source, err := parameterNodesSource(declaration.ParameterAST)
	if err != nil {
		return ""
	}
	return source
}

func mustAnnotationParameterInfos(declaration *AnnotationDecl) []parameterInfo {
	parameters, _ := annotationParameterInfos(declaration)
	return parameters
}

func annotationFullName(declaration *AnnotationDecl) string {
	if declaration.Package == "" || declaration.Package == "main" {
		return declaration.Name
	}
	return declaration.Package + "." + declaration.Name
}

func annotationDescriptorReference(use AnnotationUse, declaration *AnnotationDecl) string {
	name := "GppAnnotation_" + declaration.Name
	if dot := strings.LastIndex(use.Name, "."); dot >= 0 {
		return use.Name[:dot] + "." + name
	}
	return name
}

func emitAnnotationDescriptors(out *strings.Builder, file *File, context constructorContext) {
	names := make([]string, 0, len(context.Annotations))
	for name := range context.Annotations {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		declaration := context.Annotations[name]
		if declaration.SourceFile != "" && declaration.SourceFile != file.Name {
			continue
		}
		fmt.Fprintf(out, "var GppAnnotation_%s = &GppAnnotationType{Name: %q, FullName: %q}\n", declaration.Name, declaration.Name, annotationFullName(declaration))
	}
	if len(context.Annotations) > 0 {
		out.WriteByte('\n')
	}
}

func annotationScopeForFile(file *File, model *SemanticModel, modulePath string) (map[string]*AnnotationDecl, error) {
	scope := map[string]*AnnotationDecl{}
	local := model.Packages[file.Package]
	if local != nil {
		for name, declaration := range local.Annotations {
			scope[name] = declaration
		}
	}
	imports, err := goImports(file)
	if err != nil {
		return scope, nil
	}
	for _, spec := range imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		logical, ok := logicalPackageForImport(importPath, modulePath, model)
		if !ok {
			continue
		}
		pkg := model.Packages[logical]
		if pkg == nil {
			continue
		}
		alias := path.Base(importPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias == "_" {
			continue
		}
		for name, declaration := range pkg.Annotations {
			if !declaration.Exported {
				continue
			}
			if alias == "." {
				scope[name] = declaration
			} else {
				scope[alias+"."+name] = declaration
			}
		}
	}
	return scope, nil
}
