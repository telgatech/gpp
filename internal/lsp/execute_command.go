package lsp

import (
	"encoding/json"
	"fmt"
)

// executeCommand exposes server-side operations that the extension can invoke
// through workspace/executeCommand. Returned edits are left for the client to
// apply so the server never writes into an open editor buffer behind its back.
func (s *server) executeCommand(params executeCommandParams) (any, error) {
	uri, err := commandDocumentURI(params.Arguments)
	if err != nil {
		return nil, err
	}
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil {
		return nil, fmt.Errorf("document is not in the workspace: %s", uri)
	}
	switch params.Command {
	case "gpp.organizeImports":
		return workspaceEdit{Changes: map[string][]TextEdit{uri: organizeImportEdits(state)}}, nil
	case "gpp.getDiagnostics":
		return s.diagnosticsForURI(uri), nil
	default:
		return nil, fmt.Errorf("unsupported Go++ command: %s", params.Command)
	}
}

func commandDocumentURI(arguments []json.RawMessage) (string, error) {
	if len(arguments) == 0 {
		return "", fmt.Errorf("command requires a document URI argument")
	}
	var uri string
	if json.Unmarshal(arguments[0], &uri) == nil && uri != "" {
		return uri, nil
	}
	var document struct {
		URI string `json:"uri"`
	}
	if json.Unmarshal(arguments[0], &document) == nil && document.URI != "" {
		return document.URI, nil
	}
	return "", fmt.Errorf("command argument must be a document URI or {uri: ...}")
}
