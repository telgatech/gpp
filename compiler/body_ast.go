package compiler

import (
	"fmt"
	"reflect"
)

// BodyStmtKind classifies a parsed statement without losing its original
// tokens. It is deliberately syntax-level; name and type resolution happen in
// later compiler phases.
type BodyStmtKind uint8

const (
	BodyStmtUnknown BodyStmtKind = iota
	BodyStmtExpression
	BodyStmtDeclaration
	BodyStmtReturn
	BodyStmtThrow
	BodyStmtIf
	BodyStmtFor
	BodyStmtSwitch
	BodyStmtTry
	BodyStmtCatch
	BodyStmtFinally
	BodyStmtDefer
	BodyStmtGo
	BodyStmtBranch
	BodyStmtBlock
	BodyStmtLabel
	BodyStmtTypeDeclaration
)

type Stmt interface {
	Node
	stmt()
}

type ExpressionStmt struct {
	Expression ExprNode
	SpanValue  Span
}

func (*ExpressionStmt) node()                {}
func (*ExpressionStmt) stmt()                {}
func (statement *ExpressionStmt) Span() Span { return statement.SpanValue }

type DeclarationStmt struct {
	Keyword   string
	Names     []Token
	Type      TypeNode
	Values    []ExprNode
	SpanValue Span
}

func (*DeclarationStmt) node()                {}
func (*DeclarationStmt) stmt()                {}
func (statement *DeclarationStmt) Span() Span { return statement.SpanValue }

type TypeDeclarationStmt struct {
	Name      string
	Alias     bool
	Type      TypeNode
	SpanValue Span
}

func (*TypeDeclarationStmt) node()                {}
func (*TypeDeclarationStmt) stmt()                {}
func (statement *TypeDeclarationStmt) Span() Span { return statement.SpanValue }

type AssignmentStmt struct {
	Left      []ExprNode
	Operator  string
	Right     []ExprNode
	SpanValue Span
}

func (*AssignmentStmt) node()                {}
func (*AssignmentStmt) stmt()                {}
func (statement *AssignmentStmt) Span() Span { return statement.SpanValue }

type LabelStmt struct {
	Name      string
	SpanValue Span
}

func (*LabelStmt) node()                {}
func (*LabelStmt) stmt()                {}
func (statement *LabelStmt) Span() Span { return statement.SpanValue }

type ReturnStmt struct {
	Values    []ExprNode
	SpanValue Span
}

func (*ReturnStmt) node()                {}
func (*ReturnStmt) stmt()                {}
func (statement *ReturnStmt) Span() Span { return statement.SpanValue }

type ThrowStmt struct {
	Value     ExprNode
	SpanValue Span
}

func (*ThrowStmt) node()                {}
func (*ThrowStmt) stmt()                {}
func (statement *ThrowStmt) Span() Span { return statement.SpanValue }

type BranchStmt struct {
	Keyword   string
	Target    []Token
	SpanValue Span
}

func (*BranchStmt) node()                {}
func (*BranchStmt) stmt()                {}
func (statement *BranchStmt) Span() Span { return statement.SpanValue }

type DeferStmt struct {
	Expression ExprNode
	SpanValue  Span
}

func (*DeferStmt) node()                {}
func (*DeferStmt) stmt()                {}
func (statement *DeferStmt) Span() Span { return statement.SpanValue }

type GoStmt struct {
	Expression ExprNode
	SpanValue  Span
}

func (*GoStmt) node()                {}
func (*GoStmt) stmt()                {}
func (statement *GoStmt) Span() Span { return statement.SpanValue }

// SendStmt represents Go's channel send statement (channel <- value). It is
// separate from BinaryExpr because send is a statement in Go's grammar, not a
// value-producing operator.
type SendStmt struct {
	Channel   ExprNode
	Value     ExprNode
	SpanValue Span
}

func (*SendStmt) node()                {}
func (*SendStmt) stmt()                {}
func (statement *SendStmt) Span() Span { return statement.SpanValue }

type IncDecStmt struct {
	Expression ExprNode
	Operator   string
	SpanValue  Span
}

func (*IncDecStmt) node()                {}
func (*IncDecStmt) stmt()                {}
func (statement *IncDecStmt) Span() Span { return statement.SpanValue }

type IfStmt struct {
	Init      ExprNode
	Condition ExprNode
	Body      *BlockStmt
	Else      *BlockStmt
	ElseIf    *IfStmt
	SpanValue Span
}

func (*IfStmt) node()                {}
func (*IfStmt) stmt()                {}
func (statement *IfStmt) Span() Span { return statement.SpanValue }

type CatchClause struct {
	Types     []TypeNode
	Binding   string
	Body      *BlockStmt
	SpanValue Span
}

func (*CatchClause) node() {}
func (clause *CatchClause) Span() Span {
	if clause == nil {
		return Span{}
	}
	return clause.SpanValue
}

type TryStmt struct {
	Body      *BlockStmt
	Catches   []CatchClause
	Finally   *BlockStmt
	SpanValue Span
}

func (*TryStmt) node()                {}
func (*TryStmt) stmt()                {}
func (statement *TryStmt) Span() Span { return statement.SpanValue }

type ForStmt struct {
	Init      ExprNode
	Condition ExprNode
	Post      ExprNode
	// RangeKey contains the one or two binding expressions before `range`.
	// RangeOperator is `:=` or `=` when the source explicitly provides one.
	// Keeping the bindings as expressions prevents assignment punctuation from
	// being mistaken for an identifier during Go AST lowering.
	RangeKey      []ExprNode
	RangeOperator string
	RangeExpr     ExprNode
	Body          *BlockStmt
	SpanValue     Span
}

func (*ForStmt) node()                {}
func (*ForStmt) stmt()                {}
func (statement *ForStmt) Span() Span { return statement.SpanValue }

type SwitchStmt struct {
	Select    bool
	Init      ExprNode
	Tag       ExprNode
	Body      *BlockStmt
	SpanValue Span
}

