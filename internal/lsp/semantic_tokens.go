package lsp

import (
	"sort"
	"strings"

	"github.com/telgatech/gpp/compiler"
)

// Keep this order stable: clients persist token type and modifier indexes.
var semanticTokenTypes = []string{
	"namespace", "type", "class", "enum", "interface", "struct",
	"typeParameter", "parameter", "variable", "property", "enumMember",
	"function", "method", "macro", "keyword", "modifier", "comment",
	"string", "number", "operator", "decorator",
}

var semanticTokenModifiers = []string{
	"declaration", "readonly", "static", "deprecated", "defaultLibrary",
}

const (
	semanticNamespace = iota
	semanticType
	semanticClass
	semanticEnum
	semanticInterface
	semanticStruct
	semanticTypeParameter
	semanticParameter
	semanticVariable
	semanticProperty
	semanticEnumMember
	semanticFunction
	semanticMethod
	semanticMacro
	semanticKeyword
	semanticModifier
	semanticComment
	semanticString
	semanticNumber
	semanticOperator
	semanticDecorator
)

const (
	modifierDeclaration = 1 << iota
	modifierReadonly
	modifierStatic
	modifierDeprecated
	modifierDefaultLibrary
)

type semanticToken struct {
	line      int
	character int
	length    int
	tokenType int
	modifiers int
	start     int
	end       int
}

type semanticTokensParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

func (s *server) semanticTokens(uri string) map[string]any {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil {
		return map[string]any{"data": []uint32{}}
	}
	classified := map[int]semanticToken{}
	if state.File != nil {
		classifyGoPlusTokens(state.File, classified)
	}
	for _, token := range state.FileTokens() {
		if token.Kind == compiler.TokenEOF || token.Kind == compiler.TokenNewline || token.Kind == compiler.TokenPunctuation {
			continue
		}
		if body := templateBodyAt(state.File, token.Span.Start); body != nil {
			continue
		}
		classifiedToken, ok := classified[token.Span.Start]
		if !ok {
			switch token.Kind {
			case compiler.TokenKeyword:
				classifiedToken.tokenType = semanticKeyword
			case compiler.TokenComment:
				classifiedToken.tokenType = semanticComment
			case compiler.TokenString, compiler.TokenRawString, compiler.TokenRune:
				classifiedToken.tokenType = semanticString
			case compiler.TokenNumber:
				classifiedToken.tokenType = semanticNumber
			case compiler.TokenOperator:
				classifiedToken.tokenType = semanticOperator
			case compiler.TokenIdentifier:
				classifiedToken.tokenType = semanticVariable
			default:
				continue
			}
		}
		for _, part := range splitSourceLines(state.Source, token.Span.Start, token.Span.End) {
			line, character := positionAtByteOffset(state.Source, part[0])
			length := utf16Length(state.Source[part[0]:part[1]])
			if length == 0 {
				continue
			}
			classified[part[0]] = semanticToken{line: line, character: character, length: length, tokenType: classifiedToken.tokenType, modifiers: semanticModifiersAt(classified, token.Span.Start), start: part[0], end: part[1]}
		}
	}

	// Template bodies are opaque HTML/XML payloads. Return one string token per
	// line so every token range stays single-line and valid for LSP clients.
	if state.File != nil {
		for _, declaration := range state.File.Decls {
			template, ok := declaration.(*compiler.TemplateDecl)
			if !ok || template.BodySpan.End <= template.BodySpan.Start || template.BodySpan.End > len(state.Source) {
				continue
			}
			for _, part := range splitSourceLines(state.Source, template.BodySpan.Start, template.BodySpan.End) {
				line, character := positionAtByteOffset(state.Source, part[0])
				classified[part[0]] = semanticToken{line: line, character: character, length: utf16Length(state.Source[part[0]:part[1]]), tokenType: semanticString, start: part[0], end: part[1]}
			}
		}
	}

	tokens := make([]semanticToken, 0, len(classified))
	for _, token := range classified {
		if token.length > 0 {
			tokens = append(tokens, token)
		}
	}
	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].line != tokens[j].line {
			return tokens[i].line < tokens[j].line
		}
		return tokens[i].character < tokens[j].character
	})
	data := make([]uint32, 0, len(tokens)*5)
	lastLine, lastCharacter := 0, 0
	for _, token := range tokens {
		deltaLine := token.line - lastLine
		deltaCharacter := token.character
		if deltaLine == 0 {
			deltaCharacter -= lastCharacter
		}
		if deltaLine < 0 || deltaCharacter < 0 {
			continue
		}
		data = append(data, uint32(deltaLine), uint32(deltaCharacter), uint32(token.length), uint32(token.tokenType), uint32(token.modifiers))
		lastLine, lastCharacter = token.line, token.character
	}
	return map[string]any{"data": data}
}

