package compiler

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

type recordFieldType struct {
	Name string
	Type string
}

type recordShape struct {
	Key    string
	GoName string
	Fields []recordFieldType
}

type recordContext struct {
	Shapes          map[string]*recordShape
	FunctionResults map[string]string
	MethodResults   map[string]map[string]string
	FunctionParams  map[string]map[int]string
	Emitted         map[string]bool
	Prepared        bool
}

func newRecordContext() *recordContext {
	return &recordContext{
		Shapes:          map[string]*recordShape{},
		FunctionResults: map[string]string{},
		MethodResults:   map[string]map[string]string{},
		FunctionParams:  map[string]map[int]string{},
		Emitted:         map[string]bool{},
	}
}

func cloneRecordContext(source *recordContext) *recordContext {
	result := newRecordContext()
	if source == nil {
		return result
	}
	for key, shape := range source.Shapes {
		fields := append([]recordFieldType(nil), shape.Fields...)
		result.Shapes[key] = &recordShape{Key: shape.Key, GoName: shape.GoName, Fields: fields}
	}
	for name, typeName := range source.FunctionResults {
		result.FunctionResults[name] = typeName
	}
	for className, methods := range source.MethodResults {
		result.MethodResults[className] = map[string]string{}
		for name, typeName := range methods {
			result.MethodResults[className][name] = typeName
		}
	}
	for functionName, parameters := range source.FunctionParams {
		result.FunctionParams[functionName] = map[int]string{}
		for index, typeName := range parameters {
			result.FunctionParams[functionName][index] = typeName
		}
	}
	for name, emitted := range source.Emitted {
		result.Emitted[name] = emitted
	}
	result.Prepared = source.Prepared
	return result
}

