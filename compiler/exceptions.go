package compiler

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

type exceptionContext struct {
	RuntimeEmitted bool
}

func exceptionRuntimeDefinitions() string {
	return `type __gppThrownError struct {
	err error
}

func (thrown __gppThrownError) GppThrownError() error {
	return thrown.err
}

type __gppExceptionReturn struct {
	values []any
}

func __gppThrow(err error) {
	if err != nil {
		panic(__gppThrownError{err: err})
	}
}

func __gppRun(block func()) {
	block()
}

func __gppCoalesce[T any](left func() T, fallback func() T) (result T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if _, ok := recovered.(__gppThrownError); ok {
				result = fallback()
				return
			}
			panic(recovered)
		}
	}()
	return left()
}

func __gppUnwrap[T any](value T, err error) T {
	__gppThrow(err)
	return value
}

func __gppUnwrap2[A any, B any](first A, second B, err error) (A, B) {
	__gppThrow(err)
	return first, second
}

func __gppUnwrap3[A any, B any, C any](first A, second B, third C, err error) (A, B, C) {
	__gppThrow(err)
	return first, second, third
}

func __gppDiscard[T any](value T, err error) {
	__gppThrow(err)
}

func __gppDiscard2[A any, B any](first A, second B, err error) {
	__gppThrow(err)
}

func __gppDiscard3[A any, B any, C any](first A, second B, third C, err error) {
	__gppThrow(err)
}

`
}

type catchClause struct {
	typeNodes []TypeNode
	variable  string
	body      *ast.BlockStmt
}

func transformExceptions(src string, context constructorContext) (string, error) {
	return transformExceptionRegion(src, context, "")
}

func transformExceptionRegion(src string, context constructorContext, rethrowName string) (string, error) {
	if transformed, handled, err := transformExceptionRegionAST(src, context, rethrowName); handled {
		return transformed, err
	}
	if hasExceptionSyntaxTokens(src) {
		return "", fmt.Errorf("exception syntax could not be represented by the body AST")
	}
	return src, nil
}

func hasExceptionSyntaxTokens(src string) bool {
	tokens, err := LexSource("exception syntax", src)
	if err != nil {
		return false
	}
	for _, token := range tokens {
		if token.Kind == TokenKeyword && (token.Text == "try" || token.Text == "throw") {
			return true
		}
	}
	return false
}

// transformExceptionRegionAST lowers try/catch/finally and throw statements
// discovered from the structured body AST. It deliberately delegates only
// the existing type validation and runtime wrapper construction to the
// compatibility helpers below; statement boundaries and exception clauses are
// sourced from typed nodes and spans.
func transformExceptionRegionAST(src string, context constructorContext, rethrowName string) (string, bool, error) {
	tokens, err := LexSource("exceptions", src)
	if err != nil {
		return src, false, nil
	}
	blocks := []*BlockStmt{}
	// A complete function declaration is accepted by the body parser as a
	// token fallback. Prefer its structured function body so exception syntax
	// cannot disappear into that fallback.
	functions := parseTopLevelFunctions("exceptions", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function != nil && function.Method.BodyAST != nil {
				blocks = append(blocks, function.Method.BodyAST)
			}
		}
	} else if block, parseErr := ParseBodyAST(tokens); parseErr == nil {
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return src, false, nil
	}
	statements := []Stmt{}
	for _, block := range blocks {
		collectExceptionSyntaxStatements(block, &statements)
	}
	if len(statements) == 0 {
		return src, false, nil
	}

	type edit struct {
		start int
		end   int
		text  string
	}
	edits := make([]edit, 0, len(statements))
	for _, statement := range statements {
		if statement == nil {
			continue
		}
		start := statement.Span().Start
		end := statement.Span().End
		if start < 0 || end > len(src) || start >= end {
			return src, false, nil
		}
		switch statement := statement.(type) {
		case *ThrowStmt:
			text, err := lowerASTThrow(statement, context, rethrowName)
			if err != nil {
				return "", true, err
			}
			edits = append(edits, edit{start: start, end: end, text: text})
		case *TryStmt:
			text, err := lowerASTTry(statement, src, context, rethrowName)
			if err != nil {
				return "", true, err
			}
			edits = append(edits, edit{start: start, end: end, text: text})
		}
	}
	if len(edits) == 0 {
		return src, false, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

func collectExceptionSyntaxStatements(block *BlockStmt, result *[]Stmt) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectExceptionStatement(statement, result)
	}
}

func collectExceptionStatement(statement Stmt, result *[]Stmt) {
	if statement == nil {
		return
	}
	switch value := statement.(type) {
	case *TryStmt, *ThrowStmt:
		// A try statement owns its nested body. lowerASTTry recursively lowers
		// that body, so nested statements must not overlap this edit.
		*result = append(*result, statement)
	case *TokenStmt:
		collectExceptionSyntaxStatements(value.Body, result)
		for _, child := range value.Children {
			collectExceptionStatement(child, result)
		}
	case *IfStmt:
		collectExceptionSyntaxStatements(value.Body, result)
		collectExceptionSyntaxStatements(value.Else, result)
		if value.ElseIf != nil {
			collectExceptionStatement(value.ElseIf, result)
		}
	case *ForStmt:
		collectExceptionSyntaxStatements(value.Body, result)
	case *SwitchStmt:
		collectExceptionSyntaxStatements(value.Body, result)
	case *CaseStmt:
		collectExceptionSyntaxStatements(value.Clause.Body, result)
	case *BlockStmt:
		collectExceptionSyntaxStatements(value, result)
	}
}

func blockBodySource(block *BlockStmt, src string) (string, bool) {
	if block == nil || block.Open.End < 0 || block.Close.Start < block.Open.End || block.Close.Start > len(src) {
		return "", false
	}
	return src[block.Open.End:block.Close.Start], true
}

func lowerASTThrow(statement *ThrowStmt, context constructorContext, rethrowName string) (string, error) {
	if statement == nil || statement.Value == nil {
		if rethrowName == "" {
			return "", fmt.Errorf("bare throw is only valid inside catch")
		}
		return "panic(" + rethrowName + ")", nil
	}
	expressionText, err := expressionNodeSource(statement.Value)
	if err != nil {
		return "", err
	}
	expression := strings.TrimSpace(expressionText)
	if expression == "" {
		return "", fmt.Errorf("throw requires an expression")
	}
	if err := validateThrowExpression(expression, context); err != nil {
		return "", err
	}
	return "__gppThrow(" + expression + ")", nil
}

func lowerASTTry(statement *TryStmt, src string, context constructorContext, rethrowName string) (string, error) {
	if statement == nil {
		return "", fmt.Errorf("try statement is nil")
	}
	tryBody, ok := blockBodySource(statement.Body, src)
	if !ok {
		return "", fmt.Errorf("try body has invalid source span")
	}
	tryBody, err := transformExceptionRegion(tryBody, context, rethrowName)
	if err != nil {
		return "", err
	}
	clauses := []catchClause{}
	clauseSources := []string{}
	finallyBody := ""
	for _, parsed := range statement.Catches {
		body, bodyOK := blockBodySource(parsed.Body, src)
		if !bodyOK {
			return "", fmt.Errorf("exception clause has invalid source span")
		}
		catchTypes := append([]TypeNode(nil), parsed.Types...)
		catchVariable := parsed.Binding
		// The body parser cannot resolve the one-token ambiguity between
		// `catch Problem` (a type) and `catch problem` (a binding). Resolve it
		// against semantic context here instead of encoding source heuristics
		// in the parser.
		if len(catchTypes) == 0 && catchVariable != "" && isCatchTypeName(catchVariable, context) {
			catchTypes = append(catchTypes, parseTypeText(catchVariable))
			catchVariable = ""
		}
		clause := catchClause{variable: catchVariable}
		for _, typeNode := range catchTypes {
			nameText, typeErr := typeNodeSource(typeNode)
			if typeErr != nil {
				return "", typeErr
			}
			name := strings.TrimSpace(nameText)
			if name == "" {
				return "", fmt.Errorf("catch type is empty")
			}
			clause.typeNodes = append(clause.typeNodes, typeNode)
		}
		if len(clause.typeNodes) == 0 {
			clause.typeNodes = []TypeNode{parseTypeText("error")}
		}
		body, err = transformExceptionRegion(body, context, "__gppRecovered")
		if err != nil {
			return "", err
		}
		for _, typeNode := range clause.typeNodes {
			nameText, typeErr := typeNodeSource(typeNode)
			if typeErr != nil {
				return "", typeErr
			}
			typeName := strings.TrimSpace(nameText)
			if !isCatchTypeName(typeName, context) {
				return "", fmt.Errorf("invalid catch type %s", typeName)
			}
		}
		if clause.variable != "" && (!isIdentifier(clause.variable) || isGoKeyword(clause.variable)) {
			return "", fmt.Errorf("invalid catch variable %s", clause.variable)
		}
		clauses = append(clauses, clause)
		clauseSources = append(clauseSources, body)
	}
	if statement.Finally != nil {
		body, bodyOK := blockBodySource(statement.Finally, src)
		if !bodyOK {
			return "", fmt.Errorf("exception clause has invalid source span")
		}
		finallyBody, err = transformExceptionRegion(body, context, rethrowName)
		if err != nil {
			return "", err
		}
		if err := validateFinallyControlTransfers(finallyBody, context); err != nil {
			return "", err
		}
	}
	for index := range clauses {
		body := clauseSources[index]
		body, err = transformImplicitErrorPromotion(body, context)
		if err != nil {
			return "", err
		}
		body, err = rewriteExceptionReturns(body, context)
		if err != nil {
			return "", err
		}
		clauses[index].body, err = parseLoweredBlock(body)
		if err != nil {
			return "", fmt.Errorf("catch body is not valid Go after AST lowering: %w", err)
		}
	}
	if err := validateCatchOrdering(clauses, context); err != nil {
		return "", err
	}
	if len(clauses) == 0 && finallyBody == "" {
		return "{\n" + tryBody + "\n}", nil
	}
	tryBody, err = transformImplicitErrorPromotion(tryBody, context)
	if err != nil {
		return "", err
	}
	tryBody, err = rewriteExceptionReturns(tryBody, context)
	if err != nil {
		return "", err
	}
	return lowerTryAST(tryBody, clauses, finallyBody, context)
}

