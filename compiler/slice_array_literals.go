package compiler

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
)

// resolveBracketLiteralCandidates uses declaration and local-value metadata to
// distinguish a typed literal such as Item[value] from ordinary indexing such
// as items[index]. The parser retains both interpretations until this pass.
func resolveBracketLiteralCandidates(file *File, context constructorContext) {
	if file == nil {
		return
	}
	globalValues := map[string]bool{}
	for name := range context.GlobalValueTypes {
		globalValues[name] = true
	}
	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *ValueDecl:
			for _, name := range value.Names {
				globalValues[name.Text] = true
			}
		case *GoDecl:
			collectASTPackageValues(value.Declarations, globalValues)
		case *MixedDecl:
			collectASTPackageValues(value.GoASTDecls, globalValues)
		}
	}

	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *FunctionDecl:
			resolveBracketLiteralsInMethod(value.Method, context, globalValues, nil)
		case *ClassDecl:
			classTypes := map[string]bool{}
			for _, parameter := range value.TypeParamsAST {
				classTypes[parameter.Name] = true
			}
			for _, method := range value.Methods {
				resolveBracketLiteralsInMethod(method, context, globalValues, classTypes)
			}
		case *ExtendDecl:
			for _, method := range value.Methods {
				resolveBracketLiteralsInMethod(method, context, globalValues, nil)
			}
		case *MixedDecl:
			for _, function := range value.Functions {
				if function != nil {
					resolveBracketLiteralsInMethod(function.Method, context, globalValues, nil)
				}
			}
		}
	}
}

func collectASTPackageValues(declarations []ast.Decl, values map[string]bool) {
	for _, declaration := range declarations {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.VAR && group.Tok != token.CONST {
			continue
		}
		for _, specification := range group.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				values[name.Name] = true
			}
		}
	}
}

func resolveBracketLiteralsInMethod(method Method, context constructorContext, globalValues, typeParameters map[string]bool) {
	if method.BodyAST == nil {
		return
	}
	values := make(map[string]bool, len(globalValues)+len(method.ParameterAST)+2)
	for name := range globalValues {
		values[name] = true
	}
	for _, parameter := range method.ParameterAST {
		if parameter.Name != "" {
			values[parameter.Name] = true
		}
	}
	values["this"] = true
	types := make(map[string]bool, len(context.KnownTypes)+len(typeParameters))
	for name := range context.KnownTypes {
		types[name] = true
	}
	for name := range typeParameters {
		types[name] = true
	}
	for _, parameter := range method.TypeParamsAST {
		types[parameter.Name] = true
	}

	resolveBracketLiteralBlock(method.BodyAST, context, values, types)
}

func resolveBracketLiteralBlock(block *BlockStmt, context constructorContext, values, localTypes map[string]bool) {
	if block == nil {
		return
	}
	scopeValues := cloneBoolMap(values)
	scopeTypes := cloneBoolMap(localTypes)
	for _, statement := range block.Statements {
		resolveBracketLiteralStatement(statement, context, scopeValues, scopeTypes)
	}
}

