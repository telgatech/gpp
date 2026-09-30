package lsp

import (
	"sort"
	"strings"

	"github.com/telgatech/gpp/compiler"
)

func (s *server) contextualCompletions(state *fileState, offset int) []CompletionItem {
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

	if strings.HasSuffix(strings.TrimRight(prefix, " \t\r\n"), ".") {
		receiverText := strings.TrimSuffix(strings.TrimRight(prefix, " \t\r\n"), ".")
		receiverText = strings.TrimSuffix(receiverText, "?")
		receiver := selectorChainBefore(receiverText)
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
		if packageName, ok := s.logicalPackageForQualifier(state, receiver); ok {
			for _, info := range s.workspace.symbols {
				if info.Symbol.Package == packageName {
					add(completionForSymbol(info.Symbol))
				}
			}
			return sortedCompletionItems(items)
		}
		typeName := s.receiverType(state, receiver, offset)
		if typeName != "" {
			for _, info := range s.workspace.symbols {
				if info.Symbol.Parent == typeName && (info.Symbol.Kind == compiler.DocField || info.Symbol.Kind == compiler.DocMethod || info.Symbol.Kind == compiler.DocExtension || info.Symbol.Kind == compiler.DocValue) {
					add(completionForSymbol(info.Symbol))
				}
			}
			for _, extension := range s.extensionMethodsFor(typeName) {
				add(completionForSymbol(extension))
			}
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
		if info.Symbol.Package == "prelude" || info.Symbol.Package == currentPackage {
			switch info.Symbol.Kind {
			case compiler.DocClass, compiler.DocEnum, compiler.DocFunction, compiler.DocAnnotation, compiler.DocTemplate, compiler.DocValue:
				add(completionForSymbol(info.Symbol))
			}
		}
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

func completionForSymbol(symbol compiler.DocSymbol) CompletionItem {
	return CompletionItem{Label: symbol.Name, Kind: lspSymbolKind(symbol.Kind), Detail: symbol.Signature, Documentation: symbol.Doc, Deprecated: deprecated(symbol)}
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
	for index, token := range tokens {
		if token.Span.Start < span.Start || token.Span.Start >= span.End || token.Text != name || token.Kind != compiler.TokenIdentifier || index+1 >= len(tokens) {
			continue
		}
		next := tokens[index+1]
		if next.Kind == compiler.TokenIdentifier {
			return next.Text
		}
		if next.Text == "*" && index+2 < len(tokens) && tokens[index+2].Kind == compiler.TokenIdentifier {
			return tokens[index+2].Text
		}
		if next.Text == "[" && index+3 < len(tokens) && tokens[index+3].Kind == compiler.TokenIdentifier {
			return "[]" + tokens[index+3].Text
		}
	}
	return ""
}

func sourceType(source string, span compiler.Span) string {
	if span.Start < 0 || span.End > len(source) || span.Start > span.End {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(source[span.Start:span.End], "*"))
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
