package lsp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/telgatech/gpp/compiler"
)

func (s *server) codeActions(params codeActionParams) []codeAction {
	wantDefaultKinds := len(params.Context.Only) == 0
	wantQuickfix := wantDefaultKinds || codeActionKindRequested(params.Context.Only, "quickfix")
	wantFixAll := codeActionKindRequested(params.Context.Only, "source.fixAll")
	wantOrganize := codeActionKindRequested(params.Context.Only, "source.organizeImports")
	wantRefactor := wantDefaultKinds || codeActionKindRequested(params.Context.Only, "refactor.extract")
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if state == nil {
		return []codeAction{}
	}
	var fixes []codeAction
	for _, diagnostic := range params.Context.Diagnostics {
		if diagnostic.Code != diagnosticUnclosedLiteral && diagnostic.Code != diagnosticUnclosedComment {
			continue
		}
		data, ok := diagnostic.Data.(map[string]any)
		if !ok || data["fix"] != "insert-delimiter" {
			continue
		}
		text, ok := data["text"].(string)
		if !ok || text == "" || !strings.Contains("\"'`*/", text) {
			continue
		}
		offset, ok := actionOffset(data["offset"])
		if !ok || offset < 0 || offset > len(state.Source) {
			continue
		}
		if diagnostic.Code == diagnosticUnclosedLiteral && len(text) != 1 || diagnostic.Code == diagnosticUnclosedComment && text != "*/" {
			continue
		}
		position := rangeForOffsets(state.Source, offset, offset)
		title := "Close unterminated string literal"
		if diagnostic.Code == diagnosticUnclosedComment {
			title = "Close unterminated block comment"
		}
		fixes = append(fixes, codeAction{
			Title:       title,
			Kind:        "quickfix",
			Diagnostics: []Diagnostic{diagnostic},
			IsPreferred: true,
			Edit: workspaceEdit{Changes: map[string][]TextEdit{
				params.TextDocument.URI: {{Range: position, NewText: text}},
			}},
		})
	}
	if !wantQuickfix && !wantFixAll && !wantOrganize && !wantRefactor {
		return []codeAction{}
	}
	var actions []codeAction
	if wantRefactor {
		if action, ok := extractVariableAction(state, params.Range); ok {
			actions = append(actions, action)
		}
	}
	if wantOrganize {
		if edits := organizeImportEdits(state); len(edits) > 0 {
			actions = append(actions, codeAction{Title: "Organize Imports", Kind: "source.organizeImports", Edit: workspaceEdit{Changes: map[string][]TextEdit{params.TextDocument.URI: edits}}})
		}
	}
	if wantFixAll {
		edits := map[string][]TextEdit{}
		seen := map[string]bool{}
		for _, fix := range fixes {
			for uri, changes := range fix.Edit.Changes {
				for _, change := range changes {
					key := fmt.Sprintf("%s:%d:%d:%d:%d:%s", uri, change.Range.Start.Line, change.Range.Start.Character, change.Range.End.Line, change.Range.End.Character, change.NewText)
					if seen[key] {
						continue
					}
					seen[key] = true
					edits[uri] = append(edits[uri], change)
				}
			}
		}
		if len(edits) > 0 {
			actions = append(actions, codeAction{Title: "Fix all safely fixable issues", Kind: "source.fixAll", Edit: workspaceEdit{Changes: edits}})
		}
	}
	if wantQuickfix {
		actions = append(actions, fixes...)
	}
	for index := range actions {
		actions[index].Data = codeActionResolveData{Params: params, Title: actions[index].Title, Kind: actions[index].Kind}
	}
	return actions
}

func (s *server) resolveCodeAction(action codeAction) codeAction {
	if action.Data == nil {
		return action
	}
	encoded, err := json.Marshal(action.Data)
	if err != nil {
		return action
	}
	var data codeActionResolveData
	if json.Unmarshal(encoded, &data) != nil || data.Params.TextDocument.URI == "" || data.Title == "" {
		return action
	}
	for _, candidate := range s.codeActions(data.Params) {
		if candidate.Title == data.Title && candidate.Kind == data.Kind {
			return candidate
		}
	}
	return action
}

