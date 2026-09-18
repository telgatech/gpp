package compiler

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed prelude.gpp
var preludeSource string

var (
	preludeOnce sync.Once
	preludeFile *File
	preludeErr  error
)

func loadPrelude() (*File, error) {
	preludeOnce.Do(func() {
		preludeFile, preludeErr = ParseFile("prelude.gpp", preludeSource)
		if preludeErr != nil {
			return
		}
		_, preludeErr = ResolveProgram(&Program{Files: []*File{preludeFile}})
	})
	return preludeFile, preludeErr
}

// LoadPrelude exposes the implicit extension declarations to source-level
// tooling such as `gpp doc`.
func LoadPrelude() (*File, error) {
	return loadPrelude()
}

func configurePrelude(context *constructorContext, emit bool) error {
	file, err := loadPrelude()
	if err != nil {
		return fmt.Errorf("load prelude: %w", err)
	}

	context.EmitPrelude = emit
	for _, declaration := range file.Decls {
		extension, ok := declaration.(*ExtendDecl)
		if !ok {
			continue
		}
		methods := extensionMethodsForDeclarations([]*ExtendDecl{extension}, "")
		for index := range methods {
			methods[index].Prelude = true
			methods[index].GoName = "GppPreludeExt_" + strings.TrimPrefix(methods[index].GoName, "GppExt_")
			context.Extensions = append(context.Extensions, methods[index])
			if emit {
				context.PreludeExtensions = append(context.PreludeExtensions, methods[index])
			}
		}
	}

	imports, err := goImports(file)
	if err != nil {
		return fmt.Errorf("parse prelude imports: %w", err)
	}
	for _, spec := range imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return err
		}
		context.PreludeImports = append(context.PreludeImports, importPath)
	}
	return nil
}

func emitPreludeExtensions(context constructorContext, body string) (string, error) {
	if len(context.PreludeExtensions) == 0 {
		return "", nil
	}
	var out strings.Builder
	for _, extension := range context.PreludeExtensions {
		if !context.EmitPreludeAll && !containsExtensionSelector(body, extension.Method.Name) && !strings.Contains(body, extension.GoName+"(") {
			continue
		}
		if err := emitExtensionMethod(&out, extension, context, ""); err != nil {
			return "", fmt.Errorf("prelude extension %s for %s: %w", extension.Method.Name, extension.Target, err)
		}
	}
	return out.String(), nil
}

func preludeUsedInFile(file *File) (bool, error) {
	prelude, err := loadPrelude()
	if err != nil {
		return false, err
	}
	names := map[string]bool{}
	for _, declaration := range prelude.Decls {
		extension, ok := declaration.(*ExtendDecl)
		if !ok {
			continue
		}
		for _, method := range extension.Methods {
			names[method.Name] = true
		}
	}
	for _, declaration := range file.Decls {
		var bodies []string
		switch value := declaration.(type) {
		case *RawDecl:
			bodies = append(bodies, value.Code)
		case *ClassDecl:
			for _, method := range value.Methods {
				bodies = append(bodies, method.Body)
			}
		case *ExtendDecl:
			for _, method := range value.Methods {
				bodies = append(bodies, method.Body)
			}
		}
		for _, body := range bodies {
			for name := range names {
				if containsExtensionSelector(body, name) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func containsExtensionSelector(body, name string) bool {
	for index := 0; index < len(body); index++ {
		if body[index] != '.' || index+1 >= len(body) {
			continue
		}
		cursor := index + 1
		if !strings.HasPrefix(body[cursor:], name) {
			continue
		}
		cursor += len(name)
		if cursor < len(body) && (isIdentPart(body[cursor]) || body[cursor] == '[') {
			continue
		}
		for cursor < len(body) && (body[cursor] == ' ' || body[cursor] == '\t' || body[cursor] == '\n' || body[cursor] == '\r') {
			cursor++
		}
		if cursor < len(body) && body[cursor] == '(' || cursor < len(body) && body[cursor] == '[' {
			return true
		}
	}
	return false
}

func preludeImportsForBody(body string, imports []string) []string {
	if body == "" {
		return nil
	}
	result := []string{}
	for _, importPath := range imports {
		name := path.Base(importPath)
		if strings.Contains(body, name+".") {
			result = append(result, importPath)
		}
	}
	return result
}

// rewritePreludeImportAliases keeps generated prelude helpers valid when the
// user or another generated feature already imports one of their dependencies
// under an alias. Go rejects importing the same path twice, so the helper body
// must use the existing alias instead of introducing a second import.
func rewritePreludeImportAliases(preludeBody, body string, imports []string) string {
	if preludeBody == "" || body == "" || len(imports) == 0 {
		return preludeBody
	}
	const prefix = "package main\n\n"
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "generated.go", prefix+body, parser.ImportsOnly)
	if err != nil {
		return preludeBody
	}
	allowed := map[string]bool{}
	for _, importPath := range imports {
		allowed[importPath] = true
	}
	aliases := map[string]string{}
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !allowed[importPath] {
			continue
		}
		defaultName := path.Base(importPath)
		alias := defaultName
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias != defaultName && alias != "_" && alias != "." {
			aliases[defaultName] = alias
		}
	}
	if len(aliases) == 0 {
		return preludeBody
	}

	preludeParsed, fileSet, prefixLength, err := parsePreludeBody(preludeBody)
	if err != nil {
		return preludeBody
	}
	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	ast.Inspect(preludeParsed, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		alias := aliases[identifier.Name]
		if alias == "" {
			return true
		}
		start := fileSet.Position(identifier.Pos()).Offset - prefixLength
		end := fileSet.Position(identifier.End()).Offset - prefixLength
		if start >= 0 && end <= len(preludeBody) {
			edits = append(edits, edit{start: start, end: end, text: alias})
		}
		return true
	})
	sort.Slice(edits, func(left, right int) bool { return edits[left].start > edits[right].start })
	for _, edit := range edits {
		preludeBody = preludeBody[:edit.start] + edit.text + preludeBody[edit.end:]
	}
	return preludeBody
}

func parsePreludeBody(body string) (ast.Node, *token.FileSet, int, error) {
	const prefix = "package main\n\n"
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "generated.go", prefix+body, 0)
	if err != nil {
		return nil, nil, 0, err
	}
	return parsed, fileSet, len(prefix), nil
}

func prependPreludeImports(body string, imports []string) string {
	if len(imports) == 0 {
		return body
	}
	existing := map[string]bool{}
	parsed, err := parser.ParseFile(token.NewFileSet(), "generated.go", "package main\n\n"+body, parser.ImportsOnly)
	if err == nil {
		for _, spec := range parsed.Imports {
			if path, err := strconv.Unquote(spec.Path.Value); err == nil {
				existing[path] = true
			}
		}
	}
	missing := []string{}
	for _, importPath := range imports {
		if existing[importPath] {
			continue
		}
		existing[importPath] = true
		missing = append(missing, importPath)
	}
	if len(missing) == 0 {
		return body
	}
	var out strings.Builder
	if len(missing) == 1 {
		fmt.Fprintf(&out, "import %q\n\n", missing[0])
	} else {
		out.WriteString("import (\n")
		for _, importPath := range missing {
			fmt.Fprintf(&out, "\t%q\n", importPath)
		}
		out.WriteString(")\n\n")
	}
	out.WriteString(body)
	return out.String()
}
