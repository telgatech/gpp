package compiler

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzCompileSourceProducesValidGoSyntax sends whole source files through
// parsing, semantic resolution, and code generation. Invalid GPP source may
// return a compiler error; generated Go must remain syntactically valid.
func FuzzCompileSourceProducesValidGoSyntax(f *testing.F) {
	f.Add("package main\nfunc main() {}\n")
	f.Add(`package main

func main() {
	values := []string{"x"}
	upper := values.Map(value => value.ToUpper())
	_ = upper
}
`)
	f.Add(`package main

func summary(user struct { Name string }) struct {
	return struct(Name: user.Name, Active: true)
}

func main() {
	result := summary(struct(Name: "Ada"))
	_ = result.Active
}
`)
	f.Add(`package main

class Base {
	func Name() string { return "base" }
}

class Child : Base {}

func main() {
	child := Child()
	_ = child.Name()
}
`)
	f.Add(`package main

func main() {
	values := []string{"a", "b"}
	values.Each(value => println(value))
}
`)
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 16*1024 {
			t.Skip()
		}
		if strings.TrimSpace(source) == "" {
			t.Skip()
		}
		inputDir := t.TempDir()
		outputDir := t.TempDir()
		main := filepath.Join(inputDir, "main.gpp")
		if err := os.WriteFile(main, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		if err := CompileFilesWithOptions([]string{main}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
			return
		}
		if err := filepath.WalkDir(outputDir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" {
				return nil
			}
			if _, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.AllErrors); parseErr != nil {
				return parseErr
			}
			return nil
		}); err != nil {
			t.Fatalf("compiler generated invalid Go syntax: %v\nGPP source:\n%s", err, source)
		}
	})
}
