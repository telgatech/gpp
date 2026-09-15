package compiler

import (
	"fmt"
	"go/parser"
	"path"
	"sort"
	"strconv"
	"strings"
)

func validateAnnotationDeclarations(pkg *PackageSymbols) error {
	for _, declaration := range pkg.Annotations {
		if _, err := parseParameterInfos(declaration.Params); err != nil {
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
		case *RawDecl:
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
	parameters, err := parseParameterInfos(declaration.Params)
	if err != nil {
		return err
	}
	args := []string{}
	if use.HasArguments {
		args, err = splitTopLevel(use.Arguments, ',')
		if err != nil {
			return err
		}
		if len(args) == 1 && strings.TrimSpace(args[0]) == "" {
			args = nil
		}
	}
	signature := callableSignature{Name: declaration.Name, Parameters: parameters}
	resolved, changed, resolveErr := resolveCallableCall(declaration.Name, args, []callableSignature{signature})
	if resolveErr != nil || !changed {
		return fmt.Errorf("annotation %s expects %d argument(s), got %d", declaration.Name, len(parameters), len(args))
	}
	for index, argument := range resolved {
		if index >= len(parameters) {
			break
		}
		parsed, err := parser.ParseExpr(strings.TrimSpace(argument))
		if err != nil {
			return fmt.Errorf("invalid argument %d to annotation %s: %w", index+1, declaration.Name, err)
		}
		actual := astExpressionTypeKey(parsed)
		expected := strings.Join(strings.Fields(parameters[index].Type), " ")
		if actual == "" || actual == "nil" && !isNilableGoType(expected) {
			return fmt.Errorf("argument %d to annotation %s: expected %s", index+1, declaration.Name, expected)
		}
		if actual != expected && expected != "any" && !annotationNumericCompatible(actual, expected) {
			return fmt.Errorf("argument %d to annotation %s: expected %s, got %s", index+1, declaration.Name, expected, actual)
		}
	}
	return nil
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
		if use.HasArguments && strings.TrimSpace(use.Arguments) != "" {
			args, err := splitTopLevel(use.Arguments, ',')
			if err == nil {
				resolved, changed, resolveErr := resolveCallableCall(
					declaration.Name,
					args,
					[]callableSignature{{Name: declaration.Name, Parameters: mustParameterInfos(declaration.Params)}},
				)
				if resolveErr == nil && changed {
					args = resolved
				}
				arguments = "[]any{" + strings.Join(args, ", ") + "}"
			}
		}
		parts = append(parts, fmt.Sprintf(
			"GppAnnotation{Name: %q, FullName: %q, Args: %s, Type: %s}",
			declaration.Name,
			annotationFullName(declaration),
			arguments,
			annotationDescriptorReference(use, declaration),
		))
	}
	return "GppAnnotations{" + strings.Join(parts, ", ") + "}"
}

func mustParameterInfos(params string) []parameterInfo {
	parameters, _ := parseParameterInfos(params)
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
