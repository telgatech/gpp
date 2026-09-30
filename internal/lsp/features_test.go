package lsp

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/telgatech/gpp/compiler"
)

func TestSemanticTokensCoverGoPlusSyntaxWithStableRanges(t *testing.T) {
	source := `package main
annotation Route(path string) on method
annotation Table(name string) on class
/* 🌟 comment
   continued comment */

class User @{Table("users")} {
	Name string
	func Greeting() string @{Route("/users")} { return "hello" }
}

extend User { func Slug() string { return "user" } }
template UserCard(user User) { <strong>{{.Name}}</strong> }

func inspect(users []User) {
	user := User()
	text := __RAW__first line
second line__RAW__
	_ = text
	_ = user?.Name
	_ = record(Name: "Ada")
	_ = users.Any(it => it.Name != "")
	try { user.Greeting() } catch err { _ = err }
}

func atExit() {}
`
	source = strings.ReplaceAll(source, "__RAW__", "`")
	file, err := compiler.ParseFile("features.gpp", source)
	if err != nil {
		t.Fatalf("feature sample should parse: %v", err)
	}
	state := &fileState{Path: "/features.gpp", URI: "file:///features.gpp", Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{state.Path: state}}}
	result := server.semanticTokens(state.URI)
	data, ok := result["data"].([]uint32)
	if !ok || len(data)%5 != 0 || len(data) == 0 {
		t.Fatalf("invalid semantic token payload: %#v", result)
	}
	found := map[string]bool{}
	types := map[string]map[int]bool{}
	modifiers := map[string]int{}
	previousLine, previousCharacter, previousLength := 0, 0, 0
	for index := 0; index < len(data); index += 5 {
		deltaLine, deltaCharacter := int(data[index]), int(data[index+1])
		line := previousLine + deltaLine
		character := deltaCharacter
		if deltaLine == 0 {
			character += previousCharacter
		}
		length := int(data[index+2])
		tokenType := int(data[index+3])
		if length == 0 || tokenType >= len(semanticTokenTypes) {
			t.Fatalf("invalid token range or type: %v", data[index:index+5])
		}
		if line == previousLine && character < previousCharacter+previousLength {
			t.Fatalf("overlapping semantic tokens on line %d: %d < %d", line, character, previousCharacter+previousLength)
		}
		lineText := sourceLine(source, line)
		if character+length > utf16Length(lineText) {
			t.Fatalf("token extends past its source line: line=%d character=%d length=%d", line, character, length)
		}
		start := offsetAt(source, Position{Line: line, Character: character})
		end := offsetAt(source, Position{Line: line, Character: character + length})
		found[source[start:end]] = true
		name := source[start:end]
		if types[name] == nil {
			types[name] = map[int]bool{}
		}
		types[name][tokenType] = true
		modifiers[name] |= int(data[index+4])
		previousLine, previousCharacter, previousLength = line, character, length
	}
	for _, expected := range []string{"class", "User", "Table", "Name", "Greeting", "template", "UserCard", "record", "?.", "=>", "catch", "err", "atExit"} {
		if !found[expected] {
			t.Errorf("semantic tokens omitted %q; got %#v", expected, found)
		}
	}
	for text, expectedType := range map[string]int{
		"User":     semanticClass,
		"Table":    semanticDecorator,
		"Name":     semanticProperty,
		"Greeting": semanticMethod,
		"UserCard": semanticFunction,
		"atExit":   semanticFunction,
		"?.":       semanticOperator,
	} {
		if !types[text][expectedType] {
			t.Errorf("%q has semantic types %#v, want %d", text, types[text], expectedType)
		}
	}
	if modifiers["User"]&modifierDeclaration == 0 {
		t.Error("class declaration token is missing its declaration modifier")
	}
}

