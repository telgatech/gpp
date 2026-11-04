package command

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type unusedLocalWarning struct {
	file string
	line int
	name string
}

func (warning unusedLocalWarning) String() string {
	diagnostic := fmt.Sprintf("%s:%d: warning: declared and not used: %s", warning.file, warning.line, warning.name)
	return normalizeBackendLocation(diagnostic, "")
}

// suppressUnusedLocalErrors marks source-level local variables as used in the
// generated Go so Go's backend does not reject them. It returns source-mapped
// warnings for those variables; other backend diagnostics remain errors.
func suppressUnusedLocalErrors(root string) ([]unusedLocalWarning, error) {
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	warnings := []unusedLocalWarning{}
	for _, path := range paths {
		fileWarnings, err := suppressUnusedLocalsInFile(path)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, fileWarnings...)
	}
	return warnings, nil
}

func suppressUnusedLocalsInFile(path string) ([]unusedLocalWarning, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, 0)
	if err != nil {
		return nil, nil
	}
	parents := astParentMap(file)
	definitions := map[*ast.Object]*ast.Ident{}
	uses := map[*ast.Object]int{}
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || ident.Name == "_" || ident.Obj == nil || ident.Obj.Kind != ast.Var || strings.HasPrefix(ident.Name, "__gpp") {
			return true
		}
		if isVariableDefinition(ident, ident.Obj) {
			if isLocalVariable(ident.Obj, parents) {
				definitions[ident.Obj] = ident
			}
		} else {
			uses[ident.Obj]++
		}
		return true
	})

	byBlock := map[*ast.BlockStmt][]*ast.Ident{}
	warnings := make([]unusedLocalWarning, 0)
	for object, ident := range definitions {
		if uses[object] != 0 {
			continue
		}
		block := unusedVariableScopeBlock(object, parents)
		if block == nil {
			continue
		}
		position := fset.Position(ident.Pos())
		if position.Filename == "" || position.Line <= 0 {
			continue
		}
		warnings = append(warnings, unusedLocalWarning{file: position.Filename, line: position.Line, name: ident.Name})
		byBlock[block] = append(byBlock[block], ident)
	}
	if len(byBlock) == 0 {
		return warnings, nil
	}
	type edit struct {
		offset int
		text   string
	}
	edits := make([]edit, 0, len(byBlock))
	for block, names := range byBlock {
		position := fset.Position(block.Rbrace)
		if !position.IsValid() || position.Offset < 0 || position.Offset > len(source) {
			continue
		}
		sort.Slice(names, func(i, j int) bool { return names[i].Pos() < names[j].Pos() })
		var addition strings.Builder
		addition.WriteString("\n")
		for _, name := range names {
			fmt.Fprintf(&addition, "\t_ = %s\n", name.Name)
		}
		edits = append(edits, edit{offset: position.Offset, text: addition.String()})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].offset > edits[j].offset })
	updated := string(source)
	for _, item := range edits {
		updated = updated[:item.offset] + item.text + updated[item.offset:]
	}
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return nil, err
	}
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].file == warnings[j].file {
			if warnings[i].line == warnings[j].line {
				return warnings[i].name < warnings[j].name
			}
			return warnings[i].line < warnings[j].line
		}
		return warnings[i].file < warnings[j].file
	})
	return warnings, nil
}

func astParentMap(root ast.Node) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	stack := []ast.Node{}
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	return parents
}

func isVariableDefinition(ident *ast.Ident, object *ast.Object) bool {
	if ident == nil || object == nil {
		return false
	}
	switch declaration := object.Decl.(type) {
	case *ast.AssignStmt:
		for _, left := range declaration.Lhs {
			if left.Pos() == ident.Pos() {
				return true
			}
		}
	case *ast.ValueSpec:
		for _, name := range declaration.Names {
			if name.Pos() == ident.Pos() {
				return true
			}
		}
	case *ast.RangeStmt:
		return declaration.Key != nil && declaration.Key.Pos() == ident.Pos() || declaration.Value != nil && declaration.Value.Pos() == ident.Pos()
	}
	return false
}

func isLocalVariable(object *ast.Object, parents map[ast.Node]ast.Node) bool {
	if object == nil {
		return false
	}
	declaration, ok := object.Decl.(ast.Node)
	if !ok {
		return false
	}
	for node := declaration; node != nil; node = parents[node] {
		switch node.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return true
		case *ast.File:
			return false
		}
	}
	return false
}

func unusedVariableScopeBlock(object *ast.Object, parents map[ast.Node]ast.Node) *ast.BlockStmt {
	if object == nil {
		return nil
	}
	declaration, ok := object.Decl.(ast.Node)
	if !ok {
		return nil
	}
	if loop, ok := declaration.(*ast.RangeStmt); ok {
		return loop.Body
	}
	for node := declaration; node != nil; node = parents[node] {
		switch statement := node.(type) {
		case *ast.IfStmt:
			if statement.Init == object.Decl {
				return statement.Body
			}
		case *ast.ForStmt:
			if statement.Init == object.Decl {
				return statement.Body
			}
		case *ast.SwitchStmt:
			if statement.Init == object.Decl && len(statement.Body.List) > 0 {
				if clause, ok := statement.Body.List[0].(*ast.CaseClause); ok {
					return &ast.BlockStmt{Lbrace: clause.Case, List: clause.Body, Rbrace: clause.End()}
				}
			}
		case *ast.TypeSwitchStmt:
			if statement.Init == object.Decl && len(statement.Body.List) > 0 {
				if clause, ok := statement.Body.List[0].(*ast.CaseClause); ok {
					return &ast.BlockStmt{Lbrace: clause.Case, List: clause.Body, Rbrace: clause.End()}
				}
			}
		case *ast.BlockStmt:
			return statement
		}
	}
	return nil
}
