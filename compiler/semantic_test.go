package compiler

import (
	"strings"
	"testing"
)

func parseProgram(t *testing.T, sources ...string) *Program {
	t.Helper()

	program := &Program{}
	for index, source := range sources {
		file, err := ParseFile("file"+string(rune('A'+index))+".gpp", source)
		if err != nil {
			t.Fatal(err)
		}
		program.Files = append(program.Files, file)
	}

	return program
}

func TestResolveProgramBuildsCrossFilePackageSymbols(t *testing.T) {
	program := parseProgram(t,
		"package telga.web\nclass Person {\n    Name string\n}\n",
		"package telga.web\nclass Employee: Person {}\n",
	)

	model, err := ResolveProgram(program)
	if err != nil {
		t.Fatal(err)
	}

	pkg := model.Packages["telga.web"]
	if pkg == nil || pkg.Classes["Person"] == nil || pkg.Classes["Employee"] == nil {
		t.Fatalf("cross-file package symbols were not built: %#v", model.Packages)
	}
}

func TestResolveProgramRejectsDuplicateClasses(t *testing.T) {
	program := parseProgram(t,
		"class Person {}\n",
		"class Person {}\n",
	)

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "duplicate class Person") {
		t.Fatalf("expected duplicate class error, got %v", err)
	}
}

func TestResolveProgramCollectsDuplicateClassDeclarations(t *testing.T) {
	program := parseProgram(t,
		"class First {}\nclass Second {}\n",
		"class First {}\nclass Second {}\n",
	)
	_, err := ResolveProgram(program)
	if err == nil {
		t.Fatal("expected duplicate class diagnostics")
	}
	diagnostics := DiagnosticsOf(err)
	if len(diagnostics) != 2 {
		t.Fatalf("expected duplicate diagnostics for both classes, got %#v", diagnostics)
	}
	for index, want := range []string{"First", "Second"} {
		position, ok := PositionOfDiagnostic(diagnostics[index])
		if !ok || !strings.Contains(position.Message, "duplicate class "+want) {
			t.Errorf("diagnostic %d = %#v, want duplicate class %s", index, diagnostics[index], want)
		}
		if related := RelatedPositionsOfDiagnostic(diagnostics[index]); len(related) != 1 || related[0].Message != "first declaration is here" {
			t.Errorf("duplicate class %s should retain its related first declaration: %#v", want, related)
		}
	}
}

func TestResolveProgramRejectsEnumClassNameCollision(t *testing.T) {
	program := parseProgram(t, "enum Person int { Human }\n", "class Person {}\n")

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "duplicate type Person") {
		t.Fatalf("expected enum/class collision error, got %v", err)
	}
}

func TestResolveProgramRejectsInvalidInheritance(t *testing.T) {
	program := parseProgram(t, "class Employee: Missing {}\n")

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "unresolved parent Missing") {
		t.Fatalf("expected unresolved parent error, got %v", err)
	}
}

func TestResolveProgramRejectsChildAccessToPrivateInheritedFieldsAndMethods(t *testing.T) {
	tests := []struct {
		name   string
		source string
		member string
	}{
		{
			name: "field",
			source: `class Parent {
    state string
}
class Child : Parent {
    func Read() string { return this.state }
}`,
			member: "state",
		},
		{
			name: "method",
			source: `class Parent {
    func helper() {}
}
class Child : Parent {
    func Read() { this.helper() }
}`,
			member: "helper",
		},
		{
			name: "parent-typed-parameter",
			source: `class Parent {
    state string
}
class Child : Parent {
    func Read(other Parent) string { return other.state }
}`,
			member: "state",
		},
		{
			name: "static-method",
			source: `class Parent {
    static func helper() {}
}
class Child : Parent {
    static func Read() { Parent.helper() }
}`,
			member: "helper",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveProgram(parseProgram(t, test.source))
			if err == nil || !strings.Contains(err.Error(), "private inherited member "+test.member) {
				t.Fatalf("expected child access to private member %q to be rejected, got %v", test.member, err)
			}
		})
	}
}

func TestResolveProgramAllowsProtectedInheritedFieldsAndMethods(t *testing.T) {
	program := parseProgram(t, `class Parent {
    _state string
    func _reset() { this._state = "" }
    static func _factory() Parent { return Parent{} }
}
class Child : Parent {
    func Reset() { this._reset(); this._state = "ready" }
    static func New() Parent { return Parent._factory() }
}`)
	if _, err := ResolveProgram(program); err != nil {
		t.Fatalf("protected inherited members should be accessible to the child: %v", err)
	}
}

