package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)

func ParseFile(name, src string) (*File, error) {
	file := &File{
		Name:    name,
		Source:  src,
		Package: "main",
	}
	if tokens, err := LexSource(name, src); err == nil {
		file.Tokens = tokens
		for _, token := range tokens {
			if token.Kind == TokenComment {
				file.Comments = append(file.Comments, token)
			}
		}
	}
	hasPackage := false

	pos := 0

	for pos < len(src) {
		pos = skipSpace(src, pos)

		if pos >= len(src) {
			break
		}
		declarationPos := pos
		doc := ""
		if candidate, next := leadingDocComments(src, pos); candidate != "" && documentationTargetAt(src, next) {
			doc = candidate
			pos = next
		}

		switch {
		case keywordAt(src, pos, "package"):
			if hasPackage {
				return nil, fmt.Errorf(
					"%s:%d: duplicate package declaration",
					name,
					sourceLine(src, declarationPos),
				)
			}

			end := lineEnd(src, pos)
			line := strings.TrimSpace(src[pos:end])
			packageText := strings.TrimSpace(strings.TrimPrefix(line, "package"))
			nameEnd := 0
			for nameEnd < len(packageText) && !unicode.IsSpace(rune(packageText[nameEnd])) {
				nameEnd++
			}
			packageName := packageText[:nameEnd]
			if !isLogicalPackageName(packageName) {
				return nil, fmt.Errorf(
					"%s:%d: invalid package declaration",
					name,
					sourceLine(src, pos),
				)
			}

			file.Package = packageName
			file.Doc = doc
			rest := strings.TrimSpace(packageText[nameEnd:])
			packageDecl := &PackageDecl{
				Name:      packageName,
				SpanValue: sourceSpan(src, pos, end),
			}
			if rest != "" {
				uses, _, err := parseAnnotationUses(rest, 0)
				if err != nil {
					return nil, fmt.Errorf("%s:%d: invalid package annotations: %w", name, sourceLine(src, pos), err)
				}
				setAnnotationUseLocations(uses, name, sourceLine(src, pos))
				file.Annotations = append(file.Annotations, uses...)
				packageDecl.Annotations = uses
			}
			file.PackageAST = packageDecl
			hasPackage = true
			pos = end

		case legacyUseAt(src, pos):
			return nil, fmt.Errorf(
				"%s:%d: use declarations are no longer supported; use Go import syntax",
				name,
				sourceLine(src, pos),
			)

		case keywordAt(src, pos, "class"):
			class, end, err := parseClass(src, pos)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", name, sourceLine(src, pos), err)
			}
			class.SourceFile = name
			class.Owner = file
			class.SourceLine = sourceLine(src, declarationPos)
			class.SpanValue = sourceSpan(src, declarationPos, end)
			class.Doc = doc
			setClassAnnotationLocations(class, name)
			attachMethodOwners(file, class.Methods)
			attachFieldOwners(file, class.Fields)

			file.Decls = append(file.Decls, class)
			pos = end

		case keywordAt(src, pos, "enum"):
			enums, end, err := parseEnums(src, pos)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", name, sourceLine(src, pos), err)
			}
			for _, enum := range enums {
				enum.SourceFile = name
				enum.SourceLine = sourceLine(src, declarationPos)
				enum.SpanValue = sourceSpan(src, declarationPos, end)
				enum.Doc = doc
			}
			for _, enum := range enums {
				file.Decls = append(file.Decls, enum)
			}
			pos = end

		case keywordAt(src, pos, "extend"):
			extend, end, err := parseExtend(src, pos)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", name, sourceLine(src, pos), err)
			}
			extend.SourceFile = name
			extend.SourceLine = sourceLine(src, declarationPos)
			extend.SpanValue = sourceSpan(src, declarationPos, end)
			extend.Doc = doc
			attachMethodOwners(file, extend.Methods)
			for index := range extend.Methods {
				setMethodAnnotationLocations(&extend.Methods[index], name, extend.SourceLine)
			}
			file.Decls = append(file.Decls, extend)
			pos = end

		case keywordAt(src, pos, "annotation"):
			declarations, end, err := parseAnnotationDecls(src, pos)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", name, sourceLine(src, pos), err)
			}
			for _, declaration := range declarations {
				declaration.SourceFile = name
				declaration.SourceLine = sourceLine(src, declarationPos)
				declaration.SpanValue = sourceSpan(src, declarationPos, end)
				declaration.Doc = doc
				declaration.Exported = isExportedIdentifier(declaration.Name)
			}
			file.Decls = append(file.Decls, declarationsToDecls(declarations)...)
			pos = end

		case keywordAt(src, pos, "embed"):
			embed, end, err := parseEmbed(src, pos)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", name, sourceLine(src, pos), err)
			}
			embed.SourceFile = name
			embed.SourceLine = sourceLine(src, declarationPos)
			embed.SpanValue = sourceSpan(src, declarationPos, end)
			for index := range embed.Entries {
				embed.Entries[index].SourceFile = name
				embed.Entries[index].SourceLine = sourceLine(src, declarationPos)
			}
			file.Decls = append(file.Decls, embed)
			pos = end

		case keywordAt(src, pos, "template"):
			template, end, err := parseTemplate(src, pos)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", name, sourceLine(src, pos), err)
			}
			template.SourceFile = name
			template.SourceLine = sourceLine(src, declarationPos)
			template.SpanValue = sourceSpan(src, declarationPos, end)
			template.Doc = doc
			setAnnotationUseLocations(template.Annotations, name, template.SourceLine)
			file.Decls = append(file.Decls, template)
			pos = end

		default:
			start := pos

			next := findNextExtension(src, pos)

			if next == pos {
				return nil, fmt.Errorf(
					"%s:%d: parser stalled near %q",
					name,
					sourceLine(src, pos),
					src[pos:min(pos+30, len(src))],
				)
			}

			if next < 0 {
				next = len(src)
			}

			code := src[start:next]

			if strings.TrimSpace(code) != "" {
				placements, err := collectRawAnnotationPlacements(code, name)
				if err != nil {
					return nil, fmt.Errorf("%s:%d: invalid annotation use: %w", name, sourceLine(src, start), err)
				}
				bodyTokens, _ := LexSource(name, code)
				bodyTokens = rebaseTokens(bodyTokens, src, start)
				imports, importErr := importNodesFromTokens(bodyTokens)
				if importErr != nil {
					return nil, fmt.Errorf("%s:%d: invalid import declaration: %w", name, sourceLine(src, start), importErr)
				}
				file.Imports = append(file.Imports, imports...)
				goASTDecls, goASTFileSet := parseGoDeclarations(name, file.Package, code)
				functions, functionErr := parseTopLevelFunctionsChecked(name, file.Package, code, start, src, doc, declarationPos)
				if functionErr != nil {
					return nil, fmt.Errorf("%s:%d: invalid function body: %w", name, sourceLine(src, start), functionErr)
				}
				for _, function := range functions {
					attachFunctionOwner(file, function)
				}
				if structuredFunctionAnnotations(placements) && standaloneFunctionChunk(src, declarationPos, next, functions) {
					for _, function := range functions {
						promoteLeadingFunctionAnnotations(src, start, function, placements)
						file.Decls = append(file.Decls, function)
					}
					pos = next
					continue
				}
				if structuredFunctionAnnotations(placements) && len(functions) == 0 {
					if value := parseTopLevelValueDecl(bodyTokens); value != nil {
						line, column := sourcePosition(src, start)
						value.Owner = file
						value.SourceSpan = Span{Start: start, End: next, Line: line, Column: column}
						value.SourceFile = name
						value.SourceLine = sourceLine(src, start)
						file.Decls = append(file.Decls, value)
						pos = next
						continue
					}
				}
				if len(functions) == 0 && len(goASTDecls) > 0 {
					line, column := sourcePosition(src, start)
					file.Decls = append(file.Decls, &GoDecl{
						Declarations:         goASTDecls,
						FileSet:              goASTFileSet,
						Tokens:               bodyTokens,
						AnnotationPlacements: placements,
						Owner:                file,
						SourceSpan:           Span{Start: start, End: next, Line: line, Column: column},
						SourceFile:           name,
						SourceLine:           sourceLine(src, start),
					})
					pos = next
					continue
				}
				if len(functions) == 0 && len(goASTDecls) == 0 {
					return nil, fmt.Errorf(
						"%s:%d: unsupported top-level syntax; no structured AST representation",
						name,
						sourceLine(src, start),
					)
				}
				if len(placements) == 0 && len(goASTDecls) > 0 && appendStructuredMixedParts(file, functions, goASTDecls, goASTFileSet, start, next) {
					pos = next
					continue
				}
				line, column := sourcePosition(src, start)
				file.Decls = append(file.Decls, &MixedDecl{
					GoASTDecls:           goASTDecls,
					GoASTFileSet:         goASTFileSet,
					Functions:            functions,
					Tokens:               bodyTokens,
					AnnotationPlacements: placements,
					Owner:                file,
					SourceSpan:           Span{Start: start, End: next, Line: line, Column: column},
					SourceFile:           name,
					SourceLine:           sourceLine(src, start),
				})
			}

			pos = next
		}
	}

	return file, nil
}

