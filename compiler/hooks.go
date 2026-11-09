package compiler

import (
	"fmt"
	"strings"
)

// validateHookCallsFromMethod enforces that ordinary source calls to a hook
// are made from the class that declares that hook. Framework dispatch remains
// possible through its internal interfaces and generated/runtime dispatchers.
func validateHookCallsFromMethod(caller *ClassDecl, callerMethod *Method, classes map[string]*ClassDecl) error {
	if caller == nil || callerMethod == nil || callerMethod.BodyAST == nil {
		return nil
	}

	valueTypes := parameterTypeMapFromNodes(callerMethod.ParameterAST)
	valueTypes["this"] = caller.Name
	collectHookLocalTypes(callerMethod.BodyAST, valueTypes)
	var validationErr error
	walkIntrospectionBlockExpressions(callerMethod.BodyAST, func(expression ExprNode) {
		if validationErr != nil {
			return
		}
		call, ok := expression.(*CallExpr)
		if !ok {
			return
		}

		methodName := ""
		receiverClass := caller
		switch callee := call.Callee.(type) {
		case *NameExpr:
			methodName = callee.Name
		case *SelectorExpr:
			methodName = callee.Name
			receiverClass = classForHookReceiver(callee.Receiver, valueTypes, classes)
			if receiverClass == nil {
				return
			}
		default:
			return
		}

		hook, declaringClass := resolveHookMethod(receiverClass, methodName, len(call.Arguments), classes, map[*ClassDecl]bool{})
		if hook == nil || declaringClass == nil || declaringClass == caller {
			return
		}
		validationErr = diagnosticAt(
			caller.SourceFile,
			completeSpan(call.Span(), callerMethod.Span().Line),
			fmt.Errorf("hook %s may only be called by its declaring class %s", methodName, declaringClass.Name),
		)
	})
	return validationErr
}

func validateHookCallsFromFunction(function *FunctionDecl, context constructorContext) error {
	if function == nil || function.Method.BodyAST == nil {
		return nil
	}
	valueTypes := parameterTypeMapFromNodes(function.Method.ParameterAST)
	collectHookLocalTypes(function.Method.BodyAST, valueTypes)
	classes := allContextClasses(context)
	var validationErr error
	walkIntrospectionBlockExpressions(function.Method.BodyAST, func(expression ExprNode) {
		if validationErr != nil {
			return
		}
		call, ok := expression.(*CallExpr)
		if !ok {
			return
		}
		selector, ok := call.Callee.(*SelectorExpr)
		if !ok {
			return
		}
		class := classForHookReceiver(selector.Receiver, valueTypes, classes)
		hook, declaringClass := resolveHookMethod(class, selector.Name, len(call.Arguments), classes, map[*ClassDecl]bool{})
		if hook == nil || declaringClass == nil {
			return
		}
		validationErr = diagnosticAt(
			function.SourceFile,
			completeSpan(call.Span(), function.SourceLine),
			fmt.Errorf("hook %s may only be called by its declaring class %s", selector.Name, declaringClass.Name),
		)
	})
	return validationErr
}

func validateHookCallsFromExtension(method *Method, target, sourceFile string, sourceLine int, context constructorContext) error {
	if method == nil || method.BodyAST == nil {
		return nil
	}
	valueTypes := parameterTypeMapFromNodes(method.ParameterAST)
	valueTypes["this"] = target
	collectHookLocalTypes(method.BodyAST, valueTypes)
	classes := allContextClasses(context)
	var validationErr error
	walkIntrospectionBlockExpressions(method.BodyAST, func(expression ExprNode) {
		if validationErr != nil {
			return
		}
		call, ok := expression.(*CallExpr)
		if !ok {
			return
		}
		selector, ok := call.Callee.(*SelectorExpr)
		if !ok {
			return
		}
		class := classForHookReceiver(selector.Receiver, valueTypes, classes)
		hook, declaringClass := resolveHookMethod(class, selector.Name, len(call.Arguments), classes, map[*ClassDecl]bool{})
		if hook == nil || declaringClass == nil {
			return
		}
		validationErr = diagnosticAt(
			sourceFile,
			completeSpan(call.Span(), sourceLine),
			fmt.Errorf("hook %s may only be called by its declaring class %s", selector.Name, declaringClass.Name),
		)
	})
	return validationErr
}

