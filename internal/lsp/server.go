// Package lsp implements the editor-facing JSON-RPC service for Go++.
//
// The server deliberately stays dependency-free. It speaks the small JSON-RPC
// envelope used by LSP directly and delegates parsing, semantic validation,
// documentation, and formatting to the compiler package.
package lsp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	gotoken "go/token"
	"go/types"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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
	Range              Range                          `json:"range"`
	Severity           int                            `json:"severity,omitempty"`
	Source             string                         `json:"source,omitempty"`
	Code               string                         `json:"code,omitempty"`
	Data               any                            `json:"data,omitempty"`
	Message            string                         `json:"message"`
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
}

type DiagnosticRelatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

const (
	diagnosticSyntax          = "GPP1000"
	diagnosticUnclosedLiteral = "GPP1001"
	diagnosticUnclosedComment = "GPP1002"
	diagnosticSemantic        = "GPP2000"
)

var errExitWithoutShutdown = errors.New("LSP exit received before shutdown")

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type PrepareRenameResult struct {
	Range       Range  `json:"range"`
	Placeholder string `json:"placeholder"`
}

type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Deprecated    bool     `json:"deprecated,omitempty"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
	Data          any      `json:"data,omitempty"`
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
	Label               string     `json:"label"`
	Kind                int        `json:"kind,omitempty"`
	Detail              string     `json:"detail,omitempty"`
	Documentation       string     `json:"documentation,omitempty"`
	Deprecated          bool       `json:"deprecated,omitempty"`
	InsertText          string     `json:"insertText,omitempty"`
	TextEdit            *TextEdit  `json:"textEdit,omitempty"`
	AdditionalTextEdits []TextEdit `json:"additionalTextEdits,omitempty"`
	InsertTextFormat    int        `json:"insertTextFormat,omitempty"`
	Data                any        `json:"data,omitempty"`
}

type completionResolveData struct {
	URI      string   `json:"uri"`
	Position Position `json:"position"`
	Label    string   `json:"label"`
}

type codeActionResolveData struct {
	Params codeActionParams `json:"params"`
	Title  string           `json:"title"`
	Kind   string           `json:"kind"`
}

type workspaceSymbolResolveData struct {
	FullName string `json:"fullName"`
	Path     string `json:"path"`
}

type executeCommandParams struct {
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments,omitempty"`
}

type DocumentHighlight struct {
	Range Range `json:"range"`
	Kind  int   `json:"kind,omitempty"`
}

type FoldingRange struct {
	StartLine      int    `json:"startLine"`
	StartCharacter int    `json:"startCharacter,omitempty"`
	EndLine        int    `json:"endLine"`
	EndCharacter   int    `json:"endCharacter,omitempty"`
	Kind           string `json:"kind,omitempty"`
}

type SelectionRange struct {
	Range  Range           `json:"range"`
	Parent *SelectionRange `json:"parent,omitempty"`
}

type DocumentLink struct {
	Range   Range  `json:"range"`
	Target  string `json:"target,omitempty"`
	Tooltip string `json:"tooltip,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type InlayHint struct {
	Position     Position `json:"position"`
	Label        string   `json:"label"`
	Kind         int      `json:"kind,omitempty"`
	PaddingLeft  bool     `json:"paddingLeft,omitempty"`
	PaddingRight bool     `json:"paddingRight,omitempty"`
	Tooltip      any      `json:"tooltip,omitempty"`
	Data         any      `json:"data,omitempty"`
}

type CodeLens struct {
	Range   Range    `json:"range"`
	Command *Command `json:"command,omitempty"`
	Data    any      `json:"data,omitempty"`
}

type documentLinkResolveData struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type inlayHintResolveData struct {
	URI      string   `json:"uri"`
	Position Position `json:"position"`
	Label    string   `json:"label"`
}

type codeLensResolveData struct {
	URI      string `json:"uri"`
	FullName string `json:"fullName"`
	Path     string `json:"path"`
}

type Command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation string                 `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

type ParameterInformation struct {
	Label string `json:"label"`
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
	Context      codeActionContext      `json:"context"`
}

type codeActionContext struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Only        []string     `json:"only,omitempty"`
}

type codeAction struct {
	Title       string        `json:"title"`
	Kind        string        `json:"kind"`
	Diagnostics []Diagnostic  `json:"diagnostics,omitempty"`
	Edit        workspaceEdit `json:"edit"`
	IsPreferred bool          `json:"isPreferred,omitempty"`
	Data        any           `json:"data,omitempty"`
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
		URI     string `json:"uri"`
		Version *int   `json:"version,omitempty"`
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
	Capabilities     struct {
		Window struct {
			WorkDoneProgress bool `json:"workDoneProgress"`
		} `json:"window"`
		Workspace struct {
			Symbol struct {
				ResolveSupport struct {
					Properties []string `json:"properties"`
				} `json:"resolveSupport"`
			} `json:"symbol"`
			DidChangeWatchedFiles struct {
				DynamicRegistration bool `json:"dynamicRegistration"`
			} `json:"didChangeWatchedFiles"`
		} `json:"workspace"`
	} `json:"capabilities"`
}

type fileState struct {
	Path       string
	URI        string
	Source     string
	Open       bool
	Version    int
	HasVersion bool
	File       *compiler.File
	goHoverMu  sync.Mutex
	goHover    *nativeGoHoverCache
}

type symbolInfo struct {
	Symbol compiler.DocSymbol
	Path   string
}

type workspaceState struct {
	roots       []string
	files       map[string]*fileState
	diagnostics map[string][]Diagnostic
	codegenKeys map[string][32]byte
	docIndex    *compiler.DocIndex
	symbols     []symbolInfo
	official    []*compiler.File
	logger      *log.Logger
}

var materializedOfficialStdlib = struct {
	sync.Once
	root string
	err  error
}{}

func newWorkspace(logger *log.Logger) *workspaceState {
	workspace := &workspaceState{
		files:       map[string]*fileState{},
		diagnostics: map[string][]Diagnostic{},
		codegenKeys: map[string][32]byte{},
		logger:      logger,
	}
	// Official packages are immutable compiler inputs. Parse them once per
	// LSP session instead of rebuilding their ASTs on every editor keystroke.
	for _, packagePath := range compiler.OfficialStdlibPackages() {
		files, err := compiler.LoadOfficialPackage(packagePath)
		if err == nil {
			for _, file := range files {
				file.SourcePath = materializeOfficialSource(file)
			}
			workspace.official = append(workspace.official, files...)
		}
	}
	return workspace
}

