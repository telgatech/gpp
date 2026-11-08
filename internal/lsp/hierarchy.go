package lsp

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"

	"github.com/telgatech/gpp/compiler"
)

type CallHierarchyItem struct {
	Name           string         `json:"name"`
	Kind           int            `json:"kind"`
	Tags           []int          `json:"tags,omitempty"`
	Detail         string         `json:"detail,omitempty"`
	URI            string         `json:"uri"`
	Range          Range          `json:"range"`
	SelectionRange Range          `json:"selectionRange"`
	Data           map[string]any `json:"data,omitempty"`
}

type CallHierarchyCall struct {
	Item       CallHierarchyItem `json:"from"`
	FromRanges []Range           `json:"fromRanges"`
}

type CallHierarchyOutgoingCall struct {
	Item       CallHierarchyItem `json:"to"`
	FromRanges []Range           `json:"fromRanges"`
}

type TypeHierarchyItem struct {
	Name           string         `json:"name"`
	Kind           int            `json:"kind"`
	Tags           []int          `json:"tags,omitempty"`
	Detail         string         `json:"detail,omitempty"`
	URI            string         `json:"uri"`
	Range          Range          `json:"range"`
	SelectionRange Range          `json:"selectionRange"`
	Data           map[string]any `json:"data,omitempty"`
}

func (s *server) prepareTypeHierarchy(params textDocumentPositionParams) []TypeHierarchyItem {
	name, _ := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" {
		return nil
	}
	var result []TypeHierarchyItem
	for _, info := range s.resolveSymbolsAt(params.TextDocument.URI, params.Position, name) {
		if info.Symbol.Kind != compiler.DocClass && info.Symbol.Kind != compiler.DocEnum && info.Symbol.Kind != compiler.DocType {
			continue
		}
		if item, ok := s.typeHierarchyItem(info); ok {
			result = append(result, item)
		}
	}
	return result
}

func (s *server) typeSupertypes(item TypeHierarchyItem) []TypeHierarchyItem {
	info := s.symbolInfoForHierarchy(item.Data)
	if info.Symbol.Name == "" {
		return nil
	}
	var result []TypeHierarchyItem
	for _, parent := range s.hierarchyParents(info) {
		if s.cancelled() {
			return nil
		}
		if value, ok := s.typeHierarchyItem(parent); ok {
			result = append(result, value)
		}
	}
	return uniqueTypeHierarchyItems(result)
}

func (s *server) typeSubtypes(item TypeHierarchyItem) []TypeHierarchyItem {
	info := s.symbolInfoForHierarchy(item.Data)
	if info.Symbol.Name == "" {
		return nil
	}
	var result []TypeHierarchyItem
	for _, candidate := range s.workspace.symbols {
		if s.cancelled() {
			return nil
		}
		if candidate.Symbol.Kind != compiler.DocClass && candidate.Symbol.Kind != compiler.DocType {
			continue
		}
		for _, parent := range s.hierarchyParents(candidate) {
			if semanticSymbolKey(parent) == semanticSymbolKey(info) {
				if value, ok := s.typeHierarchyItem(candidate); ok {
					result = append(result, value)
				}
				break
			}
		}
	}
	return uniqueTypeHierarchyItems(result)
}

func (s *server) isGoInterface(info symbolInfo) bool {
	if info.Symbol.Kind != compiler.DocType {
		return false
	}
	_, ok := s.goInterfaceMethodSet(info)
	return ok
}

func (s *server) hierarchyParents(info symbolInfo) []symbolInfo {
	result := s.parentClasses(info)
	seen := map[string]bool{}
	for _, parent := range result {
		seen[semanticSymbolKey(parent)] = true
	}
	if s.isGoInterface(info) {
		spec, _, ok := s.goTypeSpecForSymbol(info)
		if !ok {
			return result
		}
		interfaceType, ok := spec.Type.(*ast.InterfaceType)
		if !ok {
			return result
		}
		for _, field := range interfaceType.Methods.List {
			if len(field.Names) != 0 {
				continue
			}
			parent, found := s.goEmbeddedInterfaceSymbol(info, field.Type)
			if !found || !s.isGoInterface(parent) {
				continue
			}
			key := semanticSymbolKey(parent)
			if !seen[key] {
				seen[key] = true
				result = append(result, parent)
			}
		}
		return result
	}
	if info.Symbol.Kind != compiler.DocClass && info.Symbol.Kind != compiler.DocType {
		return result
	}
	for _, candidate := range s.workspace.symbols {
		if !s.isGoInterface(candidate) || semanticSymbolKey(candidate) == semanticSymbolKey(info) {
			continue
		}
		methods, ok := s.goInterfaceMethodSet(candidate)
		if !ok || len(methods) == 0 || !s.goTypeImplements(info, candidate, methods) {
			continue
		}
		key := semanticSymbolKey(candidate)
		if !seen[key] {
			seen[key] = true
			result = append(result, candidate)
		}
	}
	return result
}

