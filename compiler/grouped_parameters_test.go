package compiler

import (
	"strings"
	"testing"
)

func TestGroupedConstructorParametersEmitNamedArguments(t *testing.T) {
	file, err := ParseFile("grouped-constructor.gpp", `class Greeting {
	func init(name, prefix, language string) {}
}`)
	if err != nil {
		t.Fatal(err)
	}

	output, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(output)
	for _, expected := range []string{
		"func (this *Greeting) __gppInit(name string, prefix string, language string)",
		"func (this *Greeting) GppInitialize(name string, prefix string, language string) error",
	} {
		if !strings.Contains(generated, expected) {
			t.Errorf("generated code is missing %q:\n%s", expected, generated)
		}
	}
	if strings.Contains(generated, "__gppInitArg0 name") || strings.Contains(generated, "__gppInitArg1 prefix") {
		t.Errorf("grouped parameter names were emitted as types:\n%s", generated)
	}
}
