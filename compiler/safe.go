package compiler

import (
	"fmt"
	"go/ast"
	"strings"
)

func fileHasSafeAccess(file *File) bool {
	for _, decl := range file.Decls {
		switch value := decl.(type) {
		case *MixedDecl:
			if tokenSequence(mixedDeclTokens(value), "?.") {
				return true
			}
		case *FunctionDecl:
			if tokenSequence(methodBodyTokens(value.Method), "?.") {
				return true
			}
		case *ValueDecl:
			if tokenSequence(valueDeclTokens(value), "?.") {
				return true
			}
		case *ClassDecl:
			for _, method := range value.Methods {
				if tokenSequence(methodBodyTokens(method), "?.") {
					return true
				}
			}
		case *ExtendDecl:
			for _, method := range value.Methods {
				if tokenSequence(methodBodyTokens(method), "?.") {
					return true
				}
			}
		}
	}
	return false
}

func transformSafeAccess(src string, context constructorContext) (string, error) {
	tokens, err := LexSource("safe-access", src)
	if err != nil || !tokenSequence(tokens, "?.") {
		return src, nil
	}
	if transformed, handled, transformErr := transformSafeAccessAST(src, context); handled {
		return transformed, transformErr
	}
	return "", fmt.Errorf("safe access expression could not be represented by the body AST")
}

func transformSafeAccessAST(src string, context constructorContext) (string, bool, error) {
	tokens, err := LexSource("safe-access", src)
	if err != nil {
		return src, false, nil
	}
	edits := []safeAccessEdit{}
	blocks := []struct {
		body    *BlockStmt
		context constructorContext
		types   map[string]string
	}{}
	// Prefer structured top-level function bodies. ParseBodyAST deliberately
	// keeps unknown declaration-shaped input as a TokenStmt, which would lose
	// the function's parameter types when this helper receives a full source
	// region.
	functions := parseTopLevelFunctions("safe-access", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function == nil || function.Method.BodyAST == nil {
				continue
			}
			functionContext := context
			functionContext.CurrentParameterTypes = parameterTypeMap(methodParametersSource(function.Method))
			bodySource := methodBodySource(function.Method)
			if bodySource == "" && function.Method.BodySpan.Start >= 0 && function.Method.BodySpan.End <= len(src) {
				bodySource = src[function.Method.BodySpan.Start:function.Method.BodySpan.End]
			}
			types := safeValueTypes(bodySource)
			for name, typeName := range functionContext.CurrentParameterTypes {
				types[name] = typeName
			}
			if functionContext.CurrentClass != "" {
				types["this"] = functionContext.CurrentClass
			}
			blocks = append(blocks, struct {
				body    *BlockStmt
				context constructorContext
				types   map[string]string
			}{function.Method.BodyAST, functionContext, types})
		}
	} else if body, parseErr := ParseBodyAST(tokens); parseErr == nil && body != nil {
		types := safeValueTypes(src)
		for name, typeName := range context.CurrentParameterTypes {
			types[name] = typeName
		}
		if context.CurrentClass != "" {
			types["this"] = context.CurrentClass
		}
		blocks = append(blocks, struct {
			body    *BlockStmt
			context constructorContext
			types   map[string]string
		}{body, context, types})
	}
	if len(blocks) == 0 {
		return src, false, nil
	}
	for _, block := range blocks {
		if err := collectSafeAccessEdits(block.body, src, block.context, block.types, &edits); err != nil {
			return src, true, err
		}
	}
	if len(edits) == 0 {
		return src, false, nil
	}
	for left := 0; left < len(edits); left++ {
		for right := left + 1; right < len(edits); right++ {
			if edits[right].Start < edits[left].Start {
				edits[left], edits[right] = edits[right], edits[left]
			}
		}
	}
	for index := 1; index < len(edits); index++ {
		if edits[index-1].End > edits[index].Start {
			return src, false, nil
		}
	}
	for index := len(edits) - 1; index >= 0; index-- {
		edit := edits[index]
		if edit.Start < 0 || edit.End > len(src) || edit.Start > edit.End {
			return src, false, nil
		}
		src = src[:edit.Start] + edit.Text + src[edit.End:]
	}
	remaining, lexErr := LexSource("safe-access-result", src)
	if lexErr != nil || tokenSequence(remaining, "?.") {
		return src, false, nil
	}
	return src, true, nil
}

type safeAccessEdit struct {
	Start int
	End   int
	Text  string
}

func collectSafeAccessEdits(block *BlockStmt, src string, context constructorContext, types map[string]string, edits *[]safeAccessEdit) error {
	if block == nil {
		return nil
	}
	for _, statement := range block.Statements {
		var first error
		collectSafeStmtExpressions(statement, func(expression ExprNode) {
			if first != nil {
				return
			}
			first = collectSafeExprEdits(expression, src, context, types, edits)
		})
		if first != nil {
			return first
		}
	}
	return nil
}