func materializeOfficialSource(file *compiler.File) string {
	if file == nil || file.OfficialPackage == "" || file.Source == "" {
		return ""
	}
	materializedOfficialStdlib.Do(func() {
		materializedOfficialStdlib.root, materializedOfficialStdlib.err = os.MkdirTemp("", "gpp-lsp-stdlib-")
	})
	if materializedOfficialStdlib.err != nil || materializedOfficialStdlib.root == "" {
		return ""
	}
	directory := filepath.Join(materializedOfficialStdlib.root, filepath.FromSlash(file.OfficialPackage))
	if err := os.MkdirAll(directory, 0755); err != nil {
		return ""
	}
	path := filepath.Join(directory, filepath.Base(file.Name))
	if existing, err := os.ReadFile(path); err == nil && string(existing) == file.Source {
		return path
	}
	if err := os.WriteFile(path, []byte(file.Source), 0644); err != nil {
		return ""
	}
	return path
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

func (w *workspaceState) refresh() []string {
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
			if !isWorkspaceSourceFile(path) {
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
	var removed []string
	for path, state := range w.files {
		if !known[path] && !state.Open {
			removed = append(removed, state.URI)
			delete(w.files, path)
			delete(w.diagnostics, state.URI)
		}
	}
	w.analyze()
	sort.Strings(removed)
	return removed
}

func (w *workspaceState) analyze() {
	parsed := make([]*compiler.File, 0, len(w.files))
	byModule := map[string][]*compiler.File{}
	modulePaths := map[string]string{}
	filePaths := make([]string, 0, len(w.files))
	for path := range w.files {
		filePaths = append(filePaths, path)
	}
	sort.Strings(filePaths)
	for _, path := range filePaths {
		state := w.files[path]
		state.File = nil
		if !isWorkspaceSourceFile(state.Path) {
			continue
		}
		file, err := compiler.ParseFile(state.Path, state.Source)
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
		moduleRoot, modulePath := moduleInfoForPath(state.Path)
		if moduleRoot == "" {
			moduleRoot = filepath.Dir(state.Path)
		}
		byModule[moduleRoot] = append(byModule[moduleRoot], file)
		modulePaths[moduleRoot] = modulePath
		w.diagnostics[state.URI] = []Diagnostic{}
	}

	// Include the immutable compiler-owned ASTs so imported annotations/classes
	// and standard extensions are represented consistently in editor features.
	officialFiles := w.official
	for root, files := range byModule {
		allFiles := append([]*compiler.File{}, files...)
		allFiles = append(allFiles, officialFiles...)
		_, err := compiler.ResolveProgramWithModulePath(&compiler.Program{Files: allFiles}, modulePaths[root])
		if err != nil {
			for _, diagnosticErr := range compiler.DiagnosticsOf(err) {
				path, source, ok := w.errorFile(diagnosticErr)
				if !ok && len(files) == 1 {
					path, source = files[0].SourcePath, files[0].Source
					ok = true
				}
				if ok {
					uri := uriForPath(path)
					diagnostic := diagnosticFromError(uri, source, diagnosticErr)
					diagnostic.RelatedInformation = w.relatedDiagnostics(diagnosticErr)
					w.diagnostics[uri] = append(w.diagnostics[uri], diagnostic)
				}
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

// checkGeneratedGo runs the compiler's code-generation pipeline against the
// current in-memory workspace buffers. It catches errors that only appear
// after Go++ lowering (including invalid generated Go) while keeping generated
// files and build metadata out of the user's project tree.
func (w *workspaceState) checkGeneratedGo(ctx context.Context, byModule map[string][]*compiler.File, modulePaths map[string]string) {
	if w.codegenKeys == nil {
		w.codegenKeys = map[string][32]byte{}
	}
	for root, files := range byModule {
		if ctx.Err() != nil {
			return
		}
		// A syntax or semantic error makes the generated package incomplete and
		// can create cascading errors during emission. The original diagnostic is
		// already available, so wait for the next complete edit before emitting.
		incomplete := false
		for _, state := range w.files {
			if state == nil || !isWorkspaceSourceFile(state.Path) {
				continue
			}
			stateRoot, _ := moduleInfoForPath(state.Path)
			if stateRoot == "" {
				stateRoot = filepath.Dir(state.Path)
			}
			if stateRoot == root && len(w.diagnostics[state.URI]) > 0 {
				incomplete = true
				break
			}
		}
		if incomplete {
			continue
		}
		fingerprint := sha256.New()
		fingerprint.Write([]byte(root))
		fingerprint.Write([]byte(modulePaths[root]))
		fingerprint.Write([]byte{0})
		for _, metadata := range []string{"go.mod", "go.sum"} {
			fingerprint.Write([]byte(metadata))
			fingerprint.Write([]byte{0})
			if contents, err := os.ReadFile(filepath.Join(root, metadata)); err == nil {
				fingerprint.Write(contents)
			}
			fingerprint.Write([]byte{0})
		}
		for _, file := range files {
			fingerprint.Write([]byte(file.SourcePath))
			fingerprint.Write([]byte{0})
			fingerprint.Write([]byte(file.Source))
			fingerprint.Write([]byte{0})
		}
		var key [32]byte
		copy(key[:], fingerprint.Sum(nil))
		if previous, ok := w.codegenKeys[root]; ok && previous == key {
			continue
		}
		w.codegenKeys[root] = key

		rootHash := sha256.Sum256([]byte(root))
		outputDir := filepath.Join(os.TempDir(), "gpp-lsp", strconv.Itoa(os.Getpid()), hex.EncodeToString(rootHash[:12]))
		programFiles := make([]*compiler.File, 0, len(files))
		for _, file := range files {
			parsed, parseErr := compiler.ParseFile(filepath.Base(file.SourcePath), file.Source)
			if parseErr != nil {
				programFiles = nil
				break
			}
			parsed.SourcePath = file.SourcePath
			programFiles = append(programFiles, parsed)
		}
		if programFiles == nil {
			continue
		}
		compileErr := compiler.CompileProgramWithOptions(&compiler.Program{Files: programFiles}, outputDir, compiler.CompileOptions{
			ModulePath:        modulePaths[root],
			ProjectRoot:       root,
			IncrementalOutput: true,
		})
		if compileErr == nil {
			if modulePaths[root] != "" {
				for _, diagnostic := range generatedGoBuildDiagnostics(ctx, root, outputDir) {
					state := w.files[filepath.Clean(diagnostic.File)]
					if state == nil {
						state = w.generatedSourceFile(diagnostic.File, root, outputDir)
					}
					if state == nil {
						continue
					}
					uri := state.URI
					diagnostic.Span = backendSourceSpan(state.Source, diagnostic)
					lspDiagnostic := diagnosticFromError(uri, state.Source, diagnostic)
					lspDiagnostic.Source = "go build"
					lspDiagnostic.Code = "GPP3000"
					w.diagnostics[uri] = append(w.diagnostics[uri], lspDiagnostic)
				}
			}
			continue
		}
		for _, diagnosticErr := range compiler.DiagnosticsOf(compileErr) {
			path, source, ok := w.errorFile(diagnosticErr)
			if !ok {
				continue
			}
			uri := uriForPath(path)
			diagnostic := diagnosticFromError(uri, source, diagnosticErr)
			diagnostic.Source = "gpp compiler"
			duplicate := false
			for _, existing := range w.diagnostics[uri] {
				if existing.Range == diagnostic.Range && existing.Message == diagnostic.Message {
					duplicate = true
					break
				}
			}
			if !duplicate {
				w.diagnostics[uri] = append(w.diagnostics[uri], diagnostic)
			}
		}
	}
}

func (w *workspaceState) generatedSourceFile(generatedPath, moduleRoot, outputDir string) *fileState {
	relative, err := filepath.Rel(outputDir, generatedPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil
	}
	generatedName := filepath.Base(relative)
	generatedDir := filepath.Dir(relative)
	for _, state := range w.files {
		if state == nil || !isGoPlusFile(state.Path) || state.File == nil {
			continue
		}
		root, _ := moduleInfoForPath(state.Path)
		if root == "" {
			root = filepath.Dir(state.Path)
		}
		if root != moduleRoot {
			continue
		}
		base := filepath.Base(state.Path)
		name := strings.TrimSuffix(base, ".gpp") + ".go"
		if strings.HasSuffix(base, ".gpp.tpl") {
			name = strings.TrimSuffix(base, ".gpp.tpl") + "_tpl.go"
		}
		if generatedName != name && !strings.HasPrefix(generatedName, strings.TrimSuffix(name, ".go")+"_") {
			continue
		}
		packageDir := "."
		if state.File.Package != "main" {
			packageDir = filepath.Join(strings.Split(state.File.Package, ".")...)
		}
		if filepath.Clean(generatedDir) == filepath.Clean(packageDir) {
			return state
		}
	}
	return nil
}

var goBuildDiagnosticLine = regexp.MustCompile(`^(.+):(\d+):(\d+): (.*)$`)
var goBuildLineDiagnostic = regexp.MustCompile(`^(.+):(\d+): (.*)$`)
var backendIdentifier = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func backendSourceSpan(source string, diagnostic compiler.DiagnosticError) compiler.Span {
	span := diagnostic.Span
	if span.Line < 1 {
		return span
	}
	lineStart := offsetForLineColumn(source, span.Line-1, 0)
	lineEnd := strings.IndexByte(source[lineStart:], '\n')
	if lineEnd < 0 {
		lineEnd = len(source)
	} else {
		lineEnd += lineStart
	}
	line := source[lineStart:lineEnd]
	for _, identifier := range backendIdentifier.FindAllString(diagnostic.Cause.Error(), -1) {
		for offset := 0; offset+len(identifier) <= len(line); {
			index := strings.Index(line[offset:], identifier)
			if index < 0 {
				break
			}
			index += offset
			end := index + len(identifier)
			leftBoundary := index == 0 || !isIdentifierByte(line[index-1])
			rightBoundary := end == len(line) || !isIdentifierByte(line[end])
			if leftBoundary && rightBoundary {
				return compiler.Span{Start: lineStart + index, End: lineStart + end, Line: span.Line, Column: index + 1}
			}
			offset = end
		}
	}
	return span
}

// generatedGoBuildDiagnostics checks generated source in a temporary module
// tree. It keeps generated files and module metadata changes out of the
// user's checkout.
func generatedGoBuildDiagnostics(ctx context.Context, moduleRoot, outputDir string) []compiler.DiagnosticError {
	moduleData, err := os.ReadFile(filepath.Join(moduleRoot, "go.mod"))
	if err != nil || os.WriteFile(filepath.Join(outputDir, "go.mod"), moduleData, 0644) != nil {
		return nil
	}
	if sumData, sumErr := os.ReadFile(filepath.Join(moduleRoot, "go.sum")); sumErr == nil {
		if os.WriteFile(filepath.Join(outputDir, "go.sum"), sumData, 0644) != nil {
			return nil
		}
	}
	// Relative local replacements in go.mod are relative to its location. The
	// temporary module lives elsewhere, so rewrite those replacements to their
	// absolute targets in the copied go.mod.
	moduleJSON, err := exec.CommandContext(ctx, "go", "mod", "edit", "-json", filepath.Join(moduleRoot, "go.mod")).Output()
	if err != nil {
		return nil
	}
	var moduleInfo struct {
		Replace []struct {
			Old struct{ Path, Version string }
			New struct{ Path, Version string }
		}
	}
	if json.Unmarshal(moduleJSON, &moduleInfo) != nil {
		return nil
	}
	for _, replacement := range moduleInfo.Replace {
		if replacement.New.Version != "" || filepath.IsAbs(replacement.New.Path) {
			continue
		}
		target, absErr := filepath.Abs(filepath.Join(moduleRoot, replacement.New.Path))
		if absErr != nil {
			continue
		}
		old := replacement.Old.Path
		if replacement.Old.Version != "" {
			old += "@" + replacement.Old.Version
		}
		edit := exec.CommandContext(ctx, "go", "mod", "edit", "-modfile=go.mod", "-replace="+old+"="+target)
		edit.Dir = outputDir
		if edit.Run() != nil {
			return nil
		}
	}
	modMode := "readonly"
	if _, statErr := os.Stat(filepath.Join(moduleRoot, "vendor")); statErr == nil {
		modMode = "vendor"
		if _, lstatErr := os.Lstat(filepath.Join(outputDir, "vendor")); os.IsNotExist(lstatErr) {
			if os.Symlink(filepath.Join(moduleRoot, "vendor"), filepath.Join(outputDir, "vendor")) != nil {
				return nil
			}
		}
	}
	goFlags := make([]string, 0)
	for _, flag := range strings.Fields(os.Getenv("GOFLAGS")) {
		if !strings.HasPrefix(flag, "-mod=") {
			goFlags = append(goFlags, flag)
		}
	}
	goFlags = append(goFlags, "-mod="+modMode)
	command := exec.CommandContext(ctx, "go", "build", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS="+strings.Join(goFlags, " "))
	output, buildErr := command.CombinedOutput()
	if buildErr == nil {
		return nil
	}
	var diagnostics []compiler.DiagnosticError
	for _, line := range strings.Split(string(output), "\n") {
		match := goBuildDiagnosticLine.FindStringSubmatch(line)
		lineOnly := false
		if match == nil {
			match = goBuildLineDiagnostic.FindStringSubmatch(line)
			lineOnly = true
		}
		if match == nil {
			continue
		}
		path := match[1]
		if !filepath.IsAbs(path) {
			path = filepath.Join(outputDir, filepath.FromSlash(path))
		}
		path, err = filepath.Abs(path)
		if err != nil {
			continue
		}
		lineNumber, lineErr := strconv.Atoi(match[2])
		column := 1
		message := match[3]
		columnErr := error(nil)
		if !lineOnly {
			column, columnErr = strconv.Atoi(match[3])
			message = match[4]
		}
		if lineErr != nil || columnErr != nil || lineNumber < 1 || column < 1 {
			continue
		}
		diagnostics = append(diagnostics, compiler.DiagnosticError{
			File:  path,
			Span:  compiler.Span{Line: lineNumber, Column: column},
			Cause: errors.New(message),
		})
	}
	return diagnostics
}

func (w *workspaceState) relatedDiagnostics(err error) []DiagnosticRelatedInformation {
	var result []DiagnosticRelatedInformation
	for _, related := range compiler.RelatedPositionsOfDiagnostic(err) {
		state := w.fileForSourcePath(related.File)
		if state == nil {
			continue
		}
		start, end := offsetForLineColumn(state.Source, related.Span.Line-1, related.Span.Column-1), 0
		if related.Span.End > related.Span.Start && related.Span.Start >= 0 && related.Span.End <= len(state.Source) {
			start, end = related.Span.Start, related.Span.End
		} else {
			end = start
		}
		location := Location{URI: state.URI, Range: rangeForOffsets(state.Source, start, end)}
		result = append(result, DiagnosticRelatedInformation{Location: location, Message: related.Message})
	}
	return result
}

func (w *workspaceState) errorFile(err error) (string, string, bool) {
	if position, ok := compiler.PositionOfDiagnostic(err); ok {
		if state := w.fileForSourcePath(position.File); state != nil {
			return state.Path, state.Source, true
		}
	}
	message := err.Error()
	var match *fileState
	for path, state := range w.files {
		if !strings.Contains(message, path) {
			continue
		}
		if match != nil {
			return "", "", false
		}
		match = state
	}
	if match != nil {
		return match.Path, match.Source, true
	}
	for path, state := range w.files {
		if !strings.Contains(message, filepath.Base(path)) {
			continue
		}
		if match != nil {
			return "", "", false
		}
		match = state
	}
	if match != nil {
		return match.Path, match.Source, true
	}
	return "", "", false
}

func (w *workspaceState) fileForSourcePath(sourcePath string) *fileState {
	if sourcePath == "" {
		return nil
	}
	clean := filepath.Clean(sourcePath)
	if state := w.files[clean]; state != nil {
		return state
	}
	if absolute, err := filepath.Abs(clean); err == nil {
		if state := w.files[absolute]; state != nil {
			return state
		}
	}
	var match *fileState
	for path, state := range w.files {
		if filepath.Base(path) != filepath.Base(clean) {
			continue
		}
		if match != nil {
			return nil
		}
		match = state
	}
	return match
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
		if state.File == nil || state.File.Package != symbol.Package {
			continue
		}
		if filepath.Clean(path) == filepath.Clean(symbol.SourceFile) {
			return path
		}
	}
	var basenameMatch string
	for path, state := range w.files {
		if state.File == nil || state.File.Package != symbol.Package {
			continue
		}
		if filepath.Base(path) == filepath.Base(symbol.SourceFile) {
			if basenameMatch != "" {
				return ""
			}
			basenameMatch = path
		}
	}
	for _, file := range w.official {
		if file == nil || file.Package != symbol.Package || file.SourcePath == "" {
			continue
		}
		if filepath.Base(file.SourcePath) == filepath.Base(symbol.SourceFile) || filepath.Base(file.Name) == filepath.Base(symbol.SourceFile) {
			return filepath.Clean(file.SourcePath)
		}
	}
	return basenameMatch
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

func isWorkspaceSourceFile(path string) bool {
	return isGoPlusFile(path) || strings.HasSuffix(path, ".go")
}

func moduleInfoForPath(filename string) (string, string) {
	directory := filepath.Dir(filename)
	for {
		goMod := filepath.Join(directory, "go.mod")
		if contents, err := os.ReadFile(goMod); err == nil {
			for _, line := range strings.Split(string(contents), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "module" {
					return directory, strings.Trim(fields[1], "\"`")
				}
			}
			return directory, ""
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", ""
		}
		directory = parent
	}
}

func diagnosticFromError(uri, source string, err error) Diagnostic {
	message := err.Error()
	line := 0
	byteCharacter := 0
	var sourceSpan compiler.Span
	if position, ok := compiler.PositionOfDiagnostic(err); ok {
		sourceSpan = position.Span
		line = sourceSpan.Line - 1
		if sourceSpan.Column > 0 {
			byteCharacter = sourceSpan.Column - 1
		}
		message = position.Message
	}
	if line < 0 {
		line = 0
	}
	startOffset := offsetForLineColumn(source, line, byteCharacter)
	endOffset := startOffset
	if sourceSpan.End > sourceSpan.Start && sourceSpan.Start >= 0 && sourceSpan.End <= len(source) {
		startOffset, endOffset = sourceSpan.Start, sourceSpan.End
	} else if startOffset < len(source) {
		runeValue, width := decodeRune(source[startOffset:])
		if width > 0 && runeValue != '\n' && runeValue != '\r' {
			endOffset = startOffset + width
		}
	}
	startPosition := positionAtOffset(source, startOffset)
	endPosition := positionAtOffset(source, endOffset)
	if endPosition.Line < startPosition.Line || endPosition.Line == startPosition.Line && endPosition.Character < startPosition.Character {
		endPosition = startPosition
	}
	diagnostic := Diagnostic{Range: Range{Start: startPosition, End: endPosition}, Severity: 1, Source: "gpp", Code: diagnosticSemantic, Message: message}
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

func offsetForLineColumn(source string, line, byteColumn int) int {
	if line < 0 {
		line = 0
	}
	start := 0
	for current := 0; current < line; current++ {
		next := strings.IndexByte(source[start:], '\n')
		if next < 0 {
			return len(source)
		}
		start += next + 1
	}
	end := strings.IndexByte(source[start:], '\n')
	if end < 0 {
		end = len(source) - start
	}
	if byteColumn < 0 {
		byteColumn = 0
	}
	if byteColumn > end {
		byteColumn = end
	}
	return start + byteColumn
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
	reader                              *bufio.Reader
	writer                              *bufio.Writer
	writeLock                           sync.Mutex
	workspace                           *workspaceState
	logger                              *log.Logger
	initializeReceived                  bool
	initializedReceived                 bool
	shutdown                            bool
	supportsWorkDoneProgress            bool
	supportsWatchedFileRegistration     bool
	supportsWorkspaceSymbolRangeResolve bool
	handleRequest                       func(rpcRequest) (bool, error)
	requestContext                      context.Context
	protocolDispatch                    sync.Mutex
	semanticTokensMu                    sync.Mutex
	semanticTokensSequence              uint64
	semanticTokensByURI                 map[string]semanticTokenSnapshot
	nativeGoDiagnosticsMu               sync.Mutex
	nativeGoDiagnosticsKey              [32]byte
	nativeGoDiagnosticsCached           bool
	nativeGoDiagnosticsByURI            map[string][]Diagnostic
	backendResults                      chan backendDiagnosticResult
	backendStop                         chan struct{}
	backendScheduleMu                   sync.Mutex
	backendCompileMu                    sync.Mutex
	backendTimer                        *time.Timer
	backendGeneration                   uint64
	backendDiagnostics                  map[string][]Diagnostic
	backendWorkspaceKey                 [32]byte
	backendCancel                       context.CancelFunc
	backendStopOnce                     sync.Once
}

// scheduleGeneratedDiagnostics snapshots the current workspace and runs the
// generated-Go backend pass after edits have settled. The LSP worker remains
// free to process requests and document changes while Go code is compiling.
func (s *server) scheduleGeneratedDiagnostics() {
	if s.workspace == nil || s.backendResults == nil || s.backendStop == nil {
		return
	}

	files := make(map[string]*fileState, len(s.workspace.files))
	diagnostics := make(map[string][]Diagnostic, len(s.workspace.diagnostics))
	baselineCounts := make(map[string]int, len(s.workspace.diagnostics))
	byModule := map[string][]*compiler.File{}
	modulePaths := map[string]string{}
	fingerprint := sha256.New()
	moduleRoots := map[string]bool{}
	roots := append([]string(nil), s.workspace.roots...)
	sort.Strings(roots)
	for _, root := range roots {
		fingerprint.Write([]byte(root))
		fingerprint.Write([]byte{0})
	}
	paths := make([]string, 0, len(s.workspace.files))
	for path := range s.workspace.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		state := s.workspace.files[path]
		files[path] = &fileState{Path: state.Path, URI: state.URI, Source: state.Source, File: state.File}
		fingerprint.Write([]byte(path))
		fingerprint.Write([]byte{0})
		fingerprint.Write([]byte(state.Source))
		fingerprint.Write([]byte{0})
		if isWorkspaceSourceFile(path) {
			root, modulePath := moduleInfoForPath(path)
			if root == "" {
				root = filepath.Dir(path)
			}
			moduleRoots[root] = true
			fingerprint.Write([]byte(root))
			fingerprint.Write([]byte(modulePath))
			fingerprint.Write([]byte{0})
			if state.File == nil {
				continue
			}
			byModule[root] = append(byModule[root], state.File)
			modulePaths[root] = modulePath
		}
	}
	orderedModuleRoots := make([]string, 0, len(moduleRoots))
	for root := range moduleRoots {
		orderedModuleRoots = append(orderedModuleRoots, root)
	}
	sort.Strings(orderedModuleRoots)
	for _, root := range orderedModuleRoots {
		for _, metadata := range []string{"go.mod", "go.sum"} {
			fingerprint.Write([]byte(metadata))
			fingerprint.Write([]byte{0})
			if contents, err := os.ReadFile(filepath.Join(root, metadata)); err == nil {
				fingerprint.Write(contents)
			}
			fingerprint.Write([]byte{0})
		}
	}
	var workspaceKey [32]byte
	copy(workspaceKey[:], fingerprint.Sum(nil))
	for uri, current := range s.workspace.diagnostics {
		diagnostics[uri] = append([]Diagnostic(nil), current...)
		baselineCounts[uri] = len(current)
	}

	s.backendScheduleMu.Lock()
	if s.backendWorkspaceKey == workspaceKey {
		s.backendScheduleMu.Unlock()
		return
	}
	s.backendWorkspaceKey = workspaceKey
	s.backendGeneration++
	generation := s.backendGeneration
	if s.backendCancel != nil {
		s.backendCancel()
	}
	backendContext, cancelBackend := context.WithCancel(context.Background())
	s.backendCancel = cancelBackend
	if s.backendTimer != nil {
		s.backendTimer.Stop()
	}
	s.backendDiagnostics = map[string][]Diagnostic{}
	keys := make(map[string][32]byte, len(s.workspace.codegenKeys))
	for root, key := range s.workspace.codegenKeys {
		keys[root] = key
	}
	s.backendTimer = time.AfterFunc(250*time.Millisecond, func() {
		defer cancelBackend()
		select {
		case <-s.backendStop:
			return
		default:
		}
		s.backendCompileMu.Lock()
		defer s.backendCompileMu.Unlock()
		if backendContext.Err() != nil {
			return
		}
		s.backendScheduleMu.Lock()
		current := generation == s.backendGeneration
		s.backendScheduleMu.Unlock()
		if !current {
			return
		}
		snapshot := &workspaceState{
			files: files, diagnostics: diagnostics, codegenKeys: keys,
			official: s.workspace.official,
		}
		snapshot.checkGeneratedGo(backendContext, byModule, modulePaths)
		if backendContext.Err() != nil {
			return
		}
		backend := make(map[string][]Diagnostic)
		for uri, all := range snapshot.diagnostics {
			baseline := baselineCounts[uri]
			if baseline < len(all) {
				backend[uri] = append([]Diagnostic(nil), all[baseline:]...)
			}
		}
		result := backendDiagnosticResult{generation: generation, diagnostics: backend, codegenKeys: snapshot.codegenKeys}
		select {
		case s.backendResults <- result:
		case <-s.backendStop:
		}
	})
	s.backendScheduleMu.Unlock()
}

func (s *server) stopBackend() {
	s.backendScheduleMu.Lock()
	if s.backendCancel != nil {
		s.backendCancel()
	}
	if s.backendTimer != nil {
		s.backendTimer.Stop()
	}
	s.backendScheduleMu.Unlock()
	if s.backendStop == nil {
		s.backendStop = make(chan struct{})
	}
	s.backendStopOnce.Do(func() { close(s.backendStop) })
}

func (s *server) acceptBackendDiagnostics(result backendDiagnosticResult) bool {
	s.backendScheduleMu.Lock()
	defer s.backendScheduleMu.Unlock()
	if s.shutdown || result.generation != s.backendGeneration {
		return false
	}
	s.backendDiagnostics = result.diagnostics
	s.workspace.codegenKeys = result.codegenKeys
	return true
}

type backendDiagnosticResult struct {
	generation  uint64
	diagnostics map[string][]Diagnostic
	codegenKeys map[string][32]byte
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
	return serve(s)
}

type queuedRequest struct {
	request rpcRequest
	ctx     context.Context
	cancel  context.CancelFunc
	key     string
}

// serve keeps reading notifications while one request is being handled so
// cancellation messages can reach the active request. The worker processes
// document changes and requests in wire order, preserving the server's
// single-writer workspace model.
func serve(s *server) error {
	if s.backendResults == nil {
		s.backendResults = make(chan backendDiagnosticResult, 1)
	}
	if s.backendStop == nil {
		s.backendStop = make(chan struct{})
	}
	// Buffer requests while a long operation runs so the reader can still
	// consume cancellation messages and workspace notifications.
	queue := make(chan queuedRequest, 1024)
	workerDone := make(chan struct{})
	active := map[string]context.CancelFunc{}
	activeProgress := map[string]context.CancelFunc{}
	var activeMu sync.Mutex
	go func() {
		defer close(workerDone)
		queueChannel := (<-chan queuedRequest)(queue)
		backendChannel := (<-chan backendDiagnosticResult)(s.backendResults)
		backendStop := (<-chan struct{})(s.backendStop)
		for queueChannel != nil || backendChannel != nil {
			select {
			case <-backendStop:
				backendChannel = nil
				backendStop = nil
			case result := <-backendChannel:
				if !s.shutdown && s.acceptBackendDiagnostics(result) {
					s.publishDiagnostics()
				}
			case item, ok := <-queueChannel:
				if !ok {
					queueChannel = nil
					continue
				}
				if item.ctx.Err() != nil {
					_ = s.respondError(item.request.ID, -32800, "request cancelled")
					activeMu.Lock()
					delete(active, item.key)
					if token, ok := requestProgressToken(item.request); ok {
						delete(activeProgress, progressTokenKey(token))
					}
					activeMu.Unlock()
					item.cancel()
					continue
				}
				handler := s.handleSafely
				if s.handleRequest != nil {
					handler = s.handleRequest
				}
				s.requestContext = item.ctx
				progressToken, hasProgress := requestProgressToken(item.request)
				hasProgress = hasProgress && s.supportsWorkDoneProgress
				if hasProgress {
					_ = s.writeProgress(progressToken, map[string]any{"kind": "begin", "title": "Searching workspace"})
				}
				keepRunning, err := handler(item.request)
				if hasProgress {
					_ = s.writeProgress(progressToken, map[string]any{"kind": "end"})
				}
				if item.ctx.Err() != nil {
					_ = s.writeCancellation(item.request.ID)
				} else if err != nil {
					s.logger.Printf("request %s failed: %v", item.request.Method, err)
					_ = s.respondError(item.request.ID, -32603, err.Error())
				}
				activeMu.Lock()
				delete(active, item.key)
				if hasProgress {
					delete(activeProgress, progressTokenKey(progressToken))
				}
				activeMu.Unlock()
				item.cancel()
				s.requestContext = context.Background()
				if !keepRunning {
					return
				}
			}
		}
	}()
	for {
		payload, err := readMessage(s.reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.stopBackend()
				close(queue)
				<-workerDone
				return nil
			}
			s.stopBackend()
			close(queue)
			<-workerDone
			return err
		}
		var request rpcRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			s.logger.Printf("invalid JSON-RPC message: %v", err)
			continue
		}
		if request.Method == "$/cancelRequest" {
			var params struct {
				ID json.RawMessage `json:"id"`
			}
			if err := decodeParams(request.Params, &params); err == nil && len(params.ID) > 0 {
				activeMu.Lock()
				cancel := active[string(params.ID)]
				activeMu.Unlock()
				if cancel != nil {
					cancel()
				}
			}
			continue
		} else if request.Method == "window/workDoneProgress/cancel" {
			var params struct {
				Token any `json:"token"`
			}
			if err := decodeParams(request.Params, &params); err == nil && params.Token != nil {
				activeMu.Lock()
				cancel := activeProgress[progressTokenKey(params.Token)]
				activeMu.Unlock()
				if cancel != nil {
					cancel()
				}
			}
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		key := string(request.ID)
		if key != "" {
			activeMu.Lock()
			active[key] = cancel
			if token, ok := requestProgressToken(request); ok {
				activeProgress[progressTokenKey(token)] = cancel
			}
			activeMu.Unlock()
		}
		queue <- queuedRequest{request: request, ctx: ctx, cancel: cancel, key: key}
		if request.Method == "exit" {
			s.stopBackend()
			<-workerDone
			if !s.shutdown {
				return errExitWithoutShutdown
			}
			return nil
		}
	}
}

func progressTokenKey(token any) string {
	encoded, err := json.Marshal(token)
	if err != nil {
		return fmt.Sprintf("%T:%v", token, token)
	}
	return string(encoded)
}

func requestProgressToken(request rpcRequest) (any, bool) {
	if request.Method != "workspace/symbol" || len(request.Params) == 0 {
		return nil, false
	}
	var params struct {
		WorkDoneToken any `json:"workDoneToken"`
	}
	if json.Unmarshal(request.Params, &params) != nil || params.WorkDoneToken == nil {
		return nil, false
	}
	return params.WorkDoneToken, true
}

func (s *server) writeProgress(token, value any) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "method": "$/progress", "params": map[string]any{"token": token, "value": value}})
}

func (s *server) writeCancellation(id json.RawMessage) error {
	if len(id) == 0 {
		return nil
	}
	return s.write(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: -32800, Message: "request cancelled"}})
}

func (s *server) cancelled() bool {
	return s.requestContext != nil && s.requestContext.Err() != nil
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
	if s.shutdown && request.Method != "exit" {
		if len(request.ID) > 0 {
			return true, s.respondError(request.ID, -32600, "server has shut down")
		}
		return true, nil
	}
	switch request.Method {
	case "initialize":
		if s.initializeReceived {
			return true, s.respondError(request.ID, -32600, "initialize may only be requested once")
		}
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
		s.supportsWorkDoneProgress = params.Capabilities.Window.WorkDoneProgress
		s.supportsWatchedFileRegistration = params.Capabilities.Workspace.DidChangeWatchedFiles.DynamicRegistration
		s.supportsWorkspaceSymbolRangeResolve = false
		for _, property := range params.Capabilities.Workspace.Symbol.ResolveSupport.Properties {
			if property == "location.range" {
				s.supportsWorkspaceSymbolRangeResolve = true
				break
			}
		}
		s.workspace.setRoots(roots)
		removed := s.workspace.refresh()
		s.scheduleGeneratedDiagnostics()
		workspaceSymbolProvider := map[string]any{"resolveProvider": true}
		if s.supportsWorkDoneProgress {
			workspaceSymbolProvider["workDoneProgress"] = true
		}
		s.initializeReceived = true
		result := map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":                 map[string]any{"openClose": true, "change": 2, "save": map[string]any{"includeText": false}},
				"completionProvider":               map[string]any{"triggerCharacters": []string{".", "@"}, "resolveProvider": true},
				"codeActionProvider":               map[string]any{"codeActionKinds": []string{"quickfix", "refactor.extract", "source.fixAll", "source.organizeImports"}, "resolveProvider": true},
				"semanticTokensProvider":           map[string]any{"legend": map[string]any{"tokenTypes": semanticTokenTypes, "tokenModifiers": semanticTokenModifiers}, "full": map[string]any{"delta": true}, "range": true},
				"diagnosticProvider":               map[string]any{"identifier": "gpp", "interFileDependencies": true, "workspaceDiagnostics": true},
				"executeCommandProvider":           map[string]any{"commands": []string{"gpp.organizeImports", "gpp.getDiagnostics"}},
				"hoverProvider":                    true,
				"definitionProvider":               true,
				"declarationProvider":              true,
				"typeDefinitionProvider":           true,
				"implementationProvider":           true,
				"documentHighlightProvider":        true,
				"foldingRangeProvider":             true,
				"selectionRangeProvider":           true,
				"documentLinkProvider":             map[string]any{"resolveProvider": true},
				"inlayHintProvider":                map[string]any{"resolveProvider": true},
				"codeLensProvider":                 map[string]any{"resolveProvider": true},
				"callHierarchyProvider":            true,
				"typeHierarchyProvider":            true,
				"referencesProvider":               true,
				"renameProvider":                   map[string]any{"prepareProvider": true},
				"documentSymbolProvider":           true,
				"workspaceSymbolProvider":          workspaceSymbolProvider,
				"documentFormattingProvider":       true,
				"documentRangeFormattingProvider":  map[string]any{"rangesSupport": true},
				"documentOnTypeFormattingProvider": map[string]any{"firstTriggerCharacter": "}", "moreTriggerCharacter": []string{"\n"}},
				"inlineCompletionProvider":         map[string]any{},
				"inlineValueProvider":              true,
				"linkedEditingRangeProvider":       true,
				"colorProvider":                    true,
				"monikerProvider":                  true,
				"signatureHelpProvider":            map[string]any{"triggerCharacters": []string{"(", ","}},
				"workspace":                        map[string]any{"workspaceFolders": map[string]any{"supported": true, "changeNotifications": true}},
			},
			"serverInfo": map[string]string{"name": "gpp", "version": "0.1.0"},
		}
		if err := s.respond(request.ID, result); err != nil {
			return true, err
		}
		s.publishDiagnostics(removed...)
		return true, nil
	case "initialized":
		if !s.initializeReceived || s.initializedReceived {
			return true, nil
		}
		s.initializedReceived = true
		if s.supportsWatchedFileRegistration {
			registration := map[string]any{
				"id":     "gpp-watch-source-files",
				"method": "workspace/didChangeWatchedFiles",
				"registerOptions": map[string]any{"watchers": []any{
					map[string]any{"globPattern": "**/*.gpp"},
					map[string]any{"globPattern": "**/*.gpp.tpl"},
					map[string]any{"globPattern": "**/*.go"},
					map[string]any{"globPattern": "**/go.mod"},
					map[string]any{"globPattern": "**/go.sum"},
				}},
			}
			if err := s.write(map[string]any{
				"jsonrpc": "2.0",
				"id":      "gpp-watch-source-files",
				"method":  "client/registerCapability",
				"params":  map[string]any{"registrations": []any{registration}},
			}); err != nil {
				return true, err
			}
		}
		return true, nil
	case "workspace/didChangeWorkspaceFolders":
		var params struct {
			Event struct {
				Added   []workspaceFolder `json:"added"`
				Removed []workspaceFolder `json:"removed"`
			} `json:"event"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		roots := append([]string(nil), s.workspace.roots...)
		for _, removed := range params.Event.Removed {
			path := filepath.Clean(pathFromURI(removed.URI))
			filtered := roots[:0]
			for _, root := range roots {
				if filepath.Clean(root) != path {
					filtered = append(filtered, root)
				}
			}
			roots = filtered
		}
		for _, added := range params.Event.Added {
			if path := pathFromURI(added.URI); path != "" {
				roots = append(roots, path)
			}
		}
		s.workspace.setRoots(roots)
		removed := s.workspace.refresh()
		s.scheduleGeneratedDiagnostics()
		s.publishDiagnostics(removed...)
		return true, nil
	case "shutdown":
		s.shutdown = true
		s.stopBackend()
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
		state := s.ensureFile(params.TextDocument.URI)
		state.Version, state.HasVersion = params.TextDocument.Version, true
		s.open(params.TextDocument.URI, params.TextDocument.Text)
		return true, nil
	case "textDocument/didChange":
		var params didChangeParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		if len(params.ContentChanges) > 0 {
			state := s.ensureFile(params.TextDocument.URI)
			if params.TextDocument.Version != nil && state.HasVersion && *params.TextDocument.Version <= state.Version {
				return true, nil
			}
			text := state.Source
			for _, change := range params.ContentChanges {
				if change.Range == nil {
					text = change.Text
				} else {
					text = applyChange(text, *change.Range, change.Text)
				}
			}
			state.Source, state.Open = text, true
			if params.TextDocument.Version != nil {
				state.Version, state.HasVersion = *params.TextDocument.Version, true
			}
			s.workspace.analyze()
			s.scheduleGeneratedDiagnostics()
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
		s.scheduleGeneratedDiagnostics()
		s.publishDiagnostics()
		return true, nil
	case "workspace/didChangeWatchedFiles":
		var params watchedFilesParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		var removed []string
		for _, change := range params.Changes {
			path := pathFromURI(change.URI)
			if path == "" {
				continue
			}
			state := s.workspace.files[path]
			if change.Type == 3 {
				if state != nil && !state.Open {
					removed = append(removed, state.URI)
					delete(s.workspace.files, path)
					delete(s.workspace.diagnostics, state.URI)
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
		removed = append(removed, s.workspace.refresh()...)
		s.scheduleGeneratedDiagnostics()
		s.publishDiagnostics(removed...)
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
	case "textDocument/semanticTokens/range":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Range        Range                  `json:"range"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.semanticTokensForRange(params.TextDocument.URI, params.Range))
	case "textDocument/semanticTokens/full/delta":
		var params struct {
			TextDocument     textDocumentIdentifier `json:"textDocument"`
			PreviousResultID string                 `json:"previousResultId"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.semanticTokensDelta(params.TextDocument.URI, params.PreviousResultID))
	case "textDocument/diagnostic":
		var params documentDiagnosticParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.pullDocumentDiagnostics(params.TextDocument.URI, params.PreviousResultID))
	case "workspace/diagnostic":
		var params workspaceDiagnosticParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.pullWorkspaceDiagnostics(params.PreviousResultIDs))
	case "workspace/executeCommand":
		var params executeCommandParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		result, err := s.executeCommand(params)
		if err != nil {
			return true, s.respondError(request.ID, -32602, err.Error())
		}
		return true, s.respond(request.ID, result)
	case "textDocument/codeAction":
		var params codeActionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.codeActions(params))
	case "completionItem/resolve":
		var item CompletionItem
		if err := decodeParams(request.Params, &item); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.resolveCompletionItem(item))
	case "codeAction/resolve":
		var action codeAction
		if err := decodeParams(request.Params, &action); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.resolveCodeAction(action))
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
	case "textDocument/declaration":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.definitions(params))
	case "textDocument/typeDefinition":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.typeDefinitions(params))
	case "textDocument/implementation":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.implementations(params))
	case "textDocument/documentHighlight":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.documentHighlights(params))
	case "textDocument/foldingRange":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.foldingRanges(params.TextDocument.URI))
	case "textDocument/selectionRange":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Positions    []Position             `json:"positions"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.selectionRanges(params.TextDocument.URI, params.Positions))
	case "textDocument/documentLink":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.documentLinks(params.TextDocument.URI))
	case "textDocument/inlayHint":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Range        Range                  `json:"range"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.inlayHints(params.TextDocument.URI, params.Range))
	case "textDocument/codeLens":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.codeLenses(params.TextDocument.URI))
	case "codeLens/resolve":
		var lens CodeLens
		if err := decodeParams(request.Params, &lens); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.resolveCodeLens(lens))
	case "documentLink/resolve":
		var link DocumentLink
		if err := decodeParams(request.Params, &link); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.resolveDocumentLink(link))
	case "inlayHint/resolve":
		var hint InlayHint
		if err := decodeParams(request.Params, &hint); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.resolveInlayHint(hint))
	case "textDocument/prepareCallHierarchy":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.prepareCallHierarchy(params))
	case "callHierarchy/incomingCalls":
		var params struct {
			Item CallHierarchyItem `json:"item"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.incomingCalls(params.Item))
	case "callHierarchy/outgoingCalls":
		var params struct {
			Item CallHierarchyItem `json:"item"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.outgoingCalls(params.Item))
	case "textDocument/prepareTypeHierarchy":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.prepareTypeHierarchy(params))
	case "typeHierarchy/supertypes":
		var params struct {
			Item TypeHierarchyItem `json:"item"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.typeSupertypes(params.Item))
	case "typeHierarchy/subtypes":
		var params struct {
			Item TypeHierarchyItem `json:"item"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.typeSubtypes(params.Item))
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
		return true, s.respond(request.ID, s.references(params.TextDocument.URI, params.Position, params.Context.IncludeDeclaration))
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
	case "textDocument/prepareRename":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.prepareRename(params))
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
		return true, s.respond(request.ID, s.workspaceSymbolResults(params.Query))
	case "textDocument/formatting":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Options      any                    `json:"options"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.formatting(params.TextDocument.URI))
	case "textDocument/rangeFormatting":
		var params rangeFormattingParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.formattingInRanges(params.TextDocument.URI, []Range{params.Range}))
	case "textDocument/rangesFormatting":
		var params rangesFormattingParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.formattingInRanges(params.TextDocument.URI, params.Ranges))
	case "textDocument/onTypeFormatting":
		var params onTypeFormattingParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.formattingOnType(params.TextDocument.URI, params.Position, params.Character))
	case "textDocument/inlineCompletion":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.inlineCompletions(params))
	case "textDocument/inlineValue":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Range        Range                  `json:"range"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.inlineValues(params.TextDocument.URI, params.Range))
	case "textDocument/linkedEditingRange":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.linkedEditingRange(params))
	case "textDocument/documentColor":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.documentColors(params.TextDocument.URI))
	case "textDocument/colorPresentation":
		var params struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Color        colorValue             `json:"color"`
			Range        Range                  `json:"range"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.colorPresentations(params.TextDocument.URI, params.Range, params.Color))
	case "textDocument/moniker":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.monikers(params))
	case "textDocument/signatureHelp":
		var params textDocumentPositionParams
		if err := decodeParams(request.Params, &params); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.signatureHelp(params))
	case "workspaceSymbol/resolve":
		var symbol map[string]json.RawMessage
		if err := decodeParams(request.Params, &symbol); err != nil {
			return true, err
		}
		return true, s.respond(request.ID, s.resolveWorkspaceSymbol(symbol))
	default:
		if len(request.ID) > 0 {
			return true, s.respondError(request.ID, -32601, "method not found: "+request.Method)
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
	if len(id) == 0 || s.cancelled() {
		return nil
	}
	resultValue := result
	return s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: &resultValue})
}

