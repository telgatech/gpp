package compiler

import "strings"

// FormatSource applies the formatter shared by gpp fmt and editor tooling.
// Go++ formatting is intentionally conservative until the language formatter
// grows syntax-aware layout rules.
func FormatSource(source string) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	lines := strings.Split(source, "\n")
	indent := 0
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			lines[index] = ""
			continue
		}
		if strings.HasPrefix(trimmed, "}") {
			indent--
			if indent < 0 {
				indent = 0
			}
		}
		lines[index] = strings.Repeat("\t", indent) + trimmed
		indent += formatBraceDelta(trimmed)
		if indent < 0 {
			indent = 0
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func formatBraceDelta(line string) int {
	open, close := 0, 0
	inString := false
	escaped := false
	for _, char := range line {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && inString {
			escaped = true
			continue
		}
		if char == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch char {
		case '{':
			open++
		case '}':
			close++
		}
	}
	return open - close
}
