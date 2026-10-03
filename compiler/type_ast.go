package compiler

import (
	"fmt"
	"strings"
)

// TypeNode is the syntax-level representation of a Go++ type. Identifier
// spellings and source metadata remain strings because they are lexical data;
// the type grammar itself is represented by these nodes and can be consumed
// without reparsing type text.
type TypeNode interface {
	Node
	typeNode()
}

type NamedType struct {
	Parts     []string
	Arguments []TypeNode
	SpanValue Span
}

func (*NamedType) node()               {}
func (*NamedType) typeNode()           {}
func (typeNode *NamedType) Span() Span { return typeNode.SpanValue }

type PointerType struct {
	Element   TypeNode
	SpanValue Span
}

func (*PointerType) node()               {}
func (*PointerType) typeNode()           {}
func (typeNode *PointerType) Span() Span { return typeNode.SpanValue }

type SliceType struct {
	Element   TypeNode
	SpanValue Span
}

func (*SliceType) node()               {}
func (*SliceType) typeNode()           {}
func (typeNode *SliceType) Span() Span { return typeNode.SpanValue }

type ArrayType struct {
	Length    ExprNode
	Element   TypeNode
	Ellipsis  bool
	SpanValue Span
}

func (*ArrayType) node()               {}
func (*ArrayType) typeNode()           {}
func (typeNode *ArrayType) Span() Span { return typeNode.SpanValue }

type MapType struct {
	Key       TypeNode
	Value     TypeNode
	SpanValue Span
}

func (*MapType) node()               {}
func (*MapType) typeNode()           {}
func (typeNode *MapType) Span() Span { return typeNode.SpanValue }

type ChannelType struct {
	Direction string
	Element   TypeNode
	SpanValue Span
}

func (*ChannelType) node()               {}
func (*ChannelType) typeNode()           {}
func (typeNode *ChannelType) Span() Span { return typeNode.SpanValue }

type VariadicType struct {
	Element   TypeNode
	SpanValue Span
}

func (*VariadicType) node()               {}
func (*VariadicType) typeNode()           {}
func (typeNode *VariadicType) Span() Span { return typeNode.SpanValue }

type FunctionType struct {
	Parameters []ParameterNode
	Results    []TypeNode
	SpanValue  Span
}

func (*FunctionType) node()               {}
func (*FunctionType) typeNode()           {}
func (typeNode *FunctionType) Span() Span { return typeNode.SpanValue }

type StructFieldNode struct {
	Names     []string
	Type      TypeNode
	Tag       string
	SpanValue Span
}

func (*StructFieldNode) node() {}
func (field *StructFieldNode) Span() Span {
	if field == nil {
		return Span{}
	}
	return field.SpanValue
}

type StructType struct {
	Fields    []StructFieldNode
	SpanValue Span
}

func (*StructType) node()               {}
func (*StructType) typeNode()           {}
func (typeNode *StructType) Span() Span { return typeNode.SpanValue }

type InterfaceMethodNode struct {
	Name      string
	Signature TypeNode
	SpanValue Span
}

func (*InterfaceMethodNode) node() {}
func (method *InterfaceMethodNode) Span() Span {
	if method == nil {
		return Span{}
	}
	return method.SpanValue
}

type InterfaceType struct {
	Embeds    []TypeNode
	Methods   []InterfaceMethodNode
	SpanValue Span
}

func (*InterfaceType) node()               {}
func (*InterfaceType) typeNode()           {}
func (typeNode *InterfaceType) Span() Span { return typeNode.SpanValue }

// TupleType represents a parenthesized, multi-value type list such as a
// function result `(Value, error)`. It is kept distinct from FunctionType so
// signature printing does not have to fall back to token text.
type TupleType struct {
	Elements  []TypeNode
	SpanValue Span
}

func (*TupleType) node()               {}
func (*TupleType) typeNode()           {}
func (typeNode *TupleType) Span() Span { return typeNode.SpanValue }

type UnderlyingType struct {
	Element   TypeNode
	SpanValue Span
}

