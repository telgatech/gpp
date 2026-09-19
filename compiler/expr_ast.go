package compiler

import (
	"fmt"
	"strings"
)

type ExprNode interface {
	Node
	expr()
}

type NameExpr struct {
	Name      string
	SpanValue Span
}

func (*NameExpr) node()                 {}
func (*NameExpr) expr()                 {}
func (expression *NameExpr) Span() Span { return expression.SpanValue }

type LiteralExpr struct {
	Text      string
	Kind      TokenKind
	SpanValue Span
}

func (*LiteralExpr) node()                 {}
func (*LiteralExpr) expr()                 {}
func (expression *LiteralExpr) Span() Span { return expression.SpanValue }

type StringSegment struct {
	Text             string
	Expression       ExprNode
	ExpressionTokens []Token
	Format           string
}

type InterpolatedStringExpr struct {
	Raw       bool
	Segments  []StringSegment
	SpanValue Span
}

func (*InterpolatedStringExpr) node()                 {}
func (*InterpolatedStringExpr) expr()                 {}
func (expression *InterpolatedStringExpr) Span() Span { return expression.SpanValue }

type UnaryExpr struct {
	Operator  string
	Operand   ExprNode
	SpanValue Span
}

func (*UnaryExpr) node()                 {}
func (*UnaryExpr) expr()                 {}
func (expression *UnaryExpr) Span() Span { return expression.SpanValue }

type BinaryExpr struct {
	Left      ExprNode
	Operator  string
	Right     ExprNode
	SpanValue Span
}

func (*BinaryExpr) node()                 {}
func (*BinaryExpr) expr()                 {}
func (expression *BinaryExpr) Span() Span { return expression.SpanValue }

type SelectorExpr struct {
	Receiver  ExprNode
	Name      string
	Safe      bool
	SpanValue Span
}

func (*SelectorExpr) node()                 {}
func (*SelectorExpr) expr()                 {}
func (expression *SelectorExpr) Span() Span { return expression.SpanValue }

type IndexExpr struct {
	Receiver  ExprNode
	Index     ExprNode
	SpanValue Span
}

func (*IndexExpr) node()                 {}
func (*IndexExpr) expr()                 {}
func (expression *IndexExpr) Span() Span { return expression.SpanValue }

type IndexListExpr struct {
	Receiver  ExprNode
	Indices   []ExprNode
	SpanValue Span
}

func (*IndexListExpr) node()                 {}
func (*IndexListExpr) expr()                 {}
func (expression *IndexListExpr) Span() Span { return expression.SpanValue }

type SliceExpr struct {
	Receiver  ExprNode
	Low       ExprNode
	High      ExprNode
	Max       ExprNode
	SpanValue Span
}

func (*SliceExpr) node()                 {}
func (*SliceExpr) expr()                 {}
func (expression *SliceExpr) Span() Span { return expression.SpanValue }

type TypeAssertExpr struct {
	Expression ExprNode
	Type       TypeNode
	TypeSwitch bool
	SpanValue  Span
}

func (*TypeAssertExpr) node()                 {}
func (*TypeAssertExpr) expr()                 {}
func (expression *TypeAssertExpr) Span() Span { return expression.SpanValue }

type PostfixExpr struct {
	Expression ExprNode
	Operator   string
	SpanValue  Span
}

func (*PostfixExpr) node()                 {}
func (*PostfixExpr) expr()                 {}
func (expression *PostfixExpr) Span() Span { return expression.SpanValue }

// SpreadExpr represents a variadic argument such as args... . The expansion
// marker is part of the call argument syntax, but keeping it as a node makes
// the expression lossless for lowering and diagnostics.
type SpreadExpr struct {
	Expression ExprNode
	SpanValue  Span
}

func (*SpreadExpr) node()                 {}
func (*SpreadExpr) expr()                 {}
func (expression *SpreadExpr) Span() Span { return expression.SpanValue }

type TypeExpr struct {
	Type      TypeNode
	SpanValue Span
}

func (*TypeExpr) node()                 {}
func (*TypeExpr) expr()                 {}
func (expression *TypeExpr) Span() Span { return expression.SpanValue }

type SendExpr struct {
	Channel   ExprNode
	Value     ExprNode
	SpanValue Span
}

func (*SendExpr) node()                 {}
func (*SendExpr) expr()                 {}
func (expression *SendExpr) Span() Span { return expression.SpanValue }

type FunctionLiteralExpr struct {
	Type       *FunctionType
	Body       *BlockStmt
	BodyTokens []Token
	SpanValue  Span
}