func (context *recordContext) register(fields []recordFieldType) *recordShape {
	sorted := append([]recordFieldType(nil), fields...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	parts := make([]string, len(sorted))
	for index, field := range sorted {
		parts[index] = field.Name + ":" + field.Type
	}
	key := strings.Join(parts, ";")
	if shape, ok := context.Shapes[key]; ok {
		return shape
	}
	hash := sha256.Sum256([]byte(key))
	shape := &recordShape{
		Key:    key,
		GoName: fmt.Sprintf("__gpp_record_%x", hash[:4]),
		Fields: sorted,
	}
	context.Shapes[key] = shape
	return shape
}

func (context *recordContext) definitions() string {
	if context == nil {
		return ""
	}
	shapes := []*recordShape{}
	for _, shape := range context.Shapes {
		if context.Emitted[shape.GoName] {
			continue
		}
		shapes = append(shapes, shape)
	}
	sort.Slice(shapes, func(i, j int) bool { return shapes[i].GoName < shapes[j].GoName })
	var output strings.Builder
	for _, shape := range shapes {
		fmt.Fprintf(&output, "type %s struct {\n", shape.GoName)
		for _, field := range shape.Fields {
			fmt.Fprintf(&output, "\t%s %s\n", field.Name, field.Type)
		}
		output.WriteString("}\n\n")
		context.Emitted[shape.GoName] = true
	}
	return output.String()
}

func transformRecords(src string, context constructorContext) (string, error) {
	if context.Records == nil || !strings.Contains(src, "record") && !strings.Contains(src, "let") {
		return src, nil
	}

	src = rewriteLetSyntax(src)
	trial := cloneRecordContext(context.Records)
	trialContext := context
	trialContext.Records = trial

	env := collectRecordValueTypes(src, trialContext)
	speculative, err := transformRecordLiterals(src, env, trialContext, false)
	if err != nil {
		return "", err
	}
	speculative, err = rewriteRecordFunctionResults(speculative, trialContext)
	if err != nil {
		return "", err
	}
	speculative, err = rewriteRecordCollectionLiterals(speculative, trialContext)
	if err != nil {
		return "", err
	}
	speculative, err = rewriteRecordFunctionParameters(speculative, trialContext)
	if err != nil {
		return "", err
	}

	finalRecords := cloneRecordContext(context.Records)
	for name, typeName := range trial.FunctionResults {
		finalRecords.FunctionResults[name] = typeName
	}
	for className, methods := range trial.MethodResults {
		if finalRecords.MethodResults[className] == nil {
			finalRecords.MethodResults[className] = map[string]string{}
		}
		for name, typeName := range methods {
			finalRecords.MethodResults[className][name] = typeName
		}
	}
	finalContext := context
	finalContext.Records = finalRecords
	finalEnv := collectRecordValueTypes(speculative, finalContext)
	transformed, err := transformRecordLiterals(src, finalEnv, finalContext, true)
	if err != nil {
		return "", err
	}
	transformed, err = rewriteRecordFunctionParameters(transformed, finalContext)
	if err != nil {
		return "", err
	}
	transformed, err = rewriteRecordFunctionResults(transformed, finalContext)
	if err != nil {
		return "", err
	}
	transformed, err = rewriteRecordCollectionLiterals(transformed, finalContext)
	if err != nil {
		return "", err
	}

	*context.Records = *finalRecords
	return transformed, nil
}

// prepareRecordContextForFunction preserves cross-function record inference
// after standalone top-level functions stop being grouped inside MixedDecl. The
// preparation pass observes the structured FunctionDecl siblings once, so a
// function parameter can still be inferred from a record call in a later
// function without making the declaration parser depend on a source bundle.
func prepareRecordContextForFunction(function *FunctionDecl, context constructorContext) error {
	if function == nil || function.Owner == nil || context.Records == nil || context.Records.Prepared {
		return nil
	}
	// The owner already contains every parsed declaration. Reuse the typed
	// record-preparation pass instead of concatenating sibling functions into a
	// synthetic source file and reparsing it. This helper remains for callers
	// that emit one function outside emitFile; normal package emission reaches
	// prepareRecordContextAST directly during its prepass.
	return prepareRecordContextAST(function.Owner, context)
}

// recordASTCallable is the small amount of declaration context needed by the
// AST preparation pass. It deliberately points at the parsed Method instead
// of reconstructing a synthetic function declaration from source text.
type recordASTCallable struct {
	Name      string
	ClassName string
	Method    *Method
}

// prepareRecordContextAST discovers record shapes and resolves record
// signatures before declaration emission. The old source-based preparation is
// retained for compatibility callers, but normal package emission now has the
// complete typed declaration tree available and must not wrap/reparse a method
// just to infer its record result.
func prepareRecordContextAST(file *File, context constructorContext) error {
	if file == nil || context.Records == nil {
		return nil
	}
	callables := recordASTCallables(file)
	if len(callables) == 0 {
		return nil
	}

	valueTypes := map[*Method]map[string]string{}
	for _, callable := range callables {
		if callable.Method == nil || callable.Method.BodyAST == nil {
			continue
		}
		callContext := context
		callContext.CurrentClass = callable.ClassName
		callContext.CurrentMethod = callable.Name
		callContext.CurrentParameterTypes = parameterTypeMapFromNodes(callable.Method.ParameterAST)
		if callable.ClassName != "" {
			callContext.CurrentParameterTypes["this"] = "*" + callable.ClassName
		}
		callContext.RecordValueTypes = cloneStringMap(callContext.CurrentParameterTypes)
		if callContext.RecordValueTypes == nil {
			callContext.RecordValueTypes = map[string]string{}
		}
		collectRecordValueTypesBlock(callable.Method.BodyAST, callContext.RecordValueTypes, callContext)
		// Only record syntax is lowered during this preparation pass. Running the
		// complete semantic lowerer here would resolve enums, overloads,
		// introspection, lambdas, and safe access before their normal declaration
		// context is ready.
		callContext.RecordOnlyLowering = true
		if err := lowerRecordOnlyBlock(callable.Method.BodyAST, callContext); err != nil {
			return err
		}
		collectRecordValueTypesBlock(callable.Method.BodyAST, callContext.RecordValueTypes, callContext)
		valueTypes[callable.Method] = callContext.RecordValueTypes
	}

	// Result inference may depend on a function called by another function.
	// Iterate until the generated record names stop changing, matching the
	// fixed-point behavior of the former source compatibility pass.
	for pass := 0; pass <= len(callables); pass++ {
		changed := false
		for _, callable := range callables {
			if callable.Method == nil || callable.Method.BodyAST == nil {
				continue
			}
			kind, ok := recordResultKindAST(callable.Method.ResultAST)
			if !ok {
				continue
			}
			callContext := context
			callContext.CurrentClass = callable.ClassName
			callContext.CurrentMethod = callable.Name
			callContext.RecordValueTypes = valueTypes[callable.Method]
			goType, err := inferRecordFunctionResultAST(callable.Method.BodyAST, kind, callContext.RecordValueTypes, callContext, callable.Name)
			if err != nil {
				return err
			}
			if goType == "" {
				continue
			}
			if replaceRecordTypeNode(&callable.Method.ResultAST, kind, goType) {
				changed = true
			}
			if callable.ClassName != "" {
				if context.Records.MethodResults[callable.ClassName] == nil {
					context.Records.MethodResults[callable.ClassName] = map[string]string{}
				}
				context.Records.MethodResults[callable.ClassName][callable.Name] = goType
			} else {
				context.Records.FunctionResults[callable.Name] = goType
			}
		}
		if !changed {
			break
		}
	}

	// Result inference above can make calls in another function concrete. For
	// example, once getUser() is known to return a generated record shape, the
	// `user` local in main can acquire that shape and become usable as an
	// argument to userName(record). Rebuild each callable's value environment
	// after the result fixed point instead of retaining the pre-inference maps.
	// Without this refresh, AST-only inference sees the call but cannot infer
	// the record argument's shape.
	for _, callable := range callables {
		if callable.Method == nil || callable.Method.BodyAST == nil {
			continue
		}
		callContext := context
		callContext.CurrentClass = callable.ClassName
		callContext.CurrentMethod = callable.Name
		callContext.CurrentParameterTypes = parameterTypeMapFromNodes(callable.Method.ParameterAST)
		if callable.ClassName != "" {
			callContext.CurrentParameterTypes["this"] = "*" + callable.ClassName
		}
		refreshed := cloneStringMap(callContext.CurrentParameterTypes)
		if refreshed == nil {
			refreshed = map[string]string{}
		}
		collectRecordValueTypesBlock(callable.Method.BodyAST, refreshed, callContext)
		valueTypes[callable.Method] = refreshed
	}

	// Resolve record parameters from typed call sites after all record calls
	// have been lowered. This mirrors the language's existing inference rule:
	// a `record` parameter is valid only when its call sites agree on one shape.
	for _, callable := range callables {
		if callable.Method == nil {
			continue
		}
		for parameterIndex := range callable.Method.ParameterAST {
			kind, ok := recordResultKindAST(callable.Method.ParameterAST[parameterIndex].Type)
			if !ok {
				continue
			}
			provided := ""
			for _, caller := range callables {
				callerTypes := valueTypes[caller.Method]
				collectRecordCallsInBlock(caller.Method.BodyAST, func(call *CallExpr) {
					matches := false
					switch callee := call.Callee.(type) {
					case *NameExpr:
						matches = callable.ClassName == "" && callee.Name == callable.Name
					case *SelectorExpr:
						if receiver, receiverOK := callee.Receiver.(*NameExpr); receiverOK {
							matches = receiver.Name == "this" && callable.ClassName != "" && callee.Name == callable.Name
						}
					}
					if !matches || parameterIndex >= len(call.Arguments) {
						return
					}
					actual := inferRecordExprType(call.Arguments[parameterIndex].Value, callerTypes, context)
					if !recordTypeMatchesKind(actual, kind) {
						return
					}
					if provided == "" {
						provided = actual
					}
				})
			}
			if provided != "" {
				replaceRecordTypeNode(&callable.Method.ParameterAST[parameterIndex].Type, kind, provided)
				if context.Records.FunctionParams[callable.Name] == nil {
					context.Records.FunctionParams[callable.Name] = map[int]string{}
				}
				context.Records.FunctionParams[callable.Name][parameterIndex] = provided
			}
		}
	}
	// Normal package emission has now completed the record inference pass from
	// the typed declaration tree. Mark it prepared so a later compatibility
	// fallback does not reconstruct the same declarations as source merely to
	// repeat record discovery.
	context.Records.Prepared = true
	return nil
}

// lowerRecordOnlyBlock is deliberately narrower than lowerExceptionBlockNodes.
// Record inference runs before the ordinary per-declaration semantic context is
// complete, so this pass may rewrite record calls and collection element types,
// but must leave every other Go++ construct untouched.
func lowerRecordOnlyBlock(block *BlockStmt, context constructorContext) error {
	if block == nil {
		return nil
	}
	for _, statement := range block.Statements {
		if err := lowerRecordOnlyStatement(statement, context); err != nil {
			return err
		}
	}
	return nil
}

func lowerRecordOnlyStatement(statement Stmt, context constructorContext) error {
	if statement == nil {
		return nil
	}
	lower := func(expression ExprNode) (ExprNode, error) {
		return lowerRecordOnlyExpr(expression, context)
	}
	switch value := statement.(type) {
	case *TokenStmt:
		for index := range value.Exprs {
			lowered, err := lower(value.Exprs[index])
			if err != nil {
				return err
			}
			value.Exprs[index] = lowered
		}
		if err := lowerRecordOnlyBlock(value.Body, context); err != nil {
			return err
		}
		for _, child := range value.Children {
			if err := lowerRecordOnlyStatement(child, context); err != nil {
				return err
			}
		}
	case *ExpressionStmt:
		lowered, err := lower(value.Expression)
		value.Expression = lowered
		return err
	case *DeclarationStmt:
		for index := range value.Values {
			lowered, err := lower(value.Values[index])
			if err != nil {
				return err
			}
			value.Values[index] = lowered
		}
	case *AssignmentStmt:
		for index := range value.Left {
			lowered, err := lower(value.Left[index])
			if err != nil {
				return err
			}
			value.Left[index] = lowered
		}
		for index := range value.Right {
			lowered, err := lower(value.Right[index])
			if err != nil {
				return err
			}
			value.Right[index] = lowered
		}
	case *ReturnStmt:
		for index := range value.Values {
			lowered, err := lower(value.Values[index])
			if err != nil {
				return err
			}
			value.Values[index] = lowered
		}
	case *ThrowStmt:
		lowered, err := lower(value.Value)
		value.Value = lowered
		return err
	case *DeferStmt:
		lowered, err := lower(value.Expression)
		value.Expression = lowered
		return err
	case *GoStmt:
		lowered, err := lower(value.Expression)
		value.Expression = lowered
		return err
	case *SendStmt:
		var err error
		value.Channel, err = lower(value.Channel)
		if err != nil {
			return err
		}
		value.Value, err = lower(value.Value)
		return err
	case *IncDecStmt:
		lowered, err := lower(value.Expression)
		value.Expression = lowered
		return err
	case *IfStmt:
		var err error
		value.Init, err = lower(value.Init)
		if err != nil {
			return err
		}
		value.Condition, err = lower(value.Condition)
		if err != nil {
			return err
		}
		if err := lowerRecordOnlyBlock(value.Body, context); err != nil {
			return err
		}
		if err := lowerRecordOnlyBlock(value.Else, context); err != nil {
			return err
		}
		if value.ElseIf != nil {
			return lowerRecordOnlyStatement(value.ElseIf, context)
		}
	case *ForStmt:
		var err error
		value.Init, err = lower(value.Init)
		if err != nil {
			return err
		}
		value.Condition, err = lower(value.Condition)
		if err != nil {
			return err
		}
		value.Post, err = lower(value.Post)
		if err != nil {
			return err
		}
		for index := range value.RangeKey {
			value.RangeKey[index], err = lower(value.RangeKey[index])
			if err != nil {
				return err
			}
		}
		value.RangeExpr, err = lower(value.RangeExpr)
		if err != nil {
			return err
		}
		return lowerRecordOnlyBlock(value.Body, context)
	case *SwitchStmt:
		var err error
		value.Init, err = lower(value.Init)
		if err != nil {
			return err
		}
		value.Tag, err = lower(value.Tag)
		if err != nil {
			return err
		}
		return lowerRecordOnlyBlock(value.Body, context)
	case *CaseStmt:
		for index := range value.Clause.Expressions {
			lowered, err := lower(value.Clause.Expressions[index])
			if err != nil {
				return err
			}
			value.Clause.Expressions[index] = lowered
		}
		return lowerRecordOnlyBlock(value.Clause.Body, context)
	case *TryStmt:
		if err := lowerRecordOnlyBlock(value.Body, context); err != nil {
			return err
		}
		for _, clause := range value.Catches {
			if err := lowerRecordOnlyBlock(clause.Body, context); err != nil {
				return err
			}
		}
		return lowerRecordOnlyBlock(value.Finally, context)
	case *BlockStmt:
		return lowerRecordOnlyBlock(value, context)
	}
	return nil
}

func lowerRecordOnlyExpr(expression ExprNode, context constructorContext) (ExprNode, error) {
	if expression == nil {
		return nil, nil
	}
	if call, ok := expression.(*CallExpr); ok {
		if lowered, handled, err := lowerRecordCallExprNode(call, context); handled {
			return lowered, err
		}
	}
	lower := func(value ExprNode) (ExprNode, error) {
		return lowerRecordOnlyExpr(value, context)
	}
	switch value := expression.(type) {
	case *UnaryExpr:
		lowered, err := lower(value.Operand)
		value.Operand = lowered
		return value, err
	case *BinaryExpr:
		var err error
		value.Left, err = lower(value.Left)
		if err != nil {
			return nil, err
		}
		value.Right, err = lower(value.Right)
		return value, err
	case *AssignmentExpr:
		for index := range value.Left {
			lowered, err := lower(value.Left[index])
			if err != nil {
				return nil, err
			}
			value.Left[index] = lowered
		}
		for index := range value.Right {
			lowered, err := lower(value.Right[index])
			if err != nil {
				return nil, err
			}
			value.Right[index] = lowered
		}
	case *SelectorExpr:
		lowered, err := lower(value.Receiver)
		value.Receiver = lowered
		return value, err
	case *IndexExpr:
		var err error
		value.Receiver, err = lower(value.Receiver)
		if err != nil {
			return nil, err
		}
		value.Index, err = lower(value.Index)
		return value, err
	case *IndexListExpr:
		var err error
		value.Receiver, err = lower(value.Receiver)
		if err != nil {
			return nil, err
		}
		for index := range value.Indices {
			value.Indices[index], err = lower(value.Indices[index])
			if err != nil {
				return nil, err
			}
		}
	case *SliceExpr:
		var err error
		value.Receiver, err = lower(value.Receiver)
		if err != nil {
			return nil, err
		}
		value.Low, err = lower(value.Low)
		if err != nil {
			return nil, err
		}
		value.High, err = lower(value.High)
		if err != nil {
			return nil, err
		}
		value.Max, err = lower(value.Max)
		if err != nil {
			return nil, err
		}
	case *TypeAssertExpr:
		lowered, err := lower(value.Expression)
		value.Expression = lowered
		return value, err
	case *PostfixExpr:
		lowered, err := lower(value.Expression)
		value.Expression = lowered
		return value, err
	case *SpreadExpr:
		lowered, err := lower(value.Expression)
		value.Expression = lowered
		return value, err
	case *SendExpr:
		var err error
		value.Channel, err = lower(value.Channel)
		if err != nil {
			return nil, err
		}
		value.Value, err = lower(value.Value)
		return value, err
	case *CallExpr:
		var err error
		value.Callee, err = lower(value.Callee)
		if err != nil {
			return nil, err
		}
		for index := range value.Arguments {
			value.Arguments[index].Value, err = lower(value.Arguments[index].Value)
			if err != nil {
				return nil, err
			}
		}
	case *ParenthesizedExpr:
		lowered, err := lower(value.Inner)
		value.Inner = lowered
		return value, err
	case *CompositeLiteralExpr:
		for index := range value.Elements {
			var err error
			value.Elements[index].Key, err = lower(value.Elements[index].Key)
			if err != nil {
				return nil, err
			}
			value.Elements[index].Value, err = lower(value.Elements[index].Value)
			if err != nil {
				return nil, err
			}
		}
		if err := lowerRecordCollectionTypeNode(value, context); err != nil {
			return nil, err
		}
	case *InterpolatedStringExpr:
		for index := range value.Segments {
			var err error
			value.Segments[index].Expression, err = lower(value.Segments[index].Expression)
			if err != nil {
				return nil, err
			}
		}
	case *LambdaExpr:
		var err error
		value.Body, err = lower(value.Body)
		if err != nil {
			return nil, err
		}
		if err := lowerRecordOnlyBlock(value.BlockBody, context); err != nil {
			return nil, err
		}
	case *FunctionLiteralExpr:
		if err := lowerRecordOnlyBlock(value.Body, context); err != nil {
			return nil, err
		}
	}
	return expression, nil
}

func recordASTCallables(file *File) []recordASTCallable {
	result := []recordASTCallable{}
	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *FunctionDecl:
			result = append(result, recordASTCallable{Name: value.Name, Method: &value.Method})
		case *ClassDecl:
			for index := range value.Methods {
				result = append(result, recordASTCallable{Name: value.Methods[index].Name, ClassName: value.Name, Method: &value.Methods[index]})
			}
		case *ExtendDecl:
			for index := range value.Methods {
				result = append(result, recordASTCallable{Name: value.Methods[index].Name, Method: &value.Methods[index]})
			}
		}
	}
	return result
}

