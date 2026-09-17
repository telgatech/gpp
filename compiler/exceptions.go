package compiler

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

type exceptionContext struct {
	RuntimeEmitted bool
}

func exceptionRuntimeDefinitions() string {
	return `type __gppThrownError struct {
	err error
}

func (thrown __gppThrownError) GppThrownError() error {
	return thrown.err
}

type __gppExceptionReturn struct {
	values []any
}

func __gppThrow(err error) {
	if err != nil {
		panic(__gppThrownError{err: err})
	}
}

func __gppRun(block func()) {
	block()
}

func __gppCoalesce[T any](left func() T, fallback func() T) (result T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if _, ok := recovered.(__gppThrownError); ok {
				result = fallback()
				return
			}
			panic(recovered)
		}
	}()
	return left()
}

func __gppUnwrap[T any](value T, err error) T {
	__gppThrow(err)
	return value
}

func __gppUnwrap2[A any, B any](first A, second B, err error) (A, B) {
	__gppThrow(err)
	return first, second
}

func __gppUnwrap3[A any, B any, C any](first A, second B, third C, err error) (A, B, C) {
	__gppThrow(err)
	return first, second, third
}

func __gppDiscard[T any](value T, err error) {
	__gppThrow(err)
}

func __gppDiscard2[A any, B any](first A, second B, err error) {
	__gppThrow(err)
}

func __gppDiscard3[A any, B any, C any](first A, second B, third C, err error) {
	__gppThrow(err)
}

`
}

type catchClause struct {
	typeName string
	variable string
	body     string
}

func transformExceptions(src string, context constructorContext) (string, error) {
	return transformExceptionRegion(src, context, "")
}

func transformExceptionRegion(src string, context constructorContext, rethrowName string) (string, error) {
	var out strings.Builder
	for index := 0; index < len(src); {
		if end, ok, err := copyIgnoredSource(src, index, &out); err != nil {
			return "", err
		} else if ok {
			index = end
			continue
		}

		if keywordAt(src, index, "try") && exceptionStatementStart(src, index) {
			open := skipSpace(src, index+len("try"))
			if open >= len(src) || src[open] != '{' {
				return "", fmt.Errorf("try must be followed by a block")
			}
			close, err := findMatchingBrace(src, open)
			if err != nil {
				return "", err
			}
			tryBody, err := transformExceptionRegion(src[open+1:close], context, rethrowName)
			if err != nil {
				return "", err
			}

			cursor := skipSpace(src, close+1)
			clauses := []catchClause{}
			for cursor < len(src) && keywordAt(src, cursor, "catch") {
				headerStart := skipSpace(src, cursor+len("catch"))
				bodyOpen := findExceptionBlockOpen(src, headerStart)
				if bodyOpen < 0 {
					return "", fmt.Errorf("catch must be followed by a block")
				}
				header := strings.TrimSpace(src[headerStart:bodyOpen])
				clause, err := parseCatchClause(header, context)
				if err != nil {
					return "", err
				}
				bodyClose, err := findMatchingBrace(src, bodyOpen)
				if err != nil {
					return "", err
				}
				clause.body, err = transformExceptionRegion(src[bodyOpen+1:bodyClose], context, "__gppRecovered")
				if err != nil {
					return "", err
				}
				clauses = append(clauses, clause)
				cursor = skipSpace(src, bodyClose+1)
			}
			if err := validateCatchOrdering(clauses, context); err != nil {
				return "", err
			}

			finallyBody := ""
			if cursor < len(src) && keywordAt(src, cursor, "finally") {
				bodyOpen := skipSpace(src, cursor+len("finally"))
				if bodyOpen >= len(src) || src[bodyOpen] != '{' {
					return "", fmt.Errorf("finally must be followed by a block")
				}
				bodyClose, err := findMatchingBrace(src, bodyOpen)
				if err != nil {
					return "", err
				}
				finallyBody, err = transformExceptionRegion(src[bodyOpen+1:bodyClose], context, rethrowName)
				if err != nil {
					return "", err
				}
				if err := validateFinallyControlTransfers(finallyBody, context); err != nil {
					return "", err
				}
				cursor = skipSpace(src, bodyClose+1)
			}

			if len(clauses) == 0 && finallyBody == "" {
				out.WriteString("{\n")
				out.WriteString(tryBody)
				out.WriteString("\n}")
			} else {
				tryBody, err = transformImplicitErrorPromotion(tryBody, context)
				if err != nil {
					return "", err
				}
				tryBody, err = rewriteExceptionReturns(tryBody, context)
				if err != nil {
					return "", err
				}
				for clauseIndex := range clauses {
					clauses[clauseIndex].body, err = transformImplicitErrorPromotion(clauses[clauseIndex].body, context)
					if err != nil {
						return "", err
					}
					clauses[clauseIndex].body, err = rewriteExceptionReturns(clauses[clauseIndex].body, context)
					if err != nil {
						return "", err
					}
				}
				out.WriteString(lowerTry(tryBody, clauses, finallyBody))
				out.WriteByte('\n')
			}
			index = cursor
			continue
		}

		if keywordAt(src, index, "throw") && exceptionStatementStart(src, index) {
			end, expression, err := readThrowExpression(src, index+len("throw"))
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(expression) == "" {
				if rethrowName == "" {
					return "", fmt.Errorf("bare throw is only valid inside catch")
				}
				out.WriteString("panic(" + rethrowName + ")")
			} else {
				if err := validateThrowExpression(expression, context); err != nil {
					return "", err
				}
				out.WriteString("__gppThrow(")
				out.WriteString(expression)
				out.WriteByte(')')
			}
			index = end
			continue
		}

		out.WriteByte(src[index])
		index++
	}
	return out.String(), nil
}

