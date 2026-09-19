package compiler

import (
	"fmt"
	"sort"
	"strings"
)

type lambdaParameter struct {
	Name    string
	TypeAST TypeNode
}

type lambdaSource struct {
	Placeholder string
	Params      []lambdaParameter
	Captured    map[string]string
	BodyExpr    ExprNode
	BlockBody   *BlockStmt
	BodyTokens  []Token
}

type lambdaFunctionType struct {
	Parameters []TypeNode
	ResultAST  TypeNode
}

func (function lambdaFunctionType) resultText() string {
	if function.ResultAST == nil {
		return ""
	}
	text, _ := typeNodeSource(function.ResultAST)
	return text
}

// lambdaASTSpan keeps the parsed lambda node intact through discovery and
// overload selection. Source text is materialized only when the final Go
// function literal is emitted; it is not part of the structured span state.
type lambdaASTSpan struct {
	Start  int
	End    int
	Lambda *LambdaExpr
	Name   string
}

func transformLambdas(src string, context constructorContext) (string, error) {
	if transformed, handled, err := transformLambdasAST(src, context); handled {
		return transformed, err
	}
	if tokens, err := LexSource("lambdas", src); err == nil && tokenSequence(tokens, "=>") {
		return "", fmt.Errorf("lambda expression could not be represented by the body AST")
	}
	return src, nil
}

func transformLambdasAST(src string, context constructorContext) (string, bool, error) {
	tokens, err := LexSource("lambdas", src)
	if err != nil {
		return src, false, nil
	}
	blocks := []*BlockStmt{}
	// ParseBodyAST intentionally preserves unknown declaration-shaped syntax as
	// TokenStmt. Prefer structured top-level function bodies so lambdas inside
	// functions are not handed back to the source scanner.
	functions := parseTopLevelFunctions("lambdas", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function != nil && function.Method.BodyAST != nil {
				blocks = append(blocks, function.Method.BodyAST)
			}
		}
	} else if block, parseErr := ParseBodyAST(tokens); parseErr == nil {
		blocks = append(blocks, block)
	}
	astContext := context
	astContext.CurrentParameterTypes = cloneStringMap(context.CurrentParameterTypes)
	if astContext.CurrentParameterTypes == nil {
		astContext.CurrentParameterTypes = map[string]string{}
	}
	for _, function := range functions {
		if function == nil {
			continue
		}
		for _, parameter := range function.Method.ParameterAST {
			if parameter.Type == nil {
				continue
			}
			if typeName, typeErr := typeNodeSource(parameter.Type); typeErr == nil {
				astContext.CurrentParameterTypes[parameter.Name] = strings.TrimSpace(typeName)
			}
		}
	}
	if astContext.CurrentClass != "" {
		astContext.CurrentParameterTypes["this"] = "*" + astContext.CurrentClass
	} else if astContext.CurrentExtensionReceiver != "" {
		astContext.CurrentParameterTypes["this"] = astContext.CurrentExtensionReceiver
	}
	lambdas := []*LambdaExpr{}
	for _, block := range blocks {
		collectLambdaBodyExpressions(block, func(expression ExprNode) {
			collectLambdaExpressions(expression, &lambdas)
		})
	}
	if len(lambdas) == 0 {
		return src, false, nil
	}
	spans := make([]lambdaASTSpan, 0, len(lambdas))
	for _, lambda := range lambdas {
		span, ok, spanErr := lambdaASTSpanFromAST(lambda, src)
		if spanErr != nil {
			return "", true, spanErr
		}
		if !ok {
			return src, false, nil
		}
		spans = append(spans, span)
	}
	transformed, err := lowerLambdaASTSpans(src, spans, blocks, astContext)
	if err != nil {
		return "", true, err
	}
	return transformed, true, nil
}

func lambdaASTSpanFromAST(lambda *LambdaExpr, src string) (lambdaASTSpan, bool, error) {
	if lambda == nil || (lambda.Body == nil && lambda.BlockBody == nil) {
		return lambdaASTSpan{}, false, nil
	}
	span := lambda.Span()
	bodySpan := Span{}
	if lambda.BlockBody != nil {
		bodySpan = lambda.BlockBody.Span()
	} else {
		bodySpan = lambda.Body.Span()
	}
	if span.Start < 0 || span.End > len(src) || span.Start >= span.End || bodySpan.Start < 0 || bodySpan.End > len(src) || bodySpan.Start >= bodySpan.End {
		return lambdaASTSpan{}, false, nil
	}
	return lambdaASTSpan{Start: span.Start, End: span.End, Lambda: lambda}, true, nil
}