func TestResolveProgramRejectsOrdinaryAccessToPrivateAndProtectedMembers(t *testing.T) {
	tests := []struct {
		name   string
		source string
		member string
		level  string
	}{
		{
			name: "private-field",
			source: `class Account { pin string }
func show(account Account) string { return account.pin }`,
			member: "pin",
			level:  "private",
		},
		{
			name: "private-method",
			source: `class Account { func reset() {} }
func reset(account Account) { account.reset() }`,
			member: "reset",
			level:  "private",
		},
		{
			name: "private-field-on-inferred-local",
			source: `class Account { pin string }
func show() string { account := Account{}; return account.pin }`,
			member: "pin",
			level:  "private",
		},
		{
			name: "private-field-in-composite-literal",
			source: `class Account { pin string }
func show() Account { return Account{pin: "hidden"} }`,
			member: "pin",
			level:  "private",
		},
		{
			name: "private-field-on-computed-function-result",
			source: `class Account { pin string }
func GetAccount() Account { return Account{} }
func show() string { return GetAccount().pin }`,
			member: "pin",
			level:  "private",
		},
		{
			name: "private-field-through-type-alias",
			source: `class Account { pin string }
type AccountAlias = Account
func show(account AccountAlias) string { return account.pin }`,
			member: "pin",
			level:  "private",
		},
		{
			name: "private-field-after-type-switch",
			source: `class Account { pin string }
func show(value any) string {
    switch account := value.(type) {
    case Account:
        return account.pin
    default:
        return ""
    }
}`,
			member: "pin",
			level:  "private",
		},
		{
			name: "private-field-inside-function-literal",
			source: `class Account { pin string }
func show() {
	func() {
		account := Account{}
		_ = account.pin
	}()
}`,
			member: "pin",
			level:  "private",
		},
		{
			name: "private-field-inside-contextual-lambda",
			source: `class Account { pin string }
func Visit(callback func(Account)) {}
func show() { Visit(account => println(account.pin)) }`,
			member: "pin",
			level:  "private",
		},
		{
			name: "private-field-inside-contextual-lambda-through-function-value",
			source: `class Account { pin string }
func show(visit func(func(Account))) { visit(account => println(account.pin)) }`,
			member: "pin",
			level:  "private",
		},
		{
			name: "private-field-on-computed-method-result",
			source: `class Account { pin string }
class Store { func Get() Account { return Account{} } }
func show(store Store) string { return store.Get().pin }`,
			member: "pin",
			level:  "private",
		},
		{
			name: "private-field-on-indexed-value",
			source: `class Account { pin string }
func show(accounts []Account) string { return accounts[0].pin }`,
			member: "pin",
			level:  "private",
		},
		{
			name: "protected-field",
			source: `class Account { _balance int }
func show(account Account) int { return account._balance }`,
			member: "_balance",
			level:  "protected",
		},
		{
			name: "protected-field-in-composite-literal",
			source: `class Account { _balance int }
func show() Account { return Account{_balance: 100} }`,
			member: "_balance",
			level:  "protected",
		},
		{
			name: "protected-static-method",
			source: `class Account { static func _reset() {} }
func show() { Account._reset() }`,
			member: "_reset",
			level:  "protected",
		},
		{
			name: "protected-method-on-base-and-child-values",
			source: `class Account { func _reset() {} }
class SavingsAccount : Account {}
func reset(account Account, savings SavingsAccount) { account._reset(); savings._reset() }`,
			member: "_reset",
			level:  "protected",
		},
		{
			name: "protected-member-in-extension",
			source: `class Account { _balance int }
extend Account { func Leak() int { return this._balance } }`,
			member: "_balance",
			level:  "protected",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ResolveProgram(parseProgram(t, test.source))
			want := "cannot access " + test.level + " member " + test.member
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %q access to be rejected, got %v", want, err)
			}
		})
	}
}

func TestResolveProgramAllowsProtectedAccessFromChildToLocalBaseValue(t *testing.T) {
	program := parseProgram(t, `class Parent { _state string }
class Child : Parent {
    func Read() string {
        parent := Parent{}
        return parent._state
    }
}`)
	if _, err := ResolveProgram(program); err != nil {
		t.Fatalf("a child should be able to access protected members through a local base value: %v", err)
	}
}

func TestResolveProgramAllowsDeclaringClassAndChildCompositeInitialization(t *testing.T) {
	program := parseProgram(t, `class Parent {
    pin string
    _state string
    func Initialize() Parent { return Parent{pin: "own", _state: "ready"} }
}
class Child : Parent {
    func InitializeChild() Child { return Child{_state: "ready"} }
}`)
	if _, err := ResolveProgram(program); err != nil {
		t.Fatalf("classes should be able to initialize their private fields and children their protected fields: %v", err)
	}
}