func codeActionKindRequested(filters []string, actionKind string) bool {
	for _, filter := range filters {
		if filter == actionKind || strings.HasPrefix(actionKind, filter+".") {
			return true
		}
	}
	return false
}

func extractVariableAction(state *fileState, selection Range) (codeAction, bool) {
	if state == nil || state.File == nil || selection.Start == selection.End {
		return codeAction{}, false
	}
	start, end := offsetAt(state.Source, selection.Start), offsetAt(state.Source, selection.End)
	if start < 0 || end <= start || end > len(state.Source) {
		return codeAction{}, false
	}
	tokens := state.File.Tokens
	startIndex := -1
	endIndex := -1
	for index, token := range tokens {
		if token.Span.Start == start {
			startIndex = index
		}
		if token.Span.End == end {
			endIndex = index
		}
	}
	if startIndex < 2 || endIndex < startIndex || endIndex+1 >= len(tokens) {
		return codeAction{}, false
	}
	operator := tokens[startIndex-1]
	if operator.Text != ":=" && operator.Text != "=" {
		return codeAction{}, false
	}
	left := tokens[startIndex-2]
	if left.Kind != compiler.TokenIdentifier {
		return codeAction{}, false
	}
	if startIndex > 2 {
		beforeLeft := tokens[startIndex-3]
		if beforeLeft.Kind != compiler.TokenNewline && beforeLeft.Text != "{" && beforeLeft.Text != ";" && !(operator.Text == "=" && beforeLeft.Text == "var") {
			return codeAction{}, false
		}
	}
	trailing := endIndex + 1
	if tokens[trailing].Kind == compiler.TokenComment {
		trailing++
	}
	if trailing >= len(tokens) || tokens[trailing].Kind != compiler.TokenNewline && tokens[trailing].Text != ";" && tokens[trailing].Text != "}" {
		return codeAction{}, false
	}
	expressionTokens := tokens[startIndex : endIndex+1]
	expression, err := compiler.ParseExpressionTokens(expressionTokens)
	if err != nil || !extractableExpression(expression) {
		return codeAction{}, false
	}
	lineStart := strings.LastIndex(state.Source[:left.Span.Start], "\n") + 1
	indent := state.Source[lineStart:left.Span.Start]
	if strings.Trim(indent, " \t") != "" {
		return codeAction{}, false
	}
	name := "value"
	used := map[string]bool{}
	method, _ := enclosingMethod(state, start)
	if method != nil {
		for _, token := range tokens {
			if token.Kind == compiler.TokenIdentifier && token.Span.Start >= method.SpanValue.Start && token.Span.End <= method.SpanValue.End {
				used[token.Text] = true
			}
		}
	}
	for suffix := 2; used[name]; suffix++ {
		name = fmt.Sprintf("value%d", suffix)
	}
	lineEnding := "\n"
	if strings.Contains(state.Source, "\r\n") {
		lineEnding = "\r\n"
	}
	newDeclaration := indent + name + " := " + state.Source[start:end] + lineEnding
	newLine := rangeForOffsets(state.Source, lineStart, lineStart)
	selected := rangeForOffsets(state.Source, start, end)
	return codeAction{
		Title: "Extract variable " + name,
		Kind:  "refactor.extract",
		Edit: workspaceEdit{Changes: map[string][]TextEdit{
			state.URI: {
				{Range: selected, NewText: name},
				{Range: newLine, NewText: newDeclaration},
			},
		}},
	}, true
}

