// Package lsp implements the editor-facing JSON-RPC service for Go++.
//
// The server deliberately stays dependency-free. It speaks the small JSON-RPC
// envelope used by LSP directly and delegates parsing, semantic validation,
// documentation, and formatting to the compiler package.
package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/telgatech/gpp/compiler"
)

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	Code     string `json:"code,omitempty"`
	Data     any    `json:"data,omitempty"`
	Message  string `json:"message"`
}

const (
	diagnosticSyntax          = "GPP1000"
	diagnosticUnclosedLiteral = "GPP1001"
	diagnosticUnclosedComment = "GPP1002"
	diagnosticSemantic        = "GPP2000"
)

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Deprecated    bool     `json:"deprecated,omitempty"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Deprecated     bool             `json:"deprecated,omitempty"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind,omitempty"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	Deprecated    bool   `json:"deprecated,omitempty"`
}

type SignatureInformation struct {
	Label         string `json:"label"`
	Documentation string `json:"documentation,omitempty"`
}

type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature,omitempty"`
	ActiveParameter int                    `json:"activeParameter,omitempty"`
}

type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type hover struct {
	Contents markupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type workspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

type codeActionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      struct {
		Diagnostics []Diagnostic `json:"diagnostics"`
		Only        []string     `json:"only,omitempty"`
	} `json:"context"`
}

