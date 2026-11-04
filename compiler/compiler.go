package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type CompileOptions struct {
	ModulePath        string
	ProjectRoot       string
	Development       bool
	NoPrelude         bool
	NoStdlib          bool
	CleanOutput       bool
	IncrementalOutput bool
}

const generatedFilesManifestName = ".gpp-generated-files.json"

type generatedFilesManifest struct {
	Files    []string                          `json:"files"`
	Packages map[string]generatedPackageOutput `json:"packages,omitempty"`
}

type generatedPackageOutput struct {
	Fingerprint string            `json:"fingerprint"`
	Files       map[string]string `json:"files"`
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
	previousGeneratedFiles := map[string]bool{}
	previousPackages := map[string]generatedPackageOutput{}
	if options.IncrementalOutput && !options.CleanOutput {
		var found bool
		var err error
		previousGeneratedFiles, previousPackages, found, err = loadGeneratedFilesManifest(outputDir)
		if err != nil {
			return err
		}
		if !found {
			if err := cleanGeneratedOutput(outputDir); err != nil {
				return err
			}
			previousGeneratedFiles = map[string]bool{}
			previousPackages = map[string]generatedPackageOutput{}
		}
	}
	currentGeneratedFiles := map[string]bool{}
	trackGeneratedFile := func(path string) error {
		if !options.IncrementalOutput {
			return nil
		}
		relative, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return fmt.Errorf("generated file %s is outside output directory %s", path, outputDir)
		}
		currentGeneratedFiles[filepath.ToSlash(relative)] = true
		return nil
	}

	program := &Program{}
	parseCacheDir := ""
	if options.IncrementalOutput {
		parseCacheDir = outputDir
	}
	parsedFiles, err := parseInputFiles(files, parseCacheDir)
	if err != nil {
		return err
	}
	program.Files = parsedFiles
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
	packageFingerprints := generatedPackageFingerprints(program, model, options, atExit, usesIntrospection)
	packageOutputs := make(map[string]generatedPackageOutput, len(packageFingerprints))
	reusePackage := make(map[string]bool, len(packageFingerprints))
	for packageName, fingerprint := range packageFingerprints {
		previous, exists := previousPackages[packageName]
		if exists && previous.Fingerprint == fingerprint && generatedPackageFilesMatch(outputDir, previous.Files, program.Files, outputNames, packageName) {
			packageOutputs[packageName] = previous
			reusePackage[packageName] = true
		}
	}
	if usesIntrospection && options.ModulePath != "" {
		if err := ensureSharedIntrospectionRuntime(outputDir); err != nil {
			return err
		}
		if err := trackGeneratedFile(filepath.Join(outputDir, "gpp", "runtime", "runtime.go")); err != nil {
			return err
		}
	}

	fileEmbeds := make(map[*File][]compiledEmbed, len(program.Files))
	fileOutputPaths := make(map[*File]string, len(program.Files))
	for _, file := range program.Files {
		dir := generatedPackageDirectory(outputDir, file.Package)
		fileOutputPaths[file] = filepath.Join(dir, outputNames[file])
		embeds, err := prepareEmbeds(file, dir, options.ProjectRoot)
		if err != nil {
			return fmt.Errorf("%s: %w", file.Name, err)
		}
		fileEmbeds[file] = embeds
		for _, embed := range embeds {
			path := filepath.Join(dir, filepath.FromSlash(embed.Path))
			if embed.Directory {
				if err := filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if entry.IsDir() {
						return nil
					}
					return trackGeneratedFile(current)
				}); err != nil {
					return err
				}
			} else if err := trackGeneratedFile(path); err != nil {
				return err
			}
		}
		if err := trackGeneratedFile(fileOutputPaths[file]); err != nil {
			return err
		}
	}

	packageFiles := map[string][]*File{}
	packageOrder := []string{}
	for _, file := range program.Files {
		if packageFiles[file.Package] == nil {
			packageOrder = append(packageOrder, file.Package)
		}
		packageFiles[file.Package] = append(packageFiles[file.Package], file)
	}
	type packageEmission struct {
		output generatedPackageOutput
		err    error
	}
	emissions := make([]packageEmission, len(packageOrder))
	workerCount := runtime.GOMAXPROCS(0)
	if workerCount > len(packageOrder) {
		workerCount = len(packageOrder)
	}
	jobs := make(chan int)
	var emitters sync.WaitGroup
	emitters.Add(workerCount)
	for range workerCount {
		go func() {
			defer emitters.Done()
			for index := range jobs {
				packageName := packageOrder[index]
				if reusePackage[packageName] {
					emissions[index].output = packageOutputs[packageName]
					continue
				}
				emissions[index].output, emissions[index].err = emitPackageFiles(
					packageFiles[packageName],
					outputDir,
					options,
					model,
					functionSignatures,
					usesIntrospection,
					atExit,
					preludeExtensionsNeeded[packageName],
					fileEmbeds,
					fileOutputPaths,
					packageFingerprints[packageName],
				)
			}
		}()
	}
	for index := range packageOrder {
		jobs <- index
	}
	close(jobs)
	emitters.Wait()
	for index, packageName := range packageOrder {
		if emissions[index].err != nil {
			return emissions[index].err
		}
		packageOutputs[packageName] = emissions[index].output
	}
	if options.IncrementalOutput {
		for oldPath := range previousGeneratedFiles {
			if currentGeneratedFiles[oldPath] {
				continue
			}
			path := filepath.Join(outputDir, filepath.FromSlash(oldPath))
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			removeEmptyGeneratedParents(filepath.Dir(path), outputDir)
		}
		if err := writeGeneratedFilesManifest(outputDir, currentGeneratedFiles, packageOutputs); err != nil {
			return err
		}
	}

	return nil
}

