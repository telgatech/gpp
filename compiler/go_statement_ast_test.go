package compiler

import "testing"

func TestGoStatementFallbackAdaptsOrdinaryGoAST(t *testing.T) {
	tokens, err := LexSource("go-statement-fallback.gpp", "if value > 0")
	if err != nil {
		t.Fatal(err)
	}
	draft := &statementDraft{Header: tokens, Body: &BlockStmt{}}
	statement := goStatementFromDraft(draft)
	ifStmt, ok := statement.(*IfStmt)
	if !ok {
		t.Fatalf("expected typed if statement, got %T", statement)
	}
	condition, ok := ifStmt.Condition.(*BinaryExpr)
	if !ok || condition.Operator != ">" {
		t.Fatalf("expected typed Go condition, got %#v", ifStmt.Condition)
	}
}

func TestGoStatementFallbackAdaptsTypeSwitchAST(t *testing.T) {
	tokens, err := LexSource("go-type-switch-fallback.gpp", "switch value := source.(type)")
	if err != nil {
		t.Fatal(err)
	}
	draft := &statementDraft{Header: tokens, Body: &BlockStmt{}}
	statement := goStatementFromDraft(draft)
	switchStmt, ok := statement.(*SwitchStmt)
	if !ok || !isTypeSwitchAssignment(switchStmt.Init) {
		t.Fatalf("expected typed type switch, got %#v", statement)
	}
}