func (s *server) respondError(id json.RawMessage, code int, message string) error {
	if len(id) == 0 || s.cancelled() {
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
	s.scheduleGeneratedDiagnostics()
	s.publishDiagnostics()
}

func (s *server) close(uri string) {
	state := s.ensureFile(uri)
	state.Open, state.HasVersion = false, false
	if data, err := os.ReadFile(state.Path); err == nil {
		state.Source = string(data)
	} else {
		delete(s.workspace.files, state.Path)
		delete(s.workspace.diagnostics, state.URI)
		s.workspace.analyze()
		s.scheduleGeneratedDiagnostics()
		s.publishDiagnostics(state.URI)
		return
	}
	s.workspace.analyze()
	s.scheduleGeneratedDiagnostics()
	s.publishDiagnostics()
}

func (s *server) publishDiagnostics(removed ...string) {
	nativeGoDiagnostics := s.nativeGoDiagnostics()
	paths := make([]string, 0, len(s.workspace.files))
	for path := range s.workspace.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		state := s.workspace.files[path]
		if !isWorkspaceSourceFile(state.Path) {
			continue
		}
		diagnostics := append([]Diagnostic(nil), s.workspace.diagnostics[state.URI]...)
		diagnostics = append(diagnostics, s.backendDiagnostics[state.URI]...)
		diagnostics = append(diagnostics, nativeGoDiagnostics[state.URI]...)
		diagnostics = deduplicateDiagnostics(diagnostics)
		if diagnostics == nil {
			diagnostics = []Diagnostic{}
		}
		params := map[string]any{"uri": state.URI, "diagnostics": diagnostics}
		if state.HasVersion {
			params["version"] = state.Version
		}
		if err := s.write(map[string]any{
			"jsonrpc": "2.0",
			"method":  "textDocument/publishDiagnostics",
			"params":  params,
		}); err != nil {
			s.logger.Printf("publish diagnostics: %v", err)
		}
	}
	seen := make(map[string]bool, len(removed))
	for _, uri := range removed {
		if uri == "" || seen[uri] {
			continue
		}
		seen[uri] = true
		if err := s.write(map[string]any{
			"jsonrpc": "2.0",
			"method":  "textDocument/publishDiagnostics",
			"params":  map[string]any{"uri": uri, "diagnostics": []Diagnostic{}},
		}); err != nil {
			s.logger.Printf("clear diagnostics: %v", err)
		}
	}
}

func deduplicateDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	result := make([]Diagnostic, 0, len(diagnostics))
	indices := map[string]int{}
	for _, diagnostic := range diagnostics {
		key := fmt.Sprintf("%d:%d\x00%s", diagnostic.Range.Start.Line, diagnostic.Range.Start.Character, diagnostic.Message)
		if index, exists := indices[key]; exists {
			previous := result[index]
			previousWidth := previous.Range.End.Character - previous.Range.Start.Character
			width := diagnostic.Range.End.Character - diagnostic.Range.Start.Character
			if width > previousWidth || width == previousWidth && diagnostic.Source == "go/types" && previous.Source != "go/types" {
				result[index] = diagnostic
			}
			continue
		}
		indices[key] = len(result)
		result = append(result, diagnostic)
	}
	return result
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
	currentDirectory := ""
	hasCurrentFile := false
	if state := s.workspace.files[pathFromURI(uri)]; state != nil && state.File != nil {
		hasCurrentFile = true
		currentPackage = state.File.Package
		currentDirectory = filepath.Clean(filepath.Dir(state.Path))
	} else if state != nil {
		hasCurrentFile = true
		currentDirectory = filepath.Clean(filepath.Dir(state.Path))
	}
	for _, info := range s.workspace.symbols {
		if info.Symbol.Name != name {
			continue
		}
		if currentPackage != "" && info.Symbol.Package == currentPackage && filepath.Clean(filepath.Dir(info.Path)) == currentDirectory {
			result = append(result, info)
		}
	}
	if len(result) == 0 && currentPackage == "" && hasCurrentFile {
		for _, info := range s.workspace.symbols {
			if info.Symbol.Name == name && filepath.Clean(filepath.Dir(info.Path)) == currentDirectory {
				result = append(result, info)
			}
		}
	}
	if len(result) == 0 && currentPackage == "" && !hasCurrentFile {
		for _, info := range s.workspace.symbols {
			if info.Symbol.Name == name {
				result = append(result, info)
			}
		}
	}
	return result
}

func workspaceScopePath(path string) string {
	return filepath.Clean(filepath.Dir(path))
}

func (s *server) resolveSymbolsAt(uri string, position Position, name string) []symbolInfo {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil || state.File == nil || name == "" {
		return s.findSymbols(name, uri)
	}
	offset := offsetAt(state.Source, position)
	if token, ok := identifierTokenAt(state.File.Tokens, offset); ok {
		index := tokenIndexAt(state.File.Tokens, token.Span.Start)
		if index >= 0 && index+1 < len(state.File.Tokens) && state.File.Tokens[index+1].Text == ":" {
			if packageName, className, classes, found := s.constructorClassAt(state, token.Span.Start); found {
				for _, info := range s.constructorFieldSymbols(packageName, className, classes) {
					if info.Symbol.Name == name && info.Symbol.Kind == compiler.DocField {
						return []symbolInfo{info}
					}
				}
				return nil
			}
		}
		if index > 0 && state.File.Tokens[index-1].Text == "." {
			if selected := s.nativeGoMemberAt(state, name, token.Span.Start); len(selected) > 0 {
				return selected
			}
			receiver := selectorReceiverBefore(state, state.File.Tokens[index-1].Span.Start)
			if receiver == "" {
				receiver = selectorChainBefore(state.Source[:token.Span.Start])
			}
			if packageName, packageDir, ok := s.importedPackageForQualifier(state, receiver); ok {
				var result []symbolInfo
				for _, info := range s.packageSymbols(packageName, packageDir) {
					if info.Symbol.Name == name {
						result = append(result, info)
					}
				}
				return result
			}
			if className := s.classTypeExpression(state, receiver, offset); className != "" {
				if classMembers := s.classMembersForType(state, className, name, true); len(classMembers) > 0 {
					return selectCallArityOverloadAt(state, token.Span.Start, name, classMembers)
				}
			}
			receiverType := s.receiverTypeForExpression(state, receiver, state.File.Tokens[index-1].Span.Start, offset)
			if receiverType == "" {
				receiverType = s.receiverType(state, receiver, offset)
			}
			if receiverType != "" {
				baseType := strings.TrimSpace(receiverType)
				for strings.HasPrefix(baseType, "*") {
					baseType = strings.TrimSpace(strings.TrimPrefix(baseType, "*"))
				}
				if bracket := strings.IndexAny(baseType, "[< "); bracket >= 0 {
					baseType = baseType[:bracket]
				}
				if extensions := s.extensionMembersForType(state, receiverType, name); len(extensions) > 0 {
					selected := selectGenericOverloadAt(state, token.Span.Start, name, extensions)
					return selectCallArityOverloadAt(state, token.Span.Start, name, selected)
				}
				if packageName, packageDir, memberType, found := s.importedTypePackage(state, baseType); found {
					var result []symbolInfo
					for _, info := range s.packageSymbols(packageName, packageDir) {
						if info.Symbol.Parent == memberType && info.Symbol.Name == name {
							result = append(result, info)
						}
					}
					if len(result) == 0 {
						result = s.inheritedMembersForType(packageName, packageDir, memberType, name)
					}
					if len(result) > 0 {
						return result
					}
				}
				members := s.classMembersForType(state, baseType, name, false)
				if len(members) == 0 {
					members = s.symbolsMatching(uri, name, func(symbol compiler.DocSymbol) bool {
						return symbol.Parent == baseType || symbol.Name == baseType && (symbol.Kind == compiler.DocClass || symbol.Kind == compiler.DocEnum || symbol.Kind == compiler.DocType)
					})
				}
				if len(members) > 0 {
					return members
				}
			}
		}
	}
	// At a declaration, prefer the declaration in this exact file. This
	// disambiguates identically named methods declared by different classes.
	for _, info := range s.workspace.symbols {
		if info.Path != state.Path || info.Symbol.Name != name {
			continue
		}
		if symbolDeclarationContains(state, info.Symbol, offset) {
			return []symbolInfo{info}
		}
	}
	return s.findSymbols(name, uri)
}

func (s *server) inheritedMembersForType(packageName, packageDir, typeName, memberName string) []symbolInfo {
	var queue []symbolInfo
	for _, info := range s.packageSymbols(packageName, packageDir) {
		if info.Symbol.Kind == compiler.DocClass && info.Symbol.Name == typeName {
			queue = append(queue, info)
		}
	}
	seenClasses := map[string]bool{}
	seenMembers := map[string]bool{}
	var result []symbolInfo
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		key := semanticSymbolKey(current)
		if seenClasses[key] {
			continue
		}
		seenClasses[key] = true
		for _, parent := range s.parentClasses(current) {
			parentKey := semanticSymbolKey(parent)
			if seenClasses[parentKey] {
				continue
			}
			for _, info := range s.workspace.symbols {
				if info.Symbol.Package != parent.Symbol.Package || info.Symbol.Parent != parent.Symbol.Name || memberName != "" && info.Symbol.Name != memberName {
					continue
				}
				memberKey := semanticSymbolKey(info)
				if !seenMembers[memberKey] {
					seenMembers[memberKey] = true
					result = append(result, info)
				}
			}
			queue = append(queue, parent)
		}
	}
	return result
}

func (s *server) classMembersForType(state *fileState, typeName, memberName string, static bool) []symbolInfo {
	var result []symbolInfo
	seen := map[string]bool{}
	add := func(info symbolInfo) {
		if memberName != "" && info.Symbol.Name != memberName || !isClassMemberKind(info.Symbol.Kind) {
			return
		}
		if info.Symbol.Kind == compiler.DocField && static {
			return
		}
		if info.Symbol.Kind == compiler.DocMethod && s.methodIsStatic(info) != static {
			return
		}
		key := semanticSymbolKey(info)
		if !seen[key] {
			seen[key] = true
			result = append(result, info)
		}
	}
	for _, class := range s.typeSymbolsForName(state, typeName) {
		if class.Symbol.Kind != compiler.DocClass {
			continue
		}
		classDir := filepath.Clean(filepath.Dir(class.Path))
		beforeDirect := len(result)
		for _, info := range s.workspace.symbols {
			if info.Symbol.Package == class.Symbol.Package && info.Symbol.Parent == class.Symbol.Name && filepath.Clean(filepath.Dir(info.Path)) == classDir {
				add(info)
			}
		}
		if memberName != "" && len(result) > beforeDirect {
			continue
		}
		for _, info := range s.inheritedMembersForType(class.Symbol.Package, classDir, class.Symbol.Name, memberName) {
			add(info)
		}
	}
	return result
}

func isClassMemberKind(kind compiler.DocKind) bool {
	return kind == compiler.DocField || kind == compiler.DocMethod
}

func (s *server) methodIsStatic(info symbolInfo) bool {
	file := s.fileForSymbolPath(info.Path)
	if file == nil {
		return false
	}
	for _, declaration := range file.Decls {
		class, ok := declaration.(*compiler.ClassDecl)
		if !ok || class.Name != info.Symbol.Parent {
			continue
		}
		for _, method := range class.Methods {
			if method.Name == info.Symbol.Name {
				return method.IsStatic
			}
		}
	}
	return false
}

func (s *server) fileForSymbolPath(path string) *compiler.File {
	if state := s.workspace.files[filepath.Clean(path)]; state != nil {
		return state.File
	}
	for _, file := range s.workspace.official {
		if file != nil && filepath.Clean(file.SourcePath) == filepath.Clean(path) {
			return file
		}
	}
	return nil
}

func (s *server) extensionMembersForType(state *fileState, receiverType, memberName string) []symbolInfo {
	if state == nil || state.File == nil || receiverType == "" {
		return nil
	}
	receiverIdentity := s.canonicalTypeIdentity(state, receiverType)
	var result []symbolInfo
	for _, info := range s.workspace.symbols {
		if info.Symbol.Kind != compiler.DocExtension || memberName != "" && info.Symbol.Name != memberName || !s.extensionPackageVisible(state, info) {
			continue
		}
		extensionFile := s.fileForSymbolPath(info.Path)
		if extensionFile == nil {
			target := s.canonicalTypeIdentity(state, info.Symbol.Target)
			if target == receiverIdentity || genericExtensionTargetMatches(target, receiverIdentity) {
				result = append(result, info)
			}
			continue
		}
		extensionState := &fileState{Path: info.Path, File: extensionFile}
		target := s.canonicalTypeIdentity(extensionState, info.Symbol.Target)
		if target == receiverIdentity || genericExtensionTargetMatches(target, receiverIdentity) {
			result = append(result, info)
		}
	}
	return result
}

func genericExtensionTargetMatches(target, actual string) bool {
	target = strings.TrimSpace(target)
	actual = strings.TrimSpace(actual)
	if strings.HasPrefix(target, "[]") && strings.HasPrefix(actual, "[]") {
		parameter := strings.TrimSpace(strings.TrimPrefix(target, "[]"))
		return extensionTypeParameterName(parameter) && strings.TrimSpace(strings.TrimPrefix(actual, "[]")) != ""
	}
	if !strings.HasPrefix(target, "map[") || !strings.HasPrefix(actual, "map[") {
		return false
	}
	targetClose := strings.IndexByte(target, ']')
	actualClose := strings.IndexByte(actual, ']')
	if targetClose < 0 || actualClose < 0 || targetClose == len(target)-1 || actualClose == len(actual)-1 {
		return false
	}
	targetKey := strings.TrimSpace(target[len("map["):targetClose])
	targetValue := strings.TrimSpace(target[targetClose+1:])
	actualKey := strings.TrimSpace(actual[len("map["):actualClose])
	actualValue := strings.TrimSpace(actual[actualClose+1:])
	return (extensionTypeParameterName(targetKey) || targetKey == actualKey) &&
		(extensionTypeParameterName(targetValue) || targetValue == actualValue)
}

func extensionTypeParameterName(name string) bool {
	switch name {
	case "T", "K", "V", "E":
		return true
	default:
		return false
	}
}

func (s *server) extensionPackageVisible(state *fileState, extension symbolInfo) bool {
	if state == nil || state.File == nil {
		return false
	}
	if extension.Symbol.Package == "prelude" || extension.Symbol.Package == state.File.Package && filepath.Clean(filepath.Dir(extension.Path)) == filepath.Clean(filepath.Dir(state.Path)) {
		return true
	}
	for _, declaration := range state.File.Imports {
		for _, file := range s.workspace.official {
			if file != nil && file.OfficialPackage == declaration.Path && file.Package == extension.Symbol.Package {
				return true
			}
		}
		packageName, packageDir, found := s.workspacePackageForImport(state, declaration.Path)
		if found && packageName == extension.Symbol.Package && filepath.Clean(filepath.Dir(extension.Path)) == filepath.Clean(packageDir) {
			return true
		}
	}
	return false
}

func (s *server) canonicalTypeIdentity(state *fileState, typeName string) string {
	typeName = strings.TrimSpace(typeName)
	prefix := ""
	for {
		switch {
		case strings.HasPrefix(typeName, "*"):
			prefix += "*"
			typeName = strings.TrimSpace(strings.TrimPrefix(typeName, "*"))
		case strings.HasPrefix(typeName, "[]"):
			prefix += "[]"
			typeName = strings.TrimSpace(strings.TrimPrefix(typeName, "[]"))
		default:
			goto base
		}
	}
base:
	if separator := strings.LastIndexByte(typeName, '.'); separator > 0 {
		qualifier, member := typeName[:separator], typeName[separator+1:]
		if packageName, _, found := s.importedPackageForQualifier(state, qualifier); found && packageName != "" {
			return prefix + packageName + "." + member
		}
	}
	return prefix + typeName
}

func selectGenericOverloadAt(state *fileState, offset int, name string, choices []symbolInfo) []symbolInfo {
	if state == nil || state.File == nil {
		return choices
	}
	index := tokenIndexAt(state.File.Tokens, offset)
	if index < 0 || index+1 >= len(state.File.Tokens) || state.File.Tokens[index+1].Text != "[" {
		return choices
	}
	depth, parens, braces, commas, hasArgument := 0, 0, 0, 0, false
	for cursor := index + 1; cursor < len(state.File.Tokens); cursor++ {
		token := state.File.Tokens[cursor]
		switch token.Text {
		case "[":
			depth++
		case "]":
			depth--
			if depth == 0 {
				if !hasArgument {
					return choices
				}
				argumentCount := commas + 1
				var matched []symbolInfo
				for _, info := range choices {
					if len(genericTypeParameterNames(info.Symbol.Signature, name)) == argumentCount {
						matched = append(matched, info)
					}
				}
				if len(matched) > 0 {
					return matched
				}
				return choices
			}
		case "(":
			parens++
		case ")":
			if parens > 0 {
				parens--
			}
		case "{":
			braces++
		case "}":
			if braces > 0 {
				braces--
			}
		case ",":
			if depth == 1 && parens == 0 && braces == 0 {
				commas++
			}
		default:
			if depth == 1 && parens == 0 && braces == 0 && token.Kind != compiler.TokenNewline {
				hasArgument = true
			}
		}
	}
	return choices
}

