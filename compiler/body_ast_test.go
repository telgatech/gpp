package compiler

import (
	"strings"
	"testing"
)

func TestParseMethodBuildsStructuredBodyAST(t *testing.T) {
	source := `
class User {
    func Save() {
        // preserve this comment
        if ready {
            return
        }
    }
}
	`
	file, err := ParseFile("body-ast.gpp", source)
	if err != nil {
		t.Fatal(err)
	}
	class := file.Decls[0].(*ClassDecl)
	method := class.Methods[0]
	if method.Owner != file || methodBodySource(method) == "" {
		t.Fatalf("parsed method lost source ownership: %#v", method)
	}
	returnOffset := strings.Index(source, "return")
	returnLine, _ := sourcePosition(source, returnOffset)
	for _, token := range method.BodyTokens {
		if token.Text == "return" {
			if token.Span.Start != returnOffset || token.Span.Line != returnLine {
				t.Fatalf("method token was not rebased: token=%#v want offset=%d", token, returnOffset)
			}
			break
		}
	}
	if method.BodyAST == nil {
		body, bodyErr := ParseBodyAST(method.BodyTokens)
		t.Fatalf("method body parse failed: %v tokens=%#v body=%#v", bodyErr, method.BodyTokens, body)
	}
	if method.BodyAST == nil || len(method.BodyAST.Statements) != 1 || len(method.BodyAST.Comments) != 1 {
		t.Fatalf("method body was not structured: %#v", method.BodyAST)
	}
	ifStmt, ok := method.BodyAST.Statements[0].(*IfStmt)
	if !ok || ifStmt.Body == nil || len(ifStmt.Body.Statements) != 1 {
		t.Fatalf("unexpected if statement: %#v", method.BodyAST.Statements[0])
	}
	if ifStmt.Condition == nil {
		t.Fatalf("if statement was not typed: %#v", ifStmt)
	}
	returnStmt, ok := ifStmt.Body.Statements[0].(*ReturnStmt)
	if !ok {
		t.Fatalf("unexpected nested return statement: %#v", ifStmt.Body.Statements[0])
	}
	if len(returnStmt.Values) != 0 {
		t.Fatalf("return statement was not typed: %#v", returnStmt)
	}
}

