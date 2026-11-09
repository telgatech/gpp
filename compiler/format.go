package compiler

import (
	gofmt "go/format"
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
	if formatted, ok := formatGoCompatibleSource(source); ok {
		return formatted
	}
	lines := strings.Split(source, "\n")
	dropBlankLines := make([]bool, len(lines))
	indent := 0
	state := formatScanState{}
	previousLineOpenedBlock := false
	previousCodeLineBlank := false
	templateLines, templateEnds := templateBodyLinesForFile(file, source, lines)

	for index, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[index] = ""
			if templateLines[index] || state.mode == '`' || state.mode == '/' {
				lines[index] = line
				formatBraceDelta(line, &state)
				previousLineOpenedBlock = false
				previousCodeLineBlank = false
				continue
			}
			if previousCodeLineBlank || previousLineOpenedBlock || nextNonBlankStartsWithBlockClose(lines, index+1) {
				dropBlankLines[index] = true
				previousCodeLineBlank = true
				continue
			}
			previousCodeLineBlank = true
			continue
		}
		previousCodeLineBlank = false
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
			previousLineOpenedBlock = false
			continue
		}
		// Raw strings and block comments may contain intentional leading
		// whitespace. Preserve those lines byte-for-byte while still advancing
		// the scanner state so subsequent Go++ code is indented correctly.
		if state.mode == '`' || state.mode == '/' {
			lines[index] = line
			formatBraceDelta(line, &state)
			previousLineOpenedBlock = false
			continue
		}

		content := canonicalizeFormatLine(strings.TrimLeft(line, " \t"))
		startsWithBraceClose := formatLineStartsWithClose(content, &state)
		leadingBraceCloses, leadingContinuationCloses := formatLeadingClosers(content)
		if startsWithBraceClose {
			indent -= leadingBraceCloses
			if indent < 0 {
				indent = 0
			}
		}
		continuation := state.parenDepth + state.bracketDepth - leadingContinuationCloses
		if continuation < 0 {
			continuation = 0
		}
		lineIndent := indent + continuation
		if isCaseClauseLine(content) && lineIndent > 0 {
			lineIndent--
		}
		lines[index] = strings.Repeat("    ", lineIndent) + content
		delta := formatBraceDelta(content, &state)
		if startsWithBraceClose {
			// The leading brace was already removed to align this line. Count
			// only the braces after it when determining the next line's indent.
			delta += leadingBraceCloses
		}
		indent += delta
		previousLineOpenedBlock = delta > 0
		if indent < 0 {
			indent = 0
		}
	}

	formattedLines := make([]string, 0, len(lines))
	for index, line := range lines {
		if !dropBlankLines[index] {
			formattedLines = append(formattedLines, line)
		}
	}
	return strings.TrimRight(strings.Join(formattedLines, "\n"), "\n") + "\n"
}

// formatGoCompatibleSource delegates ordinary Go files and snippets to
// gofmt. Go++ constructs that Go cannot parse continue through the
// source-preserving formatter below.
func formatGoCompatibleSource(source string) (string, bool) {
	prefix := ""
	tokens, err := LexSource("format", source)
	if err != nil {
		return "", false
	}
	first := ""
	for _, token := range tokens {
		if token.Kind == TokenComment || token.Kind == TokenNewline || token.Kind == TokenEOF {
			continue
		}
		first = token.Text
		break
	}
	if first != "package" {
		prefix = "package __gpp_format__\n\n"
	}
	formatted, err := gofmt.Source([]byte(prefix + source))
	if err != nil {
		return "", false
	}
	result := string(formatted)
	if prefix != "" {
		result = strings.TrimPrefix(result, prefix)
	}
	return trimGoBlockEdgeBlankLines(result), true
}

