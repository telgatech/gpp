package lsp

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	"go/doc"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/telgatech/gpp/compiler"
)

type nativeGoHoverCache struct {
	key              [32]byte
	files            map[string]*ast.File
	fset             *token.FileSet
	importedFileSets map[string]*token.FileSet
	info             *types.Info
	diagnostics      []nativeGoTypeDiagnostic
}

type nativeGoTypeDiagnostic struct {
	path    string
	offset  int
	message string
}

var nativeGoDocs = struct {
	sync.Mutex
	packages  map[string]*doc.Package
	attempted map[string]bool
}{packages: map[string]*doc.Package{}, attempted: map[string]bool{}}

// nativeGoExternalHover supplies useful hover details for imported Go symbols
// that are resolved by go/types but whose source is outside the workspace.
// Workspace imports continue through the normal symbol index so navigation and
// documentation use the editor's current unsaved source.
func (s *server) nativeGoExternalHover(state *fileState, name string, offset int, selection Range) *hover {
	if state == nil || state.File == nil || name == "" {
		return nil
	}
	var object types.Object
	if strings.EqualFold(filepath.Ext(state.Path), ".go") {
		object = s.nativeGoObjectAt(state, name, offset)
	} else {
		object = s.nativeGoImportedObjectAt(state, name, offset)
		if object == nil {
			object = s.nativeGoGPPObjectAt(state, name, offset)
		}
	}
	if object == nil || object.Pkg() == nil {
		return nil
	}
	if object.Pkg().Path() == state.File.Package {
		return nil
	}
	if cache, _ := s.nativeGoInfoForFile(state); cache != nil && cache.importedFileSets[object.Pkg().Path()] != nil {
		return nil
	}
	return nativeGoHoverForObject(object, selection)
}

func (s *server) nativeGoObjectAt(state *fileState, name string, offset int) types.Object {
	cache, file := s.nativeGoInfoForFile(state)
	if cache == nil || file == nil || cache.info == nil || name == "" {
		return nil
	}
	var object types.Object
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != name {
			return true
		}
		start := cache.fset.Position(selector.Sel.Pos()).Offset
		end := cache.fset.Position(selector.Sel.End()).Offset
		if offset < start || offset > end {
			return true
		}
		if selected := cache.info.Selections[selector]; selected != nil {
			object = selected.Obj()
		} else {
			object = cache.info.Uses[selector.Sel]
		}
		return object == nil
	})
	if object != nil {
		return object
	}
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok || identifier.Name != name {
			return true
		}
		start := cache.fset.Position(identifier.Pos()).Offset
		end := cache.fset.Position(identifier.End()).Offset
		if offset < start || offset > end {
			return true
		}
		object = cache.info.Uses[identifier]
		if object == nil {
			object = cache.info.Defs[identifier]
		}
		return false
	})
	return object
}

func (s *server) nativeGoImportedObjectAt(state *fileState, name string, offset int) types.Object {
	if state == nil || state.File == nil || name == "" {
		return nil
	}
	identifier, ok := identifierTokenAt(state.File.Tokens, offset)
	if !ok {
		return nil
	}
	index := tokenIndexAt(state.File.Tokens, identifier.Span.Start)
	if index < 2 || state.File.Tokens[index-1].Text != "." {
		return nil
	}
	selector := selectorChainBefore(state.Source[:identifier.Span.Start])
	parts := strings.Split(selector, ".")
	if len(parts) == 0 {
		return nil
	}
	var importPath string
	for _, imported := range state.File.Imports {
		if imported.LogicalPackage {
			continue
		}
		alias := imported.Alias
		if alias == "" {
			if packageName, _, found := s.workspacePackageForImport(state, imported.Path); found {
				alias = packageName
			} else {
				alias = packageLeaf(imported.Path)
			}
		}
		if alias == parts[0] {
			importPath = imported.Path
			break
		}
	}
	if importPath == "" {
		return nil
	}
	packageValue, err := importer.Default().Import(importPath)
	if err != nil || packageValue == nil {
		return nil
	}
	if len(parts) == 1 {
		return packageValue.Scope().Lookup(name)
	}
	object := packageValue.Scope().Lookup(parts[1])
	typeName, ok := object.(*types.TypeName)
	if !ok {
		return nil
	}
	receiver := typeName.Type()
	for _, part := range parts[2:] {
		selected, _, _ := types.LookupFieldOrMethod(receiver, true, packageValue, part)
		if selected == nil {
			return nil
		}
		typeName, ok = selected.(*types.TypeName)
		if !ok {
			return nil
		}
		receiver = typeName.Type()
	}
	selected, _, _ := types.LookupFieldOrMethod(receiver, true, packageValue, name)
	return selected
}

func (s *server) nativeGoImportedDefinition(state *fileState, name string, offset int) *Location {
	if location := s.nativeGoImportedSourceDefinition(state, name, offset); location != nil {
		return location
	}
	object := s.nativeGoImportedObjectAt(state, name, offset)
	if object == nil {
		object = s.nativeGoGPPObjectAt(state, name, offset)
	}
	return s.nativeGoDefinitionForObject(object)
}

