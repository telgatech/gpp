package lsp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

type documentDiagnosticParams struct {
	TextDocument     textDocumentIdentifier `json:"textDocument"`
	PreviousResultID string                 `json:"previousResultId,omitempty"`
}

type workspaceDiagnosticParams struct {
	PreviousResultIDs []previousDiagnosticResult `json:"previousResultIds,omitempty"`
}

type previousDiagnosticResult struct {
	URI   string `json:"uri"`
	Value string `json:"value"`
}

func (s *server) pullDocumentDiagnostics(uri, previousResultID string) map[string]any {
	diagnostics := s.diagnosticsForURI(uri)
	resultID := diagnosticResultID(uri, diagnostics)
	if previousResultID != "" && previousResultID == resultID {
		return map[string]any{"kind": "unchanged", "resultId": resultID}
	}
	return map[string]any{"kind": "full", "resultId": resultID, "items": diagnostics}
}

func (s *server) pullWorkspaceDiagnostics(previous []previousDiagnosticResult) map[string]any {
	previousIDs := make(map[string]string, len(previous))
	for _, result := range previous {
		previousIDs[result.URI] = result.Value
	}
	paths := make([]string, 0, len(s.workspace.files))
	for path, state := range s.workspace.files {
		if state != nil && isWorkspaceSourceFile(path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	items := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		state := s.workspace.files[path]
		diagnostics := s.diagnosticsForURI(state.URI)
		resultID := diagnosticResultID(state.URI, diagnostics)
		report := map[string]any{"uri": state.URI, "resultId": resultID}
		if state.HasVersion {
			report["version"] = state.Version
		}
		if previousIDs[state.URI] == resultID && resultID != "" {
			report["kind"] = "unchanged"
		} else {
			report["kind"] = "full"
			report["items"] = diagnostics
		}
		items = append(items, report)
	}
	return map[string]any{"items": items}
}

func (s *server) diagnosticsForURI(uri string) []Diagnostic {
	if s == nil || s.workspace == nil {
		return []Diagnostic{}
	}
	diagnostics := append([]Diagnostic(nil), s.workspace.diagnostics[uri]...)
	diagnostics = append(diagnostics, s.backendDiagnostics[uri]...)
	diagnostics = append(diagnostics, s.nativeGoDiagnostics()[uri]...)
	if diagnostics == nil {
		diagnostics = []Diagnostic{}
	}
	return deduplicateDiagnostics(diagnostics)
}

func diagnosticResultID(uri string, diagnostics []Diagnostic) string {
	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		return ""
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(uri))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(encoded)
	return hex.EncodeToString(hash.Sum(nil)[:16])
}
