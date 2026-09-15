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
//go:embed stdlib/gpp/*/*.gpp
var officialStdlib embed.FS

func appendOfficialStdlib(program *Program) error {
	loaded := map[string]bool{}
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
	if importPath != "gpp/http" && importPath != "gpp/orm" {
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
		files = append(files, file)
	}
	return files, nil
}