// appendStructuredMixedParts splits an otherwise ordinary mixed top-level
// chunk into the AST declarations that the Go parser already recognized. The
// parser historically kept such chunks in MixedDecl so source rewriters could
// preserve ordering; that container is unnecessary when there are no
// annotation placements or opaque remainder. Function bodies retain their
// Go++ AST, while ordinary Go declarations retain their go/ast nodes.
func appendStructuredMixedParts(file *File, functions []*FunctionDecl, goDeclarations []ast.Decl, fileSet *token.FileSet, chunkStart, chunkEnd int) bool {
	if file == nil || fileSet == nil || chunkStart < 0 || chunkEnd < chunkStart || len(goDeclarations) == 0 {
		return false
	}
	prefixLength := len("package " + goPackageName(file.Package) + "\n")
	type item struct {
		start    int
		end      int
		order    int
		function *FunctionDecl
		goDecl   ast.Decl
	}
	items := make([]item, 0, len(functions)+len(goDeclarations))
	order := 0
	for _, function := range functions {
		if function == nil || function.SourceSpan.Start < chunkStart || function.SourceSpan.End > chunkEnd {
			return false
		}
		items = append(items, item{start: function.SourceSpan.Start, end: function.SourceSpan.End, order: order, function: function})
		order++
	}
	for _, declaration := range goDeclarations {
		if declaration == nil {
			continue
		}
		if _, function := declaration.(*ast.FuncDecl); function {
			continue
		}
		startPosition := fileSet.Position(declaration.Pos())
		endPosition := fileSet.Position(declaration.End())
		if !startPosition.IsValid() || !endPosition.IsValid() {
			return false
		}
		start := chunkStart + startPosition.Offset - prefixLength
		end := chunkStart + endPosition.Offset - prefixLength
		if start < chunkStart || end < start || end > chunkEnd {
			return false
		}
		items = append(items, item{start: start, end: end, order: order, goDecl: declaration})
		order++
	}
	if len(items) == 0 {
		return false
	}
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].start != items[right].start {
			return items[left].start < items[right].start
		}
		return items[left].order < items[right].order
	})
	for _, current := range items {
		if current.function != nil {
			file.Decls = append(file.Decls, current.function)
			continue
		}
		line, column := sourcePosition(file.Source, current.start)
		file.Decls = append(file.Decls, &GoDecl{
			Declarations: []ast.Decl{current.goDecl},
			FileSet:      fileSet,
			Tokens:       tokensWithinSpan(file.Tokens, current.start, current.end),
			Owner:        file,
			SourceSpan:   Span{Start: current.start, End: current.end, Line: line, Column: column},
			SourceFile:   file.Name,
			SourceLine:   line,
		})
	}
	return true
}

func tokensWithinSpan(tokens []Token, start, end int) []Token {
	if len(tokens) == 0 || start < 0 || end < start {
		return nil
	}
	result := make([]Token, 0)
	for _, current := range tokens {
		if current.Kind == TokenEOF {
			continue
		}
		if current.Span.Start >= start && current.Span.End <= end {
			result = append(result, current)
		}
	}
	return result
}

func funcDeclAST(declaration ast.Decl) *ast.FuncDecl {
	function, _ := declaration.(*ast.FuncDecl)
	if function != nil && function.Recv == nil {
		return function
	}
	return nil
}

func structuredFunctionAnnotations(placements []AnnotationPlacement) bool {
	for _, placement := range placements {
		if placement.Target != AnnotationTargetFunction && placement.Target != AnnotationTargetParameter {
			return false
		}
	}
	return true
}

func parseTopLevelValueDecl(tokens []Token) *ValueDecl {
	block, err := ParseBodyAST(tokens)
	if err != nil || block == nil || len(block.Statements) != 1 {
		return nil
	}
	declaration, ok := block.Statements[0].(*DeclarationStmt)
	if !ok || len(declaration.Names) == 0 {
		return nil
	}
	return &ValueDecl{
		Keyword: declaration.Keyword,
		Names:   append([]Token(nil), declaration.Names...),
		Type:    declaration.Type,
		Values:  append([]ExprNode(nil), declaration.Values...),
		Tokens:  append([]Token(nil), tokens...),
	}
}

func attachFunctionOwner(file *File, function *FunctionDecl) {
	if function == nil {
		return
	}
	function.Owner = file
	function.Method.Owner = file
	function.Annotations = append([]AnnotationUse(nil), function.Method.Annotations...)
}

func standaloneFunctionChunk(source string, start, end int, functions []*FunctionDecl) bool {
	if len(functions) == 0 || start < 0 || end < start || end > len(source) {
		return false
	}
	position := start
	for _, function := range functions {
		if function == nil {
			return false
		}
		functionStart := function.SourceSpan.Start
		functionEnd := function.SourceSpan.End
		if functionStart < position || functionEnd < functionStart || functionEnd > end {
			return false
		}
		prefix := source[position:functionStart]
		if strings.TrimSpace(prefix) != "" && !leadingAnnotationsOnly(prefix) {
			return false
		}
		position = functionEnd
	}
	suffix := source[position:end]
	if strings.TrimSpace(suffix) == "" {
		return true
	}
	if leadingCommentsOnly(suffix) {
		// Keep trailing documentation/comments attached to the final
		// structured function rather than losing them in a compatibility
		// declaration between adjacent top-level functions.
		functions[len(functions)-1].SourceSpan.End = end
		return true
	}
	return false
}

func leadingAnnotationsOnly(source string) bool {
	position := skipSpace(source, 0)
	for position < len(source) {
		if strings.HasPrefix(source[position:], "//") {
			position = skipSpace(source, lineEnd(source, position))
			continue
		}
		if strings.HasPrefix(source[position:], "/*") {
			end := strings.Index(source[position+2:], "*/")
			if end < 0 {
				return false
			}
			position = skipSpace(source, position+end+4)
			continue
		}
		if source[position] != '@' || position+1 >= len(source) || source[position+1] != '{' {
			return false
		}
		_, end, err := parseAnnotationUses(source, position)
		if err != nil {
			return false
		}
		position = skipSpace(source, end)
	}
	return true
}

func leadingCommentsOnly(source string) bool {
	position := skipSpace(source, 0)
	for position < len(source) {
		if strings.HasPrefix(source[position:], "//") {
			position = skipSpace(source, lineEnd(source, position))
			continue
		}
		if strings.HasPrefix(source[position:], "/*") {
			end := strings.Index(source[position+2:], "*/")
			if end < 0 {
				return false
			}
			position = skipSpace(source, position+end+4)
			continue
		}
		return false
	}
	return true
}

func topLevelDeclarationBoundary(source string, start, position int) bool {
	if keywordAt(source, position, "func") && leadingAnnotationsOnly(source[start:position]) {
		return false
	}
	for _, keyword := range []string{"func", "var", "const", "let", "type", "import"} {
		if keywordAt(source, position, keyword) {
			return true
		}
	}
	return false
}

