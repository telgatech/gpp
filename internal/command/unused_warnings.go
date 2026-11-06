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

	"github.com/telgatech/gpp/compiler"
)

type unusedLocalWarning struct {
	file   string
	line   int
	column int
	name   string
}

func (warning unusedLocalWarning) String() string {
	column := warning.column
	if column <= 0 {
		column = 1
	}
	diagnostic := compiler.NewDiagnosticError(
		warning.file,
		compiler.Span{Line: warning.line, Column: column},
		fmt.Errorf("warning: declared and not used: %s", warning.name),
	)
	return normalizeBackendLocation(diagnostic.Error(), "")
}

// suppressUnusedLocalErrors marks source-level local variables as used in the
// generated Go so Go's backend does not reject them. It returns source-mapped
// warnings for those variables; other backend diagnostics remain errors.
func suppressUnusedLocalErrors(root string) ([]unusedLocalWarning, error) {
	return suppressUnusedLocalErrorsWithRescan(root, false)
}

func suppressUnusedLocalErrorsWithRescan(root string, rescanSuppressed bool) ([]unusedLocalWarning, error) {
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
		fileWarnings, err := suppressUnusedLocalsInFile(path, rescanSuppressed)
		if err != nil {
			return nil, err
		}
		warnings = append(warnings, fileWarnings...)
	}
	return warnings, nil
}

func suppressUnusedLocalsInFile(path string, rescanSuppressed bool) ([]unusedLocalWarning, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	analysisSource := string(source)
	if rescanSuppressed {
		analysisSource = stripUnusedSuppressionLines(analysisSource)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, []byte(analysisSource), 0)
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

	byOffset := map[int][]*ast.Ident{}
	warnings := make([]unusedLocalWarning, 0)
	for object, ident := range definitions {
		if uses[object] != 0 {
			continue
		}
		position := fset.Position(ident.Pos())
		if position.Filename == "" || position.Line <= 0 {
			continue
		}
		warnings = append(warnings, unusedLocalWarning{file: position.Filename, line: position.Line, column: position.Column, name: ident.Name})
		offset, ok := unusedSuppressionOffset(object, parents, fset, len(analysisSource))
		if !ok {
			continue
		}
		byOffset[offset] = append(byOffset[offset], ident)
	}
	legacySuppression := strings.Contains(string(source), "// gpp:unused-suppression")
	if rescanSuppressed && !legacySuppression && sameUnusedNameCounts(suppressedUnusedNameCounts(string(source)), unusedNameCounts(warnings)) {
		sortUnusedWarnings(warnings)
		return warnings, nil
	}
	if len(byOffset) == 0 {
		if analysisSource != string(source) {
			if err := os.WriteFile(path, []byte(analysisSource), 0644); err != nil {
				return nil, err
			}
		}
		return warnings, nil
	}
	type edit struct {
		offset int
		text   string
	}
	edits := make([]edit, 0, len(byOffset))
	for offset, names := range byOffset {
		sort.Slice(names, func(i, j int) bool { return names[i].Pos() < names[j].Pos() })
		var addition strings.Builder
		for _, name := range names {
			fmt.Fprintf(&addition, "; _ = %s /*gpp:unused-suppression*/", name.Name)
		}
		edits = append(edits, edit{offset: offset, text: addition.String()})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].offset > edits[j].offset })
	updated := analysisSource
	for _, item := range edits {
		updated = updated[:item.offset] + item.text + updated[item.offset:]
	}
	if updated == string(source) {
		return warnings, nil
	}
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return nil, err
	}
	sortUnusedWarnings(warnings)
	return warnings, nil
}

func sortUnusedWarnings(warnings []unusedLocalWarning) {
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].file == warnings[j].file {
			if warnings[i].line == warnings[j].line {
				return warnings[i].name < warnings[j].name
			}
			return warnings[i].line < warnings[j].line
		}
		return warnings[i].file < warnings[j].file
	})
}