// TokenStmt is a token-backed compatibility fallback, but its expressions
// are already typed. Safe access lowering walks those nodes and only retains
// the wrapper as a source-span/trivia carrier.
func collectSafeStmtExpressions(statement Stmt, visit func(ExprNode)) {
	if statement == nil {
		return
	}
	switch value := statement.(type) {
	case *TokenStmt:
		for _, expression := range value.Exprs {
			visit(expression)
		}
		collectSafeBlockExpressions(value.Body, visit)
		for _, child := range value.Children {
			collectSafeStmtExpressions(child, visit)
		}
	case *ExpressionStmt:
		visit(value.Expression)
	case *DeclarationStmt:
		for _, expression := range value.Values {
			visit(expression)
		}
	case *AssignmentStmt:
		for _, expression := range append(value.Left, value.Right...) {
			visit(expression)
		}
	case *ReturnStmt:
		for _, expression := range value.Values {
			visit(expression)
		}
	case *ThrowStmt:
		visit(value.Value)
	case *DeferStmt:
		visit(value.Expression)
	case *GoStmt:
		visit(value.Expression)
	case *IfStmt:
		visit(value.Init)
		visit(value.Condition)
		collectSafeBlockExpressions(value.Body, visit)
		collectSafeBlockExpressions(value.Else, visit)
	case *TryStmt:
		collectSafeBlockExpressions(value.Body, visit)
		for _, clause := range value.Catches {
			collectSafeBlockExpressions(clause.Body, visit)
		}
		collectSafeBlockExpressions(value.Finally, visit)
	case *ForStmt:
		visit(value.Init)
		visit(value.Condition)
		visit(value.Post)
		visit(value.RangeExpr)
		collectSafeBlockExpressions(value.Body, visit)
	case *SwitchStmt:
		visit(value.Init)
		visit(value.Tag)
		collectSafeBlockExpressions(value.Body, visit)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			visit(expression)
		}
		collectSafeBlockExpressions(value.Clause.Body, visit)
	case *BlockStmt:
		collectSafeBlockExpressions(value, visit)
	}
}

func collectSafeStructuredHeader(statement Stmt, visit func(ExprNode)) {
	switch value := statement.(type) {
	case *IfStmt:
		visit(value.Condition)
	case *ForStmt:
		visit(value.Init)
		visit(value.Condition)
		visit(value.Post)
		visit(value.RangeExpr)
	case *SwitchStmt:
		visit(value.Tag)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			visit(expression)
		}
	}
}

func collectSafeBlockExpressions(block *BlockStmt, visit func(ExprNode)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectSafeStmtExpressions(statement, visit)
	}
}

func collectSafeExprEdits(expression ExprNode, src string, context constructorContext, types map[string]string, edits *[]safeAccessEdit) error {
	if expression == nil {
		return nil
	}
	var visit func(ExprNode) error
	visit = func(current ExprNode) error {
		if current == nil {
			return nil
		}
		switch value := current.(type) {
		case *CallExpr:
			if selector, ok := value.Callee.(*SelectorExpr); ok && selector.Safe {
				edit, err := safeAccessEditForSelector(selector, value, src, context, types)
				if err != nil {
					return err
				}
				*edits = append(*edits, edit)
				if err := visit(selector.Receiver); err != nil {
					return err
				}
				for _, argument := range value.Arguments {
					if err := visit(argument.Value); err != nil {
						return err
					}
				}
				return nil
			}
			if err := visit(value.Callee); err != nil {
				return err
			}
			for _, argument := range value.Arguments {
				if err := visit(argument.Value); err != nil {
					return err
				}
			}
		case *SelectorExpr:
			if value.Safe {
				edit, err := safeAccessEditForSelector(value, nil, src, context, types)
				if err != nil {
					return err
				}
				*edits = append(*edits, edit)
			}
			return visit(value.Receiver)
		case *UnaryExpr:
			return visit(value.Operand)
		case *BinaryExpr:
			if err := visit(value.Left); err != nil {
				return err
			}
			return visit(value.Right)
		case *IndexExpr:
			if err := visit(value.Receiver); err != nil {
				return err
			}
			return visit(value.Index)
		case *IndexListExpr:
			if err := visit(value.Receiver); err != nil {
				return err
			}
			for _, index := range value.Indices {
				if err := visit(index); err != nil {
					return err
				}
			}
		case *SliceExpr:
			if err := visit(value.Receiver); err != nil {
				return err
			}
			if err := visit(value.Low); err != nil {
				return err
			}
			if err := visit(value.High); err != nil {
				return err
			}
			return visit(value.Max)
		case *TypeAssertExpr:
			return visit(value.Expression)
		case *PostfixExpr:
			return visit(value.Expression)
		case *SpreadExpr:
			return visit(value.Expression)
		case *TypeExpr:
			return nil
		case *SendExpr:
			if err := visit(value.Channel); err != nil {
				return err
			}
			return visit(value.Value)
		case *ParenthesizedExpr:
			return visit(value.Inner)
		case *CompositeLiteralExpr:
			for _, element := range value.Elements {
				if err := visit(element.Key); err != nil {
					return err
				}
				if err := visit(element.Value); err != nil {
					return err
				}
			}
		case *InterpolatedStringExpr:
			for _, segment := range value.Segments {
				if err := visit(segment.Expression); err != nil {
					return err
				}
			}
		case *LambdaExpr:
			if err := visit(value.Body); err != nil {
				return err
			}
			if value.BlockBody != nil {
				var bodyErr error
				collectSafeBlockExpressions(value.BlockBody, func(expression ExprNode) {
					if bodyErr == nil {
						bodyErr = visit(expression)
					}
				})
				if bodyErr != nil {
					return bodyErr
				}
			}
		case *FunctionLiteralExpr:
			if value.Body != nil {
				var bodyErr error
				collectSafeBlockExpressions(value.Body, func(expression ExprNode) {
					if bodyErr == nil {
						bodyErr = visit(expression)
					}
				})
				if bodyErr != nil {
					return bodyErr
				}
			}
		}
		return nil
	}
	return visit(expression)
}

