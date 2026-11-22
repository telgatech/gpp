package compiler

import (
	"bytes"
	"crypto/sha256"
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

func rewriteRegexpBuiltins(source, alias, sourceKey string) (string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "generated.go", "package generated\n"+source, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return "", fmt.Errorf("cannot lower regexp builtin: %w", err)
	}
	rewriter := regexpBuiltinRewriter{
		alias:        alias,
		sourceKey:    sourceKey,
		casePatterns: map[string]string{},
	}
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
	alias        string
	sourceKey    string
	next         int
	used         map[string]bool
	casePatterns map[string]string
	extraDecls   []ast.Decl
}

type regexpCaptureField struct {
	name  string
	index int
}

type regexpCaseCapture struct {
	temporary     string
	patternIndex  string
	patternCalls  []*ast.CallExpr
	patternFields [][]regexpCaptureField
	fields        []regexpCaptureField
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
		if literal, ok := node.(*ast.FuncLit); ok {
			statements, err := rewriter.rewriteStmtList(literal.Body.List)
			if err != nil {
				rewriteErr = err
				return false
			}
			literal.Body.List = statements
			return true
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
	if rewriteErr != nil {
		return rewriteErr
	}
	file.Decls = append(file.Decls, rewriter.extraDecls...)
	return nil
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
	captures := map[*ast.CaseClause]regexpCaseCapture{}
	for _, item := range statement.Body.List {
		clause, ok := item.(*ast.CaseClause)
		if !ok || clause.List == nil || !caseClauseUsesMatch(clause) {
			continue
		}
		patternCalls := make([]*ast.CallExpr, 0, len(clause.List))
		for _, expression := range clause.List {
			if call := regexpCaseCall(expression); call != nil {
				patternCalls = append(patternCalls, call)
				continue
			}
			return nil, fmt.Errorf("match captures require regexp(pattern) expressions only in the case")
		}
		if len(patternCalls) == 0 {
			return nil, fmt.Errorf("match is available only in a case with regexp(pattern) expressions")
		}
		capture := regexpCaseCapture{
			temporary:    rewriter.freshTemporary(),
			patternCalls: patternCalls,
		}
		fieldByName := map[string]regexpCaptureField{}
		allLiteralPatterns := true
		for _, call := range patternCalls {
			fields, literalPattern, err := regexpCaptureFields(call)
			if err != nil {
				return nil, err
			}
			if !literalPattern {
				allLiteralPatterns = false
			}
			capture.patternFields = append(capture.patternFields, fields)
			if len(capture.patternFields) == 1 {
				for _, field := range fields {
					fieldByName[field.name] = field
					capture.fields = append(capture.fields, field)
				}
				continue
			}
			currentNames := map[string]bool{}
			for _, field := range fields {
				currentNames[field.name] = true
			}
			filtered := capture.fields[:0]
			for _, field := range capture.fields {
				if currentNames[field.name] {
					filtered = append(filtered, field)
				}
			}
			capture.fields = filtered
			for name := range fieldByName {
				if !currentNames[name] {
					delete(fieldByName, name)
				}
			}
		}
		if len(capture.fields) > 0 {
			capture.patternIndex = rewriter.freshTemporary()
		}
		fieldReferences := caseMatchFieldReferences(clause)
		fieldNames := make([]string, 0, len(fieldReferences))
		for name := range fieldReferences {
			fieldNames = append(fieldNames, name)
		}
		sort.Strings(fieldNames)
		for _, name := range fieldNames {
			if name == "Items" {
				continue
			}
			if _, found := fieldByName[name]; !found {
				if !allLiteralPatterns {
					return nil, fmt.Errorf("named match fields require a literal capture shared by every regexp alternative; use match.Items for dynamic patterns")
				}
				return nil, fmt.Errorf("regexp alternatives do not all define named capture %q; use match.Items for positional captures", name)
			}
		}
		captures[clause] = capture
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
		if capture, ok := captures[clause]; ok {
			condition, err := rewriter.capturingCaseCondition(capture, temporary)
			if err != nil {
				return nil, err
			}
			clause.List = []ast.Expr{condition}
			clause.Body = append(regexpMatchBinding(capture), clause.Body...)
			continue
		}
		conditions := make([]ast.Expr, 0, len(clause.List))
		for _, expression := range clause.List {
			if originalCall := regexpCaseCall(expression); originalCall != nil {
				call, err := rewriter.caseRegexp(originalCall)
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
	if originalInit != nil || len(captures) > 0 {
		body := make([]ast.Stmt, 0, 1+len(captures)*2)
		if originalInit != nil {
			body = append(body, originalInit)
		}
		for _, item := range statement.Body.List {
			clause, ok := item.(*ast.CaseClause)
			if !ok {
				continue
			}
			capture, ok := captures[clause]
			if !ok {
				continue
			}
			body = append(body, &ast.DeclStmt{Decl: &ast.GenDecl{
				Tok: token.VAR,
				Specs: []ast.Spec{&ast.ValueSpec{
					Names: []*ast.Ident{ast.NewIdent(capture.temporary)},
					Type:  &ast.ArrayType{Elt: ast.NewIdent("string")},
				}},
			}})
			if capture.patternIndex != "" {
				body = append(body, &ast.DeclStmt{Decl: &ast.GenDecl{
					Tok: token.VAR,
					Specs: []ast.Spec{&ast.ValueSpec{
						Names: []*ast.Ident{ast.NewIdent(capture.patternIndex)},
						Type:  ast.NewIdent("int"),
					}},
				}})
			}
		}
		body = append(body, statement)
		return []ast.Stmt{&ast.BlockStmt{List: body}}, nil
	}
	return []ast.Stmt{statement}, nil
}

func (rewriter *regexpBuiltinRewriter) capturingCaseCondition(capture regexpCaseCapture, switchValue string) (ast.Expr, error) {
	conditions := make([]ast.Expr, 0, len(capture.patternCalls))
	for index, patternCall := range capture.patternCalls {
		compiled, err := rewriter.caseRegexp(patternCall)
		if err != nil {
			return nil, err
		}
		find := &ast.CallExpr{
			Fun:  &ast.SelectorExpr{X: compiled, Sel: ast.NewIdent("FindStringSubmatch")},
			Args: []ast.Expr{ast.NewIdent(switchValue)},
		}
		captures := ast.NewIdent(capture.temporary)
		innerBody := []ast.Stmt{&ast.AssignStmt{Lhs: []ast.Expr{captures}, Tok: token.ASSIGN, Rhs: []ast.Expr{find}}}
		if capture.patternIndex != "" {
			innerBody = append(innerBody, &ast.IfStmt{
				Cond: &ast.BinaryExpr{X: ast.NewIdent(capture.temporary), Op: token.NEQ, Y: ast.NewIdent("nil")},
				Body: &ast.BlockStmt{List: []ast.Stmt{
					&ast.AssignStmt{
						Lhs: []ast.Expr{ast.NewIdent(capture.patternIndex)}, Tok: token.ASSIGN,
						Rhs: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(index + 1)}},
					},
					&ast.ReturnStmt{Results: []ast.Expr{ast.NewIdent("true")}},
				}},
			})
			innerBody = append(innerBody, &ast.ReturnStmt{Results: []ast.Expr{ast.NewIdent("false")}})
		} else {
			innerBody = append(innerBody, &ast.ReturnStmt{Results: []ast.Expr{
				&ast.BinaryExpr{X: ast.NewIdent(capture.temporary), Op: token.NEQ, Y: ast.NewIdent("nil")},
			}})
		}
		inner := &ast.FuncLit{
			Type: &ast.FuncType{Params: &ast.FieldList{}, Results: &ast.FieldList{List: []*ast.Field{{Type: ast.NewIdent("bool")}}}},
			Body: &ast.BlockStmt{List: innerBody},
		}
		conditions = append(conditions, &ast.CallExpr{Fun: inner})
	}
	matched := conditions[0]
	for _, condition := range conditions[1:] {
		matched = &ast.BinaryExpr{X: matched, Op: token.LOR, Y: condition}
	}
	outerBody := []ast.Stmt{
		&ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(capture.temporary)}, Tok: token.ASSIGN, Rhs: []ast.Expr{ast.NewIdent("nil")}},
	}
	if capture.patternIndex != "" {
		outerBody = append(outerBody, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(capture.patternIndex)}, Tok: token.ASSIGN, Rhs: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "0"}}})
	}
	outerBody = append(outerBody, &ast.ReturnStmt{Results: []ast.Expr{matched}})
	outer := &ast.FuncLit{
		Type: &ast.FuncType{Params: &ast.FieldList{}, Results: &ast.FieldList{List: []*ast.Field{{Type: ast.NewIdent("bool")}}}},
		Body: &ast.BlockStmt{List: outerBody},
	}
	return &ast.CallExpr{Fun: outer}, nil
}

