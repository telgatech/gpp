package compiler

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// officialStdlib contains the compiler-distributed Go++ standard packages.
// They are embedded so official imports never require a network lookup.
//
//go:embed stdlib/gpp/*/*.gpp stdlib/gpp/http/assets/swagger/*
var officialStdlib embed.FS

// EmbeddedStdlibAvailable reports whether the compiler was built with its
// compiler-owned Go++ standard library resources.
func EmbeddedStdlibAvailable() bool {
	entries, err := fs.Glob(officialStdlib, "stdlib/gpp/*/*.gpp")
	return err == nil && len(entries) > 0
}

// OfficialStdlibPackages returns the compiler-shipped Go++ package paths that
// can be inspected by source-level tooling such as `gpp doc`.
func OfficialStdlibPackages() []string {
	return []string{"gpp/cron", "gpp/encoding", "gpp/http", "gpp/orm", "gpp/test", "gpp/tpl"}
}

// LoadOfficialPackage exposes the embedded source model to documentation and
// other read-only tooling without requiring generated Go output.
func LoadOfficialPackage(importPath string) ([]*File, error) {
	return loadOfficialPackage(importPath)
}

func appendOfficialStdlib(program *Program) error {
	loaded := map[string]bool{}
	for _, file := range program.Files {
		for _, declaration := range file.Decls {
			if _, ok := declaration.(*TemplateDecl); ok {
				loaded["gpp/tpl"] = true
				files, err := loadOfficialPackage("gpp/tpl")
				if err != nil {
					return err
				}
				program.Files = append(program.Files, files...)
				break
			}
		}
		if loaded["gpp/tpl"] {
			break
		}
	}
	for index := 0; index < len(program.Files); index++ {
		imports, err := goImports(program.Files[index])
		if err != nil {
			continue
		}
		for _, spec := range imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if !strings.HasPrefix(importPath, "gpp/") || loaded[importPath] {
				continue
			}
			loaded[importPath] = true

			files, err := loadOfficialPackage(importPath)
			if err != nil {
				return err
			}
			program.Files = append(program.Files, files...)
		}
	}
	return nil
}

func loadOfficialPackage(importPath string) ([]*File, error) {
	if importPath != "gpp/cron" && importPath != "gpp/http" && importPath != "gpp/orm" && importPath != "gpp/encoding" && importPath != "gpp/test" && importPath != "gpp/tpl" {
		return nil, fmt.Errorf("official package %q is not bundled with this compiler", importPath)
	}

	directory := path.Join("stdlib", importPath)
	entries, err := fs.Glob(officialStdlib, directory+"/*.gpp")
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		return nil, fmt.Errorf("official package %q has no bundled source", importPath)
	}

	files := make([]*File, 0, len(entries))
	for _, entry := range entries {
		data, err := officialStdlib.ReadFile(entry)
		if err != nil {
			return nil, err
		}
		file, err := ParseFile(path.Base(entry), string(data))
		if err != nil {
			return nil, fmt.Errorf("load official package %s: %w", importPath, err)
		}
		file.Official = true
		file.OfficialPackage = importPath
		files = append(files, file)
	}
	return files, nil
}
