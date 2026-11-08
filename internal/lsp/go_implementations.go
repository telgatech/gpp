package lsp

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/telgatech/gpp/compiler"
)

func (s *server) goTypeSpecForSymbol(info symbolInfo) (*ast.TypeSpec, *token.FileSet, bool) {
	state := s.workspace.files[filepath.Clean(info.Path)]
	if state == nil || state.File == nil || info.Symbol.Kind != compiler.DocType {
		return nil, nil, false
	}
	for _, declaration := range state.File.Decls {
		var declarations []ast.Decl
		var fileSet *token.FileSet
		switch value := declaration.(type) {
		case *compiler.GoDecl:
			declarations, fileSet = value.Declarations, value.FileSet
		case *compiler.MixedDecl:
			declarations, fileSet = value.GoASTDecls, value.GoASTFileSet
		}
		for _, raw := range declarations {
			group, ok := raw.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, rawSpec := range group.Specs {
				spec, ok := rawSpec.(*ast.TypeSpec)
				if ok && spec.Name.Name == info.Symbol.Name {
					return spec, fileSet, true
				}
			}
		}
	}
	return nil, nil, false
}

func (s *server) goInterfaceMethodSet(info symbolInfo) (map[string]string, bool) {
	return s.goInterfaceMethodSetSeen(info, map[string]bool{})
}

func (s *server) goInterfaceMethodSetSeen(info symbolInfo, seen map[string]bool) (map[string]string, bool) {
	key := semanticSymbolKey(info)
	if seen[key] {
		return nil, false
	}
	seen[key] = true
	defer delete(seen, key)

	spec, fileSet, ok := s.goTypeSpecForSymbol(info)
	if !ok {
		return nil, false
	}
	interfaceType, ok := spec.Type.(*ast.InterfaceType)
	if !ok {
		return nil, false
	}
	methods := map[string]string{}
	for _, field := range interfaceType.Methods.List {
		functionType, isMethod := field.Type.(*ast.FuncType)
		if isMethod && len(field.Names) > 0 {
			signature, valid := canonicalGoFuncType(fileSet, functionType)
			if !valid || !mergeInterfaceMethod(methods, field.Names, signature) {
				return nil, false
			}
			continue
		}

		// An unnamed interface field embeds another interface. Resolve it in
		// the declaring package (or through the import alias for qualified
		// names), then include its complete method set recursively.
		embedded, found := s.goEmbeddedInterfaceSymbol(info, field.Type)
		if !found {
			return nil, false
		}
		embeddedMethods, valid := s.goInterfaceMethodSetSeen(embedded, seen)
		if !valid {
			return nil, false
		}
		for name, signature := range embeddedMethods {
			if previous, exists := methods[name]; exists && previous != signature {
				return nil, false
			}
			methods[name] = signature
		}
	}
	return methods, true
}

func mergeInterfaceMethod(methods map[string]string, names []*ast.Ident, signature string) bool {
	for _, name := range names {
		if previous, exists := methods[name.Name]; exists && previous != signature {
			return false
		}
		methods[name.Name] = signature
	}
	return true
}

func (s *server) goEmbeddedInterfaceSymbol(from symbolInfo, expression ast.Expr) (symbolInfo, bool) {
	var name, qualifier string
	switch value := expression.(type) {
	case *ast.Ident:
		name = value.Name
	case *ast.SelectorExpr:
		base, ok := value.X.(*ast.Ident)
		if !ok {
			return symbolInfo{}, false
		}
		qualifier, name = base.Name, value.Sel.Name
	case *ast.IndexExpr:
		return s.goEmbeddedInterfaceSymbol(from, value.X)
	case *ast.IndexListExpr:
		return s.goEmbeddedInterfaceSymbol(from, value.X)
	case *ast.ParenExpr:
		return s.goEmbeddedInterfaceSymbol(from, value.X)
	default:
		return symbolInfo{}, false
	}
	if name == "" {
		return symbolInfo{}, false
	}
	packageName, packageDir := from.Symbol.Package, filepath.Clean(filepath.Dir(from.Path))
	if qualifier != "" {
		state := s.workspace.files[filepath.Clean(from.Path)]
		var found bool
		packageName, packageDir, found = s.importedPackageForQualifier(state, qualifier)
		if !found {
			return symbolInfo{}, false
		}
	}
	var match symbolInfo
	for _, candidate := range s.packageSymbols(packageName, packageDir) {
		if candidate.Symbol.Kind != compiler.DocType || candidate.Symbol.Name != name {
			continue
		}
		if match.Symbol.Name != "" {
			return symbolInfo{}, false
		}
		match = candidate
	}
	return match, match.Symbol.Name != ""
}

func canonicalGoFuncType(fileSet *token.FileSet, function *ast.FuncType) (string, bool) {
	if fileSet == nil || function == nil {
		return "", false
	}
	copyFunction := *function
	copyFunction.Params = fieldsWithoutNames(function.Params)
	copyFunction.Results = fieldsWithoutNames(function.Results)
	var buffer bytes.Buffer
	if err := format.Node(&buffer, fileSet, &copyFunction); err != nil {
		return "", false
	}
	return strings.TrimSpace(buffer.String()), true
}

func fieldsWithoutNames(fields *ast.FieldList) *ast.FieldList {
	if fields == nil {
		return nil
	}
	copyFields := *fields
	copyFields.List = make([]*ast.Field, len(fields.List))
	for index, field := range fields.List {
		copyField := *field
		copyField.Names = nil
		copyFields.List[index] = &copyField
	}
	return &copyFields
}

