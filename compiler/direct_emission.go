package compiler

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"sort"
	"strings"
)

// directFunctionSource emits a structured function without recovering its
// executable body from File.Source. It is deliberately conservative: a body
// that needs a Go++ semantic pass returns handled=false and remains on the
// established lowering pipeline.
func directFunctionSource(function *FunctionDecl, context constructorContext) (string, bool, error) {
	if function == nil || function.Method.BodyAST == nil || !directSignatureSafe(function.Method) {
		return "", false, nil
	}
	methodContext := context
	methodContext.CurrentParameterTypes = parameterTypeMapFromNodes(function.Method.ParameterAST)
	methodContext.CurrentParameterAST = parameterTypeNodeMapFromNodes(function.Method.ParameterAST)
	methodContext.CurrentResultAST = function.Method.ResultAST
	body, handled, err := directMethodBody(function.Method, methodContext)
	if !handled {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	name := function.Name
	if overloaded := overloadFunctionName(function, context.Overloads); overloaded != "" {
		name = overloaded
	}
	return formatDirectFunctionDecl(name, nil, function.Method.TypeParamsAST, function.Method.ParameterAST, function.Method.ResultAST, function.Method.ResultFieldsAST, methodContext, true, body)
}

func directMethodSource(class *ClassDecl, method Method, context constructorContext) (string, bool, error) {
	if class == nil || method.BodyAST == nil || !directSignatureSafe(method) {
		return "", false, nil
	}
	methodContext := context
	methodContext.CurrentClass = class.Name
	methodContext.CurrentMethod = method.Name
	methodContext.CurrentParameterTypes = parameterTypeMapFromNodes(method.ParameterAST)
	methodContext.CurrentParameterAST = parameterTypeNodeMapFromNodes(method.ParameterAST)
	methodContext.CurrentResultAST = method.ResultAST
	body, handled, err := directMethodBody(method, methodContext)
	if !handled {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	name := methodOutputName(method)
	receiverType := directNamedType(methodReceiverType(class, method))
	if receiverType == nil {
		return "", false, nil
	}
	receiverExpr, err := goTypeExpr(receiverType)
	if err != nil {
		return "", false, nil
	}
	receiver := &ast.Field{Names: []*ast.Ident{ast.NewIdent("this")}, Type: receiverExpr}
	return formatDirectFunctionDecl(name, receiver, method.TypeParamsAST, method.ParameterAST, method.ResultAST, method.ResultFieldsAST, methodContext, true, body)
}

func directStaticMethodSource(class *ClassDecl, method Method, context constructorContext) (string, bool, error) {
	if class == nil || method.BodyAST == nil || !directSignatureSafe(method) {
		return "", false, nil
	}
	methodContext := context
	methodContext.CurrentParameterTypes = parameterTypeMapFromNodes(method.ParameterAST)
	methodContext.CurrentParameterAST = parameterTypeNodeMapFromNodes(method.ParameterAST)
	methodContext.CurrentResultAST = method.ResultAST
	body, handled, err := directMethodBody(method, methodContext)
	if !handled {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	return formatDirectFunctionDecl(staticMethodGoName(class, method), nil, method.TypeParamsAST, method.ParameterAST, method.ResultAST, method.ResultFieldsAST, methodContext, !method.Generated, body)
}

// directExtensionMethodSource emits an extension body from its owned
// BlockStmt. Extensions do not have a ClassDecl receiver, so their receiver
// type is supplied by the extension semantic model rather than inferred from
// the class method formatter.
func directExtensionMethodSource(extension extensionMethod, context constructorContext) (string, bool, error) {
	method := extension.Method
	if method.BodyAST == nil || !directSignatureSafe(method) {
		return "", false, nil
	}
	methodContext := context
	methodContext.CurrentExtensionReceiver = extension.ReceiverType
	methodContext.CurrentResultAST = method.ResultAST
	methodContext.CurrentParameterTypes = parameterTypeMapFromNodes(method.ParameterAST)
	methodContext.CurrentParameterAST = parameterTypeNodeMapFromNodes(method.ParameterAST)
	// One extension declaration may emit the same body for several targets.
	// Direct lowering mutates the typed body, so parse a fresh copy from its
	// original tokens for each emitted target.
	if len(method.BodyTokens) > 0 {
		if bodyCopy, parseErr := ParseBodyAST(method.BodyTokens); parseErr == nil {
			method.BodyAST = bodyCopy
		}
	}
	body, handled, err := directMethodBody(method, methodContext)
	if !handled {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	receiverType := parseTypeText(strings.TrimSpace(extension.ReceiverType))
	if receiverType == nil {
		return "", false, nil
	}
	// Extensions are emitted as package-level functions. Go does not permit
	// adding methods to non-local types such as string or sql.DB, so the
	// receiver is the first ordinary parameter rather than FuncDecl.Recv.
	parameters := make([]ParameterNode, 0, len(method.ParameterAST)+1)
	parameters = append(parameters, ParameterNode{Name: "this", Type: receiverType})
	for _, parameter := range method.ParameterAST {
		parameter.Type = directExtensionParameterTypeNode(parameter.Type, methodContext)
		parameters = append(parameters, parameter)
	}
	return formatDirectFunctionDecl(extension.GoName, nil, directExtensionTypeParameters(extension), parameters, method.ResultAST, method.ResultFieldsAST, methodContext, true, body)
}

// directValueDeclSource emits a top-level ValueDecl from its structured
// initializer expressions. A ValueDecl is already parsed as a declaration
// node, so rebuilding `var ... = ...` as source just to send it through the
// compatibility rewriters is an unnecessary AST-to-source boundary.
//
// Keep this path conservative. Record-shaped declarations and declarations
// whose type still needs a Go++-specific rewrite continue through the source
// fallback until their type lowering is structural too.
func directValueDeclSource(declaration *ValueDecl, context constructorContext) (string, bool, error) {
	if declaration == nil || len(declaration.Names) == 0 || len(declaration.Values) == 0 {
		return "", false, nil
	}
	if typeNodeHasRecord(declaration.Type) {
		return "", false, nil
	}
	values := make([]ast.Expr, 0, len(declaration.Values))
	for _, value := range declaration.Values {
		lowered, err := lowerExceptionExprNode(value, context)
		if err != nil {
			return "", false, nil
		}
		if !directExpressionSafe(lowered, context) {
			return "", false, nil
		}
		expression, err := goExprNode(lowered)
		if err != nil {
			return "", false, nil
		}
		values = append(values, expression)
	}
	spec := &ast.ValueSpec{}
	for _, name := range declaration.Names {
		if name.Text == "" {
			return "", false, nil
		}
		spec.Names = append(spec.Names, ast.NewIdent(name.Text))
	}
	if declaration.Type != nil {
		var err error
		spec.Type, err = goTypeExpr(declaration.Type)
		if err != nil {
			return "", false, nil
		}
	}
	spec.Values = values
	var output bytes.Buffer
	declarationToken := token.VAR
	if declaration.Keyword == "const" {
		declarationToken = token.CONST
	}
	if err := format.Node(&output, token.NewFileSet(), &ast.GenDecl{
		Tok:   declarationToken,
		Specs: []ast.Spec{spec},
	}); err != nil {
		return "", true, err
	}
	return output.String(), true, nil
}

func directTypeText(typeNode TypeNode) (string, error) {
	if typeNode == nil {
		return "", nil
	}
	text, err := typeNodeSource(typeNode)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func transformDirectResultType(typeNode TypeNode, context constructorContext) (string, error) {
	text, err := directTypeText(typeNode)
	if err != nil {
		return "", err
	}
	return transformPolymorphicResultType(text, context), nil
}

func transformParameterNodes(parameters []ParameterNode, context constructorContext) (string, error) {
	parts := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		text, err := directTypeText(parameter.Type)
		if err != nil {
			return "", err
		}
		text = transformPolymorphicType(text, context)
		if parameter.Name == "" {
			parts = append(parts, text)
		} else {
			parts = append(parts, parameter.Name+" "+text)
		}
	}
	return strings.Join(parts, ", "), nil
}

func directMethodBody(method Method, context constructorContext) (*ast.BlockStmt, bool, error) {
	if method.BodyAST == nil || !directEmissionAllowed(context) {
		return nil, false, nil
	}
	bodyAST := method.BodyAST

	// The method already owns a structured body. Keep lowering on that AST;
	// reparsing BodyTokens here would reintroduce the AST-to-source-to-AST
	// round trip this path is intended to remove.
	bodyContext := context
	bodyContext.CurrentParameterTypes = cloneStringMap(context.CurrentParameterTypes)
	if bodyContext.CurrentParameterTypes == nil {
		bodyContext.CurrentParameterTypes = map[string]string{}
	}
	if bodyContext.CurrentClass != "" {
		bodyContext.CurrentParameterTypes["this"] = "*" + bodyContext.CurrentClass
	}
	for name, typeName := range context.GlobalValueTypes {
		bodyContext.CurrentParameterTypes[name] = typeName
	}
	if context.CurrentExtensionReceiver != "" {
		bodyContext.CurrentParameterTypes["this"] = context.CurrentExtensionReceiver
	}
	for name, typeName := range lambdaValueTypesAST([]*BlockStmt{bodyAST}, context) {
		bodyContext.CurrentParameterTypes[name] = typeName
	}
	for name, typeName := range enumValueTypesAST([]*BlockStmt{bodyAST}, bodyContext) {
		bodyContext.CurrentParameterTypes[name] = typeName
	}
	introspectionKinds := map[string]int{}
	collectIntrospectionMetadataBlock(bodyAST, introspectionKinds, bodyContext, bodyContext.CurrentParameterTypes, func(int, int, string) {})
	bodyContext.CurrentIntrospectionKinds = introspectionKinds
	bodyContext.RecordValueTypes = cloneStringMap(bodyContext.CurrentParameterTypes)
	if bodyContext.RecordValueTypes == nil {
		bodyContext.RecordValueTypes = map[string]string{}
	}
	if hasExceptionSyntaxAST(bodyAST) {
		if err := validateExceptionASTBlock(bodyAST, bodyContext); err != nil {
			return nil, true, err
		}
	}
	collectRecordValueTypesBlock(bodyAST, bodyContext.RecordValueTypes, bodyContext)
	// Normalize expression-level constructs before exception promotion. A
	// selector such as Enum.From(...) can become a generated Go call during
	// this pass; promotion must inspect that lowered call rather than the
	// original Go++ selector and miss its multi-result error ABI.
	if err := lowerExceptionBlockNodes(bodyAST, bodyContext); err != nil {
		return nil, false, nil
	}
	if err := lowerExceptionPromotions(bodyAST, bodyContext); err != nil {
		return nil, false, nil
	}
	if err := lowerPolymorphismBlockNode(bodyAST, bodyContext); err != nil {
		return nil, false, nil
	}
	if hasExceptionSyntaxAST(bodyAST) {
		// Promotion must happen before expression lowering so an extension or
		// native call that returns an error is wrapped while it is still a typed
		// CallExpr. The exception ABI then turns control-transfer panics back into
		// the declared function result.
		if err := lowerExceptionBlockNodes(bodyAST, bodyContext); err != nil {
			return nil, false, nil
		}
		body, err := lowerFunctionGoBlockNode(bodyAST, bodyContext, "")
		if err != nil {
			return nil, false, nil
		}
		body, err = lowerExceptionABIBoundaryNode(body, method.ResultAST, method.ResultFieldsAST, bodyContext)
		if err != nil {
			return nil, true, err
		}
		return body, true, nil
	}

	// Lower expression-level Go++ constructs structurally even when the body
	// has no try/throw statements. This covers coalescing, class construction,
	// and simple extension calls without first rendering the body to source.
	if err := lowerExceptionBlockNodes(bodyAST, bodyContext); err != nil {
		return nil, false, nil
	}
	body, err := lowerFunctionGoBlockNode(bodyAST, bodyContext, "")
	if err != nil {
		return nil, false, nil
	}
	if astContainsExceptionRuntime(body) && directExceptionBoundaryRequired(body, method.ResultAST, method.ResultFieldsAST) {
		body, err = lowerExceptionABIBoundaryNode(body, method.ResultAST, method.ResultFieldsAST, bodyContext)
		if err != nil {
			return nil, true, err
		}
	}
	if context.CurrentClass != "" {
		if target, ok := context.Targets[context.CurrentClass]; ok && target.Class != nil {
			body, err = formatImplicitThisBody(body, target.Class, target.Classes, method)
			if err != nil {
				return nil, true, err
			}
		}
	}
	return body, true, nil
}

func directExceptionBoundaryRequired(body *ast.BlockStmt, resultType TypeNode, resultFields []ParameterNode) bool {
	if astContainsExceptionReturn(body) {
		return true
	}
	fields, ok := exceptionResultFieldsForResult(resultType, resultFields)
	if !ok || len(fields) == 0 {
		return false
	}
	return isErrorTypeNode(fields[len(fields)-1].typeNode)
}

func isErrorTypeNode(typeNode TypeNode) bool {
	name, ok := typeNode.(*NamedType)
	return ok && len(name.Parts) == 1 && name.Parts[0] == "error" && len(name.Arguments) == 0
}

func astContainsExceptionReturn(root ast.Node) bool {
	found := false
	ast.Inspect(root, func(node ast.Node) bool {
		if found || node == nil {
			return !found
		}
		identifier, ok := node.(*ast.Ident)
		if ok && identifier.Name == "__gppExceptionReturn" {
			found = true
		}
		return !found
	})
	return found
}

// astContainsExceptionRuntime identifies generated wrappers whose failures are
// represented as panics and therefore need the function's exception ABI
// boundary. It deliberately inspects the generated Go AST, not source text.
func astContainsExceptionRuntime(root ast.Node) bool {
	found := false
	ast.Inspect(root, func(node ast.Node) bool {
		if found || node == nil {
			return !found
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		switch identifier.Name {
		case "__gppThrow", "__gppUnwrap", "__gppUnwrap2", "__gppUnwrap3", "__gppDiscard", "__gppDiscard2", "__gppDiscard3":
			found = true
		}
		return !found
	})
	return found
}

func hasExceptionSyntaxAST(block *BlockStmt) bool {
	if block == nil {
		return false
	}
	for _, statement := range block.Statements {
		if hasExceptionSyntaxStatementAST(statement) {
			return true
		}
	}
	return false
}

func hasExceptionSyntaxStatementAST(statement Stmt) bool {
	if statement == nil {
		return false
	}
	switch value := statement.(type) {
	case *TryStmt:
		return value != nil
	case *ThrowStmt:
		return value != nil
	case *IfStmt:
		if value == nil {
			return false
		}
		return hasExceptionSyntaxAST(value.Body) || hasExceptionSyntaxAST(value.Else) || hasExceptionSyntaxStatementAST(value.ElseIf)
	case *ForStmt:
		if value == nil {
			return false
		}
		return hasExceptionSyntaxAST(value.Body)
	case *SwitchStmt:
		if value == nil {
			return false
		}
		return hasExceptionSyntaxAST(value.Body)
	case *CaseStmt:
		if value == nil {
			return false
		}
		return hasExceptionSyntaxAST(value.Clause.Body)
	case *BlockStmt:
		if value == nil {
			return false
		}
		return hasExceptionSyntaxAST(value)
	default:
		return false
	}
}

// validateExceptionASTBlock performs exception-specific semantic checks before
// a function enters the broader node-based lowering path. A construct may be
// unsuitable for the strict fast path while still being representable by the
// AST compatibility wrapper.
func validateExceptionASTBlock(block *BlockStmt, context constructorContext) error {
	if block == nil {
		return nil
	}
	for _, statement := range block.Statements {
		if err := validateExceptionASTStatement(statement, context); err != nil {
			return err
		}
	}
	return nil
}

func validateExceptionASTStatement(statement Stmt, context constructorContext) error {
	if statement == nil || isNilStmt(statement) {
		return nil
	}
	switch value := statement.(type) {
	case *ThrowStmt:
		if value.Value == nil {
			return fmt.Errorf("bare throw is only valid inside catch")
		}
		return validateThrowExpressionNode(value.Value, context)
	case *TryStmt:
		if err := validateASTCatchClauses(value, context); err != nil {
			return err
		}
		if err := validateExceptionASTBlock(value.Body, context); err != nil {
			return err
		}
		for _, clause := range value.Catches {
			if err := validateExceptionASTBlock(clause.Body, context); err != nil {
				return err
			}
		}
		if err := validateExceptionASTBlock(value.Finally, context); err != nil {
			return err
		}
		return validateFinallyControlTransfersBlock(value.Finally)
	case *IfStmt:
		if err := validateExceptionASTBlock(value.Body, context); err != nil {
			return err
		}
		if err := validateExceptionASTBlock(value.Else, context); err != nil {
			return err
		}
		return validateExceptionASTStatement(value.ElseIf, context)
	case *ForStmt:
		return validateExceptionASTBlock(value.Body, context)
	case *SwitchStmt:
		return validateExceptionASTBlock(value.Body, context)
	case *CaseStmt:
		return validateExceptionASTBlock(value.Clause.Body, context)
	case *BlockStmt:
		return validateExceptionASTBlock(value, context)
	case *TokenStmt:
		if err := validateExceptionASTBlock(value.Body, context); err != nil {
			return err
		}
		for _, child := range value.Children {
			if err := validateExceptionASTStatement(child, context); err != nil {
				return err
			}
		}
	}
	return nil
}

func directEmissionAllowed(context constructorContext) bool {
	// Direct emission is decided per statement/expression by the structural
	// safety checks below.  Package-wide features such as introspection must not
	// force unrelated functions through the legacy source-rewrite pipeline.
	return true
}

func directSignatureSafe(method Method) bool {
	for _, parameter := range method.ParameterAST {
		if typeNodeHasRecord(parameter.Type) || !directSignatureTypeSafe(parameter.Type) {
			return false
		}
	}
	if typeNodeHasRecord(method.ResultAST) || !directSignatureResultSafe(method.ResultAST, method.ResultFieldsAST) {
		return false
	}
	return true
}

func directSignatureTypeSafe(typeNode TypeNode) bool {
	if typeNode == nil {
		return true
	}
	if tuple, ok := typeNode.(*TupleType); ok {
		for _, element := range tuple.Elements {
			if !directSignatureTypeSafe(element) {
				return false
			}
		}
		return true
	}
	_, err := goTypeExpr(typeNode)
	return err == nil
}

func directSignatureResultSafe(result TypeNode, fields []ParameterNode) bool {
	if len(fields) > 0 {
		for _, field := range fields {
			if !directSignatureTypeSafe(field.Type) {
				return false
			}
		}
		return true
	}
	return directSignatureTypeSafe(result)
}

func typeNodeHasRecord(typeNode TypeNode) bool {
	if typeNode == nil {
		return false
	}
	switch value := typeNode.(type) {
	case *NamedType:
		if len(value.Parts) == 1 && value.Parts[0] == "record" {
			return true
		}
		for _, argument := range value.Arguments {
			if typeNodeHasRecord(argument) {
				return true
			}
		}
	case *PointerType:
		return typeNodeHasRecord(value.Element)
	case *SliceType:
		return typeNodeHasRecord(value.Element)
	case *ArrayType:
		return typeNodeHasRecord(value.Element)
	case *MapType:
		return typeNodeHasRecord(value.Key) || typeNodeHasRecord(value.Value)
	case *ChannelType:
		return typeNodeHasRecord(value.Element)
	case *VariadicType:
		return typeNodeHasRecord(value.Element)
	case *FunctionType:
		for _, parameter := range value.Parameters {
			if typeNodeHasRecord(parameter.Type) {
				return true
			}
		}
		for _, result := range value.Results {
			if typeNodeHasRecord(result) {
				return true
			}
		}
	case *StructType:
		for _, field := range value.Fields {
			if typeNodeHasRecord(field.Type) {
				return true
			}
		}
	case *InterfaceType:
		for _, embed := range value.Embeds {
			if typeNodeHasRecord(embed) {
				return true
			}
		}
		for _, method := range value.Methods {
			if typeNodeHasRecord(method.Signature) {
				return true
			}
		}
	case *TupleType:
		for _, element := range value.Elements {
			if typeNodeHasRecord(element) {
				return true
			}
		}
	case *UnderlyingType:
		return typeNodeHasRecord(value.Element)
	case *UnionType:
		for _, term := range value.Terms {
			if typeNodeHasRecord(term) {
				return true
			}
		}
	case *TokenType:
		for _, token := range value.Tokens {
			if token.Text == "record" {
				return true
			}
		}
	}
	return false
}

// formatDirectFunctionDecl formats the fully lowered function declaration at
// the Go-generation boundary. The declaration, signature, and body are all
// Go AST nodes; no function header is assembled as source and reparsed.
func formatDirectFunctionDecl(name string, receiver *ast.Field, typeParameters []TypeParameterNode, parameters []ParameterNode, result TypeNode, resultFields []ParameterNode, context constructorContext, transformResult bool, body *ast.BlockStmt) (string, bool, error) {
	parameterFields, err := directParameterFields(parameters, context)
	if err != nil {
		return "", false, nil
	}
	resultList, err := directResultFields(result, resultFields, context, transformResult)
	if err != nil {
		return "", false, nil
	}
	typeParameterFields, err := directTypeParameterFields(typeParameters)
	if err != nil {
		return "", false, nil
	}
	declaration := &ast.FuncDecl{
		Name: ast.NewIdent(name),
		Type: &ast.FuncType{
			Params:     &ast.FieldList{List: parameterFields},
			Results:    resultList,
			TypeParams: typeParameterFields,
		},
		Body: body,
	}
	if receiver != nil {
		declaration.Recv = &ast.FieldList{List: []*ast.Field{receiver}}
	}
	var output bytes.Buffer
	if err := format.Node(&output, token.NewFileSet(), declaration); err != nil {
		return "", true, err
	}
	return output.String(), true, nil
}

func directParameterFields(parameters []ParameterNode, context constructorContext) ([]*ast.Field, error) {
	result := make([]*ast.Field, 0, len(parameters))
	for _, parameter := range parameters {
		typeNode, ok := transformPolymorphicTypeNode(parameter.Type, context)
		if !ok || typeNode == nil {
			return nil, fmt.Errorf("unsupported parameter type")
		}
		typeExpr, err := goTypeExpr(typeNode)
		if err != nil {
			return nil, err
		}
		field := &ast.Field{Type: typeExpr}
		if parameter.Name != "" {
			field.Names = []*ast.Ident{ast.NewIdent(parameter.Name)}
		}
		result = append(result, field)
	}
	return result, nil
}

func directResultFields(result TypeNode, named []ParameterNode, context constructorContext, transformResult bool) (*ast.FieldList, error) {
	if result == nil && len(named) == 0 {
		return nil, nil
	}
	fields := make([]ParameterNode, 0, len(named))
	useNamedFields := len(named) > 0 && (result == nil || hasNamedResultField(named))
	if useNamedFields {
		fields = append(fields, named...)
	} else if tuple, ok := result.(*TupleType); ok {
		for _, element := range tuple.Elements {
			fields = append(fields, ParameterNode{Type: element})
		}
	} else {
		fields = append(fields, ParameterNode{Type: result})
	}
	output := &ast.FieldList{}
	for _, fieldNode := range fields {
		typeNode := fieldNode.Type
		if transformResult {
			var ok bool
			typeNode, ok = directResultTypeNode(typeNode, context)
			if !ok {
				return nil, fmt.Errorf("unsupported result type")
			}
		}
		typeExpr, err := goTypeExpr(typeNode)
		if err != nil {
			return nil, err
		}
		field := &ast.Field{Type: typeExpr}
		if fieldNode.Name != "" {
			field.Names = []*ast.Ident{ast.NewIdent(fieldNode.Name)}
		}
		output.List = append(output.List, field)
	}
	return output, nil
}

func hasNamedResultField(fields []ParameterNode) bool {
	for _, field := range fields {
		if field.Name != "" {
			return true
		}
	}
	return false
}

func directResultTypeNode(typeNode TypeNode, context constructorContext) (TypeNode, bool) {
	if typeNode == nil {
		return nil, false
	}
	transformed, ok := transformPolymorphicTypeNode(typeNode, context)
	if !ok || transformed == nil {
		return nil, false
	}
	if transformed != typeNode {
		return transformed, true
	}
	if _, pointer := typeNode.(*PointerType); pointer {
		return typeNode, true
	}
	named, ok := typeNode.(*NamedType)
	if !ok || len(named.Parts) != 1 {
		return typeNode, true
	}
	target, exists := context.Targets[named.Parts[0]]
	if !exists || !classParticipatesInDispatch(target.Class, target.Classes) {
		return typeNode, true
	}
	return &PointerType{Element: typeNode}, true
}

func directTypeParameterFields(parameters []TypeParameterNode) (*ast.FieldList, error) {
	if len(parameters) == 0 {
		return nil, nil
	}
	result := &ast.FieldList{}
	for _, parameter := range parameters {
		if parameter.Name == "" {
			return nil, fmt.Errorf("type parameter requires a name")
		}
		constraint := parameter.Constraint
		if constraint == nil {
			constraint = &NamedType{Parts: []string{"any"}}
		}
		typeExpr, err := goTypeExpr(constraint)
		if err != nil {
			return nil, err
		}
		result.List = append(result.List, &ast.Field{Names: []*ast.Ident{ast.NewIdent(parameter.Name)}, Type: typeExpr})
	}
	return result, nil
}

func directExtensionTypeParameters(extension extensionMethod) []TypeParameterNode {
	parameters := []TypeParameterNode{}
	names := extensionTargetTypeParameterNames(extension.Target)
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		constraint := extension.TargetConstraints[name]
		if constraint == nil {
			constraintName := "any"
			if name == extensionMapKeyParameter(extension.Target) {
				constraintName = "comparable"
			}
			constraint = &NamedType{Parts: []string{constraintName}}
		}
		parameters = append(parameters, TypeParameterNode{Name: name, Constraint: constraint})
	}
	parameters = append(parameters, extension.Method.TypeParamsAST...)
	return parameters
}

func directNamedType(source string) TypeNode {
	return parseTypeText(strings.TrimSpace(source))
}

func directExtensionParameterTypeNode(typeNode TypeNode, context constructorContext) TypeNode {
	transformed, ok := transformPolymorphicTypeNode(typeNode, context)
	if !ok || transformed == nil {
		return typeNode
	}
	if transformed != typeNode {
		return transformed
	}
	named, ok := typeNode.(*NamedType)
	if !ok || len(named.Parts) != 1 {
		return transformed
	}
	target, exists := context.Targets[named.Parts[0]]
	if !exists || target.Class == nil || target.Qualifier != "" {
		return transformed
	}
	return &NamedType{Parts: []string{"Gpp" + target.Class.Name}}
}

// directBlockSafe rejects syntax whose meaning depends on a Go++ lowering
// pass. Token fallbacks and comments are rejected rather than silently lost.
func directBlockSafe(block *BlockStmt, context constructorContext) bool {
	if block == nil {
		return true
	}
	if len(block.Comments) > 0 {
		return false
	}
	for _, statement := range block.Statements {
		if !directStatementSafe(statement, context) {
			return false
		}
	}
	return true
}

func directStatementSafe(statement Stmt, context constructorContext) bool {
	switch value := statement.(type) {
	case *TokenStmt, *TryStmt, *ThrowStmt:
		return false
	case *ExpressionStmt:
		return directExpressionSafe(value.Expression, context)
	case *DeclarationStmt:
		for _, expression := range value.Values {
			if !directExpressionSafe(expression, context) {
				return false
			}
		}
	case *AssignmentStmt:
		for _, expression := range append(append([]ExprNode{}, value.Left...), value.Right...) {
			if !directExpressionSafe(expression, context) {
				return false
			}
		}
	case *ReturnStmt:
		for _, expression := range value.Values {
			if !directExpressionSafe(expression, context) {
				return false
			}
		}
	case *DeferStmt:
		return directExpressionSafe(value.Expression, context)
	case *GoStmt:
		return directExpressionSafe(value.Expression, context)
	case *SendStmt:
		if _, call := value.Channel.(*CallExpr); call {
			// A call result is not an assignable channel expression. If the
			// parser has merged adjacent newline statements into a send node,
			// leave it for the source-compatible path instead of emitting invalid
			// Go such as close(done) <- value.
			return false
		}
		return directExpressionSafe(value.Channel, context) && directExpressionSafe(value.Value, context)
	case *IncDecStmt:
		return directExpressionSafe(value.Expression, context)
	case *IfStmt:
		return directExpressionSafe(value.Init, context) && directExpressionSafe(value.Condition, context) && directBlockSafe(value.Body, context) && directBlockSafe(value.Else, context) && (value.ElseIf == nil || directStatementSafe(value.ElseIf, context))
	case *ForStmt:
		return directExpressionSafe(value.Init, context) && directExpressionSafe(value.Condition, context) && directExpressionSafe(value.Post, context) && directExpressionSafe(value.RangeExpr, context) && directBlockSafe(value.Body, context)
	case *SwitchStmt:
		return directExpressionSafe(value.Init, context) && directExpressionSafe(value.Tag, context) && directBlockSafe(value.Body, context)
	case *CaseStmt:
		for _, expression := range value.Clause.Expressions {
			if !directExpressionSafe(expression, context) {
				return false
			}
		}
		return directBlockSafe(value.Clause.Body, context)
	case *TypeDeclarationStmt:
		return true
	case *LabelStmt:
		return value.Name != ""
	case *BranchStmt:
		return value.Keyword == "break" || value.Keyword == "continue" || value.Keyword == "fallthrough" || value.Keyword == "goto"
	default:
		return false
	}
	return true
}

func directExpressionSafe(expression ExprNode, context constructorContext) bool {
	if expression == nil {
		return true
	}
	switch value := expression.(type) {
	case *NameExpr, *LiteralExpr, *TypeExpr:
		return true
	case *UnaryExpr:
		return directExpressionSafe(value.Operand, context)
	case *BinaryExpr:
		return value.Operator != "??" && directExpressionSafe(value.Left, context) && directExpressionSafe(value.Right, context)
	case *PostfixExpr:
		return directExpressionSafe(value.Expression, context) && (value.Operator == "++" || value.Operator == "--")
	case *AssignmentExpr:
		for _, expression := range append(append([]ExprNode{}, value.Left...), value.Right...) {
			if !directExpressionSafe(expression, context) {
				return false
			}
		}
		return true
	case *SelectorExpr:
		if value.Safe || !directExpressionSafe(value.Receiver, context) {
			return false
		}
		if context.Introspection != nil && context.Introspection.Enabled && directIntrospectionSelector(value) {
			// Introspection selectors still use the scoped metadata rewrite. Do
			// not emit them as ordinary Go selectors until that rewrite also
			// returns expression nodes.
			return false
		}
		// Enum members, metadata, and helpers are rewritten to generated Go
		// names by the enum pass. Do not let a structurally valid selector skip
		// that semantic lowering merely because it is valid Go syntax.
		if directEnumReference(value.Receiver, context) {
			return false
		}
		if staticMethodReceiverKey(value.Receiver, value.Name, context) != "" {
			return false
		}
		if directExtensionCallNeedsLowering(value, context) {
			return false
		}
		return true
	case *IndexExpr:
		return directExpressionSafe(value.Receiver, context) && directExpressionSafe(value.Index, context)
	case *IndexListExpr:
		if !directExpressionSafe(value.Receiver, context) {
			return false
		}
		for _, index := range value.Indices {
			if !directExpressionSafe(index, context) {
				return false
			}
		}
		return true
	case *SliceExpr:
		return directExpressionSafe(value.Receiver, context) && directExpressionSafe(value.Low, context) && directExpressionSafe(value.High, context) && directExpressionSafe(value.Max, context)
	case *TypeAssertExpr:
		return directExpressionSafe(value.Expression, context)
	case *CallExpr:
		if selector, ok := value.Callee.(*SelectorExpr); ok {
			if context.Templates != nil && context.Templates[selector.Name] != nil {
				// Template calls need the template registry/static wrapper lowering.
				return false
			}
			if receiver, ok := selector.Receiver.(*NameExpr); ok && strings.HasSuffix(context.AvailableImports[receiver.Name], "/gpp/tpl") {
				// Keep imported template facades on the compatibility path when
				// this file has no local template metadata to lower them.
				return false
			}
		}
		if name, ok := value.Callee.(*NameExpr); ok && name.Name == "record" {
			// Record calls are Go++ syntax and must be lowered to a generated
			// structural type before direct Go AST emission.
			return false
		}
		if runtimeWrapper, ok := value.Callee.(*NameExpr); ok && runtimeWrapper.Name == "__gpp_safe" {
			if len(value.Arguments) != 2 || !directExpressionSafe(value.Arguments[0].Value, context) {
				return false
			}
			access, ok := value.Arguments[1].Value.(*FunctionLiteralExpr)
			return ok && directExpressionSafe(access, context)
		}
		if runtimeWrapper, ok := value.Callee.(*NameExpr); ok && isDirectRuntimeWrapper(runtimeWrapper.Name) {
			if len(value.Arguments) != 1 {
				return false
			}
			inner, ok := value.Arguments[0].Value.(*CallExpr)
			return ok && directCallShapeSafe(inner, context)
		}
		if _, _, isConstructor := constructorTargetForCallee(value.Callee, context); isConstructor {
			return false
		}
		if _, found, err := overloadCallName(value, context.CurrentParameterTypes, context.Overloads); err != nil || found {
			return false
		}
		// A single error result is already a normal Go value when it is assigned,
		// returned, or passed onward. Implicit exception promotion only changes
		// multi-result calls whose trailing error must be unwrapped; keeping the
		// former case on the source fallback needlessly blocks direct AST output
		// for ordinary calls such as fmt.Errorf and hook-style error helpers.
		if promoted, ok := promotedCallForExpr(value, context, context.CurrentParameterTypes); ok && promoted.trailingError && len(promoted.types) > 1 {
			return false
		}
		if value.Callee == nil || !directExpressionSafe(value.Callee, context) {
			return false
		}
		for _, argument := range value.Arguments {
			if argument.Name != "" || !directExpressionSafe(argument.Value, context) {
				return false
			}
		}
		return true
	case *ParenthesizedExpr:
		return directExpressionSafe(value.Inner, context)
	case *SpreadExpr:
		return directExpressionSafe(value.Expression, context)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			if !directExpressionSafe(element.Key, context) || !directExpressionSafe(element.Value, context) {
				return false
			}
		}
		return true
	case *FunctionLiteralExpr:
		return directBlockSafe(value.Body, context)
	case *SendExpr:
		return directExpressionSafe(value.Channel, context) && directExpressionSafe(value.Value, context)
	default:
		return false
	}
}

func directIntrospectionSelector(selector *SelectorExpr) bool {
	if selector == nil {
		return false
	}
	if selector.Name == "class" {
		return true
	}
	_, metadataName := introspectionSelectorNames[selector.Name]
	return metadataName
}

func directEnumReference(expression ExprNode, context constructorContext) bool {
	path, ok := directSelectorPath(expression)
	if !ok || context.Enums == nil {
		return false
	}
	_, exists := context.Enums[path]
	return exists
}

func directSelectorPath(expression ExprNode) (string, bool) {
	switch value := expression.(type) {
	case *NameExpr:
		return value.Name, true
	case *SelectorExpr:
		prefix, ok := directSelectorPath(value.Receiver)
		if !ok || value.Safe {
			return "", false
		}
		return prefix + "." + value.Name, true
	default:
		return "", false
	}
}

func isDirectRuntimeWrapper(name string) bool {
	switch name {
	case "__gppUnwrap", "__gppUnwrap2", "__gppUnwrap3", "__gppDiscard", "__gppDiscard2", "__gppDiscard3":
		return true
	default:
		return false
	}
}

func directCallShapeSafe(call *CallExpr, context constructorContext) bool {
	if call == nil || call.Callee == nil || !directExpressionSafe(call.Callee, context) {
		return false
	}
	for _, argument := range call.Arguments {
		if argument.Name != "" || !directExpressionSafe(argument.Value, context) {
			return false
		}
	}
	return true
}

func directExtensionCallNeedsLowering(selector *SelectorExpr, context constructorContext) bool {
	if selector == nil {
		return false
	}
	actual := staticExpressionTypeNode(selector.Receiver, context, context.CurrentParameterTypes)
	if receiver, ok := selector.Receiver.(*NameExpr); ok && context.CurrentParameterTypes[receiver.Name] == "" {
		// staticExpressionTypeNode intentionally preserves an unresolved name as
		// a possible type name. For direct emission, a local receiver with no
		// inferred type must be treated as unknown; otherwise `text.Empty()` can
		// be emitted before the extension rewriter gets a chance to lower it.
		if _, isClass := context.Targets[receiver.Name]; !isClass && len(context.NativeMethods[receiver.Name]) == 0 {
			actual = ""
		}
	}
	for _, extensions := range [][]extensionMethod{context.Extensions, context.PreludeExtensions} {
		for _, extension := range extensions {
			if extension.Method.Name != selector.Name {
				continue
			}
			if actual == "" {
				return true
			}
			if !extensionTargetMatches(extension.Target, extension.ReceiverType, actual, context) {
				continue
			}
			if !realMethodAppliesAST(actual, selector.Name, nil, context) {
				return true
			}
		}
	}
	return false
}