func isCatchTypeName(name string, context constructorContext) bool {
	name = strings.TrimSpace(name)
	if name == "error" || name == "any" {
		return name == "error"
	}
	base := strings.TrimPrefix(name, "*")
	if target, ok := context.Targets[base]; ok {
		return classHasErrorMethod(target.Class, context, map[string]bool{})
	}
	parts := strings.Split(base, ".")
	if len(parts) != 2 || context.AvailableImports == nil {
		return false
	}
	importPath := context.AvailableImports[parts[0]]
	if importPath == "" {
		return false
	}
	pkg, err := importNativePackage(importPath)
	if err != nil {
		return false
	}
	typeName, ok := pkg.Scope().Lookup(parts[1]).(*types.TypeName)
	if !ok {
		return false
	}
	errorType := types.Universe.Lookup("error").Type()
	catchType := types.Unalias(typeName.Type())
	if strings.HasPrefix(name, "*") {
		if named, namedOK := catchType.(*types.Named); namedOK {
			catchType = types.NewPointer(named)
		}
	}
	return types.AssignableTo(catchType, errorType)
}

func validateCatchOrdering(clauses []catchClause, context constructorContext) error {
	for index := 0; index < len(clauses); index++ {
		seenTypes := map[string]bool{}
		typeNames := catchClauseTypeNames(clauses[index])
		for _, typeName := range typeNames {
			if seenTypes[typeName] {
				return fmt.Errorf("duplicate catch type %s", typeName)
			}
			seenTypes[typeName] = true
		}
		for laterIndex, laterType := range typeNames {
			for earlierIndex := 0; earlierIndex < laterIndex; earlierIndex++ {
				earlierType := typeNames[earlierIndex]
				if catchTypeCovers(earlierType, laterType, context) {
					return fmt.Errorf("unreachable catch: %s is already matched by %s", laterType, earlierType)
				}
			}
		}
		for previous := 0; previous < index; previous++ {
			for _, earlierType := range catchClauseTypeNames(clauses[previous]) {
				for _, laterType := range typeNames {
					if catchTypeCovers(earlierType, laterType, context) {
						return fmt.Errorf("unreachable catch: %s is already matched by %s", laterType, earlierType)
					}
				}
			}
		}
	}
	return nil
}

func catchClauseTypeNames(clause catchClause) []string {
	names := make([]string, 0, len(clause.typeNodes))
	for _, typeNode := range clause.typeNodes {
		if name, err := typeNodeSource(typeNode); err == nil {
			names = append(names, strings.TrimSpace(name))
		}
	}
	return names
}

func catchTypeCovers(earlier, later string, context constructorContext) bool {
	earlier = strings.TrimSpace(earlier)
	later = strings.TrimSpace(later)
	if earlier == later || earlier == "error" {
		return true
	}
	// Local class inheritance is represented in the semantic model. A later
	// derived class is unreachable after an earlier parent catch.
	earlierBase := strings.TrimPrefix(earlier, "*")
	laterBase := strings.TrimPrefix(later, "*")
	earlierTarget, earlierOK := context.Targets[earlierBase]
	laterTarget, laterOK := context.Targets[laterBase]
	if !earlierOK || !laterOK {
		return false
	}
	return classInheritsTarget(laterTarget, earlierTarget, context)
}

func validateFinallyControlTransfers(body string, context constructorContext) error {
	if err, handled := validateFinallyControlTransfersAST(body); handled {
		return err
	}
	return nil
}

func validateFinallyControlTransfersAST(body string) (error, bool) {
	tokens, err := LexSource("finally", body)
	if err != nil {
		return nil, false
	}
	block, err := ParseBodyAST(tokens)
	if err != nil {
		return nil, false
	}
	var transferErr error
	var visitBlock func(*BlockStmt)
	var visitStmt func(Stmt)
	visitBlock = func(current *BlockStmt) {
		if current == nil || transferErr != nil {
			return
		}
		for _, statement := range current.Statements {
			visitStmt(statement)
			if transferErr != nil {
				return
			}
		}
	}
	visitStmt = func(statement Stmt) {
		if statement == nil || transferErr != nil {
			return
		}
		switch value := statement.(type) {
		case *TokenStmt:
			switch value.Kind {
			case BodyStmtReturn:
				transferErr = fmt.Errorf("control transfer from finally is not allowed: return")
			case BodyStmtBranch:
				keyword := firstSyntaxText(value.Tokens)
				if keyword == "break" || keyword == "continue" || keyword == "goto" {
					transferErr = fmt.Errorf("control transfer from finally is not allowed: %s", keyword)
				}
			}
			visitBlock(value.Body)
			for _, child := range value.Children {
				visitStmt(child)
			}
		case *BlockStmt:
			visitBlock(value)
		case *ReturnStmt:
			transferErr = fmt.Errorf("control transfer from finally is not allowed: return")
		case *BranchStmt:
			if value.Keyword == "break" || value.Keyword == "continue" || value.Keyword == "goto" {
				transferErr = fmt.Errorf("control transfer from finally is not allowed: %s", value.Keyword)
			}
		case *IfStmt:
			visitBlock(value.Body)
			visitBlock(value.Else)
			if value.ElseIf != nil {
				visitStmt(value.ElseIf)
			}
		case *ForStmt:
			visitBlock(value.Body)
		case *SwitchStmt:
			visitBlock(value.Body)
		case *CaseStmt:
			visitBlock(value.Clause.Body)
		case *TryStmt:
			visitBlock(value.Body)
			for _, clause := range value.Catches {
				visitBlock(clause.Body)
			}
			visitBlock(value.Finally)
		}
	}
	visitBlock(block)
	return transferErr, true
}

