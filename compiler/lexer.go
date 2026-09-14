package compiler

import "fmt"

func findMatchingBrace(src string, open int) (int, error) {
	if open >= len(src) || src[open] != '{' {
		return -1, fmt.Errorf("expected { at %d", open)
	}

	depth := 0

	for i := open; i < len(src); i++ {
		switch src[i] {
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
			end := i + 1

			for end < len(src) && src[end] != '`' {
				end++
			}

			if end >= len(src) {
				return -1, fmt.Errorf("unterminated raw string")
			}

			i = end

		case '/':
			if i+1 >= len(src) {
				continue
			}

			switch src[i+1] {
			case '/':
				i += 2

				for i < len(src) && src[i] != '\n' {
					i++
				}

			case '*':
				i += 2

				for i+1 < len(src) &&
					!(src[i] == '*' && src[i+1] == '/') {
					i++
				}

				if i+1 >= len(src) {
					return -1, fmt.Errorf("unterminated comment")
				}

				i++
			}

		case '{':
			depth++

		case '}':
			depth--

			if depth == 0 {
				return i, nil
			}
		}
	}

	return -1, fmt.Errorf("missing }")
}

func skipQuoted(src string, start int, quote byte) (int, error) {
	for i := start + 1; i < len(src); i++ {
		if src[i] == '\\' {
			i++
			continue
		}

		if src[i] == quote {
			return i, nil
		}
	}

	return -1, fmt.Errorf("unterminated quoted string")
}