func (*SwitchStmt) node()                {}
func (*SwitchStmt) stmt()                {}
func (statement *SwitchStmt) Span() Span { return statement.SpanValue }

type CaseClause struct {
	Default     bool
	Expressions []ExprNode
	Body        *BlockStmt
	SpanValue   Span
}

func (*CaseClause) node() {}
func (clause *CaseClause) Span() Span {
	if clause == nil {
		return Span{}
	}
	return clause.SpanValue
}

type CaseStmt struct {
	Clause    CaseClause
	SpanValue Span
}

func (*CaseStmt) node()                {}
func (*CaseStmt) stmt()                {}
func (statement *CaseStmt) Span() Span { return statement.SpanValue }

type BlockStmt struct {
	Open       Span
	Close      Span
	Comments   []Token
	Statements []Stmt
	SpanValue  Span
}

func (*BlockStmt) node() {}
func (*BlockStmt) stmt() {}
func (block *BlockStmt) Span() Span {
	if block == nil {
		return Span{}
	}
	return block.SpanValue
}

// statementDraft is parser-only scratch state. It is never exposed in a
// parsed BlockStmt: recognized drafts become typed statements, and
// unrecognized drafts become TokenStmt values.
type statementDraft struct {
	Kind   BodyStmtKind
	Header []Token
	// Exprs contains the expressions that have already been recognized in the
	// statement. Header remains available for syntax that still needs a
	// dedicated statement node (for example, a full for-clause).
	Exprs     []ExprNode
	Body      *BlockStmt
	Children  []*statementDraft
	SpanValue Span
}

// TokenStmt is the lossless fallback for statement syntax that does not yet
// have a dedicated node. It contains tokens and recursively structured
// blocks, never an executable source string or a duplicate typed statement.
// As statement coverage grows, values of this type should disappear from
// ordinary Go++ programs.
type TokenStmt struct {
	Kind      BodyStmtKind
	Tokens    []Token
	Exprs     []ExprNode
	Body      *BlockStmt
	Children  []Stmt
	SpanValue Span
}

func (*TokenStmt) node() {}
func (*TokenStmt) stmt() {}
func (statement *TokenStmt) Span() Span {
	if statement == nil {
		return Span{}
	}
	return statement.SpanValue
}

func (statement *statementDraft) Span() Span {
	if statement == nil {
		return Span{}
	}
	return statement.SpanValue
}

// walkStmtExpressions is the shared expression walk for lowering passes. It
// understands token fallbacks and direct statement nodes, so adding a
// statement family does not require every pass to duplicate the same
// recursive traversal.
func walkStmtExpressions(statement Stmt, visit func(ExprNode)) {
	if statement == nil || visit == nil || isNilStmt(statement) {
		return
	}
	switch value := statement.(type) {
	case *TokenStmt:
		for _, expression := range value.Exprs {
			visit(expression)
		}
		walkBlockExpressions(value.Body, visit)
		for _, child := range value.Children {
			walkStmtExpressions(child, visit)
		}
	case *ExpressionStmt:
		visit(value.Expression)
	case *DeclarationStmt:
		for _, expression := range value.Values {
			visit(expression)
		}
	case *AssignmentStmt:
		for _, expression := range value.Left {
			visit(expression)
		}
		for _, expression := range value.Right {
			visit(expression)
		}
	case *ReturnStmt:
		for _, expression := range value.Values {
			visit(expression)
		}
	case *ThrowStmt:
		visit(value.Value)
	case *DeferStmt:
		visit(value.Expression)
	case *GoStmt:
		visit(value.Expression)
	case *SendStmt:
		visit(value.Channel)
		visit(value.Value)
	case *IncDecStmt:
		visit(value.Expression)
	case *IfStmt:
		visit(value.Init)
		visit(value.Condition)
		walkBlockExpressions(value.Body, visit)
		walkBlockExpressions(value.Else, visit)
		if value.ElseIf != nil {
			walkStmtExpressions(value.ElseIf, visit)
		}
	case *TryStmt:
		walkBlockExpressions(value.Body, visit)
		for _, clause := range value.Catches {
			walkBlockExpressions(clause.Body, visit)
		}
		walkBlockExpressions(value.Finally, visit)
	case *ForStmt:
		visit(value.Init)
		visit(value.Condition)
		visit(value.Post)
		visit(value.RangeExpr)
		walkBlockExpressions(value.Body, visit)
	case *SwitchStmt:
		visit(value.Init)
		visit(value.Tag)
		walkBlockExpressions(value.Body, visit)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			visit(expression)
		}
		walkBlockExpressions(value.Clause.Body, visit)
	case *BlockStmt:
		walkBlockExpressions(value, visit)
	}
}

func isNilStmt(statement Stmt) bool {
	value := reflect.ValueOf(statement)
	return value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) && value.IsNil()
}

func walkBlockExpressions(block *BlockStmt, visit func(ExprNode)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		walkStmtExpressions(statement, visit)
	}
}

func ParseBodyAST(tokens []Token) (*BlockStmt, error) {
	parser := bodyParser{tokens: tokens}
	block, err := parser.parseBlock(false)
	if err != nil {
		return nil, err
	}
	remaining := parser.nextSignificant()
	if remaining < len(parser.tokens) && parser.tokens[remaining].Kind != TokenEOF {
		return nil, fmt.Errorf("unexpected token after body")
	}
	return block, nil
}

type bodyParser struct {
	tokens []Token
	index  int
}