func (s *server) parentClasses(info symbolInfo) []symbolInfo {
	state := s.workspace.files[filepath.Clean(info.Path)]
	parents := append([]string(nil), info.Symbol.Parents...)
	var result []symbolInfo
	seen := map[string]bool{}
	for _, parent := range parents {
		parent = strings.TrimSpace(parent)
		for strings.HasPrefix(parent, "*") {
			parent = strings.TrimSpace(strings.TrimPrefix(parent, "*"))
		}
		if bracket := strings.IndexAny(parent, "[< "); bracket >= 0 {
			parent = parent[:bracket]
		}
		packageName, packageDir := info.Symbol.Package, ""
		if dot := strings.LastIndexByte(parent, '.'); dot >= 0 {
			qualifier := parent[:dot]
			parent = parent[dot+1:]
			if state != nil {
				if importedName, importedDir, found := s.importedPackageForQualifier(state, qualifier); found {
					packageName, packageDir = importedName, importedDir
				} else {
					packageName = qualifier
				}
			} else {
				packageName = qualifier
			}
		}
		var matches []symbolInfo
		for _, candidate := range s.workspace.symbols {
			if candidate.Symbol.Kind != compiler.DocClass || candidate.Symbol.Name != parent || candidate.Symbol.Package != packageName {
				continue
			}
			candidateDir := filepath.Clean(filepath.Dir(candidate.Path))
			if packageDir != "" && candidateDir != filepath.Clean(packageDir) {
				continue
			}
			if packageDir == "" && state != nil && candidateDir != filepath.Clean(filepath.Dir(info.Path)) && packageName == info.Symbol.Package {
				continue
			}
			matches = append(matches, candidate)
		}
		for _, match := range matches {
			key := semanticSymbolKey(match)
			if !seen[key] {
				seen[key] = true
				result = append(result, match)
			}
		}
	}
	return result
}

func uniqueTypeHierarchyItems(items []TypeHierarchyItem) []TypeHierarchyItem {
	seen := map[string]bool{}
	result := make([]TypeHierarchyItem, 0, len(items))
	for _, item := range items {
		key, _ := item.Data["symbolKey"].(string)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
	}
	return result
}

func (s *server) classIsSubtype(candidate, target symbolInfo, seen map[string]bool) bool {
	candidateKey, targetKey := semanticSymbolKey(candidate), semanticSymbolKey(target)
	if candidateKey == targetKey || seen[candidateKey] {
		return false
	}
	seen[candidateKey] = true
	for _, parent := range s.parentClasses(candidate) {
		if semanticSymbolKey(parent) == targetKey || s.classIsSubtype(parent, target, seen) {
			return true
		}
	}
	return false
}

func hierarchyName(name string) string {
	parts := strings.Split(name, ".")
	return parts[len(parts)-1]
}

func (s *server) typeHierarchyItem(info symbolInfo) (TypeHierarchyItem, bool) {
	location, ok := s.symbolLocation(info)
	if !ok {
		return TypeHierarchyItem{}, false
	}
	declarationRange, selectionRange := s.hierarchyRanges(info, location)
	kind := 5
	if info.Symbol.Kind == compiler.DocEnum {
		kind = 10
	} else if s.isGoInterface(info) {
		kind = 11
	} else if info.Symbol.Kind == compiler.DocType {
		if spec, _, ok := s.goTypeSpecForSymbol(info); ok {
			if _, isStruct := spec.Type.(*ast.StructType); isStruct {
				kind = 23
			}
		}
	}
	return TypeHierarchyItem{Name: info.Symbol.Name, Kind: kind, Detail: info.Symbol.Package, URI: location.URI, Range: declarationRange, SelectionRange: selectionRange, Data: map[string]any{"symbolKey": semanticSymbolKey(info)}}, true
}

