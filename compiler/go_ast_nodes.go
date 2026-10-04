package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"strconv"
	"strings"
)

func lowerInterpolatedStringExprNode(expression *InterpolatedStringExpr, context constructorContext) (ExprNode, error) {
	if expression == nil {
		return nil, fmt.Errorf("nil interpolated string expression")
	}
	if context.InterpolationName == "" {
		return nil, fmt.Errorf("interpolation formatter is not configured")
	}
	var formatText strings.Builder
	arguments := make([]CallArg, 0)
	for _, segment := range expression.Segments {
		if segment.Expression == nil && len(segment.ExpressionTokens) == 0 {
			text := segment.Text
			if !expression.Raw {
				decoded, err := strconv.Unquote(`"` + text + `"`)
				if err != nil {
					return nil, fmt.Errorf("invalid interpolated string literal: %w", err)
				}
				text = decoded
			}
			formatText.WriteString(strings.ReplaceAll(text, "%", "%%"))
			continue
		}
		formatSpec := segment.Format
		if formatSpec == "" {
			formatSpec = "%v"
		}
		if err := validateInterpolationFormat(formatSpec); err != nil {
			return nil, err
		}
		formatText.WriteString(formatSpec)
		value := segment.Expression
		if value == nil {
			parsed, err := ParseExpressionTokens(segment.ExpressionTokens)
			if err != nil {
				return nil, err
			}
			value = parsed
		}
		if value == nil {
			return nil, fmt.Errorf("empty interpolation expression")
		}
		lowered, err := lowerExceptionExprNode(value, context)
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, CallArg{Value: lowered})
	}
	if len(arguments) == 0 {
		return &LiteralExpr{Text: strconv.Quote(formatText.String()), Kind: TokenString}, nil
	}
	fmtCall := context.InterpolationName + ".Sprintf"
	if context.InterpolationName == "Sprintf" {
		fmtCall = context.InterpolationName
	}
	var callee ExprNode
	for _, part := range strings.Split(fmtCall, ".") {
		if part == "" {
			return nil, fmt.Errorf("invalid interpolation formatter %q", context.InterpolationName)
		}
		if callee == nil {
			callee = &NameExpr{Name: part}
		} else {
			callee = &SelectorExpr{Receiver: callee, Name: part}
		}
	}
	arguments = append([]CallArg{{Value: &LiteralExpr{Text: strconv.Quote(formatText.String()), Kind: TokenString}}}, arguments...)
	return &CallExpr{Callee: callee, Arguments: arguments}, nil
}

// goTypeExpr lowers the type forms that can appear in generated Go AST. It is
// deliberately structural: exception catch types no longer need to be printed
// and reparsed through go/parser just to become a type-switch expression.
func goTypeExpr(typeNode TypeNode) (ast.Expr, error) {
	switch value := typeNode.(type) {
	case *NamedType:
		if len(value.Parts) == 0 {
			return nil, fmt.Errorf("empty named type")
		}
		expression := ast.Expr(ast.NewIdent(value.Parts[0]))
		for _, part := range value.Parts[1:] {
			expression = &ast.SelectorExpr{X: expression, Sel: ast.NewIdent(part)}
		}
		if len(value.Arguments) == 0 {
			return expression, nil
		}
		arguments := make([]ast.Expr, 0, len(value.Arguments))
		for _, argument := range value.Arguments {
			lowered, err := goTypeExpr(argument)
			if err != nil {
				return nil, err
			}
			arguments = append(arguments, lowered)
		}
		if len(arguments) == 1 {
			return &ast.IndexExpr{X: expression, Index: arguments[0]}, nil
		}
		return &ast.IndexListExpr{X: expression, Indices: arguments}, nil
	case *PointerType:
		element, err := goTypeExpr(value.Element)
		if err != nil {
			return nil, err
		}
		return &ast.StarExpr{X: element}, nil
	case *SliceType:
		element, err := goTypeExpr(value.Element)
		if err != nil {
			return nil, err
		}
		return &ast.ArrayType{Elt: element}, nil
	case *ArrayType:
		element, err := goTypeExpr(value.Element)
		if err != nil {
			return nil, err
		}
		array := &ast.ArrayType{Elt: element}
		if value.Ellipsis {
			array.Len = &ast.Ellipsis{}
		} else if value.Length != nil {
			array.Len, err = goExprNode(value.Length)
			if err != nil {
				return nil, err
			}
		}
		return array, nil
	case *MapType:
		key, err := goTypeExpr(value.Key)
		if err != nil {
			return nil, err
		}
		element, err := goTypeExpr(value.Value)
		if err != nil {
			return nil, err
		}
		return &ast.MapType{Key: key, Value: element}, nil
	case *ChannelType:
		element, err := goTypeExpr(value.Element)
		if err != nil {
			return nil, err
		}
		channel := &ast.ChanType{Value: element}
		switch value.Direction {
		case "<-":
			channel.Dir = ast.RECV
		case "->":
			channel.Dir = ast.SEND
		case "", "both":
			// go/ast uses the combined direction bits for a bidirectional
			// channel. Zero is not the printable `chan T` form; it causes the
			// printer to emit only the element type when the channel appears as
			// a call argument (for example make(chan T, n)).
			channel.Dir = ast.SEND | ast.RECV
		default:
			return nil, fmt.Errorf("unsupported channel direction %q", value.Direction)
		}
		return channel, nil
	case *VariadicType:
		element, err := goTypeExpr(value.Element)
		if err != nil {
			return nil, err
		}
		return &ast.Ellipsis{Elt: element}, nil
	case *FunctionType:
		parameters := &ast.FieldList{}
		for _, parameter := range value.Parameters {
			typeExpr, err := goTypeExpr(parameter.Type)
			if err != nil {
				return nil, err
			}
			field := &ast.Field{Type: typeExpr}
			if parameter.Name != "" {
				field.Names = []*ast.Ident{ast.NewIdent(parameter.Name)}
			}
			parameters.List = append(parameters.List, field)
		}
		results := &ast.FieldList{}
		for _, result := range value.Results {
			typeExpr, err := goTypeExpr(result)
			if err != nil {
				return nil, err
			}
			results.List = append(results.List, &ast.Field{Type: typeExpr})
		}
		var resultList *ast.FieldList
		if len(results.List) > 0 {
			resultList = results
		}
		return &ast.FuncType{Params: parameters, Results: resultList}, nil
	case *StructType:
		fields := &ast.FieldList{}
		for _, fieldNode := range value.Fields {
			field := &ast.Field{}
			for _, name := range fieldNode.Names {
				field.Names = append(field.Names, ast.NewIdent(name))
			}
			var err error
			field.Type, err = goTypeExpr(fieldNode.Type)
			if err != nil {
				return nil, err
			}
			if fieldNode.Tag != "" {
				field.Tag = &ast.BasicLit{Kind: gotoken.STRING, Value: fieldNode.Tag}
			}
			fields.List = append(fields.List, field)
		}
		return &ast.StructType{Fields: fields}, nil
	case *InterfaceType:
		methods := &ast.FieldList{}
		for _, embed := range value.Embeds {
			typeExpr, err := goTypeExpr(embed)
			if err != nil {
				return nil, err
			}
			methods.List = append(methods.List, &ast.Field{Type: typeExpr})
		}
		for _, methodNode := range value.Methods {
			typeExpr, err := goTypeExpr(methodNode.Signature)
			if err != nil {
				return nil, err
			}
			methods.List = append(methods.List, &ast.Field{Names: []*ast.Ident{ast.NewIdent(methodNode.Name)}, Type: typeExpr})
		}
		return &ast.InterfaceType{Methods: methods}, nil
	default:
		return nil, fmt.Errorf("unsupported generated Go type %T", typeNode)
	}
}