func (parser *bodyParser) parseBlock(expectClose bool) (*BlockStmt, error) {
	start := parser.nextSignificant()
	block := &BlockStmt{}
	if start < len(parser.tokens) {
		block.SpanValue.Start = parser.tokens[start].Span.Start
		block.SpanValue.Line = parser.tokens[start].Span.Line
		block.SpanValue.Column = parser.tokens[start].Span.Column
	}

	for {
		parser.consumeSeparators(block)
		if parser.index >= len(parser.tokens) || parser.tokens[parser.index].Kind == TokenEOF {
			if expectClose {
				return nil, fmt.Errorf("unterminated body block")
			}
			block.SpanValue.End = blockEnd(block, parser.tokens)
			return block, nil
		}
		if parser.isPunctuation(parser.index, '}') {
			if !expectClose {
				return nil, fmt.Errorf("unexpected closing body brace")
			}
			block.Close = parser.tokens[parser.index].Span
			parser.index++
			block.SpanValue.End = block.Close.End
			return block, nil
		}

		statement, err := parser.parseStatement()
		if err != nil {
			return nil, err
		}
		if statement != nil {
			block.Statements = appendBodyStatement(block.Statements, statement)
		}
	}
}

func appendBodyStatement(statements []Stmt, statement *statementDraft) []Stmt {
	if statement == nil {
		return statements
	}
	if structured := structuredStatement(statement); structured != nil {
		return append(statements, structured)
	}
	if structured := goStatementFromDraft(statement); structured != nil {
		return append(statements, structured)
	}
	return append(statements, tokenStmtFromDraft(statement))
}

func tokenStmtFromDraft(statement *statementDraft) *TokenStmt {
	if statement == nil {
		return nil
	}
	children := make([]Stmt, 0, len(statement.Children))
	for _, child := range statement.Children {
		if child == nil {
			continue
		}
		if structured := structuredStatement(child); structured != nil {
			children = append(children, structured)
		} else {
			children = append(children, tokenStmtFromDraft(child))
		}
	}
	return &TokenStmt{
		Kind:      statement.Kind,
		Tokens:    append([]Token(nil), statement.Header...),
		Exprs:     append([]ExprNode(nil), statement.Exprs...),
		Body:      statement.Body,
		Children:  children,
		SpanValue: statement.SpanValue,
	}
}

func (parser *bodyParser) parseStatement() (*statementDraft, error) {
	start := parser.nextSignificant()
	if start >= len(parser.tokens) || parser.tokens[start].Kind == TokenEOF {
		return nil, nil
	}
	parser.index = start

	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	header := []Token{}
	for parser.index < len(parser.tokens) {
		token := parser.tokens[parser.index]
		if token.Kind == TokenEOF {
			break
		}
		if token.Kind == TokenNewline || (token.Kind == TokenPunctuation && token.Text == ";") {
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 && !(token.Text == ";" && blockHeader(header)) {
				parser.index++
				return newStatementDraft(header, nil, nil), nil
			}
			header = append(header, token)
			parser.index++
			continue
		}
		if token.Kind == TokenPunctuation {
			switch token.Text {
			case "(":
				parenDepth++
			case ")":
				parenDepth--
			case "[":
				bracketDepth++
			case "]":
				bracketDepth--
			case "{":
				if !blockHeader(header) || parenDepth != 0 || bracketDepth != 0 {
					braceDepth++
					header = append(header, token)
					parser.index++
					continue
				}
			case "}":
				if braceDepth > 0 {
					// Braces belonging to a composite or function literal can
					// appear inside parentheses, so they must close before the
					// surrounding call's paren depth is considered.
					braceDepth--
					header = append(header, token)
					parser.index++
					continue
				}
				if parenDepth == 0 && bracketDepth == 0 {
					if len(header) == 0 {
						return nil, fmt.Errorf("unexpected closing body brace")
					}
					return newStatementDraft(header, nil, nil), nil
				}
			}
		}
		if token.Kind == TokenPunctuation && token.Text == "{" && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
			openSpan := token.Span
			parser.index++
			var body *BlockStmt
			var err error
			if statementKind(header) == BodyStmtSwitch {
				body, err = parser.parseClauseBlock()
			} else {
				body, err = parser.parseBlock(true)
			}
			if err != nil {
				return nil, err
			}
			body.Open = openSpan
			statement := newStatementDraft(header, body, nil)
			if len(significantSyntaxTokens(header)) == 0 {
				statement.Kind = BodyStmtBlock
				return statement, nil
			}
			if statement.Kind == BodyStmtTry {
				if err := parser.parseTryClauses(statement); err != nil {
					return nil, err
				}
			} else if statement.Kind == BodyStmtIf {
				if child, err := parser.parseOptionalClause("else"); err != nil {
					return nil, err
				} else if child != nil {
					statement.Children = append(statement.Children, child)
					if statement.SpanValue.End < child.SpanValue.End {
						statement.SpanValue.End = child.SpanValue.End
					}
				}
			}
			return statement, nil
		}
		header = append(header, token)
		parser.index++
	}
	return newStatementDraft(header, nil, nil), nil
}

func (parser *bodyParser) parseTryClauses(statement *statementDraft) error {
	for {
		keyword := parser.peekKeyword()
		if keyword != "catch" && keyword != "finally" {
			return nil
		}
		start := parser.nextSignificant()
		parser.index = start + 1
		header := []Token{parser.tokens[start]}
		for parser.index < len(parser.tokens) {
			token := parser.tokens[parser.index]
			if token.Kind == TokenPunctuation && token.Text == "{" {
				openSpan := token.Span
				parser.index++
				body, err := parser.parseBlock(true)
				if err != nil {
					return err
				}
				body.Open = openSpan
				clause := newStatementDraft(header, body, nil)
				if statement.SpanValue.End < clause.SpanValue.End {
					statement.SpanValue.End = clause.SpanValue.End
				}
				if keyword == "finally" {
					statement.Children = append(statement.Children, clause)
					return nil
				}
				statement.Children = append(statement.Children, clause)
				break
			}
			if token.Kind == TokenNewline {
				parser.index++
				continue
			}
			header = append(header, token)
			parser.index++
		}
	}
}

func statementKind(header []Token) BodyStmtKind {
	first := firstSyntaxToken(header)
	if first == nil {
		return BodyStmtUnknown
	}
	kind := syntaxStmtKind(first.Text)
	if kind == BodyStmtExpression && isLabelHeader(header) {
		return BodyStmtLabel
	}
	return kind
}

