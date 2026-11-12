package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/telgatech/gpp/compiler"
)

func fullDocumentRange(source string) Range {
	line := strings.Count(source, "\n")
	return Range{End: Position{Line: line, Character: utf16Length(sourceLine(source, line))}}
}

func TestLSPAcceptsBlockCommentInClassHeader(t *testing.T) {
	source := `package main

class Base {}
class Todo : Base /* @{orm.Table("todos")} */ {
    Id int64 /* @{orm.Column("id"), orm.PK} */
}
`
	path := "/todo.gpp"
	uri := uriForPath(path)
	workspace := &workspaceState{
		files:       map[string]*fileState{path: {Path: path, URI: uri, Source: source, Open: true}},
		diagnostics: map[string][]Diagnostic{},
	}
	workspace.analyze()
	if diagnostics := workspace.diagnostics[uri]; len(diagnostics) != 0 {
		t.Fatalf("LSP reported diagnostics for a valid commented class header: %#v", diagnostics)
	}
}

func TestSemanticTokensCoverGPPSyntaxWithStableRanges(t *testing.T) {
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
	_ = user?.Name ?? "Unknown"
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
	for _, expected := range []string{"class", "User", "Table", "Name", "Greeting", "template", "UserCard", "record", "?.", "??", "=>", "catch", "err", "atExit"} {
		if !found[expected] {
			t.Errorf("semantic tokens omitted %q; got %#v", expected, found)
		}
	}
	for _, parameter := range []string{"path", "name", "user", "users"} {
		if !types[parameter][semanticParameter] {
			t.Errorf("parameter %q should have the parameter semantic-token type; got %#v", parameter, types[parameter])
		}
	}
	for _, local := range []string{"user", "text", "err"} {
		if !types[local][semanticVariable] {
			t.Errorf("local binding %q should have the variable semantic-token type; got %#v", local, types[local])
		}
	}
	if types["string"][semanticParameter] {
		t.Errorf("parameter type names must not be marked as parameter declarations: %#v", types["string"])
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
	if modifiers["text"]&modifierDeclaration == 0 || modifiers["err"]&modifierDeclaration == 0 {
		t.Errorf("local and catch declarations should carry the declaration modifier: text=%b err=%b", modifiers["text"], modifiers["err"])
	}
}

func TestSemanticTokensClassifyFunctionsInMixedDecl(t *testing.T) {
	file, err := compiler.ParseFile("mixed-function.gpp", "package main\nfunc combined(input string) {}\n")
	if err != nil {
		t.Fatalf("function sample should parse: %v", err)
	}
	function, ok := file.Decls[0].(*compiler.FunctionDecl)
	if !ok {
		t.Fatalf("expected structured function declaration, got %T", file.Decls[0])
	}
	file.Decls = []compiler.Decl{&compiler.MixedDecl{Functions: []*compiler.FunctionDecl{function}}}

	classified := map[int]semanticToken{}
	classifyGPPTokens(file, classified)
	name := classified[function.Method.NameSpan.Start]
	if name.tokenType != semanticFunction || name.modifiers&modifierDeclaration == 0 {
		t.Errorf("MixedDecl function name token = %#v, want declared function", name)
	}
	parameterStart := -1
	for _, token := range file.Tokens {
		if token.Text == "input" {
			parameterStart = token.Span.Start
			break
		}
	}
	if parameterStart < 0 {
		t.Fatal("parsed parameter token not found")
	}
	parameter := classified[parameterStart]
	if parameter.tokenType != semanticParameter || parameter.modifiers&modifierDeclaration == 0 {
		t.Errorf("MixedDecl parameter token = %#v, want declared parameter", parameter)
	}
}

func TestSemanticTokenRangeAndDeltaRequests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.gpp")
	uri := uriForPath(path)
	source := "package main\nfunc first() { value := 1 }\nfunc second() { other := 2 }\n"
	file, err := compiler.ParseFile(path, source)
	if err != nil {
		t.Fatal(err)
	}
	file.SourcePath = path
	state := &fileState{Path: path, URI: uri, Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	requested := Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 40}}
	ranged, ok := server.semanticTokensForRange(uri, requested)["data"].([]uint32)
	if !ok || len(ranged) == 0 || len(ranged)%5 != 0 {
		t.Fatalf("range token request returned invalid data: %#v", ranged)
	}
	line, character := 0, 0
	for index := 0; index < len(ranged); index += 5 {
		deltaLine, deltaCharacter := int(ranged[index]), int(ranged[index+1])
		line += deltaLine
		if deltaLine == 0 {
			character += deltaCharacter
		} else {
			character = deltaCharacter
		}
		if line != 1 || character < requested.Start.Character || character+int(ranged[index+2]) > requested.End.Character {
			t.Fatalf("range response included a token outside its requested range: token=%d data=%#v", index/5, ranged)
		}
	}

	first := server.semanticTokensDelta(uri, "")
	previousResultID, ok := first["resultId"].(string)
	previousData, dataOK := first["data"].([]uint32)
	if !ok || !dataOK {
		t.Fatalf("first delta request should return a full result and result id: %#v", first)
	}
	source = "package main\nfunc first() { value := 10 }\nfunc second() { other := 2 }\n"
	file, err = compiler.ParseFile(path, source)
	if err != nil {
		t.Fatal(err)
	}
	file.SourcePath = path
	state.Source, state.File = source, file
	delta := server.semanticTokensDelta(uri, previousResultID)
	newData, _ := server.semanticTokens(uri)["data"].([]uint32)
	edits, ok := delta["edits"].([]any)
	if !ok || len(edits) != 1 {
		t.Fatalf("changed tokens should produce one delta edit: %#v", delta)
	}
	edit, ok := edits[0].(map[string]any)
	if !ok {
		t.Fatalf("delta edit has an invalid shape: %#v", edits[0])
	}
	start, _ := edit["start"].(int)
	deleteCount, _ := edit["deleteCount"].(int)
	inserted, _ := edit["data"].([]uint32)
	if start < 0 || deleteCount < 0 || start+deleteCount > len(previousData) {
		t.Fatalf("delta edit is out of bounds: %#v", edit)
	}
	reconstructed := append([]uint32(nil), previousData[:start]...)
	reconstructed = append(reconstructed, inserted...)
	reconstructed = append(reconstructed, previousData[start+deleteCount:]...)
	if !equalSemanticToken(reconstructed, newData) {
		t.Fatalf("delta edit did not reconstruct current token data: got=%v want=%v edit=%#v", reconstructed, newData, edit)
	}
}

func TestSemanticTokensClassifyGenericTypeParametersAndUses(t *testing.T) {
	source := `package main
class Box[T any] {
	Value T
	func Convert[U any](value U) T { return this.Value }
}

func Identity[V any](value V) V { return value }
`
	file, err := compiler.ParseFile("generic.gpp", source)
	if err != nil {
		t.Fatalf("generic declarations should parse: %v", err)
	}
	state := &fileState{Path: "/generic.gpp", URI: "file:///generic.gpp", Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{state.Path: state}}}
	result := server.semanticTokens(state.URI)
	data, ok := result["data"].([]uint32)
	if !ok || len(data)%5 != 0 {
		t.Fatalf("invalid semantic token payload: %#v", result)
	}
	types := map[string][]int{}
	declarations := map[string]int{}
	line, character := 0, 0
	for index := 0; index < len(data); index += 5 {
		deltaLine, deltaCharacter := int(data[index]), int(data[index+1])
		line += deltaLine
		if deltaLine == 0 {
			character += deltaCharacter
		} else {
			character = deltaCharacter
		}
		length := int(data[index+2])
		start := offsetAt(source, Position{Line: line, Character: character})
		end := offsetAt(source, Position{Line: line, Character: character + length})
		name := source[start:end]
		types[name] = append(types[name], int(data[index+3]))
		if int(data[index+4])&modifierDeclaration != 0 {
			declarations[name]++
		}
	}
	for _, name := range []string{"T", "U", "V"} {
		if declarations[name] != 1 {
			t.Errorf("type parameter %q should have one declaration token, got %d", name, declarations[name])
		}
		if len(types[name]) < 2 {
			t.Errorf("type parameter %q should classify both declaration and uses, got %v", name, types[name])
		}
		for _, tokenType := range types[name] {
			if tokenType != semanticTypeParameter {
				t.Errorf("type parameter %q occurrence has semantic type %d, want %d (all: %v)", name, tokenType, semanticTypeParameter, types[name])
			}
		}
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
	params.Context.Only = []string{"quickfix.unknown"}
	if actions := server.codeActions(params); len(actions) != 0 {
		t.Fatalf("a generic quick fix must not match a more specific unsupported filter, got %#v", actions)
	}
	params.Context.Only = nil
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
	secret string
	_secret string
	func Save() @{Route("/")} {}
	func hidden() {}
	func inspectPrivate() { _ = this. }
}
class Parent {
	inheritedSecret string
	_protectedState string
	func _helper() {}
}
class Child : Parent { func inspect() { _ = this. } }
func inspectChild(child Child) { _ = child. }
extend User { func Slug() string { return "slug" } }
template UserCard(user User) { <strong>{{.Name}}</strong> }
func inspect(users []User, user User) {
	local := "name"
	userValue := User()
	childValue := Child()
	labels := []string{"a", "b"}
	profile := record(Name: "Ada", Age: 42)
	_ = user.Name
	_ = userValue
	_ = labels.Any(item => item != "")
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
	otherSource := "package foo.bar\nclass Widget { Name string }\nfunc PublicHelper() {}\nfunc privateHelper() {}\n"
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
	if memberItems["secret"] || memberItems["_secret"] || memberItems["hidden"] {
		t.Errorf("class-private members leaked to a same-package consumer: %#v", memberItems)
	}
	constructorOffset := strings.Index(mainSource, "userValue := User()") + len("userValue := User(")
	constructorItems := completionLabels(server.contextualCompletions(state, constructorOffset))
	if constructorItems["secret"] {
		t.Errorf("class-private fields leaked to constructor completions: %#v", constructorItems)
	}
	childConstructorOffset := strings.Index(mainSource, "childValue := Child()") + len("childValue := Child(")
	childConstructorItems := completionLabels(server.contextualCompletions(state, childConstructorOffset))
	if !childConstructorItems["_protectedState"] || childConstructorItems["inheritedSecret"] {
		t.Errorf("child constructor completions should offer inherited protected fields but omit private ones: %#v", childConstructorItems)
	}
	privateCompletionOffset := strings.Index(mainSource, "this. }") + len("this.")
	privateItems := completionLabels(server.contextualCompletions(state, privateCompletionOffset))
	if !privateItems["secret"] || !privateItems["_secret"] || !privateItems["hidden"] {
		t.Errorf("a class should see its own private members: %#v", privateItems)
	}
	labelsOffset := strings.Index(mainSource, "labels.Any") + len("labels.")
	labelsType := server.receiverType(state, "labels", labelsOffset)
	labelsItems := completionLabels(server.contextualCompletions(state, labelsOffset))
	for _, expected := range []string{"Any", "Filter", "Map"} {
		if !labelsItems[expected] {
			t.Errorf("local string slice completion omitted %q (type %q, receiver %q): %#v", expected, labelsType, selectorReceiverBefore(state, labelsOffset-1), labelsItems)
		}
	}
	childOffset := strings.Index(mainSource, "child Child) { _ = child.") + len("child Child) { _ = child.")
	childItems := completionLabels(server.contextualCompletions(state, childOffset))
	if childItems["inheritedSecret"] || childItems["_protectedState"] || childItems["_helper"] {
		t.Errorf("ordinary callers should not see private or protected member completions: %#v", childItems)
	}
	childMethodOffset := strings.LastIndex(mainSource, "_ = this.") + len("_ = this.")
	childMethodItems := completionLabels(server.contextualCompletions(state, childMethodOffset))
	if !childMethodItems["_protectedState"] || !childMethodItems["_helper"] || childMethodItems["inheritedSecret"] {
		t.Errorf("a child class should see protected members but not private members: %#v", childMethodItems)
	}
	if memberItems["Widget"] {
		t.Errorf("receiver completion leaked an unrelated package class: %#v", memberItems)
	}
	importCompletionOffset := strings.Index(mainSource, "local :=") + len("local")
	var importedWidget *CompletionItem
	for _, item := range server.contextualCompletions(state, importCompletionOffset) {
		if item.Label == "bar.Widget" {
			importedWidget = &item
			break
		}
	}
	if importedWidget == nil || len(importedWidget.AdditionalTextEdits) != 1 || !strings.Contains(importedWidget.AdditionalTextEdits[0].NewText, "import foo.bar") {
		t.Errorf("unimported workspace class completion should add its logical import: %#v", importedWidget)
	} else {
		edit := importedWidget.AdditionalTextEdits[0]
		start, end := offsetAt(mainSource, edit.Range.Start), offsetAt(mainSource, edit.Range.End)
		withImport := mainSource[:start] + edit.NewText + mainSource[end:]
		if _, parseErr := compiler.ParseFile("completion.gpp", withImport); parseErr != nil {
			t.Errorf("completion import edit should leave valid GPP source: %v\n%s", parseErr, withImport)
		}
	}
	for _, item := range server.contextualCompletions(state, importCompletionOffset) {
		if item.Label == "bar.privateHelper" {
			t.Errorf("unexported package function should not be offered as an import completion: %#v", item)
		}
		if item.Label == "bar.PublicHelper" {
			if len(item.AdditionalTextEdits) != 1 || !strings.Contains(item.AdditionalTextEdits[0].NewText, "import foo.bar") {
				t.Errorf("exported package function completion should add its import: %#v", item)
			}
		}
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

func TestCallableCompletionsInsertSnippetsAndKeepImportEdits(t *testing.T) {
	root := t.TempDir()
	mainSource := "package main\nclass User { func Rename(name string) {} }\nfunc local(id int) {}\nfunc use(user User) { user.Re }\n"
	mainFile, err := compiler.ParseFile("main.gpp", mainSource)
	if err != nil {
		t.Fatal(err)
	}
	mainFile.SourcePath = filepath.Join(root, "main.gpp")
	mainState := &fileState{Path: mainFile.SourcePath, URI: uriForPath(mainFile.SourcePath), Source: mainSource, File: mainFile}

	libSource := "package lib\n/// Load a user by ID.\nfunc Load(id int) User { return User{} }\n"
	libFile, err := compiler.ParseFile("lib.gpp", libSource)
	if err != nil {
		t.Fatal(err)
	}
	libFile.SourcePath = filepath.Join(root, "lib", "lib.gpp")
	libState := &fileState{Path: libFile.SourcePath, URI: uriForPath(libFile.SourcePath), Source: libSource, File: libFile}

	workspace := &workspaceState{files: map[string]*fileState{mainState.Path: mainState, libState.Path: libState}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{mainFile, libFile})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	var rename *CompletionItem
	memberOffset := strings.Index(mainSource, "user.Re") + len("user.Re")
	for _, item := range server.completions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: mainState.URI}, Position: positionAtOffset(mainSource, memberOffset)}) {
		if item.Label == "Rename" {
			copy := item
			rename = &copy
		}
	}
	if rename == nil || rename.InsertText != "Rename(${1:name})$0" || rename.InsertTextFormat != 2 || rename.TextEdit == nil || !strings.Contains(rename.Detail, "name string") {
		t.Fatalf("method completion should include its signature, snippet, and replacement edit: %#v", rename)
	}
	inlineItems := server.inlineCompletions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: mainState.URI}, Position: positionAtOffset(mainSource, memberOffset)})
	var inlineRename map[string]any
	for _, item := range inlineItems {
		if item["filterText"] == "Rename" {
			inlineRename = item
			break
		}
	}
	if inlineRename == nil || inlineRename["range"] == nil {
		t.Fatalf("inline completion should expose a replacement range for the method: %#v", inlineItems)
	}
	if snippet, ok := inlineRename["insertText"].(map[string]any); !ok || snippet["kind"] != "snippet" || snippet["value"] != "Rename(${1:name})$0" {
		t.Fatalf("inline completion snippets must use the standard discriminated shape: %#v", inlineRename["insertText"])
	}

	var load *CompletionItem
	completionOffset := strings.Index(mainSource, "func use")
	for _, item := range server.contextualCompletions(mainState, completionOffset) {
		if item.Label == "lib.Load" {
			copy := item
			load = &copy
		}
	}
	if load == nil || load.InsertText != "lib.Load(${1:id})$0" || load.InsertTextFormat != 2 || load.Detail == "" || !strings.Contains(load.Documentation, "Load a user") || len(load.AdditionalTextEdits) != 1 {
		t.Fatalf("imported function completion should preserve its docs, snippet, signature, and import edit: %#v", load)
	}
}

func TestCallableSnippetExtractsNamedGroupedAndUnnamedParameters(t *testing.T) {
	tests := []struct {
		signature string
		name      string
		want      string
	}{
		{signature: "func Load(ctx context.Context, ids []int64, limit int) []Row", name: "Load", want: "Load(${1:ctx}, ${2:ids}, ${3:limit})$0"},
		{signature: "func Pair(a, b int)", name: "Pair", want: "Pair(${1:a}, ${2:b})$0"},
		{signature: "func Convert(int, string, map[string]int)", name: "Convert", want: "Convert(${1:arg1}, ${2:arg2}, ${3:arg3})$0"},
		{signature: "func Map[T any](values []T, fn func(T) U)", name: "Map", want: "Map(${1:values}, ${2:value => ${3:}})$0"},
		{signature: "func Reset()", name: "Reset", want: "Reset()$0"},
	}
	for _, test := range tests {
		if got := callableSnippet(test.name, test.signature); got != test.want {
			t.Errorf("callable snippet for %q = %q, want %q", test.signature, got, test.want)
		}
	}
}

func TestGoSourceCompletionAddsQuotedModuleImport(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "cmd", "main.gpp")
	nativePath := filepath.Join(root, "models", "widget.go")
	mainSource := "package main\nfunc use() { _ = Wid }\n"
	nativeSource := "package models\ntype Widget struct { Name string }\n"
	for path, source := range map[string]string{mainPath: mainSource, nativePath: nativeSource} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mainState := &fileState{Path: mainPath, URI: uriForPath(mainPath), Source: mainSource, Open: true}
	workspace := &workspaceState{files: map[string]*fileState{mainPath: mainState}, diagnostics: map[string][]Diagnostic{}}
	workspace.setRoots([]string{root})
	workspace.refresh()
	if workspace.files[mainPath] == nil || workspace.files[mainPath].File == nil {
		t.Fatalf("GPP completion source should parse: %#v", workspace.diagnostics[uriForPath(mainPath)])
	}
	server := &server{workspace: workspace}
	cursor := strings.Index(mainSource, "Wid") + len("Wid")
	var suggestion *CompletionItem
	for _, item := range server.completions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: mainState.URI}, Position: positionAtOffset(mainSource, cursor),
	}) {
		if item.Label == "models.Widget" {
			copy := item
			suggestion = &copy
			break
		}
	}
	if suggestion == nil || suggestion.TextEdit == nil || len(suggestion.AdditionalTextEdits) != 1 || !strings.Contains(suggestion.AdditionalTextEdits[0].NewText, `import "example.com/app/models"`) {
		t.Fatalf("native Go completion should insert a quoted module import and replacement edit: %#v", suggestion)
	}

	type editWithOffsets struct {
		start, end int
		text       string
	}
	edits := []editWithOffsets{}
	for _, edit := range append([]TextEdit{*suggestion.TextEdit}, suggestion.AdditionalTextEdits...) {
		edits = append(edits, editWithOffsets{start: offsetAt(mainSource, edit.Range.Start), end: offsetAt(mainSource, edit.Range.End), text: edit.NewText})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	updated := mainSource
	for _, edit := range edits {
		updated = updated[:edit.start] + edit.text + updated[edit.end:]
	}
	parsed, err := compiler.ParseFile("completed.gpp", updated)
	if err != nil {
		t.Fatalf("applying native Go completion edits should produce valid GPP source: %v\n%s", err, updated)
	}
	if len(parsed.Imports) != 1 || parsed.Imports[0].Path != "example.com/app/models" || parsed.Imports[0].LogicalPackage {
		t.Fatalf("completion should add a regular quoted Go import: %#v", parsed.Imports)
	}
}

func TestGPPCompletionResolvesMembersOnImportedGoTypes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "cmd", "main.gpp")
	modelPath := filepath.Join(root, "models", "widget.go")
	mainSource := `package main
import "example.com/app/models"
func use(widget models.Widget) {
	_ = widget.
}
`
	modelSource := `package models
type Widget struct { Name string }
func (Widget) Save() error { return nil }
`
	for path, source := range map[string]string{mainPath: mainSource, modelPath: modelSource} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	state := &fileState{Path: mainPath, URI: uriForPath(mainPath), Source: mainSource, Open: true}
	workspace := &workspaceState{files: map[string]*fileState{mainPath: state}, diagnostics: map[string][]Diagnostic{}}
	workspace.setRoots([]string{root})
	workspace.refresh()
	state = workspace.files[mainPath]
	if state == nil || state.File == nil {
		t.Fatalf("GPP selector source should parse: %#v", workspace.diagnostics[uriForPath(mainPath)])
	}
	server := &server{workspace: workspace}
	cursor := strings.Index(mainSource, "widget.") + len("widget.")
	items := completionLabels(server.completions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(mainSource, cursor),
	}))
	for _, expected := range []string{"Name", "Save"} {
		if !items[expected] {
			t.Errorf("imported Go type completion omitted %q: %#v", expected, items)
		}
	}
	if items["Widget"] {
		t.Errorf("imported Go type completion leaked package-level symbols: %#v", items)
	}
}

func TestCompletionResolvesMembersAcrossGenericSliceAndStringChains(t *testing.T) {
	root := t.TempDir()
	source := `package main
func main() {
	a := []string{"a", "b"}
	a.Each(it => print(it))
mapped := a.Map(value => value.ToUpper()).
	bool := a.First().ToUpper().
}
`
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatalf("completion sample should parse: %v", err)
	}
	path := filepath.Join(root, "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	files := []*compiler.File{file}
	if prelude, loadErr := compiler.LoadPrelude(); loadErr == nil {
		files = append(files, prelude)
	} else {
		t.Fatalf("load standard prelude: %v", loadErr)
	}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex(files)
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	cursor := strings.Index(source, "bool := a.First().ToUpper().") + len("bool := a.First().ToUpper().")
	items := completionLabels(server.contextualCompletions(state, cursor))
	for _, expected := range []string{"ToUpper", "TrimSpace", "ToLower"} {
		if !items[expected] {
			t.Errorf("chained completion omitted string extension %q: %#v", expected, items)
		}
	}
	mapDot := strings.Index(source, "mapped := a.Map(value => value.ToUpper()).") + len("mapped := a.Map(value => value.ToUpper()).")
	mapItems := completionLabels(server.contextualCompletions(state, mapDot))
	for _, expected := range []string{"Each", "First", "Map"} {
		if !mapItems[expected] {
			t.Errorf("Map chain completion omitted slice extension %q: %#v", expected, mapItems)
		}
	}
}

func TestMethodCompletionDoesNotInsertResultAssignment(t *testing.T) {
	root := t.TempDir()
	source := `package main
class Store {
	func Read() (string, error) { return "", nil }
}
func use(store Store) {
	store.
}
`
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatalf("completion sample should parse: %v", err)
	}
	path := filepath.Join(root, "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	cursor := strings.Index(source, "store.") + len("store.")
	for _, item := range server.completions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, cursor),
	}) {
		if item.Label != "Read" {
			continue
		}
		if item.TextEdit == nil || item.TextEdit.NewText != item.InsertText || strings.Contains(item.TextEdit.NewText, " := ") {
			t.Fatalf("method completion should insert only the callable snippet, got %#v", item)
		}
		return
	}
	t.Fatal("method completion omitted Read")
}

