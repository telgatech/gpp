package compiler

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)

func ParseFile(name, src string) (*File, error) {
	file := &File{
		Name:    name,
		Package: "main",
	}
	hasPackage := false

	pos := 0

	for pos < len(src) {
		pos = skipSpace(src, pos)

		if pos >= len(src) {
			break
		}

		switch {
		case keywordAt(src, pos, "package"):
			if hasPackage {
				return nil, fmt.Errorf(
					"%s:%d: duplicate package declaration",
					name,
					sourceLine(src, pos),
				)
			}

			end := lineEnd(src, pos)

			line := strings.TrimSpace(src[pos:end])
			parts := strings.Fields(line)

			if len(parts) != 2 || !isLogicalPackageName(parts[1]) {
				return nil, fmt.Errorf(
					"%s:%d: invalid package declaration",
					name,
					sourceLine(src, pos),
				)
			}

			file.Package = parts[1]
			hasPackage = true
			pos = end

		case legacyUseAt(src, pos):
			return nil, fmt.Errorf(
				"%s:%d: use declarations are no longer supported; use Go import syntax",
				name,
				sourceLine(src, pos),
			)

		case keywordAt(src, pos, "class"):
			class, end, err := parseClass(src, pos)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", name, sourceLine(src, pos), err)
			}
			class.SourceFile = name
			class.SourceLine = sourceLine(src, pos)

			file.Decls = append(file.Decls, class)
			pos = end

		case keywordAt(src, pos, "extend"):
			extend, end, err := parseExtend(src, pos)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", name, sourceLine(src, pos), err)
			}
			extend.SourceFile = name
			extend.SourceLine = sourceLine(src, pos)
			file.Decls = append(file.Decls, extend)
			pos = end

		default:
			start := pos

			next := findNextExtension(src, pos)

			if next == pos {
				return nil, fmt.Errorf(
					"%s:%d: parser stalled near %q",
					name,
					sourceLine(src, pos),
					src[pos:min(pos+30, len(src))],
				)
			}

			if next < 0 {
				next = len(src)
			}

			code := src[start:next]

			if strings.TrimSpace(code) != "" {
				file.Decls = append(file.Decls, &RawDecl{
					Code: code,
				})
			}

			pos = next
		}
	}

	return file, nil
}

func isLogicalPackageName(name string) bool {
	if name == "" {
		return false
	}

	for _, segment := range strings.Split(name, ".") {
		if !isIdentifier(segment) || isGoKeyword(segment) {
			return false
		}
	}

	return true
}

func isGoKeyword(name string) bool {
	switch name {
	case "break", "default", "func", "interface", "select",
		"case", "defer", "go", "map", "struct", "chan", "else",
		"goto", "package", "switch", "const", "fallthrough", "if",
		"range", "type", "continue", "for", "import", "return", "var":
		return true
	default:
		return false
	}
}

func parseClass(src string, start int) (*ClassDecl, int, error) {
	pos := start + len("class")
	pos = skipSpace(src, pos)

	name, n := readIdent(src[pos:])
	if n == 0 {
		return nil, 0, fmt.Errorf("class requires a name")
	}

	pos += n
	pos = skipSpace(src, pos)

	class := &ClassDecl{
		Name: name,
	}

	if pos < len(src) && src[pos] == ':' {
		pos++

		for {
			pos = skipSpace(src, pos)

			parent, n := readIdent(src[pos:])
			if n == 0 {
				return nil, 0, fmt.Errorf(
					"expected parent class",
				)
			}

			class.Parents = append(class.Parents, parent)

			pos += n
			pos = skipSpace(src, pos)

			if pos >= len(src) || src[pos] != ',' {
				break
			}

			pos++
		}
	}

	pos = skipSpace(src, pos)

	if pos >= len(src) || src[pos] != '{' {
		return nil, 0, fmt.Errorf(
			"class %s requires {",
			name,
		)
	}

	close, err := findMatchingBrace(src, pos)
	if err != nil {
		return nil, 0, err
	}

	body := src[pos+1 : close]

	if err := parseClassBody(class, body); err != nil {
		return nil, 0, err
	}

	return class, close + 1, nil
}

