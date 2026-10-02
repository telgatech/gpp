package compiler

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TokenKind is the lossless frontend category for one source token. Whitespace
// other than line boundaries is intentionally not emitted; the original file
// text remains available for exact source reproduction.
type TokenKind uint8

const (
	TokenInvalid TokenKind = iota
	TokenEOF
	TokenNewline
	TokenIdentifier
	TokenKeyword
	TokenNumber
	TokenString
	TokenRawString
	TokenRune
	TokenComment
	TokenOperator
	TokenPunctuation
)

type Span struct {
	Start  int
	End    int
	Line   int
	Column int
}

type sourceLineDiagnostic struct {
	line int
	err  error
}

func (diagnostic sourceLineDiagnostic) Error() string {
	return fmt.Sprintf("%d: %v", diagnostic.line, diagnostic.err)
}

func (diagnostic sourceLineDiagnostic) Unwrap() error { return diagnostic.err }

func sourceLineError(span Span, err error) error {
	if err == nil || span.Line <= 0 {
		return err
	}
	var located sourceLineDiagnostic
	if errors.As(err, &located) {
		return err
	}
	return sourceLineDiagnostic{line: span.Line, err: err}
}

func isSourceLineDiagnostic(err error) bool {
	var located sourceLineDiagnostic
	return errors.As(err, &located)
}

func sourceSpan(source string, start, end int) Span {
	line, column := sourcePosition(source, start)
	return Span{Start: start, End: end, Line: line, Column: column}
}

type Token struct {
	Kind TokenKind
	Text string
	Span Span
}

var goPlusKeywords = map[string]bool{
	"break": true, "case": true, "catch": true, "class": true,
	"const": true, "continue": true, "defer": true, "default": true,
	"else": true, "enum": true, "extend": true, "fallthrough": true,
	"finally": true, "for": true, "func": true, "go": true,
	"goto": true, "if": true, "import": true, "in": true,
	"interface": true, "let": true, "map": true, "package": true,
	"range": true, "record": true, "return": true, "select": true,
	"static": true, "struct": true, "switch": true, "template": true,
	"throw": true, "try": true, "type": true, "var": true,
	"this": true, "true": true, "false": true, "nil": true,
}

var goPlusOperators = []string{
	"<<=", ">>=", "...", "||=", "??=", "===", "!==",
	":=", "=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.",
	"++", "--", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=",
	"<<", ">>", "<-", "&^", "::",
	"+", "-", "*", "/", "%", "&", "|", "^", "!", "~", "=", "<", ">",
}

func LexSource(filename, source string) ([]Token, error) {
	lineStarts := []int{0}
	for index := 0; index < len(source); index++ {
		if source[index] == '\n' {
			lineStarts = append(lineStarts, index+1)
		}
	}
	span := func(start, end int) Span {
		lineIndex := sort.Search(len(lineStarts), func(index int) bool { return lineStarts[index] > start }) - 1
		if lineIndex < 0 {
			lineIndex = 0
		}
		return Span{Start: start, End: end, Line: lineIndex + 1, Column: start - lineStarts[lineIndex] + 1}
	}
	tokens := []Token{}
	add := func(kind TokenKind, start, end int) {
		tokens = append(tokens, Token{Kind: kind, Text: source[start:end], Span: span(start, end)})
	}

	for index := 0; index < len(source); {
		char := source[index]
		switch char {
		case ' ', '\t', '\v', '\f':
			index++
			continue
		case '\r', '\n':
			start := index
			if char == '\r' && index+1 < len(source) && source[index+1] == '\n' {
				index += 2
			} else {
				index++
			}
			add(TokenNewline, start, index)
			continue
		case '/':
			if index+1 < len(source) && source[index+1] == '/' {
				start := index
				index += 2
				for index < len(source) && source[index] != '\r' && source[index] != '\n' {
					index++
				}
				add(TokenComment, start, index)
				continue
			}
			if index+1 < len(source) && source[index+1] == '*' {
				start := index
				index += 2
				for index+1 < len(source) && !(source[index] == '*' && source[index+1] == '/') {
					index++
				}
				if index+1 >= len(source) {
					return nil, tokenError(filename, span(start, len(source)), "unterminated block comment")
				}
				index += 2
				add(TokenComment, start, index)
				continue
			}
		case '"', '\'', '`':
			start := index
			quote := char
			index++
			closed := false
			for index < len(source) {
				if quote != '`' && source[index] == '\\' {
					index += 2
					continue
				}
				if source[index] == quote {
					index++
					closed = true
					break
				}
				if quote != '`' && (source[index] == '\r' || source[index] == '\n') {
					break
				}
				index++
			}
			if !closed {
				return nil, tokenError(filename, span(start, index), "unterminated literal")
			}
			kind := TokenString
			if quote == '`' {
				kind = TokenRawString
			} else if quote == '\'' {
				kind = TokenRune
			}
			add(kind, start, index)
			continue
		}

		if isTokenIdentStart(char) {
			start := index
			_, width := utf8.DecodeRuneInString(source[index:])
			index += width
			for index < len(source) && isTokenIdentPart(source[index]) {
				_, width = utf8.DecodeRuneInString(source[index:])
				index += width
			}
			kind := TokenIdentifier
			if goPlusKeywords[source[start:index]] {
				kind = TokenKeyword
			}
			add(kind, start, index)
			continue
		}

		if isTokenNumberStart(source, index) {
			start := index
			for index < len(source) && isTokenNumberPart(source[index]) {
				index++
			}
			add(TokenNumber, start, index)
			continue
		}

		matchedOperator := ""
		for _, operator := range goPlusOperators {
			if len(operator) > len(matchedOperator) && index+len(operator) <= len(source) && source[index:index+len(operator)] == operator {
				matchedOperator = operator
			}
		}
		if matchedOperator != "" {
			add(TokenOperator, index, index+len(matchedOperator))
			index += len(matchedOperator)
			continue
		}

		if isTokenPunctuation(char) {
			add(TokenPunctuation, index, index+1)
			index++
			continue
		}
		return nil, tokenError(filename, span(index, index+1), fmt.Sprintf("unexpected character %q", char))
	}
	add(TokenEOF, len(source), len(source))
	return tokens, nil
}

