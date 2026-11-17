package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCompileFilesArithmeticBitwiseLogicalAndComparisonOperators(t *testing.T) {
	output := compileAndRunOperatorFixture(t, `package main

import "fmt"

func main() {
	value := 18
	value += 6
	value -= 2
	value *= 2
	value /= 4
	value %= 5
	value++
	value--
	if value != 1 {
		panic("arithmetic or increment operators failed")
	}

	bits := 0b1100
	bits |= 0b0011
	bits &= 0b1110
	bits ^= 0b0100
	bits <<= 1
	bits >>= 1
	bits &^= 0b0010
	if bits != 0b1000 {
		panic("bitwise, shift, or bit-clear assignment failed")
	}

	combined := (0b1010 | 0b0101) & (0b1111 ^ 0b0011)
	shifted := (1 << 4) >> 2
	if combined != 0b1100 || shifted != 4 || (0b1111 &^ 0b0101) != 0b1010 {
		panic("binary bitwise or shift operators failed")
	}

	valid := +3 == 3 && -3 < 0 && 2+3 == 5 && 9-4 == 5 && 3*4 == 12 && 12/3 == 4 && 13%5 == 3
	valid = valid && 1 < 2 && 2 <= 2 && 3 > 2 && 3 >= 3 && 4 != 5 && 5 == 5
	valid = valid && (true || false) && (true && !false)
	if !valid {
		panic("unary, arithmetic, logical, or comparison operator failed")
	}

	fmt.Println(value, bits, combined, shifted, valid)
}
`)
	if output != "1 8 12 4 true\n" {
		t.Fatalf("unexpected operator results: %q", output)
	}
}

func TestCompileFilesLogicalOrAssignmentIsLazyForBooleansAndPointers(t *testing.T) {
	output := compileAndRunOperatorFixture(t, `package main

import "fmt"

var boolCalls int
var pointerCalls int

func boolValue(value bool) bool {
	boolCalls++
	return value
}

func pointerValue() *int {
	pointerCalls++
	value := 7
	return &value
}

func main() {
	var enabled bool
	enabled ||= boolValue(true)
	enabled ||= boolValue(false)
	status := enabled ? "enabled" : "disabled"

	var pointer *int
	pointer ||= pointerValue()
	pointer ||= pointerValue()

	fmt.Println(enabled, status, *pointer, boolCalls, pointerCalls)
}
`)
	if output != "true enabled 7 1 1\n" {
		t.Fatalf("||= should assign only when false or nil, got %q", output)
	}
}

func TestCompileFilesTernaryAndLogicalOrAssignmentAcrossFunctions(t *testing.T) {
	output := compileAndRunOperatorFixture(t, `package main

import "fmt"

var defaultCalls int
var trueBranchCalls int
var falseBranchCalls int

func defaultEnabled() bool {
	defaultCalls++
	return true
}

func enabledLabel(enabled bool) string {
	enabled ||= defaultEnabled()
	return enabled ? "on" : "off"
}

func nestedLabel(primary, secondary bool) string {
	return primary ? "primary" : secondary ? "secondary" : "none"
}

func labelThenDefault(enabled bool) string {
	label := enabled ? "on" : "off"
	enabled ||= defaultEnabled()
	return label + ":" + (enabled ? "on" : "off")
}

func trueBranch() string {
	trueBranchCalls++
	return "true branch"
}

func falseBranch() string {
	falseBranchCalls++
	return "false branch"
}

class Worker {
	Ready bool

	func Status() string {
		this.Ready ||= defaultEnabled()
		status := this.Ready ? "ready" : "stopped"
		return status
	}
}

func main() {
	first := enabledLabel(false)
	second := enabledLabel(true)
	secondary := nestedLabel(false, true)
	none := nestedLabel(false, false)
	reordered := labelThenDefault(false)
	selected := true ? trueBranch() : falseBranch()
	unselected := false ? trueBranch() : falseBranch()

	worker := Worker()
	firstStatus := worker.Status()
	secondStatus := worker.Status()

	fmt.Println(first, second, secondary, none, reordered)
	fmt.Println(selected, unselected, trueBranchCalls, falseBranchCalls)
	fmt.Println(firstStatus, secondStatus, defaultCalls)
}
`)
	if output != "on on secondary none off:on\ntrue branch false branch 1 1\nready ready 3\n" {
		t.Fatalf("unexpected ternary/operator results across functions: %q", output)
	}
}

func TestCompileFilesErrorFallbackOnlyFallsBackOnErrors(t *testing.T) {
	output := compileAndRunOperatorFixture(t, `package main

import "fmt"

var fallbackCalls int
var nilFallbackCalls int

func number(value int) (int, error) {
	if value < 0 {
		return 0, fmt.Errorf("negative")
	}
	return value, nil
}

func fallback() int {
	fallbackCalls++
	return 42
}

func nilValue() *int { return nil }

func nilFallback() *int {
	nilFallbackCalls++
	value := 7
	return &value
}

func dimensions(fail bool) (int, int, error) {
	if fail {
		return 0, 0, fmt.Errorf("unavailable")
	}
	return 640, 480, nil
}

func cachedDimensions() (int, int, error) {
	return 320, 200, nil
}

func main() {
	good := number(5) ?? fallback()
	bad := number(-1) ?? fallback()
	pointer := nilValue() ?? nilFallback()
	x, y := dimensions(true) ?? 0, 1
	cachedX, cachedY := dimensions(true) ?? cachedDimensions() ?? 0, 1
	fmt.Println(good, bad, fallbackCalls)
	fmt.Println(pointer == nil, nilFallbackCalls)
	fmt.Println(x, y, cachedX, cachedY)
}
`)
	if output != "5 42 1\ntrue 0\n0 1 320 200\n" {
		t.Fatalf("?? should fall back only on errors and preserve successful nil values, got %q", output)
	}
}

func compileAndRunOperatorFixture(t *testing.T, source string) string {
	t.Helper()
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	sourcePath := filepath.Join(inputDir, "main.gpp")
	if err := os.WriteFile(sourcePath, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CompileFilesWithOptions([]string{sourcePath}, outputDir, CompileOptions{ModulePath: "generated"}); err != nil {
		t.Fatalf("compile operator fixture: %v", err)
	}
	command := exec.Command("go", "run", ".")
	command.Dir = outputDir
	command.Env = append(os.Environ(), "GOCACHE=/tmp/gpp-go-cache")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run operator fixture: %v\n%s", err, output)
	}
	return string(output)
}