func lowerTryAST(tryBody string, clauses []catchClause, finallyBody string, context constructorContext) (string, error) {
	var err error
	tryBody, err = transformConstructors(tryBody, context)
	if err != nil {
		return "", err
	}
	tryBlock, err := parseLoweredBlock(tryBody)
	if err != nil {
		return "", fmt.Errorf("try body is not valid Go after AST lowering: %w", err)
	}
	finallyBlock := &ast.BlockStmt{}
	if finallyBody != "" {
		finallyBody, err = transformConstructors(finallyBody, context)
		if err != nil {
			return "", err
		}
		finallyBlock, err = parseLoweredBlock(finallyBody)
		if err != nil {
			return "", fmt.Errorf("finally body is not valid Go after AST lowering: %w", err)
		}
	}
	deferBody := []ast.Stmt{}
	if finallyBody != "" {
		deferBody = append(deferBody, &ast.DeferStmt{Call: &ast.CallExpr{
			Fun: &ast.FuncLit{
				Type: &ast.FuncType{Params: &ast.FieldList{}},
				Body: finallyBlock,
			},
		}})
	}

	recovered := identifier("__gppRecovered")
	thrown := identifier("__gppThrown")
	isThrown := identifier("__gppIsThrown")
	handled := identifier("__gppHandled")
	thrownError := selector(thrown, "err")

	catchIf := &ast.IfStmt{
		Cond: &ast.BinaryExpr{X: thrownError, Op: token.NEQ, Y: identifier("nil")},
		Body: &ast.BlockStmt{},
	}
	recoverBody := []ast.Stmt{
		&ast.AssignStmt{
			Lhs: []ast.Expr{recovered},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{call(identifier("recover"))},
		},
		&ast.IfStmt{
			Cond: &ast.BinaryExpr{X: recovered, Op: token.NEQ, Y: identifier("nil")},
			Body: &ast.BlockStmt{List: []ast.Stmt{
				&ast.AssignStmt{
					Lhs: []ast.Expr{thrown, isThrown},
					Tok: token.DEFINE,
					Rhs: []ast.Expr{&ast.TypeAssertExpr{
						X:    recovered,
						Type: identifier("__gppThrownError"),
					}},
				},
				&ast.IfStmt{
					Cond: &ast.UnaryExpr{Op: token.NOT, X: isThrown},
					Body: &ast.BlockStmt{List: []ast.Stmt{
						&ast.ExprStmt{X: call(identifier("panic"), recovered)},
					}},
				},
				&ast.AssignStmt{
					Lhs: []ast.Expr{handled},
					Tok: token.DEFINE,
					Rhs: []ast.Expr{identifier("false")},
				},
				catchIf,
				&ast.IfStmt{
					Cond: &ast.UnaryExpr{Op: token.NOT, X: handled},
					Body: &ast.BlockStmt{List: []ast.Stmt{
						&ast.ExprStmt{X: call(identifier("panic"), recovered)},
					}},
				},
			}},
		},
	}
	catchSwitch, err := lowerCatchSwitch(thrownError, handled, clauses)
	if err != nil {
		return "", fmt.Errorf("catch body is not valid Go after AST lowering: %w", err)
	}
	catchIf.Body.List = []ast.Stmt{catchSwitch}
	deferBody = append(deferBody, recoverBody...)

	runBody := []ast.Stmt{
		&ast.DeferStmt{Call: &ast.CallExpr{
			Fun: &ast.FuncLit{
				Type: &ast.FuncType{Params: &ast.FieldList{}},
				Body: &ast.BlockStmt{List: deferBody},
			},
		}},
	}
	runBody = append(runBody, tryBlock.List...)
	run := &ast.ExprStmt{X: call(identifier("__gppRun"), &ast.FuncLit{
		Type: &ast.FuncType{Params: &ast.FieldList{}},
		Body: &ast.BlockStmt{List: runBody},
	})}

	var output bytes.Buffer
	if err := format.Node(&output, token.NewFileSet(), run); err != nil {
		return "", err
	}
	return output.String(), nil
}

func lowerCatchSwitch(thrownError, handled ast.Expr, clauses []catchClause) (ast.Stmt, error) {
	hasCatchVariable := false
	for _, clause := range clauses {
		if clause.variable != "" {
			hasCatchVariable = true
			break
		}
	}

	var assign ast.Stmt
	if hasCatchVariable {
		assign = &ast.AssignStmt{
			Lhs: []ast.Expr{identifier("__gppCaught")},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{&ast.TypeAssertExpr{X: thrownError, Type: nil}},
		}
	} else {
		assign = &ast.ExprStmt{X: &ast.TypeAssertExpr{X: thrownError, Type: nil}}
	}

	body := make([]ast.Stmt, 0, len(clauses))
	for _, clause := range clauses {
		caseBody := []ast.Stmt{
			&ast.AssignStmt{
				Lhs: []ast.Expr{handled},
				Tok: token.ASSIGN,
				Rhs: []ast.Expr{identifier("true")},
			},
		}
		if clause.variable != "" {
			value := ast.Expr(identifier("__gppCaught"))
			if len(clause.typeNodes) > 1 {
				value = call(identifier("error"), value)
			}
			caseBody = append(caseBody, &ast.AssignStmt{
				Lhs: []ast.Expr{identifier(clause.variable)},
				Tok: token.DEFINE,
				Rhs: []ast.Expr{value},
			})
		} else if hasCatchVariable {
			caseBody = append(caseBody, &ast.AssignStmt{
				Lhs: []ast.Expr{identifier("_")},
				Tok: token.ASSIGN,
				Rhs: []ast.Expr{identifier("__gppCaught")},
			})
		}
		if clause.body != nil {
			caseBody = append(caseBody, clause.body.List...)
		}
		caseExprs := make([]ast.Expr, 0, len(clause.typeNodes))
		for _, typeNode := range clause.typeNodes {
			typeName, typeErr := typeNodeSource(typeNode)
			if typeErr != nil {
				return nil, typeErr
			}
			expression, parseErr := parser.ParseExpr(strings.TrimSpace(typeName))
			if parseErr != nil {
				return nil, parseErr
			}
			caseExprs = append(caseExprs, expression)
		}
		body = append(body, &ast.CaseClause{List: caseExprs, Body: caseBody})
	}

	return &ast.TypeSwitchStmt{
		Assign: assign,
		Body:   &ast.BlockStmt{List: body},
	}, nil
}

func parseLoweredBlock(source string) (*ast.BlockStmt, error) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "generated.go", "package main\nfunc __gppGenerated() {\n"+source+"\n}\n", 0)
	if err != nil {
		return nil, err
	}
	if len(parsed.Decls) != 1 {
		return nil, fmt.Errorf("generated exception body did not parse as one function")
	}
	function, ok := parsed.Decls[0].(*ast.FuncDecl)
	if !ok || function.Body == nil {
		return nil, fmt.Errorf("generated exception body did not produce a function body")
	}
	return function.Body, nil
}

func identifier(name string) *ast.Ident {
	return ast.NewIdent(name)
}

func selector(expression ast.Expr, name string) *ast.SelectorExpr {
	return &ast.SelectorExpr{X: expression, Sel: identifier(name)}
}

func call(function ast.Expr, arguments ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Fun: function, Args: arguments}
}

func rewriteExceptionReturns(body string, context constructorContext) (string, error) {
	if transformed, handled, err := rewriteExceptionReturnsAST(body); handled {
		return transformed, err
	}
	return body, nil
}

func rewriteExceptionReturnsAST(body string) (string, bool, error) {
	tokens, err := LexSource("exception returns", body)
	if err != nil {
		return body, false, nil
	}
	block, err := ParseBodyAST(tokens)
	if err != nil {
		return body, false, nil
	}
	returns := []*ReturnStmt{}
	unsupported := false
	var visitBlock func(*BlockStmt)
	var visitStmt func(Stmt)
	visitBlock = func(current *BlockStmt) {
		if current == nil || unsupported {
			return
		}
		for _, statement := range current.Statements {
			visitStmt(statement)
		}
	}
	visitStmt = func(statement Stmt) {
		if statement == nil || unsupported {
			return
		}
		switch value := statement.(type) {
		case *TokenStmt:
			if value.Kind == BodyStmtReturn {
				unsupported = true
				return
			}
			visitBlock(value.Body)
			for _, child := range value.Children {
				visitStmt(child)
			}
		case *BlockStmt:
			visitBlock(value)
		case *ReturnStmt:
			returns = append(returns, value)
		case *IfStmt:
			visitBlock(value.Body)
			visitBlock(value.Else)
			if value.ElseIf != nil {
				visitStmt(value.ElseIf)
			}
		case *ForStmt:
			visitBlock(value.Body)
		case *SwitchStmt:
			visitBlock(value.Body)
		case *CaseStmt:
			visitBlock(value.Clause.Body)
		case *TryStmt:
			visitBlock(value.Body)
			for _, clause := range value.Catches {
				visitBlock(clause.Body)
			}
			visitBlock(value.Finally)
		}
	}
	visitBlock(block)
	if unsupported {
		return body, false, nil
	}
	type edit struct {
		start int
		end   int
		text  string
	}
	edits := make([]edit, 0, len(returns))
	for _, statement := range returns {
		values := make([]string, 0, len(statement.Values))
		for _, value := range statement.Values {
			text, sourceErr := expressionNodeSource(value)
			if sourceErr != nil {
				return body, false, nil
			}
			values = append(values, text)
		}
		replacement := "panic(__gppExceptionReturn{})"
		if len(values) > 0 {
			replacement = "panic(__gppExceptionReturn{values: []any{" + strings.Join(values, ", ") + "}})"
		}
		span := statement.Span()
		if span.Start < 0 || span.End > len(body) || span.Start >= span.End {
			return body, false, nil
		}
		edits = append(edits, edit{start: span.Start, end: span.End, text: replacement})
	}
	if len(edits) == 0 {
		return body, true, nil
	}
	sort.Slice(edits, func(left, right int) bool { return edits[left].start > edits[right].start })
	for _, edit := range edits {
		body = body[:edit.start] + edit.text + body[edit.end:]
	}
	return body, true, nil
}

func readThrowExpression(src string, start int) (int, string, error) {
	index := start
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for index < len(src) {
		switch src[index] {
		case '"', '\'':
			end, err := skipQuoted(src, index, src[index])
			if err != nil {
				return 0, "", err
			}
			index = end + 1
			continue
		case '`':
			end := strings.IndexByte(src[index+1:], '`')
			if end < 0 {
				return 0, "", fmt.Errorf("unterminated raw string")
			}
			index += end + 2
			continue
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			} else if parenDepth == 0 && bracketDepth == 0 {
				return index, strings.TrimSpace(src[start:index]), nil
			}
		case ';', '\n':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return index, strings.TrimSpace(src[start:index]), nil
			}
		}
		index++
	}
	return index, strings.TrimSpace(src[start:index]), nil
}

