package command

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/telgatech/gpp/compiler"
)

func TestInitCreatesFormattedStarter(t *testing.T) {
	directory := t.TempDir()
	gitConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(gitConfig, []byte("[user]\n\tname = Go++ Test\n\temail = gpp-test@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	if code := Run([]string{"init", directory}); code != 0 {
		t.Fatalf("gpp init failed with exit code %d", code)
	}

	path := filepath.Join(directory, "main.gpp")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := compiler.FormatSourceFile(path, source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, formatted) {
		t.Fatalf("gpp init created a starter that is not formatted:\n%s", source)
	}

	goMod, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil || !bytes.Contains(goMod, []byte("module example.com/")) {
		t.Fatalf("gpp init did not create a Go module: %q, %v", goMod, err)
	}
	commit, err := exec.Command("git", "-C", directory, "log", "-1", "--format=%s").Output()
	if err != nil {
		t.Fatalf("gpp init did not create an initial Git commit: %v", err)
	}
	if strings.TrimSpace(string(commit)) != "init" {
		t.Fatalf("unexpected initial commit message %q", strings.TrimSpace(string(commit)))
	}
	status, err := exec.Command("git", "-C", directory, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes.TrimSpace(status)) != 0 {
		t.Fatalf("gpp init left generated files uncommitted: %s", status)
	}
	gitignore, err := os.ReadFile(filepath.Join(directory, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{".gpp/", "/main", "/main.exe"} {
		if !bytes.Contains(gitignore, []byte(entry)) {
			t.Errorf("gpp init did not ignore generated path %q", entry)
		}
	}
}

func TestInitDoesNotCreateNestedGitRepository(t *testing.T) {
	root := t.TempDir()
	gitConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(gitConfig, []byte("[user]\n\tname = Go++ Test\n\temail = gpp-test@example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	if output, err := exec.Command("git", "-C", root, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("could not create test repository: %v\n%s", err, output)
	}
	project := filepath.Join(root, "app")
	if code := Run([]string{"init", project}); code != 0 {
		t.Fatalf("gpp init failed inside an existing repository with exit code %d", code)
	}
	if _, err := os.Stat(filepath.Join(project, ".git")); !os.IsNotExist(err) {
		t.Fatalf("gpp init created a nested Git repository: %v", err)
	}
	if _, err := exec.Command("git", "-C", root, "rev-parse", "--verify", "HEAD").Output(); err == nil {
		t.Fatal("gpp init unexpectedly committed to the existing repository")
	}
}

func TestRunGoProgramStreamsOutputBeforeProcessExit(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module streamtest\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := `package main
import (
	"fmt"
	"time"
)
func main() {
	fmt.Println("server ready")
	time.Sleep(2 * time.Second)
}`
	if err := os.WriteFile(filepath.Join(directory, "main.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStdout := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = previousStdout }()

	done := make(chan error, 1)
	go func() {
		done <- runGoProgram(directory, nil)
		_ = writer.Close()
	}()
	defer reader.Close()

	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(reader).ReadString('\n')
		lines <- line
	}()
	select {
	case line := <-lines:
		if line != "server ready\n" {
			t.Fatalf("unexpected live program output: %q", line)
		}
	case err := <-done:
		t.Fatalf("program exited before producing live output: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for live program output")
	}

	select {
	case err := <-done:
		t.Fatalf("program output was held until process exit: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := <-done; err != nil {
		t.Fatalf("program failed: %v", err)
	}
}

func TestCompileSubcommandUsesArgumentsAfterCommandName(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "main.gpp")
	if err := os.WriteFile(source, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "generated")
	if code := Run([]string{"compile", source, "-output", output}); code != 0 {
		t.Fatalf("gpp compile failed with exit code %d", code)
	}
	if _, err := os.Stat(filepath.Join(output, "main.go")); err != nil {
		t.Fatalf("gpp compile did not emit Go source: %v", err)
	}
}

func TestDefaultGeneratedWorkspaceUsesSystemCache(t *testing.T) {
	project := t.TempDir()
	cache := t.TempDir()
	t.Setenv("GPP_CACHE", cache)
	source := filepath.Join(project, "main.gpp")
	if err := os.WriteFile(source, []byte("func main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	options := compileFlags{}
	if err := setDefaultOutput(&options, []string{source}); err != nil {
		t.Fatal(err)
	}
	if !isWithin(cache, options.output) {
		t.Fatalf("default output %q is outside GPP_CACHE %q", options.output, cache)
	}
	if isWithin(project, options.output) {
		t.Fatalf("default output %q was created inside the project %q", options.output, project)
	}
	second := compileFlags{}
	if err := setDefaultOutput(&second, []string{source}); err != nil {
		t.Fatal(err)
	}
	if second.output != options.output {
		t.Fatalf("workspace is not stable: got %q and %q", options.output, second.output)
	}
}

func TestDefaultGeneratedWorkspacesDifferByProject(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("GPP_CACHE", cache)
	firstProject, secondProject := t.TempDir(), t.TempDir()
	first, err := defaultOutputForRoot(firstProject)
	if err != nil {
		t.Fatal(err)
	}
	second, err := defaultOutputForRoot(secondProject)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("different projects share generated workspace %q", first)
	}
}

func TestDefaultBuildBinaryNameUsesTargetPlatformExtension(t *testing.T) {
	sources := []string{filepath.Join(t.TempDir(), "hello.gpp")}
	if got := defaultBuildBinaryName(sources, "windows"); got != "hello.exe" {
		t.Fatalf("Windows default binary name = %q, want hello.exe", got)
	}
	if got := defaultBuildBinaryName(sources, "linux"); got != "hello" {
		t.Fatalf("Linux default binary name = %q, want hello", got)
	}
	if got := defaultBuildBinaryName([]string{filepath.Join(t.TempDir(), "hello.exe.gpp")}, "windows"); got != "hello.exe" {
		t.Fatalf("Windows source already ending in .exe got %q", got)
	}
}

func TestDefaultBuildBinaryNameUsesProjectDirectory(t *testing.T) {
	project := filepath.Join(t.TempDir(), "my-service")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(project, "main.gpp")
	if err := os.WriteFile(source, []byte("func main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := defaultBuildBinaryNameForInputs([]string{project}, []string{source}, "linux"); got != "my-service" {
		t.Fatalf("project directory default binary name = %q, want my-service", got)
	}
	if got := defaultBuildBinaryNameForInputs([]string{source}, []string{source}, "linux"); got != "main" {
		t.Fatalf("single source default binary name = %q, want main", got)
	}
}