func TestNamedStructConstructorCompletionOffersFields(t *testing.T) {
	root := t.TempDir()
	source := `package main
struct User { Name string }
func use() { User( ) }
`
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatalf("struct constructor completion sample should parse: %v", err)
	}
	path := filepath.Join(root, "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	cursor := strings.Index(source, "User(") + len("User(")
	items := completionLabels(server.contextualCompletions(state, cursor))
	if !items["Name"] {
		t.Fatalf("named struct constructor completion omitted its field: %#v", items)
	}
}

func TestFunctionResultTypeCompletionOffersTypesOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/completions\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := "package main\nimport cron \"example.test/completions/cron\"\nfunc quux() str { return \"ok\" }\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatalf("function result completion sample should parse: %v", err)
	}
	mainPath := filepath.Join(root, "main.gpp")
	file.SourcePath = mainPath
	state := &fileState{Path: mainPath, URI: uriForPath(mainPath), Source: source, File: file}
	importedPath := filepath.Join(root, "cron", "cron.go")
	importedSource := "package cron\ntype Scheduler struct{}\nfunc Start() {}\n"
	importedFile, err := compiler.ParseFile(importedPath, importedSource)
	if err != nil {
		t.Fatalf("imported package completion sample should parse: %v", err)
	}
	importedFile.SourcePath = importedPath
	importedState := &fileState{Path: importedPath, URI: uriForPath(importedPath), Source: importedSource, File: importedFile}
	workspace := &workspaceState{
		files: map[string]*fileState{mainPath: state, importedPath: importedState},
		symbols: []symbolInfo{
			{Symbol: compiler.DocSymbol{Kind: compiler.DocFunction, Name: "helper", Package: "main", Exported: false}, Path: mainPath},
			{Symbol: compiler.DocSymbol{Kind: compiler.DocType, Name: "User", Package: "main", Exported: true}, Path: mainPath},
			{Symbol: compiler.DocSymbol{Kind: compiler.DocFunction, Name: "Start", Package: "cron", Exported: true}, Path: importedPath},
			{Symbol: compiler.DocSymbol{Kind: compiler.DocType, Name: "Scheduler", Package: "cron", Exported: true}, Path: importedPath},
		},
	}
	server := &server{workspace: workspace}
	cursor := strings.Index(source, "str {") + len("str")
	labels := completionLabels(server.contextualCompletions(state, cursor))
	for _, expected := range []string{"string", "User", "cron.Scheduler"} {
		if !labels[expected] {
			t.Errorf("function result type completion omitted %q: %#v", expected, labels)
		}
	}
	for _, unexpected := range []string{"helper", "cron.Start"} {
		if labels[unexpected] {
			t.Errorf("function result type completion included non-type %q: %#v", unexpected, labels)
		}
	}
}

func TestAnonymousStructFunctionResultOffersMemberCompletion(t *testing.T) {
	root := t.TempDir()
	source := `package main
func quux() struct { return struct(works: true, count: 3, amount: 2.0, profile: struct(active: true), c: struct(d: 1, e: "ok", meta: struct(enabled: true))) }
func main() { _ = quux().works; _ = quux().profile.active; _ = quux().c.e.ToUpper(); _ = quux().c.meta.enabled; _ = quux().c.e.ToUpper(). }
`
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatalf("anonymous struct member completion sample should parse: %v", err)
	}
	path := filepath.Join(root, "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	files := []*compiler.File{file}
	if prelude, loadErr := compiler.LoadPrelude(); loadErr == nil {
		files = append(files, prelude)
	} else {
		t.Fatalf("load standard prelude: %v", loadErr)
	}
	workspace.docIndex = compiler.BuildDocIndex(files)
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	worksOffset := strings.LastIndex(source, "quux().works") + len("quux().")
	items := completionLabels(server.contextualCompletions(state, worksOffset))
	for _, expected := range []string{"works", "count", "amount", "profile", "c"} {
		if !items[expected] {
			t.Errorf("anonymous struct result completion omitted %q: %#v", expected, items)
		}
	}
	nestedOffset := strings.LastIndex(source, "quux().profile.active") + len("quux().profile.")
	nestedItems := completionLabels(server.contextualCompletions(state, nestedOffset))
	if !nestedItems["active"] {
		t.Fatalf("nested anonymous struct result completion omitted active: %#v", nestedItems)
	}
	innerOffset := strings.LastIndex(source, "quux().c.e") + len("quux().c.")
	innerItems := completionLabels(server.contextualCompletions(state, innerOffset))
	for _, expected := range []string{"d", "e", "meta"} {
		if !innerItems[expected] {
			t.Errorf("nested anonymous struct field completion omitted %q: %#v", expected, innerItems)
		}
	}
	deepOffset := strings.LastIndex(source, "quux().c.meta.enabled") + len("quux().c.meta.")
	deepItems := completionLabels(server.contextualCompletions(state, deepOffset))
	if !deepItems["enabled"] {
		t.Fatalf("three-level anonymous struct completion omitted enabled: %#v", deepItems)
	}
	stringFieldOffset := strings.LastIndex(source, "quux().c.e.ToUpper") + len("quux().c.e.")
	stringFieldItems := completionLabels(server.contextualCompletions(state, stringFieldOffset))
	for _, expected := range []string{"ToUpper", "TrimSpace", "ToLower"} {
		if !stringFieldItems[expected] {
			t.Errorf("nested string field completion omitted %q: %#v", expected, stringFieldItems)
		}
	}
	stringOffset := strings.LastIndex(source, "quux().c.e.ToUpper().") + len("quux().c.e.ToUpper().")
	stringItems := completionLabels(server.contextualCompletions(state, stringOffset))
	for _, expected := range []string{"ToUpper", "TrimSpace", "ToLower"} {
		if !stringItems[expected] {
			t.Errorf("nested string field completion omitted %q: %#v", expected, stringItems)
		}
	}
	fieldOffset := strings.LastIndex(source, "works")
	hoverResult := server.hover(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     positionAtOffset(source, fieldOffset),
	})
	if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, "**field** `works` : `bool`") {
		t.Fatalf("anonymous struct result field hover should show its inferred type: %#v", hoverResult)
	}
	for fieldName, wantType := range map[string]string{"count": "int", "amount": "float64", "e": "string"} {
		fieldOffset := strings.LastIndex(source, "quux().c.e.ToUpper") + len("quux().c.")
		if fieldName != "e" {
			fieldOffset = strings.Index(source, fieldName+":")
		} else {
			fieldOffset = strings.LastIndex(source, ".e.") + 1
		}
		result := server.hover(textDocumentPositionParams{
			TextDocument: textDocumentIdentifier{URI: state.URI},
			Position:     positionAtOffset(source, fieldOffset),
		})
		if result == nil || !strings.Contains(result.Contents.Value, "**field** `"+fieldName+"` : `"+wantType+"`") {
			t.Errorf("anonymous struct field %q hover should show %s: %#v", fieldName, wantType, result)
		}
	}
	literalFieldOffset := strings.Index(source, "works")
	literalHover := server.hover(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     positionAtOffset(source, literalFieldOffset),
	})
	if literalHover == nil || !strings.Contains(literalHover.Contents.Value, "**field** `works` : `bool`") {
		t.Fatalf("anonymous struct literal field hover should show its inferred type: %#v", literalHover)
	}
	cOffset := strings.Index(source, "c: struct")
	cHover := server.hover(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     positionAtOffset(source, cOffset),
	})
	if cHover == nil || !strings.Contains(cHover.Contents.Value, "**field** `c` : `anonymous struct`") || strings.Contains(cHover.Contents.Value, "struct(d:") {
		t.Fatalf("nested anonymous struct hover should show its type without literal details: %#v", cHover)
	}
	nestedFieldOffset := strings.LastIndex(source, "active")
	nestedHover := server.hover(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     positionAtOffset(source, nestedFieldOffset),
	})
	if nestedHover == nil || !strings.Contains(nestedHover.Contents.Value, "**field** `active` : `bool`") {
		t.Fatalf("nested anonymous struct field hover should show its inferred type: %#v", nestedHover)
	}
}

func TestInferredMapLiteralProvidesTypedMapCompletions(t *testing.T) {
	root := t.TempDir()
	source := `package main
func main() {
    prices := map(coffee: 2.5, tea: 1.75)
    prices.
}
`
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatalf("map literal completion sample should parse: %v", err)
	}
	path := filepath.Join(root, "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	prelude, err := compiler.LoadPrelude()
	if err != nil {
		t.Fatalf("load standard prelude: %v", err)
	}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file, prelude})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	offset := strings.Index(source, "prices.\n") + len("prices.")
	items := completionLabels(server.contextualCompletions(state, offset))
	for _, expected := range []string{"Each", "Get", "Keys", "Values", "Has", "Clone"} {
		if !items[expected] {
			t.Errorf("inferred map completion omitted %q (receiver type %q): %#v", expected, server.receiverType(state, "prices", offset), items)
		}
	}
}

func TestMapGetHoverSelectsCallOverloadAndListsAlternatives(t *testing.T) {
	root := t.TempDir()
	source := `package main
func main() {
    values := map(item: "value")
    value, found := values.Get("item")
    fallback := values.Get("missing", "default")
    _, _ = value, found
    _ = fallback
}
`
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatalf("map overload hover sample should parse: %v", err)
	}
	path := filepath.Join(root, "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	prelude, err := compiler.LoadPrelude()
	if err != nil {
		t.Fatalf("load standard prelude: %v", err)
	}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file, prelude})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	for _, testCase := range []struct {
		call        string
		active      string
		alternative string
	}{
		{call: `Get("item")`, active: `Get(key K) (V, bool)`, alternative: `Get(key K, fallback V) V`},
		{call: `Get("missing", "default")`, active: `Get(key K, fallback V) V`, alternative: `Get(key K) (V, bool)`},
	} {
		offset := strings.Index(source, testCase.call)
		if offset < 0 {
			t.Fatalf("test call %q not found", testCase.call)
		}
		hover := server.hover(textDocumentPositionParams{
			TextDocument: textDocumentIdentifier{URI: state.URI},
			Position:     positionAtOffset(source, offset),
		})
		if hover == nil || !strings.Contains(hover.Contents.Value, testCase.active) ||
			!strings.Contains(hover.Contents.Value, "**Other overloads**") || !strings.Contains(hover.Contents.Value, testCase.alternative) {
			t.Errorf("hover for %s should show the active overload and its alternative, got %#v", testCase.call, hover)
		}
	}
}

func TestMapGetInfersConcreteTypesInLocalHoverAndInlayHints(t *testing.T) {
	root := t.TempDir()
	source := `package main
func main() {
    config := map(port: "8080", host: "localhost")
    port, portFound := config.Get("port")
    missingHost, hostFound := config.Get("missing")
    fallbackHost := config.Get("missing", "localhost")
    _, _, _, _ = port, portFound, missingHost, hostFound
    _ = fallbackHost
}
`
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatalf("map local type sample should parse: %v", err)
	}
	path := filepath.Join(root, "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	prelude, err := compiler.LoadPrelude()
	if err != nil {
		t.Fatalf("load standard prelude: %v", err)
	}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file, prelude})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	configHover := server.hover(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     positionAtOffset(source, strings.Index(source, "config :=")+1),
	})
	if configHover == nil || !strings.Contains(configHover.Contents.Value, "`config` : `map[string]string`") {
		t.Fatalf("map local hover should show the concrete inferred type, got %#v", configHover)
	}

	hints := server.inlayHints(state.URI, fullDocumentRange(source))
	labelsByLine := map[int][]string{}
	for _, hint := range hints {
		labelsByLine[hint.Position.Line] = append(labelsByLine[hint.Position.Line], hint.Label)
	}
	for line, expected := range map[int]string{
		2: ": map[string]string",
		3: ": string",
		4: ": string",
		5: ": string",
	} {
		found := false
		for _, label := range labelsByLine[line] {
			if strings.Contains(label, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("line %d should include inlay type %q, got %v", line+1, expected, labelsByLine[line])
		}
	}
}

func TestNativeGoCompletionResolvesMembersOnLocalTypes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "models.go")
	source := `package main
type Widget struct { Name string }
func (Widget) Save() error { return nil }
func use(widget Widget) { _ = widget. }
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[path]
	if state == nil || state.File == nil {
		t.Fatalf("native Go selector source should parse: %#v", workspace.diagnostics[uriForPath(path)])
	}
	server := &server{workspace: workspace}
	cursor := strings.Index(source, "widget.") + len("widget.")
	items := completionLabels(server.completions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, cursor),
	}))
	for _, expected := range []string{"Name", "Save"} {
		if !items[expected] {
			t.Errorf("native Go selector completion omitted %q: %#v", expected, items)
		}
	}
}

func TestNativeGoCompletionResolvesMembersOnSiblingPackageTypes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(root, "models", "widget.go")
	mainPath := filepath.Join(root, "main", "main.go")
	modelSource := "package models\ntype Widget struct { Name string }\nfunc (Widget) Save() error { return nil }\n"
	mainSource := "package main\nimport \"example.com/app/models\"\nfunc use(widget models.Widget) { _ = widget.Name }\n"
	for path, source := range map[string]string{modelPath: modelSource, mainPath: mainSource} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[mainPath]
	if state == nil || state.File == nil {
		t.Fatalf("native Go source should be indexed: %#v", workspace.diagnostics[uriForPath(mainPath)])
	}
	server := &server{workspace: workspace}
	cursor := strings.Index(mainSource, "widget.") + len("widget.")
	if object, _, ok := server.nativeGoSelectionAt(state, "Name", cursor); !ok || object == nil {
		t.Fatal("native Go type information should resolve fields from an imported sibling package")
	}
	fieldPosition := positionAtOffset(mainSource, cursor)
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: fieldPosition})
	if len(definitions) != 1 || definitions[0].URI != uriForPath(modelPath) {
		t.Fatalf("native Go imported field definition should point to the sibling declaration: %#v", definitions)
	}
	parameterPosition := positionAtOffset(mainSource, strings.Index(mainSource, "widget models"))
	parameterHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: parameterPosition})
	if parameterHover == nil || !strings.Contains(parameterHover.Contents.Value, "models.Widget") {
		t.Fatalf("native Go parameter hover should resolve the imported sibling type: %#v", parameterHover)
	}
	fieldHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: fieldPosition})
	if fieldHover == nil || !strings.Contains(fieldHover.Contents.Value, "Name string") {
		t.Fatalf("native Go imported field hover should resolve the sibling declaration: %#v", fieldHover)
	}
	references := server.references(state.URI, fieldPosition, true)
	if len(references) != 2 || references[0].URI != uriForPath(mainPath) || references[1].URI != uriForPath(modelPath) {
		t.Fatalf("native Go imported field references should include both use and declaration: %#v", references)
	}
	edit, err := server.rename(state.URI, fieldPosition, "Title")
	if err != nil || len(edit.Changes[uriForPath(mainPath)]) != 1 || len(edit.Changes[uriForPath(modelPath)]) != 1 {
		t.Fatalf("native Go imported field rename should edit both packages: edits=%#v err=%v", edit.Changes, err)
	}
	items := completionLabels(server.completions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(mainSource, cursor),
	}))
	for _, expected := range []string{"Name", "Save"} {
		if !items[expected] {
			t.Errorf("native Go selector completion omitted imported sibling member %q: %#v", expected, items)
		}
	}
	modelState := workspace.files[modelPath]
	mainState := workspace.files[mainPath]
	modelState.Source = strings.Replace(modelState.Source, "Name", "Title", 1)
	mainState.Source = strings.Replace(mainState.Source, "Name", "Title", 1)
	workspace.analyze()
	updatedCursor := strings.Index(mainState.Source, "widget.") + len("widget.")
	if object, _, ok := server.nativeGoSelectionAt(mainState, "Title", updatedCursor); !ok || object == nil {
		t.Fatal("native Go type information should invalidate when an imported sibling package changes")
	}
}

func TestGoSourceCompletionDisambiguatesConflictingPackageAliases(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "cmd", "main.gpp")
	firstPath := filepath.Join(root, "models", "first", "model.go")
	secondPath := filepath.Join(root, "models", "second", "model.go")
	mainSource := "package main\nimport \"example.com/app/models/first\"\nfunc use() { _ = Wid }\n"
	firstSource := "package models\ntype Widget struct { First bool }\n"
	secondSource := "package models\ntype Widget struct { Second bool }\n"
	for path, source := range map[string]string{mainPath: mainSource, firstPath: firstSource, secondPath: secondSource} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mainState := &fileState{Path: mainPath, URI: uriForPath(mainPath), Source: mainSource, Open: true}
	workspace := &workspaceState{files: map[string]*fileState{mainPath: mainState}, diagnostics: map[string][]Diagnostic{}}
	workspace.setRoots([]string{root})
	workspace.refresh()
	server := &server{workspace: workspace}
	cursor := strings.Index(mainSource, "Wid") + len("Wid")
	var suggestion *CompletionItem
	for _, item := range server.completions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: mainState.URI}, Position: positionAtOffset(mainSource, cursor),
	}) {
		if item.Label == "example_com_app_models_second.Widget" {
			copy := item
			suggestion = &copy
			break
		}
	}
	if suggestion == nil || suggestion.TextEdit == nil || len(suggestion.AdditionalTextEdits) != 1 {
		t.Fatalf("completion should offer the second package using a collision-free alias: %#v", suggestion)
	}
	if !strings.Contains(suggestion.AdditionalTextEdits[0].NewText, `import example_com_app_models_second "example.com/app/models/second"`) {
		t.Fatalf("completion should insert the unique package alias: %#v", suggestion.AdditionalTextEdits)
	}
	updated := mainSource
	for _, edit := range append([]TextEdit{*suggestion.TextEdit}, suggestion.AdditionalTextEdits...) {
		start, end := offsetAt(mainSource, edit.Range.Start), offsetAt(mainSource, edit.Range.End)
		updated = updated[:start] + edit.NewText + updated[end:]
	}
	parsed, err := compiler.ParseFile(mainPath, updated)
	if err != nil {
		t.Fatalf("applying colliding-package completion should produce valid GPP: %v\n%s", err, updated)
	}
	found := false
	for _, imported := range parsed.Imports {
		if imported.Path == "example.com/app/models/second" && imported.Alias == "example_com_app_models_second" {
			found = true
		}
	}
	if !found {
		t.Fatalf("completion import edit lost the disambiguating alias: %#v", parsed.Imports)
	}
	usedAliases := map[string]string{
		"models":                          "example.com/app/models/first",
		"example_com_app_models_second":   "other/path",
		"example_com_app_models_second_2": "another/path",
	}
	if alias := uniqueCompletionImportAlias("example.com/app/models/second", "models", usedAliases); alias != "example_com_app_models_second_3" {
		t.Fatalf("sanitized import aliases should keep searching for a unique identifier, got %q", alias)
	}
}

func TestImportCompletionEditsPreserveBlankLinesAndLineEndings(t *testing.T) {
	root := t.TempDir()
	widgetPath := filepath.Join(root, "foo", "widget.gpp")
	workspace := &workspaceState{symbols: []symbolInfo{{
		Symbol: compiler.DocSymbol{Kind: compiler.DocClass, Name: "Widget", FullName: "foo.bar.Widget", Package: "foo.bar", Signature: "class Widget", Exported: true},
		Path:   widgetPath,
	}}}
	server := &server{workspace: workspace}
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "no existing package gap",
			source: "package main\nfunc use() {}\n",
			want:   "package main\n\nimport foo.bar\n\nfunc use() {}\n",
		},
		{
			name:   "existing package gap",
			source: "package main\n\nfunc use() {}\n",
			want:   "package main\n\nimport foo.bar\n\nfunc use() {}\n",
		},
		{
			name:   "existing imports with CRLF",
			source: "package main\r\n\r\nimport \"fmt\"\r\n\r\nfunc use() {}\r\n",
			want:   "package main\r\n\r\nimport foo.bar\r\nimport \"fmt\"\r\n\r\nfunc use() {}\r\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, "main.gpp")
			file, err := compiler.ParseFile(path, test.source)
			if err != nil {
				t.Fatal(err)
			}
			file.SourcePath = path
			state := &fileState{Path: path, URI: uriForPath(path), Source: test.source, File: file}
			var completion *CompletionItem
			for _, item := range server.importCompletions(state) {
				if item.Label == "bar.Widget" {
					itemCopy := item
					completion = &itemCopy
					break
				}
			}
			if completion == nil || len(completion.AdditionalTextEdits) != 1 {
				t.Fatalf("expected a workspace import completion, got %#v", completion)
			}
			edit := completion.AdditionalTextEdits[0]
			start, end := offsetAt(test.source, edit.Range.Start), offsetAt(test.source, edit.Range.End)
			got := test.source[:start] + edit.NewText + test.source[end:]
			if got != test.want {
				t.Fatalf("completion import edit produced:\n%q\nwant:\n%q", got, test.want)
			}
			if _, err := compiler.ParseFile("completion.gpp", got); err != nil {
				t.Fatalf("completion import edit produced invalid GPP: %v", err)
			}
		})
	}
}

func TestLSPAdvertisesSemanticTokensAndCodeActions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.gpp")
	uri := uriForPath(path)
	var input bytes.Buffer
	writeTestMessage(&input, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"rootUri": uriForPath(root), "capabilities": map[string]any{"window": map[string]any{"workDoneProgress": true}},
	}})
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
	if capabilities == nil || capabilities["codeActionProvider"] == nil || capabilities["semanticTokensProvider"] == nil || capabilities["declarationProvider"] != true {
		t.Fatalf("server did not advertise new LSP support: %#v", capabilities)
	}
	completionProvider, ok := capabilities["completionProvider"].(map[string]any)
	if !ok || completionProvider["resolveProvider"] != true {
		t.Fatalf("completion item resolution should be advertised: %#v", capabilities["completionProvider"])
	}
	codeActionProvider, ok := capabilities["codeActionProvider"].(map[string]any)
	if !ok || codeActionProvider["resolveProvider"] != true {
		t.Fatalf("code action resolution should be advertised: %#v", capabilities["codeActionProvider"])
	}
	rangeFormatting, ok := capabilities["documentRangeFormattingProvider"].(map[string]any)
	if !ok || rangeFormatting["rangesSupport"] != true || capabilities["documentOnTypeFormattingProvider"] == nil {
		t.Fatalf("range, multi-range, and on-type formatting must be advertised when implemented: %#v", capabilities)
	}
	for _, capability := range []string{"inlineCompletionProvider", "inlineValueProvider", "linkedEditingRangeProvider", "colorProvider", "monikerProvider"} {
		if capabilities[capability] == nil {
			t.Fatalf("editor feature %s should be advertised when implemented: %#v", capability, capabilities)
		}
	}
	for _, capability := range []string{"documentLinkProvider", "inlayHintProvider", "codeLensProvider"} {
		provider, ok := capabilities[capability].(map[string]any)
		if !ok || provider["resolveProvider"] != true {
			t.Fatalf("%s resolution should be advertised: %#v", capability, capabilities[capability])
		}
	}
	diagnosticProvider, ok := capabilities["diagnosticProvider"].(map[string]any)
	if !ok || diagnosticProvider["identifier"] != "gpp" || diagnosticProvider["workspaceDiagnostics"] != true {
		t.Fatalf("pull diagnostics should be advertised for documents and workspaces: %#v", capabilities["diagnosticProvider"])
	}
	executeProvider, ok := capabilities["executeCommandProvider"].(map[string]any)
	if !ok || len(executeProvider["commands"].([]any)) < 2 {
		t.Fatalf("server-side commands should be advertised: %#v", capabilities["executeCommandProvider"])
	}
	renameProvider, ok := capabilities["renameProvider"].(map[string]any)
	if !ok || renameProvider["prepareProvider"] != true {
		t.Fatalf("rename should advertise the implemented prepareRename request: %#v", capabilities["renameProvider"])
	}
	workspaceSymbols, ok := capabilities["workspaceSymbolProvider"].(map[string]any)
	if !ok || workspaceSymbols["workDoneProgress"] != true || workspaceSymbols["resolveProvider"] != true {
		t.Fatalf("workspace symbol progress and resolution should be advertised when implemented: %#v", capabilities["workspaceSymbolProvider"])
	}
	provider := capabilities["semanticTokensProvider"].(map[string]any)
	legend := provider["legend"].(map[string]any)
	if got := legend["tokenTypes"].([]any); len(got) != len(semanticTokenTypes) || got[0] != "namespace" || got[len(got)-1] != "decorator" {
		t.Fatalf("server advertised an unstable semantic token legend: %#v", legend)
	}
	fullTokens, ok := provider["full"].(map[string]any)
	if !ok || fullTokens["delta"] != true || provider["range"] != true {
		t.Fatalf("semantic token delta and range handlers must be advertised: %#v", provider)
	}
	if semanticResult == nil || len(semanticResult["data"].([]any)) == 0 {
		t.Fatalf("semantic token request returned no data: %#v", semanticResult)
	}
	if len(actionList) != 1 || actionResult == nil {
		t.Fatalf("code action request returned no quick fix: %#v", actionResult)
	}
}

func TestHoverAndNavigationResolveLocalBindings(t *testing.T) {
	source := "package main\nfunc greet(name string) {\n\tmessage := \"hello\"\n\t_ = message\n\t_ = name\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	server := &server{workspace: workspace}

	messageOffset := strings.Index(source, "_ = message") + len("_ = ")
	messagePosition := positionAtOffset(source, messageOffset)
	hoverResult := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: messagePosition})
	if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, "variable") || !strings.Contains(hoverResult.Contents.Value, "string") {
		t.Fatalf("local variable hover should include its kind and inferred type, got %#v", hoverResult)
	}
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: messagePosition})
	if len(definitions) != 1 {
		t.Fatalf("local variable definition should resolve to one declaration, got %#v", definitions)
	}
	declarationOffset := strings.Index(source, "message :=")
	if got := offsetAt(source, definitions[0].Range.Start); got != declarationOffset {
		t.Fatalf("local definition points at byte %d, want %d", got, declarationOffset)
	}
	refs := server.references(state.URI, messagePosition, false)
	if len(refs) != 1 {
		t.Fatalf("references without declarations should return only the local use, got %#v", refs)
	}

	nameOffset := strings.LastIndex(source, "_ = name") + len("_ = ")
	namePosition := positionAtOffset(source, nameOffset)
	parameterHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: namePosition})
	if parameterHover == nil || !strings.Contains(parameterHover.Contents.Value, "parameter") || !strings.Contains(parameterHover.Contents.Value, "string") {
		t.Fatalf("parameter hover should include its kind and declared type, got %#v", parameterHover)
	}
	parameterRefs := server.references(state.URI, namePosition, true)
	if len(parameterRefs) != 2 {
		t.Fatalf("parameter references should include declaration and use, got %#v", parameterRefs)
	}
	parameterDeclaration := strings.Index(source, "name string")
	parameterDeclarationPosition := positionAtOffset(source, parameterDeclaration)
	parameterDeclarationHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: parameterDeclarationPosition})
	if parameterDeclarationHover == nil || !strings.Contains(parameterDeclarationHover.Contents.Value, "parameter") {
		t.Fatalf("parameter declaration should have hover information, got %#v", parameterDeclarationHover)
	}
	parameterDefinition := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: parameterDeclarationPosition})
	if len(parameterDefinition) != 1 || offsetAt(source, parameterDefinition[0].Range.Start) != parameterDeclaration {
		t.Fatalf("parameter declaration should define itself, got %#v", parameterDefinition)
	}
}

func TestDocumentHighlightsAndFoldingRangesCoverLocalCodeAndComments(t *testing.T) {
	source := "package main\n/* intro\n   details */\nfunc main() {\n\tvalue := 1\n\t_ = value\n}\n"
	file, err := compiler.ParseFile("editor_features.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "editor_features.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	server := &server{workspace: workspace}

	useOffset := strings.LastIndex(source, "value")
	highlights := server.documentHighlights(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     positionAtOffset(source, useOffset),
	})
	if len(highlights) != 2 {
		t.Fatalf("local variable should have declaration and use highlights, got %#v", highlights)
	}
	seenDeclaration, seenUse := false, false
	for _, highlight := range highlights {
		switch highlight.Kind {
		case 3:
			seenDeclaration = true
		case 2:
			seenUse = true
		}
	}
	if !seenDeclaration || !seenUse {
		t.Fatalf("local highlights should mark the declaration and read separately, got %#v", highlights)
	}

	ranges := server.foldingRanges(state.URI)
	seenComment, seenFunction := false, false
	for _, folded := range ranges {
		if folded.Kind == "comment" && folded.StartLine == 1 && folded.EndLine == 2 {
			seenComment = true
		}
		if folded.StartLine == 3 && folded.EndLine == 6 {
			seenFunction = true
		}
	}
	if !seenComment || !seenFunction {
		t.Fatalf("folding ranges should include multiline comments and function bodies, got %#v", ranges)
	}
}

func TestDeclarationRequestUsesDefinitionLocations(t *testing.T) {
	source := "package main\ntype Thing struct{}\nfunc use() { var value Thing; _ = value }\n"
	file, err := compiler.ParseFile("declaration.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "declaration.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	var output bytes.Buffer
	server := &server{workspace: workspace, writer: bufio.NewWriter(&output)}
	offset := strings.Index(source, "Thing;")
	params, err := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": state.URI},
		"position":     positionAtOffset(source, offset),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.handle(rpcRequest{ID: json.RawMessage("1"), Method: "textDocument/declaration", Params: params}); err != nil {
		t.Fatal(err)
	}
	messages := readTestMessages(t, output.Bytes())
	if len(messages) != 1 {
		t.Fatalf("expected one declaration response, got %#v", messages)
	}
	locations, ok := messages[0]["result"].([]any)
	if !ok || len(locations) != 1 {
		t.Fatalf("declaration should resolve to the Thing type: %#v", messages[0])
	}
	location, ok := locations[0].(map[string]any)
	if !ok || location["uri"] != state.URI {
		t.Fatalf("declaration location should point to the source file: %#v", locations[0])
	}
}

func TestLocalHoverPreservesDeclaredTypesAndShowsConstantValues(t *testing.T) {
	source := `package main
