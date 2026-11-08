package lsp

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/telgatech/gpp/compiler"
)

func (s *server) contextualCompletions(state *fileState, offset int) []CompletionItem {
	if s.cancelled() {
		return nil
	}
	items := map[string]CompletionItem{}
	add := func(item CompletionItem) {
		if item.Label != "" {
			items[item.Label] = item
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(state.Source) {
		offset = len(state.Source)
	}
	prefix := state.Source[:offset]
	lineStart := strings.LastIndex(prefix, "\n") + 1
	linePrefix := prefix[lineStart:]
	trimmedLine := strings.TrimSpace(linePrefix)

	if fields := s.constructorFieldCompletions(state, offset); len(fields) > 0 {
		for _, field := range fields {
			add(field)
		}
		return sortedCompletionItems(items)
	}

	if strings.HasPrefix(trimmedLine, "import ") {
		for _, label := range s.logicalPackageCompletions(strings.TrimSpace(strings.TrimPrefix(trimmedLine, "import "))) {
			add(CompletionItem{Label: label, Kind: 9, Detail: "Go++ logical package"})
		}
		return sortedCompletionItems(items)
	}

	if target, annotationContext := annotationCompletionTarget(state, offset); annotationContext {
		for _, declaration := range s.annotationDeclarations(state) {
			if len(declaration.Targets) > 0 && !containsAnnotationTarget(declaration.Targets, target) {
				continue
			}
			add(CompletionItem{Label: declaration.Name, Kind: 13, Detail: "annotation", Documentation: declaration.Doc})
		}
		return sortedCompletionItems(items)
	}

	selectorPrefix := prefix
	if state.File != nil {
		if token, ok := identifierTokenAt(state.File.Tokens, offset); ok && token.Span.Start < offset {
			selectorPrefix = state.Source[:token.Span.Start]
		}
	}
	selectorPrefix = strings.TrimRight(selectorPrefix, " \t\r\n")
	if strings.HasSuffix(selectorPrefix, ".") {
		receiverText := strings.TrimSuffix(selectorPrefix, ".")
		receiverText = strings.TrimSuffix(receiverText, "?")
		receiver := selectorReceiverBefore(state, len(selectorPrefix)-1)
		if receiver == "" {
			receiver = selectorChainBefore(receiverText)
		}
		if receiver == "" {
			if typeName := templateDataType(state, offset); typeName != "" {
				for _, info := range s.workspace.symbols {
					if info.Symbol.Parent == typeName && (info.Symbol.Kind == compiler.DocField || info.Symbol.Kind == compiler.DocMethod || info.Symbol.Kind == compiler.DocValue) {
						add(completionForSymbol(info.Symbol))
					}
				}
				return sortedCompletionItems(items)
			}
		}
		if packageName, packageDir, ok := s.importedPackageForQualifier(state, receiver); ok {
			for _, info := range s.packageSymbols(packageName, packageDir) {
				add(completionForSymbol(info.Symbol))
			}
			return sortedCompletionItems(items)
		}
		if className := s.classTypeExpression(state, receiver, offset); className != "" {
			for _, info := range s.classMembersForType(state, className, "", true) {
				add(completionForSymbol(contextualizeMethodForClass(info.Symbol, className)))
			}
			return sortedCompletionItems(items)
		}
		for _, info := range s.nativeGoMembersForExpression(state, receiver, offset) {
			add(completionForSymbol(info.Symbol))
		}
		typeName := s.receiverType(state, receiver, offset)
		if typeName != "" {
			if packageName, packageDir, memberType, ok := s.importedTypePackage(state, typeName); ok {
				var matched []CompletionItem
				for _, info := range s.packageSymbols(packageName, packageDir) {
					if info.Symbol.Parent == memberType && (info.Symbol.Kind == compiler.DocField || info.Symbol.Kind == compiler.DocMethod || info.Symbol.Kind == compiler.DocExtension || info.Symbol.Kind == compiler.DocValue) {
						matched = append(matched, completionForSymbol(info.Symbol))
					}
				}
				if len(matched) == 0 {
					for _, info := range s.inheritedMembersForType(packageName, packageDir, memberType, "") {
						matched = append(matched, completionForSymbol(info.Symbol))
					}
				}
				for _, item := range matched {
					add(item)
				}
			}
			if !strings.HasPrefix(strings.TrimSpace(typeName), "[]") {
				className := s.classTypeForReceiver(state, receiver, offset)
				for _, info := range s.classMembersForType(state, typeName, "", false) {
					if className != "" {
						add(completionForSymbol(contextualizeMethodForClass(info.Symbol, className)))
					} else {
						add(completionForSymbol(info.Symbol))
					}
				}
				for _, info := range s.classMembersForType(state, typeName, "", true) {
					if className != "" {
						add(completionForSymbol(contextualizeMethodForClass(info.Symbol, className)))
					} else {
						add(completionForSymbol(info.Symbol))
					}
				}
			}
			for _, extension := range s.extensionMembersForType(state, typeName, "") {
				add(completionForSymbol(extension.Symbol))
			}
		}
		if len(items) > 0 {
			return sortedCompletionItems(items)
		}
		if fields := recordFieldCompletions(state, receiver, offset); len(fields) > 0 {
			for _, field := range fields {
				add(field)
			}
			return sortedCompletionItems(items)
		}
		return sortedCompletionItems(items)
	}

	currentPackage := ""
	if state.File != nil {
		currentPackage = state.File.Package
	}
	for _, info := range s.workspace.symbols {
		if s.cancelled() {
			return sortedCompletionItems(items)
		}
		if info.Symbol.Package == "prelude" || info.Symbol.Package == currentPackage {
			switch info.Symbol.Kind {
			case compiler.DocClass, compiler.DocEnum, compiler.DocType, compiler.DocFunction, compiler.DocAnnotation, compiler.DocTemplate, compiler.DocValue:
				add(completionForSymbol(info.Symbol))
			}
		}
	}
	for _, item := range s.importCompletions(state) {
		add(item)
	}

	className := enclosingClassName(state, offset)
	if className != "" {
		add(CompletionItem{Label: "this", Kind: 6, Detail: className})
		for _, info := range s.workspace.symbols {
			if info.Symbol.Parent == className && (info.Symbol.Kind == compiler.DocField || info.Symbol.Kind == compiler.DocMethod || info.Symbol.Kind == compiler.DocValue) {
				add(completionForSymbol(info.Symbol))
			}
		}
	}
	for _, name := range lexicalNames(state, offset) {
		add(CompletionItem{Label: name, Kind: 6, Detail: "in-scope value"})
	}
	for _, keyword := range []string{"class", "enum", "extend", "func", "if", "else", "for", "try", "catch", "throw", "import", "return", "var", "const", "let", "template", "record", "atExit"} {
		add(CompletionItem{Label: keyword, Kind: 14})
	}
	return sortedCompletionItems(items)
}

func (s *server) importedTypePackage(state *fileState, typeName string) (string, string, string, bool) {
	for separator := strings.LastIndexByte(typeName, '.'); separator > 0; separator = strings.LastIndexByte(typeName[:separator], '.') {
		qualifier, memberType := typeName[:separator], typeName[separator+1:]
		if packageName, packageDir, found := s.importedPackageForQualifier(state, qualifier); found && memberType != "" {
			return packageName, packageDir, memberType, true
		}
	}
	return "", "", "", false
}

func (s *server) importCompletions(state *fileState) []CompletionItem {
	if state == nil || state.File == nil {
		return nil
	}
	usedAliases := map[string]string{}
	imported := map[string]bool{}
	for _, declaration := range state.File.Imports {
		imported[declaration.Path] = true
		alias := declaration.Alias
		if alias == "" {
			if declaration.LogicalPackage {
				alias = packageLeaf(declaration.Path)
			} else if packageName, _, found := s.workspacePackageForImport(state, declaration.Path); found {
				alias = packageName
			} else {
				alias = packageLeaf(declaration.Path)
			}
		}
		usedAliases[alias] = declaration.Path
	}
	var result []CompletionItem
	seen := map[string]bool{}
	for _, info := range s.workspace.symbols {
		if s.cancelled() {
			return result
		}
		symbol := info.Symbol
		if info.Path == "" || symbol.Package == "prelude" || filepath.Clean(filepath.Dir(info.Path)) == filepath.Clean(filepath.Dir(state.Path)) {
			continue
		}
		if symbol.Kind != compiler.DocClass && symbol.Kind != compiler.DocEnum && symbol.Kind != compiler.DocType && symbol.Kind != compiler.DocFunction {
			continue
		}
		importPath := symbol.Package
		alias := packageLeaf(symbol.Package)
		importSpec := "import " + symbol.Package
		if strings.HasSuffix(info.Path, ".go") {
			var ok bool
			importPath, ok = moduleImportPath(info.Path)
			if !ok {
				continue
			}
			alias = symbol.Package
			importSpec = "import " + strconv.Quote(importPath)
		}
		if imported[importPath] {
			continue
		}
		alias = uniqueCompletionImportAlias(importPath, alias, usedAliases)
		if alias == "" {
			continue
		}
		usedAliases[alias] = importPath
		label := alias + "." + symbol.Name
		if seen[label] {
			continue
		}
		seen[label] = true
		if strings.HasSuffix(info.Path, ".go") {
			if alias != symbol.Package {
				importSpec = "import " + alias + " " + strconv.Quote(importPath)
			}
		} else if alias != packageLeaf(symbol.Package) {
			importSpec = "import " + alias + " " + symbol.Package
		}
		insertAt, importText := completionImportInsertion(state)
		importText += importSpec + completionLineEnding(state.Source)
		if len(state.File.Imports) == 0 {
			importText += completionLineEnding(state.Source)
		}
		completion := completionForSymbol(symbol)
		if completion.InsertText == "" {
			completion.InsertText = label
		}
		result = append(result, CompletionItem{
			Label: label, Kind: completion.Kind, Detail: completion.Detail,
			Documentation: completion.Documentation, Deprecated: completion.Deprecated,
			InsertText: strings.Replace(completion.InsertText, symbol.Name, label, 1), InsertTextFormat: completion.InsertTextFormat,
			AdditionalTextEdits: []TextEdit{{Range: rangeForOffsets(state.Source, insertAt, insertAt), NewText: importText}},
		})
	}
	return result
}

func uniqueCompletionImportAlias(importPath, preferred string, used map[string]string) string {
	if preferred == "" {
		preferred = packageLeaf(importPath)
	}
	if existing, found := used[preferred]; !found || existing == importPath {
		return preferred
	}
	base := strings.ReplaceAll(strings.ReplaceAll(importPath, "/", "_"), ".", "_")
	if base == "" {
		return ""
	}
	alias := base
	for suffix := 2; ; suffix++ {
		if existing, found := used[alias]; !found || existing == importPath {
			return alias
		}
		alias = base + "_" + strconv.Itoa(suffix)
	}
}

func moduleImportPath(filename string) (string, bool) {
	moduleRoot, modulePath := moduleInfoForPath(filename)
	if moduleRoot == "" || modulePath == "" {
		return "", false
	}
	relativeDir, err := filepath.Rel(moduleRoot, filepath.Dir(filename))
	if err != nil {
		return "", false
	}
	if relativeDir != "." {
		modulePath += "/" + filepath.ToSlash(relativeDir)
	}
	return modulePath, true
}

func (s *server) packageSymbols(packageName, packageDir string) []symbolInfo {
	if packageName == "" && packageDir == "" {
		return nil
	}
	var result []symbolInfo
	for _, info := range s.workspace.symbols {
		if packageName != "" && info.Symbol.Package != packageName {
			continue
		}
		if packageDir != "" {
			if filepath.Clean(filepath.Dir(info.Path)) == packageDir {
				result = append(result, info)
			}
		} else if info.Symbol.Package == packageName {
			result = append(result, info)
		}
	}
	return result
}

func packageLeaf(packageName string) string {
	parts := strings.Split(packageName, ".")
	return parts[len(parts)-1]
}

func packageImportInsertion(source string) int {
	for start := 0; start < len(source); {
		end := strings.IndexByte(source[start:], '\n')
		if end < 0 {
			end = len(source) - start
		}
		lineEnd := start + end
		if strings.HasPrefix(strings.TrimSpace(source[start:lineEnd]), "package ") {
			if lineEnd < len(source) {
				return lineEnd + 1
			}
			return lineEnd
		}
		if lineEnd == len(source) {
			break
		}
		start = lineEnd + 1
	}
	return 0
}

func completionImportInsertion(state *fileState) (int, string) {
	if state == nil {
		return 0, ""
	}
	source := state.Source
	newline := completionLineEnding(source)
	insertAt := packageImportInsertion(source)
	if insertAt > 0 && !strings.HasSuffix(source[:insertAt], newline) {
		return insertAt, newline
	}
	if strings.HasPrefix(source[insertAt:], newline) {
		insertAt += len(newline)
		return insertAt, ""
	}
	return insertAt, newline
}

func completionLineEnding(source string) string {
	if strings.Contains(source, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func templateDataType(state *fileState, offset int) string {
	if state == nil || state.File == nil {
		return ""
	}
	for _, declaration := range state.File.Decls {
		template, ok := declaration.(*compiler.TemplateDecl)
		if !ok || offset < template.BodySpan.Start || offset > template.BodySpan.End || len(template.ParameterAST) == 0 {
			continue
		}
		return parameterTypeFromTokens(state.File.Tokens, template.SpanValue, template.ParameterAST[0].Name)
	}
	return ""
}

func selectorChainBefore(value string) string {
	end := len(value)
	start := end
	for start > 0 {
		character := value[start-1]
		if isIdentifierByte(character) || character == '.' {
			start--
			continue
		}
		break
	}
	return strings.Trim(value[start:end], ".")
}

// selectorReceiverBefore returns the expression immediately to the left of a
// selector dot. Unlike selectorChainBefore, it also handles call results such
// as `pkg.Server(DB: db).Listen`.
func selectorReceiverBefore(state *fileState, dotOffset int) string {
	if state == nil || state.File == nil {
		return ""
	}
	tokens := state.File.Tokens
	dot := -1
	for index, token := range tokens {
		if token.Span.Start == dotOffset && (token.Text == "." || token.Text == "?.") {
			dot = index
			break
		}
	}
	if dot <= 0 {
		return ""
	}
	end := dot - 1
	if tokens[end].Text == ")" {
		depth := 1
		open := end - 1
		for ; open >= 0; open-- {
			switch tokens[open].Text {
			case ")":
				depth++
			case "(":
				depth--
			}
			if depth == 0 {
				break
			}
		}
		if open < 1 {
			return ""
		}
		end = open - 1
	}
	if end < 0 || tokens[end].Kind != compiler.TokenIdentifier {
		return ""
	}
	start := end
	for start >= 2 && tokens[start-1].Text == "." && tokens[start-2].Kind == compiler.TokenIdentifier {
		start -= 2
	}
	return strings.TrimSpace(state.Source[tokens[start].Span.Start:tokens[dot-1].Span.End])
}

func (s *server) constructorClassAt(state *fileState, offset int) (string, string, []symbolInfo, bool) {
	if state == nil || state.File == nil {
		return "", "", nil, false
	}
	tokens := state.File.Tokens
	limit := tokenIndexAt(tokens, offset)
	if limit < 0 || limit > len(tokens) {
		limit = len(tokens)
	}
	var stack []int
	for index := 0; index < limit; index++ {
		switch tokens[index].Text {
		case "(":
			stack = append(stack, index)
		case ")":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(stack) == 0 {
		return "", "", nil, false
	}
	open := stack[len(stack)-1]
	if open == 0 {
		return "", "", nil, false
	}
	calleeEnd := open - 1
	if tokens[calleeEnd].Kind != compiler.TokenIdentifier {
		return "", "", nil, false
	}
	calleeStart := calleeEnd
	for calleeStart >= 2 && tokens[calleeStart-1].Text == "." && tokens[calleeStart-2].Kind == compiler.TokenIdentifier {
		calleeStart -= 2
	}
	callee := state.Source[tokens[calleeStart].Span.Start:tokens[calleeEnd].Span.End]
	var className, packageName, packageDir string
	if separator := strings.LastIndexByte(callee, '.'); separator >= 0 {
		qualifier := callee[:separator]
		className = callee[separator+1:]
		packageName, packageDir, _ = s.importedPackageForQualifier(state, qualifier)
	} else {
		className = callee
		packageName = state.File.Package
		packageDir = filepath.Dir(state.Path)
	}
	if className == "" {
		return "", "", nil, false
	}
	var classes []symbolInfo
	for _, info := range s.packageSymbols(packageName, packageDir) {
		if info.Symbol.Kind == compiler.DocClass && info.Symbol.Name == className {
			classes = append(classes, info)
		}
	}
	return packageName, className, classes, len(classes) > 0
}

func (s *server) constructorFieldCompletions(state *fileState, offset int) []CompletionItem {
	if state == nil || state.File == nil {
		return nil
	}
	tokens := state.File.Tokens
	limit := tokenIndexAt(tokens, offset)
	if limit < 0 || limit > len(tokens) {
		limit = len(tokens)
	}
	packageName, className, classes, ok := s.constructorClassAt(state, offset)
	if !ok {
		return nil
	}
	open := -1
	var delimiters []int
	for index := 0; index < limit; index++ {
		switch tokens[index].Text {
		case "(":
			delimiters = append(delimiters, index)
		case ")":
			if len(delimiters) > 0 {
				delimiters = delimiters[:len(delimiters)-1]
			}
		}
	}
	if len(delimiters) > 0 {
		open = delimiters[len(delimiters)-1]
	}
	if open >= 0 {
		depth := 0
		segmentStart := open + 1
		for index := open + 1; index < limit; index++ {
			switch tokens[index].Text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth > 0 {
					depth--
				}
			case ",":
				if depth == 0 {
					segmentStart = index + 1
				}
			case ":":
				if depth == 0 && index >= segmentStart {
					return nil
				}
			}
		}
	}
	members := s.constructorFieldSymbols(packageName, className, classes)
	var result []CompletionItem
	for _, info := range members {
		if info.Symbol.Kind == compiler.DocField {
			result = append(result, completionForSymbol(info.Symbol))
		}
	}
	return result
}

func (s *server) constructorFieldSymbols(packageName, className string, classes []symbolInfo) []symbolInfo {
	packageDir := ""
	if len(classes) > 0 {
		packageDir = filepath.Dir(classes[0].Path)
	}
	var members []symbolInfo
	for _, info := range s.packageSymbols(packageName, packageDir) {
		if info.Symbol.Parent == className && info.Symbol.Kind == compiler.DocField {
			members = append(members, info)
		}
	}
	members = append(members, s.inheritedMembersForType(packageName, packageDir, className, "")...)
	return members
}

func calledExpressionCallee(state *fileState, expression string, offset int) string {
	if state == nil || state.File == nil {
		return ""
	}
	tokens := state.File.Tokens
	member, ok := identifierTokenAt(tokens, offset)
	if !ok {
		return ""
	}
	memberIndex := tokenIndexAt(tokens, member.Span.Start)
	if memberIndex < 2 || tokens[memberIndex-1].Text != "." {
		return ""
	}
	end := memberIndex - 2
	if end < 0 || tokens[end].Text != ")" {
		return ""
	}
	depth := 1
	open := end - 1
	for ; open >= 0; open-- {
		switch tokens[open].Text {
		case ")":
			depth++
		case "(":
			depth--
		}
		if depth == 0 {
			break
		}
	}
	if open < 1 {
		return ""
	}
	calleeEnd := open - 1
	if tokens[calleeEnd].Kind != compiler.TokenIdentifier {
		return ""
	}
	calleeStart := calleeEnd
	for calleeStart >= 2 && tokens[calleeStart-1].Text == "." && tokens[calleeStart-2].Kind == compiler.TokenIdentifier {
		calleeStart -= 2
	}
	callee := state.Source[tokens[calleeStart].Span.Start:tokens[calleeEnd].Span.End]
	if strings.TrimSpace(state.Source[tokens[calleeStart].Span.Start:tokens[end].Span.End]) != strings.TrimSpace(expression) {
		return ""
	}
	return callee
}

func completionForSymbol(symbol compiler.DocSymbol) CompletionItem {
	item := CompletionItem{Label: symbol.Name, Kind: lspSymbolKind(symbol.Kind), Detail: symbol.Signature, Documentation: symbol.Doc, Deprecated: deprecated(symbol)}
	if symbol.Kind == compiler.DocFunction || symbol.Kind == compiler.DocMethod || symbol.Kind == compiler.DocExtension {
		item.InsertText = callableSnippet(symbol.Name, symbol.Signature)
		item.InsertTextFormat = 2 // LSP InsertTextFormatSnippet
	}
	return item
}

func callableSnippet(label, signature string) string {
	parameters := callableParameterNames(signature)
	if len(parameters) == 0 {
		return label + "()$0"
	}
	placeholders := make([]string, len(parameters))
	for index, name := range parameters {
		placeholders[index] = "${" + strconv.Itoa(index+1) + ":" + name + "}"
	}
	return label + "(" + strings.Join(placeholders, ", ") + ")$0"
}

func callableParameterNames(signature string) []string {
	function := strings.Index(signature, "func")
	if function < 0 {
		return nil
	}
	index := function + len("func")
	skipSpace := func() {
		for index < len(signature) && unicode.IsSpace(rune(signature[index])) {
			index++
		}
	}
	skipSpace()
	if index < len(signature) && signature[index] == '(' {
		end, ok := matchingSignatureDelimiter(signature, index, '(', ')')
		if !ok {
			return nil
		}
		index = end + 1
		skipSpace()
	}
	for index < len(signature) && (isIdentifierByte(signature[index]) || signature[index] == '.' || signature[index] >= 0x80) {
		index++
	}
	skipSpace()
	if index < len(signature) && (signature[index] == '[' || signature[index] == '<') {
		open, close := byte('['), byte(']')
		if signature[index] == '<' {
			open, close = '<', '>'
		}
		end, ok := matchingSignatureDelimiter(signature, index, open, close)
		if !ok {
			return nil
		}
		index = end + 1
		skipSpace()
	}
	if index >= len(signature) || signature[index] != '(' {
		return nil
	}
	end, ok := matchingSignatureDelimiter(signature, index, '(', ')')
	if !ok || end == index+1 {
		return nil
	}
	parts := splitSignatureParameters(signature[index+1 : end])
	result := make([]string, 0, len(parts))
	for partIndex, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		name := ""
		if len(fields) >= 2 && validSnippetIdentifier(fields[0]) {
			name = fields[0]
		} else if len(fields) == 1 && partIndex+1 < len(parts) {
			nextFields := strings.Fields(strings.TrimSpace(parts[partIndex+1]))
			if len(nextFields) >= 2 && validSnippetIdentifier(fields[0]) {
				name = fields[0]
			}
		}
		if name == "" {
			name = "arg" + strconv.Itoa(partIndex+1)
		}
		result = append(result, name)
	}
	return result
}

func splitSignatureParameters(source string) []string {
	var result []string
	start := 0
	depth := [4]int{}
	for index := 0; index < len(source); index++ {
		switch source[index] {
		case '(':
			depth[0]++
		case ')':
			depth[0]--
		case '[':
			depth[1]++
		case ']':
			depth[1]--
		case '{':
			depth[2]++
		case '}':
			depth[2]--
		case '<':
			depth[3]++
		case '>':
			depth[3]--
		case ',':
			if depth == [4]int{} {
				result = append(result, source[start:index])
				start = index + 1
			}
		}
	}
	if tail := strings.TrimSpace(source[start:]); tail != "" {
		result = append(result, tail)
	}
	return result
}

func matchingSignatureDelimiter(source string, start int, open, close byte) (int, bool) {
	if start < 0 || start >= len(source) || source[start] != open {
		return 0, false
	}
	depth := 0
	for index := start; index < len(source); index++ {
		if source[index] == open {
			depth++
		} else if source[index] == close {
			depth--
			if depth == 0 {
				return index, true
			}
		}
	}
	return 0, false
}

func validSnippetIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if index == 0 && !(character == '_' || unicode.IsLetter(character)) {
			return false
		}
		if index > 0 && !(character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character)) {
			return false
		}
	}
	return true
}

func sortedCompletionItems(items map[string]CompletionItem) []CompletionItem {
	result := make([]CompletionItem, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Label < result[j].Label })
	return result
}

