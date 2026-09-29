package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

func cronFunctionRegistrationSource(function *FunctionDecl, context constructorContext) (string, error) {
	if function == nil {
		return "", nil
	}
	for _, use := range function.Annotations {
		declaration := use.Declaration
		if declaration == nil || declaration.Package != "gpp.cron" || (declaration.Name != "Cron" && declaration.Name != "Every") {
			continue
		}
		if len(function.Method.TypeParamsAST) != 0 || len(function.Method.ParameterAST) != 1 {
			return "", fmt.Errorf("%s: cron function %s must accept context.Context and return error", annotationUseLocation(use), function.Name)
		}
		parameterType, err := typeNodeSource(function.Method.ParameterAST[0].Type)
		if err != nil || strings.TrimSpace(parameterType) != "context.Context" || strings.TrimSpace(methodResultSource(function.Method)) != "error" {
			return "", fmt.Errorf("%s: cron function %s must have signature func(context.Context) error", annotationUseLocation(use), function.Name)
		}
		arguments := use.ArgumentTexts()
		if len(arguments) != 1 {
			return "", fmt.Errorf("%s: %s requires one argument", annotationUseLocation(use), declaration.Name)
		}
		register := "Register"
		schedule := strings.TrimSpace(arguments[0])
		if declaration.Name == "Cron" {
			if _, err := strconv.Unquote(schedule); err != nil {
				return "", fmt.Errorf("%s: Cron expression must be a string literal", annotationUseLocation(use))
			}
		} else {
			register = "RegisterEvery"
		}
		qualifier, err := cronAnnotationQualifier(use, context)
		if err != nil {
			return "", fmt.Errorf("%s: %w", annotationUseLocation(use), err)
		}
		if qualifier != "" {
			register = qualifier + "." + register
		}
		functionName := function.Name
		if overloaded := overloadFunctionName(function, context.Overloads); overloaded != "" {
			functionName = overloaded
		}
		return fmt.Sprintf("\nfunc init() { if err := %s(%q, %s, %s); err != nil { panic(err) } }\n", register, context.Package+"."+functionName, schedule, functionName), nil
	}
	return "", nil
}

func cronAnnotationQualifier(use AnnotationUse, context constructorContext) (string, error) {
	dot := strings.LastIndex(use.Name, ".")
	if dot < 0 {
		return "", fmt.Errorf("%s must be imported from gpp/cron with a package qualifier", use.Declaration.Name)
	}
	qualifier := use.Name[:dot]
	importPath := context.AvailableImports[qualifier]
	logical, ok := officialLogicalPackage(importPath)
	if !ok && context.ModulePath != "" && strings.HasPrefix(importPath, context.ModulePath+"/") {
		logical = strings.ReplaceAll(strings.TrimPrefix(importPath, context.ModulePath+"/"), "/", ".")
		ok = true
	}
	if !ok || logical != "gpp.cron" {
		return "", fmt.Errorf("%s annotations must come from gpp/cron", use.Declaration.Name)
	}
	return qualifier, nil
}