func inspect() {
	var value any = "initial"
	const limit = 8
	_ = value
	_ = limit
}
`
	file, err := compiler.ParseFile("hover.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "hover.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}

	valueOffset := strings.Index(source, "_ = value") + len("_ = ")
	valueHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, valueOffset)})
	if valueHover == nil || !strings.Contains(valueHover.Contents.Value, "`any`") || strings.Contains(valueHover.Contents.Value, "= `initial`") {
		t.Fatalf("explicit local type should be preserved without presenting a mutable initializer as its current value: %#v", valueHover)
	}

	limitOffset := strings.Index(source, "_ = limit") + len("_ = ")
	limitHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, limitOffset)})
	if limitHover == nil || !strings.Contains(limitHover.Contents.Value, "**constant**") || !strings.Contains(limitHover.Contents.Value, "= `8`") {
		t.Fatalf("constant hover should include its literal value: %#v", limitHover)
	}
}

func TestHoverResolvesTypeParametersWithoutConfusingSameNamedValueParameters(t *testing.T) {
	source := `package main
class Box[T any] {
	Value T
	func Put[U any](T T, value U) U { return T }
}

func Identity[V any](value V) V { return value }
`
	file, err := compiler.ParseFile("generic-hover.gpp", source)
	if err != nil {
		t.Fatalf("generic sample should parse: %v", err)
	}
	path := filepath.Join(t.TempDir(), "generic-hover.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	server := &server{workspace: workspace}
	hoverAt := func(offset int) string {
		t.Helper()
		result := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, offset)})
		if result == nil {
			t.Fatalf("expected hover at source offset %d", offset)
		}
		return result.Contents.Value
	}

	for _, offset := range []int{
		strings.Index(source, "Box[") + len("Box["),
		strings.Index(source, "Value T") + len("Value "),
		strings.Index(source, "Put[U any](T T") + len("Put[U any](T "),
		strings.Index(source, "Put[U") + len("Put["),
		strings.Index(source, "value U") + len("value "),
		strings.Index(source, "Identity[V") + len("Identity["),
		strings.Index(source, "value V") + len("value "),
	} {
		value := hoverAt(offset)
		if !strings.Contains(value, "**type parameter**") || !strings.Contains(value, "Constraint: `any`") {
			t.Errorf("generic type use at %d should show its kind and constraint, got %q", offset, value)
		}
	}

	valueParameterName := strings.Index(source, "Put[U any](T") + len("Put[U any](")
	if value := hoverAt(valueParameterName); !strings.Contains(value, "**parameter** `T` : `T`") {
		t.Errorf("same-named value parameter declaration should keep parameter hover, got %q", value)
	}
	valueUse := strings.Index(source, "return T") + len("return ")
	if value := hoverAt(valueUse); !strings.Contains(value, "**parameter** `T` : `T`") {
		t.Errorf("same-named value parameter use should not be mistaken for the type parameter, got %q", value)
	}

	typeUse := strings.Index(source, "Value T") + len("Value ")
	declaration := strings.Index(source, "Box[T") + len("Box[")
	params := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, typeUse)}
	definitions := server.definitions(params)
	if len(definitions) != 1 || offsetAt(source, definitions[0].Range.Start) != declaration {
		t.Errorf("generic type definition should point to its declaration, got %#v", definitions)
	}
	if definitions := server.typeDefinitions(params); len(definitions) != 1 || offsetAt(source, definitions[0].Range.Start) != declaration {
		t.Errorf("generic type definition request should return the type parameter declaration, got %#v", definitions)
	}
	references := server.references(state.URI, positionAtOffset(source, typeUse), true)
	if len(references) != 3 {
		t.Errorf("generic type parameter references should omit same-named value references, got %#v", references)
	}
	edit, err := server.rename(state.URI, positionAtOffset(source, typeUse), "Element")
	if err != nil || len(edit.Changes[state.URI]) != 3 {
		t.Errorf("generic type parameter rename should update only its declaration and type uses, changes=%#v err=%v", edit.Changes, err)
	}
	methodTypeUse := strings.Index(source, "value U") + len("value ")
	methodTypeDeclaration := strings.Index(source, "Put[U") + len("Put[")
	methodRefs := server.references(state.URI, positionAtOffset(source, methodTypeUse), true)
	if len(methodRefs) != 3 || offsetAt(source, methodRefs[0].Range.Start) != methodTypeDeclaration {
		t.Errorf("method type parameter references should stay within the method generic scope, got %#v", methodRefs)
	}
}

func TestHoverPrefersPackageSymbolsThatShadowPredeclaredGoNames(t *testing.T) {
	source := "package main\nvar len = 4\nfunc use() { _ = len }\n"
	file, err := compiler.ParseFile("shadow.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shadow.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	position := positionAtOffset(source, strings.LastIndex(source, "len"))
	result := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: position})
	if result == nil || !strings.Contains(result.Contents.Value, "package variable") || !strings.Contains(result.Contents.Value, "var len = 4") {
		t.Fatalf("a declared package symbol should shadow the predeclared builtin in hover, got %#v", result)
	}
}

func TestFlatModulePackageImportsResolveClassesAndInheritedMethods(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/telga\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"common.gpp": `package common
import "gpp/http"
class Server : http.Server {
    // DB stores the database connection.
    DB string
}
`,
		"academies.gpp": `package academies
import "example.test/telga/common"
// Server serves the academies application.
class Server : common.Server {}
`,
		"main.gpp": `package main
import "example.test/telga/academies"
func main() {
    server := academies.Server()
    server.Listen()
    _ = server.DB
    _ = server.PublicDir
    academies.Server(DB: "db", PublicDir: "public").Listen()
}
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	mainPath := filepath.Join(root, "main.gpp")
	state := workspace.files[mainPath]
	if state == nil || state.File == nil {
		t.Fatalf("flat multi-package main file should be indexed: %#v", workspace.diagnostics[uriForPath(mainPath)])
	}
	server := &server{workspace: workspace}
	if packageName, packageDir, ok := server.workspacePackageForImport(state, "example.test/telga/academies"); !ok || packageName != "academies" || packageDir != root {
		t.Fatalf("flat GPP package imports should map by declared package name: got %q %q %v", packageName, packageDir, ok)
	}
	classOffset := strings.Index(files["main.gpp"], "academies.Server") + len("academies.")
	classParams := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(state.Source, classOffset)}
	classHover := server.hover(classParams)
	if classHover == nil || !strings.Contains(classHover.Contents.Value, "**class**") || !strings.Contains(classHover.Contents.Value, "Server serves the academies application") {
		t.Fatalf("imported GPP classes should show hover documentation: %#v", classHover)
	}
	classDefinitions := server.definitions(classParams)
	if len(classDefinitions) != 1 || classDefinitions[0].URI != uriForPath(filepath.Join(root, "academies.gpp")) {
		t.Fatalf("imported GPP classes should navigate to their source declaration: %#v", classDefinitions)
	}
	methodOffset := strings.Index(files["main.gpp"], "Listen")
	methodParams := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(state.Source, methodOffset)}
	methodHover := server.hover(methodParams)
	if methodHover == nil || !strings.Contains(methodHover.Contents.Value, "**method**") || !strings.Contains(methodHover.Contents.Value, "Listen") {
		t.Fatalf("inherited GPP methods should show hover documentation: hover=%#v receiver=%q matches=%#v", methodHover, server.receiverType(state, "server", methodOffset), server.resolveSymbolsAt(state.URI, methodParams.Position, "Listen"))
	}
	methodDefinitions := server.definitions(methodParams)
	if len(methodDefinitions) != 1 || !strings.HasSuffix(pathFromURI(methodDefinitions[0].URI), filepath.Join("gpp", "http", "server.gpp")) {
		t.Fatalf("inherited GPP methods should navigate to their base declaration: %#v", methodDefinitions)
	}
	chainedMethodOffset := strings.LastIndex(files["main.gpp"], "Listen")
	chainedMethodParams := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(state.Source, chainedMethodOffset)}
	chainedHover := server.hover(chainedMethodParams)
	if chainedHover == nil || !strings.Contains(chainedHover.Contents.Value, "Listen") {
		dotOffset := strings.LastIndex(state.Source[:chainedMethodOffset], ".")
		t.Fatalf("methods chained from imported class constructors should resolve hover: %#v receiver=%q resolved=%#v", chainedHover, selectorReceiverBefore(state, dotOffset), server.resolveSymbolsAt(state.URI, chainedMethodParams.Position, "Listen"))
	}
	chainedDefinitions := server.definitions(chainedMethodParams)
	if len(chainedDefinitions) != 1 || !strings.HasSuffix(pathFromURI(chainedDefinitions[0].URI), filepath.Join("gpp", "http", "server.gpp")) {
		t.Fatalf("methods chained from imported class constructors should navigate to their declaration: %#v", chainedDefinitions)
	}
	fieldOffset := strings.LastIndex(files["main.gpp"], "PublicDir:")
	fieldParams := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(state.Source, fieldOffset)}
	fieldHover := server.hover(fieldParams)
	if fieldHover == nil || !strings.Contains(fieldHover.Contents.Value, "PublicDir string") {
		t.Fatalf("inherited named constructor fields should show field documentation: %#v", fieldHover)
	}
	fieldDefinitions := server.definitions(fieldParams)
	if len(fieldDefinitions) != 1 || !strings.HasSuffix(pathFromURI(fieldDefinitions[0].URI), filepath.Join("gpp", "http", "server.gpp")) {
		t.Fatalf("inherited named constructor fields should navigate to their declaration: %#v", fieldDefinitions)
	}
	instanceFieldOffset := strings.Index(files["main.gpp"], "server.DB") + len("server.")
	instanceFieldParams := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(state.Source, instanceFieldOffset)}
	instanceFieldHover := server.hover(instanceFieldParams)
	if instanceFieldHover == nil || !strings.Contains(instanceFieldHover.Contents.Value, "DB stores the database connection") {
		t.Fatalf("inherited instance fields should show hover documentation: %#v", instanceFieldHover)
	}
	instanceFieldDefinitions := server.definitions(instanceFieldParams)
	if len(instanceFieldDefinitions) != 1 || instanceFieldDefinitions[0].URI != uriForPath(filepath.Join(root, "common.gpp")) {
		t.Fatalf("inherited instance fields should navigate to their declaration: %#v", instanceFieldDefinitions)
	}
	publicDirOffset := strings.Index(files["main.gpp"], "server.PublicDir") + len("server.")
	publicDirParams := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(state.Source, publicDirOffset)}
	publicDirHover := server.hover(publicDirParams)
	if publicDirHover == nil || !strings.Contains(publicDirHover.Contents.Value, "PublicDir string") {
		t.Fatalf("inherited standard-library instance fields should show hover information: %#v", publicDirHover)
	}
	publicDirDefinitions := server.definitions(publicDirParams)
	if len(publicDirDefinitions) != 1 || !strings.HasSuffix(pathFromURI(publicDirDefinitions[0].URI), filepath.Join("gpp", "http", "server.gpp")) {
		t.Fatalf("inherited standard-library instance fields should navigate to their source: %#v", publicDirDefinitions)
	}
	constructorCompletionOffset := strings.Index(files["main.gpp"], "academies.Server()") + len("academies.Server(")
	constructorCompletions := completionLabels(server.contextualCompletions(state, constructorCompletionOffset))
	if !constructorCompletions["DB"] || !constructorCompletions["PublicDir"] {
		t.Fatalf("inherited fields should appear as named constructor argument completions: %#v", constructorCompletions)
	}
	instanceCompletionOffset := strings.Index(files["main.gpp"], "server.DB") + len("server.")
	instanceCompletions := completionLabels(server.contextualCompletions(state, instanceCompletionOffset))
	if !instanceCompletions["DB"] || !instanceCompletions["PublicDir"] {
		t.Fatalf("inherited fields should appear in instance member completion: %#v", instanceCompletions)
	}
	for _, privateMember := range []string{"mounts", "shutdownLifecycle", "shutdownResult", "stopTemplateWatcher"} {
		if instanceCompletions[privateMember] {
			t.Errorf("imported class completion leaked private member %q: %#v", privateMember, instanceCompletions)
		}
	}
	completionOffset := strings.LastIndex(files["main.gpp"], "server.Listen") + len("server.Li")
	completions := completionLabels(server.contextualCompletions(state, completionOffset))
	if !completions["Listen"] {
		t.Fatalf("inherited GPP methods should appear in member completion: %#v", completions)
	}
	chainedCompletionOffset := strings.LastIndex(files["main.gpp"], ").Listen") + len(").")
	chainedCompletions := completionLabels(server.contextualCompletions(state, chainedCompletionOffset))
	if !chainedCompletions["Listen"] {
		t.Fatalf("methods should appear in completion after a class constructor call: %#v", chainedCompletions)
	}
}

func TestNativeGoHoverResolvesStandardLibraryFunctionsAndDocumentation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	source := "package main\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\nfunc use(value string) { _ = fmt.Sprintf(\"%s\", value); var builder strings.Builder; builder.Grow(8) }\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[path]
	if state == nil || state.File == nil {
		t.Fatalf("native Go source should be indexed: %#v", workspace.diagnostics[uriForPath(path)])
	}
	server := &server{workspace: workspace}
	offset := strings.Index(source, "Sprintf")
	result := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, offset)})
	if result == nil {
		t.Fatal("standard-library package function should resolve to hover information")
	}
	for _, expected := range []string{"**function**", "fmt.Sprintf", "Sprintf formats", "format string"} {
		if !strings.Contains(result.Contents.Value, expected) {
			t.Errorf("standard-library function hover omitted %q: %s", expected, result.Contents.Value)
		}
	}
	methodOffset := strings.Index(source, "Grow(8)")
	methodHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, methodOffset)})
	if methodHover == nil || !strings.Contains(methodHover.Contents.Value, "**method**") || !strings.Contains(methodHover.Contents.Value, "Grow grows b's capacity") {
		t.Fatalf("standard-library method hover should include its signature and docs, got %#v", methodHover)
	}
}

func TestGPPDefinitionsResolveStandardLibraryImportAliasAndFunction(t *testing.T) {
	source := "package main\nimport stdsql \"database/sql\"\nfunc use() { _, _ = stdsql.Open(\"driver\", \"source\") }\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	aliasOffset := strings.Index(source, "stdsql.Open")
	aliasDefinitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, aliasOffset)})
	if len(aliasDefinitions) != 1 || aliasDefinitions[0].URI != state.URI || aliasDefinitions[0].Range.Start.Line != 1 {
		t.Fatalf("an imported package alias should navigate to its import declaration: %#v", aliasDefinitions)
	}

	openOffset := strings.Index(source, "Open(")
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, openOffset)})
	if len(definitions) != 1 || !strings.Contains(filepath.ToSlash(pathFromURI(definitions[0].URI)), "/src/database/sql/") {
		t.Fatalf("a standard-library function should navigate to its Go source declaration: %#v", definitions)
	}
	declaration, err := os.ReadFile(pathFromURI(definitions[0].URI))
	if err != nil {
		t.Fatalf("standard-library definition target should be readable: %v", err)
	}
	if start := offsetAt(string(declaration), definitions[0].Range.Start); start < 0 || start+len("Open") > len(declaration) || string(declaration[start:start+len("Open")]) != "Open" {
		t.Fatalf("standard-library definition range should select Open: range=%#v", definitions[0].Range)
	}
}

func TestGPPHoverResolvesStandardLibraryImports(t *testing.T) {
	source := "package main\nimport \"fmt\"\nfunc use() { fmt.Println(\"hello\") }\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	offset := strings.Index(source, "Println")
	result := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, offset)})
	if result == nil || !strings.Contains(result.Contents.Value, "**function**") || !strings.Contains(result.Contents.Value, "Println") || !strings.Contains(result.Contents.Value, "Println formats") {
		t.Fatalf("GPP source should resolve standard-library hover with docs, got %#v", result)
	}
}

func TestLambdaParameterHoverUsesInferredElementTypeAndShadowing(t *testing.T) {
	source := `package main
class User { Name string }
func inspect(item string, users []User) {
	users.Any(item => item.Name != "")
	_ = item
}
`
	file, err := compiler.ParseFile("lambda.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "lambda.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}, symbols: []symbolInfo{}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	for _, offset := range []int{strings.Index(source, "item =>"), strings.LastIndex(source, "item.Name")} {
		result := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, offset)})
		if result == nil || !strings.Contains(result.Contents.Value, "**parameter**") || !strings.Contains(result.Contents.Value, "User") {
			t.Fatalf("lambda parameter hover should report the inferred User type at declarations and uses: %#v", result)
		}
	}
	lambdaUse := strings.LastIndex(source, "item.Name")
	lambdaPosition := positionAtOffset(source, lambdaUse)
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: lambdaPosition})
	lambdaDeclaration := strings.Index(source, "item =>")
	if len(definitions) != 1 || offsetAt(source, definitions[0].Range.Start) != lambdaDeclaration {
		t.Fatalf("lambda parameter definition should point to its declaration, got %#v", definitions)
	}
	if references := server.references(state.URI, lambdaPosition, true); len(references) != 2 {
		t.Fatalf("lambda parameter references should include only its declaration and use, got %#v", references)
	}
	edit, err := server.rename(state.URI, lambdaPosition, "user")
	if err != nil || len(edit.Changes[state.URI]) != 2 {
		t.Fatalf("lambda parameter rename should exclude the shadowed outer parameter, edits=%#v err=%v", edit.Changes, err)
	}
	outerUse := strings.LastIndex(source, "_ = item") + len("_ = ")
	if references := server.references(state.URI, positionAtOffset(source, outerUse), true); len(references) != 2 {
		t.Fatalf("outer parameter references should exclude the lambda parameter, got %#v", references)
	}
}

func TestNativeGoHoverResolvesReceiversParametersAndLocals(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "worker.go")
	modelPath := filepath.Join(root, "model.go")
	if err := os.WriteFile(modelPath, []byte("package main\ntype Worker struct{}\ntype Size = int\nfunc makeSize() Size { return 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main
import "context"
func (worker *Worker) Run(ctx context.Context, count int) {
	label := "worker"
	const retries = 3
	size := makeSize()
	_, _, _, _, _ = worker, ctx, label, retries, size
	_ = count
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[path]
	if state == nil || state.File == nil {
		t.Fatalf("native Go file should be parsed for hover: %#v", workspace.diagnostics[uriForPath(path)])
	}
	server := &server{workspace: workspace}
	tests := []struct {
		name string
		at   string
		want []string
	}{
		{name: "receiver", at: "worker, ctx", want: []string{"**receiver**", "*Worker"}},
		{name: "parameter", at: "ctx, label", want: []string{"**parameter**", "context.Context"}},
		{name: "local", at: "label, retries", want: []string{"**variable**", "string"}},
		{name: "local constant", at: "retries, size", want: []string{"**constant**", "int", "3"}},
		{name: "sibling file type", at: "size\n", want: []string{"**variable**", "Size"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			offset := strings.LastIndex(source, test.at)
			if offset < 0 {
				t.Fatalf("test token %q not found", test.at)
			}
			result := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, offset)})
			if result == nil {
				t.Fatal("expected hover")
			}
			for _, expected := range test.want {
				if !strings.Contains(result.Contents.Value, expected) {
					t.Errorf("hover omitted %q: %s", expected, result.Contents.Value)
				}
			}
		})
	}
	modelState := workspace.files[modelPath]
	modelState.Source = "package main\ntype Worker struct{}\ntype Size = int\nfunc makeSize() string { return \"\" }\n"
	sizeOffset := strings.LastIndex(source, "size\n")
	updated := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, sizeOffset)})
	if updated == nil || !strings.Contains(updated.Contents.Value, "string") {
		t.Fatalf("native Go hover cache should refresh after a sibling file changes, got %#v", updated)
	}
}