func lowerLambdaASTSpans(src string, spans []lambdaASTSpan, blocks []*BlockStmt, context constructorContext) (string, error) {
	for index := range spans {
		spans[index].Name = fmt.Sprintf("__gpp_lambda_%d", index)
	}
	valueTypes := lambdaValueTypesAST(blocks, context)
	candidatesBySpan := map[lambdaASTKey][]string{}
	parameterTypesBySpan := map[lambdaASTKey]map[string]string{}
	for _, span := range spans {
		candidates := lambdaExpectedTypesAST(blocks, span.Lambda, context, valueTypes)
		if declared := lambdaAssignmentTypeAST(blocks, span.Lambda, valueTypes); declared != "" {
			candidates = append(candidates, declared)
		}
		key := lambdaASTKey{Start: span.Start, End: span.End}
		candidatesBySpan[key] = candidates
		parameterTypesBySpan[key] = lambdaParameterTypes(span.Lambda, candidates)
	}
	// Render children first. A parent lambda's token-backed body is then
	// rebuilt with the already-rendered child function literals, so nested
	// lambdas never require overlapping source edits or a scanner fallback.
	ordered := append([]lambdaASTSpan(nil), spans...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].Start != ordered[right].Start {
			return ordered[left].Start > ordered[right].Start
		}
		return ordered[left].End < ordered[right].End
	})
	renderedBySpan := map[lambdaASTKey]string{}
	for _, span := range ordered {
		lambda, err := lambdaSourceFromAST(span.Lambda)
		if err != nil {
			return "", err
		}
		lambda.Placeholder = span.Name
		lambda.Captured = lambdaCapturedTypes(span, spans, parameterTypesBySpan)
		bodyTokens, bodyErr := lambdaBodyTokensWithChildren(span.Lambda, src, renderedBySpan)
		if bodyErr != nil {
			return "", bodyErr
		}
		lambda.BodyTokens = bodyTokens
		candidates := candidatesBySpan[lambdaASTKey{Start: span.Start, End: span.End}]
		var rendered string
		if len(candidates) == 0 {
			rendered, err = renderStandaloneLambda(lambda, context)
		} else {
			rendered, err = renderContextualLambda(lambda, candidates, context)
		}
		if err != nil {
			return "", err
		}
		renderedBySpan[lambdaASTKey{Start: span.Start, End: span.End}] = rendered
	}

	// Only outermost lambdas are written back. Their rendered bodies already
	// contain all nested replacements.
	replacements := make([]lambdaReplacement, 0, len(spans))
	for _, span := range spans {
		nested := false
		for _, other := range spans {
			if other.Start <= span.Start && other.End >= span.End &&
				(other.Start < span.Start || other.End > span.End) {
				nested = true
				break
			}
		}
		if !nested {
			replacements = append(replacements, lambdaReplacement{
				start: span.Start,
				end:   span.End,
				text:  renderedBySpan[lambdaASTKey{Start: span.Start, End: span.End}],
			})
		}
	}
	sort.Slice(replacements, func(left, right int) bool { return replacements[left].start > replacements[right].start })
	for _, replacement := range replacements {
		src = src[:replacement.start] + replacement.text + src[replacement.end:]
	}
	return src, nil
}

func lambdaPlaceholderSource(src string, spans []lambdaASTSpan) string {
	outermost := make([]lambdaASTSpan, 0, len(spans))
	for _, span := range spans {
		nested := false
		for _, other := range spans {
			if other.Start <= span.Start && other.End >= span.End &&
				(other.Start < span.Start || other.End > span.End) {
				nested = true
				break
			}
		}
		if !nested {
			outermost = append(outermost, span)
		}
	}
	sort.Slice(outermost, func(left, right int) bool { return outermost[left].Start < outermost[right].Start })
	var result strings.Builder
	last := 0
	for _, span := range outermost {
		if span.Start < last || span.Start < 0 || span.End > len(src) || span.Start >= span.End {
			return src
		}
		result.WriteString(src[last:span.Start])
		result.WriteString(span.Name)
		last = span.End
	}
	result.WriteString(src[last:])
	return result.String()
}

func lambdaParameterTypes(lambda *LambdaExpr, candidates []string) map[string]string {
	result := map[string]string{}
	if lambda == nil {
		return result
	}
	for _, candidate := range candidates {
		function, err := parseLambdaFunctionType(candidate)
		if err != nil || len(function.Parameters) != len(lambda.Parameters) {
			continue
		}
		for index, parameter := range lambda.Parameters {
			name := parameter.Text
			if name == "" {
				continue
			}
			typeName, _ := typeNodeSource(function.Parameters[index])
			if typeName != "" {
				result[name] = typeName
			}
		}
		return result
	}
	parameters, err := parseLambdaParameters(expressionTokensSource(lambda.Parameters))
	if err != nil {
		return result
	}
	for _, parameter := range parameters {
		if parameter.TypeAST == nil {
			continue
		}
		typeName, _ := typeNodeSource(parameter.TypeAST)
		if typeName != "" {
			result[parameter.Name] = typeName
		}
	}
	return result
}

