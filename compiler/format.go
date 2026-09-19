package compiler

import (
	"strings"
)

// FormatSource applies the formatter shared by gpp fmt and editor tooling.
// It is intentionally source-preserving: the formatter canonicalizes
// indentation and line endings without attempting to reprint opaque template,
// string, or comment contents.
func FormatSource(source string) string {
	return formatSourceWithFile(nil, source)
}

// formatSourceWithFile formats a source buffer using the already parsed AST
// when one is available. The line printer remains deliberately conservative,
// but declaration-specific opaque regions (currently template bodies) now
// come from parser spans rather than a second source scan.
func formatSourceWithFile(file *File, source string) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	lines := strings.Split(source, "\n")
	indent := 0
	state := formatScanState{}
	templateLines, templateEnds := templateBodyLinesForFile(file, source, lines)

	for index, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[index] = ""
			continue
		}
		if templateLines[index] {
			// Template bodies are opaque HTML/template source. Preserve their
			// whitespace and only close the surrounding declaration after its
			// final body line.
			lines[index] = line
			if templateEnds[index] {
				indent--
				if indent < 0 {
					indent = 0
				}
			}
			continue
		}
		// Raw strings and block comments may contain intentional leading
		// whitespace. Preserve those lines byte-for-byte while still advancing
		// the scanner state so subsequent Go++ code is indented correctly.
		if state.mode == '`' || state.mode == '/' {
			lines[index] = line
			formatBraceDelta(line, &state)
			continue
		}

		content := canonicalizeFormatLine(strings.TrimLeft(line, " \t"))
		if formatLineStartsWithClose(content, &state) {
			indent--
			if indent < 0 {
				indent = 0
			}
		}
		lines[index] = strings.Repeat("    ", indent) + content
		indent += formatBraceDelta(content, &state)
		if indent < 0 {
			indent = 0
		}
	}

	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func templateBodyLines(source string, lines []string) (map[int]bool, map[int]bool) {
	bodyLines := map[int]bool{}
	bodyEnds := map[int]bool{}
	lineOffsets := make([]int, len(lines))
	offset := 0
	for index, line := range lines {
		lineOffsets[index] = offset
		offset += len(line) + 1
	}
	lineForOffset := func(target int) int {
		for index := len(lineOffsets) - 1; index >= 0; index-- {
			if target >= lineOffsets[index] {
				return index
			}
		}
		return 0
	}

	for index := 0; index < len(source); {
		var ignored strings.Builder
		end, ok, err := copyIgnoredSource(source, index, &ignored)
		if err == nil && ok {
			index = end
			continue
		}
		if !keywordAt(source, index, "template") {
			index++
			continue
		}
		open, close, end, ok := templateBodySpan(source, index)
		if !ok {
			index++
			continue
		}
		openLine := lineForOffset(open)
		closeLine := lineForOffset(close)
		for line := openLine + 1; line <= closeLine && line < len(lines); line++ {
			bodyLines[line] = true
		}
		if closeLine > openLine {
			bodyEnds[closeLine] = true
		}
		index = end
	}
	return bodyLines, bodyEnds
}

func templateBodyLinesForFile(file *File, source string, lines []string) (map[int]bool, map[int]bool) {
	if file == nil {
		return templateBodyLines(source, lines)
	}
	bodyLines := map[int]bool{}
	bodyEnds := map[int]bool{}
	lineForOffset := func(target int) int {
		if target < 0 {
			target = 0
		}
		if target > len(source) {
			target = len(source)
		}
		return strings.Count(source[:target], "\n")
	}
	for _, declaration := range file.Decls {
		template, ok := declaration.(*TemplateDecl)
		if !ok || template.BodySpan.Start < 0 || template.BodySpan.End < template.BodySpan.Start {
			continue
		}
		openLine := lineForOffset(template.SpanValue.Start)
		closeLine := lineForOffset(template.BodySpan.End)
		for line := openLine + 1; line <= closeLine && line < len(lines); line++ {
			bodyLines[line] = true
		}
		if closeLine > openLine {
			bodyEnds[closeLine] = true
		}
	}
	return bodyLines, bodyEnds
}

func templateBodySpan(source string, start int) (int, int, int, bool) {
	position := skipSpace(source, start+len("template"))
	_, nameLength := readIdent(source[position:])
	if nameLength == 0 {
		return 0, 0, 0, false
	}
	position = skipSpace(source, position+nameLength)
	if position >= len(source) || source[position] != '(' {
		return 0, 0, 0, false
	}
	parametersEnd, err := findMatchingParen(source, position)
	if err != nil {
		return 0, 0, 0, false
	}
	position = skipSpace(source, parametersEnd+1)
	if position < len(source) && source[position] == ':' {
		position = skipSpace(source, position+1)
		_, layoutLength := readIdent(source[position:])
		if layoutLength == 0 {
			return 0, 0, 0, false
		}
		position = skipSpace(source, position+layoutLength)
	}
	_, position, err = parseOptionalAnnotationUses(source, position)
	if err != nil {
		return 0, 0, 0, false
	}
	position = skipSpace(source, position)
	if position >= len(source) || source[position] != '{' {
		return 0, 0, 0, false
	}
	close, err := findTemplateBodyClose(source, position)
	if err != nil {
		return 0, 0, 0, false
	}
	return position, close, close + 1, true
}