func TestNativeGoStructFieldsSupportHoverNavigationReferencesAndRename(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "models.go")
	source := `package main
type Person struct {
	// DisplayName is the public-facing name.
	DisplayName string
}
type Invoice struct {
	DisplayName int
}
func show(person Person) { _ = person.DisplayName }
func amount(invoice Invoice) { _ = invoice.DisplayName }
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[path]
	if state == nil || state.File == nil {
		t.Fatalf("native Go source should be indexed: %#v", workspace.diagnostics[uriForPath(path)])
	}
	server := &server{workspace: workspace}
	use := strings.Index(source, "person.DisplayName") + len("person.")
	position := positionAtOffset(source, use)
	hoverResult := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: position})
	if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, "**field**") || !strings.Contains(hoverResult.Contents.Value, "DisplayName string") || !strings.Contains(hoverResult.Contents.Value, "public-facing name") {
		t.Fatalf("Go struct field hover should include type and field documentation: %#v", hoverResult)
	}
	declaration := strings.Index(source, "DisplayName string")
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: position})
	if len(definitions) != 1 || offsetAt(source, definitions[0].Range.Start) != declaration {
		t.Fatalf("Go struct field definition should point to its declaration: %#v", definitions)
	}
	references := server.references(state.URI, position, true)
	if len(references) != 2 {
		t.Fatalf("field references should exclude the same-named field on Invoice: %#v", references)
	}
	edit, err := server.rename(state.URI, position, "PublicName")
	if err != nil || len(edit.Changes[state.URI]) != 2 {
		t.Fatalf("renaming the Go struct field should edit only its declaration and use: edits=%#v err=%v", edit.Changes, err)
	}
	for _, symbol := range server.documentSymbols(state.URI) {
		if symbol.Name != "Person" {
			continue
		}
		for _, child := range symbol.Children {
			if child.Name == "DisplayName" {
				if child.Range.Start.Line != 3 || child.SelectionRange.Start.Line != 3 {
					t.Fatalf("field document symbol should use the exact field line: %#v", child)
				}
				return
			}
		}
	}
	t.Fatal("Person document symbol should contain its DisplayName field")
}

func TestSymbolHoverIncludesKindSignatureAndDocumentation(t *testing.T) {
	source := "package main\n// Represents a person.\nclass Person { Name string }\nenum Status string {\n    // Ready means the resource is available.\n    Ready = \"ready\"\n}\nfunc use(value Person) { _ = value.Name; _ = Status.Ready }\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	tests := []struct {
		name string
		want []string
	}{
		{name: "Person", want: []string{"**class**", "class Person", "Represents a person."}},
		{name: "Name", want: []string{"**field**", "Name string"}},
		{name: "Ready", want: []string{"**enum member**", "Status.Ready = \"ready\"", "Ready means the resource is available."}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			offset := strings.Index(source, test.name)
			if test.name == "Ready" {
				offset = strings.LastIndex(source, "Ready")
			}
			result := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, offset)})
			if result == nil {
				t.Fatalf("expected hover for %s", test.name)
			}
			for _, expected := range test.want {
				if !strings.Contains(result.Contents.Value, expected) {
					t.Errorf("hover for %s omitted %q: %s", test.name, expected, result.Contents.Value)
				}
			}
		})
	}
}

func TestHoverAndNavigationResolveGoTypesAndConstants(t *testing.T) {
	source := `package main
// MaxItems is the default page size.
const MaxItems = 64

// MaxBatch is the maximum batch size.
const (
	MaxBatch = 16
)

// UserID identifies a user.
type UserID int64

func use(id UserID) {
	_ = MaxItems
	_ = MaxBatch
	_ = id
}
`
	path := filepath.Join(t.TempDir(), "native.gpp")
	state := &fileState{Path: path, URI: uriForPath(path), Source: source}
	workspace := &workspaceState{files: map[string]*fileState{path: state}, diagnostics: map[string][]Diagnostic{}}
	workspace.analyze()
	if state.File == nil {
		t.Fatalf("Go declarations should parse in a GPP file: %#v", workspace.diagnostics[state.URI])
	}
	server := &server{workspace: workspace}
	constantUse := strings.LastIndex(source, "MaxItems")
	constantPosition := positionAtOffset(source, constantUse)
	constantHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: constantPosition})
	if constantHover == nil || !strings.Contains(constantHover.Contents.Value, "**constant**") || !strings.Contains(constantHover.Contents.Value, "const MaxItems = 64") || !strings.Contains(constantHover.Contents.Value, "default page size") {
		t.Fatalf("package constant hover should include kind, value, and docs: %#v", constantHover)
	}
	constantDeclaration := strings.Index(source, "const MaxItems") + len("const ")
	constantDefinitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: constantPosition})
	if len(constantDefinitions) != 1 || offsetAt(source, constantDefinitions[0].Range.Start) != constantDeclaration {
		t.Fatalf("constant reference should resolve to the exact declaration name: %#v", constantDefinitions)
	}
	if references := server.references(state.URI, constantPosition, true); len(references) != 2 {
		t.Fatalf("constant references should include declaration and use, got %#v", references)
	}
	groupedUse := strings.LastIndex(source, "MaxBatch")
	groupedPosition := positionAtOffset(source, groupedUse)
	groupedHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: groupedPosition})
	if groupedHover == nil || !strings.Contains(groupedHover.Contents.Value, "**constant**") || !strings.Contains(groupedHover.Contents.Value, "MaxBatch = 16") || !strings.Contains(groupedHover.Contents.Value, "maximum batch") {
		t.Fatalf("grouped Go constant hover should include kind, value, and docs: %#v", groupedHover)
	}
	groupedDeclaration := strings.Index(source, "MaxBatch = 16")
	groupedDefinitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: groupedPosition})
	if len(groupedDefinitions) != 1 || offsetAt(source, groupedDefinitions[0].Range.Start) != groupedDeclaration {
		t.Fatalf("grouped constant should resolve to the exact declaration name: %#v", groupedDefinitions)
	}
	typeUse := strings.LastIndex(source, "UserID")
	typePosition := positionAtOffset(source, typeUse)
	typeHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: typePosition})
	if typeHover == nil || !strings.Contains(typeHover.Contents.Value, "**type**") || !strings.Contains(typeHover.Contents.Value, "type UserID int64") || !strings.Contains(typeHover.Contents.Value, "identifies a user") {
		t.Fatalf("Go type hover should include kind, signature, and docs: %#v", typeHover)
	}
	typeDeclaration := strings.Index(source, "type UserID") + len("type ")
	typeDefinitions := server.typeDefinitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: typePosition})
	if len(typeDefinitions) != 1 || offsetAt(source, typeDefinitions[0].Range.Start) != typeDeclaration {
		t.Fatalf("Go named type should resolve to its declaration: %#v", typeDefinitions)
	}
}

func TestHoverResolvesImplicitExtensionReceiver(t *testing.T) {
	source := "package main\nextend string {\n\tfunc Trimmed() string {\n\t\tresult := this.TrimSpace()\n\t\t_ = result\n\t\treturn result\n\t}\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	position := positionAtOffset(source, strings.Index(source, "this.TrimSpace")+1)
	result := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: position})
	if result == nil || !strings.Contains(result.Contents.Value, "receiver") || !strings.Contains(result.Contents.Value, "string") {
		t.Fatalf("implicit extension receiver hover should show its kind and type, got %#v", result)
	}
	useOffset := strings.Index(source, "_ = result") + len("_ = ")
	usePosition := positionAtOffset(source, useOffset)
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: usePosition})
	declarationOffset := strings.Index(source, "result :=")
	if len(definitions) != 1 || offsetAt(source, definitions[0].Range.Start) != declarationOffset {
		t.Fatalf("extension-local definition should resolve to its declaration, got %#v", definitions)
	}
	if references := server.references(state.URI, usePosition, false); len(references) != 2 {
		t.Fatalf("extension-local references should include both uses, got %#v", references)
	}
}

func TestLocalNavigationRespectsShadowedBlockBindings(t *testing.T) {
	source := "package main\nfunc check() {\n\tvalue := \"outer\"\n\tif true {\n\t\tvalue := 42\n\t\t_ = value\n\t}\n\t_ = value\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}

	innerUse := strings.Index(source, "_ = value") + len("_ = ")
	innerPosition := positionAtOffset(source, innerUse)
	innerHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: innerPosition})
	if innerHover == nil || !strings.Contains(innerHover.Contents.Value, "int") {
		t.Fatalf("inner shadow should hover as the integer binding, got %#v", innerHover)
	}
	innerReferences := server.references(state.URI, innerPosition, true)
	if len(innerReferences) != 2 {
		t.Fatalf("inner binding should have its declaration and one use, got %#v", innerReferences)
	}
	innerRename, err := server.rename(state.URI, innerPosition, "innerValue")
	if err != nil || len(innerRename.Changes[state.URI]) != 2 {
		t.Fatalf("renaming the inner binding should edit only its declaration and use, edits=%#v err=%v", innerRename.Changes, err)
	}
	outerUse := strings.LastIndex(source, "_ = value") + len("_ = ")
	outerPosition := positionAtOffset(source, outerUse)
	outerReferences := server.references(state.URI, outerPosition, true)
	if len(outerReferences) != 2 {
		t.Fatalf("outer binding should have its declaration and one use, got %#v", outerReferences)
	}
	innerDeclaration := strings.Index(source, "value := 42")
	innerDefinition := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: innerPosition})
	if len(innerDefinition) != 1 || offsetAt(source, innerDefinition[0].Range.Start) != innerDeclaration {
		t.Fatalf("inner use should resolve to the inner declaration, got %#v", innerDefinition)
	}
}

func TestCatchBindingsParticipateInHoverAndNavigation(t *testing.T) {
	source := "package main\nfunc recoverValue() {\n\ttry { panic(\"failure\") } catch err {\n\t\t_ = err\n\t}\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	use := strings.LastIndex(source, "_ = err") + len("_ = ")
	position := positionAtOffset(source, use)
	hoverResult := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: position})
	if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, "variable") || !strings.Contains(hoverResult.Contents.Value, "error") {
		t.Fatalf("catch binding hover should include its error type, got %#v", hoverResult)
	}
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: position})
	declaration := strings.Index(source, "catch err") + len("catch ")
	if len(definitions) != 1 || offsetAt(source, definitions[0].Range.Start) != declaration {
		t.Fatalf("catch binding should define its body uses, got %#v", definitions)
	}
	references := server.references(state.URI, position, true)
	if len(references) != 2 {
		t.Fatalf("catch binding should have declaration and use references, got %#v", references)
	}
}

func TestLocalHoverUsesDeclaredFunctionResultTypes(t *testing.T) {
	source := "package main\nclass Person { Name string }\nfunc newPerson() Person { return Person{} }\nfunc countValue() int { return 3 }\nfunc inspect() {\n\tperson := newPerson()\n\tcount := countValue()\n\t_ = person\n\t_ = count\n}\n"
	path := filepath.Join(t.TempDir(), "main.gpp")
	state := &fileState{Path: path, URI: uriForPath(path), Source: source}
	workspace := &workspaceState{files: map[string]*fileState{path: state}, diagnostics: map[string][]Diagnostic{}}
	workspace.analyze()
	if state.File == nil {
		t.Fatalf("source should parse before local type inference: %#v", workspace.diagnostics[state.URI])
	}
	server := &server{workspace: workspace}
	for _, test := range []struct {
		name string
		want string
	}{
		{name: "person", want: "Person"},
		{name: "count", want: "int"},
	} {
		use := strings.LastIndex(source, "_ = "+test.name) + len("_ = ")
		hoverResult := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, use)})
		if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, test.want) {
			t.Errorf("%s hover should use the declared function result type %s, got %#v", test.name, test.want, hoverResult)
		}
	}
}

func TestWorkspaceSymbolsUseExactPathsForDuplicateBasenames(t *testing.T) {
	root := t.TempDir()
	leftPath := filepath.Join(root, "left", "model.gpp")
	rightPath := filepath.Join(root, "right", "model.gpp")
	if err := os.MkdirAll(filepath.Dir(leftPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(rightPath), 0o755); err != nil {
		t.Fatal(err)
	}
	left := &fileState{Path: leftPath, URI: uriForPath(leftPath), Source: "package models\nclass LeftModel {}\n"}
	right := &fileState{Path: rightPath, URI: uriForPath(rightPath), Source: "package models\nclass RightModel {}\n"}
	workspace := &workspaceState{files: map[string]*fileState{leftPath: left, rightPath: right}, diagnostics: map[string][]Diagnostic{}}
	workspace.analyze()
	if left.File == nil || right.File == nil {
		t.Fatalf("both same-basename workspace files should parse: left=%v right=%v", left.File, right.File)
	}
	leftSymbol, rightSymbol := compiler.DocSymbol{}, compiler.DocSymbol{}
	for _, info := range workspace.symbols {
		switch info.Symbol.Name {
		case "LeftModel":
			leftSymbol = info.Symbol
			if info.Path != leftPath {
				t.Fatalf("LeftModel resolved to %q, want %q", info.Path, leftPath)
			}
		case "RightModel":
			rightSymbol = info.Symbol
			if info.Path != rightPath {
				t.Fatalf("RightModel resolved to %q, want %q", info.Path, rightPath)
			}
		}
	}
	if leftSymbol.Name == "" || rightSymbol.Name == "" {
		t.Fatalf("both symbols should be indexed, got left=%#v right=%#v", leftSymbol, rightSymbol)
	}
	if got := workspace.fileForSourcePath("model.gpp"); got != nil {
		t.Fatalf("ambiguous basename must not select an arbitrary file, got %q", got.Path)
	}
	if got := workspace.fileForSourcePath(leftPath); got != left {
		t.Fatalf("exact diagnostic path should resolve to left file, got %#v", got)
	}
	server := &server{workspace: workspace}
	if got := server.documentSymbols(left.URI); len(got) != 1 || got[0].Name != "LeftModel" {
		t.Fatalf("left document symbols must contain only left-file declarations, got %#v", got)
	}
	if got := server.documentSymbols(right.URI); len(got) != 1 || got[0].Name != "RightModel" {
		t.Fatalf("right document symbols must contain only right-file declarations, got %#v", got)
	}
}

func TestCodeActionsOfferSafeFixAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.gpp")
	state := &fileState{Path: path, URI: uriForPath(path), Source: "package main\nfunc main() { _ = \"unfinished\n/* unfinished"}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	quoteOffset := strings.Index(state.Source, "unfinished") + len("unfinished")
	commentOffset := strings.LastIndex(state.Source, "unfinished") + len("unfinished")
	diagnostics := []Diagnostic{
		{Code: diagnosticUnclosedLiteral, Message: "unterminated string literal", Data: map[string]any{"fix": "insert-delimiter", "text": "\"", "offset": quoteOffset}},
		{Code: diagnosticUnclosedComment, Message: "unterminated block comment", Data: map[string]any{"fix": "insert-delimiter", "text": "*/", "offset": commentOffset}},
	}
	params := codeActionParams{TextDocument: textDocumentIdentifier{URI: state.URI}}
	params.Context.Diagnostics = diagnostics
	params.Context.Only = []string{"source.fixAll"}
	actions := server.codeActions(params)
	if len(actions) != 1 || actions[0].Kind != "source.fixAll" {
		t.Fatalf("expected one source.fixAll action, got %#v", actions)
	}
	edits := actions[0].Edit.Changes[state.URI]
	if len(edits) != 2 {
		t.Fatalf("fix-all should combine both safe insertions, got %#v", edits)
	}
}

func TestCodeActionsOrganizePlainImportBlocks(t *testing.T) {
	source := "package main\nimport (\n\t\"zeta/pkg\"\n\talpha \"alpha/pkg\"\n)\nfunc main() {}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	lspServer := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	params := codeActionParams{TextDocument: textDocumentIdentifier{URI: state.URI}}
	for _, filter := range []string{"source", "source.organizeImports"} {
		params.Context.Only = []string{filter}
		actions := lspServer.codeActions(params)
		if len(actions) != 1 || actions[0].Kind != "source.organizeImports" {
			t.Fatalf("filter %q should return the organize imports action, got %#v", filter, actions)
		}
		edits := actions[0].Edit.Changes[state.URI]
		if len(edits) != 1 || !strings.Contains(edits[0].NewText, "alpha \"alpha/pkg\"\n\t\"zeta/pkg\"") {
			t.Fatalf("imports should be sorted by path, got %#v", edits)
		}
	}
}

func TestCodeActionsExtractWholeInitializerSafely(t *testing.T) {
	source := "package main\nfunc main() {\n\tresult := strings.TrimSpace(input)\n\t_ = result\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	lspServer := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	start := strings.Index(source, "strings.TrimSpace")
	end := start + len("strings.TrimSpace(input)")
	params := codeActionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Range: rangeForOffsets(source, start, end)}
	for _, filter := range []string{"refactor", "refactor.extract"} {
		params.Context.Only = []string{filter}
		actions := lspServer.codeActions(params)
		if len(actions) != 1 || actions[0].Kind != "refactor.extract" {
			t.Fatalf("filter %q should offer Extract variable for a whole call initializer, got %#v", filter, actions)
		}
	}
	params.Context.Only = []string{"refactor.extract"}
	actions := lspServer.codeActions(params)
	edits := actions[0].Edit.Changes[state.URI]
	if len(edits) != 2 {
		t.Fatalf("extract variable should replace the expression and insert one declaration, got %#v", edits)
	}
	for left := 0; left < len(edits); left++ {
		for right := left + 1; right < len(edits); right++ {
			if offsetAt(source, edits[left].Range.Start) < offsetAt(source, edits[right].Range.Start) {
				edits[left], edits[right] = edits[right], edits[left]
			}
		}
	}
	updated := source
	for _, edit := range edits {
		startOffset, endOffset := offsetAt(updated, edit.Range.Start), offsetAt(updated, edit.Range.End)
		updated = updated[:startOffset] + edit.NewText + updated[endOffset:]
	}
	if _, err := compiler.ParseFile("main.gpp", updated); err != nil {
		t.Fatalf("extract variable edit should preserve valid GPP syntax: %v\n%s", err, updated)
	}
	if !strings.Contains(updated, "value := strings.TrimSpace(input)\n\tresult := value") {
		t.Fatalf("extract variable edits have the wrong result:\n%s", updated)
	}

	unsafeSource := "package main\nfunc main() {\n\tresult := wrap(strings.TrimSpace(input))\n}\n"
	unsafeFile, err := compiler.ParseFile("unsafe.gpp", unsafeSource)
	if err != nil {
		t.Fatal(err)
	}
	unsafePath := filepath.Join(t.TempDir(), "unsafe.gpp")
	unsafeState := &fileState{Path: unsafePath, URI: uriForPath(unsafePath), Source: unsafeSource, File: unsafeFile}
	unsafeServer := &server{workspace: &workspaceState{files: map[string]*fileState{unsafePath: unsafeState}}}
	innerStart := strings.Index(unsafeSource, "strings.TrimSpace")
	innerEnd := innerStart + len("strings.TrimSpace(input)")
	unsafeParams := codeActionParams{TextDocument: textDocumentIdentifier{URI: unsafeState.URI}, Range: rangeForOffsets(unsafeSource, innerStart, innerEnd)}
	unsafeParams.Context.Only = []string{"refactor.extract"}
	if got := unsafeServer.codeActions(unsafeParams); len(got) != 0 {
		t.Fatalf("nested call extraction could change evaluation order and must not be offered, got %#v", got)
	}
}

func TestExtractVariableAvoidsParameterNames(t *testing.T) {
	source := "package main\nfunc main(value string) {\n\tresult := wrap(input)\n\t_ = result\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	lspServer := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	start := strings.Index(source, "wrap(input)")
	end := start + len("wrap(input)")
	params := codeActionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Range: rangeForOffsets(source, start, end)}
	params.Context.Only = []string{"refactor.extract"}
	actions := lspServer.codeActions(params)
	if len(actions) != 1 || actions[0].Title != "Extract variable value2" {
		t.Fatalf("extract variable should avoid reusing an unused parameter name, got %#v", actions)
	}
	for _, edit := range actions[0].Edit.Changes[state.URI] {
		if strings.Contains(edit.NewText, "value := wrap(input)") {
			t.Fatalf("extract variable would reassign the existing parameter: %#v", edit)
		}
	}
}

func TestHoverIncludesPredeclaredGoBuiltinsAndKeywords(t *testing.T) {
	source := "package main\nconst _ = iota\nfunc check(values []int) {\n\t_ = len(values)\n\t_ = true\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	for _, name := range []string{"len", "true", "iota"} {
		offset := strings.Index(source, name)
		hoverResult := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, offset)})
		if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, "built-in") && !strings.Contains(hoverResult.Contents.Value, "predeclared") {
			t.Errorf("hover for %q should describe a predeclared symbol, got %#v", name, hoverResult)
		}
	}
}

func TestCompletionItemsReplaceCurrentIdentifier(t *testing.T) {
	source := "package main\nfunc main() {\n\tmessage := \"hello\"\n\t_ = mess\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex(append([]*compiler.File{file}, workspace.official...))
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	cursorOffset := strings.Index(source, "mess\n") + len("mess")
	items := server.completions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, cursorOffset)})
	var match *CompletionItem
	for index := range items {
		if items[index].Label == "message" {
			match = &items[index]
			break
		}
	}
	if match == nil || match.TextEdit == nil {
		t.Fatalf("completion should include TrimSpace with an explicit replacement edit, got %#v", match)
	}
	wantStart := strings.Index(source, "mess\n")
	if got := offsetAt(source, match.TextEdit.Range.Start); got != wantStart {
		t.Fatalf("completion edit starts at byte %d, want %d", got, wantStart)
	}
	if got := offsetAt(source, match.TextEdit.Range.End); got != cursorOffset {
		t.Fatalf("completion edit ends at byte %d, want %d", got, cursorOffset)
	}
}

func TestCompletionReplacementRangeSupportsUnicodeIdentifiers(t *testing.T) {
	source := "package main\nfunc main() {\n\tΔelta := 1\n\t_ = Δel\n}\n"
	file, err := compiler.ParseFile("unicode.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "unicode.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	cursorOffset := strings.LastIndex(source, "Δel") + len("Δel")
	items := server.completions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, cursorOffset)})
	var match *CompletionItem
	for index := range items {
		if items[index].Label == "Δelta" {
			match = &items[index]
			break
		}
	}
	if match == nil || match.TextEdit == nil {
		t.Fatalf("completion should resolve a partially typed Unicode identifier, got %#v", match)
	}
	wantStart := strings.LastIndex(source, "Δel")
	if got := offsetAt(source, match.TextEdit.Range.Start); got != wantStart {
		t.Fatalf("Unicode completion edit starts at byte %d, want %d", got, wantStart)
	}
	if got := offsetAt(source, match.TextEdit.Range.End); got != cursorOffset {
		t.Fatalf("Unicode completion edit ends at byte %d, want %d", got, cursorOffset)
	}
}

func TestMemberNavigationAndRenameDisambiguateByReceiverType(t *testing.T) {
	source := "package main\nclass Cat {\n\tName string\n}\nclass Dog {\n\tName string\n}\nfunc show(cat Cat, dog Dog) {\n\t_ = cat.Name\n\t_ = dog.Name\n}\n"
	file, err := compiler.ParseFile("animals.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "animals.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	for _, symbol := range server.documentSymbols(state.URI) {
		if symbol.Name == "Cat" {
			if symbol.Range.Start.Line != positionAtOffset(source, strings.Index(source, "class Cat")).Line || symbol.Range.End.Line <= symbol.SelectionRange.End.Line || source[offsetAt(source, symbol.SelectionRange.Start):offsetAt(source, symbol.SelectionRange.End)] != "Cat" {
				t.Fatalf("document symbol should separate full class range from exact selection range: %#v", symbol)
			}
		}
	}

	catOffset := strings.Index(source, "cat.Name") + len("cat.")
	catPos := positionAtOffset(source, catOffset)
	catDefinitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: catPos})
	catLine := strings.Index(source, "Name string")
	if len(catDefinitions) != 1 || catDefinitions[0].Range.Start.Line != positionAtOffset(source, catLine).Line {
		t.Fatalf("Cat.Name should resolve to Cat.Name's declaration, got %#v", catDefinitions)
	}
	catRefs := server.references(state.URI, catPos, true)
	if len(catRefs) != 2 {
		t.Fatalf("Cat.Name references should exclude Dog.Name, got %#v", catRefs)
	}
	edit, err := server.rename(state.URI, catPos, "DisplayName")
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.Changes[state.URI]) != 2 {
		t.Fatalf("renaming Cat.Name should not edit Dog.Name: %#v", edit)
	}
}

func TestMemberNavigationWinsOverSameNamedLocalBinding(t *testing.T) {
	source := "package main\nclass Person {\n\tName int\n}\nfunc show(person Person, Name string) {\n\t_ = person.Name\n\t_ = Name\n}\n"
	file, err := compiler.ParseFile("person.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "person.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	memberOffset := strings.Index(source, "person.Name") + len("person.") + 1
	position := positionAtOffset(source, memberOffset)
	hover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: position})
	if hover == nil || !strings.Contains(hover.Contents.Value, "**field**") || !strings.Contains(hover.Contents.Value, "Name int") {
		t.Fatalf("selector member should resolve to Person.Name, not the same-named local parameter: %#v", hover)
	}
	declaration := strings.Index(source, "Name int")
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: position})
	if len(definitions) != 1 || offsetAt(source, definitions[0].Range.Start) != declaration {
		t.Fatalf("field definition should point to the member declaration: %#v", definitions)
	}
	references := server.references(state.URI, position, true)
	if len(references) != 2 {
		t.Fatalf("field references should include the declaration and selector, not local Name: %#v", references)
	}
	edit, err := server.rename(state.URI, position, "DisplayName")
	if err != nil || len(edit.Changes[state.URI]) != 2 {
		t.Fatalf("field rename should update only the field and selector: edits=%#v err=%v", edit.Changes, err)
	}
}

func TestEnumMemberNavigationUsesExactDeclarationToken(t *testing.T) {
	source := "package main\nenum Status string {\n\tReady = \"ready\"\n\tOther = \"other\"\n}\nfunc use() { _ = Status.Ready }\n"
	file, err := compiler.ParseFile("status.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	ready := strings.Index(source, "Ready")
	symbol := compiler.DocSymbol{Kind: compiler.DocValue, Name: "Ready", Parent: "Status"}
	span, ok := astSymbolNameSpan(file, symbol)
	if !ok || span.Start != ready || span.End != ready+len("Ready") {
		t.Fatalf("enum member location should point to its declaration token, got %#v want [%d,%d)", span, ready, ready+len("Ready"))
	}
}

func TestReferencesAndRenameStayWithinSameNamedPackagesInDifferentDirectories(t *testing.T) {
	root := t.TempDir()
	leftPath := filepath.Join(root, "left", "model.gpp")
	rightPath := filepath.Join(root, "right", "model.gpp")
	leftSource := "package models\nfunc Load() {}\nfunc Use() { Load() }\n"
	rightSource := "package models\nfunc Load() {}\nfunc Use() { Load() }\n"
	leftFile, err := compiler.ParseFile(leftPath, leftSource)
	if err != nil {
		t.Fatal(err)
	}
	rightFile, err := compiler.ParseFile(rightPath, rightSource)
	if err != nil {
		t.Fatal(err)
	}
	leftFile.SourcePath, rightFile.SourcePath = leftPath, rightPath
	left := &fileState{Path: leftPath, URI: uriForPath(leftPath), Source: leftSource, File: leftFile}
	right := &fileState{Path: rightPath, URI: uriForPath(rightPath), Source: rightSource, File: rightFile}
	leftSymbol := compiler.DocSymbol{Kind: compiler.DocFunction, Name: "Load", FullName: "Load", Package: "models", Signature: "func Load()", SourceFile: leftPath}
	rightSymbol := compiler.DocSymbol{Kind: compiler.DocFunction, Name: "Load", FullName: "Load", Package: "models", Signature: "func Load()", SourceFile: rightPath}
	workspace := &workspaceState{
		files: map[string]*fileState{leftPath: left, rightPath: right},
		symbols: []symbolInfo{
			{Symbol: leftSymbol, Path: leftPath},
			{Symbol: rightSymbol, Path: rightPath},
		},
	}
	server := &server{workspace: workspace}
	use := strings.LastIndex(leftSource, "Load()")
	position := positionAtOffset(leftSource, use)
	references := server.references(left.URI, position, true)
	if len(references) != 2 {
		t.Fatalf("Load references should include only the selected package's declaration and use, got %#v", references)
	}
	for _, reference := range references {
		if reference.URI != left.URI {
			t.Fatalf("Load reference escaped into the sibling package: %#v", reference)
		}
	}
	edit, err := server.rename(left.URI, position, "Fetch")
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.Changes[left.URI]) != 2 || len(edit.Changes[right.URI]) != 0 {
		t.Fatalf("renaming models.Load should edit only the declaring directory: %#v", edit.Changes)
	}
}

func TestLookupDoesNotResolveUnqualifiedNamesAcrossModuleDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/separate\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	leftPath := filepath.Join(root, "left", "use.gpp")
	rightPath := filepath.Join(root, "right", "remote.gpp")
	leftSource := "package models\nfunc Use() { Remote() }\n"
	rightSource := "package models\nfunc Remote() {}\n"
	for path, source := range map[string]string{leftPath: leftSource, rightPath: rightSource} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[leftPath]
	useOffset := strings.LastIndex(state.Source, "Remote")
	server := &server{workspace: workspace}
	params := textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     positionAtOffset(state.Source, useOffset),
	}
	if definitions := server.definitions(params); len(definitions) != 0 {
		t.Fatalf("same-named declared packages in separate module directories must not satisfy unqualified references: %#v", definitions)
	}
	if choices := server.resolveSymbolsAt(state.URI, params.Position, "Remote"); len(choices) != 0 {
		t.Fatalf("unqualified lookup should stay in the source package directory: %#v", choices)
	}
}

func TestLookupStaysInDirectoryWhenCurrentFileHasSyntaxErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/brokenlookup\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	usePath := filepath.Join(root, "left", "use.gpp")
	leftPath := filepath.Join(root, "left", "decl.gpp")
	rightPath := filepath.Join(root, "right", "decl.gpp")
	files := map[string]string{
		usePath:   "package models\nfunc Use() { Local(\n",
		leftPath:  "package models\nfunc Local() {}\n",
		rightPath: "package models\nfunc Local() {}\n",
	}
	for path, source := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[usePath]
	if state.File != nil {
		t.Fatal("fixture should leave the current GPP file syntactically incomplete")
	}
	offset := strings.Index(state.Source, "Local")
	server := &server{workspace: workspace}
	definitions := server.definitions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     positionAtOffset(state.Source, offset),
	})
	if len(definitions) != 1 || definitions[0].URI != uriForPath(leftPath) {
		t.Fatalf("syntax-error fallback should resolve only within the active directory, got %#v", definitions)
	}
}

func TestReferencesReturnStableSourceOrder(t *testing.T) {
	root := t.TempDir()
	files := map[string]*fileState{}
	var parsed []*compiler.File
	for name, source := range map[string]string{
		"a.gpp": "package main\nfunc Load() {}\n",
		"b.gpp": "package main\nfunc UseB() { Load() }\n",
		"c.gpp": "package main\nfunc UseC() { Load() }\n",
	} {
		path := filepath.Join(root, name)
		file, err := compiler.ParseFile(path, source)
		if err != nil {
			t.Fatal(err)
		}
		file.SourcePath = path
		state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
		files[path] = state
		parsed = append(parsed, file)
	}
	workspace := &workspaceState{files: files}
	workspace.docIndex = compiler.BuildDocIndex(parsed)
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	state := files[filepath.Join(root, "b.gpp")]
	position := positionAtOffset(state.Source, strings.Index(state.Source, "Load"))
	for attempt := 0; attempt < 20; attempt++ {
		references := server.references(state.URI, position, true)
		if len(references) != 3 {
			t.Fatalf("expected declaration and two call sites, got %#v", references)
		}
		for index, name := range []string{"a.gpp", "b.gpp", "c.gpp"} {
			want := uriForPath(filepath.Join(root, name))
			if references[index].URI != want {
				t.Fatalf("references are not in stable source order: %#v", references)
			}
		}
	}
}

func TestPrepareRenameReturnsExactSymbolRangeAndRejectsUnknownNames(t *testing.T) {
	source := "package main\nfunc main() {\n\tname := \"Ada\"\n\t_ = name\n\t_ = missing\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	use := strings.LastIndex(source, "_ = name") + len("_ = ")
	prepared := server.prepareRename(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, use)})
	want := rangeForOffsets(source, use, use+len("name"))
	if prepared == nil || prepared.Placeholder != "name" || prepared.Range != want {
		t.Fatalf("prepareRename should return the exact local symbol range, got %#v want %#v", prepared, want)
	}
	unknown := strings.LastIndex(source, "_ = missing") + len("_ = ")
	if got := server.prepareRename(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, unknown)}); got != nil {
		t.Fatalf("unresolved identifier should not be offered for rename, got %#v", got)
	}
	if got := server.prepareRename(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, strings.Index(source, "name :="))}); got == nil {
		t.Fatal("local declaration should be offered for rename")
	}
}

func TestPrepareRenameAllowsLocalsThatShadowBuiltins(t *testing.T) {
	source := `package main
