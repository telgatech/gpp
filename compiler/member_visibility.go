package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

func validateFileMemberVisibility(model *SemanticModel, pkg *PackageSymbols, file *File) error {
	if file == nil || pkg == nil {
		return nil
	}
	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *FunctionDecl:
			if err := validateMemberVisibilityInMethod(model, pkg, nil, value.Method, ""); err != nil {
				return err
			}
		case *MixedDecl:
			if len(value.GoASTDecls) > 0 {
				raw := &GoDecl{
					Declarations: value.GoASTDecls,
					FileSet:      value.GoASTFileSet,
					Owner:        value.Owner,
					SourceSpan:   value.SourceSpan,
					SourceFile:   value.SourceFile,
					SourceLine:   value.SourceLine,
				}
				if err := validateGoDeclMemberVisibility(model, pkg, raw); err != nil {
					return err
				}
			}
			for _, function := range value.Functions {
				if function == nil {
					continue
				}
				if err := validateMemberVisibilityInMethod(model, pkg, nil, function.Method, ""); err != nil {
					return err
				}
			}
		case *ClassDecl:
			for _, method := range value.Methods {
				if err := validateMemberVisibilityInMethod(model, pkg, value, method, ""); err != nil {
					return err
				}
			}
		case *ValueDecl:
			method := Method{
				Owner:   value.Owner,
				BodyAST: &BlockStmt{Statements: []Stmt{&DeclarationStmt{Values: value.Values}}},
			}
			if err := validateMemberVisibilityInMethod(model, pkg, nil, method, ""); err != nil {
				return err
			}
		case *ExtendDecl:
			for _, method := range value.Methods {
				if len(value.TargetAST) == 0 {
					continue
				}
				target, err := typeNodeSource(value.TargetAST[0])
				if err != nil {
					continue
				}
				if err := validateMemberVisibilityInMethod(model, pkg, nil, method, target); err != nil {
					return err
				}
			}
		case *GoDecl:
			if err := validateGoDeclMemberVisibility(model, pkg, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateGoDeclMemberVisibility(model *SemanticModel, pkg *PackageSymbols, declaration *GoDecl) error {
	if declaration == nil {
		return nil
	}
	for _, raw := range declaration.Declarations {
		function, ok := raw.(*ast.FuncDecl)
		if !ok {
			var visibilityErr error
			ast.Inspect(raw, func(node ast.Node) bool {
				if visibilityErr != nil {
					return false
				}
				switch value := node.(type) {
				case *ast.SelectorExpr:
					typeName := goMemberVisibilityExprType(model, pkg, value.X, map[string]string{})
					class := semanticClassForTypeName(pkg, typeName)
					owner, found := semanticMemberOwner(model, pkg, class, value.Sel.Name, map[*ClassDecl]bool{})
					if found && !memberVisibilityAllowed(model, pkg, nil, owner, value.Sel.Name) {
						visibilityErr = goDeclMemberVisibilityError(declaration, value.Sel.Pos(), value.Sel.Name, owner)
					}
				case *ast.CompositeLit:
					class := semanticClassForTypeName(pkg, goTypeExpressionName(value.Type))
					for _, element := range value.Elts {
						key, ok := element.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						name, ok := key.Key.(*ast.Ident)
						if !ok {
							continue
						}
						owner, found := semanticFieldOwner(model, pkg, class, name.Name, map[*ClassDecl]bool{})
						if found && !memberVisibilityAllowed(model, pkg, nil, owner, name.Name) {
							visibilityErr = goDeclMemberVisibilityError(declaration, name.Pos(), name.Name, owner)
							return false
						}
					}
				}
				return true
			})
			if visibilityErr != nil {
				return visibilityErr
			}
			continue
		}
		if function.Body == nil {
			continue
		}
		valueTypes := map[string]string{}
		if function.Recv != nil {
			for _, field := range function.Recv.List {
				typeName := goTypeExpressionName(field.Type)
				for _, name := range field.Names {
					valueTypes[name.Name] = typeName
				}
			}
		}
		if function.Type.Params != nil {
			for _, field := range function.Type.Params.List {
				typeName := goTypeExpressionName(field.Type)
				for _, name := range field.Names {
					valueTypes[name.Name] = typeName
				}
			}
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.ValueSpec:
				typeName := goTypeExpressionName(value.Type)
				for index, name := range value.Names {
					inferred := typeName
					if inferred == "" && index < len(value.Values) {
						inferred = goMemberVisibilityExprType(model, pkg, value.Values[index], valueTypes)
					}
					if inferred != "" {
						valueTypes[name.Name] = inferred
					}
				}
			case *ast.AssignStmt:
				for index, left := range value.Lhs {
					if name, ok := left.(*ast.Ident); ok && index < len(value.Rhs) {
						if inferred := goMemberVisibilityExprType(model, pkg, value.Rhs[index], valueTypes); inferred != "" {
							valueTypes[name.Name] = inferred
						}
					}
				}
			case *ast.RangeStmt:
				if value.Value != nil {
					if name, ok := value.Value.(*ast.Ident); ok {
						if inferred := memberVisibilityIndexedTypeName(goMemberVisibilityExprType(model, pkg, value.X, valueTypes)); inferred != "" {
							valueTypes[name.Name] = inferred
						}
					}
				}
			case *ast.TypeSwitchStmt:
				name := goTypeSwitchBindingName(value.Assign)
				if name == "" || value.Body == nil {
					break
				}
				for _, statement := range value.Body.List {
					clause, ok := statement.(*ast.CaseClause)
					if !ok || len(clause.List) != 1 {
						continue
					}
					typeName := goTypeExpressionName(clause.List[0])
					if semanticClassForTypeName(pkg, typeName) != nil {
						valueTypes[name] = typeName
					}
				}
			}
			return true
		})
		var visibilityErr error
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if visibilityErr != nil {
				return false
			}
			switch value := node.(type) {
			case *ast.SelectorExpr:
				typeName := goMemberVisibilityExprType(model, pkg, value.X, valueTypes)
				class := semanticClassForTypeName(pkg, typeName)
				owner, found := semanticMemberOwner(model, pkg, class, value.Sel.Name, map[*ClassDecl]bool{})
				if found && !memberVisibilityAllowed(model, pkg, nil, owner, value.Sel.Name) {
					visibilityErr = goDeclMemberVisibilityError(declaration, value.Sel.Pos(), value.Sel.Name, owner)
				}
			case *ast.CompositeLit:
				class := semanticClassForTypeName(pkg, goTypeExpressionName(value.Type))
				for _, element := range value.Elts {
					key, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					name, ok := key.Key.(*ast.Ident)
					if !ok {
						continue
					}
					owner, found := semanticFieldOwner(model, pkg, class, name.Name, map[*ClassDecl]bool{})
					if found && !memberVisibilityAllowed(model, pkg, nil, owner, name.Name) {
						visibilityErr = goDeclMemberVisibilityError(declaration, name.Pos(), name.Name, owner)
						return false
					}
				}
			}
			return true
		})
		if visibilityErr != nil {
			return visibilityErr
		}
	}
	return nil
}