func promoteLeadingFunctionAnnotations(source string, declarationStart int, function *FunctionDecl, placements []AnnotationPlacement) {
	if function == nil || function.SourceSpan.Start <= declarationStart || !leadingAnnotationsOnly(source[declarationStart:function.SourceSpan.Start]) {
		return
	}
	for _, placement := range placements {
		if placement.Target != AnnotationTargetFunction {
			continue
		}
		function.Annotations = append(function.Annotations, placement.Use)
		function.Method.Annotations = append(function.Method.Annotations, placement.Use)
	}
	function.SourceSpan.Start = declarationStart
	function.SourceLine = sourceLine(source, declarationStart)
}

func parseTopLevelFunctions(filename, packageName, source string, sourceBase int, fullSource, doc string, declarationStart int) []*FunctionDecl {
	tokens, err := LexSource(filename, source)
	if err != nil {
		return nil
	}
	functions := []*FunctionDecl{}
	braceDepth := 0
	for index := 0; index < len(tokens); index++ {
		current := tokens[index]
		if current.Kind == TokenEOF {
			break
		}
		if current.Kind == TokenKeyword && current.Text == "func" && braceDepth == 0 && topLevelFunctionStart(tokens, index) && topLevelFunctionHasName(tokens, index) {
			localStart := current.Span.Start
			method, consumed, methodErr := parseMethod(source[localStart:], 0, sourceBase+localStart, fullSource)
			if methodErr == nil && consumed > 0 {
				functionDoc := doc
				functionSourceStart := localStart
				functionSourceAbsolute := sourceBase + localStart
				if len(functions) == 0 && doc != "" && declarationStart < sourceBase {
					functionSourceAbsolute = declarationStart
				}
				if len(functions) > 0 {
					previousEnd := functions[len(functions)-1].SourceSpan.End - sourceBase
					if candidate, next := leadingDocComments(source, previousEnd); candidate != "" && next == localStart {
						functionDoc = candidate
						functionSourceStart = skipSpace(source, previousEnd)
						functionSourceAbsolute = sourceBase + functionSourceStart
					}
				}
				method.Doc = functionDoc
				owned := []Method{method}
				line, column := sourcePosition(fullSource, functionSourceAbsolute)
				functionSourceText := source[functionSourceStart : localStart+consumed]
				goAST, goASTFileSet := parseGoDeclaration(filename, packageName, functionSourceText)
				functions = append(functions, &FunctionDecl{
					Name:         method.Name,
					Doc:          functionDoc,
					Method:       owned[0],
					GoAST:        funcDeclAST(goAST),
					GoASTFileSet: goASTFileSet,
					SourceSpan:   Span{Start: functionSourceAbsolute, End: sourceBase + localStart + consumed, Line: line, Column: column},
					SourceFile:   filename,
					SourceLine:   line,
				})
				index = tokenIndexAtOrAfter(tokens, localStart+consumed) - 1
				continue
			}
		}
		if current.Kind == TokenPunctuation {
			switch current.Text {
			case "{":
				braceDepth++
			case "}":
				if braceDepth > 0 {
					braceDepth--
				}
			}
		}
	}
	return functions
}

// parseTopLevelFunctionsChecked is the source-parser entry point. The
// transformation helpers intentionally tolerate a fragment that contains no
// complete function, but the frontend must not silently downgrade a malformed
// top-level function to MixedDecl. Once a top-level func token is found, every
// such declaration must have a structured Method/BodyAST representation.
func parseTopLevelFunctionsChecked(filename, packageName, source string, sourceBase int, fullSource, doc string, declarationStart int) ([]*FunctionDecl, error) {
	functions := parseTopLevelFunctions(filename, packageName, source, sourceBase, fullSource, doc, declarationStart)
	tokens, err := LexSource(filename, source)
	if err != nil {
		return nil, err
	}
	topLevelFunctions := 0
	braceDepth := 0
	for index, current := range tokens {
		if current.Kind == TokenEOF {
			break
		}
		if current.Kind == TokenKeyword && current.Text == "func" && braceDepth == 0 && topLevelFunctionStart(tokens, index) && topLevelFunctionHasName(tokens, index) {
			topLevelFunctions++
		}
		switch current.Text {
		case "{":
			braceDepth++
		case "}":
			if braceDepth > 0 {
				braceDepth--
			}
		}
	}
	if topLevelFunctions != len(functions) {
		return nil, fmt.Errorf("top-level function could not be represented by the structured body AST")
	}
	return functions, nil
}

func topLevelFunctionStart(tokens []Token, index int) bool {
	for previous := index - 1; previous >= 0; previous-- {
		token := tokens[previous]
		if token.Kind == TokenComment {
			continue
		}
		if token.Kind == TokenNewline || token.Text == ";" || token.Text == "}" {
			return true
		}
		// A function type (for example, `type Handler func(...) error`),
		// function literal, or variable initializer has a non-separator token
		// immediately before `func`; it is not a top-level declaration.
		return false
	}
	return true
}

func topLevelFunctionHasName(tokens []Token, index int) bool {
	for index++; index < len(tokens); index++ {
		if tokens[index].Kind == TokenComment || tokens[index].Kind == TokenNewline {
			continue
		}
		return tokens[index].Kind == TokenIdentifier
	}
	return false
}

func tokenIndexAtOrAfter(tokens []Token, offset int) int {
	for index, token := range tokens {
		if token.Span.Start >= offset {
			return index
		}
	}
	return len(tokens)
}

func attachMethodOwners(file *File, methods []Method) {
	for index := range methods {
		methods[index].Owner = file
	}
}

func attachFieldOwners(file *File, fields []Field) {
	for index := range fields {
		fields[index].Owner = file
	}
}

func parseGoDeclaration(filename, packageName, source string) (ast.Decl, *token.FileSet) {
	declarations, fileSet := parseGoDeclarations(filename, packageName, source)
	if len(declarations) != 1 {
		return nil, nil
	}
	return declarations[0], fileSet
}

func parseGoDeclarations(filename, packageName, source string) ([]ast.Decl, *token.FileSet) {
	fileSet := token.NewFileSet()
	// Annotation syntax is Go++ metadata, not Go syntax. Preserve its byte
	// spans as whitespace while asking go/parser for the ordinary declaration
	// AST, so annotated Go declarations can still use GoDecl instead of the
	// compatibility MixedDecl path.
	goSource := rewriteLogicalImportsForGoParser(stripAnnotationSyntaxPreserve(source))
	parsed, err := parser.ParseFile(fileSet, filename, "package "+goPackageName(packageName)+"\n"+goSource, parser.ParseComments)
	if err != nil {
		return nil, nil
	}
	return parsed.Decls, fileSet
}