func (s *server) nativeGoImportedSourceDefinition(state *fileState, name string, offset int) *Location {
	if state == nil || state.File == nil || name == "" {
		return nil
	}
	identifier, ok := identifierTokenAt(state.File.Tokens, offset)
	if !ok {
		return nil
	}
	index := tokenIndexAt(state.File.Tokens, identifier.Span.Start)
	if index < 2 || state.File.Tokens[index-1].Text != "." {
		return nil
	}
	qualifier := selectorReceiverBefore(state, state.File.Tokens[index-1].Span.Start)
	var importPath string
	for _, imported := range state.File.Imports {
		if imported.LogicalPackage {
			continue
		}
		alias := imported.Alias
		if alias == "" {
			alias = packageLeaf(imported.Path)
			if packageName, _, found := s.workspacePackageForImport(state, imported.Path); found {
				alias = packageName
			}
		}
		if alias == qualifier {
			importPath = imported.Path
			break
		}
	}
	if importPath == "" {
		return nil
	}
	packageDir := filepath.Join(runtime.GOROOT(), "src", filepath.FromSlash(importPath))
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(packageDir, entry.Name())
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if parseErr != nil || file == nil {
			continue
		}
		var sourcePosition token.Pos
		ast.Inspect(file, func(node ast.Node) bool {
			switch declaration := node.(type) {
			case *ast.TypeSpec:
				if declaration.Name.Name == name {
					sourcePosition = declaration.Name.Pos()
				}
			case *ast.FuncDecl:
				if declaration.Recv == nil && declaration.Name.Name == name {
					sourcePosition = declaration.Name.Pos()
				}
			case *ast.ValueSpec:
				for _, declared := range declaration.Names {
					if declared.Name == name {
						sourcePosition = declared.Pos()
						break
					}
				}
			}
			return !sourcePosition.IsValid()
		})
		if sourcePosition.IsValid() {
			source, readErr := os.ReadFile(path)
			if readErr == nil {
				start := fset.Position(sourcePosition).Offset
				return &Location{URI: uriForPath(path), Range: rangeForOffsets(string(source), start, start+len(name))}
			}
		}
	}
	return nil
}

func (s *server) nativeGoDefinitionForObject(object types.Object) *Location {
	if object == nil || object.Pkg() == nil {
		return nil
	}
	packageDir := filepath.Join(runtime.GOROOT(), "src", filepath.FromSlash(object.Pkg().Path()))
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(packageDir, entry.Name())
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.AllErrors)
		if parseErr != nil || file == nil {
			continue
		}
		if start, end, found := nativeGoObjectSourceSpan(file, fset, object); found {
			source, readErr := os.ReadFile(path)
			if readErr == nil {
				return &Location{URI: uriForPath(path), Range: rangeForOffsets(string(source), start, end)}
			}
		}
	}
	return nil
}

func (s *server) nativeGoGPPObjectAt(state *fileState, name string, offset int) types.Object {
	if state == nil || state.File == nil || name == "" {
		return nil
	}
	token, ok := identifierTokenAt(state.File.Tokens, offset)
	if !ok {
		return nil
	}
	memberIndex := tokenIndexAt(state.File.Tokens, token.Span.Start)
	if memberIndex < 2 || state.File.Tokens[memberIndex-1].Text != "." {
		return nil
	}
	dot := state.File.Tokens[memberIndex-1]
	receiver := selectorReceiverBefore(state, dot.Span.Start)
	if receiver == "" {
		return nil
	}
	binding, found := s.localBindingAt(state, dot.Span.Start, receiver)
	var receiverType types.Type
	if found && binding.Method != nil {
		_, initializer := bindingInitializer(binding.Method.BodyAST, binding.Declaration)
		receiverType = s.nativeGoTypeForGPPExpression(state, binding.Method, initializer, 0)
		if receiverType == nil {
			receiverType = s.nativeGoTypeFromGPPTypeName(state, binding.Type)
		}
	} else {
		receiverType = s.nativeGoTypeForGPPPackageValue(state, receiver, dot.Span.Start)
	}
	if receiverType == nil {
		return nil
	}
	selected, _, _ := types.LookupFieldOrMethod(receiverType, true, nil, name)
	return selected
}

func (s *server) nativeGoTypeForGPPPackageValue(state *fileState, name string, offset int) types.Type {
	if state == nil || state.File == nil || name == "" {
		return nil
	}
	method, _ := enclosingMethod(state, offset)
	for _, declaration := range state.File.Decls {
		value, ok := declaration.(*compiler.ValueDecl)
		if !ok {
			continue
		}
		for index, declaredName := range value.Names {
			if declaredName.Text != name {
				continue
			}
			if value.Type != nil {
				span := value.Type.Span()
				if span.Start >= 0 && span.End <= len(state.Source) && span.End > span.Start {
					if receiverType := s.nativeGoTypeFromGPPTypeName(state, state.Source[span.Start:span.End]); receiverType != nil {
						return receiverType
					}
				}
			}
			if method != nil && index < len(value.Values) {
				if receiverType := s.nativeGoTypeForGPPExpression(state, method, value.Values[index], 0); receiverType != nil {
					return receiverType
				}
			}
		}
	}
	return nil
}