func exceptionStatementStart(src string, index int) bool {
	if index == 0 {
		return true
	}
	for cursor := index - 1; cursor >= 0; cursor-- {
		switch src[cursor] {
		case ' ', '\t', '\r':
			continue
		case '\n', '{', '}', ';':
			return true
		default:
			return false
		}
	}
	return true
}

func findExceptionBlockOpen(src string, start int) int {
	parenDepth := 0
	bracketDepth := 0
	for index := start; index < len(src); index++ {
		switch src[index] {
		case '"', '\'', '`':
			end, err := skipQuoted(src, index, src[index])
			if src[index] == '`' {
				end = strings.IndexByte(src[index+1:], '`')
				if end >= 0 {
					end += index + 1
				}
			}
			if err != nil || end < 0 {
				return -1
			}
			index = end
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
		case '{':
			if parenDepth == 0 && bracketDepth == 0 {
				return index
			}
		}
	}
	return -1
}

func parseCatchClause(header string, context constructorContext) (catchClause, error) {
	fields := strings.Fields(header)
	if len(fields) > 2 {
		return catchClause{}, fmt.Errorf("invalid catch clause %q", header)
	}
	clause := catchClause{}
	switch len(fields) {
	case 0:
		clause.typeName = "error"
	case 1:
		if isCatchTypeName(fields[0], context) {
			clause.typeName = fields[0]
		} else {
			clause.typeName = "error"
			clause.variable = fields[0]
		}
	case 2:
		clause.typeName = fields[0]
		clause.variable = fields[1]
	}
	if !isCatchTypeName(clause.typeName, context) {
		return catchClause{}, fmt.Errorf("invalid catch type %s", clause.typeName)
	}
	return clause, nil
}

func isCatchTypeName(name string, context constructorContext) bool {
	name = strings.TrimSpace(name)
	if name == "error" || name == "any" {
		return name == "error"
	}
	base := strings.TrimPrefix(name, "*")
	if target, ok := context.Targets[base]; ok {
		return classHasErrorMethod(target.Class, context, map[string]bool{})
	}
	parts := strings.Split(base, ".")
	if len(parts) != 2 || context.AvailableImports == nil {
		return false
	}
	importPath := context.AvailableImports[parts[0]]
	if importPath == "" {
		return false
	}
	pkg, err := importer.Default().Import(importPath)
	if err != nil {
		return false
	}
	typeName, ok := pkg.Scope().Lookup(parts[1]).(*types.TypeName)
	if !ok {
		return false
	}
	errorType := types.Universe.Lookup("error").Type()
	catchType := types.Unalias(typeName.Type())
	if strings.HasPrefix(name, "*") {
		if named, namedOK := catchType.(*types.Named); namedOK {
			catchType = types.NewPointer(named)
		}
	}
	return types.AssignableTo(catchType, errorType)
}

func validateCatchOrdering(clauses []catchClause, context constructorContext) error {
	for index := 0; index < len(clauses); index++ {
		for previous := 0; previous < index; previous++ {
			if catchTypeCovers(clauses[previous].typeName, clauses[index].typeName, context) {
				return fmt.Errorf("unreachable catch: %s is already matched by an earlier catch", clauses[index].typeName)
			}
		}
	}
	return nil
}

func catchTypeCovers(earlier, later string, context constructorContext) bool {
	earlier = strings.TrimSpace(earlier)
	later = strings.TrimSpace(later)
	if earlier == later || earlier == "error" {
		return true
	}
	// Local class inheritance is represented in the semantic model. A later
	// derived class is unreachable after an earlier parent catch.
	earlierBase := strings.TrimPrefix(earlier, "*")
	laterBase := strings.TrimPrefix(later, "*")
	earlierTarget, earlierOK := context.Targets[earlierBase]
	laterTarget, laterOK := context.Targets[laterBase]
	if !earlierOK || !laterOK {
		return false
	}
	return classInheritsTarget(laterTarget, earlierTarget, context)
}

func validateFinallyControlTransfers(body string, context constructorContext) error {
	parsed, _, _, err := parseExceptionSource(body, context)
	if err != nil {
		return nil
	}
	var transferErr error
	ast.Inspect(parsed, func(node ast.Node) bool {
		if node == nil || transferErr != nil {
			return transferErr == nil
		}
		if transferErr != nil {
			return false
		}
		switch statement := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			transferErr = fmt.Errorf("control transfer from finally is not allowed: return")
			return false
		case *ast.BranchStmt:
			switch statement.Tok {
			case token.BREAK, token.CONTINUE, token.GOTO:
				transferErr = fmt.Errorf("control transfer from finally is not allowed: %s", statement.Tok)
				return false
			}
		}
		return true
	})
	return transferErr
}