func isLabelHeader(tokens []Token) bool {
	clean := significantSyntaxTokens(tokens)
	return len(clean) == 2 && clean[0].Kind == TokenIdentifier && clean[1].Text == ":"
}

func (parser *bodyParser) parseClauseBlock() (*BlockStmt, error) {
	block := &BlockStmt{}
	start := parser.nextSignificant()
	if start < len(parser.tokens) {
		block.SpanValue.Start = parser.tokens[start].Span.Start
		block.SpanValue.Line = parser.tokens[start].Span.Line
		block.SpanValue.Column = parser.tokens[start].Span.Column
	}
	for {
		parser.consumeSeparators(block)
		if parser.index >= len(parser.tokens) || parser.tokens[parser.index].Kind == TokenEOF {
			return nil, fmt.Errorf("unterminated selection block")
		}
		if parser.isPunctuation(parser.index, '}') {
			block.Close = parser.tokens[parser.index].Span
			parser.index++
			block.SpanValue.End = block.Close.End
			return block, nil
		}
		if parser.peekKeyword() != "case" && parser.peekKeyword() != "default" {
			return nil, fmt.Errorf("selection block requires case or default clause")
		}
		clause, err := parser.parseCaseClause()
		if err != nil {
			return nil, err
		}
		block.Statements = append(block.Statements, &CaseStmt{Clause: *clause, SpanValue: clause.SpanValue})
	}
}

func (parser *bodyParser) parseCaseClause() (*CaseClause, error) {
	start := parser.nextSignificant()
	parser.index = start
	defaultClause := parser.tokens[start].Text == "default"
	parser.index++
	header := []Token{}
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for parser.index < len(parser.tokens) {
		token := parser.tokens[parser.index]
		if token.Kind == TokenPunctuation {
			switch token.Text {
			case "(":
				parenDepth++
			case ")":
				parenDepth--
			case "[":
				bracketDepth++
			case "]":
				bracketDepth--
			case "{":
				braceDepth++
			case "}":
				braceDepth--
			case ":":
				if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
					parser.index++
					body, err := parser.parseClauseStatements()
					if err != nil {
						return nil, err
					}
					clause := &CaseClause{Default: defaultClause, Body: body}
					if !defaultClause {
						for _, part := range splitExpressionTokens(header) {
							expression, parseErr := ParseExpressionTokens(part)
							if parseErr != nil {
								return nil, parseErr
							}
							if expression != nil {
								clause.Expressions = append(clause.Expressions, expression)
							}
						}
					}
					clause.SpanValue.Start = parser.tokens[start].Span.Start
					clause.SpanValue.Line = parser.tokens[start].Span.Line
					clause.SpanValue.Column = parser.tokens[start].Span.Column
					clause.SpanValue.End = body.SpanValue.End
					return clause, nil
				}
			}
		}
		header = append(header, token)
		parser.index++
	}
	return nil, fmt.Errorf("case clause is missing a colon")
}

func (parser *bodyParser) parseClauseStatements() (*BlockStmt, error) {
	block := &BlockStmt{}
	start := parser.nextSignificant()
	if start < len(parser.tokens) {
		block.SpanValue.Start = parser.tokens[start].Span.Start
		block.SpanValue.Line = parser.tokens[start].Span.Line
		block.SpanValue.Column = parser.tokens[start].Span.Column
	}
	for {
		parser.consumeSeparators(block)
		if parser.index >= len(parser.tokens) || parser.tokens[parser.index].Kind == TokenEOF {
			return nil, fmt.Errorf("unterminated case clause")
		}
		if parser.isPunctuation(parser.index, '}') || parser.peekKeyword() == "case" || parser.peekKeyword() == "default" {
			if len(block.Statements) > 0 {
				block.SpanValue.End = block.Statements[len(block.Statements)-1].Span().End
			}
			return block, nil
		}
		statement, err := parser.parseStatement()
		if err != nil {
			return nil, err
		}
		if statement != nil {
			block.Statements = appendBodyStatement(block.Statements, statement)
		}
	}
}

func (parser *bodyParser) parseOptionalClause(keyword string) (*statementDraft, error) {
	if parser.peekKeyword() != keyword {
		return nil, nil
	}
	start := parser.nextSignificant()
	parser.index = start + 1
	parser.skipSeparators()
	if keyword == "else" && parser.peekKeyword() == "if" {
		header := []Token{parser.tokens[start]}
		for parser.index < len(parser.tokens) {
			token := parser.tokens[parser.index]
			if token.Kind == TokenPunctuation && token.Text == "{" {
				openSpan := token.Span
				parser.index++
				body, err := parser.parseBlock(true)
				if err != nil {
					return nil, err
				}
				body.Open = openSpan
				child := newStatementDraft(header, body, nil)
				child.Kind = BodyStmtIf
				if nested, err := parser.parseOptionalClause("else"); err != nil {
					return nil, err
				} else if nested != nil {
					child.Children = append(child.Children, nested)
					if child.SpanValue.End < nested.SpanValue.End {
						child.SpanValue.End = nested.SpanValue.End
					}
				}
				return child, nil
			}
			if token.Kind == TokenNewline {
				parser.index++
				continue
			}
			header = append(header, token)
			parser.index++
		}
		return nil, fmt.Errorf("else if must be followed by a block")
	}
	if parser.index >= len(parser.tokens) || !parser.isPunctuation(parser.index, '{') {
		return nil, fmt.Errorf("%s must be followed by a block", keyword)
	}
	openSpan := parser.tokens[parser.index].Span
	parser.index++
	body, err := parser.parseBlock(true)
	if err != nil {
		return nil, err
	}
	body.Open = openSpan
	return newStatementDraft(nil, body, nil), nil
}