func rewriteLogicalImportsForGoParser(source string) string {
	tokens, err := LexSource("<source>", source)
	if err != nil {
		return source
	}
	type edit struct {
		start, end int
		text       string
	}
	edits := []edit{}
	for index := 0; index < len(tokens); index++ {
		if tokens[index].Text != "import" {
			continue
		}
		cursor := nextSignificantToken(tokens, index+1)
		if cursor >= len(tokens) {
			continue
		}
		grouped := tokens[cursor].Text == "("
		if grouped {
			cursor++
		}
		for cursor < len(tokens) {
			cursor = nextSignificantToken(tokens, cursor)
			if cursor >= len(tokens) || (grouped && tokens[cursor].Text == ")") {
				break
			}
			name, importPath, next, logical, ok := sourceImportSpec(tokens, cursor)
			if !ok || next <= cursor {
				break
			}
			if logical {
				startToken := nextSignificantToken(tokens, cursor)
				endToken := nextSignificantToken(tokens, next-1)
				alias := ""
				if name != nil {
					alias = name.Name + " "
				}
				edits = append(edits, edit{
					start: tokens[startToken].Span.Start,
					end:   tokens[endToken].Span.End,
					text:  alias + strconv.Quote(importPath),
				})
			}
			cursor = next
			if !grouped {
				break
			}
		}
	}
	if len(edits) == 0 {
		return source
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, change := range edits {
		source = source[:change.start] + change.text + source[change.end:]
	}
	return source
}

func parseEmbed(src string, start int) (*EmbedDecl, int, error) {
	pos, err := skipEmbedSpace(src, start+len("embed"))
	if err != nil {
		return nil, 0, err
	}
	result := &EmbedDecl{}

	if pos < len(src) && src[pos] == '(' {
		close, err := findMatchingParen(src, pos)
		if err != nil {
			return nil, 0, err
		}
		if err := parseEmbedEntries(src[pos+1:close], result); err != nil {
			return nil, 0, err
		}
		return result, close + 1, nil
	}

	end := lineEnd(src, start)
	if err := parseEmbedEntries(src[pos:end], result); err != nil {
		return nil, 0, err
	}
	return result, end, nil
}

func parseEmbedEntries(src string, result *EmbedDecl) error {
	pos := 0
	for {
		var err error
		pos, err = skipEmbedSpace(src, pos)
		if err != nil {
			return err
		}
		if pos >= len(src) {
			break
		}

		name, length := readIdent(src[pos:])
		if length == 0 {
			return fmt.Errorf("embed entry requires a symbol name")
		}
		pos += length
		pos = skipSpace(src, pos)
		if pos >= len(src) || src[pos] != '"' {
			return fmt.Errorf("embed entry %s requires a quoted path", name)
		}
		end, err := skipQuoted(src, pos, '"')
		if err != nil {
			return err
		}
		path, err := strconv.Unquote(src[pos : end+1])
		if err != nil {
			return fmt.Errorf("invalid embed path for %s: %w", name, err)
		}
		if path == "" {
			return fmt.Errorf("embed path for %s cannot be empty", name)
		}
		result.Entries = append(result.Entries, EmbedEntry{
			Name:      name,
			Path:      path,
			Directory: strings.HasSuffix(path, "/"),
		})
		pos = end + 1
	}
	if len(result.Entries) == 0 {
		return fmt.Errorf("embed requires at least one entry")
	}
	return nil
}

func skipEmbedSpace(src string, pos int) (int, error) {
	for pos < len(src) {
		switch src[pos] {
		case ' ', '\t', '\r', '\n':
			pos++
		case '/':
			if pos+1 >= len(src) {
				return pos, nil
			}
			if src[pos+1] == '/' {
				pos += 2
				for pos < len(src) && src[pos] != '\n' {
					pos++
				}
				continue
			}
			if src[pos+1] == '*' {
				end := strings.Index(src[pos+2:], "*/")
				if end < 0 {
					return 0, fmt.Errorf("unterminated embed comment")
				}
				pos += end + 4
				continue
			}
			return pos, nil
		default:
			return pos, nil
		}
	}
	return pos, nil
}

func parseTemplate(src string, start int) (*TemplateDecl, int, error) {
	pos := skipSpace(src, start+len("template"))
	name, length := readIdent(src[pos:])
	if length == 0 {
		return nil, 0, fmt.Errorf("template requires a name")
	}
	pos += length
	pos = skipSpace(src, pos)
	if pos >= len(src) || src[pos] != '(' {
		return nil, 0, fmt.Errorf("template %s requires parameter parentheses", name)
	}
	closeParams, err := findMatchingParen(src, pos)
	if err != nil {
		return nil, 0, err
	}
	parameters := strings.TrimSpace(src[pos+1 : closeParams])
	if _, err := parseParameterInfos(parameters); err != nil {
		return nil, 0, fmt.Errorf("template %s has invalid parameters: %w", name, err)
	}
	pos = skipSpace(src, closeParams+1)
	layout := ""
	if pos < len(src) && src[pos] == ':' {
		pos = skipSpace(src, pos+1)
		layoutName, layoutLength := readIdent(src[pos:])
		if layoutLength == 0 {
			return nil, 0, fmt.Errorf("template %s has an invalid layout", name)
		}
		layout = layoutName
		pos = skipSpace(src, pos+layoutLength)
	}
	annotations, next, err := parseOptionalAnnotationUses(src, pos)
	if err != nil {
		return nil, 0, err
	}
	pos = skipSpace(src, next)
	if pos >= len(src) || src[pos] != '{' {
		return nil, 0, fmt.Errorf("template %s requires {", name)
	}
	closeBody, err := findTemplateBodyClose(src, pos)
	if err != nil {
		return nil, 0, err
	}
	body := src[pos+1 : closeBody]
	bodyTokens, _ := LexSource("template body", body)
	bodyTokens = rebaseTokens(bodyTokens, src, pos+1)
	return &TemplateDecl{
		Name:         name,
		ParameterAST: parseParameterNodes(parameters),
		Layout:       layout,
		Annotations:  annotations,
		Body:         body,
		BodyTokens:   bodyTokens,
		BodySpan:     sourceSpan(src, pos+1, closeBody),
	}, closeBody + 1, nil
}

func findTemplateBodyClose(src string, open int) (int, error) {
	depth := 1
	for index := open + 1; index < len(src); index++ {
		if index+1 < len(src) && src[index] == '{' && src[index+1] == '{' {
			end := strings.Index(src[index+2:], "}}")
			if end < 0 {
				return -1, fmt.Errorf("unterminated template action")
			}
			index += end + 3
			continue
		}
		switch src[index] {
		case '"', '\'':
			end, err := skipQuoted(src, index, src[index])
			if err != nil {
				return -1, err
			}
			index = end
		case '`':
			end := strings.IndexByte(src[index+1:], '`')
			if end < 0 {
				return -1, fmt.Errorf("unterminated template raw string")
			}
			index += end + 1
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return index, nil
			}
		}
	}
	return -1, fmt.Errorf("missing template body }")
}

func isLogicalPackageName(name string) bool {
	if name == "" {
		return false
	}

	for _, segment := range strings.Split(name, ".") {
		if !isIdentifier(segment) || isGoKeyword(segment) {
			return false
		}
	}

	return true
}

func isGoKeyword(name string) bool {
	switch name {
	case "break", "default", "func", "interface", "select",
		"case", "defer", "go", "map", "struct", "chan", "else",
		"goto", "package", "switch", "const", "fallthrough", "if",
		"range", "type", "continue", "for", "import", "return", "var":
		return true
	default:
		return false
	}
}

func parseClass(src string, start int) (*ClassDecl, int, error) {
	pos := start + len("class")
	pos = skipSpace(src, pos)

	name, n := readIdent(src[pos:])
	if n == 0 {
		return nil, 0, fmt.Errorf("class requires a name")
	}

	pos += n
	pos = skipSpace(src, pos)
	typeParams := ""
	if pos < len(src) && src[pos] == '[' {
		end, err := findMatchingBracket(src, pos)
		if err != nil {
			return nil, 0, fmt.Errorf("class %s type parameters: %w", name, err)
		}
		typeParams = src[pos : end+1]
		pos = skipSpace(src, end+1)
	}

	class := &ClassDecl{
		Name:          name,
		TypeParamsAST: parseTypeParameterNodes(typeParams),
	}
	if typeParams != "" && len(class.TypeParamsAST) == 0 {
		return nil, 0, fmt.Errorf("class %s has invalid type parameters", name)
	}

	if pos < len(src) && src[pos] == ':' {
		pos++

		for {
			pos = skipSpace(src, pos)

			parent, n := readQualifiedIdent(src[pos:])
			if n == 0 {
				return nil, 0, fmt.Errorf(
					"expected parent class",
				)
			}

			class.ParentAST = append(class.ParentAST, parseTypeText(parent))

			pos += n
			pos = skipSpace(src, pos)

			if pos >= len(src) || src[pos] != ',' {
				break
			}

			pos++
		}
	}

	pos = skipSpace(src, pos)
	annotations, next, err := parseOptionalAnnotationUses(src, pos)
	if err != nil {
		return nil, 0, err
	}
	if next != pos {
		class.Annotations = annotations
		pos = skipSpace(src, next)
	}

	if pos >= len(src) || src[pos] != '{' {
		return nil, 0, fmt.Errorf(
			"class %s requires {",
			name,
		)
	}

	close, err := findMatchingBrace(src, pos)
	if err != nil {
		return nil, 0, err
	}

	body := src[pos+1 : close]

	if err := parseClassBody(class, body, pos+1, src); err != nil {
		return nil, 0, err
	}

	return class, close + 1, nil
}

