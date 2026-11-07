package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModuleGoPlusImportPackagesCollectPackageSymbols(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"math.gpp": `package math
func Sqrt(value int) int { return value }
`,
		"nested/root.gpp": `package nested.math
class Root {}
`,
		"nested/extra.gpp": `package nested.math
func RootValue() int { return 1 }
`,
	}
	for name, source := range files {
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}

	packages, err := moduleGoPlusImportPackages(root, "example.com/project")
	if err != nil {
		t.Fatal(err)
	}
	mathPackage, ok := chooseImportPackage("math", "Sqrt", packages, nil)
	if !ok || mathPackage.ImportPath != "example.com/project/math" || !mathPackage.Module {
		t.Fatalf("did not find module math package: %#v, %t", mathPackage, ok)
	}
	nestedPackage, ok := chooseImportPackage("math", "RootValue", packages, nil)
	if !ok || nestedPackage.ImportPath != "example.com/project/nested/math" {
		t.Fatalf("did not collect symbols across dotted package files: %#v, %t", nestedPackage, ok)
	}
	if !nestedPackage.Symbols["Root"] {
		t.Fatalf("class symbol from the first package file was lost: %#v", nestedPackage.Symbols)
	}
}

func TestAvailableImportPackagesIncludesCurrentModuleGoPlusPackages(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/project\n\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "internal", "math", "math.gpp")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(`package internal.math
func Root(value int) int { return value }
`), 0644); err != nil {
		t.Fatal(err)
	}

	packages, err := availableImportPackages([]string{sourcePath})
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := chooseImportPackage("math", "Root", packages, nil)
	if !ok || selected.ImportPath != "example.com/project/internal/math" || !selected.Module {
		t.Fatalf("current-module package was not available to fiximports: %#v, %t", selected, ok)
	}
}

func TestFixImportsPrefersMatchingModulePackage(t *testing.T) {
	root := t.TempDir()
	packagePath := filepath.Join(root, "math.gpp")
	if err := os.WriteFile(packagePath, []byte(`package math
func Sqrt(value int) int { return value }
`), 0644); err != nil {
		t.Fatal(err)
	}
	modulePackages, err := moduleGoPlusImportPackages(root, "example.com/project")
	if err != nil {
		t.Fatal(err)
	}
	packages := map[string][]importPackage{
		"math": {
			{ImportPath: "math", Name: "math", Symbols: map[string]bool{"Sqrt": true}, Standard: true},
			{ImportPath: "gpp/math", Name: "math", Symbols: map[string]bool{"Sqrt": true}, GoPlus: true},
		},
	}
	for _, candidates := range modulePackages {
		for _, candidate := range candidates {
			addImportPackage(packages, candidate)
		}
	}

	source := []byte("package app\n\nfunc main() { _ = math.Sqrt(4) }\n")
	fixed, added, err := fixImports("app.gpp", source, packages)
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("expected the module package import to be added")
	}
	if !strings.Contains(string(fixed), `"example.com/project/math"`) {
		t.Fatalf("module package was not selected:\n%s", fixed)
	}
	if strings.Contains(string(fixed), `"math"`) || strings.Contains(string(fixed), `"gpp/math"`) {
		t.Fatalf("a lower-priority candidate was selected:\n%s", fixed)
	}
}

func TestFixImportsMergesNewImportIntoExistingGroup(t *testing.T) {
	packages := map[string][]importPackage{
		"common": {{
			ImportPath: "telga/common",
			Name:       "common",
			Symbols:    map[string]bool{"Server": true},
			Module:     true,
		}},
	}
	source := []byte(`package app

import (
    "gpp/http"
    gppsql  "gpp/sql"
    stdsql "database/sql"

)

func main() { _ = common.Server{} }
`)
	fixed, added, err := fixImports("app.gpp", source, packages)
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("expected the missing module import to be added")
	}
	want := `package app

import (
    "gpp/http"
    gppsql  "gpp/sql"
    stdsql "database/sql"
    "telga/common"
)

func main() { _ = common.Server{} }
`
	if string(fixed) != want {
		t.Fatalf("new import was not merged cleanly into the existing group:\n%s", fixed)
	}
}

func TestFixImportsUsesMatchingSymbolWhenModulePackageNameCollides(t *testing.T) {
	packages := map[string][]importPackage{
		"math": {
			{ImportPath: "example.com/project/math", Name: "math", Symbols: map[string]bool{"Other": true}, Module: true},
			{ImportPath: "math", Name: "math", Symbols: map[string]bool{"Sqrt": true}, Standard: true},
		},
	}
	selected, ok := chooseImportPackage("math", "Sqrt", packages, nil)
	if !ok || selected.ImportPath != "math" {
		t.Fatalf("symbol matching chose the wrong package: %#v, %t", selected, ok)
	}
}
