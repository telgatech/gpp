package compiler

import (
	"go/ast"
	"go/parser"
	gotoken "go/token"
)

// goStatementFromDraft is the ordinary-Go interoperability path for a
// statement that the Go++ statement recognizer did not classify. It parses a
// synthetic one-statement function only as a frontend adapter, then converts
// the Go AST into the compiler's typed statement tree. The resulting tree is
// retained; the source text is never stored as a declaration fallback.
//
// Go++ constructs which Go cannot parse, or which do not yet have a matching
// Go++ node, deliberately return nil and remain TokenStmt values.
func goStatementFromDraft(draft *statementDraft) Stmt {
	if draft == nil || len(significantSyntaxTokens(draft.Header)) == 0 {
		return nil
	}
	source := expressionTokensSource(draft.Header)
	if source == "" {
		return nil
	}
	if draft.Body != nil {
		source += " {}"
	}
	parsed, ok := parseGoStatementSource(source)
	if !ok {
		return nil
	}
	statement, ok := goASTStatementNode(parsed, draft)
	if !ok {
		return nil
	}
	return statement
}

func parseGoStatementSource(source string) (ast.Stmt, bool) {
	file, err := parser.ParseFile(
		gotoken.NewFileSet(),
		"gpp-statement.go",
		"package gppstmt\n\nfunc __gpp_statement() {\n"+source+"\n}\n",
		0,
	)
	if err != nil || file == nil || len(file.Decls) != 1 {
		return nil, false
	}
	function, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok || function.Body == nil || len(function.Body.List) != 1 {
		return nil, false
	}
	return function.Body.List[0], true
}