func parseClassBody(class *ClassDecl, body string) error {
	pos := 0

	for pos < len(body) {
		pos = skipSpace(body, pos)

		if pos >= len(body) {
			break
		}

		if keywordAt(body, pos, "func") {
			method, end, err := parseMethod(body, pos)

			if err != nil {
				return err
			}

			class.Methods = append(class.Methods, method)
			pos = end
			continue
		}

		end := lineEnd(body, pos)
		line := strings.TrimSpace(body[pos:end])

		if line != "" {
			parts := strings.Fields(line)

			if len(parts) < 2 {
				return fmt.Errorf(
					"invalid field in class %s: %s",
					class.Name,
					line,
				)
			}

			class.Fields = append(class.Fields, Field{
				Name: parts[0],
				Type: strings.Join(parts[1:], " "),
			})
		}

		pos = end
	}

	return nil
}

func parseExtend(src string, start int) (*ExtendDecl, int, error) {
	pos := skipSpace(src, start+len("extend"))
	bodyOpen := findExtendBodyOpen(src, pos)
	if bodyOpen < 0 {
		return nil, 0, fmt.Errorf("extend declaration requires a target and body")
	}
	target := strings.TrimSpace(src[pos:bodyOpen])
	if target == "" {
		return nil, 0, fmt.Errorf("extend declaration requires a target type")
	}
	close, err := findMatchingBrace(src, bodyOpen)
	if err != nil {
		return nil, 0, err
	}
	container := &ClassDecl{Name: "<extension>"}
	if err := parseClassBody(container, src[bodyOpen+1:close]); err != nil {
		return nil, 0, err
	}
	if len(container.Fields) > 0 {
		return nil, 0, fmt.Errorf("extension declarations may contain methods only")
	}
	return &ExtendDecl{Target: target, Methods: container.Methods}, close + 1, nil
}

func findExtendBodyOpen(src string, start int) int {
	bracketDepth := 0
	for index := start; index < len(src); index++ {
		switch src[index] {
		case '"', '\'', '`':
			end, err := skipQuoted(src, index, src[index])
			if src[index] == '`' {
				end = strings.IndexByte(src[index+1:], '`')
				if end >= 0 {
					end += index + 1
				}
			}
			if err != nil || end < 0 {
				return -1
			}
			index = end
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{':
			if bracketDepth == 0 {
				return index
			}
		case '/':
			if index+1 < len(src) && src[index+1] == '/' {
				index += 2
				for index < len(src) && src[index] != '\n' {
					index++
				}
			} else if index+1 < len(src) && src[index+1] == '*' {
				end := strings.Index(src[index+2:], "*/")
				if end < 0 {
					return -1
				}
				index += end + 3
			}
		}
	}
	return -1
}

func parseMethod(src string, start int) (Method, int, error) {
	pos := start + len("func")
	pos = skipSpace(src, pos)

	name, n := readIdent(src[pos:])

	if n == 0 {
		return Method{}, 0, fmt.Errorf(
			"method requires a name",
		)
	}

	pos += n
	pos = skipSpace(src, pos)
	typeParams := ""
	if pos < len(src) && src[pos] == '[' {
		end, err := findMatchingBracket(src, pos)
		if err != nil {
			return Method{}, 0, err
		}
		typeParams = src[pos : end+1]
		pos = skipSpace(src, end+1)
	}

	if pos >= len(src) || src[pos] != '(' {
		return Method{}, 0, fmt.Errorf(
			"method %s requires (",
			name,
		)
	}

	paramEnd, err := findMatchingParen(src, pos)
	if err != nil {
		return Method{}, 0, err
	}

	params := src[pos+1 : paramEnd]

	pos = paramEnd + 1
	pos = skipSpace(src, pos)

	resultStart := pos

	for pos < len(src) && src[pos] != '{' {
		pos++
	}

	if pos >= len(src) {
		return Method{}, 0, fmt.Errorf(
			"method %s missing body",
			name,
		)
	}

	result := strings.TrimSpace(src[resultStart:pos])

	close, err := findMatchingBrace(src, pos)
	if err != nil {
		return Method{}, 0, err
	}

	return Method{
		Name:       name,
		TypeParams: typeParams,
		Parameters: params,
		Result:     result,
		Body:       src[pos+1 : close],
	}, close + 1, nil
}

func findMatchingBracket(src string, open int) (int, error) {
	if open >= len(src) || src[open] != '[' {
		return -1, fmt.Errorf("expected [ at %d", open)
	}
	depth := 0
	for index := open; index < len(src); index++ {
		switch src[index] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return index, nil
			}
		case '"', '\'':
			end, err := skipQuoted(src, index, src[index])
			if err != nil {
				return -1, err
			}
			index = end
		case '`':
			end := strings.IndexByte(src[index+1:], '`')
			if end < 0 {
				return -1, fmt.Errorf("unterminated raw string")
			}
			index += end + 1
		}
	}
	return -1, fmt.Errorf("missing ]")
}