func main() {
	len := 1
	_ = len
}

func useBuiltin() { _ = len([]int{}) }
`
	file, err := compiler.ParseFile("shadow-builtin.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shadow-builtin.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	server := &server{workspace: workspace}

	localUse := strings.Index(source, "_ = len") + len("_ = ")
	prepared := server.prepareRename(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, localUse)})
	if prepared == nil || prepared.Placeholder != "len" {
		t.Fatalf("a local variable shadowing a builtin should be renameable, got %#v", prepared)
	}
	edit, err := server.rename(state.URI, positionAtOffset(source, localUse), "length")
	if err != nil || len(edit.Changes[state.URI]) != 2 {
		t.Fatalf("renaming the local builtin shadow should edit only its declaration and use, got %#v err=%v", edit.Changes, err)
	}
	builtinUse := strings.LastIndex(source, "len([]int{})")
	if got := server.prepareRename(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, builtinUse)}); got != nil {
		t.Fatalf("the predeclared builtin itself should not be offered for rename, got %#v", got)
	}
}

func TestRenameRejectsReservedAndBlankIdentifiers(t *testing.T) {
	source := "package main\nfunc main() {\n\tname := 1\n\t_ = name\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	use := strings.LastIndex(source, "_ = name") + len("_ = ")
	position := positionAtOffset(source, use)
	for _, name := range []string{"if", "class", "true", "nil", "_"} {
		if _, err := server.rename(state.URI, position, name); err == nil {
			t.Errorf("rename to reserved or blank identifier %q should be rejected", name)
		}
	}
}

func TestDidChangeIgnoresStaleDocumentVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "versioned.gpp")
	uri := uriForPath(path)
	source := "package main\nfunc old() {}\n"
	state := &fileState{Path: path, URI: uri, Source: source, Open: true, Version: 4, HasVersion: true}
	workspace := &workspaceState{files: map[string]*fileState{path: state}, diagnostics: map[string][]Diagnostic{}}
	var output bytes.Buffer
	server := &server{workspace: workspace, writer: bufio.NewWriter(&output)}

	stale := []byte(`{"textDocument":{"uri":"` + uri + `","version":3},"contentChanges":[{"text":"package main\nfunc stale() {}\n"}]}`)
	if _, err := server.handle(rpcRequest{Method: "textDocument/didChange", Params: stale}); err != nil {
		t.Fatal(err)
	}
	if state.Source != source || state.Version != 4 {
		t.Fatalf("stale document change overwrote newer source: version=%d source=%q", state.Version, state.Source)
	}

	newer := []byte(`{"textDocument":{"uri":"` + uri + `","version":5},"contentChanges":[{"text":"package main\nfunc fresh() {}\n"}]}`)
	if _, err := server.handle(rpcRequest{Method: "textDocument/didChange", Params: newer}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.Source, "func fresh()") || state.Version != 5 {
		t.Fatalf("newer document change was not applied: version=%d source=%q", state.Version, state.Source)
	}
}

func TestWorkspaceIndexesNativeGoFilesForGPPNavigation(t *testing.T) {
	root := t.TempDir()
	goPath := filepath.Join(root, "native.go")
	gppPath := filepath.Join(root, "main.gpp")
	goSource := "package main\n// Native is defined in an ordinary Go source file.\ntype Native struct { Name string }\n"
	gppSource := "package main\nfunc use() { _ = Native{Name: \"Ada\"} }\n"
	if err := os.WriteFile(goPath, []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gppPath, []byte(gppSource), 0644); err != nil {
		t.Fatal(err)
	}
	gppState := &fileState{Path: gppPath, URI: uriForPath(gppPath), Source: gppSource, Open: true}
	workspace := &workspaceState{files: map[string]*fileState{gppPath: gppState}, diagnostics: map[string][]Diagnostic{}}
	workspace.setRoots([]string{root})
	workspace.refresh()
	server := &server{workspace: workspace}

	use := strings.Index(gppSource, "Native{Name")
	position := positionAtOffset(gppSource, use)
	hoverResult := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: gppState.URI}, Position: position})
	if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, "type Native struct") || !strings.Contains(hoverResult.Contents.Value, "ordinary Go source file") {
		t.Fatalf("GPP hover should resolve a documented type from a .go file, got %#v (diagnostics: %#v)", hoverResult, workspace.diagnostics[gppState.URI])
	}
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: gppState.URI}, Position: position})
	if len(definitions) != 1 || definitions[0].URI != uriForPath(goPath) {
		t.Fatalf("GPP definition should point to the native Go file, got %#v", definitions)
	}
	references := server.references(gppState.URI, position, true)
	if len(references) != 2 {
		t.Fatalf("type references should include the .go declaration and .gpp use, got %#v", references)
	}
	edit, err := server.rename(gppState.URI, position, "NativeRecord")
	if err != nil || len(edit.Changes[uriForPath(goPath)]) != 1 || len(edit.Changes[gppState.URI]) != 1 {
		t.Fatalf("cross-language rename should update the Go declaration and GPP use, edits=%#v err=%v", edit.Changes, err)
	}
	var found bool
	for _, symbol := range server.workspaceSymbols("Native") {
		if symbol.Name == "Native" && symbol.Location.URI == uriForPath(goPath) {
			found = true
		}
	}
	if !found {
		t.Fatal("workspace symbol search should include declarations from .go files")
	}
}

func TestWorkspacePublishesDiagnosticsForNativeGoSources(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "broken.go")
	if err := os.WriteFile(path, []byte("package main\nfunc broken( {\n"), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := &workspaceState{files: map[string]*fileState{}, diagnostics: map[string][]Diagnostic{}}
	workspace.setRoots([]string{root})
	workspace.refresh()
	uri := uriForPath(path)
	if len(workspace.diagnostics[uri]) == 0 {
		t.Fatal("workspace analysis should diagnose syntax errors in native Go files")
	}
	var output bytes.Buffer
	server := &server{workspace: workspace, writer: bufio.NewWriter(&output)}
	server.publishDiagnostics()
	for _, message := range readTestMessages(t, output.Bytes()) {
		if message["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		params, _ := message["params"].(map[string]any)
		if params["uri"] == uri {
			diagnostics, _ := params["diagnostics"].([]any)
			if len(diagnostics) == 0 {
				t.Fatalf("native Go syntax diagnostics should be published to the client: %#v", message)
			}
			return
		}
	}
	t.Fatal("native Go diagnostics were not published")
}

func TestDeletingSourceFileClearsGeneratedDiagnostics(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "removed.gpp")
	source := "package main\nfunc main() {}\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	uri := uriForPath(path)
	workspace := &workspaceState{
		files:       map[string]*fileState{path: {Path: path, URI: uri, Source: source}},
		diagnostics: map[string][]Diagnostic{},
	}
	workspace.setRoots([]string{root})
	var output bytes.Buffer
	server := &server{
		workspace: workspace, writer: bufio.NewWriter(&output),
		backendResults: make(chan backendDiagnosticResult, 1), backendStop: make(chan struct{}),
		backendDiagnostics: map[string][]Diagnostic{uri: {{Message: "stale generated Go error", Source: "go build"}}},
	}
	defer server.stopBackend()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	params, err := json.Marshal(watchedFilesParams{Changes: []watchedFileChange{{URI: uri, Type: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.handle(rpcRequest{Method: "workspace/didChangeWatchedFiles", Params: params}); err != nil {
		t.Fatal(err)
	}
	if _, exists := workspace.files[path]; exists {
		t.Fatal("deleted source file remained in the workspace index")
	}
	if len(server.backendDiagnostics[uri]) != 0 {
		t.Fatalf("generated diagnostics survived source deletion: %#v", server.backendDiagnostics[uri])
	}
	for _, message := range readTestMessages(t, output.Bytes()) {
		if message["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		params, _ := message["params"].(map[string]any)
		if params["uri"] == uri {
			diagnostics, _ := params["diagnostics"].([]any)
			if len(diagnostics) != 0 {
				t.Fatalf("deleted file should publish an empty diagnostic list, got %#v", diagnostics)
			}
			return
		}
	}
	t.Fatal("deleting a source file should publish an empty diagnostics notification")
}

func TestWorkspaceDiagnosticsIncludeGeneratedGoTypeErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/lspgenerated\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "main.gpp")
	source := `class Person {
	Name string
}

func main() {
	name := "Ada"
	_ = name * 2
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	server := &server{
		workspace: workspace, backendResults: make(chan backendDiagnosticResult, 1),
		backendStop: make(chan struct{}),
	}
	defer server.stopBackend()
	server.scheduleGeneratedDiagnostics()
	var result backendDiagnosticResult
	select {
	case result = <-server.backendResults:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for generated-Go diagnostics")
	}
	if !server.acceptBackendDiagnostics(result) {
		t.Fatal("current generated-Go diagnostics result was rejected")
	}
	diagnostics := append([]Diagnostic(nil), workspace.diagnostics[uriForPath(path)]...)
	diagnostics = append(diagnostics, server.backendDiagnostics[uriForPath(path)]...)
	if len(diagnostics) == 0 {
		t.Fatalf("workspace analysis should report Go backend errors from generated GPP code; result=%#v compiler=%#v", server.backendDiagnostics, workspace.diagnostics[uriForPath(path)])
	}
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Source == "go build" && diagnostic.Code == "GPP3000" && strings.Contains(diagnostic.Message, "mismatched types") {
			found = true
			start := strings.Index(source, "name * 2")
			want := rangeForOffsets(source, start, start+len("name"))
			if diagnostic.Range != want {
				t.Fatalf("backend diagnostic should point to the offending identifier in the template expression, got %#v want %#v", diagnostic.Range, want)
			}
		}
	}
	if !found {
		t.Fatalf("expected a source-mapped backend type error, got %#v", diagnostics)
	}
	state := workspace.files[path]
	state.Source = `func main() {
	name := "Ada"
	_ = name
}
`
	state.Open = true
	workspace.analyze()
	server.scheduleGeneratedDiagnostics()
	select {
	case result = <-server.backendResults:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for generated-Go diagnostics after fixing the buffer")
	}
	if !server.acceptBackendDiagnostics(result) {
		t.Fatal("current generated-Go diagnostics result was rejected after fixing the buffer")
	}
	if diagnostics := server.backendDiagnostics[uriForPath(path)]; len(diagnostics) != 0 {
		t.Fatalf("fixing an unsaved buffer should clear backend diagnostics: %#v", diagnostics)
	}
	diskSource, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(diskSource) != source {
		t.Fatal("checking an unsaved buffer must not overwrite the project file")
	}
}

func TestGeneratedDiagnosticsCacheTracksModuleMetadata(t *testing.T) {
	root := t.TempDir()
	moduleFile := filepath.Join(root, "go.mod")
	if err := os.WriteFile(moduleFile, []byte("module example.test/metadata\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "main.gpp")
	if err := os.WriteFile(path, []byte("func main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	server := &server{
		workspace: workspace, backendResults: make(chan backendDiagnosticResult, 1),
		backendStop: make(chan struct{}),
	}
	defer server.stopBackend()
	server.scheduleGeneratedDiagnostics()
	firstGeneration := server.backendGeneration
	server.scheduleGeneratedDiagnostics()
	if server.backendGeneration != firstGeneration {
		t.Fatal("unchanged workspace should reuse the pending backend diagnostics pass")
	}
	var result backendDiagnosticResult
	select {
	case result = <-server.backendResults:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for initial generated-Go build")
	}
	if !server.acceptBackendDiagnostics(result) {
		t.Fatal("initial generated-Go result was stale")
	}
	firstCodegenKey := result.codegenKeys[root]
	if err := os.WriteFile(moduleFile, []byte("module example.test/metadata\n\ngo 1.27\n\nreplace example.test/other => ../other\n"), 0644); err != nil {
		t.Fatal(err)
	}
	server.scheduleGeneratedDiagnostics()
	if server.backendGeneration == firstGeneration {
		t.Fatal("go.mod changes should invalidate generated-Go diagnostics even when source files are unchanged")
	}
	select {
	case result = <-server.backendResults:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for generated-Go build after go.mod changed")
	}
	if !server.acceptBackendDiagnostics(result) {
		t.Fatal("generated-Go result after go.mod changed was stale")
	}
	if result.codegenKeys[root] == firstCodegenKey {
		t.Fatal("go.mod changes should invalidate the codegen cache key")
	}
}

func TestNativeGoTypeDiagnosticsCoverImportedWorkspacePackages(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/diagnostics\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(root, "models", "model.go")
	mainPath := filepath.Join(root, "app", "main.go")
	modelSource := "package models\nfunc Value() int {\n\ttext := \"oops\"\n\tvar count int = text\n\treturn count\n}\n"
	mainSource := "package app\nimport \"example.test/diagnostics/models\"\nfunc main() { _ = models.Value() }\n"
	for path, source := range map[string]string{modelPath: modelSource, mainPath: mainSource} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	languageServer := &server{workspace: workspace}
	diagnostics := languageServer.nativeGoDiagnostics()
	modelDiagnostics := diagnostics[uriForPath(modelPath)]
	if len(modelDiagnostics) != 1 {
		t.Fatalf("expected one native Go type error in the imported model file, got %#v", diagnostics)
	}
	diagnostic := modelDiagnostics[0]
	if diagnostic.Source != "go/types" || diagnostic.Code != "GPP3000" || !strings.Contains(diagnostic.Message, "cannot use text") {
		t.Fatalf("native Go type diagnostic should include stable source, code, and message fields: %#v", diagnostic)
	}
	start := strings.Index(modelSource, "text\n\treturn")
	if start < 0 {
		start = strings.LastIndex(modelSource, "= text") + 2
	}
	want := rangeForOffsets(modelSource, start, start+len("text"))
	if diagnostic.Range != want {
		t.Fatalf("native Go type diagnostic range=%#v, want exact identifier range %#v", diagnostic.Range, want)
	}
	if len(diagnostics[uriForPath(mainPath)]) != 0 {
		t.Fatalf("a valid importing package should not inherit an imported package's diagnostics: %#v", diagnostics[uriForPath(mainPath)])
	}
	modelState := workspace.files[modelPath]
	modelState.Version, modelState.HasVersion = 7, true
	var output bytes.Buffer
	publisher := &server{workspace: workspace, writer: bufio.NewWriter(&output)}
	publisher.publishDiagnostics()
	for _, message := range readTestMessages(t, output.Bytes()) {
		if message["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		params, _ := message["params"].(map[string]any)
		if params["uri"] != uriForPath(modelPath) {
			continue
		}
		if params["version"] != float64(7) {
			t.Fatalf("native Go type diagnostics should retain the document version: %#v", params)
		}
		published, _ := params["diagnostics"].([]any)
		if len(published) != 1 {
			t.Fatalf("native Go type diagnostics should be published to VS Code: %#v", params)
		}
		publishedDiagnostic, _ := published[0].(map[string]any)
		if publishedDiagnostic["code"] != "GPP3000" || publishedDiagnostic["source"] != "go/types" {
			t.Fatalf("published native Go diagnostics should preserve code and source: %#v", publishedDiagnostic)
		}
		return
	}
	t.Fatal("native Go type diagnostics were not published")
}

func TestNativeGoTypeDiagnosticsRefreshAndClearOnDocumentChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	source := "package main\nfunc value() string {\n\tvar text int = \"wrong\"\n\t_ = text\n\treturn \"ok\"\n}\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[path]
	state.Open, state.Version, state.HasVersion = true, 1, true
	languageServer := &server{workspace: workspace}
	publish := func() ([]any, float64) {
		var output bytes.Buffer
		languageServer.writer = bufio.NewWriter(&output)
		languageServer.publishDiagnostics()
		for _, message := range readTestMessages(t, output.Bytes()) {
			if message["method"] != "textDocument/publishDiagnostics" {
				continue
			}
			params, _ := message["params"].(map[string]any)
			if params["uri"] == state.URI {
				diagnostics, _ := params["diagnostics"].([]any)
				version, _ := params["version"].(float64)
				return diagnostics, version
			}
		}
		t.Fatalf("no diagnostics were published for %s", state.URI)
		return nil, 0
	}
	diagnostics, version := publish()
	if len(diagnostics) != 1 || version != 1 {
		t.Fatalf("type error should publish against version 1, got diagnostics=%#v version=%v", diagnostics, version)
	}
	state.Source = "package main\nfunc value() string {\n\tvar text string = \"ok\"\n\treturn text\n}\n"
	state.Version = 2
	workspace.analyze()
	diagnostics, version = publish()
	if len(diagnostics) != 0 || version != 2 {
		t.Fatalf("fixing the type error should clear diagnostics at version 2, got diagnostics=%#v version=%v", diagnostics, version)
	}
}

func TestQualifiedGoImportsDisambiguateNativeGoPackageSymbols(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	nativePath := filepath.Join(root, "native", "native.go")
	otherPath := filepath.Join(root, "other", "other.go")
	mainPath := filepath.Join(root, "main", "main.gpp")
	for path, source := range map[string]string{
		nativePath: "package native\n// Widget is the native package widget.\ntype Widget struct { Name string }\n",
		otherPath:  "package other\ntype Widget int\n",
		mainPath:   "package main\nimport \"example.com/app/native\"\nfunc use() { _ = native.Widget{} }\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mainSource, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	mainState := &fileState{Path: mainPath, URI: uriForPath(mainPath), Source: string(mainSource), Open: true}
	workspace := &workspaceState{files: map[string]*fileState{mainPath: mainState}, diagnostics: map[string][]Diagnostic{}}
	workspace.setRoots([]string{root})
	workspace.refresh()
	server := &server{workspace: workspace}
	widgetOffset := strings.Index(mainState.Source, "Widget")
	position := positionAtOffset(mainState.Source, widgetOffset)

	hoverResult := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: mainState.URI}, Position: position})
	if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, "native package widget") {
		t.Fatalf("qualified Go import hover should select the imported package declaration: %#v", hoverResult)
	}
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: mainState.URI}, Position: position})
	if len(definitions) != 1 || definitions[0].URI != uriForPath(nativePath) {
		t.Fatalf("qualified Go import definition should not select a same-named type in another package: %#v", definitions)
	}
	links := server.documentLinks(mainState.URI)
	if len(links) != 1 || server.resolveDocumentLink(links[0]).Target != uriForPath(nativePath) {
		t.Fatalf("ordinary module imports should link to their workspace package: %#v", links)
	}
	completionOffset := strings.Index(mainState.Source, "native.Widget") + len("native.")
	var widgetCompletion *CompletionItem
	for _, item := range server.contextualCompletions(mainState, completionOffset) {
		if item.Label == "Widget" {
			copy := item
			widgetCompletion = &copy
			break
		}
	}
	if widgetCompletion == nil || !strings.Contains(widgetCompletion.Detail, "struct") {
		t.Fatalf("qualified Go import completion should use the imported package's Widget declaration: %#v", widgetCompletion)
	}
}

