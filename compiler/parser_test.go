package compiler

import (
	"strings"
	"testing"
)

func TestParseRejectsUseDeclaration(t *testing.T) {
	_, err := ParseFile("legacy.gpp", "use fmt\n")
	if err == nil || !strings.Contains(err.Error(), "use declarations are no longer supported") {
		t.Fatalf("expected use declaration error, got %v", err)
	}
}

func TestParseReportsSourceLineForExtensionErrors(t *testing.T) {
	_, err := ParseFile("broken.gpp", "\n\nclass Broken\n")
	if err == nil || !strings.Contains(err.Error(), "broken.gpp:3:") {
		t.Fatalf("expected source line in parser error, got %v", err)
	}
}

func TestParseRejectsDuplicatePackageDeclaration(t *testing.T) {
	_, err := ParseFile("duplicate.gpp", "package one\npackage two\n")
	if err == nil || !strings.Contains(err.Error(), "duplicate package declaration") {
		t.Fatalf("expected duplicate package error, got %v", err)
	}
}

func TestParseValidatesDottedPackageNames(t *testing.T) {
	file, err := ParseFile("nested.gpp", "package telga.db.models\n")
	if err != nil {
		t.Fatal(err)
	}
	if file.Package != "telga.db.models" {
		t.Fatalf("unexpected logical package: %q", file.Package)
	}

	_, err = ParseFile("invalid.gpp", "package telga..models\n")
	if err == nil || !strings.Contains(err.Error(), "invalid package declaration") {
		t.Fatalf("expected invalid package error, got %v", err)
	}
}

func TestParseIgnoresExtensionsInsideGoBodiesAndStrings(t *testing.T) {
	file, err := ParseFile("lexical_boundaries.gpp", "func main() {\n"+
		"    _ = `class NotAClass {}`\n"+
		"    // package not a declaration\n"+
		"    _ = 1\n"+
		"}\n\n"+
		"class RealClass {}\n")
	if err != nil {
		t.Fatal(err)
	}

	if len(file.Decls) != 2 {
		t.Fatalf("expected one raw declaration and one class, got %d", len(file.Decls))
	}
	if class, ok := file.Decls[1].(*ClassDecl); !ok || class.Name != "RealClass" {
		t.Fatalf("expected RealClass after Go body, got %#v", file.Decls[1])
	}
}