// typeNodeFromGoExpr adapts ordinary Go declarations that were already parsed
// by go/parser into the Go++ type tree. This keeps signature discovery from
// formatting a Go AST node and reparsing the resulting text.
func typeNodeFromGoExpr(expression ast.Expr) (TypeNode, bool) {
	switch value := expression.(type) {
	case *ast.Ident:
		return &NamedType{Parts: []string{value.Name}}, true
	case *ast.SelectorExpr:
		left, ok := typeNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		name, ok := left.(*NamedType)
		if !ok {
			return nil, false
		}
		return &NamedType{Parts: append(append([]string(nil), name.Parts...), value.Sel.Name)}, true
	case *ast.ParenExpr:
		return typeNodeFromGoExpr(value.X)
	case *ast.StarExpr:
		element, ok := typeNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		return &PointerType{Element: element}, true
	case *ast.ArrayType:
		element, ok := typeNodeFromGoExpr(value.Elt)
		if !ok {
			return nil, false
		}
		if value.Len == nil {
			return &SliceType{Element: element}, true
		}
		if _, ok := value.Len.(*ast.Ellipsis); ok {
			return &ArrayType{Element: element, Ellipsis: true}, true
		}
		length, ok := exprNodeFromGoExpr(value.Len)
		if !ok {
			return nil, false
		}
		return &ArrayType{Length: length, Element: element}, true
	case *ast.MapType:
		key, keyOK := typeNodeFromGoExpr(value.Key)
		element, elementOK := typeNodeFromGoExpr(value.Value)
		if !keyOK || !elementOK {
			return nil, false
		}
		return &MapType{Key: key, Value: element}, true
	case *ast.ChanType:
		element, ok := typeNodeFromGoExpr(value.Value)
		if !ok {
			return nil, false
		}
		direction := "both"
		if value.Dir == ast.RECV {
			direction = "<-"
		} else if value.Dir == ast.SEND {
			direction = "->"
		}
		return &ChannelType{Direction: direction, Element: element}, true
	case *ast.Ellipsis:
		element, ok := typeNodeFromGoExpr(value.Elt)
		if !ok {
			return nil, false
		}
		return &VariadicType{Element: element}, true
	case *ast.IndexExpr:
		base, baseOK := typeNodeFromGoExpr(value.X)
		argument, argumentOK := typeNodeFromGoExpr(value.Index)
		if !baseOK || !argumentOK {
			return nil, false
		}
		name, ok := base.(*NamedType)
		if !ok {
			return nil, false
		}
		name.Arguments = []TypeNode{argument}
		return name, true
	case *ast.IndexListExpr:
		base, ok := typeNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		name, ok := base.(*NamedType)
		if !ok {
			return nil, false
		}
		arguments := make([]TypeNode, len(value.Indices))
		for index, expression := range value.Indices {
			argument, argumentOK := typeNodeFromGoExpr(expression)
			if !argumentOK {
				return nil, false
			}
			arguments[index] = argument
		}
		name.Arguments = arguments
		return name, true
	case *ast.FuncType:
		parameters, ok := parameterNodesFromGoFields(value.Params)
		if !ok {
			return nil, false
		}
		results := []TypeNode{}
		if value.Results != nil {
			for _, field := range value.Results.List {
				result, resultOK := typeNodeFromGoExpr(field.Type)
				if !resultOK {
					return nil, false
				}
				for range field.Names {
					results = append(results, result)
				}
				if len(field.Names) == 0 {
					results = append(results, result)
				}
			}
		}
		return &FunctionType{Parameters: parameters, Results: results}, true
	case *ast.StructType:
		if value.Fields == nil {
			return &StructType{}, true
		}
		fields := make([]StructFieldNode, 0, len(value.Fields.List))
		for _, field := range value.Fields.List {
			fieldType, ok := typeNodeFromGoExpr(field.Type)
			if !ok {
				return nil, false
			}
			names := make([]string, 0, len(field.Names))
			for _, name := range field.Names {
				names = append(names, name.Name)
			}
			tag := ""
			if field.Tag != nil {
				tag = field.Tag.Value
			}
			fields = append(fields, StructFieldNode{Names: names, Type: fieldType, Tag: tag})
		}
		return &StructType{Fields: fields}, true
	case *ast.InterfaceType:
		if value.Methods == nil {
			return &InterfaceType{}, true
		}
		interfaceNode := &InterfaceType{}
		for _, field := range value.Methods.List {
			fieldType, ok := typeNodeFromGoExpr(field.Type)
			if !ok {
				return nil, false
			}
			if len(field.Names) == 0 {
				interfaceNode.Embeds = append(interfaceNode.Embeds, fieldType)
				continue
			}
			for _, name := range field.Names {
				interfaceNode.Methods = append(interfaceNode.Methods, InterfaceMethodNode{Name: name.Name, Signature: fieldType})
			}
		}
		return interfaceNode, true
	default:
		return nil, false
	}
}

func parameterNodesFromGoFields(fields *ast.FieldList) ([]ParameterNode, bool) {
	if fields == nil {
		return nil, true
	}
	parameters := []ParameterNode{}
	for _, field := range fields.List {
		typeNode, ok := typeNodeFromGoExpr(field.Type)
		if !ok {
			return nil, false
		}
		if len(field.Names) == 0 {
			parameters = append(parameters, ParameterNode{Type: typeNode})
			continue
		}
		for _, name := range field.Names {
			parameters = append(parameters, ParameterNode{Name: name.Name, Type: typeNode})
		}
	}
	return parameters, true
}

