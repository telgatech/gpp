package command

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func captureFmtOutput(t *testing.T, run func() int) (int, string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	code := run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return code, string(output)
}

func TestRunFmtCheckStdoutAndAtomicRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.gpp")
	original := "func main(){\nprintln()\n}\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	code, output := captureFmtOutput(t, func() int {
		return runFmt([]string{"--check", path})
	})
	if code != 1 || output == "" {
		t.Fatalf("--check did not report an unformatted file: code=%d output=%q", code, output)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != original {
		t.Fatal("--check modified the source file")
	}

	if code := runFmt([]string{path}); code != 0 {
		t.Fatalf("in-place formatting failed with code %d", code)
	}
	formatted := "func main() {\n    println()\n}\n"
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != formatted {
		t.Fatalf("unexpected in-place formatting: %q", contents)
	}

	code, output = captureFmtOutput(t, func() int {
		return runFmt([]string{"--check", path})
	})
	if code != 0 || output != "" {
		t.Fatalf("formatted --check was not silent and successful: code=%d output=%q", code, output)
	}

	code, output = captureFmtOutput(t, func() int {
		return runFmt([]string{"--stdout", path})
	})
	if code != 0 || output != formatted {
		t.Fatalf("--stdout returned unexpected output: code=%d output=%q", code, output)
	}
}

func TestRunFmtFormatsExceptionHandlers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exceptions.gpp")
	original := `package main
func main(){

try{

work()
} catch ValidationError,  PermissionError e{
handle(e)
} catch e{
handle(e)
} finally{
cleanup()
}

}
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	code, output := captureFmtOutput(t, func() int {
		return runFmt([]string{"--stdout", path})
	})
	want := `package main
func main() {
    try {
        work()
    } catch ValidationError, PermissionError e {
        handle(e)
    } catch e {
        handle(e)
    } finally {
        cleanup()
    }
}
`
	if code != 0 || output != want {
		t.Fatalf("fmt did not format exception handlers: code=%d\n%s", code, output)
	}
}