func readQualifiedIdent(src string) (string, int) {
	name, n := readIdent(src)
	if n == 0 {
		return "", 0
	}
	end := n
	for end < len(src) && src[end] == '.' {
		part, partLength := readIdent(src[end+1:])
		if partLength == 0 {
			break
		}
		name += "." + part
		end += 1 + partLength
	}
	return name, end
}

func parseClassBody(class *ClassDecl, body string, sourceBase int, fullSource string) error {
	pos := 0

	for pos < len(body) {
		pos = skipSpace(body, pos)

		if pos >= len(body) {
			break
		}
		doc := ""
		if candidate, next := leadingDocComments(body, pos); candidate != "" {
			doc = candidate
			pos = next
		}

		if keywordAt(body, pos, "static") {
			staticPos := pos
			pos = skipSpace(body, pos+len("static"))
			if !keywordAt(body, pos, "func") {
				return fmt.Errorf(
					"class %s: static must be followed by func",
					class.Name,
				)
			}
			method, end, err := parseMethod(body, pos, sourceBase, fullSource)
			if err != nil {
				return err
			}
			method.IsStatic = true
			if end <= staticPos {
				return fmt.Errorf("class %s: invalid static method", class.Name)
			}
			method.Doc = doc
			class.Methods = append(class.Methods, method)
			pos = end
			continue
		}

		if keywordAt(body, pos, "func") {
			method, end, err := parseMethod(body, pos, sourceBase, fullSource)

			if err != nil {
				return err
			}

			method.Doc = doc
			class.Methods = append(class.Methods, method)
			pos = end
			continue
		}

		end := lineEnd(body, pos)
		line := strings.TrimSpace(body[pos:end])
		field, err := parseFieldLine(line)
		if err != nil {
			return err
		}

		if line != "" {
			if field.Name == "" {
				return fmt.Errorf(
					"invalid field in class %s: %s",
					class.Name,
					line,
				)
			}
			field.Doc = doc
			fieldStart := sourceBase + pos
			fieldLine, fieldColumn := sourcePosition(fullSource, fieldStart)
			field.SpanValue = Span{Start: fieldStart, End: sourceBase + end, Line: fieldLine, Column: fieldColumn}
			if field.TypeAST != nil {
				if typeText, typeErr := typeNodeSource(field.TypeAST); typeErr == nil {
					if typeOffset := strings.Index(line, typeText); typeOffset >= 0 {
						start := sourceBase + pos + typeOffset
						lineNumber, column := sourcePosition(fullSource, start)
						field.TypeSpan = Span{Start: start, End: start + len(typeText), Line: lineNumber, Column: column}
					}
				}
			}

			class.Fields = append(class.Fields, field)
		}

		pos = end
	}

	return nil
}

func parseFieldLine(line string) (Field, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Field{}, nil
	}
	base := line
	annotations := []AnnotationUse{}
	if at := findAnnotationStart(line, 0); at >= 0 {
		uses, end, err := parseAnnotationUses(line, at)
		if err != nil {
			return Field{}, err
		}
		if strings.TrimSpace(line[end:]) != "" {
			return Field{}, fmt.Errorf("field annotations must appear at the end of a field declaration")
		}
		base = strings.TrimSpace(line[:at])
		annotations = uses
	}
	parts := strings.Fields(base)
	if len(parts) < 2 {
		return Field{}, nil
	}
	typeText := strings.Join(parts[1:], " ")
	return Field{
		Name:        parts[0],
		TypeAST:     parseTypeText(typeText),
		Annotations: annotations,
	}, nil
}

func parseAnnotationDecls(src string, start int) ([]*AnnotationDecl, int, error) {
	pos := skipSpace(src, start+len("annotation"))
	if pos < len(src) && src[pos] == '(' {
		close, err := findMatchingParen(src, pos)
		if err != nil {
			return nil, 0, err
		}
		declarations := []*AnnotationDecl{}
		body := src[pos+1 : close]
		for cursor := 0; cursor < len(body); {
			cursor = skipSpace(body, cursor)
			if cursor >= len(body) {
				break
			}
			end := lineEnd(body, cursor)
			line := strings.TrimSpace(body[cursor:end])
			if line != "" {
				declaration, err := parseAnnotationSpec(line)
				if err != nil {
					return nil, 0, err
				}
				declarations = append(declarations, declaration)
			}
			cursor = end
		}
		if len(declarations) == 0 {
			return nil, 0, fmt.Errorf("annotation group cannot be empty")
		}
		return declarations, close + 1, nil
	}

	end := lineEnd(src, pos)
	line := strings.TrimSpace(src[pos:end])
	if line == "" {
		return nil, 0, fmt.Errorf("annotation declaration requires a name")
	}
	declaration, err := parseAnnotationSpec(line)
	if err != nil {
		return nil, 0, err
	}
	return []*AnnotationDecl{declaration}, end, nil
}

func declarationsToDecls(declarations []*AnnotationDecl) []Decl {
	result := make([]Decl, len(declarations))
	for index, declaration := range declarations {
		result[index] = declaration
	}
	return result
}

func parseAnnotationSpec(src string) (*AnnotationDecl, error) {
	pos := 0
	name, length := readIdent(src)
	if length == 0 {
		return nil, fmt.Errorf("annotation declaration requires a name")
	}
	pos += length
	pos = skipSpace(src, pos)
	params := ""
	if pos < len(src) && src[pos] == '(' {
		close, err := findMatchingParen(src, pos)
		if err != nil {
			return nil, err
		}
		params = src[pos+1 : close]
		pos = close + 1
	}
	rest := strings.TrimSpace(src[pos:])
	targets := []AnnotationTarget{}
	if rest != "" {
		if len(rest) < 2 || rest[:2] != "on" || (len(rest) > 2 && isIdentPart(rest[2])) {
			return nil, fmt.Errorf("invalid annotation declaration suffix %q", rest)
		}
		targetText := strings.TrimSpace(rest[2:])
		if targetText == "" {
			return nil, fmt.Errorf("annotation %s requires a target after on", name)
		}
		parts, err := splitTopLevel(targetText, ',')
		if err != nil {
			return nil, err
		}
		seen := map[AnnotationTarget]bool{}
		for _, part := range parts {
			target := AnnotationTarget(strings.TrimSpace(part))
			if !validAnnotationTarget(target) {
				return nil, fmt.Errorf("unknown annotation target %s", target)
			}
			if seen[target] {
				return nil, fmt.Errorf("annotation target %s is listed more than once", target)
			}
			seen[target] = true
			targets = append(targets, target)
		}
	}
	if _, err := parseParameterInfos(params); err != nil {
		return nil, fmt.Errorf("annotation %s has invalid parameters: %w", name, err)
	}
	return &AnnotationDecl{
		Name:         name,
		ParameterAST: parseParameterNodes(params),
		Targets:      targets,
	}, nil
}

func validAnnotationTarget(target AnnotationTarget) bool {
	switch target {
	case AnnotationTargetClass, AnnotationTargetField, AnnotationTargetMethod,
		AnnotationTargetFunction, AnnotationTargetParameter, AnnotationTargetType,
		AnnotationTargetPackage, AnnotationTargetTemplate:
		return true
	default:
		return false
	}
}