func resolveBracketLiteralStatement(statement Stmt, context constructorContext, values, localTypes map[string]bool) {
	if statement == nil || isNilStmt(statement) {
		return
	}
	resolve := func(expression ExprNode) {
		resolveBracketLiteralExpression(expression, context, values, localTypes)
	}
	switch value := statement.(type) {
	case *DeclarationStmt:
		for _, expression := range value.Values {
			resolve(expression)
		}
		for _, name := range value.Names {
			values[name.Text] = true
		}
	case *AssignmentStmt:
		for _, expression := range value.Left {
			resolve(expression)
		}
		for _, expression := range value.Right {
			resolve(expression)
		}
		if value.Operator == ":=" {
			for _, expression := range value.Left {
				if name, ok := expression.(*NameExpr); ok {
					values[name.Name] = true
				}
			}
		}
	case *ReturnStmt:
		for _, expression := range value.Values {
			resolve(expression)
		}
	case *ExpressionStmt:
		resolve(value.Expression)
	case *ThrowStmt:
		resolve(value.Value)
	case *DeferStmt:
		resolve(value.Expression)
	case *GoStmt:
		resolve(value.Expression)
	case *SendStmt:
		resolve(value.Channel)
		resolve(value.Value)
	case *IncDecStmt:
		resolve(value.Expression)
	case *IfStmt:
		ifValues := cloneBoolMap(values)
		resolveBracketLiteralExpression(value.Init, context, ifValues, localTypes)
		bindDefinedNames(value.Init, ifValues)
		resolveBracketLiteralExpression(value.Condition, context, ifValues, localTypes)
		resolveBracketLiteralBlock(value.Body, context, ifValues, localTypes)
		resolveBracketLiteralBlock(value.Else, context, values, localTypes)
		resolveBracketLiteralStatement(value.ElseIf, context, values, localTypes)
	case *ForStmt:
		loopValues := cloneBoolMap(values)
		resolveBracketLiteralExpression(value.Init, context, loopValues, localTypes)
		bindDefinedNames(value.Init, loopValues)
		resolveBracketLiteralExpression(value.Condition, context, loopValues, localTypes)
		resolveBracketLiteralExpression(value.Post, context, loopValues, localTypes)
		resolveBracketLiteralExpression(value.RangeExpr, context, loopValues, localTypes)
		if value.RangeOperator == ":=" {
			for _, expression := range value.RangeKey {
				if name, ok := expression.(*NameExpr); ok {
					loopValues[name.Name] = true
				}
			}
		}
		resolveBracketLiteralBlock(value.Body, context, loopValues, localTypes)
	case *SwitchStmt:
		switchValues := cloneBoolMap(values)
		resolveBracketLiteralExpression(value.Init, context, switchValues, localTypes)
		bindDefinedNames(value.Init, switchValues)
		resolveBracketLiteralExpression(value.Tag, context, switchValues, localTypes)
		resolveBracketLiteralBlock(value.Body, context, switchValues, localTypes)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			resolve(expression)
		}
		resolveBracketLiteralBlock(value.Clause.Body, context, values, localTypes)
	case *TryStmt:
		resolveBracketLiteralBlock(value.Body, context, values, localTypes)
		for _, clause := range value.Catches {
			catchValues := cloneBoolMap(values)
			if clause.Binding != "" {
				catchValues[clause.Binding] = true
			}
			resolveBracketLiteralBlock(clause.Body, context, catchValues, localTypes)
		}
		resolveBracketLiteralBlock(value.Finally, context, values, localTypes)
	case *BlockStmt:
		resolveBracketLiteralBlock(value, context, values, localTypes)
	case *TokenStmt:
		for _, expression := range value.Exprs {
			resolve(expression)
		}
		resolveBracketLiteralBlock(value.Body, context, values, localTypes)
		for _, child := range value.Children {
			resolveBracketLiteralStatement(child, context, values, localTypes)
		}
	}
}

func bindDefinedNames(expression ExprNode, values map[string]bool) {
	assignment, ok := expression.(*AssignmentExpr)
	if !ok || assignment.Operator != ":=" {
		return
	}
	for _, expression := range assignment.Left {
		if name, ok := expression.(*NameExpr); ok {
			values[name.Name] = true
		}
	}
}

func cloneBoolMap(values map[string]bool) map[string]bool {
	result := make(map[string]bool, len(values))
	for name, value := range values {
		result[name] = value
	}
	return result
}