func lowerTry(tryBody string, clauses []catchClause, finallyBody string) string {
	var out strings.Builder
	out.WriteString("__gppRun(func() {\n")
	out.WriteString("defer func() {\n")
	if finallyBody != "" {
		out.WriteString("defer func() {\n")
		out.WriteString(finallyBody)
		out.WriteString("\n}()\n")
	}
	out.WriteString("__gppRecovered := recover()\n")
	out.WriteString("if __gppRecovered != nil {\n")
	out.WriteString("__gppThrown, __gppIsThrown := __gppRecovered.(__gppThrownError)\n")
	out.WriteString("if !__gppIsThrown {\n")
	out.WriteString("panic(__gppRecovered)\n")
	out.WriteString("}\n")
	out.WriteString("__gppHandled := false\n")
	out.WriteString("if __gppThrown.err != nil {\n")
	hasCatchVariable := false
	for _, clause := range clauses {
		if clause.variable != "" {
			hasCatchVariable = true
			break
		}
	}
	if hasCatchVariable {
		out.WriteString("switch __gppCaught := __gppThrown.err.(type) {\n")
	} else {
		out.WriteString("switch __gppThrown.err.(type) {\n")
	}
	for _, clause := range clauses {
		out.WriteString("case ")
		out.WriteString(clause.typeName)
		out.WriteString(":\n")
		out.WriteString("__gppHandled = true\n")
		if clause.variable != "" {
			out.WriteString(clause.variable)
			out.WriteString(" := __gppCaught\n")
		}
		out.WriteString("{\n")
		out.WriteString(clause.body)
		out.WriteString("\n}\n")
	}
	out.WriteString("}\n")
	out.WriteString("}\n")
	out.WriteString("if !__gppHandled {\n")
	out.WriteString("panic(__gppRecovered)\n")
	out.WriteString("}\n")
	out.WriteString("}\n")
	out.WriteString("}()\n")
	out.WriteString(tryBody)
	out.WriteString("\n})")
	return out.String()
}

func rewriteExceptionReturns(body string, context constructorContext) (string, error) {
	parsed, fileSet, prefixLength, err := parseExceptionSource(body, context)
	if err != nil {
		return body, nil
	}
	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		returnStmt, ok := node.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		text := sourceNodeText(returnStmt, fileSet, prefixLength, body)
		if text == "" {
			return true
		}
		expression := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "return"))
		replacement := "panic(__gppExceptionReturn{})"
		if expression != "" {
			replacement = "panic(__gppExceptionReturn{values: []any{" + expression + "}})"
		}
		start := fileSet.Position(returnStmt.Pos()).Offset - prefixLength
		end := fileSet.Position(returnStmt.End()).Offset - prefixLength
		if start >= 0 && end <= len(body) {
			edits = append(edits, edit{start: start, end: end, text: replacement})
		}
		return true
	})
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		body = body[:edit.start] + edit.text + body[edit.end:]
	}
	return body, nil
}

func readThrowExpression(src string, start int) (int, string, error) {
	index := start
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for index < len(src) {
		switch src[index] {
		case '"', '\'':
			end, err := skipQuoted(src, index, src[index])
			if err != nil {
				return 0, "", err
			}
			index = end + 1
			continue
		case '`':
			end := strings.IndexByte(src[index+1:], '`')
			if end < 0 {
				return 0, "", fmt.Errorf("unterminated raw string")
			}
			index += end + 2
			continue
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			} else if parenDepth == 0 && bracketDepth == 0 {
				return index, strings.TrimSpace(src[start:index]), nil
			}
		case ';', '\n':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return index, strings.TrimSpace(src[start:index]), nil
			}
		}
		index++
	}
	return index, strings.TrimSpace(src[start:index]), nil
}

func validateThrowExpression(expression string, context constructorContext) error {
	expression = strings.TrimSpace(expression)
	if expression == "nil" {
		return nil
	}
	if len(expression) > 0 && (expression[0] == '"' || expression[0] == '\'') {
		return fmt.Errorf("cannot throw string; thrown value must implement error")
	}
	name, length := readIdent(expression)
	if length > 0 {
		if length == len(expression) && (name == "true" || name == "false") {
			return fmt.Errorf("cannot throw bool; thrown value must implement error")
		}
		if open := skipSpace(expression, length); open < len(expression) && expression[open] == '(' {
			if target, ok := context.Targets[name]; ok && !classHasErrorMethod(target.Class, context, map[string]bool{}) {
				return fmt.Errorf("cannot throw %s; thrown value must implement error", name)
			}
		}
	}
	return nil
}

func classHasErrorMethod(class *ClassDecl, context constructorContext, visiting map[string]bool) bool {
	if class == nil || visiting[class.Name] {
		return false
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)
	for _, method := range class.Methods {
		if !method.IsStatic && method.Name == "Error" && method.Parameters == "" && strings.TrimSpace(method.Result) == "string" {
			return true
		}
	}
	for _, parent := range class.Parents {
		if target, ok := context.Targets[parent]; ok && classHasErrorMethod(target.Class, context, visiting) {
			return true
		}
	}
	return false
}