func TestDiagnosticsUsePreciseUTF16RangesWhenCompilerSpanIsAvailable(t *testing.T) {
	source := "package main\n// 🌟\nfunc main() {\n\tbad := value\n}\n"
	start := strings.Index(source, "value")
	span := compiler.Span{Start: start, End: start + len("value"), Line: 4, Column: len("\tbad := ") + 1}
	err := compiler.NewDiagnosticError("main.gpp", span, fmt.Errorf("undefined: value"))
	diagnostic := diagnosticFromError("file:///main.gpp", source, err)
	want := rangeForOffsets(source, start, start+len("value"))
	if diagnostic.Range != want {
		t.Fatalf("diagnostic range=%#v, want precise UTF-16 source range %#v", diagnostic.Range, want)
	}
}

func TestDiagnosticsAtLineBreakUseZeroWidthEndOfLineRange(t *testing.T) {
	for _, source := range []string{"abc\nnext", "abc\r\nnext"} {
		err := compiler.NewDiagnosticError("main.gpp", compiler.Span{Line: 1, Column: 4}, fmt.Errorf("expected expression"))
		diagnostic := diagnosticFromError("file:///main.gpp", source, err)
		want := Range{Start: Position{Line: 0, Character: 3}, End: Position{Line: 0, Character: 3}}
		if diagnostic.Range != want {
			t.Errorf("line-ending diagnostic range for %q = %#v, want zero-width EOL range %#v", source, diagnostic.Range, want)
		}
	}
}

func TestWorkspacePublishesIndependentClassDiagnosticsTogether(t *testing.T) {
	root := t.TempDir()
	firstSource := "package main\nclass GppClass {}\n"
	secondSource := "package main\nclass GppField {}\n"
	firstPath := filepath.Join(root, "first.gpp")
	secondPath := filepath.Join(root, "second.gpp")
	first := &fileState{Path: firstPath, URI: uriForPath(firstPath), Source: firstSource}
	second := &fileState{Path: secondPath, URI: uriForPath(secondPath), Source: secondSource}
	workspace := &workspaceState{files: map[string]*fileState{firstPath: first, secondPath: second}, diagnostics: map[string][]Diagnostic{}}
	workspace.analyze()

	for _, testCase := range []struct {
		state *fileState
		want  string
	}{{first, "GppClass"}, {second, "GppField"}} {
		state := testCase.state
		diagnostics := workspace.diagnostics[state.URI]
		if len(diagnostics) != 1 {
			t.Errorf("expected one class diagnostic for %s, got %#v", state.Path, diagnostics)
			continue
		}
		if !strings.Contains(diagnostics[0].Message, testCase.want) {
			t.Errorf("diagnostic for %s points to the wrong class: %#v", state.Path, diagnostics[0])
		}
	}
}

func TestWorkspacePublishesDiagnosticsAcrossValidationKinds(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.gpp")
	source := `package main
class GppClass {}
extend string { func First() {} }
extend string { func First() {} }
`
	state := &fileState{Path: path, URI: uriForPath(path), Source: source}
	workspace := &workspaceState{files: map[string]*fileState{path: state}, diagnostics: map[string][]Diagnostic{}}
	workspace.analyze()
	diagnostics := workspace.diagnostics[state.URI]
	if len(diagnostics) != 2 {
		t.Fatalf("expected independent class and extension diagnostics, got %#v", diagnostics)
	}
	messages := map[string]bool{}
	for _, diagnostic := range diagnostics {
		messages[diagnostic.Message] = true
		if diagnostic.Range.Start == (Position{}) && diagnostic.Range.End == (Position{}) {
			t.Errorf("diagnostic should have a source range: %#v", diagnostic)
		}
	}
	if !messages["class name GppClass is reserved for GPP introspection metadata"] || !messages["extension method First for string is declared more than once with parameter types "] {
		t.Fatalf("missing class or duplicate-extension diagnostic: %#v", messages)
	}
}

func TestWorkspacePublishesAllUnknownAnnotationDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.gpp")
	source := `package main
class Employee {
    Name string @{Required}
    Age int @{Positive}
}
`
	state := &fileState{Path: path, URI: uriForPath(path), Source: source}
	workspace := &workspaceState{files: map[string]*fileState{path: state}, diagnostics: map[string][]Diagnostic{}}
	workspace.analyze()
	diagnostics := workspace.diagnostics[state.URI]
	if len(diagnostics) != 2 {
		t.Fatalf("expected diagnostics for both unknown annotations, got %#v", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "Required") || !strings.Contains(diagnostics[1].Message, "Positive") {
		t.Fatalf("annotation diagnostics should preserve source order and names: %#v", diagnostics)
	}
}

func TestDuplicateDeclarationDiagnosticIncludesOriginalLocation(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.gpp")
	secondPath := filepath.Join(root, "second.gpp")
	firstSource := "package main\nclass Duplicate {}\n"
	secondSource := "package main\nclass Duplicate {}\n"
	first := &fileState{Path: firstPath, URI: uriForPath(firstPath), Source: firstSource}
	second := &fileState{Path: secondPath, URI: uriForPath(secondPath), Source: secondSource}
	workspace := &workspaceState{files: map[string]*fileState{firstPath: first, secondPath: second}, diagnostics: map[string][]Diagnostic{}}
	workspace.analyze()
	diagnostics := workspace.diagnostics[second.URI]
	if len(diagnostics) != 1 {
		t.Fatalf("duplicate declaration should produce one diagnostic on the second file, got %#v", workspace.diagnostics)
	}
	if len(diagnostics[0].RelatedInformation) != 1 {
		t.Fatalf("duplicate declaration should include a structured location for the first declaration: %#v", diagnostics[0])
	}
	related := diagnostics[0].RelatedInformation[0]
	if related.Location.URI != first.URI || related.Message != "first declaration is here" {
		t.Fatalf("related information points at the wrong declaration: %#v", related)
	}
}

func TestImportLinksSelectionRangesAndInlayHints(t *testing.T) {
	root := t.TempDir()
	mainSource := "package main\nimport foo.bar\nfunc main() {\n\tname := \"Ada\"\n\t_ = name\n}\n"
	mainFile, err := compiler.ParseFile("main.gpp", mainSource)
	if err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "main.gpp")
	mainFile.SourcePath = mainPath
	mainState := &fileState{Path: mainPath, URI: uriForPath(mainPath), Source: mainSource, File: mainFile}
	otherSource := "package foo.bar\nclass Person {}\n"
	otherFile, err := compiler.ParseFile("person.gpp", otherSource)
	if err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(root, "person.gpp")
	otherFile.SourcePath = otherPath
	otherState := &fileState{Path: otherPath, URI: uriForPath(otherPath), Source: otherSource, File: otherFile}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{mainPath: mainState, otherPath: otherState}}}

	links := server.documentLinks(mainState.URI)
	if len(links) != 1 || links[0].Target != "" || links[0].Data == nil {
		t.Fatalf("logical package import should return a deferred workspace link, got %#v", links)
	}
	resolvedLink := server.resolveDocumentLink(links[0])
	if resolvedLink.Target != otherState.URI || resolvedLink.Tooltip != "Package foo.bar" {
		t.Fatalf("document link resolution should add target and tooltip, got %#v", resolvedLink)
	}
	importOffset := strings.Index(mainSource, "foo.bar") + len("foo.")
	importHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: mainState.URI}, Position: positionAtOffset(mainSource, importOffset)})
	if importHover == nil || !strings.Contains(importHover.Contents.Value, "GPP package") || !strings.Contains(importHover.Contents.Value, "foo.bar") {
		t.Fatalf("logical package import should provide package hover, got %#v", importHover)
	}

	nameOffset := strings.Index(mainSource, "name :=")
	hints := server.inlayHints(mainState.URI, fullDocumentRange(mainSource))
	if len(hints) != 1 || hints[0].Label != ": string" || hints[0].Position != rangeForOffsets(mainSource, nameOffset+len("name"), nameOffset+len("name")).Start {
		t.Fatalf("local inferred-type hint is incorrect: %#v", hints)
	}
	if hints[0].Data == nil || hints[0].Tooltip != nil {
		t.Fatalf("inlay hint details should be deferred with resolvable data: %#v", hints[0])
	}
	resolvedHint := server.resolveInlayHint(hints[0])
	if resolvedHint.Tooltip == nil || !strings.Contains(resolvedHint.Tooltip.(string), "Inferred string") {
		t.Fatalf("inlay hint resolution should provide its inferred type tooltip: %#v", resolvedHint)
	}
	selection := server.selectionRanges(mainState.URI, []Position{positionAtOffset(mainSource, nameOffset+1)})
	if len(selection) != 1 || selection[0].Range != rangeForOffsets(mainSource, nameOffset, nameOffset+len("name")) || selection[0].Parent == nil || selection[0].Parent.Parent == nil {
		t.Fatalf("selection range should nest token, line, and document ranges: %#v", selection)
	}
}

func TestEditorResolveItemsSurviveLSPJSONRoundTrip(t *testing.T) {
	root := t.TempDir()
	mainSource := "package main\nimport foo.bar\nfunc inspect() {\n    name := \"Ada\"\n    _ = name\n}\nfunc target() {}\nfunc caller() { target() }\n"
	mainPath := filepath.Join(root, "main.gpp")
	mainFile, err := compiler.ParseFile(mainPath, mainSource)
	if err != nil {
		t.Fatal(err)
	}
	mainFile.SourcePath = mainPath
	mainState := &fileState{Path: mainPath, URI: uriForPath(mainPath), Source: mainSource, File: mainFile}
	otherPath := filepath.Join(root, "person.gpp")
	otherSource := "package foo.bar\nclass Person {}\n"
	otherFile, err := compiler.ParseFile(otherPath, otherSource)
	if err != nil {
		t.Fatal(err)
	}
	otherFile.SourcePath = otherPath
	otherState := &fileState{Path: otherPath, URI: uriForPath(otherPath), Source: otherSource, File: otherFile}
	workspace := &workspaceState{files: map[string]*fileState{mainPath: mainState, otherPath: otherState}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{mainFile, otherFile})
	workspace.symbols = workspace.collectSymbols()
	var output bytes.Buffer
	server := &server{workspace: workspace, writer: bufio.NewWriter(&output)}
	request := func(method string, id int, params any) map[string]any {
		t.Helper()
		encoded, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.handle(rpcRequest{ID: json.RawMessage(strconv.Itoa(id)), Method: method, Params: encoded}); err != nil {
			t.Fatalf("%s request failed: %v", method, err)
		}
		messages := readTestMessages(t, output.Bytes())
		for _, message := range messages {
			if message["id"] == float64(id) {
				result, _ := message["result"].(map[string]any)
				return result
			}
		}
		return nil
	}
	arrayResult := func(method string, id int, params any) []any {
		t.Helper()
		encoded, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := server.handle(rpcRequest{ID: json.RawMessage(strconv.Itoa(id)), Method: method, Params: encoded}); err != nil {
			t.Fatalf("%s request failed: %v", method, err)
		}
		messages := readTestMessages(t, output.Bytes())
		for _, message := range messages {
			if message["id"] == float64(id) {
				result, _ := message["result"].([]any)
				return result
			}
		}
		return nil
	}
	textDocument := map[string]any{"textDocument": map[string]any{"uri": mainState.URI}}
	links := arrayResult("textDocument/documentLink", 1, textDocument)
	if len(links) != 1 {
		t.Fatalf("document link request should return one deferred item: %#v", links)
	}
	link := links[0].(map[string]any)
	if link["data"] == nil || link["target"] != nil {
		t.Fatalf("document link response should preserve its resolve data and defer the target: %#v", link)
	}
	resolvedLink := request("documentLink/resolve", 2, link)
	if resolvedLink["target"] != otherState.URI || resolvedLink["tooltip"] != "Package foo.bar" {
		t.Fatalf("document link resolve should survive the JSON round trip: %#v", resolvedLink)
	}
	lineRange := fullDocumentRange(mainSource)
	hints := arrayResult("textDocument/inlayHint", 3, map[string]any{"textDocument": textDocument["textDocument"], "range": lineRange})
	if len(hints) != 1 {
		t.Fatalf("inlay hint request should return one deferred item: %#v", hints)
	}
	hint := hints[0].(map[string]any)
	if hint["data"] == nil || hint["tooltip"] != nil {
		t.Fatalf("inlay hint should carry opaque resolver data: %#v", hint)
	}
	resolvedHint := request("inlayHint/resolve", 4, hint)
	if !strings.Contains(resolvedHint["tooltip"].(string), "Inferred string") {
		t.Fatalf("inlay hint resolve should survive the JSON round trip: %#v", resolvedHint)
	}
	lenses := arrayResult("textDocument/codeLens", 5, textDocument)
	if len(lenses) == 0 {
		t.Fatal("code lens request should return deferred reference lenses")
	}
	lens := lenses[0].(map[string]any)
	if lens["data"] == nil || lens["command"] != nil {
		t.Fatalf("code lens should carry opaque resolver data and defer its command: %#v", lens)
	}
	resolvedLens := request("codeLens/resolve", 6, lens)
	if resolvedLens["command"] == nil {
		t.Fatalf("code lens resolve should add its command after the JSON round trip: %#v", resolvedLens)
	}
}

func TestSelectionRangesExpandThroughNestedExpressions(t *testing.T) {
	source := "package main\nfunc main() {\n\tresult := strings.TrimSpace(name)\n\t_ = result\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	nameOffset := strings.Index(source, "name)")
	selection := server.selectionRanges(state.URI, []Position{positionAtOffset(source, nameOffset+1)})
	if len(selection) != 1 {
		t.Fatalf("expected one selection chain, got %#v", selection)
	}
	var selected []string
	for current := &selection[0]; current != nil; current = current.Parent {
		selected = append(selected, source[offsetAt(source, current.Range.Start):offsetAt(source, current.Range.End)])
	}
	want := []string{"name", "strings.TrimSpace(name)", "result := strings.TrimSpace(name)"}
	if len(selected) < len(want)+1 || selected[len(selected)-1] != source {
		t.Fatalf("selection chain should include nested syntax and end at the document, got %#v", selected)
	}
	for index := range want {
		if selected[index] != want[index] {
			t.Fatalf("selection range %d = %q, want %q (chain %#v)", index, selected[index], want[index], selected)
		}
	}
}

func TestLogicalPackageDocumentLinksStayWithinTheImportingModule(t *testing.T) {
	root := t.TempDir()
	oneRoot := filepath.Join(root, "one")
	twoRoot := filepath.Join(root, "two")
	for dir, module := range map[string]string{oneRoot: "example.com/one", twoRoot: "example.com/two"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.27\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mainPath := filepath.Join(oneRoot, "main.gpp")
	onePackagePath := filepath.Join(oneRoot, "foo.gpp")
	twoPackagePath := filepath.Join(twoRoot, "foo.gpp")
	files := map[string]string{
		mainPath:       "package main\nimport foo.bar\nfunc use() {}\n",
		onePackagePath: "package foo.bar\nclass OnePackage {}\n",
		twoPackagePath: "package foo.bar\nclass TwoPackage {}\n",
	}
	for path, source := range files {
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[mainPath]
	if state == nil || state.File == nil {
		t.Fatalf("importing file should parse: %#v", workspace.diagnostics[uriForPath(mainPath)])
	}
	server := &server{workspace: workspace}
	links := server.documentLinks(state.URI)
	if len(links) != 1 || server.resolveDocumentLink(links[0]).Target != uriForPath(onePackagePath) {
		t.Fatalf("logical import link should point into the importing module, got %#v", links)
	}
}

func TestInlayHintsUseFunctionResultTypes(t *testing.T) {
	source := "package main\nclass Person {}\nfunc makePerson() Person { return Person{} }\nfunc main() {\n\tperson := makePerson()\n\t_ = person\n}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	hints := server.inlayHints(state.URI, fullDocumentRange(source))
	if len(hints) != 1 || hints[0].Label != ": Person" {
		t.Fatalf("function-result initializer should show the returned type, got %#v", hints)
	}
}

func TestInlayHintsHonorZeroWidthRangeAtDocumentStart(t *testing.T) {
	source := "package main\nfunc main() {\n\tvalue := 42\n\t_ = value\n}\n"
	file, err := compiler.ParseFile("inlay-range.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "inlay-range.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}

	hints := server.inlayHints(state.URI, Range{})
	if len(hints) != 0 {
		t.Fatalf("a zero-width range at the document start must not return hints from later lines: %#v", hints)
	}
}

func TestNativeGoInlayHintsUseResolvedTypesAndHonorRange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	source := `package main
func use() {
	name := "Ada"
	count := 3
	_, _ = name, count
}
`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[path]
	if state == nil || state.File == nil {
		t.Fatalf("native Go file should parse for inlay hints: %#v", workspace.diagnostics[uriForPath(path)])
	}
	server := &server{workspace: workspace}
	all := server.inlayHints(state.URI, fullDocumentRange(source))
	if len(all) != 2 || all[0].Label != ": string" || all[1].Label != ": int" {
		t.Fatalf("native Go short declarations should have inferred type hints: %#v", all)
	}
	lineStart := strings.Index(source, "\tname")
	lineEnd := lineStart + strings.Index(source[lineStart:], "\n")
	lineRange := Range{Start: positionAtOffset(source, lineStart), End: positionAtOffset(source, lineEnd)}
	filtered := server.inlayHints(state.URI, lineRange)
	if len(filtered) != 1 || filtered[0].Label != ": string" {
		t.Fatalf("native Go inlay hints should respect the requested range: %#v", filtered)
	}
}

func TestTypeDefinitionAndImplementationsUseWorkspaceInheritance(t *testing.T) {
	source := "package main\nclass Base {\n\tfunc Save() {}\n}\nclass Child : Base {\n\tfunc Save() {}\n}\nfunc use(value Base) {}\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	baseOffset := strings.Index(source, "class Base") + len("class ")
	basePos := positionAtOffset(source, baseOffset)
	implementations := server.implementations(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: basePos})
	childLine := positionAtOffset(source, strings.Index(source, "class Child")).Line
	if len(implementations) != 1 || implementations[0].Range.Start.Line != childLine {
		t.Fatalf("Base implementations should include Child, got %#v", implementations)
	}
	methodOffset := strings.Index(source, "Save")
	methodImplementations := server.implementations(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, methodOffset)})
	childMethodLine := positionAtOffset(source, strings.LastIndex(source, "Save")).Line
	if len(methodImplementations) != 1 || methodImplementations[0].Range.Start.Line != childMethodLine {
		t.Fatalf("Base.Save implementations should include Child.Save, got %#v", methodImplementations)
	}
	parameterTypeOffset := strings.LastIndex(source, "Base")
	types := server.typeDefinitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, parameterTypeOffset)})
	if len(types) != 1 || types[0].Range.Start.Line != basePos.Line {
		t.Fatalf("parameter type definition should resolve to Base, got %#v", types)
	}
}

func TestTypeDefinitionResolvesQualifiedLocalTypesThroughImports(t *testing.T) {
	root := t.TempDir()
	mainSource := "package main\nimport foo.bar\nfunc main() {\n\tperson := &bar.Person{}\n\t_ = person\n\tpeople := []bar.Person{}\n\t_ = people\n}\n"
	modelSource := "package foo.bar\nclass Person {}\n"
	otherModelSource := "package foo.baz\nclass Person {}\n"
	mainFile, err := compiler.ParseFile("main.gpp", mainSource)
	if err != nil {
		t.Fatal(err)
	}
	modelFile, err := compiler.ParseFile("person.gpp", modelSource)
	if err != nil {
		t.Fatal(err)
	}
	otherModelFile, err := compiler.ParseFile("other-person.gpp", otherModelSource)
	if err != nil {
		t.Fatal(err)
	}
	mainPath, modelPath, otherModelPath := filepath.Join(root, "main.gpp"), filepath.Join(root, "person.gpp"), filepath.Join(root, "other-person.gpp")
	mainFile.SourcePath, modelFile.SourcePath, otherModelFile.SourcePath = mainPath, modelPath, otherModelPath
	mainState := &fileState{Path: mainPath, URI: uriForPath(mainPath), Source: mainSource, File: mainFile}
	modelState := &fileState{Path: modelPath, URI: uriForPath(modelPath), Source: modelSource, File: modelFile}
	otherModelState := &fileState{Path: otherModelPath, URI: uriForPath(otherModelPath), Source: otherModelSource, File: otherModelFile}
	workspace := &workspaceState{files: map[string]*fileState{mainPath: mainState, modelPath: modelState, otherModelPath: otherModelState}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{mainFile, modelFile, otherModelFile})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	for _, variable := range []string{"person", "people"} {
		use := strings.LastIndex(mainSource, "_ = "+variable) + len("_ = ")
		definitions := server.typeDefinitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: mainState.URI}, Position: positionAtOffset(mainSource, use)})
		if len(definitions) != 1 || definitions[0].URI != modelState.URI {
			t.Fatalf("local %s type definition should resolve to the imported Person, not a same-named type in another package: %#v", variable, definitions)
		}
	}
}

func TestTypeDefinitionResolvesNativeGoTypesThroughImportAliases(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/native\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(root, "models", "person.go")
	otherPath := filepath.Join(root, "other", "person.go")
	mainPath := filepath.Join(root, "app", "main.gpp")
	modelSource := "package models\ntype Person struct { Name string }\n"
	otherSource := "package other\ntype Person struct { Code int }\n"
	mainSource := "package app\nimport people \"example.test/native/models\"\nfunc use() { person := &people.Person{}; _ = person }\n"
	for path, source := range map[string]string{modelPath: modelSource, otherPath: otherSource, mainPath: mainSource} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[mainPath]
	if state == nil || state.File == nil {
		t.Fatalf("GPP importing file should parse: %#v", workspace.diagnostics[uriForPath(mainPath)])
	}
	server := &server{workspace: workspace}
	useOffset := strings.LastIndex(mainSource, "_ = person") + len("_ = ")
	definitions := server.typeDefinitions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     positionAtOffset(mainSource, useOffset),
	})
	if len(definitions) != 1 || definitions[0].URI != uriForPath(modelPath) {
		t.Fatalf("type definition should follow an aliased module import to its Go declaration, got %#v", definitions)
	}
	typeOffset := strings.Index(mainSource, "people.Person") + len("people.")
	directPosition := positionAtOffset(mainSource, typeOffset)
	directDefinitions := server.typeDefinitions(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     directPosition,
	})
	if len(directDefinitions) != 1 || directDefinitions[0].URI != uriForPath(modelPath) {
		t.Fatalf("direct imported type definition should preserve its package identity among duplicate names: %#v", directDefinitions)
	}
}

func TestCallAndTypeHierarchyReturnCrossFileItems(t *testing.T) {
	root := t.TempDir()
	callerSource := "package main\nfunc caller() { target[int]() }\n"
	callerFile, err := compiler.ParseFile("caller.gpp", callerSource)
	if err != nil {
		t.Fatal(err)
	}
	callerPath := filepath.Join(root, "caller.gpp")
	callerFile.SourcePath = callerPath
	callerState := &fileState{Path: callerPath, URI: uriForPath(callerPath), Source: callerSource, File: callerFile}
	targetSource := "package main\nfunc target[T any]() {}\nclass Parent {}\n"
	targetFile, err := compiler.ParseFile("target.gpp", targetSource)
	if err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(root, "target.gpp")
	targetFile.SourcePath = targetPath
	targetState := &fileState{Path: targetPath, URI: uriForPath(targetPath), Source: targetSource, File: targetFile}
	childSource := "package main\nclass Child : Parent {}\n"
	childFile, err := compiler.ParseFile("child.gpp", childSource)
	if err != nil {
		t.Fatal(err)
	}
	childPath := filepath.Join(root, "child.gpp")
	childFile.SourcePath = childPath
	childState := &fileState{Path: childPath, URI: uriForPath(childPath), Source: childSource, File: childFile}
	workspace := &workspaceState{files: map[string]*fileState{callerPath: callerState, targetPath: targetState, childPath: childState}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{callerFile, targetFile, childFile})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	targetPosition := positionAtOffset(targetSource, strings.Index(targetSource, "target"))
	targetItems := server.prepareCallHierarchy(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: targetState.URI}, Position: targetPosition})
	if len(targetItems) != 1 {
		t.Fatalf("expected call hierarchy item for target, got %#v", targetItems)
	}
	if targetItems[0].Range.Start != positionAtOffset(targetSource, strings.Index(targetSource, "func target")) ||
		targetItems[0].Range == targetItems[0].SelectionRange ||
		targetItems[0].SelectionRange.Start != positionAtOffset(targetSource, strings.Index(targetSource, "target")) {
		t.Fatalf("call hierarchy ranges should cover the declaration and select its name: %#v", targetItems[0])
	}
	incoming := server.incomingCalls(targetItems[0])
	if len(incoming) != 1 || incoming[0].Item.Name != "caller" || len(incoming[0].FromRanges) != 1 {
		t.Fatalf("incoming call hierarchy should find caller in sibling file, got %#v", incoming)
	}
	callerPosition := positionAtOffset(callerSource, strings.Index(callerSource, "caller"))
	callerItems := server.prepareCallHierarchy(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: callerState.URI}, Position: callerPosition})
	outgoing := server.outgoingCalls(callerItems[0])
	if len(outgoing) != 1 || outgoing[0].Item.Name != "target" {
		t.Fatalf("outgoing call hierarchy should find target, got %#v", outgoing)
	}

	parentPosition := positionAtOffset(targetSource, strings.Index(targetSource, "Parent"))
	parentItems := server.prepareTypeHierarchy(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: targetState.URI}, Position: parentPosition})
	if len(parentItems) != 1 {
		t.Fatalf("expected type hierarchy item for Parent, got %#v", parentItems)
	}
	if parentItems[0].Range.Start != positionAtOffset(targetSource, strings.Index(targetSource, "class Parent")) ||
		parentItems[0].Range == parentItems[0].SelectionRange ||
		parentItems[0].SelectionRange.Start != positionAtOffset(targetSource, strings.Index(targetSource, "Parent")) {
		t.Fatalf("type hierarchy ranges should cover the declaration and select its name: %#v", parentItems[0])
	}
	children := server.typeSubtypes(parentItems[0])
	if len(children) != 1 || children[0].Name != "Child" || children[0].URI != childState.URI {
		t.Fatalf("type hierarchy should find Child in a sibling file, got %#v", children)
	}
}