func lambdaCapturedTypes(span lambdaASTSpan, spans []lambdaASTSpan, parameterTypes map[lambdaASTKey]map[string]string) map[string]string {
	result := map[string]string{}
	for _, outer := range spans {
		if outer.Start <= span.Start && outer.End >= span.End &&
			(outer.Start < span.Start || outer.End > span.End) {
			for name, typeName := range parameterTypes[lambdaASTKey{Start: outer.Start, End: outer.End}] {
				result[name] = typeName
			}
		}
	}
	return result
}

type lambdaASTKey struct {
	Start int
	End   int
}

type lambdaReplacement struct {
	start int
	end   int
	text  string
}

func lambdaBodyTokensWithChildren(lambda *LambdaExpr, src string, rendered map[lambdaASTKey]string) ([]Token, error) {
	if lambda == nil {
		return nil, fmt.Errorf("lambda is nil")
	}
	bodySpan := Span{}
	if lambda.BlockBody != nil {
		bodySpan = lambda.BlockBody.Span()
	} else if lambda.Body != nil {
		bodySpan = lambda.Body.Span()
	} else {
		return append([]Token(nil), lambda.BodyTokens...), nil
	}
	span := bodySpan
	if span.Start < 0 || span.End > len(src) || span.Start >= span.End {
		return nil, fmt.Errorf("lambda body has invalid source span")
	}
	replacements := make([]lambdaReplacement, 0)
	for key, text := range rendered {
		if key.Start >= span.Start && key.End <= span.End {
			replacements = append(replacements, lambdaReplacement{start: key.Start, end: key.End, text: text})
		}
	}
	if len(replacements) == 0 {
		return append([]Token(nil), lambda.BodyTokens...), nil
	}
	sort.Slice(replacements, func(left, right int) bool { return replacements[left].start > replacements[right].start })
	bodySource := src[span.Start:span.End]
	for _, replacement := range replacements {
		start := replacement.start - span.Start
		end := replacement.end - span.Start
		if start < 0 || end > len(bodySource) || start >= end {
			return nil, fmt.Errorf("lambda child has invalid source span")
		}
		bodySource = bodySource[:start] + replacement.text + bodySource[end:]
	}
	tokens, err := LexSource("lambda body", bodySource)
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

// lambdaSourceFromAST is an emission-boundary adapter. The parser and
// resolver operate on LambdaExpr; only the final source printer needs the
// original body spelling to build a Go function literal.
func lambdaSourceFromAST(lambda *LambdaExpr) (lambdaSource, error) {
	if lambda == nil {
		return lambdaSource{}, fmt.Errorf("lambda is nil")
	}
	parameters, err := parseLambdaParameters(expressionTokensSource(lambda.Parameters))
	if err != nil {
		return lambdaSource{}, err
	}
	return lambdaSource{
		Params:     parameters,
		Captured:   map[string]string{},
		BodyExpr:   lambda.Body,
		BlockBody:  lambda.BlockBody,
		BodyTokens: append([]Token(nil), lambda.BodyTokens...),
	}, nil
}

func collectLambdaBodyExpressions(block *BlockStmt, visit func(ExprNode)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectLambdaStmtExpressions(statement, visit)
	}
}

func collectLambdaStmtExpressions(statement Stmt, visit func(ExprNode)) {
	walkStmtExpressions(statement, visit)
}

func collectLambdaStructuredHeader(statement Stmt, visit func(ExprNode)) {
	switch value := statement.(type) {
	case *IfStmt:
		visit(value.Init)
		visit(value.Condition)
	case *ForStmt:
		visit(value.Init)
		visit(value.Condition)
		visit(value.Post)
		visit(value.RangeExpr)
	case *SwitchStmt:
		visit(value.Init)
		visit(value.Tag)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			visit(expression)
		}
	}
}