func validateThrowExpression(expression string, context constructorContext) error {
	expression = strings.TrimSpace(expression)
	if expression == "nil" {
		return nil
	}
	if len(expression) > 0 && (expression[0] == '"' || expression[0] == '\'') {
		return fmt.Errorf("cannot throw string; thrown value must implement error")
	}
	name, length := readIdent(expression)
	if length > 0 {
		if length == len(expression) && (name == "true" || name == "false") {
			return fmt.Errorf("cannot throw bool; thrown value must implement error")
		}
		if open := skipSpace(expression, length); open < len(expression) && expression[open] == '(' {
			if target, ok := context.Targets[name]; ok && !classHasErrorMethod(target.Class, context, map[string]bool{}) {
				return fmt.Errorf("cannot throw %s; thrown value must implement error", name)
			}
		}
	}
	return nil
}

func classHasErrorMethod(class *ClassDecl, context constructorContext, visiting map[string]bool) bool {
	if class == nil || visiting[class.Name] {
		return false
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)
	for _, method := range class.Methods {
		if !method.IsStatic && method.Name == "Error" && methodParametersSource(method) == "" && strings.TrimSpace(methodResultSource(method)) == "string" {
			return true
		}
	}
	for _, parent := range classParentNames(class) {
		if target, ok := context.Targets[parent]; ok && classHasErrorMethod(target.Class, context, visiting) {
			return true
		}
	}
	return false
}

type promotedCall struct {
	types         []string
	trailingError bool
}

func transformImplicitErrorPromotion(src string, context constructorContext) (string, error) {
	if transformed, handled, err := transformImplicitErrorPromotionAST(src, context); handled {
		return transformed, err
	}
	return src, nil
}

// transformImplicitErrorPromotionLegacy is retained temporarily as a source
// compatibility reference while callers use the structured implementation
// above. It is intentionally not part of the compiler pipeline.
func transformImplicitErrorPromotionLegacy(src string, context constructorContext) (string, error) {
	if transformed, handled, err := transformImplicitErrorPromotionAST(src, context); handled {
		return transformed, err
	}
	return src, nil
	parsed, fileSet, prefixLength, err := parseExceptionSourceLegacy(src, context)
	if err != nil {
		return src, nil
	}
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(parsed, func(node ast.Node) bool {
		if node == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})

	type edit struct {
		start, end int
		text       string
	}
	edits := []edit{}
	valueTypes := polymorphicValueTypes(parsed, context)
	var promotionErr error
	ast.Inspect(parsed, func(node ast.Node) bool {
		if promotionErr != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		result, ok := promotedCallFor(call, context, valueTypes)
		if !ok || !result.trailingError || len(result.types) == 0 {
			return true
		}
		parent := parents[call]
		if _, isGoroutineCall := parent.(*ast.GoStmt); isGoroutineCall {
			promotionErr = fmt.Errorf("cannot implicitly propagate error from goroutine call; handle the error inside the goroutine")
			return true
		}
		if explicitlyCapturedError(call, parent, len(result.types)) {
			return true
		}
		if returnStatementMatchesFunctionResults(parent, parents, len(result.types)) {
			return true
		}
		replacement := ""
		discard := false
		if _, isStatement := parent.(*ast.ExprStmt); isStatement {
			discard = true
		} else if len(result.types) > 1 &&
			!callHasExpectedReducedResults(parent, len(result.types)-1) &&
			!callRequiresSingleValue(parent) {
			return true
		}
		nonErrorCount := len(result.types) - 1
		// An error-only result is still a value when it appears in an
		// expression context (for example, `done <- Save()`). It is omitted
		// only for a standalone statement.
		if nonErrorCount == 0 && !discard {
			return true
		}
		if discard {
			switch nonErrorCount {
			case 0:
				replacement = "__gppThrow(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			case 1:
				replacement = "__gppDiscard(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			case 2:
				replacement = "__gppDiscard2(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			case 3:
				replacement = "__gppDiscard3(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			default:
				replacement = inlineUnwrapCall(sourceNodeText(call, fileSet, prefixLength, src), result.types, true, context)
			}
		} else {
			switch nonErrorCount {
			case 1:
				replacement = "__gppUnwrap(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			case 2:
				replacement = "__gppUnwrap2(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			case 3:
				replacement = "__gppUnwrap3(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			default:
				replacement = inlineUnwrapCall(sourceNodeText(call, fileSet, prefixLength, src), result.types, false, context)
			}
		}
		if replacement == "" {
			return true
		}
		start := fileSet.Position(call.Pos()).Offset - prefixLength
		end := fileSet.Position(call.End()).Offset - prefixLength
		if start >= 0 && end <= len(src) {
			edits = append(edits, edit{start: start, end: end, text: replacement})
		}
		return true
	})
	if promotionErr != nil {
		return src, promotionErr
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, nil
}

type promotionASTBlock struct {
	block       *BlockStmt
	resultCount int
}

func transformImplicitErrorPromotionAST(src string, context constructorContext) (string, bool, error) {
	tokens, err := LexSource("implicit error promotion", src)
	if err != nil {
		return src, false, nil
	}
	for _, token := range tokens {
		if token.Text == "??" {
			// Error coalescing owns this expression and must see the original
			// multi-result call before promotion can wrap it.
			return src, true, nil
		}
	}
	blocks := []promotionASTBlock{}
	functions := parseTopLevelFunctions("implicit error promotion", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function == nil || function.Method.BodyAST == nil {
				continue
			}
			blocks = append(blocks, promotionASTBlock{
				block:       function.Method.BodyAST,
				resultCount: exceptionMethodResultCount(function.Method),
			})
		}
	} else if block, parseErr := ParseBodyAST(tokens); parseErr == nil && block != nil {
		blocks = append(blocks, promotionASTBlock{block: block, resultCount: exceptionContextResultCount(context)})
	}
	if len(blocks) == 0 {
		return src, false, nil
	}

	astContext := context
	astContext.CurrentParameterTypes = cloneStringMap(context.CurrentParameterTypes)
	if astContext.CurrentParameterTypes == nil {
		astContext.CurrentParameterTypes = map[string]string{}
	}
	for _, function := range functions {
		if function == nil {
			continue
		}
		for _, parameter := range function.Method.ParameterAST {
			if parameter.Type == nil {
				continue
			}
			if typeName, typeErr := typeNodeSource(parameter.Type); typeErr == nil {
				astContext.CurrentParameterTypes[parameter.Name] = strings.TrimSpace(typeName)
			}
		}
	}
	valueTypes := lambdaValueTypesAST(promotionBlocks(blocks), astContext)
	edits := []introspectionASTEdit{}
	var promotionErr error
	for _, current := range blocks {
		promotionWalkBlock(current.block, func(call *CallExpr, parent ExprNode, statement Stmt) {
			if promotionErr != nil || call == nil {
				return
			}
			if promotionASTAlreadyWrapped(parent) {
				return
			}
			result, ok := promotedCallForExpr(call, astContext, valueTypes)
			if !ok || !result.trailingError || len(result.types) == 0 {
				return
			}
			if _, isGoroutine := statement.(*GoStmt); isGoroutine && parent == nil {
				promotionErr = fmt.Errorf("cannot implicitly propagate error from goroutine call; handle the error inside the goroutine")
				return
			}
			resultCount := len(result.types)
			if promotionASTExplicitlyCaptured(statement, call, resultCount) ||
				promotionASTReturnMatches(statement, current.resultCount, resultCount) {
				return
			}
			direct := parent == nil
			discard := direct && isExpressionStatement(statement)
			if !discard && resultCount > 1 && !promotionASTHasExpectedReducedResults(statement, resultCount-1) && !promotionASTRequiresSingleValue(parent) {
				return
			}
			nonErrorCount := resultCount - 1
			if nonErrorCount == 0 && !discard {
				return
			}
			callText, sourceErr := expressionNodeSource(call)
			if sourceErr != nil {
				return
			}
			replacement := promotionASTReplacement(callText, result.types, discard, astContext)
			if replacement == "" {
				return
			}
			span := call.Span()
			if span.Start >= 0 && span.End <= len(src) && span.Start < span.End {
				edits = append(edits, introspectionASTEdit{start: span.Start, end: span.End, text: replacement})
			}
		})
	}
	if promotionErr != nil {
		return src, true, promotionErr
	}
	if len(edits) == 0 {
		return src, true, nil
	}
	sort.SliceStable(edits, func(left, right int) bool { return edits[left].start > edits[right].start })
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

func promotionBlocks(blocks []promotionASTBlock) []*BlockStmt {
	result := make([]*BlockStmt, 0, len(blocks))
	for _, block := range blocks {
		result = append(result, block.block)
	}
	return result
}

func exceptionContextResultCount(context constructorContext) int {
	if context.CurrentResultAST == nil {
		return 0
	}
	text, err := typeNodeSource(context.CurrentResultAST)
	if err != nil {
		return 0
	}
	return exceptionResultCountFromText(text)
}

func exceptionMethodResultCount(method Method) int {
	return exceptionResultCountFromText(methodResultSource(method))
}

func exceptionResultCountFromText(text string) int {
	parts, err := resultTypeParts(strings.TrimSpace(text))
	if err != nil {
		return 0
	}
	return len(parts)
}

func promotionWalkBlock(block *BlockStmt, visit func(*CallExpr, ExprNode, Stmt)) {
	if block == nil || visit == nil {
		return
	}
	for _, statement := range block.Statements {
		promotionWalkStatement(statement, visit)
	}
}

func promotionWalkStatement(statement Stmt, visit func(*CallExpr, ExprNode, Stmt)) {
	if statement == nil {
		return
	}
	root := func(expression ExprNode) {
		promotionWalkExpression(expression, nil, statement, visit)
	}
	switch value := statement.(type) {
	case *TokenStmt:
		for _, expression := range value.Exprs {
			root(expression)
		}
		promotionWalkBlock(value.Body, visit)
		for _, child := range value.Children {
			promotionWalkStatement(child, visit)
		}
	case *ExpressionStmt:
		root(value.Expression)
	case *DeclarationStmt:
		for _, expression := range value.Values {
			root(expression)
		}
	case *AssignmentStmt:
		for _, expression := range value.Left {
			root(expression)
		}
		for _, expression := range value.Right {
			root(expression)
		}
	case *ReturnStmt:
		for _, expression := range value.Values {
			root(expression)
		}
	case *ThrowStmt:
		root(value.Value)
	case *DeferStmt:
		root(value.Expression)
	case *GoStmt:
		root(value.Expression)
	case *SendStmt:
		root(value.Channel)
		root(value.Value)
	case *IncDecStmt:
		root(value.Expression)
	case *IfStmt:
		root(value.Init)
		root(value.Condition)
		promotionWalkBlock(value.Body, visit)
		promotionWalkBlock(value.Else, visit)
		if value.ElseIf != nil {
			promotionWalkStatement(value.ElseIf, visit)
		}
	case *ForStmt:
		root(value.Init)
		root(value.Condition)
		root(value.Post)
		root(value.RangeExpr)
		promotionWalkBlock(value.Body, visit)
	case *SwitchStmt:
		root(value.Init)
		root(value.Tag)
		promotionWalkBlock(value.Body, visit)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			root(expression)
		}
		promotionWalkBlock(value.Clause.Body, visit)
	case *TryStmt:
		promotionWalkBlock(value.Body, visit)
		for _, clause := range value.Catches {
			promotionWalkBlock(clause.Body, visit)
		}
		promotionWalkBlock(value.Finally, visit)
	}
}

