package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileFilesResolvesClassesAcrossDottedPackageFiles(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()

	classFile := filepath.Join(inputDir, "person.gpp")
	userFile := filepath.Join(inputDir, "use_person.gpp")

	if err := os.WriteFile(classFile, []byte(`package telga.web

class Person {
    Name string
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, []byte(`package telga.web

func NewPerson() Person {
    return Person("Bob")
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFiles([]string{classFile, userFile}, outputDir); err != nil {
		t.Fatal(err)
	}

	generatedPath := filepath.Join(outputDir, "telga", "web", "use_person.go")
	generated, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(generated), "package web") {
		t.Fatalf("generated file has incorrect package name:\n%s", generated)
	}
	if !strings.Contains(string(generated), `Person{Name: "Bob"}`) {
		t.Fatalf("class resolution did not lower the constructor:\n%s", generated)
	}
}

func TestCompileFilesWithOptionsCreatesAndChecksGoModule(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(sourcePath, []byte("func main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	options := CompileOptions{ModulePath: "example.com/acme/app"}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, options); err != nil {
		t.Fatal(err)
	}

	goMod, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(goMod) != "module example.com/acme/app\n\ngo 1.26\n" {
		t.Fatalf("unexpected generated go.mod:\n%s", goMod)
	}

	err = CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "different.example/app"},
	)
	if err == nil || !strings.Contains(err.Error(), "generated module is") {
		t.Fatalf("expected module mismatch error, got %v", err)
	}
}

func TestCompileFilesLowersQualifiedCrossPackageConstructor(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()

	classFile := filepath.Join(inputDir, "person.gpp")
	mainFile := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(classFile, []byte(`package telga.web

class Person {
    Name string
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainFile, []byte(`package main

import "generated/telga/web"

func main() {
    person := web.Person("Bob")
    _ = person
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{classFile, mainFile},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), `web.Person{Name: "Bob"}`) {
		t.Fatalf("qualified constructor was not lowered:\n%s", generated)
	}
}

func TestCompileFilesLowersCrossPackagePolymorphicTypes(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()

	classFile := filepath.Join(inputDir, "people.gpp")
	mainFile := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(classFile, []byte(`package telga.web

class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainFile, []byte(`package main

import "generated/telga/web"

func SpeakFor(person web.Person) string {
    return person.Speak()
}

func MakePerson() web.Person {
    return web.Employee()
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{classFile, mainFile},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"func SpeakFor(person web.GoppPerson) string",
		"func MakePerson() web.GoppPerson",
		"return &web.Employee{}",
	} {
		if !strings.Contains(string(generated), expected) {
			t.Fatalf("cross-package polymorphism missing %q:\n%s", expected, generated)
		}
	}
}

func TestCompileFilesResolvesFunctionOverloadsAcrossFiles(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	first := filepath.Join(inputDir, "add_one.gpp")
	second := filepath.Join(inputDir, "add_two.gpp")
	use := filepath.Join(inputDir, "use.gpp")

	for filename, source := range map[string]string{
		first: `func Add(value int) int { return value }
`,
		second: `func Add(left int, right int) int { return left + right }
`,
		use: `func Use() int { return Add(1) + Add(1, 2) }
`,
	} {
		if err := os.WriteFile(filename, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := CompileFiles([]string{first, second, use}, outputDir); err != nil {
		t.Fatal(err)
	}

	generated, err := os.ReadFile(filepath.Join(outputDir, "use.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "Add__gopp_1(1)") ||
		!strings.Contains(string(generated), "Add__gopp_2(1, 2)") {
		t.Fatalf("cross-file overload calls were not resolved:\n%s", generated)
	}
}

func TestCompileFilesGeneratedCodeBuildsWithPolymorphism(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
import "fmt"

class Person {
    Name string

    func Speak() string {
        return this.Name
    }
}

class Employee: Person {
    func Speak() string {
        return "employee: " + this.Name
    }
}

class Holder {
    Value Person
}

class Factory {
    func Make() Employee {
        return Employee("factory")
    }
}

func Announce(person Person) string {
    return person.Speak()
}

func MakePerson() Person {
    return Person("base")
}

func main() {
    var person Person = Employee("Bob")
    holder := Holder(Employee("Alice"))
    factory := Factory()
    fmt.Println(person.Speak(), holder.Value.Speak(), MakePerson().Speak())
    fmt.Println(Announce(factory.Make()))
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "test", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gopp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Go did not build: %v\n%s", err, output)
	}
}

func TestCompileFilesDeduplicatesRecordShapesAcrossFiles(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	first := filepath.Join(inputDir, "first.gpp")
	second := filepath.Join(inputDir, "second.gpp")

	for filename, source := range map[string]string{
		first:  "func First() record { return record(name: \"A\") }\n",
		second: "func Second() record { return record(name: \"B\") }\n",
	} {
		if err := os.WriteFile(filename, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := CompileFiles([]string{first, second}, outputDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(outputDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		count += strings.Count(string(data), "type __gopp_record_")
	}
	if count != 1 {
		t.Fatalf("expected one generated record type across package files, got %d", count)
	}
}

func TestCompileFilesSupportsExportedCrossPackageRecords(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	people := filepath.Join(inputDir, "people.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(people, []byte(`package demo.people

func GetUser() record {
    return record(Name: "Bob", Age: 42)
}

`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import (
    "fmt"
    "generated/demo/people"
)

func main() {
    user := people.GetUser()
    fmt.Println(user.Name, user.Age)
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{people, main},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "test", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gopp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated cross-package record code did not build: %v\n%s", err, output)
	}
}

func TestCompileFilesGeneratedCodeBuildsWithIntrospection(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
import "fmt"

class Model {
    Id string

    func Describe() string {
        return this.class.name
    }
}

class Employee: Model {
    Name string
}

class Manager: Employee {}

func main() {
    first := Employee("id", "Bob")
    second := Employee("id2", "Alice")
    field := Employee.class.fields[1]
    field.set(first, "Carol")
    fmt.Println(first.class.name, Employee.class.name, first.class == second.class)
    fmt.Println(field.name, field.get(first), field.addr(first) != nil)

    var model Model = Employee("id3", "Dan")
    fmt.Println(model.Describe())

    manager := Manager()
    fmt.Println(manager.Describe())
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gopp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated introspection Go did not run: %v\n%s", err, output)
	}
	expected := "Employee Employee true\nName Carol true\nEmployee\nManager\n"
	if string(output) != expected {
		t.Fatalf("unexpected introspection output:\n%s\nwant:\n%s", output, expected)
	}
}

func TestCompileFilesSupportsCrossPackageClassIntrospection(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	people := filepath.Join(inputDir, "people.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(people, []byte(`package demo.people

class Person {
    Name string
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import (
    "fmt"
    "generated/demo/people"
)

func main() {
    person := people.Person("Bob")
    fmt.Println(people.Person.class.name, person.class.name)
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{people, main},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gopp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated cross-package introspection Go did not run: %v\n%s", err, output)
	}
	if string(output) != "Person Person\n" {
		t.Fatalf("unexpected cross-package introspection output: %s", output)
	}
}

func TestCompileFilesPrefersNativeMethodsOverExtensions(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
import "database/sql"

extend *sql.DB {
    func Ping() bool {
        return true
    }
}

func Use(db *sql.DB) error {
    return db.Ping()
}

func main() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "test", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gopp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native method precedence did not produce buildable Go: %v\n%s", err, output)
	}
}

func TestCompileFilesSupportsMultiTargetImportedExtensions(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
import "database/sql"

extend *sql.DB, *sql.Tx {
    func ExecOne(query string, args ...any) error {
        _, err := this.Exec(query, args...)
        return err
    }
}

func main() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "test", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gopp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("multi-target imported extension did not produce buildable Go: %v\n%s", err, output)
	}
}

func TestCompileFilesSupportsImportedGoPlusExtensions(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	people := filepath.Join(inputDir, "people.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(people, []byte(`package demo.people

class Person {
    Name string
}

extend Person {
    func Greeting() string {
        return "Hello, " + this.Name
    }
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import (
    "fmt"
    "generated/demo/people"
)

func main() {
    person := people.Person("Ada")
    fmt.Println(person.Greeting())
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{people, main}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gopp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("imported Go++ extension did not run: %v\n%s", err, output)
	}
	if string(output) != "Hello, Ada\n" {
		t.Fatalf("unexpected imported extension output: %s", output)
	}
}

func TestCompileFilesSupportsExportedImportedAnnotations(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	model := filepath.Join(inputDir, "model.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(model, []byte(`package demo.model

annotation Public on class
annotation private on class
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import "generated/demo/model"

class Employee @{model.Public} {}

func main() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{model, main}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gopp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("imported annotation did not produce buildable Go: %v\n%s", err, output)
	}
}

func TestCompileFilesRejectsUnexportedImportedAnnotations(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	model := filepath.Join(inputDir, "model.gpp")
	main := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(model, []byte(`package demo.model

annotation private on class
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(`package main

import "generated/demo/model"

class Employee @{model.private} {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	err := CompileFilesWithOptions([]string{model, main}, outputDir, CompileOptions{ModulePath: "generated"})
	if err == nil || !strings.Contains(err.Error(), "undefined annotation model.private") {
		t.Fatalf("expected unexported annotation error, got %v", err)
	}
}