func nativeGoObjectSourceSpan(file *ast.File, fset *token.FileSet, object types.Object) (int, int, bool) {
	if file == nil || fset == nil || object == nil {
		return 0, 0, false
	}
	var start, end token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		if start.IsValid() {
			return false
		}
		switch declaration := node.(type) {
		case *ast.FuncDecl:
			function, ok := object.(*types.Func)
			if !ok || declaration.Name.Name != object.Name() {
				return true
			}
			wantReceiver := function.Signature().Recv() != nil
			if (declaration.Recv != nil) != wantReceiver {
				return true
			}
			if wantReceiver && nativeGoReceiverName(declaration.Recv) != nativeGoObjectReceiverName(function) {
				return true
			}
			start, end = declaration.Name.Pos(), declaration.Name.End()
		case *ast.TypeSpec:
			if _, ok := object.(*types.TypeName); ok && declaration.Name.Name == object.Name() {
				start, end = declaration.Name.Pos(), declaration.Name.End()
			}
		case *ast.ValueSpec:
			switch object.(type) {
			case *types.Const, *types.Var:
				for _, name := range declaration.Names {
					if name.Name == object.Name() {
						start, end = name.Pos(), name.End()
						break
					}
				}
			}
		}
		return true
	})
	if !start.IsValid() || !end.IsValid() {
		return 0, 0, false
	}
	return fset.Position(start).Offset, fset.Position(end).Offset, true
}

func nativeGoReceiverName(receivers *ast.FieldList) string {
	if receivers == nil || len(receivers.List) == 0 {
		return ""
	}
	var name func(ast.Expr) string
	name = func(expression ast.Expr) string {
		switch value := expression.(type) {
		case *ast.Ident:
			return value.Name
		case *ast.StarExpr:
			return name(value.X)
		case *ast.IndexExpr:
			return name(value.X)
		case *ast.IndexListExpr:
			return name(value.X)
		case *ast.SelectorExpr:
			return value.Sel.Name
		default:
			return ""
		}
	}
	return name(receivers.List[0].Type)
}

func nativeGoObjectReceiverName(function *types.Func) string {
	if function == nil || function.Signature().Recv() == nil {
		return ""
	}
	typeValue := types.Unalias(function.Signature().Recv().Type())
	if pointer, ok := typeValue.(*types.Pointer); ok {
		typeValue = types.Unalias(pointer.Elem())
	}
	if named, ok := typeValue.(*types.Named); ok {
		return named.Obj().Name()
	}
	return ""
}

func nativeGoHoverForObject(object types.Object, selection Range) *hover {
	if object == nil || object.Pkg() == nil {
		return nil
	}
	kind := "symbol"
	switch value := object.(type) {
	case *types.Func:
		kind = "function"
		if signature, ok := value.Type().(*types.Signature); ok && signature.Recv() != nil {
			kind = "method"
		}
	case *types.TypeName:
		kind = "type"
	case *types.Const:
		kind = "constant"
	case *types.Var:
		kind = "variable"
		if value.IsField() {
			kind = "field"
		}
	case *types.PkgName:
		kind = "package"
	}
	signature := types.ObjectString(object, func(pkg *types.Package) string {
		return pkg.Name()
	})
	value := "**" + kind + "**\n\n```go\n" + signature + "\n```"
	if constant, ok := object.(*types.Const); ok {
		value += "\n\nValue: `" + constant.Val().ExactString() + "`"
	}
	if documentation := nativeGoExternalDocumentation(object); documentation != "" {
		value += "\n\n" + documentation
	}
	return &hover{Contents: markupContent{Kind: "markdown", Value: value}, Range: &selection}
}

func nativeGoExternalDocumentation(object types.Object) string {
	if object == nil || object.Pkg() == nil {
		return ""
	}
	packageDoc := nativeGoPackageDocumentation(object.Pkg().Path())
	if packageDoc == nil {
		return ""
	}
	name := object.Name()
	switch value := object.(type) {
	case *types.Func:
		if signature, ok := value.Type().(*types.Signature); ok && signature.Recv() != nil {
			receiver := types.TypeString(signature.Recv().Type(), func(pkg *types.Package) string {
				if pkg == object.Pkg() {
					return ""
				}
				return pkg.Name()
			})
			receiver = strings.TrimPrefix(receiver, "*")
			if named, ok := types.Unalias(signature.Recv().Type()).(*types.Pointer); ok {
				receiver = types.TypeString(named.Elem(), func(pkg *types.Package) string {
					if pkg == object.Pkg() {
						return ""
					}
					return pkg.Name()
				})
			}
			for _, documentedType := range packageDoc.Types {
				if documentedType.Name == receiver {
					for _, method := range documentedType.Methods {
						if method.Name == name {
							return strings.TrimSpace(method.Doc)
						}
					}
				}
			}
		}
		for _, function := range packageDoc.Funcs {
			if function.Name == name {
				return strings.TrimSpace(function.Doc)
			}
		}
	case *types.TypeName:
		for _, documentedType := range packageDoc.Types {
			if documentedType.Name == name {
				return strings.TrimSpace(documentedType.Doc)
			}
		}
	case *types.Const:
		for _, group := range packageDoc.Consts {
			for _, declaredName := range group.Names {
				if declaredName == name {
					return strings.TrimSpace(group.Doc)
				}
			}
		}
	case *types.Var:
		for _, group := range packageDoc.Vars {
			for _, declaredName := range group.Names {
				if declaredName == name {
					return strings.TrimSpace(group.Doc)
				}
			}
		}
	}
	return ""
}

func nativeGoPackageDocumentation(importPath string) *doc.Package {
	if importPath == "" || filepath.IsAbs(importPath) {
		return nil
	}
	nativeGoDocs.Lock()
	defer nativeGoDocs.Unlock()
	if nativeGoDocs.attempted[importPath] {
		return nativeGoDocs.packages[importPath]
	}
	nativeGoDocs.attempted[importPath] = true
	root := filepath.Join(runtime.GOROOT(), "src")
	directory := filepath.Join(root, filepath.FromSlash(importPath))
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil
	}
	packages, err := parser.ParseDir(token.NewFileSet(), directory, func(entry os.FileInfo) bool {
		return !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go")
	}, parser.ParseComments|parser.AllErrors)
	if err != nil || len(packages) == 0 {
		return nil
	}
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)
	nativeGoDocs.packages[importPath] = doc.New(packages[names[0]], importPath, doc.AllDecls)
	return nativeGoDocs.packages[importPath]
}