func newStatementDraft(header []Token, body *BlockStmt, children []*statementDraft) *statementDraft {
	statement := &statementDraft{Header: header, Body: body, Children: children}
	if token := firstSyntaxToken(header); token != nil {
		statement.Kind = statementKind(header)
		if statement.Kind == BodyStmtExpression && hasTopLevelToken(header, ":=") {
			statement.Kind = BodyStmtDeclaration
		}
		statement.SpanValue.Start = token.Span.Start
		statement.SpanValue.Line = token.Span.Line
		statement.SpanValue.Column = token.Span.Column
	}
	if body != nil {
		if len(header) == 0 {
			statement.SpanValue.Start = body.SpanValue.Start
			statement.SpanValue.Line = body.SpanValue.Line
			statement.SpanValue.Column = body.SpanValue.Column
		}
		statement.SpanValue.End = body.SpanValue.End
	} else if len(header) > 0 {
		statement.SpanValue.End = header[len(header)-1].Span.End
	}
	statement.Exprs = parseStatementExpressions(statement.Kind, header)
	return statement
}

func structuredStatement(statement *statementDraft) Stmt {
	if statement == nil {
		return nil
	}
	switch statement.Kind {
	case BodyStmtDeclaration:
		return &DeclarationStmt{
			Keyword:   declarationKeyword(statement.Header),
			Names:     declarationNames(statement.Header),
			Type:      declarationType(statement.Header),
			Values:    statement.Exprs,
			SpanValue: statement.SpanValue,
		}
	case BodyStmtTypeDeclaration:
		if result := typeDeclarationStatement(statement.Header, statement.SpanValue); result != nil {
			return result
		}
		return goStatementFromDraft(statement)
	case BodyStmtReturn:
		return &ReturnStmt{Values: statement.Exprs, SpanValue: statement.SpanValue}
	case BodyStmtThrow:
		var value ExprNode
		if len(statement.Exprs) > 0 {
			value = statement.Exprs[0]
		}
		return &ThrowStmt{Value: value, SpanValue: statement.SpanValue}
	case BodyStmtBranch:
		return &BranchStmt{Keyword: firstSyntaxText(statement.Header), Target: significantSyntaxTokens(statement.Header[1:]), SpanValue: statement.SpanValue}
	case BodyStmtDefer:
		var expression ExprNode
		if len(statement.Exprs) > 0 {
			expression = statement.Exprs[0]
		}
		return &DeferStmt{Expression: expression, SpanValue: statement.SpanValue}
	case BodyStmtGo:
		var expression ExprNode
		if len(statement.Exprs) > 0 {
			expression = statement.Exprs[0]
		}
		return &GoStmt{Expression: expression, SpanValue: statement.SpanValue}
	case BodyStmtExpression:
		if send := sendStatement(statement.Header, statement.SpanValue); send != nil {
			return send
		}
		if assignment := assignmentStatement(statement.Header, statement.SpanValue); assignment != nil {
			return assignment
		}
		if len(statement.Exprs) == 1 {
			if postfix, ok := statement.Exprs[0].(*PostfixExpr); ok {
				return &IncDecStmt{Expression: postfix.Expression, Operator: postfix.Operator, SpanValue: statement.SpanValue}
			}
			return &ExpressionStmt{Expression: statement.Exprs[0], SpanValue: statement.SpanValue}
		}
		if fallback := goStatementFromDraft(statement); fallback != nil {
			return fallback
		}
	case BodyStmtLabel:
		return labelStatement(statement.Header, statement.SpanValue)
	case BodyStmtBlock:
		return statement.Body
	case BodyStmtIf:
		init, condition := statementConditionParts(statement.Header)
		if condition == nil && len(significantSyntaxTokens(statement.Header)) > 1 {
			if fallback := goStatementFromDraft(statement); fallback != nil {
				return fallback
			}
		}
		var elseBody *BlockStmt
		var elseIf *IfStmt
		if len(statement.Children) > 0 {
			if nested, ok := structuredStatement(statement.Children[0]).(*IfStmt); ok {
				elseIf = nested
			} else {
				elseBody = statement.Children[0].Body
			}
		}
		return &IfStmt{Init: init, Condition: condition, Body: statement.Body, Else: elseBody, ElseIf: elseIf, SpanValue: statement.SpanValue}
	case BodyStmtTry:
		result := &TryStmt{Body: statement.Body, SpanValue: statement.SpanValue}
		for _, child := range statement.Children {
			switch child.Kind {
			case BodyStmtCatch:
				result.Catches = append(result.Catches, astCatchClause(child))
			case BodyStmtFinally:
				result.Finally = child.Body
			}
		}
		return result
	case BodyStmtFor:
		result := structuredForStatement(statement)
		clean := significantSyntaxTokens(statement.Header)
		if len(clean) > 1 && result.Init == nil && result.Condition == nil && result.Post == nil && result.RangeExpr == nil {
			if fallback := goStatementFromDraft(statement); fallback != nil {
				return fallback
			}
		}
		return result
	case BodyStmtSwitch:
		clean := significantSyntaxTokens(statement.Header)
		selectStatement := len(clean) > 0 && clean[0].Text == "select"
		var init ExprNode
		var tag ExprNode
		if len(clean) > 1 {
			parts := splitStatementSeparators(clean[1:], ";")
			if len(parts) > 1 {
				init, _ = ParseExpressionTokens(parts[0])
				tag, _ = ParseExpressionTokens(parts[1])
			} else {
				tag, _ = parseSimpleStatementTokens(clean[1:])
			}
		}
		if len(clean) > 1 && !selectStatement && tag == nil {
			if fallback := goStatementFromDraft(statement); fallback != nil {
				return fallback
			}
		}
		if assignment, ok := tag.(*AssignmentExpr); ok && isTypeSwitchAssignment(assignment) {
			return &SwitchStmt{Init: assignment, Body: statement.Body, SpanValue: statement.SpanValue}
		}
		return &SwitchStmt{Select: selectStatement, Init: init, Tag: tag, Body: statement.Body, SpanValue: statement.SpanValue}
	}
	return nil
}