func goASTStatementNode(statement ast.Stmt, draft *statementDraft) (Stmt, bool) {
	switch value := statement.(type) {
	case *ast.ExprStmt:
		expression, ok := exprNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		return &ExpressionStmt{Expression: expression, SpanValue: spanFromDraft(draft)}, true
	case *ast.ReturnStmt:
		values := make([]ExprNode, 0, len(value.Results))
		for _, result := range value.Results {
			expression, ok := exprNodeFromGoExpr(result)
			if !ok {
				return nil, false
			}
			values = append(values, expression)
		}
		return &ReturnStmt{Values: values, SpanValue: spanFromDraft(draft)}, true
	case *ast.AssignStmt:
		left, ok := goASTExpressions(value.Lhs)
		if !ok {
			return nil, false
		}
		right, ok := goASTExpressions(value.Rhs)
		if !ok {
			return nil, false
		}
		return &AssignmentStmt{Left: left, Operator: value.Tok.String(), Right: right, SpanValue: spanFromDraft(draft)}, true
	case *ast.DeclStmt:
		return goASTDeclarationStatement(value.Decl, spanFromDraft(draft))
	case *ast.IncDecStmt:
		expression, ok := exprNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		return &IncDecStmt{Expression: expression, Operator: value.Tok.String(), SpanValue: spanFromDraft(draft)}, true
	case *ast.SendStmt:
		channel, channelOK := exprNodeFromGoExpr(value.Chan)
		item, itemOK := exprNodeFromGoExpr(value.Value)
		if !channelOK || !itemOK {
			return nil, false
		}
		return &SendStmt{Channel: channel, Value: item, SpanValue: spanFromDraft(draft)}, true
	case *ast.DeferStmt:
		expression, ok := exprNodeFromGoExpr(value.Call)
		if !ok {
			return nil, false
		}
		return &DeferStmt{Expression: expression, SpanValue: spanFromDraft(draft)}, true
	case *ast.GoStmt:
		expression, ok := exprNodeFromGoExpr(value.Call)
		if !ok {
			return nil, false
		}
		return &GoStmt{Expression: expression, SpanValue: spanFromDraft(draft)}, true
	case *ast.BranchStmt:
		if value.Tok == gotoken.BREAK || value.Tok == gotoken.CONTINUE || value.Tok == gotoken.FALLTHROUGH || value.Tok == gotoken.GOTO {
			var target []Token
			if value.Label != nil {
				target = []Token{{Kind: TokenIdentifier, Text: value.Label.Name}}
			}
			return &BranchStmt{Keyword: value.Tok.String(), Target: target, SpanValue: spanFromDraft(draft)}, true
		}
		return nil, false
	case *ast.IfStmt:
		condition, ok := exprNodeFromGoExpr(value.Cond)
		if !ok {
			return nil, false
		}
		init, ok := goASTSimpleExpression(value.Init)
		if !ok {
			return nil, false
		}
		body, ok := goASTBlockNode(value.Body)
		if draft != nil && draft.Body != nil {
			body = draft.Body
			ok = true
		}
		if !ok {
			return nil, false
		}
		result := &IfStmt{Init: init, Condition: condition, Body: body, SpanValue: spanFromDraft(draft)}
		if draft != nil && len(draft.Children) > 0 {
			child := draft.Children[0]
			if child != nil {
				if childStatement := structuredStatement(child); childStatement != nil {
					switch nested := childStatement.(type) {
					case *IfStmt:
						result.ElseIf = nested
					case *BlockStmt:
						result.Else = nested
					default:
						result.Else = child.Body
					}
				} else if child.Body != nil {
					result.Else = child.Body
				}
			}
		} else if value.Else != nil {
			if elseBlock, elseOK := value.Else.(*ast.BlockStmt); elseOK {
				result.Else, ok = goASTBlockNode(elseBlock)
				if !ok {
					return nil, false
				}
			} else if elseIf, elseOK := value.Else.(*ast.IfStmt); elseOK {
				converted, convertedOK := goASTStatementNode(elseIf, nil)
				if !convertedOK {
					return nil, false
				}
				result.ElseIf, ok = converted.(*IfStmt)
				if !ok {
					return nil, false
				}
			}
		}
		return result, true
	case *ast.ForStmt:
		body, ok := goASTBlockNode(value.Body)
		if draft != nil && draft.Body != nil {
			body = draft.Body
			ok = true
		}
		if !ok {
			return nil, false
		}
		init, ok := goASTSimpleExpression(value.Init)
		if !ok {
			return nil, false
		}
		condition, ok := exprNodeFromGoExpr(value.Cond)
		if value.Cond != nil && !ok {
			return nil, false
		}
		post, ok := goASTSimpleExpression(value.Post)
		if !ok {
			return nil, false
		}
		return &ForStmt{Init: init, Condition: condition, Post: post, Body: body, SpanValue: spanFromDraft(draft)}, true
	case *ast.RangeStmt:
		rangeExpression, ok := exprNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		body, ok := goASTBlockNode(value.Body)
		if draft != nil && draft.Body != nil {
			body = draft.Body
			ok = true
		}
		if !ok {
			return nil, false
		}
		keys := []ExprNode{}
		if value.Key != nil {
			key, keyOK := exprNodeFromGoExpr(value.Key)
			if !keyOK {
				return nil, false
			}
			keys = append(keys, key)
		}
		if value.Value != nil {
			item, itemOK := exprNodeFromGoExpr(value.Value)
			if !itemOK {
				return nil, false
			}
			keys = append(keys, item)
		}
		return &ForStmt{RangeKey: keys, RangeOperator: value.Tok.String(), RangeExpr: rangeExpression, Body: body, SpanValue: spanFromDraft(draft)}, true
	case *ast.SwitchStmt:
		body, ok := goASTBlockNode(value.Body)
		if draft != nil && draft.Body != nil {
			body = draft.Body
			ok = true
		}
		if !ok {
			return nil, false
		}
		init, ok := goASTSimpleExpression(value.Init)
		if !ok {
			return nil, false
		}
		tag, ok := exprNodeFromGoExpr(value.Tag)
		if value.Tag != nil && !ok {
			return nil, false
		}
		return &SwitchStmt{Init: init, Tag: tag, Body: body, SpanValue: spanFromDraft(draft)}, true
	case *ast.TypeSwitchStmt:
		body, ok := goASTBlockNode(value.Body)
		if draft != nil && draft.Body != nil {
			body = draft.Body
			ok = true
		}
		if !ok {
			return nil, false
		}
		assignment, ok := goASTSimpleExpression(value.Assign)
		if !ok {
			return nil, false
		}
		result := &SwitchStmt{Body: body, SpanValue: spanFromDraft(draft)}
		if isTypeSwitchAssignment(assignment) {
			result.Init = assignment
		} else if isTypeSwitchAssertion(assignment) {
			result.Tag = assignment
		} else {
			return nil, false
		}
		return result, true
	case *ast.SelectStmt:
		body, ok := goASTBlockNode(value.Body)
		if draft != nil && draft.Body != nil {
			body = draft.Body
			ok = true
		}
		if !ok {
			return nil, false
		}
		return &SwitchStmt{Select: true, Body: body, SpanValue: spanFromDraft(draft)}, true
	case *ast.LabeledStmt:
		if value.Label == nil {
			return nil, false
		}
		return &LabelStmt{Name: value.Label.Name, SpanValue: spanFromDraft(draft)}, true
	case *ast.BlockStmt:
		if draft != nil && draft.Body != nil {
			return draft.Body, true
		}
		return goASTBlockNode(value)
	case *ast.CaseClause:
		return goASTCaseClause(value)
	case *ast.CommClause:
		return goASTCommClause(value)
	default:
		return nil, false
	}
}