func selectCallArityOverloadAt(state *fileState, offset int, name string, choices []symbolInfo) []symbolInfo {
	if len(choices) < 2 {
		return choices
	}
	argumentCount, ok := callArgumentCountAt(state, offset, name)
	if !ok {
		return choices
	}
	matched := make([]symbolInfo, 0, len(choices))
	for _, choice := range choices {
		parameters, _ := callableSignatureParts(choice.Symbol.Signature)
		if len(parameters) == 0 && argumentCount != 0 {
			continue
		}
		required := 0
		for _, parameter := range parameters {
			if !strings.Contains(parameter, "=") {
				required++
			}
		}
		if argumentCount >= required && argumentCount <= len(parameters) {
			matched = append(matched, choice)
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return choices
}

func callArgumentCountAt(state *fileState, offset int, name string) (int, bool) {
	if state == nil || state.File == nil {
		return 0, false
	}
	index := tokenIndexAt(state.File.Tokens, offset)
	if index < 0 || index >= len(state.File.Tokens) || state.File.Tokens[index].Text != name {
		return 0, false
	}
	index++
	if index < len(state.File.Tokens) && state.File.Tokens[index].Text == "[" {
		depth := 0
		for ; index < len(state.File.Tokens); index++ {
			switch state.File.Tokens[index].Text {
			case "[":
				depth++
			case "]":
				depth--
				if depth == 0 {
					index++
					goto openingParen
				}
			}
		}
		return 0, false
	}

openingParen:
	for index < len(state.File.Tokens) && (state.File.Tokens[index].Kind == compiler.TokenNewline || state.File.Tokens[index].Kind == compiler.TokenComment) {
		index++
	}
	if index >= len(state.File.Tokens) || state.File.Tokens[index].Text != "(" {
		return 0, false
	}
	parenDepth, bracketDepth, braceDepth := 0, 0, 0
	argumentCount := 0
	hasArgument := false
	for cursor := index; cursor < len(state.File.Tokens); cursor++ {
		token := state.File.Tokens[cursor]
		if token.Kind == compiler.TokenNewline || token.Kind == compiler.TokenComment {
			continue
		}
		switch token.Text {
		case "(":
			parenDepth++
		case ")":
			parenDepth--
			if parenDepth == 0 {
				if hasArgument {
					argumentCount++
				}
				return argumentCount, true
			}
		case "[":
			bracketDepth++
		case "]":
			if bracketDepth > 0 {
				bracketDepth--
			}
		case "{":
			braceDepth++
		case "}":
			if braceDepth > 0 {
				braceDepth--
			}
		case ",":
			if parenDepth == 1 && bracketDepth == 0 && braceDepth == 0 {
				argumentCount++
				hasArgument = false
				continue
			}
		}
		if parenDepth == 1 && bracketDepth == 0 && braceDepth == 0 && token.Kind != compiler.TokenEOF {
			hasArgument = true
		}
	}
	if hasArgument {
		argumentCount++
	}
	return argumentCount, true
}

func (s *server) nativeGoMemberAt(state *fileState, name string, offset int) []symbolInfo {
	object, fileSet, ok := s.nativeGoSelectionAt(state, name, offset)
	if !ok || object == nil {
		return nil
	}
	kind := compiler.DocKind("")
	switch value := object.(type) {
	case *types.Var:
		if value.IsField() {
			kind = compiler.DocField
		}
	case *types.Func:
		kind = compiler.DocMethod
	}
	if kind == "" {
		return nil
	}
	position := fileSet.Position(object.Pos())
	if !position.IsValid() {
		return nil
	}
	path := filepath.Clean(position.Filename)
	if !filepath.IsAbs(path) {
		if absolute, err := filepath.Abs(path); err == nil {
			path = absolute
		}
	}
	stateAtDeclaration := s.workspace.files[path]
	if stateAtDeclaration == nil {
		return nil
	}
	want := positionAtOffset(stateAtDeclaration.Source, position.Offset)
	for _, info := range s.workspace.symbols {
		if info.Path != path || info.Symbol.Kind != kind || info.Symbol.Name != object.Name() {
			continue
		}
		location, found := s.symbolLocation(info)
		if found && location.Range.Start == want {
			return []symbolInfo{info}
		}
	}
	return nil
}

func (s *server) symbolsMatching(uri, name string, accept func(compiler.DocSymbol) bool) []symbolInfo {
	var result []symbolInfo
	state := s.workspace.files[pathFromURI(uri)]
	scope := ""
	if state != nil && state.File != nil {
		scope = workspaceScopePath(state.Path)
	}
	for _, info := range s.workspace.symbols {
		if info.Symbol.Name == name && accept(info.Symbol) && (scope == "" || workspaceScopePath(info.Path) == scope) {
			result = append(result, info)
		}
	}
	return result
}

func symbolDeclarationContains(state *fileState, symbol compiler.DocSymbol, offset int) bool {
	if state == nil || state.File == nil {
		return false
	}
	for _, declaration := range state.File.Decls {
		switch value := declaration.(type) {
		case *compiler.FunctionDecl:
			if value.Name == symbol.Name && offset >= value.SourceSpan.Start && offset <= value.SourceSpan.End {
				return true
			}
		case *compiler.ClassDecl:
			if value.Name == symbol.Name && offset >= value.SpanValue.Start && offset <= value.SpanValue.End {
				return true
			}
			for _, field := range value.Fields {
				if symbol.Parent == value.Name && field.Name == symbol.Name && offset >= field.SpanValue.Start && offset <= field.SpanValue.End {
					return true
				}
			}
			for _, method := range value.Methods {
				if symbol.Parent == value.Name && method.Name == symbol.Name && offset >= method.NameSpan.Start && offset <= method.NameSpan.End {
					return true
				}
			}
		case *compiler.EnumDecl:
			if value.Name == symbol.Name && offset >= value.SpanValue.Start && offset <= value.SpanValue.End {
				return true
			}
			for _, member := range value.Members {
				if symbol.Parent == value.Name && member.Name == symbol.Name && offset >= value.SpanValue.Start && offset <= value.SpanValue.End {
					return true
				}
			}
		case *compiler.ValueDecl:
			for _, declaredName := range value.Names {
				if declaredName.Text == symbol.Name && offset >= declaredName.Span.Start && offset <= declaredName.Span.End {
					return true
				}
			}
		case *compiler.GoDecl, *compiler.MixedDecl:
			if span, ok := astSymbolNameSpanInDecl(state.File, declaration, symbol); ok && offset >= span.Start && offset <= span.End {
				return true
			}
		}
	}
	return false
}

func (s *server) hover(params textDocumentPositionParams) *hover {
	stateAtPosition := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if stateAtPosition != nil && stateAtPosition.File != nil {
		if importInfo := s.importHoverAt(stateAtPosition, offsetAt(stateAtPosition.Source, params.Position)); importInfo != nil {
			return importInfo
		}
	}
	name, selection := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" {
		state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
		if state == nil || state.File == nil {
			return nil
		}
		offset := offsetAt(state.Source, params.Position)
		if token, ok := identifierTokenAt(state.File.Tokens, offset); ok {
			name, selection = token.Text, rangeForOffsets(state.Source, token.Span.Start, token.Span.End)
		}
		if name == "" {
			for _, token := range state.File.Tokens {
				if (token.Text == "this" || builtinSymbol(token.Text) != nil) && token.Span.Start <= offset && offset <= token.Span.End {
					name, selection = token.Text, rangeForOffsets(state.Source, token.Span.Start, token.Span.End)
					break
				}
			}
		}
		if name == "" {
			return nil
		}
	}
	if stateAtPosition != nil {
		if value := s.nativeGoExternalHover(stateAtPosition, name, offsetAt(stateAtPosition.Source, params.Position), selection); value != nil {
			return value
		}
	}
	if local := s.localHover(params.TextDocument.URI, name, params.Position); local != nil {
		local.Range = &selection
		return local
	}
	if parameter := s.typeParameterHover(stateAtPosition, name, offsetAt(stateAtPosition.Source, params.Position)); parameter != nil {
		parameter.Range = &selection
		return parameter
	}
	offset := offsetAt(stateAtPosition.Source, params.Position)
	if field, ok := s.anonymousStructFieldDeclarationAt(stateAtPosition, name, offset); ok {
		value := localHoverValue("field", name, field.Type, "")
		value.Range = &selection
		return value
	}
	if field, ok := s.returnedStructFieldAt(stateAtPosition, name, offset); ok {
		value := localHoverValue("field", name, field.Type, "")
		value.Range = &selection
		return value
	}
	choices := s.resolveSymbolsAt(params.TextDocument.URI, params.Position, name)
	if len(choices) == 0 {
		if value := builtinHover(name); value != nil {
			value.Range = &selection
			return value
		}
		return nil
	}
	info := choices[0]
	symbol := s.contextualMethodSymbol(stateAtPosition, offset, info.Symbol)
	signature := symbol.Signature
	if signature == "" {
		signature = strings.TrimSpace(info.Symbol.Source)
	}
	if signature == "" {
		signature = info.Symbol.FullName
	}
	value := "**" + hoverSymbolKind(info.Symbol) + "**\n\n```gpp\n" + signature + "\n```"
	if alternatives := s.otherOverloadSignatures(info); len(alternatives) > 0 {
		value += "\n\n**Other overloads**\n"
		for _, alternative := range alternatives {
			value += "\n- `" + alternative + "`"
		}
	}
	documentation := symbol.Doc
	if documentation == "" && symbol.Kind == compiler.DocClass {
		if inherited, parent := s.inheritedClassDocumentation(info); inherited != "" {
			documentation = "**Inherited from `" + parent + "`.**\n\n" + inherited
		}
	}
	if documentation != "" {
		value += "\n\n" + documentation
	}
	if deprecated(symbol) {
		value += "\n\n**Deprecated.**"
	}
	return &hover{Contents: markupContent{Kind: "markdown", Value: value}, Range: &selection}
}

func (s *server) otherOverloadSignatures(info symbolInfo) []string {
	if s.workspace == nil || info.Symbol.Kind != compiler.DocMethod && info.Symbol.Kind != compiler.DocExtension && info.Symbol.Kind != compiler.DocFunction {
		return nil
	}
	seen := map[string]bool{info.Symbol.Signature: true}
	var result []string
	for _, candidate := range s.workspace.symbols {
		if candidate.Path != info.Path || candidate.Symbol.Package != info.Symbol.Package || candidate.Symbol.Kind != info.Symbol.Kind ||
			candidate.Symbol.Parent != info.Symbol.Parent || candidate.Symbol.Name != info.Symbol.Name {
			continue
		}
		signature := strings.TrimSpace(candidate.Symbol.Signature)
		if signature == "" || seen[signature] {
			continue
		}
		seen[signature] = true
		result = append(result, signature)
	}
	return result
}

func (s *server) inheritedClassDocumentation(info symbolInfo) (string, string) {
	queue := s.parentClasses(info)
	seen := map[string]bool{}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		key := semanticSymbolKey(parent)
		if seen[key] {
			continue
		}
		seen[key] = true
		if documentation := strings.TrimSpace(parent.Symbol.Doc); documentation != "" {
			name := parent.Symbol.FullName
			if name == "" {
				name = parent.Symbol.Name
			}
			return documentation, name
		}
		queue = append(queue, s.parentClasses(parent)...)
	}
	return "", ""
}

func (s *server) contextualMethodSymbol(state *fileState, offset int, symbol compiler.DocSymbol) compiler.DocSymbol {
	if state == nil || state.File == nil || symbol.Kind != compiler.DocMethod {
		return symbol
	}
	token, ok := identifierTokenAt(state.File.Tokens, offset)
	if !ok {
		return symbol
	}
	index := tokenIndexAt(state.File.Tokens, token.Span.Start)
	if index < 1 || state.File.Tokens[index-1].Text != "." {
		return symbol
	}
	receiver := selectorReceiverBefore(state, state.File.Tokens[index-1].Span.Start)
	if receiver == "" {
		return symbol
	}
	className := s.classTypeForReceiver(state, receiver, token.Span.Start)
	return contextualizeMethodForClass(symbol, className)
}

func contextualizeMethodForClass(symbol compiler.DocSymbol, className string) compiler.DocSymbol {
	if className == "" || symbol.Kind != compiler.DocMethod {
		return symbol
	}
	symbol.Signature = replaceTypeIdentifier(symbol.Signature, "self", className)
	symbol.Signature = strings.Replace(symbol.Signature, symbol.Parent+"."+symbol.Name, className+"."+symbol.Name, 1)
	return symbol
}

func hoverSymbolKind(symbol compiler.DocSymbol) string {
	switch symbol.Kind {
	case compiler.DocPackage:
		return "package"
	case compiler.DocClass:
		return "class"
	case compiler.DocEnum:
		return "enum"
	case compiler.DocType:
		return "type"
	case compiler.DocFunction:
		return "function"
	case compiler.DocMethod:
		return "method"
	case compiler.DocField:
		return "field"
	case compiler.DocExtension:
		return "extension method"
	case compiler.DocAnnotation:
		return "annotation"
	case compiler.DocTemplate:
		return "template"
	case compiler.DocValue:
		if symbol.Parent != "" {
			return "enum member"
		}
		if strings.HasPrefix(symbol.Signature, "var ") || strings.HasPrefix(symbol.Signature, "let ") {
			return "package variable"
		}
		return "constant"
	default:
		return "symbol"
	}
}

func (s *server) importHoverAt(state *fileState, offset int) *hover {
	if state == nil || state.File == nil {
		return nil
	}
	for _, imported := range state.File.Imports {
		if offset < imported.SpanValue.Start || offset > imported.SpanValue.End {
			continue
		}
		alias := imported.Alias
		if alias == "" {
			alias = packageLeaf(filepath.Base(imported.Path))
		}
		kind, documentation := "import", ""
		if imported.LogicalPackage {
			kind = "Go++ package"
		} else if s.workspace.docIndex != nil {
			packageName := packageLeaf(imported.Path)
			if pkg := s.workspace.docIndex.Packages[packageName]; pkg != nil {
				documentation = pkg.Doc
			}
		}
		value := fmt.Sprintf("**%s** `%s`\n\nImport path: `%s`", kind, alias, imported.Path)
		if documentation != "" {
			value += "\n\n" + documentation
		}
		return &hover{Contents: markupContent{Kind: "markdown", Value: value}, Range: pointerRange(rangeForOffsets(state.Source, imported.SpanValue.Start, imported.SpanValue.End))}
	}
	return nil
}

func pointerRange(value Range) *Range { return &value }

type builtinDescription struct{ kind, signature, documentation string }

func builtinSymbol(name string) *builtinDescription {
	values := map[string]builtinDescription{
		"append":     {"built-in function", "append(slice []T, values ...T) []T", "Appends values to a slice and returns the resulting slice."},
		"cap":        {"built-in function", "cap(value) int", "Returns the capacity of an array, slice, or channel."},
		"clear":      {"built-in function", "clear(value)", "Removes all entries from a map or zeros all elements of a slice."},
		"close":      {"built-in function", "close(channel)", "Closes a channel."},
		"complex":    {"built-in function", "complex(real, imaginary float32|float64) complex64|complex128", "Constructs a complex number from its real and imaginary parts."},
		"copy":       {"built-in function", "copy(dst, src []T) int", "Copies elements between slices and returns the number copied."},
		"delete":     {"built-in function", "delete(map, key)", "Removes a map entry."},
		"imag":       {"built-in function", "imag(complex) float32|float64", "Returns the imaginary part of a complex number."},
		"len":        {"built-in function", "len(value) int", "Returns the length of a string, array, slice, map, or channel."},
		"make":       {"built-in function", "make(type, size ...int) type", "Allocates and initializes a slice, map, or channel."},
		"max":        {"built-in function", "max(values ...T) T", "Returns the largest ordered value."},
		"min":        {"built-in function", "min(values ...T) T", "Returns the smallest ordered value."},
		"new":        {"built-in function", "new(type) *type", "Allocates zeroed storage for a value of the given type."},
		"panic":      {"built-in function", "panic(value any)", "Stops normal execution and begins panicking."},
		"print":      {"built-in function", "print(values ...any)", "Writes values to standard error for debugging."},
		"println":    {"built-in function", "println(values ...any)", "Writes values and a newline to standard error for debugging."},
		"real":       {"built-in function", "real(complex) float32|float64", "Returns the real part of a complex number."},
		"recover":    {"built-in function", "recover() any", "Stops a panic when called from a deferred function."},
		"any":        {"predeclared type", "any", "any is an alias for interface{}."},
		"bool":       {"predeclared type", "bool", "The boolean type."},
		"byte":       {"predeclared type", "byte", "An alias for uint8."},
		"comparable": {"predeclared constraint", "comparable", "The set of types that support == and !=."},
		"complex64":  {"predeclared type", "complex64", "A complex number with float32 components."},
		"complex128": {"predeclared type", "complex128", "A complex number with float64 components."},
		"error":      {"predeclared interface", "error", "The standard error interface with an Error() string method."},
		"float32":    {"predeclared type", "float32", "A 32-bit floating-point type."},
		"float64":    {"predeclared type", "float64", "A 64-bit floating-point type."},
		"int":        {"predeclared type", "int", "The implementation-sized signed integer type."},
		"int8":       {"predeclared type", "int8", "An 8-bit signed integer type."},
		"int16":      {"predeclared type", "int16", "A 16-bit signed integer type."},
		"int32":      {"predeclared type", "int32", "A 32-bit signed integer type."},
		"int64":      {"predeclared type", "int64", "A 64-bit signed integer type."},
		"iota":       {"predeclared constant", "iota untyped int", "The successive integer value used in a const declaration."},
		"rune":       {"predeclared type", "rune", "An alias for int32, commonly used for Unicode code points."},
		"string":     {"predeclared type", "string", "An immutable sequence of bytes, conventionally UTF-8 text."},
		"uint":       {"predeclared type", "uint", "The implementation-sized unsigned integer type."},
		"uint8":      {"predeclared type", "uint8", "An 8-bit unsigned integer type."},
		"uint16":     {"predeclared type", "uint16", "A 16-bit unsigned integer type."},
		"uint32":     {"predeclared type", "uint32", "A 32-bit unsigned integer type."},
		"uint64":     {"predeclared type", "uint64", "A 64-bit unsigned integer type."},
		"uintptr":    {"predeclared type", "uintptr", "An unsigned integer large enough to hold a pointer value."},
		"true":       {"predeclared constant", "true untyped bool = true", "The boolean true value."},
		"false":      {"predeclared constant", "false untyped bool = false", "The boolean false value."},
		"nil":        {"predeclared constant", "nil", "The zero value for pointer, function, interface, map, slice, and channel types."},
	}
	value, ok := values[name]
	if !ok {
		return nil
	}
	return &value
}

func builtinHover(name string) *hover {
	description := builtinSymbol(name)
	if description == nil {
		return nil
	}
	markdown := "**" + description.kind + "**\n\n```go\n" + description.signature + "\n```\n\n" + description.documentation
	return &hover{Contents: markupContent{Kind: "markdown", Value: markdown}}
}

// localHover resolves parameters and simple local declarations from the parsed
// function body. Workspace symbols do not contain local bindings, so keeping
// this lookup scoped to the enclosing callable prevents unrelated same-name
// symbols from being shown in hover.
func (s *server) localHover(uri, name string, position Position) *hover {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil || state.File == nil {
		return nil
	}
	offset := offsetAt(state.Source, position)
	if selectorMemberAt(state, offset) {
		return nil
	}
	type callable struct {
		method       *compiler.Method
		receiverType string
	}
	var callables []callable
	for _, declaration := range state.File.Decls {
		switch value := declaration.(type) {
		case *compiler.FunctionDecl:
			callables = append(callables, callable{method: &value.Method})
		case *compiler.ClassDecl:
			for index := range value.Methods {
				callables = append(callables, callable{method: &value.Methods[index], receiverType: value.Name})
			}
		case *compiler.ExtendDecl:
			receiverType := extensionReceiverType(state.Source, value)
			for index := range value.Methods {
				callables = append(callables, callable{method: &value.Methods[index], receiverType: receiverType})
			}
		case *compiler.MixedDecl:
			for _, function := range value.Functions {
				if function != nil {
					callables = append(callables, callable{method: &function.Method})
				}
			}
		}
	}
	for _, entry := range callables {
		method := entry.method
		if method == nil || offset < method.SpanValue.Start || offset > method.BodySpan.End {
			continue
		}
		if lambda, ok := s.lambdaParameterAt(state, method, name, offset); ok {
			local, hasLocal := s.localBindingInMethod(state, method, name, offset)
			if !hasLocal || local.Declaration.Start < lambda.declaration.Start {
				return localHoverValue("parameter", name, lambda.typeName, "")
			}
		}
		if local := s.localDeclarationHover(state, method, name, offset); local != nil {
			return local
		}
		if binding, ok := s.localBindingInMethod(state, method, name, offset); ok && binding.Kind == "parameter" {
			typeName := binding.Type
			if typeName == "" {
				typeName = "any"
			}
			return localHoverValue("parameter", name, typeName, "")
		}
		if name == "this" && entry.receiverType != "" {
			return localHoverValue("receiver", name, entry.receiverType, "")
		}
	}
	if value := s.nativeGoLocalHover(state, name, offset); value != nil {
		return value
	}
	return nil
}

func (s *server) typeParameterHover(state *fileState, name string, offset int) *hover {
	parameter := s.typeParameterAt(state, name, offset)
	if parameter == nil {
		return nil
	}
	value := "**type parameter** `" + parameter.Name + "`"
	if parameter.Constraint != "" {
		value += "\n\nConstraint: `" + parameter.Constraint + "`"
	}
	return &hover{Contents: markupContent{Kind: "markdown", Value: value}}
}

type typeParameterBinding struct {
	Name        string
	Constraint  string
	Declaration compiler.Span
	Scope       compiler.Span
}