func isTypeSwitchAssertion(expression ExprNode) bool {
	assertion, ok := expression.(*TypeAssertExpr)
	return ok && assertion.TypeSwitch
}

func sendStatement(tokens []Token, span Span) *SendStmt {
	clean := significantSyntaxTokens(tokens)
	operator := topLevelToken(clean, "<-")
	if operator <= 0 || operator+1 >= len(clean) {
		return nil
	}
	channel, channelErr := ParseExpressionTokens(clean[:operator])
	value, valueErr := ParseExpressionTokens(clean[operator+1:])
	if channelErr != nil || valueErr != nil || channel == nil || value == nil {
		return nil
	}
	return &SendStmt{Channel: channel, Value: value, SpanValue: span}
}

func typeDeclarationStatement(tokens []Token, span Span) *TypeDeclarationStmt {
	clean := significantSyntaxTokens(tokens)
	if len(clean) < 3 || clean[0].Text != "type" {
		return nil
	}
	name := clean[1]
	if name.Kind != TokenIdentifier && name.Kind != TokenKeyword {
		return nil
	}
	typeTokens := clean[2:]
	alias := false
	if len(typeTokens) > 0 && typeTokens[0].Text == "=" {
		alias = true
		typeTokens = typeTokens[1:]
	}
	typeNode, err := ParseTypeTokens(typeTokens)
	if err != nil || typeNode == nil {
		return nil
	}
	return &TypeDeclarationStmt{Name: name.Text, Alias: alias, Type: typeNode, SpanValue: span}
}

func assignmentStatement(tokens []Token, span Span) *AssignmentStmt {
	clean := significantSyntaxTokens(tokens)
	operator := topLevelAssignment(clean)
	if operator < 0 || operator >= len(clean) {
		return nil
	}
	left := []ExprNode{}
	for _, part := range splitStatementSeparators(clean[:operator], ",") {
		expression, err := ParseExpressionTokens(part)
		if err != nil || expression == nil {
			return nil
		}
		left = append(left, expression)
	}
	right := []ExprNode{}
	for _, part := range splitStatementSeparators(clean[operator+1:], ",") {
		expression, err := ParseExpressionTokens(part)
		if err != nil || expression == nil {
			return nil
		}
		right = append(right, expression)
	}
	return &AssignmentStmt{Left: left, Operator: clean[operator].Text, Right: right, SpanValue: span}
}

func labelStatement(tokens []Token, span Span) *LabelStmt {
	clean := significantSyntaxTokens(tokens)
	if isLabelHeader(tokens) {
		return &LabelStmt{Name: clean[0].Text, SpanValue: span}
	}
	return nil
}

func declarationKeyword(tokens []Token) string {
	first := firstSyntaxToken(tokens)
	if first != nil && (first.Text == "var" || first.Text == "let" || first.Text == "const") {
		return first.Text
	}
	return ":="
}

func declarationNames(tokens []Token) []Token {
	clean := significantSyntaxTokens(tokens)
	if len(clean) > 0 && (clean[0].Text == "var" || clean[0].Text == "let" || clean[0].Text == "const") {
		clean = clean[1:]
	}
	assignment := topLevelAssignment(clean)
	if assignment < 0 {
		assignment = len(clean)
	}
	left := clean[:assignment]
	result := []Token{}
	for _, part := range splitStatementSeparators(left, ",") {
		if len(part) > 0 && (part[0].Kind == TokenIdentifier || part[0].Kind == TokenKeyword) {
			result = append(result, part[0])
		}
	}
	return result
}

func declarationType(tokens []Token) TypeNode {
	clean := significantSyntaxTokens(tokens)
	if len(clean) == 0 || (clean[0].Text != "var" && clean[0].Text != "let" && clean[0].Text != "const") {
		return nil
	}
	assignment := topLevelAssignment(clean)
	if assignment < 0 {
		assignment = len(clean)
	}
	left := clean[1:assignment]
	for index := 1; index < len(left); index++ {
		validNames := true
		for _, name := range splitStatementSeparators(left[:index], ",") {
			if len(name) != 1 || (name[0].Kind != TokenIdentifier && name[0].Kind != TokenKeyword) {
				validNames = false
				break
			}
		}
		if !validNames {
			continue
		}
		if typeNode, err := ParseTypeTokens(left[index:]); err == nil && typeNode != nil {
			return typeNode
		}
	}
	return nil
}

func firstSyntaxText(tokens []Token) string {
	if token := firstSyntaxToken(tokens); token != nil {
		return token.Text
	}
	return ""
}

func statementCondition(tokens []Token) ExprNode {
	_, condition := statementConditionParts(tokens)
	return condition
}

func statementConditionParts(tokens []Token) (ExprNode, ExprNode) {
	clean := significantSyntaxTokens(tokens)
	if len(clean) < 2 {
		return nil, nil
	}
	start := 1
	if clean[0].Text == "else" && len(clean) > 1 && clean[1].Text == "if" {
		start = 2
	}
	parts := splitStatementSeparators(clean[start:], ";")
	if len(parts) > 1 {
		init, _ := parseSimpleStatementTokens(parts[0])
		condition, _ := ParseExpressionTokens(parts[1])
		return init, condition
	}
	condition, _ := ParseExpressionTokens(clean[start:])
	return nil, condition
}