func (*UnderlyingType) node()               {}
func (*UnderlyingType) typeNode()           {}
func (typeNode *UnderlyingType) Span() Span { return typeNode.SpanValue }

type UnionType struct {
	Terms     []TypeNode
	SpanValue Span
}

func (*UnionType) node()               {}
func (*UnionType) typeNode()           {}
func (typeNode *UnionType) Span() Span { return typeNode.SpanValue }

// TokenType is the type equivalent of TokenExpr: it keeps unfamiliar type
// syntax lossless while dedicated type forms are added incrementally.
type TokenType struct {
	Tokens    []Token
	SpanValue Span
}

func (*TokenType) node()               {}
func (*TokenType) typeNode()           {}
func (typeNode *TokenType) Span() Span { return typeNode.SpanValue }

func ParseTypeTokens(tokens []Token) (TypeNode, error) {
	filtered := significantSyntaxTokens(tokens)
	if len(filtered) == 0 {
		return nil, nil
	}
	// Struct and interface members use newlines as declaration separators.
	// Keep those separators while parsing the aggregate itself; the ordinary
	// compact type path intentionally removes them for names and type lists.
	if filtered[0].Text == "struct" || filtered[0].Text == "interface" || containsAggregateType(filtered) {
		structural := make([]Token, 0, len(tokens))
		for _, token := range tokens {
			if token.Kind != TokenComment && token.Kind != TokenEOF {
				structural = append(structural, token)
			}
		}
		parser := typeParser{tokens: structural}
		typeNode, err := parser.parse()
		if err != nil {
			if fallback, ok := parseGoTypeTokens(filtered); ok {
				return fallback, nil
			}
			return nil, err
		}
		if parser.index == len(parser.tokens) || allTypeTrivia(parser.tokens[parser.index:]) {
			return typeNode, nil
		}
		if fallback, ok := parseGoTypeTokens(filtered); ok {
			return fallback, nil
		}
		return &TokenType{Tokens: filtered, SpanValue: tokenSpan(filtered)}, nil
	}
	if parts := splitTypeUnionTokens(filtered); len(parts) > 1 {
		terms := make([]TypeNode, 0, len(parts))
		for _, part := range parts {
			term, err := ParseTypeTokens(part)
			if err != nil || term == nil {
				return nil, fmt.Errorf("invalid union type constraint")
			}
			terms = append(terms, term)
		}
		return &UnionType{Terms: terms, SpanValue: tokenSpan(filtered)}, nil
	}
	if filtered[0].Text == "~" {
		element, err := ParseTypeTokens(filtered[1:])
		if err != nil || element == nil {
			return nil, fmt.Errorf("invalid underlying type constraint")
		}
		return &UnderlyingType{Element: element, SpanValue: spanFrom(filtered[0].Span, element.Span())}, nil
	}
	parser := typeParser{tokens: filtered}
	typeNode, err := parser.parse()
	if err != nil {
		if fallback, ok := parseGoTypeTokens(filtered); ok {
			return fallback, nil
		}
		return nil, err
	}
	if parser.index != len(parser.tokens) {
		if fallback, ok := parseGoTypeTokens(filtered); ok {
			return fallback, nil
		}
		return &TokenType{Tokens: filtered, SpanValue: tokenSpan(filtered)}, nil
	}
	return typeNode, nil
}

func containsAggregateType(tokens []Token) bool {
	for index := 0; index+1 < len(tokens); index++ {
		if (tokens[index].Text == "struct" || tokens[index].Text == "interface") && tokens[index+1].Text == "{" {
			return true
		}
	}
	return false
}

func allTypeTrivia(tokens []Token) bool {
	for _, token := range tokens {
		if token.Kind != TokenNewline && token.Kind != TokenComment && token.Kind != TokenEOF {
			return false
		}
	}
	return true
}

type typeParser struct {
	tokens []Token
	index  int
}