type codeAction struct {
	Title       string        `json:"title"`
	Kind        string        `json:"kind"`
	Diagnostics []Diagnostic  `json:"diagnostics,omitempty"`
	Edit        workspaceEdit `json:"edit"`
	IsPreferred bool          `json:"isPreferred,omitempty"`
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  *any            `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId,omitempty"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type contentChange struct {
	Range       *Range `json:"range,omitempty"`
	Text        string `json:"text"`
	RangeLength int    `json:"rangeLength,omitempty"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	ContentChanges []contentChange `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type didSaveParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

type watchedFileChange struct {
	URI  string `json:"uri"`
	Type int    `json:"type"`
}

type watchedFilesParams struct {
	Changes []watchedFileChange `json:"changes"`
}

type workspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type initializeParams struct {
	RootURI          *string           `json:"rootUri"`
	RootPath         string            `json:"rootPath,omitempty"`
	WorkspaceFolders []workspaceFolder `json:"workspaceFolders,omitempty"`
}

type fileState struct {
	Path   string
	URI    string
	Source string
	Open   bool
	File   *compiler.File
}

type symbolInfo struct {
	Symbol compiler.DocSymbol
	Path   string
}

type workspaceState struct {
	roots       []string
	files       map[string]*fileState
	diagnostics map[string][]Diagnostic
	docIndex    *compiler.DocIndex
	symbols     []symbolInfo
	official    []*compiler.File
	logger      *log.Logger
}

func newWorkspace(logger *log.Logger) *workspaceState {
	workspace := &workspaceState{
		files:       map[string]*fileState{},
		diagnostics: map[string][]Diagnostic{},
		logger:      logger,
	}
	// Official packages are immutable compiler inputs. Parse them once per
	// LSP session instead of rebuilding their ASTs on every editor keystroke.
	for _, packagePath := range compiler.OfficialStdlibPackages() {
		files, err := compiler.LoadOfficialPackage(packagePath)
		if err == nil {
			workspace.official = append(workspace.official, files...)
		}
	}
	return workspace
}

func (w *workspaceState) setRoots(roots []string) {
	seen := map[string]bool{}
	w.roots = nil
	for _, root := range roots {
		if root == "" {
			continue
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if seen[absolute] {
			continue
		}
		seen[absolute] = true
		w.roots = append(w.roots, absolute)
	}
}

func (w *workspaceState) refresh() {
	known := map[string]bool{}
	for _, root := range w.roots {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root && (entry.Name() == ".gpp" || entry.Name() == ".git" || entry.Name() == "vendor") {
					return filepath.SkipDir
				}
				return nil
			}
			if !isGoPlusFile(path) {
				return nil
			}
			absolute, err := filepath.Abs(path)
			if err != nil {
				return nil
			}
			known[absolute] = true
			state := w.files[absolute]
			if state == nil {
				state = &fileState{Path: absolute, URI: uriForPath(absolute)}
				w.files[absolute] = state
			}
			if !state.Open {
				if data, readErr := os.ReadFile(absolute); readErr == nil {
					state.Source = string(data)
				}
			}
			return nil
		})
	}
	for path, state := range w.files {
		if !known[path] && !state.Open {
			delete(w.files, path)
		}
	}
	w.analyze()
}

func (w *workspaceState) analyze() {
	parsed := make([]*compiler.File, 0, len(w.files))
	userFiles := make([]*fileState, 0, len(w.files))
	byDirectory := map[string][]*compiler.File{}
	for _, state := range w.files {
		state.File = nil
		if !isGoPlusFile(state.Path) {
			continue
		}
		file, err := compiler.ParseFile(filepath.Base(state.Path), state.Source)
		if err != nil {
			if _, lexErr := compiler.LexSource(filepath.Base(state.Path), state.Source); lexErr != nil &&
				(strings.Contains(lexErr.Error(), "unterminated literal") || strings.Contains(lexErr.Error(), "unterminated block comment")) {
				err = lexErr
			}
			diagnostic := diagnosticFromError(state.URI, state.Source, err)
			if diagnostic.Code == diagnosticSemantic {
				diagnostic.Code = diagnosticSyntax
			}
			w.diagnostics[state.URI] = []Diagnostic{diagnostic}
			continue
		}
		file.SourcePath = state.Path
		state.File = file
		parsed = append(parsed, file)
		userFiles = append(userFiles, state)
		byDirectory[filepath.Dir(state.Path)] = append(byDirectory[filepath.Dir(state.Path)], file)
		w.diagnostics[state.URI] = nil
	}

	// Include the immutable compiler-owned ASTs so imported annotations/classes
	// and standard extensions are represented consistently in editor features.
	officialFiles := w.official
	for _, files := range byDirectory {
		allFiles := append([]*compiler.File{}, files...)
		allFiles = append(allFiles, officialFiles...)
		_, err := compiler.ResolveProgram(&compiler.Program{Files: allFiles})
		if err != nil {
			path, source, ok := w.errorFile(err)
			if !ok && len(userFiles) > 0 {
				path, source = userFiles[0].Path, userFiles[0].Source
				ok = true
			}
			if ok {
				uri := uriForPath(path)
				w.diagnostics[uri] = append(w.diagnostics[uri], diagnosticFromError(uri, source, err))
			}
		}
	}

	// The prelude is a compiler-owned source file rather than a package import.
	// Add it to the documentation index for completion and hover, but not to
	// semantic resolution where it would become a second user package.
	indexFiles := append([]*compiler.File{}, parsed...)
	indexFiles = append(indexFiles, officialFiles...)
	if prelude, err := compiler.LoadPrelude(); err == nil {
		indexFiles = append(indexFiles, prelude)
	}
	w.docIndex = compiler.BuildDocIndex(indexFiles)
	w.symbols = w.collectSymbols()
}

func (w *workspaceState) errorFile(err error) (string, string, bool) {
	message := err.Error()
	for path, state := range w.files {
		base := filepath.Base(path)
		if !strings.Contains(message, base) {
			continue
		}
		return path, state.Source, true
	}
	return "", "", false
}

func (w *workspaceState) collectSymbols() []symbolInfo {
	if w.docIndex == nil {
		return nil
	}
	var result []symbolInfo
	seen := map[string]bool{}
	for _, pkg := range w.docIndex.Packages {
		for _, symbol := range pkg.Symbols {
			w.appendSymbol(&result, seen, symbol)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Symbol.FullName < result[j].Symbol.FullName
	})
	return result
}

func (w *workspaceState) appendSymbol(result *[]symbolInfo, seen map[string]bool, symbol compiler.DocSymbol) {
	path := w.symbolPath(symbol)
	key := path + "\x00" + symbol.FullName + "\x00" + string(symbol.Kind) + "\x00" + symbol.Signature
	if !seen[key] {
		seen[key] = true
		*result = append(*result, symbolInfo{Symbol: symbol, Path: path})
	}
	for _, field := range symbol.Fields {
		child := compiler.DocSymbol{Kind: compiler.DocField, Name: field.Name, FullName: symbol.FullName + "." + field.Name, Package: symbol.Package, Parent: symbol.Name, Doc: field.Doc, Signature: field.Name + " " + field.Type, Exported: exportedName(field.Name), SourceFile: symbol.SourceFile, SourceLine: symbol.SourceLine}
		w.appendSymbol(result, seen, child)
	}
	for _, child := range append(append([]compiler.DocSymbol{}, symbol.Methods...), symbol.Static...) {
		w.appendSymbol(result, seen, child)
	}
	for _, value := range symbol.Values {
		child := compiler.DocSymbol{Kind: compiler.DocValue, Name: value.Name, FullName: symbol.FullName + "." + value.Name, Package: symbol.Package, Parent: symbol.Name, Doc: value.Doc, Signature: symbol.Name + "." + value.Name + " = " + value.Value, Exported: exportedName(value.Name), SourceFile: symbol.SourceFile, SourceLine: symbol.SourceLine}
		w.appendSymbol(result, seen, child)
	}
}

func (w *workspaceState) symbolPath(symbol compiler.DocSymbol) string {
	for path, state := range w.files {
		if state.File != nil && state.File.Package == symbol.Package && filepath.Base(path) == symbol.SourceFile {
			return path
		}
	}
	return ""
}

func exportedName(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func isGoPlusFile(path string) bool {
	return strings.HasSuffix(path, ".gpp") || strings.HasSuffix(path, ".gpp.tpl")
}

var locationPattern = regexp.MustCompile(`(^|:)([^:\n]+):(\d+)(?::(\d+))?:\s*(.*)$`)

func diagnosticFromError(uri, source string, err error) Diagnostic {
	message := err.Error()
	line := 0
	byteCharacter := 0
	if match := locationPattern.FindStringSubmatch(message); match != nil {
		line, _ = strconv.Atoi(match[3])
		line--
		if match[4] != "" {
			byteCharacter, _ = strconv.Atoi(match[4])
			byteCharacter--
		}
		message = match[5]
	}
	if line < 0 {
		line = 0
	}
	lineText := sourceLine(source, line)
	character := byteCharacter
	if byteCharacter >= 0 && byteCharacter <= len(lineText) {
		character = utf16Length(lineText[:byteCharacter])
	}
	endCharacter := utf16Length(lineText)
	if character > endCharacter {
		character = endCharacter
	}
	diagnostic := Diagnostic{Range: Range{Start: Position{Line: line, Character: character}, End: Position{Line: line, Character: endCharacter}}, Severity: 1, Source: "gpp", Code: diagnosticSemantic, Message: message}
	if strings.Contains(message, "unterminated literal") {
		diagnostic.Code = diagnosticUnclosedLiteral
		text := literalDelimiterAt(source, line, byteCharacter)
		diagnostic.Data = map[string]any{"fix": "insert-delimiter", "text": text, "offset": unterminatedLiteralEnd(source, line, byteCharacter, text)}
	} else if strings.Contains(message, "unterminated block comment") {
		diagnostic.Code = diagnosticUnclosedComment
		diagnostic.Data = map[string]any{"fix": "insert-delimiter", "text": "*/", "offset": len(source)}
	} else if strings.Contains(message, "expected") || strings.Contains(message, "unexpected") || strings.Contains(message, "syntax") {
		diagnostic.Code = diagnosticSyntax
	}
	return diagnostic
}

func literalDelimiterAt(source string, line, character int) string {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	if line < 0 || line >= len(lines) {
		return "\""
	}
	text := lines[line]
	if character >= 0 && character < len(text) {
		switch text[character] {
		case '\'', '"', '`':
			return string(text[character])
		}
	}
	return "\""
}