func (s *server) logicalPackageCompletions(prefix string) []string {
	prefix = strings.Trim(prefix, "\"'")
	var result []string
	seen := map[string]bool{}
	if s.workspace.docIndex == nil {
		return result
	}
	for packageName := range s.workspace.docIndex.Packages {
		if !strings.HasPrefix(packageName, prefix) || packageName == prefix {
			continue
		}
		suffix := strings.TrimPrefix(packageName, prefix)
		if strings.HasSuffix(prefix, ".") {
			suffix = strings.TrimPrefix(suffix, ".")
		} else if strings.HasPrefix(suffix, ".") {
			suffix = strings.TrimPrefix(suffix, ".")
		}
		segment := strings.SplitN(suffix, ".", 2)[0]
		if segment != "" && !seen[segment] {
			seen[segment] = true
			result = append(result, segment)
		}
	}
	sort.Strings(result)
	return result
}

func (s *server) logicalPackageForQualifier(state *fileState, qualifier string) (string, bool) {
	if state == nil || state.File == nil || qualifier == "" {
		return "", false
	}
	for _, declaration := range state.File.Imports {
		if !declaration.LogicalPackage {
			continue
		}
		alias := declaration.Alias
		if alias == "" {
			alias = declaration.Path
		}
		if alias == qualifier || strings.HasSuffix(alias, "."+qualifier) {
			return declaration.Path, true
		}
	}
	return "", false
}

