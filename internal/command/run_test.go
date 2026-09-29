package command

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