func (parser *typeParser) parse() (TypeNode, error) {
	if parser.index >= len(parser.tokens) {
		return nil, fmt.Errorf("expected type")
	}
	start := parser.tokens[parser.index]
	if start.Text == "func" {
		return parser.parseFunctionType()
	}
	if start.Text == "struct" && parser.peek(1, "{") {
		return parser.parseStructType()
	}
	if start.Text == "interface" && parser.peek(1, "{") {
		return parser.parseInterfaceType()
	}
	if start.Text == "(" {
		parser.index++
		close, ok := parser.findClosing("(")
		if !ok {
			return nil, fmt.Errorf("unterminated parenthesized type")
		}
		elements := []TypeNode{}
		for _, part := range splitExpressionTokens(parser.tokens[parser.index:close]) {
			element, err := ParseTypeTokens(part)
			if err != nil || element == nil {
				return nil, fmt.Errorf("invalid parenthesized type")
			}
			elements = append(elements, element)
		}
		parser.index = close + 1
		return &TupleType{Elements: elements, SpanValue: spanFrom(start.Span, parser.tokens[close].Span)}, nil
	}

	if start.Text == "*" {
		parser.index++
		element, err := parser.parse()
		if err != nil {
			return nil, err
		}
		return &PointerType{Element: element, SpanValue: spanFrom(start.Span, element.Span())}, nil
	}
	if start.Text == "<-" {
		parser.index++
		if parser.take("chan") {
			element, err := parser.parse()
			if err != nil {
				return nil, err
			}
			return &ChannelType{Direction: "receive", Element: element, SpanValue: spanFrom(start.Span, element.Span())}, nil
		}
		element, err := parser.parse()
		if err != nil {
			return nil, err
		}
		return &ChannelType{Direction: "receive", Element: element, SpanValue: spanFrom(start.Span, element.Span())}, nil
	}
	if start.Text == "chan" {
		parser.index++
		direction := "both"
		if parser.take("<-") {
			direction = "send"
		}
		element, err := parser.parse()
		if err != nil {
			return nil, err
		}
		return &ChannelType{Direction: direction, Element: element, SpanValue: spanFrom(start.Span, element.Span())}, nil
	}
	if start.Text == "..." {
		parser.index++
		element, err := parser.parse()
		if err != nil {
			return nil, err
		}
		return &VariadicType{Element: element, SpanValue: spanFrom(start.Span, element.Span())}, nil
	}
	if start.Text == "[" {
		parser.index++
		if parser.take("]") {
			element, err := parser.parse()
			if err != nil {
				return nil, err
			}
			return &SliceType{Element: element, SpanValue: spanFrom(start.Span, element.Span())}, nil
		}
		if parser.take("...") {
			if !parser.take("]") {
				return nil, fmt.Errorf("expected ] in ellipsis array type")
			}
			element, err := parser.parse()
			if err != nil {
				return nil, err
			}
			return &ArrayType{Element: element, Ellipsis: true, SpanValue: spanFrom(start.Span, element.Span())}, nil
		}
		lengthStart := parser.index
		for parser.index < len(parser.tokens) && parser.tokens[parser.index].Text != "]" {
			parser.index++
		}
		if !parser.take("]") {
			return nil, fmt.Errorf("expected ] in array type")
		}
		length, err := ParseExpressionTokens(parser.tokens[lengthStart : parser.index-1])
		if err != nil {
			return nil, err
		}
		element, err := parser.parse()
		if err != nil {
			return nil, err
		}
		return &ArrayType{Length: length, Element: element, SpanValue: spanFrom(start.Span, element.Span())}, nil
	}
	if start.Text == "map" && parser.peek(1, "[") {
		parser.index += 2
		keyStart := parser.index
		keyEnd, ok := parser.findClosing("[")
		if !ok {
			return nil, fmt.Errorf("expected ] in map type")
		}
		key, err := ParseTypeTokens(parser.tokens[keyStart:keyEnd])
		if err != nil {
			return nil, err
		}
		parser.index = keyEnd + 1
		value, err := parser.parse()
		if err != nil {
			return nil, err
		}
		return &MapType{Key: key, Value: value, SpanValue: spanFrom(start.Span, value.Span())}, nil
	}

	if start.Kind != TokenIdentifier && start.Kind != TokenKeyword {
		parser.index++
		return &TokenType{Tokens: []Token{start}, SpanValue: start.Span}, nil
	}
	parts := []string{start.Text}
	parser.index++
	for parser.take(".") {
		if parser.index >= len(parser.tokens) {
			return nil, fmt.Errorf("expected qualified type name")
		}
		parts = append(parts, parser.tokens[parser.index].Text)
		parser.index++
	}
	arguments := []TypeNode{}
	if parser.take("[") {
		for parser.index < len(parser.tokens) && parser.tokens[parser.index].Text != "]" {
			argument, err := parser.parse()
			if err != nil {
				return nil, err
			}
			arguments = append(arguments, argument)
			if !parser.take(",") {
				break
			}
		}
		if !parser.take("]") {
			return nil, fmt.Errorf("expected ] in generic type")
		}
	}
	end := parser.tokens[parser.index-1].Span
	return &NamedType{Parts: parts, Arguments: arguments, SpanValue: spanFrom(start.Span, end)}, nil
}