func (s *server) typeParameterAt(state *fileState, name string, offset int) *typeParameterBinding {
	if state == nil || state.File == nil || name == "" {
		return nil
	}
	type candidate struct {
		binding typeParameterBinding
	}
	var candidates []candidate
	add := func(declarationName string, scope compiler.Span, parameters []compiler.TypeParameterNode) {
		if offset < scope.Start || offset > scope.End {
			return
		}
		declarations := markDeclaredTypeParameters(state.File.Tokens, declarationName, scope, parameters, func(compiler.Span, int, int) {})
		span, found := declarations[name]
		if !found {
			return
		}
		constraint := typeParameterConstraint(state.Source, state.File.Tokens, span, scope)
		candidates = append(candidates, candidate{binding: typeParameterBinding{Name: name, Constraint: constraint, Declaration: span, Scope: scope}})
	}
	for _, declaration := range state.File.Decls {
		switch value := declaration.(type) {
		case *compiler.ClassDecl:
			if offset < value.SpanValue.Start || offset > value.SpanValue.End {
				continue
			}
			for index := range value.Methods {
				method := &value.Methods[index]
				add(method.Name, method.SpanValue, method.TypeParamsAST)
			}
			add(value.Name, value.SpanValue, value.TypeParamsAST)
		case *compiler.FunctionDecl:
			add(value.Name, value.SourceSpan, value.Method.TypeParamsAST)
		case *compiler.MixedDecl:
			for _, function := range value.Functions {
				if function != nil {
					add(function.Name, function.SourceSpan, function.Method.TypeParamsAST)
				}
			}
		case *compiler.ExtendDecl:
			for index := range value.Methods {
				method := &value.Methods[index]
				add(method.Name, method.SpanValue, method.TypeParamsAST)
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	// A method-level type parameter shadows one on its containing class.
	selected := candidates[0]
	for _, value := range candidates[1:] {
		if value.binding.Scope.End-value.binding.Scope.Start < selected.binding.Scope.End-selected.binding.Scope.Start {
			selected = value
		}
	}
	return &selected.binding
}

func typeParameterConstraint(source string, tokens []compiler.Token, declaration, bounds compiler.Span) string {
	if declaration.End <= declaration.Start {
		return ""
	}
	declIndex := tokenIndexAt(tokens, declaration.Start)
	if declIndex < 0 {
		return ""
	}
	depth := 0
	for index := declIndex + 1; index < len(tokens) && tokens[index].Span.Start < bounds.End; index++ {
		switch tokens[index].Text {
		case "[", "(", "{":
			depth++
		case "]":
			if depth == 0 {
				return strings.TrimSpace(source[declaration.End:tokens[index].Span.Start])
			}
			depth--
		case ")", "}":
			if depth > 0 {
				depth--
			}
		case ",":
			if depth == 0 {
				return strings.TrimSpace(source[declaration.End:tokens[index].Span.Start])
			}
		}
	}
	return ""
}

type lambdaParameterBinding struct {
	declaration compiler.Span
	scope       compiler.Span
	typeName    string
}

func (s *server) lambdaParameterAt(state *fileState, method *compiler.Method, name string, offset int) (lambdaParameterBinding, bool) {
	lambda, ok := lambdaParameterAt(state, method, name, offset)
	if !ok {
		return lambdaParameterBinding{}, false
	}
	typeOffset := offset
	if typeOffset <= lambda.arrow.Span.Start {
		typeOffset = lambda.arrow.Span.End
	}
	typeName := lambdaElementType(s, state, name, typeOffset)
	if typeName == "" {
		typeName = "inferred"
	}
	return lambdaParameterBinding{declaration: lambda.declaration, scope: lambda.scope, typeName: typeName}, true
}

func extensionReceiverType(source string, extension *compiler.ExtendDecl) string {
	if extension == nil {
		return ""
	}
	span := extension.Span()
	if span.Start < 0 || span.End > len(source) || span.End <= span.Start {
		return ""
	}
	text := source[span.Start:span.End]
	keyword := strings.Index(text, "extend")
	body := strings.Index(text, "{")
	if keyword < 0 || body < keyword+len("extend") {
		return ""
	}
	targets := strings.TrimSpace(text[keyword+len("extend") : body])
	if where := strings.Index(targets, " where "); where >= 0 {
		targets = strings.TrimSpace(targets[:where])
	}
	parts := strings.Split(targets, ",")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return strings.Join(parts, " | ")
}

func localHoverValue(kind, name, typeName, value string) *hover {
	line := "**" + kind + "** `" + name + "`"
	if typeName != "" {
		line += " : `" + typeName + "`"
	}
	if value != "" {
		line += " = `" + value + "`"
	}
	return &hover{Contents: markupContent{Kind: "markdown", Value: line}}
}

func (s *server) localDeclarationHover(state *fileState, method *compiler.Method, name string, offset int) *hover {
	binding, ok := s.localBindingInMethod(state, method, name, offset)
	if !ok || binding.Kind == "parameter" || binding.Kind == "receiver" {
		return nil
	}
	return localHoverValue(binding.Kind, name, binding.Type, binding.Value)
}

type localBinding struct {
	Declaration compiler.Span
	ScopeStart  int
	ScopeEnd    int
	Kind        string
	Type        string
	Value       string
	Method      *compiler.Method
}

func (s *server) localBindingAt(state *fileState, offset int, name string) (localBinding, bool) {
	method, className := enclosingMethod(state, offset)
	if method == nil {
		return localBinding{}, false
	}
	lambda, hasLambda := s.lambdaParameterAt(state, method, name, offset)
	binding, hasBinding := s.localBindingInMethod(state, method, name, offset)
	if hasLambda && (!hasBinding || binding.Declaration.Start < lambda.declaration.Start) {
		return localBinding{
			Declaration: lambda.declaration,
			ScopeStart:  lambda.scope.Start,
			ScopeEnd:    lambda.scope.End,
			Kind:        "parameter",
			Type:        lambda.typeName,
			Method:      method,
		}, true
	}
	if hasBinding {
		if binding.Kind == "receiver" {
			binding.Type = className
		}
		return binding, true
	}
	return localBinding{}, false
}

func enclosingMethod(state *fileState, offset int) (*compiler.Method, string) {
	if state == nil || state.File == nil {
		return nil, ""
	}
	for _, declaration := range state.File.Decls {
		switch value := declaration.(type) {
		case *compiler.FunctionDecl:
			if offset >= value.Method.SpanValue.Start && offset <= value.Method.BodySpan.End {
				return &value.Method, ""
			}
		case *compiler.ClassDecl:
			for index := range value.Methods {
				method := &value.Methods[index]
				if offset >= method.SpanValue.Start && offset <= method.BodySpan.End {
					return method, value.Name
				}
			}
		case *compiler.ExtendDecl:
			receiverType := extensionReceiverType(state.Source, value)
			for index := range value.Methods {
				method := &value.Methods[index]
				if offset >= method.SpanValue.Start && offset <= method.BodySpan.End {
					return method, receiverType
				}
			}
		case *compiler.MixedDecl:
			for _, function := range value.Functions {
				if function != nil && offset >= function.Method.SpanValue.Start && offset <= function.Method.BodySpan.End {
					return &function.Method, ""
				}
			}
		}
	}
	return nil, ""
}

type lexicalBlock struct{ start, end, depth int }

func (s *server) localBindingInMethod(state *fileState, method *compiler.Method, name string, offset int) (localBinding, bool) {
	if state == nil || state.File == nil || method == nil || offset < method.SpanValue.Start || offset > method.BodySpan.End {
		return localBinding{}, false
	}
	if selectorMemberAt(state, offset) {
		return localBinding{}, false
	}
	type candidate struct {
		binding localBinding
		depth   int
	}
	var best *candidate
	blocks := lexicalBlocks(method.BodyAST, method.BodySpan)
	tokens := state.File.Tokens
	for index, token := range tokens {
		if token.Kind != compiler.TokenIdentifier || token.Text != name || token.Span.Start > offset || token.Span.Start < method.BodySpan.Start || token.Span.End > method.BodySpan.End || !localDeclarationToken(tokens, token) {
			continue
		}
		block, ok := declarationBlock(blocks, token.Span.Start)
		if !ok || offset < block.start || offset > block.end {
			continue
		}
		binding := localBinding{Declaration: token.Span, ScopeStart: block.start, ScopeEnd: block.end, Kind: "variable", Type: "inferred", Method: method}
		if index > 0 && tokens[index-1].Text == "const" {
			binding.Kind = "constant"
		}
		s.inferLocalBinding(state, method, index, &binding)
		candidate := candidate{binding: binding, depth: block.depth}
		if best == nil || candidate.depth > best.depth || candidate.depth == best.depth && candidate.binding.Declaration.Start > best.binding.Declaration.Start {
			best = &candidate
		}
	}
	for _, catch := range catchBindings(state, method.BodyAST) {
		inDeclaration := offset >= catch.Declaration.Start && offset <= catch.Declaration.End
		if catch.Name != name || !inDeclaration && (offset < catch.ScopeStart || offset > catch.ScopeEnd) {
			continue
		}
		block, ok := declarationBlock(blocks, catch.ScopeStart)
		if !ok {
			continue
		}
		binding := localBinding{Declaration: catch.Declaration, ScopeStart: catch.ScopeStart, ScopeEnd: catch.ScopeEnd, Kind: "variable", Type: catch.Type, Method: method}
		candidate := candidate{binding: binding, depth: block.depth}
		if best == nil || candidate.depth > best.depth || candidate.depth == best.depth && candidate.binding.Declaration.Start > best.binding.Declaration.Start {
			best = &candidate
		}
	}
	if best != nil {
		return best.binding, true
	}
	for _, parameter := range method.ParameterAST {
		if parameter.Name != name {
			continue
		}
		declaration := parameterNameSpan(tokens, method.ParameterAST, method.ParametersSpan, name)
		if declaration.End <= declaration.Start {
			continue
		}
		typeName := parameterTypeFromTokens(tokens, method.SpanValue, name)
		if typeName == "" && parameter.Type != nil {
			typeName = sourceType(state.Source, parameter.Type.Span())
		}
		if offset < method.BodySpan.Start && (offset < declaration.Start || offset > declaration.End) {
			continue
		}
		return localBinding{Declaration: declaration, ScopeStart: method.BodySpan.Start, ScopeEnd: method.BodySpan.End, Kind: "parameter", Type: typeName, Method: method}, true
	}
	return localBinding{}, false
}

func parameterNameSpan(tokens []compiler.Token, parameters []compiler.ParameterNode, bounds compiler.Span, name string) compiler.Span {
	index := sort.Search(len(tokens), func(index int) bool { return tokens[index].Span.Start >= bounds.Start })
	for _, parameter := range parameters {
		segmentStart := index
		depth := 0
		segmentEnd := index
		for segmentEnd < len(tokens) && tokens[segmentEnd].Span.Start < bounds.End {
			token := tokens[segmentEnd]
			switch token.Text {
			case "[", "(", "{":
				depth++
			case "]", ")", "}":
				if depth > 0 {
					depth--
				}
			case ",":
				if depth == 0 {
					goto segmentFound
				}
			}
			segmentEnd++
		}
	segmentFound:
		if parameter.Name == name {
			for candidate := segmentStart; candidate < segmentEnd; candidate++ {
				if tokens[candidate].Kind != compiler.TokenIdentifier {
					continue
				}
				if tokens[candidate].Text == name {
					return tokens[candidate].Span
				}
				break
			}
		}
		index = segmentEnd + 1
	}
	return compiler.Span{}
}

func selectorMemberAt(state *fileState, offset int) bool {
	if state == nil || state.File == nil {
		return false
	}
	token, ok := identifierTokenAt(state.File.Tokens, offset)
	if !ok {
		return false
	}
	index := tokenIndexAt(state.File.Tokens, token.Span.Start)
	return index > 0 && (state.File.Tokens[index-1].Text == "." || state.File.Tokens[index-1].Text == "?.")
}

type catchLocal struct {
	Name        string
	Declaration compiler.Span
	ScopeStart  int
	ScopeEnd    int
	Type        string
}

func catchBindings(state *fileState, root *compiler.BlockStmt) []catchLocal {
	if state == nil || root == nil {
		return nil
	}
	var result []catchLocal
	var visit func(*compiler.BlockStmt)
	visit = func(block *compiler.BlockStmt) {
		if block == nil {
			return
		}
		for _, statement := range block.Statements {
			switch value := statement.(type) {
			case *compiler.TryStmt:
				visit(value.Body)
				for _, clause := range value.Catches {
					if clause.Binding != "" && clause.Body != nil {
						var declaration compiler.Span
						for _, token := range state.File.Tokens {
							if token.Kind == compiler.TokenIdentifier && token.Text == clause.Binding && token.Span.Start >= clause.SpanValue.Start && token.Span.Start < clause.Body.SpanValue.Start {
								declaration = token.Span
							}
						}
						if declaration.End > declaration.Start {
							typeName := "error"
							if len(clause.Types) > 0 {
								types := make([]string, 0, len(clause.Types))
								for _, caughtType := range clause.Types {
									types = append(types, sourceType(state.Source, caughtType.Span()))
								}
								typeName = strings.Join(types, " | ")
							}
							result = append(result, catchLocal{Name: clause.Binding, Declaration: declaration, ScopeStart: clause.Body.SpanValue.Start, ScopeEnd: clause.Body.SpanValue.End, Type: typeName})
						}
					}
					visit(clause.Body)
				}
				visit(value.Finally)
			case *compiler.IfStmt:
				visit(value.Body)
				visit(value.Else)
				if value.ElseIf != nil {
					visit(value.ElseIf.Body)
					visit(value.ElseIf.Else)
				}
			case *compiler.ForStmt:
				visit(value.Body)
			case *compiler.SwitchStmt:
				visit(value.Body)
			case *compiler.CaseStmt:
				visit(value.Clause.Body)
			case *compiler.TokenStmt:
				visit(value.Body)
				for _, child := range value.Children {
					if nested, ok := child.(*compiler.BlockStmt); ok {
						visit(nested)
					}
				}
			case *compiler.BlockStmt:
				visit(value)
			}
		}
	}
	visit(root)
	return result
}

func (s *server) inferLocalBinding(state *fileState, method *compiler.Method, index int, binding *localBinding) {
	if binding == nil || state == nil || state.File == nil || index < 0 || index >= len(state.File.Tokens) {
		return
	}
	tokens := state.File.Tokens
	if index > 0 && (tokens[index-1].Text == "var" || tokens[index-1].Text == "let" || tokens[index-1].Text == "const") && index+1 < len(tokens) && tokens[index+1].Kind == compiler.TokenIdentifier && tokens[index+1].Text != "=" {
		binding.Type = tokens[index+1].Text
	}
	explicitType, initializer := bindingInitializer(method.BodyAST, binding.Declaration)
	if explicitType != nil {
		binding.Type = sourceType(state.Source, explicitType.Span())
	}
	if initializer != nil {
		if explicitType == nil {
			if inferred := s.expressionType(state, method, initializer, 0); inferred != "" {
				binding.Type = inferred
			}
		}
		if literal, ok := initializer.(*compiler.LiteralExpr); ok && binding.Kind == "constant" {
			binding.Value = literal.Text
		}
	}
	if binding.Type == "" || binding.Type == "inferred" {
		rhs := index + 2
		if index+1 < len(tokens) && tokens[index+1].Text == "," {
			for rhs < len(tokens) && tokens[rhs].Text != ":=" {
				rhs++
			}
			rhs++
		}
		if rhs < len(tokens) && (tokens[index+1].Text == ":=" || tokens[index+1].Text == "=" || tokens[index+1].Text == ",") {
			if inferred := inferredTokenType(tokens, rhs); inferred != "" {
				binding.Type = inferred
			}
			if binding.Value == "" && binding.Kind == "constant" {
				token := tokens[rhs]
				if token.Kind == compiler.TokenString || token.Kind == compiler.TokenRawString || token.Kind == compiler.TokenRune || token.Kind == compiler.TokenNumber || token.Text == "true" || token.Text == "false" {
					binding.Value = token.Text
				}
			}
		}
	}
	if binding.Type == "" {
		binding.Type = "inferred"
	}
}

func bindingInitializer(root *compiler.BlockStmt, declaration compiler.Span) (compiler.TypeNode, compiler.ExprNode) {
	var declaredType compiler.TypeNode
	var initializer compiler.ExprNode
	var visitExpr func(compiler.ExprNode)
	var visitBlock func(*compiler.BlockStmt)
	visitExpr = func(expression compiler.ExprNode) {
		switch value := expression.(type) {
		case *compiler.AssignmentExpr:
			for index, left := range value.Left {
				if name, ok := left.(*compiler.NameExpr); ok && name.SpanValue.Start == declaration.Start {
					if index < len(value.Right) {
						initializer = value.Right[index]
					}
				}
			}
		}
	}
	visitBlock = func(block *compiler.BlockStmt) {
		if block == nil {
			return
		}
		for _, statement := range block.Statements {
			switch value := statement.(type) {
			case *compiler.DeclarationStmt:
				for index, name := range value.Names {
					if name.Span.Start == declaration.Start {
						declaredType = value.Type
						if index < len(value.Values) {
							initializer = value.Values[index]
						}
					}
				}
			case *compiler.AssignmentStmt:
				for index, left := range value.Left {
					if name, ok := left.(*compiler.NameExpr); ok && name.SpanValue.Start == declaration.Start && index < len(value.Right) {
						initializer = value.Right[index]
					}
				}
			case *compiler.IfStmt:
				visitExpr(value.Init)
				visitBlock(value.Body)
				visitBlock(value.Else)
				if value.ElseIf != nil {
					visitBlock(value.ElseIf.Body)
					visitBlock(value.ElseIf.Else)
				}
			case *compiler.ForStmt:
				visitExpr(value.Init)
				visitBlock(value.Body)
			case *compiler.TryStmt:
				visitBlock(value.Body)
				for _, clause := range value.Catches {
					visitBlock(clause.Body)
				}
				visitBlock(value.Finally)
			case *compiler.SwitchStmt:
				visitExpr(value.Init)
				visitBlock(value.Body)
			case *compiler.CaseStmt:
				visitBlock(value.Clause.Body)
			case *compiler.TokenStmt:
				for _, expression := range value.Exprs {
					visitExpr(expression)
				}
				visitBlock(value.Body)
				for _, child := range value.Children {
					if nested, ok := child.(*compiler.BlockStmt); ok {
						visitBlock(nested)
					}
				}
			case *compiler.BlockStmt:
				visitBlock(value)
			}
		}
	}
	visitBlock(root)
	return declaredType, initializer
}

func (s *server) expressionType(state *fileState, method *compiler.Method, expression compiler.ExprNode, depth int) string {
	if state == nil || expression == nil || depth > 64 {
		return ""
	}
	switch value := expression.(type) {
	case *compiler.LiteralExpr:
		return inferredLiteralType(value.Kind, value.Text)
	case *compiler.InterpolatedStringExpr:
		return "string"
	case *compiler.CompositeLiteralExpr:
		if value.Type != nil {
			return sourceType(state.Source, value.Type.Span())
		}
	case *compiler.TypeExpr:
		if value.Type != nil {
			return sourceType(state.Source, value.Type.Span())
		}
	case *compiler.ParenthesizedExpr:
		return s.expressionType(state, method, value.Inner, depth+1)
	case *compiler.UnaryExpr:
		typeName := s.expressionType(state, method, value.Operand, depth+1)
		switch value.Operator {
		case "&":
			if typeName != "" {
				return "*" + typeName
			}
		case "*":
			return strings.TrimPrefix(typeName, "*")
		case "!":
			return "bool"
		}
		return typeName
	case *compiler.BinaryExpr:
		switch value.Operator {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||", "is":
			return "bool"
		}
		return s.expressionType(state, method, value.Left, depth+1)
	case *compiler.NameExpr:
		if method != nil {
			if binding, found := s.localBindingAt(state, value.SpanValue.Start, value.Name); found && binding.Type != "" && binding.Type != "inferred" {
				return binding.Type
			}
			for _, parameter := range method.ParameterAST {
				if parameter.Name == value.Name && parameter.Type != nil {
					return sourceType(state.Source, parameter.Type.Span())
				}
			}
		}
		if info := s.symbolAtName(state, value.Name, value.SpanValue.Start); info != nil {
			if info.Kind == compiler.DocClass || info.Kind == compiler.DocEnum || info.Kind == compiler.DocType {
				return info.Name
			}
			if info.Kind == compiler.DocField {
				return symbolResultType(info.Signature, info.Name)
			}
		}
	case *compiler.SelectorExpr:
		receiverType := s.expressionType(state, method, value.Receiver, depth+1)
		if receiverType == "" {
			return ""
		}
		for _, info := range s.classMembersForType(state, receiverType, value.Name, false) {
			if info.Symbol.Kind == compiler.DocField {
				return symbolResultType(info.Symbol.Signature, info.Symbol.Name)
			}
			if info.Symbol.Kind == compiler.DocMethod {
				return callableResultTypeFor(info, s.classTypeForReceiver(state, sourceExpression(state.Source, value.Receiver), value.SpanValue.Start), nil)
			}
		}
		if field, found := s.returnedStructFieldAt(state, value.Name, value.SpanValue.End-len(value.Name)); found {
			return field.Type
		}
	case *compiler.CallExpr:
		if isMapConstructorCall(value.Callee) {
			valueType := ""
			for _, argument := range value.Arguments {
				entryType := s.expressionType(state, method, argument.Value, depth+1)
				if entryType == "" {
					return "map[string]any"
				}
				if valueType == "" {
					valueType = entryType
				} else if normalizedTypeName(valueType) != normalizedTypeName(entryType) {
					return "map[string]any"
				}
			}
			if valueType != "" {
				return "map[string]" + valueType
			}
			return ""
		}
		return s.callExpressionType(state, method, value, depth+1)
	case *compiler.IndexExpr:
		container := s.expressionType(state, method, value.Receiver, depth+1)
		if strings.HasPrefix(container, "[]") {
			return strings.TrimPrefix(container, "[]")
		}
		if index := strings.LastIndex(container, "]"); strings.HasPrefix(container, "map[") && index >= 0 {
			return container[index+1:]
		}
	case *compiler.SliceExpr:
		container := s.expressionType(state, method, value.Receiver, depth+1)
		if strings.HasPrefix(container, "[") {
			if index := strings.Index(container, "]"); index >= 0 {
				return "[]" + container[index+1:]
			}
		}
		return container
	case *compiler.TypeAssertExpr:
		if value.Type != nil {
			return sourceType(state.Source, value.Type.Span())
		}
	}
	return ""
}

func isMapConstructorCall(expression compiler.ExprNode) bool {
	switch value := expression.(type) {
	case *compiler.NameExpr:
		return value.Name == "map"
	case *compiler.TypeExpr:
		return sourceTypeName(value.Type) == "map"
	}
	return false
}

func sourceTypeName(typeNode compiler.TypeNode) string {
	if named, ok := typeNode.(*compiler.NamedType); ok && len(named.Parts) == 1 {
		return named.Parts[0]
	}
	return ""
}

func normalizedTypeName(name string) string {
	return strings.Join(strings.Fields(name), "")
}

func inferredLiteralType(kind compiler.TokenKind, text string) string {
	switch kind {
	case compiler.TokenString, compiler.TokenRawString:
		return "string"
	case compiler.TokenRune:
		return "rune"
	case compiler.TokenNumber:
		if strings.ContainsAny(text, ".eE") {
			return "float64"
		}
		return "int"
	case compiler.TokenKeyword:
		if text == "true" || text == "false" {
			return "bool"
		}
	}
	return ""
}

func (s *server) callExpressionType(state *fileState, method *compiler.Method, call *compiler.CallExpr, depth int) string {
	if call == nil {
		return ""
	}
	if selector, ok := call.Callee.(*compiler.SelectorExpr); ok {
		memberOffset := selector.SpanValue.End - len(selector.Name)
		if object := s.nativeGoImportedObjectAt(state, selector.Name, memberOffset); object != nil {
			if function, ok := object.(*types.Func); ok {
				if signature, ok := function.Type().(*types.Signature); ok && signature.Results() != nil && signature.Results().Len() > 0 {
					return types.TypeString(signature.Results().At(0).Type(), func(pkg *types.Package) string {
						if pkg == nil {
							return ""
						}
						return pkg.Name()
					})
				}
			}
		}
	}
	if name, ok := call.Callee.(*compiler.NameExpr); ok {
		switch name.Name {
		case "len", "cap", "copy":
			return "int"
		case "make":
			if len(call.Arguments) > 0 {
				return s.expressionType(state, method, call.Arguments[0].Value, depth+1)
			}
		case "new":
			if len(call.Arguments) > 0 {
				if typeName := s.expressionType(state, method, call.Arguments[0].Value, depth+1); typeName != "" {
					return "*" + typeName
				}
			}
		case "append":
			if len(call.Arguments) > 0 {
				return s.expressionType(state, method, call.Arguments[0].Value, depth+1)
			}
		}
	}
	if selector, typeArguments := selectorCallTarget(call.Callee, state.Source); selector != nil {
		memberOffset := selector.SpanValue.End - len(selector.Name)
		if memberOffset >= 0 && memberOffset <= len(state.Source) {
			position := positionAtOffset(state.Source, memberOffset)
			receiverText := sourceExpression(state.Source, selector.Receiver)
			_, _, packageQualifier := s.importedPackageForQualifier(state, receiverText)
			dynamicType := s.expressionType(state, method, selector.Receiver, depth+1)
			if dynamicType == "" {
				dynamicType = s.receiverType(state, receiverText, memberOffset)
			}
			if dynamicType == "" {
				dynamicType = s.classTypeForReceiver(state, receiverText, memberOffset)
			}
			for _, info := range s.resolveSymbolsAt(state.URI, position, selector.Name) {
				if info.Symbol.Kind != compiler.DocMethod && info.Symbol.Kind != compiler.DocExtension && info.Symbol.Kind != compiler.DocFunction {
					continue
				}
				if packageQualifier && (info.Symbol.Kind != compiler.DocFunction || info.Symbol.Parent != "") {
					continue
				}
				if result := callableResultTypeFor(info, dynamicType, typeArguments); result != "" {
					return result
				}
			}
		}
	}
	span := call.Callee.Span()
	for _, token := range state.File.Tokens {
		if token.Kind != compiler.TokenIdentifier || token.Span.Start < span.Start || token.Span.End > span.End {
			continue
		}
		position := positionAtOffset(state.Source, token.Span.Start)
		for _, info := range s.resolveSymbolsAt(state.URI, position, token.Text) {
			if info.Symbol.Kind == compiler.DocClass || info.Symbol.Kind == compiler.DocEnum || info.Symbol.Kind == compiler.DocType {
				if _, qualified := call.Callee.(*compiler.SelectorExpr); qualified && info.Symbol.Package != state.File.Package {
					if span := call.Callee.Span(); span.Start >= 0 && span.End <= len(state.Source) {
						return state.Source[span.Start:span.End]
					}
				}
				if info.Symbol.Kind == compiler.DocClass {
					return "*" + info.Symbol.Name
				}
				return info.Symbol.Name
			}
			if info.Symbol.Kind == compiler.DocFunction || info.Symbol.Kind == compiler.DocMethod {
				if result := s.callableResultType(info); result != "" {
					return result
				}
			}
		}
	}
	return ""
}

func (s *server) callableResultType(info symbolInfo) string {
	return callableResultTypeFor(info, info.Symbol.Parent, nil)
}

func callableResultTypeFor(info symbolInfo, receiverType string, typeArguments []string) string {
	result := firstCallableResult(signatureResultType(info.Symbol.Signature, info.Symbol.Name))
	if result == "" {
		return ""
	}
	if info.Symbol.Kind == compiler.DocExtension && receiverType != "" {
		result = specializeExtensionResultType(info.Symbol.Target, receiverType, result)
	}
	for index, parameter := range genericTypeParameterNames(info.Symbol.Signature, info.Symbol.Name) {
		if index < len(typeArguments) {
			result = replaceTypeIdentifier(result, parameter, typeArguments[index])
		}
	}
	if receiverType != "" {
		result = replaceTypeIdentifier(result, "self", receiverType)
	}
	return result
}

func specializeExtensionResultType(target, actual, result string) string {
	target = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(target), "*"))
	actual = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(actual), "*"))
	if strings.HasPrefix(target, "[]") && strings.HasPrefix(actual, "[]") {
		parameter := strings.TrimSpace(strings.TrimPrefix(target, "[]"))
		if extensionTypeParameterName(parameter) {
			return replaceTypeIdentifier(result, parameter, strings.TrimSpace(strings.TrimPrefix(actual, "[]")))
		}
	}
	if strings.HasPrefix(target, "map[") && strings.HasPrefix(actual, "map[") {
		targetClose := strings.IndexByte(target, ']')
		actualClose := strings.IndexByte(actual, ']')
		if targetClose > 0 && actualClose > 0 {
			bindings := map[string]string{
				strings.TrimSpace(target[len("map["):targetClose]): strings.TrimSpace(actual[len("map["):actualClose]),
				strings.TrimSpace(target[targetClose+1:]):          strings.TrimSpace(actual[actualClose+1:]),
			}
			for parameter, value := range bindings {
				if extensionTypeParameterName(parameter) {
					result = replaceTypeIdentifier(result, parameter, value)
				}
			}
		}
	}
	return result
}

