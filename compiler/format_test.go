package compiler

import (
	"os"
	"path/filepath"
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
		"\tvalue := \"literal { not a block }\"\n" +
		"\traw := `raw {\n" +
		"  intentionally indented }`\n" +
		"\tif true {\n" +
		"\t\t// Must remain inside the block.\n" +
		"\t\treturn\n" +
		"\t}\n" +
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

func TestFormatSourcePreservesUnderscoreVisibilityNames(t *testing.T) {
	source := "class Base {\n_state string\nfunc _reset(){ this._state = \"\" }\n}\nclass Child : Base {}\n"
	formatted := FormatSource(source)
	for _, member := range []string{"_state string", "func _reset()", "this._state"} {
		if !strings.Contains(formatted, member) {
			t.Errorf("formatter changed underscore-prefixed member %q:\n%s", member, formatted)
		}
	}
	if again := FormatSource(formatted); again != formatted {
		t.Fatalf("formatter is not idempotent for underscore-prefixed members:\nfirst:\n%s\nsecond:\n%s", formatted, again)
	}
}

func TestFormatSourceFileRejectsInvalidSource(t *testing.T) {
	if _, err := FormatSourceFile("broken.gpp", []byte("class {\n")); err == nil {
		t.Fatal("expected invalid GPP source to be rejected")
	}
}

func TestFormatSourceNormalizesLineEndings(t *testing.T) {
	formatted := FormatSource("func main() {\r\n\treturn\r\n}\r\n\r\n")
	if formatted != "func main() {\n\treturn\n}\n" {
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

func TestFormatSourceFormatsNestedExceptionHandlers(t *testing.T) {
	source := `import (
"errors"
"fmt"
)
func main(){
try{
if true{
work()
}
} catch ValidationError,  PermissionError e{
fmt.Println(e)
} catch e{
fmt.Println(errors.Unwrap(e))
} finally{
fmt.Println("finished")
}
}
`
	want := `import (
    "errors"
    "fmt"
)
func main() {
    try {
        if true {
            work()
        }
    } catch ValidationError, PermissionError e {
        fmt.Println(e)
    } catch e {
        fmt.Println(errors.Unwrap(e))
    } finally {
        fmt.Println("finished")
    }
}
`
	got := FormatSource(source)
	if got != want {
		t.Fatalf("exception blocks or grouped imports were not formatted:\n%s", got)
	}
	if again := FormatSource(got); again != got {
		t.Fatalf("exception formatting is not idempotent:\nfirst:\n%s\nsecond:\n%s", got, again)
	}
}

func TestFormatSourceIndentsMultilineCallsAndNestedClosers(t *testing.T) {
	source := `func main(){
result:=call(
first,
second(
third,
),
)
use(result)
if ready{
run()
}else if waiting{
wait()
}
}
`
	want := `func main() {
	result := call(
		first,
		second(
			third,
		),
	)
	use(result)
	if ready {
		run()
	} else if waiting {
		wait()
	}
}
`
	got := FormatSource(source)
	if got != want {
		t.Fatalf("multiline nesting was not formatted:\n%s", got)
	}
}

func TestFormatSourceIsIdempotentAcrossGPPSources(t *testing.T) {
	for _, root := range []string{filepath.Join("..", "compiler"), filepath.Join("..", "examples")} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".gpp") && !strings.HasSuffix(path, ".gpp.tpl") {
				return nil
			}
			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			formatted, err := FormatSourceFile(path, source)
			if err != nil {
				return err
			}
			formattedAgain, err := FormatSourceFile(path, formatted)
			if err != nil {
				return err
			}
			if string(formattedAgain) != string(formatted) {
				t.Errorf("formatter is not idempotent for %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFormatSourceAlignsSwitchAndSelectClauses(t *testing.T) {
	source := `func main(){
switch value{
case 1:
first()
case 2, 3:
second()
default:
fallback()
}
select{
case value := <-values:
receive(value)
default:
wait()
}
}
`
	want := `func main() {
	switch value {
	case 1:
		first()
	case 2, 3:
		second()
	default:
		fallback()
	}
	select {
	case value := <-values:
		receive(value)
	default:
		wait()
	}
}
`
	got := FormatSource(source)
	if got != want {
		t.Fatalf("switch/select clauses were not formatted:\n%s", got)
	}
}

func TestFormatSourceSpacesGoTypeBlockBraces(t *testing.T) {
	source := `type Person struct{
Name string
}
type Speaker interface{
Speak() string
}
`
	want := `type Person struct {
	Name string
}
type Speaker interface {
	Speak() string
}
`
	if got := FormatSource(source); got != want {
		t.Fatalf("Go type block braces were not formatted:\n%s", got)
	}
}

func TestFormatSourceSpacesCommasAndInlineFunctionBlocks(t *testing.T) {
	source := `func some() (int,int,int) {return 0,0,0}
func quux() struct {return struct(works: true, a:1, b:2)}
`
	want := `func some() (int, int, int) { return 0, 0, 0 }
func quux() struct { return struct(works: true, a: 1, b: 2) }
`
	if got := FormatSource(source); got != want {
		t.Fatalf("comma and inline block spacing was not normalized:\n%s", got)
	}
}

func TestFormatSourceFormatsInferredMapLiterals(t *testing.T) {
	source := `func main(){
values:=map(coffee:2.5, tea:1.75)
headers:=map("X-Request-ID":"req-42")
}`
	want := `func main() {
    values := map(coffee: 2.5, tea: 1.75)
    headers := map("X-Request-ID": "req-42")
}
`
	if got := FormatSource(source); got != want {
		t.Fatalf("inferred map literals were formatted incorrectly:\n%s", got)
	}
}

func TestFormatSourceCollapsesExcessSpacingAroundExpressions(t *testing.T) {
	source := `func Summary() string {

return             "{{this.Named.Name}} is a {{this.Role}}"

}
`
	want := `func Summary() string {
	return "{{this.Named.Name}} is a {{this.Role}}"
}
`
	if got := FormatSource(source); got != want {
		t.Fatalf("expression spacing was not normalized:\n%s", got)
	}
}

func TestFormatSourceRemovesSpaceBeforeClosingParentheses(t *testing.T) {
	source := `func main(){
people.Each(person => fmt.Printf("  %d %s\n", person.Id, person.Name) )
outer(inner() )
println("keep this ) ")
}`
	want := `func main() {
    people.Each(person => fmt.Printf("  %d %s\n", person.Id, person.Name))
    outer(inner())
    println("keep this ) ")
}
`
	if got := FormatSource(source); got != want {
		t.Fatalf("space before a closing parenthesis was not removed safely:\n%s", got)
	}
}

func TestFormatSourceTrimsBlockEdgeBlankLinesAndPreservesOpaqueWhitespace(t *testing.T) {
	source := "func main(){\n\nraw := `first\n  \n  \nlast`\ncall()\n\n}\n" +
		"template Page() {\n<div>\n  \n\n\n</div>\n}\n"
	want := "func main() {\n    raw := `first\n  \n  \nlast`\n    call()\n}\n" +
		"template Page() {\n<div>\n  \n\n\n</div>\n}\n"
	if got := FormatSource(source); got != want {
		t.Fatalf("block edge blank lines or opaque whitespace were mishandled:\n%s", got)
	}
}

func TestFormatSourceCollapsesRepeatedBlankLinesLikeGoFmt(t *testing.T) {
	source := "func main() {\n\tfirst()\n\n\n\n\tsecond()\n\n}\n\n\nfunc next() {}\n"
	want := "func main() {\n\tfirst()\n\n\tsecond()\n}\n\nfunc next() {}\n"
	if got := FormatSource(source); got != want {
		t.Fatalf("repeated blank lines were not collapsed:\n%s", got)
	}
}