// importedPackageForQualifier resolves a selector's package qualifier to the
// package declaration and directory that its import names. Logical Go++
// imports use their package name directly; ordinary Go imports are mapped
// through the workspace module path so same-named declarations in other
// packages cannot leak into lookup results.
func (s *server) importedPackageForQualifier(state *fileState, qualifier string) (string, string, bool) {
	if state == nil || state.File == nil || qualifier == "" {
		return "", "", false
	}
	for _, declaration := range state.File.Imports {
		if declaration.LogicalPackage {
			alias := declaration.Alias
			if alias == "" {
				alias = declaration.Path
			}
			if alias == qualifier || strings.HasSuffix(alias, "."+qualifier) {
				return declaration.Path, "", true
			}
			continue
		}
		if s.workspace != nil {
			for _, official := range s.workspace.official {
				if official != nil && official.OfficialPackage == declaration.Path {
					alias := declaration.Alias
					if alias == "" {
						alias = official.Package[strings.LastIndexByte(official.Package, '.')+1:]
					}
					if alias == qualifier {
						return official.Package, "", true
					}
				}
			}
		}

		packageName, packageDir, found := s.workspacePackageForImport(state, declaration.Path)
		alias := declaration.Alias
		if alias == "" {
			alias = packageName
			if alias == "" {
				alias = packageLeaf(declaration.Path)
			}
		}
		if alias == qualifier {
			if !found {
				return "", "", true
			}
			return packageName, packageDir, true
		}
	}
	return "", "", false
}

