package compiler

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

// generatedMemberName maps protected GPP names to exported implementation
// names so a derived class in another Go package can reach the member. The
// generated spelling is an implementation detail; source tooling keeps the
// original name.
func generatedMemberName(name string) string {
	if strings.HasPrefix(name, "_") {
		return "GppProtected" + name
	}
	return name
}

func rewriteProtectedMemberSelectors(body *ast.BlockStmt, class *ClassDecl, classes map[string]*ClassDecl, method Method) {
	if body == nil || class == nil {
		return
	}
	lineage := map[string]bool{}
	var collect func(*ClassDecl, map[*ClassDecl]bool)
	collect = func(current *ClassDecl, visiting map[*ClassDecl]bool) {
		if current == nil || visiting[current] {
			return
		}
		visiting[current] = true
		defer delete(visiting, current)
		lineage[current.Name] = true
		for _, parentName := range classParentNames(current) {
			lineage[parentName] = true
			collect(classes[parentName], visiting)
		}
	}
	collect(class, map[*ClassDecl]bool{})
	protectedNames := map[string]bool{}
	for _, candidate := range classes {
		if !lineage[candidate.Name] {
			continue
		}
		for _, field := range candidate.Fields {
			if memberVisibility(field.Name) == memberProtected {
				protectedNames[field.Name] = true
			}
		}
		for _, candidateMethod := range candidate.Methods {
			if memberVisibility(candidateMethod.Name) == memberProtected {
				protectedNames[candidateMethod.Name] = true
				if candidateMethod.GoName != "" {
					protectedNames[candidateMethod.GoName] = true
				}
			}
		}
	}
	typedReceivers := map[string]bool{"this": true}
	for _, parameter := range method.ParameterAST {
		if parameter.Name == "" {
			continue
		}
		typeName, err := typeNodeSource(parameter.Type)
		if err == nil && classTypeInLineage(typeName, lineage) {
			typedReceivers[parameter.Name] = true
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || !protectedNames[selector.Sel.Name] {
			return true
		}
		root := selectorRootName(selector.X)
		if typedReceivers[root] || lineage[root] {
			selector.Sel.Name = generatedMemberName(selector.Sel.Name)
		}
		return true
	})
}

func selectorRootName(expression ast.Expr) string {
	for {
		switch value := expression.(type) {
		case *ast.SelectorExpr:
			expression = value.X
		case *ast.ParenExpr:
			expression = value.X
		default:
			if identifier, ok := expression.(*ast.Ident); ok {
				return identifier.Name
			}
			return ""
		}
	}
}

func classTypeInLineage(typeName string, lineage map[string]bool) bool {
	typeName = strings.TrimSpace(typeName)
	for strings.HasPrefix(typeName, "*") {
		typeName = strings.TrimSpace(strings.TrimPrefix(typeName, "*"))
	}
	if bracket := strings.IndexAny(typeName, "[< "); bracket >= 0 {
		typeName = typeName[:bracket]
	}
	if dot := strings.LastIndexByte(typeName, '.'); dot >= 0 {
		typeName = typeName[dot+1:]
	}
	return lineage[typeName]
}

func rewriteProtectedMemberSource(body string, class *ClassDecl, classes map[string]*ClassDecl, method Method) string {
	if strings.TrimSpace(body) == "" {
		return body
	}
	const prefix = "package gppprotected\nfunc __body() {\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "protected.go", prefix+body+"\n}\n", parser.AllErrors)
	if err != nil || len(file.Decls) == 0 {
		return body
	}
	function, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok || function.Body == nil {
		return body
	}
	rewriteProtectedMemberSelectors(function.Body, class, classes, method)
	var output bytes.Buffer
	if err := format.Node(&output, fset, function.Body); err != nil {
		return body
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(output.String(), "{"), "}"))
}