func (*FunctionLiteralExpr) node()                 {}
func (*FunctionLiteralExpr) expr()                 {}
func (expression *FunctionLiteralExpr) Span() Span { return expression.SpanValue }

type CallArg struct {
	Name  string
	Value ExprNode
}

type CallExpr struct {
	Callee    ExprNode
	Arguments []CallArg
	SpanValue Span
}

func (*CallExpr) node()                 {}
func (*CallExpr) expr()                 {}
func (expression *CallExpr) Span() Span { return expression.SpanValue }

type ParenthesizedExpr struct {
	Inner     ExprNode
	SpanValue Span
}

func (*ParenthesizedExpr) node()                 {}
func (*ParenthesizedExpr) expr()                 {}
func (expression *ParenthesizedExpr) Span() Span { return expression.SpanValue }

type CompositeElement struct {
	Key   ExprNode
	Value ExprNode
}

type CompositeLiteralExpr struct {
	Type      TypeNode
	Elements  []CompositeElement
	SpanValue Span
}

func (*CompositeLiteralExpr) node()                 {}
func (*CompositeLiteralExpr) expr()                 {}
func (expression *CompositeLiteralExpr) Span() Span { return expression.SpanValue }

type LambdaExpr struct {
	Parameters []Token
	Body       ExprNode
	BlockBody  *BlockStmt
	BodyTokens []Token
	SpanValue  Span
}

func (*LambdaExpr) node()                 {}
func (*LambdaExpr) expr()                 {}
func (expression *LambdaExpr) Span() Span { return expression.SpanValue }

// TokenExpr is a lossless syntax fallback for an expression that has not yet
// been given a dedicated Go++ node. It is still structured token data, never a
// raw executable string, and is intended to disappear as expression coverage
// expands.
type TokenExpr struct {
	Tokens    []Token
	SpanValue Span
}

func (*TokenExpr) node()                 {}
func (*TokenExpr) expr()                 {}
func (expression *TokenExpr) Span() Span { return expression.SpanValue }

