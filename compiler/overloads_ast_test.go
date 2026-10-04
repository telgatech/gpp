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
	overloads := overloadContext{
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
	transformed, err := transformOverloads(source, overloads)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(transformed, "Text__gpp_1") != 1 || strings.Count(transformed, "Text__gpp_2") != 1 {
		t.Fatalf("nested overload calls were not lowered exactly once:\n%s", transformed)
	}
}

func TestTransformOverloadsResolvesImportedClassFieldsInCallbacks(t *testing.T) {
	source := `
func Register(ctx *http.Context, data []byte) error {
	return Accept(func(ctx *http.Context) error {
		_, err := ctx.Conn.Read(data)
		if err != nil { return err }
		_, err = ctx.Conn.Write(data)
		if err != nil { return err }
		_, err := rand.Read(data)
		if err != nil { return err }
		_, err = ctx.Response.Write(data)
		return err
	})
}
`
	overloads := overloadContext{
		ClassFieldTypes: map[string]map[string]string{
			"http.Context": {"Conn": "*websocket.Conn", "Response": "stdhttp.ResponseWriter"},
		},
	}
	transformed, err := transformOverloads(source, overloads)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"ctx.Conn.Read(data)", "ctx.Conn.Write(data)", "rand.Read(data)", "ctx.Response.Write(data)"} {
		if !strings.Contains(transformed, expected) {
			t.Errorf("transformed code is missing %q:\n%s", expected, transformed)
		}
	}
}