func firstCallableResult(result string) string {
	result = strings.TrimSpace(result)
	if len(result) < 2 || result[0] != '(' || result[len(result)-1] != ')' {
		return result
	}
	inside := result[1 : len(result)-1]
	depth := 0
	for index, char := range inside {
		switch char {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(inside[:index])
			}
		}
	}
	return strings.TrimSpace(inside)
}

func genericTypeParameterNames(signature, name string) []string {
	nameIndex := strings.Index(signature, name)
	if nameIndex < 0 {
		return nil
	}
	index := nameIndex + len(name)
	for index < len(signature) && (signature[index] == ' ' || signature[index] == '\t') {
		index++
	}
	if index >= len(signature) || signature[index] != '[' {
		return nil
	}
	end := matchingDelimiter(signature, index, '[', ']')
	if end < 0 {
		return nil
	}
	var names []string
	for _, parameter := range splitTypeList(signature[index+1 : end]) {
		fields := strings.Fields(strings.TrimSpace(parameter))
		if len(fields) > 0 {
			names = append(names, fields[0])
		}
	}
	return names
}

func splitTypeList(source string) []string {
	var result []string
	start, depth := 0, 0
	for index, char := range source {
		switch char {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, strings.TrimSpace(source[start:index]))
				start = index + 1
			}
		}
	}
	if start < len(source) {
		result = append(result, strings.TrimSpace(source[start:]))
	}
	return result
}

func matchingDelimiter(source string, start int, open, close byte) int {
	depth := 0
	for index := start; index < len(source); index++ {
		switch source[index] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func replaceTypeIdentifier(source, name, replacement string) string {
	if name == "" || replacement == "" {
		return source
	}
	var output strings.Builder
	for index := 0; index < len(source); {
		if !isIdentifierByte(source[index]) {
			output.WriteByte(source[index])
			index++
			continue
		}
		start := index
		for index < len(source) && isIdentifierByte(source[index]) {
			index++
		}
		identifier := source[start:index]
		if identifier == name {
			output.WriteString(replacement)
		} else {
			output.WriteString(identifier)
		}
	}
	return output.String()
}

func selectorCallTarget(callee compiler.ExprNode, source string) (*compiler.SelectorExpr, []string) {
	var typeArguments []string
	for {
		switch value := callee.(type) {
		case *compiler.IndexExpr:
			if value.Index != nil {
				typeArguments = append(typeArguments, sourceExpression(source, value.Index))
			}
			callee = value.Receiver
		case *compiler.IndexListExpr:
			for _, argument := range value.Indices {
				typeArguments = append(typeArguments, sourceExpression(source, argument))
			}
			callee = value.Receiver
		case *compiler.SelectorExpr:
			return value, typeArguments
		default:
			return nil, nil
		}
	}
}

func sourceExpression(source string, expression compiler.ExprNode) string {
	if expression == nil {
		return ""
	}
	span := expression.Span()
	if span.Start < 0 || span.End > len(source) || span.Start > span.End {
		return ""
	}
	return strings.TrimSpace(source[span.Start:span.End])
}

func (s *server) classTypeForReceiver(state *fileState, receiver string, offset int) string {
	for _, info := range s.typeSymbolsForName(state, receiver) {
		if info.Symbol.Kind == compiler.DocClass {
			return info.Symbol.Name
		}
	}
	typeName := s.receiverType(state, receiver, offset)
	for _, info := range s.typeSymbolsForName(state, typeName) {
		if info.Symbol.Kind == compiler.DocClass {
			return info.Symbol.Name
		}
	}
	return ""
}

func (s *server) classTypeExpression(state *fileState, expression string, offset int) string {
	if state == nil || state.File == nil || expression == "" {
		return ""
	}
	if !strings.Contains(expression, ".") {
		if _, local := s.localBindingAt(state, offset, expression); local {
			return ""
		}
	}
	for _, info := range s.typeSymbolsForName(state, expression) {
		if info.Symbol.Kind == compiler.DocClass {
			return info.Symbol.Name
		}
	}
	return ""
}

func signatureResultType(signature, name string) string {
	nameIndex := strings.Index(signature, name)
	if nameIndex < 0 {
		return ""
	}
	open := strings.IndexByte(signature[nameIndex+len(name):], '(')
	if open < 0 {
		return ""
	}
	open += nameIndex + len(name)
	depth, close := 0, -1
	for index := open; index < len(signature); index++ {
		switch signature[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				close = index
			}
		}
		if close >= 0 {
			break
		}
	}
	if close < 0 {
		return ""
	}
	return strings.TrimSpace(signature[close+1:])
}

func symbolResultType(signature, name string) string {
	if index := strings.Index(signature, name); index >= 0 {
		return strings.TrimSpace(signature[index+len(name):])
	}
	return ""
}

func (s *server) symbolAtName(state *fileState, name string, offset int) *compiler.DocSymbol {
	position := positionAtOffset(state.Source, offset)
	for _, info := range s.resolveSymbolsAt(state.URI, position, name) {
		if info.Symbol.Name == name {
			symbol := info.Symbol
			return &symbol
		}
	}
	return nil
}

func lexicalBlocks(root *compiler.BlockStmt, fallback compiler.Span) []lexicalBlock {
	if root == nil {
		return []lexicalBlock{{start: fallback.Start, end: fallback.End, depth: 0}}
	}
	var result []lexicalBlock
	var visit func(*compiler.BlockStmt, int)
	visit = func(block *compiler.BlockStmt, depth int) {
		if block == nil {
			return
		}
		result = append(result, lexicalBlock{start: block.SpanValue.Start, end: block.SpanValue.End, depth: depth})
		for _, statement := range block.Statements {
			switch value := statement.(type) {
			case *compiler.IfStmt:
				visit(value.Body, depth+1)
				visit(value.Else, depth+1)
				if value.ElseIf != nil {
					visit(value.ElseIf.Body, depth+1)
					visit(value.ElseIf.Else, depth+1)
				}
			case *compiler.ForStmt:
				visit(value.Body, depth+1)
			case *compiler.TryStmt:
				visit(value.Body, depth+1)
				for _, clause := range value.Catches {
					visit(clause.Body, depth+1)
				}
				visit(value.Finally, depth+1)
			case *compiler.SwitchStmt:
				visit(value.Body, depth+1)
			case *compiler.CaseStmt:
				visit(value.Clause.Body, depth+1)
			case *compiler.TokenStmt:
				visit(value.Body, depth+1)
				for _, child := range value.Children {
					if nested, ok := child.(*compiler.BlockStmt); ok {
						visit(nested, depth+1)
					}
				}
			case *compiler.BlockStmt:
				visit(value, depth+1)
			}
		}
	}
	visit(root, 0)
	return result
}

func declarationBlock(blocks []lexicalBlock, offset int) (lexicalBlock, bool) {
	var best lexicalBlock
	found := false
	for _, block := range blocks {
		if offset < block.start || offset > block.end {
			continue
		}
		if !found || block.depth > best.depth || block.depth == best.depth && block.end-block.start < best.end-best.start {
			best, found = block, true
		}
	}
	return best, found
}

func shortDeclarationAhead(tokens []compiler.Token, index int) bool {
	for cursor := index + 1; cursor < len(tokens) && cursor < index+8; cursor++ {
		if tokens[cursor].Text == ":=" {
			return true
		}
		if tokens[cursor].Text != "," && tokens[cursor].Kind != compiler.TokenIdentifier {
			return false
		}
	}
	return false
}

func (s *server) documentHighlights(params textDocumentPositionParams) []DocumentHighlight {
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if state == nil || state.File == nil {
		return nil
	}
	var result []DocumentHighlight
	for _, location := range s.references(params.TextDocument.URI, params.Position, true) {
		if location.URI != state.URI {
			continue
		}
		kind := 2
		offset := offsetAt(state.Source, location.Range.Start)
		if tokenIndex := tokenIndexAt(state.File.Tokens, offset); tokenIndex >= 0 && tokenIndex+1 < len(state.File.Tokens) {
			next := state.File.Tokens[tokenIndex+1].Text
			if next == ":=" || next == "=" {
				kind = 3
			}
		}
		result = append(result, DocumentHighlight{Range: location.Range, Kind: kind})
	}
	return result
}

func (s *server) foldingRanges(uri string) []FoldingRange {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil || state.File == nil {
		return nil
	}
	var result []FoldingRange
	var braces []compiler.Token
	for _, token := range state.File.Tokens {
		if token.Kind == compiler.TokenComment && strings.Contains(token.Text, "\n") {
			start, end := positionAtOffset(state.Source, token.Span.Start), positionAtOffset(state.Source, token.Span.End)
			if end.Line > start.Line {
				result = append(result, FoldingRange{StartLine: start.Line, EndLine: end.Line, Kind: "comment"})
			}
		}
		if token.Text == "{" {
			braces = append(braces, token)
		}
		if token.Text == "}" && len(braces) > 0 {
			open := braces[len(braces)-1]
			braces = braces[:len(braces)-1]
			start, end := positionAtOffset(state.Source, open.Span.Start), positionAtOffset(state.Source, token.Span.End)
			if end.Line > start.Line {
				result = append(result, FoldingRange{StartLine: start.Line, StartCharacter: start.Character, EndLine: end.Line, EndCharacter: end.Character})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].StartLine == result[j].StartLine {
			return result[i].EndLine > result[j].EndLine
		}
		return result[i].StartLine < result[j].StartLine
	})
	return result
}

func (s *server) selectionRanges(uri string, positions []Position) []SelectionRange {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil {
		return nil
	}
	result := make([]SelectionRange, 0, len(positions))
	for _, position := range positions {
		offset := offsetAt(state.Source, position)
		inner := rangeForOffsets(state.Source, offset, offset)
		if state.File != nil {
			if token, ok := identifierTokenAt(state.File.Tokens, offset); ok {
				inner = rangeForOffsets(state.Source, token.Span.Start, token.Span.End)
			}
		}
		parents := []Range{inner}
		if state.File != nil {
			candidates := syntaxSelectionRanges(state.File, state.Source, offset)
			sort.SliceStable(candidates, func(i, j int) bool {
				left := offsetAt(state.Source, candidates[i].End) - offsetAt(state.Source, candidates[i].Start)
				right := offsetAt(state.Source, candidates[j].End) - offsetAt(state.Source, candidates[j].Start)
				return left < right
			})
			for _, candidate := range candidates {
				current := parents[len(parents)-1]
				if candidate == current || !rangeContainsRange(candidate, current) {
					continue
				}
				parents = append(parents, candidate)
			}
		}
		lineStart := offset
		for lineStart > 0 && state.Source[lineStart-1] != '\n' {
			lineStart--
		}
		lineEnd := offset
		for lineEnd < len(state.Source) && state.Source[lineEnd] != '\n' {
			lineEnd++
		}
		line := rangeForOffsets(state.Source, lineStart, lineEnd)
		whole := rangeForOffsets(state.Source, 0, len(state.Source))
		if rangeContainsRange(line, parents[len(parents)-1]) && line != parents[len(parents)-1] {
			parents = append(parents, line)
		}
		if whole != parents[len(parents)-1] {
			parents = append(parents, whole)
		}
		var chain *SelectionRange
		for index := len(parents) - 1; index >= 0; index-- {
			chain = &SelectionRange{Range: parents[index], Parent: chain}
		}
		result = append(result, *chain)
	}
	return result
}

func rangeContainsRange(outer, inner Range) bool {
	return (outer.Start.Line < inner.Start.Line || outer.Start.Line == inner.Start.Line && outer.Start.Character <= inner.Start.Character) &&
		(outer.End.Line > inner.End.Line || outer.End.Line == inner.End.Line && outer.End.Character >= inner.End.Character)
}

func (s *server) documentLinks(uri string) []DocumentLink {
	return s.documentLinksWithTargets(uri, false)
}

func (s *server) documentLinksWithTargets(uri string, resolve bool) []DocumentLink {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil || state.File == nil {
		return nil
	}
	scope := workspacePathScope(s.workspace, state.Path)
	paths := make([]string, 0, len(s.workspace.files))
	for path := range s.workspace.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var result []DocumentLink
	for _, imported := range state.File.Imports {
		packageName, packageDir := "", ""
		if imported.LogicalPackage {
			packageName = imported.Path
		} else {
			var found bool
			packageName, packageDir, found = s.workspacePackageForImport(state, imported.Path)
			if !found {
				continue
			}
		}
		for _, path := range paths {
			target := s.workspace.files[path]
			if target.File == nil || target.File.Package != packageName {
				continue
			}
			if packageDir != "" && filepath.Clean(filepath.Dir(target.Path)) != filepath.Clean(packageDir) {
				continue
			}
			if imported.LogicalPackage && scope != "" && workspacePathScope(s.workspace, target.Path) != scope {
				continue
			}
			linkRange := rangeForOffsets(state.Source, imported.SpanValue.Start, imported.SpanValue.End)
			link := DocumentLink{Range: linkRange, Data: documentLinkResolveData{URI: uri, Range: linkRange}}
			if resolve {
				link.Target = target.URI
				link.Tooltip = "Package " + packageName
			}
			result = append(result, link)
			break
		}
	}
	return result
}

func (s *server) resolveDocumentLink(link DocumentLink) DocumentLink {
	if link.Data == nil {
		return link
	}
	encoded, err := json.Marshal(link.Data)
	if err != nil {
		return link
	}
	var data documentLinkResolveData
	if json.Unmarshal(encoded, &data) != nil || data.URI == "" {
		return link
	}
	for _, candidate := range s.documentLinksWithTargets(data.URI, true) {
		if candidate.Range == data.Range {
			candidate.Data = link.Data
			return candidate
		}
	}
	return link
}

func workspacePathScope(workspace *workspaceState, path string) string {
	if path == "" {
		return ""
	}
	if root, _ := moduleInfoForPath(path); root != "" {
		return filepath.Clean(root)
	}
	if workspace == nil {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	best := ""
	for _, root := range workspace.roots {
		root, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(root, absolute)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			continue
		}
		if len(root) > len(best) {
			best = root
		}
	}
	return best
}

func (s *server) inlayHints(uri string, requested Range) []InlayHint {
	state := s.workspace.files[pathFromURI(uri)]
	if state == nil || state.File == nil {
		return []InlayHint{}
	}
	start, end := offsetAt(state.Source, requested.Start), offsetAt(state.Source, requested.End)
	result := s.nativeGoInlayHints(state, start, end)
	tokens := state.File.Tokens
	for index := 0; index+2 < len(tokens); index++ {
		name, assign := tokens[index], tokens[index+1]
		shortDeclaration := assign.Text == ":=" || assign.Text == "," && shortDeclarationAhead(tokens, index)
		if s.cancelled() {
			return []InlayHint{}
		}
		if name.Kind != compiler.TokenIdentifier || !shortDeclaration || name.Span.Start < start || name.Span.End > end {
			continue
		}
		method, _ := enclosingMethod(state, name.Span.Start)
		if method == nil {
			continue
		}
		_, initializer := bindingInitializer(method.BodyAST, name.Span)
		typeName := s.expressionType(state, method, initializer, 0)
		if typeName == "" {
			rhs := index + 2
			if assign.Text == "," {
				for rhs < len(tokens) && tokens[rhs].Text != ":=" {
					rhs++
				}
				rhs++
			}
			typeName = inferredTokenType(tokens, rhs)
		}
		if typeName == "" {
			continue
		}
		result = append(result, InlayHint{Position: rangeForOffsets(state.Source, name.Span.End, name.Span.End).Start, Label: ": " + typeName, Kind: 1, PaddingLeft: true})
	}
	type hintKey struct {
		Position Position
		Label    string
	}
	seen := make(map[hintKey]bool, len(result))
	unique := result[:0]
	for _, hint := range result {
		key := hintKey{Position: hint.Position, Label: hint.Label}
		if seen[key] {
			continue
		}
		seen[key] = true
		hint.Data = inlayHintResolveData{URI: uri, Position: hint.Position, Label: hint.Label}
		unique = append(unique, hint)
	}
	return unique
}

func (s *server) resolveInlayHint(hint InlayHint) InlayHint {
	if hint.Data == nil {
		return hint
	}
	encoded, err := json.Marshal(hint.Data)
	if err != nil {
		return hint
	}
	var data inlayHintResolveData
	if json.Unmarshal(encoded, &data) != nil || data.URI == "" {
		return hint
	}
	state := s.workspace.files[pathFromURI(data.URI)]
	if state == nil {
		return hint
	}
	line := sourceLine(state.Source, data.Position.Line)
	lineRange := Range{Start: Position{Line: data.Position.Line}, End: Position{Line: data.Position.Line, Character: utf16Length(line)}}
	for _, candidate := range s.inlayHints(data.URI, lineRange) {
		if candidate.Position == data.Position && candidate.Label == data.Label {
			typeName := strings.TrimSpace(strings.TrimPrefix(data.Label, ":"))
			candidate.Tooltip = fmt.Sprintf("Inferred %s from `%s`", typeName, strings.TrimSpace(line))
			candidate.Data = hint.Data
			return candidate
		}
	}
	return hint
}

func insideCallable(file *compiler.File, offset int) bool {
	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *compiler.FunctionDecl:
			if offset >= value.Method.BodySpan.Start && offset <= value.Method.BodySpan.End {
				return true
			}
		case *compiler.ClassDecl:
			for _, method := range value.Methods {
				if offset >= method.BodySpan.Start && offset <= method.BodySpan.End {
					return true
				}
			}
		}
	}
	return false
}

func inferredTokenType(tokens []compiler.Token, rhsIndex int) string {
	if rhsIndex < 0 || rhsIndex >= len(tokens) {
		return ""
	}
	rhs := tokens[rhsIndex]
	switch rhs.Kind {
	case compiler.TokenString, compiler.TokenRawString:
		return "string"
	case compiler.TokenRune:
		return "rune"
	case compiler.TokenNumber:
		if strings.ContainsAny(rhs.Text, ".eE") {
			return "float64"
		}
		return "int"
	case compiler.TokenKeyword:
		if rhs.Text == "true" || rhs.Text == "false" {
			return "bool"
		}
	case compiler.TokenIdentifier:
		if rhsIndex+1 < len(tokens) && tokens[rhsIndex+1].Text == "(" {
			return rhs.Text
		}
	}
	return ""
}