func goTypeSwitchBindingName(assignment ast.Stmt) string {
	switch value := assignment.(type) {
	case *ast.AssignStmt:
		if len(value.Lhs) == 1 {
			if name, ok := value.Lhs[0].(*ast.Ident); ok {
				return name.Name
			}
		}
	case *ast.ExprStmt:
		if assertion, ok := value.X.(*ast.TypeAssertExpr); ok {
			if name, ok := assertion.X.(*ast.Ident); ok {
				return name.Name
			}
		}
	}
	return ""
}

func goDeclMemberVisibilityError(declaration *GoDecl, position token.Pos, member string, owner *ClassDecl) error {
	visibility := "private"
	if memberVisibility(member) == memberProtected {
		visibility = "protected"
	}
	filename := declaration.SourceFile
	span := declaration.SourceSpan
	if declaration.FileSet != nil {
		positionInfo := declaration.FileSet.Position(position)
		if positionInfo.IsValid() {
			prefixLength := 0
			if declaration.Owner != nil {
				prefixLength = len("package " + goPackageName(declaration.Owner.Package) + "\n")
			}
			start := declaration.SourceSpan.Start + positionInfo.Offset - prefixLength
			if start >= declaration.SourceSpan.Start && start <= declaration.SourceSpan.End {
				span = Span{Start: start, End: start + len(member), Line: declaration.SourceLine + positionInfo.Line - 1, Column: positionInfo.Column}
			}
			if positionInfo.Filename != "" {
				filename = positionInfo.Filename
			}
		}
	}
	return diagnosticAt(filename, span, fmt.Errorf("cannot access %s member %s of class %s", visibility, member, owner.Name))
}

func goTypeExpressionName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return "*" + goTypeExpressionName(value.X)
	case *ast.SelectorExpr:
		return goTypeExpressionName(value.X) + "." + value.Sel.Name
	case *ast.ArrayType:
		if value.Len == nil {
			return "[]" + goTypeExpressionName(value.Elt)
		}
		return "[N]" + goTypeExpressionName(value.Elt)
	case *ast.IndexExpr:
		return goTypeExpressionName(value.X) + "[" + goTypeExpressionName(value.Index) + "]"
	case *ast.IndexListExpr:
		return goTypeExpressionName(value.X) + "[T]"
	case *ast.MapType:
		return "map[" + goTypeExpressionName(value.Key) + "]" + goTypeExpressionName(value.Value)
	case *ast.ParenExpr:
		return goTypeExpressionName(value.X)
	}
	return ""
}