func safeAccessEditForSelector(selector *SelectorExpr, call *CallExpr, src string, context constructorContext, types map[string]string) (safeAccessEdit, error) {
	receiver, ok := selector.Receiver.(*NameExpr)
	if !ok {
		return safeAccessEdit{}, fmt.Errorf("safe access receiver must be an identifier")
	}
	receiverName := receiver.Name
	typeName := types[receiverName]
	if typeName == "" && receiverName == "this" {
		typeName = context.CurrentClass
	}
	target, ok := safeTargetForType(typeName, context)
	if !ok {
		return safeAccessEdit{}, fmt.Errorf("safe access receiver %s has no known class type", receiverName)
	}
	memberType, isMethod, err := safeMemberType(target.Class, target.Classes, selector.Name, map[string]bool{})
	if err != nil {
		return safeAccessEdit{}, err
	}
	if memberType == "" {
		return safeAccessEdit{}, fmt.Errorf("class %s has no member %s", target.Class.Name, selector.Name)
	}
	if !safeReceiverCanBeNil(typeName, target) {
		return safeAccessEdit{}, fmt.Errorf("safe access receiver %s must be a pointer or interface", receiverName)
	}
	if isMethod && call == nil {
		return safeAccessEdit{}, fmt.Errorf("safe method access %s?.%s requires a call", receiverName, selector.Name)
	}
	if !isMethod && call != nil {
		return safeAccessEdit{}, fmt.Errorf("safe field access %s?.%s is not callable", receiverName, selector.Name)
	}
	end := selector.Span().End
	access := receiverName + "." + selector.Name
	if call != nil {
		end = call.Span().End
		if selector.Span().End > len(src) || end > len(src) {
			return safeAccessEdit{}, fmt.Errorf("invalid safe access source span")
		}
		access += src[selector.Span().End:end]
	}
	return safeAccessEdit{
		Start: selector.Span().Start,
		End:   end,
		Text:  fmt.Sprintf("__gpp_safe(%s == nil, func() %s { return %s })", receiverName, memberType, access),
	}, nil
}

// transformSafeAccessInFunctionSource lowers safe access in a structured
// FunctionDecl while the final Go emitter still writes source-shaped output.
func transformSafeAccessInFunctionSource(src string, function *FunctionDecl, context constructorContext) (string, error) {
	if function == nil {
		return src, nil
	}
	bodySource := methodBodySource(function.Method)
	if bodySource == "" {
		return src, nil
	}
	methodContext := context
	methodContext.CurrentParameterTypes = parameterTypeMap(methodParametersSource(function.Method))
	transformed, handled, err := transformSafeAccessAST(bodySource, methodContext)
	if err != nil || !handled || transformed == bodySource {
		return src, err
	}
	start := function.Method.BodySpan.Start - function.SourceSpan.Start
	end := function.Method.BodySpan.End - function.SourceSpan.Start
	if start < 0 || end < start || end > len(src) {
		return src, fmt.Errorf("invalid function body source span")
	}
	return src[:start] + transformed + src[end:], nil
}

