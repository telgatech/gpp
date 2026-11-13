package compiler

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func fileUsesRegexpBuiltin(file *File) bool {
	if file == nil {
		return false
	}
	for index, token := range file.Tokens {
		if token.Kind != TokenIdentifier || token.Text != "regexp" {
			continue
		}
		previous := index - 1
		for previous >= 0 && (file.Tokens[previous].Kind == TokenComment || file.Tokens[previous].Kind == TokenNewline) {
			previous--
		}
		if previous >= 0 && file.Tokens[previous].Text == "." {
			continue
		}
		next := index + 1
		for next < len(file.Tokens) && (file.Tokens[next].Kind == TokenComment || file.Tokens[next].Kind == TokenNewline) {
			next++
		}
		if next < len(file.Tokens) && file.Tokens[next].Text == "(" {
			return true
		}
	}
	return false
}

func regexpBuiltinImportAlias(imports map[string]string) string {
	aliases := make([]string, 0, len(imports))
	for alias := range imports {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		if imports[alias] == "regexp" {
			return alias
		}
	}
	return generatedStaticImportAlias(imports, "gppRegexp")
}

func regexpBuiltinTypeName(context constructorContext) string {
	return "*" + regexpBuiltinImportAlias(context.AvailableImports) + ".Regexp"
}

func isRegexpBuiltinCallee(expression ExprNode) bool {
	name, ok := expression.(*NameExpr)
	return ok && name.Name == "regexp"
}

func rewriteRegexpBuiltins(source, alias string) (string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "generated.go", "package generated\n"+source, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return "", fmt.Errorf("cannot lower regexp builtin: %w", err)
	}
	rewriter := regexpBuiltinRewriter{alias: alias}
	if err := rewriter.rewriteFile(file); err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := format.Node(&output, fset, file); err != nil {
		return "", err
	}
	formatted := output.String()
	if newline := strings.IndexByte(formatted, '\n'); newline >= 0 {
		formatted = strings.TrimPrefix(formatted[newline+1:], "\n")
	}
	return formatted, nil
}

type regexpBuiltinRewriter struct {
	alias string
	next  int
	used  map[string]bool
}

func (rewriter *regexpBuiltinRewriter) rewriteFile(file *ast.File) error {
	rewriter.used = map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		if identifier, ok := node.(*ast.Ident); ok {
			rewriter.used[identifier.Name] = true
		}
		return true
	})
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		statements, err := rewriter.rewriteStmtList(function.Body.List)
		if err != nil {
			return err
		}
		function.Body.List = statements
	}
	var rewriteErr error
	ast.Inspect(file, func(node ast.Node) bool {
		if rewriteErr != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok || !isRegexpBuiltinCall(call) {
			return true
		}
		rewritten, err := rewriter.compileCall(call)
		if err != nil {
			rewriteErr = err
			return false
		}
		*call = *rewritten
		return true
	})
	return rewriteErr
}

func (rewriter *regexpBuiltinRewriter) rewriteStmtList(statements []ast.Stmt) ([]ast.Stmt, error) {
	var output []ast.Stmt
	for _, statement := range statements {
		switch value := statement.(type) {
		case *ast.SwitchStmt:
			for _, item := range value.Body.List {
				clause, ok := item.(*ast.CaseClause)
				if !ok {
					continue
				}
				body, err := rewriter.rewriteStmtList(clause.Body)
				if err != nil {
					return nil, err
				}
				clause.Body = body
			}
			transformed, err := rewriter.rewriteSwitch(value)
			if err != nil {
				return nil, err
			}
			output = append(output, transformed...)
		case *ast.TypeSwitchStmt:
			for _, item := range value.Body.List {
				if clause, ok := item.(*ast.CaseClause); ok {
					body, err := rewriter.rewriteStmtList(clause.Body)
					if err != nil {
						return nil, err
					}
					clause.Body = body
				}
			}
			output = append(output, value)
		case *ast.SelectStmt:
			for _, item := range value.Body.List {
				if clause, ok := item.(*ast.CommClause); ok {
					body, err := rewriter.rewriteStmtList(clause.Body)
					if err != nil {
						return nil, err
					}
					clause.Body = body
				}
			}
			output = append(output, value)
		case *ast.BlockStmt:
			if err := rewriter.rewriteBlock(value); err != nil {
				return nil, err
			}
			output = append(output, value)
		case *ast.IfStmt:
			if value.Body != nil {
				if err := rewriter.rewriteBlock(value.Body); err != nil {
					return nil, err
				}
			}
			if value.Else != nil {
				list, err := rewriter.rewriteStmtList([]ast.Stmt{value.Else})
				if err != nil {
					return nil, err
				}
				if len(list) == 1 {
					value.Else = list[0]
				} else {
					value.Else = &ast.BlockStmt{List: list}
				}
			}
			output = append(output, value)
		case *ast.ForStmt:
			if err := rewriter.rewriteBlock(value.Body); err != nil {
				return nil, err
			}
			output = append(output, value)
		case *ast.RangeStmt:
			if err := rewriter.rewriteBlock(value.Body); err != nil {
				return nil, err
			}
			output = append(output, value)
		case *ast.LabeledStmt:
			list, err := rewriter.rewriteStmtList([]ast.Stmt{value.Stmt})
			if err != nil {
				return nil, err
			}
			if len(list) == 1 {
				if _, wasSwitch := value.Stmt.(*ast.SwitchStmt); wasSwitch {
					if _, remainsSwitch := list[0].(*ast.SwitchStmt); !remainsSwitch {
						return nil, fmt.Errorf("regexp case matching is not supported on a labeled switch")
					}
				}
				value.Stmt = list[0]
				output = append(output, value)
			} else {
				return nil, fmt.Errorf("regexp case matching is not supported on a labeled switch")
			}
		default:
			output = append(output, statement)
		}
	}
	return output, nil
}

