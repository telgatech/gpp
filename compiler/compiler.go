package compiler

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

type CompileOptions struct {
	ModulePath  string
	ProjectRoot string
	Development bool
	NoPrelude   bool
	NoStdlib    bool
	CleanOutput bool
}

func CompileFiles(files []string, outputDir string) error {
	return CompileFilesWithOptions(files, outputDir, CompileOptions{})
}

func availableImportsForFile(file *File, model *SemanticModel, modulePath string) map[string]string {
	imports := map[string]string{}
	pkg := model.Packages[file.Package]
	if pkg == nil {
		return imports
	}
	for alias, importPath := range pkg.Imports {
		imports[alias] = importPath
		if logical, ok := logicalPackageForImport(importPath, modulePath, model); ok {
			if imported := model.Packages[logical]; imported != nil {
				for importedAlias, importedPath := range imported.Imports {
					if _, exists := imports[importedAlias]; !exists {
						imports[importedAlias] = importedPath
					}
				}
			}
		}
	}
	return imports
}

func CompileFilesWithOptions(files []string, outputDir string, options CompileOptions) error {
	if options.CleanOutput {
		if err := cleanGeneratedOutput(outputDir); err != nil {
			return err
		}
	}

	program := &Program{}

	for _, filename := range files {
		data, err := os.ReadFile(filename)

		if err != nil {
			return err
		}

		file, err := ParseFile(
			filepath.Base(filename),
			string(data),
		)

		if err != nil {
			return err
		}
		file.SourcePath, err = filepath.Abs(filename)
		if err != nil {
			return err
		}

		program.Files = append(program.Files, file)
	}
	if !options.NoStdlib {
		if err := appendOfficialStdlib(program); err != nil {
			return err
		}
	}

	model, err := ResolveProgram(program)
	if err != nil {
		return err
	}
	addDefaultObjectMethods(model)
	model, err = ResolveProgram(program)
	if err != nil {
		return err
	}
	if err := expandSerializableClasses(program, model, options.ModulePath); err != nil {
		return err
	}
	model, err = ResolveProgram(program)
	if err != nil {
		return err
	}
	atExit, err := validateAtExit(program)
	if err != nil {
		return err
	}
	for _, file := range program.Files {
		scope, err := annotationScopeForFile(file, model, options.ModulePath)
		if err != nil {
			return fmt.Errorf("%s: %w", file.Name, err)
		}
		if err := validateFileAnnotations(file, model.Packages[file.Package], scope, false); err != nil {
			return err
		}
	}
	functionSignatures, err := functionSignaturesForProgram(program)
	if err != nil {
		return err
	}
	outputNames := generatedOutputNames(program.Files)

	if options.ModulePath != "" {
		if err := ensureGoModule(outputDir, options.ModulePath); err != nil {
			return err
		}
		if programUsesModule(model, "golang.org/x/net/websocket") {
			if err := ensureGoModuleRequirement(outputDir, "golang.org/x/net", "v0.58.0"); err != nil {
				return err
			}
		}
		if err := ensureGeneratedMarker(outputDir); err != nil {
			return err
		}
	}
	recordContexts := map[string]*recordContext{}
	introspectionContexts := map[string]*introspectionContext{}
	exceptionContexts := map[string]*exceptionContext{}
	preludeEmitted := map[string]bool{}
	preludeExtensionsNeeded := map[string]map[string]bool{}
	if !options.NoPrelude {
		for _, file := range program.Files {
			used, err := preludeExtensionsUsedInFile(file)
			if err != nil {
				return err
			}
			if preludeExtensionsNeeded[file.Package] == nil {
				preludeExtensionsNeeded[file.Package] = map[string]bool{}
			}
			for name := range used {
				preludeExtensionsNeeded[file.Package][name] = true
			}
		}
	}
	usesIntrospection := programUsesIntrospection(program)
	if usesIntrospection && options.ModulePath != "" {
		if err := ensureSharedIntrospectionRuntime(outputDir); err != nil {
			return err
		}
	}

	for _, file := range program.Files {
		context, err := constructorContextForFile(
			file,
			model,
			options.ModulePath,
		)
		if err != nil {
			return fmt.Errorf("%s: %w", file.Name, err)
		}
		if recordContexts[file.Package] == nil {
			recordContexts[file.Package] = newRecordContext()
		}
		context.Records = recordContexts[file.Package]
		if introspectionContexts[file.Package] == nil {
			introspectionContexts[file.Package] = newIntrospectionContext(model.Packages[file.Package].Classes)
		}
		context.Introspection = introspectionContexts[file.Package]
		context.Introspection.Enabled = usesIntrospection
		if exceptionContexts[file.Package] == nil {
			exceptionContexts[file.Package] = &exceptionContext{}
		}
		context.Exceptions = exceptionContexts[file.Package]
		context.Annotations = model.Packages[file.Package].Annotations
		context.Package = file.Package
		context.AtExit = file.Package == "main" && atExit
		context.ModulePath = options.ModulePath
		context.Development = options.Development
		context.AvailableImports = availableImportsForFile(file, model, options.ModulePath)
		context.Templates = model.Packages[file.Package].Templates
		if usesIntrospection && options.ModulePath != "" {
			context.IntrospectionRuntimeImport = options.ModulePath + "/gpp/runtime"
		}
		if !options.NoPrelude {
			context.EmitPrelude = !preludeEmitted[file.Package]
			context.PreludeExtensionsNeeded = preludeExtensionsNeeded[file.Package]
			preludeEmitted[file.Package] = true
			if err := configurePrelude(&context, context.EmitPrelude); err != nil {
				return fmt.Errorf("%s: %w", file.Name, err)
			}
		}
		configureNativeExtensionMethods(&context, file)
		context.FunctionSignatures = functionSignatures[file.Package]
		context.Enums = model.Packages[file.Package].Enums
		configureImportedEnums(&context, file, model, options.ModulePath)
		configureEnumSignatures(&context)
		if err := configureFunctionOverloads(&context.Overloads, context.FunctionSignatures); err != nil {
			return fmt.Errorf("%s: %w", file.Name, err)
		}

		dir := outputDir

		if file.Package != "main" {
			for _, segment := range strings.Split(file.Package, ".") {
				dir = filepath.Join(dir, segment)
			}
		}
		embeds, err := prepareEmbeds(file, dir, options.ProjectRoot)
		if err != nil {
			return fmt.Errorf("%s: %w", file.Name, err)
		}
		context.Embeds = embeds

		code, err := emitFile(
			file,
			context,
		)

		if err != nil {
			if isSourceLineDiagnostic(err) {
				return fmt.Errorf("%s:%w", file.Name, err)
			}
			return fmt.Errorf(
				"%s: %w",
				file.Name,
				err,
			)
		}

		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}

		name := outputNames[file]

		if err := os.WriteFile(
			filepath.Join(dir, name),
			code,
			0644,
		); err != nil {
			return err
		}
	}

	return nil
}