func (s *server) hierarchyRanges(info symbolInfo, location Location) (Range, Range) {
	selectionRange := location.Range
	declarationRange := selectionRange
	state := s.workspace.files[filepath.Clean(info.Path)]
	if state != nil && state.File != nil {
		if span, ok := astSymbolDeclarationSpan(state.File, info.Symbol); ok {
			declarationRange = rangeForOffsets(state.Source, span.Start, span.End)
		}
	}
	return declarationRange, selectionRange
}

func (s *server) symbolInfoForHierarchy(data map[string]any) symbolInfo {
	key := data["symbolKey"]
	for _, info := range s.workspace.symbols {
		if semanticSymbolKey(info) == key {
			return info
		}
	}
	return symbolInfo{}
}

func (s *server) prepareCallHierarchy(params textDocumentPositionParams) []CallHierarchyItem {
	name, _ := s.symbolsAt(params.TextDocument.URI, params.Position)
	if name == "" {
		return nil
	}
	var result []CallHierarchyItem
	for _, info := range s.resolveSymbolsAt(params.TextDocument.URI, params.Position, name) {
		if item, ok := s.callHierarchyItem(info); ok {
			result = append(result, item)
		}
	}
	return result
}

func (s *server) incomingCalls(target CallHierarchyItem) []CallHierarchyCall {
	key := target.Data["symbolKey"]
	if key == nil {
		return nil
	}
	type group struct {
		item   CallHierarchyItem
		ranges []Range
	}
	groups := map[string]*group{}
	for _, state := range s.workspace.files {
		if s.cancelled() {
			return nil
		}
		if state.File == nil {
			continue
		}
		for index, token := range state.File.Tokens {
			if index%128 == 0 && s.cancelled() {
				return nil
			}
			if token.Kind != compiler.TokenIdentifier || token.Text != target.Name || !callArgumentsFollow(state.File.Tokens, index) {
				continue
			}
			position := positionAtOffset(state.Source, token.Span.Start)
			for _, candidate := range s.resolveSymbolsAt(state.URI, position, token.Text) {
				if semanticSymbolKey(candidate) != key {
					continue
				}
				caller, ok := s.callableAt(state, token.Span.Start)
				if !ok {
					continue
				}
				callerKey := semanticSymbolKey(caller)
				if groups[callerKey] == nil {
					item, valid := s.callHierarchyItem(caller)
					if !valid {
						continue
					}
					groups[callerKey] = &group{item: item}
				}
				groups[callerKey].ranges = append(groups[callerKey].ranges, rangeForOffsets(state.Source, token.Span.Start, token.Span.End))
				break
			}
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]CallHierarchyCall, 0, len(keys))
	for _, key := range keys {
		result = append(result, CallHierarchyCall{Item: groups[key].item, FromRanges: groups[key].ranges})
	}
	return result
}

func (s *server) outgoingCalls(source CallHierarchyItem) []CallHierarchyOutgoingCall {
	info, state, start, end, ok := s.callableByItem(source)
	if !ok {
		return nil
	}
	type group struct {
		item   CallHierarchyItem
		ranges []Range
	}
	groups := map[string]*group{}
	for index, token := range state.File.Tokens {
		if index%128 == 0 && s.cancelled() {
			return nil
		}
		if token.Span.Start < start || token.Span.End > end || token.Kind != compiler.TokenIdentifier || !callArgumentsFollow(state.File.Tokens, index) {
			continue
		}
		position := positionAtOffset(state.Source, token.Span.Start)
		for _, target := range s.resolveSymbolsAt(state.URI, position, token.Text) {
			if target.Symbol.Kind != compiler.DocFunction && target.Symbol.Kind != compiler.DocMethod && target.Symbol.Kind != compiler.DocExtension {
				continue
			}
			targetKey := semanticSymbolKey(target)
			if targetKey == semanticSymbolKey(info) {
				continue
			}
			if groups[targetKey] == nil {
				item, valid := s.callHierarchyItem(target)
				if !valid {
					continue
				}
				groups[targetKey] = &group{item: item}
			}
			groups[targetKey].ranges = append(groups[targetKey].ranges, rangeForOffsets(state.Source, token.Span.Start, token.Span.End))
			break
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]CallHierarchyOutgoingCall, 0, len(keys))
	for _, key := range keys {
		result = append(result, CallHierarchyOutgoingCall{Item: groups[key].item, FromRanges: groups[key].ranges})
	}
	return result
}