func trimGoBlockEdgeBlankLines(source string) string {
	lines := strings.Split(source, "\n")
	dropBlankLines := make([]bool, len(lines))
	state := formatScanState{}
	previousLineOpenedBlock := false
	previousCodeLineBlank := false
	for index, line := range lines {
		if strings.TrimSpace(line) == "" {
			if state.mode == '`' || state.mode == '/' {
				formatBraceDelta(line, &state)
				previousLineOpenedBlock = false
				previousCodeLineBlank = false
				continue
			}
			if previousCodeLineBlank || previousLineOpenedBlock || nextNonBlankStartsWithBlockClose(lines, index+1) {
				dropBlankLines[index] = true
				previousCodeLineBlank = true
				continue
			}
			previousCodeLineBlank = true
			previousLineOpenedBlock = false
			continue
		}
		previousCodeLineBlank = false
		if state.mode == '`' || state.mode == '/' {
			formatBraceDelta(line, &state)
			previousLineOpenedBlock = false
			continue
		}
		previousLineOpenedBlock = formatBraceDelta(line, &state) > 0
	}
	result := make([]string, 0, len(lines))
	for index, line := range lines {
		if !dropBlankLines[index] {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

func nextNonBlankStartsWithBlockClose(lines []string, index int) bool {
	for ; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		return strings.HasPrefix(line, "}")
	}
	return false
}

func isCaseClauseLine(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "case ") && line != "case" && !strings.HasPrefix(line, "default") {
		return false
	}
	if strings.HasPrefix(line, "default") && len(line) > len("default") && isTokenIdentPart(line[len("default")]) {
		return false
	}
	start := len("case")
	if strings.HasPrefix(line, "default") {
		start = len("default")
	}
	var quote byte
	parenDepth, bracketDepth, braceDepth := 0, 0, 0
	for index := start; index < len(line); index++ {
		char := line[index]
		if quote != 0 {
			if char == '\\' && quote != '`' {
				index++
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '/' && index+1 < len(line) && line[index+1] == '/' {
			return false
		}
		switch char {
		case '"', '\'', '`':
			quote = char
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
		case '{':
			braceDepth++
		case '}':
			braceDepth--
		case ':':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return true
			}
		}
	}
	return false
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
	line = collapseCodeWhitespace(line)
	line = canonicalizeClassHeaderSpacing(line)
	if formatted, ok := formatGoCompatibleStatement(line); ok {
		line = formatted
	} else if formatted, ok := formatGoCompatibleFunctionHeader(line); ok {
		line = formatted
	} else {
		line = spaceDefineAssignment(line)
	}
	line = canonicalizeLambdaArrowSpacing(line)
	if strings.HasPrefix(line, "}") {
		index := 0
		for index < len(line) && line[index] == '}' {
			index++
		}
		if index < len(line) && line[index] != ' ' && line[index] != '\t' &&
			(strings.HasPrefix(line[index:], "else") || strings.HasPrefix(line[index:], "catch") || strings.HasPrefix(line[index:], "finally")) {
			line = line[:index] + " " + line[index:]
		}
	}
	if isCatchHeader(line) && strings.Contains(line, ",") {
		line = canonicalizeCatchSpacing(line)
	}
	line = canonicalizeInlineBlockSpacing(line)
	line = canonicalizeCommaSpacing(line)
	line = canonicalizeClosingParenSpacing(line)
	return canonicalizeColonSpacing(line)
}

func canonicalizeInlineBlockSpacing(line string) string {
	var output strings.Builder
	output.Grow(len(line) + 8)
	var blockStack []bool
	for index := 0; index < len(line); {
		character := line[index]
		if character == '"' || character == '\'' || character == '`' {
			quote := character
			start := index
			index++
			for index < len(line) {
				if line[index] == '\\' && quote != '`' {
					index += 2
					continue
				}
				if line[index] == quote {
					index++
					break
				}
				index++
			}
			output.WriteString(line[start:index])
			continue
		}
		if character == '/' && index+1 < len(line) && (line[index+1] == '/' || line[index+1] == '*') {
			output.WriteString(line[index:])
			break
		}
		if character == '{' {
			isBlock := isBlockBracePrefix(line[:index])
			if isBlock && output.Len() > 0 {
				current := output.String()
				last := current[len(current)-1]
				if last != ' ' && last != '\t' {
					output.WriteByte(' ')
				}
			}
			output.WriteByte(character)
			blockStack = append(blockStack, isBlock)
			index++
			if isBlock {
				for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
					index++
				}
				if index < len(line) && line[index] != '}' {
					output.WriteByte(' ')
				}
			}
			continue
		}
		if character == '}' {
			isBlock := false
			if len(blockStack) > 0 {
				isBlock = blockStack[len(blockStack)-1]
				blockStack = blockStack[:len(blockStack)-1]
			}
			if isBlock {
				current := output.String()
				if len(current) > 0 && current[len(current)-1] != ' ' && current[len(current)-1] != '\t' && current[len(current)-1] != '{' {
					output.WriteByte(' ')
				}
			}
			output.WriteByte(character)
			index++
			continue
		}
		output.WriteByte(character)
		index++
	}
	return output.String()
}

func canonicalizeCommaSpacing(line string) string {
	var output strings.Builder
	output.Grow(len(line) + 8)
	for index := 0; index < len(line); {
		character := line[index]
		if character == '"' || character == '\'' || character == '`' {
			quote := character
			start := index
			index++
			for index < len(line) {
				if line[index] == '\\' && quote != '`' {
					index += 2
					continue
				}
				if line[index] == quote {
					index++
					break
				}
				index++
			}
			output.WriteString(line[start:index])
			continue
		}
		if character == '/' && index+1 < len(line) && (line[index+1] == '/' || line[index+1] == '*') {
			output.WriteString(line[index:])
			break
		}
		output.WriteByte(character)
		index++
		if character != ',' {
			continue
		}
		for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
			index++
		}
		if index < len(line) && line[index] != ')' && line[index] != ']' && line[index] != '}' && line[index] != ';' {
			output.WriteByte(' ')
		}
	}
	return output.String()
}

