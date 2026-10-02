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

func __gppSafeCoalesce[T any](isNil bool, access func() T, fallback func() T) T {
	if isNil {
		return fallback()
	}
	return __gppCoalesce(access, fallback)
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
	typeNodes  []TypeNode
	value      string
	errorAlias string
	variable   string
	body       *ast.BlockStmt
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

// transformExceptionMethodBodyAST lowers an already-parsed method body. It is
// used by declaration emitters that still have to assemble a surrounding
// declaration from source, but must not send the executable body through the
// source parser a second time merely to lower exceptions.
func transformExceptionMethodBodyAST(body string, method *Method, context constructorContext) (string, bool, error) {
	if method == nil || method.BodyAST == nil || !blockContainsExceptionSyntax(method.BodyAST) {
		return body, false, nil
	}
	methodContext := context
	methodContext.CurrentResultAST = method.ResultAST
	methodContext.CurrentParameterTypes = parameterTypeMapFromNodes(method.ParameterAST)
	methodContext.CurrentParameterAST = parameterTypeNodeMapFromNodes(method.ParameterAST)
	if err := lowerExceptionPromotions(method.BodyAST, methodContext); err != nil {
		return body, true, err
	}
	lowered, err := lowerFunctionGoBlockNode(method.BodyAST, methodContext, "")
	if err != nil {
		// TokenStmt and other unsupported nodes are still handled by the
		// compatibility source lowerer. Do not claim that the typed path handled
		// a body it could not represent completely.
		return body, false, nil
	}
	text, err := formatExceptionASTBody(lowered)
	if err != nil {
		return body, true, err
	}
	return text, true, nil
}

func blockContainsExceptionSyntax(block *BlockStmt) bool {
	if block == nil {
		return false
	}
	for _, statement := range block.Statements {
		if statementContainsExceptionSyntax(statement) {
			return true
		}
	}
	return false
}

func statementContainsExceptionSyntax(statement Stmt) bool {
	if statement == nil || isNilStmt(statement) {
		return false
	}
	switch value := statement.(type) {
	case *TryStmt, *ThrowStmt:
		return true
	case *TokenStmt:
		if blockContainsExceptionSyntax(value.Body) {
			return true
		}
		for _, child := range value.Children {
			if statementContainsExceptionSyntax(child) {
				return true
			}
		}
	case *IfStmt:
		return blockContainsExceptionSyntax(value.Body) || blockContainsExceptionSyntax(value.Else) || statementContainsExceptionSyntax(value.ElseIf)
	case *ForStmt:
		return blockContainsExceptionSyntax(value.Body)
	case *SwitchStmt:
		return blockContainsExceptionSyntax(value.Body)
	case *CaseStmt:
		return blockContainsExceptionSyntax(value.Clause.Body)
	case *BlockStmt:
		return blockContainsExceptionSyntax(value)
	}
	return false
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
			lowered, err := lowerASTThrowNode(statement, context, rethrowName)
			if err != nil {
				return "", true, err
			}
			text, formatErr := formatExceptionASTNode(lowered)
			if formatErr != nil {
				return "", true, formatErr
			}
			edits = append(edits, edit{start: start, end: end, text: text})
		case *TryStmt:
			lowered, err := lowerASTTryNode(statement, context, rethrowName)
			if err != nil {
				return "", true, err
			}
			text, formatErr := formatExceptionASTNode(lowered)
			if formatErr != nil {
				return "", true, formatErr
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

func lowerASTThrowNode(statement *ThrowStmt, context constructorContext, rethrowName string) (ast.Stmt, error) {
	if statement == nil || statement.Value == nil {
		if rethrowName == "" {
			return nil, fmt.Errorf("bare throw is only valid inside catch")
		}
		return &ast.ExprStmt{X: &ast.CallExpr{Fun: ast.NewIdent("panic"), Args: []ast.Expr{ast.NewIdent(rethrowName)}}}, nil
	}
	if err := validateThrowExpressionNode(statement.Value, context); err != nil {
		return nil, err
	}
	expression, err := lowerExceptionExprNode(statement.Value, context)
	if err != nil {
		return nil, err
	}
	lowered, err := goExprNode(expression)
	if err != nil {
		return nil, err
	}
	return &ast.ExprStmt{X: &ast.CallExpr{Fun: ast.NewIdent("__gppThrow"), Args: []ast.Expr{lowered}}}, nil
}

func lowerASTTryNode(statement *TryStmt, context constructorContext, rethrowName string) (ast.Stmt, error) {
	if statement == nil {
		return nil, fmt.Errorf("try statement is nil")
	}
	if err := validateASTCatchClauses(statement, context); err != nil {
		return nil, err
	}
	if lowered, handled, err := lowerASTTryDirectNode(statement, context, rethrowName); handled {
		return lowered, err
	}
	// The compatibility path remains node-first for constructs that cannot use
	// the stricter direct subset but are still representable by Go AST nodes.
	lowered, err := lowerASTTryCompatibilityNode(statement, context, rethrowName)
	if err != nil {
		return nil, err
	}
	return lowered, nil
}

func formatExceptionASTNode(node ast.Node) (string, error) {
	if node == nil {
		return "", fmt.Errorf("exception lowering produced a nil Go AST node")
	}
	var output bytes.Buffer
	if err := format.Node(&output, token.NewFileSet(), node); err != nil {
		return "", err
	}
	return output.String(), nil
}

func validateASTCatchClauses(statement *TryStmt, context constructorContext) error {
	if statement == nil {
		return fmt.Errorf("try statement is nil")
	}
	for _, clause := range statement.Catches {
		types := clause.Types
		if clause.Value != "" {
			if isCatchTypeName(clause.Value, context) {
				types = []TypeNode{parseTypeText(clause.Value)}
			} else if isCatchErrorValue(clause.Value, context) {
				continue
			} else {
				return fmt.Errorf("invalid catch error value %s", clause.Value)
			}
		}
		if len(types) == 0 && clause.Binding != "" && isCatchTypeName(clause.Binding, context) {
			types = []TypeNode{parseTypeText(clause.Binding)}
		}
		if len(types) == 0 {
			types = []TypeNode{parseTypeText("error")}
		}
		seen := map[string]bool{}
		for _, typeNode := range types {
			name, err := typeNodeSource(typeNode)
			if err != nil {
				return err
			}
			name = strings.TrimSpace(name)
			if !isCatchTypeName(name, context) {
				return fmt.Errorf("invalid catch type %s", name)
			}
			if seen[name] {
				return fmt.Errorf("duplicate catch type %s", name)
			}
			seen[name] = true
		}
		if clause.Binding != "" && (!isIdentifier(clause.Binding) || isGoKeyword(clause.Binding) || seen[clause.Binding]) {
			return fmt.Errorf("invalid catch variable %s", clause.Binding)
		}
	}
	return nil
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

func isCatchErrorValue(name string, context constructorContext) bool {
	parts := strings.Split(strings.TrimSpace(name), ".")
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
	variable, ok := pkg.Scope().Lookup(parts[1]).(*types.Var)
	if !ok {
		return false
	}
	errorType := types.Universe.Lookup("error").Type()
	return types.AssignableTo(types.Unalias(variable.Type()), errorType)
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

// lowerASTTryDirect handles the ordinary Go-compatible subset without ever
// materializing a body as source.
func lowerASTTryDirectNode(statement *TryStmt, context constructorContext, rethrowName string) (ast.Stmt, bool, error) {
	// Keep this path conservative: promotion and expression lowering must finish
	// before the ordinary Go AST is constructed, while finally/return handling
	// remains owned by the exception wrapper below.
	if statement == nil {
		return nil, false, nil
	}
	if !directEmissionAllowed(context) {
		return nil, false, nil
	}
	if err := lowerExceptionPromotions(statement.Body, context); err != nil {
		return nil, false, nil
	}
	if err := lowerPolymorphismBlockNode(statement.Body, context); err != nil {
		return nil, false, nil
	}
	for index := range statement.Catches {
		if err := lowerExceptionPromotions(statement.Catches[index].Body, context); err != nil {
			return nil, false, nil
		}
		if err := lowerPolymorphismBlockNode(statement.Catches[index].Body, context); err != nil {
			return nil, false, nil
		}
	}
	if err := lowerExceptionPromotions(statement.Finally, context); err != nil {
		return nil, false, nil
	}
	if err := lowerPolymorphismBlockNode(statement.Finally, context); err != nil {
		return nil, false, nil
	}
	if !exceptionDirectSafe(statement, context) {
		return nil, false, nil
	}
	tryBlock, err := lowerExceptionGoBlockNode(statement.Body, context, rethrowName)
	if err != nil {
		return nil, false, nil
	}
	clauses := make([]catchClause, 0, len(statement.Catches))
	for _, parsed := range statement.Catches {
		if parsed.Value != "" {
			return nil, false, nil
		}
		types := append([]TypeNode(nil), parsed.Types...)
		variable := parsed.Binding
		if len(types) == 0 && variable != "" && isCatchTypeName(variable, context) {
			types = append(types, parseTypeText(variable))
			variable = ""
		}
		if len(types) == 0 {
			types = []TypeNode{parseTypeText("error")}
		}
		for _, typeNode := range types {
			name, typeErr := typeNodeSource(typeNode)
			if typeErr != nil || !isCatchTypeName(strings.TrimSpace(name), context) {
				return nil, false, nil
			}
		}
		body, bodyErr := lowerExceptionGoBlockNode(parsed.Body, context, "__gppRecovered")
		if bodyErr != nil {
			return nil, false, nil
		}
		clauses = append(clauses, catchClause{typeNodes: types, variable: variable, body: body})
	}
	finallyBlock := &ast.BlockStmt{}
	hasFinally := statement.Finally != nil
	if hasFinally {
		finallyBlock, err = lowerExceptionGoBlockNode(statement.Finally, context, rethrowName)
		if err != nil {
			return nil, false, nil
		}
		if transferErr := validateFinallyControlTransfersBlock(statement.Finally); transferErr != nil {
			return nil, true, transferErr
		}
	}
	if err := validateCatchOrdering(clauses, context); err != nil {
		return nil, true, err
	}
	lowered, err := lowerTryASTNode(tryBlock, clauses, finallyBlock, hasFinally)
	return lowered, true, err
}

// lowerASTTryCompatibilityNode is the node-first fallback for exception
// lowering.  It intentionally does not require directEmissionAllowed: the
// surrounding function may still need one of the older source-rewrite
// lowerers, while the try/catch tree itself can be represented entirely by Go
// AST nodes.
func lowerASTTryCompatibilityNode(statement *TryStmt, context constructorContext, rethrowName string) (ast.Stmt, error) {
	if statement == nil {
		return nil, fmt.Errorf("try statement is nil")
	}
	if err := lowerExceptionPromotions(statement.Body, context); err != nil {
		return nil, err
	}
	if err := lowerPolymorphismBlockNode(statement.Body, context); err != nil {
		return nil, err
	}
	for index := range statement.Catches {
		if err := lowerExceptionPromotions(statement.Catches[index].Body, context); err != nil {
			return nil, err
		}
		if err := lowerPolymorphismBlockNode(statement.Catches[index].Body, context); err != nil {
			return nil, err
		}
	}
	if err := lowerExceptionPromotions(statement.Finally, context); err != nil {
		return nil, err
	}
	if err := lowerPolymorphismBlockNode(statement.Finally, context); err != nil {
		return nil, err
	}
	if err := lowerExceptionBlockNodes(statement.Body, context); err != nil {
		return nil, err
	}
	for index := range statement.Catches {
		if err := lowerExceptionBlockNodes(statement.Catches[index].Body, context); err != nil {
			return nil, err
		}
	}
	if err := lowerExceptionBlockNodes(statement.Finally, context); err != nil {
		return nil, err
	}

	tryBlock, err := lowerExceptionGoBlockNode(statement.Body, context, rethrowName)
	if err != nil {
		return nil, err
	}
	clauses, err := exceptionCatchClauses(statement, context)
	if err != nil {
		return nil, err
	}
	finallyBlock := &ast.BlockStmt{}
	hasFinally := statement.Finally != nil
	if hasFinally {
		finallyBlock, err = lowerExceptionGoBlockNode(statement.Finally, context, rethrowName)
		if err != nil {
			return nil, err
		}
		if transferErr := validateFinallyControlTransfersBlock(statement.Finally); transferErr != nil {
			return nil, transferErr
		}
	}
	if err := validateCatchOrdering(clauses, context); err != nil {
		return nil, err
	}
	return lowerTryASTNode(tryBlock, clauses, finallyBlock, hasFinally)
}

func exceptionCatchClauses(statement *TryStmt, context constructorContext) ([]catchClause, error) {
	if statement == nil {
		return nil, fmt.Errorf("try statement is nil")
	}
	clauses := make([]catchClause, 0, len(statement.Catches))
	for _, parsed := range statement.Catches {
		types := append([]TypeNode(nil), parsed.Types...)
		variable := parsed.Binding
		value := ""
		if parsed.Value != "" {
			if isCatchTypeName(parsed.Value, context) {
				types = append(types, parseTypeText(parsed.Value))
			} else if isCatchErrorValue(parsed.Value, context) {
				value = parsed.Value
			} else {
				return nil, fmt.Errorf("invalid catch error value %s", parsed.Value)
			}
		}
		if len(types) == 0 && variable != "" && isCatchTypeName(variable, context) {
			types = append(types, parseTypeText(variable))
			variable = ""
		}
		if len(types) == 0 && value == "" {
			types = []TypeNode{parseTypeText("error")}
		}
		for _, typeNode := range types {
			name, typeErr := typeNodeSource(typeNode)
			if typeErr != nil || !isCatchTypeName(strings.TrimSpace(name), context) {
				if typeErr != nil {
					return nil, typeErr
				}
				return nil, fmt.Errorf("invalid catch type %s", strings.TrimSpace(name))
			}
		}
		if variable != "" && (!isIdentifier(variable) || isGoKeyword(variable)) {
			return nil, fmt.Errorf("invalid catch variable %s", variable)
		}
		body, bodyErr := lowerExceptionGoBlockNode(parsed.Body, context, "__gppRecovered")
		if bodyErr != nil {
			return nil, bodyErr
		}
		errorAlias := ""
		if value != "" {
			errorAlias = "errors"
			for alias, importPath := range context.AvailableImports {
				if importPath == "errors" {
					errorAlias = alias
					break
				}
			}
		}
		clauses = append(clauses, catchClause{typeNodes: types, value: value, errorAlias: errorAlias, variable: variable, body: body})
	}
	return clauses, nil
}

func lowerExceptionPromotions(block *BlockStmt, context constructorContext) error {
	if block == nil {
		return nil
	}
	resultCount := exceptionContextResultCount(context)
	for _, statement := range block.Statements {
		if err := lowerExceptionPromotionStatement(statement, context, resultCount); err != nil {
			return err
		}
	}
	return nil
}

// lowerNestedExceptionPromotionExpr handles calls that occur inside another
// expression, such as `CompileRegex().MatchString(value)` or an if condition.
// The original statement-level pass only considered a call when it was the
// entire initializer/assignment/return expression, which left multi-result
// calls in selectors untouched and produced Go's "multiple-value ... in
// single-value context" error.
func lowerNestedExceptionPromotionExpr(expression ExprNode, context constructorContext) (ExprNode, error) {
	if expression == nil {
		return nil, nil
	}
	lower := func(value ExprNode) (ExprNode, error) {
		return lowerNestedExceptionPromotionExpr(value, context)
	}
	switch value := expression.(type) {
	case *CallExpr:
		if err := validateCallArgumentStyle(value); err != nil {
			return nil, err
		}
		if promotionASTAlreadyWrapped(value) {
			return value, nil
		}
		var err error
		value.Callee, err = lower(value.Callee)
		if err != nil {
			return nil, err
		}
		for index := range value.Arguments {
			value.Arguments[index].Value, err = lower(value.Arguments[index].Value)
			if err != nil {
				return nil, err
			}
		}
		return promoteExceptionCallNode(value, context, false)
	case *UnaryExpr:
		var err error
		value.Operand, err = lower(value.Operand)
		return value, err
	case *BinaryExpr:
		var err error
		value.Left, err = lower(value.Left)
		if err != nil {
			return nil, err
		}
		value.Right, err = lower(value.Right)
		return value, err
	case *AssignmentExpr:
		for index := range value.Left {
			var err error
			value.Left[index], err = lower(value.Left[index])
			if err != nil {
				return nil, err
			}
		}
		for index := range value.Right {
			var err error
			if call, ok := value.Right[index].(*CallExpr); ok {
				if result, found := promotedCallForExpr(call, context, context.CurrentParameterTypes); found &&
					result.trailingError && len(value.Left) == len(result.types) {
					value.Right[index], err = lowerExceptionPromotionCallChildren(call, context)
				} else {
					value.Right[index], err = lower(value.Right[index])
				}
			} else {
				value.Right[index], err = lower(value.Right[index])
			}
			if err != nil {
				return nil, err
			}
		}
	case *SelectorExpr:
		var err error
		value.Receiver, err = lower(value.Receiver)
		return value, err
	case *IndexExpr:
		var err error
		value.Receiver, err = lower(value.Receiver)
		if err != nil {
			return nil, err
		}
		value.Index, err = lower(value.Index)
		return value, err
	case *IndexListExpr:
		var err error
		value.Receiver, err = lower(value.Receiver)
		if err != nil {
			return nil, err
		}
		for index := range value.Indices {
			value.Indices[index], err = lower(value.Indices[index])
			if err != nil {
				return nil, err
			}
		}
	case *SliceExpr:
		var err error
		value.Receiver, err = lower(value.Receiver)
		if err != nil {
			return nil, err
		}
		value.Low, err = lower(value.Low)
		if err != nil {
			return nil, err
		}
		value.High, err = lower(value.High)
		if err != nil {
			return nil, err
		}
		value.Max, err = lower(value.Max)
		if err != nil {
			return nil, err
		}
	case *TypeAssertExpr:
		var err error
		value.Expression, err = lower(value.Expression)
		return value, err
	case *PostfixExpr:
		var err error
		value.Expression, err = lower(value.Expression)
		return value, err
	case *SpreadExpr:
		var err error
		value.Expression, err = lower(value.Expression)
		return value, err
	case *SendExpr:
		var err error
		value.Channel, err = lower(value.Channel)
		if err != nil {
			return nil, err
		}
		value.Value, err = lower(value.Value)
		return value, err
	case *ParenthesizedExpr:
		var err error
		value.Inner, err = lower(value.Inner)
		return value, err
	case *CompositeLiteralExpr:
		for index := range value.Elements {
			var err error
			value.Elements[index].Key, err = lower(value.Elements[index].Key)
			if err != nil {
				return nil, err
			}
			value.Elements[index].Value, err = lower(value.Elements[index].Value)
			if err != nil {
				return nil, err
			}
		}
	case *InterpolatedStringExpr:
		for index := range value.Segments {
			var err error
			value.Segments[index].Expression, err = lower(value.Segments[index].Expression)
			if err != nil {
				return nil, err
			}
		}
	case *LambdaExpr:
		var err error
		value.Body, err = lower(value.Body)
		if err != nil {
			return nil, err
		}
		if err := lowerExceptionPromotions(value.BlockBody, context); err != nil {
			return nil, err
		}
	case *FunctionLiteralExpr:
		if err := lowerExceptionPromotions(value.Body, context); err != nil {
			return nil, err
		}
	}
	return expression, nil
}

func lowerExceptionPromotionCallChildren(call *CallExpr, context constructorContext) (*CallExpr, error) {
	if call == nil {
		return nil, nil
	}
	loweredCallee, err := lowerNestedExceptionPromotionExpr(call.Callee, context)
	if err != nil {
		return nil, err
	}
	call.Callee = loweredCallee
	for index := range call.Arguments {
		call.Arguments[index].Value, err = lowerNestedExceptionPromotionExpr(call.Arguments[index].Value, context)
		if err != nil {
			return nil, err
		}
	}
	return call, nil
}

func promoteExceptionCallNode(call *CallExpr, context constructorContext, discard bool) (ExprNode, error) {
	if call == nil || promotionASTAlreadyWrapped(call) {
		return call, nil
	}
	result, found := promotedCallForExpr(call, context, context.CurrentParameterTypes)
	if !found || !result.trailingError || len(result.types) == 0 {
		return call, nil
	}
	nonErrorCount := len(result.types) - 1
	// A call returning only error is already a valid value expression. It is
	// propagated by an enclosing statement when appropriate, but it must not be
	// rewritten as a value unwrap while lowering a throw operand or return.
	if nonErrorCount == 0 && !discard {
		return call, nil
	}
	name := ""
	if discard {
		switch nonErrorCount {
		case 0:
			name = "__gppThrow"
		case 1:
			name = "__gppDiscard"
		case 2:
			name = "__gppDiscard2"
		case 3:
			name = "__gppDiscard3"
		}
	} else {
		switch nonErrorCount {
		case 1:
			name = "__gppUnwrap"
		case 2:
			name = "__gppUnwrap2"
		case 3:
			name = "__gppUnwrap3"
		}
	}
	if name == "" {
		return call, fmt.Errorf("direct exception lowering does not support %d-result error promotion", len(result.types))
	}
	return &CallExpr{Callee: &NameExpr{Name: name}, Arguments: []CallArg{{Value: call}}}, nil
}

func lowerExceptionPromotionStatement(statement Stmt, context constructorContext, functionResultCount int) error {
	if statement == nil {
		return nil
	}
	wrap := func(expression ExprNode, discard bool) (ExprNode, error) {
		call, ok := expression.(*CallExpr)
		if !ok {
			return lowerNestedExceptionPromotionExpr(expression, context)
		}
		if promotionASTAlreadyWrapped(call) {
			return call, nil
		}
		// Promote calls nested in the callee or arguments before deciding
		// whether the outer call itself returns an error. This matters for
		// selectors such as `compile().MatchString(...)`: the outer call is
		// single-result, but its receiver still needs an unwrap.
		var err error
		call.Callee, err = lowerNestedExceptionPromotionExpr(call.Callee, context)
		if err != nil {
			return nil, err
		}
		for index := range call.Arguments {
			call.Arguments[index].Value, err = lowerNestedExceptionPromotionExpr(call.Arguments[index].Value, context)
			if err != nil {
				return nil, err
			}
		}
		return promoteExceptionCallNode(call, context, discard)
	}
	switch value := statement.(type) {
	case *ExpressionStmt:
		lowered, err := wrap(value.Expression, true)
		if err != nil {
			return err
		}
		value.Expression = lowered
	case *DeclarationStmt:
		if len(value.Values) == 1 {
			if call, ok := value.Values[0].(*CallExpr); ok {
				if result, found := promotedCallForExpr(call, context, context.CurrentParameterTypes); found && result.trailingError && len(value.Names) == len(result.types)-1 {
					lowered, err := wrap(call, false)
					if err != nil {
						return err
					}
					value.Values[0] = lowered
				}
			}
		} else {
			for index := range value.Values {
				lowered, err := lowerNestedExceptionPromotionExpr(value.Values[index], context)
				if err != nil {
					return err
				}
				value.Values[index] = lowered
			}
		}
	case *AssignmentStmt:
		if len(value.Right) == 1 {
			if call, ok := value.Right[0].(*CallExpr); ok {
				if result, found := promotedCallForExpr(call, context, context.CurrentParameterTypes); found && result.trailingError && len(value.Left) == len(result.types)-1 {
					lowered, err := wrap(call, false)
					if err != nil {
						return err
					}
					value.Right[0] = lowered
				}
			}
		} else {
			for index := range value.Right {
				lowered, err := lowerNestedExceptionPromotionExpr(value.Right[index], context)
				if err != nil {
					return err
				}
				value.Right[index] = lowered
			}
		}
	case *ReturnStmt:
		if len(value.Values) == 1 {
			if call, ok := value.Values[0].(*CallExpr); ok {
				if result, found := promotedCallForExpr(call, context, context.CurrentParameterTypes); found && result.trailingError && functionResultCount != len(result.types) && len(value.Values) == len(result.types)-1 {
					lowered, err := wrap(call, false)
					if err != nil {
						return err
					}
					value.Values[0] = lowered
				}
			}
		} else {
			for index := range value.Values {
				lowered, err := lowerNestedExceptionPromotionExpr(value.Values[index], context)
				if err != nil {
					return err
				}
				value.Values[index] = lowered
			}
		}
	case *IfStmt:
		var err error
		value.Init, err = lowerNestedExceptionPromotionExpr(value.Init, context)
		if err != nil {
			return err
		}
		value.Condition, err = lowerNestedExceptionPromotionExpr(value.Condition, context)
		if err != nil {
			return err
		}
		if err := lowerExceptionPromotions(value.Body, context); err != nil {
			return err
		}
		if err := lowerExceptionPromotions(value.Else, context); err != nil {
			return err
		}
		if value.ElseIf != nil {
			return lowerExceptionPromotionStatement(value.ElseIf, context, functionResultCount)
		}
	case *ForStmt:
		var err error
		value.Init, err = lowerNestedExceptionPromotionExpr(value.Init, context)
		if err != nil {
			return err
		}
		value.Condition, err = lowerNestedExceptionPromotionExpr(value.Condition, context)
		if err != nil {
			return err
		}
		value.Post, err = lowerNestedExceptionPromotionExpr(value.Post, context)
		if err != nil {
			return err
		}
		value.RangeExpr, err = lowerNestedExceptionPromotionExpr(value.RangeExpr, context)
		if err != nil {
			return err
		}
		return lowerExceptionPromotions(value.Body, context)
	case *SwitchStmt:
		var err error
		value.Init, err = lowerNestedExceptionPromotionExpr(value.Init, context)
		if err != nil {
			return err
		}
		value.Tag, err = lowerNestedExceptionPromotionExpr(value.Tag, context)
		if err != nil {
			return err
		}
		return lowerExceptionPromotions(value.Body, context)
	case *CaseStmt:
		for index := range value.Clause.Expressions {
			lowered, err := lowerNestedExceptionPromotionExpr(value.Clause.Expressions[index], context)
			if err != nil {
				return err
			}
			value.Clause.Expressions[index] = lowered
		}
		return lowerExceptionPromotions(value.Clause.Body, context)
	case *BlockStmt:
		return lowerExceptionPromotions(value, context)
	case *TryStmt:
		if err := lowerExceptionPromotions(value.Body, context); err != nil {
			return err
		}
		for _, clause := range value.Catches {
			if err := lowerExceptionPromotions(clause.Body, context); err != nil {
				return err
			}
		}
		return lowerExceptionPromotions(value.Finally, context)
	case *GoStmt:
		if call, ok := value.Expression.(*CallExpr); ok {
			if result, found := promotedCallForExpr(call, context, context.CurrentParameterTypes); found && result.trailingError {
				return fmt.Errorf("cannot implicitly propagate error from goroutine call")
			}
		}
	case *ThrowStmt:
		lowered, err := lowerNestedExceptionPromotionExpr(value.Value, context)
		if err != nil {
			return err
		}
		value.Value = lowered
	case *DeferStmt:
		lowered, err := lowerNestedExceptionPromotionExpr(value.Expression, context)
		if err != nil {
			return err
		}
		value.Expression = lowered
	case *SendStmt:
		var err error
		value.Channel, err = lowerNestedExceptionPromotionExpr(value.Channel, context)
		if err != nil {
			return err
		}
		value.Value, err = lowerNestedExceptionPromotionExpr(value.Value, context)
		if err != nil {
			return err
		}
	case *IncDecStmt:
		lowered, err := lowerNestedExceptionPromotionExpr(value.Expression, context)
		if err != nil {
			return err
		}
		value.Expression = lowered
	}
	return nil
}

// lowerExceptionGoBlockNode is the structural exception-body lowering path.
// It is separate from goExceptionBlockNode because nested try statements need
// the semantic context in order to recursively produce their wrapper AST.
func lowerExceptionGoBlockNode(block *BlockStmt, context constructorContext, rethrowName string) (*ast.BlockStmt, error) {
	if block == nil {
		return &ast.BlockStmt{}, nil
	}
	result := &ast.BlockStmt{}
	for _, statement := range block.Statements {
		lowered, err := lowerExceptionGoStmtNode(statement, context, rethrowName)
		if err != nil {
			return nil, err
		}
		result.List = append(result.List, lowered...)
	}
	return result, nil
}

// lowerFunctionGoBlockNode lowers a complete function body while preserving
// ordinary function returns. A try statement owns its nested return semantics
// and is lowered through lowerASTTryDirectNode; returns outside try remain
// ordinary Go return statements.
func lowerFunctionGoBlockNode(block *BlockStmt, context constructorContext, rethrowName string) (*ast.BlockStmt, error) {
	if block == nil {
		return &ast.BlockStmt{}, nil
	}
	result := &ast.BlockStmt{}
	for _, statement := range block.Statements {
		lowered, err := lowerFunctionGoStmtNode(statement, context, rethrowName)
		if err != nil {
			return nil, err
		}
		result.List = append(result.List, lowered...)
	}
	return result, nil
}

func lowerFunctionGoStmtNode(statement Stmt, context constructorContext, rethrowName string) ([]ast.Stmt, error) {
	if statement == nil {
		return nil, nil
	}
	lowerExpression := func(expression ExprNode) (ExprNode, error) {
		return lowerExceptionExprNode(expression, context)
	}
	switch value := statement.(type) {
	case *TryStmt:
		lowered, handled, err := lowerASTTryDirectNode(value, context, rethrowName)
		if err != nil {
			return nil, err
		}
		if !handled {
			lowered, err = lowerASTTryCompatibilityNode(value, context, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		return []ast.Stmt{lowered}, nil
	case *IfStmt:
		init, err := lowerExpression(value.Init)
		if err != nil {
			return nil, err
		}
		condition, err := lowerExpression(value.Condition)
		if err != nil {
			return nil, err
		}
		body, err := lowerFunctionGoBlockNode(value.Body, context, rethrowName)
		if err != nil {
			return nil, err
		}
		result := &ast.IfStmt{Body: body}
		result.Cond, err = goExprNode(condition)
		if err != nil {
			return nil, err
		}
		if init != nil {
			result.Init, err = goSimpleStmt(init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Else != nil {
			result.Else, err = lowerFunctionGoBlockNode(value.Else, context, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.ElseIf != nil {
			nested, nestedErr := lowerFunctionGoStmtNode(value.ElseIf, context, rethrowName)
			if nestedErr != nil {
				return nil, nestedErr
			}
			if len(nested) != 1 {
				return nil, fmt.Errorf("unsupported else-if")
			}
			result.Else = nested[0]
		}
		return []ast.Stmt{result}, nil
	case *ForStmt:
		body, err := lowerFunctionGoBlockNode(value.Body, context, rethrowName)
		if err != nil {
			return nil, err
		}
		if value.RangeExpr != nil {
			keys := make([]ast.Expr, 0, len(value.RangeKey))
			for _, item := range value.RangeKey {
				lowered, itemErr := lowerExpression(item)
				if itemErr != nil {
					return nil, itemErr
				}
				key, itemErr := goExprNode(lowered)
				if itemErr != nil {
					return nil, itemErr
				}
				keys = append(keys, key)
			}
			rangeExpr, err := lowerExpression(value.RangeExpr)
			if err != nil {
				return nil, err
			}
			var key, item ast.Expr
			if len(keys) > 0 {
				key = keys[0]
			}
			if len(keys) > 1 {
				item = keys[1]
			}
			rangeAST, err := goExprNode(rangeExpr)
			if err != nil {
				return nil, err
			}
			rangeToken := token.DEFINE
			if value.RangeOperator == "=" {
				rangeToken = token.ASSIGN
			}
			return []ast.Stmt{&ast.RangeStmt{Key: key, Value: item, Tok: rangeToken, X: rangeAST, Body: body}}, nil
		}
		result := &ast.ForStmt{Body: body}
		if value.Init != nil {
			init, lowerErr := lowerExpression(value.Init)
			if lowerErr != nil {
				return nil, lowerErr
			}
			result.Init, err = goSimpleStmt(init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Condition != nil {
			condition, lowerErr := lowerExpression(value.Condition)
			if lowerErr != nil {
				return nil, lowerErr
			}
			result.Cond, err = goExprNode(condition)
			if err != nil {
				return nil, err
			}
		}
		if value.Post != nil {
			post, lowerErr := lowerExpression(value.Post)
			if lowerErr != nil {
				return nil, lowerErr
			}
			result.Post, err = goSimpleStmt(post, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		return []ast.Stmt{result}, nil
	case *SwitchStmt:
		if value.Select {
			selectStmt, selectErr := goSelectStmtFromBlock(value.Body, func(block *BlockStmt) (*ast.BlockStmt, error) {
				return lowerFunctionGoBlockNode(block, context, rethrowName)
			})
			if selectErr != nil {
				return nil, selectErr
			}
			return []ast.Stmt{selectStmt}, nil
		}
		if isTypeSwitchAssertion(value.Tag) || isTypeSwitchAssignment(value.Init) {
			body, err := lowerFunctionGoBlockNode(value.Body, context, rethrowName)
			if err != nil {
				return nil, err
			}
			var assignment ast.Stmt
			if value.Init != nil {
				init, lowerErr := lowerExpression(value.Init)
				if lowerErr != nil {
					return nil, lowerErr
				}
				assignment, err = goSimpleStmt(init, rethrowName)
			} else {
				tag, lowerErr := lowerExpression(value.Tag)
				if lowerErr != nil {
					return nil, lowerErr
				}
				var expression ast.Expr
				expression, err = goExprNode(tag)
				if err == nil {
					assignment = &ast.ExprStmt{X: expression}
				}
			}
			if err != nil {
				return nil, err
			}
			return []ast.Stmt{&ast.TypeSwitchStmt{Assign: assignment, Body: body}}, nil
		}
		body, err := lowerFunctionGoBlockNode(value.Body, context, rethrowName)
		if err != nil {
			return nil, err
		}
		result := &ast.SwitchStmt{Body: body}
		if value.Init != nil {
			init, lowerErr := lowerExpression(value.Init)
			if lowerErr != nil {
				return nil, lowerErr
			}
			result.Init, err = goSimpleStmt(init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Tag != nil {
			tag, lowerErr := lowerExpression(value.Tag)
			if lowerErr != nil {
				return nil, lowerErr
			}
			result.Tag, err = goExprNode(tag)
			if err != nil {
				return nil, err
			}
		}
		return []ast.Stmt{result}, nil
	case *CaseStmt:
		body, err := lowerFunctionGoBlockNode(value.Clause.Body, context, rethrowName)
		if err != nil {
			return nil, err
		}
		var expressions []ast.Expr
		if !value.Clause.Default {
			expressions = make([]ast.Expr, 0, len(value.Clause.Expressions))
			for _, expression := range value.Clause.Expressions {
				lowered, lowerErr := lowerExpression(expression)
				if lowerErr != nil {
					return nil, lowerErr
				}
				item, lowerErr := goExprNode(lowered)
				if lowerErr != nil {
					return nil, lowerErr
				}
				expressions = append(expressions, item)
			}
		}
		return []ast.Stmt{&ast.CaseClause{List: expressions, Body: body.List}}, nil
	case *BlockStmt:
		body, err := lowerFunctionGoBlockNode(value, context, rethrowName)
		if err != nil {
			return nil, err
		}
		return []ast.Stmt{body}, nil
	default:
		if err := lowerExceptionStmtNodes(statement, context); err != nil {
			return nil, err
		}
		return goStmtNode(statement, rethrowName)
	}
}

// lowerExceptionABIBoundaryNode wraps a lowered function body structurally.
// The wrapper catches the internal control-transfer panic used for returns
// from try blocks and converts thrown errors to a declared trailing error.
func lowerExceptionABIBoundaryNode(body *ast.BlockStmt, resultType TypeNode, resultFields []ParameterNode, context constructorContext) (*ast.BlockStmt, error) {
	fields, ok := exceptionResultFieldsForResult(resultType, resultFields)
	if !ok {
		result, _ := directTypeText(resultType)
		return nil, fmt.Errorf("invalid exception boundary result %q", result)
	}
	used := map[string]bool{}
	if body != nil {
		ast.Inspect(body, func(node ast.Node) bool {
			if identifier, ok := node.(*ast.Ident); ok {
				used[identifier.Name] = true
			}
			return true
		})
	}
	for index := range fields {
		transformed, ok := transformPolymorphicTypeNode(fields[index].typeNode, context)
		if !ok || transformed == nil {
			result, _ := directTypeText(fields[index].typeNode)
			return nil, fmt.Errorf("invalid exception boundary result type %q", result)
		}
		fields[index].typeNode = transformed
		if fields[index].name == "" || fields[index].name == "_" {
			fields[index].name = nextExceptionName(used, fmt.Sprintf("__gppBoundaryResult%d", index))
		} else {
			used[fields[index].name] = true
		}
	}
	if len(fields) == 0 {
		return lowerExceptionVoidBoundaryNode(body, used)
	}
	return lowerExceptionResultBoundaryNode(body, fields, used)
}

// transformPolymorphicTypeNode is the typed counterpart of
// transformPolymorphicType. It is deliberately limited to type forms whose
// semantics are known here; opaque token-preserving types remain on the
// compatibility path instead of being rendered and reparsed.
func transformPolymorphicTypeNode(typeNode TypeNode, context constructorContext) (TypeNode, bool) {
	switch value := typeNode.(type) {
	case nil:
		return nil, true
	case *NamedType:
		name := strings.Join(value.Parts, ".")
		target, ok := context.Targets[name]
		if !ok || !classHasDerived(target.Class, target.Classes) {
			return typeNode, true
		}
		interfaceName := dispatchInterfaceType(target)
		return &NamedType{Parts: strings.Split(interfaceName, ".")}, true
	case *PointerType:
		// Match transformPolymorphicType: pointer declarations already carry
		// their intended representation and are not replaced by an interface.
		return typeNode, true
	case *TupleType:
		elements := make([]TypeNode, len(value.Elements))
		for index, element := range value.Elements {
			transformed, ok := transformPolymorphicTypeNode(element, context)
			if !ok || transformed == nil {
				return nil, false
			}
			elements[index] = transformed
		}
		return &TupleType{Elements: elements, SpanValue: value.SpanValue}, true
	case *TokenType:
		return nil, false
	default:
		return typeNode, true
	}
}

func lowerExceptionVoidBoundaryNode(body *ast.BlockStmt, used map[string]bool) (*ast.BlockStmt, error) {
	returned := ast.NewIdent(nextExceptionName(used, "__gppBoundaryReturned"))
	recovered := ast.NewIdent(nextExceptionName(used, "__gppBoundaryRecovered"))
	isReturned := ast.NewIdent(nextExceptionName(used, "__gppIsReturned"))
	deferBody := &ast.BlockStmt{List: []ast.Stmt{
		&ast.AssignStmt{Lhs: []ast.Expr{recovered}, Tok: token.DEFINE, Rhs: []ast.Expr{call(ast.NewIdent("recover"))}},
		&ast.IfStmt{Cond: &ast.BinaryExpr{X: recovered, Op: token.NEQ, Y: ast.NewIdent("nil")}, Body: &ast.BlockStmt{List: []ast.Stmt{
			&ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("_"), isReturned}, Tok: token.DEFINE, Rhs: []ast.Expr{&ast.TypeAssertExpr{X: recovered, Type: ast.NewIdent("__gppExceptionReturn")}}},
			&ast.IfStmt{Cond: &ast.UnaryExpr{Op: token.NOT, X: isReturned}, Body: &ast.BlockStmt{List: []ast.Stmt{
				&ast.ExprStmt{X: call(ast.NewIdent("panic"), recovered)},
			}}},
			&ast.AssignStmt{Lhs: []ast.Expr{returned}, Tok: token.ASSIGN, Rhs: []ast.Expr{ast.NewIdent("true")}},
		}}},
	}}
	deferStatement := &ast.DeferStmt{Call: &ast.CallExpr{Fun: &ast.FuncLit{Type: &ast.FuncType{Params: &ast.FieldList{}}, Body: deferBody}}}
	functionBody := &ast.BlockStmt{List: append([]ast.Stmt{deferStatement}, body.List...)}
	run := &ast.ExprStmt{X: call(&ast.FuncLit{Type: &ast.FuncType{Params: &ast.FieldList{}}, Body: functionBody})}
	return &ast.BlockStmt{List: []ast.Stmt{
		&ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{returned}, Type: ast.NewIdent("bool")}}}},
		run,
		&ast.IfStmt{Cond: returned, Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{}}}},
	}}, nil
}

func lowerExceptionResultBoundaryNode(body *ast.BlockStmt, fields []exceptionResultField, used map[string]bool) (*ast.BlockStmt, error) {
	resultFields := &ast.FieldList{}
	fieldTypes := make([]ast.Expr, len(fields))
	for index, field := range fields {
		typeExpr, err := goTypeExpr(field.typeNode)
		if err != nil {
			return nil, err
		}
		fieldTypes[index] = typeExpr
		resultFields.List = append(resultFields.List, &ast.Field{Names: []*ast.Ident{ast.NewIdent(field.name)}, Type: typeExpr})
	}
	recovered := ast.NewIdent(nextExceptionName(used, "__gppBoundaryRecovered"))
	deferStatements := []ast.Stmt{
		&ast.AssignStmt{Lhs: []ast.Expr{recovered}, Tok: token.DEFINE, Rhs: []ast.Expr{call(ast.NewIdent("recover"))}},
	}
	returned := ast.NewIdent(nextExceptionName(used, "__gppReturned"))
	isReturned := ast.NewIdent(nextExceptionName(used, "__gppIsReturned"))
	returnBody := []ast.Stmt{
		&ast.IfStmt{Cond: &ast.BinaryExpr{X: call(ast.NewIdent("len"), selector(returned, "values")), Op: token.EQL, Y: &ast.BasicLit{Kind: token.INT, Value: "0"}}, Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{}}}},
	}
	for index, field := range fields {
		value := &ast.IndexExpr{X: selector(returned, "values"), Index: &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", index)}}
		assignment := &ast.AssignStmt{
			Lhs: []ast.Expr{ast.NewIdent(field.name)}, Tok: token.ASSIGN,
			Rhs: []ast.Expr{&ast.TypeAssertExpr{X: value, Type: fieldTypes[index]}},
		}
		if exceptionResultFieldType(field) == "error" {
			returnBody = append(returnBody, &ast.IfStmt{
				Cond: &ast.BinaryExpr{X: value, Op: token.EQL, Y: ast.NewIdent("nil")},
				Body: &ast.BlockStmt{List: []ast.Stmt{&ast.AssignStmt{
					Lhs: []ast.Expr{ast.NewIdent(field.name)}, Tok: token.ASSIGN, Rhs: []ast.Expr{ast.NewIdent("nil")},
				}}},
				Else: &ast.BlockStmt{List: []ast.Stmt{assignment}},
			})
		} else {
			returnBody = append(returnBody, assignment)
		}
	}
	returnBody = append(returnBody, &ast.ReturnStmt{})
	isErrorBoundary := exceptionResultFieldType(fields[len(fields)-1]) == "error"
	if isErrorBoundary {
		thrown := ast.NewIdent(nextExceptionName(used, "__gppBoundaryThrown"))
		isThrown := ast.NewIdent(nextExceptionName(used, "__gppBoundaryIsThrown"))
		thrownBody := []ast.Stmt{
			&ast.AssignStmt{Lhs: []ast.Expr{thrown, isThrown}, Tok: token.DEFINE, Rhs: []ast.Expr{&ast.TypeAssertExpr{X: recovered, Type: ast.NewIdent("__gppThrownError")}}},
			&ast.IfStmt{Cond: &ast.UnaryExpr{Op: token.NOT, X: isThrown}, Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: call(ast.NewIdent("panic"), recovered)}}}},
		}
		for index, field := range fields[:len(fields)-1] {
			zero := ast.NewIdent(nextExceptionName(used, fmt.Sprintf("__gppBoundaryZero%d", index)))
			thrownBody = append(thrownBody,
				&ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{zero}, Type: fieldTypes[index]}}}},
				&ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(field.name)}, Tok: token.ASSIGN, Rhs: []ast.Expr{zero}},
			)
		}
		thrownBody = append(thrownBody, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(fields[len(fields)-1].name)}, Tok: token.ASSIGN, Rhs: []ast.Expr{selector(thrown, "err")}})
		deferStatements = append(deferStatements, &ast.IfStmt{Cond: &ast.BinaryExpr{X: recovered, Op: token.NEQ, Y: ast.NewIdent("nil")}, Body: &ast.BlockStmt{List: append([]ast.Stmt{
			&ast.AssignStmt{Lhs: []ast.Expr{returned, isReturned}, Tok: token.DEFINE, Rhs: []ast.Expr{&ast.TypeAssertExpr{X: recovered, Type: ast.NewIdent("__gppExceptionReturn")}}},
			&ast.IfStmt{Cond: isReturned, Body: &ast.BlockStmt{List: returnBody}},
		}, thrownBody...)}})
	} else {
		deferStatements = append(deferStatements, &ast.IfStmt{Cond: &ast.BinaryExpr{X: recovered, Op: token.NEQ, Y: ast.NewIdent("nil")}, Body: &ast.BlockStmt{List: []ast.Stmt{
			&ast.AssignStmt{Lhs: []ast.Expr{returned, isReturned}, Tok: token.DEFINE, Rhs: []ast.Expr{&ast.TypeAssertExpr{X: recovered, Type: ast.NewIdent("__gppExceptionReturn")}}},
			&ast.IfStmt{Cond: &ast.UnaryExpr{Op: token.NOT, X: isReturned}, Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: call(ast.NewIdent("panic"), recovered)}}}},
			&ast.IfStmt{Cond: isReturned, Body: &ast.BlockStmt{List: returnBody}},
		}}})
	}
	deferStmt := &ast.DeferStmt{Call: &ast.CallExpr{Fun: &ast.FuncLit{Type: &ast.FuncType{Params: &ast.FieldList{}}, Body: &ast.BlockStmt{List: deferStatements}}}}
	closureBody := &ast.BlockStmt{List: append([]ast.Stmt{deferStmt}, append(body.List, &ast.ReturnStmt{})...)}
	closure := &ast.FuncLit{Type: &ast.FuncType{Params: &ast.FieldList{}, Results: resultFields}, Body: closureBody}
	return &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{&ast.CallExpr{Fun: closure}}}}}, nil
}

