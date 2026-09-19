package compiler

import (
	"fmt"
	"strings"
)

type introspectionASTEdit struct {
	start int
	end   int
	text  string
}

func transformIntrospectionInFunctionSource(src string, context constructorContext) (string, error) {
	if transformed, handled, err := transformIntrospectionAST(src, context); handled {
		return transformed, err
	}
	return transformIntrospection(src, context)
}

func transformIntrospectionAST(src string, context constructorContext) (string, bool, error) {
	if context.Introspection == nil && len(context.Targets) == 0 {
		return src, false, nil
	}
	if transformed, handled, err := transformIntrospectionClassAST(src, context); handled {
		if err != nil {
			return "", true, err
		}
		src = transformed
	}
	transformed, handled, err := transformIntrospectionMetadataAST(src, context)
	if err != nil {
		return "", true, err
	}
	if handled {
		return transformed, true, nil
	}
	return src, strings.Contains(src, ".class"), nil
}

func transformIntrospectionMetadataAST(src string, context constructorContext) (string, bool, error) {
	if !strings.Contains(src, ".") {
		return src, false, nil
	}
	bodies, handled := introspectionASTBodies(src, context)
	if !handled {
		return src, false, nil
	}
	valueTypes := introspectionASTValueTypes(src, bodies, context)
	metadataTypes := map[string]int{}
	if context.CurrentClass != "" {
		valueTypes["this"] = "*" + context.CurrentClass
	}

	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	addEdit := func(start, end int, text string) {
		if start >= 0 && end >= start && end <= len(src) {
			edits = append(edits, edit{start: start, end: end, text: text})
		}
	}
	for _, body := range bodies {
		collectIntrospectionMetadataBlock(body, metadataTypes, context, valueTypes, addEdit)
		walkIntrospectionBlockExpressions(body, func(expression ExprNode) {
			selector, ok := expression.(*SelectorExpr)
			if !ok {
				return
			}
			kind := introspectionExpressionKindNode(selector.Receiver, metadataTypes, context, valueTypes)
			if replacement, exists := introspectionSelectorNames[selector.Name]; exists && introspectionSelectorValid(kind, selector.Name) {
				span := selector.Span()
				addEdit(span.End-len(selector.Name), span.End, replacement)
			}
			if selector.Name == "type" {
				span := selector.Span()
				addEdit(span.End-len(selector.Name), span.End, "Type")
			}
		})
		walkIntrospectionBlockExpressions(body, func(expression ExprNode) {
			call, ok := expression.(*CallExpr)
			if !ok || len(call.Arguments) == 0 {
				return
			}
			selector, ok := call.Callee.(*SelectorExpr)
			if !ok {
				return
			}
			kind := introspectionExpressionKindNode(selector.Receiver, metadataTypes, context, valueTypes)
			if kind == introspectionField && (selector.Name == "set" || selector.Name == "addr") {
				argument := call.Arguments[0].Value
				actual := staticExpressionTypeNode(argument, context, valueTypes)
				if actual != "" && !strings.HasPrefix(strings.TrimSpace(actual), "*") && introspectionClassNameNode(argument, context, valueTypes) != "" {
					span := argument.Span()
					addEdit(span.Start, span.Start, "&")
				}
			}
			if kind != introspectionAnnotations || (selector.Name != "Has" && selector.Name != "Get" && selector.Name != "All" && selector.Name != "has" && selector.Name != "get" && selector.Name != "all") {
				return
			}
			name, err := expressionNodeSource(call.Arguments[0].Value)
			if err != nil {
				return
			}
			declaration := context.Annotations[name]
			if declaration == nil {
				for qualified, candidate := range context.Annotations {
					suffix := qualified
					if dot := strings.LastIndex(suffix, "."); dot >= 0 {
						suffix = suffix[dot+1:]
					}
					if suffix == name {
						declaration = candidate
						break
					}
				}
			}
			if declaration == nil {
				return
			}
			span := call.Arguments[0].Value.Span()
			addEdit(span.Start, span.End, annotationDescriptorReference(AnnotationUse{Name: name}, declaration))
		})
	}
	if len(edits) == 0 {
		return src, true, nil
	}
	for index := range edits {
		for previous := index + 1; previous < len(edits); previous++ {
			if edits[previous].start > edits[index].start {
				edits[index], edits[previous] = edits[previous], edits[index]
			}
		}
	}
	for _, edit := range edits {
		src = src[:edit.start] + edit.text + src[edit.end:]
	}
	return src, true, nil
}

