package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type CompileOptions struct {
	ModulePath string
	NoPrelude  bool
	NoStdlib   bool
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

	if options.ModulePath != "" {
		if err := ensureGoModule(outputDir, options.ModulePath); err != nil {
			return err
		}
	}
	recordContexts := map[string]*recordContext{}
	introspectionContexts := map[string]*introspectionContext{}
	preludeEmitted := map[string]bool{}
	preludeNeeded := map[string]bool{}
	if !options.NoPrelude {
		for _, file := range program.Files {
			used, err := preludeUsedInFile(file)
			if err != nil {
				return err
			}
			if used {
				preludeNeeded[file.Package] = true
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
		context.Annotations = model.Packages[file.Package].Annotations
		context.Package = file.Package
		context.ModulePath = options.ModulePath
		context.AvailableImports = availableImportsForFile(file, model, options.ModulePath)
		if usesIntrospection && options.ModulePath != "" {
			context.IntrospectionRuntimeImport = options.ModulePath + "/gpp/runtime"
		}
		if !options.NoPrelude {
			context.EmitPrelude = !preludeEmitted[file.Package]
			context.EmitPreludeAll = context.EmitPrelude && preludeNeeded[file.Package]
			preludeEmitted[file.Package] = true
			if err := configurePrelude(&context, context.EmitPrelude); err != nil {
				return fmt.Errorf("%s: %w", file.Name, err)
			}
		}
		configureNativeExtensionMethods(&context, file)
		context.FunctionSignatures = functionSignatures[file.Package]
		if err := configureFunctionOverloads(&context.Overloads, context.FunctionSignatures); err != nil {
			return fmt.Errorf("%s: %w", file.Name, err)
		}

		code, err := emitFile(
			file,
			context,
		)

		if err != nil {
			return fmt.Errorf(
				"%s: %w",
				file.Name,
				err,
			)
		}

		dir := outputDir

		if file.Package != "main" {
			for _, segment := range strings.Split(file.Package, ".") {
				dir = filepath.Join(dir, segment)
			}
		}

		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}

		name := strings.TrimSuffix(
			file.Name,
			filepath.Ext(file.Name),
		) + ".go"

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

func ensureSharedIntrospectionRuntime(outputDir string) error {
	directory := filepath.Join(outputDir, "gpp", "runtime")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	source := "package runtime\n\n" + introspectionRuntimeDefinitions()
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
