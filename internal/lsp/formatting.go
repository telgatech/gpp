package lsp

import (
	"sort"
	"strconv"
	"strings"

	"github.com/telgatech/gpp/compiler"
)

type rangeFormattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Options      any                    `json:"options"`
}

type rangesFormattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Ranges       []Range                `json:"ranges"`
	Options      any                    `json:"options"`
}

type onTypeFormattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Character    string                 `json:"ch"`
	Options      any                    `json:"options"`
}

// formattingInRanges computes the normal whole-document formatting, then
// returns only independent edits fully contained by one of the requested
// ranges. The formatter remains the single source of formatting behavior.
func (s *server) formattingInRanges(uri string, ranges []Range) []TextEdit {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil || len(ranges) == 0 {
		return []TextEdit{}
	}
	formattedBytes, err := compiler.FormatSourceFile(state.Path, []byte(state.Source))
	if err != nil {
		return []TextEdit{}
	}
	formatted := string(formattedBytes)
	if formatted == state.Source {
		return []TextEdit{}
	}
	allEdits := formattingDiff(state.Source, formatted)
	result := make([]TextEdit, 0, len(allEdits))
	for _, edit := range allEdits {
		for _, requested := range ranges {
			if compareLSPPosition(edit.Range.Start, requested.Start) >= 0 && compareLSPPosition(edit.Range.End, requested.End) <= 0 {
				result = append(result, edit)
				break
			}
		}
	}
	return result
}

func (s *server) formattingOnType(uri string, position Position, character string) []TextEdit {
	if character != "}" && character != "\n" {
		return []TextEdit{}
	}
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil {
		return []TextEdit{}
	}
	lineCount := strings.Count(state.Source, "\n") + 1
	startLine := position.Line
	if startLine >= lineCount {
		startLine = lineCount - 1
	}
	if character == "\n" && startLine > 0 {
		startLine--
	}
	endLine := position.Line
	if endLine < startLine {
		endLine = startLine
	}
	if endLine >= lineCount {
		endLine = lineCount - 1
	}
	end := Position{Line: endLine, Character: utf16Length(sourceLine(state.Source, endLine))}
	return s.formattingInRanges(uri, []Range{{Start: Position{Line: startLine}, End: end}})
}

type formatLine struct {
	text string
	key  string
}

// formattingDiff uses a line LCS to produce non-overlapping edits. A bounded
// matrix prevents range-format requests on very large files from allocating
// unbounded memory; in that case the formatter falls back to one whole-file
// edit, which range filtering will accept only for a range covering the file.
func formattingDiff(source, formatted string) []TextEdit {
	oldLines, oldOffsets := formattingLines(source)
	newLines, _ := formattingLines(formatted)
	setFormattingLineKeys(source, oldLines)
	setFormattingLineKeys(formatted, newLines)
	if len(oldLines) == 0 && len(newLines) == 0 {
		return []TextEdit{}
	}
	if (len(oldLines) + 1) > 8_000_000/(len(newLines)+1) {
		return []TextEdit{{Range: rangeForOffsets(source, 0, len(source)), NewText: formatted}}
	}
	columns := len(newLines) + 1
	dp := make([]uint16, (len(oldLines)+1)*columns)
	for oldIndex := len(oldLines) - 1; oldIndex >= 0; oldIndex-- {
		row := oldIndex * columns
		nextRow := row + columns
		for newIndex := len(newLines) - 1; newIndex >= 0; newIndex-- {
			if oldLines[oldIndex].key == newLines[newIndex].key {
				dp[row+newIndex] = dp[nextRow+newIndex+1] + 1
			} else if dp[nextRow+newIndex] >= dp[row+newIndex+1] {
				dp[row+newIndex] = dp[nextRow+newIndex]
			} else {
				dp[row+newIndex] = dp[row+newIndex+1]
			}
		}
	}
	formattedOffsets := make([]int, len(newLines)+1)
	for index, line := range newLines {
		formattedOffsets[index+1] = formattedOffsets[index] + len(line.text)
	}
	var edits []TextEdit
	oldIndex, newIndex := 0, 0
	for oldIndex < len(oldLines) || newIndex < len(newLines) {
		if oldIndex < len(oldLines) && newIndex < len(newLines) && oldLines[oldIndex].key == newLines[newIndex].key {
			if oldLines[oldIndex].text != newLines[newIndex].text {
				oldStartOffset := oldOffsets[oldIndex]
				oldEndOffset := oldOffsets[oldIndex+1]
				newStartOffset := formattedOffsets[newIndex]
				appendMinimalFormattingEdit(&edits, source, formatted, oldStartOffset, oldEndOffset, newStartOffset, newStartOffset+len(newLines[newIndex].text))
			}
			oldIndex++
			newIndex++
			continue
		}
		oldStart, newStart := oldIndex, newIndex
		for oldIndex < len(oldLines) || newIndex < len(newLines) {
			if oldIndex < len(oldLines) && newIndex < len(newLines) && oldLines[oldIndex].key == newLines[newIndex].key {
				break
			}
			if oldIndex == len(oldLines) {
				newIndex++
				continue
			}
			if newIndex == len(newLines) {
				oldIndex++
				continue
			}
			if dp[(oldIndex+1)*columns+newIndex] >= dp[oldIndex*columns+newIndex+1] {
				oldIndex++
			} else {
				newIndex++
			}
		}
		oldStartOffset := formattingOffsetAt(oldLines, oldOffsets, oldStart, len(source))
		oldEndOffset := formattingOffsetAt(oldLines, oldOffsets, oldIndex, len(source))
		newStartOffset, newEndOffset := formattedOffsets[newStart], formattedOffsets[newIndex]
		appendMinimalFormattingEdit(&edits, source, formatted, oldStartOffset, oldEndOffset, newStartOffset, newEndOffset)
	}
	return edits
}