func replaceRecordTypeNode(target *TypeNode, kind, goType string) bool {
	if target == nil || *target == nil || goType == "" {
		return false
	}
	name := &NamedType{Parts: strings.Split(goType, ".")}
	switch value := (*target).(type) {
	case *NamedType:
		if kind != "record" || len(value.Parts) != 1 || value.Parts[0] != "record" {
			return false
		}
		*target = name
		return true
	case *SliceType:
		if kind != "slice" {
			return false
		}
		if named, ok := value.Element.(*NamedType); !ok || len(named.Parts) != 1 || named.Parts[0] != "record" {
			return false
		}
		value.Element = name
		return true
	case *MapType:
		if kind != "map" {
			return false
		}
		if named, ok := value.Value.(*NamedType); !ok || len(named.Parts) != 1 || named.Parts[0] != "record" {
			return false
		}
		value.Value = name
		return true
	default:
		return false
	}
}

func rewriteLetSyntax(src string) string {
	tokens, err := LexSource("let", src)
	if err != nil || !tokenSequence(tokens, "let") {
		return src
	}
	if transformed, handled := rewriteLetSyntaxAST(src); handled {
		return transformed
	}
	return src
}

func transformRecordLiterals(src string, valueTypes map[string]string, context constructorContext, strict bool) (string, error) {
	tokens, err := LexSource("record", src)
	if err != nil || !tokenSequence(tokens, "record", "(") {
		return src, nil
	}
	if transformed, handled, err := transformRecordLiteralsAST(src, valueTypes, context, strict); handled {
		return transformed, err
	}
	return "", fmt.Errorf("record literal could not be represented by the body AST")
}

func rewriteLetSyntaxAST(src string) (string, bool) {
	tokens, err := LexSource("let", src)
	if err != nil {
		return src, false
	}
	blocks := []*BlockStmt{}
	functions := parseTopLevelFunctions("let", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function != nil && function.Method.BodyAST != nil {
				blocks = append(blocks, function.Method.BodyAST)
			}
		}
	} else if block, parseErr := ParseBodyAST(tokens); parseErr == nil && block != nil {
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return src, false
	}
	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	for _, block := range blocks {
		collectRecordDeclarationStatements(block, func(statement *DeclarationStmt) {
			if statement == nil || statement.Keyword != "let" || len(statement.Names) != 1 || len(statement.Values) != 1 {
				return
			}
			value, valueErr := expressionNodeSource(statement.Values[0])
			if valueErr != nil {
				return
			}
			edits = append(edits, edit{
				start: statement.SpanValue.Start,
				end:   statement.SpanValue.End,
				text:  statement.Names[0].Text + " := " + value,
			})
		})
	}
	if len(edits) == 0 {
		return src, false
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		if edit.start < 0 || edit.end > len(src) || edit.start >= edit.end {
			return src, false
		}
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true
}