func nativeGoTypeError(fileSet *token.FileSet, err error) (nativeGoTypeDiagnostic, bool) {
	var typeError types.Error
	if fileSet == nil || !errors.As(err, &typeError) {
		return nativeGoTypeDiagnostic{}, false
	}
	position := fileSet.Position(typeError.Pos)
	if !position.IsValid() || position.Offset < 0 || position.Filename == "" {
		return nativeGoTypeDiagnostic{}, false
	}
	return nativeGoTypeDiagnostic{path: filepath.Clean(position.Filename), offset: position.Offset, message: typeError.Msg}, true
}

func (s *server) nativeGoDiagnostics() map[string][]Diagnostic {
	result := map[string][]Diagnostic{}
	if s.workspace == nil {
		return result
	}
	workspaceSources := make([]nativeGoSource, 0, len(s.workspace.files))
	for path, state := range s.workspace.files {
		if state != nil && state.File != nil && strings.EqualFold(filepath.Ext(path), ".go") {
			workspaceSources = append(workspaceSources, nativeGoSource{path: path, source: state.Source})
		}
	}
	sort.Slice(workspaceSources, func(i, j int) bool { return workspaceSources[i].path < workspaceSources[j].path })
	key := nativeGoSourcesKey(workspaceSources)
	s.nativeGoDiagnosticsMu.Lock()
	defer s.nativeGoDiagnosticsMu.Unlock()
	if s.nativeGoDiagnosticsCached && s.nativeGoDiagnosticsKey == key {
		return s.nativeGoDiagnosticsByURI
	}
	paths := make([]string, 0, len(s.workspace.files))
	for path, state := range s.workspace.files {
		if state != nil && state.File != nil && strings.EqualFold(filepath.Ext(path), ".go") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	seenPackages := map[string]bool{}
	seenDiagnostics := map[string]bool{}
	for _, path := range paths {
		state := s.workspace.files[path]
		packageKey := filepath.Clean(filepath.Dir(path)) + "\x00" + state.File.Package
		if seenPackages[packageKey] {
			continue
		}
		seenPackages[packageKey] = true
		cache, _ := s.nativeGoInfoForFile(state)
		if cache == nil {
			continue
		}
		for _, typeDiagnostic := range cache.diagnostics {
			diagnosticState := s.workspace.files[filepath.Clean(typeDiagnostic.path)]
			if diagnosticState == nil || diagnosticState.File == nil {
				continue
			}
			start := typeDiagnostic.offset
			if start < 0 || start > len(diagnosticState.Source) {
				continue
			}
			end := start
			if token, ok := sourceTokenAt(diagnosticState.File.Tokens, start); ok {
				start, end = token.Span.Start, token.Span.End
			}
			key := fmt.Sprintf("%s:%d:%s", diagnosticState.Path, start, typeDiagnostic.message)
			if seenDiagnostics[key] {
				continue
			}
			seenDiagnostics[key] = true
			result[diagnosticState.URI] = append(result[diagnosticState.URI], Diagnostic{
				Range:    rangeForOffsets(diagnosticState.Source, start, end),
				Severity: 1,
				Source:   "go/types",
				Code:     "GPP3000",
				Message:  typeDiagnostic.message,
			})
		}
	}
	for uri := range result {
		sort.SliceStable(result[uri], func(i, j int) bool {
			left, right := result[uri][i].Range, result[uri][j].Range
			if left.Start.Line != right.Start.Line {
				return left.Start.Line < right.Start.Line
			}
			if left.Start.Character != right.Start.Character {
				return left.Start.Character < right.Start.Character
			}
			return result[uri][i].Message < result[uri][j].Message
		})
	}
	s.nativeGoDiagnosticsKey = key
	s.nativeGoDiagnosticsByURI = result
	s.nativeGoDiagnosticsCached = true
	return result
}

// nativeGoLocalHover uses Go's own type checker for local declarations in
// ordinary .go files. The compiler documentation index covers package
// symbols, while this supplies precise types for receivers, parameters,
// locals, and local constants.
func (s *server) nativeGoLocalHover(state *fileState, name string, offset int) *hover {
	if name == "" {
		return nil
	}
	cache, file := s.nativeGoInfoForFile(state)
	if cache == nil || file == nil {
		return nil
	}
	var enclosing *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		start := cache.fset.Position(function.Pos()).Offset
		end := cache.fset.Position(function.End()).Offset
		if offset >= start && offset <= end {
			enclosing = function
			break
		}
	}
	if enclosing == nil {
		return nil
	}
	var object types.Object
	ast.Inspect(enclosing, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || ident.Name != name {
			return true
		}
		start := cache.fset.Position(ident.Pos()).Offset
		end := cache.fset.Position(ident.End()).Offset
		if offset < start || offset > end {
			return true
		}
		object = cache.info.Uses[ident]
		if object == nil {
			object = cache.info.Defs[ident]
		}
		return false
	})
	if object == nil {
		return nil
	}
	typeName := types.TypeString(object.Type(), func(pkg *types.Package) string {
		if pkg.Name() == file.Name.Name {
			return ""
		}
		return pkg.Name()
	})
	switch value := object.(type) {
	case *types.Var:
		if value.IsField() {
			return nil
		}
		position := cache.fset.Position(value.Pos()).Offset
		functionStart := cache.fset.Position(enclosing.Pos()).Offset
		functionEnd := cache.fset.Position(enclosing.End()).Offset
		if position < functionStart || position > functionEnd {
			return nil
		}
		kind := "variable"
		if nativeGoFieldHasName(enclosing.Recv, name, cache.fset, position) {
			kind = "receiver"
		} else if nativeGoFieldHasName(enclosing.Type.Params, name, cache.fset, position) {
			kind = "parameter"
		}
		return localHoverValue(kind, name, typeName, "")
	case *types.Const:
		position := cache.fset.Position(value.Pos()).Offset
		if position < cache.fset.Position(enclosing.Pos()).Offset || position > cache.fset.Position(enclosing.End()).Offset {
			return nil
		}
		return localHoverValue("constant", name, typeName, value.Val().ExactString())
	default:
		return nil
	}
}