func goMemberVisibilityExprType(model *SemanticModel, pkg *PackageSymbols, expression ast.Expr, valueTypes map[string]string) string {
	switch value := expression.(type) {
	case *ast.Ident:
		if valueTypes[value.Name] != "" {
			return valueTypes[value.Name]
		}
		if pkg.Classes[value.Name] != nil || pkg.ImportedClasses[value.Name] != nil {
			return value.Name
		}
	case *ast.SelectorExpr:
		qualified := goTypeExpressionName(value)
		if semanticClassForTypeName(pkg, qualified) != nil {
			return qualified
		}
		class := semanticClassForTypeName(pkg, goMemberVisibilityExprType(model, pkg, value.X, valueTypes))
		if class != nil {
			for _, parentName := range classParentNames(class) {
				if classParentFieldName(parentName) == value.Sel.Name || parentName == value.Sel.Name {
					return parentName
				}
			}
			owner, found := semanticMemberOwner(model, pkg, class, value.Sel.Name, map[*ClassDecl]bool{})
			if found {
				for _, field := range owner.Fields {
					if field.Name == value.Sel.Name && field.TypeAST != nil {
						result, _ := typeNodeSource(field.TypeAST)
						return result
					}
				}
				for _, method := range owner.Methods {
					if method.Name == value.Sel.Name {
						return methodResultType(method)
					}
				}
			}
		}
	case *ast.CallExpr:
		if name := goTypeExpressionName(value.Fun); semanticClassForTypeName(pkg, name) != nil {
			return name
		}
		if selector, ok := value.Fun.(*ast.SelectorExpr); ok {
			class := semanticClassForTypeName(pkg, goMemberVisibilityExprType(model, pkg, selector.X, valueTypes))
			if method, _ := semanticMethodForCall(model, pkg, class, selector.Sel.Name, len(value.Args), map[*ClassDecl]bool{}); method != nil {
				return methodResultType(*method)
			}
		}
		if name, ok := value.Fun.(*ast.Ident); ok {
			return semanticFunctionResultType(pkg, name.Name)
		}
	case *ast.CompositeLit:
		return goTypeExpressionName(value.Type)
	case *ast.IndexExpr:
		return memberVisibilityIndexedTypeName(goMemberVisibilityExprType(model, pkg, value.X, valueTypes))
	case *ast.StarExpr:
		return goMemberVisibilityExprType(model, pkg, value.X, valueTypes)
	case *ast.ParenExpr:
		return goMemberVisibilityExprType(model, pkg, value.X, valueTypes)
	case *ast.TypeAssertExpr:
		return goTypeExpressionName(value.Type)
	}
	return ""
}

func memberVisibilityIndexedTypeName(typeName string) string {
	typeName = strings.TrimSpace(typeName)
	if strings.HasPrefix(typeName, "[]") {
		return strings.TrimSpace(strings.TrimPrefix(typeName, "[]"))
	}
	if strings.HasPrefix(typeName, "[") {
		if close := strings.IndexByte(typeName, ']'); close >= 0 {
			return strings.TrimSpace(typeName[close+1:])
		}
	}
	if strings.HasPrefix(typeName, "map[") {
		if close := strings.IndexByte(typeName, ']'); close >= 0 {
			return strings.TrimSpace(typeName[close+1:])
		}
	}
	return ""
}

// currentClass is non-nil only for methods declared on that class. An
// extension has an implicit receiver type, but does not acquire the target
// class's private or protected privileges.
func validateMemberVisibilityInMethod(model *SemanticModel, pkg *PackageSymbols, currentClass *ClassDecl, method Method, implicitReceiverType string) error {
	if method.BodyAST == nil {
		return validateLegacyMemberVisibility(model, pkg, currentClass, method, memberVisibilityParameterTypes(pkg, method, implicitReceiverType))
	}
	valueTypes := map[string]string{}
	if currentClass != nil {
		valueTypes["this"] = currentClass.Name
	}
	if implicitReceiverType != "" {
		valueTypes["this"] = implicitReceiverType
	}
	for _, parameter := range method.ParameterAST {
		if parameter.Name == "" || parameter.Type == nil {
			continue
		}
		if typeName, err := typeNodeSource(parameter.Type); err == nil {
			valueTypes[parameter.Name] = typeName
		}
	}
	collectMemberVisibilityBindings(model, pkg, method.BodyAST, valueTypes)
	var validationErr error
	walkMemberVisibilityBlockExpressions(method.BodyAST, func(expression ExprNode) {
		if validationErr != nil {
			return
		}
		collectMemberVisibilityExprs(expression, func(candidate ExprNode) {
			if validationErr != nil {
				return
			}
			if literal, ok := candidate.(*CompositeLiteralExpr); ok {
				validationErr = validateCompositeLiteralVisibility(model, pkg, currentClass, method, literal, valueTypes)
				return
			}
			selector, ok := candidate.(*SelectorExpr)
			if !ok {
				return
			}
			receiverType := memberVisibilityExpressionType(model, pkg, selector.Receiver, valueTypes, map[ExprNode]bool{})
			receiverClass := semanticClassForTypeName(pkg, receiverType)
			if receiverClass == nil {
				return
			}
			owner, found := semanticMemberOwner(model, pkg, receiverClass, selector.Name, map[*ClassDecl]bool{})
			if !found {
				return
			}
			if memberVisibilityAllowed(model, pkg, currentClass, owner, selector.Name) {
				return
			}
			validationErr = memberVisibilityDiagnostic(currentClass, method, selector.Name, owner, selector.Span())
		})
	})
	if validationErr != nil {
		return validationErr
	}

	return nil
}

func memberVisibilityAllowed(model *SemanticModel, pkg *PackageSymbols, currentClass, owner *ClassDecl, member string) bool {
	visibility := memberVisibility(member)
	if visibility == memberPublic || visibility == memberPrivate && currentClass == owner {
		return true
	}
	return visibility == memberProtected && currentClass != nil && semanticClassInherits(model, pkg, currentClass, owner, map[*ClassDecl]bool{})
}

func memberVisibilityDiagnostic(currentClass *ClassDecl, method Method, member string, owner *ClassDecl, span Span) error {
	visibilityName := "private"
	if memberVisibility(member) == memberProtected {
		visibilityName = "protected"
	}
	cause := fmt.Errorf("cannot access %s member %s of class %s", visibilityName, member, owner.Name)
	if currentClass != nil {
		return methodDiagnosticf(currentClass, &method, "class %s method %s %w", currentClass.Name, method.Name, cause)
	}
	fileName := ""
	if method.Owner != nil {
		fileName = method.Owner.Name
		if method.Owner.SourcePath != "" {
			fileName = method.Owner.SourcePath
		}
	}
	return diagnosticAt(fileName, completeSpan(span, 0), cause)
}