func callArgumentsFollow(tokens []compiler.Token, callee int) bool {
	next := callee + 1
	for next < len(tokens) && (tokens[next].Kind == compiler.TokenComment || tokens[next].Kind == compiler.TokenNewline) {
		next++
	}
	if next >= len(tokens) {
		return false
	}
	if tokens[next].Text == "(" {
		return true
	}
	if tokens[next].Text != "[" {
		return false
	}
	depth := 0
	for index := next; index < len(tokens); index++ {
		switch tokens[index].Text {
		case "[":
			depth++
		case "]":
			depth--
			if depth == 0 {
				index++
				for index < len(tokens) && (tokens[index].Kind == compiler.TokenComment || tokens[index].Kind == compiler.TokenNewline) {
					index++
				}
				return index < len(tokens) && tokens[index].Text == "("
			}
		}
	}
	return false
}

func (s *server) callHierarchyItem(info symbolInfo) (CallHierarchyItem, bool) {
	if info.Symbol.Kind != compiler.DocFunction && info.Symbol.Kind != compiler.DocMethod && info.Symbol.Kind != compiler.DocExtension {
		return CallHierarchyItem{}, false
	}
	location, ok := s.symbolLocation(info)
	if !ok {
		return CallHierarchyItem{}, false
	}
	declarationRange, selectionRange := s.hierarchyRanges(info, location)
	kind := 12
	if info.Symbol.Kind == compiler.DocMethod || info.Symbol.Kind == compiler.DocExtension {
		kind = 6
	}
	item := CallHierarchyItem{
		Name: info.Symbol.Name, Kind: kind, Detail: info.Symbol.Signature, URI: location.URI,
		Range: declarationRange, SelectionRange: selectionRange,
		Data: map[string]any{"symbolKey": semanticSymbolKey(info)},
	}
	if deprecated(info.Symbol) {
		item.Tags = []int{1}
	}
	return item, true
}

func (s *server) callableAt(state *fileState, offset int) (symbolInfo, bool) {
	if state == nil || state.File == nil {
		return symbolInfo{}, false
	}
	for _, declaration := range state.File.Decls {
		switch value := declaration.(type) {
		case *compiler.FunctionDecl:
			if offset >= value.Method.BodySpan.Start && offset <= value.Method.BodySpan.End {
				if info, ok := s.findSymbolInfo(state.Path, value.Name, ""); ok {
					return info, true
				}
			}
		case *compiler.MixedDecl:
			for _, function := range value.Functions {
				if offset >= function.Method.BodySpan.Start && offset <= function.Method.BodySpan.End {
					if info, ok := s.findSymbolInfo(state.Path, function.Name, ""); ok {
						return info, true
					}
				}
			}
			for _, declaration := range value.GoASTDecls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Body == nil {
					continue
				}
				bodyStart, bodyEnd, valid := goFunctionBodyOffsets(value.GoASTFileSet, function)
				if valid && offset >= bodyStart && offset <= bodyEnd {
					parent := lspGoReceiverTypeName(function.Recv)
					if info, found := s.findSymbolInfo(state.Path, function.Name.Name, parent); found {
						return info, true
					}
				}
			}
		case *compiler.ClassDecl:
			for _, method := range value.Methods {
				if offset >= method.BodySpan.Start && offset <= method.BodySpan.End {
					if info, ok := s.findSymbolInfo(state.Path, method.Name, value.Name); ok {
						return info, true
					}
				}
			}
		case *compiler.ExtendDecl:
			for _, method := range value.Methods {
				if offset < method.BodySpan.Start || offset > method.BodySpan.End {
					continue
				}
				for _, target := range strings.Split(extensionReceiverType(state.Source, value), "|") {
					if info, ok := s.findSymbolInfo(state.Path, method.Name, strings.TrimSpace(target)); ok {
						return info, true
					}
				}
			}
		case *compiler.GoDecl:
			for _, declaration := range value.Declarations {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Body == nil {
					continue
				}
				bodyStart, bodyEnd, valid := goFunctionBodyOffsets(value.FileSet, function)
				if valid && offset >= bodyStart && offset <= bodyEnd {
					parent := lspGoReceiverTypeName(function.Recv)
					if info, found := s.findSymbolInfo(state.Path, function.Name.Name, parent); found {
						return info, true
					}
				}
			}
		}
	}
	return symbolInfo{}, false
}