func structuredForStatement(statement *statementDraft) *ForStmt {
	clean := significantSyntaxTokens(statement.Header)
	result := &ForStmt{Body: statement.Body, SpanValue: statement.SpanValue}
	if len(clean) <= 1 {
		return result
	}
	clean = clean[1:]
	if rangeIndex := topLevelToken(clean, "range"); rangeIndex >= 0 {
		bindingTokens := clean[:rangeIndex]
		if assignment := topLevelAssignment(bindingTokens); assignment >= 0 {
			result.RangeOperator = bindingTokens[assignment].Text
			bindingTokens = bindingTokens[:assignment]
		}
		for _, part := range splitStatementSeparators(bindingTokens, ",") {
			if expression, err := ParseExpressionTokens(part); err == nil && expression != nil {
				result.RangeKey = append(result.RangeKey, expression)
			}
		}
		if rangeIndex+1 < len(clean) {
			result.RangeExpr, _ = ParseExpressionTokens(clean[rangeIndex+1:])
		}
		return result
	}
	parts := splitStatementSeparators(clean, ";")
	if len(parts) == 1 {
		result.Condition, _ = ParseExpressionTokens(parts[0])
		return result
	}
	if len(parts) > 0 {
		result.Init, _ = parseSimpleStatementTokens(parts[0])
	}
	if len(parts) > 1 {
		result.Condition, _ = ParseExpressionTokens(parts[1])
	}
	if len(parts) > 2 {
		result.Post, _ = ParseExpressionTokens(parts[2])
	}
	return result
}

func parseSimpleStatementTokens(tokens []Token) (ExprNode, error) {
	clean := significantSyntaxTokens(tokens)
	if assignment := topLevelAssignment(clean); assignment >= 0 {
		leftParts := splitStatementSeparators(clean[:assignment], ",")
		rightParts := splitStatementSeparators(clean[assignment+1:], ",")
		if len(leftParts) == 0 || len(rightParts) == 0 {
			return nil, fmt.Errorf("invalid assignment statement")
		}
		left := make([]ExprNode, 0, len(leftParts))
		for _, part := range leftParts {
			expression, err := ParseExpressionTokens(part)
			if err != nil || expression == nil {
				return nil, fmt.Errorf("invalid assignment target")
			}
			left = append(left, expression)
		}
		right := make([]ExprNode, 0, len(rightParts))
		for _, part := range rightParts {
			expression, err := ParseExpressionTokens(part)
			if err != nil || expression == nil {
				return nil, fmt.Errorf("invalid assignment value")
			}
			right = append(right, expression)
		}
		return &AssignmentExpr{Left: left, Operator: clean[assignment].Text, Right: right, SpanValue: tokenSpan(clean)}, nil
	}
	return ParseExpressionTokens(clean)
}

func splitStatementSeparators(tokens []Token, separator string) [][]Token {
	parts := [][]Token{}
	start := 0
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for index, token := range tokens {
		if token.Kind == TokenPunctuation {
			switch token.Text {
			case "(":
				parenDepth++
			case ")":
				parenDepth--
			case "[":
				bracketDepth++
			case "]":
				bracketDepth--
			case "{":
				braceDepth++
			case "}":
				braceDepth--
			}
		}
		if token.Text == separator && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
			parts = append(parts, tokens[start:index])
			start = index + 1
		}
	}
	parts = append(parts, tokens[start:])
	return parts
}

func astCatchClause(statement *statementDraft) CatchClause {
	clean := significantSyntaxTokens(statement.Header)
	if len(clean) == 0 || clean[0].Text != "catch" {
		return CatchClause{Body: statement.Body, SpanValue: statement.SpanValue}
	}
	clean = clean[1:]
	clause := CatchClause{Body: statement.Body, SpanValue: statement.SpanValue}
	if len(clean) == 0 {
		return clause
	}
	last := clean[len(clean)-1]
	if len(clean) > 1 && (last.Kind == TokenIdentifier || last.Kind == TokenKeyword) && clean[len(clean)-2].Text != "," {
		candidate := clean[:len(clean)-1]
		isMultiType := topLevelToken(candidate, ",") >= 0
		if isMultiType {
			clause.Binding = last.Text
			clean = candidate
		} else if typeNode, err := ParseTypeTokens(candidate); err == nil && typeNode != nil {
			clause.Binding = last.Text
			clean = candidate
		}
	} else if len(clean) == 1 && last.Text != "error" {
		// A single user identifier is ambiguous until semantic context is
		// available. Keep it as a binding; lowerASTTry promotes it to a type
		// when the name resolves to an exception type.
		clause.Binding = last.Text
		return clause
	}
	// A pointer-qualified catch type must be parsed after the optional binding
	// has been removed (`catch *Problem` or `catch *Problem problem`).
	if clean[0].Text == "*" {
		for _, part := range splitExpressionTokens(clean) {
			if typeNode, err := ParseTypeTokens(part); err == nil && typeNode != nil {
				clause.Types = append(clause.Types, typeNode)
				continue
			}
			return clause
		}
		return clause
	}
	for _, part := range splitExpressionTokens(clean) {
		if typeNode, err := ParseTypeTokens(part); err == nil && typeNode != nil {
			clause.Types = append(clause.Types, typeNode)
		}
	}
	return clause
}

func parseStatementExpressions(kind BodyStmtKind, header []Token) []ExprNode {
	tokens := significantSyntaxTokens(header)
	if len(tokens) == 0 {
		return nil
	}

	var expressionTokens []Token
	var expressionParts [][]Token
	preserveExpressionRange := func(start, end int) []Token {
		if start < 0 || end < start || start >= len(tokens) || end >= len(tokens) {
			return nil
		}
		preserved := tokensBetweenSpans(header, tokens[start].Span.Start, tokens[end].Span.End)
		if len(preserved) == 0 {
			return tokens[start : end+1]
		}
		return preserved
	}
	switch kind {
	case BodyStmtReturn, BodyStmtThrow:
		if len(tokens) == 1 {
			return nil
		}
		expressionTokens = preserveExpressionRange(1, len(tokens)-1)
	case BodyStmtDeclaration:
		assignment := topLevelAssignment(tokens)
		if assignment < 0 || assignment+1 >= len(tokens) {
			return nil
		}
		expressionTokens = preserveExpressionRange(assignment+1, len(tokens)-1)
	case BodyStmtDefer, BodyStmtGo:
		if len(tokens) == 1 {
			return nil
		}
		expressionTokens = preserveExpressionRange(1, len(tokens)-1)
	case BodyStmtExpression:
		if send := topLevelToken(tokens, "<-"); send > 0 && send+1 < len(tokens) {
			expressionParts = [][]Token{tokens[:send], tokens[send+1:]}
		} else {
			expressionTokens = preserveExpressionRange(0, len(tokens)-1)
		}
	default:
		return nil
	}

	parts := expressionParts
	if parts == nil {
		parts = splitExpressionTokens(expressionTokens)
	}
	result := make([]ExprNode, 0, len(parts))
	for _, part := range parts {
		expression, err := ParseExpressionTokens(part)
		if err != nil || expression == nil {
			// The token stream remains the authoritative representation until
			// this syntax gets a dedicated expression parser. Do not silently
			// drop an expression: a missing value can make a Go++ return or call
			// appear valid to a later AST emitter.
			preserved := significantSyntaxTokens(part)
			if len(preserved) > 0 {
				result = append(result, &TokenExpr{Tokens: preserved, SpanValue: tokenSpan(preserved)})
			}
			continue
		}
		result = append(result, expression)
	}
	return result
}