func ParseExpressionTokens(tokens []Token) (ExprNode, error) {
	filtered := make([]Token, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind != TokenComment && token.Kind != TokenNewline && token.Kind != TokenEOF {
			filtered = append(filtered, token)
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	if send := topLevelToken(filtered, "<-"); send > 0 && send+1 < len(filtered) {
		channel, channelErr := ParseExpressionTokens(filtered[:send])
		value, valueErr := ParseExpressionTokens(filtered[send+1:])
		if channelErr == nil && valueErr == nil && channel != nil && value != nil {
			return &SendExpr{Channel: channel, Value: value, SpanValue: tokenSpan(filtered)}, nil
		}
		// A receive expression can contain `<-` after an assignment or other
		// operator, as in `err := <-done`. In that case the prefix parser below
		// must handle the unary receive instead of reporting a malformed send.
	}
	if arrow := topLevelToken(filtered, "=>"); arrow >= 0 {
		parameters := filtered[:arrow]
		if len(parameters) >= 2 && parameters[0].Text == "(" && parameters[len(parameters)-1].Text == ")" {
			parameters = parameters[1 : len(parameters)-1]
		}
		bodyTokens := filtered[arrow+1:]
		if len(bodyTokens) >= 2 && bodyTokens[0].Text == "{" && bodyTokens[len(bodyTokens)-1].Text == "}" {
			bodySourceTokens := tokensBetweenSpans(tokens, bodyTokens[0].Span.Start, bodyTokens[len(bodyTokens)-1].Span.End)
			body, bodyErr := ParseBodyAST(bodySourceTokens[1 : len(bodySourceTokens)-1])
			if bodyErr == nil {
				body.Open = bodyTokens[0].Span
				body.Close = bodyTokens[len(bodyTokens)-1].Span
				body.SpanValue = spanFrom(body.Open, body.Close)
				return &LambdaExpr{Parameters: parameters, BlockBody: body, BodyTokens: bodySourceTokens, SpanValue: tokenSpan(filtered)}, nil
			}
		}
		body, err := ParseExpressionTokens(bodyTokens)
		if err != nil {
			return nil, err
		}
		return &LambdaExpr{Parameters: parameters, Body: body, BodyTokens: bodyTokens, SpanValue: tokenSpan(filtered)}, nil
	}
	if filtered[0].Text == "func" && topLevelToken(filtered, "{") > 0 {
		if function, ok, err := parseFunctionLiteral(filtered, tokens); err != nil {
			return nil, err
		} else if ok {
			return function, nil
		}
	}
	if !isUnaryOperator(filtered[0].Text) {
		if composite, ok, err := parseCompositeLiteral(filtered, tokens); err != nil {
			return nil, err
		} else if ok {
			return composite, nil
		}
	}
	parser := expressionParser{tokens: filtered}
	expression, err := parser.parse(0)
	if err != nil {
		return nil, err
	}
	if parser.index != len(parser.tokens) {
		preserved := tokensBetweenSpans(tokens, filtered[0].Span.Start, filtered[len(filtered)-1].Span.End)
		if len(preserved) == 0 {
			preserved = filtered
		}
		return &TokenExpr{Tokens: preserved, SpanValue: tokenSpan(filtered)}, nil
	}
	return expression, nil
}

func parseFunctionLiteral(tokens, sourceTokens []Token) (ExprNode, bool, error) {
	open := topLevelToken(tokens, "{")
	if open <= 0 || len(tokens) == 0 || tokens[len(tokens)-1].Text != "}" {
		return nil, false, nil
	}
	close := matchingBrace(tokens, open)
	if close != len(tokens)-1 {
		return nil, false, nil
	}
	typeNode, err := ParseTypeTokens(tokens[:open])
	if err != nil {
		return nil, false, err
	}
	functionType, ok := typeNode.(*FunctionType)
	if !ok {
		return nil, false, fmt.Errorf("function literal requires a function type")
	}
	bodyTokens := tokensBetweenSpans(sourceTokens, tokens[open].Span.End, tokens[close].Span.Start)
	body, err := ParseBodyAST(bodyTokens)
	if err != nil {
		return nil, false, err
	}
	return &FunctionLiteralExpr{Type: functionType, Body: body, BodyTokens: bodyTokens, SpanValue: tokenSpan(tokens)}, true, nil
}

func tokensBetweenSpans(tokens []Token, start, end int) []Token {
	result := make([]Token, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == TokenEOF {
			continue
		}
		if token.Span.Start >= start && token.Span.End <= end {
			result = append(result, token)
		}
	}
	return result
}

func parseCompositeLiteral(tokens, sourceTokens []Token) (ExprNode, bool, error) {
	open := compositeLiteralOpen(tokens)
	if open < 1 || len(tokens) < 2 || tokens[len(tokens)-1].Text != "}" {
		return nil, false, nil
	}
	typeTokens := tokens[:open]
	if len(typeTokens) > 0 && len(sourceTokens) > 0 {
		preserved := tokensBetweenSpans(sourceTokens, typeTokens[0].Span.Start, typeTokens[len(typeTokens)-1].Span.End)
		if len(preserved) > 0 {
			typeTokens = preserved
		}
	}
	typeNode, err := ParseTypeTokens(typeTokens)
	if err != nil {
		return nil, false, err
	}
	if typeNode == nil {
		return nil, false, nil
	}
	close := matchingBrace(tokens, open)
	if close != len(tokens)-1 {
		return nil, false, nil
	}
	elements := []CompositeElement{}
	for _, part := range splitExpressionTokens(tokens[open+1 : close]) {
		colon := topLevelToken(part, ":")
		if colon > 0 && colon+1 < len(part) {
			key, keyErr := ParseExpressionTokens(part[:colon])
			if keyErr != nil {
				return nil, false, keyErr
			}
			value, valueErr := ParseExpressionTokens(part[colon+1:])
			if valueErr != nil {
				return nil, false, valueErr
			}
			elements = append(elements, CompositeElement{Key: key, Value: value})
			continue
		}
		value, valueErr := ParseExpressionTokens(part)
		if valueErr != nil {
			return nil, false, valueErr
		}
		elements = append(elements, CompositeElement{Value: value})
	}
	return &CompositeLiteralExpr{Type: typeNode, Elements: elements, SpanValue: tokenSpan(tokens)}, true, nil
}

func compositeLiteralOpen(tokens []Token) int {
	if len(tokens) == 0 {
		return -1
	}
	// When parsing a call argument, the remaining token stream may contain
	// later arguments. Do not mistake a composite/function literal in a later
	// argument for a literal whose type starts at the current token.
	comma := topLevelToken(tokens, ",")
	openBrace := topLevelToken(tokens, "{")
	if comma >= 0 && (openBrace < 0 || comma < openBrace) {
		return -1
	}
	if tokens[0].Text != "struct" && tokens[0].Text != "interface" {
		return openBrace
	}
	typeOpen := openBrace
	if typeOpen < 0 {
		return -1
	}
	typeClose := matchingBrace(tokens, typeOpen)
	if typeClose < 0 {
		return -1
	}
	for index := typeClose + 1; index < len(tokens); index++ {
		if tokens[index].Text == "{" {
			return index
		}
	}
	return -1
}

func topLevelToken(tokens []Token, wanted string) int {
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
				if wanted == "{" && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
					return index
				}
				braceDepth++
			case "}":
				braceDepth--
			}
		}
		if token.Text == wanted && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
			return index
		}
	}
	return -1
}

