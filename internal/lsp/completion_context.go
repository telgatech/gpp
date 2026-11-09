package lsp

import (
	"go/types"
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

	if functionResultTypeContext(state, offset) {
		for _, info := range s.workspace.symbols {
			if info.Symbol.Package != "prelude" && info.Symbol.Package != state.File.Package {
				continue
			}
			if info.Symbol.Kind == compiler.DocClass || info.Symbol.Kind == compiler.DocEnum || info.Symbol.Kind == compiler.DocType {
				add(completionForSymbol(info.Symbol))
			}
		}
		for _, item := range s.importedTypeCompletions(state) {
			add(item)
		}
		for _, item := range s.importTypeCompletions(state) {
			add(item)
		}
		for _, name := range []string{"any", "bool", "byte", "complex64", "complex128", "error", "float32", "float64", "int", "int8", "int16", "int32", "int64", "rune", "string", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "struct"} {
			add(CompletionItem{Label: name, Kind: 25, Detail: "type"})
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
				if !s.classMemberVisibleInCompletion(state, offset, info.Symbol) {
					continue
				}
				add(completionForSymbol(contextualizeMethodForClass(info.Symbol, className)))
			}
			return sortedCompletionItems(items)
		}
		for _, info := range s.nativeGoMembersForExpression(state, receiver, offset) {
			add(completionForSymbol(info.Symbol))
		}
		for _, info := range s.nativeGoMembersForGoPlusExpression(state, receiver, offset) {
			add(completionForSymbol(info.Symbol))
		}
		typeName := s.receiverTypeForExpression(state, receiver, len(selectorPrefix)-1, offset)
		if typeName != "" {
			if packageName, packageDir, memberType, ok := s.importedTypePackage(state, typeName); ok {
				var matched []CompletionItem
				for _, info := range s.packageSymbols(packageName, packageDir) {
					if info.Symbol.Parent == memberType && (info.Symbol.Kind == compiler.DocField || info.Symbol.Kind == compiler.DocMethod || info.Symbol.Kind == compiler.DocExtension || info.Symbol.Kind == compiler.DocValue) {
						if !s.classMemberVisibleInCompletion(state, offset, info.Symbol) {
							continue
						}
						matched = append(matched, completionForSymbol(info.Symbol))
					}
				}
				if len(matched) == 0 {
					for _, info := range s.inheritedMembersForType(packageName, packageDir, memberType, "") {
						if !s.classMemberVisibleInCompletion(state, offset, info.Symbol) {
							continue
						}
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
					if !s.classMemberVisibleInCompletion(state, offset, info.Symbol) {
						continue
					}
					if className != "" {
						add(completionForSymbol(contextualizeMethodForClass(info.Symbol, className)))
					} else {
						add(completionForSymbol(info.Symbol))
					}
				}
				for _, info := range s.classMembersForType(state, typeName, "", true) {
					if !s.classMemberVisibleInCompletion(state, offset, info.Symbol) {
						continue
					}
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
		if fields := s.returnedStructFieldCompletions(state, receiver, len(selectorPrefix)-1); len(fields) > 0 {
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

func functionResultTypeContext(state *fileState, offset int) bool {
	if state == nil || state.File == nil {
		return false
	}
	tokens := state.File.Tokens
	for index, token := range tokens {
		if token.Text != "func" || token.Span.Start >= offset {
			continue
		}
		close, ok := functionParameterList(tokens, index)
		if !ok || offset <= tokens[close].Span.End {
			continue
		}
		body := functionBodyOpen(tokens, close+1)
		if body < 0 || offset <= tokens[body].Span.Start {
			return true
		}
	}
	return false
}

func functionParameterList(tokens []compiler.Token, functionIndex int) (int, bool) {
	index := nextCompletionToken(tokens, functionIndex+1)
	if index < 0 {
		return 0, false
	}
	if tokens[index].Text == "(" {
		receiverClose := matchingCompletionToken(tokens, index, "(", ")")
		if receiverClose < 0 {
			return 0, false
		}
		afterReceiver := nextCompletionToken(tokens, receiverClose+1)
		if afterReceiver >= 0 && tokens[afterReceiver].Kind == compiler.TokenIdentifier {
			afterName := nextCompletionToken(tokens, afterReceiver+1)
			if afterName >= 0 && tokens[afterName].Text == "[" {
				genericClose := matchingCompletionToken(tokens, afterName, "[", "]")
				if genericClose < 0 {
					return 0, false
				}
				afterName = nextCompletionToken(tokens, genericClose+1)
			}
			if afterName >= 0 && tokens[afterName].Text == "(" {
				index = afterName
			}
		} else {
			return receiverClose, true
		}
	} else if tokens[index].Kind == compiler.TokenIdentifier {
		index = nextCompletionToken(tokens, index+1)
		if index >= 0 && tokens[index].Text == "[" {
			genericClose := matchingCompletionToken(tokens, index, "[", "]")
			if genericClose < 0 {
				return 0, false
			}
			index = nextCompletionToken(tokens, genericClose+1)
		}
	}
	if index < 0 || tokens[index].Text != "(" {
		return 0, false
	}
	close := matchingCompletionToken(tokens, index, "(", ")")
	return close, close >= 0
}

func matchingCompletionToken(tokens []compiler.Token, open int, opening, closing string) int {
	depth := 0
	for index := open; index < len(tokens); index++ {
		if tokens[index].Kind == compiler.TokenComment {
			continue
		}
		switch tokens[index].Text {
		case opening:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func nextCompletionToken(tokens []compiler.Token, index int) int {
	for index < len(tokens) {
		if tokens[index].Kind != compiler.TokenComment && tokens[index].Kind != compiler.TokenNewline && tokens[index].Kind != compiler.TokenEOF {
			return index
		}
		index++
	}
	return -1
}

func functionBodyOpen(tokens []compiler.Token, start int) int {
	parenDepth, bracketDepth, structDepth := 0, 0, 0
	for index := start; index < len(tokens); index++ {
		if tokens[index].Kind == compiler.TokenComment || tokens[index].Kind == compiler.TokenNewline {
			continue
		}
		switch tokens[index].Text {
		case "(":
			parenDepth++
		case ")":
			if parenDepth > 0 {
				parenDepth--
			}
		case "[":
			bracketDepth++
		case "]":
			if bracketDepth > 0 {
				bracketDepth--
			}
		case "{":
			if structDepth > 0 {
				structDepth++
				continue
			}
			previous := nextCompletionTokenBackward(tokens, index-1)
			if previous >= 0 && (tokens[previous].Text == "struct" || tokens[previous].Text == "interface") {
				structDepth = 1
				continue
			}
			if parenDepth == 0 && bracketDepth == 0 {
				return index
			}
		case "}":
			if structDepth > 0 {
				structDepth--
			}
		}
	}
	return -1
}

func nextCompletionTokenBackward(tokens []compiler.Token, index int) int {
	for ; index >= 0; index-- {
		if tokens[index].Kind != compiler.TokenComment && tokens[index].Kind != compiler.TokenNewline && tokens[index].Kind != compiler.TokenEOF {
			return index
		}
	}
	return -1
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
	return s.importCompletionsMatching(state, false)
}

func (s *server) importTypeCompletions(state *fileState) []CompletionItem {
	return s.importCompletionsMatching(state, true)
}

func (s *server) importedTypeCompletions(state *fileState) []CompletionItem {
	if state == nil || state.File == nil {
		return nil
	}
	var result []CompletionItem
	for _, declaration := range state.File.Imports {
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
		if alias == "" || alias == "_" || alias == "." {
			continue
		}
		packageName, packageDir, found := s.importedPackageForQualifier(state, alias)
		if !found {
			continue
		}
		for _, info := range s.packageSymbols(packageName, packageDir) {
			if info.Symbol.Kind != compiler.DocClass && info.Symbol.Kind != compiler.DocEnum && info.Symbol.Kind != compiler.DocType {
				continue
			}
			if info.Symbol.Package != state.File.Package && !info.Symbol.Exported {
				continue
			}
			item := completionForSymbol(info.Symbol)
			item.Label = alias + "." + info.Symbol.Name
			item.InsertText = item.Label
			item.InsertTextFormat = 1
			result = append(result, item)
		}
	}
	return result
}

func (s *server) importCompletionsMatching(state *fileState, typesOnly bool) []CompletionItem {
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
		if !symbol.Exported {
			continue
		}
		if typesOnly {
			if symbol.Kind != compiler.DocClass && symbol.Kind != compiler.DocEnum && symbol.Kind != compiler.DocType {
				continue
			}
		} else if symbol.Kind != compiler.DocClass && symbol.Kind != compiler.DocEnum && symbol.Kind != compiler.DocType && symbol.Kind != compiler.DocFunction {
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
	minStart, parentheses, brackets := 0, 0, 0
	type lambdaBoundary struct {
		start int
		depth int
	}
	var lambdaBoundaries []lambdaBoundary
	for index := 0; index <= end; index++ {
		if tokens[index].Kind == compiler.TokenNewline && parentheses == 0 && brackets == 0 {
			previous := previousSignificantToken(tokens, index-1)
			next := nextSignificantToken(tokens, index+1, end)
			if !selectorLineContinuation(previous, next) {
				minStart = index + 1
			}
			continue
		}
		switch tokens[index].Text {
		case "(":
			parentheses++
		case ")":
			if parentheses > 0 {
				parentheses--
			}
			for len(lambdaBoundaries) > 0 && lambdaBoundaries[len(lambdaBoundaries)-1].depth > parentheses {
				lambdaBoundaries = lambdaBoundaries[:len(lambdaBoundaries)-1]
			}
		case "[":
			brackets++
		case "]":
			if brackets > 0 {
				brackets--
			}
		case "=>":
			lambdaBoundaries = append(lambdaBoundaries, lambdaBoundary{start: index + 1, depth: parentheses})
		case ":=", "=", ";":
			if parentheses == 0 && brackets == 0 {
				minStart = index + 1
			}
		}
	}
	if len(lambdaBoundaries) > 0 && lambdaBoundaries[len(lambdaBoundaries)-1].start > minStart {
		minStart = lambdaBoundaries[len(lambdaBoundaries)-1].start
	}
	// Parse candidate suffixes until one forms a complete expression. This
	// keeps nested calls and selectors together, for example
	// `a.First().ToUpper()`; peeling only the final call would incorrectly
	// return just `ToUpper` as the receiver of the last dot.
	for start := minStart; start <= end; start++ {
		if tokens[start].Kind == compiler.TokenEOF || tokens[start].Kind == compiler.TokenComment {
			continue
		}
		candidate := tokens[start : end+1]
		if expression, err := compiler.ParseExpressionTokens(candidate); err == nil && expression != nil {
			if _, incomplete := expression.(*compiler.TokenExpr); !incomplete {
				span := expression.Span()
				if span.Start == tokens[start].Span.Start && span.End == tokens[end].Span.End {
					return strings.TrimSpace(state.Source[span.Start:span.End])
				}
			}
		}
	}
	return ""
}

func previousSignificantToken(tokens []compiler.Token, index int) compiler.Token {
	for index >= 0 {
		if tokens[index].Kind != compiler.TokenNewline && tokens[index].Kind != compiler.TokenComment {
			return tokens[index]
		}
		index--
	}
	return compiler.Token{}
}

func nextSignificantToken(tokens []compiler.Token, index, end int) compiler.Token {
	for index <= end {
		if tokens[index].Kind != compiler.TokenNewline && tokens[index].Kind != compiler.TokenComment {
			return tokens[index]
		}
		index++
	}
	return compiler.Token{}
}

func selectorLineContinuation(previous, next compiler.Token) bool {
	switch previous.Text {
	case ".", "?.", ",", ":=", "=", "=>", "+", "-", "*", "/", "&&", "||", "??", "(", "[":
		return true
	}
	switch next.Text {
	case ".", "?.", ",", "=>", "+", "-", "*", "/", "&&", "||", "??":
		return true
	}
	return false
}

func (s *server) constructorClassAt(state *fileState, offset int) (string, string, []symbolInfo, bool) {
	if state == nil || state.File == nil {
		return "", "", nil, false
	}
	tokens := state.File.Tokens
	limit := tokenLimitAtOffset(tokens, offset)
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
	if len(classes) == 0 {
		for _, info := range s.packageSymbols(packageName, packageDir) {
			if info.Symbol.Kind == compiler.DocType && info.Symbol.Name == className {
				for _, field := range s.packageSymbols(packageName, filepath.Dir(info.Path)) {
					if field.Symbol.Parent == className && field.Symbol.Kind == compiler.DocField {
						classes = append(classes, info)
						break
					}
				}
			}
		}
	}
	return packageName, className, classes, len(classes) > 0
}

func (s *server) constructorFieldCompletions(state *fileState, offset int) []CompletionItem {
	if state == nil || state.File == nil {
		return nil
	}
	tokens := state.File.Tokens
	limit := tokenLimitAtOffset(tokens, offset)
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
	if open < 0 {
		return nil
	}
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
	members := s.constructorFieldSymbols(packageName, className, classes)
	var result []CompletionItem
	for _, info := range members {
		constructorVisible := info.Symbol.Exported || strings.HasPrefix(info.Symbol.Name, "_") || enclosingClassName(state, offset) == info.Symbol.Parent
		if len(classes) > 0 && classes[0].Symbol.Kind == compiler.DocType {
			constructorVisible = info.Symbol.Exported || packageName == state.File.Package
		}
		if info.Symbol.Kind == compiler.DocField && constructorVisible {
			result = append(result, completionForSymbol(info.Symbol))
		}
	}
	return result
}

func (s *server) classMemberVisibleInCompletion(state *fileState, offset int, symbol compiler.DocSymbol) bool {
	if symbol.Exported {
		return true
	}
	currentClass := enclosingClassName(state, offset)
	if currentClass == symbol.Parent {
		return true
	}
	if strings.HasPrefix(symbol.Name, "_") {
		return s.classContextIsSubclassOf(state, currentClass, symbol.Parent, map[string]bool{})
	}
	return false
}

func (s *server) classContextIsSubclassOf(state *fileState, className, parentName string, visiting map[string]bool) bool {
	if className == "" || parentName == "" || visiting[className] {
		return false
	}
	visiting[className] = true
	defer delete(visiting, className)
	for _, symbol := range s.typeSymbolsForName(state, className) {
		if symbol.Symbol.Kind != compiler.DocClass {
			continue
		}
		file := s.fileForSymbolPath(symbol.Path)
		if file == nil {
			continue
		}
		for _, declaration := range file.Decls {
			class, ok := declaration.(*compiler.ClassDecl)
			if !ok || class.Name != symbol.Symbol.Name {
				continue
			}
			for _, parent := range class.ParentNames() {
				parentLeaf := parent
				if dot := strings.LastIndexByte(parentLeaf, '.'); dot >= 0 {
					parentLeaf = parentLeaf[dot+1:]
				}
				if parentLeaf == parentName || s.classContextIsSubclassOf(state, parent, parentName, visiting) {
					return true
				}
			}
		}
	}
	return false
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
	separator := -1
	if member, ok := identifierTokenAt(tokens, offset); ok {
		memberIndex := tokenIndexAt(tokens, member.Span.Start)
		if memberIndex > 0 && (tokens[memberIndex-1].Text == "." || tokens[memberIndex-1].Text == "?.") {
			separator = memberIndex - 1
		}
	}
	// On the completion trigger itself there may not be a member-name token
	// yet. Resolve the call result from the selector separator at the cursor.
	if separator < 0 {
		for index, token := range tokens {
			if (token.Text == "." || token.Text == "?.") && token.Span.End == offset {
				separator = index
				break
			}
		}
	}
	if separator < 1 {
		return ""
	}
	end := separator - 1
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
	for _, annotation := range symbol.Annotations {
		name := strings.TrimPrefix(annotation, "@")
		if name == "Hook" || strings.HasSuffix(name, ".Hook") {
			return CompletionItem{}
		}
	}
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
	types := callableParameterTypes(signature)
	placeholders := make([]string, len(parameters))
	nextTabStop := 1
	for index, name := range parameters {
		if index < len(types) {
			if lambda := callableLambdaSnippet(types[index], nextTabStop+1); lambda != "" {
				placeholders[index] = "${" + strconv.Itoa(nextTabStop) + ":" + lambda + "}"
				nextTabStop += 2
				continue
			}
		}
		placeholders[index] = "${" + strconv.Itoa(nextTabStop) + ":" + name + "}"
		nextTabStop++
	}
	return label + "(" + strings.Join(placeholders, ", ") + ")$0"
}

func callableParameterTypes(signature string) []string {
	parts, _ := callableSignatureParts(signature)
	result := make([]string, len(parts))
	for partIndex, part := range parts {
		trimmed := strings.TrimSpace(part)
		if strings.HasPrefix(trimmed, "func(") {
			result[partIndex] = trimmed
			continue
		}
		if strings.HasPrefix(trimmed, "...") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "..."))
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 {
			typeStart := strings.Index(trimmed, fields[1])
			if typeStart >= 0 && strings.HasPrefix(trimmed[typeStart:], "func(") {
				result[partIndex] = trimmed[typeStart:]
			}
		}
	}
	return result
}

func callableLambdaSnippet(functionType string, bodyTabStop int) string {
	index := strings.Index(functionType, "func")
	if index < 0 {
		return ""
	}
	open := strings.IndexByte(functionType[index+len("func"):], '(')
	if open < 0 {
		return ""
	}
	open += index + len("func")
	close, ok := matchingSignatureDelimiter(functionType, open, '(', ')')
	if !ok {
		return ""
	}
	var names []string
	for _, parameter := range splitSignatureParameters(functionType[open+1 : close]) {
		fields := strings.Fields(strings.TrimSpace(parameter))
		if len(fields) >= 2 && validSnippetIdentifier(strings.TrimSuffix(fields[0], ",")) {
			names = append(names, strings.TrimSuffix(fields[0], ","))
		} else {
			names = append(names, inferredLambdaParameterName(parameter))
		}
	}
	left := strings.Join(names, ", ")
	if len(names) != 1 {
		left = "(" + left + ")"
	}
	return left + " => ${" + strconv.Itoa(bodyTabStop) + ":}"
}

func inferredLambdaParameterName(typeText string) string {
	typeText = strings.TrimSpace(typeText)
	typeText = strings.TrimPrefix(typeText, "...")
	typeText = strings.TrimLeft(typeText, "*[]")
	if bracket := strings.IndexByte(typeText, '['); bracket >= 0 {
		typeText = typeText[:bracket]
	}
	if dot := strings.LastIndexByte(typeText, '.'); dot >= 0 {
		typeText = typeText[dot+1:]
	}
	if typeText == "" || len(typeText) == 1 && typeText[0] >= 'A' && typeText[0] <= 'Z' {
		return "value"
	}
	runes := []rune(typeText)
	if len(runes) > 0 {
		runes[0] = unicode.ToLower(runes[0])
		if unicode.IsLetter(runes[0]) || runes[0] == '_' {
			return string(runes)
		}
	}
	return "value"
}

func callableParameterNames(signature string) []string {
	parts, _ := callableSignatureParts(signature)
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

func callableSignatureParts(signature string) (parameters, results []string) {
	function := strings.Index(signature, "func")
	if function < 0 {
		return nil, nil
	}
	index := function + len("func")
	functionEnd := index
	skipSpace := func() {
		for index < len(signature) && unicode.IsSpace(rune(signature[index])) {
			index++
		}
	}
	skipSpace()
	if index < len(signature) && signature[index] == '(' && index > functionEnd {
		end, ok := matchingSignatureDelimiter(signature, index, '(', ')')
		if !ok {
			return nil, nil
		}
		afterReceiver := end + 1
		for afterReceiver < len(signature) && unicode.IsSpace(rune(signature[afterReceiver])) {
			afterReceiver++
		}
		// A method signature has a receiver followed by its name. A bare
		// function type starts its parameter list immediately after `func`.
		if afterReceiver < len(signature) && (isIdentifierByte(signature[afterReceiver]) || signature[afterReceiver] == '[') {
			index = afterReceiver
		}
	}
	bracketDepth := 0
	for index < len(signature) {
		switch signature[index] {
		case '[', '<':
			bracketDepth++
		case ']', '>':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '(':
			if bracketDepth == 0 {
				goto parameterList
			}
		}
		index++
	}
	return nil, nil

parameterList:
	if index >= len(signature) || signature[index] != '(' {
		return nil, nil
	}
	parameterEnd, ok := matchingSignatureDelimiter(signature, index, '(', ')')
	if !ok {
		return nil, nil
	}
	parameters = splitSignatureParameters(signature[index+1 : parameterEnd])
	resultText := strings.TrimSpace(signature[parameterEnd+1:])
	if resultText == "" {
		return parameters, nil
	}
	if strings.HasPrefix(resultText, "(") {
		resultEnd, ok := matchingSignatureDelimiter(resultText, 0, '(', ')')
		if !ok {
			return parameters, nil
		}
		resultText = resultText[1:resultEnd]
	}
	return parameters, splitSignatureParameters(resultText)
}

func (s *server) receiverTypeForExpression(state *fileState, receiver string, dotOffset, offset int) string {
	if state == nil || state.File == nil || receiver == "" || dotOffset < len(receiver) {
		return s.receiverType(state, receiver, offset)
	}
	start, end := dotOffset-len(receiver), dotOffset
	if start < 0 || end > len(state.Source) || state.Source[start:end] != receiver {
		return s.receiverType(state, receiver, offset)
	}
	var tokens []compiler.Token
	for _, token := range state.File.Tokens {
		if token.Span.Start >= start && token.Span.End <= end && token.Kind != compiler.TokenEOF {
			tokens = append(tokens, token)
		}
	}
	expression, err := compiler.ParseExpressionTokens(tokens)
	if err != nil || expression == nil {
		return s.receiverType(state, receiver, offset)
	}
	method, _ := enclosingMethod(state, dotOffset)
	if typeName := s.expressionType(state, method, expression, 0); typeName != "" {
		return typeName
	}
	if nativeType := s.nativeGoTypeForGoPlusExpression(state, method, expression, 0); nativeType != nil {
		return types.TypeString(nativeType, func(pkg *types.Package) string {
			if pkg == nil {
				return ""
			}
			return pkg.Name()
		})
	}
	return s.receiverType(state, receiver, offset)
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
	// At the `.` completion trigger there is no member-name token yet. Query
	// the receiver binding just before the separator so local values such as
	// `server.` still resolve to their inferred type.
	if offset > 0 {
		if binding, found := s.localBindingAt(state, offset-1, receiver); found && binding.Type != "" && binding.Type != "inferred" {
			return binding.Type
		}
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
			if tokens[cursor].Text != "record" && tokens[cursor].Text != "struct" || cursor+1 >= len(tokens) || tokens[cursor+1].Text != "(" {
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
						fields[name] = CompletionItem{Label: name, Kind: 5, Detail: "anonymous struct field"}
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

func (s *server) returnedStructFieldCompletions(state *fileState, receiver string, dotOffset int) []CompletionItem {
	fields := s.returnedStructFields(state, receiver, dotOffset)
	result := make([]CompletionItem, 0, len(fields))
	for name := range fields {
		result = append(result, CompletionItem{Label: name, Kind: 5, Detail: "anonymous struct field"})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Label < result[j].Label })
	return result
}

type returnedStructField struct {
	Type   string
	Span   compiler.Span
	Nested map[string]returnedStructField
}

func (s *server) returnedStructFields(state *fileState, receiver string, dotOffset int) map[string]returnedStructField {
	if state == nil || state.File == nil || s.workspace == nil {
		return nil
	}
	receiverStart := dotOffset - len(receiver)
	if receiverStart < 0 || dotOffset > len(state.Source) || state.Source[receiverStart:dotOffset] != receiver {
		return nil
	}
	var tokens []compiler.Token
	for _, token := range state.File.Tokens {
		if token.Span.Start >= receiverStart && token.Span.End <= dotOffset && token.Kind != compiler.TokenEOF {
			tokens = append(tokens, token)
		}
	}
	expression, err := compiler.ParseExpressionTokens(tokens)
	if err != nil || expression == nil {
		return nil
	}
	var path []string
	for {
		selector, ok := expression.(*compiler.SelectorExpr)
		if !ok {
			break
		}
		path = append(path, selector.Name)
		expression = selector.Receiver
	}
	call, ok := expression.(*compiler.CallExpr)
	if !ok {
		return nil
	}
	calleeName := ""
	calleeOffset := call.Callee.Span().Start
	switch callee := call.Callee.(type) {
	case *compiler.NameExpr:
		calleeName = callee.Name
	case *compiler.SelectorExpr:
		calleeName = callee.Name
		calleeOffset = callee.SpanValue.End - len(callee.Name)
	}
	if calleeName == "" {
		return nil
	}
	calleePosition := positionAtOffset(state.Source, calleeOffset)
	files := map[string]*fileState{}
	for _, symbol := range s.resolveSymbolsAt(state.URI, calleePosition, calleeName) {
		if symbol.Symbol.Kind != compiler.DocFunction {
			continue
		}
		if functionFile := s.workspace.files[symbol.Path]; functionFile != nil && functionFile.File != nil {
			files[symbol.Path] = functionFile
		}
	}
	if len(files) == 0 && call.Callee != nil {
		if _, local := call.Callee.(*compiler.NameExpr); local {
			files[state.Path] = state
		}
	}
	fields := map[string]returnedStructField{}
	for _, functionFile := range files {
		for _, function := range functionDeclarations(functionFile.File) {
			if function.Name == calleeName {
				collectReturnedStructFields(s, functionFile, function, function.Method.BodyAST, fields)
			}
		}
	}
	for index := len(path) - 1; index >= 0; index-- {
		field, found := fields[path[index]]
		if !found {
			return nil
		}
		fields = field.Nested
	}
	return fields
}

func (s *server) returnedStructFieldAt(state *fileState, name string, offset int) (returnedStructField, bool) {
	if state == nil || state.File == nil {
		return returnedStructField{}, false
	}
	token, ok := identifierTokenAt(state.File.Tokens, offset)
	if !ok {
		return returnedStructField{}, false
	}
	index := tokenIndexAt(state.File.Tokens, token.Span.Start)
	if index <= 0 || state.File.Tokens[index-1].Text != "." {
		return returnedStructField{}, false
	}
	dot := state.File.Tokens[index-1]
	receiver := selectorReceiverBefore(state, dot.Span.Start)
	field, found := s.returnedStructFields(state, receiver, dot.Span.Start)[name]
	return field, found
}

func (s *server) anonymousStructFieldDeclarationAt(state *fileState, name string, offset int) (returnedStructField, bool) {
	if state == nil || state.File == nil {
		return returnedStructField{}, false
	}
	for _, function := range functionDeclarations(state.File) {
		fields := map[string]returnedStructField{}
		collectReturnedStructFields(s, state, function, function.Method.BodyAST, fields)
		if field, found := returnedStructFieldAtSpan(fields, name, offset); found {
			return field, true
		}
	}
	return returnedStructField{}, false
}

func returnedStructFieldAtSpan(fields map[string]returnedStructField, name string, offset int) (returnedStructField, bool) {
	for fieldName, field := range fields {
		if fieldName == name && field.Span.Start <= offset && offset <= field.Span.End {
			return field, true
		}
		if field, found := returnedStructFieldAtSpan(field.Nested, name, offset); found {
			return field, true
		}
	}
	return returnedStructField{}, false
}

func functionDeclarations(file *compiler.File) []*compiler.FunctionDecl {
	if file == nil {
		return nil
	}
	var result []*compiler.FunctionDecl
	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *compiler.FunctionDecl:
			result = append(result, value)
		case *compiler.MixedDecl:
			result = append(result, value.Functions...)
		}
	}
	return result
}

func collectReturnedStructFields(s *server, state *fileState, function *compiler.FunctionDecl, block *compiler.BlockStmt, fields map[string]returnedStructField) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		switch value := statement.(type) {
		case *compiler.ReturnStmt:
			for _, expression := range value.Values {
				call, ok := expression.(*compiler.CallExpr)
				if !ok {
					continue
				}
				callee, ok := call.Callee.(*compiler.NameExpr)
				if !ok || callee.Name != "struct" && callee.Name != "record" {
					continue
				}
				collectStructLiteralFields(s, state, function, call, fields)
			}
		case *compiler.IfStmt:
			collectReturnedStructFields(s, state, function, value.Body, fields)
			collectReturnedStructFields(s, state, function, value.Else, fields)
			if value.ElseIf != nil {
				collectReturnedStructFields(s, state, function, &compiler.BlockStmt{Statements: []compiler.Stmt{value.ElseIf}}, fields)
			}
		case *compiler.TryStmt:
			collectReturnedStructFields(s, state, function, value.Body, fields)
			for _, clause := range value.Catches {
				collectReturnedStructFields(s, state, function, clause.Body, fields)
			}
			collectReturnedStructFields(s, state, function, value.Finally, fields)
		case *compiler.ForStmt:
			collectReturnedStructFields(s, state, function, value.Body, fields)
		case *compiler.SwitchStmt:
			collectReturnedStructFields(s, state, function, value.Body, fields)
		case *compiler.CaseStmt:
			collectReturnedStructFields(s, state, function, value.Clause.Body, fields)
		case *compiler.BlockStmt:
			collectReturnedStructFields(s, state, function, value, fields)
		case *compiler.TokenStmt:
			collectReturnedStructFields(s, state, function, value.Body, fields)
			collectReturnedStructFields(s, state, function, &compiler.BlockStmt{Statements: value.Children}, fields)
		}
	}
}

func collectStructLiteralFields(s *server, state *fileState, function *compiler.FunctionDecl, literal *compiler.CallExpr, fields map[string]returnedStructField) {
	for _, argument := range literal.Arguments {
		if argument.Name == "" {
			continue
		}
		fieldType := s.expressionType(state, &function.Method, argument.Value, 0)
		if fieldType == "" && argument.Value != nil {
			fieldType = sourceType(state.Source, argument.Value.Span())
		}
		field := returnedStructField{Type: fieldType, Span: argument.NameSpan}
		if nested, ok := anonymousStructLiteralCall(argument.Value); ok {
			field.Type = "anonymous struct"
			field.Nested = map[string]returnedStructField{}
			collectStructLiteralFields(s, state, function, nested, field.Nested)
		}
		fields[argument.Name] = field
	}
}

func anonymousStructLiteralCall(expression compiler.ExprNode) (*compiler.CallExpr, bool) {
	for {
		parenthesized, ok := expression.(*compiler.ParenthesizedExpr)
		if !ok {
			break
		}
		expression = parenthesized.Inner
	}
	call, ok := expression.(*compiler.CallExpr)
	if !ok {
		return nil, false
	}
	callee, ok := call.Callee.(*compiler.NameExpr)
	return call, ok && (callee.Name == "struct" || callee.Name == "record")
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

func tokenLimitAtOffset(tokens []compiler.Token, offset int) int {
	for index, token := range tokens {
		if token.Span.Start >= offset {
			return index
		}
	}
	return len(tokens)
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