func significantSyntaxTokens(tokens []Token) []Token {
	result := make([]Token, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind != TokenComment && token.Kind != TokenNewline && token.Kind != TokenEOF {
			result = append(result, token)
		}
	}
	return result
}

func topLevelAssignment(tokens []Token) int {
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for index, token := range tokens {
		if token.Kind == TokenPunctuation {
			switch token.Text {
			case "(":
				parenDepth++
			case ")":
				parenDepth--
			case "[":
				bracketDepth++
			case "]":
				bracketDepth--
			case "{":
				braceDepth++
			case "}":
				braceDepth--
			}
		}
		if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 && isAssignmentOperator(token.Text) {
			return index
		}
	}
	return -1
}

func isAssignmentOperator(text string) bool {
	switch text {
	case "=", ":=", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<=", ">>=", "??=", "||=":
		return true
	default:
		return false
	}
}

func hasTopLevelToken(tokens []Token, wanted string) bool {
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for _, token := range tokens {
		if token.Kind == TokenPunctuation {
			switch token.Text {
			case "(":
				parenDepth++
			case ")":
				parenDepth--
			case "[":
				bracketDepth++
			case "]":
				bracketDepth--
			case "{":
				braceDepth++
			case "}":
				braceDepth--
			}
		}
		if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 && token.Text == wanted {
			return true
		}
	}
	return false
}

func splitExpressionTokens(tokens []Token) [][]Token {
	parts := [][]Token{}
	start := 0
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for index, token := range tokens {
		if token.Kind == TokenPunctuation {
			switch token.Text {
			case "(":
				parenDepth++
			case ")":
				parenDepth--
			case "[":
				bracketDepth++
			case "]":
				bracketDepth--
			case "{":
				braceDepth++
			case "}":
				braceDepth--
			case ",":
				if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
					if len(significantSyntaxTokens(tokens[start:index])) > 0 {
						parts = append(parts, tokens[start:index])
					}
					start = index + 1
				}
			}
		}
	}
	if len(significantSyntaxTokens(tokens[start:])) > 0 {
		parts = append(parts, tokens[start:])
	}
	return parts
}

func syntaxStmtKind(keyword string) BodyStmtKind {
	switch keyword {
	case "type":
		return BodyStmtTypeDeclaration
	case "var", "let", "const":
		return BodyStmtDeclaration
	case "return":
		return BodyStmtReturn
	case "throw":
		return BodyStmtThrow
	case "if":
		return BodyStmtIf
	case "for":
		return BodyStmtFor
	case "switch", "select":
		return BodyStmtSwitch
	case "try":
		return BodyStmtTry
	case "catch":
		return BodyStmtCatch
	case "finally":
		return BodyStmtFinally
	case "defer":
		return BodyStmtDefer
	case "go":
		return BodyStmtGo
	case "break", "continue", "fallthrough", "goto":
		return BodyStmtBranch
	default:
		return BodyStmtExpression
	}
}

func blockHeader(tokens []Token) bool {
	first := firstSyntaxToken(tokens)
	if first == nil {
		return true
	}
	switch first.Text {
	case "if", "for", "switch", "select", "try", "else", "catch", "finally":
		return true
	default:
		return false
	}
}

func firstSyntaxToken(tokens []Token) *Token {
	for index := range tokens {
		if tokens[index].Kind != TokenComment && tokens[index].Kind != TokenNewline {
			return &tokens[index]
		}
	}
	return nil
}

func (parser *bodyParser) nextSignificant() int {
	index := parser.index
	for index < len(parser.tokens) && (parser.tokens[index].Kind == TokenNewline || parser.tokens[index].Kind == TokenComment) {
		index++
	}
	return index
}

func (parser *bodyParser) skipSeparators() {
	parser.index = parser.nextSignificant()
}

func (parser *bodyParser) consumeSeparators(block *BlockStmt) {
	for parser.index < len(parser.tokens) {
		token := parser.tokens[parser.index]
		if token.Kind == TokenNewline {
			parser.index++
			continue
		}
		if token.Kind == TokenComment {
			block.Comments = append(block.Comments, token)
			parser.index++
			continue
		}
		return
	}
}

func (parser *bodyParser) peekKeyword() string {
	index := parser.nextSignificant()
	if index >= len(parser.tokens) || parser.tokens[index].Kind != TokenKeyword {
		return ""
	}
	return parser.tokens[index].Text
}

func (parser *bodyParser) isPunctuation(index int, text byte) bool {
	return index < len(parser.tokens) && parser.tokens[index].Kind == TokenPunctuation && parser.tokens[index].Text == string(text)
}

func blockEnd(block *BlockStmt, tokens []Token) int {
	if block == nil || len(tokens) == 0 {
		return 0
	}
	return tokens[len(tokens)-1].Span.End
}