func TestResolveProgramRejectsProtectedAccessThroughParentQualifiedReceiver(t *testing.T) {
	program := parseProgram(t, `class Parent { _state string }
class Child : Parent {}
func show(child Child) string { return child.Parent._state }`)
	if _, err := ResolveProgram(program); err == nil || !strings.Contains(err.Error(), "cannot access protected member _state") {
		t.Fatalf("a parent qualifier must not expose protected state to ordinary callers, got %v", err)
	}
}

func TestResolveProgramChecksComputedMemberReceiversAcrossFiles(t *testing.T) {
	program := parseProgram(t,
		"package example\nclass Account { pin string }\nfunc GetAccount() Account { return Account{} }\n",
		"package example\nfunc show() string { return GetAccount().pin }\n",
	)
	if _, err := ResolveProgram(program); err == nil || !strings.Contains(err.Error(), "cannot access private member pin") {
		t.Fatalf("private access through a cross-file function result should be rejected, got %v", err)
	}
}

func TestResolveProgramAllowsPublicExtensionAccess(t *testing.T) {
	program := parseProgram(t, `class Account { Name string }
extend Account { func ReadName() string { return this.Name } }`)
	if _, err := ResolveProgram(program); err != nil {
		t.Fatalf("extensions should be able to access public target members: %v", err)
	}
}

func TestResolveProgramRejectsPrivateAccessFromNativeGoMethod(t *testing.T) {
	program := parseProgram(t, `class Account { pin string }
func (account Account) Leak() string { return account.pin }`)
	if _, err := ResolveProgram(program); err == nil || !strings.Contains(err.Error(), "cannot access private member pin") {
		t.Fatalf("a native Go method must not bypass GPP class privacy, got %v", err)
	}
}

func TestResolveProgramRejectsPrivateAccessFromNativeGoTypeSwitch(t *testing.T) {
	program := parseProgram(t, `class Account { pin string }
func reveal(value any) string {
	switch account := value.(type) {
	case Account:
		return account.pin
	default:
		return ""
	}
}`)
	if _, err := ResolveProgram(program); err == nil || !strings.Contains(err.Error(), "cannot access private member pin") {
		t.Fatalf("a native Go type switch must not bypass GPP class privacy, got %v", err)
	}
}

func TestResolveProgramRejectsPrivateAccessFromNativeGoPackageValue(t *testing.T) {
	program := parseProgram(t, `class Account { pin string }
var leaked = Account{pin: "hidden"}`)
	if _, err := ResolveProgram(program); err == nil || !strings.Contains(err.Error(), "cannot access private member pin") {
		t.Fatalf("a native Go package value must not bypass GPP class privacy, got %v", err)
	}
}

func TestResolveProgramAllowsPublicAccessAndDeclaringClassPrivateAccess(t *testing.T) {
	program := parseProgram(t, `class Account {
    Name string
    pin string
    func ownPin() string { return this.pin }
    func Reset() { this.reset() }
    func reset() {}
}
class SavingsAccount : Account {
    func ReadName(account Account) string { return account.Name }
}
func show(account Account) string { return account.Name }`)
	if _, err := ResolveProgram(program); err != nil {
		t.Fatalf("public or declaring-class private access should be allowed: %v", err)
	}
}

func TestResolveProgramCollectsIndependentClassDiagnostics(t *testing.T) {
	program := parseProgram(t, "class GppClass {}\nclass GppField {}\n")
	_, err := ResolveProgram(program)
	if err == nil {
		t.Fatal("expected reserved class-name diagnostics")
	}
	diagnostics := DiagnosticsOf(err)
	if len(diagnostics) != 2 {
		t.Fatalf("expected both independent class diagnostics, got %#v", diagnostics)
	}
	for index, want := range []string{"GppClass", "GppField"} {
		position, ok := PositionOfDiagnostic(diagnostics[index])
		if !ok || !strings.Contains(position.Message, want) {
			t.Fatalf("diagnostic %d = %#v, want source-located error for %s", index, diagnostics[index], want)
		}
	}
	if !strings.Contains(err.Error(), "GppClass") {
		t.Fatalf("ordinary error formatting should preserve the first diagnostic: %v", err)
	}
}