func (s *server) codeLenses(uri string) []CodeLens {
	var result []CodeLens
	for _, info := range s.workspace.symbols {
		if info.Path == "" || uriForPath(info.Path) != uri {
			continue
		}
		switch info.Symbol.Kind {
		case compiler.DocClass, compiler.DocEnum, compiler.DocFunction, compiler.DocMethod, compiler.DocExtension:
		default:
			continue
		}
		location, ok := s.symbolLocation(info)
		if !ok {
			continue
		}
		result = append(result, CodeLens{
			Range: Range{Start: location.Range.Start, End: location.Range.Start},
			Data:  codeLensResolveData{URI: uri, FullName: info.Symbol.FullName, Path: info.Path},
		})
	}
	return result
}

func (s *server) resolveCodeLens(lens CodeLens) CodeLens {
	if lens.Data == nil {
		return lens
	}
	encoded, err := json.Marshal(lens.Data)
	if err != nil {
		return lens
	}
	var data codeLensResolveData
	if json.Unmarshal(encoded, &data) != nil || data.URI == "" || data.FullName == "" {
		return lens
	}
	for _, info := range s.workspace.symbols {
		if info.Symbol.FullName != data.FullName || filepath.Clean(info.Path) != filepath.Clean(data.Path) {
			continue
		}
		location, ok := s.symbolLocation(info)
		if !ok {
			return lens
		}
		references := s.references(data.URI, location.Range.Start, false)
		lens.Range = Range{Start: location.Range.Start, End: location.Range.Start}
		lens.Command = &Command{
			Title: fmt.Sprintf("%d references", len(references)), Command: "gpp.showReferences",
			Arguments: []any{map[string]any{"uri": data.URI, "position": location.Range.Start, "locations": references}},
		}
		return lens
	}
	return lens
}

func (s *server) definitions(params textDocumentPositionParams) []Location {
	name, _ := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" {
		return nil
	}
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if state != nil && state.File != nil {
		offset := offsetAt(state.Source, params.Position)
		if location := s.nativeGoImportedDefinition(state, name, offset); location != nil {
			return []Location{*location}
		}
		if binding, ok := s.localBindingAt(state, offset, name); ok {
			return []Location{{URI: state.URI, Range: rangeForOffsets(state.Source, binding.Declaration.Start, binding.Declaration.End)}}
		}
		if parameter := s.typeParameterAt(state, name, offset); parameter != nil {
			return []Location{{URI: state.URI, Range: rangeForOffsets(state.Source, parameter.Declaration.Start, parameter.Declaration.End)}}
		}
		if location, ok := s.importAliasDefinition(state, name, offset); ok {
			return []Location{location}
		}
	}
	result := []Location{}
	for _, info := range s.resolveSymbolsAt(params.TextDocument.URI, params.Position, name) {
		if location, ok := s.symbolLocation(info); ok {
			result = append(result, location)
		}
	}
	return result
}

func (s *server) importAliasDefinition(state *fileState, name string, offset int) (Location, bool) {
	if state == nil || state.File == nil || name == "" {
		return Location{}, false
	}
	token, hasToken := identifierTokenAt(state.File.Tokens, offset)
	index := -1
	if hasToken {
		index = tokenIndexAt(state.File.Tokens, token.Span.Start)
	}
	for _, imported := range state.File.Imports {
		alias := imported.Alias
		if alias == "" {
			if imported.LogicalPackage {
				alias = packageLeaf(imported.Path)
			} else if packageName, _, found := s.workspacePackageForImport(state, imported.Path); found {
				alias = packageName
			} else {
				alias = packageLeaf(imported.Path)
			}
		}
		inImportDeclaration := hasToken && token.Span.Start >= imported.SpanValue.Start && token.Span.End <= imported.SpanValue.End
		isQualifierUse := hasToken && index+1 < len(state.File.Tokens) && state.File.Tokens[index+1].Text == "."
		if alias == name && (inImportDeclaration || isQualifierUse) && imported.SpanValue.End > imported.SpanValue.Start {
			return Location{URI: state.URI, Range: rangeForOffsets(state.Source, imported.SpanValue.Start, imported.SpanValue.End)}, true
		}
	}
	return Location{}, false
}

func (s *server) typeDefinitions(params textDocumentPositionParams) []Location {
	name, _ := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" {
		return nil
	}
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	typeName := name
	if state != nil && state.File != nil {
		offset := offsetAt(state.Source, params.Position)
		if token, ok := identifierTokenAt(state.File.Tokens, offset); ok {
			index := tokenIndexAt(state.File.Tokens, token.Span.Start)
			if index > 0 && state.File.Tokens[index-1].Text == "." {
				qualifier := strings.TrimSpace(selectorReceiverBefore(state, state.File.Tokens[index-1].Span.Start))
				qualifier = strings.TrimLeft(qualifier, "&*")
				qualifier = strings.TrimSpace(qualifier)
				if packageName, packageDir, found := s.importedPackageForQualifier(state, qualifier); found {
					var importedTypes []Location
					for _, symbol := range s.packageSymbols(packageName, packageDir) {
						if symbol.Symbol.Name == name && (symbol.Symbol.Kind == compiler.DocClass || symbol.Symbol.Kind == compiler.DocEnum || symbol.Symbol.Kind == compiler.DocType) {
							if location, ok := s.symbolLocation(symbol); ok {
								importedTypes = append(importedTypes, location)
							}
						}
					}
					if len(importedTypes) > 0 {
						return uniqueLocations(importedTypes)
					}
				}
			}
		}
		if binding, ok := s.localBindingAt(state, offset, name); ok {
			typeName = binding.Type
		} else if parameter := s.typeParameterAt(state, name, offset); parameter != nil {
			return []Location{{URI: state.URI, Range: rangeForOffsets(state.Source, parameter.Declaration.Start, parameter.Declaration.End)}}
		} else if local := s.localHover(params.TextDocument.URI, name, params.Position); local != nil {
			parts := strings.Split(local.Contents.Value, "`")
			if len(parts) >= 4 {
				typeName = parts[3]
			}
		} else {
			var resolvedTypes []Location
			for _, symbol := range s.resolveSymbolsAt(params.TextDocument.URI, params.Position, name) {
				if symbol.Symbol.Kind == compiler.DocClass || symbol.Symbol.Kind == compiler.DocEnum || symbol.Symbol.Kind == compiler.DocType {
					if location, ok := s.symbolLocation(symbol); ok {
						resolvedTypes = append(resolvedTypes, location)
					}
					continue
				}
				if symbol.Symbol.Kind == compiler.DocField {
					typeName = strings.TrimSpace(strings.TrimPrefix(symbol.Symbol.Signature, symbol.Symbol.Name))
					continue
				}
			}
			if len(resolvedTypes) > 0 {
				return uniqueLocations(resolvedTypes)
			}
		}
	}
	result := []Location{}
	for _, info := range s.typeSymbolsForName(state, typeName) {
		if location, ok := s.symbolLocation(info); ok {
			result = append(result, location)
		}
	}
	return result
}

func (s *server) typeSymbolsForName(state *fileState, typeName string) []symbolInfo {
	typeName = strings.TrimSpace(typeName)
	for strings.HasPrefix(typeName, "*") {
		typeName = strings.TrimSpace(strings.TrimPrefix(typeName, "*"))
	}
	if strings.HasPrefix(typeName, "map[") {
		if close := strings.IndexByte(typeName, ']'); close >= 0 {
			typeName = strings.TrimSpace(typeName[close+1:])
		}
	} else if strings.HasPrefix(typeName, "[") {
		if close := strings.IndexByte(typeName, ']'); close >= 0 {
			typeName = strings.TrimSpace(typeName[close+1:])
		}
	}
	if bracket := strings.IndexAny(typeName, "[< "); bracket >= 0 {
		typeName = typeName[:bracket]
	}
	packageName, packageDir := "", ""
	if dot := strings.LastIndexByte(typeName, '.'); dot >= 0 {
		qualifier := typeName[:dot]
		typeName = typeName[dot+1:]
		if state != nil {
			if importedName, importedDir, found := s.importedPackageForQualifier(state, qualifier); found {
				packageName, packageDir = importedName, importedDir
				if packageName == "" && packageDir == "" {
					return nil
				}
			} else {
				packageName = qualifier
			}
		} else {
			packageName = qualifier
		}
	}
	var matches []symbolInfo
	for _, info := range s.workspace.symbols {
		if info.Symbol.Name != typeName || info.Symbol.Kind != compiler.DocClass && info.Symbol.Kind != compiler.DocEnum && info.Symbol.Kind != compiler.DocType {
			continue
		}
		if packageName != "" && info.Symbol.Package != packageName {
			continue
		}
		if packageDir != "" && filepath.Clean(filepath.Dir(info.Path)) != filepath.Clean(packageDir) {
			continue
		}
		matches = append(matches, info)
	}
	if packageName == "" && state != nil && state.File != nil {
		var local []symbolInfo
		for _, info := range matches {
			if info.Symbol.Package == state.File.Package && filepath.Clean(filepath.Dir(info.Path)) == filepath.Clean(filepath.Dir(state.Path)) {
				local = append(local, info)
			}
		}
		if len(local) > 0 {
			return local
		}
	}
	return matches
}

func (s *server) implementations(params textDocumentPositionParams) []Location {
	name, _ := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" {
		return nil
	}
	choices := s.resolveSymbolsAt(params.TextDocument.URI, params.Position, name)
	if len(choices) == 0 {
		return nil
	}
	var result []Location
	for _, target := range choices {
		if target.Symbol.Kind == compiler.DocType {
			result = append(result, s.goInterfaceImplementations(target)...)
			continue
		}
		if target.Symbol.Kind == compiler.DocClass {
			for _, candidate := range s.workspace.symbols {
				if candidate.Symbol.Kind == compiler.DocClass && s.classIsSubtype(candidate, target, map[string]bool{}) {
					if location, ok := s.symbolLocation(candidate); ok {
						result = append(result, location)
					}
				}
			}
			continue
		}
		if target.Symbol.Kind != compiler.DocMethod {
			continue
		}
		if owner, ok := s.goInterfaceOwner(target); ok {
			result = append(result, s.goInterfaceMethodImplementations(target, owner)...)
			continue
		}
		targetOwner, ok := s.classInfoForMember(target)
		if !ok {
			continue
		}
		for _, candidate := range s.workspace.symbols {
			if candidate.Symbol.Kind != compiler.DocMethod || candidate.Symbol.Name != target.Symbol.Name {
				continue
			}
			if owner, ok := s.classInfoForMember(candidate); ok && s.classIsSubtype(owner, targetOwner, map[string]bool{}) {
				if location, ok := s.symbolLocation(candidate); ok {
					result = append(result, location)
				}
			}
		}
	}
	return uniqueLocations(result)
}

func (s *server) classInfoForMember(member symbolInfo) (symbolInfo, bool) {
	for _, info := range s.workspace.symbols {
		if info.Symbol.Package == member.Symbol.Package && info.Symbol.Kind == compiler.DocClass && info.Symbol.Name == member.Symbol.Parent && filepath.Clean(filepath.Dir(info.Path)) == filepath.Clean(filepath.Dir(member.Path)) {
			return info, true
		}
	}
	return symbolInfo{}, false
}

func uniqueLocations(locations []Location) []Location {
	seen := map[Location]bool{}
	result := make([]Location, 0, len(locations))
	for _, location := range locations {
		if !seen[location] {
			seen[location] = true
			result = append(result, location)
		}
	}
	return result
}

func (s *server) references(uri string, position Position, includeDeclaration bool) []Location {
	name, _ := s.symbolsAt(uri, position)
	if name == "" {
		return nil
	}
	if state := s.workspace.files[pathFromURI(uri)]; state != nil && state.File != nil {
		if binding, ok := s.localBindingAt(state, offsetAt(state.Source, position), name); ok {
			var local []Location
			for _, token := range state.File.Tokens {
				if s.cancelled() {
					return nil
				}
				if token.Kind != compiler.TokenIdentifier || token.Text != name {
					continue
				}
				if token.Span.Start == binding.Declaration.Start {
					if includeDeclaration {
						local = append(local, Location{URI: state.URI, Range: rangeForOffsets(state.Source, token.Span.Start, token.Span.End)})
					}
					continue
				}
				if token.Span.Start < binding.ScopeStart || token.Span.End > binding.ScopeEnd {
					continue
				}
				at := token.Span.Start
				occurrence, ok := s.localBindingAt(state, at, name)
				if !ok || occurrence.Declaration.Start != binding.Declaration.Start {
					continue
				}
				if !includeDeclaration && token.Span.Start == binding.Declaration.Start {
					continue
				}
				local = append(local, Location{URI: state.URI, Range: rangeForOffsets(state.Source, token.Span.Start, token.Span.End)})
			}
			return sortLocations(local)
		}
		if parameter := s.typeParameterAt(state, name, offsetAt(state.Source, position)); parameter != nil {
			var local []Location
			for _, token := range state.File.Tokens {
				if s.cancelled() {
					return nil
				}
				if token.Kind != compiler.TokenIdentifier || token.Text != name || token.Span.Start < parameter.Scope.Start || token.Span.End > parameter.Scope.End {
					continue
				}
				if token.Span.Start == parameter.Declaration.Start {
					if includeDeclaration {
						local = append(local, Location{URI: state.URI, Range: rangeForOffsets(state.Source, token.Span.Start, token.Span.End)})
					}
					continue
				}
				if s.isNonGenericDeclaration(state, name, token.Span.Start) {
					continue
				}
				if _, shadowed := s.localBindingAt(state, token.Span.Start, name); shadowed {
					continue
				}
				resolved := s.typeParameterAt(state, name, token.Span.Start)
				if resolved == nil || resolved.Declaration.Start != parameter.Declaration.Start {
					continue
				}
				local = append(local, Location{URI: state.URI, Range: rangeForOffsets(state.Source, token.Span.Start, token.Span.End)})
			}
			return sortLocations(local)
		}
	}
	choices := s.resolveSymbolsAt(uri, position, name)
	declarations := map[Location]bool{}
	packages := map[string]bool{}
	selected := map[string]bool{}
	for _, choice := range choices {
		packages[choice.Symbol.Package] = true
		selected[semanticSymbolKey(choice)] = true
		if location, ok := s.symbolLocation(choice); ok {
			declarations[location] = true
		}
	}
	if len(packages) == 0 {
		if state := s.workspace.files[pathFromURI(uri)]; state != nil && state.File != nil {
			packages[state.File.Package] = true
		}
	}
	result := []Location{}
	for _, state := range s.workspace.files {
		if s.cancelled() {
			return nil
		}
		if !isWorkspaceSourceFile(state.Path) {
			continue
		}
		if state.File == nil || len(selected) == 0 && len(packages) > 0 && !packages[state.File.Package] {
			continue
		}
		for _, span := range identifierSpans(state.File, state.Source, name) {
			if s.cancelled() {
				return nil
			}
			location := Location{URI: state.URI, Range: span}
			if _, localBinding := s.localBindingAt(state, offsetAt(state.Source, span.Start), name); localBinding {
				continue
			}
			if len(selected) > 0 {
				at := positionAtOffset(state.Source, offsetAt(state.Source, span.Start))
				resolved := s.resolveSymbolsAt(state.URI, at, name)
				matches := false
				for _, candidate := range resolved {
					if selected[semanticSymbolKey(candidate)] {
						matches = true
						break
					}
				}
				if !matches {
					continue
				}
			}
			if includeDeclaration || !declarations[location] {
				result = append(result, location)
			}
		}
	}
	return sortLocations(result)
}

func sortLocations(locations []Location) []Location {
	sort.SliceStable(locations, func(i, j int) bool {
		if locations[i].URI != locations[j].URI {
			return locations[i].URI < locations[j].URI
		}
		left, right := locations[i].Range, locations[j].Range
		if left.Start.Line != right.Start.Line {
			return left.Start.Line < right.Start.Line
		}
		if left.Start.Character != right.Start.Character {
			return left.Start.Character < right.Start.Character
		}
		if left.End.Line != right.End.Line {
			return left.End.Line < right.End.Line
		}
		return left.End.Character < right.End.Character
	})
	return locations
}

func semanticSymbolKey(info symbolInfo) string {
	symbol := info.Symbol
	return filepath.Clean(info.Path) + "\x00" + symbol.Package + "\x00" + symbol.FullName + "\x00" + string(symbol.Kind) + "\x00" + symbol.Signature
}

func (s *server) isNonGenericDeclaration(state *fileState, name string, offset int) bool {
	if state == nil || state.File == nil {
		return false
	}
	for _, declaration := range state.File.Decls {
		switch value := declaration.(type) {
		case *compiler.ClassDecl:
			if value.Name == name {
				for _, token := range state.File.Tokens {
					if token.Span.Start >= value.SpanValue.Start && token.Span.Start < value.SpanValue.End && token.Kind == compiler.TokenIdentifier && token.Text == name {
						if tokenIndexAt(state.File.Tokens, token.Span.Start) > 0 && state.File.Tokens[tokenIndexAt(state.File.Tokens, token.Span.Start)-1].Text == "class" && token.Span.Start == offset {
							return true
						}
						break
					}
				}
			}
			for _, field := range value.Fields {
				if field.Name == name && firstIdentifierInSpan(state.File.Tokens, field.SpanValue).Start == offset {
					return true
				}
			}
			for _, method := range value.Methods {
				if method.Name == name && method.NameSpan.Start == offset {
					return true
				}
			}
		case *compiler.EnumDecl:
			if value.Name == name {
				for _, token := range state.File.Tokens {
					if token.Span.Start >= value.SpanValue.Start && token.Span.Start < value.SpanValue.End && token.Kind == compiler.TokenIdentifier && token.Text == name {
						index := tokenIndexAt(state.File.Tokens, token.Span.Start)
						if index > 0 && state.File.Tokens[index-1].Text == "enum" && token.Span.Start == offset {
							return true
						}
						break
					}
				}
			}
			for _, member := range value.Members {
				if member.Name == name && member.NameSpan.Start == offset {
					return true
				}
			}
		case *compiler.FunctionDecl:
			if value.Name == name && value.Method.NameSpan.Start == offset {
				return true
			}
		case *compiler.MixedDecl:
			for _, function := range value.Functions {
				if function != nil && function.Name == name && function.Method.NameSpan.Start == offset {
					return true
				}
			}
		case *compiler.ExtendDecl:
			for _, method := range value.Methods {
				if method.Name == name && method.NameSpan.Start == offset {
					return true
				}
			}
		}
	}
	return false
}

func localDeclarationToken(tokens []compiler.Token, token compiler.Token) bool {
	index := tokenIndexAt(tokens, token.Span.Start)
	if index < 0 {
		return false
	}
	if index > 0 && (tokens[index-1].Text == "var" || tokens[index-1].Text == "let" || tokens[index-1].Text == "const") {
		return true
	}
	return index+1 < len(tokens) && (tokens[index+1].Text == ":=" || tokens[index+1].Text == "," && shortDeclarationAhead(tokens, index))
}

func (s *server) prepareRename(params textDocumentPositionParams) *PrepareRenameResult {
	name, selection := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" || name == "this" {
		return nil
	}
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if state == nil || state.File == nil {
		return nil
	}
	offset := offsetAt(state.Source, params.Position)
	_, hasLocal := s.localBindingAt(state, offset, name)
	hasTypeParameter := s.typeParameterAt(state, name, offset) != nil
	symbols := s.resolveSymbolsAt(params.TextDocument.URI, params.Position, name)
	if !hasLocal && !hasTypeParameter && len(symbols) == 0 {
		return nil
	}
	return &PrepareRenameResult{Range: selection, Placeholder: name}
}

func (s *server) rename(uri string, position Position, newName string) (workspaceEdit, error) {
	if !validIdentifier(newName) {
		return workspaceEdit{}, fmt.Errorf("invalid identifier %q", newName)
	}
	if s.prepareRename(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: uri}, Position: position}) == nil {
		return workspaceEdit{}, errors.New("no renameable symbol at position")
	}
	name, _ := s.symbolsAt(uri, position)
	if name == "" {
		return workspaceEdit{}, errors.New("no symbol at position")
	}
	changes := map[string][]TextEdit{}
	for _, location := range s.references(uri, position, true) {
		changes[location.URI] = append(changes[location.URI], TextEdit{Range: location.Range, NewText: newName})
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
		if s.symbolPath(symbol) != state.Path {
			continue
		}
		result = append(result, s.documentSymbol(symbol))
	}
	return result
}

func (s *server) documentSymbol(symbol compiler.DocSymbol) DocumentSymbol {
	info := symbolInfo{Symbol: symbol, Path: s.symbolPath(symbol)}
	rangeValue := Range{}
	selectionRange := Range{}
	if location, ok := s.symbolLocation(info); ok {
		selectionRange = location.Range
		rangeValue = location.Range
		if state := s.workspace.files[info.Path]; state != nil && state.File != nil {
			if span, ok := astSymbolDeclarationSpan(state.File, symbol); ok {
				rangeValue = rangeForOffsets(state.Source, span.Start, span.End)
			}
		}
	}
	result := DocumentSymbol{Name: symbol.Name, Detail: symbol.Signature, Kind: lspSymbolKind(symbol.Kind), Deprecated: deprecated(symbol), Range: rangeValue, SelectionRange: selectionRange}
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

func astSymbolDeclarationSpan(file *compiler.File, symbol compiler.DocSymbol) (compiler.Span, bool) {
	if file == nil {
		return compiler.Span{}, false
	}
	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *compiler.ClassDecl:
			if symbol.Kind == compiler.DocClass && value.Name == symbol.Name {
				return value.SpanValue, true
			}
			if symbol.Parent != value.Name {
				continue
			}
			for _, field := range value.Fields {
				if symbol.Kind == compiler.DocField && field.Name == symbol.Name {
					return field.Span(), true
				}
			}
			for _, method := range value.Methods {
				if method.Name == symbol.Name && (symbol.Kind == compiler.DocMethod || symbol.Kind == compiler.DocExtension) {
					return method.Span(), true
				}
			}
		case *compiler.ExtendDecl:
			if symbol.Kind == compiler.DocExtension {
				for _, method := range value.Methods {
					if method.Name == symbol.Name {
						return method.Span(), true
					}
				}
			}
		case *compiler.FunctionDecl:
			if symbol.Kind == compiler.DocFunction && value.Name == symbol.Name {
				return value.SourceSpan, true
			}
		case *compiler.MixedDecl:
			if symbol.Kind == compiler.DocFunction {
				for _, function := range value.Functions {
					if function != nil && function.Name == symbol.Name {
						return function.SourceSpan, true
					}
				}
			}
			if span, ok := goASTSpecificDeclarationSpan(file.Source, value.SourceSpan, value.GoASTFileSet, value.GoASTDecls, symbol); ok {
				return span, true
			}
			if _, ok := goDeclarationName(value.GoASTDecls, symbol); ok {
				return value.SourceSpan, true
			}
		case *compiler.EnumDecl:
			if symbol.Kind == compiler.DocEnum && value.Name == symbol.Name {
				return value.SpanValue, true
			}
			if symbol.Kind == compiler.DocValue && symbol.Parent == value.Name {
				for _, member := range value.Members {
					if member.Name == symbol.Name && member.NameSpan.End > member.NameSpan.Start {
						return member.NameSpan, true
					}
				}
			}
		case *compiler.AnnotationDecl:
			if symbol.Kind == compiler.DocAnnotation && value.Name == symbol.Name {
				return value.SpanValue, true
			}
		case *compiler.TemplateDecl:
			if symbol.Kind == compiler.DocTemplate && value.Name == symbol.Name {
				return value.SpanValue, true
			}
		case *compiler.ValueDecl:
			if symbol.Kind == compiler.DocValue {
				return value.Span(), true
			}
		case *compiler.GoDecl:
			if span, ok := goASTSpecificDeclarationSpan(file.Source, value.SourceSpan, value.FileSet, value.Declarations, symbol); ok {
				return span, true
			}
			if _, ok := goDeclarationName(value.Declarations, symbol); ok {
				return value.Span(), true
			}
		}
	}
	return compiler.Span{}, false
}