func findMatchingParen(src string, open int) (int, error) {
	depth := 0

	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++

		case ')':
			depth--

			if depth == 0 {
				return i, nil
			}

		case '"':
			end, err := skipQuoted(src, i, '"')
			if err != nil {
				return -1, err
			}

			i = end

		case '\'':
			end, err := skipQuoted(src, i, '\'')
			if err != nil {
				return -1, err
			}

			i = end

		case '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return -1, fmt.Errorf("unterminated raw string")
			}

			i += end + 1

		case '/':
			if i+1 >= len(src) {
				continue
			}
			if src[i+1] == '/' {
				i += 2
				for i < len(src) && src[i] != '\n' {
					i++
				}
			} else if src[i+1] == '*' {
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return -1, fmt.Errorf("unterminated comment")
				}
				i += end + 3
			}
		}
	}

	return -1, fmt.Errorf("missing )")
}

func findNextExtension(src string, start int) int {
	lineStart := true
	braceDepth := 0

	for i := start; i < len(src); {
		if lineStart && braceDepth == 0 {
			p := skipHorizontal(src, i)

			if keywordAt(src, p, "class") || keywordAt(src, p, "extend") ||
				keywordAt(src, p, "package") {
				return p
			}
		}

		switch src[i] {
		case '"', '\'':
			end, err := skipQuoted(src, i, src[i])
			if err != nil {
				return -1
			}
			i = end + 1
			lineStart = false
			continue

		case '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return -1
			}
			i += end + 2
			lineStart = false
			continue

		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				i += 2
				for i < len(src) && src[i] != '\n' {
					i++
				}
				continue
			}
			if i+1 < len(src) && src[i+1] == '*' {
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return -1
				}
				i += end + 4
				lineStart = false
				continue
			}

		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '\n':
			lineStart = true
			i++
			continue
		}

		if src[i] != ' ' && src[i] != '\t' && src[i] != '\r' {
			lineStart = false
		}
		i++
	}

	return -1
}

func legacyUseAt(src string, pos int) bool {
	if !keywordAt(src, pos, "use") {
		return false
	}

	end := lineEnd(src, pos)
	parts := strings.Fields(strings.TrimSpace(src[pos:end]))
	return len(parts) == 2 && isImportLikePath(parts[1])
}

func isImportLikePath(path string) bool {
	if path == "" {
		return false
	}

	for i, part := range strings.Split(path, ".") {
		if part == "" {
			return false
		}

		if i == 0 {
			if !isIdentifier(part) {
				return false
			}
			continue
		}

		if !isIdentifier(part) {
			return false
		}
	}

	return true
}

func readIdent(src string) (string, int) {
	match := identRE.FindString(src)

	if match == "" {
		return "", 0
	}

	return match, len(match)
}

func keywordAt(src string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(src) {
		return false
	}

	if src[pos:pos+len(keyword)] != keyword {
		return false
	}

	end := pos + len(keyword)

	if end < len(src) {
		r := rune(src[end])

		if unicode.IsLetter(r) ||
			unicode.IsDigit(r) ||
			r == '_' {
			return false
		}
	}

	return true
}

func skipSpace(src string, pos int) int {
	for pos < len(src) {
		switch src[pos] {
		case ' ', '\t', '\r', '\n':
			pos++

		default:
			return pos
		}
	}

	return pos
}

func skipHorizontal(src string, pos int) int {
	for pos < len(src) &&
		(src[pos] == ' ' || src[pos] == '\t') {
		pos++
	}

	return pos
}

func lineEnd(src string, pos int) int {
	for pos < len(src) && src[pos] != '\n' {
		pos++
	}

	if pos < len(src) {
		pos++
	}

	return pos
}

func sourceLine(src string, pos int) int {
	if pos < 0 {
		return 1
	}
	if pos > len(src) {
		pos = len(src)
	}
	return 1 + strings.Count(src[:pos], "\n")
}