func TestResolveProgramCollectsIndependentExtensionDiagnostics(t *testing.T) {
	program := parseProgram(t, `
extend string { func First() {} }
extend string { func First() {} }
extend int { func Second() {} }
extend int { func Second() {} }
`)
	_, err := ResolveProgram(program)
	if err == nil {
		t.Fatal("expected duplicate extension diagnostics")
	}
	diagnostics := DiagnosticsOf(err)
	if len(diagnostics) != 2 {
		t.Fatalf("expected both duplicate extension diagnostics, got %#v", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		position, ok := PositionOfDiagnostic(diagnostic)
		if !ok || !strings.Contains(position.Message, "declared more than once") {
			t.Errorf("expected source-located duplicate extension diagnostic, got %#v", diagnostic)
		}
	}
}

func TestResolveProgramRejectsInheritanceCycles(t *testing.T) {
	program := parseProgram(t, `
class A: C {}
class B: A {}
class C: B {}
`)

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "inheritance cycle") {
		t.Fatalf("expected inheritance cycle error, got %v", err)
	}
}

func TestResolveProgramRejectsDuplicateMembers(t *testing.T) {
	program := parseProgram(t, `
class Person {
    Name string
    Name int

    func Speak() {}
    func Speak() {}
}
`)

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "field Name more than once") {
		t.Fatalf("expected duplicate field error, got %v", err)
	}
}

func TestResolveProgramAllowsTypedMethodOverloads(t *testing.T) {
	program := parseProgram(t, `
class Formatter {
    func Format(value int) string { return "int" }
    func Format(value string) string { return "string" }
}
`)

	if _, err := ResolveProgram(program); err != nil {
		t.Fatal(err)
	}
	methods := program.Files[0].Decls[0].(*ClassDecl).Methods
	if methods[0].GoName != "Format__gpp_1_int" || methods[1].GoName != "Format__gpp_1_string" {
		t.Fatalf("typed overloads were not assigned stable Go names: %#v", methods)
	}
}

func TestResolveProgramAllowsGenericAndNonGenericMethodOverloads(t *testing.T) {
	program := parseProgram(t, `
class Client {
	func Get(url string) string { return "raw" }
	func Get[T any](url string) T { var value T; return value }
}
`)
	if _, err := ResolveProgram(program); err != nil {
		t.Fatal(err)
	}
	methods := program.Files[0].Decls[0].(*ClassDecl).Methods
	if methods[0].GoName == "" || methods[1].GoName == "" || methods[0].GoName == methods[1].GoName {
		t.Fatalf("generic and non-generic overloads were not assigned distinct Go names: %#v", methods)
	}
}

func TestResolveProgramRejectsDuplicateTypedMethodOverloads(t *testing.T) {
	program := parseProgram(t, `
class Formatter {
	func Format(value int) string { return "one" }
	func Format(other int) string { return "two" }
}
`)

	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "parameter types int") {
		t.Fatalf("expected duplicate typed overload error, got %v", err)
	}
}

func TestResolveProgramValidatesAnnotationPlacement(t *testing.T) {
	program := parseProgram(t, `
annotation PK on field
class Employee @{PK} {}
`)
	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "annotation PK cannot be applied to class") {
		t.Fatalf("expected annotation placement error, got %v", err)
	}
}

func TestResolveProgramValidatesAnnotationArguments(t *testing.T) {
	program := parseProgram(t, `
annotation Min(value int) on field
class Employee {
    Age int @{Min("18")}
}
`)
	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("expected annotation argument type error, got %v", err)
	}
}

func TestResolveProgramRejectsUnknownAnnotation(t *testing.T) {
	program := parseProgram(t, `
class Employee {
    Name string @{Required}
}
`)
	_, err := ResolveProgram(program)
	if err == nil || !strings.Contains(err.Error(), "undefined annotation Required") {
		t.Fatalf("expected unknown annotation error, got %v", err)
	}
}

func TestResolveProgramCollectsUnknownAnnotationUses(t *testing.T) {
	program := parseProgram(t, `
class Employee {
    Name string @{Required}
    Age int @{Positive}
}
`)
	_, err := ResolveProgram(program)
	if err == nil {
		t.Fatal("expected unknown annotation diagnostics")
	}
	diagnostics := DiagnosticsOf(err)
	if len(diagnostics) != 2 {
		t.Fatalf("expected both unknown annotation uses, got %#v", diagnostics)
	}
	for index, want := range []string{"Required", "Positive"} {
		position, ok := PositionOfDiagnostic(diagnostics[index])
		if !ok || !strings.Contains(position.Message, want) {
			t.Errorf("diagnostic %d = %#v, want source-located undefined annotation %s", index, diagnostics[index], want)
		}
	}
}
