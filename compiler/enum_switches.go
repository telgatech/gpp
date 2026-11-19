package compiler

import (
	"fmt"
	"strings"
)

// validateFileEnumSwitches validates enum-qualified selectors and structured
// enum switches. Without a default clause, every member must appear as a case.
func validateFileEnumSwitches(file *File, pkg *PackageSymbols, model *SemanticModel, modulePath string) error {
	if file == nil || pkg == nil {
		return nil
	}
	context := localConstructorContext(pkg.Classes)
	context.Enums = make(map[string]*EnumDecl, len(pkg.Enums))
	for name, enum := range pkg.Enums {
		context.Enums[name] = enum
	}
	context.Package = pkg.Name
	context.GlobalValueTypes = globalValueTypesForFile(file)
	configureImportedEnums(&context, file, model, modulePath)
	context.Extensions = fileExtensionMethods(file, model, pkg, modulePath)

	var diagnostics []error
	checkMethod := func(method Method, class *ClassDecl) {
		if method.BodyAST == nil {
			return
		}
		methodContext := context
		methodContext.CurrentParameterTypes = parameterTypeMapFromNodes(method.ParameterAST)
		if class != nil {
			methodContext.CurrentClass = class.Name
			methodContext.CurrentParameterTypes["this"] = "*" + class.Name
			for name, fieldType := range classFieldTypesForClass(class, pkg.Classes, map[*ClassDecl]bool{}) {
				methodContext.CurrentParameterTypes[name] = fieldType
			}
		}
		valueTypes := polymorphismValueTypesAST(method.BodyAST, methodContext)
		validateEnumMembersInBlock(method.BodyAST, methodContext, file.Name, &diagnostics)
		validateEnumSwitchesInBlock(method.BodyAST, methodContext, valueTypes, file.Name, &diagnostics)
	}

	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *ValueDecl:
			validateEnumMembersInBlock(&BlockStmt{Statements: []Stmt{&DeclarationStmt{Values: value.Values}}}, context, file.Name, &diagnostics)
		case *FunctionDecl:
			checkMethod(value.Method, nil)
		case *ClassDecl:
			for _, method := range value.Methods {
				checkMethod(method, value)
			}
		case *ExtendDecl:
			for _, method := range value.Methods {
				checkMethod(method, nil)
			}
		case *MixedDecl:
			for _, function := range value.Functions {
				if function != nil {
					checkMethod(function.Method, nil)
				}
			}
		}
	}
	return NewDiagnosticErrors(diagnostics)
}

func fileExtensionMethods(file *File, model *SemanticModel, pkg *PackageSymbols, modulePath string) []extensionMethod {
	methods := extensionMethodsForDeclarations(pkg.Extensions, "")
	for alias, importPath := range availableImportsForFile(file, model, modulePath) {
		logicalPackage, ok := logicalPackageForImport(importPath, modulePath, model)
		if !ok {
			continue
		}
		importedPackage := model.Packages[logicalPackage]
		if importedPackage == nil {
			continue
		}
		importedMethods := extensionMethodsForDeclarations(importedPackage.Extensions, alias)
		if alias != "" && alias != "." {
			for index := range importedMethods {
				baseTarget := strings.TrimPrefix(importedMethods[index].Target, "*")
				if _, exists := importedPackage.Classes[baseTarget]; exists || importedPackage.Enums[baseTarget] != nil {
					importedMethods[index].Target = alias + "." + importedMethods[index].Target
				}
			}
		}
		methods = append(methods, importedMethods...)
	}
	return methods
}

func validateEnumMembersInBlock(block *BlockStmt, context constructorContext, file string, diagnostics *[]error) {
	walkBlockExpressions(block, func(expression ExprNode) {
		collectMemberVisibilityExprs(expression, func(candidate ExprNode) {
			selector, ok := candidate.(*SelectorExpr)
			if !ok {
				return
			}
			key, enum, ok := enumReferenceExpr(selector.Receiver, context)
			if !ok || !enumTypeReferenceKey(key, enum) {
				return
			}
			switch selector.Name {
			case "From", "values":
				return
			}
			if _, exists := enumMember(enum, selector.Name); exists || hasStaticEnumExtension(context.Extensions, key, enum, selector.Name) {
				return
			}
			message := fmt.Sprintf("invalid enum member %q for %s", selector.Name, key)
			*diagnostics = append(*diagnostics, diagnosticAt(file, selector.Span(), fmt.Errorf("%s", message)))
		})
	})
}

func enumTypeReferenceKey(key string, enum *EnumDecl) bool {
	if enum == nil {
		return false
	}
	return key == enum.Name || strings.HasSuffix(key, "."+enum.Name)
}

func hasStaticEnumExtension(extensions []extensionMethod, key string, enum *EnumDecl, name string) bool {
	for _, extension := range extensions {
		if extension.Method.Name != name || !extension.Method.IsStatic {
			continue
		}
		target := strings.TrimPrefix(normalizeExtensionTarget(extension.Target), "*")
		if bracket := strings.IndexByte(target, '['); bracket >= 0 {
			target = target[:bracket]
		}
		if target == key || key == enum.Name && target == enum.Name {
			return true
		}
	}
	return false
}