func matchingBrace(tokens []Token, open int) int {
	depth := 0
	for index := open; index < len(tokens); index++ {
		switch tokens[index].Text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

type expressionParser struct {
	tokens []Token
	index  int
}

func (parser *expressionParser) parse(minPrecedence int) (ExprNode, error) {
	left, err := parser.parsePrefix()
	if err != nil {
		return nil, err
	}
	for parser.index < len(parser.tokens) {
		if parser.isPostfixStart() {
			left, err = parser.parsePostfix(left)
			if err != nil {
				return nil, err
			}
			continue
		}
		if operator := parser.tokens[parser.index].Text; operator == "++" || operator == "--" {
			parser.index++
			left = &PostfixExpr{Expression: left, Operator: operator, SpanValue: Span{Start: left.Span().Start, End: parser.tokens[parser.index-1].Span.End, Line: left.Span().Line, Column: left.Span().Column}}
			continue
		}
		operator := parser.tokens[parser.index].Text
		precedence, ok := expressionPrecedence(operator)
		if !ok || precedence < minPrecedence {
			break
		}
		parser.index++
		right, err := parser.parse(precedence + 1)
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Left: left, Operator: operator, Right: right, SpanValue: Span{Start: left.Span().Start, End: right.Span().End, Line: left.Span().Line, Column: left.Span().Column}}
	}
	return left, nil
}

func (parser *expressionParser) parsePrefix() (ExprNode, error) {
	if parser.index >= len(parser.tokens) {
		return nil, fmt.Errorf("expected expression")
	}
	// Composite and function literals can appear as operands, call arguments,
	// or receivers of selectors; they are not limited to being the complete
	// expression passed to ParseExpressionTokens.
	remaining := parser.tokens[parser.index:]
	if open := compositeLiteralOpen(remaining); open > 0 {
		if close := matchingBrace(remaining, open); close >= open {
			candidate := remaining[:close+1]
			var literal ExprNode
			var ok bool
			var err error
			if candidate[0].Text == "func" {
				literal, ok, err = parseFunctionLiteral(candidate, candidate)
			} else if !isUnaryOperator(candidate[0].Text) {
				literal, ok, err = parseCompositeLiteral(candidate, candidate)
			}
			if err != nil {
				return nil, err
			}
			if ok {
				parser.index += close + 1
				return literal, nil
			}
		}
	}
	token := parser.tokens[parser.index]
	if isUnaryOperator(token.Text) {
		parser.index++
		operand, err := parser.parse(10)
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Operator: token.Text, Operand: operand, SpanValue: Span{Start: token.Span.Start, End: operand.Span().End, Line: token.Span.Line, Column: token.Span.Column}}, nil
	}
	if token.Text == "(" {
		parser.index++
		inner, err := parser.parse(0)
		if err != nil {
			return nil, err
		}
		if !parser.take(")") {
			return nil, fmt.Errorf("expected ) in expression")
		}
		return &ParenthesizedExpr{Inner: inner, SpanValue: Span{Start: token.Span.Start, End: parser.tokens[parser.index-1].Span.End, Line: token.Span.Line, Column: token.Span.Column}}, nil
	}
	parser.index++
	if token.Kind == TokenIdentifier || token.Kind == TokenKeyword {
		if token.Text == "true" || token.Text == "false" || token.Text == "nil" {
			return &LiteralExpr{Text: token.Text, Kind: token.Kind, SpanValue: token.Span}, nil
		}
		return &NameExpr{Name: token.Text, SpanValue: token.Span}, nil
	}
	if token.Kind == TokenString || token.Kind == TokenRawString {
		interpolated, err := parseInterpolatedString(token)
		if err != nil {
			return nil, err
		}
		if interpolated != nil {
			return interpolated, nil
		}
		return &LiteralExpr{Text: token.Text, Kind: token.Kind, SpanValue: token.Span}, nil
	}
	if token.Kind == TokenNumber || token.Kind == TokenRune {
		return &LiteralExpr{Text: token.Text, Kind: token.Kind, SpanValue: token.Span}, nil
	}
	return &TokenExpr{Tokens: []Token{token}, SpanValue: token.Span}, nil
}