func collectIntrospectionMetadataBlock(block *BlockStmt, metadataTypes map[string]int, context constructorContext, valueTypes map[string]string, addEdit func(int, int, string)) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectIntrospectionMetadataStatement(statement, metadataTypes, context, valueTypes, addEdit)
	}
}

func collectIntrospectionMetadataStatement(statement Stmt, metadataTypes map[string]int, context constructorContext, valueTypes map[string]string, addEdit func(int, int, string)) {
	if statement == nil {
		return
	}
	assign := func(names []Token, values []ExprNode) {
		for index, name := range names {
			if index < len(values) && name.Text != "" {
				if kind := introspectionExpressionKindNode(values[index], metadataTypes, context, valueTypes); kind != introspectionUnknown {
					metadataTypes[name.Text] = int(kind)
				}
			}
		}
	}
	switch value := statement.(type) {
	case *DeclarationStmt:
		assign(value.Names, value.Values)
	case *AssignmentStmt:
		names := make([]*NameExpr, 0, len(value.Left))
		for _, left := range value.Left {
			if name, ok := left.(*NameExpr); ok {
				names = append(names, name)
			} else {
				names = append(names, nil)
			}
		}
		for index, name := range names {
			if name != nil && index < len(value.Right) {
				if kind := introspectionExpressionKindNode(value.Right[index], metadataTypes, context, valueTypes); kind != introspectionUnknown {
					metadataTypes[name.Name] = int(kind)
				}
			}
		}
	case *ForStmt:
		kind := introspectionExpressionKindNode(value.RangeExpr, metadataTypes, context, valueTypes)
		if kind == introspectionFields || kind == introspectionMethods || kind == introspectionParents || kind == introspectionParameters {
			if name, start, ok := introspectionRangeValue(value.RangeKey); ok {
				metadataTypes[name] = int(map[introspectionExprKind]introspectionExprKind{
					introspectionFields:     introspectionField,
					introspectionMethods:    introspectionMethod,
					introspectionParents:    introspectionClass,
					introspectionParameters: introspectionParameter,
				}[kind])
				if start >= 0 && len(value.RangeKey) > 0 && value.RangeKey[0].Text != "_" {
					addEdit(start, start, "_, ")
				}
			}
		}
		collectIntrospectionMetadataBlock(value.Body, metadataTypes, context, valueTypes, addEdit)
	case *TokenStmt:
		collectIntrospectionMetadataBlock(value.Body, metadataTypes, context, valueTypes, addEdit)
		for _, child := range value.Children {
			collectIntrospectionMetadataStatement(child, metadataTypes, context, valueTypes, addEdit)
		}
	case *IfStmt:
		collectIntrospectionMetadataBlock(value.Body, metadataTypes, context, valueTypes, addEdit)
		collectIntrospectionMetadataBlock(value.Else, metadataTypes, context, valueTypes, addEdit)
		if value.ElseIf != nil {
			collectIntrospectionMetadataStatement(value.ElseIf, metadataTypes, context, valueTypes, addEdit)
		}
	case *SwitchStmt:
		collectIntrospectionMetadataBlock(value.Body, metadataTypes, context, valueTypes, addEdit)
	case *CaseStmt:
		collectIntrospectionMetadataBlock(value.Clause.Body, metadataTypes, context, valueTypes, addEdit)
	case *TryStmt:
		collectIntrospectionMetadataBlock(value.Body, metadataTypes, context, valueTypes, addEdit)
		for _, clause := range value.Catches {
			collectIntrospectionMetadataBlock(clause.Body, metadataTypes, context, valueTypes, addEdit)
		}
		collectIntrospectionMetadataBlock(value.Finally, metadataTypes, context, valueTypes, addEdit)
	}
}

func introspectionRangeValue(tokens []Token) (string, int, bool) {
	if len(tokens) == 0 {
		return "", 0, false
	}
	ids := []Token{}
	for _, token := range tokens {
		if token.Kind == TokenIdentifier || token.Kind == TokenKeyword {
			if token.Text != "_" && token.Text != "range" {
				ids = append(ids, token)
			}
		}
	}
	if len(ids) == 0 {
		return "", 0, false
	}
	return ids[len(ids)-1].Text, ids[0].Span.Start, true
}