func (parser *typeParser) parseFunctionType() (TypeNode, error) {
	start := parser.tokens[parser.index]
	parser.index++
	if !parser.take("(") {
		return nil, fmt.Errorf("function type requires parameter parentheses")
	}
	parameterOpen := parser.index - 1
	parameterClose, ok := parser.findClosing("(")
	if !ok {
		return nil, fmt.Errorf("unterminated function type parameters")
	}
	parameterTokens := parser.tokens[parameterOpen+1 : parameterClose]
	parameters := parseParameterNodes(expressionTokensSource(parameterTokens))
	if len(parameterTokens) > 0 && len(parameters) == 0 {
		return nil, fmt.Errorf("invalid function type parameters")
	}
	parser.index = parameterClose + 1
	results := []TypeNode{}
	if parser.index < len(parser.tokens) && parser.tokens[parser.index].Text == "(" {
		resultOpen := parser.index
		parser.index++
		resultClose, ok := parser.findClosing("(")
		if !ok {
			return nil, fmt.Errorf("unterminated function type results")
		}
		resultTokens := parser.tokens[resultOpen+1 : resultClose]
		for _, part := range splitExpressionTokens(resultTokens) {
			resultParameters := parseParameterNodes(expressionTokensSource(part))
			if len(resultParameters) == 1 && resultParameters[0].Name != "" {
				results = append(results, resultParameters[0].Type)
				continue
			}
			result, err := ParseTypeTokens(part)
			if err != nil || result == nil {
				return nil, fmt.Errorf("invalid function type result")
			}
			results = append(results, result)
		}
		parser.index = resultClose + 1
	} else if parser.index < len(parser.tokens) && parser.tokens[parser.index].Text != ")" {
		result, err := parser.parse()
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	end := start.Span
	if parser.index > 0 {
		end = parser.tokens[parser.index-1].Span
	}
	return &FunctionType{Parameters: parameters, Results: results, SpanValue: spanFrom(start.Span, end)}, nil
}

func (parser *typeParser) parseStructType() (TypeNode, error) {
	start := parser.tokens[parser.index]
	parser.index += 2
	close, ok := parser.findClosingDelimiter("{")
	if !ok {
		return nil, fmt.Errorf("unterminated struct type")
	}
	fields := []StructFieldNode{}
	for _, part := range splitTypeMemberTokens(parser.tokens[parser.index:close]) {
		field, err := parseStructFieldNode(part)
		if err != nil {
			return nil, err
		}
		if field != nil {
			fields = append(fields, *field)
		}
	}
	parser.index = close + 1
	return &StructType{Fields: fields, SpanValue: spanFrom(start.Span, parser.tokens[close].Span)}, nil
}

func (parser *typeParser) parseInterfaceType() (TypeNode, error) {
	start := parser.tokens[parser.index]
	parser.index += 2
	close, ok := parser.findClosingDelimiter("{")
	if !ok {
		return nil, fmt.Errorf("unterminated interface type")
	}
	interfaceType := &InterfaceType{SpanValue: spanFrom(start.Span, parser.tokens[close].Span)}
	for _, part := range splitTypeMemberTokens(parser.tokens[parser.index:close]) {
		member, err := parseInterfaceMemberNode(part)
		if err != nil {
			return nil, err
		}
		if member.method != nil {
			interfaceType.Methods = append(interfaceType.Methods, *member.method)
		} else if member.embed != nil {
			interfaceType.Embeds = append(interfaceType.Embeds, member.embed)
		}
	}
	parser.index = close + 1
	return interfaceType, nil
}

type interfaceMemberNode struct {
	method *InterfaceMethodNode
	embed  TypeNode
}

func parseStructFieldNode(tokens []Token) (*StructFieldNode, error) {
	clean := significantSyntaxTokens(tokens)
	if len(clean) == 0 {
		return nil, nil
	}
	tag := ""
	if last := clean[len(clean)-1]; last.Kind == TokenString || last.Kind == TokenRawString {
		tag = last.Text
		clean = clean[:len(clean)-1]
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("struct field is missing a type")
	}
	names, typeTokens := splitNamedTypeMember(clean)
	if len(typeTokens) == 0 {
		return nil, fmt.Errorf("struct field is missing a type")
	}
	typeNode, err := ParseTypeTokens(typeTokens)
	if err != nil || typeNode == nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("struct field is missing a type")
	}
	return &StructFieldNode{Names: names, Type: typeNode, Tag: tag, SpanValue: tokenSpan(tokens)}, nil
}