func promotionWalkExpression(expression ExprNode, parent ExprNode, statement Stmt, visit func(*CallExpr, ExprNode, Stmt)) {
	if expression == nil {
		return
	}
	if call, ok := expression.(*CallExpr); ok {
		visit(call, parent, statement)
	}
	next := func(child ExprNode) { promotionWalkExpression(child, expression, statement, visit) }
	switch value := expression.(type) {
	case *UnaryExpr:
		next(value.Operand)
	case *BinaryExpr:
		next(value.Left)
		next(value.Right)
	case *SelectorExpr:
		next(value.Receiver)
	case *IndexExpr:
		next(value.Receiver)
		next(value.Index)
	case *IndexListExpr:
		next(value.Receiver)
		for _, index := range value.Indices {
			next(index)
		}
	case *SliceExpr:
		next(value.Receiver)
		next(value.Low)
		next(value.High)
		next(value.Max)
	case *TypeAssertExpr:
		next(value.Expression)
	case *PostfixExpr:
		next(value.Expression)
	case *SpreadExpr:
		next(value.Expression)
	case *SendExpr:
		next(value.Channel)
		next(value.Value)
	case *CallExpr:
		next(value.Callee)
		for _, argument := range value.Arguments {
			next(argument.Value)
		}
	case *ParenthesizedExpr:
		next(value.Inner)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			next(element.Key)
			next(element.Value)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			next(segment.Expression)
		}
	case *LambdaExpr:
		next(value.Body)
		promotionWalkBlock(value.BlockBody, visit)
	case *FunctionLiteralExpr:
		promotionWalkBlock(value.Body, visit)
	}
}

func isExpressionStatement(statement Stmt) bool {
	_, ok := statement.(*ExpressionStmt)
	return ok
}

func promotionASTExplicitlyCaptured(statement Stmt, call *CallExpr, resultCount int) bool {
	switch value := statement.(type) {
	case *AssignmentStmt:
		return len(value.Right) == 1 && len(value.Left) == resultCount && value.Right[0] == call
	case *DeclarationStmt:
		return len(value.Values) == 1 && len(value.Names) == resultCount && value.Values[0] == call
	case *ReturnStmt:
		return len(value.Values) == resultCount && len(value.Values) == 1 && value.Values[0] == call
	default:
		return false
	}
}

func promotionASTReturnMatches(statement Stmt, functionResultCount, resultCount int) bool {
	value, ok := statement.(*ReturnStmt)
	return ok && len(value.Values) == 1 && functionResultCount == resultCount
}

func promotionASTHasExpectedReducedResults(statement Stmt, reducedCount int) bool {
	switch value := statement.(type) {
	case *AssignmentStmt:
		return len(value.Right) == 1 && len(value.Left) == reducedCount
	case *DeclarationStmt:
		return len(value.Values) == 1 && len(value.Names) == reducedCount
	case *ReturnStmt:
		return len(value.Values) == reducedCount
	default:
		return false
	}
}

func promotionASTRequiresSingleValue(expression ExprNode) bool {
	switch expression.(type) {
	case *BinaryExpr, *UnaryExpr, *ParenthesizedExpr, *SelectorExpr, *IndexExpr, *IndexListExpr, *SliceExpr, *TypeAssertExpr:
		return true
	default:
		return false
	}
}

func promotionASTAlreadyWrapped(expression ExprNode) bool {
	call, ok := expression.(*CallExpr)
	if !ok || call == nil {
		return false
	}
	name, ok := call.Callee.(*NameExpr)
	if !ok {
		return false
	}
	switch name.Name {
	case "__gppUnwrap", "__gppUnwrap2", "__gppUnwrap3", "__gppDiscard", "__gppDiscard2", "__gppDiscard3", "__gppThrow":
		return true
	default:
		return false
	}
}

func promotionASTReplacement(callText string, resultTypes []string, discard bool, context constructorContext) string {
	nonErrorCount := len(resultTypes) - 1
	if discard {
		switch nonErrorCount {
		case 0:
			return "__gppThrow(" + callText + ")"
		case 1:
			return "__gppDiscard(" + callText + ")"
		case 2:
			return "__gppDiscard2(" + callText + ")"
		case 3:
			return "__gppDiscard3(" + callText + ")"
		}
	}
	switch nonErrorCount {
	case 1:
		return "__gppUnwrap(" + callText + ")"
	case 2:
		return "__gppUnwrap2(" + callText + ")"
	case 3:
		return "__gppUnwrap3(" + callText + ")"
	default:
		return inlineUnwrapCall(callText, resultTypes, discard, context)
	}
}

func promotedCallForExpr(call *CallExpr, context constructorContext, valueTypes map[string]string) (promotedCall, bool) {
	if call == nil {
		return promotedCall{}, false
	}
	candidates := []callableSignature{}
	switch function := call.Callee.(type) {
	case *NameExpr:
		candidates = append(candidates, context.FunctionSignatures[function.Name]...)
		for _, signatures := range context.StaticMethodSignatures {
			for _, methods := range signatures {
				for _, candidate := range methods {
					if candidate.GoName == function.Name {
						candidates = append(candidates, candidate)
					}
				}
			}
		}
		for _, extension := range context.Extensions {
			if extension.GoName == function.Name {
				candidates = append(candidates, callableSignature{Parameters: extensionCallParameters(extension), ResultAST: parseTypeText(strings.TrimSpace(methodResultSource(extension.Method)))})
			}
		}
	case *SelectorExpr:
		actualType := staticExpressionTypeNode(function.Receiver, context, valueTypes)
		if receiver, ok := function.Receiver.(*NameExpr); ok {
			if _, isClass := context.Targets[receiver.Name]; isClass {
				candidates = append(candidates, context.StaticMethodSignatures[receiver.Name][function.Name]...)
			} else {
				className := strings.TrimPrefix(strings.TrimSpace(actualType), "*")
				if receiver.Name == "this" && context.CurrentClass != "" {
					className = context.CurrentClass
				}
				candidates = append(candidates, context.ClassMethodSignatures[className][function.Name]...)
			}
		}
		for _, extension := range context.Extensions {
			if extension.Method.Name == function.Name && extensionTargetMatches(extension.Target, extension.ReceiverType, actualType, context) {
				candidates = append(candidates, callableSignature{Parameters: extensionCallParameters(extension), ResultAST: parseTypeText(strings.TrimSpace(methodResultSource(extension.Method)))})
			}
		}
		if native, ok := promotedNativePackageCall(function, call, context); ok {
			return native, true
		}
		if native, ok := promotedNativeMethodCall(function, call, context, valueTypes); ok {
			return native, true
		}
	}
	for _, candidate := range candidates {
		if len(call.Arguments) < requiredParameterCount(candidate) || len(call.Arguments) > len(candidate.Parameters) {
			continue
		}
		resultTypes := resultTypesFromText(candidate.resultText())
		if len(resultTypes) > 0 && isErrorLikeType(resultTypes[len(resultTypes)-1], context) {
			return promotedCall{types: resultTypes, trailingError: true}, true
		}
	}
	return promotedCall{}, false
}

