package command

import (
	"strings"
	"testing"

	"github.com/telgatech/gpp/compiler"
)

func TestBackendDiagnosticsKeepContextAndExposeStructuredPosition(t *testing.T) {
	err := newBackendDiagnosticError("# example.com/app\nfoo.go:4:2: declared and not used: x")
	if got, want := compiler.FormatError(err), "# example.com/app\nfoo.go:4:2: declared and not used: x"; got != want {
		t.Fatalf("formatted backend diagnostic = %q, want %q", got, want)
	}
	position, ok := compiler.PositionOfDiagnostic(err)
	if !ok {
		t.Fatal("backend error did not expose its source diagnostic")
	}
	if position.File != "foo.go" || position.Span.Line != 4 || position.Span.Column != 2 || position.Message != "declared and not used: x" {
		t.Fatalf("unexpected backend diagnostic position: %#v", position)
	}
}

func TestBackendDiagnosticWithMissingColumnKeepsKnownLocation(t *testing.T) {
	err := newBackendDiagnosticError("foo.go:4: compiler omitted column")
	got := compiler.FormatError(err)
	if got != "foo.go:4: compiler omitted column" {
		t.Fatalf("known backend location was not preserved: %q", got)
	}
	position, ok := compiler.PositionOfDiagnostic(err)
	if !ok || position.File != "foo.go" || position.Span.Line != 4 || position.Span.Column != 0 {
		t.Fatalf("backend line-only position was not exposed: %#v, %v", position, ok)
	}
}

func TestBackendDiagnosticsCollapseDuplicateGeneratedReports(t *testing.T) {
	err := newBackendDiagnosticError(strings.Join([]string{
		"dump.gpp:66: undefined: http.Contextx",
		"dump.gpp:66: undefined: http.Contextx",
		"dump.gpp:67: undefined: http.Contextx",
	}, "\n"))
	if got, want := compiler.FormatError(err), "dump.gpp:66: undefined: http.Contextx\ndump.gpp:67: undefined: http.Contextx"; got != want {
		t.Fatalf("formatted backend diagnostics = %q, want %q", got, want)
	}
}
