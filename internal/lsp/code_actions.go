package lsp

import "strings"

func (s *server) codeActions(params codeActionParams) []codeAction {
	for _, requestedKind := range params.Context.Only {
		if requestedKind != "quickfix" && !strings.HasPrefix(requestedKind, "quickfix.") {
			return []codeAction{}
		}
	}
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if state == nil {
		return []codeAction{}
	}
	var actions []codeAction
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
		actions = append(actions, codeAction{
			Title:       title,
			Kind:        "quickfix",
			Diagnostics: []Diagnostic{diagnostic},
			IsPreferred: true,
			Edit: workspaceEdit{Changes: map[string][]TextEdit{
				params.TextDocument.URI: {{Range: position, NewText: text}},
			}},
		})
	}
	return actions
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