func resolveBracketLiteralExpression(expression ExprNode, context constructorContext, values, localTypes map[string]bool) {
	if expression == nil {
		return
	}
	resolve := func(child ExprNode) { resolveBracketLiteralExpression(child, context, values, localTypes) }
	switch value := expression.(type) {
	case *IndexExpr:
		resolve(value.Receiver)
		resolve(value.Index)
		if candidate := value.BracketLiteralCandidate; candidate != nil && !bracketReceiverIsLiteral(value.Receiver) && bracketCandidateIsType(candidate, context, values, localTypes) {
			value.BracketLiteralResolved = true
			applyClassPointerElement(candidate, context)
			resolveBracketLiteralElements(candidate, context, values, localTypes)
		}
	case *IndexListExpr:
		resolve(value.Receiver)
		for _, index := range value.Indices {
			resolve(index)
		}
		if candidate := value.BracketLiteralCandidate; candidate != nil && !bracketReceiverIsLiteral(value.Receiver) && bracketCandidateIsType(candidate, context, values, localTypes) {
			value.BracketLiteralResolved = true
			applyClassPointerElement(candidate, context)
			resolveBracketLiteralElements(candidate, context, values, localTypes)
		}
	case *CallExpr:
		resolve(value.Callee)
		for _, argument := range value.Arguments {
			resolve(argument.Value)
		}
	case *UnaryExpr:
		resolve(value.Operand)
	case *BinaryExpr:
		resolve(value.Left)
		resolve(value.Right)
	case *AssignmentExpr:
		for _, child := range append(append([]ExprNode(nil), value.Left...), value.Right...) {
			resolve(child)
		}
	case *SelectorExpr:
		resolve(value.Receiver)
	case *SliceExpr:
		resolve(value.Receiver)
		resolve(value.Low)
		resolve(value.High)
		resolve(value.Max)
	case *TypeAssertExpr:
		resolve(value.Expression)
	case *PostfixExpr:
		resolve(value.Expression)
	case *SpreadExpr:
		resolve(value.Expression)
	case *SendExpr:
		resolve(value.Channel)
		resolve(value.Value)
	case *ParenthesizedExpr:
		resolve(value.Inner)
	case *CompositeLiteralExpr:
		if value.TypedBracket {
			applyClassPointerElement(value, context)
		}
		for _, element := range value.Elements {
			resolve(element.Key)
			resolve(element.Value)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			resolve(segment.Expression)
		}
	case *LambdaExpr:
		resolve(value.Body)
		lambdaValues := cloneBoolMap(values)
		for _, parameter := range parseParameterNodes(expressionTokensSource(value.Parameters)) {
			if parameter.Name != "" {
				lambdaValues[parameter.Name] = true
			}
		}
		resolveBracketLiteralBlock(value.BlockBody, context, lambdaValues, localTypes)
	case *FunctionLiteralExpr:
		functionValues := cloneBoolMap(values)
		if value.Type != nil {
			for _, parameter := range value.Type.Parameters {
				if parameter.Name != "" {
					functionValues[parameter.Name] = true
				}
			}
		}
		resolveBracketLiteralBlock(value.Body, context, functionValues, localTypes)
	}
}

func bracketReceiverIsLiteral(expression ExprNode) bool {
	switch value := expression.(type) {
	case *CompositeLiteralExpr:
		return value.TypedBracket
	case *IndexExpr:
		return value.BracketLiteralResolved
	case *IndexListExpr:
		return value.BracketLiteralResolved
	default:
		return false
	}
}

func resolveBracketLiteralElements(literal *CompositeLiteralExpr, context constructorContext, values, localTypes map[string]bool) {
	if literal == nil {
		return
	}
	for _, element := range literal.Elements {
		resolveBracketLiteralExpression(element.Key, context, values, localTypes)
		resolveBracketLiteralExpression(element.Value, context, values, localTypes)
	}
}

func bracketCandidateIsType(candidate *CompositeLiteralExpr, context constructorContext, values, localTypes map[string]bool) bool {
	if candidate == nil || candidate.Type == nil {
		return false
	}
	elementType := candidate.Type
	if slice, ok := candidate.Type.(*SliceType); ok {
		elementType = slice.Element
	} else if array, ok := candidate.Type.(*ArrayType); ok {
		elementType = array.Element
	}
	name, qualifier, ok := bracketTypeName(elementType)
	if !ok {
		return isStructuralBracketType(elementType)
	}
	if qualifier == "" && values[name] {
		return false
	}
	if qualifier == "" && localTypes[name] {
		return true
	}
	known := false
	if qualifier == "" {
		known = builtinTypeNames[name] || context.KnownTypes[name] || context.Enums[name] != nil || context.StructConstructors[name] != nil
		if target, exists := context.Targets[name]; exists && target.Class != nil {
			known = true
			if len(target.Class.TypeParamsAST) > 0 && !hasTypeArguments(elementType) && bracketElementsAreTypes(candidate.Elements) {
				return false
			}
		}
	} else {
		known = context.Targets[qualifier+"."+name].Class != nil || context.ImportedTypes[qualifier][name] || context.ImportedPackageTypes[qualifier][name]
		if !known {
			if importPath := context.AvailableImports[qualifier]; importPath != "" {
				if pkg, err := importNativePackage(importPath); err == nil {
					_, known = pkg.Scope().Lookup(name).(*types.TypeName)
				}
			}
		}
	}
	if !known {
		return false
	}
	return true
}

func isStructuralBracketType(typeNode TypeNode) bool {
	switch value := typeNode.(type) {
	case *PointerType, *SliceType, *ArrayType, *MapType, *ChannelType, *FunctionType, *StructType, *InterfaceType:
		return true
	case *VariadicType:
		return isStructuralBracketType(value.Element)
	default:
		return false
	}
}

func bracketTypeName(typeNode TypeNode) (name, qualifier string, ok bool) {
	switch value := typeNode.(type) {
	case *PointerType:
		return bracketTypeName(value.Element)
	case *NamedType:
		if len(value.Parts) == 1 {
			return value.Parts[0], "", true
		}
		if len(value.Parts) == 2 {
			return value.Parts[1], value.Parts[0], true
		}
	}
	return "", "", false
}