func isExportedIdentifier(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func setAnnotationUseLocations(uses []AnnotationUse, fileName string, line int) {
	for index := range uses {
		uses[index].SourceFile = fileName
		if uses[index].SourceLine == 0 {
			uses[index].SourceLine = line
		}
	}
}

func setClassAnnotationLocations(class *ClassDecl, fileName string) {
	setAnnotationUseLocations(class.Annotations, fileName, class.SourceLine)
	for index := range class.Fields {
		setAnnotationUseLocations(class.Fields[index].Annotations, fileName, class.SourceLine)
	}
	for index := range class.Methods {
		setMethodAnnotationLocations(&class.Methods[index], fileName, class.SourceLine)
	}
}

func setMethodAnnotationLocations(method *Method, fileName string, line int) {
	setAnnotationUseLocations(method.Annotations, fileName, line)
	for _, uses := range method.ParameterAnnotations {
		setAnnotationUseLocations(uses, fileName, line)
	}
}

func parseOptionalAnnotationUses(src string, start int) ([]AnnotationUse, int, error) {
	start = skipSpace(src, start)
	if start >= len(src) || src[start] != '@' {
		return nil, start, nil
	}
	uses, end, err := parseAnnotationUses(src, start)
	return uses, end, err
}

func parseAnnotationUses(src string, start int) ([]AnnotationUse, int, error) {
	if start < 0 || start+2 > len(src) || src[start] != '@' || src[start+1] != '{' {
		return nil, 0, fmt.Errorf("annotation use must start with @{")
	}
	close, err := findMatchingBrace(src, start+1)
	if err != nil {
		return nil, 0, err
	}
	body := strings.TrimSpace(src[start+2 : close])
	if body == "" {
		return nil, 0, fmt.Errorf("annotation use cannot be empty")
	}
	parts, err := splitTopLevel(body, ',')
	if err != nil {
		return nil, 0, err
	}
	uses := make([]AnnotationUse, 0, len(parts))
	for _, part := range parts {
		use, err := parseAnnotationUseSpec(strings.TrimSpace(part))
		if err != nil {
			return nil, 0, err
		}
		uses = append(uses, use)
	}
	return uses, close + 1, nil
}

func parseAnnotationUseSpec(src string) (AnnotationUse, error) {
	pos := 0
	first, length := readIdent(src)
	if length == 0 {
		return AnnotationUse{}, fmt.Errorf("annotation use requires a name")
	}
	pos += length
	for {
		pos = skipSpace(src, pos)
		if pos >= len(src) || src[pos] != '.' {
			break
		}
		pos = skipSpace(src, pos+1)
		part, partLength := readIdent(src[pos:])
		if partLength == 0 {
			return AnnotationUse{}, fmt.Errorf("invalid qualified annotation name")
		}
		first += "." + part
		pos += partLength
	}
	pos = skipSpace(src, pos)
	use := AnnotationUse{Name: first}
	if pos < len(src) {
		if src[pos] != '(' {
			return AnnotationUse{}, fmt.Errorf("unexpected annotation use suffix %q", src[pos:])
		}
		close, err := findMatchingParen(src, pos)
		if err != nil {
			return AnnotationUse{}, err
		}
		if strings.TrimSpace(src[close+1:]) != "" {
			return AnnotationUse{}, fmt.Errorf("unexpected annotation use suffix %q", src[close+1:])
		}
		use.HasArguments = true
		argumentTokens, err := LexSource("annotation arguments", src[pos+1:close])
		if err != nil {
			return AnnotationUse{}, err
		}
		use.ArgumentTokens = splitExpressionTokens(argumentTokens)
		for _, tokens := range use.ArgumentTokens {
			expression, parseErr := ParseExpressionTokens(tokens)
			if parseErr == nil && expression != nil {
				use.ArgumentsAST = append(use.ArgumentsAST, expression)
			}
		}
	}
	return use, nil
}

func findAnnotationStart(src string, start int) int {
	for index := start; index+1 < len(src); index++ {
		if end, ok, _ := copyIgnoredSource(src, index, &strings.Builder{}); ok {
			index = end - 1
			continue
		}
		if src[index] == '@' && src[index+1] == '{' {
			return index
		}
	}
	return -1
}

func findMethodBodyOpen(src string, start int) (int, error) {
	for index := start; index < len(src); index++ {
		if end, ok, err := copyIgnoredSource(src, index, &strings.Builder{}); err != nil {
			return -1, err
		} else if ok {
			index = end - 1
			continue
		}
		if src[index] == '@' && index+1 < len(src) && src[index+1] == '{' {
			end, err := findMatchingBrace(src, index+1)
			if err != nil {
				return -1, err
			}
			index = end
			continue
		}
		if src[index] == '{' {
			return index, nil
		}
	}
	return -1, fmt.Errorf("method missing body")
}

func parseMethodAnnotations(signature string) ([]AnnotationUse, string, error) {
	at := findAnnotationStart(signature, 0)
	if at < 0 {
		return nil, strings.TrimSpace(signature), nil
	}
	uses, end, err := parseAnnotationUses(signature, at)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(signature[end:]) != "" {
		return nil, "", fmt.Errorf("method annotations must appear at the end of a method declaration")
	}
	return uses, strings.TrimSpace(signature[:at]), nil
}

func extractParameterAnnotations(params string) (string, map[string][]AnnotationUse, error) {
	parts, err := splitTopLevel(params, ',')
	if err != nil {
		return "", nil, err
	}
	annotations := map[string][]AnnotationUse{}
	for index, part := range parts {
		at := findAnnotationStart(part, 0)
		if at < 0 {
			continue
		}
		uses, end, err := parseAnnotationUses(part, at)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(part[end:]) != "" {
			return "", nil, fmt.Errorf("parameter annotations must appear at the end of a parameter")
		}
		clean := strings.TrimSpace(part[:at])
		parameters, err := parseParameterInfos(clean)
		if err != nil || len(parameters) == 0 {
			return "", nil, fmt.Errorf("invalid annotated parameter %q", part)
		}
		for _, parameter := range parameters {
			annotations[parameter.Name] = uses
		}
		parts[index] = clean
	}
	return strings.Join(parts, ","), annotations, nil
}

func collectRawAnnotationPlacements(src, fileName string) ([]AnnotationPlacement, error) {
	type pending struct {
		use AnnotationUse
		pos int
	}
	pendingUses := []pending{}
	for index := 0; index < len(src); index++ {
		if end, ok, err := copyIgnoredSource(src, index, &strings.Builder{}); err != nil {
			return nil, err
		} else if ok {
			index = end - 1
			continue
		}
		if src[index] != '@' || index+1 >= len(src) || src[index+1] != '{' {
			continue
		}
		uses, end, err := parseAnnotationUses(src, index)
		if err != nil {
			return nil, err
		}
		for _, use := range uses {
			use.SourceFile = fileName
			use.SourceLine = sourceLine(src, index)
			pendingUses = append(pendingUses, pending{use: use, pos: index})
		}
		index = end - 1
	}
	if len(pendingUses) == 0 {
		return nil, nil
	}
	stripped := stripAnnotationSyntaxPreserve(src)
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, fileName, "package main\n\n"+stripped, 0)
	if err != nil {
		return nil, nil
	}
	prefix := len("package main\n\n")
	placements := make([]AnnotationPlacement, len(pendingUses))
	for index, item := range pendingUses {
		absolute := token.Pos(prefix + item.pos + 1)
		target := AnnotationTargetFunction
		lineStart := strings.LastIndex(src[:item.pos], "\n") + 1
		linePrefix := strings.TrimSpace(src[lineStart:item.pos])
		if strings.HasPrefix(linePrefix, "type ") {
			target = AnnotationTargetType
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			function, ok := node.(*ast.FuncDecl)
			if !ok {
				return true
			}
			if function.Type.Params != nil && absolute >= function.Type.Params.Pos() && absolute <= function.Type.Params.End() {
				target = AnnotationTargetParameter
				return false
			}
			if function.Body != nil && absolute >= function.Type.End() && absolute <= function.Body.Pos() {
				if function.Recv != nil {
					target = AnnotationTargetMethod
				}
				return false
			}
			return true
		})
		ast.Inspect(parsed, func(node ast.Node) bool {
			declaration, ok := node.(*ast.GenDecl)
			if ok && declaration.Tok.String() == "type" && absolute >= declaration.Pos() && absolute <= declaration.End() {
				target = AnnotationTargetType
			}
			return true
		})
		// A postfix annotation sits after the Go declaration's End position,
		// so the AST interval check above cannot see `type T ... @{Marker}`.
		// Recover that ownership from the lossless token stream instead of
		// classifying every non-function postfix annotation as a function.
		if target == AnnotationTargetFunction && topLevelDeclarationKeywordBefore(src, item.pos) == "type" {
			target = AnnotationTargetType
		}
		placements[index] = AnnotationPlacement{Use: item.use, Target: target}
	}
	return placements, nil
}