func validateEnumSwitchesInBlock(block *BlockStmt, context constructorContext, valueTypes map[string]string, file string, diagnostics *[]error) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		switch value := statement.(type) {
		case *SwitchStmt:
			if err := validateEnumSwitch(value, context, valueTypes, file); err != nil {
				*diagnostics = append(*diagnostics, err)
			}
			validateEnumSwitchesInBlock(value.Body, context, valueTypes, file, diagnostics)
		case *IfStmt:
			validateEnumSwitchesInBlock(value.Body, context, valueTypes, file, diagnostics)
			validateEnumSwitchesInBlock(value.Else, context, valueTypes, file, diagnostics)
			if value.ElseIf != nil {
				validateEnumSwitchesInBlock(&BlockStmt{Statements: []Stmt{value.ElseIf}}, context, valueTypes, file, diagnostics)
			}
		case *ForStmt:
			validateEnumSwitchesInBlock(value.Body, context, valueTypes, file, diagnostics)
		case *TryStmt:
			validateEnumSwitchesInBlock(value.Body, context, valueTypes, file, diagnostics)
			for _, clause := range value.Catches {
				validateEnumSwitchesInBlock(clause.Body, context, valueTypes, file, diagnostics)
			}
			validateEnumSwitchesInBlock(value.Finally, context, valueTypes, file, diagnostics)
		case *CaseStmt:
			validateEnumSwitchesInBlock(value.Clause.Body, context, valueTypes, file, diagnostics)
		case *BlockStmt:
			validateEnumSwitchesInBlock(value, context, valueTypes, file, diagnostics)
		case *TokenStmt:
			validateEnumSwitchesInBlock(value.Body, context, valueTypes, file, diagnostics)
			for _, child := range value.Children {
				validateEnumSwitchesInBlock(&BlockStmt{Statements: []Stmt{child}}, context, valueTypes, file, diagnostics)
			}
		}
	}
}

func validateEnumSwitch(statement *SwitchStmt, context constructorContext, valueTypes map[string]string, file string) error {
	if statement == nil || statement.Select || statement.Tag == nil || statement.Body == nil {
		return nil
	}

	tagType := strings.TrimSpace(staticExpressionTypeNode(statement.Tag, context, valueTypes))
	enumKey := strings.TrimPrefix(tagType, "*")
	enum := context.Enums[enumKey]
	if enum == nil {
		// A bare unresolved tag may not have enough local type information yet.
		// Enum-qualified selectors in its cases still identify the enum, while a
		// tag with a known non-enum type is left to ordinary Go type checking.
		if !enumSwitchTagTypeUnknown(statement.Tag, context, valueTypes) {
			return nil
		}
		enumKey, enum = enumFromSwitchCases(statement, context)
	}
	if enum == nil {
		return nil
	}

	seen := map[string]bool{}
	var diagnostics []error
	hasDefault := false
	for _, child := range statement.Body.Statements {
		clause, ok := child.(*CaseStmt)
		if !ok {
			continue
		}
		if clause.Clause.Default {
			hasDefault = true
			continue
		}
		for _, expression := range clause.Clause.Expressions {
			key, member, _, ok := enumSwitchCaseSelector(expression, context)
			if !ok || key != enumKey {
				continue
			}
			if _, exists := enumMember(enum, member); exists {
				seen[member] = true
				continue
			}
		}
	}

	if !hasDefault {
		missing := make([]string, 0)
		for _, member := range enum.Members {
			if !seen[member.Name] {
				missing = append(missing, fmt.Sprintf("%q", member.Name))
			}
		}
		if len(missing) > 0 {
			caseLabel := "cases"
			if len(missing) == 1 {
				caseLabel = "case"
			}
			diagnostics = append(diagnostics, diagnosticAt(file, statement.Span(), fmt.Errorf("missing enum %s %s for %s", caseLabel, strings.Join(missing, ", "), enumKey)))
		}
	}
	return NewDiagnosticErrors(diagnostics)
}

func enumSwitchTagTypeUnknown(tag ExprNode, context constructorContext, valueTypes map[string]string) bool {
	name, ok := tag.(*NameExpr)
	if !ok {
		return staticExpressionTypeNode(tag, context, valueTypes) == ""
	}
	if valueTypes[name.Name] != "" || context.GlobalValueTypes[name.Name] != "" {
		return false
	}
	return true
}

func enumFromSwitchCases(statement *SwitchStmt, context constructorContext) (string, *EnumDecl) {
	if statement == nil || statement.Body == nil {
		return "", nil
	}
	var key string
	var found *EnumDecl
	for _, child := range statement.Body.Statements {
		clause, ok := child.(*CaseStmt)
		if !ok {
			continue
		}
		for _, expression := range clause.Clause.Expressions {
			caseKey, _, _, ok := enumSwitchCaseSelector(expression, context)
			if !ok {
				continue
			}
			caseEnum := context.Enums[caseKey]
			if found != nil && (key != caseKey || found != caseEnum) {
				return "", nil
			}
			key, found = caseKey, caseEnum
		}
	}
	return key, found
}

func enumSwitchCaseSelector(expression ExprNode, context constructorContext) (string, string, *SelectorExpr, bool) {
	selector := unwrapEnumCaseSelector(expression)
	if selector == nil {
		return "", "", nil, false
	}
	key, _, ok := enumReferenceExpr(selector.Receiver, context)
	if !ok {
		return "", "", nil, false
	}
	return key, selector.Name, selector, true
}

func unwrapEnumCaseSelector(expression ExprNode) *SelectorExpr {
	if parenthesized, ok := expression.(*ParenthesizedExpr); ok {
		expression = parenthesized.Inner
	}
	selector, ok := expression.(*SelectorExpr)
	if !ok {
		return nil
	}
	return selector
}