func validateCompositeLiteralVisibility(model *SemanticModel, pkg *PackageSymbols, currentClass *ClassDecl, method Method, literal *CompositeLiteralExpr, valueTypes map[string]string) error {
	if literal == nil || literal.Type == nil {
		return nil
	}
	typeName, err := typeNodeSource(literal.Type)
	if err != nil {
		return nil
	}
	class := semanticClassForTypeName(pkg, typeName)
	if class == nil {
		return nil
	}
	for _, element := range literal.Elements {
		if element.Key == nil {
			continue
		}
		member := ""
		span := element.Key.Span()
		qualifier := ""
		switch key := element.Key.(type) {
		case *NameExpr:
			member = key.Name
		case *SelectorExpr:
			member = key.Name
			qualifier, _ = expressionTypeName(key.Receiver)
		default:
			continue
		}
		owner, found := semanticFieldOwner(model, pkg, class, member, map[*ClassDecl]bool{})
		if !found {
			continue
		}
		if qualifier != "" && classHasParentQualifier(class, qualifier) {
			for _, parentName := range classParentNames(class) {
				if parentName != qualifier && classParentFieldName(parentName) != qualifier {
					continue
				}
				if parent := semanticParentClass(model, pkg, class, parentName); parent != nil {
					if qualifiedOwner, exists := semanticFieldOwner(model, pkg, parent, member, map[*ClassDecl]bool{}); exists {
						owner = qualifiedOwner
					}
				}
				break
			}
		}
		if memberVisibilityAllowed(model, pkg, currentClass, owner, member) {
			continue
		}
		return memberVisibilityDiagnostic(currentClass, method, member, owner, span)
	}
	return nil
}

func semanticFieldOwner(model *SemanticModel, pkg *PackageSymbols, class *ClassDecl, name string, visiting map[*ClassDecl]bool) (*ClassDecl, bool) {
	if class == nil || visiting[class] {
		return nil, false
	}
	visiting[class] = true
	defer delete(visiting, class)
	for _, field := range class.Fields {
		if field.Name == name {
			return class, true
		}
	}
	for _, parentName := range classParentNames(class) {
		if owner, found := semanticFieldOwner(model, pkg, semanticParentClass(model, pkg, class, parentName), name, visiting); found {
			return owner, true
		}
	}
	return nil, false
}

func classHasParentQualifier(class *ClassDecl, qualifier string) bool {
	for _, parentName := range classParentNames(class) {
		if parentName == qualifier || classParentFieldName(parentName) == qualifier {
			return true
		}
	}
	return false
}

func memberVisibilityParameterTypes(pkg *PackageSymbols, method Method, implicitReceiverType string) map[string]string {
	valueTypes := map[string]string{}
	if implicitReceiverType != "" {
		valueTypes["this"] = implicitReceiverType
	}
	for _, parameter := range method.ParameterAST {
		if parameter.Name == "" || parameter.Type == nil {
			continue
		}
		if typeName, err := typeNodeSource(parameter.Type); err == nil {
			valueTypes[parameter.Name] = typeName
		}
	}
	return valueTypes
}

func walkMemberVisibilityBlockExpressions(block *BlockStmt, visit func(ExprNode)) {
	if block == nil || visit == nil {
		return
	}
	for _, statement := range block.Statements {
		walkStmtExpressions(statement, visit)
	}
}

func collectMemberVisibilityExprs(expression ExprNode, visit func(ExprNode)) {
	if expression == nil || visit == nil {
		return
	}
	visit(expression)
	switch value := expression.(type) {
	case *UnaryExpr:
		collectMemberVisibilityExprs(value.Operand, visit)
	case *BinaryExpr:
		collectMemberVisibilityExprs(value.Left, visit)
		collectMemberVisibilityExprs(value.Right, visit)
	case *AssignmentExpr:
		for _, item := range value.Left {
			collectMemberVisibilityExprs(item, visit)
		}
		for _, item := range value.Right {
			collectMemberVisibilityExprs(item, visit)
		}
	case *SelectorExpr:
		collectMemberVisibilityExprs(value.Receiver, visit)
	case *IndexExpr:
		collectMemberVisibilityExprs(value.Receiver, visit)
		collectMemberVisibilityExprs(value.Index, visit)
	case *IndexListExpr:
		collectMemberVisibilityExprs(value.Receiver, visit)
		for _, item := range value.Indices {
			collectMemberVisibilityExprs(item, visit)
		}
	case *SliceExpr:
		collectMemberVisibilityExprs(value.Receiver, visit)
		collectMemberVisibilityExprs(value.Low, visit)
		collectMemberVisibilityExprs(value.High, visit)
		collectMemberVisibilityExprs(value.Max, visit)
	case *TypeAssertExpr:
		collectMemberVisibilityExprs(value.Expression, visit)
	case *PostfixExpr:
		collectMemberVisibilityExprs(value.Expression, visit)
	case *SpreadExpr:
		collectMemberVisibilityExprs(value.Expression, visit)
	case *SendExpr:
		collectMemberVisibilityExprs(value.Channel, visit)
		collectMemberVisibilityExprs(value.Value, visit)
	case *CallExpr:
		collectMemberVisibilityExprs(value.Callee, visit)
		for _, argument := range value.Arguments {
			collectMemberVisibilityExprs(argument.Value, visit)
		}
	case *ParenthesizedExpr:
		collectMemberVisibilityExprs(value.Inner, visit)
	case *CompositeLiteralExpr:
		for _, element := range value.Elements {
			collectMemberVisibilityExprs(element.Key, visit)
			collectMemberVisibilityExprs(element.Value, visit)
		}
	case *InterpolatedStringExpr:
		for _, segment := range value.Segments {
			collectMemberVisibilityExprs(segment.Expression, visit)
		}
	case *LambdaExpr:
		collectMemberVisibilityExprs(value.Body, visit)
		walkMemberVisibilityBlockExpressions(value.BlockBody, func(nested ExprNode) { collectMemberVisibilityExprs(nested, visit) })
	case *FunctionLiteralExpr:
		walkMemberVisibilityBlockExpressions(value.Body, func(nested ExprNode) { collectMemberVisibilityExprs(nested, visit) })
	}
}