func (s *server) nativeGoInfoForFile(state *fileState) (*nativeGoHoverCache, *ast.File) {
	if state == nil || state.File == nil || !strings.EqualFold(filepath.Ext(state.Path), ".go") {
		return nil, nil
	}
	sources := s.nativeGoPackageSources(state)
	if len(sources) == 0 {
		return nil, nil
	}
	key := nativeGoSourcesKey(s.nativeGoTypeCheckSources(state))
	state.goHoverMu.Lock()
	defer state.goHoverMu.Unlock()
	cache := state.goHover
	if cache == nil || cache.key != key {
		fset := token.NewFileSet()
		files := make([]*ast.File, 0, len(sources))
		byPath := make(map[string]*ast.File, len(sources))
		for _, source := range sources {
			file, _ := parser.ParseFile(fset, source.path, source.source, parser.ParseComments|parser.AllErrors)
			if file == nil {
				continue
			}
			files = append(files, file)
			byPath[filepath.Clean(source.path)] = file
		}
		if len(files) == 0 {
			state.goHover = nil
			return nil, nil
		}
		info := &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
		}
		workspaceImporter := newNativeGoWorkspaceImporter(s.workspace)
		var diagnostics []nativeGoTypeDiagnostic
		config := types.Config{Importer: workspaceImporter, Error: func(err error) {
			if diagnostic, ok := nativeGoTypeError(fset, err); ok {
				diagnostics = append(diagnostics, diagnostic)
			}
		}}
		_, _ = config.Check(state.File.Package, fset, files, info)
		diagnostics = append(diagnostics, workspaceImporter.diagnostics...)
		cache = &nativeGoHoverCache{key: key, files: byPath, fset: fset, importedFileSets: workspaceImporter.fileSets, info: info, diagnostics: diagnostics}
		state.goHover = cache
	}
	cache = state.goHover
	return cache, cache.files[filepath.Clean(state.Path)]
}

func (s *server) nativeGoTypeCheckSources(state *fileState) []nativeGoSource {
	if s.workspace == nil || state == nil || state.File == nil {
		return nil
	}
	byImportPath := map[string]string{}
	ambiguous := map[string]bool{}
	for _, candidate := range s.workspace.files {
		if candidate == nil || candidate.File == nil || !strings.EqualFold(filepath.Ext(candidate.Path), ".go") {
			continue
		}
		if importPath, ok := moduleImportPath(candidate.Path); ok {
			if previous := byImportPath[importPath]; previous != "" && filepath.Clean(previous) != filepath.Clean(filepath.Dir(candidate.Path)) {
				ambiguous[importPath] = true
			} else {
				byImportPath[importPath] = filepath.Dir(candidate.Path)
			}
		}
	}
	var queue []string
	if importPath, ok := moduleImportPath(state.Path); ok {
		queue = append(queue, importPath)
	}
	seenDirs := map[string]bool{filepath.Clean(filepath.Dir(state.Path)): true}
	var result []nativeGoSource
	addDirectory := func(directory string, packageName string) []*fileState {
		var files []*fileState
		for _, candidate := range s.workspace.files {
			if candidate == nil || candidate.File == nil || !strings.EqualFold(filepath.Ext(candidate.Path), ".go") || filepath.Clean(filepath.Dir(candidate.Path)) != filepath.Clean(directory) || candidate.File.Package != packageName {
				continue
			}
			files = append(files, candidate)
			result = append(result, nativeGoSource{path: candidate.Path, source: candidate.Source})
		}
		return files
	}
	files := addDirectory(filepath.Dir(state.Path), state.File.Package)
	for _, file := range files {
		for _, declaration := range file.File.Imports {
			if !declaration.LogicalPackage {
				queue = append(queue, declaration.Path)
			}
		}
	}
	for len(queue) > 0 {
		importPath := queue[0]
		queue = queue[1:]
		if ambiguous[importPath] {
			continue
		}
		directory := byImportPath[importPath]
		if directory == "" || seenDirs[filepath.Clean(directory)] {
			continue
		}
		seenDirs[filepath.Clean(directory)] = true
		var packageName string
		for _, candidate := range s.workspace.files {
			if candidate != nil && candidate.File != nil && filepath.Clean(filepath.Dir(candidate.Path)) == filepath.Clean(directory) && strings.EqualFold(filepath.Ext(candidate.Path), ".go") {
				packageName = candidate.File.Package
				break
			}
		}
		for _, file := range addDirectory(directory, packageName) {
			for _, declaration := range file.File.Imports {
				if !declaration.LogicalPackage {
					queue = append(queue, declaration.Path)
				}
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].path < result[j].path })
	return result
}