func lowerExceptionGoStmtNode(statement Stmt, context constructorContext, rethrowName string) ([]ast.Stmt, error) {
	if statement == nil {
		return nil, nil
	}
	switch value := statement.(type) {
	case *TryStmt:
		lowered, handled, err := lowerASTTryDirectNode(value, context, rethrowName)
		if err != nil {
			return nil, err
		}
		if !handled {
			compatibility, compatibilityErr := lowerASTTryCompatibilityNode(value, context, rethrowName)
			if compatibilityErr != nil {
				return nil, compatibilityErr
			}
			lowered = compatibility
		}
		return []ast.Stmt{lowered}, nil
	case *IfStmt:
		condition, err := goExprNode(value.Condition)
		if err != nil {
			return nil, err
		}
		body, err := lowerExceptionGoBlockNode(value.Body, context, rethrowName)
		if err != nil {
			return nil, err
		}
		result := &ast.IfStmt{Cond: condition, Body: body}
		if value.Else != nil {
			result.Else, err = lowerExceptionGoBlockNode(value.Else, context, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.ElseIf != nil {
			nested, nestedErr := lowerExceptionGoStmtNode(value.ElseIf, context, rethrowName)
			if nestedErr != nil || len(nested) != 1 {
				if nestedErr != nil {
					return nil, nestedErr
				}
				return nil, fmt.Errorf("unsupported else-if")
			}
			result.Else = nested[0]
		}
		return []ast.Stmt{result}, nil
	case *ForStmt:
		body, err := lowerExceptionGoBlockNode(value.Body, context, rethrowName)
		if err != nil {
			return nil, err
		}
		if value.RangeExpr != nil {
			key := make([]ast.Expr, 0, len(value.RangeKey))
			for _, item := range value.RangeKey {
				lowered, itemErr := lowerExceptionExprNode(item, context)
				if itemErr != nil {
					return nil, itemErr
				}
				keyExpr, itemErr := goExprNode(lowered)
				if itemErr != nil {
					return nil, itemErr
				}
				key = append(key, keyExpr)
			}
			rangeExpr, err := goExprNode(value.RangeExpr)
			if err != nil {
				return nil, err
			}
			var keyExpr, valueExpr ast.Expr
			if len(key) > 0 {
				keyExpr = key[0]
			}
			if len(key) > 1 {
				valueExpr = key[1]
			}
			rangeToken := token.DEFINE
			if value.RangeOperator == "=" {
				rangeToken = token.ASSIGN
			}
			return []ast.Stmt{&ast.RangeStmt{Key: keyExpr, Value: valueExpr, Tok: rangeToken, X: rangeExpr, Body: body}}, nil
		}
		result := &ast.ForStmt{Body: body}
		if value.Condition != nil {
			result.Cond, err = goExprNode(value.Condition)
			if err != nil {
				return nil, err
			}
		}
		if value.Init != nil {
			result.Init, err = goSimpleStmt(value.Init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Post != nil {
			result.Post, err = goSimpleStmt(value.Post, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		return []ast.Stmt{result}, nil
	case *SwitchStmt:
		if value.Select {
			selectStmt, selectErr := goSelectStmtFromBlock(value.Body, func(block *BlockStmt) (*ast.BlockStmt, error) {
				return lowerExceptionGoBlockNode(block, context, rethrowName)
			})
			if selectErr != nil {
				return nil, selectErr
			}
			return []ast.Stmt{selectStmt}, nil
		}
		if isTypeSwitchAssertion(value.Tag) || isTypeSwitchAssignment(value.Init) {
			body, err := lowerExceptionGoBlockNode(value.Body, context, rethrowName)
			if err != nil {
				return nil, err
			}
			var assignment ast.Stmt
			if value.Init != nil {
				init, lowerErr := lowerExceptionExprNode(value.Init, context)
				if lowerErr != nil {
					return nil, lowerErr
				}
				assignment, err = goSimpleStmt(init, rethrowName)
			} else {
				tag, lowerErr := lowerExceptionExprNode(value.Tag, context)
				if lowerErr != nil {
					return nil, lowerErr
				}
				var expression ast.Expr
				expression, err = goExprNode(tag)
				if err == nil {
					assignment = &ast.ExprStmt{X: expression}
				}
			}
			if err != nil {
				return nil, err
			}
			return []ast.Stmt{&ast.TypeSwitchStmt{Assign: assignment, Body: body}}, nil
		}
		body, err := lowerExceptionGoBlockNode(value.Body, context, rethrowName)
		if err != nil {
			return nil, err
		}
		result := &ast.SwitchStmt{Body: body}
		if value.Init != nil {
			result.Init, err = goSimpleStmt(value.Init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Tag != nil {
			result.Tag, err = goExprNode(value.Tag)
			if err != nil {
				return nil, err
			}
		}
		return []ast.Stmt{result}, nil
	case *CaseStmt:
		body, err := lowerExceptionGoBlockNode(value.Clause.Body, context, rethrowName)
		if err != nil {
			return nil, err
		}
		var expressions []ast.Expr
		for _, expression := range value.Clause.Expressions {
			lowered, expressionErr := goExprNode(expression)
			if expressionErr != nil {
				return nil, expressionErr
			}
			expressions = append(expressions, lowered)
		}
		return []ast.Stmt{&ast.CaseClause{List: expressions, Body: body.List}}, nil
	case *BlockStmt:
		body, err := lowerExceptionGoBlockNode(value, context, rethrowName)
		if err != nil {
			return nil, err
		}
		return []ast.Stmt{body}, nil
	default:
		if err := lowerExceptionStmtNodes(statement, context); err != nil {
			return nil, err
		}
		return goExceptionStmtNode(statement, rethrowName)
	}
}

func lowerExceptionBlockNodes(block *BlockStmt, context constructorContext) error {
	if block == nil {
		return nil
	}
	for _, statement := range block.Statements {
		if err := lowerExceptionStmtNodes(statement, context); err != nil {
			return err
		}
	}
	return nil
}

func lowerExceptionStmtNodes(statement Stmt, context constructorContext) error {
	if statement == nil {
		return nil
	}
	lower := func(expression ExprNode) (ExprNode, error) { return lowerExceptionExprNode(expression, context) }
	switch value := statement.(type) {
	case *ExpressionStmt:
		var err error
		value.Expression, err = lower(value.Expression)
		return err
	case *DeclarationStmt:
		for index := range value.Values {
			lowered, err := lower(value.Values[index])
			if err != nil {
				return err
			}
			value.Values[index] = lowered
		}
		return nil
	case *AssignmentStmt:
		for index := range value.Left {
			lowered, err := lower(value.Left[index])
			if err != nil {
				return err
			}
			value.Left[index] = lowered
		}
		for index := range value.Right {
			lowered, err := lower(value.Right[index])
			if err != nil {
				return err
			}
			value.Right[index] = lowered
		}
		return nil
	case *ReturnStmt:
		for index := range value.Values {
			lowered, err := lower(value.Values[index])
			if err != nil {
				return err
			}
			value.Values[index] = lowered
		}
		return nil
	case *ThrowStmt:
		var err error
		value.Value, err = lower(value.Value)
		return err
	case *DeferStmt:
		var err error
		value.Expression, err = lower(value.Expression)
		return err
	case *GoStmt:
		var err error
		value.Expression, err = lower(value.Expression)
		return err
	case *SendStmt:
		var err error
		value.Channel, err = lower(value.Channel)
		if err != nil {
			return err
		}
		value.Value, err = lower(value.Value)
		return err
	case *IncDecStmt:
		var err error
		value.Expression, err = lower(value.Expression)
		return err
	case *IfStmt:
		var err error
		value.Init, err = lower(value.Init)
		if err != nil {
			return err
		}
		value.Condition, err = lower(value.Condition)
		if err != nil {
			return err
		}
		if err = lowerExceptionBlockNodes(value.Body, context); err != nil {
			return err
		}
		if err = lowerExceptionBlockNodes(value.Else, context); err != nil {
			return err
		}
		if value.ElseIf != nil {
			return lowerExceptionStmtNodes(value.ElseIf, context)
		}
		return nil
	case *ForStmt:
		var err error
		value.Init, err = lower(value.Init)
		if err != nil {
			return err
		}
		value.Condition, err = lower(value.Condition)
		if err != nil {
			return err
		}
		value.Post, err = lower(value.Post)
		if err != nil {
			return err
		}
		if len(value.RangeKey) == 1 && value.RangeKey[0] != nil {
			kind := introspectionExpressionKindNode(value.RangeExpr, context.CurrentIntrospectionKinds, context, context.CurrentParameterTypes)
			enumElement := enumRangeElementTypeNode(value.RangeExpr, context, context.CurrentParameterTypes) != ""
			if kind == introspectionFields || kind == introspectionMethods || kind == introspectionParents || kind == introspectionParameters || enumElement {
				value.RangeKey = append([]ExprNode{&NameExpr{Name: "_"}}, value.RangeKey...)
			}
		}
		value.RangeExpr, err = lower(value.RangeExpr)
		if err != nil {
			return err
		}
		return lowerExceptionBlockNodes(value.Body, context)
	case *SwitchStmt:
		var err error
		value.Init, err = lower(value.Init)
		if err != nil {
			return err
		}
		value.Tag, err = lower(value.Tag)
		if err != nil {
			return err
		}
		return lowerExceptionBlockNodes(value.Body, context)
	case *CaseStmt:
		for index := range value.Clause.Expressions {
			lowered, err := lower(value.Clause.Expressions[index])
			if err != nil {
				return err
			}
			value.Clause.Expressions[index] = lowered
		}
		return lowerExceptionBlockNodes(value.Clause.Body, context)
	case *BlockStmt:
		return lowerExceptionBlockNodes(value, context)
	case *TryStmt:
		if err := lowerExceptionBlockNodes(value.Body, context); err != nil {
			return err
		}
		for _, clause := range value.Catches {
			if err := lowerExceptionBlockNodes(clause.Body, context); err != nil {
				return err
			}
		}
		return lowerExceptionBlockNodes(value.Finally, context)
	default:
		return nil
	}
}

func lowerExceptionExprNode(expression ExprNode, context constructorContext) (ExprNode, error) {
	if expression == nil {
		return nil, nil
	}
	if callExpression, ok := expression.(*CallExpr); ok {
		if err := validateCallArgumentStyle(callExpression); err != nil {
			return nil, err
		}
		if lowered, handled, err := lowerRecordCallExprNode(callExpression, context); handled {
			return lowered, err
		}
		if lowered, handled, err := constructorExprNode(callExpression, context); handled {
			return lowered, err
		}
		if lowered, handled, err := lowerOverloadCallNode(callExpression, context); handled {
			if err != nil {
				return nil, err
			}
			return lowerExceptionExprNode(lowered, context)
		}
	}
	switch value := expression.(type) {
	case *UnaryExpr:
		lowered, err := lowerExceptionExprNode(value.Operand, context)
		value.Operand = lowered
		return value, err
	case *BinaryExpr:
		if value.Operator == "??" {
			return lowerExceptionCoalesceExprNode(value, context)
		}
		left, err := lowerExceptionExprNode(value.Left, context)
		if err != nil {
			return nil, err
		}
		right, err := lowerExceptionExprNode(value.Right, context)
		value.Left, value.Right = left, right
		return value, err
	case *AssignmentExpr:
		for index := range value.Left {
			lowered, err := lowerExceptionExprNode(value.Left[index], context)
			if err != nil {
				return nil, err
			}
			value.Left[index] = lowered
		}
		for index := range value.Right {
			lowered, err := lowerExceptionExprNode(value.Right[index], context)
			if err != nil {
				return nil, err
			}
			value.Right[index] = lowered
		}
		return value, nil
	case *SelectorExpr:
		if value.Safe {
			return lowerSafeAccessExprNode(value, nil, context)
		}
		if lowered, handled, err := lowerEnumSelectorNode(value, context); handled {
			return lowered, err
		}
		if lowered, handled, err := lowerIntrospectionClassExprNode(value, context); handled {
			return lowered, err
		}
		lowered, err := lowerExceptionExprNode(value.Receiver, context)
		if err != nil {
			return nil, err
		}
		value.Receiver = lowered
		if introspection, handled, err := lowerIntrospectionMetadataSelectorNode(value, context); handled {
			return introspection, err
		}
		return value, err
	case *IndexExpr:
		left, err := lowerExceptionExprNode(value.Receiver, context)
		if err != nil {
			return nil, err
		}
		right, err := lowerExceptionExprNode(value.Index, context)
		value.Receiver, value.Index = left, right
		return value, err
	case *IndexListExpr:
		lowered, err := lowerExceptionExprNode(value.Receiver, context)
		if err != nil {
			return nil, err
		}
		value.Receiver = lowered
		for index := range value.Indices {
			value.Indices[index], err = lowerExceptionExprNode(value.Indices[index], context)
			if err != nil {
				return nil, err
			}
		}
		return value, nil
	case *CallExpr:
		if selector, ok := value.Callee.(*SelectorExpr); ok && selector.Safe {
			return lowerSafeAccessExprNode(selector, value, context)
		}
		if lowered, handled := lowerStaticTemplateCallNode(value, context); handled {
			return lowered, nil
		}
		if lowered, handled, err := lowerExceptionExtensionCallNode(value, context); handled {
			return lowered, err
		}
		if lowered, handled, err := lowerCallableCallNode(value, context); handled {
			if err != nil {
				return nil, err
			}
			return lowerExceptionExprNode(lowered, context)
		}
		lowered, err := lowerExceptionExprNode(value.Callee, context)
		if err != nil {
			return nil, err
		}
		value.Callee = lowered
		for index := range value.Arguments {
			if lambda, ok := value.Arguments[index].Value.(*LambdaExpr); ok {
				value.Arguments[index].Value, err = lowerLambdaExprNode(lambda, context, lambdaFunctionTypesForCall(value, index, context))
			} else {
				value.Arguments[index].Value, err = lowerExceptionExprNode(value.Arguments[index].Value, context)
			}
			if err != nil {
				return nil, err
			}
		}
		lowerIntrospectionCallArguments(value, context)
		return value, nil
	case *ParenthesizedExpr:
		lowered, err := lowerExceptionExprNode(value.Inner, context)
		value.Inner = lowered
		return value, err
	case *TypeAssertExpr:
		lowered, err := lowerExceptionExprNode(value.Expression, context)
		value.Expression = lowered
		return value, err
	case *InterpolatedStringExpr:
		return lowerInterpolatedStringExprNode(value, context)
	case *CompositeLiteralExpr:
		for index := range value.Elements {
			var err error
			value.Elements[index].Key, err = lowerExceptionExprNode(value.Elements[index].Key, context)
			if err != nil {
				return nil, err
			}
			value.Elements[index].Value, err = lowerExceptionExprNode(value.Elements[index].Value, context)
			if err != nil {
				return nil, err
			}
		}
		if err := lowerRecordCollectionTypeNode(value, context); err != nil {
			return nil, err
		}
		return value, nil
	case *FunctionLiteralExpr:
		if err := lowerExceptionBlockNodes(value.Body, context); err != nil {
			return nil, err
		}
		return value, nil
	case *LambdaExpr:
		return lowerLambdaExprNode(value, context, nil)
	default:
		return expression, nil
	}
}

func validateCallArgumentStyle(call *CallExpr) error {
	if call == nil {
		return nil
	}
	named := false
	positional := false
	for _, argument := range call.Arguments {
		if argument.Name == "" {
			positional = true
		} else {
			named = true
		}
	}
	if !named || !positional {
		return nil
	}
	err := fmt.Errorf("cannot mix named and positional arguments in call to %s", callableName(call))
	return sourceLineError(call.Span(), err)
}

// lowerRecordCollectionTypeNode resolves []record and map[K]record directly
// on the expression AST.  Collection literals are the one record form where
// the element shape is inferred from their children; leaving the TypeNode as
// `record` would let direct Go AST emission produce invalid Go source.
func lowerRecordCollectionTypeNode(literal *CompositeLiteralExpr, context constructorContext) error {
	if literal == nil || literal.Type == nil || context.Records == nil {
		return nil
	}
	var element *TypeNode
	switch collection := literal.Type.(type) {
	case *SliceType:
		element = &collection.Element
	case *MapType:
		element = &collection.Value
	default:
		return nil
	}
	named, ok := (*element).(*NamedType)
	if !ok || len(named.Parts) != 1 || named.Parts[0] != "record" {
		return nil
	}
	for _, item := range literal.Elements {
		candidate := inferRecordExprType(item.Value, context.RecordValueTypes, context)
		if !strings.HasPrefix(candidate, "__gpp_record_") {
			continue
		}
		if context.Records.recordByGoName(candidate) == nil {
			continue
		}
		*element = &NamedType{Parts: []string{candidate}}
		return nil
	}
	return fmt.Errorf("cannot infer record collection element type")
}

// lowerRecordCallExprNode lowers the Go++ record(name: value, ...) form to a
// generated structural composite literal.  Keeping this conversion on the
// expression AST means direct function/method emission does not need to render
// the call to source merely to infer the record shape.
func lowerRecordCallExprNode(call *CallExpr, context constructorContext) (ExprNode, bool, error) {
	if call == nil || context.Records == nil {
		return nil, false, nil
	}
	name, ok := call.Callee.(*NameExpr)
	if !ok || name.Name != "record" {
		return nil, false, nil
	}
	valueTypes := context.RecordValueTypes
	if valueTypes == nil {
		valueTypes = context.CurrentParameterTypes
	}
	fields := make([]recordFieldType, 0, len(call.Arguments))
	elements := make([]CompositeElement, 0, len(call.Arguments))
	seen := map[string]bool{}
	for index, argument := range call.Arguments {
		if argument.Name == "" || !isIdentifier(argument.Name) {
			return nil, true, fmt.Errorf("record literal argument %d must be named", index+1)
		}
		if seen[argument.Name] {
			return nil, true, fmt.Errorf("record literal repeats field %s", argument.Name)
		}
		seen[argument.Name] = true
		var value ExprNode
		var err error
		if context.RecordOnlyLowering {
			value, err = lowerRecordOnlyExpr(argument.Value, context)
		} else {
			value, err = lowerExceptionExprNode(argument.Value, context)
		}
		if err != nil {
			return nil, true, err
		}
		fieldType := strings.TrimSpace(inferRecordExprType(value, valueTypes, context))
		if fieldType == "" {
			return nil, true, fmt.Errorf("cannot infer type of record field %s", argument.Name)
		}
		fields = append(fields, recordFieldType{Name: argument.Name, Type: fieldType})
		elements = append(elements, CompositeElement{
			Key:   &NameExpr{Name: argument.Name},
			Value: value,
		})
	}
	shape := context.Records.register(fields)
	return &CompositeLiteralExpr{
		Type:     &NamedType{Parts: []string{shape.GoName}},
		Elements: elements,
	}, true, nil
}

// lowerExceptionExtensionCallNode resolves an extension call entirely on the
// expression tree. In particular, defaults, named arguments, variadics, and
// explicit type arguments must not be rendered to source and reparsed just to
// call the generated extension function.
func lowerExceptionExtensionCallNode(call *CallExpr, context constructorContext) (ExprNode, bool, error) {
	receiver, methodName, typeArguments, ok := extensionASTCallParts(call)
	if !ok || len(context.Extensions) == 0 {
		return nil, false, nil
	}
	actualType := extensionASTStaticType(receiver, context, context.CurrentParameterTypes)
	candidates := applicableExtensionASTs(methodName, actualType, call.Arguments, typeArguments, context.CurrentParameterTypes, context)
	if len(candidates) == 0 || realMethodAppliesAST(actualType, methodName, call.Arguments, context) {
		return nil, false, nil
	}
	if len(candidates) > 1 {
		return nil, true, fmt.Errorf("ambiguous extension method %s for %s", methodName, actualType)
	}
	candidate := candidates[0]
	parameters, err := parameterInfosForMethod(candidate.Method.Method)
	if err != nil {
		return nil, true, err
	}
	loweredReceiver, err := lowerExceptionExprNode(receiver, context)
	if err != nil {
		return nil, true, err
	}
	if extensionNeedsAddress(candidate.Method.ReceiverType, actualType) {
		loweredReceiver = &UnaryExpr{Operator: "&", Operand: loweredReceiver}
	}
	arguments := []CallArg{{Value: loweredReceiver}}
	methodArguments, resolved, resolveErr := lowerExtensionArgumentNodes(call, parameters, context)
	if resolveErr != nil {
		return nil, true, resolveErr
	}
	if !resolved {
		return nil, false, nil
	}
	arguments = append(arguments, methodArguments...)

	callee := ExprNode(&NameExpr{Name: candidate.Method.GoName})
	if candidate.Method.Qualifier != "" {
		parts := strings.Split(candidate.Method.Qualifier, ".")
		callee = &NameExpr{Name: parts[0]}
		for _, part := range parts[1:] {
			callee = &SelectorExpr{Receiver: callee, Name: part}
		}
		callee = &SelectorExpr{Receiver: callee, Name: candidate.Method.GoName}
	}
	if len(typeArguments) == 1 {
		callee = &IndexExpr{Receiver: callee, Index: &TypeExpr{Type: typeArguments[0]}}
	} else if len(typeArguments) > 1 {
		indices := make([]ExprNode, len(typeArguments))
		for index, typeArgument := range typeArguments {
			indices[index] = &TypeExpr{Type: typeArgument}
		}
		callee = &IndexListExpr{Receiver: callee, Indices: indices}
	}
	return &CallExpr{Callee: callee, Arguments: arguments}, true, nil
}

func lowerExtensionArgumentNodes(call *CallExpr, parameters []parameterInfo, context constructorContext) ([]CallArg, bool, error) {
	if call == nil {
		return nil, false, nil
	}
	args := call.Arguments
	if len(parameters) == 0 {
		return nil, len(args) == 0, nil
	}
	variadic := strings.HasPrefix(strings.TrimSpace(parameters[len(parameters)-1].typeText()), "...")
	fixedCount := len(parameters)
	if variadic {
		fixedCount--
	}
	hasNamed := false
	for _, argument := range args {
		if argument.Name != "" {
			hasNamed = true
			break
		}
	}
	if hasNamed {
		for _, argument := range args {
			if argument.Name == "" {
				return nil, true, sourceLineError(call.Span(), fmt.Errorf("cannot mix named and positional arguments in call to %s", callableName(call)))
			}
		}
	}

	values := make([]ExprNode, len(parameters))
	provided := make([]bool, len(parameters))
	if hasNamed {
		for _, argument := range args {
			index := -1
			for parameterIndex, parameter := range parameters {
				if parameter.Name == argument.Name {
					index = parameterIndex
					break
				}
			}
			if index < 0 {
				return nil, true, sourceLineError(call.Span(), fmt.Errorf("unknown named argument %s in call to %s", argument.Name, callableName(call)))
			}
			if provided[index] {
				return nil, true, sourceLineError(call.Span(), fmt.Errorf("named argument %s is repeated in call to %s", argument.Name, callableName(call)))
			}
			provided[index] = true
			values[index] = argument.Value
		}
	} else {
		if !variadic && len(args) > len(parameters) {
			return nil, false, nil
		}
		if variadic && len(args) < fixedCount {
			return nil, false, nil
		}
		for index, argument := range args {
			if index < fixedCount || !variadic {
				values[index] = argument.Value
				provided[index] = true
				continue
			}
			// Variadic arguments are appended below; keeping them in `args`
			// avoids pretending the parameter has one fixed value.
		}
	}
	for index := 0; index < fixedCount; index++ {
		if provided[index] {
			continue
		}
		if !parameters[index].HasDefault || parameters[index].DefaultAST == nil {
			return nil, false, nil
		}
		values[index] = parameters[index].DefaultAST
	}
	if !variadic {
		for index := fixedCount; index < len(parameters); index++ {
			if provided[index] {
				continue
			}
			if !parameters[index].HasDefault || parameters[index].DefaultAST == nil {
				return nil, false, nil
			}
			values[index] = parameters[index].DefaultAST
		}
	}

	result := make([]CallArg, 0, len(args)+len(parameters))
	lowerValue := func(expression ExprNode) (ExprNode, error) {
		return lowerExceptionExprNode(expression, context)
	}
	for index := 0; index < fixedCount; index++ {
		lowered, err := lowerValue(values[index])
		if err != nil {
			return nil, true, err
		}
		result = append(result, CallArg{Value: lowered})
	}
	if variadic {
		start := fixedCount
		if hasNamed {
			// A named variadic parameter is one slice argument, not a list of
			// individual values. It is valid only when supplied explicitly.
			if provided[fixedCount] {
				lowered, err := lowerValue(values[fixedCount])
				if err != nil {
					return nil, true, err
				}
				result = append(result, CallArg{Value: lowered})
			}
		} else {
			for _, argument := range args[start:] {
				lowered, err := lowerValue(argument.Value)
				if err != nil {
					return nil, true, err
				}
				result = append(result, CallArg{Value: lowered})
			}
		}
		return result, true, nil
	}
	for index := fixedCount; index < len(parameters); index++ {
		if !provided[index] {
			continue
		}
		lowered, err := lowerValue(values[index])
		if err != nil {
			return nil, true, err
		}
		result = append(result, CallArg{Value: lowered})
	}
	return result, true, nil
}

func lowerExceptionCoalesceExprNode(expression *BinaryExpr, context constructorContext) (ExprNode, error) {
	if expression == nil || expression.Operator != "??" {
		return expression, nil
	}
	return lowerCoalesceExpressionNode(expression, context, context.CurrentParameterTypes)
}

func exceptionDirectSafe(statement *TryStmt, context constructorContext) bool {
	if statement == nil || !exceptionDirectSafeBlock(statement.Body, context) {
		return false
	}
	for _, clause := range statement.Catches {
		if !exceptionDirectSafeBlock(clause.Body, context) {
			return false
		}
	}
	return statement.Finally == nil || exceptionDirectSafeBlock(statement.Finally, context)
}

func exceptionDirectSafeBlock(block *BlockStmt, context constructorContext) bool {
	if block == nil {
		return true
	}
	for _, statement := range block.Statements {
		switch value := statement.(type) {
		case *ThrowStmt:
			if !exceptionDirectSafeThrowExpr(value.Value, context) {
				return false
			}
			if validateThrowExpressionNode(value.Value, context) != nil {
				return false
			}
		case *ReturnStmt:
			for _, expression := range value.Values {
				if !exceptionDirectSafeExpr(expression, context) {
					return false
				}
			}
		case *BlockStmt:
			if !exceptionDirectSafeBlock(value, context) {
				return false
			}
		case *ExpressionStmt:
			if !exceptionDirectSafeExpr(value.Expression, context) {
				return false
			}
		case *DeclarationStmt:
			for _, expression := range value.Values {
				if !exceptionDirectSafeExpr(expression, context) {
					return false
				}
			}
		case *AssignmentStmt:
			for _, expression := range append(append([]ExprNode{}, value.Left...), value.Right...) {
				if !exceptionDirectSafeExpr(expression, context) {
					return false
				}
			}
		case *DeferStmt:
			if !exceptionDirectSafeExpr(value.Expression, context) {
				return false
			}
		case *GoStmt:
			if !exceptionDirectSafeExpr(value.Expression, context) {
				return false
			}
		case *SendStmt:
			if !exceptionDirectSafeExpr(value.Channel, context) || !exceptionDirectSafeExpr(value.Value, context) {
				return false
			}
		case *IncDecStmt:
			if !exceptionDirectSafeExpr(value.Expression, context) {
				return false
			}
		case *IfStmt:
			if !exceptionDirectSafeExpr(value.Init, context) || !exceptionDirectSafeExpr(value.Condition, context) || !exceptionDirectSafeBlock(value.Body, context) || !exceptionDirectSafeBlock(value.Else, context) {
				return false
			}
			if value.ElseIf != nil && !exceptionDirectSafeBlock(&BlockStmt{Statements: []Stmt{value.ElseIf}}, context) {
				return false
			}
		case *ForStmt:
			if !exceptionDirectSafeExpr(value.Init, context) || !exceptionDirectSafeExpr(value.Condition, context) || !exceptionDirectSafeExpr(value.Post, context) || !exceptionDirectSafeExpr(value.RangeExpr, context) || !exceptionDirectSafeBlock(value.Body, context) {
				return false
			}
		case *SwitchStmt:
			if !exceptionDirectSafeExpr(value.Init, context) || !exceptionDirectSafeExpr(value.Tag, context) || !exceptionDirectSafeBlock(value.Body, context) {
				return false
			}
		case *CaseStmt:
			for _, expression := range value.Clause.Expressions {
				if !exceptionDirectSafeExpr(expression, context) {
					return false
				}
			}
			if !exceptionDirectSafeBlock(value.Clause.Body, context) {
				return false
			}
		case *TryStmt:
			if !exceptionDirectSafe(value, context) {
				return false
			}
		case *TypeDeclarationStmt:
			continue
		default:
			return false
		}
	}
	return true
}

func exceptionDirectSafeExpr(expression ExprNode, context constructorContext) bool {
	lowered, err := lowerExceptionExprNode(expression, context)
	return err == nil && directExpressionSafe(lowered, context)
}

func exceptionDirectSafeThrowExpr(expression ExprNode, context constructorContext) bool {
	lowered, err := lowerExceptionExprNode(expression, context)
	if err != nil {
		return false
	}
	if callExpression, ok := lowered.(*CallExpr); ok {
		if result, found := promotedCallForExpr(callExpression, context, context.CurrentParameterTypes); found && result.trailingError {
			// A throw expression may be the single error result itself. It must
			// not be unwrapped before __gppThrow receives it.
			if len(result.types) != 1 || !isErrorLikeType(result.types[0], context) {
				return false
			}
			if !directExpressionSafe(callExpression.Callee, context) {
				return false
			}
			for _, argument := range callExpression.Arguments {
				if argument.Name != "" || !directExpressionSafe(argument.Value, context) {
					return false
				}
			}
			return true
		}
	}
	return directExpressionSafe(lowered, context)
}

func validateFinallyControlTransfersBlock(block *BlockStmt) error {
	var result error
	var visitBlock func(*BlockStmt)
	var visitStmt func(Stmt)
	visitBlock = func(current *BlockStmt) {
		if current == nil || result != nil {
			return
		}
		for _, statement := range current.Statements {
			visitStmt(statement)
		}
	}
	visitStmt = func(statement Stmt) {
		if statement == nil || result != nil {
			return
		}
		switch value := statement.(type) {
		case *ReturnStmt:
			result = fmt.Errorf("control transfer from finally is not allowed: return")
		case *BranchStmt:
			if value.Keyword == "break" || value.Keyword == "continue" || value.Keyword == "goto" {
				result = fmt.Errorf("control transfer from finally is not allowed: %s", value.Keyword)
			}
		case *TokenStmt:
			if value.Kind == BodyStmtReturn {
				result = fmt.Errorf("control transfer from finally is not allowed: return")
			}
			visitBlock(value.Body)
			for _, child := range value.Children {
				visitStmt(child)
			}
		case *IfStmt:
			visitBlock(value.Body)
			visitBlock(value.Else)
			visitStmt(value.ElseIf)
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
	return result
}

func lowerTryASTNode(tryBlock *ast.BlockStmt, clauses []catchClause, finallyBlock *ast.BlockStmt, hasFinally bool) (ast.Stmt, error) {
	deferBody := []ast.Stmt{}
	if hasFinally {
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
		return nil, fmt.Errorf("catch body is not valid Go after AST lowering: %w", err)
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

	return run, nil
}

func lowerCatchSwitch(thrownError, handled ast.Expr, clauses []catchClause) (ast.Stmt, error) {
	hasValueCatch := false
	for _, clause := range clauses {
		if clause.value != "" {
			hasValueCatch = true
			break
		}
	}
	if hasValueCatch {
		statements := []ast.Stmt{}
		for index, clause := range clauses {
			caseBody := []ast.Stmt{&ast.AssignStmt{
				Lhs: []ast.Expr{handled}, Tok: token.ASSIGN, Rhs: []ast.Expr{identifier("true")},
			}}
			if clause.body != nil {
				caseBody = append(caseBody, clause.body.List...)
			}
			if clause.value != "" {
				value, err := parser.ParseExpr(clause.value)
				if err != nil {
					return nil, err
				}
				condition := &ast.BinaryExpr{
					X:  &ast.BinaryExpr{X: thrownError, Op: token.NEQ, Y: identifier("nil")},
					Op: token.LAND,
					Y:  call(selector(identifier(clause.errorAlias), "Is"), thrownError, value),
				}
				condition = &ast.BinaryExpr{X: &ast.UnaryExpr{Op: token.NOT, X: handled}, Op: token.LAND, Y: condition}
				statements = append(statements, &ast.IfStmt{Cond: condition, Body: &ast.BlockStmt{List: caseBody}})
				continue
			}
			typeCases := make([]ast.Expr, 0, len(clause.typeNodes))
			for _, typeNode := range clause.typeNodes {
				typeExpr, err := goTypeExpr(typeNode)
				if err != nil {
					return nil, err
				}
				typeCases = append(typeCases, typeExpr)
			}
			catchName := identifier(fmt.Sprintf("__gppCaught%d", index))
			if clause.variable != "" {
				value := ast.Expr(catchName)
				if len(clause.typeNodes) > 1 {
					value = call(identifier("error"), catchName)
				}
				caseBody = append([]ast.Stmt{&ast.AssignStmt{
					Lhs: []ast.Expr{identifier(clause.variable)}, Tok: token.DEFINE, Rhs: []ast.Expr{value},
				}}, caseBody...)
			}
			switchStatement := &ast.TypeSwitchStmt{
				Assign: &ast.AssignStmt{
					Lhs: []ast.Expr{catchName}, Tok: token.DEFINE,
					Rhs: []ast.Expr{&ast.TypeAssertExpr{X: thrownError, Type: nil}},
				},
				Body: &ast.BlockStmt{List: []ast.Stmt{&ast.CaseClause{List: typeCases, Body: caseBody}}},
			}
			condition := &ast.BinaryExpr{X: &ast.UnaryExpr{Op: token.NOT, X: handled}, Op: token.LAND, Y: &ast.BinaryExpr{X: thrownError, Op: token.NEQ, Y: identifier("nil")}}
			statements = append(statements, &ast.IfStmt{Cond: condition, Body: &ast.BlockStmt{List: []ast.Stmt{switchStatement}}})
		}
		return &ast.BlockStmt{List: statements}, nil
	}

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
			expression, typeErr := goTypeExpr(typeNode)
			if typeErr != nil {
				return nil, typeErr
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
		lowered, lowerErr := goExceptionStmtNode(statement, "")
		if lowerErr != nil || len(lowered) != 1 {
			return body, false, nil
		}
		replacement, formatErr := formatExceptionASTNode(lowered[0])
		if formatErr != nil {
			return body, false, nil
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

// validateThrowExpressionNode is the AST equivalent used by the direct
// exception path. Keeping this check on nodes avoids rendering an expression
// solely to inspect its first token and preserves the original source spans
// for diagnostics.
func validateThrowExpressionNode(expression ExprNode, context constructorContext) error {
	if expression == nil {
		return nil
	}
	switch value := expression.(type) {
	case *LiteralExpr:
		if value.Kind == TokenString || value.Kind == TokenRawString || value.Kind == TokenRune {
			return fmt.Errorf("cannot throw string; thrown value must implement error")
		}
	case *NameExpr:
		if value.Name == "true" || value.Name == "false" {
			return fmt.Errorf("cannot throw bool; thrown value must implement error")
		}
	case *ParenthesizedExpr:
		return validateThrowExpressionNode(value.Inner, context)
	case *CallExpr:
		if name, ok := value.Callee.(*NameExpr); ok {
			if target, found := context.Targets[name.Name]; found && !classHasErrorMethod(target.Class, context, map[string]bool{}) {
				return fmt.Errorf("cannot throw %s; thrown value must implement error", name.Name)
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
			replacement, lowered, sourceErr := promotionASTReplacementNode(call, result.types, discard, astContext)
			if sourceErr != nil {
				promotionErr = sourceErr
				return
			}
			if !lowered {
				return
			}
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
	return exceptionResultCountFromTypeNode(context.CurrentResultAST)
}

func exceptionMethodResultCount(method Method) int {
	if len(method.ResultFieldsAST) > 0 {
		return len(method.ResultFieldsAST)
	}
	return exceptionResultCountFromTypeNode(methodResultTypeNode(method))
}

func exceptionResultCountFromTypeNode(result TypeNode) int {
	if result == nil {
		return 0
	}
	if tuple, ok := result.(*TupleType); ok {
		if len(tuple.Elements) == 1 {
			if _, opaque := tuple.Elements[0].(*TokenType); opaque {
				text, err := typeNodeSource(result)
				if err == nil {
					return exceptionResultCountFromText(text)
				}
			}
		}
		return len(tuple.Elements)
	}
	if _, opaque := result.(*TokenType); opaque {
		text, err := typeNodeSource(result)
		if err == nil {
			return exceptionResultCountFromText(text)
		}
		return 0
	}
	return 1
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
	case *AssignmentExpr:
		for _, expression := range value.Left {
			next(expression)
		}
		for _, expression := range value.Right {
			next(expression)
		}
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

// promotionASTReplacementNode handles the ordinary promotion wrappers from
// typed nodes. The compatibility rewriter still needs a final replacement
// string, but it no longer serializes the call merely to put it back inside a
// generated wrapper.
func promotionASTReplacementNode(call *CallExpr, resultTypes []string, discard bool, context constructorContext) (string, bool, error) {
	if call == nil {
		return "", false, nil
	}
	nonErrorCount := len(resultTypes) - 1
	name := ""
	if discard {
		switch nonErrorCount {
		case 0:
			name = "__gppThrow"
		case 1:
			name = "__gppDiscard"
		case 2:
			name = "__gppDiscard2"
		case 3:
			name = "__gppDiscard3"
		}
	} else {
		switch nonErrorCount {
		case 1:
			name = "__gppUnwrap"
		case 2:
			name = "__gppUnwrap2"
		case 3:
			name = "__gppUnwrap3"
		}
	}
	if name == "" {
		return promotionASTInlineReplacementNode(call, resultTypes, discard, context)
	}
	expression, err := goExprNode(call)
	if err != nil {
		return "", false, nil
	}
	formatted, err := formatNode(&ast.CallExpr{Fun: ast.NewIdent(name), Args: []ast.Expr{expression}})
	return formatted, true, err
}

func promotionASTInlineReplacementNode(call *CallExpr, resultTypes []string, discard bool, context constructorContext) (string, bool, error) {
	if call == nil || len(resultTypes) < 2 {
		return "", false, nil
	}
	callExpression, err := goExprNode(call)
	if err != nil {
		return "", false, nil
	}

	nonErrorCount := len(resultTypes) - 1
	valueNames := make([]string, nonErrorCount)
	left := make([]ast.Expr, 0, len(resultTypes))
	for index := range valueNames {
		if discard {
			valueNames[index] = "_"
		} else {
			valueNames[index] = fmt.Sprintf("__gppExceptionValue%d", index)
		}
		left = append(left, ast.NewIdent(valueNames[index]))
	}
	errorName := "__gppExceptionError"
	left = append(left, ast.NewIdent(errorName))

	body := []ast.Stmt{&ast.AssignStmt{
		Lhs: left,
		Tok: token.DEFINE,
		Rhs: []ast.Expr{callExpression},
	}}
	throw := &ast.ExprStmt{X: &ast.CallExpr{
		Fun:  ast.NewIdent("__gppThrow"),
		Args: []ast.Expr{ast.NewIdent(errorName)},
	}}
	if discard {
		body = append(body, throw)
		function := &ast.FuncLit{
			Type: &ast.FuncType{Params: &ast.FieldList{}},
			Body: &ast.BlockStmt{List: body},
		}
		formatted, formatErr := formatNode(&ast.CallExpr{Fun: function})
		return formatted, true, formatErr
	}

	results := make([]*ast.Field, 0, nonErrorCount)
	for _, resultType := range resultTypes[:nonErrorCount] {
		resultType = transformPolymorphicType(resultType, context)
		typeNode := parseTypeText(strings.TrimSpace(resultType))
		if typeNode == nil {
			return "", false, nil
		}
		typeExpression, typeErr := goTypeExpr(typeNode)
		if typeErr != nil {
			return "", false, nil
		}
		results = append(results, &ast.Field{Type: typeExpression})
	}
	values := make([]ast.Expr, 0, len(valueNames))
	for _, name := range valueNames {
		values = append(values, ast.NewIdent(name))
	}
	body = append(body, throw, &ast.ReturnStmt{Results: values})
	function := &ast.FuncLit{
		Type: &ast.FuncType{
			Params:  &ast.FieldList{},
			Results: &ast.FieldList{List: results},
		},
		Body: &ast.BlockStmt{List: body},
	}
	formatted, formatErr := formatNode(&ast.CallExpr{Fun: function})
	return formatted, true, formatErr
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
				candidates = append(candidates, callableSignature{Parameters: extensionCallParameters(extension), ResultAST: methodResultTypeNode(extension.Method)})
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
				// Selector syntax supplies the receiver separately. The generated
				// function signature includes `this`, but the source call's
				// argument list does not.
				parameters := methodCallParameters(extension.Method)
				candidates = append(candidates, callableSignature{Parameters: parameters, ResultAST: methodResultTypeNode(extension.Method)})
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
	if transformed, handled, err := transformExceptionABIBoundariesAST(src, context); handled {
		return transformed, err
	}
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
		fields, hasResults := exceptionResultFieldsForResult(function.Method.ResultAST, function.Method.ResultFieldsAST)
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

// transformExceptionABIBoundariesAST keeps the compatibility ABI rewrite on
// the same typed body path used by direct function/method emission. The source
// span is only formatted at the final replacement boundary; the wrapper,
// recover logic, assignments, and returns are all constructed as Go AST.
func transformExceptionABIBoundariesAST(src string, context constructorContext) (string, bool, error) {
	functions := parseTopLevelFunctions("exception ABI", "main", src, 0, src, "", 0)
	if len(functions) == 0 {
		return src, false, nil
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
		methodContext := context
		methodContext.CurrentResultAST = function.Method.ResultAST
		methodContext.CurrentParameterTypes = parameterTypeMapFromNodes(function.Method.ParameterAST)
		body, lowerErr := lowerFunctionGoBlockNode(function.Method.BodyAST, methodContext, "")
		if lowerErr != nil {
			return src, false, nil
		}
		fields, hasResults := exceptionResultFieldsForResult(function.Method.ResultAST, function.Method.ResultFieldsAST)
		hasExceptionReturn := astContainsExceptionReturn(body)
		if !hasResults || len(fields) == 0 {
			if !hasExceptionReturn {
				continue
			}
		} else if !hasExceptionReturn && exceptionResultFieldType(fields[len(fields)-1]) != "error" {
			continue
		}
		boundary, boundaryErr := lowerExceptionABIBoundaryNode(body, function.Method.ResultAST, function.Method.ResultFieldsAST, methodContext)
		if boundaryErr != nil {
			return src, false, nil
		}
		text, formatErr := formatExceptionASTBody(boundary)
		if formatErr != nil {
			return src, false, nil
		}
		span := function.Method.BodySpan
		if span.Start < 0 || span.End < span.Start || span.End > len(src) {
			return src, false, nil
		}
		replacements = append(replacements, replacement{start: span.Start, end: span.End, text: text})
	}
	if len(replacements) == 0 {
		return src, true, nil
	}
	sort.Slice(replacements, func(left, right int) bool { return replacements[left].start > replacements[right].start })
	for _, replacement := range replacements {
		src = src[:replacement.start] + replacement.text + src[replacement.end:]
	}
	return src, true, nil
}

func formatExceptionASTBody(body *ast.BlockStmt) (string, error) {
	if body == nil {
		return "", nil
	}
	text, err := formatNode(body)
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	if len(text) < 2 || text[0] != '{' || text[len(text)-1] != '}' {
		return "", fmt.Errorf("formatted exception body is not a block")
	}
	return strings.TrimSpace(text[1 : len(text)-1]), nil
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
		typeNode, ok := typeNodeFromGoExpr(field.Type)
		if !ok {
			typeName, err := formatNode(field.Type)
			if err != nil {
				return nil, false
			}
			typeNode = parseTypeText(strings.TrimSpace(typeName))
		}
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

// exceptionResultFieldsFromTypeNode keeps the direct exception ABI path on
// the typed signature tree. TupleType represents multiple results; all other
// result nodes represent one unnamed result. Synthetic methods that do not
// carry ResultFieldsAST may still use the narrow opaque compatibility fallback.
func exceptionResultFieldsFromTypeNode(result TypeNode) ([]exceptionResultField, bool) {
	if result == nil {
		return nil, true
	}
	if tuple, ok := result.(*TupleType); ok {
		fields := make([]exceptionResultField, 0, len(tuple.Elements))
		for _, element := range tuple.Elements {
			if element == nil {
				return nil, false
			}
			if _, opaque := element.(*TokenType); opaque {
				// A one-element tuple containing TokenType is the current
				// representation of a named result such as `(value int)`.
				// Preserve that narrow compatibility fallback until result
				// fields have their own structured AST node.
				text, err := typeNodeSource(result)
				if err != nil {
					return nil, false
				}
				return exceptionResultFieldsFromText(text)
			}
			fields = append(fields, exceptionResultField{typeNode: element})
		}
		return fields, true
	}
	if _, opaque := result.(*TokenType); opaque {
		// Named Go result fields are still represented by the token-preserving
		// type fallback. Keep this compatibility path narrow; unnamed and
		// ordinary structured results above never round-trip through text.
		text, err := typeNodeSource(result)
		if err != nil {
			return nil, false
		}
		return exceptionResultFieldsFromText(text)
	}
	return []exceptionResultField{{typeNode: result}}, true
}

func exceptionResultFieldsForResult(result TypeNode, resultFields []ParameterNode) ([]exceptionResultField, bool) {
	if len(resultFields) > 0 {
		fields := make([]exceptionResultField, 0, len(resultFields))
		for _, field := range resultFields {
			if field.Type == nil {
				return nil, false
			}
			fields = append(fields, exceptionResultField{name: field.Name, typeNode: field.Type})
		}
		return fields, true
	}
	return exceptionResultFieldsFromTypeNode(result)
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
	if transformed, handled := wrapExceptionBoundaryBodyAST(body, result, context); handled {
		return transformed, true
	}
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
		if transformed, ok := transformPolymorphicTypeNode(fields[index].typeNode, context); ok {
			fields[index].typeNode = transformed
		} else {
			typeName := transformPolymorphicType(exceptionResultFieldType(fields[index]), context)
			fields[index].typeNode = parseTypeText(typeName)
		}
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

// wrapExceptionBoundaryBodyAST is the typed first pass for the compatibility
// emitters that still receive a rendered method body. The body is parsed only
// at this final compatibility boundary; the recover wrapper and all of its
// assignments/returns are constructed as Go AST and formatted once. Opaque
// token-preserved bodies intentionally fall through to wrapExceptionBoundaryBody.
func wrapExceptionBoundaryBodyAST(body, result string, context constructorContext) (string, bool) {
	fields, ok := exceptionResultFieldsFromText(result)
	if !ok {
		return body, false
	}
	tokens, err := LexSource("exception boundary body", body)
	if err != nil {
		return body, false
	}
	parsed, err := ParseBodyAST(tokens)
	if err != nil || parsed == nil {
		return body, false
	}
	lowered, err := lowerFunctionGoBlockNode(parsed, context, "")
	if err != nil {
		return body, false
	}
	hasExceptionReturn := astContainsExceptionReturn(lowered)
	if len(fields) == 0 {
		if !hasExceptionReturn {
			return body, false
		}
	} else if !hasExceptionReturn && exceptionResultFieldType(fields[len(fields)-1]) != "error" {
		return body, false
	}
	resultFields := make([]ParameterNode, 0, len(fields))
	for _, field := range fields {
		resultFields = append(resultFields, ParameterNode{Name: field.name, Type: field.typeNode})
	}
	boundary, err := lowerExceptionABIBoundaryNode(lowered, nil, resultFields, context)
	if err != nil {
		return body, false
	}
	formatted, err := formatExceptionASTBody(boundary)
	if err != nil {
		return body, false
	}
	return formatted, true
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
					ResultAST:  methodResultTypeNode(extension.Method),
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
						ResultAST:  methodResultTypeNode(extension.Method),
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
	return append(parameters, methodCallParameters(extension.Method)...)
}

func methodCallParameters(method Method) []parameterInfo {
	if parameters, err := parameterInfosForMethod(method); err == nil && (method.Owner != nil || len(method.ParameterAST) > 0) {
		return parameters
	}
	return mustParameters(methodParametersSource(method))
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