func collectRecordDeclarationStatements(block *BlockStmt, visit func(*DeclarationStmt)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectRecordStmtDeclarations(statement, visit)
	}
}

func collectRecordStmtDeclarations(statement Stmt, visit func(*DeclarationStmt)) {
	if statement == nil {
		return
	}
	switch value := statement.(type) {
	case *TokenStmt:
		collectRecordDeclarationStatements(value.Body, visit)
		for _, child := range value.Children {
			collectRecordStmtDeclarations(child, visit)
		}
	case *DeclarationStmt:
		visit(value)
	case *IfStmt:
		collectRecordDeclarationStatements(value.Body, visit)
		collectRecordDeclarationStatements(value.Else, visit)
		if value.ElseIf != nil {
			collectRecordStmtDeclarations(value.ElseIf, visit)
		}
	case *ForStmt:
		collectRecordDeclarationStatements(value.Body, visit)
	case *SwitchStmt:
		collectRecordDeclarationStatements(value.Body, visit)
	case *CaseStmt:
		collectRecordDeclarationStatements(value.Clause.Body, visit)
	case *TryStmt:
		collectRecordDeclarationStatements(value.Body, visit)
		for _, clause := range value.Catches {
			collectRecordDeclarationStatements(clause.Body, visit)
		}
		collectRecordDeclarationStatements(value.Finally, visit)
	case *BlockStmt:
		collectRecordDeclarationStatements(value, visit)
	}
}

type recordCallNode struct {
	call       *CallExpr
	start, end int
}

func transformRecordLiteralsAST(src string, valueTypes map[string]string, context constructorContext, strict bool) (string, bool, error) {
	tokens, err := LexSource("record", src)
	if err != nil {
		return src, false, nil
	}
	blocks := []*BlockStmt{}
	functions := parseTopLevelFunctions("record", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function != nil && function.Method.BodyAST != nil {
				blocks = append(blocks, function.Method.BodyAST)
			}
		}
	} else if block, parseErr := ParseBodyAST(tokens); parseErr == nil && block != nil {
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return src, false, nil
	}
	calls := []recordCallNode{}
	for _, block := range blocks {
		collectRecordBodyExpressions(block, func(expression ExprNode) {
			collectRecordCalls(expression, &calls)
		})
	}
	if len(calls) == 0 {
		return src, false, nil
	}
	outermost := []recordCallNode{}
	for index, candidate := range calls {
		nested := false
		for otherIndex, other := range calls {
			if index != otherIndex && other.start <= candidate.start && other.end >= candidate.end &&
				(other.start < candidate.start || other.end > candidate.end) {
				nested = true
				break
			}
		}
		if !nested {
			outermost = append(outermost, candidate)
		}
	}
	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	for _, candidate := range outermost {
		args := make([]string, 0, len(candidate.call.Arguments))
		for _, argument := range candidate.call.Arguments {
			value, valueErr := expressionNodeSource(argument.Value)
			if valueErr != nil {
				return "", true, valueErr
			}
			if argument.Name == "" {
				return "", true, fmt.Errorf("record literal arguments must be named")
			}
			args = append(args, argument.Name+": "+value)
		}
		literal, literalErr := transformRecordLiteral(strings.Join(args, ", "), valueTypes, context, strict)
		if literalErr != nil {
			return "", true, literalErr
		}
		edits = append(edits, edit{start: candidate.start, end: candidate.end, text: literal})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		if edit.start < 0 || edit.end > len(src) || edit.start >= edit.end {
			return src, false, nil
		}
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

func collectRecordBodyExpressions(block *BlockStmt, visit func(ExprNode)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectRecordStmtExpressions(statement, visit)
	}
}

func collectRecordStmtExpressions(statement Stmt, visit func(ExprNode)) {
	walkStmtExpressions(statement, visit)
}

func collectRecordStructuredHeader(statement Stmt, visit func(ExprNode)) {
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

func collectRecordCalls(expression ExprNode, result *[]recordCallNode) {
	if expression == nil {
		return
	}
	switch value := expression.(type) {
	case *CallExpr:
		if name, ok := value.Callee.(*NameExpr); ok && name.Name == "record" {
			span := value.Span()
			*result = append(*result, recordCallNode{call: value, start: span.Start, end: span.End})
		}
		collectRecordCalls(value.Callee, result)
		for _, argument := range value.Arguments {
			collectRecordCalls(argument.Value, result)
		}
	case *UnaryExpr:
		collectRecordCalls(value.Operand, result)
	case *BinaryExpr:
		collectRecordCalls(value.Left, result)
		collectRecordCalls(value.Right, result)
	case *AssignmentExpr:
		for _, expression := range value.Left {
			collectRecordCalls(expression, result)
		}
		for _, expression := range value.Right {
			collectRecordCalls(expression, result)
		}
	case *SelectorExpr:
		collectRecordCalls(value.Receiver, result)
	case *IndexExpr:
		collectRecordCalls(value.Receiver, result)
		collectRecordCalls(value.Index, result)
	case *IndexListExpr:
		collectRecordCalls(value.Receiver, result)
		for _, index := range value.Indices {
			collectRecordCalls(index, result)
		}
	case *SliceExpr:
		collectRecordCalls(value.Receiver, result)
		collectRecordCalls(value.Low, result)
		collectRecordCalls(value.High, result)
		collectRecordCalls(value.Max, result)
	case *TypeAssertExpr:
		collectRecordCalls(value.Expression, result)
	case *PostfixExpr:
		collectRecordCalls(value.Expression, result)
	case *SpreadExpr:
		collectRecordCalls(value.Expression, result)
	case *TypeExpr:
		// Type expressions do not contain record calls.
	case *SendExpr:
		collectRecordCalls(value.Channel, result)
		collectRecordCalls(value.Value, result)
	case *ParenthesizedExpr:
		collectRecordCalls(value.Inner, result)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectRecordCalls(element.Key, result)
			collectRecordCalls(element.Value, result)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectRecordCalls(segment.Expression, result)
		}
	case *LambdaExpr:
		collectRecordCalls(value.Body, result)
		collectRecordBodyExpressions(value.BlockBody, func(expression ExprNode) {
			collectRecordCalls(expression, result)
		})
	case *FunctionLiteralExpr:
		collectRecordBodyExpressions(value.Body, func(expression ExprNode) {
			collectRecordCalls(expression, result)
		})
	}
}

func transformRecordLiteral(argsSource string, valueTypes map[string]string, context constructorContext, strict bool) (string, error) {
	args, err := splitTopLevel(argsSource, ',')
	if err != nil {
		return "", fmt.Errorf("record literal: %w", err)
	}
	fields := []recordFieldType{}
	values := map[string]string{}
	order := []string{}
	for index, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		colon := topLevelColon(arg)
		if colon < 0 {
			return "", fmt.Errorf("record literal argument %d must be named", index+1)
		}
		name := strings.TrimSpace(arg[:colon])
		if !isIdentifier(name) {
			return "", fmt.Errorf("record literal field %q is not a valid identifier", name)
		}
		if _, exists := values[name]; exists {
			return "", fmt.Errorf("record literal repeats field %s", name)
		}
		valueSource := strings.TrimSpace(arg[colon+1:])
		if valueSource == "" {
			return "", fmt.Errorf("record literal field %s is missing a value", name)
		}
		value, err := transformRecordLiterals(valueSource, valueTypes, context, strict)
		if err != nil {
			return "", err
		}
		fieldType := inferRecordExpressionType(value, valueTypes, context)
		if fieldType == "" {
			if strict {
				return "", fmt.Errorf("cannot infer type of record field %s from %s", name, valueSource)
			}
			fieldType = "any"
		}
		values[name] = value
		order = append(order, name)
		fields = append(fields, recordFieldType{Name: name, Type: fieldType})
	}

	shape := context.Records.register(fields)
	var output strings.Builder
	fmt.Fprintf(&output, "%s{", shape.GoName)
	for index, name := range order {
		if index > 0 {
			output.WriteString(", ")
		}
		fmt.Fprintf(&output, "%s: %s", name, values[name])
	}
	output.WriteByte('}')
	return output.String(), nil
}