func (s *server) goInterfaceOwner(method symbolInfo) (symbolInfo, bool) {
	for _, candidate := range s.workspace.symbols {
		if candidate.Symbol.Kind != compiler.DocType || candidate.Symbol.Package != method.Symbol.Package || candidate.Symbol.Name != method.Symbol.Parent || filepath.Clean(filepath.Dir(candidate.Path)) != filepath.Clean(filepath.Dir(method.Path)) {
			continue
		}
		if _, ok := s.goInterfaceMethodSet(candidate); ok {
			return candidate, true
		}
	}
	return symbolInfo{}, false
}

func (s *server) goInterfaceImplementations(target symbolInfo) []Location {
	interfaceMethods, ok := s.goInterfaceMethodSet(target)
	if !ok {
		return nil
	}
	var result []Location
	for _, candidate := range s.workspace.symbols {
		if candidate.Symbol.Kind != compiler.DocType && candidate.Symbol.Kind != compiler.DocClass || semanticSymbolKey(candidate) == semanticSymbolKey(target) {
			continue
		}
		if candidate.Symbol.Kind == compiler.DocType {
			if _, isInterface := s.goInterfaceMethodSet(candidate); isInterface {
				continue
			}
		}
		if !s.goTypeImplements(candidate, target, interfaceMethods) {
			continue
		}
		if location, ok := s.symbolLocation(candidate); ok {
			result = append(result, location)
		}
	}
	return uniqueLocations(result)
}

func (s *server) goInterfaceMethodImplementations(target, owner symbolInfo) []Location {
	interfaceMethods, ok := s.goInterfaceMethodSet(owner)
	if !ok {
		return nil
	}
	want, ok := interfaceMethods[target.Symbol.Name]
	if !ok {
		return nil
	}
	var result []Location
	for _, candidate := range s.workspace.symbols {
		if candidate.Symbol.Kind != compiler.DocType && candidate.Symbol.Kind != compiler.DocClass || semanticSymbolKey(candidate) == semanticSymbolKey(owner) {
			continue
		}
		if candidate.Symbol.Kind == compiler.DocType {
			if _, isInterface := s.goInterfaceMethodSet(candidate); isInterface {
				continue
			}
		}
		for _, method := range s.workspace.symbols {
			if method.Symbol.Kind != compiler.DocMethod || method.Symbol.Name != target.Symbol.Name || method.Symbol.Parent != candidate.Symbol.Name || method.Symbol.Package != candidate.Symbol.Package || filepath.Clean(filepath.Dir(method.Path)) != filepath.Clean(filepath.Dir(candidate.Path)) {
				continue
			}
			if signature, ok := s.goMethodFuncType(method); ok && signature == want {
				if location, ok := s.symbolLocation(method); ok {
					result = append(result, location)
				}
			}
		}
	}
	return uniqueLocations(result)
}

func (s *server) goTypeImplements(candidate, target symbolInfo, required map[string]string) bool {
	for name, signature := range required {
		if !isExportedGoIdentifier(name) && filepath.Clean(filepath.Dir(candidate.Path)) != filepath.Clean(filepath.Dir(target.Path)) {
			return false
		}
		found := false
		for _, method := range s.workspace.symbols {
			if method.Symbol.Kind != compiler.DocMethod || method.Symbol.Name != name || method.Symbol.Parent != candidate.Symbol.Name || method.Symbol.Package != candidate.Symbol.Package || filepath.Clean(filepath.Dir(method.Path)) != filepath.Clean(filepath.Dir(candidate.Path)) {
				continue
			}
			if actual, ok := s.goMethodFuncType(method); ok && actual == signature {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *server) goMethodFuncType(method symbolInfo) (string, bool) {
	state := s.workspace.files[filepath.Clean(method.Path)]
	if state == nil || state.File == nil || method.Symbol.Kind != compiler.DocMethod {
		return "", false
	}
	for _, declaration := range state.File.Decls {
		var declarations []ast.Decl
		var fileSet *token.FileSet
		switch value := declaration.(type) {
		case *compiler.GoDecl:
			declarations, fileSet = value.Declarations, value.FileSet
		case *compiler.MixedDecl:
			declarations, fileSet = value.GoASTDecls, value.GoASTFileSet
		}
		for _, raw := range declarations {
			function, ok := raw.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Name.Name != method.Symbol.Name || lspGoReceiverTypeName(function.Recv) != method.Symbol.Parent {
				continue
			}
			return canonicalGoFuncType(fileSet, function.Type)
		}
	}
	signature := method.Symbol.Signature
	functionStart := strings.Index(signature, "func ")
	if functionStart < 0 {
		return "", false
	}
	nameStart := strings.Index(signature[functionStart+len("func "):], method.Symbol.Name)
	if nameStart < 0 {
		return "", false
	}
	nameEnd := functionStart + len("func ") + nameStart + len(method.Symbol.Name)
	parameters := strings.IndexByte(signature[nameEnd:], '(')
	if parameters < 0 {
		return "", false
	}
	fileSet := token.NewFileSet()
	expression, err := parser.ParseExprFrom(fileSet, "", "func"+signature[nameEnd+parameters:], parser.AllErrors)
	if err != nil {
		return "", false
	}
	functionType, ok := expression.(*ast.FuncType)
	if !ok {
		return "", false
	}
	return canonicalGoFuncType(fileSet, functionType)
}

func isExportedGoIdentifier(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}
