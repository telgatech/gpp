package compiler

import (
	"strings"
	"testing"
)

func testPersonFile(t *testing.T) *File {
	t.Helper()

	file, err := ParseFile("test.gpp", `
class Person {
    Name string
    Age int
}
`)
	if err != nil {
		t.Fatal(err)
	}

	return file
}

func TestEmitPositionalConstructor(t *testing.T) {
	file := testPersonFile(t)
	file.Decls = append(file.Decls, &RawDecl{
		Code: `func main() {
    p := Person("Bob", 42)
    _ = p
}`,
	})

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `p := Person{Name: "Bob", Age: 42}`) {
		t.Fatalf("generated code did not contain compact construction:\n%s", code)
	}
}

func TestEmitNamedConstructor(t *testing.T) {
	file := testPersonFile(t)
	file.Decls = append(file.Decls, &RawDecl{
		Code: `func main() {
    p := Person(
    Name: "Bob",
    Age: 42,
    )
    _ = p
}`,
	})

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `p := Person{Name: "Bob", Age: 42}`) {
		t.Fatalf("generated code did not contain named construction:\n%s", code)
	}
}

func TestEmitConstructorHandlesNestedExpressions(t *testing.T) {
	file := testPersonFile(t)
	file.Decls = append(file.Decls, &RawDecl{
		Code: `func main() {
    p := Person(makeName("Bob, Jr."), add(20, 22))
    _ = p
}`,
	})

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `p := Person{Name: makeName("Bob, Jr."), Age: add(20, 22)}`) {
		t.Fatalf("generated code did not preserve nested expressions:\n%s", code)
	}
}