func (s *server) callableByItem(item CallHierarchyItem) (symbolInfo, *fileState, int, int, bool) {
	state := s.workspace.files[pathFromURI(item.URI)]
	if state == nil || state.File == nil || item.Data["symbolKey"] == nil {
		return symbolInfo{}, nil, 0, 0, false
	}
	var info symbolInfo
	for _, candidate := range s.workspace.symbols {
		if semanticSymbolKey(candidate) == item.Data["symbolKey"] {
			info = candidate
			break
		}
	}
	if info.Path == "" {
		return symbolInfo{}, nil, 0, 0, false
	}
	for _, declaration := range state.File.Decls {
		switch value := declaration.(type) {
		case *compiler.FunctionDecl:
			if value.Name == info.Symbol.Name {
				return info, state, value.Method.BodySpan.Start, value.Method.BodySpan.End, true
			}
		case *compiler.MixedDecl:
			for _, function := range value.Functions {
				if function.Name == info.Symbol.Name {
					return info, state, function.Method.BodySpan.Start, function.Method.BodySpan.End, true
				}
			}
			for _, declaration := range value.GoASTDecls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Body == nil || function.Name.Name != info.Symbol.Name || lspGoReceiverTypeName(function.Recv) != info.Symbol.Parent {
					continue
				}
				bodyStart, bodyEnd, valid := goFunctionBodyOffsets(value.GoASTFileSet, function)
				if valid {
					return info, state, bodyStart, bodyEnd, true
				}
			}
		case *compiler.ClassDecl:
			if value.Name != info.Symbol.Parent {
				continue
			}
			for _, method := range value.Methods {
				if method.Name == info.Symbol.Name {
					return info, state, method.BodySpan.Start, method.BodySpan.End, true
				}
			}
		case *compiler.ExtendDecl:
			for _, method := range value.Methods {
				if method.Name != info.Symbol.Name {
					continue
				}
				for _, target := range strings.Split(extensionReceiverType(state.Source, value), "|") {
					if strings.TrimSpace(target) == info.Symbol.Parent {
						return info, state, method.BodySpan.Start, method.BodySpan.End, true
					}
				}
			}
		case *compiler.GoDecl:
			for _, declaration := range value.Declarations {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Body == nil || function.Name.Name != info.Symbol.Name || lspGoReceiverTypeName(function.Recv) != info.Symbol.Parent {
					continue
				}
				bodyStart, bodyEnd, valid := goFunctionBodyOffsets(value.FileSet, function)
				if valid {
					return info, state, bodyStart, bodyEnd, true
				}
			}
		}
	}
	return symbolInfo{}, nil, 0, 0, false
}

func goFunctionBodyOffsets(fileSet *token.FileSet, function *ast.FuncDecl) (int, int, bool) {
	if fileSet == nil || function == nil || function.Body == nil {
		return 0, 0, false
	}
	start := fileSet.Position(function.Body.Pos())
	end := fileSet.Position(function.Body.End())
	if !start.IsValid() || !end.IsValid() || start.Offset < 0 || end.Offset < start.Offset {
		return 0, 0, false
	}
	return start.Offset, end.Offset, true
}

func (s *server) findSymbolInfo(path, name, parent string) (symbolInfo, bool) {
	for _, info := range s.workspace.symbols {
		if info.Path == path && info.Symbol.Name == name && info.Symbol.Parent == parent && (info.Symbol.Kind == compiler.DocFunction || info.Symbol.Kind == compiler.DocMethod || info.Symbol.Kind == compiler.DocExtension) {
			return info, true
		}
	}
	return symbolInfo{}, false
}