func (rewriter *regexpBuiltinRewriter) rewriteBlock(block *ast.BlockStmt) error {
	if block == nil {
		return nil
	}
	statements, err := rewriter.rewriteStmtList(block.List)
	if err != nil {
		return err
	}
	block.List = statements
	return nil
}

func (rewriter *regexpBuiltinRewriter) rewriteSwitch(statement *ast.SwitchStmt) ([]ast.Stmt, error) {
	usesRegex := false
	for _, item := range statement.Body.List {
		clause, ok := item.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expression := range clause.List {
			if regexpCaseCall(expression) != nil {
				usesRegex = true
			}
		}
	}
	if !usesRegex {
		return []ast.Stmt{statement}, nil
	}
	if statement.Tag == nil {
		return nil, fmt.Errorf("regexp(pattern) cases require a tagged switch")
	}
	originalInit := statement.Init
	tag := statement.Tag
	statement.Tag = nil
	temporary := rewriter.freshTemporary()
	statement.Init = &ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent(temporary)}, Tok: token.DEFINE,
		Rhs: []ast.Expr{tag},
	}
	for _, item := range statement.Body.List {
		clause, ok := item.(*ast.CaseClause)
		if !ok || clause.List == nil {
			continue
		}
		conditions := make([]ast.Expr, 0, len(clause.List))
		for _, expression := range clause.List {
			if originalCall := regexpCaseCall(expression); originalCall != nil {
				call, err := rewriter.compileCall(originalCall)
				if err != nil {
					return nil, err
				}
				conditions = append(conditions, &ast.CallExpr{
					Fun:  &ast.SelectorExpr{X: call, Sel: ast.NewIdent("MatchString")},
					Args: []ast.Expr{ast.NewIdent(temporary)},
				})
				continue
			}
			conditions = append(conditions, &ast.BinaryExpr{X: ast.NewIdent(temporary), Op: token.EQL, Y: expression})
		}
		if len(conditions) == 0 {
			continue
		}
		condition := conditions[0]
		for _, next := range conditions[1:] {
			condition = &ast.BinaryExpr{X: condition, Op: token.LOR, Y: next}
		}
		clause.List = []ast.Expr{condition}
	}
	if originalInit != nil {
		return []ast.Stmt{&ast.BlockStmt{List: []ast.Stmt{originalInit, statement}}}, nil
	}
	return []ast.Stmt{statement}, nil
}

func (rewriter *regexpBuiltinRewriter) freshTemporary() string {
	for {
		rewriter.next++
		name := fmt.Sprintf("__gppRegexpSwitch%d", rewriter.next)
		if !rewriter.used[name] {
			rewriter.used[name] = true
			return name
		}
	}
}

func isRegexpBuiltinCall(expression ast.Expr) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return false
	}
	identifier, ok := call.Fun.(*ast.Ident)
	return ok && identifier.Name == "regexp"
}

func regexpCaseCall(expression ast.Expr) *ast.CallExpr {
	for {
		parenthesized, ok := expression.(*ast.ParenExpr)
		if !ok {
			break
		}
		expression = parenthesized.X
	}
	call, _ := expression.(*ast.CallExpr)
	if call != nil && isRegexpBuiltinCall(call) {
		return call
	}
	return nil
}

func (rewriter *regexpBuiltinRewriter) compileCall(call *ast.CallExpr) (*ast.CallExpr, error) {
	if len(call.Args) != 1 {
		return nil, fmt.Errorf("regexp(pattern) expects exactly one argument")
	}
	if literal, ok := call.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
		pattern, err := strconv.Unquote(literal.Value)
		if err != nil {
			return nil, err
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("invalid regexp pattern: %w", err)
		}
	}
	return &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: ast.NewIdent(rewriter.alias), Sel: ast.NewIdent("MustCompile")},
		Args: call.Args,
	}, nil
}