// tokenSequence reports syntax without requiring compiler phases to search
// executable source text. Compatibility callers may pass tokens produced by
// the parser; the helper intentionally ignores comments and line boundaries.
func tokenSequence(tokens []Token, sequence ...string) bool {
	if len(sequence) == 0 {
		return false
	}
	for index := 0; index < len(tokens); index++ {
		if tokens[index].Kind == TokenComment || tokens[index].Kind == TokenNewline {
			continue
		}
		candidate := index
		matched := true
		for _, wanted := range sequence {
			for candidate < len(tokens) && (tokens[candidate].Kind == TokenComment || tokens[candidate].Kind == TokenNewline) {
				candidate++
			}
			if candidate >= len(tokens) || tokens[candidate].Text != wanted {
				matched = false
				break
			}
			candidate++
		}
		if matched {
			return true
		}
	}
	return false
}

func mixedDeclTokens(raw *MixedDecl) []Token {
	if raw == nil {
		return nil
	}
	return raw.Tokens
}

func goDeclTokens(declaration *GoDecl) []Token {
	if declaration == nil {
		return nil
	}
	if len(declaration.Tokens) > 0 {
		return declaration.Tokens
	}
	return nil
}

func valueDeclTokens(declaration *ValueDecl) []Token {
	if declaration == nil {
		return nil
	}
	return declaration.Tokens
}

// valueDeclASTSource renders a Go++ value declaration from its structured
// fields. The declaration's source span remains available for diagnostics,
// but emission does not need to recover executable syntax from it.
func valueDeclASTSource(declaration *ValueDecl) (string, error) {
	if declaration == nil {
		return "", nil
	}
	parts := []string{declaration.Keyword}
	names := make([]string, 0, len(declaration.Names))
	for _, name := range declaration.Names {
		if name.Text != "" {
			names = append(names, name.Text)
		}
	}
	if len(names) > 0 {
		parts = append(parts, strings.Join(names, ", "))
	}
	if declaration.Type != nil {
		typeText, err := typeNodeSource(declaration.Type)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(typeText) != "" {
			parts = append(parts, typeText)
		}
	}
	result := strings.Join(parts, " ")
	if len(declaration.Values) > 0 {
		values := make([]string, 0, len(declaration.Values))
		for _, value := range declaration.Values {
			text, err := expressionNodeSource(value)
			if err != nil {
				return "", err
			}
			values = append(values, text)
		}
		result += " = " + strings.Join(values, ", ")
	}
	return result + "\n", nil
}

func methodBodyTokens(method Method) []Token {
	if len(method.BodyTokens) > 0 {
		return method.BodyTokens
	}
	if method.Owner != nil && method.BodySpan.Start >= 0 && method.BodySpan.End <= len(method.Owner.Source) && method.BodySpan.Start <= method.BodySpan.End {
		tokens, _ := LexSource("method body", method.Owner.Source[method.BodySpan.Start:method.BodySpan.End])
		return rebaseTokens(tokens, method.Owner.Source, method.BodySpan.Start)
	}
	return nil
}

func methodBodySource(method Method) string {
	if method.Owner != nil && method.BodySpan.Start >= 0 && method.BodySpan.End <= len(method.Owner.Source) && method.BodySpan.Start <= method.BodySpan.End {
		return method.Owner.Source[method.BodySpan.Start:method.BodySpan.End]
	}
	return ""
}

func fieldTypeSource(field Field) string {
	if field.TypeAST != nil {
		if source, err := typeNodeSource(field.TypeAST); err == nil && strings.TrimSpace(source) != "" {
			return source
		}
	}
	if field.Owner != nil && field.TypeSpan.Start >= 0 && field.TypeSpan.End <= len(field.Owner.Source) && field.TypeSpan.Start <= field.TypeSpan.End {
		return field.Owner.Source[field.TypeSpan.Start:field.TypeSpan.End]
	}
	return ""
}

