package compiler

import (
	"strings"
	"testing"
)

func TestTransformOverloadsHandlesTokenBackedNestedFunctionCalls(t *testing.T) {
	source := `
func Register(ctx *Context) error {
	return Accept(func(ctx *Context) error {
		if failed() { return ctx.Text(400, "failed") }
		return ctx.Text("ok")
	})
}
`
	overlows := overloadContext{
		ClassMethods: map[string]map[string]map[int]string{
			"Context": {
				"Text": {1: "Text__gpp_1", 2: "Text__gpp_2"},
			},
		},
		ClassMethodTypes: map[string]map[string]map[string]string{
			"Context": {
				"Text": {"string": "Text__gpp_1", "int,string": "Text__gpp_2"},
			},
		},
	}
	transformed, err := transformOverloads(source, overlows)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(transformed, "Text__gpp_1") != 1 || strings.Count(transformed, "Text__gpp_2") != 1 {
		t.Fatalf("nested overload calls were not lowered exactly once:\n%s", transformed)
	}
}