func collectLambdaExpressions(expression ExprNode, result *[]*LambdaExpr) {
	if expression == nil {
		return
	}
	switch value := expression.(type) {
	case *LambdaExpr:
		*result = append(*result, value)
		collectLambdaExpressions(value.Body, result)
		collectLambdaBodyExpressions(value.BlockBody, func(expression ExprNode) {
			collectLambdaExpressions(expression, result)
		})
	case *UnaryExpr:
		collectLambdaExpressions(value.Operand, result)
	case *BinaryExpr:
		collectLambdaExpressions(value.Left, result)
		collectLambdaExpressions(value.Right, result)
	case *SelectorExpr:
		collectLambdaExpressions(value.Receiver, result)
	case *IndexExpr:
		collectLambdaExpressions(value.Receiver, result)
		collectLambdaExpressions(value.Index, result)
	case *IndexListExpr:
		collectLambdaExpressions(value.Receiver, result)
		for _, index := range value.Indices {
			collectLambdaExpressions(index, result)
		}
	case *SliceExpr:
		collectLambdaExpressions(value.Receiver, result)
		collectLambdaExpressions(value.Low, result)
		collectLambdaExpressions(value.High, result)
		collectLambdaExpressions(value.Max, result)
	case *TypeAssertExpr:
		collectLambdaExpressions(value.Expression, result)
	case *PostfixExpr:
		collectLambdaExpressions(value.Expression, result)
	case *SpreadExpr:
		collectLambdaExpressions(value.Expression, result)
	case *TypeExpr:
		// Type expressions do not contain lambda expressions.
	case *SendExpr:
		collectLambdaExpressions(value.Channel, result)
		collectLambdaExpressions(value.Value, result)
	case *CallExpr:
		collectLambdaExpressions(value.Callee, result)
		for _, argument := range value.Arguments {
			collectLambdaExpressions(argument.Value, result)
		}
	case *ParenthesizedExpr:
		collectLambdaExpressions(value.Inner, result)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectLambdaExpressions(element.Key, result)
			collectLambdaExpressions(element.Value, result)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectLambdaExpressions(segment.Expression, result)
		}
	case *FunctionLiteralExpr:
		collectLambdaBodyExpressions(value.Body, func(expression ExprNode) {
			collectLambdaExpressions(expression, result)
		})
	}
}

func parseLambdaParameters(src string) ([]lambdaParameter, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	parts, err := splitTopLevel(src, ',')
	if err != nil {
		return nil, err
	}
	result := make([]lambdaParameter, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("lambda parameter cannot be empty")
		}
		if isIdentifier(part) {
			result = append(result, lambdaParameter{Name: part})
			continue
		}
		parameters, err := parseParameterInfos(part)
		if err != nil || len(parameters) != 1 || parameters[0].Name == "" {
			if err != nil {
				return nil, fmt.Errorf("invalid lambda parameter %q: %w", part, err)
			}
			return nil, fmt.Errorf("invalid lambda parameter %q", part)
		}
		result = append(result, lambdaParameter{
			Name:    parameters[0].Name,
			TypeAST: parameters[0].TypeAST,
		})
	}
	return result, nil
}

func lambdaValueTypesAST(blocks []*BlockStmt, context constructorContext) map[string]string {
	result := cloneStringMap(context.CurrentParameterTypes)
	if result == nil {
		result = map[string]string{}
	}
	for _, block := range blocks {
		lambdaCollectValueTypesBlock(block, &result, context)
	}
	return result
}

func lambdaCollectValueTypesBlock(block *BlockStmt, types *map[string]string, context constructorContext) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		lambdaCollectValueTypesStatement(statement, types, context)
	}
}

func lambdaCollectValueTypesStatement(statement Stmt, types *map[string]string, context constructorContext) {
	if statement == nil {
		return
	}
	visit := func(expression ExprNode) {
		lambdaWalkInferenceExpression(expression, func(ExprNode) {})
	}
	switch value := statement.(type) {
	case *DeclarationStmt:
		for index, name := range value.Names {
			if value.Type != nil {
				if typeName, err := typeNodeSource(value.Type); err == nil {
					(*types)[name.Text] = strings.TrimSpace(typeName)
				}
				continue
			}
			if index < len(value.Values) {
				if typeName := staticExpressionTypeNode(value.Values[index], context, *types); typeName != "" {
					(*types)[name.Text] = typeName
				}
			}
		}
		for _, expression := range value.Values {
			visit(expression)
		}
	case *AssignmentStmt:
		for index, left := range value.Left {
			name, ok := left.(*NameExpr)
			if !ok || index >= len(value.Right) {
				continue
			}
			if typeName := staticExpressionTypeNode(value.Right[index], context, *types); typeName != "" {
				(*types)[name.Name] = typeName
			}
		}
		for _, expression := range value.Right {
			visit(expression)
		}
	case *TokenStmt:
		for _, expression := range value.Exprs {
			visit(expression)
		}
		lambdaCollectValueTypesBlock(value.Body, types, context)
		for _, child := range value.Children {
			lambdaCollectValueTypesStatement(child, types, context)
		}
	case *IfStmt:
		visit(value.Init)
		visit(value.Condition)
		lambdaCollectValueTypesBlock(value.Body, types, context)
		lambdaCollectValueTypesBlock(value.Else, types, context)
		if value.ElseIf != nil {
			lambdaCollectValueTypesStatement(value.ElseIf, types, context)
		}
	case *ForStmt:
		visit(value.Init)
		visit(value.Condition)
		visit(value.Post)
		visit(value.RangeExpr)
		lambdaCollectValueTypesBlock(value.Body, types, context)
	case *SwitchStmt:
		visit(value.Init)
		visit(value.Tag)
		lambdaCollectValueTypesBlock(value.Body, types, context)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			visit(expression)
		}
		lambdaCollectValueTypesBlock(value.Clause.Body, types, context)
	case *TryStmt:
		lambdaCollectValueTypesBlock(value.Body, types, context)
		for _, clause := range value.Catches {
			lambdaCollectValueTypesBlock(clause.Body, types, context)
		}
		lambdaCollectValueTypesBlock(value.Finally, types, context)
	default:
		walkStmtExpressions(statement, visit)
	}
}