func unterminatedLiteralEnd(source string, line, character int, delimiter string) int {
	if line < 0 {
		return len(source)
	}
	lineStart := 0
	for currentLine := 0; currentLine < line; currentLine++ {
		newline := strings.IndexByte(source[lineStart:], '\n')
		if newline < 0 {
			return len(source)
		}
		lineStart += newline + 1
	}
	start := lineStart + character
	if start < lineStart || start >= len(source) {
		return len(source)
	}
	for cursor := start + 1; cursor < len(source); cursor++ {
		if delimiter != "`" && source[cursor] == '\\' {
			cursor++
			continue
		}
		if delimiter != "`" && (source[cursor] == '\r' || source[cursor] == '\n') {
			return cursor
		}
	}
	return len(source)
}

func sourceLine(source string, line int) string {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	return lines[line]
}

type server struct {
	reader    *bufio.Reader
	writer    *bufio.Writer
	writeLock sync.Mutex
	workspace *workspaceState
	logger    *log.Logger
	shutdown  bool
}

// Run serves LSP messages until the client sends exit or the input stream
// closes. Protocol output is written only to out; diagnostics and optional
// debugging logs use logOut.
func Run(in io.Reader, out io.Writer, logOut io.Writer) error {
	logger := log.New(logOut, "gpp lsp: ", log.LstdFlags)
	s := &server{
		reader: bufio.NewReader(in),
		writer: bufio.NewWriter(out),
		logger: logger,
	}
	s.workspace = newWorkspace(logger)
	for {
		payload, err := readMessage(s.reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var request rpcRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			logger.Printf("invalid JSON-RPC message: %v", err)
			continue
		}
		keepRunning, err := s.handleSafely(request)
		if err != nil {
			logger.Printf("request %s failed: %v", request.Method, err)
			if len(request.ID) > 0 {
				_ = s.respondError(request.ID, -32603, err.Error())
			}
		}
		if !keepRunning {
			return nil
		}
	}
}

func (s *server) handleSafely(request rpcRequest) (keepRunning bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			keepRunning = true
			err = fmt.Errorf("internal LSP error: %v", recovered)
			if s.logger != nil {
				s.logger.Printf("panic handling %s: %v\n%s", request.Method, recovered, debug.Stack())
			}
		}
	}()
	return s.handle(request)
}