func introspectionExpressionKindNode(expression ExprNode, variables map[string]int, context constructorContext, valueTypes map[string]string) introspectionExprKind {
	if expression == nil {
		return introspectionUnknown
	}
	switch value := expression.(type) {
	case *NameExpr:
		if kind, ok := variables[value.Name]; ok {
			return introspectionExprKind(kind)
		}
		if strings.HasPrefix(value.Name, "Gpp") && strings.HasSuffix(value.Name, "Class") {
			return introspectionClass
		}
		if target, ok := context.Targets[value.Name]; ok && target.Class != nil {
			return introspectionClass
		}
		if strings.HasPrefix(value.Name, "__gpp_") {
			if target, ok := context.Targets[strings.TrimPrefix(value.Name, "__gpp_")]; ok && target.Class != nil {
				return introspectionClass
			}
		}
	case *ParenthesizedExpr:
		return introspectionExpressionKindNode(value.Inner, variables, context, valueTypes)
	case *CallExpr:
		if selector, ok := value.Callee.(*SelectorExpr); ok {
			if selector.Name == "__gpp_class" || selector.Name == "GppRuntimeClass" {
				return introspectionClass
			}
			base := introspectionExpressionKindNode(selector.Receiver, variables, context, valueTypes)
			if base == introspectionAnnotations && selector.Name == "Get" || base == introspectionAnnotations && selector.Name == "get" {
				return introspectionAnnotation
			}
			if base == introspectionAnnotations && (selector.Name == "All" || selector.Name == "all") {
				return introspectionAnnotations
			}
		}
	case *IndexExpr:
		return introspectionIndexedKind(introspectionExpressionKindNode(value.Receiver, variables, context, valueTypes))
	case *IndexListExpr:
		return introspectionIndexedKind(introspectionExpressionKindNode(value.Receiver, variables, context, valueTypes))
	case *SelectorExpr:
		if value.Name == "class" {
			if name, ok := value.Receiver.(*NameExpr); ok {
				if target, exists := context.Targets[name.Name]; exists && target.Class != nil {
					return introspectionClass
				}
			}
			if className := introspectionClassNameNode(value.Receiver, context, valueTypes); className != "" {
				return introspectionClass
			}
		}
		if strings.HasPrefix(value.Name, "Gpp") && strings.HasSuffix(value.Name, "Class") {
			return introspectionClass
		}
		base := introspectionExpressionKindNode(value.Receiver, variables, context, valueTypes)
		switch value.Name {
		case "fields", "Fields":
			if base == introspectionClass {
				return introspectionFields
			}
		case "methods", "Methods":
			if base == introspectionClass {
				return introspectionMethods
			}
		case "parents", "Parents":
			if base == introspectionClass {
				return introspectionParents
			}
		case "parameters", "Parameters":
			if base == introspectionMethod {
				return introspectionParameters
			}
		case "annotations", "Annotations":
			if base == introspectionClass || base == introspectionField || base == introspectionMethod || base == introspectionParameter {
				return introspectionAnnotations
			}
		case "has", "Has", "get", "Get", "all", "All":
			if base == introspectionAnnotations {
				if value.Name == "get" || value.Name == "Get" {
					return introspectionAnnotation
				}
				return introspectionAnnotations
			}
		case "type", "Type":
			if base == introspectionField || base == introspectionParameter {
				return introspectionType
			}
		case "owner", "Owner":
			if base == introspectionField || base == introspectionMethod {
				return introspectionClass
			}
		case "result", "Result":
			if base == introspectionMethod {
				return introspectionType
			}
		case "name", "Name":
			if base == introspectionParameter {
				return introspectionParameter
			}
		}
		if base == introspectionAnnotation && (value.Name == "name" || value.Name == "Name" || value.Name == "fullName" || value.Name == "FullName" || value.Name == "args" || value.Name == "Args") {
			return introspectionAnnotation
		}
	}
	return introspectionUnknown
}

