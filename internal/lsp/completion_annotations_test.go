package lsp

import (
	"testing"

	"github.com/telgatech/gpp/compiler"
)

func TestCompletionHidesHookSymbols(t *testing.T) {
	item := completionForSymbol(compiler.DocSymbol{
		Kind:        compiler.DocMethod,
		Name:        "BeforeSave",
		Annotations: []string{"@std.Hook"},
	})
	if item.Label != "" {
		t.Fatalf("hook symbol should be omitted from completions, got %#v", item)
	}

	visible := completionForSymbol(compiler.DocSymbol{Kind: compiler.DocMethod, Name: "Save"})
	if visible.Label != "Save" {
		t.Fatalf("unannotated symbol should remain in completions, got %#v", visible)
	}
}
