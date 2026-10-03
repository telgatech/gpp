package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validateTemplateSourceForTest(t *testing.T, source, templateName string) error {
	t.Helper()
	file, err := ParseFile("typed-template.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	model, err := ResolveProgram(&Program{Files: []*File{file}})
	if err != nil {
		t.Fatal(err)
	}
	packageSymbols := model.Packages[file.Package]
	context := localConstructorContext(packageSymbols.Classes)
	context.Templates = packageSymbols.Templates
	context.Enums = packageSymbols.Enums
	template := packageSymbols.Templates[templateName]
	if template == nil {
		t.Fatalf("template %s was not parsed", templateName)
	}
	names := make([]string, 0, len(context.Templates))
	for name := range context.Templates {
		names = append(names, name)
	}
	return validateTemplateBody(template, names, context)
}

func TestTypedTemplateChecksNestedFieldsAndControlFlow(t *testing.T) {
	source := `package main
class Author { Name string }
class Post {
    Author Author
    Tags []string
}
template Page(post Post) {
    {{with .Author}}{{$name := .Name}}{{if $name}}{{$name}}{{end}}{{end}}
    {{if .Tags}}{{range .Tags}}{{.}}{{end}}{{end}}
}
`
	if err := validateTemplateSourceForTest(t, source, "Page"); err != nil {
		t.Fatalf("expected typed template to validate: %v", err)
	}
}

func TestTypedTemplateRejectsUnknownClassMember(t *testing.T) {
	source := `package main
class Post { Title string }
template Page(post Post) {
    {{.Missing}}
}
`
	err := validateTemplateSourceForTest(t, source, "Page")
	if err == nil || !strings.Contains(err.Error(), "has no field or method Missing") || !strings.HasPrefix(err.Error(), "4:") {
		t.Fatalf("expected a source-located missing-member diagnostic, got %v", err)
	}
}

func TestCompileFilesReportsUnknownTemplateCallWithSingleSourceLocation(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "tpl.gpp")
	source := `package main

template Page() {
    {{PostCards}}
}

func main() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated", NoStdlib: true})
	want := `tpl.gpp:4: invalid template body: function "PostCards" not defined`
	if err == nil || err.Error() != want {
		t.Fatalf("expected one precise template diagnostic %q, got %v", want, err)
	}
}

func TestTypedTemplateChecksInheritedMembersAndMethodArguments(t *testing.T) {
	source := `package main
class Named {
    Name string
    func Label(prefix string) string { return prefix + this.Name }
}
class User: Named { Active bool }
template Page(user User) {
    {{.Name}} {{.Label "User:"}} {{.Active}}
}
`
	if err := validateTemplateSourceForTest(t, source, "Page"); err != nil {
		t.Fatalf("expected inherited fields and methods to validate: %v", err)
	}
	badCall := strings.Replace(source, `{{.Label "User:"}}`, `{{.Label 42}}`, 1)
	err := validateTemplateSourceForTest(t, badCall, "Page")
	if err == nil || !strings.Contains(err.Error(), "cannot use int as string") {
		t.Fatalf("expected a typed method-argument diagnostic, got %v", err)
	}
}

func TestTypedTemplateChecksKnownTemplateCallsInsideRange(t *testing.T) {
	valid := `package main
class Post { Title string }
template Card(post Post) { {{.Title}} }
template Page(posts []Post) { {{range .}}{{Card .}}{{end}} }
`
	if err := validateTemplateSourceForTest(t, valid, "Page"); err != nil {
		t.Fatalf("expected nested template call to validate: %v", err)
	}
	invalid := `package main
class Post { Title string }
class User { Name string }
template Card(post Post) { {{.Title}} }
template Page(users []User) { {{range .}}{{Card .}}{{end}} }
`
	err := validateTemplateSourceForTest(t, invalid, "Page")
	if err == nil || !strings.Contains(err.Error(), "cannot use User as Post") {
		t.Fatalf("expected a nested template argument type error, got %v", err)
	}
}

func TestTypedTemplateChecksNamedFieldsForMultipleParameters(t *testing.T) {
	source := `package main
class User { Name string }
class Post { Title string }
template Page(user User, posts []Post) {
    {{.user.Name}}{{range .posts}}{{.Title}}{{end}}
}
`
	if err := validateTemplateSourceForTest(t, source, "Page"); err != nil {
		t.Fatalf("expected named template data fields to validate: %v", err)
	}
}

func TestTypedTemplateLeavesAnyAccessForRuntime(t *testing.T) {
	source := `package main
template Dynamic(data any) { {{.UnknownAtCompileTime}} }
`
	if err := validateTemplateSourceForTest(t, source, "Dynamic"); err != nil {
		t.Fatalf("expected any member access to remain dynamic: %v", err)
	}
}
