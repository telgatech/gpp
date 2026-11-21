package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypedSliceAndArrayLiteralsRun(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	source := `package main
import (
	"fmt"
	"time"
)

class Box[T any] {
	Value T
	func init(value T) { this.Value = value }
}

struct User { Name string }

func main() {
	numbers := int[1, 2, 3]
	empty := int[]
	matrix := []int[[1, 2], [3, 4]]
	grid := [2][2]int[[1, 2], [3, 4]]
	combined := [3]int[11, 12, 13][1] + int[4, 5][0]
	times := time.Time[time.Time{}]
	boxes := Box[int][(value: 5), (6)]
	boxArray := [2]Box[int][(7), (8)]
	emptyBoxes := Box[int][]
	users := User[(Name: "Ada"), (Name: "Lin")]
	selected := numbers[1]
	scores := int[3, 1, 2]
	withoutTwo := scores.DeleteBy(value => value == 2)
	found, foundTwo := scores.Find(value => value == 2)
	replaced := scores.Map(value => value == 2 ? 20 : value)
	goScores := []int{3, 1, 2}
	goFiltered := goScores.Filter(value => value > 1)
	int := []int{9}
	shadowed := int[0]
	fmt.Println(numbers, empty, matrix, grid, combined, len(times))
	fmt.Println(withoutTwo, found, foundTwo, replaced)
	fmt.Println(goFiltered)
	fmt.Println(boxes[0].Value, boxes[1].Value, len(emptyBoxes), boxArray[0].Value, boxArray[1].Value, users[0].Name, users[1].Name, selected, shadowed)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{
		ModulePath: "generated",
	}); err != nil {
		generated, _ := os.ReadFile(filepath.Join(outputDir, "main.go"))
		t.Fatalf("compile typed collection literals: %v\n%s", err, generated)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run typed collection literal fixture: %v\n%s", err, output)
	}
	want := "[1 2 3] [] [[1 2] [3 4]] [[1 2] [3 4]] 16 1\n[3 1] 2 true [3 1 20]\n[3 2]\n5 6 0 7 8 Ada Lin 2 9\n"
	if string(output) != want {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", output, want)
	}
}

func TestTypedCollectionLiteralFormattingPreservesSyntax(t *testing.T) {
	source := "func values() {\nitems:=int[1,2,3]\nmatrix:=[]int[[1,2],[3,4]]\n}\n"
	formatted := FormatSource(source)
	for _, syntax := range []string{"items := int[1, 2, 3]", "matrix := []int[[1, 2], [3, 4]]"} {
		if !strings.Contains(formatted, syntax) {
			t.Errorf("formatter did not preserve typed literal syntax %q:\n%s", syntax, formatted)
		}
	}
	if again := FormatSource(formatted); again != formatted {
		t.Fatalf("formatter is not idempotent:\nfirst:\n%s\nsecond:\n%s", formatted, again)
	}
}