func parseInputFiles(filenames []string, cacheDir string) ([]*File, error) {
	type result struct {
		file *File
		err  error
	}
	results := make([]result, len(filenames))
	workers := runtime.GOMAXPROCS(0)
	if workers > len(filenames) {
		workers = len(filenames)
	}
	if workers < 1 {
		return nil, nil
	}
	jobs := make(chan int)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			for index := range jobs {
				filename := filenames[index]
				data, err := os.ReadFile(filename)
				if err != nil {
					results[index].err = err
					continue
				}
				file, err := ParseFileWithCache(filepath.Base(filename), filename, string(data), cacheDir)
				if err != nil {
					results[index].err = err
					continue
				}
				results[index].file = file
			}
		}()
	}
	for index := range filenames {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	files := make([]*File, len(results))
	for index, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		files[index] = result.file
	}
	return files, nil
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

func generatedPackageDirectory(outputDir, packageName string) string {
	directory := outputDir
	if packageName != "main" {
		for _, segment := range strings.Split(packageName, ".") {
			directory = filepath.Join(directory, segment)
		}
	}
	return directory
}

func emitPackageFiles(files []*File, outputDir string, options CompileOptions, model *SemanticModel, functionSignatures map[string]map[string][]callableSignature, usesIntrospection, atExit bool, preludeExtensions map[string]bool, embeds map[*File][]compiledEmbed, outputPaths map[*File]string, fingerprint string) (generatedPackageOutput, error) {
	packageName := files[0].Package
	records := newRecordContext()
	introspection := newIntrospectionContext(model.Packages[packageName].Classes)
	introspection.Enabled = usesIntrospection
	exceptions := &exceptionContext{}
	preludeEmitted := false
	output := generatedPackageOutput{Fingerprint: fingerprint, Files: make(map[string]string, len(files))}
	for _, file := range files {
		context, err := constructorContextForFile(file, model, options.ModulePath)
		if err != nil {
			return generatedPackageOutput{}, fmt.Errorf("%s: %w", file.Name, err)
		}
		context.Records = records
		context.Introspection = introspection
		context.Exceptions = exceptions
		context.Annotations = model.Packages[packageName].Annotations
		context.Package = packageName
		context.AtExit = packageName == "main" && atExit
		context.ModulePath = options.ModulePath
		context.Development = options.Development
		context.AvailableImports = availableImportsForFile(file, model, options.ModulePath)
		context.ImportedFunctionSignatures = map[string]map[string][]callableSignature{}
		if packageSymbols := model.Packages[packageName]; packageSymbols != nil {
			for alias, importPath := range packageSymbols.Imports {
				logical, ok := logicalPackageForImport(importPath, options.ModulePath, model)
				if !ok || functionSignatures[logical] == nil {
					continue
				}
				context.ImportedFunctionSignatures[alias] = functionSignatures[logical]
			}
		}
		context.Templates = model.Packages[packageName].Templates
		if usesIntrospection && options.ModulePath != "" {
			context.IntrospectionRuntimeImport = options.ModulePath + "/gpp/runtime"
		}
		if !options.NoPrelude {
			context.EmitPrelude = !preludeEmitted
			context.PreludeExtensionsNeeded = preludeExtensions
			preludeEmitted = true
			if err := configurePrelude(&context, context.EmitPrelude); err != nil {
				return generatedPackageOutput{}, fmt.Errorf("%s: %w", file.Name, err)
			}
		}
		configureNativeExtensionMethods(&context, file)
		context.FunctionSignatures = functionSignatures[packageName]
		context.Enums = model.Packages[packageName].Enums
		configureImportedEnums(&context, file, model, options.ModulePath)
		configureEnumSignatures(&context)
		if err := configureFunctionOverloads(&context.Overloads, context.FunctionSignatures); err != nil {
			return generatedPackageOutput{}, err
		}
		context.Embeds = embeds[file]

		code, err := emitFile(file, context)
		if err != nil {
			if isSourceLineDiagnostic(err) {
				return generatedPackageOutput{}, fmt.Errorf("%s:%w", file.Name, err)
			}
			return generatedPackageOutput{}, fmt.Errorf("%s: %w", file.Name, err)
		}

		generatedPath := outputPaths[file]
		if err := os.MkdirAll(filepath.Dir(generatedPath), 0755); err != nil {
			return generatedPackageOutput{}, err
		}
		if err := writeFileIfChanged(generatedPath, code, 0644); err != nil {
			return generatedPackageOutput{}, err
		}
		relative, err := filepath.Rel(outputDir, generatedPath)
		if err != nil {
			return generatedPackageOutput{}, err
		}
		digest, err := generatedFileDigest(generatedPath)
		if err != nil {
			return generatedPackageOutput{}, err
		}
		output.Files[filepath.ToSlash(relative)] = digest
	}
	return output, nil
}

