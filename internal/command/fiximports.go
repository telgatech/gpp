package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/telgatech/gpp/compiler"
)

type importPackage struct {
	ImportPath string
	Name       string
	Symbols    map[string]bool
	GoPlus     bool
	Standard   bool
}

type goListPackage struct {
	ImportPath string
	Name       string
	Dir        string
	GoFiles    []string
	CgoFiles   []string
	Standard   bool
}

func runFixImports(args []string) int {
	check, stdout := false, false
	paths := []string{}
	for _, arg := range args {
		switch arg {
		case "--check":
			check = true
		case "--stdout":
			stdout = true
		case "-h", "--help":
			return commandHelp("fiximports")
		default:
			if strings.HasPrefix(arg, "-") {
				return reportError(fmt.Errorf("unknown fiximports option %q", arg))
			}
			paths = append(paths, arg)
		}
	}
	if check && stdout {
		return reportError(fmt.Errorf("--check and --stdout cannot be used together"))
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	sources, err := discoverSources(paths)
	if err != nil {
		return reportError(err)
	}
	if stdout && len(sources) != 1 {
		return reportError(fmt.Errorf("--stdout requires exactly one Go++ source file"))
	}
	packages, err := availableImportPackages()
	if err != nil {
		return reportError(err)
	}
	needsImports := []string{}
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			return reportError(err)
		}
		fixed, added, err := fixImports(source, data, packages)
		if err != nil {
			return reportError(err)
		}
		if stdout {
			_, _ = os.Stdout.Write(fixed)
			continue
		}
		if !added {
			continue
		}
		if check {
			needsImports = append(needsImports, source)
			continue
		}
		if err := atomicWriteFile(source, fixed); err != nil {
			return reportError(err)
		}
	}
	if len(needsImports) > 0 {
		fmt.Println("Import fixes needed:")
		for _, source := range needsImports {
			fmt.Printf("    %s\n", source)
		}
		return 1
	}
	return 0
}

func availableImportPackages() (map[string][]importPackage, error) {
	goPath, err := findGo()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(goPath, "list", "-e", "-json", "std", "all")
	if workingDir := moduleDirectoryForGoList(); workingDir != "" {
		cmd.Dir = workingDir
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list available Go packages: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	byName := map[string][]importPackage{}
	for {
		var listed goListPackage
		if err := decoder.Decode(&listed); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("read Go package list: %w", err)
		}
		if listed.Name == "" || listed.ImportPath == "" || listed.Dir == "" || strings.HasPrefix(listed.Name, "_") {
			continue
		}
		packageInfo := importPackage{ImportPath: listed.ImportPath, Name: listed.Name, Symbols: map[string]bool{}, Standard: listed.Standard}
		for _, filename := range append(append([]string(nil), listed.GoFiles...), listed.CgoFiles...) {
			if strings.HasSuffix(filename, "_test.go") {
				continue
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(listed.Dir, filename), nil, 0)
			if parseErr != nil {
				continue
			}
			for _, declaration := range file.Decls {
				switch declaration := declaration.(type) {
				case *ast.FuncDecl:
					if declaration.Recv == nil && declaration.Name.IsExported() {
						packageInfo.Symbols[declaration.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, spec := range declaration.Specs {
						switch spec := spec.(type) {
						case *ast.TypeSpec:
							if spec.Name.IsExported() {
								packageInfo.Symbols[spec.Name.Name] = true
							}
						case *ast.ValueSpec:
							for _, name := range spec.Names {
								if name.IsExported() {
									packageInfo.Symbols[name.Name] = true
								}
							}
						}
					}
				}
			}
		}
		byName[listed.Name] = append(byName[listed.Name], packageInfo)
	}
	for _, importPath := range compiler.OfficialStdlibPackages() {
		files, err := compiler.LoadOfficialPackage(importPath)
		if err != nil {
			continue
		}
		packageName := pathBase(importPath)
		packageInfo := importPackage{ImportPath: importPath, Name: packageName, Symbols: map[string]bool{}, GoPlus: true}
		for _, file := range files {
			for _, declaration := range file.Decls {
				switch declaration := declaration.(type) {
				case *compiler.ClassDecl:
					if ast.IsExported(declaration.Name) {
						packageInfo.Symbols[declaration.Name] = true
					}
				case *compiler.EnumDecl:
					if ast.IsExported(declaration.Name) {
						packageInfo.Symbols[declaration.Name] = true
					}
				case *compiler.FunctionDecl:
					if ast.IsExported(declaration.Name) {
						packageInfo.Symbols[declaration.Name] = true
					}
				case *compiler.GoDecl:
					addExportedASTSymbols(packageInfo.Symbols, declaration.Declarations)
				case *compiler.MixedDecl:
					addExportedASTSymbols(packageInfo.Symbols, declaration.GoASTDecls)
					for _, function := range declaration.Functions {
						if ast.IsExported(function.Name) {
							packageInfo.Symbols[function.Name] = true
						}
					}
				}
			}
		}
		byName[packageName] = append(byName[packageName], packageInfo)
	}
	return byName, nil
}

func addExportedASTSymbols(symbols map[string]bool, declarations []ast.Decl) {
	for _, declaration := range declarations {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Recv == nil && declaration.Name.IsExported() {
				symbols[declaration.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					if spec.Name.IsExported() {
						symbols[spec.Name.Name] = true
					}
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						if name.IsExported() {
							symbols[name.Name] = true
						}
					}
				}
			}
		}
	}
}