func goASTDeclarationStatement(declaration ast.Decl, span Span) (Stmt, bool) {
	group, ok := declaration.(*ast.GenDecl)
	if !ok || len(group.Specs) != 1 {
		return nil, false
	}
	switch spec := group.Specs[0].(type) {
	case *ast.ValueSpec:
		names := make([]Token, 0, len(spec.Names))
		for _, name := range spec.Names {
			names = append(names, Token{Kind: TokenIdentifier, Text: name.Name})
		}
		values, ok := goASTExpressions(spec.Values)
		if !ok {
			return nil, false
		}
		var typeNode TypeNode
		if spec.Type != nil {
			var typeOK bool
			typeNode, typeOK = typeNodeFromGoExpr(spec.Type)
			if !typeOK {
				return nil, false
			}
		}
		keyword := group.Tok.String()
		return &DeclarationStmt{Keyword: keyword, Names: names, Type: typeNode, Values: values, SpanValue: span}, true
	case *ast.TypeSpec:
		typeNode, ok := typeNodeFromGoExpr(spec.Type)
		if !ok {
			return nil, false
		}
		return &TypeDeclarationStmt{Name: spec.Name.Name, Alias: spec.Assign.IsValid(), Type: typeNode, SpanValue: span}, true
	default:
		return nil, false
	}
}

func goASTExpressions(expressions []ast.Expr) ([]ExprNode, bool) {
	result := make([]ExprNode, 0, len(expressions))
	for _, expression := range expressions {
		converted, ok := exprNodeFromGoExpr(expression)
		if !ok {
			return nil, false
		}
		result = append(result, converted)
	}
	return result, true
}

func goASTSimpleExpression(statement ast.Stmt) (ExprNode, bool) {
	if statement == nil {
		return nil, true
	}
	switch value := statement.(type) {
	case *ast.ExprStmt:
		return exprNodeFromGoExpr(value.X)
	case *ast.AssignStmt:
		left, leftOK := goASTExpressions(value.Lhs)
		right, rightOK := goASTExpressions(value.Rhs)
		if !leftOK || !rightOK {
			return nil, false
		}
		return &AssignmentExpr{Left: left, Operator: value.Tok.String(), Right: right}, true
	case *ast.IncDecStmt:
		expression, ok := exprNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		return &PostfixExpr{Expression: expression, Operator: value.Tok.String()}, true
	default:
		return nil, false
	}
}

func goASTBlockNode(block *ast.BlockStmt) (*BlockStmt, bool) {
	if block == nil {
		return &BlockStmt{}, true
	}
	result := &BlockStmt{}
	for _, statement := range block.List {
		converted, ok := goASTStatementNode(statement, nil)
		if !ok {
			return nil, false
		}
		result.Statements = append(result.Statements, converted)
	}
	return result, true
}

func goASTCaseClause(clause *ast.CaseClause) (Stmt, bool) {
	if clause == nil {
		return nil, false
	}
	expressions, ok := goASTExpressions(clause.List)
	if !ok {
		return nil, false
	}
	body, ok := goASTBlockNode(&ast.BlockStmt{List: clause.Body})
	if !ok {
		return nil, false
	}
	return &CaseStmt{Clause: CaseClause{Default: len(clause.List) == 0, Expressions: expressions, Body: body}}, true
}

func goASTCommClause(clause *ast.CommClause) (Stmt, bool) {
	if clause == nil {
		return nil, false
	}
	body, ok := goASTBlockNode(&ast.BlockStmt{List: clause.Body})
	if !ok {
		return nil, false
	}
	caseClause := CaseClause{Default: clause.Comm == nil, Body: body}
	if clause.Comm != nil {
		communication, ok := goASTCommunicationExpr(clause.Comm)
		if !ok {
			return nil, false
		}
		caseClause.Expressions = []ExprNode{communication}
	}
	return &CaseStmt{Clause: caseClause}, true
}

func goASTCommunicationExpr(statement ast.Stmt) (ExprNode, bool) {
	switch value := statement.(type) {
	case *ast.SendStmt:
		channel, channelOK := exprNodeFromGoExpr(value.Chan)
		item, itemOK := exprNodeFromGoExpr(value.Value)
		if !channelOK || !itemOK {
			return nil, false
		}
		return &SendExpr{Channel: channel, Value: item}, true
	case *ast.AssignStmt:
		left, leftOK := goASTExpressions(value.Lhs)
		right, rightOK := goASTExpressions(value.Rhs)
		if !leftOK || !rightOK || len(left) != 1 || len(right) != 1 {
			return nil, false
		}
		return &BinaryExpr{Left: left[0], Operator: value.Tok.String(), Right: right[0]}, true
	case *ast.ExprStmt:
		return exprNodeFromGoExpr(value.X)
	default:
		return nil, false
	}
}

func spanFromDraft(draft *statementDraft) Span {
	if draft == nil {
		return Span{}
	}
	return draft.SpanValue
}