func parseInterfaceMemberNode(tokens []Token) (interfaceMemberNode, error) {
	clean := significantSyntaxTokens(tokens)
	if len(clean) == 0 {
		return interfaceMemberNode{}, nil
	}
	if len(clean) > 1 && (clean[0].Kind == TokenIdentifier || clean[0].Kind == TokenKeyword) && clean[1].Text == "(" {
		name := clean[0].Text
		functionTokens := append([]Token{{Kind: TokenKeyword, Text: "func", Span: clean[0].Span}}, clean[1:]...)
		signature, err := ParseTypeTokens(functionTokens)
		if err != nil {
			return interfaceMemberNode{}, err
		}
		return interfaceMemberNode{method: &InterfaceMethodNode{Name: name, Signature: signature, SpanValue: tokenSpan(tokens)}}, nil
	}
	embed, err := ParseTypeTokens(clean)
	if err != nil || embed == nil {
		if err != nil {
			return interfaceMemberNode{}, err
		}
		return interfaceMemberNode{}, fmt.Errorf("invalid embedded interface type")
	}
	return interfaceMemberNode{embed: embed}, nil
}

func splitNamedTypeMember(tokens []Token) ([]string, []Token) {
	clean := significantSyntaxTokens(tokens)
	if len(clean) < 2 {
		return nil, clean
	}
	index := 0
	names := []string{}
	for index < len(clean) {
		if clean[index].Kind != TokenIdentifier && clean[index].Kind != TokenKeyword {
			break
		}
		if index+1 >= len(clean) {
			break
		}
		names = append(names, clean[index].Text)
		index++
		if clean[index].Text != "," {
			break
		}
		index++
	}
	if len(names) == 0 || index >= len(clean) || (index == 1 && clean[index].Text == ".") {
		return nil, clean
	}
	return names, clean[index:]
}

func splitTypeMemberTokens(tokens []Token) [][]Token {
	parts := [][]Token{}
	start := 0
	parenDepth, bracketDepth, braceDepth := 0, 0, 0
	flush := func(end int) {
		if len(significantSyntaxTokens(tokens[start:end])) > 0 {
			parts = append(parts, tokens[start:end])
		}
		start = end + 1
	}
	for index, token := range tokens {
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
		if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 &&
			(token.Kind == TokenNewline || token.Text == ";") {
			flush(index)
		}
	}
	if len(significantSyntaxTokens(tokens[start:])) > 0 {
		parts = append(parts, tokens[start:])
	}
	return parts
}