type promotedCall struct {
	types         []string
	trailingError bool
}

func transformImplicitErrorPromotion(src string, context constructorContext) (string, error) {
	parsed, fileSet, prefixLength, err := parseExceptionSource(src, context)
	if err != nil {
		return src, nil
	}
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(parsed, func(node ast.Node) bool {
		if node == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})

	type edit struct {
		start, end int
		text       string
	}
	edits := []edit{}
	valueTypes := polymorphicValueTypes(parsed, context)
	var promotionErr error
	ast.Inspect(parsed, func(node ast.Node) bool {
		if promotionErr != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		result, ok := promotedCallFor(call, context, valueTypes)
		if !ok || !result.trailingError || len(result.types) == 0 {
			return true
		}
		parent := parents[call]
		if _, isGoroutineCall := parent.(*ast.GoStmt); isGoroutineCall {
			promotionErr = fmt.Errorf("cannot implicitly propagate error from goroutine call; handle the error inside the goroutine")
			return true
		}
		if explicitlyCapturedError(call, parent, len(result.types)) {
			return true
		}
		replacement := ""
		discard := false
		if _, isStatement := parent.(*ast.ExprStmt); isStatement {
			discard = true
		} else if len(result.types) > 1 &&
			!callHasExpectedReducedResults(parent, len(result.types)-1) &&
			!callRequiresSingleValue(parent) {
			return true
		}
		nonErrorCount := len(result.types) - 1
		// An error-only result is still a value when it appears in an
		// expression context (for example, `done <- Save()`). It is omitted
		// only for a standalone statement.
		if nonErrorCount == 0 && !discard {
			return true
		}
		if discard {
			switch nonErrorCount {
			case 0:
				replacement = "__gppThrow(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			case 1:
				replacement = "__gppDiscard(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			case 2:
				replacement = "__gppDiscard2(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			case 3:
				replacement = "__gppDiscard3(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			default:
				replacement = inlineUnwrapCall(sourceNodeText(call, fileSet, prefixLength, src), result.types, true, context)
			}
		} else {
			switch nonErrorCount {
			case 1:
				replacement = "__gppUnwrap(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			case 2:
				replacement = "__gppUnwrap2(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			case 3:
				replacement = "__gppUnwrap3(" + sourceNodeText(call, fileSet, prefixLength, src) + ")"
			default:
				replacement = inlineUnwrapCall(sourceNodeText(call, fileSet, prefixLength, src), result.types, false, context)
			}
		}
		if replacement == "" {
			return true
		}
		start := fileSet.Position(call.Pos()).Offset - prefixLength
		end := fileSet.Position(call.End()).Offset - prefixLength
		if start >= 0 && end <= len(src) {
			edits = append(edits, edit{start: start, end: end, text: replacement})
		}
		return true
	})
	if promotionErr != nil {
		return src, promotionErr
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, nil
}

func inlineUnwrapCall(callText string, resultTypes []string, discard bool, context constructorContext) string {
	nonErrorCount := len(resultTypes) - 1
	valueNames := make([]string, nonErrorCount)
	for index := range valueNames {
		valueNames[index] = fmt.Sprintf("__gppExceptionValue%d", index)
	}
	errorName := "__gppExceptionError"
	assignmentNames := append([]string{}, valueNames...)
	if discard {
		for index := range assignmentNames {
			assignmentNames[index] = "_"
		}
	}
	assignmentNames = append(assignmentNames, errorName)
	assignment := strings.Join(assignmentNames, ", ") + " := " + callText
	if discard {
		return "func() { " + assignment + "; __gppThrow(" + errorName + ") }()"
	}
	resultTypesText := make([]string, nonErrorCount)
	for index, resultType := range resultTypes[:nonErrorCount] {
		resultTypesText[index] = transformPolymorphicType(resultType, context)
	}
	return "func() (" + strings.Join(resultTypesText, ", ") + ") { " + assignment + "; __gppThrow(" + errorName + "); return " + strings.Join(valueNames, ", ") + " }()"
}

type exceptionResultField struct {
	name     string
	typeName string
}

func transformExceptionABIBoundaries(src string, context constructorContext) (string, error) {
	parsed, fileSet, prefixLength, err := parseExceptionSource(src, context)
	if err != nil {
		return src, nil
	}
	type replacement struct {
		start int
		end   int
		text  string
	}
	replacements := []replacement{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		function, ok := node.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			return true
		}
		used := map[string]bool{}
		ast.Inspect(function, func(node ast.Node) bool {
			if identifier, ok := node.(*ast.Ident); ok {
				used[identifier.Name] = true
			}
			return true
		})
		bodyStart := fileSet.Position(function.Body.Lbrace).Offset - prefixLength + 1
		bodyEnd := fileSet.Position(function.Body.Rbrace).Offset - prefixLength
		if bodyStart < 0 || bodyEnd < bodyStart || bodyEnd > len(src) {
			return true
		}
		bodyText := src[bodyStart:bodyEnd]
		fields, hasResults := exceptionResultFields(function.Type.Results)
		if !hasResults {
			if !strings.Contains(bodyText, "__gppExceptionReturn") {
				return true
			}
			bodyText, _ = rewriteExceptionReturns(bodyText, context)
			replacements = append(replacements, replacement{
				start: bodyStart,
				end:   bodyEnd,
				text:  exceptionVoidBoundaryBody(bodyText, nextExceptionName(used, "__gppBoundaryReturned"), used),
			})
			return true
		}
		isErrorBoundary := fields[len(fields)-1].typeName == "error"
		if !isErrorBoundary && !strings.Contains(bodyText, "__gppExceptionReturn") {
			return true
		}
		if strings.Contains(bodyText, "__gppExceptionReturn") {
			bodyText, _ = rewriteExceptionReturns(bodyText, context)
		}
		for index := range fields {
			if fields[index].name == "" || fields[index].name == "_" {
				fields[index].name = nextExceptionName(used, fmt.Sprintf("__gppBoundaryResult%d", index))
			}
		}
		recoverName := nextExceptionName(used, "__gppBoundaryRecovered")
		thrownName := nextExceptionName(used, "__gppBoundaryThrown")
		okName := nextExceptionName(used, "__gppBoundaryIsThrown")
		opening := exceptionReturnBoundaryOpening(fields, recoverName, thrownName, okName)
		if isErrorBoundary {
			opening = exceptionBoundaryOpening(fields, recoverName, thrownName, okName)
		}
		replacements = append(replacements, replacement{
			start: bodyStart,
			end:   bodyEnd,
			text:  opening + bodyText + "\nreturn\n}()",
		})
		return true
	})
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	for _, replacement := range replacements {
		src = src[:replacement.start] + replacement.text + src[replacement.end:]
	}
	return src, nil
}