func (s *server) workspaceSymbols(query string) []SymbolInformation {
	query = strings.ToLower(query)
	result := []SymbolInformation{}
	for _, info := range s.workspace.symbols {
		if s.cancelled() {
			return nil
		}
		if info.Path == "" || (query != "" && !strings.Contains(strings.ToLower(info.Symbol.FullName), query)) {
			continue
		}
		location, ok := s.symbolLocation(info)
		if !ok {
			continue
		}
		result = append(result, SymbolInformation{Name: info.Symbol.Name, Kind: lspSymbolKind(info.Symbol.Kind), Deprecated: deprecated(info.Symbol), Location: location, ContainerName: info.Symbol.Parent, Data: workspaceSymbolResolveData{FullName: info.Symbol.FullName, Path: info.Path}})
	}
	return result
}

func (s *server) workspaceSymbolResults(query string) []any {
	symbols := s.workspaceSymbols(query)
	if !s.supportsWorkspaceSymbolRangeResolve {
		result := make([]any, len(symbols))
		for index := range symbols {
			result[index] = symbols[index]
		}
		return result
	}
	result := make([]any, 0, len(symbols))
	for _, symbol := range symbols {
		result = append(result, map[string]any{
			"name": symbol.Name, "kind": symbol.Kind, "location": map[string]any{"uri": symbol.Location.URI},
			"containerName": symbol.ContainerName, "deprecated": symbol.Deprecated, "data": symbol.Data,
		})
	}
	return result
}

func (s *server) resolveWorkspaceSymbol(symbol map[string]json.RawMessage) map[string]json.RawMessage {
	var data workspaceSymbolResolveData
	dataJSON, ok := symbol["data"]
	if !ok {
		return symbol
	}
	if json.Unmarshal(dataJSON, &data) != nil || data.FullName == "" {
		return symbol
	}
	for _, info := range s.workspace.symbols {
		if info.Symbol.FullName != data.FullName || data.Path != "" && filepath.Clean(info.Path) != filepath.Clean(data.Path) {
			continue
		}
		location, ok := s.symbolLocation(info)
		if !ok {
			return symbol
		}
		encodedLocation, err := json.Marshal(location)
		if err != nil {
			return symbol
		}
		symbol["location"] = encodedLocation
		return symbol
	}
	return symbol
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
	if state == nil || state.File == nil {
		return nil
	}
	offset := offsetAt(state.Source, params.Position)
	openIndex, activeParameter := activeCallArgument(state.File.Tokens, offset)
	if openIndex < 0 {
		return nil
	}
	nameIndex := signatureCalleeTokenIndex(state.File.Tokens, openIndex)
	if nameIndex < 0 {
		return nil
	}
	nameToken := state.File.Tokens[nameIndex]
	if nameToken.Kind != compiler.TokenIdentifier {
		return nil
	}
	name := nameToken.Text
	if name == "" {
		return nil
	}
	position := positionAtOffset(state.Source, nameToken.Span.Start)
	choices := s.resolveSymbolsAt(params.TextDocument.URI, position, name)
	if len(choices) == 0 {
		if builtin := builtinSymbol(name); builtin != nil {
			return &SignatureHelp{Signatures: []SignatureInformation{{Label: builtin.signature, Documentation: builtin.documentation, Parameters: signatureParameters(builtin.signature)}}, ActiveParameter: activeParameter}
		}
		return nil
	}
	result := &SignatureHelp{}
	for _, choice := range choices {
		if choice.Symbol.Signature == "" {
			continue
		}
		result.Signatures = append(result.Signatures, SignatureInformation{Label: choice.Symbol.Signature, Documentation: choice.Symbol.Doc, Parameters: signatureParameters(choice.Symbol.Signature)})
	}
	if len(result.Signatures) == 0 {
		return nil
	}
	result.ActiveParameter = activeParameter
	return result
}

func signatureCalleeTokenIndex(tokens []compiler.Token, openIndex int) int {
	index := openIndex - 1
	if index < 0 || index >= len(tokens) {
		return -1
	}
	if tokens[index].Text != "]" {
		return index
	}
	depth := 0
	for cursor := index; cursor >= 0; cursor-- {
		switch tokens[cursor].Text {
		case "]":
			depth++
		case "[":
			depth--
			if depth == 0 {
				return cursor - 1
			}
		}
	}
	return -1
}

func activeCallArgument(tokens []compiler.Token, offset int) (int, int) {
	type delimiter struct {
		text  string
		index int
	}
	var stack []delimiter
	for index, token := range tokens {
		if token.Span.Start >= offset {
			break
		}
		switch token.Text {
		case "(", "[", "{":
			stack = append(stack, delimiter{text: token.Text, index: index})
		case ")", "]", "}":
			if len(stack) == 0 {
				continue
			}
			want := map[string]string{")": "(", "]": "[", "}": "{"}[token.Text]
			if stack[len(stack)-1].text == want {
				stack = stack[:len(stack)-1]
			}
		}
	}
	openIndex := -1
	for index := len(stack) - 1; index >= 0; index-- {
		if stack[index].text == "(" {
			openIndex = stack[index].index
			break
		}
	}
	if openIndex < 0 {
		return -1, 0
	}
	paren, bracket, brace, commas := 0, 0, 0, 0
	for index := openIndex + 1; index < len(tokens) && tokens[index].Span.Start < offset; index++ {
		switch tokens[index].Text {
		case "(":
			paren++
		case ")":
			if paren > 0 {
				paren--
			}
		case "[":
			bracket++
		case "]":
			if bracket > 0 {
				bracket--
			}
		case "{":
			brace++
		case "}":
			if brace > 0 {
				brace--
			}
		case ",":
			if paren == 0 && bracket == 0 && brace == 0 {
				commas++
			}
		}
	}
	return openIndex, commas
}

func signatureParameters(signature string) []ParameterInformation {
	open := strings.Index(signature, "(")
	if open < 0 {
		return nil
	}
	depth, close := 0, -1
	for index := open; index < len(signature); index++ {
		switch signature[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				close = index
				index = len(signature)
			}
		}
	}
	if close < 0 || close == open+1 {
		return nil
	}
	inside := signature[open+1 : close]
	var result []ParameterInformation
	start, paren, bracket, brace := 0, 0, 0, 0
	for index := 0; index <= len(inside); index++ {
		if index < len(inside) {
			switch inside[index] {
			case '(':
				paren++
			case ')':
				paren--
			case '[':
				bracket++
			case ']':
				bracket--
			case '{':
				brace++
			case '}':
				brace--
			}
		}
		if index == len(inside) || inside[index] == ',' && paren == 0 && bracket == 0 && brace == 0 {
			parameter := strings.TrimSpace(inside[start:index])
			if parameter != "" {
				result = append(result, ParameterInformation{Label: parameter})
			}
			start = index + 1
		}
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
		for _, file := range s.workspace.official {
			if file == nil || filepath.Clean(file.SourcePath) != filepath.Clean(info.Path) {
				continue
			}
			if span, ok := astSymbolNameSpan(file, info.Symbol); ok {
				return Location{URI: uriForPath(info.Path), Range: rangeForOffsets(file.Source, span.Start, span.End)}, true
			}
			line, start, end := findDeclaration(file.Source, info.Symbol)
			return Location{URI: uriForPath(info.Path), Range: Range{Start: Position{Line: line, Character: start}, End: Position{Line: line, Character: end}}}, true
		}
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
			for _, name := range value.Names {
				if name.Text == symbol.Name {
					return name.Span, true
				}
			}
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
			for _, member := range value.Members {
				if member.Name == symbol.Name && member.NameSpan.End > member.NameSpan.Start {
					return member.NameSpan, true
				}
			}
		}
	case *compiler.GoDecl:
		if span, ok := goASTSpecificDeclarationSpan(file.Source, declSpan, value.FileSet, value.Declarations, symbol); ok {
			return span, true
		}
		if pos, ok := goDeclarationName(value.Declarations, symbol); ok {
			return goASTNameSpan(file.Source, declSpan, value.FileSet, pos, symbol.Name)
		}
	case *compiler.MixedDecl:
		for _, function := range value.Functions {
			if function != nil && symbol.Kind == compiler.DocFunction && function.Name == symbol.Name {
				return function.Method.NameSpan, function.Method.NameSpan.End > function.Method.NameSpan.Start
			}
		}
		if span, ok := goASTSpecificDeclarationSpan(file.Source, declSpan, value.GoASTFileSet, value.GoASTDecls, symbol); ok {
			return span, true
		}
		if pos, ok := goDeclarationName(value.GoASTDecls, symbol); ok {
			return goASTNameSpan(file.Source, declSpan, value.GoASTFileSet, pos, symbol.Name)
		}
	}
	return compiler.Span{}, false
}

func goDeclarationName(declarations []ast.Decl, symbol compiler.DocSymbol) (gotoken.Pos, bool) {
	for _, declaration := range declarations {
		switch value := declaration.(type) {
		case *ast.GenDecl:
			for _, specification := range value.Specs {
				switch spec := specification.(type) {
				case *ast.TypeSpec:
					if symbol.Kind == compiler.DocType && spec.Name.Name == symbol.Name {
						return spec.Name.Pos(), true
					}
					if symbol.Kind == compiler.DocField && spec.Name.Name == symbol.Parent {
						if structure, ok := spec.Type.(*ast.StructType); ok {
							for _, field := range structure.Fields.List {
								for _, fieldName := range lspGoStructFieldNames(field) {
									if fieldName.Name == symbol.Name {
										return fieldName.Pos(), true
									}
								}
							}
						}
					}
					if symbol.Kind == compiler.DocMethod && spec.Name.Name == symbol.Parent {
						if interfaceType, ok := spec.Type.(*ast.InterfaceType); ok {
							for _, method := range interfaceType.Methods.List {
								for _, name := range method.Names {
									if name.Name == symbol.Name {
										return name.Pos(), true
									}
								}
							}
						}
					}
				case *ast.ValueSpec:
					if symbol.Kind == compiler.DocValue {
						for _, name := range spec.Names {
							if name.Name == symbol.Name {
								return name.Pos(), true
							}
						}
					}
				}
			}
		case *ast.FuncDecl:
			if value.Name.Name != symbol.Name {
				continue
			}
			if symbol.Kind == compiler.DocFunction && value.Recv == nil {
				return value.Name.Pos(), true
			}
			if symbol.Kind == compiler.DocMethod && value.Recv != nil && lspGoReceiverTypeName(value.Recv) == symbol.Parent {
				return value.Name.Pos(), true
			}
		}
	}
	return gotoken.NoPos, false
}

func goASTSpecificDeclarationSpan(source string, declaration compiler.Span, fileSet *gotoken.FileSet, declarations []ast.Decl, symbol compiler.DocSymbol) (compiler.Span, bool) {
	if fileSet == nil {
		return compiler.Span{}, false
	}
	for _, raw := range declarations {
		switch value := raw.(type) {
		case *ast.GenDecl:
			for _, rawSpec := range value.Specs {
				switch spec := rawSpec.(type) {
				case *ast.TypeSpec:
					if symbol.Kind == compiler.DocType && spec.Name.Name == symbol.Name {
						return goASTNodeSpan(source, declaration, fileSet, spec)
					}
					if symbol.Kind == compiler.DocField && spec.Name.Name == symbol.Parent {
						if structure, ok := spec.Type.(*ast.StructType); ok {
							for _, field := range structure.Fields.List {
								for _, name := range lspGoStructFieldNames(field) {
									if name.Name == symbol.Name {
										return goASTNodeSpan(source, declaration, fileSet, field)
									}
								}
							}
						}
					}
					if symbol.Kind == compiler.DocMethod && spec.Name.Name == symbol.Parent {
						if interfaceType, ok := spec.Type.(*ast.InterfaceType); ok {
							for _, method := range interfaceType.Methods.List {
								for _, name := range method.Names {
									if name.Name == symbol.Name {
										return goASTNodeSpan(source, declaration, fileSet, method)
									}
								}
							}
						}
					}
				case *ast.ValueSpec:
					if symbol.Kind == compiler.DocValue {
						for _, name := range spec.Names {
							if name.Name == symbol.Name {
								return goASTNodeSpan(source, declaration, fileSet, spec)
							}
						}
					}
				}
			}
		case *ast.FuncDecl:
			if value.Name.Name != symbol.Name {
				continue
			}
			if symbol.Kind == compiler.DocFunction && value.Recv == nil || symbol.Kind == compiler.DocMethod && value.Recv != nil && lspGoReceiverTypeName(value.Recv) == symbol.Parent {
				return goASTNodeSpan(source, declaration, fileSet, value)
			}
		}
	}
	return compiler.Span{}, false
}

func goASTNodeSpan(source string, declaration compiler.Span, fileSet *gotoken.FileSet, node ast.Node) (compiler.Span, bool) {
	if fileSet == nil || node == nil || declaration.Line <= 0 {
		return compiler.Span{}, false
	}
	startPosition := fileSet.Position(node.Pos())
	endPosition := fileSet.Position(node.End())
	if !startPosition.IsValid() || !endPosition.IsValid() || startPosition.Line < 2 || endPosition.Line < 2 {
		return compiler.Span{}, false
	}
	startLine := declaration.Line + startPosition.Line - 2
	endLine := declaration.Line + endPosition.Line - 2
	start := offsetForLineColumn(source, startLine-1, startPosition.Column-1)
	end := offsetForLineColumn(source, endLine-1, endPosition.Column-1)
	if start < declaration.Start || end > declaration.End || end < start {
		return compiler.Span{}, false
	}
	return compiler.Span{Start: start, End: end, Line: startLine, Column: startPosition.Column}, true
}

func lspGoStructFieldNames(field *ast.Field) []*ast.Ident {
	if field == nil {
		return nil
	}
	if len(field.Names) > 0 {
		return field.Names
	}
	var embeddedName func(ast.Expr) *ast.Ident
	embeddedName = func(expression ast.Expr) *ast.Ident {
		switch value := expression.(type) {
		case *ast.Ident:
			return value
		case *ast.SelectorExpr:
			return value.Sel
		case *ast.StarExpr:
			return embeddedName(value.X)
		case *ast.IndexExpr:
			return embeddedName(value.X)
		case *ast.IndexListExpr:
			return embeddedName(value.X)
		case *ast.ParenExpr:
			return embeddedName(value.X)
		default:
			return nil
		}
	}
	if name := embeddedName(field.Type); name != nil {
		return []*ast.Ident{name}
	}
	return nil
}

func lspGoReceiverTypeName(receiver *ast.FieldList) string {
	if receiver == nil || len(receiver.List) == 0 || receiver.List[0] == nil {
		return ""
	}
	var typeName func(ast.Expr) string
	typeName = func(expression ast.Expr) string {
		switch value := expression.(type) {
		case *ast.Ident:
			return value.Name
		case *ast.StarExpr:
			return typeName(value.X)
		case *ast.IndexExpr:
			return typeName(value.X)
		case *ast.IndexListExpr:
			return typeName(value.X)
		case *ast.SelectorExpr:
			return value.Sel.Name
		case *ast.ParenExpr:
			return typeName(value.X)
		default:
			return ""
		}
	}
	return typeName(receiver.List[0].Type)
}

func goASTNameSpan(source string, declaration compiler.Span, fileSet *gotoken.FileSet, position gotoken.Pos, name string) (compiler.Span, bool) {
	if fileSet == nil || !position.IsValid() || declaration.Line <= 0 || name == "" {
		return compiler.Span{}, false
	}
	goPosition := fileSet.Position(position)
	if !goPosition.IsValid() || goPosition.Line < 2 || goPosition.Column < 1 {
		return compiler.Span{}, false
	}
	line := declaration.Line + goPosition.Line - 2
	start := offsetForLineColumn(source, line-1, goPosition.Column-1)
	end := start + len(name)
	if start < 0 || end > len(source) || source[start:end] != name {
		return compiler.Span{}, false
	}
	return compiler.Span{Start: start, End: end, Line: line, Column: goPosition.Column}, true
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
		name := strings.TrimPrefix(annotation, "@")
		if open := strings.IndexByte(name, '('); open >= 0 {
			name = name[:open]
		}
		if name == "Deprecated" || strings.HasSuffix(name, ".Deprecated") {
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
	case compiler.DocType:
		return 5
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
	if s.cancelled() {
		return []CompletionItem{}
	}
	state := s.workspace.files[pathFromURI(params.TextDocument.URI)]
	if state == nil {
		return []CompletionItem{}
	}
	offset := offsetAt(state.Source, params.Position)
	items := s.contextualCompletions(completionState(state, offset), offset)
	start, end := completionReplacementOffsets(state, offset)
	editRange := rangeForOffsets(state.Source, start, end)
	for index := range items {
		if s.cancelled() {
			return []CompletionItem{}
		}
		newText := items[index].InsertText
		if newText == "" {
			newText = items[index].Label
		}
		items[index].TextEdit = &TextEdit{Range: editRange, NewText: newText}
		items[index].Data = completionResolveData{URI: params.TextDocument.URI, Position: params.Position, Label: items[index].Label}
	}
	return items
}

func completionState(state *fileState, offset int) *fileState {
	if state == nil || state.File != nil || offset < 0 || offset > len(state.Source) {
		return state
	}
	start := offset
	for start > 0 && isIdentifierByte(state.Source[start-1]) {
		start--
	}
	selectorPrefix := strings.TrimRight(state.Source[:start], " \t\r\n")
	if !strings.HasSuffix(selectorPrefix, ".") {
		return state
	}
	const placeholder = "__gpp_completion_cursor()"
	source := state.Source[:start] + placeholder + state.Source[offset:]
	file, err := compiler.ParseFile(state.Path, source)
	if err != nil {
		return state
	}
	file.SourcePath = state.Path
	recovered := *state
	recovered.Source = source
	recovered.File = file
	return &recovered
}

func (s *server) resolveCompletionItem(item CompletionItem) CompletionItem {
	if item.Data == nil {
		return item
	}
	encoded, err := json.Marshal(item.Data)
	if err != nil {
		return item
	}
	var data completionResolveData
	if json.Unmarshal(encoded, &data) != nil || data.URI == "" || data.Label == "" {
		return item
	}
	params := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: data.URI}, Position: data.Position}
	for _, candidate := range s.completions(params) {
		if candidate.Label != data.Label {
			continue
		}
		if item.Kind == 0 {
			item.Kind = candidate.Kind
		}
		if item.Detail == "" {
			item.Detail = candidate.Detail
		}
		if item.Documentation == "" {
			item.Documentation = candidate.Documentation
		}
		if item.TextEdit == nil {
			item.TextEdit = candidate.TextEdit
		}
		if item.InsertText == "" {
			item.InsertText = candidate.InsertText
		}
		if item.InsertTextFormat == 0 {
			item.InsertTextFormat = candidate.InsertTextFormat
		}
		if len(item.AdditionalTextEdits) == 0 {
			item.AdditionalTextEdits = candidate.AdditionalTextEdits
		}
		item.Deprecated = item.Deprecated || candidate.Deprecated
		return item
	}
	return item
}

func completionReplacementOffsets(state *fileState, offset int) (int, int) {
	if state == nil {
		return offset, offset
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(state.Source) {
		offset = len(state.Source)
	}
	if state.File != nil {
		if token, ok := identifierTokenAt(state.File.Tokens, offset); ok {
			return token.Span.Start, token.Span.End
		}
	}
	start, end := offset, offset
	for start > 0 && isIdentifierByte(state.Source[start-1]) {
		start--
	}
	for end < len(state.Source) && isIdentifierByte(state.Source[end]) {
		end++
	}
	return start, end
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
		return tokens[index].Span.Start >= offset
	})
	if index < len(tokens) {
		token := tokens[index]
		if token.Kind == compiler.TokenIdentifier && token.Span.Start <= offset && offset <= token.Span.End {
			return token, true
		}
	}
	if index > 0 {
		token := tokens[index-1]
		if token.Kind == compiler.TokenIdentifier && token.Span.Start <= offset && offset <= token.Span.End {
			return token, true
		}
	}
	return compiler.Token{}, false
}

func sourceTokenAt(tokens []compiler.Token, offset int) (compiler.Token, bool) {
	index := sort.Search(len(tokens), func(index int) bool {
		return tokens[index].Span.Start >= offset
	})
	for _, candidate := range []int{index, index - 1} {
		if candidate < 0 || candidate >= len(tokens) {
			continue
		}
		token := tokens[candidate]
		if token.Kind == compiler.TokenInvalid || token.Kind == compiler.TokenEOF || token.Kind == compiler.TokenNewline || token.Kind == compiler.TokenComment {
			continue
		}
		if token.Span.Start <= offset && offset < token.Span.End {
			return token, true
		}
	}
	return compiler.Token{}, false
}

func validIdentifier(value string) bool {
	if value == "" || value == "_" || compiler.IsKeyword(value) {
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
	if position.Line < 0 {
		position.Line = 0
	}
	start, line := 0, 0
	for start < len(source) && line < position.Line {
		newline := strings.IndexByte(source[start:], '\n')
		if newline < 0 {
			return len(source)
		}
		start += newline + 1
		line++
	}
	if line < position.Line {
		return len(source)
	}
	end := strings.IndexByte(source[start:], '\n')
	if end < 0 {
		end = len(source) - start
	}
	end += start
	if end > start && source[end-1] == '\r' {
		end--
	}
	if position.Character < 0 {
		position.Character = 0
	}
	return start + utf16Offset(source[start:end], position.Character)
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
	linePrefix := source[lineStart:offset]
	// LSP positions count the content of a line, excluding the CR in CRLF.
	// Compiler spans can end on either byte of that line terminator, so map
	// both offsets to the same end-of-line position.
	linePrefix = strings.TrimSuffix(linePrefix, "\r")
	return Position{Line: line, Character: utf16Length(linePrefix)}
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