func caseClauseUsesMatch(clause *ast.CaseClause) bool {
	used := false
	for _, statement := range clause.Body {
		ast.Inspect(statement, func(node ast.Node) bool {
			if _, nestedSwitch := node.(*ast.SwitchStmt); nestedSwitch {
				return false
			}
			if _, nestedTypeSwitch := node.(*ast.TypeSwitchStmt); nestedTypeSwitch {
				return false
			}
			identifier, ok := node.(*ast.Ident)
			if ok && identifier.Name == "match" {
				used = true
				return false
			}
			return !used
		})
		if used {
			break
		}
	}
	return used
}

func caseMatchFieldReferences(clause *ast.CaseClause) map[string]bool {
	fields := map[string]bool{}
	for _, statement := range clause.Body {
		ast.Inspect(statement, func(node ast.Node) bool {
			if _, nestedSwitch := node.(*ast.SwitchStmt); nestedSwitch {
				return false
			}
			if _, nestedTypeSwitch := node.(*ast.TypeSwitchStmt); nestedTypeSwitch {
				return false
			}
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			base, ok := selector.X.(*ast.Ident)
			if ok && base.Name == "match" {
				fields[selector.Sel.Name] = true
			}
			return true
		})
	}
	return fields
}

func regexpCaptureFields(call *ast.CallExpr) ([]regexpCaptureField, bool, error) {
	if len(call.Args) != 1 {
		return nil, false, fmt.Errorf("regexp(pattern) expects exactly one argument")
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return nil, false, nil
	}
	pattern, err := strconv.Unquote(literal.Value)
	if err != nil {
		return nil, true, err
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, true, fmt.Errorf("invalid regexp pattern: %w", err)
	}
	seen := map[string]bool{}
	fields := []regexpCaptureField{}
	for index, name := range compiled.SubexpNames() {
		if index == 0 || name == "" || name == "Items" || seen[name] || !token.IsIdentifier(name) || IsKeyword(name) {
			continue
		}
		seen[name] = true
		fields = append(fields, regexpCaptureField{name: name, index: index})
	}
	return fields, true, nil
}