func canonicalizeClosingParenSpacing(line string) string {
	output := make([]byte, 0, len(line))
	for index := 0; index < len(line); {
		character := line[index]
		if character == '"' || character == '\'' || character == '`' {
			quote := character
			start := index
			index++
			for index < len(line) {
				if line[index] == '\\' && quote != '`' {
					index += 2
					continue
				}
				if line[index] == quote {
					index++
					break
				}
				index++
			}
			output = append(output, line[start:index]...)
			continue
		}
		if character == '/' && index+1 < len(line) && (line[index+1] == '/' || line[index+1] == '*') {
			output = append(output, line[index:]...)
			break
		}
		if character == ')' {
			for len(output) > 0 {
				last := output[len(output)-1]
				if last != ' ' && last != '\t' {
					break
				}
				output = output[:len(output)-1]
			}
		}
		output = append(output, character)
		index++
	}
	return string(output)
}

func canonicalizeColonSpacing(line string) string {
	var output strings.Builder
	output.Grow(len(line) + 8)
	for index := 0; index < len(line); {
		character := line[index]
		if character == '"' || character == '\'' || character == '`' {
			quote := character
			start := index
			index++
			for index < len(line) {
				if line[index] == '\\' && quote != '`' {
					index += 2
					continue
				}
				if line[index] == quote {
					index++
					break
				}
				index++
			}
			output.WriteString(line[start:index])
			continue
		}
		if character == '/' && index+1 < len(line) && (line[index+1] == '/' || line[index+1] == '*') {
			output.WriteString(line[index:])
			break
		}
		output.WriteByte(character)
		index++
		if character != ':' || index < len(line) && line[index] == '=' {
			continue
		}
		for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
			index++
		}
		if index < len(line) && line[index] != '}' && line[index] != ')' && line[index] != ']' && line[index] != ',' && line[index] != ';' {
			output.WriteByte(' ')
		}
	}
	return output.String()
}

func canonicalizeLambdaArrowSpacing(line string) string {
	var output strings.Builder
	for index := 0; index < len(line); {
		character := line[index]
		if character == '"' || character == '\'' || character == '`' {
			quote := character
			output.WriteByte(character)
			index++
			for index < len(line) {
				output.WriteByte(line[index])
				if line[index] == '\\' && quote != '`' && index+1 < len(line) {
					index++
					output.WriteByte(line[index])
				} else if line[index] == quote {
					index++
					break
				}
				index++
			}
			continue
		}
		if character == '/' && index+1 < len(line) && (line[index+1] == '/' || line[index+1] == '*') {
			output.WriteString(line[index:])
			break
		}
		if character == '=' && index+1 < len(line) && line[index+1] == '>' {
			for output.Len() > 0 {
				current := output.String()
				last := current[len(current)-1]
				if last != ' ' && last != '\t' {
					break
				}
				output.Reset()
				output.WriteString(current[:len(current)-1])
			}
			output.WriteString(" => ")
			index += 2
			for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
				index++
			}
			continue
		}
		output.WriteByte(character)
		index++
	}
	return output.String()
}

