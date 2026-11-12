package lsp

import (
	"fmt"
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

type semanticTokenSnapshot struct {
	resultID string
	data     []uint32
}

func (s *server) semanticTokens(uri string) map[string]any {
	if s.cancelled() {
		return map[string]any{"data": []uint32{}}
	}
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil {
		return map[string]any{"data": []uint32{}}
	}
	classified := map[int]semanticToken{}
	if state.File != nil {
		classifyGPPTokens(state.File, classified)
		s.classifyLocalBindings(state, classified)
	}
	for _, token := range state.FileTokens() {
		if s.cancelled() {
			return map[string]any{"data": []uint32{}}
		}
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

func (s *server) semanticTokensForRange(uri string, requested Range) map[string]any {
	full, _ := s.semanticTokens(uri)["data"].([]uint32)
	filtered := make([]uint32, 0, len(full))
	line, character := 0, 0
	lastLine, lastCharacter := 0, 0
	for index := 0; index+4 < len(full); index += 5 {
		deltaLine, deltaCharacter := int(full[index]), int(full[index+1])
		line += deltaLine
		if deltaLine == 0 {
			character += deltaCharacter
		} else {
			character = deltaCharacter
		}
		length := int(full[index+2])
		start := Position{Line: line, Character: character}
		end := Position{Line: line, Character: character + length}
		if compareLSPPosition(start, requested.Start) < 0 || compareLSPPosition(end, requested.End) > 0 {
			continue
		}
		deltaLine = start.Line - lastLine
		deltaCharacter = start.Character
		if deltaLine == 0 {
			deltaCharacter -= lastCharacter
		}
		filtered = append(filtered, uint32(deltaLine), uint32(deltaCharacter), full[index+2], full[index+3], full[index+4])
		lastLine, lastCharacter = start.Line, start.Character
	}
	return map[string]any{"data": filtered}
}

func (s *server) semanticTokensDelta(uri, previousResultID string) map[string]any {
	full, _ := s.semanticTokens(uri)["data"].([]uint32)
	path := pathFromURI(uri)
	s.semanticTokensMu.Lock()
	defer s.semanticTokensMu.Unlock()
	if s.semanticTokensByURI == nil {
		s.semanticTokensByURI = make(map[string]semanticTokenSnapshot)
	}
	s.semanticTokensSequence++
	resultID := fmt.Sprintf("%d", s.semanticTokensSequence)
	previous, hasPrevious := s.semanticTokensByURI[path]
	s.semanticTokensByURI[path] = semanticTokenSnapshot{resultID: resultID, data: append([]uint32(nil), full...)}
	if !hasPrevious || previous.resultID != previousResultID {
		return map[string]any{"resultId": resultID, "data": full}
	}
	prefix := 0
	for prefix+5 <= len(previous.data) && prefix+5 <= len(full) && equalSemanticToken(previous.data[prefix:prefix+5], full[prefix:prefix+5]) {
		prefix += 5
	}
	suffix := 0
	for suffix+5 <= len(previous.data)-prefix && suffix+5 <= len(full)-prefix && equalSemanticToken(previous.data[len(previous.data)-suffix-5:len(previous.data)-suffix], full[len(full)-suffix-5:len(full)-suffix]) {
		suffix += 5
	}
	edit := map[string]any{
		"start":       prefix,
		"deleteCount": len(previous.data) - prefix - suffix,
		"data":        append([]uint32(nil), full[prefix:len(full)-suffix]...),
	}
	return map[string]any{"resultId": resultID, "edits": []any{edit}}
}

func equalSemanticToken(left, right []uint32) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func compareLSPPosition(left, right Position) int {
	if left.Line < right.Line {
		return -1
	}
	if left.Line > right.Line {
		return 1
	}
	if left.Character < right.Character {
		return -1
	}
	if left.Character > right.Character {
		return 1
	}
	return 0
}

func (s *server) classifyLocalBindings(state *fileState, classified map[int]semanticToken) {
	if state == nil || state.File == nil {
		return
	}
	for _, token := range state.File.Tokens {
		if s.cancelled() {
			return
		}
		if token.Kind != compiler.TokenIdentifier {
			continue
		}
		binding, ok := s.localBindingAt(state, token.Span.Start, token.Text)
		if !ok {
			continue
		}
		if binding.Kind == "parameter" && binding.Method != nil && token.Span.Start < binding.Method.BodySpan.Start && token.Span.Start != binding.Declaration.Start {
			continue
		}
		tokenType := semanticVariable
		if binding.Kind == "parameter" {
			tokenType = semanticParameter
		}
		modifiers := 0
		if token.Span.Start == binding.Declaration.Start {
			modifiers |= modifierDeclaration
		}
		if binding.Kind == "constant" {
			modifiers |= modifierReadonly
		}
		classified[token.Span.Start] = semanticToken{tokenType: tokenType, modifiers: modifiers, start: token.Span.Start, end: token.Span.End}
	}
}

func (state *fileState) FileTokens() []compiler.Token {
	if state == nil || state.File == nil {
		return nil
	}
	return state.File.Tokens
}

func classifyGPPTokens(file *compiler.File, result map[int]semanticToken) {
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
			classTypeParams := markDeclaredTypeParameters(file.Tokens, value.Name, value.SpanValue, value.TypeParamsAST, mark)
			markTypeParameterUses(file.Tokens, classTypeParams, value.SpanValue, mark)
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
				methodTypeParams := markDeclaredTypeParameters(file.Tokens, method.Name, method.SpanValue, method.TypeParamsAST, mark)
				markTypeParameterUses(file.Tokens, methodTypeParams, method.SpanValue, mark)
				markParameters(file.Tokens, method.ParameterAST, method.ParametersSpan, mark)
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
			markParameters(file.Tokens, value.ParameterAST, parameterListSpan(file.Tokens, value.SpanValue, "annotation"), mark)
		case *compiler.TemplateDecl:
			markNameAfter(value.SpanValue, "template", semanticFunction, modifierDeclaration)
			markParameters(file.Tokens, value.ParameterAST, parameterListSpan(file.Tokens, value.SpanValue, "template"), mark)
		case *compiler.FunctionDecl:
			markNameAfter(value.SourceSpan, "func", semanticFunction, modifierDeclaration)
			functionTypeParams := markDeclaredTypeParameters(file.Tokens, value.Name, value.SourceSpan, value.Method.TypeParamsAST, mark)
			markTypeParameterUses(file.Tokens, functionTypeParams, value.SourceSpan, mark)
			markParameters(file.Tokens, value.Method.ParameterAST, value.Method.ParametersSpan, mark)
		case *compiler.MixedDecl:
			for _, function := range value.Functions {
				if function == nil {
					continue
				}
				mark(function.Method.NameSpan, semanticFunction, modifierDeclaration)
				functionTypeParams := markDeclaredTypeParameters(file.Tokens, function.Name, function.SourceSpan, function.Method.TypeParamsAST, mark)
				markTypeParameterUses(file.Tokens, functionTypeParams, function.SourceSpan, mark)
				markParameters(file.Tokens, function.Method.ParameterAST, function.Method.ParametersSpan, mark)
			}
		case *compiler.ExtendDecl:
			for _, method := range value.Methods {
				methodNames[method.Name] = true
				mark(method.NameSpan, semanticMethod, modifierDeclaration)
				methodTypeParams := markDeclaredTypeParameters(file.Tokens, method.Name, method.SpanValue, method.TypeParamsAST, mark)
				markTypeParameterUses(file.Tokens, methodTypeParams, method.SpanValue, mark)
				markParameters(file.Tokens, method.ParameterAST, method.ParametersSpan, mark)
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

	// Lambda parameter names are identified by the GPP arrow token.
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
	// syntactically, but dedicated tokens make their GPP intent visible.
	for index, token := range file.Tokens {
		if token.Text == "catch" && index+1 < len(file.Tokens) && file.Tokens[index+1].Kind == compiler.TokenIdentifier {
			mark(file.Tokens[index+1].Span, semanticParameter, 0)
		}
		if token.Text == "atExit" && token.Kind == compiler.TokenIdentifier {
			mark(token.Span, semanticFunction, modifierDeclaration)
		}
	}
}

func markDeclaredTypeParameters(tokens []compiler.Token, declarationName string, bounds compiler.Span, parameters []compiler.TypeParameterNode, mark func(compiler.Span, int, int)) map[string]compiler.Span {
	if len(parameters) == 0 || declarationName == "" {
		return nil
	}
	names := make(map[string]bool, len(parameters))
	for _, parameter := range parameters {
		if parameter.Name != "" {
			names[parameter.Name] = true
		}
	}
	if len(names) == 0 {
		return nil
	}

	// GPP places generic parameters directly after the class or function name.
	// Find that bracket list in source tokens rather than using TypeParameterNode
	// spans, which are relative to the parsed type-parameter fragment.
	nameIndex := -1
	for index, token := range tokens {
		if token.Span.Start >= bounds.Start && token.Span.Start < bounds.End && token.Kind == compiler.TokenIdentifier && token.Text == declarationName {
			nameIndex = index
			break
		}
	}
	if nameIndex < 0 {
		return nil
	}
	open := nameIndex + 1
	for open < len(tokens) && tokens[open].Span.Start < bounds.End && tokens[open].Text != "[" && tokens[open].Text != "(" && tokens[open].Text != "{" {
		open++
	}
	if open >= len(tokens) || tokens[open].Span.Start >= bounds.End || tokens[open].Text != "[" {
		return nil
	}
	depth := 0
	close := -1
	for index := open; index < len(tokens) && tokens[index].Span.Start < bounds.End; index++ {
		switch tokens[index].Text {
		case "[":
			depth++
		case "]":
			depth--
			if depth == 0 {
				close = index
				index = len(tokens)
			}
		}
	}
	if close < 0 {
		return nil
	}

	found := map[string]compiler.Span{}
	segmentStart := open + 1
	depth = 0
	markSegment := func(end int) {
		for index := segmentStart; index < end; index++ {
			if tokens[index].Kind != compiler.TokenIdentifier {
				continue
			}
			if names[tokens[index].Text] {
				found[tokens[index].Text] = tokens[index].Span
				mark(tokens[index].Span, semanticTypeParameter, modifierDeclaration)
			}
			break
		}
	}
	for index := open + 1; index <= close; index++ {
		if index == close {
			markSegment(index)
			break
		}
		switch tokens[index].Text {
		case "[", "(", "{":
			depth++
		case "]", ")", "}":
			if depth > 0 {
				depth--
			}
		case ",":
			if depth == 0 {
				markSegment(index)
				segmentStart = index + 1
			}
		}
	}
	return found
}

func markTypeParameterUses(tokens []compiler.Token, declarations map[string]compiler.Span, bounds compiler.Span, mark func(compiler.Span, int, int)) {
	if len(declarations) == 0 {
		return
	}
	for _, token := range tokens {
		declaration, ok := declarations[token.Text]
		if token.Span.Start < bounds.Start || token.Span.End > bounds.End || token.Kind != compiler.TokenIdentifier || !ok || token.Span.Start == declaration.Start {
			continue
		}
		mark(token.Span, semanticTypeParameter, 0)
	}
}

func markParameters(tokens []compiler.Token, parameters []compiler.ParameterNode, bounds compiler.Span, mark func(compiler.Span, int, int)) {
	searchFrom := bounds.Start
	for _, parameter := range parameters {
		if parameter.Name == "" {
			continue
		}
		for _, token := range tokens {
			if token.Span.Start < searchFrom || token.Span.Start < bounds.Start || token.Span.End > bounds.End {
				continue
			}
			if token.Kind != compiler.TokenIdentifier || token.Text != parameter.Name {
				continue
			}
			mark(token.Span, semanticParameter, modifierDeclaration)
			searchFrom = token.Span.End
			break
		}
	}
}

func parameterListSpan(tokens []compiler.Token, bounds compiler.Span, declarationKeyword string) compiler.Span {
	for index, token := range tokens {
		if token.Span.Start < bounds.Start || token.Span.Start >= bounds.End || token.Text != declarationKeyword {
			continue
		}
		for open := index + 1; open < len(tokens) && tokens[open].Span.Start < bounds.End; open++ {
			if tokens[open].Text != "(" {
				continue
			}
			depth := 1
			for close := open + 1; close < len(tokens) && tokens[close].Span.Start < bounds.End; close++ {
				switch tokens[close].Text {
				case "(":
					depth++
				case ")":
					depth--
					if depth == 0 {
						return compiler.Span{Start: tokens[open].Span.End, End: tokens[close].Span.Start}
					}
				}
			}
			return compiler.Span{}
		}
	}
	return compiler.Span{}
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