func parseInterpolatedString(token Token) (ExprNode, error) {
	if len(token.Text) < 2 {
		return nil, nil
	}
	raw := token.Kind == TokenRawString
	content := token.Text[1 : len(token.Text)-1]
	segments := []StringSegment{}
	literal := strings.Builder{}
	changed := false
	flush := func() {
		if literal.Len() == 0 {
			return
		}
		segments = append(segments, StringSegment{Text: literal.String()})
		literal.Reset()
	}
	for index := 0; index < len(content); {
		if strings.HasPrefix(content[index:], "{{{{") {
			literal.WriteString("{{")
			changed = true
			index += 4
			continue
		}
		if strings.HasPrefix(content[index:], "}}}}") {
			literal.WriteString("}}")
			changed = true
			index += 4
			continue
		}
		if !strings.HasPrefix(content[index:], "{{") {
			literal.WriteByte(content[index])
			index++
			continue
		}
		end, err := findInterpolationEnd(content, index+2)
		if err != nil {
			return nil, err
		}
		flush()
		body := content[index+2 : end]
		separator := interpolationFormatSeparator(body)
		expressionText := body
		format := ""
		if separator >= 0 {
			expressionText = body[:separator]
			format = strings.TrimSpace(body[separator+1:])
			if err := validateInterpolationFormat(format); err != nil {
				return nil, err
			}
		}
		expressionText = strings.TrimSpace(expressionText)
		if expressionText == "" {
			return nil, fmt.Errorf("empty interpolation expression")
		}
		expressionTokens, err := LexSource("interpolation", expressionText)
		if err != nil {
			return nil, err
		}
		expression, err := ParseExpressionTokens(expressionTokens)
		if err != nil {
			return nil, err
		}
		segments = append(segments, StringSegment{
			Expression:       expression,
			ExpressionTokens: expressionTokens,
			Format:           format,
		})
		changed = true
		index = end + 2
	}
	flush()
	if !changed {
		return nil, nil
	}
	return &InterpolatedStringExpr{Raw: raw, Segments: segments, SpanValue: token.Span}, nil
}

func expressionNodeSource(expression ExprNode) (string, error) {
	switch value := expression.(type) {
	case nil:
		return "", nil
	case *NameExpr:
		return value.Name, nil
	case *LiteralExpr:
		return value.Text, nil
	case *InterpolatedStringExpr:
		return interpolatedStringSource(value)
	case *UnaryExpr:
		operand, err := expressionNodeSource(value.Operand)
		return value.Operator + operand, err
	case *BinaryExpr:
		left, err := expressionNodeSource(value.Left)
		if err != nil {
			return "", err
		}
		right, err := expressionNodeSource(value.Right)
		return left + " " + value.Operator + " " + right, err
	case *SelectorExpr:
		receiver, err := expressionNodeSource(value.Receiver)
		if err != nil {
			return "", err
		}
		operator := "."
		if value.Safe {
			operator = "?."
		}
		return receiver + operator + value.Name, nil
	case *IndexExpr:
		receiver, err := expressionNodeSource(value.Receiver)
		if err != nil {
			return "", err
		}
		index, err := expressionNodeSource(value.Index)
		return receiver + "[" + index + "]", err
	case *IndexListExpr:
		receiver, err := expressionNodeSource(value.Receiver)
		if err != nil {
			return "", err
		}
		indices := make([]string, 0, len(value.Indices))
		for _, index := range value.Indices {
			text, indexErr := expressionNodeSource(index)
			if indexErr != nil {
				return "", indexErr
			}
			indices = append(indices, text)
		}
		return receiver + "[" + strings.Join(indices, ", ") + "]", nil
	case *SliceExpr:
		receiver, err := expressionNodeSource(value.Receiver)
		if err != nil {
			return "", err
		}
		parts := []string{"", "", ""}
		for index, expression := range []ExprNode{value.Low, value.High, value.Max} {
			if expression == nil {
				continue
			}
			text, expressionErr := expressionNodeSource(expression)
			if expressionErr != nil {
				return "", expressionErr
			}
			parts[index] = text
		}
		if value.Max != nil {
			return receiver + "[" + parts[0] + ":" + parts[1] + ":" + parts[2] + "]", nil
		}
		return receiver + "[" + parts[0] + ":" + parts[1] + "]", nil
	case *TypeAssertExpr:
		receiver, err := expressionNodeSource(value.Expression)
		if err != nil {
			return "", err
		}
		if value.TypeSwitch {
			return receiver + ".(type)", nil
		}
		typeText, err := typeNodeSource(value.Type)
		return receiver + ".(" + typeText + ")", err
	case *PostfixExpr:
		expression, err := expressionNodeSource(value.Expression)
		return expression + value.Operator, err
	case *SpreadExpr:
		expression, err := expressionNodeSource(value.Expression)
		return expression + "...", err
	case *TypeExpr:
		return typeNodeSource(value.Type)
	case *SendExpr:
		channel, err := expressionNodeSource(value.Channel)
		if err != nil {
			return "", err
		}
		valueText, err := expressionNodeSource(value.Value)
		return channel + " <- " + valueText, err
	case *CallExpr:
		callee, err := expressionNodeSource(value.Callee)
		if err != nil {
			return "", err
		}
		arguments := make([]string, 0, len(value.Arguments))
		for _, argument := range value.Arguments {
			text, argumentErr := expressionNodeSource(argument.Value)
			if argumentErr != nil {
				return "", argumentErr
			}
			if argument.Name != "" {
				text = argument.Name + ": " + text
			}
			arguments = append(arguments, text)
		}
		return callee + "(" + strings.Join(arguments, ", ") + ")", nil
	case *ParenthesizedExpr:
		inner, err := expressionNodeSource(value.Inner)
		return "(" + inner + ")", err
	case *CompositeLiteralExpr:
		typeText, err := typeNodeSource(value.Type)
		if err != nil {
			return "", err
		}
		elements := make([]string, 0, len(value.Elements))
		for _, element := range value.Elements {
			text, elementErr := expressionNodeSource(element.Value)
			if elementErr != nil {
				return "", elementErr
			}
			if element.Key != nil {
				key, keyErr := expressionNodeSource(element.Key)
				if keyErr != nil {
					return "", keyErr
				}
				text = key + ": " + text
			}
			elements = append(elements, text)
		}
		return typeText + "{" + strings.Join(elements, ", ") + "}", nil
	case *TokenExpr:
		return tokenExpressionSource(value.Tokens), nil
	case *LambdaExpr:
		parameters := expressionTokensSource(value.Parameters)
		body := expressionTokensSource(value.BodyTokens)
		if value.BlockBody != nil {
			body = blockTokensSource(value.BodyTokens)
		}
		return parameters + " => " + body, nil
	case *FunctionLiteralExpr:
		typeText, err := typeNodeSource(value.Type)
		if err != nil {
			return "", err
		}
		return typeText + " {" + blockTokensSource(value.BodyTokens) + "}", nil
	default:
		return "", fmt.Errorf("unsupported expression AST node %T", expression)
	}
}