func (s *server) workspacePackageForImport(from *fileState, importPath string) (string, string, bool) {
	if s.workspace == nil || from == nil || importPath == "" {
		return "", "", false
	}
	currentModuleRoot, _ := moduleInfoForPath(from.Path)
	var fallbackName, fallbackDir string
	for _, candidate := range s.workspace.files {
		if candidate == nil || candidate.File == nil {
			continue
		}
		moduleRoot, modulePath := moduleInfoForPath(candidate.Path)
		if moduleRoot == "" || modulePath == "" {
			continue
		}
		relativeDir, err := filepath.Rel(moduleRoot, filepath.Dir(candidate.Path))
		if err != nil {
			continue
		}
		candidateImport := modulePath
		if relativeDir != "." {
			candidateImport += "/" + filepath.ToSlash(relativeDir)
		}
		// Go++ supports flat source layouts where each .gpp file declares a
		// package and imports use the module path plus that package name, even
		// though the files all live in the module root directory.
		if relativeDir == "." && isGoPlusFile(candidate.Path) && importPath == modulePath+"/"+candidate.File.Package {
			candidateDir := filepath.Clean(filepath.Dir(candidate.Path))
			if filepath.Clean(moduleRoot) == filepath.Clean(currentModuleRoot) {
				return candidate.File.Package, candidateDir, true
			}
			if fallbackDir != "" && (fallbackDir != candidateDir || fallbackName != candidate.File.Package) {
				return "", "", false
			}
			fallbackName, fallbackDir = candidate.File.Package, candidateDir
			continue
		}
		if candidateImport == importPath {
			candidateDir := filepath.Clean(filepath.Dir(candidate.Path))
			if filepath.Clean(moduleRoot) == filepath.Clean(currentModuleRoot) {
				return candidate.File.Package, candidateDir, true
			}
			if fallbackDir != "" && fallbackDir != candidateDir {
				return "", "", false
			}
			fallbackName, fallbackDir = candidate.File.Package, candidateDir
		}
	}
	return fallbackName, fallbackDir, fallbackDir != ""
}