func TestCodeActionsUseStableDiagnosticCodesAndFixData(t *testing.T) {
	uri := "file:///main.gpp"
	source := "func main() {\n\tvalue := \"unfinished\n}\n"
	state := &fileState{Path: "/main.gpp", URI: uri, Source: source}
	workspace := &workspaceState{files: map[string]*fileState{state.Path: state}, diagnostics: map[string][]Diagnostic{}}
	workspace.analyze()
	diagnostics := workspace.diagnostics[uri]
	if len(diagnostics) != 1 {
		t.Fatalf("expected an unterminated string diagnostic, got %#v", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Code != diagnosticUnclosedLiteral {
		t.Fatalf("unstable or missing diagnostic code: %#v", diagnostic)
	}
	server := &server{workspace: workspace}
	params := codeActionParams{TextDocument: textDocumentIdentifier{URI: uri}}
	params.Context.Diagnostics = []Diagnostic{diagnostic}
	actions := server.codeActions(params)
	if len(actions) != 1 || actions[0].Title != "Close unterminated string literal" {
		t.Fatalf("expected one string quick fix, got %#v", actions)
	}
	edits := actions[0].Edit.Changes[uri]
	wantStringEnd := positionAtOffset(source, strings.Index(source, "\n}"))
	if len(edits) != 1 || edits[0].NewText != `"` || edits[0].Range.Start != edits[0].Range.End || edits[0].Range.Start != wantStringEnd {
		t.Fatalf("quick fix has invalid edit data: %#v", actions[0])
	}

	commentSource := "/* unfinished"
	state.Source = commentSource
	workspace.analyze()
	diagnostics = workspace.diagnostics[uri]
	if len(diagnostics) != 1 {
		t.Fatalf("expected an unterminated comment diagnostic, got %#v", diagnostics)
	}
	commentDiagnostic := diagnostics[0]
	if commentDiagnostic.Code != diagnosticUnclosedComment {
		t.Fatalf("unexpected comment diagnostic: %#v", commentDiagnostic)
	}
	params.Context.Diagnostics = []Diagnostic{commentDiagnostic}
	actions = server.codeActions(params)
	if len(actions) != 1 || actions[0].Edit.Changes[uri][0].NewText != "*/" {
		t.Fatalf("expected block comment quick fix, got %#v", actions)
	}
}

func TestContextualCompletionsUseScopeReceiverImportsAndAnnotationTargets(t *testing.T) {
	root := t.TempDir()
	mainSource := `package main
annotation Route(path string) on method
annotation Table(name string) on class
class User @{Table("users")} {
	Name string
	func Save() @{Route("/")} {}
}
extend User { func Slug() string { return "slug" } }
template UserCard(user User) { <strong>{{.Name}}</strong> }
func inspect(users []User, user User) {
	local := "name"
	profile := record(Name: "Ada", Age: 42)
	_ = user.Name
	_ = user?.Name
	_ = profile.Name
	_ = users.Any(item => item.Name != "")
	try { user.Save() } catch err { _ = err }
}
`
	userFile, err := compiler.ParseFile("main.gpp", mainSource)
	if err != nil {
		t.Fatal(err)
	}
	userFile.SourcePath = filepath.Join(root, "main.gpp")
	state := &fileState{Path: userFile.SourcePath, URI: uriForPath(userFile.SourcePath), Source: mainSource, File: userFile}
	otherSource := "package foo.bar\nclass Widget { Name string }\n"
	otherFile, err := compiler.ParseFile("widget.gpp", otherSource)
	if err != nil {
		t.Fatal(err)
	}
	otherFile.SourcePath = filepath.Join(root, "foo", "widget.gpp")
	otherState := &fileState{Path: otherFile.SourcePath, URI: uriForPath(otherFile.SourcePath), Source: otherSource, File: otherFile}
	importSource := "package main\nimport foo.bar\nfunc use() { _ = bar.Widget() }\n"
	importFile, err := compiler.ParseFile("imports.gpp", importSource)
	if err != nil {
		t.Fatal(err)
	}
	importFile.SourcePath = filepath.Join(root, "imports.gpp")
	importState := &fileState{Path: importFile.SourcePath, URI: uriForPath(importFile.SourcePath), Source: importSource, File: importFile}

	workspace := &workspaceState{files: map[string]*fileState{state.Path: state, otherState.Path: otherState, importState.Path: importState}}
	files := []*compiler.File{userFile, otherFile, importFile}
	if prelude, loadErr := compiler.LoadPrelude(); loadErr == nil {
		files = append(files, prelude)
	}
	workspace.docIndex = compiler.BuildDocIndex(files)
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	memberOffset := strings.Index(mainSource, "user.Name") + len("user.")
	memberItems := completionLabels(server.contextualCompletions(state, memberOffset))
	for _, expected := range []string{"Name", "Save", "Slug"} {
		if !memberItems[expected] {
			t.Errorf("receiver/scope completion omitted %q: %#v", expected, memberItems)
		}
	}
	if memberItems["Widget"] {
		t.Errorf("receiver completion leaked an unrelated package class: %#v", memberItems)
	}
	safeOffset := strings.Index(mainSource, "user?.Name") + len("user?.")
	safeItems := completionLabels(server.contextualCompletions(state, safeOffset))
	if !safeItems["Name"] {
		t.Errorf("safe-access completion omitted a receiver field: %#v", safeItems)
	}
	recordOffset := strings.Index(mainSource, "profile.Name") + len("profile.")
	recordItems := completionLabels(server.contextualCompletions(state, recordOffset))
	for _, expected := range []string{"Name", "Age"} {
		if !recordItems[expected] {
			t.Errorf("record-shape completion omitted %q: %#v", expected, recordItems)
		}
	}
	lambdaOffset := strings.Index(mainSource, "item.Name") + len("item.")
	lambdaItems := completionLabels(server.contextualCompletions(state, lambdaOffset))
	if !lambdaItems["Name"] {
		t.Errorf("lambda receiver completion omitted a class field: %#v", lambdaItems)
	}
	catchOffset := strings.Index(mainSource, "_ = err") + len("_ = ")
	catchItems := completionLabels(server.contextualCompletions(state, catchOffset))
	if !catchItems["err"] {
		t.Errorf("exception catch completion omitted its binding: %#v", catchItems)
	}
	scopeOffset := strings.LastIndex(mainSource, "\n}\n")
	scopeItems := completionLabels(server.contextualCompletions(state, scopeOffset))
	for _, expected := range []string{"user", "local"} {
		if !scopeItems[expected] {
			t.Errorf("lexical scope completion omitted %q: %#v", expected, scopeItems)
		}
	}
	if scopeItems["err"] {
		t.Errorf("catch binding leaked outside its lexical scope: %#v", scopeItems)
	}

	annotationOffset := strings.Index(mainSource, "@{Table") + 2
	annotationItems := completionLabels(server.contextualCompletions(state, annotationOffset))
	if !annotationItems["Table"] || annotationItems["Route"] {
		t.Errorf("class annotation completion ignored target restrictions: %#v", annotationItems)
	}
	methodAnnotationOffset := strings.Index(mainSource, "@{Route") + 2
	methodAnnotationItems := completionLabels(server.contextualCompletions(state, methodAnnotationOffset))
	if !methodAnnotationItems["Route"] || methodAnnotationItems["Table"] {
		t.Errorf("method annotation completion ignored target restrictions: %#v", methodAnnotationItems)
	}
	templateOffset := strings.Index(mainSource, "<strong>{{.Name") + len("<strong>{{.")
	templateItems := completionLabels(server.contextualCompletions(state, templateOffset))
	if !templateItems["Name"] {
		t.Errorf("template data completion omitted a declared field: %#v", templateItems)
	}
	lambdaScopeOffset := strings.Index(mainSource, "item.Name")
	lambdaScopeItems := completionLabels(server.contextualCompletions(state, lambdaScopeOffset))
	if !lambdaScopeItems["item"] {
		t.Errorf("lambda lexical scope omitted its parameter: %#v", lambdaScopeItems)
	}

	qualifiedOffset := strings.Index(importSource, "bar.Widget") + len("bar.")
	qualifiedItems := completionLabels(server.contextualCompletions(importState, qualifiedOffset))
	if !qualifiedItems["Widget"] {
		t.Errorf("logical package completion omitted Widget: %#v", qualifiedItems)
	}
	importOffset := strings.Index(importSource, "foo.bar") + len("foo.")
	importItems := completionLabels(server.contextualCompletions(&fileState{Path: importState.Path, URI: importState.URI, Source: "package main\nimport foo.\n"}, importOffset))
	if !importItems["bar"] {
		t.Errorf("logical package path completion omitted bar: %#v", importItems)
	}
}

func TestLSPAdvertisesSemanticTokensAndCodeActions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.gpp")
	uri := uriForPath(path)
	var input bytes.Buffer
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"rootUri": uriForPath(root)}})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": "func main() {}\n"}}})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "textDocument/semanticTokens/full", "params": map[string]any{"textDocument": map[string]any{"uri": uri}}})
	diagnostic := Diagnostic{Range: Range{}, Severity: 1, Source: "gpp", Code: diagnosticUnclosedComment, Data: map[string]any{"fix": "insert-delimiter", "text": "*/", "offset": 13}, Message: "unterminated block comment"}
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "textDocument/codeAction", "params": map[string]any{"textDocument": map[string]any{"uri": uri}, "range": Range{}, "context": map[string]any{"diagnostics": []Diagnostic{diagnostic}}}})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "shutdown", "params": nil})
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	var output bytes.Buffer
	if err := Run(&input, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	messages := readTestMessages(t, output.Bytes())
	var capabilities map[string]any
	var semanticResult, actionResult map[string]any
	var actionList []any
	for _, message := range messages {
		if id, _ := message["id"].(float64); id == 1 {
			result := message["result"].(map[string]any)
			capabilities = result["capabilities"].(map[string]any)
		} else if id, _ := message["id"].(float64); id == 2 {
			semanticResult, _ = message["result"].(map[string]any)
		} else if id, _ := message["id"].(float64); id == 3 {
			actionList, _ = message["result"].([]any)
			actionResult = map[string]any{"items": actionList}
		}
	}
	if capabilities == nil || capabilities["codeActionProvider"] == nil || capabilities["semanticTokensProvider"] == nil {
		t.Fatalf("server did not advertise new LSP support: %#v", capabilities)
	}
	provider := capabilities["semanticTokensProvider"].(map[string]any)
	legend := provider["legend"].(map[string]any)
	if got := legend["tokenTypes"].([]any); len(got) != len(semanticTokenTypes) || got[0] != "namespace" || got[len(got)-1] != "decorator" {
		t.Fatalf("server advertised an unstable semantic token legend: %#v", legend)
	}
	if semanticResult == nil || len(semanticResult["data"].([]any)) == 0 {
		t.Fatalf("semantic token request returned no data: %#v", semanticResult)
	}
	if len(actionList) != 1 || actionResult == nil {
		t.Fatalf("code action request returned no quick fix: %#v", actionResult)
	}
}

func completionLabels(items []CompletionItem) map[string]bool {
	result := map[string]bool{}
	for _, item := range items {
		result[item.Label] = true
	}
	return result
}
