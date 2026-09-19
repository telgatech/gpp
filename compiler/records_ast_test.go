package compiler

import (
	"strings"
	"testing"
)

func TestCollectRecordValueTypesUsesBodyAST(t *testing.T) {
	typeName := "__gpp_record_deadbeef"
	source := `func Build(input ` + typeName + `) {
    current := input
    if current != nil {
        nested := current
        _ = nested
    }
}`

	valueTypes, handled := collectRecordValueTypesAST(source, constructorContext{})
	if !handled {
		t.Fatal("expected record value collection to use the structured body AST")
	}
	for _, name := range []string{"input", "current", "nested"} {
		if valueTypes[name] != typeName {
			t.Fatalf("expected %s to have type %s, got %q", name, typeName, valueTypes[name])
		}
	}
}

func TestRewriteRecordCollectionLiteralsUsesBodyAST(t *testing.T) {
	source := `func Users() []record {
    return []record{__gpp_record_deadbeef{Name: "A"}}
}`
	transformed, handled, err := rewriteRecordCollectionLiteralsAST(source, constructorContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(transformed, "[]__gpp_record_deadbeef{") {
		t.Fatalf("expected structured collection type lowering, handled=%v output=%s", handled, transformed)
	}
}