func collectHookLocalTypes(block *BlockStmt, valueTypes map[string]string) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		if declaration, ok := statement.(*DeclarationStmt); ok {
			typeName := ""
			if declaration.Type != nil {
				typeName, _ = typeNodeSource(declaration.Type)
			} else {
				for index, value := range declaration.Values {
					if index < len(declaration.Names) {
						valueTypes[declaration.Names[index].Text] = inferRecordExprType(value, valueTypes, constructorContext{})
					}
				}
			}
			if typeName != "" {
				for _, name := range declaration.Names {
					valueTypes[name.Text] = typeName
				}
			}
		}
		walkHookNestedBlocks(statement, func(nested *BlockStmt) { collectHookLocalTypes(nested, valueTypes) })
	}
}

func walkHookNestedBlocks(statement Stmt, visit func(*BlockStmt)) {
	if statement == nil || visit == nil {
		return
	}
	switch value := statement.(type) {
	case *TokenStmt:
		visit(value.Body)
		for _, child := range value.Children {
			walkHookNestedBlocks(child, visit)
		}
	case *IfStmt:
		visit(value.Body)
		visit(value.Else)
		if value.ElseIf != nil {
			walkHookNestedBlocks(value.ElseIf, visit)
		}
	case *TryStmt:
		visit(value.Body)
		for _, clause := range value.Catches {
			visit(clause.Body)
		}
		visit(value.Finally)
	case *ForStmt:
		visit(value.Body)
	case *SwitchStmt:
		visit(value.Body)
	case *CaseStmt:
		visit(value.Clause.Body)
	case *BlockStmt:
		for _, child := range value.Statements {
			walkHookNestedBlocks(child, visit)
		}
	}
}

func allContextClasses(context constructorContext) map[string]*ClassDecl {
	classes := make(map[string]*ClassDecl)
	for name, target := range context.Targets {
		if target.Class != nil {
			classes[name] = target.Class
			classes[target.Class.Name] = target.Class
		}
		for childName, class := range target.Classes {
			if class != nil {
				classes[childName] = class
				classes[class.Name] = class
			}
		}
	}
	return classes
}

func classForHookType(typeName string, classes map[string]*ClassDecl) *ClassDecl {
	name := strings.TrimSpace(typeName)
	name = strings.TrimLeft(name, "*&")
	if bracket := strings.IndexByte(name, '['); bracket >= 0 {
		name = name[:bracket]
	}
	if class := classes[name]; class != nil {
		return class
	}
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		if class := classes[name[dot+1:]]; class != nil {
			return class
		}
	}
	return nil
}

func classForHookReceiver(receiver ExprNode, valueTypes map[string]string, classes map[string]*ClassDecl) *ClassDecl {
	switch value := receiver.(type) {
	case *NameExpr:
		if typeName := valueTypes[value.Name]; typeName != "" {
			if class := classForHookType(typeName, classes); class != nil {
				return class
			}
		}
		return classForHookType(value.Name, classes)
	case *SelectorExpr:
		if typeName, ok := expressionTypeName(value); ok {
			if class := classForHookType(typeName, classes); class != nil {
				return class
			}
		}
	case *ParenthesizedExpr:
		return classForHookReceiver(value.Inner, valueTypes, classes)
	case *UnaryExpr:
		return classForHookReceiver(value.Operand, valueTypes, classes)
	}
	return nil
}

func resolveHookMethod(class *ClassDecl, name string, arity int, classes map[string]*ClassDecl, visiting map[*ClassDecl]bool) (*Method, *ClassDecl) {
	if class == nil || visiting[class] {
		return nil, nil
	}
	visiting[class] = true
	var localHook *Method
	for index := range class.Methods {
		method := &class.Methods[index]
		if method.Name == name && !method.IsStatic && hasHookAnnotation(method.Annotations) && len(method.ParameterAST) == arity {
			localHook = method
			break
		}
	}
	for _, parentName := range class.ParentNames() {
		parent := classes[parentName]
		if parent == nil {
			if dot := strings.LastIndexByte(parentName, '.'); dot >= 0 {
				parent = classes[parentName[dot+1:]]
			}
		}
		if method, owner := resolveHookMethod(parent, name, arity, classes, visiting); method != nil {
			return method, owner
		}
	}
	if localHook != nil {
		return localHook, class
	}
	return nil, nil
}

func hasHookAnnotation(annotations []AnnotationUse) bool {
	for _, annotation := range annotations {
		name := strings.TrimPrefix(annotation.Name, "@")
		if name == "std.Hook" || name == "Hook" {
			return true
		}
	}
	return false
}