func classParentNames(class *ClassDecl) []string {
	if class == nil {
		return nil
	}
	parents := make([]string, 0, len(class.ParentAST))
	for _, parent := range class.ParentAST {
		name, err := typeNodeSource(parent)
		if err == nil && strings.TrimSpace(name) != "" {
			parents = append(parents, name)
		}
	}
	return parents
}

func extensionTargetNames(extension *ExtendDecl) []string {
	if extension == nil {
		return nil
	}
	targets := make([]string, 0, len(extension.TargetAST))
	for _, target := range extension.TargetAST {
		name, err := typeNodeSource(target)
		if err == nil && strings.TrimSpace(name) != "" {
			targets = append(targets, name)
		}
	}
	return targets
}

func methodParametersSource(method Method) string {
	if method.Owner != nil || len(method.ParameterAST) > 0 {
		if source, err := parameterNodesSource(method.ParameterAST); err == nil {
			return source
		}
	}
	if method.Owner != nil && method.ParametersSpan.Start >= 0 && method.ParametersSpan.End > method.ParametersSpan.Start && method.ParametersSpan.End <= len(method.Owner.Source) {
		source := method.Owner.Source[method.ParametersSpan.Start:method.ParametersSpan.End]
		if cleaned, _, err := extractParameterAnnotations(source); err == nil {
			return cleaned
		}
		return source
	}
	return ""
}

func methodResultSource(method Method) string {
	if method.Owner != nil || method.ResultAST != nil {
		if source, err := typeNodeSource(method.ResultAST); err == nil {
			return source
		}
	}
	if method.Owner != nil && method.ResultSpan.Start >= 0 && method.ResultSpan.End > method.ResultSpan.Start && method.ResultSpan.End <= len(method.Owner.Source) {
		return method.Owner.Source[method.ResultSpan.Start:method.ResultSpan.End]
	}
	return ""
}

// methodResultTypeNode keeps signature consumers on the typed path. The
// source fallback is only for compatibility Methods assembled by older
// callers that do not carry ResultAST yet.
func methodResultTypeNode(method Method) TypeNode {
	if method.ResultAST != nil {
		return method.ResultAST
	}
	if source := strings.TrimSpace(methodResultSource(method)); source != "" {
		return parseTypeText(source)
	}
	return nil
}

func methodTypeParamsSource(method Method) string {
	if len(method.TypeParamsAST) > 0 {
		if source, err := typeParameterNodesSource(method.TypeParamsAST); err == nil && source != "" {
			return source
		}
	}
	return ""
}

func templateParametersSource(template *TemplateDecl) string {
	if template == nil {
		return ""
	}
	source, err := parameterNodesSource(template.ParameterAST)
	if err != nil {
		return ""
	}
	return source
}

func functionSource(function *FunctionDecl) string {
	if function == nil {
		return ""
	}
	if function.Owner != nil && function.SourceSpan.Start >= 0 && function.SourceSpan.End <= len(function.Owner.Source) && function.SourceSpan.Start <= function.SourceSpan.End {
		return function.Owner.Source[function.SourceSpan.Start:function.SourceSpan.End]
	}
	if function.GoAST != nil {
		var output bytes.Buffer
		fileSet := function.GoASTFileSet
		if fileSet == nil {
			fileSet = token.NewFileSet()
		}
		if err := format.Node(&output, fileSet, function.GoAST); err == nil {
			return output.String()
		}
	}
	return ""
}

func rebaseTokens(tokens []Token, source string, offset int) []Token {
	result := make([]Token, len(tokens))
	copy(result, tokens)
	for index := range result {
		result[index].Span.Start += offset
		result[index].Span.End += offset
		result[index].Span.Line, result[index].Span.Column = sourcePosition(source, result[index].Span.Start)
	}
	return result
}

func sourcePosition(source string, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	line := 1
	lineStart := 0
	for index := 0; index < offset; index++ {
		if source[index] == '\n' {
			line++
			lineStart = index + 1
		}
	}
	return line, offset - lineStart + 1
}

func tokenError(filename string, span Span, message string) error {
	if filename == "" {
		return fmt.Errorf("%d:%d: %s", span.Line, span.Column, message)
	}
	return fmt.Errorf("%s:%d:%d: %s", filename, span.Line, span.Column, message)
}

func isTokenIdentStart(char byte) bool {
	return char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= 0x80
}

func isTokenIdentPart(char byte) bool {
	return isTokenIdentStart(char) || char >= '0' && char <= '9'
}

func isTokenNumberStart(source string, index int) bool {
	return source[index] >= '0' && source[index] <= '9'
}

func isTokenNumberPart(char byte) bool {
	return char == '_' || char == '.' || char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F'
}

func isTokenPunctuation(char byte) bool {
	if unicode.IsSpace(rune(char)) {
		return false
	}
	switch char {
	case '(', ')', '[', ']', '{', '}', ',', ';', ':', '.', '@', '?':
		return true
	default:
		return false
	}
}