func exceptionVoidBoundaryBody(body, returnedName string, used map[string]bool) string {
	recoverName := nextExceptionName(used, "__gppBoundaryRecovered")
	okName := nextExceptionName(used, "__gppIsReturned")
	var out strings.Builder
	out.WriteString("var ")
	out.WriteString(returnedName)
	out.WriteString(" bool\n")
	out.WriteString("func() {\n")
	out.WriteString("defer func() {\n")
	out.WriteString(recoverName)
	out.WriteString(" := recover()\n")
	out.WriteString("if ")
	out.WriteString(recoverName)
	out.WriteString(" != nil {\n")
	out.WriteString("_, ")
	out.WriteString(okName)
	out.WriteString(" := ")
	out.WriteString(recoverName)
	out.WriteString(".(__gppExceptionReturn)\n")
	out.WriteString("if !")
	out.WriteString(okName)
	out.WriteString(" { panic(")
	out.WriteString(recoverName)
	out.WriteString(") }\n")
	out.WriteString(returnedName)
	out.WriteString(" = true\n")
	out.WriteString("}\n")
	out.WriteString("}()\n")
	out.WriteString(body)
	out.WriteString("\n}()\n")
	out.WriteString("if ")
	out.WriteString(returnedName)
	out.WriteString(" { return }")
	return out.String()
}

func exceptionResultFields(results *ast.FieldList) ([]exceptionResultField, bool) {
	if results == nil {
		return nil, false
	}
	fields := []exceptionResultField{}
	for _, field := range results.List {
		typeName, err := formatNode(field.Type)
		if err != nil {
			return nil, false
		}
		if len(field.Names) == 0 {
			fields = append(fields, exceptionResultField{typeName: strings.TrimSpace(typeName)})
			continue
		}
		for _, name := range field.Names {
			fields = append(fields, exceptionResultField{name: name.Name, typeName: strings.TrimSpace(typeName)})
		}
	}
	return fields, true
}

func exceptionResultFieldsFromText(result string) ([]exceptionResultField, bool) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "result.go", "package main\nfunc __gpp_result() "+strings.TrimSpace(result)+" {}", 0)
	if err != nil || len(parsed.Decls) != 1 {
		return nil, false
	}
	function, ok := parsed.Decls[0].(*ast.FuncDecl)
	if !ok {
		return nil, false
	}
	return exceptionResultFields(function.Type.Results)
}

func wrapExceptionBoundaryBody(body, result string, context constructorContext) (string, bool) {
	fields, ok := exceptionResultFieldsFromText(result)
	if !ok {
		return body, false
	}
	if len(fields) == 0 {
		if !strings.Contains(body, "__gppExceptionReturn") {
			return body, false
		}
		body, _ = rewriteExceptionReturns(body, context)
		used := map[string]bool{}
		returnedName := nextExceptionName(used, "__gppBoundaryReturned")
		return exceptionVoidBoundaryBody(body, returnedName, used), true
	}
	isErrorBoundary := fields[len(fields)-1].typeName == "error"
	if !isErrorBoundary && !strings.Contains(body, "__gppExceptionReturn") {
		return body, false
	}
	if strings.Contains(body, "__gppExceptionReturn") {
		body, _ = rewriteExceptionReturns(body, context)
	}
	used := map[string]bool{}
	if parsed, err := parser.ParseFile(token.NewFileSet(), "body.go", "package main\nfunc __gpp_body() {\n"+body+"\n}", 0); err == nil {
		ast.Inspect(parsed, func(node ast.Node) bool {
			if identifier, ok := node.(*ast.Ident); ok {
				used[identifier.Name] = true
			}
			return true
		})
	}
	for index := range fields {
		fields[index].typeName = transformPolymorphicType(fields[index].typeName, context)
		if fields[index].name == "" || fields[index].name == "_" {
			fields[index].name = nextExceptionName(used, fmt.Sprintf("__gppBoundaryResult%d", index))
		}
	}
	recoverName := nextExceptionName(used, "__gppBoundaryRecovered")
	thrownName := nextExceptionName(used, "__gppBoundaryThrown")
	okName := nextExceptionName(used, "__gppBoundaryIsThrown")
	opening := exceptionReturnBoundaryOpening(fields, recoverName, thrownName, okName)
	if isErrorBoundary {
		opening = exceptionBoundaryOpening(fields, recoverName, thrownName, okName)
	}
	return opening + body + "\nreturn\n}()", true
}