func (s *server) annotationDeclarations(state *fileState) []*compiler.AnnotationDecl {
	seen := map[string]bool{}
	var result []*compiler.AnnotationDecl
	files := append([]*compiler.File{}, s.workspace.official...)
	for _, fileState := range s.workspace.files {
		if fileState.File != nil {
			files = append(files, fileState.File)
		}
	}
	for _, file := range files {
		for _, declaration := range file.Decls {
			annotation, ok := declaration.(*compiler.AnnotationDecl)
			if !ok || seen[annotation.Package+"."+annotation.Name] {
				continue
			}
			if !annotation.Exported && state != nil && state.File != nil && annotation.Package != state.File.Package {
				continue
			}
			seen[annotation.Package+"."+annotation.Name] = true
			result = append(result, annotation)
		}
	}
	return result
}

func annotationCompletionTarget(state *fileState, offset int) (compiler.AnnotationTarget, bool) {
	if state == nil {
		return "", false
	}
	prefix := state.Source[:offset]
	start := strings.LastIndex(prefix, "@{")
	if start < 0 || strings.Contains(prefix[start+2:], "}") {
		return "", false
	}
	lineStart := strings.LastIndex(prefix[:start], "\n") + 1
	declarationPrefix := strings.TrimSpace(prefix[lineStart:start])
	switch {
	case strings.HasPrefix(declarationPrefix, "package "):
		return compiler.AnnotationTargetPackage, true
	case strings.Contains(declarationPrefix, "template "):
		return compiler.AnnotationTargetTemplate, true
	case strings.Contains(declarationPrefix, "func "):
		if enclosingClassName(state, start) != "" {
			return compiler.AnnotationTargetMethod, true
		}
		return compiler.AnnotationTargetFunction, true
	case strings.Contains(declarationPrefix, "class "):
		return compiler.AnnotationTargetClass, true
	case strings.Contains(declarationPrefix, "enum "):
		return compiler.AnnotationTargetType, true
	}
	if enclosingClassName(state, start) != "" {
		return compiler.AnnotationTargetField, true
	}
	return "", false
}

func containsAnnotationTarget(targets []compiler.AnnotationTarget, target compiler.AnnotationTarget) bool {
	for _, candidate := range targets {
		if candidate == target {
			return true
		}
	}
	return false
}

func enclosingClassName(state *fileState, offset int) string {
	if state == nil || state.File == nil {
		return ""
	}
	for _, declaration := range state.File.Decls {
		class, ok := declaration.(*compiler.ClassDecl)
		if ok && offset >= class.SpanValue.Start && offset <= class.SpanValue.End {
			return class.Name
		}
	}
	return ""
}

