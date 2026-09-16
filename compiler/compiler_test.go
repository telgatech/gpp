package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileFilesRunsBundledHTTPServer(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
package main

import (
	"context"
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"
	"time"
	http "gpp/http"
)

var events []string

class App : http.Server @{http.IP("127.0.0.1")} {
    func BeforeListen() error {
        events = append(events, "before-listen")
        return nil
    }

    func AfterListen() error {
        events = append(events, "after-listen")
        return nil
    }

    func BeforeRequest(ctx *http.Context) error {
        events = append(events, "before-request")
        ctx.Set("marker", "shared")
        return nil
    }

    func Hello(ctx *http.Context) error @{http.GET("/hello/{name}")} {
        events = append(events, "route")
        return ctx.Text(fmt.Sprintf("%s %s", ctx.Get("marker"), ctx.Param("name")))
    }

    func AfterRequest(ctx *http.Context) error {
        events = append(events, "after-request")
        return nil
    }

    func BeforeShutdown() error {
        events = append(events, "before-shutdown")
        return nil
    }

    func AfterShutdown() error {
        events = append(events, "after-shutdown")
        return nil
    }
}

func main() {
    app := App()
    done := make(chan error, 1)
    go func() { done <- app.Listen() }()

    var response *stdhttp.Response
    var err error
    for response == nil {
        select {
        case err := <-done:
            if err != nil { fmt.Println("SKIP:", err); return }
        default:
        }
        if app.HTTPServer != nil {
            response, err = stdhttp.Get("http://" + app.HTTPServer.Addr + "/hello/Go")
        }
        if response == nil { time.Sleep(time.Millisecond) }
    }
    body, err := io.ReadAll(response.Body)
    if err != nil { panic(err) }
    response.Body.Close()
    fmt.Println(response.StatusCode, string(body))

    if err := app.Shutdown(context.Background()); err != nil { panic(err) }
    if err := <-done; err != nil && err != stdhttp.ErrServerClosed { panic(err) }
    fmt.Println(strings.Join(events, ","))
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated HTTP server did not run: %v\n%s", err, output)
	}
	if strings.HasPrefix(string(output), "SKIP:") {
		t.Skipf("loopback sockets unavailable: %s", output)
	}
	if string(output) != "200 shared Go\nbefore-listen,after-listen,before-request,route,after-request,before-shutdown,after-shutdown\n" {
		t.Fatalf("unexpected HTTP response:\n%s", output)
	}
}

func TestCompileFilesGeneratesDefaultStringAndDump(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "fmt"
)

class Person {
    Name string
    Age int
}

class Employee : Person {
    Id int
}

class Custom {
    Name string

    func String() string {
        return "custom:" + this.Name
    }
}

func main() {
    employee := Employee("Ada", 42, 7)
    fmt.Println(employee)
    fmt.Println(employee.Dump())

	custom := Custom("Secret")
	fmt.Println(custom.String())
	fmt.Println(custom.Dump())
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated default object methods did not run: %v\n%s", err, output)
	}
	expected := "Employee{Name: \"Ada\", Age: 42, Id: 7}\nEmployee {\n    Name: \"Ada\"\n    Age: 42\n    Id: 7\n}\ncustom:Secret\nCustom {\n    Name: \"Secret\"\n}\n"
	if string(output) != expected {
		t.Fatalf("unexpected default object formatting:\n%s", output)
	}
}

func TestCompileFilesSupportsEnums(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "encoding/json"
    "fmt"
)

enum Status int {
    Pending
    Active = 7
    Disabled
}

enum Role string {
    User
    Admin = "administrator"
}

enum (
    Level uint8 {
        Low
        High
    }
)

func main() {
    status := Status.Active
    fmt.Println(status == Status.Active, status.name, status.value)
    autoStatus := Status.From(7)
    fmt.Println(autoStatus.name)
    for _, item := range Status.values {
        fmt.Println(item.name, item.value)
    }

    role, err := Role.From("administrator")
    if err != nil { panic(err) }
    fmt.Println(role.name, role.value)
    _, err = Role.From("unknown")
    fmt.Println(err != nil, Level.High.value)

    encoded, err := json.Marshal(Role.Admin)
    if err != nil { panic(err) }
    var decoded Role
    if err := json.Unmarshal(encoded, &decoded); err != nil { panic(err) }
    _, invalidJSONErr := json.Marshal(Role("unknown"))
    fmt.Println(string(encoded), decoded.name, invalidJSONErr != nil)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated enum program did not run: %v\n%s", err, output)
	}
	expected := "true Active 7\nActive\nPending 0\nActive 7\nDisabled 8\nAdmin administrator\ntrue 1\n\"administrator\" Admin true\n"
	if string(output) != expected {
		t.Fatalf("unexpected enum output:\n%s", output)
	}
}

