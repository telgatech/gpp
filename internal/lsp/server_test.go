package lsp

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunServesInitializeCompletionAndShutdown(t *testing.T) {
	root := t.TempDir()
	source := "package main\n\nfunc main() {\n\tname := \"Ada\"\n\t_ = name.\n}\n"
	path := filepath.Join(root, "main.gpp")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	uri := uriForPath(path)
	rootURI := uriForPath(root)
	var input bytes.Buffer
	writeTestMessage(&input, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"rootUri": rootURI},
	})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
	writeTestMessage(&input, map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "gpp", "version": 1, "text": source}},
	})
	writeTestMessage(&input, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/completion",
		"params": map[string]any{"textDocument": map[string]any{"uri": uri}, "position": map[string]any{"line": 4, "character": 9}},
	})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "shutdown", "params": nil})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "method": "exit"})

	var output bytes.Buffer
	if err := Run(&input, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	messages := readTestMessages(t, output.Bytes())
	if len(messages) < 3 {
		t.Fatalf("expected initialize, diagnostics, completion, and shutdown messages; got %d", len(messages))
	}
	var sawCompletion, sawShutdown bool
	for _, message := range messages {
		if method, _ := message["method"].(string); method == "textDocument/publishDiagnostics" {
			continue
		}
		id, _ := message["id"].(float64)
		switch int(id) {
		case 2:
			sawCompletion = true
			payload, ok := message["result"].([]any)
			if !ok {
				t.Fatalf("completion result has unexpected shape: %#v", message["result"])
			}
			found := false
			for _, value := range payload {
				item, _ := value.(map[string]any)
				if item["label"] == "TrimSpace" {
					found = true
				}
			}
			if !found {
				t.Fatalf("completion did not include TrimSpace: %#v", payload)
			}
		case 3:
			sawShutdown = true
		}
	}
	if !sawCompletion || !sawShutdown {
		t.Fatalf("missing responses: completion=%v shutdown=%v", sawCompletion, sawShutdown)
	}
}

func writeTestMessage(output *bytes.Buffer, value any) {
	payload, _ := json.Marshal(value)
	output.WriteString("Content-Length: ")
	output.WriteString(strconvItoa(len(payload)))
	output.WriteString("\r\n\r\n")
	output.Write(payload)
}

func readTestMessages(t *testing.T, data []byte) []map[string]any {
	var result []map[string]any
	for len(data) > 0 {
		separator := bytes.Index(data, []byte("\r\n\r\n"))
		if separator < 0 {
			t.Fatalf("missing LSP header separator")
		}
		header := string(data[:separator])
		const prefix = "Content-Length: "
		if !strings.HasPrefix(header, prefix) {
			t.Fatalf("unexpected header %q", header)
		}
		length := 0
		if _, err := fmtSscanf(header[len(prefix):], &length); err != nil {
			t.Fatal(err)
		}
		data = data[separator+4:]
		if len(data) < length {
			t.Fatalf("short LSP payload")
		}
		var message map[string]any
		if err := json.Unmarshal(data[:length], &message); err != nil {
			t.Fatal(err)
		}
		result = append(result, message)
		data = data[length:]
	}
	return result
}

func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}

func fmtSscanf(value string, target *int) (int, error) {
	parsed := 0
	for _, r := range strings.TrimSpace(value) {
		if r < '0' || r > '9' {
			return 0, os.ErrInvalid
		}
		parsed = parsed*10 + int(r-'0')
	}
	*target = parsed
	return 1, nil
}
