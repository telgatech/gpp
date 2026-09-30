package compiler

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

// validateAtExit recognizes the optional package-main shutdown hook and
// enforces the signature used by the generated entry point.
func validateAtExit(program *Program) (bool, error) {
	var found bool
	for _, file := range program.Files {
		if file == nil || file.Package != "main" {
			continue
		}
		for _, declaration := range file.Decls {
			switch value := declaration.(type) {
			case *FunctionDecl:
				if value.Name != "atExit" {
					continue
				}
				if found {
					return false, fmt.Errorf("%s:%d: atExit may be declared only once in package main", value.SourceFile, value.SourceLine)
				}
				found = true
				if len(value.Method.ParameterAST) != 0 || value.Method.ResultAST != nil || len(value.Method.ResultFieldsAST) != 0 {
					return false, fmt.Errorf("%s:%d: atExit must have no parameters and no return values", value.SourceFile, value.SourceLine)
				}
			case *GoDecl:
				for _, goDeclaration := range value.Declarations {
					function, ok := goDeclaration.(*ast.FuncDecl)
					if !ok || function.Recv != nil || function.Name.Name != "atExit" {
						continue
					}
					if found {
						return false, fmt.Errorf("%s:%d: atExit may be declared only once in package main", value.SourceFile, value.SourceLine)
					}
					found = true
					if function.Type.Params.NumFields() != 0 || function.Type.Results != nil && function.Type.Results.NumFields() != 0 {
						return false, fmt.Errorf("%s:%d: atExit must have no parameters and no return values", value.SourceFile, value.SourceLine)
					}
				}
			}
		}
	}
	return found, nil
}

func injectAtExitDefer(source string) (string, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "generated.go", "package main\n"+source, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("could not add atExit call to generated main: %w", err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || function.Name.Name != "main" || function.Body == nil {
			continue
		}
		call := &ast.CallExpr{Fun: ast.NewIdent("atExit")}
		function.Body.List = append([]ast.Stmt{&ast.DeferStmt{Call: call}}, function.Body.List...)
		var output strings.Builder
		if err := format.Node(&output, fileSet, function); err != nil {
			return "", fmt.Errorf("could not format generated main with atExit call: %w", err)
		}
		return output.String(), nil
	}
	return "", fmt.Errorf("could not add atExit call: generated main function was not found")
}