func collectMemberVisibilityBindings(model *SemanticModel, pkg *PackageSymbols, block *BlockStmt, valueTypes map[string]string) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		switch value := statement.(type) {
		case *DeclarationStmt:
			typeName := ""
			if value.Type != nil {
				typeName, _ = typeNodeSource(value.Type)
			}
			for index, name := range value.Names {
				inferred := typeName
				if inferred == "" && index < len(value.Values) {
					inferred = memberVisibilityExpressionType(model, pkg, value.Values[index], valueTypes, map[ExprNode]bool{})
				}
				if inferred != "" {
					valueTypes[name.Text] = inferred
				}
			}
		case *AssignmentStmt:
			for index, left := range value.Left {
				name, ok := left.(*NameExpr)
				if ok && index < len(value.Right) {
					if inferred := memberVisibilityExpressionType(model, pkg, value.Right[index], valueTypes, map[ExprNode]bool{}); inferred != "" {
						valueTypes[name.Name] = inferred
					}
				}
			}
		case *IfStmt:
			collectMemberVisibilityExprBinding(model, pkg, value.Init, valueTypes)
			collectMemberVisibilityBindings(model, pkg, value.Body, valueTypes)
			collectMemberVisibilityBindings(model, pkg, value.Else, valueTypes)
			if value.ElseIf != nil {
				collectMemberVisibilityBindings(model, pkg, &BlockStmt{Statements: []Stmt{value.ElseIf}}, valueTypes)
			}
		case *ForStmt:
			collectMemberVisibilityExprBinding(model, pkg, value.Init, valueTypes)
			for index, name := range value.RangeKey {
				if identifier, ok := name.(*NameExpr); ok && index == 1 {
					if inferred := memberVisibilityIndexedType(model, pkg, value.RangeExpr, valueTypes); inferred != "" {
						valueTypes[identifier.Name] = inferred
					}
				}
			}
			collectMemberVisibilityBindings(model, pkg, value.Body, valueTypes)
		case *TryStmt:
			collectMemberVisibilityBindings(model, pkg, value.Body, valueTypes)
			collectMemberVisibilityBindings(model, pkg, value.Finally, valueTypes)
			for _, clause := range value.Catches {
				collectMemberVisibilityBindings(model, pkg, clause.Body, valueTypes)
			}
		case *SwitchStmt:
			collectMemberVisibilityExprBinding(model, pkg, value.Init, valueTypes)
			if assignment, ok := value.Init.(*AssignmentExpr); ok && isTypeSwitchAssignment(assignment) {
				collectMemberVisibilityTypeSwitchBindings(model, pkg, assignment, value.Body, valueTypes)
			} else {
				collectMemberVisibilityBindings(model, pkg, value.Body, valueTypes)
			}
		case *CaseStmt:
			if value.Clause.Body != nil {
				collectMemberVisibilityBindings(model, pkg, value.Clause.Body, valueTypes)
			}
		case *TokenStmt:
			collectMemberVisibilityBindings(model, pkg, value.Body, valueTypes)
			for _, child := range value.Children {
				collectMemberVisibilityBindings(model, pkg, &BlockStmt{Statements: []Stmt{child}}, valueTypes)
			}
		}
		collectMemberVisibilityNestedBindings(model, pkg, statement, valueTypes)
	}
}

func collectMemberVisibilityNestedBindings(model *SemanticModel, pkg *PackageSymbols, statement Stmt, valueTypes map[string]string) {
	walkStmtExpressions(statement, func(expression ExprNode) {
		collectMemberVisibilityExprs(expression, func(candidate ExprNode) {
			localTypes := cloneMemberVisibilityTypes(valueTypes)
			switch value := candidate.(type) {
			case *CallExpr:
				parameters := memberVisibilityCallParameters(model, pkg, value, valueTypes)
				for index, argument := range value.Arguments {
					parameterIndex := index
					if argument.Name != "" {
						for candidateIndex, parameter := range parameters {
							if parameter.Name == argument.Name {
								parameterIndex = candidateIndex
								break
							}
						}
					}
					if parameterIndex < len(parameters) {
						bindMemberVisibilityFunctionArgument(model, pkg, argument.Value, parameters[parameterIndex].Type, valueTypes)
					}
				}
			case *LambdaExpr:
				parameters, err := lambdaParametersFromTokens(value.Parameters)
				if err == nil {
					for _, parameter := range parameters {
						if parameter.Name != "" && parameter.Type != nil {
							if typeName, typeErr := typeNodeSource(parameter.Type); typeErr == nil {
								localTypes[parameter.Name] = typeName
							}
						}
					}
				}
				collectMemberVisibilityBindings(model, pkg, value.BlockBody, localTypes)
			case *FunctionLiteralExpr:
				if value.Type != nil {
					for _, parameter := range value.Type.Parameters {
						if parameter.Name != "" && parameter.Type != nil {
							if typeName, typeErr := typeNodeSource(parameter.Type); typeErr == nil {
								localTypes[parameter.Name] = typeName
							}
						}
					}
				}
				collectMemberVisibilityBindings(model, pkg, value.Body, localTypes)
			default:
				return
			}
			for name, typeName := range localTypes {
				valueTypes[name] = typeName
			}
		})
	})
}