func rewriteRecordCollectionLiterals(src string, context constructorContext) (string, error) {
	if transformed, handled, err := rewriteRecordCollectionLiteralsAST(src, context); handled {
		return transformed, err
	}
	var output strings.Builder
	for index := 0; index < len(src); {
		if end, ok, err := copyIgnoredSource(src, index, &output); err != nil {
			return "", err
		} else if ok {
			index = end
			continue
		}
		if !keywordAt(src, index, "record") || (index > 0 && isIdentPart(src[index-1])) {
			output.WriteByte(src[index])
			index++
			continue
		}
		previous := index - 1
		for previous >= 0 && (src[previous] == ' ' || src[previous] == '\t' || src[previous] == '\n' || src[previous] == '\r') {
			previous--
		}
		if previous < 0 || src[previous] != ']' {
			output.WriteByte(src[index])
			index++
			continue
		}
		open := skipSpace(src, index+len("record"))
		if open >= len(src) || src[open] != '{' {
			output.WriteString(src[index : index+len("record")])
			index += len("record")
			continue
		}
		close, err := findMatchingBrace(src, open)
		if err != nil {
			return "", fmt.Errorf("record collection literal: %w", err)
		}
		shapeName := firstRecordTypeName(src[open+1 : close])
		if shapeName == "" {
			return "", fmt.Errorf("cannot infer record collection element type")
		}
		output.WriteString(shapeName)
		index = open
	}
	return output.String(), nil
}

func rewriteRecordCollectionLiteralsAST(src string, context constructorContext) (string, bool, error) {
	tokens, err := LexSource("record collections", src)
	if err != nil {
		return src, false, nil
	}
	blocks := []*BlockStmt{}
	functions := parseTopLevelFunctions("record collections", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function == nil || function.Method.BodyAST == nil {
				return src, false, nil
			}
			blocks = append(blocks, function.Method.BodyAST)
		}
	} else if block, parseErr := ParseBodyAST(tokens); parseErr == nil && block != nil {
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return src, false, nil
	}
	edits := []introspectionASTEdit{}
	for _, block := range blocks {
		collectRecordCompositeLiterals(block, func(literal *CompositeLiteralExpr) {
			if literal == nil || literal.Type == nil {
				return
			}
			collectionType, ok := literal.Type.(interface{ recordCollectionElement() TypeNode })
			if !ok {
				return
			}
			elementType := collectionType.recordCollectionElement()
			if named, namedOK := elementType.(*NamedType); !namedOK || len(named.Parts) != 1 || named.Parts[0] != "record" {
				return
			}
			shapeName := ""
			for _, element := range literal.Elements {
				candidate := inferRecordExprType(element.Value, map[string]string{}, context)
				if strings.HasPrefix(candidate, "__gpp_record_") {
					shapeName = candidate
					break
				}
			}
			if shapeName == "" {
				return
			}
			span := literal.Type.Span()
			edits = append(edits, introspectionASTEdit{start: span.Start, end: span.End, text: collectionTypeSource(literal.Type, shapeName)})
		})
	}
	if len(edits) == 0 {
		return src, true, nil
	}
	sort.SliceStable(edits, func(left, right int) bool { return edits[left].start > edits[right].start })
	for _, edit := range edits {
		if edit.start < 0 || edit.end < edit.start || edit.end > len(src) {
			return src, false, nil
		}
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

type recordCollectionType interface {
	TypeNode
	recordCollectionElement() TypeNode
}

func (value *SliceType) recordCollectionElement() TypeNode { return value.Element }
func (value *MapType) recordCollectionElement() TypeNode   { return value.Value }

func collectionTypeSource(typeNode TypeNode, shapeName string) string {
	switch value := typeNode.(type) {
	case *SliceType:
		return "[]" + shapeName
	case *MapType:
		key, err := typeNodeSource(value.Key)
		if err != nil {
			return "map[]" + shapeName
		}
		return "map[" + key + "]" + shapeName
	default:
		return shapeName
	}
}

func collectRecordCompositeLiterals(block *BlockStmt, visit func(*CompositeLiteralExpr)) {
	if block == nil || visit == nil {
		return
	}
	for _, statement := range block.Statements {
		walkStmtExpressions(statement, func(expression ExprNode) {
			collectRecordCompositeExpression(expression, visit)
		})
	}
}

func collectRecordCompositeExpression(expression ExprNode, visit func(*CompositeLiteralExpr)) {
	if expression == nil {
		return
	}
	if literal, ok := expression.(*CompositeLiteralExpr); ok {
		visit(literal)
	}
	switch value := expression.(type) {
	case *UnaryExpr:
		collectRecordCompositeExpression(value.Operand, visit)
	case *BinaryExpr:
		collectRecordCompositeExpression(value.Left, visit)
		collectRecordCompositeExpression(value.Right, visit)
	case *AssignmentExpr:
		for _, expression := range value.Left {
			collectRecordCompositeExpression(expression, visit)
		}
		for _, expression := range value.Right {
			collectRecordCompositeExpression(expression, visit)
		}
	case *SelectorExpr:
		collectRecordCompositeExpression(value.Receiver, visit)
	case *IndexExpr:
		collectRecordCompositeExpression(value.Receiver, visit)
		collectRecordCompositeExpression(value.Index, visit)
	case *IndexListExpr:
		collectRecordCompositeExpression(value.Receiver, visit)
		for _, index := range value.Indices {
			collectRecordCompositeExpression(index, visit)
		}
	case *SliceExpr:
		collectRecordCompositeExpression(value.Receiver, visit)
		collectRecordCompositeExpression(value.Low, visit)
		collectRecordCompositeExpression(value.High, visit)
		collectRecordCompositeExpression(value.Max, visit)
	case *TypeAssertExpr:
		collectRecordCompositeExpression(value.Expression, visit)
	case *PostfixExpr:
		collectRecordCompositeExpression(value.Expression, visit)
	case *SpreadExpr:
		collectRecordCompositeExpression(value.Expression, visit)
	case *SendExpr:
		collectRecordCompositeExpression(value.Channel, visit)
		collectRecordCompositeExpression(value.Value, visit)
	case *CallExpr:
		collectRecordCompositeExpression(value.Callee, visit)
		for _, argument := range value.Arguments {
			collectRecordCompositeExpression(argument.Value, visit)
		}
	case *ParenthesizedExpr:
		collectRecordCompositeExpression(value.Inner, visit)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectRecordCompositeExpression(element.Key, visit)
			collectRecordCompositeExpression(element.Value, visit)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectRecordCompositeExpression(segment.Expression, visit)
		}
	case *LambdaExpr:
		collectRecordCompositeExpression(value.Body, visit)
		collectRecordCompositeLiterals(value.BlockBody, visit)
	case *FunctionLiteralExpr:
		collectRecordCompositeLiterals(value.Body, visit)
	}
}

func firstRecordTypeName(src string) string {
	for index := 0; index < len(src); index++ {
		if strings.HasPrefix(src[index:], "__gpp_record_") &&
			(index == 0 || !isIdentPart(src[index-1])) {
			end := index + len("__gpp_record_")
			for end < len(src) && isIdentPart(src[end]) {
				end++
			}
			return src[index:end]
		}
	}
	return ""
}

func inferRecordExpressionType(source string, valueTypes map[string]string, context constructorContext) string {
	tokens, err := LexSource("record field", strings.TrimSpace(source))
	if err != nil {
		return ""
	}
	expression, err := ParseExpressionTokens(tokens)
	if err != nil || expression == nil {
		return ""
	}
	return inferRecordExprType(expression, valueTypes, context)
}