func promotedNativePackageCall(function *SelectorExpr, call *CallExpr, context constructorContext) (promotedCall, bool) {
	receiver, ok := function.Receiver.(*NameExpr)
	if !ok {
		return promotedCall{}, false
	}
	importPath := context.AvailableImports[receiver.Name]
	if importPath == "" {
		importPath = receiver.Name
	}
	pkg, err := importNativePackage(importPath)
	if err != nil {
		return promotedCall{}, false
	}
	object, ok := pkg.Scope().Lookup(function.Name).(*types.Func)
	if !ok {
		return promotedCall{}, false
	}
	signature, ok := object.Type().(*types.Signature)
	if !ok {
		return promotedCall{}, false
	}
	return promotedCallFromNativeSignature(signature)
}

func promotedNativeMethodCall(function *SelectorExpr, call *CallExpr, context constructorContext, valueTypes map[string]string) (promotedCall, bool) {
	typeName := staticExpressionTypeNode(function.Receiver, context, valueTypes)
	named, pkg, ok := nativeNamedType(typeName, context)
	if !ok {
		return promotedCall{}, false
	}
	method := types.NewMethodSet(types.NewPointer(named)).Lookup(pkg, function.Name)
	if method == nil {
		method = types.NewMethodSet(named).Lookup(pkg, function.Name)
	}
	if method == nil {
		return promotedCall{}, false
	}
	signature, ok := method.Type().(*types.Signature)
	if !ok {
		return promotedCall{}, false
	}
	return promotedCallFromNativeSignature(signature)
}

func inlineUnwrapCall(callText string, resultTypes []string, discard bool, context constructorContext) string {
	nonErrorCount := len(resultTypes) - 1
	valueNames := make([]string, nonErrorCount)
	for index := range valueNames {
		valueNames[index] = fmt.Sprintf("__gppExceptionValue%d", index)
	}
	errorName := "__gppExceptionError"
	assignmentNames := append([]string{}, valueNames...)
	if discard {
		for index := range assignmentNames {
			assignmentNames[index] = "_"
		}
	}
	assignmentNames = append(assignmentNames, errorName)
	assignment := strings.Join(assignmentNames, ", ") + " := " + callText
	if discard {
		return "func() { " + assignment + "; __gppThrow(" + errorName + ") }()"
	}
	resultTypesText := make([]string, nonErrorCount)
	for index, resultType := range resultTypes[:nonErrorCount] {
		resultTypesText[index] = transformPolymorphicType(resultType, context)
	}
	return "func() (" + strings.Join(resultTypesText, ", ") + ") { " + assignment + "; __gppThrow(" + errorName + "); return " + strings.Join(valueNames, ", ") + " }()"
}

type exceptionResultField struct {
	name     string
	typeNode TypeNode
}