func lambdaAssignmentTypeAST(blocks []*BlockStmt, target *LambdaExpr, types map[string]string) string {
	if target == nil {
		return ""
	}
	var result string
	var visitBlock func(*BlockStmt)
	var visitStatement func(Stmt)
	visitBlock = func(block *BlockStmt) {
		if block == nil || result != "" {
			return
		}
		for _, statement := range block.Statements {
			visitStatement(statement)
			if result != "" {
				return
			}
		}
	}
	visitStatement = func(statement Stmt) {
		if statement == nil || result != "" {
			return
		}
		switch value := statement.(type) {
		case *DeclarationStmt:
			for index, expression := range value.Values {
				if expression != target || index >= len(value.Names) {
					continue
				}
				if value.Type != nil {
					if typeName, err := typeNodeSource(value.Type); err == nil {
						result = strings.TrimSpace(typeName)
					}
				} else {
					result = types[value.Names[index].Text]
				}
			}
		case *AssignmentStmt:
			for index, expression := range value.Right {
				if expression != target || index >= len(value.Left) {
					continue
				}
				if name, ok := value.Left[index].(*NameExpr); ok {
					result = types[name.Name]
				}
			}
		case *TokenStmt:
			visitBlock(value.Body)
			for _, child := range value.Children {
				visitStatement(child)
			}
		case *IfStmt:
			visitBlock(value.Body)
			visitBlock(value.Else)
			if value.ElseIf != nil {
				visitStatement(value.ElseIf)
			}
		case *ForStmt:
			visitBlock(value.Body)
		case *SwitchStmt:
			visitBlock(value.Body)
		case *CaseStmt:
			visitBlock(value.Clause.Body)
		case *TryStmt:
			visitBlock(value.Body)
			for _, clause := range value.Catches {
				visitBlock(clause.Body)
			}
			visitBlock(value.Finally)
		}
	}
	for _, block := range blocks {
		visitBlock(block)
		if result != "" {
			break
		}
	}
	return result
}

func lambdaExpectedTypesAST(blocks []*BlockStmt, target *LambdaExpr, context constructorContext, valueTypes map[string]string) []string {
	result := []string{}
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	for _, block := range blocks {
		lambdaWalkInferenceBlock(block, func(expression ExprNode) {
			call, ok := expression.(*CallExpr)
			if !ok {
				return
			}
			for index, argument := range call.Arguments {
				if argument.Value != target {
					continue
				}
				for _, expected := range lambdaExpectedTypesForCallAST(call, index, context, valueTypes) {
					add(expected)
				}
			}
		})
	}
	return result
}

func lambdaExpectedTypesForCallAST(call *CallExpr, index int, context constructorContext, valueTypes map[string]string) []string {
	if call == nil {
		return nil
	}
	result := []string{}
	switch function := call.Callee.(type) {
	case *NameExpr:
		for _, signature := range context.FunctionSignatures[function.Name] {
			if index < len(signature.Parameters) {
				result = append(result, signature.Parameters[index].typeText())
			}
		}
	case *SelectorExpr:
		actualType := staticExpressionTypeNode(function.Receiver, context, valueTypes)
		className := strings.TrimPrefix(strings.TrimSpace(actualType), "*")
		for _, signature := range context.ClassMethodSignatures[className][function.Name] {
			if index < len(signature.Parameters) {
				result = append(result, signature.Parameters[index].typeText())
			}
		}
		for _, extension := range context.Extensions {
			if extension.Method.Name != function.Name || !extensionTargetMatches(extension.Target, extension.ReceiverType, actualType, context) {
				continue
			}
			parameters, err := parameterInfosForMethod(extension.Method)
			if err != nil || index >= len(parameters) {
				continue
			}
			result = append(result, substituteLambdaType(parameters[index].typeText(), extensionTargetBindings(extension.Target, actualType)))
		}
	}
	return result
}

func lambdaWalkInferenceBlock(block *BlockStmt, visit func(ExprNode)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		walkStmtExpressions(statement, func(expression ExprNode) {
			lambdaWalkInferenceExpression(expression, visit)
		})
	}
}