func nextExceptionName(used map[string]bool, base string) string {
	name := base
	for suffix := 1; used[name]; suffix++ {
		name = fmt.Sprintf("%s_%d", base, suffix)
	}
	used[name] = true
	return name
}

func exceptionBoundaryOpening(fields []exceptionResultField, recoverName, thrownName, okName string) string {
	resultParts := make([]string, len(fields))
	for index, field := range fields {
		resultParts[index] = field.name + " " + field.typeName
	}
	var out strings.Builder
	out.WriteString("return func() (")
	out.WriteString(strings.Join(resultParts, ", "))
	out.WriteString(") {\n")
	out.WriteString("defer func() {\n")
	out.WriteString(recoverName)
	out.WriteString(" := recover()\n")
	out.WriteString("if ")
	out.WriteString(recoverName)
	out.WriteString(" != nil {\n")
	out.WriteString("if __gppReturned, __gppIsReturned := ")
	out.WriteString(recoverName)
	out.WriteString(".(__gppExceptionReturn); __gppIsReturned {\n")
	out.WriteString("if len(__gppReturned.values) == 0 { return }\n")
	for index, field := range fields {
		out.WriteString(field.name)
		out.WriteString(" = __gppReturned.values[")
		out.WriteString(fmt.Sprintf("%d", index))
		out.WriteString("].(")
		out.WriteString(field.typeName)
		out.WriteString(")\n")
	}
	out.WriteString("return\n")
	out.WriteString("}\n")
	out.WriteString(thrownName)
	out.WriteString(", ")
	out.WriteString(okName)
	out.WriteString(" := ")
	out.WriteString(recoverName)
	out.WriteString(".(__gppThrownError)\n")
	out.WriteString("if !")
	out.WriteString(okName)
	out.WriteString(" { panic(")
	out.WriteString(recoverName)
	out.WriteString(") }\n")
	for index, field := range fields[:len(fields)-1] {
		zeroName := fmt.Sprintf("__gppBoundaryZero%d", index)
		out.WriteString("var ")
		out.WriteString(zeroName)
		out.WriteByte(' ')
		out.WriteString(field.typeName)
		out.WriteByte('\n')
		out.WriteString(field.name)
		out.WriteString(" = ")
		out.WriteString(zeroName)
		out.WriteByte('\n')
	}
	out.WriteString(fields[len(fields)-1].name)
	out.WriteString(" = ")
	out.WriteString(thrownName)
	out.WriteString(".err\n")
	out.WriteString("}\n")
	out.WriteString("}()\n")
	return out.String()
}

func exceptionReturnBoundaryOpening(fields []exceptionResultField, recoverName, _, _ string) string {
	resultParts := make([]string, len(fields))
	for index, field := range fields {
		resultParts[index] = field.name + " " + field.typeName
	}
	var out strings.Builder
	out.WriteString("return func() (")
	out.WriteString(strings.Join(resultParts, ", "))
	out.WriteString(") {\n")
	out.WriteString("defer func() {\n")
	out.WriteString(recoverName)
	out.WriteString(" := recover()\n")
	out.WriteString("if ")
	out.WriteString(recoverName)
	out.WriteString(" != nil {\n")
	out.WriteString("__gppReturned, __gppIsReturned := ")
	out.WriteString(recoverName)
	out.WriteString(".(__gppExceptionReturn)\n")
	out.WriteString("if !__gppIsReturned { panic(")
	out.WriteString(recoverName)
	out.WriteString(") }\n")
	out.WriteString("if len(__gppReturned.values) == 0 { return }\n")
	for index, field := range fields {
		out.WriteString(field.name)
		out.WriteString(" = __gppReturned.values[")
		out.WriteString(fmt.Sprintf("%d", index))
		out.WriteString("].(")
		out.WriteString(field.typeName)
		out.WriteString(")\n")
	}
	out.WriteString("}\n")
	out.WriteString("}()\n")
	return out.String()
}

