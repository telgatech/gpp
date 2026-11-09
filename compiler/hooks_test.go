package compiler

import (
	"strings"
	"testing"
)

func TestHookCallsStayWithOriginalDeclaringClass(t *testing.T) {
	file, err := ParseFile("hooks.gpp", `
class Model {
	func BeforeSave() error @{std.Hook} { return nil }
	func Run() error { return this.BeforeSave() }
}

class Child : Model {
	func Run() error { return this.BeforeSave() }
}

class Override : Model {
	func BeforeSave() error @{std.Hook} { return nil }
	func Run() error { return this.BeforeSave() }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	classes := map[string]*ClassDecl{}
	for _, declaration := range file.Decls {
		if class, ok := declaration.(*ClassDecl); ok {
			classes[class.Name] = class
		}
	}

	model := classes["Model"]
	if err := validateHookCallsFromMethod(model, &model.Methods[1], classes); err != nil {
		t.Fatalf("declaring class should be allowed to invoke its hook: %v", err)
	}

	for _, name := range []string{"Child", "Override"} {
		t.Run(name, func(t *testing.T) {
			class := classes[name]
			method := class.Methods[len(class.Methods)-1]
			err := validateHookCallsFromMethod(class, &method, classes)
			if err == nil || !strings.Contains(err.Error(), "may only be called by its declaring class Model") {
				t.Fatalf("expected inherited hook call to be rejected, got %v", err)
			}
		})
	}
}
