package compiler

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// pruneUnusedImports removes imports that were needed only while compiling
// Go++ syntax, such as an annotation qualifier, but have no reference in the
// emitted Go program. Blank and dot imports retain their Go semantics.
func pruneUnusedImports(source string) (string, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "generated.go", source, parser.ParseComments)
	if err != nil {
		return source, nil
	}
	used := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if packageName, ok := selector.X.(*ast.Ident); ok {
			used[packageName.Name] = true
		}
		return true
	})
	declarations := file.Decls[:0]
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.IMPORT {
			declarations = append(declarations, declaration)
			continue
		}
		specifications := group.Specs[:0]
		for _, specification := range group.Specs {
			imported, ok := specification.(*ast.ImportSpec)
			if !ok {
				specifications = append(specifications, specification)
				continue
			}
			importPath, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				specifications = append(specifications, specification)
				continue
			}
			name := path.Base(importPath)
			if imported.Name != nil {
				name = imported.Name.Name
			}
			if name == "_" || name == "." || used[name] {
				specifications = append(specifications, specification)
			}
		}
		group.Specs = specifications
		if len(specifications) > 0 {
			declarations = append(declarations, group)
		}
	}
	file.Decls = declarations
	file.Imports = file.Imports[:0]
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.IMPORT {
			continue
		}
		for _, specification := range group.Specs {
			if imported, ok := specification.(*ast.ImportSpec); ok {
				file.Imports = append(file.Imports, imported)
			}
		}
	}
	var output strings.Builder
	if err := format.Node(&output, fileSet, file); err != nil {
		return source, err
	}
	return output.String(), nil
}
