package compiler

import (
	"strings"
	"testing"
)

func TestFormatSourcePreservesCommentsAndOpaqueLiterals(t *testing.T) {
	source := "// don't \"fix\"   spacing in my prose\n" +
		"func main(){\n" +
		"value := \"literal { not a block }\"\n" +
		"raw := `raw {\n" +
		"  intentionally indented }`\n" +
		"if true{\n" +
		"// Must remain inside the block.\n" +
		"return\n" +
		"}\n" +
		"}\n"

	formatted := FormatSource(source)
	expected := "// don't \"fix\"   spacing in my prose\n" +
		"func main() {\n" +
		"    value := \"literal { not a block }\"\n" +
		"    raw := `raw {\n" +
		"  intentionally indented }`\n" +
		"    if true {\n" +
		"        // Must remain inside the block.\n" +
		"        return\n" +
		"    }\n" +
		"}\n"
	if formatted != expected {
		t.Fatalf("unexpected formatted source:\n%s", formatted)
	}
	if again := FormatSource(formatted); again != formatted {
		t.Fatalf("formatter is not idempotent:\nfirst:\n%s\nsecond:\n%s", formatted, again)
	}
	for _, comment := range []string{"don't \"fix\"   spacing in my prose", "Must remain inside the block."} {
		if strings.Count(formatted, comment) != 1 {
			t.Fatalf("comment %q was not preserved exactly", comment)
		}
	}
}

func TestFormatSourceFileRejectsInvalidSource(t *testing.T) {
	if _, err := FormatSourceFile("broken.gpp", []byte("class {\n")); err == nil {
		t.Fatal("expected invalid Go++ source to be rejected")
	}
}

func TestFormatSourceNormalizesLineEndings(t *testing.T) {
	formatted := FormatSource("func main() {\r\n\treturn\r\n}\r\n\r\n")
	if formatted != "func main() {\n    return\n}\n" {
		t.Fatalf("unexpected normalized source: %q", formatted)
	}
}

func TestFormatSourcePreservesTemplateBodyWhitespace(t *testing.T) {
	source := "template Page() {\n" +
		"  <div>\n" +
		"    {{.Name}}\n" +
		"  </div>\n" +
		"}\n" +
		"func main(){\n}\n"
	formatted := FormatSource(source)
	expected := "template Page() {\n" +
		"  <div>\n" +
		"    {{.Name}}\n" +
		"  </div>\n" +
		"}\n" +
		"func main() {\n}\n"
	if formatted != expected {
		t.Fatalf("template body was rewritten:\n%s", formatted)
	}
}