func canonicalizeClassHeaderSpacing(line string) string {
	if !strings.HasPrefix(line, "class ") {
		return line
	}

	nameStart := len("class ")
	_, nameLength := readIdent(line[nameStart:])
	if nameLength == 0 {
		return line
	}
	nameEnd := nameStart + nameLength
	colon := nameEnd
	for colon < len(line) && (line[colon] == ' ' || line[colon] == '\t') {
		colon++
	}
	if colon >= len(line) || line[colon] != ':' {
		return line
	}

	parent := colon + 1
	for parent < len(line) && (line[parent] == ' ' || line[parent] == '\t') {
		parent++
	}
	return line[:nameEnd] + " : " + line[parent:]
}

func formatGoCompatibleFunctionHeader(line string) (string, bool) {
	line = strings.TrimSpace(line)
	static := strings.HasPrefix(line, "static func ")
	if static {
		line = strings.TrimPrefix(line, "static ")
	}
	if !strings.HasPrefix(line, "func ") || !strings.HasSuffix(line, "{") {
		return "", false
	}
	source := "package __gpp_format__\n\n" + line + "}\n"
	formatted, err := gofmt.Source([]byte(source))
	if err != nil {
		return "", false
	}
	text := string(formatted)
	start := strings.Index(text, "func ")
	if start < 0 {
		return "", false
	}
	declaration := strings.TrimSpace(text[start:])
	declaration = strings.TrimSuffix(declaration, "\n")
	if !strings.HasSuffix(declaration, "}") {
		return "", false
	}
	declaration = strings.TrimSuffix(declaration, "}")
	declaration = strings.TrimRight(declaration, " \t")
	if static {
		declaration = "static " + declaration
	}
	return declaration, true
}

func formatGoCompatibleStatement(line string) (string, bool) {
	if strings.TrimSpace(line) == "" {
		return line, true
	}
	source := "package __gpp_format__\n\nfunc __gpp_format__() {\n" + line + "\n}\n"
	formatted, err := gofmt.Source([]byte(source))
	if err != nil {
		return "", false
	}
	text := string(formatted)
	start := strings.Index(text, "func __gpp_format__() {\n")
	if start < 0 {
		return "", false
	}
	start += len("func __gpp_format__() {\n")
	end := strings.LastIndex(text, "\n}\n")
	if end < start {
		return "", false
	}
	body := text[start:end]
	bodyLines := strings.Split(body, "\n")
	if len(bodyLines) > 1 {
		if !strings.HasSuffix(strings.TrimSpace(line), "{") {
			return "", false
		}
		for _, trailing := range bodyLines[1:] {
			if strings.TrimSpace(trailing) != "}" {
				return "", false
			}
		}
	}
	return strings.TrimPrefix(bodyLines[0], "\t"), true
}

func spaceDefineAssignment(line string) string {
	var output strings.Builder
	output.Grow(len(line) + 4)
	for index := 0; index < len(line); {
		switch line[index] {
		case '"', '\'', '`':
			quote := line[index]
			start := index
			index++
			for index < len(line) {
				if line[index] == '\\' && quote != '`' {
					index += 2
					continue
				}
				if line[index] == quote {
					index++
					break
				}
				index++
			}
			output.WriteString(line[start:index])
		case '/':
			if index+1 < len(line) && line[index+1] == '/' {
				output.WriteString(line[index:])
				return output.String()
			}
			if index+1 < len(line) && line[index+1] == '*' {
				end := strings.Index(line[index+2:], "*/")
				if end < 0 {
					output.WriteString(line[index:])
					return output.String()
				}
				end += index + 4
				output.WriteString(line[index:end])
				index = end
				continue
			}
			output.WriteByte(line[index])
			index++
		case ':':
			if index+1 < len(line) && line[index+1] == '=' {
				if output.Len() > 0 {
					current := output.String()
					if last := current[len(current)-1]; last != ' ' && last != '\t' {
						output.WriteByte(' ')
					}
				}
				output.WriteString(":=")
				index += 2
				for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
					index++
				}
				if index < len(line) && line[index] != ')' && line[index] != ']' && line[index] != '}' && line[index] != ',' && line[index] != ';' {
					output.WriteByte(' ')
				}
				continue
			}
			output.WriteByte(line[index])
			index++
		default:
			output.WriteByte(line[index])
			index++
		}
	}
	return output.String()
}