func exceptionResultFieldType(field exceptionResultField) string {
	if field.typeNode == nil {
		return ""
	}
	text, err := typeNodeSource(field.typeNode)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

func transformExceptionABIBoundaries(src string, context constructorContext) (string, error) {
	// This is the final ABI boundary for a structured top-level function. The
	// source has already gone through the Go++ body AST lowerers, so use the
	// parsed FunctionDecl/Method metadata directly instead of reparsing the
	// lowered fragment through go/parser. The generated wrapper is still
	// materialized as Go source at the emission boundary.
	functions := parseTopLevelFunctions("exception ABI", "main", src, 0, src, "", 0)
	if len(functions) == 0 {
		return src, nil
	}
	type replacement struct {
		start int
		end   int
		text  string
	}
	replacements := []replacement{}
	for _, function := range functions {
		if function == nil || function.Method.BodyAST == nil {
			continue
		}
		bodyStart := function.Method.BodySpan.Start
		bodyEnd := function.Method.BodySpan.End
		if bodyStart < 0 || bodyEnd < bodyStart || bodyEnd > len(src) {
			continue
		}
		used := map[string]bool{}
		for _, token := range function.Method.BodyTokens {
			if token.Kind == TokenIdentifier || token.Kind == TokenKeyword {
				used[token.Text] = true
			}
		}
		bodyText := src[bodyStart:bodyEnd]
		hasExceptionReturn := strings.Contains(bodyText, "__gppExceptionReturn")
		fields, hasResults := exceptionResultFieldsFromText(methodResultSource(function.Method))
		if !hasResults || len(fields) == 0 {
			if !hasExceptionReturn {
				continue
			}
			bodyText, _ = rewriteExceptionReturns(bodyText, context)
			replacements = append(replacements, replacement{
				start: bodyStart,
				end:   bodyEnd,
				text:  exceptionVoidBoundaryBody(bodyText, nextExceptionName(used, "__gppBoundaryReturned"), used),
			})
			continue
		}
		isErrorBoundary := exceptionResultFieldType(fields[len(fields)-1]) == "error"
		if !isErrorBoundary && !hasExceptionReturn {
			continue
		}
		if hasExceptionReturn {
			bodyText, _ = rewriteExceptionReturns(bodyText, context)
		}
		for index := range fields {
			if fields[index].name == "" || fields[index].name == "_" {
				fields[index].name = nextExceptionName(used, fmt.Sprintf("__gppBoundaryResult%d", index))
			}
		}
		recoverName := nextExceptionName(used, "__gppBoundaryRecovered")
		thrownName := nextExceptionName(used, "__gppBoundaryThrown")
		okName := nextExceptionName(used, "__gppBoundaryIsThrown")
		opening := exceptionReturnBoundaryOpening(fields, recoverName, thrownName, okName)
		if isErrorBoundary {
			opening = exceptionBoundaryOpening(fields, recoverName, thrownName, okName)
		}
		replacements = append(replacements, replacement{
			start: bodyStart,
			end:   bodyEnd,
			text:  opening + bodyText + "\nreturn\n}()",
		})
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	for _, replacement := range replacements {
		src = src[:replacement.start] + replacement.text + src[replacement.end:]
	}
	return src, nil
}

func exceptionVoidBoundaryBody(body, returnedName string, used map[string]bool) string {
	recoverName := nextExceptionName(used, "__gppBoundaryRecovered")
	okName := nextExceptionName(used, "__gppIsReturned")
	var out strings.Builder
	out.WriteString("var ")
	out.WriteString(returnedName)
	out.WriteString(" bool\n")
	out.WriteString("func() {\n")
	out.WriteString("defer func() {\n")
	out.WriteString(recoverName)
	out.WriteString(" := recover()\n")
	out.WriteString("if ")
	out.WriteString(recoverName)
	out.WriteString(" != nil {\n")
	out.WriteString("_, ")
	out.WriteString(okName)
	out.WriteString(" := ")
	out.WriteString(recoverName)
	out.WriteString(".(__gppExceptionReturn)\n")
	out.WriteString("if !")
	out.WriteString(okName)
	out.WriteString(" { panic(")
	out.WriteString(recoverName)
	out.WriteString(") }\n")
	out.WriteString(returnedName)
	out.WriteString(" = true\n")
	out.WriteString("}\n")
	out.WriteString("}()\n")
	out.WriteString(body)
	out.WriteString("\n}()\n")
	out.WriteString("if ")
	out.WriteString(returnedName)
	out.WriteString(" { return }")
	return out.String()
}

func exceptionResultFields(results *ast.FieldList) ([]exceptionResultField, bool) {
	if results == nil {
		return nil, false
	}
	fields := []exceptionResultField{}
	for _, field := range results.List {
		typeName, err := formatNode(field.Type)
		if err != nil {
			return nil, false
		}
		typeNode := parseTypeText(strings.TrimSpace(typeName))
		if typeNode == nil {
			return nil, false
		}
		if len(field.Names) == 0 {
			fields = append(fields, exceptionResultField{typeNode: typeNode})
			continue
		}
		for _, name := range field.Names {
			fields = append(fields, exceptionResultField{name: name.Name, typeNode: typeNode})
		}
	}
	return fields, true
}

func exceptionResultFieldsFromText(result string) ([]exceptionResultField, bool) {
	parts, err := resultTypeParts(result)
	if err != nil {
		return nil, false
	}
	fields := make([]exceptionResultField, 0, len(parts))
	for _, part := range parts {
		parameters, parameterErr := parseParameterInfos(part)
		if parameterErr != nil || len(parameters) != 1 {
			return nil, false
		}
		typeNode := parameters[0].TypeAST
		if typeNode == nil {
			typeNode = parseTypeText(strings.TrimSpace(parameters[0].typeText()))
		}
		if typeNode == nil {
			return nil, false
		}
		fields = append(fields, exceptionResultField{name: parameters[0].Name, typeNode: typeNode})
	}
	return fields, true
}

func resultTypeParts(result string) ([]string, error) {
	result = strings.TrimSpace(result)
	if result == "" {
		return nil, nil
	}
	if result[0] != '(' {
		return []string{result}, nil
	}
	close, err := findMatchingParen(result, 0)
	if err != nil || strings.TrimSpace(result[close+1:]) != "" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("invalid result type %q", result)
	}
	return splitTopLevel(result[1:close], ',')
}

func wrapExceptionBoundaryBody(body, result string, context constructorContext) (string, bool) {
	fields, ok := exceptionResultFieldsFromText(result)
	if !ok {
		return body, false
	}
	if len(fields) == 0 {
		if !strings.Contains(body, "__gppExceptionReturn") {
			return body, false
		}
		body, _ = rewriteExceptionReturns(body, context)
		used := map[string]bool{}
		returnedName := nextExceptionName(used, "__gppBoundaryReturned")
		return exceptionVoidBoundaryBody(body, returnedName, used), true
	}
	isErrorBoundary := exceptionResultFieldType(fields[len(fields)-1]) == "error"
	if !isErrorBoundary && !strings.Contains(body, "__gppExceptionReturn") {
		return body, false
	}
	if strings.Contains(body, "__gppExceptionReturn") {
		body, _ = rewriteExceptionReturns(body, context)
	}
	used := map[string]bool{}
	if tokens, err := LexSource("exception boundary body", body); err == nil {
		for _, token := range tokens {
			if token.Kind == TokenIdentifier || token.Kind == TokenKeyword {
				used[token.Text] = true
			}
		}
	}
	for index := range fields {
		typeName := transformPolymorphicType(exceptionResultFieldType(fields[index]), context)
		fields[index].typeNode = parseTypeText(typeName)
		if fields[index].name == "" || fields[index].name == "_" {
			fields[index].name = nextExceptionName(used, fmt.Sprintf("__gppBoundaryResult%d", index))
		}
	}
	recoverName := nextExceptionName(used, "__gppBoundaryRecovered")
	thrownName := nextExceptionName(used, "__gppBoundaryThrown")
	okName := nextExceptionName(used, "__gppBoundaryIsThrown")
	opening := exceptionReturnBoundaryOpening(fields, recoverName, thrownName, okName)
	if isErrorBoundary {
		opening = exceptionBoundaryOpening(fields, recoverName, thrownName, okName)
	}
	return opening + body + "\nreturn\n}()", true
}

func nextExceptionName(used map[string]bool, base string) string {
	name := base
	for suffix := 1; used[name]; suffix++ {
		name = fmt.Sprintf("%s_%d", base, suffix)
	}
	used[name] = true
	return name
}

func exceptionBoundaryOpening(fields []exceptionResultField, recoverName, thrownName, okName string) string {
	resultParts := make([]string, len(fields))
	for index, field := range fields {
		resultParts[index] = field.name + " " + exceptionResultFieldType(field)
	}
	var out strings.Builder
	out.WriteString("return func() (")
	out.WriteString(strings.Join(resultParts, ", "))
	out.WriteString(") {\n")
	out.WriteString("defer func() {\n")
	out.WriteString(recoverName)
	out.WriteString(" := recover()\n")
	out.WriteString("if ")
	out.WriteString(recoverName)
	out.WriteString(" != nil {\n")
	out.WriteString("if __gppReturned, __gppIsReturned := ")
	out.WriteString(recoverName)
	out.WriteString(".(__gppExceptionReturn); __gppIsReturned {\n")
	out.WriteString("if len(__gppReturned.values) == 0 { return }\n")
	for index, field := range fields {
		out.WriteString(field.name)
		out.WriteString(" = __gppReturned.values[")
		out.WriteString(fmt.Sprintf("%d", index))
		out.WriteString("].(")
		out.WriteString(exceptionResultFieldType(field))
		out.WriteString(")\n")
	}
	out.WriteString("return\n")
	out.WriteString("}\n")
	out.WriteString(thrownName)
	out.WriteString(", ")
	out.WriteString(okName)
	out.WriteString(" := ")
	out.WriteString(recoverName)
	out.WriteString(".(__gppThrownError)\n")
	out.WriteString("if !")
	out.WriteString(okName)
	out.WriteString(" { panic(")
	out.WriteString(recoverName)
	out.WriteString(") }\n")
	for index, field := range fields[:len(fields)-1] {
		zeroName := fmt.Sprintf("__gppBoundaryZero%d", index)
		out.WriteString("var ")
		out.WriteString(zeroName)
		out.WriteByte(' ')
		out.WriteString(exceptionResultFieldType(field))
		out.WriteByte('\n')
		out.WriteString(field.name)
		out.WriteString(" = ")
		out.WriteString(zeroName)
		out.WriteByte('\n')
	}
	out.WriteString(fields[len(fields)-1].name)
	out.WriteString(" = ")
	out.WriteString(thrownName)
	out.WriteString(".err\n")
	out.WriteString("}\n")
	out.WriteString("}()\n")
	return out.String()
}

func exceptionReturnBoundaryOpening(fields []exceptionResultField, recoverName, _, _ string) string {
	resultParts := make([]string, len(fields))
	for index, field := range fields {
		resultParts[index] = field.name + " " + exceptionResultFieldType(field)
	}
	var out strings.Builder
	out.WriteString("return func() (")
	out.WriteString(strings.Join(resultParts, ", "))
	out.WriteString(") {\n")
	out.WriteString("defer func() {\n")
	out.WriteString(recoverName)
	out.WriteString(" := recover()\n")
	out.WriteString("if ")
	out.WriteString(recoverName)
	out.WriteString(" != nil {\n")
	out.WriteString("__gppReturned, __gppIsReturned := ")
	out.WriteString(recoverName)
	out.WriteString(".(__gppExceptionReturn)\n")
	out.WriteString("if !__gppIsReturned { panic(")
	out.WriteString(recoverName)
	out.WriteString(") }\n")
	out.WriteString("if len(__gppReturned.values) == 0 { return }\n")
	for index, field := range fields {
		out.WriteString(field.name)
		out.WriteString(" = __gppReturned.values[")
		out.WriteString(fmt.Sprintf("%d", index))
		out.WriteString("].(")
		out.WriteString(exceptionResultFieldType(field))
		out.WriteString(")\n")
	}
	out.WriteString("}\n")
	out.WriteString("}()\n")
	return out.String()
}

func parseExceptionSourceLegacy(src string, context constructorContext) (*ast.File, *token.FileSet, int, error) {
	fileSet := token.NewFileSet()
	const filePrefix = "package main\n\n"
	parsed, err := parser.ParseFile(fileSet, "generated.go", filePrefix+src, 0)
	if err == nil {
		return parsed, fileSet, len(filePrefix), nil
	}
	functionPrefix := filePrefix + "func __gpp_scope()"
	if result := context.currentResultText(); result != "" {
		functionPrefix += " " + result
	}
	functionPrefix += " {\n"
	functionSet := token.NewFileSet()
	parsed, err = parser.ParseFile(functionSet, "generated.go", functionPrefix+src+"\n}", 0)
	if err != nil {
		return nil, nil, 0, err
	}
	return parsed, functionSet, len(functionPrefix), nil
}

func sourceNodeText(node ast.Node, fileSet *token.FileSet, prefixLength int, src string) string {
	start := fileSet.Position(node.Pos()).Offset - prefixLength
	end := fileSet.Position(node.End()).Offset - prefixLength
	if start < 0 || end > len(src) || start > end {
		return ""
	}
	return src[start:end]
}

func explicitlyCapturedError(call *ast.CallExpr, parent ast.Node, resultCount int) bool {
	switch statement := parent.(type) {
	case *ast.AssignStmt:
		return len(statement.Rhs) == 1 && len(statement.Lhs) == resultCount
	case *ast.ValueSpec:
		return len(statement.Values) == 1 && len(statement.Names) == resultCount
	case *ast.ReturnStmt:
		return len(statement.Results) == resultCount
	default:
		return false
	}
}

func returnStatementMatchesFunctionResults(parent ast.Node, parents map[ast.Node]ast.Node, resultCount int) bool {
	returnStatement, ok := parent.(*ast.ReturnStmt)
	if !ok || returnStatement == nil {
		return false
	}
	for node := ast.Node(returnStatement); node != nil; node = parents[node] {
		if function, ok := node.(*ast.FuncDecl); ok {
			fields, hasResults := exceptionResultFields(function.Type.Results)
			return hasResults && len(fields) == resultCount
		}
	}
	return false
}

func callHasExpectedReducedResults(parent ast.Node, reducedCount int) bool {
	switch statement := parent.(type) {
	case *ast.AssignStmt:
		return len(statement.Rhs) == 1 && len(statement.Lhs) == reducedCount
	case *ast.ValueSpec:
		return len(statement.Values) == 1 && len(statement.Names) == reducedCount
	case *ast.ReturnStmt:
		return len(statement.Results) == reducedCount
	default:
		return false
	}
}

func callRequiresSingleValue(parent ast.Node) bool {
	switch parent.(type) {
	case *ast.BinaryExpr, *ast.UnaryExpr, *ast.ParenExpr,
		*ast.SelectorExpr, *ast.IndexExpr, *ast.SliceExpr,
		*ast.TypeAssertExpr, *ast.KeyValueExpr:
		return true
	default:
		return false
	}
}

func promotedCallFor(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) (promotedCall, bool) {
	var candidates []callableSignature
	switch function := call.Fun.(type) {
	case *ast.Ident:
		candidates = append(candidates, context.FunctionSignatures[function.Name]...)
		for _, signatures := range context.StaticMethodSignatures {
			for _, methods := range signatures {
				for _, candidate := range methods {
					if candidate.GoName == function.Name {
						candidates = append(candidates, candidate)
					}
				}
			}
		}
		for _, extension := range context.Extensions {
			if extension.GoName == function.Name {
				candidates = append(candidates, callableSignature{
					Parameters: extensionCallParameters(extension),
					ResultAST:  parseTypeText(strings.TrimSpace(methodResultSource(extension.Method))),
				})
			}
		}
	case *ast.SelectorExpr:
		if receiver, ok := function.X.(*ast.Ident); ok {
			if _, isClass := context.Targets[receiver.Name]; isClass {
				candidates = append(candidates, context.StaticMethodSignatures[receiver.Name][function.Sel.Name]...)
			} else {
				className := context.CurrentClass
				if receiver.Name != "this" {
					className = strings.TrimPrefix(valueTypes[receiver.Name], "*")
				}
				candidates = append(candidates, context.ClassMethodSignatures[className][function.Sel.Name]...)
			}
			hasQualifiedExtension := false
			for _, extension := range context.Extensions {
				if extension.Qualifier == receiver.Name && extension.GoName == function.Sel.Name {
					hasQualifiedExtension = true
					candidates = append(candidates, callableSignature{
						Parameters: extensionCallParameters(extension),
						ResultAST:  parseTypeText(strings.TrimSpace(methodResultSource(extension.Method))),
					})
				}
			}
			if !hasQualifiedExtension {
				if native, ok := nativePackageFunction(call, context); ok {
					return native, true
				}
				if native, ok := nativeMethodFunction(call, context, valueTypes); ok {
					return native, true
				}
			}
		}
	}
	for _, candidate := range candidates {
		if len(call.Args) < requiredParameterCount(candidate) || len(call.Args) > len(candidate.Parameters) {
			continue
		}
		resultTypes := resultTypesFromText(candidate.resultText())
		if len(resultTypes) == 0 || !isErrorLikeType(resultTypes[len(resultTypes)-1], context) {
			continue
		}
		return promotedCall{types: resultTypes, trailingError: true}, true
	}
	return promotedCall{}, false
}

func mustParameters(source string) []parameterInfo {
	parameters, err := parseParameterInfos(source)
	if err != nil {
		return nil
	}
	return parameters
}

func extensionCallParameters(extension extensionMethod) []parameterInfo {
	parameters := []parameterInfo{{Name: "this", TypeAST: parseTypeText(extension.ReceiverType)}}
	return append(parameters, mustParameters(methodParametersSource(extension.Method))...)
}

func resultTypesFromText(result string) []string {
	fields, ok := exceptionResultFieldsFromText(result)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		values = append(values, exceptionResultFieldType(field))
	}
	return values
}