func topLevelDeclarationKeywordBefore(src string, position int) string {
	tokens, err := LexSource("annotation target", src)
	if err != nil {
		return ""
	}
	depth := 0
	keyword := ""
	for _, token := range tokens {
		if token.Span.Start >= position {
			break
		}
		if token.Kind == TokenPunctuation {
			switch token.Text {
			case "{":
				depth++
			case "}":
				if depth > 0 {
					depth--
				}
			}
		}
		if depth == 0 && token.Kind == TokenKeyword {
			switch token.Text {
			case "func", "var", "const", "let", "type", "import":
				keyword = token.Text
			}
		}
	}
	return keyword
}

func stripAnnotationSyntaxPreserve(src string) string {
	var out strings.Builder
	for index := 0; index < len(src); {
		if end, ok, _ := copyIgnoredSource(src, index, &out); ok {
			index = end
			continue
		}
		if src[index] == '@' && index+1 < len(src) && src[index+1] == '{' {
			end, err := findMatchingBrace(src, index+1)
			if err != nil {
				out.WriteByte(src[index])
				index++
				continue
			}
			for _, char := range src[index : end+1] {
				if char == '\n' || char == '\r' {
					out.WriteRune(char)
				} else {
					out.WriteByte(' ')
				}
			}
			index = end + 1
			continue
		}
		out.WriteByte(src[index])
		index++
	}
	return out.String()
}

func parseExtend(src string, start int) (*ExtendDecl, int, error) {
	pos := skipSpace(src, start+len("extend"))
	bodyOpen := findExtendBodyOpen(src, pos)
	if bodyOpen < 0 {
		return nil, 0, fmt.Errorf("extend declaration requires a target and body")
	}
	targetText, constraints, err := splitExtensionTargetConstraints(src[pos:bodyOpen])
	if err != nil {
		return nil, 0, err
	}
	targets, err := splitTopLevel(targetText, ',')
	if err != nil {
		return nil, 0, err
	}
	for index := range targets {
		targets[index] = strings.TrimSpace(targets[index])
	}
	if len(targets) == 0 || (len(targets) == 1 && targets[0] == "") {
		return nil, 0, fmt.Errorf("extend declaration requires a target type")
	}
	for _, target := range targets {
		if target == "" {
			return nil, 0, fmt.Errorf("extend declaration contains an empty target type")
		}
	}
	close, err := findMatchingBrace(src, bodyOpen)
	if err != nil {
		return nil, 0, err
	}
	container := &ClassDecl{Name: "<extension>"}
	if err := parseClassBody(container, src[bodyOpen+1:close], bodyOpen+1, src); err != nil {
		return nil, 0, err
	}
	if len(container.Fields) > 0 {
		return nil, 0, fmt.Errorf("extension declarations may contain methods only")
	}
	targetAST := make([]TypeNode, 0, len(targets))
	for _, target := range targets {
		targetAST = append(targetAST, parseTypeText(target))
	}
	return &ExtendDecl{TargetAST: targetAST, TargetConstraints: constraints, Methods: container.Methods}, close + 1, nil
}

func splitExtensionTargetConstraints(src string) (string, map[string]TypeNode, error) {
	src = strings.TrimSpace(src)
	where := strings.Index(src, " where ")
	if where < 0 {
		return src, nil, nil
	}
	targets := strings.TrimSpace(src[:where])
	constraintText := strings.TrimSpace(src[where+len(" where "):])
	if targets == "" || constraintText == "" {
		return "", nil, fmt.Errorf("extension target constraints require a target and constraint")
	}
	constraints := map[string]TypeNode{}
	parts, err := splitTopLevel(constraintText, ',')
	if err != nil {
		return "", nil, err
	}
	for _, part := range parts {
		fields := strings.Fields(part)
		if len(fields) < 2 {
			return "", nil, fmt.Errorf("invalid extension target constraint %q", strings.TrimSpace(part))
		}
		if !isTypeParameterName(fields[0]) {
			return "", nil, fmt.Errorf("invalid extension target parameter %q", fields[0])
		}
		constraint := parseTypeText(strings.Join(fields[1:], " "))
		if constraint == nil {
			return "", nil, fmt.Errorf("invalid extension target constraint %q", strings.TrimSpace(part))
		}
		constraints[fields[0]] = constraint
	}
	return targets, constraints, nil
}

func findExtendBodyOpen(src string, start int) int {
	bracketDepth := 0
	for index := start; index < len(src); index++ {
		switch src[index] {
		case '"', '\'', '`':
			end, err := skipQuoted(src, index, src[index])
			if src[index] == '`' {
				end = strings.IndexByte(src[index+1:], '`')
				if end >= 0 {
					end += index + 1
				}
			}
			if err != nil || end < 0 {
				return -1
			}
			index = end
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{':
			if bracketDepth == 0 {
				return index
			}
		case '/':
			if index+1 < len(src) && src[index+1] == '/' {
				index += 2
				for index < len(src) && src[index] != '\n' {
					index++
				}
			} else if index+1 < len(src) && src[index+1] == '*' {
				end := strings.Index(src[index+2:], "*/")
				if end < 0 {
					return -1
				}
				index += end + 3
			}
		}
	}
	return -1
}

func parseMethod(src string, start, sourceBase int, fullSource string) (Method, int, error) {
	pos := start + len("func")
	pos = skipSpace(src, pos)

	name, n := readIdent(src[pos:])

	if n == 0 {
		return Method{}, 0, fmt.Errorf(
			"method requires a name",
		)
	}

	pos += n
	nameStart := sourceBase + pos - n
	pos = skipSpace(src, pos)
	typeParams := ""
	if pos < len(src) && src[pos] == '[' {
		end, err := findMatchingBracket(src, pos)
		if err != nil {
			return Method{}, 0, err
		}
		typeParams = src[pos : end+1]
		pos = skipSpace(src, end+1)
	}

	if pos >= len(src) || src[pos] != '(' {
		return Method{}, 0, fmt.Errorf(
			"method %s requires (",
			name,
		)
	}

	parameterOpen := pos
	paramEnd, err := findMatchingParen(src, parameterOpen)
	if err != nil {
		return Method{}, 0, err
	}

	params, parameterAnnotations, err := extractParameterAnnotations(src[pos+1 : paramEnd])
	if err != nil {
		return Method{}, 0, err
	}

	pos = paramEnd + 1
	pos = skipSpace(src, pos)

	resultStart := pos
	bodyOpen, err := findMethodBodyOpen(src, pos)
	if err != nil {
		return Method{}, 0, fmt.Errorf("method %s: %w", name, err)
	}

	annotations, result, err := parseMethodAnnotations(src[resultStart:bodyOpen])
	if err != nil {
		return Method{}, 0, err
	}

	close, err := findMatchingBrace(src, bodyOpen)
	if err != nil {
		return Method{}, 0, err
	}

	body := src[bodyOpen+1 : close]
	bodyTokens, lexErr := LexSource("method body", body)
	if lexErr != nil {
		return Method{}, 0, fmt.Errorf("method %s body: %w", name, lexErr)
	}
	bodyStart := sourceBase + bodyOpen + 1
	bodyEnd := sourceBase + close
	bodyTokens = rebaseTokens(bodyTokens, fullSource, bodyStart)
	bodyAST, bodyErr := ParseBodyAST(bodyTokens)
	if bodyErr != nil {
		return Method{}, 0, fmt.Errorf("method %s body: %w", name, bodyErr)
	}
	bodyLine, bodyColumn := sourcePosition(fullSource, bodyStart)
	parameterStart := sourceBase + parameterOpen + 1
	parameterLine, parameterColumn := sourcePosition(fullSource, parameterStart)
	resultSpan := Span{}
	if result != "" {
		if resultOffset := strings.Index(src[resultStart:bodyOpen], result); resultOffset >= 0 {
			resultStartOffset := sourceBase + resultStart + resultOffset
			resultLine, resultColumn := sourcePosition(fullSource, resultStartOffset)
			resultSpan = Span{Start: resultStartOffset, End: resultStartOffset + len(result), Line: resultLine, Column: resultColumn}
		}
	}
	return Method{
		Name:                 name,
		NameSpan:             sourceSpan(fullSource, nameStart, nameStart+n),
		TypeParamsAST:        parseTypeParameterNodes(typeParams),
		ParametersSpan:       Span{Start: parameterStart, End: sourceBase + paramEnd, Line: parameterLine, Column: parameterColumn},
		ParameterAST:         parseParameterNodes(params),
		ResultSpan:           resultSpan,
		ResultAST:            parseTypeText(result),
		ResultFieldsAST:      parseResultFieldNodes(result),
		ParameterAnnotations: parameterAnnotations,
		Annotations:          annotations,
		BodySpan:             Span{Start: bodyStart, End: bodyEnd, Line: bodyLine, Column: bodyColumn},
		SpanValue:            sourceSpan(fullSource, sourceBase+start, sourceBase+close+1),
		BodyTokens:           bodyTokens,
		BodyAST:              bodyAST,
	}, close + 1, nil
}

func findMatchingBracket(src string, open int) (int, error) {
	if open >= len(src) || src[open] != '[' {
		return -1, fmt.Errorf("expected [ at %d", open)
	}
	depth := 0
	for index := open; index < len(src); index++ {
		switch src[index] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return index, nil
			}
		case '"', '\'':
			end, err := skipQuoted(src, index, src[index])
			if err != nil {
				return -1, err
			}
			index = end
		case '`':
			end := strings.IndexByte(src[index+1:], '`')
			if end < 0 {
				return -1, fmt.Errorf("unterminated raw string")
			}
			index += end + 1
		}
	}
	return -1, fmt.Errorf("missing ]")
}