func interpolatedStringSource(expression *InterpolatedStringExpr) (string, error) {
	if expression == nil {
		return "", nil
	}
	delimiter := `"`
	if expression.Raw {
		delimiter = "`"
	}
	var result strings.Builder
	result.WriteString(delimiter)
	for _, segment := range expression.Segments {
		if segment.Expression == nil {
			// The parser unescapes doubled interpolation braces into the literal
			// segment. Escape them again so a later interpolation pass does not
			// reinterpret the reconstructed string.
			text := strings.ReplaceAll(segment.Text, "{{", "{{{{")
			text = strings.ReplaceAll(text, "}}", "}}}}")
			result.WriteString(text)
			continue
		}
		result.WriteString("{{")
		text := expressionTokensSource(segment.ExpressionTokens)
		if strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("empty interpolation expression")
		}
		result.WriteString(text)
		if segment.Format != "" {
			result.WriteByte(':')
			result.WriteString(segment.Format)
		}
		result.WriteString("}}")
	}
	result.WriteString(delimiter)
	return result.String(), nil
}

// blockTokensSource preserves statement boundaries when a function literal is
// rendered as an argument of another expression. Compact expression printing
// intentionally drops newlines, but doing that for a block can join two Go
// statements and produce invalid output.
func blockTokensSource(tokens []Token) string {
	var result strings.Builder
	var previous *Token
	for index := range tokens {
		token := tokens[index]
		if token.Kind == TokenEOF {
			continue
		}
		if token.Kind == TokenNewline {
			result.WriteByte('\n')
			previous = nil
			continue
		}
		if previous != nil && expressionTokensNeedSpace(*previous, token) {
			result.WriteByte(' ')
		}
		result.WriteString(token.Text)
		copyToken := token
		previous = &copyToken
	}
	return result.String()
}

func (parser *expressionParser) isPostfixStart() bool {
	if parser.index >= len(parser.tokens) {
		return false
	}
	text := parser.tokens[parser.index].Text
	return text == "." || text == "?." || text == "(" || text == "["
}

