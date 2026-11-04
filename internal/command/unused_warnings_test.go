package command

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuppressUnusedLocalErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.go")
	source := "package main\n//line classifieds.gpp:916\nfunc main() {\n\tx := 10\n}\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	warnings, err := suppressUnusedLocalErrors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %#v", len(warnings), warnings)
	}
	if got := warnings[0].String(); !strings.HasSuffix(got, "classifieds.gpp:917: warning: declared and not used: x") {
		t.Fatalf("warning = %q, want a classifieds.gpp:917 diagnostic", got)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "_ = x") {
		t.Fatalf("generated source does not mark x as used:\n%s", updated)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), path, updated, parser.AllErrors); err != nil {
		t.Fatalf("generated source is invalid after suppression: %v", err)
	}

	warnings, err = suppressUnusedLocalErrors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("second pass returned warnings: %#v", warnings)
	}
}

func TestSuppressUnusedLocalErrorsLeavesUsedVariablesAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.go")
	source := "package main\nfunc main() {\n\tx := 10\n\t_ = x\n}\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	warnings, err := suppressUnusedLocalErrors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("got warnings for used variable: %#v", warnings)
	}
}
