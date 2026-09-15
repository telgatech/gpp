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

func TestParseMultiTargetExtension(t *testing.T) {
	file, err := ParseFile("multi_extension.gpp", `
extend
    string,
    []byte,
    map[string]int
{
    func Empty() bool {
        return len(this) == 0
    }
}

`)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 1 {
		t.Fatalf("expected one declaration, got %d", len(file.Decls))
	}
	extension, ok := file.Decls[0].(*ExtendDecl)
	if !ok {
		t.Fatalf("expected extension declaration, got %#v", file.Decls[0])
	}
	expected := []string{"string", "[]byte", "map[string]int"}
	if strings.Join(extension.Targets, "|") != strings.Join(expected, "|") {
		t.Fatalf("unexpected extension targets: %#v", extension.Targets)
	}
}

func TestParseGenericExtensionTargetConstraint(t *testing.T) {
	file, err := ParseFile("generic_extension.gpp", `
extend []T where T cmp.Ordered {
    func Min() (T, bool) { return this[0], true }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	extension, ok := file.Decls[0].(*ExtendDecl)
	if !ok {
		t.Fatalf("expected extension declaration, got %#v", file.Decls[0])
	}
	if len(extension.Targets) != 1 || extension.Targets[0] != "[]T" {
		t.Fatalf("unexpected generic target: %#v", extension.Targets)
	}
	if extension.TargetConstraints["T"] != "cmp.Ordered" {
		t.Fatalf("unexpected target constraints: %#v", extension.TargetConstraints)
	}
}