func inferRecordExprType(expression ExprNode, valueTypes map[string]string, context constructorContext) string {
	switch value := expression.(type) {
	case *NameExpr:
		if valueTypes[value.Name] != "" {
			return valueTypes[value.Name]
		}
		switch value.Name {
		case "true", "false":
			return "bool"
		}
	case *LiteralExpr:
		switch value.Kind {
		case TokenString, TokenRawString:
			return "string"
		case TokenNumber:
			if strings.ContainsAny(value.Text, ".eEpP") {
				return "float64"
			}
			return "int"
		case TokenRune:
			return "rune"
		}
		switch value.Text {
		case "true", "false":
			return "bool"
		}
	case *InterpolatedStringExpr:
		return "string"
	case *CompositeLiteralExpr:
		typeName, err := typeNodeSource(value.Type)
		if err == nil {
			return strings.Join(strings.Fields(typeName), " ")
		}
	case *ParenthesizedExpr:
		return inferRecordExprType(value.Inner, valueTypes, context)
	case *IndexListExpr:
		return inferRecordExprType(value.Receiver, valueTypes, context)
	case *SliceExpr:
		receiverType := strings.TrimSpace(inferRecordExprType(value.Receiver, valueTypes, context))
		if strings.HasPrefix(receiverType, "[]") {
			return strings.TrimPrefix(receiverType, "[]")
		}
		if receiverType == "string" {
			return "string"
		}
	case *TypeAssertExpr:
		if value.TypeSwitch || value.Type == nil {
			return ""
		}
		if typeText, err := typeNodeSource(value.Type); err == nil {
			return typeText
		}
	case *PostfixExpr:
		return inferRecordExprType(value.Expression, valueTypes, context)
	case *SpreadExpr:
		return inferRecordExprType(value.Expression, valueTypes, context)
	case *TypeExpr:
		if value.Type != nil {
			if typeText, err := typeNodeSource(value.Type); err == nil {
				return typeText
			}
		}
	case *SendExpr:
		return inferRecordExprType(value.Value, valueTypes, context)
	case *FunctionLiteralExpr:
		if value.Type != nil {
			if typeText, err := typeNodeSource(value.Type); err == nil {
				return typeText
			}
		}
	case *UnaryExpr:
		inner := inferRecordExprType(value.Operand, valueTypes, context)
		if inner != "" && value.Operator == "&" {
			return "*" + inner
		}
		return inner
	case *BinaryExpr:
		left := inferRecordExprType(value.Left, valueTypes, context)
		right := inferRecordExprType(value.Right, valueTypes, context)
		switch value.Operator {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			return "bool"
		case "+":
			if left == "string" || right == "string" {
				return "string"
			}
			if left == "int" && right == "int" {
				return "int"
			}
		}
	case *CallExpr:
		return inferRecordCallExprResult(value, valueTypes, context)
	case *SelectorExpr:
		base := inferRecordExprType(value.Receiver, valueTypes, context)
		if context.Records != nil {
			if shape := context.Records.recordByGoName(strings.TrimPrefix(base, "*")); shape != nil {
				for _, field := range shape.Fields {
					if field.Name == value.Name {
						return field.Type
					}
				}
			}
		}
		if classTarget, ok := context.Targets[strings.TrimPrefix(base, "*")]; ok {
			for _, field := range classTarget.Class.Fields {
				if field.Name == value.Name {
					return fieldTypeSource(field)
				}
			}
		}
	}
	return ""
}

func inferRecordCallExprResult(call *CallExpr, valueTypes map[string]string, context constructorContext) string {
	switch function := call.Callee.(type) {
	case *NameExpr:
		if context.Records != nil && context.Records.FunctionResults[function.Name] != "" {
			return context.Records.FunctionResults[function.Name]
		}
		if result := commonGoFunctionResult(function.Name); result != "" {
			return result
		}
		for _, signature := range context.FunctionSignatures[function.Name] {
			if signature.resultText() != "" {
				return strings.TrimSpace(signature.resultText())
			}
		}
	case *SelectorExpr:
		if receiver, ok := function.Receiver.(*NameExpr); ok {
			className := strings.TrimPrefix(valueTypes[receiver.Name], "*")
			if className == "" {
				className = context.CurrentClass
			}
			if context.Records != nil && context.Records.MethodResults[className][function.Name] != "" {
				return context.Records.MethodResults[className][function.Name]
			}
			for _, signature := range context.ClassMethodSignatures[className][function.Name] {
				if signature.resultText() != "" {
					return strings.TrimSpace(signature.resultText())
				}
			}
			switch function.Name {
			case "Sprintf", "Itoa", "ToUpper", "ToLower", "TrimSpace", "Join":
				return "string"
			}
		}
	}
	return ""
}

func (context *recordContext) recordByGoName(name string) *recordShape {
	if context == nil {
		return nil
	}
	for _, shape := range context.Shapes {
		if shape.GoName == name {
			return shape
		}
	}
	return nil
}

func commonGoFunctionResult(name string) string {
	switch name {
	case "len", "cap":
		return "int"
	case "string":
		return "string"
	case "bool":
		return "bool"
	case "byte", "uint8":
		return "uint8"
	case "rune", "int32":
		return "rune"
	case "int", "int8", "int16", "int64":
		return name
	case "float32", "float64":
		return name
	}
	return ""
}

func collectRecordValueTypes(src string, context constructorContext) map[string]string {
	if result, handled := collectRecordValueTypesAST(src, context); handled {
		return result
	}
	return map[string]string{}
}

func collectRecordValueTypesAST(src string, context constructorContext) (map[string]string, bool) {
	tokens, err := LexSource("records", src)
	if err != nil {
		return nil, false
	}
	result := map[string]string{}
	functions := parseTopLevelFunctions("records", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function == nil || function.Method.BodyAST == nil {
				continue
			}
			for _, parameter := range function.Method.ParameterAST {
				if parameter.Type == nil || parameter.Name == "" {
					continue
				}
				if typeName, typeErr := typeNodeSource(parameter.Type); typeErr == nil && strings.TrimSpace(typeName) != "" {
					result[parameter.Name] = strings.TrimSpace(typeName)
				}
			}
			collectRecordValueTypesBlock(function.Method.BodyAST, result, context)
		}
		return result, true
	}
	block, parseErr := ParseBodyAST(tokens)
	if parseErr != nil || block == nil {
		return nil, false
	}
	collectRecordValueTypesBlock(block, result, context)
	return result, true
}

func collectRecordValueTypesBlock(block *BlockStmt, valueTypes map[string]string, context constructorContext) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectRecordValueTypesStatement(statement, valueTypes, context)
	}
}

func collectRecordValueTypesStatement(statement Stmt, valueTypes map[string]string, context constructorContext) {
	if statement == nil {
		return
	}
	visitExpression := func(expression ExprNode) string {
		return inferRecordExprType(expression, valueTypes, context)
	}
	switch value := statement.(type) {
	case *TokenStmt:
		collectRecordValueTypesBlock(value.Body, valueTypes, context)
		for _, child := range value.Children {
			collectRecordValueTypesStatement(child, valueTypes, context)
		}
	case *DeclarationStmt:
		declared := ""
		if value.Type != nil {
			declared, _ = typeNodeSource(value.Type)
		}
		for index, name := range value.Names {
			inferred := strings.TrimSpace(declared)
			if inferred == "" && index < len(value.Values) {
				inferred = visitExpression(value.Values[index])
			}
			if inferred != "" {
				valueTypes[name.Text] = inferred
			}
		}
	case *AssignmentStmt:
		for index, left := range value.Left {
			name, ok := left.(*NameExpr)
			if !ok || index >= len(value.Right) {
				continue
			}
			if inferred := visitExpression(value.Right[index]); inferred != "" {
				valueTypes[name.Name] = inferred
			}
		}
	case *IfStmt:
		collectRecordValueTypesBlock(value.Body, valueTypes, context)
		collectRecordValueTypesBlock(value.Else, valueTypes, context)
		if value.ElseIf != nil {
			collectRecordValueTypesStatement(value.ElseIf, valueTypes, context)
		}
	case *ForStmt:
		collectRecordValueTypesBlock(value.Body, valueTypes, context)
	case *SwitchStmt:
		collectRecordValueTypesBlock(value.Body, valueTypes, context)
	case *CaseStmt:
		collectRecordValueTypesBlock(value.Clause.Body, valueTypes, context)
	case *TryStmt:
		collectRecordValueTypesBlock(value.Body, valueTypes, context)
		for _, clause := range value.Catches {
			collectRecordValueTypesBlock(clause.Body, valueTypes, context)
		}
		collectRecordValueTypesBlock(value.Finally, valueTypes, context)
	}
}