func exprNodeFromGoExpr(expression ast.Expr) (ExprNode, bool) {
	switch value := expression.(type) {
	case *ast.Ident:
		return &NameExpr{Name: value.Name}, true
	case *ast.BasicLit:
		kind := TokenNumber
		switch value.Kind {
		case gotoken.STRING:
			kind = TokenString
		case gotoken.CHAR:
			kind = TokenRune
		}
		return &LiteralExpr{Text: value.Value, Kind: kind}, true
	case *ast.UnaryExpr:
		operand, ok := exprNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		return &UnaryExpr{Operator: value.Op.String(), Operand: operand}, true
	case *ast.BinaryExpr:
		left, leftOK := exprNodeFromGoExpr(value.X)
		right, rightOK := exprNodeFromGoExpr(value.Y)
		if !leftOK || !rightOK {
			return nil, false
		}
		return &BinaryExpr{Left: left, Operator: value.Op.String(), Right: right}, true
	case *ast.ParenExpr:
		inner, ok := exprNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		return &ParenthesizedExpr{Inner: inner}, true
	case *ast.SelectorExpr:
		receiver, ok := exprNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		return &SelectorExpr{Receiver: receiver, Name: value.Sel.Name}, true
	case *ast.IndexExpr:
		receiver, receiverOK := exprNodeFromGoExpr(value.X)
		index, indexOK := exprNodeFromGoExpr(value.Index)
		if !receiverOK || !indexOK {
			return nil, false
		}
		return &IndexExpr{Receiver: receiver, Index: index}, true
	case *ast.IndexListExpr:
		receiver, ok := exprNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		indices := make([]ExprNode, 0, len(value.Indices))
		for _, index := range value.Indices {
			item, itemOK := exprNodeFromGoExpr(index)
			if !itemOK {
				return nil, false
			}
			indices = append(indices, item)
		}
		return &IndexListExpr{Receiver: receiver, Indices: indices}, true
	case *ast.SliceExpr:
		receiver, ok := exprNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		result := &SliceExpr{Receiver: receiver}
		var err bool
		if value.Low != nil {
			result.Low, err = exprNodeFromGoExpr(value.Low)
			if !err {
				return nil, false
			}
		}
		if value.High != nil {
			result.High, err = exprNodeFromGoExpr(value.High)
			if !err {
				return nil, false
			}
		}
		if value.Max != nil {
			result.Max, err = exprNodeFromGoExpr(value.Max)
			if !err {
				return nil, false
			}
		}
		return result, true
	case *ast.CallExpr:
		callee, ok := exprNodeFromGoExpr(value.Fun)
		if !ok {
			if typeNode, typeOK := typeNodeFromGoExpr(value.Fun); typeOK {
				callee = &TypeExpr{Type: typeNode}
			} else {
				return nil, false
			}
		}
		arguments := make([]CallArg, 0, len(value.Args))
		for index, argument := range value.Args {
			item, itemOK := exprNodeFromGoExpr(argument)
			if !itemOK {
				return nil, false
			}
			if value.Ellipsis.IsValid() && index == len(value.Args)-1 {
				item = &SpreadExpr{Expression: item}
			}
			arguments = append(arguments, CallArg{Value: item})
		}
		return &CallExpr{Callee: callee, Arguments: arguments}, true
	case *ast.TypeAssertExpr:
		inner, ok := exprNodeFromGoExpr(value.X)
		if !ok {
			return nil, false
		}
		if value.Type == nil {
			return &TypeAssertExpr{Expression: inner, TypeSwitch: true}, true
		}
		typeNode, typeOK := typeNodeFromGoExpr(value.Type)
		if !typeOK {
			return nil, false
		}
		return &TypeAssertExpr{Expression: inner, Type: typeNode}, true
	case *ast.CompositeLit:
		var typeNode TypeNode
		if value.Type != nil {
			var ok bool
			typeNode, ok = typeNodeFromGoExpr(value.Type)
			if !ok {
				return nil, false
			}
		}
		elements := make([]CompositeElement, 0, len(value.Elts))
		for _, element := range value.Elts {
			if pair, ok := element.(*ast.KeyValueExpr); ok {
				key, keyOK := exprNodeFromGoExpr(pair.Key)
				item, itemOK := exprNodeFromGoExpr(pair.Value)
				if !keyOK || !itemOK {
					return nil, false
				}
				elements = append(elements, CompositeElement{Key: key, Value: item})
				continue
			}
			item, itemOK := exprNodeFromGoExpr(element)
			if !itemOK {
				return nil, false
			}
			elements = append(elements, CompositeElement{Value: item})
		}
		return &CompositeLiteralExpr{Type: typeNode, Elements: elements}, true
	case *ast.ArrayType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.StructType:
		typeNode, ok := typeNodeFromGoExpr(value)
		if !ok {
			return nil, false
		}
		return &TypeExpr{Type: typeNode}, true
	case *ast.Ellipsis:
		inner, ok := exprNodeFromGoExpr(value.Elt)
		if !ok {
			return nil, false
		}
		return &SpreadExpr{Expression: inner}, true
	default:
		return nil, false
	}
}

// parseGoExpressionTokens is a narrow interoperability fallback. The Go++
// expression parser remains authoritative for Go++ syntax; this is used only
// after it has failed to consume a token sequence and only accepts syntax that
// go/parser can represent as a typed Go++ expression node.
func parseGoExpressionTokens(tokens []Token) (ExprNode, bool) {
	if len(tokens) == 0 {
		return nil, false
	}
	source := expressionTokensSource(tokens)
	parsed, err := parser.ParseExpr(source)
	if err != nil {
		return nil, false
	}
	return exprNodeFromGoExpr(parsed)
}

// parseGoTypeTokens is the type counterpart to parseGoExpressionTokens. Go's
// parser accepts a type only in declaration context, so the source is wrapped
// in a synthetic variable declaration and immediately adapted back to the
// compiler's TypeNode tree. The wrapper is never retained or emitted.
func parseGoTypeTokens(tokens []Token) (TypeNode, bool) {
	if len(tokens) == 0 {
		return nil, false
	}
	source := expressionTokensSource(tokens)
	file, err := parser.ParseFile(
		gotoken.NewFileSet(),
		"gpp-type.go",
		"package gpptype\n\nvar __gpp_type "+source+"\n",
		0,
	)
	if err != nil || file == nil || len(file.Decls) != 1 {
		return nil, false
	}
	declaration, ok := file.Decls[0].(*ast.GenDecl)
	if !ok || len(declaration.Specs) != 1 {
		return nil, false
	}
	spec, ok := declaration.Specs[0].(*ast.ValueSpec)
	if !ok || spec.Type == nil {
		return nil, false
	}
	return typeNodeFromGoExpr(spec.Type)
}