func (s *server) handle(request rpcRequest) (bool, error) {
	if request.Method == "" {
		return true, nil
	}
	switch request.Method {
	case "initialize":
		var params initializeParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		var roots []string
		for _, folder := range params.WorkspaceFolders {
			if path := pathFromURI(folder.URI); path != "" {
				roots = append(roots, path)
			}
		}
		if len(roots) == 0 && params.RootURI != nil {
			roots = append(roots, pathFromURI(*params.RootURI))
		}
		if len(roots) == 0 && params.RootPath != "" {
			roots = append(roots, params.RootPath)
		}
		s.workspace.setRoots(roots)
		s.workspace.refresh()
		result := map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":           map[string]any{"openClose": true, "change": 1, "save": map[string]any{"includeText": false}},
				"completionProvider":         map[string]any{"triggerCharacters": []string{".", "@"}},
				"codeActionProvider":         map[string]any{"codeActionKinds": []string{"quickfix"}},
				"semanticTokensProvider":     map[string]any{"legend": map[string]any{"tokenTypes": semanticTokenTypes, "tokenModifiers": semanticTokenModifiers}, "full": true},
				"hoverProvider":              true,
				"definitionProvider":         true,
				"referencesProvider":         true,
				"renameProvider":             true,
				"documentSymbolProvider":     true,
				"workspaceSymbolProvider":    true,
				"documentFormattingProvider": true,
				"signatureHelpProvider":      map[string]any{"triggerCharacters": []string{"(", ","}},
			},
			"serverInfo": map[string]string{"name": "gpp", "version": "0.1.0"},
		}
		if err := s.respond(request.ID, result); err != nil {
			return true, err
		}
		s.publishDiagnostics()
		return true, nil
	case "initialized":
		return true, nil
	case "shutdown":
		s.shutdown = true
		return true, s.respond(request.ID, nil)
	case "exit":
		return false, nil
	case "$/cancelRequest":
		return true, nil
	case "textDocument/didOpen":
		var params didOpenParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		s.open(params.TextDocument.URI, params.TextDocument.Text)
		return true, nil
	case "textDocument/didChange":
		var params didChangeParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		if len(params.ContentChanges) > 0 {
			state := s.ensureFile(params.TextDocument.URI)
			text := state.Source
			for _, change := range params.ContentChanges {
				if change.Range == nil {
					text = change.Text
				} else {
					text = applyChange(text, *change.Range, change.Text)
				}
			}
			state.Source, state.Open = text, true
			s.workspace.analyze()
			s.publishDiagnostics()
		}
		return true, nil
	case "textDocument/didClose":
		var params didCloseParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		s.close(params.TextDocument.URI)
		return true, nil
	case "textDocument/didSave":
		var params didSaveParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		if params.Text != nil {
			state := s.ensureFile(params.TextDocument.URI)
			state.Source, state.Open = *params.Text, true
		}
		s.workspace.analyze()
		s.publishDiagnostics()
		return true, nil
	case "workspace/didChangeWatchedFiles":
		var params watchedFilesParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		for _, change := range params.Changes {
			path := pathFromURI(change.URI)
			if path == "" {
				continue
			}
			state := s.workspace.files[path]
			if change.Type == 3 {
				if state != nil && !state.Open {
					delete(s.workspace.files, path)
				}
				continue
			}
			if state == nil {
				state = &fileState{Path: path, URI: change.URI}
				s.workspace.files[path] = state
			}
			if !state.Open {
				if data, err := os.ReadFile(path); err == nil {
					state.Source = string(data)
				}
			}
		}
		s.workspace.refresh()
		s.publishDiagnostics()
		return true, nil
	case "textDocument/completion":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.completions(params))
	case "textDocument/semanticTokens/full":
		var params semanticTokensParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.semanticTokens(params.TextDocument.URI))
	case "textDocument/codeAction":
		var params codeActionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.codeActions(params))
	case "textDocument/hover":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.hover(params))
	case "textDocument/definition":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.definitions(params))
	case "textDocument/references":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Position     Position               `json:"position"`
			Context      struct {
				IncludeDeclaration bool `json:"includeDeclaration"`
			} `json:"context"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.references(params.TextDocument.URI, params.Position))
	case "textDocument/rename":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Position     Position               `json:"position"`
			NewName      string                 `json:"newName"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		result, err := s.rename(params.TextDocument.URI, params.Position, params.NewName)
		if err != nil {
			return true, s.respondError(request.ID, -32602, err.Error())
		}
		return true, s.respond(request.ID, result)
	case "textDocument/documentSymbol":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.documentSymbols(params.TextDocument.URI))
	case "workspace/symbol":
		var params struct {
			Query string `json:"query"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.workspaceSymbols(params.Query))
	case "textDocument/formatting":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Options      any                    `json:"options"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.formatting(params.TextDocument.URI))
	case "textDocument/signatureHelp":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.signatureHelp(params))
	default:
		if len(request.ID) > 0 {
			return true, s.respond(request.ID, nil)
		}
		return true, nil
	}
}

func decodeParams(data json.RawMessage, value any) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	return json.Unmarshal(data, value)
}

func (s *server) respond(id json.RawMessage, result any) error {
	if len(id) == 0 {
		return nil
	}
	resultValue := result
	return s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: &resultValue})
}

func (s *server) respondError(id json.RawMessage, code int, message string) error {
	if len(id) == 0 {
		return nil
	}
	return s.write(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
}

func (s *server) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	if _, err := fmt.Fprintf(s.writer, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	if _, err := s.writer.Write(data); err != nil {
		return err
	}
	return s.writer.Flush()
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, err
			}
		}
	}
	if contentLength < 0 {
		return nil, errors.New("LSP message has no Content-Length")
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *server) ensureFile(uri string) *fileState {
	path := pathFromURI(uri)
	state := s.workspace.files[path]
	if state == nil {
		state = &fileState{Path: path, URI: uri}
		if data, err := os.ReadFile(path); err == nil {
			state.Source = string(data)
		}
		s.workspace.files[path] = state
	}
	return state
}