type sourceEdit struct {
	start int
	end   int
	text  string
}

type selectorUse struct {
	start       int
	end         int
	receiver    string
	packageInfo importPackage
}

type sourceImportRef struct {
	path       string
	alias      string
	aliasStart int
	aliasEnd   int
	pathStart  int
}

func fixImports(filename string, source []byte, packages map[string][]importPackage) ([]byte, bool, error) {
	file, err := compiler.ParseFile(filename, string(source))
	if err != nil {
		return nil, false, err
	}
	tokens, err := compiler.LexSource(filename, string(source))
	if err != nil {
		return nil, false, err
	}
	importRefs := sourceImportRefs(tokens)
	existing := map[string]sourceImportRef{}
	importedAliases := map[string]*importPackage{}
	for index, item := range file.Imports {
		if index >= len(importRefs) {
			break
		}
		ref := importRefs[index]
		path := normalizeImportPath(item.Path)
		ref.path = path
		if item.Alias != "" {
			ref.alias = item.Alias
		} else {
			ref.alias = importName(path, packages)
		}
		existing[path] = ref
		if info, ok := importPackageByPath(path, packages); ok {
			importedAliases[ref.alias] = &info
		} else {
			importedAliases[ref.alias] = nil
		}
	}
	declared := declaredNames(file)
	var uses []selectorUse
	for index := 0; index+2 < len(tokens); index++ {
		if tokens[index].Kind != compiler.TokenIdentifier || tokens[index+1].Text != "." || tokens[index+2].Kind != compiler.TokenIdentifier {
			continue
		}
		if index >= 2 && tokens[index-1].Text == "." && tokens[index-2].Kind == compiler.TokenIdentifier {
			continue
		}
		receiver, member := tokens[index].Text, tokens[index+2].Text
		if declared[receiver] || isKeywordLike(receiver) {
			continue
		}
		if _, alreadyImported := importedAliases[receiver]; alreadyImported && importedAliases[receiver] == nil {
			continue
		}
		info, ok := chooseImportPackage(receiver, member, packages, importedAliases)
		if !ok {
			continue
		}
		uses = append(uses, selectorUse{start: tokens[index].Span.Start, end: tokens[index].Span.End, receiver: receiver, packageInfo: info})
	}
	if len(uses) == 0 {
		return source, false, nil
	}

	usedPackages := map[string]importPackage{}
	goPlusNames, stdAliasNames := map[string]bool{}, map[string]bool{}
	for _, use := range uses {
		usedPackages[use.packageInfo.ImportPath] = use.packageInfo
		if use.packageInfo.GoPlus {
			goPlusNames[use.packageInfo.Name] = true
		}
		if use.packageInfo.Standard && use.receiver == "std"+use.packageInfo.Name {
			stdAliasNames[use.packageInfo.Name] = true
		}
	}
	for _, info := range importedAliases {
		if info != nil && info.GoPlus {
			goPlusNames[info.Name] = true
		}
	}
	aliases := map[string]string{}
	for importPath, info := range usedPackages {
		if ref, exists := existing[importPath]; exists && ref.alias != "" && ref.alias != info.Name {
			aliases[importPath] = ref.alias
			continue
		}
		naturalNameOccupied := false
		if imported, exists := importedAliases[info.Name]; exists {
			naturalNameOccupied = imported == nil || imported.ImportPath != importPath
		}
		switch {
		case info.Standard && (goPlusNames[info.Name] || stdAliasNames[info.Name] || naturalNameOccupied):
			aliases[importPath] = "std" + info.Name
		case info.GoPlus:
			aliases[importPath] = info.Name
		default:
			aliases[importPath] = info.Name
		}
	}

	var edits []sourceEdit
	missing := map[string]string{}
	for importPath, alias := range aliases {
		if ref, ok := existing[importPath]; ok {
			if ref.alias == alias {
				continue
			}
			if ref.aliasStart >= 0 {
				edits = append(edits, sourceEdit{start: ref.aliasStart, end: ref.aliasEnd, text: alias})
			} else {
				edits = append(edits, sourceEdit{start: ref.pathStart, end: ref.pathStart, text: alias + " "})
			}
		} else {
			missing[importPath] = alias
		}
	}
	for _, use := range uses {
		alias := aliases[use.packageInfo.ImportPath]
		if alias != use.receiver {
			edits = append(edits, sourceEdit{start: use.start, end: use.end, text: alias})
		}
	}
	if len(missing) > 0 {
		paths := make([]string, 0, len(missing))
		for importPath := range missing {
			paths = append(paths, importPath)
		}
		sort.Strings(paths)
		var block strings.Builder
		block.WriteString("import (\n")
		for _, importPath := range paths {
			block.WriteString("    ")
			if missing[importPath] != importName(importPath, packages) {
				block.WriteString(missing[importPath])
				block.WriteByte(' ')
			}
			block.WriteString(strconv.Quote(importPath))
			block.WriteByte('\n')
		}
		block.WriteString(")\n\n")
		insertAt := importInsertionPoint(source, tokens, file)
		edits = append(edits, sourceEdit{start: insertAt, end: insertAt, text: block.String()})
	}
	if len(edits) == 0 {
		return source, false, nil
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	updated := append([]byte(nil), source...)
	for _, edit := range edits {
		updated = append(updated[:edit.start], append([]byte(edit.text), updated[edit.end:]...)...)
	}
	return updated, true, nil
}

func chooseImportPackage(receiver, member string, packages map[string][]importPackage, imported map[string]*importPackage) (importPackage, bool) {
	if info, exists := imported[receiver]; exists && info != nil {
		// An explicitly chosen alias is authoritative. A default package name
		// remains eligible for Go++-first resolution if its name collides.
		if receiver != info.Name {
			return *info, info.Symbols[member]
		}
	}
	name := receiver
	standardOnly := false
	if strings.HasPrefix(receiver, "std") {
		name = strings.TrimPrefix(receiver, "std")
		standardOnly = true
	}
	var matches []importPackage
	for _, candidate := range packages[name] {
		if candidate.Symbols[member] && (!standardOnly || candidate.Standard) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return importPackage{}, false
	}
	priority := func(candidate importPackage) int {
		if candidate.GoPlus {
			return 0
		}
		if candidate.Standard {
			return 1
		}
		return 2
	}
	bestPriority := 3
	for _, candidate := range matches {
		if candidatePriority := priority(candidate); candidatePriority < bestPriority {
			bestPriority = candidatePriority
		}
	}
	best := map[string]importPackage{}
	for _, candidate := range matches {
		if priority(candidate) == bestPriority {
			best[candidate.ImportPath] = candidate
		}
	}
	if len(best) != 1 {
		return importPackage{}, false
	}
	for _, candidate := range best {
		return candidate, true
	}
	return importPackage{}, false
}

func sourceImportRefs(tokens []compiler.Token) []sourceImportRef {
	var refs []sourceImportRef
	for index := 0; index < len(tokens); index++ {
		if tokens[index].Kind != compiler.TokenKeyword || tokens[index].Text != "import" {
			continue
		}
		cursor := nextNonTrivia(tokens, index+1)
		grouped := cursor < len(tokens) && tokens[cursor].Text == "("
		if grouped {
			cursor++
		}
		for cursor < len(tokens) {
			cursor = nextNonTrivia(tokens, cursor)
			if cursor >= len(tokens) || (grouped && tokens[cursor].Text == ")") {
				break
			}
			ref := sourceImportRef{aliasStart: -1, aliasEnd: -1}
			start := cursor
			if tokens[cursor].Text == "." {
				next := nextNonTrivia(tokens, cursor+1)
				if next < len(tokens) && tokens[next].Kind == compiler.TokenString {
					ref.alias = "."
					ref.aliasStart, ref.aliasEnd = tokens[cursor].Span.Start, tokens[cursor].Span.End
					cursor = next
				}
			} else if tokens[cursor].Kind == compiler.TokenIdentifier {
				next := nextNonTrivia(tokens, cursor+1)
				if next < len(tokens) && tokens[next].Kind == compiler.TokenString {
					ref.alias = tokens[cursor].Text
					ref.aliasStart, ref.aliasEnd = tokens[cursor].Span.Start, tokens[cursor].Span.End
					cursor = next
				}
			}
			if cursor < len(tokens) && tokens[cursor].Kind == compiler.TokenString {
				if value, err := strconv.Unquote(tokens[cursor].Text); err == nil {
					ref.path = value
				}
				ref.pathStart = tokens[cursor].Span.Start
				cursor++
			} else {
				var path strings.Builder
				for cursor < len(tokens) {
					if tokens[cursor].Kind == compiler.TokenIdentifier {
						path.WriteString(tokens[cursor].Text)
						cursor++
						if cursor < len(tokens) && tokens[cursor].Text == "." {
							path.WriteByte('.')
							cursor++
							continue
						}
						break
					}
					break
				}
				ref.path = path.String()
				if cursor > start {
					ref.pathStart = tokens[start].Span.Start
				}
			}
			if ref.path != "" {
				refs = append(refs, ref)
			}
			if !grouped {
				break
			}
		}
		index = cursor
	}
	return refs
}

func importPackageByPath(importPath string, packages map[string][]importPackage) (importPackage, bool) {
	for _, candidates := range packages {
		for _, candidate := range candidates {
			if candidate.ImportPath == importPath {
				return candidate, true
			}
		}
	}
	return importPackage{}, false
}

func normalizeImportPath(importPath string) string {
	return strings.SplitN(importPath, "@", 2)[0]
}

func importInsertionPoint(source []byte, tokens []compiler.Token, file *compiler.File) int {
	lastImportEnd := -1
	for index := 0; index < len(tokens); index++ {
		if tokens[index].Text != "import" {
			continue
		}
		next := nextNonTrivia(tokens, index+1)
		if next >= len(tokens) {
			continue
		}
		end := tokens[next].Span.End
		if tokens[next].Text == "(" {
			depth := 1
			for cursor := next + 1; cursor < len(tokens); cursor++ {
				switch tokens[cursor].Text {
				case "(":
					depth++
				case ")":
					depth--
					if depth == 0 {
						end = tokens[cursor].Span.End
						cursor = len(tokens)
					}
				}
			}
		} else {
			end = tokens[next].Span.End
			for cursor := next + 1; cursor < len(tokens) && tokens[cursor].Span.Line == tokens[next].Span.Line; cursor++ {
				if tokens[cursor].Kind != compiler.TokenComment {
					end = tokens[cursor].Span.End
				}
			}
		}
		if end > lastImportEnd {
			lastImportEnd = end
		}
	}
	if lastImportEnd < 0 && file.PackageAST != nil {
		lastImportEnd = file.PackageAST.Span().End
	}
	if lastImportEnd < 0 {
		return 0
	}
	for lastImportEnd < len(source) && source[lastImportEnd] != '\n' {
		lastImportEnd++
	}
	if lastImportEnd < len(source) {
		lastImportEnd++
	}
	return lastImportEnd
}

func nextNonTrivia(tokens []compiler.Token, index int) int {
	for index < len(tokens) && (tokens[index].Kind == compiler.TokenNewline || tokens[index].Kind == compiler.TokenComment) {
		index++
	}
	return index
}

func declaredNames(file *compiler.File) map[string]bool {
	names := map[string]bool{}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *compiler.GoDecl:
			addGoDeclarationNames(names, declaration.Declarations)
		case *compiler.MixedDecl:
			addGoDeclarationNames(names, declaration.GoASTDecls)
			for _, function := range declaration.Functions {
				names[function.Name] = true
				for _, parameter := range function.Method.ParameterAST {
					names[parameter.Name] = true
				}
			}
		case *compiler.FunctionDecl:
			names[declaration.Name] = true
			for _, parameter := range declaration.Method.ParameterAST {
				names[parameter.Name] = true
			}
		case *compiler.ValueDecl:
			for _, name := range declaration.Names {
				names[name.Text] = true
			}
		case *compiler.ClassDecl:
			names[declaration.Name] = true
			for _, field := range declaration.Fields {
				names[field.Name] = true
			}
			for _, method := range declaration.Methods {
				names[method.Name] = true
			}
		}
	}
	for index := 0; index+1 < len(file.Tokens); index++ {
		if file.Tokens[index].Kind == compiler.TokenKeyword && (file.Tokens[index].Text == "let" || file.Tokens[index].Text == "var" || file.Tokens[index].Text == "const") {
			next := nextNonTrivia(file.Tokens, index+1)
			if next < len(file.Tokens) && file.Tokens[next].Kind == compiler.TokenIdentifier {
				names[file.Tokens[next].Text] = true
			}
		}
		if file.Tokens[index].Kind == compiler.TokenIdentifier && file.Tokens[index+1].Text == ":=" {
			names[file.Tokens[index].Text] = true
		}
	}
	return names
}