func splitTypeUnionTokens(tokens []Token) [][]Token {
	parts := [][]Token{}
	start := 0
	parenDepth, bracketDepth, braceDepth := 0, 0, 0
	for index, token := range tokens {
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
		if token.Text == "|" && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
			parts = append(parts, tokens[start:index])
			start = index + 1
		}
	}
	if len(parts) == 0 {
		return nil
	}
	parts = append(parts, tokens[start:])
	return parts
}

func (parser *typeParser) findClosingDelimiter(open string) (int, bool) {
	close := map[string]string{"{": "}"}[open]
	depth := 1
	for index := parser.index; index < len(parser.tokens); index++ {
		switch parser.tokens[index].Text {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return index, true
			}
		}
	}
	return -1, false
}

func (parser *typeParser) take(text string) bool {
	if parser.index >= len(parser.tokens) || parser.tokens[parser.index].Text != text {
		return false
	}
	parser.index++
	return true
}

func (parser *typeParser) peek(offset int, text string) bool {
	return parser.index+offset < len(parser.tokens) && parser.tokens[parser.index+offset].Text == text
}

func (parser *typeParser) findClosing(open string) (int, bool) {
	close := "]"
	if open == "(" {
		close = ")"
	}
	depth := 1
	for index := parser.index; index < len(parser.tokens); index++ {
		switch parser.tokens[index].Text {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return index, true
			}
		}
	}
	return -1, false
}

func spanFrom(start, end Span) Span {
	return Span{Start: start.Start, End: end.End, Line: start.Line, Column: start.Column}
}

func parseTypeText(text string) TypeNode {
	tokens, err := LexSource("type", text)
	if err != nil {
		return nil
	}
	typeNode, err := ParseTypeTokens(tokens)
	if err != nil {
		return nil
	}
	return typeNode
}

func parseParameterNodes(text string) []ParameterNode {
	parameters, err := parseParameterInfos(text)
	if err != nil {
		return nil
	}
	result := make([]ParameterNode, 0, len(parameters))
	for _, parameter := range parameters {
		node := ParameterNode{
			Name:       parameter.Name,
			Type:       parameter.TypeAST,
			HasDefault: parameter.HasDefault,
		}
		if parameter.HasDefault {
			node.Default = parameter.DefaultAST
			node.DefaultTokens = append([]Token(nil), parameter.DefaultTokens...)
		}
		if node.Type != nil {
			node.SpanValue = node.Type.Span()
		}
		result = append(result, node)
	}
	return result
}

func parseResultFieldNodes(text string) []ParameterNode {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "(") {
		if close, err := findMatchingParen(text, 0); err == nil && close == len(text)-1 {
			text = strings.TrimSpace(text[1:close])
		}
	}
	return parseParameterNodes(text)
}

func parseTypeParameterNodes(text string) []TypeParameterNode {
	text = strings.TrimSpace(text)
	if len(text) < 2 || text[0] != '[' || text[len(text)-1] != ']' {
		return nil
	}
	parts, err := splitTopLevel(text[1:len(text)-1], ',')
	if err != nil {
		return nil
	}
	result := make([]TypeParameterNode, 0, len(parts))
	for _, part := range parts {
		tokens, tokenErr := LexSource("type parameter", strings.TrimSpace(part))
		if tokenErr != nil {
			return nil
		}
		clean := significantSyntaxTokens(tokens)
		if len(clean) == 0 || (clean[0].Kind != TokenIdentifier && clean[0].Kind != TokenKeyword) {
			return nil
		}
		parameter := TypeParameterNode{Name: clean[0].Text, SpanValue: tokenSpan(clean)}
		if len(clean) > 1 {
			constraint, constraintErr := ParseTypeTokens(clean[1:])
			if constraintErr != nil || constraint == nil {
				return nil
			}
			parameter.Constraint = constraint
		}
		result = append(result, parameter)
	}
	return result
}

func typeParameterNodesSource(parameters []TypeParameterNode) (string, error) {
	if len(parameters) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		text := parameter.Name
		if parameter.Constraint != nil {
			constraint, err := typeNodeSource(parameter.Constraint)
			if err != nil {
				return "", err
			}
			text += " " + constraint
		}
		parts = append(parts, text)
	}
	return "[" + strings.Join(parts, ", ") + "]", nil
}