func findMatchingParen(src string, open int) (int, error) {
	depth := 0

	for i := open; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++

		case ')':
			depth--

			if depth == 0 {
				return i, nil
			}

		case '"':
			end, err := skipQuoted(src, i, '"')
			if err != nil {
				return -1, err
			}

			i = end

		case '\'':
			end, err := skipQuoted(src, i, '\'')
			if err != nil {
				return -1, err
			}

			i = end

		case '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return -1, fmt.Errorf("unterminated raw string")
			}

			i += end + 1

		case '/':
			if i+1 >= len(src) {
				continue
			}
			if src[i+1] == '/' {
				i += 2
				for i < len(src) && src[i] != '\n' {
					i++
				}
			} else if src[i+1] == '*' {
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return -1, fmt.Errorf("unterminated comment")
				}
				i += end + 3
			}
		}
	}

	return -1, fmt.Errorf("missing )")
}

func findNextExtension(src string, start int) int {
	lineStart := true
	braceDepth := 0

	for i := start; i < len(src); {
		if lineStart && braceDepth == 0 {
			p := skipHorizontal(src, i)

			if keywordAt(src, p, "class") || keywordAt(src, p, "enum") || keywordAt(src, p, "extend") ||
				keywordAt(src, p, "annotation") || keywordAt(src, p, "embed") || keywordAt(src, p, "template") || keywordAt(src, p, "package") ||
				(p > start && topLevelDeclarationBoundary(src, start, p)) {
				return leadingCommentStart(src, start, p)
			}
		}

		switch src[i] {
		case '"', '\'':
			end, err := skipQuoted(src, i, src[i])
			if err != nil {
				return -1
			}
			i = end + 1
			lineStart = false
			continue

		case '`':
			end := strings.IndexByte(src[i+1:], '`')
			if end < 0 {
				return -1
			}
			i += end + 2
			lineStart = false
			continue

		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				i += 2
				for i < len(src) && src[i] != '\n' {
					i++
				}
				continue
			}
			if i+1 < len(src) && src[i+1] == '*' {
				end := strings.Index(src[i+2:], "*/")
				if end < 0 {
					return -1
				}
				i += end + 4
				lineStart = false
				continue
			}

		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '\n':
			lineStart = true
			i++
			continue
		}

		if src[i] != ' ' && src[i] != '\t' && src[i] != '\r' {
			lineStart = false
		}
		i++
	}

	return -1
}

func leadingCommentStart(source string, start, end int) int {
	candidate := end
	for candidate > start {
		probe := candidate - 1
		for probe >= start && (source[probe] == ' ' || source[probe] == '\t' || source[probe] == '\r' || source[probe] == '\n' || source[probe] == '\v' || source[probe] == '\f') {
			probe--
		}
		if probe < start {
			return candidate
		}
		lineStart := strings.LastIndex(source[start:probe+1], "\n")
		if lineStart >= 0 {
			lineStart += start + 1
		} else {
			lineStart = start
		}
		if strings.HasPrefix(strings.TrimSpace(source[lineStart:probe+1]), "//") {
			candidate = lineStart
			continue
		}
		return candidate
	}
	return candidate
}

func legacyUseAt(src string, pos int) bool {
	if !keywordAt(src, pos, "use") {
		return false
	}

	end := lineEnd(src, pos)
	parts := strings.Fields(strings.TrimSpace(src[pos:end]))
	return len(parts) == 2 && isImportLikePath(parts[1])
}

func isImportLikePath(path string) bool {
	if path == "" {
		return false
	}

	for i, part := range strings.Split(path, ".") {
		if part == "" {
			return false
		}

		if i == 0 {
			if !isIdentifier(part) {
				return false
			}
			continue
		}

		if !isIdentifier(part) {
			return false
		}
	}

	return true
}

func readIdent(src string) (string, int) {
	match := identRE.FindString(src)

	if match == "" {
		return "", 0
	}

	return match, len(match)
}

func keywordAt(src string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(src) {
		return false
	}

	if src[pos:pos+len(keyword)] != keyword {
		return false
	}

	end := pos + len(keyword)

	if end < len(src) {
		r := rune(src[end])

		if unicode.IsLetter(r) ||
			unicode.IsDigit(r) ||
			r == '_' {
			return false
		}
	}

	return true
}

func skipSpace(src string, pos int) int {
	for pos < len(src) {
		switch src[pos] {
		case ' ', '\t', '\r', '\n':
			pos++

		default:
			return pos
		}
	}

	return pos
}

// leadingDocComments returns contiguous comments immediately before a source
// declaration. The ordinary parser keeps comments inside raw Go declarations,
// but Go++ declarations need their comments in the source-level AST so tools
// such as `gpp doc` can render them without inspecting generated Go.
func leadingDocComments(src string, pos int) (string, int) {
	pos = skipSpace(src, pos)
	var parts []string
	for pos < len(src) {
		if strings.HasPrefix(src[pos:], "//") {
			end := lineEnd(src, pos)
			text := strings.TrimSpace(src[pos+2 : end])
			parts = append(parts, text)
			pos = skipSpace(src, end)
			continue
		}
		if strings.HasPrefix(src[pos:], "/*") {
			close := strings.Index(src[pos+2:], "*/")
			if close < 0 {
				return strings.TrimSpace(strings.Join(parts, "\n")), pos
			}
			text := src[pos+2 : pos+2+close]
			lines := strings.Split(text, "\n")
			for index := range lines {
				lines[index] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[index]), "*"))
			}
			parts = append(parts, strings.TrimSpace(strings.Join(lines, "\n")))
			pos = skipSpace(src, pos+2+close+2)
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), pos
}

func documentationTargetAt(src string, pos int) bool {
	for _, keyword := range []string{"package", "import", "func", "class", "enum", "extend", "annotation", "embed", "template"} {
		if keywordAt(src, pos, keyword) {
			return true
		}
	}
	return false
}

func skipHorizontal(src string, pos int) int {
	for pos < len(src) &&
		(src[pos] == ' ' || src[pos] == '\t') {
		pos++
	}

	return pos
}

func lineEnd(src string, pos int) int {
	for pos < len(src) && src[pos] != '\n' {
		pos++
	}

	if pos < len(src) {
		pos++
	}

	return pos
}

func sourceLine(src string, pos int) int {
	if pos < 0 {
		return 1
	}
	if pos > len(src) {
		pos = len(src)
	}
	return 1 + strings.Count(src[:pos], "\n")
}