func introspectionIndexedKind(kind introspectionExprKind) introspectionExprKind {
	switch kind {
	case introspectionFields:
		return introspectionField
	case introspectionMethods:
		return introspectionMethod
	case introspectionParameters:
		return introspectionParameter
	case introspectionAnnotations:
		return introspectionAnnotation
	default:
		return introspectionUnknown
	}
}

func introspectionSelectorValid(kind introspectionExprKind, name string) bool {
	switch kind {
	case introspectionClass:
		return name == "name" || name == "fields" || name == "annotations" || name == "methods" || name == "parents"
	case introspectionField:
		return name == "name" || name == "owner" || name == "type" || name == "get" || name == "set" || name == "addr" || name == "annotations"
	case introspectionMethod:
		return name == "name" || name == "owner" || name == "parameters" || name == "result" || name == "annotations" || name == "static"
	case introspectionParameter:
		return name == "name" || name == "type" || name == "annotations"
	case introspectionAnnotations:
		return name == "has" || name == "get" || name == "all"
	case introspectionAnnotation:
		return name == "name" || name == "fullName" || name == "args"
	case introspectionType:
		return name == "name"
	default:
		return false
	}
}

func walkIntrospectionBlockExpressions(block *BlockStmt, visit func(ExprNode)) {
	if block == nil || visit == nil {
		return
	}
	for _, statement := range block.Statements {
		walkStmtExpressions(statement, func(expression ExprNode) {
			walkIntrospectionExpression(expression, visit)
		})
	}
}

func walkIntrospectionExpression(expression ExprNode, visit func(ExprNode)) {
	if expression == nil {
		return
	}
	visit(expression)
	switch value := expression.(type) {
	case *UnaryExpr:
		walkIntrospectionExpression(value.Operand, visit)
	case *BinaryExpr:
		walkIntrospectionExpression(value.Left, visit)
		walkIntrospectionExpression(value.Right, visit)
	case *SelectorExpr:
		walkIntrospectionExpression(value.Receiver, visit)
	case *IndexExpr:
		walkIntrospectionExpression(value.Receiver, visit)
		walkIntrospectionExpression(value.Index, visit)
	case *IndexListExpr:
		walkIntrospectionExpression(value.Receiver, visit)
		for _, index := range value.Indices {
			walkIntrospectionExpression(index, visit)
		}
	case *SliceExpr:
		walkIntrospectionExpression(value.Receiver, visit)
		walkIntrospectionExpression(value.Low, visit)
		walkIntrospectionExpression(value.High, visit)
		walkIntrospectionExpression(value.Max, visit)
	case *TypeAssertExpr:
		walkIntrospectionExpression(value.Expression, visit)
	case *CallExpr:
		walkIntrospectionExpression(value.Callee, visit)
		for _, argument := range value.Arguments {
			walkIntrospectionExpression(argument.Value, visit)
		}
	case *ParenthesizedExpr:
		walkIntrospectionExpression(value.Inner, visit)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			walkIntrospectionExpression(element.Key, visit)
			walkIntrospectionExpression(element.Value, visit)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			walkIntrospectionExpression(segment.Expression, visit)
		}
	case *LambdaExpr:
		walkIntrospectionExpression(value.Body, visit)
		walkIntrospectionBlockExpressions(value.BlockBody, visit)
	case *FunctionLiteralExpr:
		walkIntrospectionBlockExpressions(value.Body, visit)
	}
}