// goTypeParameterNodesSource emits constraints required by Go's syntax. Go++
// treats an omitted constraint as `any`, so preserve the concise form in
// Go++ source rendering while making generated Go declarations explicit.
func goTypeParameterNodesSource(parameters []TypeParameterNode) (string, error) {
	if len(parameters) == 0 {
		return "", nil
	}
	goParameters := append([]TypeParameterNode(nil), parameters...)
	for index := range goParameters {
		if goParameters[index].Constraint == nil {
			goParameters[index].Constraint = &NamedType{Parts: []string{"any"}}
		}
	}
	return typeParameterNodesSource(goParameters)
}

func typeNodeSource(typeNode TypeNode) (string, error) {
	switch value := typeNode.(type) {
	case nil:
		return "", nil
	case *NamedType:
		name := strings.Join(value.Parts, ".")
		if len(value.Arguments) == 0 {
			return name, nil
		}
		arguments := make([]string, 0, len(value.Arguments))
		for _, argument := range value.Arguments {
			text, err := typeNodeSource(argument)
			if err != nil {
				return "", err
			}
			arguments = append(arguments, text)
		}
		return name + "[" + strings.Join(arguments, ", ") + "]", nil
	case *PointerType:
		element, err := typeNodeSource(value.Element)
		return "*" + element, err
	case *SliceType:
		element, err := typeNodeSource(value.Element)
		return "[]" + element, err
	case *ArrayType:
		length := ""
		if value.Ellipsis {
			length = "..."
		} else {
			var err error
			length, err = expressionNodeSource(value.Length)
			if err != nil {
				return "", err
			}
		}
		element, err := typeNodeSource(value.Element)
		return "[" + length + "]" + element, err
	case *MapType:
		key, err := typeNodeSource(value.Key)
		if err != nil {
			return "", err
		}
		valueType, err := typeNodeSource(value.Value)
		return "map[" + key + "]" + valueType, err
	case *ChannelType:
		element, err := typeNodeSource(value.Element)
		if err != nil {
			return "", err
		}
		if value.Direction == "receive" {
			return "<-chan " + element, nil
		}
		if value.Direction == "send" {
			return "chan<- " + element, nil
		}
		return "chan " + element, nil
	case *VariadicType:
		element, err := typeNodeSource(value.Element)
		return "..." + element, err
	case *FunctionType:
		parameters, err := parameterNodesSource(value.Parameters)
		if err != nil {
			return "", err
		}
		results := make([]string, 0, len(value.Results))
		for _, result := range value.Results {
			text, resultErr := typeNodeSource(result)
			if resultErr != nil {
				return "", resultErr
			}
			results = append(results, text)
		}
		if len(results) == 0 {
			return "func(" + parameters + ")", nil
		}
		if len(results) == 1 {
			return "func(" + parameters + ") " + results[0], nil
		}
		return "func(" + parameters + ") (" + strings.Join(results, ", ") + ")", nil
	case *StructType:
		parts := make([]string, 0, len(value.Fields))
		for _, field := range value.Fields {
			typeText, err := typeNodeSource(field.Type)
			if err != nil {
				return "", err
			}
			text := typeText
			if len(field.Names) > 0 {
				text = strings.Join(field.Names, ", ") + " " + text
			}
			if field.Tag != "" {
				text += " " + field.Tag
			}
			parts = append(parts, text)
		}
		return "struct { " + strings.Join(parts, "; ") + " }", nil
	case *InterfaceType:
		parts := make([]string, 0, len(value.Embeds)+len(value.Methods))
		for _, embed := range value.Embeds {
			text, err := typeNodeSource(embed)
			if err != nil {
				return "", err
			}
			parts = append(parts, text)
		}
		for _, method := range value.Methods {
			signature, err := typeNodeSource(method.Signature)
			if err != nil {
				return "", err
			}
			signature = strings.TrimPrefix(signature, "func")
			parts = append(parts, method.Name+signature)
		}
		return "interface { " + strings.Join(parts, "; ") + " }", nil
	case *TupleType:
		elements := make([]string, 0, len(value.Elements))
		for _, element := range value.Elements {
			text, err := typeNodeSource(element)
			if err != nil {
				return "", err
			}
			elements = append(elements, text)
		}
		return "(" + strings.Join(elements, ", ") + ")", nil
	case *UnderlyingType:
		element, err := typeNodeSource(value.Element)
		return "~" + element, err
	case *UnionType:
		terms := make([]string, 0, len(value.Terms))
		for _, term := range value.Terms {
			text, err := typeNodeSource(term)
			if err != nil {
				return "", err
			}
			terms = append(terms, text)
		}
		return strings.Join(terms, "|"), nil
	case *TokenType:
		return tokenExpressionSource(value.Tokens), nil
	default:
		return "", fmt.Errorf("unsupported type AST node %T", typeNode)
	}
}