func memberVisibilityCallParameters(model *SemanticModel, pkg *PackageSymbols, call *CallExpr, valueTypes map[string]string) []ParameterNode {
	if call == nil {
		return nil
	}
	if selector, ok := call.Callee.(*SelectorExpr); ok {
		class := semanticClassForTypeName(pkg, memberVisibilityExpressionType(model, pkg, selector.Receiver, valueTypes, map[ExprNode]bool{}))
		if method, _ := semanticMethodForCall(model, pkg, class, selector.Name, len(call.Arguments), map[*ClassDecl]bool{}); method != nil {
			return method.ParameterAST
		}
	}
	if name, ok := call.Callee.(*NameExpr); ok {
		for _, file := range pkg.Files {
			for _, declaration := range file.Decls {
				switch value := declaration.(type) {
				case *FunctionDecl:
					if value.Name == name.Name && len(value.Method.ParameterAST) == len(call.Arguments) {
						return value.Method.ParameterAST
					}
				case *MixedDecl:
					for _, function := range value.Functions {
						if function != nil && function.Name == name.Name && len(function.Method.ParameterAST) == len(call.Arguments) {
							return function.Method.ParameterAST
						}
					}
				}
			}
		}
	}
	typeName := memberVisibilityExpressionType(model, pkg, call.Callee, valueTypes, map[ExprNode]bool{})
	functionType, _ := parseTypeText(typeName).(*FunctionType)
	if functionType != nil {
		return functionType.Parameters
	}
	return nil
}

func bindMemberVisibilityFunctionArgument(model *SemanticModel, pkg *PackageSymbols, argument ExprNode, expected TypeNode, valueTypes map[string]string) {
	functionType, ok := expected.(*FunctionType)
	if !ok {
		return
	}
	bindParameters := func(names []ParameterNode) {
		for index := range names {
			if names[index].Type == nil && index < len(functionType.Parameters) {
				names[index].Type = functionType.Parameters[index].Type
			}
			if names[index].Name != "" && names[index].Type != nil {
				if typeName, err := typeNodeSource(names[index].Type); err == nil {
					valueTypes[names[index].Name] = typeName
				}
			}
		}
	}
	switch value := argument.(type) {
	case *LambdaExpr:
		parameters, err := lambdaParametersFromTokens(value.Parameters)
		if err != nil || len(parameters) != len(functionType.Parameters) {
			return
		}
		bindParameters(parameters)
		collectMemberVisibilityBindings(model, pkg, value.BlockBody, valueTypes)
	case *FunctionLiteralExpr:
		if value.Type == nil || len(value.Type.Parameters) != len(functionType.Parameters) {
			return
		}
		parameters := append([]ParameterNode(nil), value.Type.Parameters...)
		bindParameters(parameters)
		collectMemberVisibilityBindings(model, pkg, value.Body, valueTypes)
	}
}

func collectMemberVisibilityTypeSwitchBindings(model *SemanticModel, pkg *PackageSymbols, assignment *AssignmentExpr, body *BlockStmt, valueTypes map[string]string) {
	if assignment == nil || body == nil || len(assignment.Left) != 1 {
		collectMemberVisibilityBindings(model, pkg, body, valueTypes)
		return
	}
	name, ok := assignment.Left[0].(*NameExpr)
	if !ok {
		collectMemberVisibilityBindings(model, pkg, body, valueTypes)
		return
	}
	for _, statement := range body.Statements {
		caseStatement, ok := statement.(*CaseStmt)
		if !ok {
			collectMemberVisibilityBindings(model, pkg, &BlockStmt{Statements: []Stmt{statement}}, valueTypes)
			continue
		}
		caseTypes := caseStatement.Clause.Expressions
		if len(caseTypes) == 1 {
			if typeName, ok := memberVisibilityTypeSwitchCaseType(caseTypes[0]); ok && semanticClassForTypeName(pkg, typeName) != nil {
				valueTypes[name.Name] = typeName
				branchTypes := cloneMemberVisibilityTypes(valueTypes)
				collectMemberVisibilityBindings(model, pkg, caseStatement.Clause.Body, branchTypes)
				continue
			}
		}
		collectMemberVisibilityBindings(model, pkg, caseStatement.Clause.Body, cloneMemberVisibilityTypes(valueTypes))
	}
}

func memberVisibilityTypeSwitchCaseType(expression ExprNode) (string, bool) {
	switch value := expression.(type) {
	case *NameExpr:
		return value.Name, true
	case *SelectorExpr:
		if typeName, ok := expressionTypeName(value); ok {
			return typeName, true
		}
	case *UnaryExpr:
		if value.Operator == "*" {
			return memberVisibilityTypeSwitchCaseType(value.Operand)
		}
	}
	return "", false
}