// generatedOutputNames keeps source and external-template files from
// overwriting each other when they share a basename, for example
// classifieds.gpp and classifieds.gpp.tpl.
func generatedOutputNames(files []*File) map[*File]string {
	result := map[*File]string{}
	used := map[string]map[string]bool{}

	assign := func(file *File) {
		if used[file.Package] == nil {
			used[file.Package] = map[string]bool{}
		}
		base := file.Name
		template := strings.HasSuffix(base, ".gpp.tpl")
		if template {
			base = strings.TrimSuffix(base, ".gpp.tpl")
		} else {
			base = strings.TrimSuffix(base, filepath.Ext(base))
		}

		name := base + ".go"
		if used[file.Package][name] && template {
			name = base + "_tpl.go"
		}
		if used[file.Package][name] {
			for suffix := 2; ; suffix++ {
				candidate := fmt.Sprintf("%s_%d.go", base, suffix)
				if !used[file.Package][candidate] {
					name = candidate
					break
				}
			}
		}
		used[file.Package][name] = true
		result[file] = name
	}

	// Prefer the conventional basename for Go++ source files. This makes a
	// same-basename external template consistently become <base>_tpl.go even
	// if file discovery happens to return the template first.
	for _, file := range files {
		if !strings.HasSuffix(file.Name, ".gpp.tpl") {
			assign(file)
		}
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name, ".gpp.tpl") {
			assign(file)
		}
	}
	return result
}

type compiledEmbed struct {
	Name      string
	Path      string
	Directory bool
}