func appendMinimalFormattingEdit(edits *[]TextEdit, source, formatted string, oldStartOffset, oldEndOffset, newStartOffset, newEndOffset int) {
	oldText := source[oldStartOffset:oldEndOffset]
	newText := formatted[newStartOffset:newEndOffset]
	prefix := commonPrefixBytes(oldText, newText)
	suffix := commonSuffixBytes(oldText[prefix:], newText[prefix:])
	oldStartOffset += prefix
	oldEndOffset -= suffix
	newStartOffset += prefix
	newEndOffset -= suffix
	*edits = append(*edits, TextEdit{
		Range:   rangeForOffsets(source, oldStartOffset, oldEndOffset),
		NewText: formatted[newStartOffset:newEndOffset],
	})
}

func commonPrefixBytes(left, right string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	index := 0
	for index < limit && left[index] == right[index] {
		index++
	}
	return index
}

func commonSuffixBytes(left, right string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	index := 0
	for index < limit && left[len(left)-index-1] == right[len(right)-index-1] {
		index++
	}
	return index
}

func formattingLines(source string) ([]formatLine, []int) {
	if source == "" {
		return nil, []int{0}
	}
	parts := strings.SplitAfter(source, "\n")
	lines := make([]formatLine, len(parts))
	offsets := make([]int, len(parts)+1)
	for index, part := range parts {
		start := offsets[index]
		lines[index] = formatLine{text: part}
		offsets[index+1] = start + len(part)
	}
	return lines, offsets
}

func setFormattingLineKeys(source string, lines []formatLine) {
	file, err := compiler.ParseFile("formatting.gpp", source)
	if err != nil || len(lines) == 0 {
		for index := range lines {
			lines[index].key = lines[index].text
		}
		return
	}
	var keys []strings.Builder
	keys = make([]strings.Builder, len(lines))
	starts := make([]int, len(lines)+1)
	for index, line := range lines {
		starts[index+1] = starts[index] + len(line.text)
	}
	for _, token := range file.Tokens {
		if token.Kind == compiler.TokenEOF || token.Kind == compiler.TokenNewline || token.Span.Start < 0 || token.Span.Start > len(source) {
			continue
		}
		lineIndex := sort.Search(len(lines), func(index int) bool { return starts[index+1] > token.Span.Start })
		if lineIndex >= len(lines) {
			continue
		}
		key := &keys[lineIndex]
		key.WriteString(strconv.Itoa(int(token.Kind)))
		key.WriteByte(':')
		key.WriteString(token.Text)
		key.WriteByte(0)
	}
	for index := range lines {
		lines[index].key = keys[index].String()
	}
}

func formattingOffsetAt(lines []formatLine, offsets []int, index, sourceLength int) int {
	if index < 0 {
		return 0
	}
	if index >= len(lines) {
		return sourceLength
	}
	return offsets[index]
}