func goExprNode(expression ExprNode) (ast.Expr, error) {
	switch value := expression.(type) {
	case *NameExpr:
		return ast.NewIdent(value.Name), nil
	case *LiteralExpr:
		kind := gotoken.IDENT
		switch value.Kind {
		case TokenNumber:
			kind = gotoken.INT
		case TokenString, TokenRawString:
			kind = gotoken.STRING
		case TokenRune:
			kind = gotoken.CHAR
		}
		return &ast.BasicLit{Kind: kind, Value: value.Text}, nil
	case *UnaryExpr:
		operand, err := goExprNode(value.Operand)
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{Op: goTokenForText(value.Operator), X: operand}, nil
	case *BinaryExpr:
		left, err := goExprNode(value.Left)
		if err != nil {
			return nil, err
		}
		right, err := goExprNode(value.Right)
		if err != nil {
			return nil, err
		}
		return &ast.BinaryExpr{X: left, Op: goTokenForText(value.Operator), Y: right}, nil
	case *AssignmentExpr:
		return nil, fmt.Errorf("assignment expression must be lowered as a simple statement")
	case *SelectorExpr:
		receiver, err := goExprNode(value.Receiver)
		if err != nil {
			return nil, err
		}
		if value.Safe {
			return nil, fmt.Errorf("safe selector %s is not lowered", value.Name)
		}
		return &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent(value.Name)}, nil
	case *IndexExpr:
		receiver, err := goExprNode(value.Receiver)
		if err != nil {
			return nil, err
		}
		index, err := goExprNode(value.Index)
		if err != nil {
			return nil, err
		}
		return &ast.IndexExpr{X: receiver, Index: index}, nil
	case *IndexListExpr:
		receiver, err := goExprNode(value.Receiver)
		if err != nil {
			return nil, err
		}
		indices := make([]ast.Expr, 0, len(value.Indices))
		for _, item := range value.Indices {
			index, err := goExprNode(item)
			if err != nil {
				return nil, err
			}
			indices = append(indices, index)
		}
		return &ast.IndexListExpr{X: receiver, Indices: indices}, nil
	case *SliceExpr:
		receiver, err := goExprNode(value.Receiver)
		if err != nil {
			return nil, err
		}
		result := &ast.SliceExpr{X: receiver}
		if value.Low != nil {
			result.Low, err = goExprNode(value.Low)
			if err != nil {
				return nil, err
			}
		}
		if value.High != nil {
			result.High, err = goExprNode(value.High)
			if err != nil {
				return nil, err
			}
		}
		if value.Max != nil {
			result.Max, err = goExprNode(value.Max)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	case *CallExpr:
		callee, err := goExprNode(value.Callee)
		if err != nil {
			return nil, err
		}
		arguments := make([]ast.Expr, 0, len(value.Arguments))
		call := &ast.CallExpr{Fun: callee, Args: arguments}
		for _, argument := range value.Arguments {
			if argument.Name != "" {
				return nil, fmt.Errorf("named call argument %q is not lowered", argument.Name)
			}
			spread, isSpread := argument.Value.(*SpreadExpr)
			expression := argument.Value
			if isSpread {
				expression = spread.Expression
			}
			item, err := goExprNode(expression)
			if err != nil {
				return nil, err
			}
			arguments = append(arguments, item)
			if isSpread {
				if index := len(arguments) - 1; index != len(value.Arguments)-1 {
					return nil, fmt.Errorf("spread argument must be last")
				}
				call.Ellipsis = gotoken.Pos(1)
			}
		}
		call.Args = arguments
		return call, nil
	case *ParenthesizedExpr:
		inner, err := goExprNode(value.Inner)
		if err != nil {
			return nil, err
		}
		return &ast.ParenExpr{X: inner}, nil
	case *TypeAssertExpr:
		inner, err := goExprNode(value.Expression)
		if err != nil {
			return nil, err
		}
		if value.TypeSwitch {
			return &ast.TypeAssertExpr{X: inner}, nil
		}
		typeExpr, err := goTypeExpr(value.Type)
		if err != nil {
			return nil, err
		}
		return &ast.TypeAssertExpr{X: inner, Type: typeExpr}, nil
	case *TypeExpr:
		return goTypeExpr(value.Type)
	case *CompositeLiteralExpr:
		var typeExpr ast.Expr
		if value.Type != nil {
			var err error
			typeExpr, err = goTypeExpr(value.Type)
			if err != nil {
				return nil, err
			}
		}
		elements := make([]ast.Expr, 0, len(value.Elements))
		for _, element := range value.Elements {
			item, itemErr := goExprNode(element.Value)
			if itemErr != nil {
				return nil, itemErr
			}
			if element.Key != nil {
				key, keyErr := goExprNode(element.Key)
				if keyErr != nil {
					return nil, keyErr
				}
				elements = append(elements, &ast.KeyValueExpr{Key: key, Value: item})
			} else {
				elements = append(elements, item)
			}
		}
		return &ast.CompositeLit{Type: typeExpr, Elts: elements}, nil
	case *FunctionLiteralExpr:
		typeExpr, err := goTypeExpr(value.Type)
		if err != nil {
			return nil, err
		}
		functionType, ok := typeExpr.(*ast.FuncType)
		if !ok {
			return nil, fmt.Errorf("function literal has non-function type %T", typeExpr)
		}
		body, err := goBlockNode(value.Body, "")
		if err != nil {
			return nil, err
		}
		return &ast.FuncLit{Type: functionType, Body: body}, nil
	default:
		return nil, fmt.Errorf("unsupported Go++ expression %T", expression)
	}
}

// goBlockNode is intentionally strict. Unsupported Go++ constructs are
// rejected instead of being rendered and reparsed, allowing callers to keep a
// compatibility fallback while the dedicated statement lowerers are added.
func goBlockNode(block *BlockStmt, rethrowName string) (*ast.BlockStmt, error) {
	if block == nil {
		return &ast.BlockStmt{}, nil
	}
	result := &ast.BlockStmt{}
	for _, statement := range block.Statements {
		lowered, err := goStmtNode(statement, rethrowName)
		if err != nil {
			return nil, err
		}
		result.List = append(result.List, lowered...)
	}
	return result, nil
}