func TestCallHierarchyIncludesNativeGoFunctionsInMixedWorkspace(t *testing.T) {
	root := t.TempDir()
	goPath := filepath.Join(root, "native.go")
	gppPath := filepath.Join(root, "caller.gpp")
	goSource := "package main\nfunc helper() {}\nfunc nativeCaller() { helper() }\n"
	gppSource := "package main\nfunc gppCaller() { helper() }\n"
	for path, source := range map[string]string{goPath: goSource, gppPath: gppSource} {
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	gppState := &fileState{Path: gppPath, URI: uriForPath(gppPath), Source: gppSource, Open: true}
	workspace := &workspaceState{files: map[string]*fileState{gppPath: gppState}, diagnostics: map[string][]Diagnostic{}}
	workspace.setRoots([]string{root})
	workspace.refresh()
	server := &server{workspace: workspace}
	goState := workspace.files[goPath]
	if goState == nil || goState.File == nil {
		t.Fatalf("native Go file should participate in analysis: %#v", workspace.diagnostics[uriForPath(goPath)])
	}

	helperOffset := strings.Index(goSource, "func helper") + len("func ")
	helperItems := server.prepareCallHierarchy(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: goState.URI}, Position: positionAtOffset(goSource, helperOffset)})
	if len(helperItems) != 1 {
		t.Fatalf("native Go function should prepare as a call hierarchy item: %#v", helperItems)
	}
	incoming := server.incomingCalls(helperItems[0])
	callers := map[string]bool{}
	for _, call := range incoming {
		callers[call.Item.Name] = len(call.FromRanges) == 1
	}
	if len(incoming) != 2 || !callers["nativeCaller"] || !callers["gppCaller"] {
		t.Fatalf("incoming calls should include native Go and GPP callers: %#v", incoming)
	}

	nativeCallerOffset := strings.Index(goSource, "nativeCaller")
	nativeCallerItems := server.prepareCallHierarchy(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: goState.URI}, Position: positionAtOffset(goSource, nativeCallerOffset)})
	if len(nativeCallerItems) != 1 {
		t.Fatalf("native Go caller should prepare as a call hierarchy item: %#v", nativeCallerItems)
	}
	outgoing := server.outgoingCalls(nativeCallerItems[0])
	if len(outgoing) != 1 || outgoing[0].Item.Name != "helper" || len(outgoing[0].FromRanges) != 1 {
		t.Fatalf("outgoing calls should resolve a native Go function body: %#v", outgoing)
	}
}

func TestNativeGoMethodsSupportHoverNavigationAndCallHierarchy(t *testing.T) {
	root := t.TempDir()
	goPath := filepath.Join(root, "worker.go")
	gppPath := filepath.Join(root, "use.gpp")
	goSource := "package main\ntype Worker struct{}\n// Run performs the worker task.\nfunc (w *Worker) Run() {}\n"
	gppSource := "package main\nfunc use(worker Worker) { worker.Run() }\n"
	for path, source := range map[string]string{goPath: goSource, gppPath: gppSource} {
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	gppState := &fileState{Path: gppPath, URI: uriForPath(gppPath), Source: gppSource, Open: true}
	workspace := &workspaceState{files: map[string]*fileState{gppPath: gppState}, diagnostics: map[string][]Diagnostic{}}
	workspace.setRoots([]string{root})
	workspace.refresh()
	server := &server{workspace: workspace}
	goState := workspace.files[goPath]
	if goState == nil || goState.File == nil {
		t.Fatalf("native Go method source should be analyzed: %#v", workspace.diagnostics[uriForPath(goPath)])
	}
	callOffset := strings.LastIndex(gppSource, "Run")
	callPosition := positionAtOffset(gppSource, callOffset)
	hoverResult := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: gppState.URI}, Position: callPosition})
	if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, "**method**") || !strings.Contains(hoverResult.Contents.Value, "Run performs the worker task") {
		var runSymbols []compiler.DocSymbol
		for _, info := range workspace.symbols {
			if info.Symbol.Name == "Run" {
				runSymbols = append(runSymbols, info.Symbol)
			}
		}
		t.Fatalf("method hover should resolve a documented native Go receiver method: hover=%#v symbols=%#v", hoverResult, runSymbols)
	}
	definitions := server.definitions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: gppState.URI}, Position: callPosition})
	if len(definitions) != 1 || definitions[0].URI != goState.URI {
		t.Fatalf("GPP method definition should point to the Go method declaration: %#v", definitions)
	}
	references := server.references(gppState.URI, callPosition, true)
	if len(references) != 2 {
		t.Fatalf("native method references should include declaration and GPP call: %#v", references)
	}
	edit, err := server.rename(gppState.URI, callPosition, "Execute")
	if err != nil || len(edit.Changes[goState.URI]) != 1 || len(edit.Changes[gppState.URI]) != 1 {
		t.Fatalf("native method rename should update its Go declaration and GPP call, edits=%#v err=%v", edit.Changes, err)
	}
	callerOffset := strings.Index(gppSource, "use(worker")
	callerItems := server.prepareCallHierarchy(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: gppState.URI}, Position: positionAtOffset(gppSource, callerOffset)})
	if len(callerItems) != 1 {
		t.Fatalf("GPP caller should prepare as a call hierarchy item: %#v", callerItems)
	}
	outgoing := server.outgoingCalls(callerItems[0])
	if len(outgoing) != 1 || outgoing[0].Item.Name != "Run" {
		t.Fatalf("GPP call hierarchy should find the native Go method: %#v", outgoing)
	}
	goMethodOffset := strings.LastIndex(goSource, "Run")
	methodItems := server.prepareCallHierarchy(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: goState.URI}, Position: positionAtOffset(goSource, goMethodOffset)})
	if len(methodItems) != 1 || methodItems[0].Name != "Run" {
		t.Fatalf("native Go method should prepare as a call hierarchy item: %#v", methodItems)
	}
	incoming := server.incomingCalls(methodItems[0])
	if len(incoming) != 1 || incoming[0].Item.Name != "use" {
		t.Fatalf("native Go method hierarchy should find GPP callers: %#v", incoming)
	}
}

func TestGPPNavigationResolvesMethodsOnImportedNativeGoTypes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/mixed\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(root, "models", "person.go")
	otherPath := filepath.Join(root, "other", "person.go")
	mainPath := filepath.Join(root, "app", "main.gpp")
	modelSource := "package models\ntype Person struct{}\n// Save stores the person.\nfunc (Person) Save() {}\n"
	otherSource := "package other\ntype Person struct{}\nfunc (Person) Save() {}\n"
	mainSource := "package app\nimport models \"example.test/mixed/models\"\nimport other \"example.test/mixed/other\"\nfunc use(person models.Person, otherPerson other.Person) { person.Save(); otherPerson.Save() }\n"
	for path, source := range map[string]string{modelPath: modelSource, otherPath: otherSource, mainPath: mainSource} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	state := workspace.files[mainPath]
	if state == nil || state.File == nil {
		t.Fatalf("GPP source should parse: %#v", workspace.diagnostics[uriForPath(mainPath)])
	}
	server := &server{workspace: workspace}
	callOffset := strings.Index(mainSource, "person.Save") + len("person.")
	params := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(mainSource, callOffset)}
	definitions := server.definitions(params)
	if len(definitions) != 1 || definitions[0].URI != uriForPath(modelPath) {
		t.Fatalf("GPP member definition should resolve through the imported Go type: %#v", definitions)
	}
	hoverResult := server.hover(params)
	if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, "Save stores the person") {
		t.Fatalf("GPP member hover should include imported Go method documentation: %#v", hoverResult)
	}
	dotOffset := strings.Index(mainSource, "person.Save") + len("person.")
	var saveCompletion *CompletionItem
	for _, item := range server.contextualCompletions(state, dotOffset) {
		if item.Label == "Save" {
			copy := item
			saveCompletion = &copy
			break
		}
	}
	if saveCompletion == nil || !strings.Contains(saveCompletion.Detail, "Save") {
		t.Fatalf("member completion should include methods from the imported Go type: %#v", saveCompletion)
	}
	references := server.references(state.URI, params.Position, true)
	if len(references) != 2 {
		t.Fatalf("GPP imported Go method references should include declaration and call: %#v", references)
	}
	edit, err := server.rename(state.URI, params.Position, "Persist")
	if err != nil || len(edit.Changes[uriForPath(modelPath)]) != 1 || len(edit.Changes[state.URI]) != 1 || len(edit.Changes[uriForPath(otherPath)]) != 0 {
		t.Fatalf("renaming an imported Go method should update both files: edits=%#v err=%v", edit.Changes, err)
	}
	modelState := workspace.files[modelPath]
	methodOffset := strings.Index(modelSource, "func (Person) Save") + len("func (Person) ")
	methodItems := server.prepareCallHierarchy(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: modelState.URI},
		Position:     positionAtOffset(modelSource, methodOffset),
	})
	if len(methodItems) != 1 || methodItems[0].Name != "Save" {
		t.Fatalf("imported Go method should prepare for call hierarchy: %#v", methodItems)
	}
	if methodItems[0].Detail != "func (Person) Save()" {
		t.Fatalf("call hierarchy should show the Go method signature exactly once: %#v", methodItems[0])
	}
	incoming := server.incomingCalls(methodItems[0])
	if len(incoming) != 1 || incoming[0].Item.Name != "use" {
		t.Fatalf("incoming calls should resolve the GPP call to the imported Go method: %#v", incoming)
	}
	callerOffset := strings.Index(mainSource, "func use") + len("func ")
	callerItems := server.prepareCallHierarchy(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: state.URI},
		Position:     positionAtOffset(mainSource, callerOffset),
	})
	if len(callerItems) != 1 {
		t.Fatalf("GPP caller should prepare for call hierarchy: %#v", callerItems)
	}
	outgoing := server.outgoingCalls(callerItems[0])
	if len(outgoing) != 2 {
		t.Fatalf("outgoing calls should include each same-named method from its exact imported type: %#v", outgoing)
	}
	outgoingTargets := map[string]bool{}
	for _, call := range outgoing {
		outgoingTargets[call.Item.URI] = true
	}
	if !outgoingTargets[uriForPath(modelPath)] || !outgoingTargets[uriForPath(otherPath)] {
		t.Fatalf("call hierarchy should keep same-named imported methods attached to their declaring types: %#v", outgoing)
	}
}

func TestNativeGoInterfaceImplementationsMatchMethodSignatures(t *testing.T) {
	root := t.TempDir()
	goPath := filepath.Join(root, "writers.go")
	gppPath := filepath.Join(root, "gpp_writer.gpp")
	goSource := `package main
type Writer interface {
    // Write emits bytes to the writer.
    Write(p []byte) (n int, err error)
}
type Closer interface { Close() error }
type ReadWriteCloser interface { Writer; Closer }
type Disk struct{}
func (Disk) Write(data []byte) (int, error) { return len(data), nil }
func (Disk) Close() error { return nil }
type Invalid struct{}
func (Invalid) Write(data []byte) error { return nil }
`
	if err := os.WriteFile(goPath, []byte(goSource), 0644); err != nil {
		t.Fatal(err)
	}
	gppSource := `package main
class GoDisk {
    func Write(data []byte) (int, error) { return 0, nil }
    func Close() error { return nil }
}
`
	if err := os.WriteFile(gppPath, []byte(gppSource), 0644); err != nil {
		t.Fatal(err)
	}
	workspace := &workspaceState{files: map[string]*fileState{gppPath: {Path: gppPath, URI: uriForPath(gppPath), Source: gppSource}}, diagnostics: map[string][]Diagnostic{}}
	workspace.setRoots([]string{root})
	workspace.refresh()
	server := &server{workspace: workspace}
	goState := workspace.files[goPath]
	if goState == nil || goState.File == nil {
		t.Fatalf("native Go interface source should be analyzed: %#v", workspace.diagnostics[uriForPath(goPath)])
	}

	interfacePosition := positionAtOffset(goSource, strings.Index(goSource, "Writer"))
	implementations := server.implementations(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: goState.URI}, Position: interfacePosition})
	if len(implementations) != 2 {
		t.Fatalf("interface implementations should match Go and GPP method signatures and exclude near matches: %#v", implementations)
	}
	implementationPaths := map[string]bool{}
	for _, implementation := range implementations {
		implementationPaths[implementation.URI] = true
	}
	if !implementationPaths[uriForPath(goPath)] || !implementationPaths[uriForPath(gppPath)] {
		t.Fatalf("interface implementations should include both Go and GPP types: %#v", implementations)
	}

	methodPosition := positionAtOffset(goSource, strings.Index(goSource, "Write(p"))
	hoverResult := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: goState.URI}, Position: methodPosition})
	if hoverResult == nil || !strings.Contains(hoverResult.Contents.Value, "**method**") || !strings.Contains(hoverResult.Contents.Value, "Write emits bytes") {
		t.Fatalf("interface methods should have hover and documentation: %#v", hoverResult)
	}
	methodImplementations := server.implementations(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: goState.URI}, Position: methodPosition})
	if len(methodImplementations) != 2 {
		t.Fatalf("interface method implementations should point to matching Go and GPP methods: %#v", methodImplementations)
	}

	composedPosition := positionAtOffset(goSource, strings.Index(goSource, "ReadWriteCloser"))
	composedImplementations := server.implementations(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: goState.URI}, Position: composedPosition})
	if len(composedImplementations) != 2 {
		t.Fatalf("embedded interface implementations should include types satisfying the full recursive method set: %#v", composedImplementations)
	}
}

func TestNativeGoInterfaceImplementationsResolveEmbeddedWorkspacePackage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/interfaces\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	contractsDir := filepath.Join(root, "contracts")
	if err := os.MkdirAll(contractsDir, 0755); err != nil {
		t.Fatal(err)
	}
	contractsPath := filepath.Join(contractsDir, "reader.go")
	contractsSource := "package contracts\ntype Reader interface { Read([]byte) (int, error) }\n"
	if err := os.WriteFile(contractsPath, []byte(contractsSource), 0644); err != nil {
		t.Fatal(err)
	}
	appPath := filepath.Join(root, "app.go")
	appSource := `package main
import "example.test/interfaces/contracts"
type ReadCloser interface {
    contracts.Reader
    Close() error
}
type Device struct{}
func (Device) Read([]byte) (int, error) { return 0, nil }
func (Device) Close() error { return nil }
type ReaderOnly struct{}
func (ReaderOnly) Read([]byte) (int, error) { return 0, nil }
`
	if err := os.WriteFile(appPath, []byte(appSource), 0644); err != nil {
		t.Fatal(err)
	}

	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	server := &server{workspace: workspace}
	appState := workspace.files[appPath]
	if appState == nil || appState.File == nil {
		t.Fatalf("workspace Go source should be indexed: %#v", workspace.diagnostics[uriForPath(appPath)])
	}
	targetOffset := strings.Index(appSource, "ReadCloser")
	implementations := server.implementations(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: appState.URI},
		Position:     positionAtOffset(appSource, targetOffset),
	})
	if len(implementations) != 1 || implementations[0].Range.Start.Line != 6 {
		t.Fatalf("qualified embedded workspace interface should require all inherited methods: %#v", implementations)
	}
	interfaceItems := server.prepareTypeHierarchy(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: appState.URI},
		Position:     positionAtOffset(appSource, targetOffset),
	})
	if len(interfaceItems) != 1 || interfaceItems[0].Kind != 11 {
		t.Fatalf("native Go interfaces should prepare as interface hierarchy items: %#v", interfaceItems)
	}
	supertypes := server.typeSupertypes(interfaceItems[0])
	if len(supertypes) != 1 || supertypes[0].Name != "Reader" || supertypes[0].URI != uriForPath(contractsPath) {
		t.Fatalf("ReadCloser should show its imported embedded Reader supertype: %#v", supertypes)
	}
	readerPosition := positionAtOffset(contractsSource, strings.Index(contractsSource, "Reader"))
	readerItems := server.prepareTypeHierarchy(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: uriForPath(contractsPath)},
		Position:     readerPosition,
	})
	if len(readerItems) != 1 {
		t.Fatalf("Reader should prepare as a native Go interface hierarchy item: %#v", readerItems)
	}
	subtypes := server.typeSubtypes(readerItems[0])
	readerSubtypes := map[string]string{}
	for _, subtype := range subtypes {
		readerSubtypes[subtype.Name] = subtype.URI
	}
	if len(readerSubtypes) != 3 || readerSubtypes["ReadCloser"] != appState.URI || readerSubtypes["Device"] != appState.URI || readerSubtypes["ReaderOnly"] != appState.URI {
		t.Fatalf("Reader should show its embedded interface and concrete implementers as subtypes: %#v", subtypes)
	}
	devicePosition := positionAtOffset(appSource, strings.Index(appSource, "Device"))
	deviceItems := server.prepareTypeHierarchy(textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: appState.URI},
		Position:     devicePosition,
	})
	if len(deviceItems) != 1 || deviceItems[0].Kind != 23 {
		t.Fatalf("native Go structs should prepare as struct hierarchy items: %#v", deviceItems)
	}
	deviceSupertypes := server.typeSupertypes(deviceItems[0])
	deviceSupertypeNames := map[string]string{}
	for _, supertype := range deviceSupertypes {
		deviceSupertypeNames[supertype.Name] = supertype.URI
	}
	if len(deviceSupertypeNames) != 2 || deviceSupertypeNames["Reader"] != uriForPath(contractsPath) || deviceSupertypeNames["ReadCloser"] != appState.URI {
		t.Fatalf("Device should show both implemented cross-package interfaces as supertypes: %#v", deviceSupertypes)
	}
}

func TestCallHierarchyIncludesExtensionMethodsAndCallers(t *testing.T) {
	root := t.TempDir()
	extensionSource := "package main\nextend string {\n\tfunc Clean() string { return helper() }\n}\nfunc helper() string { return \"clean\" }\n"
	extensionFile, err := compiler.ParseFile("extension.gpp", extensionSource)
	if err != nil {
		t.Fatal(err)
	}
	extensionPath := filepath.Join(root, "extension.gpp")
	extensionFile.SourcePath = extensionPath
	extensionState := &fileState{Path: extensionPath, URI: uriForPath(extensionPath), Source: extensionSource, File: extensionFile}
	callerSource := "package main\nfunc caller() { _ = \"text\".Clean() }\n"
	callerFile, err := compiler.ParseFile("caller.gpp", callerSource)
	if err != nil {
		t.Fatal(err)
	}
	callerPath := filepath.Join(root, "caller.gpp")
	callerFile.SourcePath = callerPath
	callerState := &fileState{Path: callerPath, URI: uriForPath(callerPath), Source: callerSource, File: callerFile}
	workspace := &workspaceState{files: map[string]*fileState{extensionPath: extensionState, callerPath: callerState}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{extensionFile, callerFile})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	cleanOffset := strings.Index(extensionSource, "Clean")
	cleanItems := server.prepareCallHierarchy(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: extensionState.URI}, Position: positionAtOffset(extensionSource, cleanOffset)})
	if len(cleanItems) != 1 {
		t.Fatalf("extension methods should be valid call hierarchy items, got %#v", cleanItems)
	}
	incoming := server.incomingCalls(cleanItems[0])
	if len(incoming) != 1 || incoming[0].Item.Name != "caller" {
		t.Fatalf("incoming hierarchy should find calls to extension methods in other files, got %#v", incoming)
	}
	callers := server.prepareCallHierarchy(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: extensionState.URI}, Position: positionAtOffset(extensionSource, strings.Index(extensionSource, "Clean()"))})
	outgoing := server.outgoingCalls(callers[0])
	if len(outgoing) != 1 || outgoing[0].Item.Name != "helper" {
		t.Fatalf("extension method outgoing hierarchy should find its helper call, got %#v", outgoing)
	}
}

func TestTypeHierarchyDisambiguatesSameNamedClassesAcrossPackages(t *testing.T) {
	root := t.TempDir()
	sources := []struct {
		name string
		text string
	}{
		{name: "base.gpp", text: "package foo.base\nclass Base {}\n"},
		{name: "other.gpp", text: "package foo.other\nclass Base {}\n"},
		{name: "child.gpp", text: "package foo.child\nimport foo.base\nclass Child : base.Base {}\n"},
		{name: "other-child.gpp", text: "package foo.otherchild\nimport foo.other\nclass OtherChild : other.Base {}\n"},
	}
	files := make([]*compiler.File, 0, len(sources))
	states := map[string]*fileState{}
	for _, source := range sources {
		file, err := compiler.ParseFile(source.name, source.text)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, source.name)
		file.SourcePath = path
		state := &fileState{Path: path, URI: uriForPath(path), Source: source.text, File: file}
		states[source.name] = state
		files = append(files, file)
	}
	workspace := &workspaceState{files: map[string]*fileState{}}
	for _, state := range states {
		workspace.files[state.Path] = state
	}
	workspace.docIndex = compiler.BuildDocIndex(files)
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	var base, otherBase, child, otherChild symbolInfo
	for _, info := range workspace.symbols {
		switch {
		case info.Path == states["base.gpp"].Path && info.Symbol.Name == "Base":
			base = info
		case info.Path == states["other.gpp"].Path && info.Symbol.Name == "Base":
			otherBase = info
		case info.Path == states["child.gpp"].Path && info.Symbol.Name == "Child":
			child = info
		case info.Path == states["other-child.gpp"].Path && info.Symbol.Name == "OtherChild":
			otherChild = info
		}
	}
	baseItem, ok := server.typeHierarchyItem(base)
	if !ok {
		t.Fatal("expected hierarchy item for foo.base.Base")
	}
	childItem, ok := server.typeHierarchyItem(child)
	if !ok {
		t.Fatal("expected hierarchy item for foo.child.Child")
	}
	otherChildItem, ok := server.typeHierarchyItem(otherChild)
	if !ok {
		t.Fatal("expected hierarchy item for foo.otherchild.OtherChild")
	}
	supers := server.typeSupertypes(childItem)
	if len(supers) != 1 || supers[0].URI != states["base.gpp"].URI {
		t.Fatalf("Child should resolve only to foo.base.Base, got %#v", supers)
	}
	subtypes := server.typeSubtypes(baseItem)
	if len(subtypes) != 1 || subtypes[0].URI != states["child.gpp"].URI {
		t.Fatalf("foo.base.Base should have only Child as a direct subtype, got %#v (other Base: %#v)", subtypes, otherBase)
	}
	basePosition := positionAtOffset(states["base.gpp"].Source, strings.Index(states["base.gpp"].Source, "Base"))
	implementations := server.implementations(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: states["base.gpp"].URI}, Position: basePosition})
	if len(implementations) != 1 || implementations[0].URI != states["child.gpp"].URI {
		t.Fatalf("foo.base.Base implementations should exclude the unrelated foo.other.Base hierarchy, got %#v", implementations)
	}
	otherSupers := server.typeSupertypes(otherChildItem)
	if len(otherSupers) != 1 || otherSupers[0].URI != states["other.gpp"].URI {
		t.Fatalf("OtherChild should resolve only to foo.other.Base, got %#v", otherSupers)
	}
}