func TestCompileFilesSupportsImportedEnums(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sharedPath := filepath.Join(inputDir, "shared.gpp")
	mainPath := filepath.Join(inputDir, "main.gpp")
	if err := os.WriteFile(sharedPath, []byte(`
package shared

enum Status string {
    Pending = "pending"
    Active = "active"
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(`
package main

import (
    "fmt"
    "generated/shared"
)

func main() {
    status := shared.Status.Active
    fmt.Println(status.name, status.value, shared.Status.Pending.name)
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions([]string{sharedPath, mainPath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated imported enum program did not run: %v\n%s", err, output)
	}
	if string(output) != "Active active Pending\n" {
		t.Fatalf("unexpected imported enum output: %s", output)
	}
}

func TestCompileFilesSupportsExceptionsAndImplicitErrorPromotion(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "fmt"
    "os"
)

class ValidationError {
    Field string
    Message string

    func Error() string {
        return this.Field + ": " + this.Message
    }
}

func ReadName(path string) string {
    data := os.ReadFile(path)
    return string(data)
}

func Save() error {
    return fmt.Errorf("save failed")
}

func main() {
    try {
        ReadName("/definitely/missing/gpp-file")
    } catch *os.PathError e {
        fmt.Println("path", e.Op)
    } finally {
        fmt.Println("cleanup")
    }

    try {
        Save()
    } catch e {
        fmt.Println(e)
    }

    try {
        throw ValidationError(Field: "email", Message: "invalid")
    } catch ValidationError e {
        fmt.Println(e.Field)
    }

    _, err := os.ReadFile("/definitely/missing/gpp-file")
    fmt.Println(err != nil)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated exception program did not run: %v\n%s", err, output)
	}
	expected := "path open\ncleanup\nsave failed\nemail\ntrue\n"
	if string(output) != expected {
		t.Fatalf("unexpected exception output:\n%s", output)
	}
}

func TestEmitRejectsNonErrorThrow(t *testing.T) {
	file, err := ParseFile("throw.gpp", `
func main() {
    throw "not an error"
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(file); err == nil || !strings.Contains(err.Error(), "cannot throw string") {
		t.Fatalf("expected non-error throw diagnostic, got %v", err)
	}
}

func TestEmitPromotesNativeMethodErrors(t *testing.T) {
	file, err := ParseFile("method-error.gpp", `
import "os"

func Size(file *os.File) int64 {
    info := file.Stat()
    return info.Size()
}
`)
	if err != nil {
		t.Fatal(err)
	}
	output, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "__gppUnwrap(file.Stat())") {
		t.Fatalf("native method error was not promoted:\n%s", output)
	}
}

func TestEmitRejectsImplicitErrorPromotionInGoroutine(t *testing.T) {
	file, err := ParseFile("goroutine-error.gpp", `
import "os"

func main() {
    go os.ReadFile("config.json")
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(file); err == nil || !strings.Contains(err.Error(), "goroutine") {
		t.Fatalf("expected goroutine error diagnostic, got %v", err)
	}
}

func TestCompileFilesPromotesMoreThanThreeNonErrorResults(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "fmt"

func Four() (int, int, int, int, error) {
    return 1, 2, 3, 4, nil
}

func main() {
    a, b, c, d := Four()
    fmt.Println(a + b + c + d)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated multi-result program did not run: %v\n%s", err, output)
	}
	if string(output) != "10\n" {
		t.Fatalf("unexpected multi-result output: %s", output)
	}
}

func TestCompileFilesConvertsThrownErrorsAtGoABIBoundary(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import (
    "fmt"
    "os"
)

func ReadConfig(path string) (string, error) {
    data := os.ReadFile(path)
    return string(data), nil
}

func main() {
    _, err := ReadConfig("missing-config.json")
    fmt.Println(err != nil)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated ABI-boundary program did not run: %v\n%s", err, output)
	}
	if string(output) != "true\n" {
		t.Fatalf("unexpected ABI-boundary output: %s", output)
	}
}

func TestCompileFilesPreservesReturnThroughFinally(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "fmt"

func choose(early bool) int {
    try {
        if early {
            return 7
        }
    } finally {
        fmt.Println("cleanup")
    }
    return 9
}

func chooseCatch() int {
    try {
        throw fmt.Errorf("stop")
    } catch error {
        return 11
    } finally {
        fmt.Println("catch cleanup")
    }
    return 12
}

func namedReturn() (value int) {
    value = 13
    try {
        return
    } finally {
        fmt.Println("named cleanup")
    }
}

func earlyExit(shouldExit bool) {
    try {
        if shouldExit {
            return
        }
    } finally {
        fmt.Println("void cleanup")
    }
    fmt.Println("after void try")
}

func main() {
    fmt.Println(choose(true))
	fmt.Println(choose(false))
	fmt.Println(chooseCatch())
	fmt.Println(namedReturn())
	earlyExit(true)
	earlyExit(false)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated return/finally program did not run: %v\n%s", err, output)
	}
	if string(output) != "cleanup\n7\ncleanup\n9\ncatch cleanup\n11\nnamed cleanup\n13\nvoid cleanup\nvoid cleanup\nafter void try\n" {
		t.Fatalf("unexpected return/finally output: %s", output)
	}
}

func TestEmitRejectsUnreachableCatchAndFinallyControlTransfer(t *testing.T) {
	catchFile, err := ParseFile("catch-order.gpp", `
func main() {
    try {
        panic("ordinary panic")
    } catch error e {
    } catch error e {
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(catchFile); err == nil || !strings.Contains(err.Error(), "unreachable catch") {
		t.Fatalf("expected unreachable catch diagnostic, got %v", err)
	}

	finallyFile, err := ParseFile("finally-transfer.gpp", `
func main() {
    try {
    } finally {
        return
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Emit(finallyFile); err == nil || !strings.Contains(err.Error(), "control transfer from finally") {
		t.Fatalf("expected finally control-transfer diagnostic, got %v", err)
	}
}

func TestCompileFilesGeneratesSerializationMethodsAndTags(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "gpp/encoding"

class User @{encoding.Serializable} {
    Id int @{encoding.Name("id")}
    Password string @{encoding.Ignore}
    Nickname string @{encoding.OmitEmpty}
}

func main() {
    user := User(Id: 1)
    data, _ := user.ToJSON()
    _, _ = User.FromJSON(data)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(outputDir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, expected := range []string{
		"Id               int       `json:\"id\" yaml:\"id\"`",
		"Password         string    `json:\"-\" yaml:\"-\"`",
		"Nickname         string    `json:\",omitempty\" yaml:\",omitempty\"`",
		"func (this *User) ToJSON() ([]byte, error)",
		"func (this *User) ToGOB() ([]byte, error)",
		"func GppStatic_User_FromJSON(data []byte) (User, error)",
		"return encoding.FromJSON[User](data)",
		"func (this User) GobEncode() ([]byte, error)",
		"func (this *User) GobDecode(data []byte) error",
		"func GppStatic_User_FromGOB(data []byte) (User, error)",
		"return encoding.FromGOB[User](data)",
		`{Name: "FromJSON", Parameters:`,
		`{Name: "FromGOB", Parameters:`,
		`FromJSON", Parameters: []GppParameter`,
		`FromGOB", Parameters: []GppParameter`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated serialization code is missing %q:\n%s", expected, text)
		}
	}
}

func TestCompileFilesRejectsIncompatibleSerializationMethod(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "gpp/encoding"

class User @{encoding.Serializable} {
    func ToJSON() string { return "custom" }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"})
	if err == nil || !strings.Contains(err.Error(), "encoding.Serializable requires User.ToJSON") {
		t.Fatalf("expected serialization method conflict, got %v", err)
	}
}

func TestCompileFilesPreservesAnnotationInheritanceMetadata(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `
package main

import "fmt"

annotation (
    Foo on class, field, method
    Bar on class
    Query on parameter
)

class Base @{Foo} {
    Id int @{Foo}

    func Search(q string @{Query}) string @{Foo} { return q }
    func Run() string @{Foo} { return "base" }
}

class Child : Base @{Bar} {
    func Run() string { return "child" }
}

func main() {
    fmt.Println(Child.class.annotations.has(Foo), Child.class.annotations.has(Bar))
    for parent := range Child.class.parents {
        fmt.Println(parent.name, parent.annotations.has(Foo))
    }
    for field := range Child.class.fields {
        fmt.Println(field.name, field.owner.name, field.annotations.has(Foo))
    }
    for method := range Child.class.methods {
        fmt.Println(method.name, method.owner.name, method.annotations.has(Foo))
        for parameter := range method.parameters {
            fmt.Println(parameter.name, parameter.annotations.has(Query))
        }
    }
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("annotation inheritance example did not run: %v\n%s", err, output)
	}
	expected := "false true\nBase true\nId Base true\nRun Child false\nString Child false\nDump Child false\nSearch Base true\nq true\n"
	if string(output) != expected {
		t.Fatalf("unexpected annotation inheritance metadata:\n%s", output)
	}
}

func TestCompileFilesBuildsBundledORMModelBoundary(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	source := `
package main

import (
    "database/sql"
    orm "gpp/orm"
)

class Employee : orm.Model @{orm.Table("employees")} {
    Id int64 @{orm.Column("id"), orm.PK}
}

func useORM(exec orm.SQLExecutor) error {
    db, ok := exec.(*sql.DB)
    if !ok {
        return nil
    }
    employee := Employee()
    return db.Insert(&employee)
}

func main() {}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated ORM model boundary did not build: %v\n%s", err, output)
	}
}

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

func TestCompileFilesWithOptionsCleansGeneratedOutput(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(sourcePath, []byte("func main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "stale.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "stale"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "stale", "nested.go"), []byte("package stale\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{sourcePath},
		outputDir,
		CompileOptions{ModulePath: "generated", CleanOutput: true},
	); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "stale.go")); !os.IsNotExist(err) {
		t.Fatalf("stale generated file still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "stale")); !os.IsNotExist(err) {
		t.Fatalf("stale generated directory still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "go.mod")); err != nil {
		t.Fatalf("generated module was not created: %v", err)
	}
}

func TestCompileFilesEmitsPreludeOncePerPackage(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	first := filepath.Join(inputDir, "first.gpp")
	second := filepath.Join(inputDir, "second.gpp")

	if err := os.WriteFile(first, []byte(`package demo

func First() {}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte(`package demo

func Second() bool {
    values := []int{1, 2}
    return values.Any(func(value int) bool { return value == 2 })
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{first, second},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(outputDir, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(outputDir, "demo", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		count += strings.Count(string(data), "func GppPreludeExt___T_Any_")
	}
	if count != 1 {
		t.Fatalf("expected one generated prelude helper for the package, got %d", count)
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
		"func SpeakFor(person web.GppPerson) string",
		"func MakePerson() web.GppPerson",
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
	if !strings.Contains(string(generated), "Add__gpp_1(1)") ||
		!strings.Contains(string(generated), "Add__gpp_2(1, 2)") {
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
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
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
		count += strings.Count(string(data), "type __gpp_record_")
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
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
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
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
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
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
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
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
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
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
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
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("imported Go++ extension did not run: %v\n%s", err, output)
	}
	if string(output) != "Hello, Ada\n" {
		t.Fatalf("unexpected imported extension output: %s", output)
	}
}

func TestCompileFilesSupportsImportedStaticMethods(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	modelFile := filepath.Join(inputDir, "model.gpp")
	mainFile := filepath.Join(inputDir, "main.gpp")

	if err := os.WriteFile(modelFile, []byte(`package demo.model

class User {
    Name string

    static func Guest() User {
        return User(Name: "Guest")
    }
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainFile, []byte(`package main

import (
    "fmt"
    "generated/demo/model"
)

func main() {
    user := model.User.Guest()
    fmt.Println(user.Name)
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CompileFilesWithOptions(
		[]string{modelFile, mainFile},
		outputDir,
		CompileOptions{ModulePath: "generated"},
	); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("imported static method did not run: %v\n%s", err, output)
	}
	if string(output) != "Guest\n" {
		t.Fatalf("unexpected imported static method output: %s", output)
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
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
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
