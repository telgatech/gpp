package lsp

import (
	"go/types"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/telgatech/gpp/compiler"
)

var hexColorPattern = regexp.MustCompile(`#[[:xdigit:]]{3,8}`)

func (s *server) inlineCompletions(params textDocumentPositionParams) []map[string]any {
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if state == nil || state.File == nil {
		return []map[string]any{}
	}
	offset := offsetAt(state.Source, params.Position)
	if insideImportContext(state, offset) {
		return []map[string]any{}
	}
	start, _ := completionReplacementOffsets(state, offset)
	if start < 0 || start > offset || offset > len(state.Source) {
		return []map[string]any{}
	}
	prefix := state.Source[start:offset]
	if utf8.RuneCountInString(prefix) < 2 {
		return []map[string]any{}
	}
	for index := 0; index < len(prefix); index++ {
		if !isIdentifierByte(prefix[index]) {
			return []map[string]any{}
		}
	}
	items := s.completions(params)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if !strings.HasPrefix(strings.ToLower(item.Label), strings.ToLower(prefix)) {
			continue
		}
		if item.TextEdit == nil || len(item.AdditionalTextEdits) != 0 {
			continue
		}
		if item.TextEdit.Range.Start.Line != item.TextEdit.Range.End.Line {
			continue
		}
		var insertText any = item.TextEdit.NewText
		if item.InsertTextFormat == 2 {
			insertText = map[string]any{"kind": "snippet", "value": item.TextEdit.NewText}
		}
		result = append(result, map[string]any{
			"insertText": insertText,
			"filterText": item.Label,
			"range":      item.TextEdit.Range,
		})
	}
	return result
}

func insideImportContext(state *fileState, offset int) bool {
	if state == nil || state.File == nil {
		return false
	}
	tokens := state.File.Tokens
	for index, token := range tokens {
		if token.Text != "import" || token.Span.Start > offset {
			continue
		}
		next := index + 1
		for next < len(tokens) && (tokens[next].Kind == compiler.TokenNewline || tokens[next].Kind == compiler.TokenComment) {
			next++
		}
		if next < len(tokens) && tokens[next].Text == "(" {
			depth := 1
			for close := next + 1; close < len(tokens); close++ {
				switch tokens[close].Text {
				case "(":
					depth++
				case ")":
					depth--
					if depth == 0 {
						if offset >= token.Span.Start && offset <= tokens[close].Span.End {
							return true
						}
						close = len(tokens)
					}
				}
			}
			continue
		}
		line := token.Span.Line
		if offset >= token.Span.Start && positionAtOffset(state.Source, offset).Line == line {
			return true
		}
	}
	return false
}

func (s *server) linkedEditingRange(params textDocumentPositionParams) map[string]any {
	name, _ := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" {
		return nil
	}
	var ranges []Range
	for _, location := range s.references(params.TextDocument.URI, params.Position, true) {
		if location.URI == params.TextDocument.URI {
			ranges = append(ranges, location.Range)
		}
	}
	if len(ranges) < 2 {
		return nil
	}
	return map[string]any{"ranges": ranges}
}

func (s *server) inlineValues(uri string, requested Range) []map[string]any {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil || state.File == nil {
		return []map[string]any{}
	}
	var result []map[string]any
	for _, token := range state.File.Tokens {
		if s.cancelled() {
			return []map[string]any{}
		}
		if token.Kind != compiler.TokenIdentifier {
			continue
		}
		tokenRange := rangeForOffsets(state.Source, token.Span.Start, token.Span.End)
		if compareLSPPosition(tokenRange.Start, requested.Start) < 0 || compareLSPPosition(tokenRange.End, requested.End) > 0 {
			continue
		}
		binding, ok := s.localBindingAt(state, token.Span.Start, token.Text)
		if !ok || binding.Kind == "" || token.Span.Start < binding.ScopeStart || token.Span.Start > binding.ScopeEnd {
			continue
		}
		result = append(result, map[string]any{
			"range":               tokenRange,
			"expression":          token.Text,
			"caseSensitiveLookup": true,
		})
	}
	result = append(result, s.inlineValuesNativeGo(state, requested)...)
	return result
}

type colorValue struct {
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
	Alpha float64 `json:"alpha"`
}

func (s *server) documentColors(uri string) []map[string]any {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil || state.File == nil {
		return []map[string]any{}
	}
	result := []map[string]any{}
	seen := map[int]bool{}
	addSpan := func(start, end int) {
		if start < 0 || end > len(state.Source) || start >= end {
			return
		}
		for _, match := range hexColorPattern.FindAllStringIndex(state.Source[start:end], -1) {
			colorStart, colorEnd := start+match[0], start+match[1]
			if colorEnd < end && isHexDigit(state.Source[colorEnd]) {
				continue
			}
			if seen[colorStart] {
				continue
			}
			color, ok := parseHexColor(state.Source[colorStart:colorEnd])
			if !ok {
				continue
			}
			seen[colorStart] = true
			result = append(result, map[string]any{"range": rangeForOffsets(state.Source, colorStart, colorEnd), "color": color})
		}
	}
	for _, token := range state.File.Tokens {
		if token.Kind == compiler.TokenString || token.Kind == compiler.TokenRawString {
			addSpan(token.Span.Start, token.Span.End)
		}
	}
	for _, declaration := range state.File.Decls {
		if template, ok := declaration.(*compiler.TemplateDecl); ok {
			addSpan(template.BodySpan.Start, template.BodySpan.End)
		}
	}
	return result
}