type nativeGoWorkspaceImporter struct {
	workspace   *workspaceState
	fallback    types.Importer
	packages    map[string]*types.Package
	fileSets    map[string]*token.FileSet
	loading     map[string]bool
	diagnostics []nativeGoTypeDiagnostic
}

func newNativeGoWorkspaceImporter(workspace *workspaceState) *nativeGoWorkspaceImporter {
	return &nativeGoWorkspaceImporter{
		workspace: workspace,
		fallback:  importer.Default(),
		packages:  map[string]*types.Package{},
		fileSets:  map[string]*token.FileSet{},
		loading:   map[string]bool{},
	}
}

func (i *nativeGoWorkspaceImporter) Import(importPath string) (*types.Package, error) {
	return i.ImportFrom(importPath, "", 0)
}

func (i *nativeGoWorkspaceImporter) ImportFrom(importPath, directory string, mode types.ImportMode) (*types.Package, error) {
	if pkg := i.packages[importPath]; pkg != nil {
		return pkg, nil
	}
	if i.loading[importPath] {
		return nil, fmt.Errorf("import cycle involving %s", importPath)
	}
	sources := i.sourcesForImport(importPath)
	if len(sources) == 0 {
		if importerFrom, ok := i.fallback.(types.ImporterFrom); ok {
			return importerFrom.ImportFrom(importPath, directory, mode)
		}
		return i.fallback.Import(importPath)
	}
	i.loading[importPath] = true
	defer delete(i.loading, importPath)
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(sources))
	for _, source := range sources {
		file, _ := parser.ParseFile(fset, source.path, source.source, parser.ParseComments|parser.AllErrors)
		if file != nil {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no parseable Go files for %s", importPath)
	}
	var diagnostics []nativeGoTypeDiagnostic
	config := types.Config{Importer: i, Error: func(err error) {
		if diagnostic, ok := nativeGoTypeError(fset, err); ok {
			diagnostics = append(diagnostics, diagnostic)
		}
	}}
	pkg, _ := config.Check(importPath, fset, files, nil)
	i.diagnostics = append(i.diagnostics, diagnostics...)
	if pkg == nil {
		return nil, fmt.Errorf("could not type-check workspace package %s", importPath)
	}
	i.packages[importPath] = pkg
	i.fileSets[importPath] = fset
	return pkg, nil
}

func nativeGoFileSetForPackage(cache *nativeGoHoverCache, pkg *types.Package) *token.FileSet {
	if cache == nil {
		return nil
	}
	if pkg == nil {
		return cache.fset
	}
	if fset := cache.importedFileSets[pkg.Path()]; fset != nil {
		return fset
	}
	return cache.fset
}

func (i *nativeGoWorkspaceImporter) sourcesForImport(importPath string) []nativeGoSource {
	if i.workspace == nil {
		return nil
	}
	var directory, packageName string
	for _, candidate := range i.workspace.files {
		if candidate == nil || candidate.File == nil || !strings.EqualFold(filepath.Ext(candidate.Path), ".go") {
			continue
		}
		candidatePath, ok := moduleImportPath(candidate.Path)
		if !ok || candidatePath != importPath {
			continue
		}
		candidateDir := filepath.Clean(filepath.Dir(candidate.Path))
		if directory != "" && directory != candidateDir {
			return nil
		}
		directory, packageName = candidateDir, candidate.File.Package
	}
	if directory == "" {
		return nil
	}
	var result []nativeGoSource
	for _, candidate := range i.workspace.files {
		if candidate == nil || candidate.File == nil || !strings.EqualFold(filepath.Ext(candidate.Path), ".go") || filepath.Clean(filepath.Dir(candidate.Path)) != directory || candidate.File.Package != packageName {
			continue
		}
		result = append(result, nativeGoSource{path: candidate.Path, source: candidate.Source})
	}
	sort.Slice(result, func(a, b int) bool { return result[a].path < result[b].path })
	return result
}

func (s *server) nativeGoSelectionAt(state *fileState, name string, offset int) (types.Object, *token.FileSet, bool) {
	if name == "" {
		return nil, nil, false
	}
	cache, file := s.nativeGoInfoForFile(state)
	if cache == nil || file == nil || cache.info == nil {
		return nil, nil, false
	}
	var object types.Object
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != name {
			return true
		}
		start := cache.fset.Position(selector.Sel.Pos()).Offset
		end := cache.fset.Position(selector.Sel.End()).Offset
		if offset < start || offset > end {
			return true
		}
		if selection := cache.info.Selections[selector]; selection != nil {
			object = selection.Obj()
		}
		return false
	})
	return object, nativeGoFileSetForPackage(cache, object.Pkg()), object != nil
}