func rewriteRecordFunctionResults(src string, context constructorContext) (string, error) {
	if transformed, handled, err := rewriteRecordFunctionResultsAST(src, context); handled {
		return transformed, err
	}
	return src, nil
}

func rewriteRecordFunctionResultsAST(src string, context constructorContext) (string, bool, error) {
	functions := parseTopLevelFunctions("records", "main", src, 0, src, "", 0)
	if len(functions) == 0 {
		return src, false, nil
	}
	valueTypes, handled := collectRecordValueTypesAST(src, context)
	if !handled {
		return src, false, nil
	}
	type inferredFunction struct {
		function *FunctionDecl
		kind     string
		goType   string
	}
	inferred := make([]inferredFunction, 0, len(functions))
	for _, function := range functions {
		if function == nil || function.Method.ResultAST == nil || function.Method.BodyAST == nil {
			continue
		}
		kind, ok := recordResultKindAST(function.Method.ResultAST)
		if !ok {
			continue
		}
		inferred = append(inferred, inferredFunction{function: function, kind: kind})
	}
	if len(inferred) == 0 {
		return src, true, nil
	}
	for pass := 0; pass <= len(inferred); pass++ {
		changed := false
		for index := range inferred {
			goType, err := inferRecordFunctionResultAST(inferred[index].function.Method.BodyAST, inferred[index].kind, valueTypes, context, inferred[index].function.Name)
			if err != nil {
				return "", true, err
			}
			if goType == "" {
				continue
			}
			if inferred[index].goType != goType {
				inferred[index].goType = goType
				changed = true
			}
			name := inferred[index].function.Name
			if context.Records != nil {
				if context.CurrentClass != "" && name == "__gpp_scope" {
					if context.Records.MethodResults[context.CurrentClass] == nil {
						context.Records.MethodResults[context.CurrentClass] = map[string]string{}
					}
					context.Records.MethodResults[context.CurrentClass][context.CurrentMethod] = goType
				} else {
					context.Records.FunctionResults[name] = goType
				}
			}
		}
		if !changed {
			break
		}
		valueTypes, _ = collectRecordValueTypesAST(src, context)
	}
	edits := []introspectionASTEdit{}
	for _, item := range inferred {
		if item.goType == "" {
			return "", true, fmt.Errorf("cannot infer record return type for %s", item.function.Name)
		}
		resultType := item.goType
		switch item.kind {
		case "slice":
			resultType = "[]" + resultType
		case "map":
			if mapping, ok := item.function.Method.ResultAST.(*MapType); ok {
				keyType, keyErr := typeNodeSource(mapping.Key)
				if keyErr != nil {
					return "", true, keyErr
				}
				resultType = "map[" + strings.TrimSpace(keyType) + "]" + resultType
			}
		}
		span := item.function.Method.ResultSpan
		if span.Start >= 0 && span.End <= len(src) && span.Start < span.End {
			edits = append(edits, introspectionASTEdit{start: span.Start, end: span.End, text: resultType})
		}
	}
	sort.Slice(edits, func(left, right int) bool { return edits[left].start > edits[right].start })
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

func recordResultKindAST(typeNode TypeNode) (string, bool) {
	switch value := typeNode.(type) {
	case *NamedType:
		return namedRecordResultKind(value.Parts)
	case *SliceType:
		if named, ok := value.Element.(*NamedType); ok {
			if kind, isRecord := namedRecordResultKind(named.Parts); isRecord && kind == "record" {
				return "slice", true
			}
		}
	case *MapType:
		if named, ok := value.Value.(*NamedType); ok {
			if kind, isRecord := namedRecordResultKind(named.Parts); isRecord && kind == "record" {
				return "map", true
			}
		}
	}
	return "", false
}

func namedRecordResultKind(parts []string) (string, bool) {
	if len(parts) == 1 && parts[0] == "record" {
		return "record", true
	}
	return "", false
}

func inferRecordFunctionResultAST(block *BlockStmt, kind string, valueTypes map[string]string, context constructorContext, functionName string) (string, error) {
	results := []string{}
	collectRecordReturnStatements(block, func(statement *ReturnStmt) {
		if statement == nil || len(statement.Values) != 1 {
			return
		}
		if literal, ok := statement.Values[0].(*CompositeLiteralExpr); ok && (kind == "slice" || kind == "map") {
			for _, element := range literal.Elements {
				value := element.Value
				if kind == "map" && value == nil {
					continue
				}
				typeName := inferRecordExprType(value, valueTypes, context)
				if strings.HasPrefix(typeName, "__gpp_record_") {
					results = append(results, typeName)
				}
			}
			return
		}
		typeName := inferRecordExprType(statement.Values[0], valueTypes, context)
		if strings.HasPrefix(typeName, "__gpp_record_") {
			results = append(results, typeName)
		}
	})
	if len(results) == 0 {
		return "", nil
	}
	for _, result := range results[1:] {
		if result != results[0] {
			return "", fmt.Errorf("inconsistent record return types in %s: expected %s, got %s", functionName, recordShapeDescription(results[0], context.Records), recordShapeDescription(result, context.Records))
		}
	}
	return results[0], nil
}

func collectRecordReturnStatements(block *BlockStmt, visit func(*ReturnStmt)) {
	if block == nil || visit == nil {
		return
	}
	for _, statement := range block.Statements {
		collectRecordReturnStatement(statement, visit)
	}
}

func collectRecordReturnStatement(statement Stmt, visit func(*ReturnStmt)) {
	if statement == nil {
		return
	}
	switch value := statement.(type) {
	case *ReturnStmt:
		visit(value)
	case *TokenStmt:
		collectRecordReturnStatements(value.Body, visit)
		for _, child := range value.Children {
			collectRecordReturnStatement(child, visit)
		}
	case *IfStmt:
		collectRecordReturnStatements(value.Body, visit)
		collectRecordReturnStatements(value.Else, visit)
		if value.ElseIf != nil {
			collectRecordReturnStatement(value.ElseIf, visit)
		}
	case *ForStmt:
		collectRecordReturnStatements(value.Body, visit)
	case *SwitchStmt:
		collectRecordReturnStatements(value.Body, visit)
	case *CaseStmt:
		collectRecordReturnStatements(value.Clause.Body, visit)
	case *TryStmt:
		collectRecordReturnStatements(value.Body, visit)
		for _, clause := range value.Catches {
			collectRecordReturnStatements(clause.Body, visit)
		}
		collectRecordReturnStatements(value.Finally, visit)
	}
}

func rewriteRecordFunctionParameters(src string, context constructorContext) (string, error) {
	if transformed, handled, err := rewriteRecordFunctionParametersAST(src, context); handled {
		return transformed, err
	}
	return src, nil
}

func rewriteRecordFunctionParametersAST(src string, context constructorContext) (string, bool, error) {
	functions := parseTopLevelFunctions("records", "main", src, 0, src, "", 0)
	if len(functions) == 0 {
		return src, false, nil
	}
	for _, function := range functions {
		if function == nil || function.Method.BodyAST == nil {
			return src, false, nil
		}
	}
	valueTypes, handled := collectRecordValueTypesAST(src, context)
	if !handled {
		return src, false, nil
	}
	type parameterEdit struct {
		start int
		end   int
		text  string
	}
	edits := []parameterEdit{}
	for _, function := range functions {
		for parameterIndex, parameter := range function.Method.ParameterAST {
			kind, ok := recordResultKindAST(parameter.Type)
			if !ok {
				continue
			}
			provided := ""
			inconsistent := false
			if context.Records != nil && context.Records.FunctionParams[function.Name] != nil {
				provided = context.Records.FunctionParams[function.Name][parameterIndex]
			}
			collectRecordCallsInFunctions(functions, func(call *CallExpr) {
				callee, ok := call.Callee.(*NameExpr)
				if !ok || callee.Name != function.Name || parameterIndex >= len(call.Arguments) {
					return
				}
				actual := inferRecordExprType(call.Arguments[parameterIndex].Value, valueTypes, context)
				if !recordTypeMatchesKind(actual, kind) {
					return
				}
				if provided != "" && provided != actual {
					inconsistent = true
					return
				}
				provided = actual
			})
			if inconsistent {
				return "", true, fmt.Errorf("inconsistent record argument types for %s parameter %d", function.Name, parameterIndex+1)
			}
			if provided == "" {
				return "", true, fmt.Errorf("cannot infer record parameter %d of %s from call sites", parameterIndex+1, function.Name)
			}
			if context.Records != nil {
				if context.Records.FunctionParams[function.Name] == nil {
					context.Records.FunctionParams[function.Name] = map[int]string{}
				}
				context.Records.FunctionParams[function.Name][parameterIndex] = provided
			}
			start, end, ok := recordParameterTypeSpan(src, function.Method, parameterIndex, parameter)
			if !ok {
				return src, false, nil
			}
			edits = append(edits, parameterEdit{start: start, end: end, text: provided})
		}
	}
	sort.Slice(edits, func(left, right int) bool { return edits[left].start > edits[right].start })
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

func collectRecordCallsInFunctions(functions []*FunctionDecl, visit func(*CallExpr)) {
	if visit == nil {
		return
	}
	for _, function := range functions {
		if function == nil || function.Method.BodyAST == nil {
			continue
		}
		collectRecordCallsInBlock(function.Method.BodyAST, visit)
	}
}

func collectRecordCallsInBlock(block *BlockStmt, visit func(*CallExpr)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		walkStmtExpressions(statement, func(expression ExprNode) {
			collectRecordCallsInExpression(expression, visit)
		})
	}
}

func collectRecordCallsInExpression(expression ExprNode, visit func(*CallExpr)) {
	if expression == nil {
		return
	}
	switch value := expression.(type) {
	case *CallExpr:
		visit(value)
		collectRecordCallsInExpression(value.Callee, visit)
		for _, argument := range value.Arguments {
			collectRecordCallsInExpression(argument.Value, visit)
		}
	case *UnaryExpr:
		collectRecordCallsInExpression(value.Operand, visit)
	case *BinaryExpr:
		collectRecordCallsInExpression(value.Left, visit)
		collectRecordCallsInExpression(value.Right, visit)
	case *AssignmentExpr:
		for _, expression := range value.Left {
			collectRecordCallsInExpression(expression, visit)
		}
		for _, expression := range value.Right {
			collectRecordCallsInExpression(expression, visit)
		}
	case *SelectorExpr:
		collectRecordCallsInExpression(value.Receiver, visit)
	case *IndexExpr:
		collectRecordCallsInExpression(value.Receiver, visit)
		collectRecordCallsInExpression(value.Index, visit)
	case *IndexListExpr:
		collectRecordCallsInExpression(value.Receiver, visit)
		for _, index := range value.Indices {
			collectRecordCallsInExpression(index, visit)
		}
	case *SliceExpr:
		collectRecordCallsInExpression(value.Receiver, visit)
		collectRecordCallsInExpression(value.Low, visit)
		collectRecordCallsInExpression(value.High, visit)
		collectRecordCallsInExpression(value.Max, visit)
	case *TypeAssertExpr:
		collectRecordCallsInExpression(value.Expression, visit)
	case *PostfixExpr:
		collectRecordCallsInExpression(value.Expression, visit)
	case *SpreadExpr:
		collectRecordCallsInExpression(value.Expression, visit)
	case *SendExpr:
		collectRecordCallsInExpression(value.Channel, visit)
		collectRecordCallsInExpression(value.Value, visit)
	case *ParenthesizedExpr:
		collectRecordCallsInExpression(value.Inner, visit)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectRecordCallsInExpression(element.Key, visit)
			collectRecordCallsInExpression(element.Value, visit)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectRecordCallsInExpression(segment.Expression, visit)
		}
	case *LambdaExpr:
		collectRecordCallsInExpression(value.Body, visit)
		collectRecordCallsInBlock(value.BlockBody, visit)
	case *FunctionLiteralExpr:
		collectRecordCallsInBlock(value.Body, visit)
	}
}