func (s *server) open(uri, source string) {
	state := s.ensureFile(uri)
	state.Source, state.Open = source, true
	s.workspace.analyze()
	s.publishDiagnostics()
}

func (s *server) close(uri string) {
	state := s.ensureFile(uri)
	state.Open = false
	if data, err := os.ReadFile(state.Path); err == nil {
		state.Source = string(data)
	}
	s.workspace.analyze()
	s.publishDiagnostics()
}

func (s *server) publishDiagnostics() {
	for _, state := range s.workspace.files {
		if !isGoPlusFile(state.Path) {
			continue
		}
		if err := s.write(map[string]any{
			"jsonrpc": "2.0",
			"method":  "textDocument/publishDiagnostics",
			"params":  map[string]any{"uri": state.URI, "diagnostics": s.workspace.diagnostics[state.URI]},
		}); err != nil {
			s.logger.Printf("publish diagnostics: %v", err)
		}
	}
}

func (s *server) symbolsAt(uri string, position Position) (string, Range) {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil {
		return "", Range{}
	}
	offset := offsetAt(state.Source, position)
	if state.File != nil {
		if token, ok := identifierTokenAt(state.File.Tokens, offset); ok {
			return token.Text, rangeForOffsets(state.Source, token.Span.Start, token.Span.End)
		}
		// A successfully parsed buffer has authoritative token boundaries. Do
		// not fall back to substring matching inside comments, strings, or raw
		// template content.
		return "", Range{}
	}
	start := offset
	for start > 0 && isIdentifierByte(state.Source[start-1]) {
		start--
	}
	end := offset
	for end < len(state.Source) && isIdentifierByte(state.Source[end]) {
		end++
	}
	if start == end {
		return "", Range{}
	}
	return state.Source[start:end], rangeForOffsets(state.Source, start, end)
}

func (s *server) findSymbols(name string, uri string) []symbolInfo {
	var result []symbolInfo
	currentPackage := ""
	if state := s.workspace.files[pathFromURI(uri)]; state != nil && state.File != nil {
		currentPackage = state.File.Package
	}
	for _, info := range s.workspace.symbols {
		if info.Symbol.Name != name || info.Path == "" {
			continue
		}
		if currentPackage != "" && info.Symbol.Package == currentPackage {
			result = append(result, info)
		}
	}
	if len(result) == 0 {
		for _, info := range s.workspace.symbols {
			if info.Symbol.Name == name && info.Path != "" {
				result = append(result, info)
			}
		}
	}
	return result
}

func (s *server) hover(params textDocumentPositionParams) *hover {
	name, selection := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" {
		return nil
	}
	choices := s.findSymbols(name, params.TextDocument.URI)
	if len(choices) == 0 {
		return nil
	}
	info := choices[0]
	value := "```gpp\n" + info.Symbol.Signature + "\n```"
	if info.Symbol.Doc != "" {
		value += "\n\n" + info.Symbol.Doc
	}
	if deprecated(info.Symbol) {
		value += "\n\n**Deprecated.**"
	}
	return &hover{Contents: markupContent{Kind: "markdown", Value: value}, Range: &selection}
}

func (s *server) definitions(params textDocumentPositionParams) []Location {
	name, _ := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" {
		return nil
	}
	result := []Location{}
	for _, info := range s.findSymbols(name, params.TextDocument.URI) {
		if location, ok := s.symbolLocation(info); ok {
			result = append(result, location)
		}
	}
	return result
}

func (s *server) references(uri string, position Position) []Location {
	name, _ := s.symbolsAt(uri, position)
	if name == "" {
		return nil
	}
	result := []Location{}
	for _, state := range s.workspace.files {
		if !isGoPlusFile(state.Path) {
			continue
		}
		for _, span := range identifierSpans(state.File, state.Source, name) {
			result = append(result, Location{URI: state.URI, Range: span})
		}
	}
	return result
}

func (s *server) rename(uri string, position Position, newName string) (workspaceEdit, error) {
	if !validIdentifier(newName) {
		return workspaceEdit{}, fmt.Errorf("invalid identifier %q", newName)
	}
	name, _ := s.symbolsAt(uri, position)
	if name == "" {
		return workspaceEdit{}, errors.New("no symbol at position")
	}
	changes := map[string][]TextEdit{}
	for _, state := range s.workspace.files {
		if !isGoPlusFile(state.Path) {
			continue
		}
		for _, span := range identifierSpans(state.File, state.Source, name) {
			changes[state.URI] = append(changes[state.URI], TextEdit{Range: span, NewText: newName})
		}
	}
	return workspaceEdit{Changes: changes}, nil
}

func (s *server) documentSymbols(uri string) []DocumentSymbol {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil || state.File == nil || s.workspace.docIndex == nil {
		return []DocumentSymbol{}
	}
	pkg := s.workspace.docIndex.Packages[state.File.Package]
	if pkg == nil {
		return []DocumentSymbol{}
	}
	result := []DocumentSymbol{}
	for _, symbol := range pkg.Symbols {
		if symbol.SourceFile != filepath.Base(state.Path) {
			continue
		}
		result = append(result, s.documentSymbol(symbol))
	}
	return result
}