func lambdaWalkInferenceExpression(expression ExprNode, visit func(ExprNode)) {
	if expression == nil || visit == nil {
		return
	}
	visit(expression)
	switch value := expression.(type) {
	case *UnaryExpr:
		lambdaWalkInferenceExpression(value.Operand, visit)
	case *BinaryExpr:
		lambdaWalkInferenceExpression(value.Left, visit)
		lambdaWalkInferenceExpression(value.Right, visit)
	case *SelectorExpr:
		lambdaWalkInferenceExpression(value.Receiver, visit)
	case *IndexExpr:
		lambdaWalkInferenceExpression(value.Receiver, visit)
		lambdaWalkInferenceExpression(value.Index, visit)
	case *IndexListExpr:
		lambdaWalkInferenceExpression(value.Receiver, visit)
		for _, index := range value.Indices {
			lambdaWalkInferenceExpression(index, visit)
		}
	case *SliceExpr:
		lambdaWalkInferenceExpression(value.Receiver, visit)
		lambdaWalkInferenceExpression(value.Low, visit)
		lambdaWalkInferenceExpression(value.High, visit)
		lambdaWalkInferenceExpression(value.Max, visit)
	case *TypeAssertExpr:
		lambdaWalkInferenceExpression(value.Expression, visit)
	case *PostfixExpr:
		lambdaWalkInferenceExpression(value.Expression, visit)
	case *SpreadExpr:
		lambdaWalkInferenceExpression(value.Expression, visit)
	case *SendExpr:
		lambdaWalkInferenceExpression(value.Channel, visit)
		lambdaWalkInferenceExpression(value.Value, visit)
	case *CallExpr:
		lambdaWalkInferenceExpression(value.Callee, visit)
		for _, argument := range value.Arguments {
			lambdaWalkInferenceExpression(argument.Value, visit)
		}
	case *ParenthesizedExpr:
		lambdaWalkInferenceExpression(value.Inner, visit)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			lambdaWalkInferenceExpression(element.Key, visit)
			lambdaWalkInferenceExpression(element.Value, visit)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			lambdaWalkInferenceExpression(segment.Expression, visit)
		}
	case *LambdaExpr:
		lambdaWalkInferenceExpression(value.Body, visit)
		lambdaWalkInferenceBlock(value.BlockBody, visit)
	case *FunctionLiteralExpr:
		lambdaWalkInferenceBlock(value.Body, visit)
	}
}

func extensionTargetBindings(target, actual string) map[string]string {
	bindings := map[string]string{}
	target = strings.TrimSpace(target)
	actual = strings.TrimSpace(actual)
	if strings.HasPrefix(target, "[]") && strings.HasPrefix(actual, "[]") {
		name := strings.TrimSpace(target[2:])
		if isTypeParameterName(name) {
			bindings[name] = strings.TrimSpace(actual[2:])
		}
		return bindings
	}
	if strings.HasPrefix(target, "map[") && strings.HasPrefix(actual, "map[") {
		targetClose := strings.IndexByte(target, ']')
		actualClose := strings.IndexByte(actual, ']')
		if targetClose < 0 || actualClose < 0 {
			return bindings
		}
		targetKey := strings.TrimSpace(target[len("map["):targetClose])
		actualKey := strings.TrimSpace(actual[len("map["):actualClose])
		targetValue := strings.TrimSpace(target[targetClose+1:])
		actualValue := strings.TrimSpace(actual[actualClose+1:])
		if isTypeParameterName(targetKey) {
			bindings[targetKey] = actualKey
		}
		if isTypeParameterName(targetValue) {
			bindings[targetValue] = actualValue
		}
	}
	return bindings
}

func substituteLambdaType(source string, bindings map[string]string) string {
	for name, value := range bindings {
		parts := strings.FieldsFunc(source, func(char rune) bool {
			return !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '_'
		})
		if len(parts) == 0 {
			continue
		}
		var out strings.Builder
		for index := 0; index < len(source); {
			if index+len(name) <= len(source) && source[index:index+len(name)] == name &&
				(index == 0 || !isIdentPart(source[index-1])) &&
				(index+len(name) == len(source) || !isIdentPart(source[index+len(name)])) {
				out.WriteString(value)
				index += len(name)
				continue
			}
			out.WriteByte(source[index])
			index++
		}
		source = out.String()
	}
	return source
}

func renderStandaloneLambda(lambda lambdaSource, context constructorContext) (string, error) {
	paramTypes := map[string]string{}
	parts := make([]string, len(lambda.Params))
	for index, parameter := range lambda.Params {
		if parameter.TypeAST == nil {
			return "", fmt.Errorf("cannot infer type of lambda parameter `%s`; provide an explicit type or use the lambda in a typed context", parameter.Name)
		}
		typeName, _ := typeNodeSource(parameter.TypeAST)
		paramTypes[parameter.Name] = typeName
		parts[index] = parameter.Name + " " + typeName
	}
	result := lambdaReturnType(lambda, paramTypes, context)
	return renderLambdaWithResult(lambda, parts, result), nil
}