func TestEmitConstructorRejectsUnknownNamedField(t *testing.T) {
	file := testPersonFile(t)
	file.Decls = append(file.Decls, &RawDecl{
		Code: `func main() {
    p := Person(Name: "Bob", Height: 180)
    _ = p
}`,
	})

	_, err := Emit(file)
	if err == nil || !strings.Contains(err.Error(), "unknown field Height") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestEmitConstructorRejectsWrongArity(t *testing.T) {
	file := testPersonFile(t)
	file.Decls = append(file.Decls, &RawDecl{
		Code: `func main() {
    p := Person("Bob")
    _ = p
}`,
	})

	_, err := Emit(file)
	if err == nil || !strings.Contains(err.Error(), "expects 2 arguments, got 1") {
		t.Fatalf("expected arity error, got %v", err)
	}
}

func TestEmitPreservesGoImportSyntax(t *testing.T) {
	file, err := ParseFile("imports.gpp", `
import (
    "fmt"
    j "encoding/json"
)

class Person {
    Name string
}

func main() {
    p := Person("Bob")
    _, _ = fmt.Println(p.Name), j.Compact
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, `"fmt"`) ||
		!strings.Contains(generated, `j "encoding/json"`) {
		t.Fatalf("generated code did not preserve Go imports:\n%s", code)
	}
	if strings.Count(generated, `"fmt"`) != 1 {
		t.Fatalf("fmt was imported more than once:\n%s", code)
	}
}

func TestInterpolationUsesFmtAlias(t *testing.T) {
	file, err := ParseFile("alias.gpp", `
import f "fmt"

func main() {
    _ = "value {{1}}"
    _ = f.Println
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `f.Sprintf("value %v", 1)`) {
		t.Fatalf("interpolation did not use the fmt alias:\n%s", code)
	}
}

func TestInterpolationUsesFmtAliasInDottedPackage(t *testing.T) {
	file, err := ParseFile("dotted_alias.gpp", `
package telga.web

import f "fmt"

func Greeting() string {
    return "value {{1}}"
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "package web") ||
		!strings.Contains(generated, `f.Sprintf("value %v", 1)`) {
		t.Fatalf("dotted package interpolation did not preserve the fmt alias:\n%s", code)
	}
	if strings.Contains(generated, "import \"fmt\"") {
		t.Fatalf("dotted package interpolation synthesized a duplicate fmt import:\n%s", code)
	}
}

func TestEmitUsesFinalDottedPackageSegment(t *testing.T) {
	file, err := ParseFile("web.gpp", `
package telga.web

class Page {}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(code), "package web\n") {
		t.Fatalf("generated package name was not the final segment:\n%s", code)
	}
}

func TestEmitInheritedPositionalConstructor(t *testing.T) {
	file, err := ParseFile("inheritance.gpp", `
class Person {
    Name string
}

class Employee: Person {
    Salary int
}

func main() {
    employee := Employee("Bob", 100)
    _ = employee
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `Employee{Person: Person{Name: "Bob"}, Salary: 100}`) {
		t.Fatalf("generated code did not initialize inherited fields:\n%s", code)
	}
}

func TestEmitInheritedNamedConstructor(t *testing.T) {
	file, err := ParseFile("inheritance_named.gpp", `
class Person {
    Name string
}

class Employee: Person {
    Salary int
}

func main() {
    employee := Employee(
        Name: "Bob",
        Salary: 100,
    )
    _ = employee
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `Employee{Person: Person{Name: "Bob"}, Salary: 100}`) {
		t.Fatalf("generated named construction did not initialize inherited fields:\n%s", code)
	}
}

func TestEmitRequiresQualifiedAmbiguousInheritedField(t *testing.T) {
	file, err := ParseFile("ambiguous.gpp", `
class A {
    Name string
}

class B {
    Name string
}

class C: A, B {}

func main() {
    value := C(Name: "ambiguous")
    _ = value
}
`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "ambiguous named argument Name") {
		t.Fatalf("expected ambiguous field error, got %v", err)
	}
}

func TestEmitQualifiedAmbiguousInheritedFields(t *testing.T) {
	file, err := ParseFile("qualified_ambiguity.gpp", `
class A {
    Name string
}

class B {
    Name string
}

class C: A, B {}

func main() {
    value := C(A.Name: "a", B.Name: "b")
    _ = value
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), `C{A: A{Name: "a"}, B: B{Name: "b"}}`) {
		t.Fatalf("qualified fields were not emitted into their parents:\n%s", code)
	}
}

func TestEmitRejectsUnqualifiedAmbiguousMemberAccess(t *testing.T) {
	file, err := ParseFile("ambiguous_access.gpp", `
class A {
    Name string
}

class B {
    Name string
}

class C: A, B {
    func GetName() string {
        return this.Name
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "ambiguous member Name") ||
		!strings.Contains(err.Error(), "this.A.Name") {
		t.Fatalf("expected qualified member access error, got %v", err)
	}
}

func TestEmitAllowsQualifiedAmbiguousMemberAccess(t *testing.T) {
	file, err := ParseFile("qualified_access.gpp", `
class A {
    Name string
}

class B {
    Name string
}

class C: A, B {
    func GetName() string {
        return this.A.Name
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Emit(file); err != nil {
		t.Fatal(err)
	}
}

func TestEmitLowersBaseTypedAssignmentForVirtualDispatch(t *testing.T) {
	file, err := ParseFile("polymorphism.gpp", `
class Person {
    func Speak() string {
        return "person"
    }
}

class Employee: Person {
    func Speak() string {
        return "employee"
    }
}

func main() {
    var person Person = Employee()
    _ = person.Speak()
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "type __gopp_Person interface") {
		t.Fatalf("base dispatch interface was not generated:\n%s", code)
	}
	if !strings.Contains(generated, "var person __gopp_Person = &Employee{}") {
		t.Fatalf("base-typed assignment was not lowered:\n%s", code)
	}
}

func TestEmitLowersPolymorphicFunctionParametersAndReturns(t *testing.T) {
	file, err := ParseFile("polymorphic_functions.gpp", `
class Person {
    func Speak() string {
        return "person"
    }
}

class Employee: Person {
    func Speak() string {
        return "employee"
    }
}

func SpeakFor(person Person) string {
    return person.Speak()
}

func MakePerson() Person {
    return Employee()
}
`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func SpeakFor(person __gopp_Person) string",
		"func MakePerson() __gopp_Person",
		"return &Employee{}",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("generated code did not contain %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersDerivedArgumentsForBaseParameters(t *testing.T) {
	file, err := ParseFile("polymorphic_arguments.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

func SpeakFor(person Person) string {
    return person.Speak()
}

func main() {
    _ = SpeakFor(Employee())
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func SpeakFor(person __gopp_Person) string",
		"_ = SpeakFor(&Employee{})",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("derived argument was not coerced for a base parameter, missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersDerivedArgumentsForBaseMethodParameters(t *testing.T) {
	file, err := ParseFile("polymorphic_method_arguments.gpp", `
class Person {
    func Speak() string { return "person" }

    func Use(other Person) string {
        return other.Speak()
    }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

func main() {
    person := Person()
    _ = person.Use(Employee())
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func (this *Person) Use(other __gopp_Person) string",
		"_ = person.Use(&Employee{})",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("derived method argument was not coerced, missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersBaseConstructorToPointerForDispatch(t *testing.T) {
	file, err := ParseFile("polymorphic_base_constructor.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {}

func MakePerson() Person {
    return Person()
}

func main() {
    var person Person = Person()
    _ = person
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func MakePerson() __gopp_Person",
		"return &Person{}",
		"var person __gopp_Person = &Person{}",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("base constructor was not lowered to a pointer for dispatch, missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersPolymorphicFields(t *testing.T) {
	file, err := ParseFile("polymorphic_field.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

class Holder {
    Value Person
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), "Value __gopp_Person") {
		t.Fatalf("polymorphic field was not lowered to the dispatch interface:\n%s", code)
	}
}

func TestEmitLowersPolymorphicConstructorFieldValue(t *testing.T) {
	file, err := ParseFile("polymorphic_constructor_field.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

class Holder {
    Value Person
}

func main() {
    holder := Holder(Employee())
    _ = holder
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), "Holder{Value: &Employee{}}") {
		t.Fatalf("polymorphic constructor field was not lowered to a pointer:\n%s", code)
	}
}

func TestEmitResolvesMethodOverloadsByArity(t *testing.T) {
	file, err := ParseFile("method_overloads.gpp", `
class Printer {
    func Print() string {
        return "empty"
    }

    func Print(value string) string {
        return value
    }

    func Use() string {
        return this.Print() + this.Print("value")
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func (this *Printer) Print__gopp_0() string",
		"func (this *Printer) Print__gopp_1(value string) string",
		"this.Print__gopp_0()",
		"this.Print__gopp_1(\"value\")",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("method overload output missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitScopesMethodOverloadsToDeclaringClass(t *testing.T) {
	file, err := ParseFile("scoped_method_overloads.gpp", `
class A {
    func Print(value string) string {
        return value
    }

    func Use() string {
        return this.Print("a")
    }
}

class B {
    func Print() string {
        return "empty"
    }

    func Print(value string) string {
        return value
    }

    func Use() string {
        return this.Print("b")
    }
}

func main() {
    a := A{}
    b := B{}
    _ = a.Print("a")
    _ = b.Print("b")
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "func (this *A) Print(value string)") ||
		!strings.Contains(generated, "this.Print(\"a\")") {
		t.Fatalf("unrelated class method was incorrectly renamed:\n%s", code)
	}
	if !strings.Contains(generated, "func (this *B) Print__gopp_1(value string)") ||
		!strings.Contains(generated, "this.Print__gopp_1(\"b\")") ||
		!strings.Contains(generated, "a.Print(\"a\")") ||
		!strings.Contains(generated, "b.Print__gopp_1(\"b\")") {
		t.Fatalf("overloaded class method was not renamed:\n%s", code)
	}
}

func TestEmitResolvesFunctionOverloadsByArity(t *testing.T) {
	file, err := ParseFile("function_overloads.gpp", `
func Add(value int) int {
    return value
}

func Add(left int, right int) int {
    return left + right
}

func Use() int {
    return Add(1) + Add(1, 2)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func Add__gopp_1(value int) int",
		"func Add__gopp_2(left int, right int) int",
		"return Add__gopp_1(1) + Add__gopp_2(1, 2)",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("function overload output missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitSupportsNamedAndDefaultFunctionArguments(t *testing.T) {
	file, err := ParseFile("function_defaults.gpp", `
func Greeting(name string, punctuation string = "!") string {
    return name + punctuation
}

func main() {
    first := Greeting(name: "Bob")
    second := Greeting(punctuation: "?", name: "Ada")
    _, _ = first, second
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func Greeting(name string, punctuation string) string",
		`first := Greeting("Bob", "!")`,
		`second := Greeting("Ada", "?")`,
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("named/default function arguments missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitSupportsNamedAndDefaultMethodArguments(t *testing.T) {
	file, err := ParseFile("method_defaults.gpp", `
class Greeter {
    func Greeting(name string, punctuation string = "!") string {
        return name + punctuation
    }

    func Use() string {
        return this.Greeting(punctuation: "?", name: "Ada")
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatalf("%v\n%s", err, code)
	}

	generated := string(code)
	if !strings.Contains(generated, "func (this *Greeter) Greeting(name string, punctuation string) string") ||
		!strings.Contains(generated, `this.Greeting("Ada", "?")`) {
		t.Fatalf("named/default method arguments were not lowered:\n%s", code)
	}
}

func TestEmitSupportsSafeMemberAccess(t *testing.T) {
	file, err := ParseFile("safe_access.gpp", `
class Person {
    Name string

    func Speak() string {
        return "hello " + this.Name
    }
}

func main() {
    var person *Person
    name := person?.Name
    greeting := person?.Speak()
    _, _ = name, greeting
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func __gopp_safe[R any]",
		"name := __gopp_safe(person == nil, func() string { return person.Name })",
		"greeting := __gopp_safe(person == nil, func() string { return person.Speak() })",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("safe access output missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitResolvesSameArityFunctionOverloadsByType(t *testing.T) {
	file, err := ParseFile("typed_function_overloads.gpp", `
func Format(value int) string {
    return "int"
}

func Format(value string) string {
    return "string"
}

func Use() string {
    return Format(1) + Format("value")
}

func UseVariable(value int) string {
    return Format(value)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func Format__gopp_1_int(value int) string",
		"func Format__gopp_1_string(value string) string",
		"return Format__gopp_1_int(1) + Format__gopp_1_string(\"value\")",
		"return Format__gopp_1_int(value)",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("typed function overload output missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitResolvesSameArityMethodOverloadsByType(t *testing.T) {
	file, err := ParseFile("typed_method_overloads.gpp", `
class Formatter {
    func Format(value int) string {
        return "int"
    }

    func Format(value string) string {
        return "string"
    }

    func Use() string {
        return this.Format(1) + this.Format("value")
    }

    func UseValue(value int) string {
        return this.Format(value)
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func (this *Formatter) Format__gopp_1_int(value int) string",
		"func (this *Formatter) Format__gopp_1_string(value string) string",
		"this.Format__gopp_1_int(1) + this.Format__gopp_1_string(\"value\")",
		"return this.Format__gopp_1_int(value)",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("typed method overload output missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitRejectsUnknownTypedOverloadCall(t *testing.T) {
	file, err := ParseFile("unknown_typed_overload.gpp", `
func Format(value int) string { return "int" }
func Format(value string) string { return "string" }

func Use(value bool) string {
    return Format(value)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "cannot resolve overloaded function Format") {
		t.Fatalf("expected unknown typed overload diagnostic, got %v", err)
	}
}

func TestEmitLowersKnownPolymorphicFunctionResults(t *testing.T) {
	file, err := ParseFile("polymorphic_results.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

func MakeEmployee() Employee {
    return Employee()
}

func SpeakFor(person Person) string {
    return person.Speak()
}

func main() {
    _ = SpeakFor(MakeEmployee())
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func MakeEmployee() *Employee",
		"return &Employee{}",
		"func SpeakFor(person __gopp_Person) string",
		"_ = SpeakFor(MakeEmployee())",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("known polymorphic function result was not lowered, missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitLowersKnownPolymorphicMethodResults(t *testing.T) {
	file, err := ParseFile("polymorphic_method_results.gpp", `
class Person {
    func Speak() string { return "person" }
}

class Employee: Person {
    func Speak() string { return "employee" }
}

class Factory {
    func Make() Employee {
        return Employee()
    }
}

func SpeakFor(person Person) string {
    return person.Speak()
}

func main() {
    factory := Factory()
    _ = SpeakFor(factory.Make())
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"func (this *Factory) Make() *Employee",
		"return &Employee{}",
		"_ = SpeakFor(factory.Make())",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("known polymorphic method result was not lowered, missing %q:\n%s", expected, code)
		}
	}
}

func TestEmitSupportsAnonymousRecords(t *testing.T) {
	file, err := ParseFile("records.gpp", `
func GetUser() record {
    return record(
        name: "Bob",
        age: 42,
        profile: record(active: true),
    )
}

func main() {
    let user = GetUser()
    _ = user.name
    _ = user.profile.active
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"type __gopp_record_",
		"func GetUser() __gopp_record_",
		"name: \"Bob\"",
		"age: 42",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("anonymous record output missing %q:\n%s", expected, code)
		}
	}
	if strings.Contains(generated, "let user") {
		t.Fatalf("let syntax was not lowered:\n%s", code)
	}
}

func TestEmitRejectsInconsistentRecordReturns(t *testing.T) {
	file, err := ParseFile("bad_records.gpp", `
func Broken(ok bool) record {
    if ok {
        return record(message: "yes")
    }
    return record(message: 1)
}
`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "inconsistent record return types in Broken") {
		t.Fatalf("expected inconsistent record return diagnostic, got %v", err)
	}
}

func TestEmitInfersRecordSlices(t *testing.T) {
	file, err := ParseFile("record_slice.gpp", `
func Users() []record {
    return []record{
        record(name: "A", age: 1),
        record(age: 2, name: "B"),
    }
}

func main() {
    users := Users()
    _ = users[0].name
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "func Users() []__gopp_record_") {
		t.Fatalf("record slice return type was not inferred:\n%s", code)
	}
}

func TestEmitInfersRecordMaps(t *testing.T) {
	file, err := ParseFile("record_map.gpp", `
func Users() map[string]record {
    return map[string]record{
        "first": record(name: "A"),
        "second": record(name: "B"),
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(code), "func Users() map[string]__gopp_record_") {
		t.Fatalf("record map return type was not inferred:\n%s", code)
	}
}

func TestEmitSupportsLocalRecordCollections(t *testing.T) {
	file, err := ParseFile("record_collections.gpp", `
func main() {
    users := []record{
        record(name: "A"),
        record(name: "B"),
    }
    byName := map[string]record{
        "first": record(name: "A"),
    }
    _ = users[0].name
    _ = byName["first"].name
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if strings.Contains(generated, "[]record") || strings.Contains(generated, "map[string]record") {
		t.Fatalf("local record collection types were not lowered:\n%s", code)
	}
}

func TestEmitInfersRecordFunctionParameters(t *testing.T) {
	file, err := ParseFile("record_parameter.gpp", `
func PrintUser(user record) string {
    return user.name
}

func main() {
    _ = PrintUser(record(name: "Bob"))
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	if !strings.Contains(generated, "func PrintUser(user __gopp_record_") {
		t.Fatalf("record function parameter was not inferred:\n%s", code)
	}
}

func TestEmitGeneratesClassIntrospection(t *testing.T) {
	file, err := ParseFile("introspection.gpp", `
class Model {
    Id *string
}

class Employee: Model {
    Name string
    Age int

    func Label() string {
        return this.class.name
    }
}

func main() {
    employee := Employee("id", "Bob", 42)
    c := Employee.class
    for f := range employee.class.fields {
        _ = f.name
        _ = f.owner.name
        _ = f.type.name
        _ = f.get(employee)
        f.set(employee, "Alice")
        _ = f.addr(employee)
    }
    _ = c
}
`)
	if err != nil {
		t.Fatal(err)
	}

	code, err := Emit(file)
	if err != nil {
		t.Fatal(err)
	}

	generated := string(code)
	for _, expected := range []string{
		"type GoppClass struct",
		"var GoppEmployeeClass = &GoppClass{Name: \"Employee\"",
		"func (this Employee) GoppRuntimeClass() *GoppClass",
		"return this.GoppRuntimeClass().Name",
		"for _, f := range employee.GoppRuntimeClass().Fields",
		"_ = f.Name",
		"_ = f.Owner.Name",
		"_ = f.Type.Name",
		"_ = f.Get(employee)",
		"f.Set(&employee, \"Alice\")",
		"_ = f.Addr(&employee)",
	} {
		if !strings.Contains(generated, expected) {
			t.Fatalf("introspection output missing %q:\n%s", expected, code)
		}
	}
	if strings.Index(generated, `Name: "Id"`) > strings.Index(generated, `Name: "Name"`) {
		t.Fatalf("inherited fields are not emitted before own fields:\n%s", code)
	}
}

func TestEmitRejectsNativeTypeIntrospection(t *testing.T) {
	file, err := ParseFile("native_introspection.gpp", `
import "net/http"

func main() {
    _ = http.Request.class
}
`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Emit(file)
	if err == nil || !strings.Contains(err.Error(), "http.Request.class") {
		t.Fatalf("expected native type introspection error, got %v", err)
	}
}