func (s *server) documentSymbol(symbol compiler.DocSymbol) DocumentSymbol {
	info := symbolInfo{Symbol: symbol, Path: s.symbolPath(symbol)}
	rangeValue := Range{}
	if location, ok := s.symbolLocation(info); ok {
		rangeValue = location.Range
	}
	result := DocumentSymbol{Name: symbol.Name, Detail: symbol.Signature, Kind: lspSymbolKind(symbol.Kind), Deprecated: deprecated(symbol), Range: rangeValue, SelectionRange: rangeValue}
	for _, field := range symbol.Fields {
		child := compiler.DocSymbol{Kind: compiler.DocField, Name: field.Name, FullName: symbol.FullName + "." + field.Name, Package: symbol.Package, Parent: symbol.Name, Signature: field.Name + " " + field.Type, SourceFile: symbol.SourceFile}
		result.Children = append(result.Children, s.documentSymbol(child))
	}
	for _, method := range append(append([]compiler.DocSymbol{}, symbol.Methods...), symbol.Static...) {
		result.Children = append(result.Children, s.documentSymbol(method))
	}
	for _, value := range symbol.Values {
		child := compiler.DocSymbol{Kind: compiler.DocValue, Name: value.Name, FullName: symbol.FullName + "." + value.Name, Package: symbol.Package, Parent: symbol.Name, Signature: symbol.Name + "." + value.Name + " = " + value.Value, SourceFile: symbol.SourceFile}
		result.Children = append(result.Children, s.documentSymbol(child))
	}
	return result
}

func (s *server) workspaceSymbols(query string) []SymbolInformation {
	query = strings.ToLower(query)
	result := []SymbolInformation{}
	for _, info := range s.workspace.symbols {
		if info.Path == "" || (query != "" && !strings.Contains(strings.ToLower(info.Symbol.FullName), query)) {
			continue
		}
		location, ok := s.symbolLocation(info)
		if !ok {
			continue
		}
		result = append(result, SymbolInformation{Name: info.Symbol.Name, Kind: lspSymbolKind(info.Symbol.Kind), Deprecated: deprecated(info.Symbol), Location: location, ContainerName: info.Symbol.Parent})
	}
	return result
}