func renderContextualLambda(lambda lambdaSource, candidates []string, context constructorContext) (string, error) {
	viable := []string{}
	for _, candidate := range candidates {
		function, err := parseLambdaFunctionType(candidate)
		if err != nil || len(function.Parameters) != len(lambda.Params) {
			continue
		}
		paramTypes := map[string]string{}
		parts := make([]string, len(lambda.Params))
		valid := true
		for index, parameter := range lambda.Params {
			typeName := ""
			if parameter.TypeAST != nil {
				typeName, _ = typeNodeSource(parameter.TypeAST)
			}
			functionType, _ := typeNodeSource(function.Parameters[index])
			if typeName == "" {
				typeName = functionType
			}
			if parameter.TypeAST != nil && strings.TrimSpace(typeName) != strings.TrimSpace(functionType) {
				valid = false
				break
			}
			paramTypes[parameter.Name] = typeName
			parts[index] = parameter.Name + " " + typeName
		}
		if !valid {
			continue
		}
		actualResult := lambdaReturnType(lambda, paramTypes, context)
		if lambda.BlockBody != nil && function.resultText() != "" && !lambdaHasValueReturn(lambda) {
			valid = false
			continue
		}
		if !lambdaResultMatches(actualResult, function.resultText(), context) {
			continue
		}
		viable = append(viable, renderLambdaWithResult(lambda, parts, function.resultText()))
	}
	if len(viable) == 0 {
		if len(candidates) == 0 && len(lambda.Params) == 1 && lambda.Params[0].TypeAST == nil {
			return "", fmt.Errorf("cannot infer type of lambda parameter `%s`", lambda.Params[0].Name)
		}
		return "", fmt.Errorf("lambda does not match any expected function type")
	}
	if len(viable) > 1 {
		return "", fmt.Errorf("ambiguous lambda: it matches multiple function types")
	}
	return viable[0], nil
}

func lambdaHasValueReturn(lambda lambdaSource) bool {
	if lambda.BlockBody == nil {
		return lambda.BodyExpr != nil
	}
	return blockHasValueReturn(lambda.BlockBody)
}

func parseLambdaFunctionType(source string) (lambdaFunctionType, error) {
	tokens, err := LexSource("lambda function type", strings.TrimSpace(source))
	if err != nil {
		return lambdaFunctionType{}, err
	}
	typeNode, err := ParseTypeTokens(tokens)
	if err != nil {
		return lambdaFunctionType{}, err
	}
	function, ok := typeNode.(*FunctionType)
	if !ok {
		return lambdaFunctionType{}, fmt.Errorf("%s is not a function type", source)
	}
	result := lambdaFunctionType{}
	for _, parameter := range function.Parameters {
		result.Parameters = append(result.Parameters, parameter.Type)
	}
	if len(function.Results) == 1 {
		result.ResultAST = function.Results[0]
	} else if len(function.Results) > 1 {
		result.ResultAST = &TupleType{Elements: append([]TypeNode(nil), function.Results...)}
	}
	return result, err
}

func renderLambdaWithResult(lambda lambdaSource, parameters []string, result string) string {
	body := expressionTokensSource(lambda.BodyTokens)
	var out strings.Builder
	out.WriteString("func(")
	out.WriteString(strings.Join(parameters, ", "))
	out.WriteByte(')')
	if strings.TrimSpace(result) != "" {
		out.WriteByte(' ')
		out.WriteString(result)
	}
	if lambda.BlockBody != nil {
		out.WriteString(" ")
		out.WriteString(body)
	} else if strings.TrimSpace(result) == "" {
		out.WriteString(" {\n")
		out.WriteString(body)
		out.WriteString("\n}")
	} else {
		out.WriteString(" { return ")
		out.WriteString(body)
		out.WriteString(" }")
	}
	return out.String()
}

func lambdaReturnType(lambda lambdaSource, parameters map[string]string, context constructorContext) string {
	for name, typeName := range lambda.Captured {
		if _, exists := parameters[name]; !exists {
			parameters[name] = typeName
		}
	}
	if context.CurrentClass != "" {
		parameters["this"] = "*" + context.CurrentClass
	} else if context.CurrentExtensionReceiver != "" {
		parameters["this"] = context.CurrentExtensionReceiver
	}
	if lambda.BlockBody != nil {
		result := ""
		visitLambdaReturns(lambda.BlockBody, func(statement *ReturnStmt) {
			if result == "" && len(statement.Values) > 0 {
				result = lambdaExpressionTypeNode(statement.Values[0], parameters, context)
			}
		})
		return result
	}
	if lambda.BodyExpr == nil {
		return ""
	}
	return lambdaExpressionTypeNode(lambda.BodyExpr, parameters, context)
}

func visitLambdaReturns(block *BlockStmt, visit func(*ReturnStmt)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		visitLambdaReturnStatement(statement, visit)
	}
}