func generatedPackageFingerprints(program *Program, model *SemanticModel, options CompileOptions, atExit, usesIntrospection bool) map[string]string {
	filesByPackage := map[string][]*File{}
	for _, file := range program.Files {
		filesByPackage[file.Package] = append(filesByPackage[file.Package], file)
	}
	localFingerprints := make(map[string]string, len(filesByPackage))
	for packageName, files := range filesByPackage {
		h := sha256.New()
		writePackageHashPart(h, "package-fingerprint-version=3")
		writePackageHashPart(h, "package="+packageName)
		for _, file := range files {
			writePackageHashPart(h, "source-path="+file.SourcePath)
			writePackageHashPart(h, "source-name="+file.Name)
			writePackageHashPart(h, "source="+file.Source)
			writePackageHashPart(h, "emission-order="+file.SourcePath)
		}
		if symbols := model.Packages[packageName]; symbols != nil {
			aliases := make([]string, 0, len(symbols.Imports))
			for alias := range symbols.Imports {
				aliases = append(aliases, alias)
			}
			sort.Strings(aliases)
			for _, alias := range aliases {
				writePackageHashPart(h, "import="+alias+"="+symbols.Imports[alias])
			}
		}
		localFingerprints[packageName] = hex.EncodeToString(h.Sum(nil))
	}
	fingerprints := make(map[string]string, len(filesByPackage))
	for packageName := range filesByPackage {
		reachable := map[string]bool{}
		var visit func(string)
		visit = func(current string) {
			if reachable[current] {
				return
			}
			reachable[current] = true
			packageSymbols := model.Packages[current]
			if packageSymbols == nil {
				return
			}
			for _, importPath := range packageSymbols.Imports {
				if logical, ok := logicalPackageForImport(importPath, options.ModulePath, model); ok {
					visit(logical)
				}
			}
		}
		visit(packageName)
		dependencies := make([]string, 0, len(reachable))
		for dependency := range reachable {
			dependencies = append(dependencies, dependency)
		}
		sort.Strings(dependencies)
		h := sha256.New()
		writePackageHashPart(h, "module="+options.ModulePath)
		writePackageHashPart(h, "project-root="+options.ProjectRoot)
		writePackageHashPart(h, fmt.Sprintf("development=%t", options.Development))
		writePackageHashPart(h, fmt.Sprintf("no-prelude=%t", options.NoPrelude))
		writePackageHashPart(h, fmt.Sprintf("no-stdlib=%t", options.NoStdlib))
		writePackageHashPart(h, fmt.Sprintf("at-exit=%t", atExit))
		writePackageHashPart(h, fmt.Sprintf("introspection=%t", usesIntrospection))
		for _, dependency := range dependencies {
			writePackageHashPart(h, "package="+dependency)
			writePackageHashPart(h, "dependency-fingerprint="+localFingerprints[dependency])
		}
		fingerprints[packageName] = hex.EncodeToString(h.Sum(nil))
	}
	return fingerprints
}