func isHexDigit(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func parseHexColor(value string) (colorValue, bool) {
	if len(value) == 0 || value[0] != '#' {
		return colorValue{}, false
	}
	components := strings.TrimPrefix(value, "#")
	var red, green, blue, alpha uint8
	alpha = 255
	switch len(components) {
	case 3, 4:
		red = expandNibble(components[0])
		green = expandNibble(components[1])
		blue = expandNibble(components[2])
		if len(components) == 4 {
			alpha = expandNibble(components[3])
		}
	case 6, 8:
		var ok bool
		red, ok = parseHexByte(components[0:2])
		if !ok {
			return colorValue{}, false
		}
		green, ok = parseHexByte(components[2:4])
		if !ok {
			return colorValue{}, false
		}
		blue, ok = parseHexByte(components[4:6])
		if !ok {
			return colorValue{}, false
		}
		if len(components) == 8 {
			alpha, ok = parseHexByte(components[6:8])
			if !ok {
				return colorValue{}, false
			}
		}
	default:
		return colorValue{}, false
	}
	return colorValue{Red: float64(red) / 255, Green: float64(green) / 255, Blue: float64(blue) / 255, Alpha: float64(alpha) / 255}, true
}

func expandNibble(value byte) uint8 {
	parsed, ok := parseHexByte(string([]byte{value, value}))
	if !ok {
		return 0
	}
	return parsed
}

func parseHexByte(value string) (uint8, bool) {
	var parsed uint8
	for _, character := range value {
		parsed <<= 4
		switch {
		case character >= '0' && character <= '9':
			parsed |= byte(character - '0')
		case character >= 'a' && character <= 'f':
			parsed |= byte(character-'a') + 10
		case character >= 'A' && character <= 'F':
			parsed |= byte(character-'A') + 10
		default:
			return 0, false
		}
	}
	return parsed, true
}

func (s *server) colorPresentations(uri string, requested Range, color colorValue) []map[string]any {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil {
		return []map[string]any{}
	}
	red, green, blue := colorByte(color.Red), colorByte(color.Green), colorByte(color.Blue)
	label := "#" + strings.ToUpper(hexByte(red)+hexByte(green)+hexByte(blue))
	if color.Alpha < 1 {
		label += strings.ToUpper(hexByte(colorByte(color.Alpha)))
	}
	return []map[string]any{{
		"label":    label,
		"textEdit": TextEdit{Range: requested, NewText: label},
	}}
}

func colorByte(value float64) uint8 {
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	return uint8(math.Round(value * 255))
}

func hexByte(value uint8) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[value>>4], digits[value&15]})
}

func (s *server) monikers(params textDocumentPositionParams) []map[string]any {
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if state == nil {
		return []map[string]any{}
	}
	name, _ := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" {
		return []map[string]any{}
	}
	offset := offsetAt(state.Source, params.Position)
	if binding, ok := s.localBindingAt(state, offset, name); ok {
		identifier := filepath.Clean(state.Path) + "#" + name + "@" + strconv.Itoa(binding.Declaration.Start)
		return []map[string]any{{"scheme": "gpp", "identifier": identifier, "unique": "document", "kind": "local"}}
	}
	choices := s.resolveSymbolsAt(params.TextDocument.URI, params.Position, name)
	if len(choices) == 0 {
		return []map[string]any{}
	}
	info := choices[0]
	kind := "local"
	if name != "" && isExportedIdentifier(name) {
		kind = "export"
	}
	return []map[string]any{{"scheme": "gpp", "identifier": semanticSymbolKey(info), "unique": "project", "kind": kind}}
}

func isExportedIdentifier(name string) bool {
	first, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(first)
}

func (s *server) inlineValuesNativeGo(state *fileState, requested Range) []map[string]any {
	cache, file := s.nativeGoInfoForFile(state)
	if cache == nil || file == nil || cache.info == nil {
		return nil
	}
	var result []map[string]any
	for identifier, object := range cache.info.Uses {
		variable, ok := object.(*types.Var)
		if !ok || variable.IsField() || variable.Parent() != nil && variable.Parent().Parent() == nil {
			continue
		}
		offset := cache.fset.Position(identifier.Pos()).Offset
		if offset < 0 || offset > len(state.Source) {
			continue
		}
		tokenRange := rangeForOffsets(state.Source, offset, offset+len(identifier.Name))
		if compareLSPPosition(tokenRange.Start, requested.Start) < 0 || compareLSPPosition(tokenRange.End, requested.End) > 0 {
			continue
		}
		result = append(result, map[string]any{"range": tokenRange, "expression": identifier.Name})
	}
	return result
}