func (s *server) receiverType(state *fileState, receiver string, offset int) string {
	if receiver == "" || state == nil || state.File == nil {
		return ""
	}
	if receiver == "this" {
		return enclosingClassName(state, offset)
	}
	if callee := calledExpressionCallee(state, receiver, offset); callee != "" {
		var packageName, packageDir, className string
		if separator := strings.LastIndexByte(callee, '.'); separator >= 0 {
			if importedName, importedDir, found := s.importedPackageForQualifier(state, callee[:separator]); found {
				packageName, packageDir, className = importedName, importedDir, callee[separator+1:]
			}
		} else {
			packageName, packageDir, className = state.File.Package, filepath.Dir(state.Path), callee
		}
		for _, info := range s.packageSymbols(packageName, packageDir) {
			if info.Symbol.Kind == compiler.DocClass && info.Symbol.Name == className {
				return callee
			}
		}
	}
	if token, ok := identifierTokenAt(state.File.Tokens, offset); ok {
		memberIndex := tokenIndexAt(state.File.Tokens, token.Span.Start)
		if memberIndex >= 2 && (state.File.Tokens[memberIndex-1].Text == "." || state.File.Tokens[memberIndex-1].Text == "?.") {
			receiverToken := state.File.Tokens[memberIndex-2]
			if receiverToken.Kind == compiler.TokenIdentifier && receiverToken.Text == receiver {
				if binding, found := s.localBindingAt(state, receiverToken.Span.Start, receiver); found && binding.Type != "" && binding.Type != "inferred" {
					return binding.Type
				}
			}
		}
	}
	if inferred := lambdaElementType(s, state, receiver, offset); inferred != "" {
		return inferred
	}
	if className := enclosingClassName(state, offset); className != "" {
		for _, declaration := range state.File.Decls {
			class, ok := declaration.(*compiler.ClassDecl)
			if !ok || class.Name != className {
				continue
			}
			for _, field := range class.Fields {
				if field.Name == receiver {
					if inferred := parameterTypeFromTokens(state.File.Tokens, field.SpanValue, receiver); inferred != "" {
						return inferred
					}
					if field.TypeAST != nil {
						return sourceType(state.Source, field.TypeAST.Span())
					}
				}
			}
		}
	}
	if state.File != nil {
		for _, declaration := range state.File.Decls {
			class, ok := declaration.(*compiler.ClassDecl)
			if ok {
				for _, method := range class.Methods {
					if method.Owner != state.File || offset < method.BodySpan.Start || offset > method.BodySpan.End {
						continue
					}
					if inferred := parameterTypeFromTokens(state.File.Tokens, method.SpanValue, receiver); inferred != "" {
						return inferred
					}
					for _, parameter := range method.ParameterAST {
						if parameter.Name == receiver && parameter.Type != nil {
							return sourceType(state.Source, parameter.Type.Span())
						}
					}
				}
			}
			function, ok := declaration.(*compiler.FunctionDecl)
			if ok && offset >= function.SourceSpan.Start && offset <= function.SourceSpan.End {
				if inferred := parameterTypeFromTokens(state.File.Tokens, function.SourceSpan, receiver); inferred != "" {
					return inferred
				}
				for _, parameter := range function.Method.ParameterAST {
					if parameter.Name == receiver && parameter.Type != nil {
						return sourceType(state.Source, parameter.Type.Span())
					}
				}
			}
			template, ok := declaration.(*compiler.TemplateDecl)
			if ok && offset >= template.BodySpan.Start && offset <= template.BodySpan.End {
				if inferred := parameterTypeFromTokens(state.File.Tokens, template.SpanValue, receiver); inferred != "" {
					return inferred
				}
				for _, parameter := range template.ParameterAST {
					if parameter.Name == receiver && parameter.Type != nil {
						return sourceType(state.Source, parameter.Type.Span())
					}
				}
			}
		}
	}
	if state.File != nil {
		for _, token := range state.File.Tokens {
			if token.Span.Start >= offset || token.Text != receiver || token.Kind != compiler.TokenIdentifier {
				continue
			}
			if tokenIndex := tokenIndexAt(state.File.Tokens, token.Span.Start); tokenIndex >= 0 && tokenIndex+2 < len(state.File.Tokens) {
				next := state.File.Tokens[tokenIndex+1]
				after := state.File.Tokens[tokenIndex+2]
				if next.Text == ":=" && after.Kind == compiler.TokenIdentifier {
					return after.Text
				}
				if next.Text == ":=" && (after.Kind == compiler.TokenString || after.Kind == compiler.TokenRawString || after.Kind == compiler.TokenRune) {
					return "string"
				}
				if next.Text == "=" && after.Kind == compiler.TokenIdentifier && tokenIndex+3 < len(state.File.Tokens) && state.File.Tokens[tokenIndex+3].Text == "(" {
					return after.Text
				}
			}
		}
	}
	return ""
}