func recordParameterTypeSpan(src string, method Method, index int, parameter ParameterNode) (int, int, bool) {
	if method.ParametersSpan.Start < 0 || method.ParametersSpan.End > len(src) || method.ParametersSpan.Start > method.ParametersSpan.End {
		return 0, 0, false
	}
	parameterSource := src[method.ParametersSpan.Start:method.ParametersSpan.End]
	parts, err := splitTopLevel(parameterSource, ',')
	if err != nil || index >= len(parts) {
		return 0, 0, false
	}
	base := 0
	for partIndex := 0; partIndex < index; partIndex++ {
		base += len(parts[partIndex]) + 1
	}
	part := parts[index]
	trimmed := strings.TrimSpace(part)
	leading := strings.Index(part, trimmed)
	typeText, err := typeNodeSource(parameter.Type)
	if err != nil || typeText == "" {
		return 0, 0, false
	}
	search := trimmed
	if parameter.Name != "" {
		nameAt := strings.Index(search, parameter.Name)
		if nameAt >= 0 {
			search = search[nameAt+len(parameter.Name):]
		}
	}
	typeAt := strings.Index(search, typeText)
	if typeAt < 0 {
		return 0, 0, false
	}
	start := method.ParametersSpan.Start + base + leading + (len(trimmed) - len(search)) + typeAt
	return start, start + len(typeText), true
}

func recordTypeMatchesKind(typeName, kind string) bool {
	if kind == "record" {
		return strings.HasPrefix(typeName, "__gpp_record_")
	}
	if kind == "slice" {
		return strings.HasPrefix(typeName, "[]__gpp_record_")
	}
	if kind == "map" {
		return strings.Contains(typeName, "]__gpp_record_")
	}
	return false
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func recordShapeDescription(goName string, context *recordContext) string {
	shape := context.recordByGoName(goName)
	if shape == nil {
		return goName
	}
	parts := make([]string, len(shape.Fields))
	for index, field := range shape.Fields {
		parts[index] = field.Name + " " + field.Type
	}
	return "record{" + strings.Join(parts, ", ") + "}"
}

func transformRecordMethodResult(result, body string, context constructorContext) (string, string, error) {
	if context.Records == nil || (!strings.Contains(result, "record") && !strings.Contains(body, "record") && !strings.Contains(body, "let")) {
		return result, body, nil
	}
	wrapped := "func __gpp_scope()"
	if strings.TrimSpace(result) != "" {
		wrapped += " " + strings.TrimSpace(result)
	}
	wrapped += " {\n" + body + "\n}\n"
	transformed, err := transformRecords(wrapped, context)
	if err != nil {
		return "", "", err
	}
	if functions := parseTopLevelFunctions("records", "main", transformed, 0, transformed, "", 0); len(functions) > 0 && functions[0] != nil {
		method := functions[0].Method
		newResult := result
		if method.ResultAST != nil {
			if rendered, renderErr := typeNodeSource(method.ResultAST); renderErr == nil && strings.TrimSpace(rendered) != "" {
				newResult = rendered
			}
		}
		start, end := method.BodySpan.Start, method.BodySpan.End
		if start >= 0 && end >= start && end <= len(transformed) {
			return newResult, transformed[start:end], nil
		}
	}
	return result, body, nil
}