func (parser *expressionParser) parsePostfix(left ExprNode) (ExprNode, error) {
	token := parser.tokens[parser.index]
	switch token.Text {
	case ".", "?.":
		parser.index++
		if token.Text == "." && parser.index < len(parser.tokens) && parser.tokens[parser.index].Text == "(" {
			open := parser.index
			close, ok := findExpressionClosing(parser.tokens, open, "(", ")")
			if !ok {
				return nil, fmt.Errorf("unterminated type assertion")
			}
			inside := significantSyntaxTokens(parser.tokens[open+1 : close])
			parser.index = close + 1
			if len(inside) == 1 && inside[0].Text == "type" {
				return &TypeAssertExpr{Expression: left, TypeSwitch: true, SpanValue: Span{Start: left.Span().Start, End: parser.tokens[close].Span.End, Line: left.Span().Line, Column: left.Span().Column}}, nil
			}
			typeNode, err := ParseTypeTokens(inside)
			if err != nil || typeNode == nil {
				return nil, fmt.Errorf("invalid type assertion")
			}
			return &TypeAssertExpr{Expression: left, Type: typeNode, SpanValue: Span{Start: left.Span().Start, End: parser.tokens[close].Span.End, Line: left.Span().Line, Column: left.Span().Column}}, nil
		}
		if parser.index >= len(parser.tokens) || (parser.tokens[parser.index].Kind != TokenIdentifier && parser.tokens[parser.index].Kind != TokenKeyword) {
			return nil, fmt.Errorf("expected selector name")
		}
		name := parser.tokens[parser.index]
		parser.index++
		return &SelectorExpr{Receiver: left, Name: name.Text, Safe: token.Text == "?.", SpanValue: Span{Start: left.Span().Start, End: name.Span.End, Line: left.Span().Line, Column: left.Span().Column}}, nil
	case "[":
		parser.index++
		open := parser.index - 1
		close, ok := findExpressionClosing(parser.tokens, open, "[", "]")
		if !ok {
			return nil, fmt.Errorf("expected ] in expression")
		}
		inside := parser.tokens[open+1 : close]
		colon := topLevelToken(inside, ":")
		if colon >= 0 {
			low, high, max := ExprNode(nil), ExprNode(nil), ExprNode(nil)
			var err error
			if len(significantSyntaxTokens(inside[:colon])) > 0 {
				low, err = ParseExpressionTokens(inside[:colon])
				if err != nil {
					return nil, err
				}
			}
			rest := inside[colon+1:]
			second := topLevelToken(rest, ":")
			if second >= 0 {
				if len(significantSyntaxTokens(rest[:second])) > 0 {
					high, err = ParseExpressionTokens(rest[:second])
					if err != nil {
						return nil, err
					}
				}
				if len(significantSyntaxTokens(rest[second+1:])) > 0 {
					max, err = ParseExpressionTokens(rest[second+1:])
					if err != nil {
						return nil, err
					}
				}
			} else if len(significantSyntaxTokens(rest)) > 0 {
				high, err = ParseExpressionTokens(rest)
				if err != nil {
					return nil, err
				}
			}
			parser.index = close + 1
			return &SliceExpr{Receiver: left, Low: low, High: high, Max: max, SpanValue: Span{Start: left.Span().Start, End: parser.tokens[close].Span.End, Line: left.Span().Line, Column: left.Span().Column}}, nil
		}
		index, err := ParseExpressionTokens(inside)
		if err != nil {
			return nil, err
		}
		parser.index = close + 1
		parts := splitExpressionTokens(inside)
		if len(parts) > 1 {
			indices := make([]ExprNode, 0, len(parts))
			for _, part := range parts {
				item, itemErr := ParseExpressionTokens(part)
				if itemErr != nil || item == nil {
					return nil, fmt.Errorf("invalid generic index argument")
				}
				indices = append(indices, item)
			}
			return &IndexListExpr{Receiver: left, Indices: indices, SpanValue: Span{Start: left.Span().Start, End: parser.tokens[close].Span.End, Line: left.Span().Line, Column: left.Span().Column}}, nil
		}
		return &IndexExpr{Receiver: left, Index: index, SpanValue: Span{Start: left.Span().Start, End: parser.tokens[close].Span.End, Line: left.Span().Line, Column: left.Span().Column}}, nil
	case "(":
		parser.index++
		arguments := []CallArg{}
		for parser.index < len(parser.tokens) && parser.tokens[parser.index].Text != ")" {
			name := ""
			if named, ok := parser.parseCallArgumentName(); ok {
				name = named
			}
			argument, err := parser.parseCallArgument()
			if err != nil {
				return nil, err
			}
			if parser.index < len(parser.tokens) && parser.tokens[parser.index].Text == "..." {
				spreadToken := parser.tokens[parser.index]
				parser.index++
				argument = &SpreadExpr{Expression: argument, SpanValue: Span{Start: argument.Span().Start, End: spreadToken.Span.End, Line: argument.Span().Line, Column: argument.Span().Column}}
			}
			arguments = append(arguments, CallArg{Name: name, Value: argument})
			if !parser.take(",") {
				break
			}
		}
		if !parser.take(")") {
			return nil, fmt.Errorf("expected ) in call")
		}
		return &CallExpr{Callee: left, Arguments: arguments, SpanValue: Span{Start: left.Span().Start, End: parser.tokens[parser.index-1].Span.End, Line: left.Span().Line, Column: left.Span().Column}}, nil
	default:
		return left, nil
	}
}