func (state *fileState) FileTokens() []compiler.Token {
	if state == nil || state.File == nil {
		return nil
	}
	return state.File.Tokens
}

func classifyGoPlusTokens(file *compiler.File, result map[int]semanticToken) {
	classNames := map[string]bool{}
	enumNames := map[string]bool{}
	enumMembers := map[string]bool{}
	fieldNames := map[string]bool{}
	methodNames := map[string]bool{}
	mark := func(span compiler.Span, tokenType, modifiers int) {
		if span.End <= span.Start {
			return
		}
		result[span.Start] = semanticToken{tokenType: tokenType, modifiers: modifiers, start: span.Start, end: span.End}
	}
	markNameAfter := func(span compiler.Span, keyword string, tokenType, modifiers int) {
		for index, token := range file.Tokens {
			if token.Span.Start < span.Start || token.Span.Start >= span.End || token.Text != keyword {
				continue
			}
			for next := index + 1; next < len(file.Tokens) && file.Tokens[next].Span.Start < span.End; next++ {
				candidate := file.Tokens[next]
				if candidate.Kind == compiler.TokenNewline || candidate.Kind == compiler.TokenComment {
					continue
				}
				if candidate.Kind == compiler.TokenIdentifier {
					mark(candidate.Span, tokenType, modifiers)
				}
				break
			}
			return
		}
	}
	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *compiler.ClassDecl:
			classNames[value.Name] = true
			markNameAfter(value.SpanValue, "class", semanticClass, modifierDeclaration)
			for _, field := range value.Fields {
				fieldNames[field.Name] = true
				mark(firstIdentifierInSpan(file.Tokens, field.SpanValue), semanticProperty, modifierDeclaration)
			}
			for _, method := range value.Methods {
				methodNames[method.Name] = true
				modifiers := modifierDeclaration
				if method.IsStatic {
					modifiers |= modifierStatic
				}
				mark(method.NameSpan, semanticMethod, modifiers)
				markParameters(method.ParameterAST, mark)
			}
		case *compiler.EnumDecl:
			enumNames[value.Name] = true
			markNameAfter(value.SpanValue, "enum", semanticEnum, modifierDeclaration)
			for _, member := range value.Members {
				enumMembers[member.Name] = true
			}
			inBody := false
			lineHasMember := false
			for _, token := range file.Tokens {
				if token.Span.Start < value.SpanValue.Start || token.Span.Start >= value.SpanValue.End {
					continue
				}
				if token.Text == "{" {
					inBody = true
					continue
				}
				if token.Text == "}" {
					break
				}
				if token.Kind == compiler.TokenNewline || token.Text == "," {
					lineHasMember = false
					continue
				}
				if inBody && !lineHasMember && token.Kind == compiler.TokenIdentifier {
					mark(token.Span, semanticEnumMember, modifierDeclaration|modifierReadonly)
					lineHasMember = true
				}
			}
		case *compiler.AnnotationDecl:
			markNameAfter(value.SpanValue, "annotation", semanticDecorator, modifierDeclaration)
			markParameters(value.ParameterAST, mark)
		case *compiler.TemplateDecl:
			markNameAfter(value.SpanValue, "template", semanticFunction, modifierDeclaration)
			markParameters(value.ParameterAST, mark)
		case *compiler.FunctionDecl:
			markNameAfter(value.SourceSpan, "func", semanticFunction, modifierDeclaration)
			markParameters(value.Method.ParameterAST, mark)
		case *compiler.ExtendDecl:
			for _, method := range value.Methods {
				methodNames[method.Name] = true
				mark(method.NameSpan, semanticMethod, modifierDeclaration)
				markParameters(method.ParameterAST, mark)
			}
		}
	}
	for index, token := range file.Tokens {
		if token.Kind != compiler.TokenIdentifier {
			continue
		}
		switch {
		case classNames[token.Text]:
			mark(token.Span, semanticClass, result[token.Span.Start].modifiers)
		case enumNames[token.Text]:
			mark(token.Span, semanticEnum, result[token.Span.Start].modifiers)
		case index >= 2 && file.Tokens[index-1].Text == "." && enumMembers[token.Text] && enumNames[file.Tokens[index-2].Text]:
			mark(token.Span, semanticEnumMember, 0)
		case fieldNames[token.Text]:
			mark(token.Span, semanticProperty, result[token.Span.Start].modifiers)
		case methodNames[token.Text]:
			mark(token.Span, semanticMethod, result[token.Span.Start].modifiers)
		}
	}
	for index, token := range file.Tokens {
		if token.Text != "record" || index+1 >= len(file.Tokens) || file.Tokens[index+1].Text != "(" {
			continue
		}
		depth := 0
		for cursor := index + 1; cursor < len(file.Tokens); cursor++ {
			candidate := file.Tokens[cursor]
			switch candidate.Text {
			case "(":
				depth++
			case ")":
				depth--
				if depth == 0 {
					cursor = len(file.Tokens)
				}
			case ":":
				if depth == 1 && cursor > 0 && file.Tokens[cursor-1].Kind == compiler.TokenIdentifier {
					mark(file.Tokens[cursor-1].Span, semanticProperty, 0)
				}
			}
		}
	}

	annotationDepth := 0
	for index, token := range file.Tokens {
		if token.Text == "@" && index+1 < len(file.Tokens) && file.Tokens[index+1].Text == "{" {
			annotationDepth = 1
			continue
		}
		if annotationDepth == 0 {
			continue
		}
		if token.Text == "{" {
			annotationDepth++
		} else if token.Text == "}" {
			annotationDepth--
		} else if token.Kind == compiler.TokenIdentifier {
			mark(token.Span, semanticDecorator, 0)
		}
	}

	// Lambda parameter names are identified by the Go++ arrow token.
	for index, token := range file.Tokens {
		if token.Text != "=>" || index == 0 {
			continue
		}
		previous := index - 1
		if file.Tokens[previous].Text == ")" {
			depth := 1
			for previous--; previous >= 0; previous-- {
				if file.Tokens[previous].Text == ")" {
					depth++
				} else if file.Tokens[previous].Text == "(" {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			for cursor := previous + 1; cursor < index-1; cursor++ {
				if file.Tokens[cursor].Kind == compiler.TokenIdentifier {
					mark(file.Tokens[cursor].Span, semanticParameter, 0)
				}
			}
		} else if file.Tokens[previous].Kind == compiler.TokenIdentifier {
			mark(file.Tokens[previous].Span, semanticParameter, 0)
		}
	}

	// Catch bindings and the special atExit hook are ordinary functions/locals
	// syntactically, but dedicated tokens make their Go++ intent visible.
	for index, token := range file.Tokens {
		if token.Text == "catch" && index+1 < len(file.Tokens) && file.Tokens[index+1].Kind == compiler.TokenIdentifier {
			mark(file.Tokens[index+1].Span, semanticParameter, 0)
		}
		if token.Text == "atExit" && token.Kind == compiler.TokenIdentifier {
			mark(token.Span, semanticFunction, modifierDeclaration)
		}
	}
}

func markParameters(parameters []compiler.ParameterNode, mark func(compiler.Span, int, int)) {
	for _, parameter := range parameters {
		if parameter.Name == "" {
			continue
		}
		mark(parameter.SpanValue, semanticParameter, modifierDeclaration)
	}
}

func firstIdentifierInSpan(tokens []compiler.Token, span compiler.Span) compiler.Span {
	for _, token := range tokens {
		if token.Span.Start >= span.Start && token.Span.Start < span.End && token.Kind == compiler.TokenIdentifier {
			return token.Span
		}
	}
	return compiler.Span{}
}

func templateBodyAt(file *compiler.File, offset int) *compiler.TemplateDecl {
	if file == nil {
		return nil
	}
	for _, declaration := range file.Decls {
		if template, ok := declaration.(*compiler.TemplateDecl); ok && offset >= template.BodySpan.Start && offset < template.BodySpan.End {
			return template
		}
	}
	return nil
}

func splitSourceLines(source string, start, end int) [][2]int {
	var result [][2]int
	for lineStart := start; lineStart < end; {
		lineEnd := strings.IndexByte(source[lineStart:end], '\n')
		if lineEnd < 0 {
			lineEnd = end
		} else {
			lineEnd += lineStart
		}
		if lineEnd > lineStart {
			if source[lineEnd-1] == '\r' {
				lineEnd--
			}
		}
		if lineEnd > lineStart {
			result = append(result, [2]int{lineStart, lineEnd})
		}
		if lineEnd >= end {
			break
		}
		lineStart = lineEnd + 1
	}
	return result
}

func positionAtByteOffset(source string, offset int) (int, int) {
	position := Position{}
	for index, r := range source {
		if index >= offset {
			break
		}
		if r == '\n' {
			position.Line++
			position.Character = 0
			continue
		}
		if r > 0xffff {
			position.Character += 2
		} else {
			position.Character++
		}
	}
	return position.Line, position.Character
}

func semanticModifiersAt(tokens map[int]semanticToken, start int) int {
	if token, ok := tokens[start]; ok {
		return token.modifiers
	}
	return 0
}