// goExceptionBlockNode is the exception-aware counterpart to goBlockNode.
// Returns are represented as the runtime's control-transfer panic so a
// generated finally/defer wrapper cannot swallow the original function return.
func goExceptionBlockNode(block *BlockStmt, rethrowName string) (*ast.BlockStmt, error) {
	if block == nil {
		return &ast.BlockStmt{}, nil
	}
	result := &ast.BlockStmt{}
	for _, statement := range block.Statements {
		lowered, err := goExceptionStmtNode(statement, rethrowName)
		if err != nil {
			return nil, err
		}
		result.List = append(result.List, lowered...)
	}
	return result, nil
}

func goExceptionStmtNode(statement Stmt, rethrowName string) ([]ast.Stmt, error) {
	if value, ok := statement.(*ReturnStmt); ok {
		values := make([]ast.Expr, 0, len(value.Values))
		for _, expression := range value.Values {
			lowered, err := goExprNode(expression)
			if err != nil {
				return nil, err
			}
			values = append(values, lowered)
		}
		panicValue := &ast.CompositeLit{Type: ast.NewIdent("__gppExceptionReturn")}
		if len(values) > 0 {
			panicValue.Elts = []ast.Expr{&ast.KeyValueExpr{Key: ast.NewIdent("values"), Value: &ast.CompositeLit{Type: &ast.ArrayType{Elt: ast.NewIdent("any")}, Elts: values}}}
		}
		return []ast.Stmt{&ast.ExprStmt{X: &ast.CallExpr{Fun: ast.NewIdent("panic"), Args: []ast.Expr{panicValue}}}}, nil
	}
	switch value := statement.(type) {
	case *IfStmt:
		condition, err := goExprNode(value.Condition)
		if err != nil {
			return nil, err
		}
		body, err := goExceptionBlockNode(value.Body, rethrowName)
		if err != nil {
			return nil, err
		}
		result := &ast.IfStmt{Cond: condition, Body: body}
		if value.Init != nil {
			result.Init, err = goSimpleStmt(value.Init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Else != nil {
			result.Else, err = goExceptionBlockNode(value.Else, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.ElseIf != nil {
			nested, nestedErr := goExceptionStmtNode(value.ElseIf, rethrowName)
			if nestedErr != nil || len(nested) != 1 {
				return nil, fmt.Errorf("unsupported else-if")
			}
			result.Else = nested[0]
		}
		return []ast.Stmt{result}, nil
	case *ForStmt:
		body, err := goExceptionBlockNode(value.Body, rethrowName)
		if err != nil {
			return nil, err
		}
		if value.RangeExpr != nil {
			key := make([]ast.Expr, 0, len(value.RangeKey))
			for _, item := range value.RangeKey {
				lowered, itemErr := goExprNode(item)
				if itemErr != nil {
					return nil, itemErr
				}
				key = append(key, lowered)
			}
			rangeExpr, rangeErr := goExprNode(value.RangeExpr)
			if rangeErr != nil {
				return nil, rangeErr
			}
			var keyExpr, valueExpr ast.Expr
			if len(key) > 0 {
				keyExpr = key[0]
			}
			if len(key) > 1 {
				valueExpr = key[1]
			}
			rangeToken := gotoken.DEFINE
			if value.RangeOperator == "=" {
				rangeToken = gotoken.ASSIGN
			}
			return []ast.Stmt{&ast.RangeStmt{Key: keyExpr, Value: valueExpr, Tok: rangeToken, X: rangeExpr, Body: body}}, nil
		}
		result := &ast.ForStmt{Body: body}
		if value.Condition != nil {
			result.Cond, err = goExprNode(value.Condition)
			if err != nil {
				return nil, err
			}
		}
		if value.Init != nil {
			result.Init, err = goSimpleStmt(value.Init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Post != nil {
			result.Post, err = goSimpleStmt(value.Post, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		return []ast.Stmt{result}, nil
	case *SwitchStmt:
		if value.Select {
			selectStmt, selectErr := goSelectStmtFromBlock(value.Body, func(block *BlockStmt) (*ast.BlockStmt, error) {
				return goExceptionBlockNode(block, rethrowName)
			})
			if selectErr != nil {
				return nil, selectErr
			}
			return []ast.Stmt{selectStmt}, nil
		}
		if isTypeSwitchAssertion(value.Tag) || isTypeSwitchAssignment(value.Init) {
			body, err := goExceptionBlockNode(value.Body, rethrowName)
			if err != nil {
				return nil, err
			}
			assignment, err := goTypeSwitchAssign(value, rethrowName)
			if err != nil {
				return nil, err
			}
			return []ast.Stmt{&ast.TypeSwitchStmt{Assign: assignment, Body: body}}, nil
		}
		body := &ast.BlockStmt{}
		if value.Body != nil {
			for _, child := range value.Body.Statements {
				caseStatement, ok := child.(*CaseStmt)
				if !ok {
					return nil, fmt.Errorf("unsupported switch statement %T", child)
				}
				caseBody, caseErr := goExceptionBlockNode(caseStatement.Clause.Body, rethrowName)
				if caseErr != nil {
					return nil, caseErr
				}
				var expressions []ast.Expr
				if !caseStatement.Clause.Default {
					expressions = make([]ast.Expr, 0, len(caseStatement.Clause.Expressions))
					for _, expression := range caseStatement.Clause.Expressions {
						lowered, exprErr := goExprNode(expression)
						if exprErr != nil {
							return nil, exprErr
						}
						expressions = append(expressions, lowered)
					}
				}
				body.List = append(body.List, &ast.CaseClause{List: expressions, Body: caseBody.List})
			}
		}
		result := &ast.SwitchStmt{Body: body}
		var err error
		if value.Init != nil {
			result.Init, err = goSimpleStmt(value.Init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Tag != nil {
			result.Tag, err = goExprNode(value.Tag)
			if err != nil {
				return nil, err
			}
		}
		return []ast.Stmt{result}, nil
	case *BlockStmt:
		body, err := goExceptionBlockNode(value, rethrowName)
		if err != nil {
			return nil, err
		}
		return []ast.Stmt{body}, nil
	default:
		return goStmtNode(statement, rethrowName)
	}
}

type constructorASTLiteral struct {
	Values   map[string]ExprNode
	Children map[string]*constructorASTLiteral
}

func constructorExprNode(call *CallExpr, context constructorContext) (ExprNode, bool, error) {
	name, target, ok := constructorTargetForCallee(call.Callee, context)
	if !ok {
		return nil, false, nil
	}
	class := target.Class
	classes := classesForClass(context, class)
	fields, err := constructorFields(class, classes, nil, map[string]bool{})
	if err != nil {
		return nil, true, err
	}
	root := &constructorASTLiteral{Values: map[string]ExprNode{}, Children: map[string]*constructorASTLiteral{}}
	named := false
	positional := false
	for _, argument := range call.Arguments {
		named = named || argument.Name != ""
		positional = positional || argument.Name == ""
	}
	if named && positional {
		return nil, true, sourceLineError(call.Span(), fmt.Errorf("cannot mix named and positional arguments in call to %s", name))
	}
	if !named && len(call.Arguments) != len(fields) && len(call.Arguments) != 0 {
		return nil, true, fmt.Errorf("%s constructor expects %d arguments, got %d", name, len(fields), len(call.Arguments))
	}
	for index, argument := range call.Arguments {
		field := constructorField{}
		if named {
			field, err = resolveConstructorField(fields, argument.Name)
			if err != nil {
				return nil, true, fmt.Errorf("%s constructor: %w", name, err)
			}
		} else {
			field = fields[index]
		}
		value, valueErr := lowerExceptionExprNode(argument.Value, context)
		if valueErr != nil {
			return nil, true, valueErr
		}
		if base, dispatch := dispatchTargetForTypeName(field.Type, context); dispatch {
			if concreteName, concreteOK := expressionTypeName(value); concreteOK {
				if concrete, exists := context.Targets[concreteName]; exists && sameConstructorPackage(base, concrete) && (concrete.Class == base.Class || classInherits(concrete.Class, base.Class, concrete.Classes, map[string]bool{})) {
					if _, pointer := value.(*UnaryExpr); !pointer {
						value = &UnaryExpr{Operator: "&", Operand: value}
					}
				}
			}
		}
		setConstructorASTValue(root, field.Path, field.Name, value)
	}
	value := constructorASTLiteralExpr(name, class, root, classes, target.Qualifier)
	return &UnaryExpr{Operator: "&", Operand: value}, true, nil
}

func setConstructorASTValue(root *constructorASTLiteral, path []string, fieldName string, value ExprNode) {
	node := root
	for _, segment := range path {
		child := node.Children[segment]
		if child == nil {
			child = &constructorASTLiteral{Values: map[string]ExprNode{}, Children: map[string]*constructorASTLiteral{}}
			node.Children[segment] = child
		}
		node = child
	}
	node.Values[fieldName] = value
}

func constructorASTLiteralExpr(name string, class *ClassDecl, literal *constructorASTLiteral, classes map[string]*ClassDecl, qualifier string) ExprNode {
	elements := []CompositeElement{}
	for _, parentName := range classParentNames(class) {
		child := literal.Children[parentName]
		parentFieldName := classParentFieldName(parentName)
		if child == nil {
			child = literal.Children[parentFieldName]
		}
		if child == nil {
			continue
		}
		parent := classes[parentName]
		parentNameText := qualifyTypeName(parentName, qualifier)
		value := constructorASTLiteralExpr(parentNameText, parent, child, classes, qualifier)
		elements = append(elements, CompositeElement{Key: &NameExpr{Name: parentFieldName}, Value: value})
	}
	for _, field := range class.Fields {
		if value, ok := literal.Values[field.Name]; ok {
			elements = append(elements, CompositeElement{Key: &NameExpr{Name: field.Name}, Value: value})
		}
	}
	return &CompositeLiteralExpr{Type: parseTypeText(name), Elements: elements}
}

func goStmtNode(statement Stmt, rethrowName string) ([]ast.Stmt, error) {
	switch value := statement.(type) {
	case *ExpressionStmt:
		expression, err := goExprNode(value.Expression)
		if err != nil {
			return nil, err
		}
		return []ast.Stmt{&ast.ExprStmt{X: expression}}, nil
	case *ReturnStmt:
		values := make([]ast.Expr, 0, len(value.Values))
		for _, item := range value.Values {
			expression, err := goExprNode(item)
			if err != nil {
				return nil, err
			}
			values = append(values, expression)
		}
		return []ast.Stmt{&ast.ReturnStmt{Results: values}}, nil
	case *DeclarationStmt:
		var values []ast.Expr
		if len(value.Values) > 0 {
			values = make([]ast.Expr, 0, len(value.Values))
		}
		for _, item := range value.Values {
			if item == nil {
				continue
			}
			expression, err := goExprNode(item)
			if err != nil {
				return nil, err
			}
			values = append(values, expression)
		}
		names := make([]*ast.Ident, 0, len(value.Names))
		for _, item := range value.Names {
			names = append(names, ast.NewIdent(item.Text))
		}
		if value.Keyword == ":=" || value.Keyword == "let" {
			left := make([]ast.Expr, len(names))
			for index, name := range names {
				left[index] = name
			}
			return []ast.Stmt{&ast.AssignStmt{Lhs: left, Tok: gotoken.DEFINE, Rhs: values}}, nil
		}
		spec := &ast.ValueSpec{Names: names, Values: values}
		if value.Type != nil {
			typeExpr, err := goTypeExpr(value.Type)
			if err != nil {
				return nil, err
			}
			spec.Type = typeExpr
		}
		kind := gotoken.VAR
		if value.Keyword == "const" {
			kind = gotoken.CONST
		}
		return []ast.Stmt{&ast.DeclStmt{Decl: &ast.GenDecl{Tok: kind, Specs: []ast.Spec{spec}}}}, nil
	case *AssignmentStmt:
		left := make([]ast.Expr, 0, len(value.Left))
		for _, item := range value.Left {
			expression, err := goExprNode(item)
			if err != nil {
				return nil, err
			}
			left = append(left, expression)
		}
		right := make([]ast.Expr, 0, len(value.Right))
		for _, item := range value.Right {
			expression, err := goExprNode(item)
			if err != nil {
				return nil, err
			}
			right = append(right, expression)
		}
		return []ast.Stmt{&ast.AssignStmt{Lhs: left, Tok: goTokenForText(value.Operator), Rhs: right}}, nil
	case *ThrowStmt:
		if value.Value == nil {
			if rethrowName == "" {
				return nil, fmt.Errorf("bare throw is only valid inside catch")
			}
			return []ast.Stmt{&ast.ExprStmt{X: &ast.CallExpr{Fun: ast.NewIdent("panic"), Args: []ast.Expr{ast.NewIdent(rethrowName)}}}}, nil
		}
		expression, err := goExprNode(value.Value)
		if err != nil {
			return nil, err
		}
		return []ast.Stmt{&ast.ExprStmt{X: &ast.CallExpr{Fun: ast.NewIdent("__gppThrow"), Args: []ast.Expr{expression}}}}, nil
	case *DeferStmt:
		expression, err := goExprNode(value.Expression)
		if err != nil {
			return nil, err
		}
		call, ok := expression.(*ast.CallExpr)
		if !ok {
			return nil, fmt.Errorf("defer requires a call")
		}
		return []ast.Stmt{&ast.DeferStmt{Call: call}}, nil
	case *GoStmt:
		expression, err := goExprNode(value.Expression)
		if err != nil {
			return nil, err
		}
		call, ok := expression.(*ast.CallExpr)
		if !ok {
			return nil, fmt.Errorf("go requires a call")
		}
		return []ast.Stmt{&ast.GoStmt{Call: call}}, nil
	case *SendStmt:
		channel, err := goExprNode(value.Channel)
		if err != nil {
			return nil, err
		}
		item, err := goExprNode(value.Value)
		if err != nil {
			return nil, err
		}
		return []ast.Stmt{&ast.SendStmt{Chan: channel, Value: item}}, nil
	case *IncDecStmt:
		expression, err := goExprNode(value.Expression)
		if err != nil {
			return nil, err
		}
		return []ast.Stmt{&ast.IncDecStmt{X: expression, Tok: goTokenForText(value.Operator)}}, nil
	case *IfStmt:
		condition, err := goExprNode(value.Condition)
		if err != nil {
			return nil, err
		}
		body, err := goBlockNode(value.Body, rethrowName)
		if err != nil {
			return nil, err
		}
		result := &ast.IfStmt{Cond: condition, Body: body}
		if value.Init != nil {
			result.Init, err = goSimpleStmt(value.Init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Else != nil {
			result.Else, err = goBlockNode(value.Else, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.ElseIf != nil {
			nested, nestedErr := goStmtNode(value.ElseIf, rethrowName)
			if nestedErr != nil || len(nested) != 1 {
				return nil, fmt.Errorf("unsupported else-if")
			}
			result.Else = nested[0]
		}
		return []ast.Stmt{result}, nil
	case *ForStmt:
		body, err := goBlockNode(value.Body, rethrowName)
		if err != nil {
			return nil, err
		}
		if value.RangeExpr != nil {
			key := make([]ast.Expr, 0, len(value.RangeKey))
			for _, item := range value.RangeKey {
				lowered, itemErr := goExprNode(item)
				if itemErr != nil {
					return nil, itemErr
				}
				key = append(key, lowered)
			}
			rangeExpr, rangeErr := goExprNode(value.RangeExpr)
			if rangeErr != nil {
				return nil, rangeErr
			}
			var keyExpr ast.Expr
			if len(key) > 0 {
				keyExpr = key[0]
			}
			var valueExpr ast.Expr
			if len(key) > 1 {
				valueExpr = key[1]
			}
			rangeToken := gotoken.DEFINE
			if value.RangeOperator == "=" {
				rangeToken = gotoken.ASSIGN
			}
			return []ast.Stmt{&ast.RangeStmt{Key: keyExpr, Value: valueExpr, Tok: rangeToken, X: rangeExpr, Body: body}}, nil
		}
		result := &ast.ForStmt{Body: body}
		if value.Condition != nil {
			result.Cond, err = goExprNode(value.Condition)
			if err != nil {
				return nil, err
			}
		}
		if value.Init != nil {
			result.Init, err = goSimpleStmt(value.Init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Post != nil {
			result.Post, err = goSimpleStmt(value.Post, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		return []ast.Stmt{result}, nil
	case *SwitchStmt:
		if value.Select {
			selectStmt, selectErr := goSelectStmtFromBlock(value.Body, func(block *BlockStmt) (*ast.BlockStmt, error) {
				return goBlockNode(block, rethrowName)
			})
			if selectErr != nil {
				return nil, selectErr
			}
			return []ast.Stmt{selectStmt}, nil
		}
		if isTypeSwitchAssertion(value.Tag) || isTypeSwitchAssignment(value.Init) {
			body, err := goBlockNode(value.Body, rethrowName)
			if err != nil {
				return nil, err
			}
			assignment, err := goTypeSwitchAssign(value, rethrowName)
			if err != nil {
				return nil, err
			}
			return []ast.Stmt{&ast.TypeSwitchStmt{Assign: assignment, Body: body}}, nil
		}
		body, err := goBlockNode(value.Body, rethrowName)
		if err != nil {
			return nil, err
		}
		result := &ast.SwitchStmt{Body: body}
		if value.Init != nil {
			result.Init, err = goSimpleStmt(value.Init, rethrowName)
			if err != nil {
				return nil, err
			}
		}
		if value.Tag != nil {
			result.Tag, err = goExprNode(value.Tag)
			if err != nil {
				return nil, err
			}
		}
		return []ast.Stmt{result}, nil
	case *CaseStmt:
		body, err := goBlockNode(value.Clause.Body, rethrowName)
		if err != nil {
			return nil, err
		}
		var values []ast.Expr
		if !value.Clause.Default {
			values = make([]ast.Expr, 0, len(value.Clause.Expressions))
			for _, item := range value.Clause.Expressions {
				expression, exprErr := goExprNode(item)
				if exprErr != nil {
					return nil, exprErr
				}
				values = append(values, expression)
			}
		}
		return []ast.Stmt{&ast.CaseClause{List: values, Body: body.List}}, nil
	case *TypeDeclarationStmt:
		typeExpr, err := goTypeExpr(value.Type)
		if err != nil {
			return nil, err
		}
		tok := gotoken.TYPE
		typeSpec := &ast.TypeSpec{Name: ast.NewIdent(value.Name), Type: typeExpr}
		if value.Alias {
			typeSpec.Assign = gotoken.Pos(1)
		}
		return []ast.Stmt{&ast.DeclStmt{Decl: &ast.GenDecl{Tok: tok, Specs: []ast.Spec{typeSpec}}}}, nil
	case *BlockStmt:
		body, err := goBlockNode(value, rethrowName)
		if err != nil {
			return nil, err
		}
		return []ast.Stmt{body}, nil
	case *BranchStmt:
		if !strings.Contains(" break continue fallthrough goto ", " "+value.Keyword+" ") {
			return nil, fmt.Errorf("unsupported branch %s", value.Keyword)
		}
		branch := &ast.BranchStmt{Tok: gotoken.Lookup(value.Keyword)}
		if len(value.Target) > 0 {
			branch.Label = ast.NewIdent(value.Target[0].Text)
		}
		return []ast.Stmt{branch}, nil
	case *LabelStmt:
		if value.Name == "" {
			return nil, fmt.Errorf("label requires a name")
		}
		return []ast.Stmt{&ast.LabeledStmt{Label: ast.NewIdent(value.Name), Stmt: &ast.EmptyStmt{}}}, nil
	default:
		return nil, fmt.Errorf("unsupported Go++ statement %T", statement)
	}
}

func goSelectStmtFromBlock(block *BlockStmt, lowerBody func(*BlockStmt) (*ast.BlockStmt, error)) (*ast.SelectStmt, error) {
	result := &ast.SelectStmt{Body: &ast.BlockStmt{}}
	if block == nil {
		return result, nil
	}
	for _, statement := range block.Statements {
		caseStatement, ok := statement.(*CaseStmt)
		if !ok {
			return nil, fmt.Errorf("select body contains %T instead of a case clause", statement)
		}
		caseBody, err := lowerBody(caseStatement.Clause.Body)
		if err != nil {
			return nil, err
		}
		communication, err := goSelectCommunicationExpr(caseStatement.Clause.Expressions)
		if err != nil {
			return nil, err
		}
		result.Body.List = append(result.Body.List, &ast.CommClause{Comm: communication, Body: caseBody.List})
	}
	return result, nil
}

func goSelectCommunicationExpr(expressions []ExprNode) (ast.Stmt, error) {
	if len(expressions) == 0 {
		return nil, nil
	}
	if len(expressions) != 1 {
		return nil, fmt.Errorf("select case must contain one communication")
	}
	expression := expressions[0]
	if send, ok := expression.(*SendExpr); ok {
		channel, err := goExprNode(send.Channel)
		if err != nil {
			return nil, err
		}
		value, err := goExprNode(send.Value)
		if err != nil {
			return nil, err
		}
		return &ast.SendStmt{Chan: channel, Value: value}, nil
	}
	if binary, ok := expression.(*BinaryExpr); ok && (binary.Operator == "=" || binary.Operator == ":=") {
		left, err := goExprNode(binary.Left)
		if err != nil {
			return nil, err
		}
		right, err := goExprNode(binary.Right)
		if err != nil {
			return nil, err
		}
		tok := gotoken.ASSIGN
		if binary.Operator == ":=" {
			tok = gotoken.DEFINE
		}
		return &ast.AssignStmt{Lhs: []ast.Expr{left}, Tok: tok, Rhs: []ast.Expr{right}}, nil
	}
	value, err := goExprNode(expression)
	if err != nil {
		return nil, err
	}
	return &ast.ExprStmt{X: value}, nil
}

func goSimpleStmt(expression ExprNode, rethrowName string) (ast.Stmt, error) {
	if postfix, ok := expression.(*PostfixExpr); ok {
		item, err := goExprNode(postfix.Expression)
		if err != nil {
			return nil, err
		}
		return &ast.IncDecStmt{X: item, Tok: goTokenForText(postfix.Operator)}, nil
	}
	if assignment, ok := expression.(*AssignmentExpr); ok {
		left := make([]ast.Expr, 0, len(assignment.Left))
		right := make([]ast.Expr, 0, len(assignment.Right))
		for _, item := range assignment.Left {
			lowered, err := goExprNode(item)
			if err != nil {
				return nil, err
			}
			left = append(left, lowered)
		}
		for _, item := range assignment.Right {
			lowered, err := goExprNode(item)
			if err != nil {
				return nil, err
			}
			right = append(right, lowered)
		}
		return &ast.AssignStmt{Lhs: left, Tok: goTokenForText(assignment.Operator), Rhs: right}, nil
	}
	if assignment, ok := expression.(*BinaryExpr); ok && (assignment.Operator == "=" || assignment.Operator == ":=") {
		left, err := goExprNode(assignment.Left)
		if err != nil {
			return nil, err
		}
		right, err := goExprNode(assignment.Right)
		if err != nil {
			return nil, err
		}
		tok := gotoken.ASSIGN
		if assignment.Operator == ":=" {
			tok = gotoken.DEFINE
		}
		return &ast.AssignStmt{Lhs: []ast.Expr{left}, Tok: tok, Rhs: []ast.Expr{right}}, nil
	}
	lowered, err := goExprNode(expression)
	if err != nil {
		return nil, err
	}
	return &ast.ExprStmt{X: lowered}, nil
}

func isTypeSwitchAssignment(expression ExprNode) bool {
	assignment, ok := expression.(*AssignmentExpr)
	return ok && len(assignment.Right) == 1 && isTypeSwitchAssertion(assignment.Right[0])
}

func goTypeSwitchAssign(statement *SwitchStmt, rethrowName string) (ast.Stmt, error) {
	if statement == nil {
		return nil, fmt.Errorf("type switch is nil")
	}
	if statement.Init != nil {
		return goSimpleStmt(statement.Init, rethrowName)
	}
	if statement.Tag != nil {
		expression, err := goExprNode(statement.Tag)
		if err != nil {
			return nil, err
		}
		return &ast.ExprStmt{X: expression}, nil
	}
	return nil, fmt.Errorf("type switch requires an assignment or assertion")
}

func goTokenForText(text string) gotoken.Token {
	switch text {
	case "=":
		return gotoken.ASSIGN
	case ":=":
		return gotoken.DEFINE
	case "+=":
		return gotoken.ADD_ASSIGN
	case "-=":
		return gotoken.SUB_ASSIGN
	case "*=":
		return gotoken.MUL_ASSIGN
	case "/=":
		return gotoken.QUO_ASSIGN
	case "%=":
		return gotoken.REM_ASSIGN
	case "&=":
		return gotoken.AND_ASSIGN
	case "|=":
		return gotoken.OR_ASSIGN
	case "^=":
		return gotoken.XOR_ASSIGN
	case "<<=":
		return gotoken.SHL_ASSIGN
	case ">>=":
		return gotoken.SHR_ASSIGN
	case "&^=":
		return gotoken.AND_NOT_ASSIGN
	case "||":
		return gotoken.LOR
	case "&&":
		return gotoken.LAND
	case "==":
		return gotoken.EQL
	case "!=":
		return gotoken.NEQ
	case "<":
		return gotoken.LSS
	case "<=":
		return gotoken.LEQ
	case ">":
		return gotoken.GTR
	case ">=":
		return gotoken.GEQ
	case "+":
		return gotoken.ADD
	case "-":
		return gotoken.SUB
	case "*":
		return gotoken.MUL
	case "/":
		return gotoken.QUO
	case "%":
		return gotoken.REM
	case "|":
		return gotoken.OR
	case "^":
		return gotoken.XOR
	case "&":
		return gotoken.AND
	case "<<":
		return gotoken.SHL
	case ">>":
		return gotoken.SHR
	case "<-":
		return gotoken.ARROW
	case "&^":
		return gotoken.AND_NOT
	case "!":
		return gotoken.NOT
	case "++":
		return gotoken.INC
	case "--":
		return gotoken.DEC
	default:
		return gotoken.Lookup(text)
	}
}