func prepareEmbeds(file *File, packageDir, configuredRoot string) ([]compiledEmbed, error) {
	if file.Official {
		return prepareOfficialEmbeds(file, packageDir)
	}
	var declarations []compiledEmbed
	for _, declaration := range file.Decls {
		embed, ok := declaration.(*EmbedDecl)
		if !ok {
			continue
		}
		root := configuredRoot
		if root == "" {
			root = embedProjectRoot(file.SourcePath)
		}
		if root == "" {
			return nil, fmt.Errorf("embed project root could not be determined")
		}
		root, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		for _, entry := range embed.Entries {
			cleanPath, err := normalizeEmbedPath(entry.Path)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", entry.Path, err)
			}
			source := filepath.Join(root, filepath.FromSlash(cleanPath))
			relative, err := filepath.Rel(root, source)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("embed path escapes project root: %s", entry.Path)
			}
			info, err := os.Lstat(source)
			if err != nil {
				if os.IsNotExist(err) {
					if entry.Directory {
						return nil, fmt.Errorf("embed directory not found: %s", entry.Path)
					}
					return nil, fmt.Errorf("embed file not found: %s", entry.Path)
				}
				return nil, err
			}
			if entry.Directory {
				if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
					return nil, fmt.Errorf("embed path expected a directory: %s", entry.Path)
				}
				if err := validateEmbedDirectory(source, entry.Path); err != nil {
					return nil, err
				}
			} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				if info.IsDir() {
					return nil, fmt.Errorf("embed path expected a file: %s", entry.Path)
				}
				return nil, fmt.Errorf("embed file is not a regular file: %s", entry.Path)
			}

			destination := filepath.Join(packageDir, filepath.FromSlash(cleanPath))
			if entry.Directory {
				if err := copyEmbedDirectory(source, destination); err != nil {
					return nil, err
				}
			} else if err := copyEmbedFile(source, destination, info.Mode().Perm()); err != nil {
				return nil, err
			}
			declarations = append(declarations, compiledEmbed{
				Name:      entry.Name,
				Path:      cleanPath,
				Directory: entry.Directory,
			})
		}
	}
	return declarations, nil
}

func prepareOfficialEmbeds(file *File, packageDir string) ([]compiledEmbed, error) {
	var declarations []compiledEmbed
	for _, entry := range embedsInFile(file) {
		cleanPath, err := normalizeEmbedPath(entry.Path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Path, err)
		}
		packagePath := file.OfficialPackage
		if packagePath == "" {
			packagePath = file.Package
		}
		source := path.Join("stdlib", packagePath, cleanPath)
		info, err := fs.Stat(officialStdlib, source)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				if entry.Directory {
					return nil, fmt.Errorf("embed directory not found: %s", entry.Path)
				}
				return nil, fmt.Errorf("embed file not found: %s", entry.Path)
			}
			return nil, err
		}
		if entry.Directory {
			if !info.IsDir() {
				return nil, fmt.Errorf("embed path expected a directory: %s", entry.Path)
			}
			if err := copyOfficialEmbedDirectory(source, filepath.Join(packageDir, filepath.FromSlash(cleanPath))); err != nil {
				return nil, err
			}
		} else {
			if info.IsDir() {
				return nil, fmt.Errorf("embed path expected a file: %s", entry.Path)
			}
			if err := copyOfficialEmbedFile(source, filepath.Join(packageDir, filepath.FromSlash(cleanPath))); err != nil {
				return nil, err
			}
		}
		declarations = append(declarations, compiledEmbed{Name: entry.Name, Path: cleanPath, Directory: entry.Directory})
	}
	return declarations, nil
}

func embedsInFile(file *File) []EmbedEntry {
	var entries []EmbedEntry
	for _, declaration := range file.Decls {
		if embed, ok := declaration.(*EmbedDecl); ok {
			entries = append(entries, embed.Entries...)
		}
	}
	return entries
}

func copyOfficialEmbedDirectory(source, destination string) error {
	return fs.WalkDir(officialStdlib, source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := destination
		if relative != "." {
			target = filepath.Join(destination, relative)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyOfficialEmbedFile(current, target)
	})
}

func copyOfficialEmbedFile(source, destination string) error {
	data, err := fs.ReadFile(officialStdlib, source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0644)
}

func normalizeEmbedPath(value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "*?[]\\") {
		return "", fmt.Errorf("embed path must be a non-empty literal path without wildcards: %s", value)
	}
	if strings.HasPrefix(value, "/") || filepath.IsAbs(filepath.FromSlash(value)) {
		return "", fmt.Errorf("embed path must stay within the project root: %s", value)
	}
	clean := path.Clean(strings.TrimSuffix(value, "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("embed path must stay within the project root: %s", value)
	}
	return clean, nil
}

func embedProjectRoot(source string) string {
	if source == "" {
		return ""
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return filepath.Dir(source)
	}
	directory := filepath.Dir(absolute)
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return filepath.Dir(absolute)
		}
		directory = parent
	}
}

func validateEmbedDirectory(source, declaredPath string) error {
	files := 0
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("embed path contains unsupported symlink: %s", declaredPath)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("embed path contains non-regular file: %s", declaredPath)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		visible := true
		for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
			if strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") {
				visible = false
				break
			}
		}
		if visible {
			files++
		}
		return nil
	})
	if err != nil {
		return err
	}
	if files == 0 {
		return fmt.Errorf("embed directory contains no embeddable files: %s", declaredPath)
	}
	return nil
}

func copyEmbedDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := destination
		if relative != "." {
			target = filepath.Join(destination, relative)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("embed path contains unsupported symlink: %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("embed path contains non-regular file: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyEmbedFile(path, target, info.Mode().Perm())
	})
}

func copyEmbedFile(source, destination string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, mode)
}

// cleanGeneratedOutput removes files from the compiler-owned output workspace
// while preserving Go module resolution state. Build and run commands use it
// so stale generated source cannot be compiled alongside the current project.
func cleanGeneratedOutput(outputDir string) error {
	clean := filepath.Clean(outputDir)
	if clean == "." || clean == string(filepath.Separator) {
		return fmt.Errorf("refusing to clean unsafe generated output directory %q", outputDir)
	}

	entries, err := os.ReadDir(outputDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.Name() == "go.mod" || entry.Name() == "go.sum" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(outputDir, entry.Name())); err != nil {
			return err
		}
	}

	return nil
}

func ensureGoModule(outputDir, modulePath string) error {
	if !isModulePath(modulePath) {
		return fmt.Errorf("invalid generated module path %q", modulePath)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	goModPath := filepath.Join(outputDir, "go.mod")
	existing, err := os.ReadFile(goModPath)
	if err == nil {
		for _, line := range strings.Split(string(existing), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "module" {
				if fields[1] != modulePath {
					return fmt.Errorf(
						"generated module is %q, requested %q in %s",
						fields[1],
						modulePath,
						goModPath,
					)
				}

				return nil
			}
		}

		return fmt.Errorf("existing %s has no module declaration", goModPath)
	}

	if !os.IsNotExist(err) {
		return err
	}

	content := fmt.Sprintf("module %s\n\ngo 1.26\n", modulePath)
	return os.WriteFile(goModPath, []byte(content), 0644)
}

func programUsesModule(model *SemanticModel, importPath string) bool {
	if model == nil {
		return false
	}
	for _, pkg := range model.Packages {
		for _, path := range pkg.Imports {
			if path == importPath {
				return true
			}
		}
	}
	return false
}

func ensureGoModuleRequirement(outputDir, modulePath, version string) error {
	goModPath := filepath.Join(outputDir, "go.mod")
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		for index := range fields {
			if fields[index] == modulePath {
				return nil
			}
		}
	}

	updated := strings.TrimRight(string(content), "\n") + "\n\nrequire " + modulePath + " " + version + "\n"
	if err := os.WriteFile(goModPath, []byte(updated), 0644); err != nil {
		return err
	}
	return ensureGoModuleSums(outputDir, modulePath, version)
}

func ensureGoModuleSums(outputDir, modulePath, version string) error {
	if modulePath != "golang.org/x/net" || version != "v0.58.0" {
		return nil
	}
	goSumPath := filepath.Join(outputDir, "go.sum")
	content, err := os.ReadFile(goSumPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(content), "\n")
	entries := []string{
		"golang.org/x/net v0.58.0 h1:ynWG7rqYi4ccpTEuPZ2QGWHktVEM9DMCj9yzDE0Q7To=",
		"golang.org/x/net v0.58.0/go.mod h1:YwCddHnFlT7eLQqVprV19OnhLGtc5xOKgE0RyqgfWAU=",
	}
	for _, entry := range entries {
		found := false
		for _, line := range lines {
			if line == entry {
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, entry)
		}
	}
	return os.WriteFile(goSumPath, []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n")+"\n"), 0644)
}

func ensureGeneratedMarker(outputDir string) error {
	return os.WriteFile(filepath.Join(outputDir, ".gpp-generated"), []byte("Go++ compiler output\n"), 0644)
}

func ensureSharedIntrospectionRuntime(outputDir string) error {
	directory := filepath.Join(outputDir, "gpp", "runtime")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	source := "package runtime\n\nimport (\n\tgppFmt \"fmt\"\n\tgppReflect \"reflect\"\n\tgppStrconv \"strconv\"\n\tgppStrings \"strings\"\n)\n\n" + introspectionRuntimeDefinitions()
	return os.WriteFile(filepath.Join(directory, "runtime.go"), []byte(source), 0644)
}

func isModulePath(modulePath string) bool {
	if modulePath == "" || strings.TrimSpace(modulePath) != modulePath {
		return false
	}

	for _, r := range modulePath {
		if unicode.IsSpace(r) || r == '\\' || r == ':' {
			return false
		}
	}

	return modulePath != "." && modulePath != ".." &&
		!strings.HasPrefix(modulePath, "/") &&
		!strings.HasSuffix(modulePath, "/")
}

func goPackageName(logicalPackage string) string {
	parts := strings.Split(logicalPackage, ".")
	return parts[len(parts)-1]
}