func safeValueTypes(src string) map[string]string {
	if result, ok := safeValueTypesAST(src); ok {
		return result
	}
	return map[string]string{}
}

func safeValueTypesAST(src string) (map[string]string, bool) {
	tokens, err := LexSource("safe-access-types", src)
	if err != nil {
		return nil, false
	}
	block, err := ParseBodyAST(tokens)
	if err != nil || block == nil {
		return nil, false
	}
	result := map[string]string{}
	collectSafeValueTypesBlock(block, result)
	return result, true
}

func collectSafeValueTypesBlock(block *BlockStmt, result map[string]string) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectSafeValueTypesStatement(statement, result)
	}
}

func collectSafeValueTypesStatement(statement Stmt, result map[string]string) {
	if statement == nil {
		return
	}
	switch value := statement.(type) {
	case *TokenStmt:
		collectSafeValueTypesBlock(value.Body, result)
		for _, child := range value.Children {
			if child != nil {
				collectSafeValueTypesStatement(child, result)
			}
		}
	case *DeclarationStmt:
		collectSafeDeclarationTypes(value, result)
	case *AssignmentStmt:
		collectSafeAssignmentTypes(value, result)
	case *IfStmt:
		collectSafeValueTypesBlock(value.Body, result)
		collectSafeValueTypesBlock(value.Else, result)
		if value.ElseIf != nil {
			collectSafeValueTypesBlock(value.ElseIf.Body, result)
		}
	case *ForStmt:
		collectSafeValueTypesBlock(value.Body, result)
	case *SwitchStmt:
		collectSafeValueTypesBlock(value.Body, result)
	case *CaseStmt:
		collectSafeValueTypesBlock(value.Clause.Body, result)
	case *TryStmt:
		collectSafeValueTypesBlock(value.Body, result)
		for _, clause := range value.Catches {
			collectSafeValueTypesBlock(clause.Body, result)
		}
		collectSafeValueTypesBlock(value.Finally, result)
	case *BlockStmt:
		collectSafeValueTypesBlock(value, result)
	}
}

func collectSafeDeclarationTypes(statement *DeclarationStmt, result map[string]string) {
	if statement == nil {
		return
	}
	declared := ""
	if statement.Type != nil {
		declared, _ = typeNodeSource(statement.Type)
	}
	for index, name := range statement.Names {
		inferred := declared
		if inferred == "" && index < len(statement.Values) {
			inferred = staticExpressionTypeNode(statement.Values[index], constructorContext{}, result)
		}
		if inferred != "" {
			result[name.Text] = inferred
		}
	}
}

func collectSafeAssignmentTypes(statement *AssignmentStmt, result map[string]string) {
	if statement == nil {
		return
	}
	for index, left := range statement.Left {
		name, ok := left.(*NameExpr)
		if !ok || index >= len(statement.Right) {
			continue
		}
		if inferred := staticExpressionTypeNode(statement.Right[index], constructorContext{}, result); inferred != "" {
			result[name.Name] = inferred
		}
	}
}

func safeASTTypeName(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	if star, ok := expr.(*ast.StarExpr); ok {
		if name := safeASTTypeName(star.X); name != "" {
			return "*" + name
		}
	}
	name, _ := astTypeName(expr)
	return name
}

func safeTargetForType(typeName string, context constructorContext) (constructorTarget, bool) {
	name := strings.TrimSpace(typeName)
	name = strings.TrimPrefix(name, "*")
	if target, ok := context.Targets[name]; ok {
		return target, true
	}
	return constructorTarget{}, false
}

func safeReceiverCanBeNil(typeName string, target constructorTarget) bool {
	name := strings.TrimSpace(typeName)
	return strings.HasPrefix(name, "*") || name == target.InterfaceName || name == "__gpp_"+target.Class.Name || name == "Gpp"+target.Class.Name
}

func safeMemberType(class *ClassDecl, classes map[string]*ClassDecl, name string, visiting map[string]bool) (string, bool, error) {
	if visiting[class.Name] {
		return "", false, nil
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	for _, field := range class.Fields {
		if field.Name == name {
			return fieldTypeSource(field), false, nil
		}
	}
	for _, method := range class.Methods {
		if method.Name == name {
			if strings.TrimSpace(methodResultSource(method)) == "" {
				return "", true, fmt.Errorf("safe method %s must return a value", name)
			}
			return strings.TrimSpace(methodResultSource(method)), true, nil
		}
	}
	for _, parentName := range classParentNames(class) {
		parent, ok := classes[parentName]
		if !ok {
			continue
		}
		memberType, isMethod, err := safeMemberType(parent, classes, name, visiting)
		if err != nil || memberType != "" {
			return memberType, isMethod, err
		}
	}
	return "", false, nil
}