func canonicalizeFormatLine(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "catch ") && strings.Contains(line, ",") {
		line = canonicalizeCatchSpacing(line)
	}
	for index := 0; index < len(line); {
		switch line[index] {
		case '"', '\'', '`':
			end := index + 1
			for end < len(line) {
				if line[end] == '\\' && line[index] != '`' {
					end += 2
					continue
				}
				if line[end] == line[index] {
					end++
					break
				}
				end++
			}
			index = end
		case '/':
			if index+1 < len(line) && line[index+1] == '/' {
				return line
			}
			if index+1 < len(line) && line[index+1] == '*' {
				return line
			}
			index++
		case '{':
			if index+1 < len(line) && line[index+1] == '{' {
				index += 2
				continue
			}
			if index > 0 && line[index-1] != ' ' && line[index-1] != '\t' && line[index-1] != '@' && isBlockBracePrefix(line[:index]) {
				return line[:index] + " " + line[index:]
			}
			return line
		default:
			index++
		}
	}
	return line
}

func canonicalizeCatchSpacing(line string) string {
	brace := strings.IndexByte(line, '{')
	if brace < 0 {
		return line
	}
	header := strings.TrimSpace(line[:brace])
	if !strings.HasPrefix(header, "catch ") {
		return line
	}
	typesAndName := strings.TrimSpace(strings.TrimPrefix(header, "catch"))
	for strings.Contains(typesAndName, ",  ") {
		typesAndName = strings.ReplaceAll(typesAndName, ",  ", ", ")
	}
	typesAndName = strings.ReplaceAll(typesAndName, ",", ", ")
	return "catch " + typesAndName + line[brace:]
}

func isBlockBracePrefix(prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || strings.HasSuffix(prefix, "@") {
		return false
	}
	if strings.HasSuffix(prefix, ")") {
		return true
	}
	for _, keyword := range []string{"class", "enum", "extend", "func", "if", "else", "for", "switch", "select", "try", "catch", "finally", "template"} {
		if prefix == keyword || strings.HasPrefix(prefix, keyword+" ") {
			return true
		}
	}
	return false
}

// FormatSourceFile validates source before formatting it. The CLI uses this
// entry point so malformed input is reported without partially rewriting the
// file. FormatSource remains useful to the LSP for buffer formatting.
func FormatSourceFile(filename string, source []byte) ([]byte, error) {
	file, err := ParseFile(filename, string(source))
	if err != nil {
		return nil, err
	}
	return []byte(formatSourceWithFile(file, string(source))), nil
}

type formatScanState struct {
	mode byte // '/', '"', '\'', or '`'
}

func formatLineStartsWithClose(line string, state *formatScanState) bool {
	probe := *state
	for index := 0; index < len(line); {
		if probe.mode != 0 {
			if probe.mode == '/' {
				if index+1 < len(line) && line[index] == '*' && line[index+1] == '/' {
					probe.mode = 0
					index += 2
					continue
				}
				index++
				continue
			}
			if line[index] == '\\' && probe.mode != '`' {
				index += 2
				continue
			}
			if line[index] == probe.mode {
				probe.mode = 0
			}
			index++
			continue
		}

		if line[index] == '/' && index+1 < len(line) && line[index+1] == '/' {
			return false
		}
		if line[index] == '/' && index+1 < len(line) && line[index+1] == '*' {
			probe.mode = '/'
			index += 2
			continue
		}
		switch line[index] {
		case '"', '\'', '`':
			probe.mode = line[index]
			index++
		case '{', '}':
			return line[index] == '}'
		default:
			if line[index] != ' ' && line[index] != '\t' {
				return false
			}
			index++
		}
	}
	return false
}

func formatBraceDelta(line string, state *formatScanState) int {
	delta := 0
	for index := 0; index < len(line); {
		if state.mode != 0 {
			switch state.mode {
			case '/':
				if index+1 < len(line) && line[index] == '*' && line[index+1] == '/' {
					state.mode = 0
					index += 2
					continue
				}
				index++
				continue
			case '`':
				if line[index] == '`' {
					state.mode = 0
				}
				index++
				continue
			default:
				if line[index] == '\\' {
					index += 2
					continue
				}
				if line[index] == state.mode {
					state.mode = 0
				}
				index++
				continue
			}
		}

		if line[index] == '/' && index+1 < len(line) {
			switch line[index+1] {
			case '/':
				return delta
			case '*':
				state.mode = '/'
				index += 2
				continue
			}
		}
		switch line[index] {
		case '"', '\'', '`':
			state.mode = line[index]
			index++
		case '{':
			// Template actions use a double brace pair. They are opaque
			// template syntax rather than Go++ block delimiters.
			if index+1 < len(line) && line[index+1] == '{' {
				index += 2
				continue
			}
			delta++
			index++
		case '}':
			if index+1 < len(line) && line[index+1] == '}' {
				index += 2
				continue
			}
			delta--
			index++
		default:
			index++
		}
	}
	return delta
}