func (s *server) formatting(uri string) []TextEdit {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil {
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
	end := Position{Line: strings.Count(state.Source, "\n"), Character: utf16Length(sourceLine(state.Source, strings.Count(state.Source, "\n")))}
	return []TextEdit{{Range: Range{Start: Position{}, End: end}, NewText: formatted}}
}

func (s *server) signatureHelp(params textDocumentPositionParams) *SignatureHelp {
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if state == nil {
		return nil
	}
	offset := offsetAt(state.Source, params.Position)
	prefix := state.Source[:offset]
	open := strings.LastIndex(prefix, "(")
	if open < 0 {
		return nil
	}
	name := identifierBefore(prefix[:open])
	if name == "" {
		return nil
	}
	choices := s.findSymbols(name, params.TextDocument.URI)
	if len(choices) == 0 {
		return nil
	}
	result := &SignatureHelp{}
	for _, choice := range choices {
		if choice.Symbol.Signature == "" {
			continue
		}
		result.Signatures = append(result.Signatures, SignatureInformation{Label: choice.Symbol.Signature, Documentation: choice.Symbol.Doc})
	}
	if len(result.Signatures) == 0 {
		return nil
	}
	return result
}

func (s *server) symbolPath(symbol compiler.DocSymbol) string {
	return s.workspace.symbolPath(symbol)
}

func (s *server) symbolLocation(info symbolInfo) (Location, bool) {
	if info.Path == "" {
		return Location{}, false
	}
	state := s.workspace.files[info.Path]
	if state == nil {
		return Location{}, false
	}
	if state.File != nil {
		if span, ok := astSymbolNameSpan(state.File, info.Symbol); ok {
			return Location{URI: state.URI, Range: rangeForOffsets(state.Source, span.Start, span.End)}, true
		}
	}
	line, start, end := findDeclaration(state.Source, info.Symbol)
	return Location{URI: state.URI, Range: Range{Start: Position{Line: line, Character: start}, End: Position{Line: line, Character: end}}}, true
}

func astSymbolNameSpan(file *compiler.File, symbol compiler.DocSymbol) (compiler.Span, bool) {
	if file == nil || symbol.Name == "" {
		return compiler.Span{}, false
	}
	for _, declaration := range file.Decls {
		if span, ok := astSymbolNameSpanInDecl(file, declaration, symbol); ok {
			return span, true
		}
	}
	return compiler.Span{}, false
}

func astSymbolNameSpanInDecl(file *compiler.File, declaration compiler.Decl, symbol compiler.DocSymbol) (compiler.Span, bool) {
	if declaration == nil {
		return compiler.Span{}, false
	}
	declSpan := declaration.Span()
	switch value := declaration.(type) {
	case *compiler.ClassDecl:
		if symbol.Kind == compiler.DocClass && value.Name == symbol.Name {
			return identifierSpanWithin(file, declSpan, value.Name)
		}
		if symbol.Parent != value.Name {
			return compiler.Span{}, false
		}
		for _, field := range value.Fields {
			if symbol.Kind == compiler.DocField && field.Name == symbol.Name {
				return identifierSpanWithin(file, field.Span(), field.Name)
			}
		}
		for _, method := range value.Methods {
			if symbol.Name == method.Name && (symbol.Kind == compiler.DocMethod || symbol.Kind == compiler.DocExtension) {
				return method.NameSpan, method.NameSpan.End > method.NameSpan.Start
			}
		}
	case *compiler.ExtendDecl:
		if symbol.Kind != compiler.DocExtension {
			return compiler.Span{}, false
		}
		for _, method := range value.Methods {
			if method.Name == symbol.Name {
				return method.NameSpan, method.NameSpan.End > method.NameSpan.Start
			}
		}
	case *compiler.FunctionDecl:
		if symbol.Kind == compiler.DocFunction && value.Name == symbol.Name {
			return value.Method.NameSpan, value.Method.NameSpan.End > value.Method.NameSpan.Start
		}
	case *compiler.ValueDecl:
		if symbol.Kind == compiler.DocValue {
			return identifierSpanWithin(file, declSpan, symbol.Name)
		}
	case *compiler.AnnotationDecl:
		if symbol.Kind == compiler.DocAnnotation && value.Name == symbol.Name {
			return identifierSpanWithin(file, declSpan, value.Name)
		}
	case *compiler.TemplateDecl:
		if symbol.Kind == compiler.DocTemplate && value.Name == symbol.Name {
			return identifierSpanWithin(file, declSpan, value.Name)
		}
	case *compiler.EnumDecl:
		if symbol.Kind == compiler.DocEnum && value.Name == symbol.Name {
			return identifierSpanWithin(file, declSpan, value.Name)
		}
		if symbol.Kind == compiler.DocValue && symbol.Parent == value.Name {
			return identifierSpanWithin(file, declSpan, symbol.Name)
		}
	case *compiler.GoDecl:
		if symbol.Kind == compiler.DocFunction || symbol.Kind == compiler.DocValue {
			return identifierSpanWithin(file, declSpan, symbol.Name)
		}
	case *compiler.MixedDecl:
		for _, function := range value.Functions {
			if function != nil && symbol.Kind == compiler.DocFunction && function.Name == symbol.Name {
				return function.Method.NameSpan, function.Method.NameSpan.End > function.Method.NameSpan.Start
			}
		}
	}
	return compiler.Span{}, false
}

func identifierSpanWithin(file *compiler.File, bounds compiler.Span, name string) (compiler.Span, bool) {
	if file == nil || name == "" {
		return compiler.Span{}, false
	}
	for _, token := range file.Tokens {
		if token.Kind != compiler.TokenIdentifier || token.Text != name {
			continue
		}
		if token.Span.Start >= bounds.Start && token.Span.End <= bounds.End {
			return token.Span, true
		}
	}
	return compiler.Span{}, false
}

func deprecated(symbol compiler.DocSymbol) bool {
	for _, annotation := range symbol.Annotations {
		if strings.HasPrefix(annotation, "@Deprecated") {
			return true
		}
	}
	return false
}

func lspSymbolKind(kind compiler.DocKind) int {
	switch kind {
	case compiler.DocClass:
		return 5
	case compiler.DocEnum:
		return 10
	case compiler.DocFunction:
		return 12
	case compiler.DocMethod, compiler.DocExtension:
		return 6
	case compiler.DocField:
		return 8
	case compiler.DocAnnotation:
		return 13
	case compiler.DocTemplate:
		return 12
	case compiler.DocValue:
		return 22
	default:
		return 13
	}
}

func (s *server) completions(params textDocumentPositionParams) []CompletionItem {
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if state == nil {
		return []CompletionItem{}
	}
	offset := offsetAt(state.Source, params.Position)
	return s.contextualCompletions(state, offset)
}

func findDeclaration(source string, symbol compiler.DocSymbol) (int, int, int) {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	startLine := symbol.SourceLine - 1
	if startLine < 0 {
		startLine = 0
	}
	for pass := 0; pass < 2; pass++ {
		for lineIndex, line := range lines {
			if pass == 0 && absInt(lineIndex-startLine) > 2 {
				continue
			}
			if !declarationLineMatches(line, symbol) {
				continue
			}
			start := strings.Index(line, symbol.Name)
			if start < 0 {
				continue
			}
			return lineIndex, utf16Length(line[:start]), utf16Length(line[:start+len(symbol.Name)])
		}
	}
	return startLine, 0, utf16Length(sourceLine(source, startLine))
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func declarationLineMatches(line string, symbol compiler.DocSymbol) bool {
	trimmed := strings.TrimSpace(line)
	if symbol.Kind == compiler.DocMethod || symbol.Kind == compiler.DocExtension {
		return strings.Contains(trimmed, "func "+symbol.Name)
	}
	if symbol.Kind == compiler.DocClass {
		return strings.HasPrefix(trimmed, "class ") && strings.Contains(trimmed, symbol.Name)
	}
	if symbol.Kind == compiler.DocEnum {
		return strings.HasPrefix(trimmed, "enum ") && strings.Contains(trimmed, symbol.Name)
	}
	if symbol.Kind == compiler.DocFunction {
		return strings.Contains(trimmed, "func "+symbol.Name)
	}
	if symbol.Kind == compiler.DocAnnotation {
		return strings.HasPrefix(trimmed, "annotation ")
	}
	if symbol.Kind == compiler.DocTemplate {
		return strings.HasPrefix(trimmed, "template ")
	}
	if symbol.Kind == compiler.DocField {
		return identifierAtLineStart(trimmed, symbol.Name)
	}
	return strings.Contains(trimmed, symbol.Name)
}

func identifierAtLineStart(line, name string) bool {
	return strings.HasPrefix(line, name+" ") || strings.HasPrefix(line, name+"\t") || line == name
}

func identifierSpans(file *compiler.File, source, name string) []Range {
	if name == "" {
		return nil
	}
	if file != nil && len(file.Tokens) > 0 {
		result := []Range{}
		for _, token := range file.Tokens {
			if token.Kind == compiler.TokenIdentifier && token.Text == name {
				result = append(result, rangeForOffsets(source, token.Span.Start, token.Span.End))
			}
		}
		return result
	}
	result := []Range{}
	for offset := 0; offset < len(source); {
		index := strings.Index(source[offset:], name)
		if index < 0 {
			break
		}
		start := offset + index
		end := start + len(name)
		beforeOK := start == 0 || !isIdentifierByte(source[start-1])
		afterOK := end == len(source) || !isIdentifierByte(source[end])
		if beforeOK && afterOK {
			result = append(result, rangeForOffsets(source, start, end))
		}
		offset = end
	}
	return result
}

func identifierTokenAt(tokens []compiler.Token, offset int) (compiler.Token, bool) {
	index := sort.Search(len(tokens), func(index int) bool {
		return tokens[index].Span.Start > offset
	}) - 1
	if index >= 0 {
		token := tokens[index]
		if token.Kind == compiler.TokenIdentifier && token.Span.Start <= offset && offset <= token.Span.End {
			return token, true
		}
	}
	return compiler.Token{}, false
}

func validIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, r := range value {
		if index == 0 && !(r == '_' || unicode.IsLetter(r)) {
			return false
		}
		if index > 0 && !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

func identifierBefore(value string) string {
	end := len(value)
	for end > 0 && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\n' || value[end-1] == '\r') {
		end--
	}
	start := end
	for start > 0 && isIdentifierByte(value[start-1]) {
		start--
	}
	return value[start:end]
}

func isIdentifierByte(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func offsetAt(source string, position Position) int {
	if position.Line <= 0 {
		return utf16Offset(sourceLine(source, 0), position.Character)
	}
	offset := 0
	line := 0
	for offset < len(source) && line < position.Line {
		if source[offset] == '\n' {
			line++
		}
		offset++
	}
	return offset + utf16Offset(source[offset:], position.Character)
}

func utf16Offset(source string, character int) int {
	if character <= 0 {
		return 0
	}
	count, offset := 0, 0
	for offset < len(source) && count < character {
		runeValue, size := decodeRune(source[offset:])
		units := 1
		if runeValue > 0xffff {
			units = 2
		}
		if count+units > character {
			break
		}
		count += units
		offset += size
	}
	return offset
}

func utf16Length(source string) int {
	count := 0
	for offset := 0; offset < len(source); {
		runeValue, size := decodeRune(source[offset:])
		if runeValue > 0xffff {
			count += 2
		} else {
			count++
		}
		offset += size
	}
	return count
}

func decodeRune(source string) (rune, int) {
	for _, value := range source {
		return value, len(string(value))
	}
	return 0, 0
}

func rangeForOffsets(source string, start, end int) Range {
	return Range{Start: positionAtOffset(source, start), End: positionAtOffset(source, end)}
}

func positionAtOffset(source string, offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	line := strings.Count(source[:offset], "\n")
	lineStart := strings.LastIndex(source[:offset], "\n") + 1
	return Position{Line: line, Character: utf16Length(source[lineStart:offset])}
}

func applyChange(source string, change Range, replacement string) string {
	start := offsetAt(source, change.Start)
	end := offsetAt(source, change.End)
	if start > end {
		start, end = end, start
	}
	if start > len(source) {
		start = len(source)
	}
	if end > len(source) {
		end = len(source)
	}
	return source[:start] + replacement + source[end:]
}

func pathFromURI(value string) string {
	if strings.HasPrefix(value, "file://") {
		parsed, err := url.Parse(value)
		if err == nil {
			path, err := url.PathUnescape(parsed.Path)
			if err == nil {
				return filepath.Clean(path)
			}
		}
	}
	return value
}

func uriForPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