func regexpMatchBinding(capture regexpCaseCapture) []ast.Stmt {
	fields := []*ast.Field{{
		Names: []*ast.Ident{ast.NewIdent("Items")},
		Type:  &ast.ArrayType{Elt: ast.NewIdent("string")},
	}}
	for _, field := range capture.fields {
		fields = append(fields, &ast.Field{Names: []*ast.Ident{ast.NewIdent(field.name)}, Type: ast.NewIdent("string")})
	}
	matchType := &ast.StructType{Fields: &ast.FieldList{List: fields}}
	statements := []ast.Stmt{
		&ast.DeclStmt{Decl: &ast.GenDecl{
			Tok: token.VAR,
			Specs: []ast.Spec{&ast.ValueSpec{
				Names: []*ast.Ident{ast.NewIdent("match")},
				Type:  matchType,
			}},
		}},
		&ast.AssignStmt{
			Lhs: []ast.Expr{&ast.SelectorExpr{X: ast.NewIdent("match"), Sel: ast.NewIdent("Items")}},
			Tok: token.ASSIGN,
			Rhs: []ast.Expr{ast.NewIdent(capture.temporary)},
		},
	}
	for patternNumber, patternFields := range capture.patternFields {
		for _, field := range patternFields {
			common := false
			for _, commonField := range capture.fields {
				if commonField.name == field.name {
					common = true
					break
				}
			}
			if !common {
				continue
			}
			statements = append(statements, &ast.IfStmt{
				Cond: &ast.BinaryExpr{
					X: &ast.BinaryExpr{
						X: ast.NewIdent(capture.patternIndex), Op: token.EQL,
						Y: &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(patternNumber + 1)},
					},
					Op: token.LAND,
					Y: &ast.BinaryExpr{
						X:  &ast.CallExpr{Fun: ast.NewIdent("len"), Args: []ast.Expr{ast.NewIdent(capture.temporary)}},
						Op: token.GTR,
						Y:  &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(field.index)},
					},
				},
				Body: &ast.BlockStmt{List: []ast.Stmt{&ast.AssignStmt{
					Lhs: []ast.Expr{&ast.SelectorExpr{X: ast.NewIdent("match"), Sel: ast.NewIdent(field.name)}},
					Tok: token.ASSIGN,
					Rhs: []ast.Expr{&ast.IndexExpr{X: ast.NewIdent(capture.temporary), Index: &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(field.index)}}},
				}}},
			})
		}
	}
	return statements
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

func (rewriter *regexpBuiltinRewriter) caseRegexp(call *ast.CallExpr) (ast.Expr, error) {
	compiled, err := rewriter.compileCall(call)
	if err != nil {
		return nil, err
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return compiled, nil
	}
	pattern, err := strconv.Unquote(literal.Value)
	if err != nil {
		return nil, err
	}
	if existing := rewriter.casePatterns[pattern]; existing != "" {
		return ast.NewIdent(existing), nil
	}
	name := rewriter.freshRegexpName(pattern)
	rewriter.casePatterns[pattern] = name
	rewriter.extraDecls = append(rewriter.extraDecls, &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{&ast.ValueSpec{
			Names:  []*ast.Ident{ast.NewIdent(name)},
			Values: []ast.Expr{compiled},
		}},
	})
	return ast.NewIdent(name), nil
}

func (rewriter *regexpBuiltinRewriter) freshRegexpName(pattern string) string {
	hash := sha256.Sum256([]byte(rewriter.sourceKey + "\x00" + pattern))
	base := fmt.Sprintf("__gppRegexpPattern_%x", hash[:8])
	name := base
	for suffix := 2; rewriter.used[name]; suffix++ {
		name = fmt.Sprintf("%s_%d", base, suffix)
	}
	rewriter.used[name] = true
	return name
}