func parseExceptionSource(src string, context constructorContext) (*ast.File, *token.FileSet, int, error) {
	fileSet := token.NewFileSet()
	const filePrefix = "package main\n\n"
	parsed, err := parser.ParseFile(fileSet, "generated.go", filePrefix+src, 0)
	if err == nil {
		return parsed, fileSet, len(filePrefix), nil
	}
	functionPrefix := filePrefix + "func __gpp_scope()"
	if strings.TrimSpace(context.CurrentResult) != "" {
		functionPrefix += " " + strings.TrimSpace(context.CurrentResult)
	}
	functionPrefix += " {\n"
	functionSet := token.NewFileSet()
	parsed, err = parser.ParseFile(functionSet, "generated.go", functionPrefix+src+"\n}", 0)
	if err != nil {
		return nil, nil, 0, err
	}
	return parsed, functionSet, len(functionPrefix), nil
}

func sourceNodeText(node ast.Node, fileSet *token.FileSet, prefixLength int, src string) string {
	start := fileSet.Position(node.Pos()).Offset - prefixLength
	end := fileSet.Position(node.End()).Offset - prefixLength
	if start < 0 || end > len(src) || start > end {
		return ""
	}
	return src[start:end]
}

func explicitlyCapturedError(call *ast.CallExpr, parent ast.Node, resultCount int) bool {
	switch statement := parent.(type) {
	case *ast.AssignStmt:
		return len(statement.Rhs) == 1 && len(statement.Lhs) == resultCount
	case *ast.ValueSpec:
		return len(statement.Values) == 1 && len(statement.Names) == resultCount
	case *ast.ReturnStmt:
		return len(statement.Results) == resultCount
	default:
		return false
	}
}

func callHasExpectedReducedResults(parent ast.Node, reducedCount int) bool {
	switch statement := parent.(type) {
	case *ast.AssignStmt:
		return len(statement.Rhs) == 1 && len(statement.Lhs) == reducedCount
	case *ast.ValueSpec:
		return len(statement.Values) == 1 && len(statement.Names) == reducedCount
	case *ast.ReturnStmt:
		return len(statement.Results) == reducedCount
	default:
		return false
	}
}

func callRequiresSingleValue(parent ast.Node) bool {
	switch parent.(type) {
	case *ast.BinaryExpr, *ast.UnaryExpr, *ast.ParenExpr,
		*ast.SelectorExpr, *ast.IndexExpr, *ast.SliceExpr,
		*ast.TypeAssertExpr, *ast.KeyValueExpr:
		return true
	default:
		return false
	}
}

func promotedCallFor(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) (promotedCall, bool) {
	var candidates []callableSignature
	switch function := call.Fun.(type) {
	case *ast.Ident:
		candidates = append(candidates, context.FunctionSignatures[function.Name]...)
		for _, extension := range context.Extensions {
			if extension.GoName == function.Name {
				candidates = append(candidates, callableSignature{
					Parameters: extensionCallParameters(extension),
					Result:     strings.TrimSpace(extension.Method.Result),
				})
			}
		}
	case *ast.SelectorExpr:
		if receiver, ok := function.X.(*ast.Ident); ok {
			if _, isClass := context.Targets[receiver.Name]; isClass {
				candidates = append(candidates, context.StaticMethodSignatures[receiver.Name][function.Sel.Name]...)
			} else {
				className := context.CurrentClass
				if receiver.Name != "this" {
					className = strings.TrimPrefix(valueTypes[receiver.Name], "*")
				}
				candidates = append(candidates, context.ClassMethodSignatures[className][function.Sel.Name]...)
			}
			hasQualifiedExtension := false
			for _, extension := range context.Extensions {
				if extension.Qualifier == receiver.Name && extension.GoName == function.Sel.Name {
					hasQualifiedExtension = true
					candidates = append(candidates, callableSignature{
						Parameters: extensionCallParameters(extension),
						Result:     strings.TrimSpace(extension.Method.Result),
					})
				}
			}
			if !hasQualifiedExtension {
				if native, ok := nativePackageFunction(call, context); ok {
					return native, true
				}
				if native, ok := nativeMethodFunction(call, context, valueTypes); ok {
					return native, true
				}
			}
		}
	}
	for _, candidate := range candidates {
		if len(call.Args) < requiredParameterCount(candidate) || len(call.Args) > len(candidate.Parameters) {
			continue
		}
		resultTypes := resultTypesFromText(candidate.Result)
		if len(resultTypes) == 0 || !isErrorLikeType(resultTypes[len(resultTypes)-1], context) {
			continue
		}
		return promotedCall{types: resultTypes, trailingError: true}, true
	}
	return promotedCall{}, false
}

func mustParameters(source string) []parameterInfo {
	parameters, err := parseParameterInfos(source)
	if err != nil {
		return nil
	}
	return parameters
}

func extensionCallParameters(extension extensionMethod) []parameterInfo {
	parameters := []parameterInfo{{Name: "this", Type: extension.ReceiverType}}
	return append(parameters, mustParameters(extension.Method.Parameters)...)
}

func resultTypesFromText(result string) []string {
	result = strings.TrimSpace(result)
	if result == "" {
		return nil
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), "result.go", "package main\nfunc __gpp_result() "+result+" {}", 0)
	if err != nil || len(parsed.Decls) != 1 {
		return nil
	}
	function, ok := parsed.Decls[0].(*ast.FuncDecl)
	if !ok || function.Type.Results == nil {
		return nil
	}
	values := []string{}
	for _, field := range function.Type.Results.List {
		typeName, err := formatNode(field.Type)
		if err != nil {
			return nil
		}
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			values = append(values, strings.TrimSpace(typeName))
		}
	}
	return values
}