// parseCallArgumentName accepts both ordinary named arguments (`name: value`)
// and qualified constructor fields (`Parent.name: value`). The latter is
// needed when multiple inherited classes expose the same field name.
func (parser *expressionParser) parseCallArgumentName() (string, bool) {
	start := parser.index
	if start >= len(parser.tokens) || (parser.tokens[start].Kind != TokenIdentifier && parser.tokens[start].Kind != TokenKeyword) {
		return "", false
	}
	index := start + 1
	for index+1 < len(parser.tokens) && parser.tokens[index].Text == "." &&
		(parser.tokens[index+1].Kind == TokenIdentifier || parser.tokens[index+1].Kind == TokenKeyword) {
		index += 2
	}
	if index >= len(parser.tokens) || parser.tokens[index].Text != ":" {
		return "", false
	}
	parts := make([]string, 0, (index-start+1)/2)
	for part := start; part < index; part += 2 {
		parts = append(parts, parser.tokens[part].Text)
	}
	parser.index = index + 1
	return strings.Join(parts, "."), true
}

func (parser *expressionParser) parseCallArgument() (ExprNode, error) {
	start := parser.index
	end := parser.callArgumentEnd(start)
	argumentTokens := significantSyntaxTokens(parser.tokens[start:end])
	if topLevelToken(argumentTokens, "=>") >= 0 {
		argument, err := ParseExpressionTokens(argumentTokens)
		if err != nil {
			return nil, err
		}
		parser.index = end
		return argument, nil
	}
	if end > start && looksLikeTypeExpression(parser.tokens[start:end]) {
		if typeNode, err := ParseTypeTokens(parser.tokens[start:end]); err == nil && typeNode != nil {
			if _, opaque := typeNode.(*TokenType); !opaque {
				parser.index = end
				return &TypeExpr{Type: typeNode, SpanValue: tokenSpan(parser.tokens[start:end])}, nil
			}
		}
	}
	return parser.parse(0)
}

func (parser *expressionParser) callArgumentEnd(start int) int {
	parenDepth, bracketDepth, braceDepth := 0, 0, 0
	for index := start; index < len(parser.tokens); index++ {
		switch parser.tokens[index].Text {
		case "(":
			parenDepth++
		case ")":
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return index
			}
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
				return index
			}
		}
	}
	return len(parser.tokens)
}

func looksLikeTypeExpression(tokens []Token) bool {
	if len(tokens) == 0 {
		return false
	}
	switch tokens[0].Text {
	case "chan", "map", "struct", "interface", "func", "[", "<-":
		return true
	default:
		return false
	}
}

func (parser *expressionParser) take(text string) bool {
	if parser.index >= len(parser.tokens) || parser.tokens[parser.index].Text != text {
		return false
	}
	parser.index++
	return true
}

func findExpressionClosing(tokens []Token, open int, opening, closing string) (int, bool) {
	depth := 0
	for index := open; index < len(tokens); index++ {
		switch tokens[index].Text {
		case opening:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return index, true
			}
		}
	}
	return -1, false
}

func expressionPrecedence(operator string) (int, bool) {
	switch operator {
	case "=", ":=", "+=", "-=", "*=", "/=", "??=", "||=":
		return 1, true
	case "??", "||":
		return 2, true
	case "&&":
		return 3, true
	case "==", "!=", "<", "<=", ">", ">=":
		return 4, true
	case "+", "-", "|", "^":
		return 5, true
	case "*", "/", "%", "<<", ">>", "&", "&^":
		return 6, true
	default:
		return 0, false
	}
}

func isUnaryOperator(operator string) bool {
	switch operator {
	case "+", "-", "!", "~", "*", "&", "<-":
		return true
	default:
		return false
	}
}

func tokenSpan(tokens []Token) Span {
	if len(tokens) == 0 {
		return Span{}
	}
	first := tokens[0].Span
	last := tokens[len(tokens)-1].Span
	return Span{Start: first.Start, End: last.End, Line: first.Line, Column: first.Column}
}