func TestTypeHierarchyResolvesOrdinaryModuleImportParentsByDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/hierarchy\n\ngo 1.24\n"), 0644); err != nil {
		t.Fatal(err)
	}
	baseDir := filepath.Join(root, "pkg", "base")
	otherDir := filepath.Join(root, "pkg", "other")
	appDir := filepath.Join(root, "pkg", "app")
	for _, dir := range []string{baseDir, otherDir, appDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(baseDir, "parent.gpp"):  "package base\nclass Parent {}\n",
		filepath.Join(otherDir, "parent.gpp"): "package base\nclass Parent {}\n",
		filepath.Join(appDir, "child.gpp"):    "package app\nimport \"example.test/hierarchy/pkg/base\"\nclass Child : base.Parent {}\n",
	}
	for path, source := range files {
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	workspace := newWorkspace(nil)
	workspace.setRoots([]string{root})
	workspace.refresh()
	server := &server{workspace: workspace}
	childPath := filepath.Join(appDir, "child.gpp")
	childState := workspace.files[childPath]
	if childState == nil || childState.File == nil {
		t.Fatalf("module GPP source should be indexed: %#v", workspace.diagnostics[uriForPath(childPath)])
	}
	childPosition := positionAtOffset(childState.Source, strings.Index(childState.Source, "Child"))
	items := server.prepareTypeHierarchy(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: childState.URI}, Position: childPosition})
	if len(items) != 1 {
		t.Fatalf("expected hierarchy item for Child, got %#v", items)
	}
	parents := server.typeSupertypes(items[0])
	if len(parents) != 1 || parents[0].URI != uriForPath(filepath.Join(baseDir, "parent.gpp")) {
		t.Fatalf("ordinary module import should resolve only pkg/base.Parent, got %#v", parents)
	}
}

func TestFunctionCodeLensCountsReferences(t *testing.T) {
	source := "package main\nclass Widget {}\nfunc target() {}\nfunc caller() { target(); _ = Widget{} }\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	lenses := server.codeLenses(state.URI)
	counts := map[string]string{}
	for _, lens := range lenses {
		if lens.Data == nil {
			continue
		}
		// Exercise the same JSON round trip as the LSP client between the
		// initial codeLens response and codeLens/resolve.
		encoded, err := json.Marshal(lens)
		if err != nil {
			t.Fatal(err)
		}
		var clientLens CodeLens
		if err := json.Unmarshal(encoded, &clientLens); err != nil {
			t.Fatal(err)
		}
		var data codeLensResolveData
		if encodedData, err := json.Marshal(clientLens.Data); err != nil {
			t.Fatal(err)
		} else if err := json.Unmarshal(encodedData, &data); err != nil {
			t.Fatal(err)
		}
		resolved := server.resolveCodeLens(clientLens)
		if resolved.Command == nil {
			t.Errorf("code lens for %s should resolve to a reference-count command: %#v", data.FullName, resolved)
			continue
		}
		counts[data.FullName] = resolved.Command.Title
		args := resolved.Command.Arguments[0].(map[string]any)
		if args["uri"] != state.URI {
			t.Errorf("code lens for %s should retain the current document URI: %#v", data.FullName, args)
		}
	}
	if counts["main.target"] != "1 references" {
		t.Errorf("function should show its reference count, got %#v", counts)
	}
	if counts["main.Widget"] != "1 references" {
		t.Errorf("class should show the same reference count metadata, got %#v", counts)
	}
}

func TestORMInheritedMethodsResolveAndInferSelfTypes(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "examples", "orm.gpp"))
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	file, err := compiler.ParseFile(path, source)
	if err != nil {
		t.Fatal(err)
	}
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file, Open: true}
	workspace := newWorkspace(nil)
	workspace.files[path] = state
	workspace.analyze()
	server := &server{workspace: workspace}

	for _, testCase := range []struct {
		selector string
		name     string
		parent   string
	}{
		{selector: "ada.Insert", name: "Insert", parent: "Model"},
		{selector: "Employee.Get", name: "Get", parent: "Model"},
		{selector: "Employee.Select", name: "Select", parent: "Model"},
	} {
		start := strings.Index(source, testCase.selector)
		if start < 0 {
			t.Fatalf("example is missing %s", testCase.selector)
		}
		memberOffset := start + strings.LastIndex(testCase.selector, testCase.name)
		position := positionAtOffset(source, memberOffset)
		choices := server.resolveSymbolsAt(state.URI, position, testCase.name)
		if len(choices) != 1 || choices[0].Symbol.Parent != testCase.parent || filepath.Base(choices[0].Path) != "model.gpp" {
			t.Errorf("%s should resolve to orm.Model.%s in model.gpp, got %#v", testCase.selector, testCase.name, choices)
			continue
		}
		location, ok := server.symbolLocation(choices[0])
		if !ok || filepath.Base(pathFromURI(location.URI)) != "model.gpp" {
			t.Errorf("%s should navigate to the ORM method declaration, got %#v, %v", testCase.selector, location, ok)
		}
	}
	for _, testCase := range []struct {
		selector string
		want     []string
	}{
		{selector: "ada.Insert", want: []string{"Insert", "Update", "Destroy"}},
		{selector: "Employee.Get", want: []string{"Get", "Select", "Find"}},
	} {
		start := strings.Index(source, testCase.selector)
		memberOffset := start + strings.LastIndex(testCase.selector, ".") + 1
		items := server.completions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, memberOffset)})
		labels := map[string]bool{}
		for _, item := range items {
			labels[item.Label] = true
		}
		for _, label := range testCase.want {
			if !labels[label] {
				t.Errorf("completing %s should offer inherited ORM member %s; got %#v", testCase.selector, label, labels)
			}
		}
	}

	getOffset := strings.Index(source, "Employee.Get") + len("Employee.")
	getHover := server.hover(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, getOffset)})
	if getHover == nil || !strings.Contains(getHover.Contents.Value, "Employee.Get") || !strings.Contains(getHover.Contents.Value, "*Employee") {
		t.Errorf("Employee.Get hover should specialize the inherited self signature, got %#v", getHover)
	}

	for _, testCase := range []struct {
		declaration string
		name        string
		typeName    string
	}{
		{declaration: "ada :=", name: "ada", typeName: "*Employee"},
		{declaration: "employees :=", name: "employees", typeName: "[]*Employee"},
		{declaration: "target :=", name: "target", typeName: "*Employee"},
		{declaration: "employee := orm.Get[Employee]", name: "employee", typeName: "*Employee"},
		{declaration: "departmentEmployees := orm.Select[Employee]", name: "departmentEmployees", typeName: "[]*Employee"},
	} {
		declarationOffset := strings.Index(source, testCase.declaration)
		if declarationOffset < 0 {
			t.Fatalf("example is missing %s", testCase.declaration)
		}
		nameOffset := declarationOffset + strings.Index(testCase.declaration, testCase.name)
		binding, ok := server.localBindingAt(state, nameOffset, testCase.name)
		if !ok || binding.Type != testCase.typeName {
			t.Errorf("%s should infer %s, got %#v", testCase.declaration, testCase.typeName, binding)
		}
	}

	ormGetOffset := strings.Index(source, "orm.Get[Employee]") + len("orm.")
	ormGetChoices := server.resolveSymbolsAt(state.URI, positionAtOffset(source, ormGetOffset), "Get")
	ormGetFunctionFound := false
	for _, choice := range ormGetChoices {
		if choice.Symbol.Kind == compiler.DocFunction && strings.Contains(choice.Symbol.Signature, "Get[T any]") {
			ormGetFunctionFound = true
		}
	}
	if !ormGetFunctionFound {
		t.Errorf("orm.Get[T] should resolve to the imported ORM function, got %#v", ormGetChoices)
	}
}

func TestRangeMultiRangeAndOnTypeFormattingRespectRequestedLines(t *testing.T) {
	source := "package main\nfunc first(){x:=1}\nfunc middle(){ y:=2 }\nfunc last(){z:=3}\n"
	path := filepath.Join(t.TempDir(), "format.gpp")
	uri := uriForPath(path)
	file, err := compiler.ParseFile(path, source)
	if err != nil {
		t.Fatal(err)
	}
	file.SourcePath = path
	state := &fileState{Path: path, URI: uri, Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	formattedBytes, err := compiler.FormatSourceFile(path, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	formattedLines := strings.Split(string(formattedBytes), "\n")
	sourceLines := strings.Split(source, "\n")

	lineRange := func(line int) Range {
		return Range{Start: Position{Line: line, Character: 0}, End: Position{Line: line, Character: utf16Length(sourceLines[line])}}
	}
	oneRange := server.formattingInRanges(uri, []Range{lineRange(1)})
	if len(oneRange) < 1 {
		t.Fatalf("single-range formatting should edit only the requested line: edits=%#v diff=%#v formatted=%q", oneRange, formattingDiff(source, string(formattedBytes)), formattedBytes)
	}
	for _, edit := range oneRange {
		if edit.Range.Start.Line != 1 || edit.Range.End.Line != 1 {
			t.Fatalf("single-range response edited outside the selected line: %#v", oneRange)
		}
	}
	rangeFormatted := applyFormattingEdits(source, oneRange)
	rangeLines := strings.Split(rangeFormatted, "\n")
	if rangeLines[1] != "" || rangeLines[2] != formattedLines[2] || rangeLines[3] != sourceLines[2] {
		t.Fatalf("range formatting must format the selected line and preserve adjacent contents: got=%#v want selected=%q adjacent=%q", rangeLines, formattedLines[2], sourceLines[2])
	}

	multiple := server.formattingInRanges(uri, []Range{lineRange(1), lineRange(3)})
	if len(multiple) < 2 {
		t.Fatalf("multi-range formatting should edit both requested lines: %#v", multiple)
	}
	for index, edit := range multiple {
		if edit.Range.Start.Line != 1 && edit.Range.Start.Line != 3 {
			t.Fatalf("multi-range response edited an unselected line: %#v", multiple)
		}
		if index > 0 && compareLSPPosition(multiple[index-1].Range.End, edit.Range.Start) > 0 {
			t.Fatalf("multi-range edits must be ordered and non-overlapping: %#v", multiple)
		}
	}
	multiFormatted := strings.Split(applyFormattingEdits(source, multiple), "\n")
	if multiFormatted[1] != "" || multiFormatted[2] != formattedLines[2] || multiFormatted[3] != sourceLines[2] || multiFormatted[4] != formattedLines[4] {
		t.Fatalf("multi-range formatting should change only selected lines: got=%#v", multiFormatted)
	}

	onType := server.formattingOnType(uri, Position{Line: 2, Character: utf16Length(sourceLines[2])}, "}")
	if len(onType) != 1 || onType[0].Range.Start.Line != 2 || onType[0].Range.End.Line > 3 {
		t.Fatalf("on-type formatting should be limited to the typed line: %#v", onType)
	}
	if unsupported := server.formattingOnType(uri, Position{Line: 2}, ";"); len(unsupported) != 0 {
		t.Fatalf("unsupported trigger characters should not produce edits: %#v", unsupported)
	}
	newline := server.formattingOnType(uri, Position{Line: 2}, "\n")
	for _, edit := range newline {
		if edit.Range.Start.Line < 1 || edit.Range.End.Line > 2 {
			t.Fatalf("newline formatting should stay within the adjacent lines: %#v", newline)
		}
	}
}

func TestLSPFormattingPreservesUnderscoreVisibilityNames(t *testing.T) {
	source := "package main\nclass Base{_state string\nfunc _reset(){this._state=\"\"}}\n"
	path := filepath.Join(t.TempDir(), "visibility.gpp")
	uri := uriForPath(path)
	file, err := compiler.ParseFile(path, source)
	if err != nil {
		t.Fatal(err)
	}
	file.SourcePath = path
	state := &fileState{Path: path, URI: uri, Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}
	edits := server.formatting(uri)
	if len(edits) != 1 {
		t.Fatalf("expected formatting edit for unformatted source, got %#v", edits)
	}
	for _, member := range []string{"_state", "_reset", "this._state"} {
		if !strings.Contains(edits[0].NewText, member) {
			t.Errorf("LSP formatting changed protected member name %q:\n%s", member, edits[0].NewText)
		}
	}
}

func TestEditorFeaturesExposeLinkedSymbolsInlineValuesAndColors(t *testing.T) {
	source := "package main\nfunc inspect() {\n    value := 1\n    _ = value\n    color := \"#1a2B\"\n    _ = color\n}\n"
	path := filepath.Join(t.TempDir(), "editor.gpp")
	uri := uriForPath(path)
	file, err := compiler.ParseFile(path, source)
	if err != nil {
		t.Fatal(err)
	}
	file.SourcePath = path
	state := &fileState{Path: path, URI: uri, Source: source, File: file}
	server := &server{workspace: &workspaceState{files: map[string]*fileState{path: state}}}

	valueOffset := strings.Index(source, "value :=")
	valuePosition := positionAtOffset(source, valueOffset+1)
	linked := server.linkedEditingRange(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: uri}, Position: valuePosition})
	if linked == nil || len(linked["ranges"].([]Range)) != 2 {
		t.Fatalf("linked editing should return declaration and use ranges for a local variable: %#v", linked)
	}
	monikers := server.monikers(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: uri}, Position: valuePosition})
	if len(monikers) != 1 || monikers[0]["scheme"] != "gpp" || monikers[0]["kind"] != "local" {
		t.Fatalf("local symbols should have a document-unique GPP moniker: %#v", monikers)
	}
	inline := server.inlineValues(uri, fullDocumentRange(source))
	if len(inline) == 0 {
		t.Fatal("inline values should expose in-scope local expressions in the requested range")
	}
	colors := server.documentColors(uri)
	if len(colors) != 1 {
		t.Fatalf("document colors should find the valid hex color in a string and ignore non-color text: %#v", colors)
	}
	color := colors[0]["color"].(colorValue)
	if color != (colorValue{Red: 17.0 / 255, Green: 170.0 / 255, Blue: 34.0 / 255, Alpha: 187.0 / 255}) {
		t.Fatalf("unexpected parsed color channels: %#v", color)
	}
	colorRange := colors[0]["range"].(Range)
	presentations := server.colorPresentations(uri, colorRange, color)
	if len(presentations) != 1 || presentations[0]["label"] != "#11AA22BB" {
		t.Fatalf("color presentation should preserve alpha using an 8-digit hex form: %#v", presentations)
	}
	if _, ok := parseHexColor("#123456789"); ok {
		t.Fatal("overlong hex values must not be accepted")
	}
}

func TestInlineCompletionsRequireCodePrefixAndSkipImportBlocks(t *testing.T) {
	source := "package main\nimport (\n    \"github.com/example/pkg\"\n    \n)\nfunc GetEnv(primaryName, fallbackName string) string { return primaryName }\nfunc use() { GetE }\n"
	path := filepath.Join(t.TempDir(), "inline.gpp")
	file, err := compiler.ParseFile(path, source)
	if err != nil {
		t.Fatal(err)
	}
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	blankImportOffset := strings.Index(source, "    \n)") + len("    ")
	insideImport := server.inlineCompletions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, blankImportOffset)})
	if len(insideImport) != 0 {
		t.Fatalf("inline completions must not insert function calls in an import block: %#v", insideImport)
	}
	functionPrefixOffset := strings.LastIndex(source, "GetE") + len("GetE")
	codeSuggestions := server.inlineCompletions(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, functionPrefixOffset)})
	for _, suggestion := range codeSuggestions {
		if suggestion["filterText"] == "GetEnv" {
			return
		}
	}
	t.Fatalf("a matching function completion should remain available after a code prefix: %#v", codeSuggestions)
}

func TestResolveCompletionCodeActionAndWorkspaceSymbolItems(t *testing.T) {
	root := t.TempDir()
	source := "package main\nclass User {\n    /// Renames this user.\n    static func Rename(name string) {}\n}\nfunc use(user User) { user.Re }\n"
	path := filepath.Join(root, "main.gpp")
	uri := uriForPath(path)
	file, err := compiler.ParseFile(path, source)
	if err != nil {
		t.Fatal(err)
	}
	file.SourcePath = path
	state := &fileState{Path: path, URI: uri, Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	completionOffset := strings.Index(source, "user.Re") + len("user.Re")
	params := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: uri}, Position: positionAtOffset(source, completionOffset)}
	var selected CompletionItem
	for _, item := range server.completions(params) {
		if item.Label == "Rename" {
			selected = item
			break
		}
	}
	if selected.Data == nil {
		t.Fatal("completion candidates should retain data needed to resolve their details")
	}
	selected.Detail, selected.Documentation = "", ""
	selected.TextEdit = nil
	resolvedCompletion := server.resolveCompletionItem(selected)
	if resolvedCompletion.Detail == "" || !strings.Contains(resolvedCompletion.Documentation, "Renames this user") || resolvedCompletion.TextEdit == nil {
		t.Fatalf("completion resolve should restore signature, docs, and edit from its data: %#v", resolvedCompletion)
	}

	brokenSource := "package main\n/*"
	brokenPath := filepath.Join(root, "broken.gpp")
	brokenURI := uriForPath(brokenPath)
	brokenState := &fileState{Path: brokenPath, URI: brokenURI, Source: brokenSource}
	workspace.files[brokenPath] = brokenState
	diagnostic := Diagnostic{Code: diagnosticUnclosedComment, Data: map[string]any{"fix": "insert-delimiter", "text": "*/", "offset": len(brokenSource)}, Message: "unterminated block comment"}
	actionParams := codeActionParams{TextDocument: textDocumentIdentifier{URI: brokenURI}}
	actionParams.Context.Diagnostics = []Diagnostic{diagnostic}
	actions := server.codeActions(actionParams)
	if len(actions) != 1 || actions[0].Data == nil {
		t.Fatalf("quick fixes should include data required for resolution: %#v", actions)
	}
	action := actions[0]
	action.Edit = workspaceEdit{}
	resolvedAction := server.resolveCodeAction(action)
	if edits := resolvedAction.Edit.Changes[brokenURI]; len(edits) != 1 || edits[0].NewText != "*/" {
		t.Fatalf("code action resolve should recompute and restore its edit: %#v", resolvedAction)
	}

	var workspaceSymbol SymbolInformation
	for _, item := range server.workspaceSymbols("Rename") {
		if item.Name == "Rename" {
			workspaceSymbol = item
			break
		}
	}
	if workspaceSymbol.Data == nil {
		t.Fatal("workspace symbols should include stable data for later resolution")
	}
	server.supportsWorkspaceSymbolRangeResolve = true
	deferredSymbols := server.workspaceSymbolResults("Rename")
	deferredSymbolJSON, err := json.Marshal(deferredSymbols[0])
	if err != nil {
		t.Fatal(err)
	}
	var deferredSymbol map[string]json.RawMessage
	if err := json.Unmarshal(deferredSymbolJSON, &deferredSymbol); err != nil {
		t.Fatal(err)
	}
	var initialLocation map[string]json.RawMessage
	if err := json.Unmarshal(deferredSymbol["location"], &initialLocation); err != nil {
		t.Fatal(err)
	}
	if _, hasRange := initialLocation["range"]; hasRange {
		t.Fatal("workspace symbol range should be deferred only when the client supports resolution")
	}
	resolvedSymbol := server.resolveWorkspaceSymbol(deferredSymbol)
	var resolvedLocation map[string]json.RawMessage
	if err := json.Unmarshal(resolvedSymbol["location"], &resolvedLocation); err != nil {
		t.Fatal(err)
	}
	if len(resolvedLocation["range"]) == 0 {
		t.Fatalf("workspace symbol resolve should fill the deferred location range: %#v", resolvedSymbol)
	}
}

func TestPullDiagnosticsAndExecuteCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.gpp")
	uri := uriForPath(path)
	source := "package main\nimport (\n    \"z.example/pkg\"\n    \"a.example/pkg\"\n)\nfunc main() {}\n"
	file, err := compiler.ParseFile(path, source)
	if err != nil {
		t.Fatal(err)
	}
	file.SourcePath = path
	diagnostic := Diagnostic{Range: Range{Start: Position{Line: 1}, End: Position{Line: 1, Character: 5}}, Severity: 2, Source: "gpp", Code: "GPP2000", Message: "example warning"}
	state := &fileState{Path: path, URI: uri, Source: source, File: file, Version: 7, HasVersion: true}
	workspace := &workspaceState{files: map[string]*fileState{path: state}, diagnostics: map[string][]Diagnostic{uri: {diagnostic}}}
	server := &server{workspace: workspace}

	documentReport := server.pullDocumentDiagnostics(uri, "")
	if documentReport["kind"] != "full" || documentReport["resultId"] == "" || len(documentReport["items"].([]Diagnostic)) != 1 {
		t.Fatalf("document pull should return a versioned full report: %#v", documentReport)
	}
	resultID := documentReport["resultId"].(string)
	if unchanged := server.pullDocumentDiagnostics(uri, resultID); unchanged["kind"] != "unchanged" || unchanged["items"] != nil {
		t.Fatalf("matching document result IDs should return an unchanged report: %#v", unchanged)
	}
	workspaceReport := server.pullWorkspaceDiagnostics([]previousDiagnosticResult{{URI: uri, Value: resultID}})
	workspaceItems := workspaceReport["items"].([]map[string]any)
	if len(workspaceItems) != 1 || workspaceItems[0]["kind"] != "unchanged" || workspaceItems[0]["version"] != 7 {
		t.Fatalf("workspace pull should return an unchanged report with document version: %#v", workspaceReport)
	}

	organizeResult, err := server.executeCommand(executeCommandParams{Command: "gpp.organizeImports", Arguments: []json.RawMessage{json.RawMessage(`"` + uri + `"`)}})
	if err != nil {
		t.Fatal(err)
	}
	edit := organizeResult.(workspaceEdit)
	if len(edit.Changes[uri]) == 0 {
		t.Fatalf("organize-imports command should return an applicable workspace edit: %#v", edit)
	}
	diagnosticsResult, err := server.executeCommand(executeCommandParams{Command: "gpp.getDiagnostics", Arguments: []json.RawMessage{json.RawMessage(`{"uri":"` + uri + `"}`)}})
	if err != nil || len(diagnosticsResult.([]Diagnostic)) != 1 {
		t.Fatalf("diagnostics command should return the document diagnostics: result=%#v err=%v", diagnosticsResult, err)
	}
	if _, err := server.executeCommand(executeCommandParams{Command: "gpp.notACommand", Arguments: []json.RawMessage{json.RawMessage(`"` + uri + `"`)}}); err == nil {
		t.Fatal("unknown execute commands should be rejected")
	}
}

func applyFormattingEdits(source string, edits []TextEdit) string {
	ordered := append([]TextEdit(nil), edits...)
	sort.Slice(ordered, func(i, j int) bool {
		return compareLSPPosition(ordered[i].Range.Start, ordered[j].Range.Start) > 0
	})
	for _, edit := range ordered {
		start, end := offsetAt(source, edit.Range.Start), offsetAt(source, edit.Range.End)
		source = source[:start] + edit.NewText + source[end:]
	}
	return source
}

func TestSignatureHelpTracksNestedCallsAndActiveParameter(t *testing.T) {
	source := "package main\nfunc target(first string, second int, third bool) {}\nfunc inner(left int, right int) {}\nfunc caller() { target(\"x\", inner(1, 2), true) }\n"
	file, err := compiler.ParseFile("main.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}

	innerOffset := strings.Index(source, "inner(1, 2") + len("inner(1, 2")
	innerHelp := server.signatureHelp(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, innerOffset)})
	if innerHelp == nil || innerHelp.ActiveParameter != 1 || len(innerHelp.Signatures[0].Parameters) != 2 {
		t.Fatalf("nested call should report inner call's second argument, got %#v", innerHelp)
	}
	outerOffset := strings.Index(source, ", true") + 1
	outerHelp := server.signatureHelp(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, outerOffset)})
	if outerHelp == nil || outerHelp.ActiveParameter != 2 || len(outerHelp.Signatures[0].Parameters) != 3 {
		t.Fatalf("outer call should report its third argument after nested call, got %#v", outerHelp)
	}
}

func TestSignatureHelpResolvesGenericCallsWithTypeArguments(t *testing.T) {
	source := "package main\nfunc target[T any](first T, second string) {}\nfunc caller() { target[int](1, \"x\") }\n"
	file, err := compiler.ParseFile("generic.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "generic.gpp")
	file.SourcePath = path
	state := &fileState{Path: path, URI: uriForPath(path), Source: source, File: file}
	workspace := &workspaceState{files: map[string]*fileState{path: state}}
	workspace.docIndex = compiler.BuildDocIndex([]*compiler.File{file})
	workspace.symbols = workspace.collectSymbols()
	server := &server{workspace: workspace}
	cursor := strings.Index(source, ", \"x\"") + len(", ")
	help := server.signatureHelp(textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: state.URI}, Position: positionAtOffset(source, cursor)})
	if help == nil || help.ActiveParameter != 1 || len(help.Signatures) != 1 || len(help.Signatures[0].Parameters) != 2 || !strings.Contains(help.Signatures[0].Label, "target[T any]") {
		t.Fatalf("generic call should resolve its signature and second parameter, got %#v", help)
	}
}

func completionLabels(items []CompletionItem) map[string]bool {
	result := map[string]bool{}
	for _, item := range items {
		result[item.Label] = true
	}
	return result
}
