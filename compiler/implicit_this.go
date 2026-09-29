package compiler

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// qualifyImplicitClassFields adds the receiver to unqualified field references
// in an instance method. Method bodies are parsed after Go++ lowering so this
// works for both the structured and compatibility emission paths.
func qualifyImplicitClassFields(body string, class *ClassDecl, classes map[string]*ClassDecl, method Method) string {
	if class == nil || strings.TrimSpace(body) == "" {
		return body
	}
	fields := map[string]bool{}
	for _, candidate := range classesForClass(constructorContext{Targets: map[string]constructorTarget{class.Name: {Class: class, Classes: classes}}}, class) {
		for _, field := range candidate.Fields {
			fields[field.Name] = true
		}
	}
	if len(fields) == 0 {
		return body
	}

	const prefix = "package gppimplicit\nfunc __method() {\n"
	source := prefix + body + "\n}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "implicit.go", source, parser.AllErrors)
	if err != nil || len(file.Decls) == 0 {
		return body
	}
	decl, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok || decl.Body == nil {
		return body
	}

	// A local with the same name as a field wins throughout this body. The
	// conservative whole-body scan keeps nested closures and short declarations
	// from accidentally capturing a field.
	locals := map[string]bool{}
	for _, parameter := range method.ParameterAST {
		if parameter.Name != "" && parameter.Name != "_" {
			locals[parameter.Name] = true
		}
	}
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.ValueSpec:
			for _, name := range value.Names {
				locals[name.Name] = true
			}
		case *ast.AssignStmt:
			if value.Tok == token.DEFINE {
				for _, expression := range value.Lhs {
					if name, ok := expression.(*ast.Ident); ok {
						locals[name.Name] = true
					}
				}
			}
		case *ast.RangeStmt:
			if value.Tok == token.DEFINE {
				for _, expression := range []ast.Expr{value.Key, value.Value} {
					if name, ok := expression.(*ast.Ident); ok {
						locals[name.Name] = true
					}
				}
			}
		case *ast.FuncLit:
			if value.Type.Params != nil {
				for _, field := range value.Type.Params.List {
					for _, name := range field.Names {
						locals[name.Name] = true
					}
				}
			}
		case *ast.TypeSpec:
			locals[value.Name.Name] = true
		}
		return true
	})

	type edit struct{ start, end int }
	var edits []edit
	var parents []ast.Node
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		if node == nil {
			if len(parents) > 0 {
				parents = parents[:len(parents)-1]
			}
			return false
		}
		if ident, ok := node.(*ast.Ident); ok && fields[ident.Name] && !locals[ident.Name] {
			parent := ast.Node(nil)
			if len(parents) > 0 {
				parent = parents[len(parents)-1]
			}
			if !implicitFieldReference(parent, ident) {
				parents = append(parents, node)
				return true
			}
			start := fset.Position(ident.Pos()).Offset - len(prefix)
			end := fset.Position(ident.End()).Offset - len(prefix)
			if start >= 0 && end <= len(body) {
				edits = append(edits, edit{start, end})
			}
		}
		parents = append(parents, node)
		return true
	})
	if len(edits) == 0 {
		return body
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, change := range edits {
		body = body[:change.start] + "this." + body[change.start:change.end] + body[change.end:]
	}
	return body
}

func implicitFieldReference(parent ast.Node, ident *ast.Ident) bool {
	switch value := parent.(type) {
	case *ast.SelectorExpr:
		return value.Sel != ident
	case *ast.KeyValueExpr:
		return value.Key != ident
	case *ast.Field:
		return false
	case *ast.ValueSpec:
		return false
	case *ast.TypeSpec:
		return false
	case *ast.FuncDecl:
		return value.Name != ident
	case *ast.ImportSpec:
		return false
	case *ast.LabeledStmt:
		return value.Label != ident
	case *ast.BranchStmt:
		return value.Label != ident
	case *ast.CompositeLit:
		return value.Type != ident
	case *ast.TypeAssertExpr:
		return value.Type != ident
	case *ast.ArrayType:
		return value.Elt != ident
	case *ast.StarExpr:
		return value.X != ident
	case *ast.MapType:
		return value.Key != ident && value.Value != ident
	case *ast.InterfaceType, *ast.StructType, *ast.FuncType, *ast.ChanType:
		return false
	}
	return true
}

func formatImplicitThisBody(body *ast.BlockStmt, class *ClassDecl, classes map[string]*ClassDecl, method Method) (*ast.BlockStmt, error) {
	if body == nil {
		return body, nil
	}
	var source bytes.Buffer
	if err := format.Node(&source, token.NewFileSet(), body); err != nil {
		return nil, err
	}
	rewritten := qualifyImplicitClassFields(source.String(), class, classes, method)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "implicit.go", "package gppimplicit\nfunc __method() "+rewritten, parser.AllErrors)
	if err != nil {
		return nil, err
	}
	declaration, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok {
		return nil, nil
	}
	return declaration.Body, nil
}