func writePackageHashPart(h interface{ Write([]byte) (int, error) }, value string) {
	_, _ = fmt.Fprintf(h, "%d:", len(value))
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{0})
}

func generatedPackageFilesMatch(outputDir string, cachedFiles map[string]string, files []*File, outputNames map[*File]string, packageName string) bool {
	expected := map[string]bool{}
	for _, file := range files {
		if file.Package != packageName {
			continue
		}
		relative, err := filepath.Rel(outputDir, filepath.Join(generatedPackageDirectory(outputDir, packageName), outputNames[file]))
		if err != nil {
			return false
		}
		expected[filepath.ToSlash(relative)] = true
	}
	if len(expected) == 0 || len(expected) != len(cachedFiles) {
		return false
	}
	for path := range expected {
		if cachedFiles[path] == "" {
			return false
		}
		digest, err := generatedFileDigest(filepath.Join(outputDir, filepath.FromSlash(path)))
		if err != nil || digest != cachedFiles[path] {
			return false
		}
	}
	return true
}

func generatedFileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(stripUnusedSuppressionMarkers(data))
	return hex.EncodeToString(digest[:]), nil
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
	return writeFileIfChanged(destination, data, 0644)
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
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writeFileIfChanged(destination, data, mode)
}

func writeFileIfChanged(path string, data []byte, mode os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil && (bytes.Equal(existing, data) || (filepath.Ext(path) == ".go" && bytes.Equal(stripUnusedSuppressionMarkers(existing), data))) {
		if info, statErr := os.Stat(path); statErr == nil {
			if info.Mode().Perm() == mode.Perm() {
				return nil
			}
			return os.Chmod(path, mode.Perm())
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

// stripUnusedSuppressionMarkers lets command-level warning suppression survive
// incremental compiler writes when the underlying generated Go is unchanged.
func stripUnusedSuppressionMarkers(source []byte) []byte {
	const marker = "/*gpp:unused-suppression*/"
	result := string(source)
	for searchFrom := 0; ; {
		markerAt := strings.Index(result[searchFrom:], marker)
		if markerAt < 0 {
			break
		}
		markerAt += searchFrom
		assignmentAt := strings.LastIndex(result[:markerAt], "; _ = ")
		if assignmentAt < 0 {
			searchFrom = markerAt + len(marker)
			continue
		}
		end := markerAt + len(marker)
		result = result[:assignmentAt] + result[end:]
		searchFrom = assignmentAt
	}
	return []byte(result)
}

func loadGeneratedFilesManifest(outputDir string) (map[string]bool, map[string]generatedPackageOutput, bool, error) {
	path := filepath.Join(outputDir, generatedFilesManifestName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	var manifest generatedFilesManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, nil, false, nil
	}
	files := make(map[string]bool, len(manifest.Files))
	for _, name := range manifest.Files {
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, nil, false, nil
		}
		files[filepath.ToSlash(clean)] = true
	}
	for _, packageOutput := range manifest.Packages {
		for name := range packageOutput.Files {
			clean := filepath.Clean(filepath.FromSlash(name))
			if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				return nil, nil, false, nil
			}
		}
	}
	return files, manifest.Packages, true, nil
}

func writeGeneratedFilesManifest(outputDir string, files map[string]bool, packages map[string]generatedPackageOutput) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	data, err := json.Marshal(generatedFilesManifest{Files: names, Packages: packages})
	if err != nil {
		return err
	}
	return writeFileIfChanged(filepath.Join(outputDir, generatedFilesManifestName), data, 0644)
}

func removeEmptyGeneratedParents(directory, outputDir string) {
	root, err := filepath.Abs(outputDir)
	if err != nil {
		return
	}
	for current, err := filepath.Abs(directory); err == nil && current != root && isWithinDirectory(root, current); current = filepath.Dir(current) {
		if err := os.Remove(current); err != nil {
			return
		}
	}
}

func isWithinDirectory(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
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
	return writeFileIfChanged(filepath.Join(outputDir, ".gpp-generated"), []byte("Go++ compiler output\n"), 0644)
}

func ensureSharedIntrospectionRuntime(outputDir string) error {
	directory := filepath.Join(outputDir, "gpp", "runtime")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	source := "package runtime\n\nimport (\n\tgppFmt \"fmt\"\n\tgppReflect \"reflect\"\n\tgppStrconv \"strconv\"\n\tgppStrings \"strings\"\n)\n\n" + introspectionRuntimeDefinitions()
	return writeFileIfChanged(filepath.Join(directory, "runtime.go"), []byte(source), 0644)
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