func visitLambdaReturnStatement(statement Stmt, visit func(*ReturnStmt)) {
	if statement == nil {
		return
	}
	switch value := statement.(type) {
	case *ReturnStmt:
		visit(value)
	case *TokenStmt:
		visitLambdaReturns(value.Body, visit)
		for _, child := range value.Children {
			visitLambdaReturnStatement(child, visit)
		}
	case *IfStmt:
		visitLambdaReturns(value.Body, visit)
		visitLambdaReturns(value.Else, visit)
		if value.ElseIf != nil {
			visitLambdaReturnStatement(value.ElseIf, visit)
		}
	case *TryStmt:
		visitLambdaReturns(value.Body, visit)
		for _, clause := range value.Catches {
			visitLambdaReturns(clause.Body, visit)
		}
		visitLambdaReturns(value.Finally, visit)
	case *ForStmt:
		visitLambdaReturns(value.Body, visit)
	case *SwitchStmt:
		visitLambdaReturns(value.Body, visit)
	case *CaseStmt:
		visitLambdaReturns(value.Clause.Body, visit)
	case *BlockStmt:
		visitLambdaReturns(value, visit)
	}
}

func blockHasValueReturn(block *BlockStmt) bool {
	found := false
	visitLambdaReturns(block, func(statement *ReturnStmt) {
		if len(statement.Values) > 0 {
			found = true
		}
	})
	return found
}

func lambdaResultMatches(actual, expected string, context constructorContext) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return actual == ""
	}
	if actual == "" {
		return true
	}
	return actual == expected || isAssignableStaticType(actual, expected, context)
}

func lambdaExpressionTypeNode(expression ExprNode, parameters map[string]string, context constructorContext) string {
	if expression == nil {
		return ""
	}
	if name, ok := expression.(*NameExpr); ok {
		if name.Name == "true" || name.Name == "false" {
			return "bool"
		}
		return parameters[name.Name]
	}
	if literal, ok := expression.(*LiteralExpr); ok {
		switch literal.Kind {
		case TokenString, TokenRawString:
			return "string"
		case TokenNumber:
			if strings.ContainsAny(literal.Text, ".eEpP") {
				return "float64"
			}
			return "int"
		case TokenRune:
			return "rune"
		}
	}
	if call, ok := expression.(*CallExpr); ok {
		if name, ok := call.Callee.(*NameExpr); ok && name.Name == "len" {
			return "int"
		}
	}
	if index, ok := expression.(*IndexExpr); ok {
		base := lambdaExpressionTypeNode(index.Receiver, parameters, context)
		if strings.HasPrefix(base, "[]") {
			return strings.TrimSpace(base[2:])
		}
		if strings.HasPrefix(base, "map[") {
			if close := strings.IndexByte(base, ']'); close >= 0 {
				return strings.TrimSpace(base[close+1:])
			}
		}
	}
	if indexList, ok := expression.(*IndexListExpr); ok {
		return lambdaExpressionTypeNode(indexList.Receiver, parameters, context)
	}
	if slice, ok := expression.(*SliceExpr); ok {
		base := lambdaExpressionTypeNode(slice.Receiver, parameters, context)
		if strings.HasPrefix(base, "[]") {
			return strings.TrimSpace(base[2:])
		}
		if base == "string" {
			return "string"
		}
	}
	if assertion, ok := expression.(*TypeAssertExpr); ok && !assertion.TypeSwitch && assertion.Type != nil {
		if typeName, err := typeNodeSource(assertion.Type); err == nil {
			return typeName
		}
	}
	if spread, ok := expression.(*SpreadExpr); ok {
		return lambdaExpressionTypeNode(spread.Expression, parameters, context)
	}
	if typeExpression, ok := expression.(*TypeExpr); ok && typeExpression.Type != nil {
		if typeName, err := typeNodeSource(typeExpression.Type); err == nil {
			return typeName
		}
	}
	if send, ok := expression.(*SendExpr); ok {
		return lambdaExpressionTypeNode(send.Value, parameters, context)
	}
	if nested, ok := expression.(*LambdaExpr); ok {
		lambda, err := lambdaSourceFromAST(nested)
		if err != nil {
			return ""
		}
		lambda.Captured = parameters
		parameterTypes := map[string]string{}
		parts := make([]string, 0, len(lambda.Params))
		for _, parameter := range lambda.Params {
			typeName := ""
			if parameter.TypeAST != nil {
				typeName, _ = typeNodeSource(parameter.TypeAST)
			}
			if typeName == "" {
				return ""
			}
			parameterTypes[parameter.Name] = typeName
			parts = append(parts, typeName)
		}
		result := lambdaReturnType(lambda, parameterTypes, context)
		if result == "" {
			return "func(" + strings.Join(parts, ", ") + ")"
		}
		return "func(" + strings.Join(parts, ", ") + ") " + result
	}
	return staticExpressionTypeNode(expression, context, parameters)
}