func TestParseMethodBuildsTryCatchBodyAST(t *testing.T) {
	file, err := ParseFile("try-body-ast.gpp", `
class User {
    func Save() {
        try {
            return
        } catch error e {
            throw
        } finally {
            cleanup()
        }
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	method := file.Decls[0].(*ClassDecl).Methods[0]
	tryStmt, ok := method.BodyAST.Statements[0].(*TryStmt)
	if !ok || len(tryStmt.Catches) != 1 || tryStmt.Finally == nil {
		t.Fatalf("unexpected try AST: %#v", tryStmt)
	}
	if tryStmt.Body == nil || tryStmt.Catches[0].Body == nil || tryStmt.Finally == nil {
		t.Fatalf("try clauses did not receive bodies: %#v", tryStmt)
	}
	if tryStmt.Catches[0].Binding != "e" || len(tryStmt.Catches[0].Types) != 1 {
		t.Fatalf("try statement was not typed: %#v", tryStmt)
	}
}

func TestBodyASTStructuresElseIf(t *testing.T) {
	file, err := ParseFile("else-if-body-ast.gpp", `
class User {
    func State(value int) {
        if value == 1 {
            println("one")
        } else if value == 2 {
            println("two")
        } else {
            println("other")
        }
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	method := file.Decls[0].(*ClassDecl).Methods[0]
	statement, ok := method.BodyAST.Statements[0].(*IfStmt)
	if !ok {
		t.Fatalf("expected typed if statement, got %T", method.BodyAST.Statements[0])
	}
	if statement.Condition == nil || statement.ElseIf == nil || statement.ElseIf.Condition == nil || statement.ElseIf.Else == nil {
		t.Fatalf("else-if chain was not structured: %#v", statement)
	}
}

func TestBodyASTKeepsCompositeLiteralInsideStatement(t *testing.T) {
	file, err := ParseFile("composite-body-ast.gpp", `
class User {
    func First() int {
        values := []int{1, 2, 3}
        return values[0]
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	method := file.Decls[0].(*ClassDecl).Methods[0]
	if len(method.BodyAST.Statements) != 2 {
		t.Fatalf("composite literal split the statement: %#v", method.BodyAST.Statements)
	}
	declaration, ok := method.BodyAST.Statements[0].(*DeclarationStmt)
	if !ok {
		t.Fatalf("unexpected composite declaration: %#v", declaration)
	}
	if declaration.Keyword != ":=" || len(declaration.Names) != 1 {
		t.Fatalf("declaration statement was not typed: %#v", declaration)
	}
}

func TestBodyASTStructuresLoopsAndSelection(t *testing.T) {
	file, err := ParseFile("control-body-ast.gpp", `
class User {
    func Run(values []int, status int) {
        for index := range values {
            println(index)
        }
        switch status {
        case 1:
            println("one")
        default:
            println("other")
        }
    }
}

`)
	if err != nil {
		t.Fatal(err)
	}
	statements := file.Decls[0].(*ClassDecl).Methods[0].BodyAST.Statements
	if len(statements) != 2 {
		t.Fatalf("unexpected control statements: %#v", statements)
	}
	forStatement, ok := statements[0].(*ForStmt)
	if !ok || forStatement.RangeExpr == nil || len(forStatement.RangeKey) == 0 || forStatement.Body == nil {
		t.Fatalf("for statement was not structured: %#v", statements[0])
	}
	switchStatement, ok := statements[1].(*SwitchStmt)
	if !ok {
		t.Fatalf("unexpected switch statement: %#v", statements[1])
	}
	if switchStatement.Select || switchStatement.Tag == nil || switchStatement.Body == nil {
		t.Fatalf("switch statement was not structured: %#v", switchStatement)
	}
	typedSwitch := switchStatement
	if len(typedSwitch.Body.Statements) != 2 {
		t.Fatalf("switch clauses were not structured: %#v", typedSwitch.Body.Statements)
	}
	caseStatement, ok := typedSwitch.Body.Statements[0].(*CaseStmt)
	if !ok || caseStatement.Clause.Default || len(caseStatement.Clause.Expressions) != 1 || len(caseStatement.Clause.Body.Statements) != 1 {
		t.Fatalf("case clause was not structured: %#v", typedSwitch.Body.Statements[0])
	}
	defaultStatement, ok := typedSwitch.Body.Statements[1].(*CaseStmt)
	if !ok || !defaultStatement.Clause.Default || len(defaultStatement.Clause.Body.Statements) != 1 {
		t.Fatalf("default clause was not structured: %#v", typedSwitch.Body.Statements[1])
	}
}

func TestBodyASTStructuresDeferAndGo(t *testing.T) {
	file, err := ParseFile("defer-go-body-ast.gpp", `
class User {
    func Run(task func()) {
        defer task()
        go task()
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	statements := file.Decls[0].(*ClassDecl).Methods[0].BodyAST.Statements
	if len(statements) != 2 {
		t.Fatalf("unexpected defer/go statements: %#v", statements)
	}
	if statement, ok := statements[0].(*DeferStmt); !ok || statement.Expression == nil {
		t.Fatalf("defer statement was not structured: %#v", statements[0])
	}
	if statement, ok := statements[1].(*GoStmt); !ok || statement.Expression == nil {
		t.Fatalf("go statement was not structured: %#v", statements[1])
	}
}

func TestBodyASTSeparatesControlFlowInitializers(t *testing.T) {
	file, err := ParseFile("control-initializers.gpp", `
class User {
    func Run(value int) {
        if current := value + 1; current > 0 {
            println(current)
        }
        switch current := value + 1; current {
        case 1:
            println(current)
        }
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	statements := file.Decls[0].(*ClassDecl).Methods[0].BodyAST.Statements
	ifStatement := statements[0].(*IfStmt)
	if ifStatement.Init == nil || ifStatement.Condition == nil {
		t.Fatalf("if initializer and condition were not separated: %#v", ifStatement)
	}
	switchStatement := statements[1].(*SwitchStmt)
	if switchStatement.Init == nil || switchStatement.Tag == nil {
		t.Fatalf("switch initializer and tag were not separated: %#v", switchStatement)
	}
}

func TestBodyASTStructuresDeclarationsAssignmentsAndLabels(t *testing.T) {
	file, err := ParseFile("statement-body-ast.gpp", `
class User {
    func Run() {
        var total int = 1
        total += 2
    start:
        goto start
    }
}
	`)

	if err != nil {
		t.Fatal(err)
	}
	statements := file.Decls[0].(*ClassDecl).Methods[0].BodyAST.Statements
	if len(statements) != 4 {
		t.Fatalf("unexpected statement count: %#v", statements)
	}
	declared, ok := statements[0].(*DeclarationStmt)
	if !ok {
		t.Fatalf("declaration was not typed: %#v", statements[0])
	}
	if !ok || declared.Type == nil || len(declared.Names) != 1 || declared.Names[0].Text != "total" {
		t.Fatalf("declaration was not typed: %#v", declared)
	}
	assigned, ok := statements[1].(*AssignmentStmt)
	if !ok {
		t.Fatalf("assignment was not typed: %#v", statements[1])
	}
	if !ok || assigned.Operator != "+=" || len(assigned.Left) != 1 || len(assigned.Right) != 1 {
		t.Fatalf("assignment was not typed: %#v", assigned)
	}
	label, ok := statements[2].(*LabelStmt)
	if !ok {
		t.Fatalf("label was not typed: %#v", statements[2])
	}
	if label.Name != "start" {
		t.Fatalf("label was not typed: %#v", label)
	}
}

func TestBodyASTStructuresChannelSendAndIncDec(t *testing.T) {
	file, err := ParseFile("channel-body-ast.gpp", `
class User {
    func Run(channel chan int) {
        channel <- 1
        channel++
    }
}

`)
	if err != nil {
		t.Fatal(err)
	}
	statements := file.Decls[0].(*ClassDecl).Methods[0].BodyAST.Statements
	if len(statements) != 2 {
		t.Fatalf("unexpected statement count: %#v", statements)
	}
	send, ok := statements[0].(*SendStmt)
	if !ok {
		t.Fatalf("send statement was not typed: %#v", statements[0])
	}
	if !ok || send.Channel == nil || send.Value == nil {
		t.Fatalf("send statement was not typed: %#v", send)
	}
	incDec, ok := statements[1].(*IncDecStmt)
	if !ok {
		t.Fatalf("inc/dec statement was not typed: %#v", statements[1])
	}
	if !ok || incDec.Operator != "++" || incDec.Expression == nil {
		t.Fatalf("inc/dec statement was not typed: %#v", incDec)
	}
}

func TestBodyASTStructuresLocalTypeDeclaration(t *testing.T) {
	file, err := ParseFile("local-type-body-ast.gpp", `
class User {
    func Run() {
        type Alias = map[string]int
        type Person struct { Name string }
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	statements := file.Decls[0].(*ClassDecl).Methods[0].BodyAST.Statements
	if len(statements) != 2 {
		t.Fatalf("unexpected local type statement count: %#v", statements)
	}
	alias, ok := statements[0].(*TypeDeclarationStmt)
	if !ok {
		t.Fatalf("local type was not typed: %#v", statements[0])
	}
	if !ok || !alias.Alias || alias.Name != "Alias" || alias.Type == nil {
		t.Fatalf("alias declaration was not typed: %#v", alias)
	}
	second := statements[1].(*TypeDeclarationStmt)
	if second.Alias || second.Name != "Person" {
		t.Fatalf("struct declaration was not typed: %#v", second)
	}
	if _, ok := second.Type.(*StructType); !ok {
		t.Fatalf("struct declaration type was not structured: %#v", second.Type)
	}
}