func cloneMemberVisibilityTypes(valueTypes map[string]string) map[string]string {
	clone := make(map[string]string, len(valueTypes))
	for name, typeName := range valueTypes {
		clone[name] = typeName
	}
	return clone
}

func collectMemberVisibilityExprBinding(model *SemanticModel, pkg *PackageSymbols, expression ExprNode, valueTypes map[string]string) {
	assignment, ok := expression.(*AssignmentExpr)
	if !ok || assignment.Operator != ":=" && assignment.Operator != "=" {
		return
	}
	for index, target := range assignment.Left {
		name, ok := target.(*NameExpr)
		if ok && index < len(assignment.Right) {
			if inferred := memberVisibilityExpressionType(model, pkg, assignment.Right[index], valueTypes, map[ExprNode]bool{}); inferred != "" {
				valueTypes[name.Name] = inferred
			}
		}
	}
}

func memberVisibilityExpressionType(model *SemanticModel, pkg *PackageSymbols, expression ExprNode, valueTypes map[string]string, visiting map[ExprNode]bool) string {
	if expression == nil || visiting[expression] {
		return ""
	}
	visiting[expression] = true
	defer delete(visiting, expression)
	switch value := expression.(type) {
	case *NameExpr:
		if typeName := valueTypes[value.Name]; typeName != "" {
			return typeName
		}
		if pkg.Classes[value.Name] != nil || pkg.ImportedClasses[value.Name] != nil {
			return value.Name
		}
	case *SelectorExpr:
		qualified, _ := expressionTypeName(value)
		if semanticClassForTypeName(pkg, qualified) != nil {
			return qualified
		}
		receiverType := memberVisibilityExpressionType(model, pkg, value.Receiver, valueTypes, visiting)
		class := semanticClassForTypeName(pkg, receiverType)
		if class == nil {
			return ""
		}
		for _, parentName := range classParentNames(class) {
			if classParentFieldName(parentName) == value.Name || parentName == value.Name {
				return parentName
			}
		}
		owner, found := semanticMemberOwner(model, pkg, class, value.Name, map[*ClassDecl]bool{})
		if !found {
			return ""
		}
		for _, field := range owner.Fields {
			if field.Name == value.Name && field.TypeAST != nil {
				result, _ := typeNodeSource(field.TypeAST)
				return result
			}
		}
		for _, method := range owner.Methods {
			if method.Name == value.Name {
				return methodResultType(method)
			}
		}
	case *CallExpr:
		if className, ok := expressionTypeName(value.Callee); ok && semanticClassForTypeName(pkg, className) != nil {
			return className
		}
		if selector, ok := value.Callee.(*SelectorExpr); ok {
			class := semanticClassForTypeName(pkg, memberVisibilityExpressionType(model, pkg, selector.Receiver, valueTypes, visiting))
			if class != nil {
				if method, _ := semanticMethodForCall(model, pkg, class, selector.Name, len(value.Arguments), map[*ClassDecl]bool{}); method != nil {
					return methodResultType(*method)
				}
			}
		}
		if name, ok := value.Callee.(*NameExpr); ok {
			if result := semanticFunctionResultType(pkg, name.Name); result != "" {
				return result
			}
		}
	case *CompositeLiteralExpr:
		if value.Type != nil {
			result, _ := typeNodeSource(value.Type)
			return result
		}
	case *IndexExpr:
		return memberVisibilityIndexedType(model, pkg, value.Receiver, valueTypes)
	case *IndexListExpr:
		return memberVisibilityExpressionType(model, pkg, value.Receiver, valueTypes, visiting)
	case *UnaryExpr:
		return memberVisibilityExpressionType(model, pkg, value.Operand, valueTypes, visiting)
	case *ParenthesizedExpr:
		return memberVisibilityExpressionType(model, pkg, value.Inner, valueTypes, visiting)
	case *PostfixExpr:
		return memberVisibilityExpressionType(model, pkg, value.Expression, valueTypes, visiting)
	case *TypeAssertExpr:
		if value.Type != nil {
			result, _ := typeNodeSource(value.Type)
			return result
		}
	}
	return ""
}

func memberVisibilityIndexedType(model *SemanticModel, pkg *PackageSymbols, expression ExprNode, valueTypes map[string]string) string {
	return memberVisibilityIndexedTypeName(memberVisibilityExpressionType(model, pkg, expression, valueTypes, map[ExprNode]bool{}))
}

func methodResultType(method Method) string {
	if method.ResultAST != nil {
		result, _ := typeNodeSource(method.ResultAST)
		return result
	}
	if len(method.ResultFieldsAST) == 1 && method.ResultFieldsAST[0].Type != nil {
		result, _ := typeNodeSource(method.ResultFieldsAST[0].Type)
		return result
	}
	return ""
}

func semanticMethodForCall(model *SemanticModel, pkg *PackageSymbols, class *ClassDecl, name string, arity int, visiting map[*ClassDecl]bool) (*Method, *ClassDecl) {
	if class == nil || visiting[class] {
		return nil, nil
	}
	visiting[class] = true
	defer delete(visiting, class)
	for _, method := range class.Methods {
		if method.Name == name && len(method.ParameterAST) == arity {
			return &method, class
		}
	}
	for _, parentName := range classParentNames(class) {
		parent := semanticParentClass(model, pkg, class, parentName)
		if method, owner := semanticMethodForCall(model, pkg, parent, name, arity, visiting); method != nil {
			return method, owner
		}
	}
	return nil, nil
}