func extractableExpression(expression compiler.ExprNode) bool {
	switch expression.(type) {
	case *compiler.BinaryExpr, *compiler.CallExpr, *compiler.CompositeLiteralExpr, *compiler.IndexExpr, *compiler.InterpolatedStringExpr, *compiler.SelectorExpr, *compiler.SliceExpr, *compiler.UnaryExpr:
		return true
	default:
		return false
	}
}

// organizeImportEdits sorts uncomplicated parenthesized import blocks. It
// deliberately leaves blocks containing comments, blank grouping lines, or
// multiple specs on one line untouched because their grouping/attachment may
// carry meaning for the author.
func organizeImportEdits(state *fileState) []TextEdit {
	if state == nil || state.File == nil {
		return nil
	}
	source := state.Source
	var edits []TextEdit
	tokens := state.File.Tokens
	for index, token := range tokens {
		if token.Text != "import" || index+1 >= len(tokens) {
			continue
		}
		openIndex := index + 1
		for openIndex < len(tokens) && (tokens[openIndex].Kind == compiler.TokenNewline || tokens[openIndex].Kind == compiler.TokenComment) {
			openIndex++
		}
		if openIndex >= len(tokens) || tokens[openIndex].Text != "(" {
			continue
		}
		closeIndex := openIndex + 1
		for closeIndex < len(tokens) && tokens[closeIndex].Text != ")" {
			closeIndex++
		}
		if closeIndex >= len(tokens) {
			continue
		}
		groupStart, groupEnd := token.Span.Start, tokens[closeIndex].Span.End
		if hasCommentInSpan(state.File.Comments, groupStart, groupEnd) {
			continue
		}
		var imports []compiler.ImportDecl
		for _, imported := range state.File.Imports {
			if imported.SpanValue.Start > tokens[openIndex].Span.End && imported.SpanValue.End < tokens[closeIndex].Span.Start {
				imports = append(imports, imported)
			}
		}
		if len(imports) < 2 {
			continue
		}
		startLine := token.Span.Line - 1
		endLine := tokens[closeIndex].Span.Line - 1
		lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
		if startLine < 0 || endLine >= len(lines) || endLine-startLine-1 != len(imports) {
			continue
		}
		valid := true
		indent := ""
		for lineIndex := startLine + 1; lineIndex < endLine; lineIndex++ {
			line := lines[lineIndex]
			if strings.TrimSpace(line) == "" {
				valid = false
				break
			}
			if indent == "" {
				indent = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			}
		}
		if !valid {
			continue
		}
		sort.SliceStable(imports, func(i, j int) bool {
			if imports[i].Path != imports[j].Path {
				return imports[i].Path < imports[j].Path
			}
			return imports[i].Alias < imports[j].Alias
		})
		var specs []string
		for _, imported := range imports {
			spec := indent
			if imported.Alias != "" {
				spec += imported.Alias + " "
			}
			if imported.LogicalPackage {
				spec += imported.Path
			} else {
				spec += strconv.Quote(imported.Path)
			}
			specs = append(specs, spec)
		}
		separator := "\n"
		if strings.Contains(source[groupStart:groupEnd], "\r\n") {
			separator = "\r\n"
		}
		prefix := source[groupStart:tokens[openIndex].Span.End]
		suffix := source[tokens[closeIndex].Span.Start:groupEnd]
		replacement := prefix + separator + strings.Join(specs, separator) + separator + suffix
		if replacement != source[groupStart:groupEnd] {
			edits = append(edits, TextEdit{Range: rangeForOffsets(source, groupStart, groupEnd), NewText: replacement})
		}
		index = closeIndex
	}
	return edits
}

func hasCommentInSpan(comments []compiler.Token, start, end int) bool {
	for _, comment := range comments {
		if comment.Span.Start >= start && comment.Span.End <= end {
			return true
		}
	}
	return false
}

func actionOffset(value any) (int, bool) {
	switch offset := value.(type) {
	case int:
		return offset, true
	case float64:
		return int(offset), offset >= 0 && offset == float64(int(offset))
	default:
		return 0, false
	}
}