func hasTypeArguments(typeNode TypeNode) bool {
	named, ok := typeNode.(*NamedType)
	return ok && len(named.Arguments) > 0
}

func bracketElementsAreTypes(elements []CompositeElement) bool {
	if len(elements) == 0 {
		return false
	}
	for _, element := range elements {
		if element.Value == nil {
			return false
		}
		text, err := expressionNodeSource(element.Value)
		if err != nil {
			return false
		}
		tokens, err := LexSource("bracket type argument", text)
		if err != nil {
			return false
		}
		typeNode, err := ParseTypeTokens(tokens)
		if err != nil || typeNode == nil {
			return false
		}
	}
	return true
}

func applyClassPointerElement(candidate *CompositeLiteralExpr, context constructorContext) {
	if candidate == nil || candidate.Type == nil {
		return
	}
	var element TypeNode
	switch value := candidate.Type.(type) {
	case *SliceType:
		element = value.Element
	case *ArrayType:
		element = value.Element
	default:
		return
	}
	name, qualifier, ok := bracketTypeName(element)
	if !ok {
		return
	}
	key := name
	if qualifier != "" {
		key = qualifier + "." + name
	}
	target, ok := context.Targets[key]
	if !ok || target.Class == nil {
		return
	}
	if classHasDerivedInContext(target, context) {
		named, ok := element.(*NamedType)
		if !ok {
			return
		}
		interfaceName := target.InterfaceName
		if interfaceName == "" {
			interfaceName = "Gpp" + target.Class.Name
		}
		parts := []string{interfaceName}
		if target.Qualifier != "" {
			parts = []string{target.Qualifier, interfaceName}
		}
		interfaceType := &NamedType{Parts: parts, Arguments: named.Arguments, SpanValue: named.SpanValue}
		switch value := candidate.Type.(type) {
		case *SliceType:
			value.Element = interfaceType
		case *ArrayType:
			value.Element = interfaceType
		}
		return
	}
	if _, alreadyPointer := element.(*PointerType); alreadyPointer {
		return
	}
	pointer := &PointerType{Element: element, SpanValue: element.Span()}
	switch value := candidate.Type.(type) {
	case *SliceType:
		value.Element = pointer
	case *ArrayType:
		value.Element = pointer
	}
}

func classHasDerivedInContext(base constructorTarget, context constructorContext) bool {
	if classHasDerived(base.Class, base.Classes) {
		return true
	}
	for _, candidate := range context.Targets {
		if candidate.Class == nil || candidate.Class == base.Class {
			continue
		}
		if classInheritsTarget(candidate, base, context) {
			return true
		}
	}
	return false
}

var builtinTypeNames = map[string]bool{
	"any": true, "bool": true, "byte": true, "complex64": true, "complex128": true,
	"error": true, "float32": true, "float64": true, "int": true, "int8": true,
	"int16": true, "int32": true, "int64": true, "rune": true, "string": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true,
}

type bracketSourceReplacement struct {
	start int
	end   int
	text  string
}

// rewriteResolvedBracketLiterals replaces the newly supported bracket syntax
// in source paths that still use the compiler's source-based lowering pipeline.
func rewriteResolvedBracketLiterals(source string, sourceOffset int, block *BlockStmt) string {
	if block == nil || source == "" {
		return source
	}
	replacements := []bracketSourceReplacement{}
	walkIntrospectionBlockExpressions(block, func(expression ExprNode) {
		var literal *CompositeLiteralExpr
		switch value := expression.(type) {
		case *CompositeLiteralExpr:
			if value.TypedBracket {
				literal = value
			}
		case *IndexExpr:
			if value.BracketLiteralResolved {
				literal = value.BracketLiteralCandidate
			}
		case *IndexListExpr:
			if value.BracketLiteralResolved {
				literal = value.BracketLiteralCandidate
			}
		}
		if literal == nil {
			return
		}
		span := literal.Span()
		start, end := span.Start-sourceOffset, span.End-sourceOffset
		if start < 0 || end > len(source) || start >= end {
			return
		}
		text, err := expressionNodeSource(literal)
		if err == nil {
			replacements = append(replacements, bracketSourceReplacement{start: start, end: end, text: text})
		}
	})
	if len(replacements) == 0 {
		return source
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	for _, replacement := range replacements {
		source = source[:replacement.start] + replacement.text + source[replacement.end:]
	}
	return source
}