func addGoDeclarationNames(names map[string]bool, declarations []ast.Decl) {
	for _, declaration := range declarations {
		ast.Inspect(declaration, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.FuncDecl:
				names[node.Name.Name] = true
				addFieldNames(names, node.Type.Params)
				addFieldNames(names, node.Type.Results)
			case *ast.ValueSpec:
				for _, name := range node.Names {
					names[name.Name] = true
				}
			case *ast.TypeSpec:
				names[node.Name.Name] = true
			case *ast.AssignStmt:
				for _, expression := range node.Lhs {
					if id, ok := expression.(*ast.Ident); ok {
						names[id.Name] = true
					}
				}
			case *ast.RangeStmt:
				for _, expression := range []ast.Expr{node.Key, node.Value} {
					if id, ok := expression.(*ast.Ident); ok {
						names[id.Name] = true
					}
				}
			}
			return true
		})
	}
}

func addFieldNames(names map[string]bool, fields *ast.FieldList) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			names[name.Name] = true
		}
	}
}

func pathBase(importPath string) string {
	if index := strings.LastIndex(importPath, "/"); index >= 0 {
		return importPath[index+1:]
	}
	return importPath
}

func importName(importPath string, packages map[string][]importPackage) string {
	importPath = strings.SplitN(importPath, "@", 2)[0]
	for _, candidates := range packages {
		for _, candidate := range candidates {
			if candidate.ImportPath == importPath {
				return candidate.Name
			}
		}
	}
	return pathBase(importPath)
}

func moduleDirectoryForGoList() string {
	working, err := os.Getwd()
	if err != nil {
		return ""
	}
	directory := working
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return working
		}
		directory = parent
	}
}

func isKeywordLike(name string) bool {
	return name == "_" || name == "nil" || name == "true" || name == "false" || name == "iota"
}