func semanticFunctionResultType(pkg *PackageSymbols, name string) string {
	seen := map[*File]bool{}
	for _, file := range pkg.Files {
		if file == nil || seen[file] {
			continue
		}
		seen[file] = true
		for _, declaration := range file.Decls {
			switch value := declaration.(type) {
			case *FunctionDecl:
				if value.Name == name {
					return methodResultType(value.Method)
				}
			case *MixedDecl:
				for _, function := range value.Functions {
					if function != nil && function.Name == name {
						return methodResultType(function.Method)
					}
				}
			}
		}
	}
	return ""
}

func validateLegacyMemberVisibility(model *SemanticModel, pkg *PackageSymbols, currentClass *ClassDecl, method Method, valueTypes map[string]string) error {
	// Legacy bodies do not expose expression nodes, so only simple, statically
	// typed receivers can be checked safely here.
	tokens := methodBodyTokens(method)
	for index, token := range tokens {
		if token.Kind != TokenIdentifier || index+2 >= len(tokens) || tokens[index+1].Text != "." {
			continue
		}
		class := semanticClassForTypeName(pkg, valueTypes[token.Text])
		if class == nil {
			continue
		}
		member := tokens[index+2].Text
		owner, found := semanticMemberOwner(model, pkg, class, member, map[*ClassDecl]bool{})
		if found && memberVisibility(member) != memberPublic && !(memberVisibility(member) == memberPrivate && currentClass == owner) && !(memberVisibility(member) == memberProtected && currentClass != nil && semanticClassInherits(model, pkg, currentClass, owner, map[*ClassDecl]bool{})) {
			return fmt.Errorf("cannot access %s member %s of class %s", map[memberVisibilityLevel]string{memberPrivate: "private", memberProtected: "protected"}[memberVisibility(member)], member, owner.Name)
		}
	}
	return nil
}

func semanticClassForTypeName(pkg *PackageSymbols, typeName string) *ClassDecl {
	if pkg == nil {
		return nil
	}
	typeName = strings.TrimSpace(typeName)
	for strings.HasPrefix(typeName, "*") {
		typeName = strings.TrimSpace(strings.TrimPrefix(typeName, "*"))
	}
	if strings.HasPrefix(typeName, "[]") || strings.HasPrefix(typeName, "map[") || strings.HasPrefix(typeName, "func(") {
		return nil
	}
	if bracket := strings.IndexAny(typeName, "[< "); bracket >= 0 {
		typeName = strings.TrimSpace(typeName[:bracket])
	}
	seenAliases := map[string]bool{}
	for pkg.TypeAliases[typeName] != "" && !seenAliases[typeName] {
		seenAliases[typeName] = true
		typeName = strings.TrimSpace(pkg.TypeAliases[typeName])
		if bracket := strings.IndexAny(typeName, "[< "); bracket >= 0 {
			typeName = strings.TrimSpace(typeName[:bracket])
		}
	}
	if class := pkg.Classes[typeName]; class != nil {
		return class
	}
	if class := pkg.ImportedClasses[typeName]; class != nil {
		return class
	}
	if dot := strings.LastIndexByte(typeName, '.'); dot >= 0 {
		return pkg.ImportedClasses[typeName]
	}
	return nil
}

func semanticMemberOwner(model *SemanticModel, pkg *PackageSymbols, class *ClassDecl, name string, visiting map[*ClassDecl]bool) (*ClassDecl, bool) {
	if class == nil || visiting[class] {
		return nil, false
	}
	visiting[class] = true
	defer delete(visiting, class)
	for _, field := range class.Fields {
		if field.Name == name {
			return class, true
		}
	}
	for _, method := range class.Methods {
		if method.Name == name {
			return class, true
		}
	}
	for _, parentName := range classParentNames(class) {
		parent := semanticParentClass(model, pkg, class, parentName)
		if owner, found := semanticMemberOwner(model, pkg, parent, name, visiting); found {
			return owner, true
		}
	}
	return nil, false
}

func semanticParentClass(model *SemanticModel, pkg *PackageSymbols, class *ClassDecl, parentName string) *ClassDecl {
	if parent := pkg.Classes[parentName]; parent != nil {
		return parent
	}
	if parent := pkg.ImportedClasses[parentName]; parent != nil {
		return parent
	}
	if model != nil && class != nil && class.Owner != nil {
		if ownerPackage := model.Packages[class.Owner.Package]; ownerPackage != nil {
			if parent := ownerPackage.Classes[parentName]; parent != nil {
				return parent
			}
			if parent := ownerPackage.ImportedClasses[parentName]; parent != nil {
				return parent
			}
		}
	}
	return nil
}

func semanticClassInherits(model *SemanticModel, pkg *PackageSymbols, class, ancestor *ClassDecl, visiting map[*ClassDecl]bool) bool {
	if class == nil || ancestor == nil || visiting[class] {
		return false
	}
	if class == ancestor {
		return true
	}
	visiting[class] = true
	defer delete(visiting, class)
	for _, parentName := range classParentNames(class) {
		if semanticClassInherits(model, pkg, semanticParentClass(model, pkg, class, parentName), ancestor, visiting) {
			return true
		}
	}
	return false
}