func lambdaElementType(s *server, state *fileState, name string, offset int) string {
	if state == nil || state.File == nil || name == "" {
		return ""
	}
	tokens := state.File.Tokens
	for arrow := len(tokens) - 1; arrow >= 0; arrow-- {
		if tokens[arrow].Text != "=>" || tokens[arrow].Span.Start >= offset || arrow == 0 {
			continue
		}
		parameterStart := arrow - 1
		matches := tokens[parameterStart].Text == name
		if tokens[parameterStart].Text == ")" {
			depth := 1
			for parameterStart--; parameterStart >= 0; parameterStart-- {
				if tokens[parameterStart].Text == ")" {
					depth++
				} else if tokens[parameterStart].Text == "(" {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			for index := parameterStart + 1; index < arrow-1; index++ {
				if tokens[index].Text == name && tokens[index].Kind == compiler.TokenIdentifier {
					matches = true
					break
				}
			}
		}
		if !matches {
			continue
		}
		open := parameterStart
		if tokens[arrow-1].Text != ")" {
			open = arrow - 2
		}
		if open < 0 || tokens[open].Text != "(" || open < 3 {
			return ""
		}
		method := open - 1
		collectionIndex := method - 1
		if collectionIndex >= 0 && tokens[collectionIndex].Text == "." {
			collectionIndex--
		}
		if collectionIndex < 0 {
			return ""
		}
		collection := tokens[collectionIndex].Text
		collectionType := s.receiverType(state, collection, offset)
		if strings.HasPrefix(collectionType, "[]") {
			return strings.TrimPrefix(collectionType, "[]")
		}
		return ""
	}
	return ""
}

func recordFieldCompletions(state *fileState, receiver string, offset int) []CompletionItem {
	if state == nil || state.File == nil || receiver == "" {
		return nil
	}
	tokens := state.File.Tokens
	fields := map[string]CompletionItem{}
	for index := 0; index+3 < len(tokens); index++ {
		if tokens[index].Text != receiver || tokens[index].Span.Start >= offset || tokens[index+1].Text != ":=" && tokens[index+1].Text != "=" {
			continue
		}
		for cursor := index + 2; cursor+1 < len(tokens) && tokens[cursor].Span.Start < offset; cursor++ {
			if tokens[cursor].Text != "record" || cursor+1 >= len(tokens) || tokens[cursor+1].Text != "(" {
				continue
			}
			depth := 0
			for fieldIndex := cursor + 1; fieldIndex < len(tokens) && tokens[fieldIndex].Span.Start < offset; fieldIndex++ {
				switch tokens[fieldIndex].Text {
				case "(":
					depth++
				case ")":
					depth--
				case ":":
					if depth == 1 && fieldIndex > 0 && tokens[fieldIndex-1].Kind == compiler.TokenIdentifier {
						name := tokens[fieldIndex-1].Text
						fields[name] = CompletionItem{Label: name, Kind: 10, Detail: "record field"}
					}
				}
			}
		}
	}
	result := make([]CompletionItem, 0, len(fields))
	for _, field := range fields {
		result = append(result, field)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Label < result[j].Label })
	return result
}

func parameterTypeFromTokens(tokens []compiler.Token, span compiler.Span, name string) string {
	searchStart, searchEnd := span.Start, span.End
	for index, token := range tokens {
		if token.Span.Start < span.Start || token.Span.Start >= span.End || token.Text != "func" && token.Text != "template" && token.Text != "annotation" {
			continue
		}
		for open := index + 1; open < len(tokens) && tokens[open].Span.Start < span.End; open++ {
			if tokens[open].Text != "(" {
				continue
			}
			depth := 1
			for close := open + 1; close < len(tokens) && tokens[close].Span.Start < span.End; close++ {
				switch tokens[close].Text {
				case "(":
					depth++
				case ")":
					depth--
					if depth == 0 {
						searchStart, searchEnd = tokens[open].Span.End, tokens[close].Span.Start
						open = len(tokens)
						close = len(tokens)
					}
				}
			}
			break
		}
		break
	}
	for index, token := range tokens {
		if token.Span.Start < searchStart || token.Span.Start >= searchEnd || token.Text != name || token.Kind != compiler.TokenIdentifier || index+1 >= len(tokens) {
			continue
		}
		start := index + 1
		var prefix strings.Builder
		for start < len(tokens) && (tokens[start].Text == "*" || tokens[start].Text == "[" && start+1 < len(tokens) && tokens[start+1].Text == "]") {
			if tokens[start].Text == "*" {
				prefix.WriteByte('*')
				start++
			} else {
				prefix.WriteString("[]")
				start += 2
			}
		}
		if start >= len(tokens) || tokens[start].Kind != compiler.TokenIdentifier {
			continue
		}
		typeName := tokens[start].Text
		start++
		for start+1 < len(tokens) && tokens[start].Text == "." && tokens[start+1].Kind == compiler.TokenIdentifier {
			typeName += "." + tokens[start+1].Text
			start += 2
		}
		return prefix.String() + typeName
	}
	return ""
}

func sourceType(source string, span compiler.Span) string {
	if span.Start < 0 || span.End > len(source) || span.Start > span.End {
		return ""
	}
	return strings.TrimSpace(source[span.Start:span.End])
}

func tokenIndexAt(tokens []compiler.Token, start int) int {
	for index, token := range tokens {
		if token.Span.Start == start {
			return index
		}
	}
	return -1
}

func (s *server) extensionMethodsFor(typeName string) []compiler.DocSymbol {
	var result []compiler.DocSymbol
	for _, info := range s.workspace.symbols {
		if info.Symbol.Kind != compiler.DocExtension {
			continue
		}
		target := strings.TrimSpace(info.Symbol.Target)
		if target == typeName || target == "string" && typeName == "string" || target == "[]string" && typeName == "[]string" {
			result = append(result, info.Symbol)
		}
	}
	return result
}

func lexicalNames(state *fileState, offset int) []string {
	if state == nil || state.File == nil {
		return nil
	}
	var result []string
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	for _, declaration := range state.File.Decls {
		var body *compiler.BlockStmt
		switch value := declaration.(type) {
		case *compiler.FunctionDecl:
			if value.Method.BodyAST != nil && offset >= value.Method.BodySpan.Start && offset < value.Method.BodySpan.End {
				body = value.Method.BodyAST
				for _, parameter := range value.Method.ParameterAST {
					add(parameter.Name)
				}
			}
		case *compiler.ClassDecl:
			for _, method := range value.Methods {
				if method.BodyAST != nil && offset >= method.BodySpan.Start && offset < method.BodySpan.End {
					body = method.BodyAST
					for _, parameter := range method.ParameterAST {
						add(parameter.Name)
					}
				}
			}
		case *compiler.TemplateDecl:
			if offset >= value.BodySpan.Start && offset <= value.BodySpan.End {
				for _, parameter := range value.ParameterAST {
					add(parameter.Name)
				}
			}
		}
		if body != nil {
			collectBlockNames(body, offset, add)
		}
	}
	return result
}

func collectBlockNames(block *compiler.BlockStmt, offset int, add func(string)) {
	if block == nil || offset < block.SpanValue.Start || offset >= block.SpanValue.End {
		return
	}
	for _, statement := range block.Statements {
		if statement.Span().Start > offset {
			continue
		}
		switch value := statement.(type) {
		case *compiler.DeclarationStmt:
			for _, name := range value.Names {
				add(name.Text)
			}
			for _, expression := range value.Values {
				collectLambdaNames(expression, offset, add)
			}
		case *compiler.AssignmentStmt:
			for _, expression := range append(append([]compiler.ExprNode{}, value.Left...), value.Right...) {
				collectLambdaNames(expression, offset, add)
			}
			if value.Operator == ":=" {
				for _, expression := range value.Left {
					if name, ok := expression.(*compiler.NameExpr); ok {
						add(name.Name)
					}
				}
			}
		case *compiler.IfStmt:
			collectBlockNames(value.Body, offset, add)
			collectBlockNames(value.Else, offset, add)
			if value.ElseIf != nil {
				collectBlockNames(value.ElseIf.Body, offset, add)
			}
		case *compiler.ForStmt:
			for _, expression := range value.RangeKey {
				if name, ok := expression.(*compiler.NameExpr); ok {
					add(name.Name)
				}
			}
			collectBlockNames(value.Body, offset, add)
		case *compiler.TryStmt:
			collectBlockNames(value.Body, offset, add)
			for _, clause := range value.Catches {
				if clause.Body != nil && offset >= clause.Body.SpanValue.Start && offset < clause.Body.SpanValue.End && clause.Binding != "" {
					add(clause.Binding)
				}
				collectBlockNames(clause.Body, offset, add)
			}
			collectBlockNames(value.Finally, offset, add)
		case *compiler.SwitchStmt:
			collectBlockNames(value.Body, offset, add)
		case *compiler.CaseStmt:
			collectBlockNames(value.Clause.Body, offset, add)
		case *compiler.TokenStmt:
			for _, expression := range value.Exprs {
				collectLambdaNames(expression, offset, add)
			}
			if value.Body != nil {
				collectBlockNames(value.Body, offset, add)
			}
			for _, child := range value.Children {
				collectStatementNames(child, offset, add)
			}
		default:
			collectStatementNames(statement, offset, add)
		}
	}
}

func collectStatementNames(statement compiler.Stmt, offset int, add func(string)) {
	switch value := statement.(type) {
	case *compiler.ExpressionStmt:
		collectLambdaNames(value.Expression, offset, add)
	case *compiler.DeclarationStmt:
		for _, expression := range value.Values {
			collectLambdaNames(expression, offset, add)
		}
	case *compiler.AssignmentStmt:
		for _, expression := range append(append([]compiler.ExprNode{}, value.Left...), value.Right...) {
			collectLambdaNames(expression, offset, add)
		}
	case *compiler.ReturnStmt:
		for _, expression := range value.Values {
			collectLambdaNames(expression, offset, add)
		}
	case *compiler.ThrowStmt:
		collectLambdaNames(value.Value, offset, add)
	case *compiler.IfStmt:
		collectLambdaNames(value.Init, offset, add)
		collectLambdaNames(value.Condition, offset, add)
		collectBlockNames(value.Body, offset, add)
		collectBlockNames(value.Else, offset, add)
		if value.ElseIf != nil {
			collectBlockNames(value.ElseIf.Body, offset, add)
		}
	case *compiler.ForStmt:
		collectLambdaNames(value.Init, offset, add)
		collectLambdaNames(value.Condition, offset, add)
		collectLambdaNames(value.Post, offset, add)
		collectLambdaNames(value.RangeExpr, offset, add)
		collectBlockNames(value.Body, offset, add)
	case *compiler.TryStmt:
		collectBlockNames(value.Body, offset, add)
		for _, clause := range value.Catches {
			if clause.Body != nil && offset >= clause.Body.SpanValue.Start && offset < clause.Body.SpanValue.End && clause.Binding != "" {
				add(clause.Binding)
			}
			collectBlockNames(clause.Body, offset, add)
		}
		collectBlockNames(value.Finally, offset, add)
	case *compiler.DeferStmt:
		collectLambdaNames(value.Expression, offset, add)
	case *compiler.GoStmt:
		collectLambdaNames(value.Expression, offset, add)
	case *compiler.SendStmt:
		collectLambdaNames(value.Channel, offset, add)
		collectLambdaNames(value.Value, offset, add)
	case *compiler.SwitchStmt:
		collectLambdaNames(value.Init, offset, add)
		collectLambdaNames(value.Tag, offset, add)
		collectBlockNames(value.Body, offset, add)
	case *compiler.CaseStmt:
		collectBlockNames(value.Clause.Body, offset, add)
	case *compiler.TokenStmt:
		for _, expression := range value.Exprs {
			collectLambdaNames(expression, offset, add)
		}
		collectBlockNames(value.Body, offset, add)
		for _, child := range value.Children {
			collectStatementNames(child, offset, add)
		}
	}
}

func collectLambdaNames(expression compiler.ExprNode, offset int, add func(string)) {
	if expression == nil {
		return
	}
	switch value := expression.(type) {
	case *compiler.LambdaExpr:
		if offset >= value.SpanValue.Start && offset <= value.SpanValue.End {
			for _, parameter := range value.Parameters {
				if parameter.Kind == compiler.TokenIdentifier {
					add(parameter.Text)
				}
			}
			collectBlockNames(value.BlockBody, offset, add)
		}
		collectLambdaNames(value.Body, offset, add)
	case *compiler.UnaryExpr:
		collectLambdaNames(value.Operand, offset, add)
	case *compiler.BinaryExpr:
		collectLambdaNames(value.Left, offset, add)
		collectLambdaNames(value.Right, offset, add)
	case *compiler.AssignmentExpr:
		for _, child := range append(append([]compiler.ExprNode{}, value.Left...), value.Right...) {
			collectLambdaNames(child, offset, add)
		}
	case *compiler.SelectorExpr:
		collectLambdaNames(value.Receiver, offset, add)
	case *compiler.IndexExpr:
		collectLambdaNames(value.Receiver, offset, add)
		collectLambdaNames(value.Index, offset, add)
	case *compiler.IndexListExpr:
		collectLambdaNames(value.Receiver, offset, add)
		for _, child := range value.Indices {
			collectLambdaNames(child, offset, add)
		}
	case *compiler.SliceExpr:
		collectLambdaNames(value.Receiver, offset, add)
		collectLambdaNames(value.Low, offset, add)
		collectLambdaNames(value.High, offset, add)
		collectLambdaNames(value.Max, offset, add)
	case *compiler.TypeAssertExpr:
		collectLambdaNames(value.Expression, offset, add)
	case *compiler.PostfixExpr:
		collectLambdaNames(value.Expression, offset, add)
	case *compiler.SpreadExpr:
		collectLambdaNames(value.Expression, offset, add)
	case *compiler.SendExpr:
		collectLambdaNames(value.Channel, offset, add)
		collectLambdaNames(value.Value, offset, add)
	case *compiler.CallExpr:
		collectLambdaNames(value.Callee, offset, add)
		for _, argument := range value.Arguments {
			collectLambdaNames(argument.Value, offset, add)
		}
	case *compiler.ParenthesizedExpr:
		collectLambdaNames(value.Inner, offset, add)
	case *compiler.CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectLambdaNames(element.Key, offset, add)
			collectLambdaNames(element.Value, offset, add)
		}
	case *compiler.InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectLambdaNames(segment.Expression, offset, add)
		}
	case *compiler.FunctionLiteralExpr:
		collectBlockNames(value.Body, offset, add)
	}
}