// transformIntrospectionClassAST lowers `.class` using the structured
// expression tree. Metadata member lowering is kept separate because it also
// needs scoped tracking for fields, methods, parameters, and annotations.
func transformIntrospectionClassAST(src string, context constructorContext) (string, bool, error) {
	if !strings.Contains(src, ".class") {
		return src, false, nil
	}

	bodies, handled := introspectionASTBodies(src, context)
	if !handled {
		return src, false, nil
	}
	valueTypes := introspectionASTValueTypes(src, bodies, context)
	if context.CurrentClass != "" {
		valueTypes["this"] = "*" + context.CurrentClass
	} else if context.CurrentExtensionReceiver != "" {
		valueTypes["this"] = context.CurrentExtensionReceiver
	}

	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	var invalidClassAccess string
	for _, body := range bodies {
		walkIntrospectionBlockExpressions(body, func(expression ExprNode) {
			selector, ok := expression.(*SelectorExpr)
			if !ok || selector.Name != "class" || selector.Receiver == nil {
				return
			}
			receiver, err := expressionNodeSource(selector.Receiver)
			if err != nil || strings.TrimSpace(receiver) == "" {
				return
			}
			if target, ok := introspectionStaticTargetNode(selector.Receiver, receiver, context); ok {
				edits = append(edits, edit{start: selector.Span().Start, end: selector.Span().End, text: introspectionDescriptorName(target)})
				return
			}
			className := introspectionClassNameNode(selector.Receiver, context, valueTypes)
			if className == "" {
				invalidClassAccess = receiver
				return
			}
			edits = append(edits, edit{start: selector.Span().Start, end: selector.Span().End, text: receiver + ".GppRuntimeClass()"})
		})
	}
	if invalidClassAccess != "" {
		return "", true, fmt.Errorf("%s.class is only available on Go++ class instances or class names", invalidClassAccess)
	}
	if len(edits) == 0 {
		return src, true, nil
	}
	for index := range edits {
		for previous := index + 1; previous < len(edits); previous++ {
			if edits[previous].start > edits[index].start {
				edits[index], edits[previous] = edits[previous], edits[index]
			}
		}
	}
	for _, edit := range edits {
		if edit.start >= 0 && edit.end <= len(src) && edit.start <= edit.end {
			src = src[:edit.start] + edit.text + src[edit.end:]
		}
	}
	return src, true, nil
}

func introspectionASTBodies(src string, context constructorContext) ([]*BlockStmt, bool) {
	functions := parseTopLevelFunctions("introspection", "main", src, 0, src, "", 0)
	bodies := make([]*BlockStmt, 0, len(functions))
	for _, function := range functions {
		if function == nil || function.Method.BodyAST == nil {
			return nil, false
		}
		bodies = append(bodies, function.Method.BodyAST)
	}
	if len(bodies) > 0 {
		return bodies, true
	}
	tokens, err := LexSource("introspection", src)
	if err != nil {
		return nil, false
	}
	block, err := ParseBodyAST(tokens)
	if err != nil || block == nil {
		return nil, false
	}
	return []*BlockStmt{block}, true
}

func introspectionASTValueTypes(src string, bodies []*BlockStmt, context constructorContext) map[string]string {
	result := cloneStringMap(context.CurrentParameterTypes)
	if result == nil {
		result = map[string]string{}
	}
	for _, body := range bodies {
		for name, typeName := range polymorphismValueTypesAST(body, context) {
			result[name] = typeName
		}
	}
	for _, function := range parseTopLevelFunctions("introspection", "main", src, 0, src, "", 0) {
		if function == nil {
			continue
		}
		for _, parameter := range function.Method.ParameterAST {
			if parameter.Name == "" || parameter.Type == nil {
				continue
			}
			if typeName, err := typeNodeSource(parameter.Type); err == nil {
				result[parameter.Name] = strings.TrimSpace(typeName)
			}
		}
	}
	return result
}

func introspectionStaticTargetNode(receiver ExprNode, source string, context constructorContext) (constructorTarget, bool) {
	if name, ok := receiver.(*NameExpr); ok {
		if target, exists := context.Targets[name.Name]; exists {
			return target, true
		}
	}
	if target, exists := context.Targets[source]; exists {
		return target, true
	}
	return constructorTarget{}, false
}

func introspectionClassNameNode(expression ExprNode, context constructorContext, valueTypes map[string]string) string {
	if name, ok := expression.(*NameExpr); ok && name.Name == "this" && context.CurrentClass != "" {
		return context.CurrentClass
	}
	typeName := strings.TrimPrefix(strings.TrimSpace(staticExpressionTypeNode(expression, context, valueTypes)), "*")
	if target, ok := context.Targets[typeName]; ok && target.Class != nil {
		return target.Class.Name
	}
	if strings.HasPrefix(typeName, "__gpp_") {
		if target, ok := context.Targets[strings.TrimPrefix(typeName, "__gpp_")]; ok && target.Class != nil {
			return target.Class.Name
		}
	}
	for key, target := range context.Targets {
		if target.Class != nil && target.Class.Name == typeName {
			return key
		}
	}
	return ""
}