func isErrorLikeType(typeName string, context constructorContext) bool {
	typeName = strings.TrimSpace(typeName)
	if typeName == "error" {
		return true
	}
	base := strings.TrimPrefix(typeName, "*")
	if target, ok := context.Targets[base]; ok {
		return classHasErrorMethod(target.Class, context, map[string]bool{})
	}
	return false
}

func nativePackageFunction(call *ast.CallExpr, context constructorContext) (promotedCall, bool) {
	signature, ok := nativePackageFunctionSignature(call, context)
	if !ok {
		return promotedCall{}, false
	}
	return promotedCallFromNativeSignature(signature)
}

func nativePackageFunctionSignature(call *ast.CallExpr, context constructorContext) (*types.Signature, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	receiver, ok := selector.X.(*ast.Ident)
	if !ok {
		return nil, false
	}
	importPath := ""
	if context.AvailableImports != nil {
		importPath = context.AvailableImports[receiver.Name]
	}
	if importPath == "" {
		// A standalone Emit call may not have a package import table yet. The
		// standard importer still gives us a useful fallback for conventional
		// package-qualified Go calls such as os.ReadFile.
		importPath = receiver.Name
	}
	pkg, err := importNativePackage(importPath)
	if err != nil {
		return nil, false
	}
	object := pkg.Scope().Lookup(selector.Sel.Name)
	function, ok := object.(*types.Func)
	if !ok {
		return nil, false
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Results() == nil || signature.Results().Len() == 0 {
		return nil, false
	}
	return signature, true
}

func nativeMethodFunction(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) (promotedCall, bool) {
	signature, ok := nativeMethodSignature(call, context, valueTypes)
	if !ok {
		return promotedCall{}, false
	}
	return promotedCallFromNativeSignature(signature)
}

func nativeMethodSignature(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) (*types.Signature, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	typeName := expressionStaticType(selector.X, context, valueTypes)
	named, pkg, ok := nativeNamedType(typeName, context)
	if !ok {
		return nil, false
	}
	method := types.NewMethodSet(types.NewPointer(named)).Lookup(pkg, selector.Sel.Name)
	if method == nil {
		return nil, false
	}
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Results() == nil || signature.Results().Len() == 0 {
		return nil, false
	}
	return signature, true
}

func nativePackageResultTypes(call *ast.CallExpr, context constructorContext) ([]string, bool) {
	signature, ok := nativePackageFunctionSignature(call, context)
	if !ok {
		return nil, false
	}
	return nativeSignatureResultTypes(signature, context), true
}

func nativeMethodResultTypes(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) ([]string, bool) {
	signature, ok := nativeMethodSignature(call, context, valueTypes)
	if !ok {
		return nil, false
	}
	return nativeSignatureResultTypes(signature, context), true
}

func nativeSignatureResultTypes(signature *types.Signature, context constructorContext) []string {
	if signature == nil || signature.Results() == nil || signature.Results().Len() == 0 {
		return nil
	}
	result := make([]string, signature.Results().Len())
	for index := range result {
		result[index] = types.TypeString(signature.Results().At(index).Type(), nativeTypeQualifier(context))
	}
	return result
}

func nativeNamedType(typeName string, context constructorContext) (*types.Named, *types.Package, bool) {
	typeName = strings.TrimPrefix(strings.TrimSpace(typeName), "*")
	separator := strings.LastIndex(typeName, ".")
	if separator <= 0 || separator+1 >= len(typeName) {
		return nil, nil, false
	}
	packageName := typeName[:separator]
	objectName := typeName[separator+1:]
	importPath := packageName
	if context.AvailableImports != nil {
		if resolved := context.AvailableImports[packageName]; resolved != "" {
			importPath = resolved
		}
	}
	pkg, err := importNativePackage(importPath)
	if err != nil {
		return nil, nil, false
	}
	object, ok := pkg.Scope().Lookup(objectName).(*types.TypeName)
	if !ok {
		return nil, nil, false
	}
	named, ok := object.Type().(*types.Named)
	if !ok {
		return nil, nil, false
	}
	return named, pkg, true
}

func nativeTypeQualifier(context constructorContext) func(*types.Package) string {
	return func(pkg *types.Package) string {
		if pkg == nil {
			return ""
		}
		for alias, importPath := range context.AvailableImports {
			if importPath == pkg.Path() {
				return alias
			}
		}
		return pkg.Path()
	}
}

func promotedCallFromNativeSignature(signature *types.Signature) (promotedCall, bool) {
	if signature == nil || signature.Results() == nil || signature.Results().Len() == 0 {
		return promotedCall{}, false
	}
	results := []string{}
	for index := 0; index < signature.Results().Len(); index++ {
		results = append(results, types.TypeString(signature.Results().At(index).Type(), nil))
	}
	errorType := types.Universe.Lookup("error").Type()
	if !types.AssignableTo(signature.Results().At(signature.Results().Len()-1).Type(), errorType) {
		return promotedCall{}, false
	}
	return promotedCall{types: results, trailingError: true}, true
}