func collapseCodeWhitespace(line string) string {
	var output strings.Builder
	output.Grow(len(line))
	for index := 0; index < len(line); {
		switch line[index] {
		case ' ', '\t':
			for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
				index++
			}
			if output.Len() > 0 && index < len(line) {
				output.WriteByte(' ')
			}
		case '"', '\'', '`':
			quote := line[index]
			start := index
			index++
			for index < len(line) {
				if line[index] == '\\' && quote != '`' {
					index += 2
					continue
				}
				if line[index] == quote {
					index++
					break
				}
				index++
			}
			output.WriteString(line[start:index])
		case '/':
			if index+1 < len(line) && line[index+1] == '/' {
				output.WriteString(line[index:])
				return output.String()
			}
			if index+1 < len(line) && line[index+1] == '*' {
				end := strings.Index(line[index+2:], "*/")
				if end < 0 {
					output.WriteString(line[index:])
					return output.String()
				}
				end += index + 4
				output.WriteString(line[index:end])
				index = end
				continue
			}
			output.WriteByte(line[index])
			index++
		default:
			output.WriteByte(line[index])
			index++
		}
	}
	return strings.TrimRight(output.String(), " \t")
}

func isCatchHeader(line string) bool {
	header := strings.TrimSpace(line)
	for strings.HasPrefix(header, "}") {
		header = strings.TrimSpace(strings.TrimPrefix(header, "}"))
	}
	return strings.HasPrefix(header, "catch ")
}

func canonicalizeCatchSpacing(line string) string {
	brace := strings.IndexByte(line, '{')
	if brace < 0 {
		return line
	}
	header := strings.TrimSpace(line[:brace])
	closePrefix := ""
	for strings.HasPrefix(header, "}") {
		closePrefix += "}"
		header = strings.TrimSpace(strings.TrimPrefix(header, "}"))
	}
	if !strings.HasPrefix(header, "catch ") {
		return line
	}
	typesAndName := strings.TrimSpace(strings.TrimPrefix(header, "catch"))
	for strings.Contains(typesAndName, ", ") {
		typesAndName = strings.ReplaceAll(typesAndName, ", ", ",")
	}
	typesAndName = strings.ReplaceAll(typesAndName, ",", ", ")
	return closePrefix + " catch " + typesAndName + line[brace:]
}

func isBlockBracePrefix(prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if close := strings.LastIndex(prefix, "}"); close >= 0 {
		prefix = strings.TrimSpace(prefix[close+1:])
	}
	if prefix == "" || strings.HasSuffix(prefix, "@") {
		return false
	}
	if strings.HasSuffix(prefix, ")") {
		return true
	}
	if strings.HasSuffix(prefix, "=>") {
		return true
	}
	for _, keyword := range []string{"class", "enum", "extend", "func", "if", "else", "for", "switch", "select", "try", "catch", "finally", "template", "type"} {
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
	mode         byte // '/', '"', '\'', or '`'
	parenDepth   int
	bracketDepth int
}

func formatLeadingClosers(line string) (braces, continuations int) {
	index := 0
	for index < len(line) {
		for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
			index++
		}
		if index >= len(line) {
			break
		}
		switch line[index] {
		case '}':
			braces++
		case ')', ']':
			continuations++
		default:
			return braces, continuations
		}
		index++
	}
	return braces, continuations
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
		case '(':
			state.parenDepth++
			index++
		case ')':
			state.parenDepth--
			if state.parenDepth < 0 {
				state.parenDepth = 0
			}
			index++
		case '[':
			state.bracketDepth++
			index++
		case ']':
			state.bracketDepth--
			if state.bracketDepth < 0 {
				state.bracketDepth = 0
			}
			index++
		default:
			index++
		}
	}
	return delta
}