func suppressedUnusedNameCounts(source string) map[string]int {
	counts := map[string]int{}
	for _, line := range strings.Split(source, "\n") {
		if strings.Contains(line, "// gpp:unused-suppression") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) >= 5 && fields[0] == "_" && fields[1] == "=" && fields[4] == "gpp:unused-suppression" {
				counts[fields[2]]++
			}
		}
	}
	for remaining := source; ; {
		marker := strings.Index(remaining, "/*gpp:unused-suppression*/")
		if marker < 0 {
			break
		}
		prefix := strings.TrimSpace(remaining[:marker])
		assignment := strings.LastIndex(prefix, "_ = ")
		if assignment >= 0 {
			name := strings.TrimSpace(prefix[assignment+4:])
			if name != "" && !strings.ContainsAny(name, " \t\r\n;") {
				counts[name]++
			}
		}
		remaining = remaining[marker+len("/*gpp:unused-suppression*/"):]
	}
	return counts
}

func unusedNameCounts(warnings []unusedLocalWarning) map[string]int {
	counts := map[string]int{}
	for _, warning := range warnings {
		counts[warning.name]++
	}
	return counts
}

func sameUnusedNameCounts(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for name, count := range left {
		if right[name] != count {
			return false
		}
	}
	return true
}

func stripUnusedSuppressionLines(source string) string {
	lines := strings.SplitAfter(source, "\n")
	for index, line := range lines {
		if !strings.Contains(line, "// gpp:unused-suppression") {
			continue
		}
		lines[index] = strings.Map(func(character rune) rune {
			if character == '\n' || character == '\r' {
				return character
			}
			return ' '
		}, line)
	}
	source = strings.Join(lines, "")
	const marker = "/*gpp:unused-suppression*/"
	for searchFrom := 0; ; {
		markerAt := strings.Index(source[searchFrom:], marker)
		if markerAt < 0 {
			break
		}
		markerAt += searchFrom
		assignmentAt := strings.LastIndex(source[:markerAt], "_ = ")
		if assignmentAt < 0 {
			searchFrom = markerAt + len(marker)
			continue
		}
		start := assignmentAt
		if start > 0 && source[start-1] == ';' {
			start--
		}
		end := markerAt + len(marker)
		for index := start; index < end; index++ {
			if source[index] != '\n' && source[index] != '\r' {
				source = source[:index] + " " + source[index+1:]
			}
		}
		searchFrom = end
	}
	return source
}

func unusedSuppressionOffset(object *ast.Object, parents map[ast.Node]ast.Node, fset *token.FileSet, sourceLength int) (int, bool) {
	declaration, ok := object.Decl.(ast.Node)
	if !ok {
		return 0, false
	}
	if loop, ok := declaration.(*ast.RangeStmt); ok {
		return validInsertionOffset(fset, loop.Body.Lbrace+1, sourceLength)
	}
	for node := declaration; node != nil; node = parents[node] {
		switch statement := node.(type) {
		case *ast.IfStmt:
			if statement.Init == declaration {
				return validInsertionOffset(fset, statement.Body.Lbrace+1, sourceLength)
			}
		case *ast.ForStmt:
			if statement.Init == declaration {
				return validInsertionOffset(fset, statement.Body.Lbrace+1, sourceLength)
			}
		case *ast.SwitchStmt:
			if statement.Init == declaration && len(statement.Body.List) > 0 {
				if clause, ok := statement.Body.List[0].(*ast.CaseClause); ok {
					return validInsertionOffset(fset, clause.Colon+1, sourceLength)
				}
			}
		case *ast.TypeSwitchStmt:
			if statement.Init == declaration && len(statement.Body.List) > 0 {
				if clause, ok := statement.Body.List[0].(*ast.CaseClause); ok {
					return validInsertionOffset(fset, clause.Colon+1, sourceLength)
				}
			}
		case *ast.BlockStmt:
			switch declared := declaration.(type) {
			case *ast.AssignStmt:
				return validInsertionOffset(fset, declared.End(), sourceLength)
			case *ast.ValueSpec:
				if parent, ok := parents[declared].(*ast.GenDecl); ok {
					return validInsertionOffset(fset, parent.End(), sourceLength)
				}
			}
		}
	}
	return 0, false
}

func validInsertionOffset(fset *token.FileSet, position token.Pos, sourceLength int) (int, bool) {
	if !position.IsValid() {
		return 0, false
	}
	offset := fset.Position(position).Offset
	return offset, offset >= 0 && offset <= sourceLength
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