func isErrorLikeType(typeName string, context constructorContext) bool {
	typeName = strings.TrimSpace(typeName)
	if typeName == "error" {
		return true
	}
	base := strings.TrimPrefix(typeName, "*")
	if target, ok := context.Targets[base]; ok {
		return classHasErrorMethod(target.Class, context, map[string]bool{})
	}
	return false
}

func nativePackageFunction(call *ast.CallExpr, context constructorContext) (promotedCall, bool) {
	signature, ok := nativePackageFunctionSignature(call, context)
	if !ok {
		return promotedCall{}, false
	}
	return promotedCallFromNativeSignature(signature)
}

func nativePackageFunctionSignature(call *ast.CallExpr, context constructorContext) (*types.Signature, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	receiver, ok := selector.X.(*ast.Ident)
	if !ok {
		return nil, false
	}
	importPath := ""
	if context.AvailableImports != nil {
		importPath = context.AvailableImports[receiver.Name]
	}
	if importPath == "" {
		// A standalone Emit call may not have a package import table yet. The
		// standard importer still gives us a useful fallback for conventional
		// package-qualified Go calls such as os.ReadFile.
		importPath = receiver.Name
	}
	pkg, err := importer.Default().Import(importPath)
	if err != nil {
		return nil, false
	}
	object := pkg.Scope().Lookup(selector.Sel.Name)
	function, ok := object.(*types.Func)
	if !ok {
		return nil, false
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Results() == nil || signature.Results().Len() == 0 {
		return nil, false
	}
	return signature, true
}

func nativeMethodFunction(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) (promotedCall, bool) {
	signature, ok := nativeMethodSignature(call, context, valueTypes)
	if !ok {
		return promotedCall{}, false
	}
	return promotedCallFromNativeSignature(signature)
}

func nativeMethodSignature(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) (*types.Signature, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	typeName := expressionStaticType(selector.X, context, valueTypes)
	named, pkg, ok := nativeNamedType(typeName, context)
	if !ok {
		return nil, false
	}
	method := types.NewMethodSet(types.NewPointer(named)).Lookup(pkg, selector.Sel.Name)
	if method == nil {
		return nil, false
	}
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Results() == nil || signature.Results().Len() == 0 {
		return nil, false
	}
	return signature, true
}

func nativePackageResultTypes(call *ast.CallExpr, context constructorContext) ([]string, bool) {
	signature, ok := nativePackageFunctionSignature(call, context)
	if !ok {
		return nil, false
	}
	return nativeSignatureResultTypes(signature, context), true
}

func nativeMethodResultTypes(call *ast.CallExpr, context constructorContext, valueTypes map[string]string) ([]string, bool) {
	signature, ok := nativeMethodSignature(call, context, valueTypes)
	if !ok {
		return nil, false
	}
	return nativeSignatureResultTypes(signature, context), true
}

func nativeSignatureResultTypes(signature *types.Signature, context constructorContext) []string {
	if signature == nil || signature.Results() == nil || signature.Results().Len() == 0 {
		return nil
	}
	result := make([]string, signature.Results().Len())
	for index := range result {
		result[index] = types.TypeString(signature.Results().At(index).Type(), nativeTypeQualifier(context))
	}
	return result
}

func nativeNamedType(typeName string, context constructorContext) (*types.Named, *types.Package, bool) {
	typeName = strings.TrimPrefix(strings.TrimSpace(typeName), "*")
	separator := strings.LastIndex(typeName, ".")
	if separator <= 0 || separator+1 >= len(typeName) {
		return nil, nil, false
	}
	packageName := typeName[:separator]
	objectName := typeName[separator+1:]
	importPath := packageName
	if context.AvailableImports != nil {
		if resolved := context.AvailableImports[packageName]; resolved != "" {
			importPath = resolved
		}
	}
	pkg, err := importer.Default().Import(importPath)
	if err != nil {
		return nil, nil, false
	}
	object, ok := pkg.Scope().Lookup(objectName).(*types.TypeName)
	if !ok {
		return nil, nil, false
	}
	named, ok := object.Type().(*types.Named)
	if !ok {
		return nil, nil, false
	}
	return named, pkg, true
}

func nativeTypeQualifier(context constructorContext) func(*types.Package) string {
	return func(pkg *types.Package) string {
		if pkg == nil {
			return ""
		}
		for alias, importPath := range context.AvailableImports {
			if importPath == pkg.Path() {
				return alias
			}
		}
		return pkg.Path()
	}
}

func promotedCallFromNativeSignature(signature *types.Signature) (promotedCall, bool) {
	if signature == nil || signature.Results() == nil || signature.Results().Len() == 0 {
		return promotedCall{}, false
	}
	results := []string{}
	for index := 0; index < signature.Results().Len(); index++ {
		results = append(results, types.TypeString(signature.Results().At(index).Type(), nil))
	}
	errorType := types.Universe.Lookup("error").Type()
	if !types.AssignableTo(signature.Results().At(signature.Results().Len()-1).Type(), errorType) {
		return promotedCall{}, false
	}
	return promotedCall{types: results, trailingError: true}, true
}