func (s *server) nativeGoMembersForExpression(state *fileState, receiver string, dotOffset int) []symbolInfo {
	if receiver == "" || dotOffset <= 0 {
		return nil
	}
	cache, file := s.nativeGoInfoForFile(state)
	if cache == nil || file == nil || cache.info == nil {
		return nil
	}
	receiverEnd := dotOffset - 1
	receiverStart := receiverEnd - len(receiver)
	if receiverStart < 0 || receiverEnd > len(state.Source) || state.Source[receiverStart:receiverEnd] != receiver {
		return nil
	}
	var receiverType types.Type
	ast.Inspect(file, func(node ast.Node) bool {
		expression, ok := node.(ast.Expr)
		if !ok {
			return true
		}
		start := cache.fset.Position(expression.Pos()).Offset
		end := cache.fset.Position(expression.End()).Offset
		if start != receiverStart || end != receiverEnd {
			return true
		}
		if typed, found := cache.info.Types[expression]; found {
			receiverType = typed.Type
		}
		return true
	})
	for receiverType != nil {
		receiverType = types.Unalias(receiverType)
		pointer, ok := receiverType.(*types.Pointer)
		if !ok {
			break
		}
		receiverType = pointer.Elem()
	}
	named, ok := receiverType.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return nil
	}
	position := nativeGoFileSetForPackage(cache, named.Obj().Pkg()).Position(named.Obj().Pos())
	packageName, packageDir := named.Obj().Pkg().Name(), ""
	if position.IsValid() && position.Filename != "" {
		packageDir = filepath.Clean(filepath.Dir(position.Filename))
		if !filepath.IsAbs(packageDir) {
			if absolute, err := filepath.Abs(packageDir); err == nil {
				packageDir = absolute
			}
		}
	}
	if packageDir == "" {
		var found bool
		packageName, packageDir, found = s.workspacePackageForImport(state, named.Obj().Pkg().Path())
		if !found {
			return nil
		}
	}
	var result []symbolInfo
	for _, info := range s.packageSymbols(packageName, packageDir) {
		if info.Symbol.Parent == named.Obj().Name() && (info.Symbol.Kind == compiler.DocField || info.Symbol.Kind == compiler.DocMethod || info.Symbol.Kind == compiler.DocExtension || info.Symbol.Kind == compiler.DocValue) {
			result = append(result, info)
		}
	}
	return result
}

func (s *server) nativeGoMembersForGPPExpression(state *fileState, receiver string, dotOffset int) []symbolInfo {
	if state == nil || state.File == nil || receiver == "" || dotOffset <= 0 {
		return nil
	}
	binding, found := s.localBindingAt(state, dotOffset-1, receiver)
	var typeOfReceiver types.Type
	if found && binding.Method != nil {
		_, initializer := bindingInitializer(binding.Method.BodyAST, binding.Declaration)
		typeOfReceiver = s.nativeGoTypeForGPPExpression(state, binding.Method, initializer, 0)
		if typeOfReceiver == nil {
			typeOfReceiver = s.nativeGoTypeFromGPPTypeName(state, binding.Type)
		}
	} else {
		typeOfReceiver = s.nativeGoTypeForGPPPackageValue(state, receiver, dotOffset)
	}
	if typeOfReceiver == nil {
		return nil
	}
	if pointer, ok := types.Unalias(typeOfReceiver).(*types.Pointer); ok {
		typeOfReceiver = pointer.Elem()
	}
	named, ok := types.Unalias(typeOfReceiver).(*types.Named)
	if !ok || named.Obj() == nil {
		return nil
	}
	packageName := ""
	if named.Obj().Pkg() != nil {
		packageName = named.Obj().Pkg().Name()
	}
	var result []symbolInfo
	seen := map[string]bool{}
	for _, methodSetType := range []types.Type{types.NewPointer(named), named} {
		methodSet := types.NewMethodSet(methodSetType)
		for index := 0; index < methodSet.Len(); index++ {
			method, ok := methodSet.At(index).Obj().(*types.Func)
			if !ok || !method.Exported() || seen[method.Name()] {
				continue
			}
			seen[method.Name()] = true
			symbol := compiler.DocSymbol{
				Kind:      compiler.DocMethod,
				Name:      method.Name(),
				FullName:  packageName + "." + named.Obj().Name() + "." + method.Name(),
				Package:   packageName,
				Parent:    named.Obj().Name(),
				Signature: nativeGoCompletionSignature(method.Name(), method.Type()),
				Doc:       nativeGoExternalDocumentation(method),
				Exported:  true,
			}
			result = append(result, symbolInfo{Symbol: symbol})
		}
	}
	if structure, ok := named.Underlying().(*types.Struct); ok {
		for index := 0; index < structure.NumFields(); index++ {
			field := structure.Field(index)
			if !field.Exported() || seen[field.Name()] {
				continue
			}
			seen[field.Name()] = true
			symbol := compiler.DocSymbol{
				Kind:      compiler.DocField,
				Name:      field.Name(),
				FullName:  packageName + "." + named.Obj().Name() + "." + field.Name(),
				Package:   packageName,
				Parent:    named.Obj().Name(),
				Signature: types.TypeString(field.Type(), nil),
				Exported:  true,
			}
			result = append(result, symbolInfo{Symbol: symbol})
		}
	}
	return result
}