// typeNodeSignatureKey is the canonical, source-independent spelling used for
// overload and method-dispatch identity. It intentionally mirrors the compact
// spelling of typeNodeSource, but walks the typed tree directly so hot-path
// signature comparisons do not render a type and then feed that text back
// through parseParameterInfos.
func typeNodeSignatureKey(typeNode TypeNode) string {
	switch value := typeNode.(type) {
	case nil:
		return ""
	case *NamedType:
		name := strings.Join(value.Parts, ".")
		if len(value.Arguments) == 0 {
			return name
		}
		arguments := make([]string, 0, len(value.Arguments))
		for _, argument := range value.Arguments {
			arguments = append(arguments, typeNodeSignatureKey(argument))
		}
		return name + "[" + strings.Join(arguments, ", ") + "]"
	case *PointerType:
		return "*" + typeNodeSignatureKey(value.Element)
	case *SliceType:
		return "[]" + typeNodeSignatureKey(value.Element)
	case *ArrayType:
		length := ""
		if value.Ellipsis {
			length = "..."
		} else if value.Length != nil {
			length, _ = expressionNodeSource(value.Length)
		}
		return "[" + length + "]" + typeNodeSignatureKey(value.Element)
	case *MapType:
		return "map[" + typeNodeSignatureKey(value.Key) + "]" + typeNodeSignatureKey(value.Value)
	case *ChannelType:
		element := typeNodeSignatureKey(value.Element)
		switch value.Direction {
		case "receive":
			return "<-chan " + element
		case "send":
			return "chan<- " + element
		default:
			return "chan " + element
		}
	case *VariadicType:
		return "..." + typeNodeSignatureKey(value.Element)
	case *FunctionType:
		parameters := make([]string, 0, len(value.Parameters))
		for _, parameter := range value.Parameters {
			parameters = append(parameters, typeNodeSignatureKey(parameter.Type))
		}
		results := make([]string, 0, len(value.Results))
		for _, result := range value.Results {
			results = append(results, typeNodeSignatureKey(result))
		}
		result := ""
		if len(results) == 1 {
			result = " " + results[0]
		} else if len(results) > 1 {
			result = " (" + strings.Join(results, ", ") + ")"
		}
		return "func(" + strings.Join(parameters, ", ") + ")" + result
	case *StructType, *InterfaceType, *TupleType, *UnderlyingType, *UnionType, *TokenType:
		// These less common signature forms still have a canonical source
		// spelling, but they do not require parsing. This is the final rendering
		// boundary for an opaque type node, not a source-to-AST round trip.
		text, _ := typeNodeSource(typeNode)
		return text
	default:
		return ""
	}
}

func parameterNodesSource(parameters []ParameterNode) (string, error) {
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		typeText, err := typeNodeSource(parameter.Type)
		if err != nil {
			return "", err
		}
		text := typeText
		if parameter.Name != "" {
			text = parameter.Name + " " + text
		}
		if parameter.HasDefault {
			defaultText := expressionTokensSource(parameter.DefaultTokens)
			if defaultText == "" {
				defaultText, err = expressionNodeSource(parameter.Default)
				if err != nil {
					return "", err
				}
			}
			text += " = " + defaultText
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, ", "), nil
}