func (s *server) nativeGoTypeForGPPExpression(state *fileState, method *compiler.Method, expression compiler.ExprNode, depth int) types.Type {
	if expression == nil || depth > 64 {
		return nil
	}
	switch value := expression.(type) {
	case *compiler.CallExpr:
		selector, ok := value.Callee.(*compiler.SelectorExpr)
		if !ok {
			return nil
		}
		memberOffset := selector.SpanValue.End - len(selector.Name)
		function, ok := s.nativeGoImportedObjectAt(state, selector.Name, memberOffset).(*types.Func)
		if !ok {
			receiverType := s.nativeGoTypeForGPPExpression(state, method, selector.Receiver, depth+1)
			if receiverType == nil {
				return nil
			}
			selected, _, _ := types.LookupFieldOrMethod(receiverType, true, nil, selector.Name)
			function, ok = selected.(*types.Func)
			if !ok {
				return nil
			}
		}
		signature, ok := function.Type().(*types.Signature)
		if !ok || signature.Results() == nil || signature.Results().Len() == 0 {
			return nil
		}
		return signature.Results().At(0).Type()
	case *compiler.NameExpr:
		binding, found := s.localBindingAt(state, value.SpanValue.Start, value.Name)
		if !found || binding.Method == nil {
			return nil
		}
		_, initializer := bindingInitializer(binding.Method.BodyAST, binding.Declaration)
		if result := s.nativeGoTypeForGPPExpression(state, binding.Method, initializer, depth+1); result != nil {
			return result
		}
		return s.nativeGoTypeFromGPPTypeName(state, binding.Type)
	case *compiler.SelectorExpr:
		receiverType := s.nativeGoTypeForGPPExpression(state, method, value.Receiver, depth+1)
		if receiverType == nil {
			return nil
		}
		selected, _, _ := types.LookupFieldOrMethod(receiverType, true, nil, value.Name)
		if selected == nil {
			return nil
		}
		if field, ok := selected.(*types.Var); ok {
			return field.Type()
		}
		if function, ok := selected.(*types.Func); ok {
			if signature, ok := function.Type().(*types.Signature); ok && signature.Results() != nil && signature.Results().Len() > 0 {
				return signature.Results().At(0).Type()
			}
		}
	case *compiler.ParenthesizedExpr:
		return s.nativeGoTypeForGPPExpression(state, method, value.Inner, depth+1)
	}
	return nil
}

func (s *server) nativeGoTypeFromGPPTypeName(state *fileState, typeName string) types.Type {
	typeName = strings.TrimSpace(typeName)
	pointer := strings.HasPrefix(typeName, "*")
	typeName = strings.TrimPrefix(typeName, "*")
	separator := strings.LastIndexByte(typeName, '.')
	if separator <= 0 || separator == len(typeName)-1 {
		return nil
	}
	qualifier, name := typeName[:separator], typeName[separator+1:]
	var importPath string
	for _, imported := range state.File.Imports {
		if imported.LogicalPackage {
			continue
		}
		alias := imported.Alias
		if alias == "" {
			alias = packageLeaf(imported.Path)
			if packageName, _, found := s.workspacePackageForImport(state, imported.Path); found {
				alias = packageName
			}
		}
		if alias == qualifier {
			importPath = imported.Path
			break
		}
	}
	if importPath == "" {
		return nil
	}
	packageValue, err := importer.Default().Import(importPath)
	if err != nil || packageValue == nil {
		return nil
	}
	typeObject, ok := packageValue.Scope().Lookup(name).(*types.TypeName)
	if !ok {
		return nil
	}
	result := typeObject.Type()
	if pointer {
		result = types.NewPointer(result)
	}
	return result
}

func nativeGoCompletionSignature(name string, value types.Type) string {
	signature, ok := value.(*types.Signature)
	if !ok {
		return "func " + name + "()"
	}
	qualifier := func(pkg *types.Package) string {
		if pkg == nil {
			return ""
		}
		return pkg.Name()
	}
	var parameters []string
	for index := 0; index < signature.Params().Len(); index++ {
		parameter := signature.Params().At(index)
		parameterName := parameter.Name()
		if parameterName == "" || parameterName == "_" {
			parameterName = "arg" + strconv.Itoa(index+1)
		}
		parameterType := parameter.Type()
		if signature.Variadic() && index == signature.Params().Len()-1 {
			if slice, ok := parameterType.(*types.Slice); ok {
				parameters = append(parameters, parameterName+" ..."+types.TypeString(slice.Elem(), qualifier))
				continue
			}
		}
		parameters = append(parameters, parameterName+" "+types.TypeString(parameterType, qualifier))
	}
	result := "func " + name + "(" + strings.Join(parameters, ", ") + ")"
	if signature.Results().Len() == 1 {
		result += " " + types.TypeString(signature.Results().At(0).Type(), qualifier)
	} else if signature.Results().Len() > 1 {
		results := make([]string, signature.Results().Len())
		for index := range results {
			results[index] = types.TypeString(signature.Results().At(index).Type(), qualifier)
		}
		result += " (" + strings.Join(results, ", ") + ")"
	}
	return result
}

type nativeGoSource struct {
	path   string
	source string
}

func (s *server) nativeGoPackageSources(state *fileState) []nativeGoSource {
	if s.workspace == nil || state == nil {
		return nil
	}
	var result []nativeGoSource
	for _, candidate := range s.workspace.files {
		if candidate == nil || !strings.EqualFold(filepath.Ext(candidate.Path), ".go") || filepath.Clean(filepath.Dir(candidate.Path)) != filepath.Clean(filepath.Dir(state.Path)) {
			continue
		}
		if candidate != state && candidate.File != nil && candidate.File.Package != state.File.Package {
			continue
		}
		result = append(result, nativeGoSource{path: candidate.Path, source: candidate.Source})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].path < result[j].path })
	return result
}

func nativeGoSourcesKey(sources []nativeGoSource) [32]byte {
	var source strings.Builder
	for _, file := range sources {
		fmt.Fprintf(&source, "%s\x00%s\x00", file.path, file.source)
	}
	return sha256.Sum256([]byte(source.String()))
}

func nativeGoFieldHasName(fields *ast.FieldList, name string, fset *token.FileSet, position int) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		for _, ident := range field.Names {
			if ident.Name == name && fset.Position(ident.Pos()).Offset == position {
				return true
			}
		}
	}
	return false
}
