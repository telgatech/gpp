package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func fileHasSafeAccess(file *File) bool {
	for _, decl := range file.Decls {
		switch value := decl.(type) {
		case *RawDecl:
			if strings.Contains(value.Code, "?.") {
				return true
			}
		case *ClassDecl:
			for _, method := range value.Methods {
				if strings.Contains(method.Body, "?.") {
					return true
				}
			}
		case *ExtendDecl:
			for _, method := range value.Methods {
				if strings.Contains(method.Body, "?.") {
					return true
				}
			}
		}
	}
	return false
}

func transformSafeAccess(src string, context constructorContext) (string, error) {
	if !strings.Contains(src, "?.") {
		return src, nil
	}

	types := safeValueTypes(src)
	if context.CurrentClass != "" {
		types["this"] = context.CurrentClass
	}

	var out strings.Builder
	for i := 0; i < len(src); {
		if end, ok, err := copyIgnoredSource(src, i, &out); err != nil {
			return "", err
		} else if ok {
			i = end
			continue
		}

		if src[i] != '?' || i+1 >= len(src) || src[i+1] != '.' {
			out.WriteByte(src[i])
			i++
			continue
		}

		leftEnd := i
		for leftEnd > 0 && (src[leftEnd-1] == ' ' || src[leftEnd-1] == '\t') {
			leftEnd--
		}
		leftStart := leftEnd
		for leftStart > 0 && isIdentPart(src[leftStart-1]) {
			leftStart--
		}
		if leftStart == leftEnd {
			return "", fmt.Errorf("safe access requires an identifier receiver")
		}
		receiver := src[leftStart:leftEnd]
		memberStart := i + 2
		for memberStart < len(src) && (src[memberStart] == ' ' || src[memberStart] == '\t') {
			memberStart++
		}
		member, memberLength := readIdent(src[memberStart:])
		if memberLength == 0 {
			return "", fmt.Errorf("safe access requires a member name")
		}

		typeName := types[receiver]
		if typeName == "" && receiver == "this" {
			typeName = context.CurrentClass
		}
		target, ok := safeTargetForType(typeName, context)
		if !ok {
			return "", fmt.Errorf("safe access receiver %s has no known class type", receiver)
		}
		memberType, isMethod, err := safeMemberType(target.Class, target.Classes, member, map[string]bool{})
		if err != nil {
			return "", err
		}
		if memberType == "" {
			return "", fmt.Errorf("class %s has no member %s", target.Class.Name, member)
		}

		end := memberStart + memberLength
		access := receiver + "." + member
		if isMethod {
			open := skipSpace(src, end)
			if open >= len(src) || src[open] != '(' {
				return "", fmt.Errorf("safe method access %s?.%s requires a call", receiver, member)
			}
			close, err := findMatchingParen(src, open)
			if err != nil {
				return "", err
			}
			access += src[end : close+1]
			end = close + 1
		} else if next := skipSpace(src, end); next < len(src) && src[next] == '(' {
			return "", fmt.Errorf("safe field access %s?.%s is not callable", receiver, member)
		}

		if !safeReceiverCanBeNil(typeName, target) {
			return "", fmt.Errorf("safe access receiver %s must be a pointer or interface", receiver)
		}
		prefix := src[leftStart:i]
		previous := out.String()
		if !strings.HasSuffix(previous, prefix) {
			return "", fmt.Errorf("safe access receiver %s could not be resolved", receiver)
		}
		out.Reset()
		out.WriteString(previous[:len(previous)-len(prefix)])
		fmt.Fprintf(&out, "__gopp_safe(%s == nil, func() %s { return %s })", receiver, memberType, access)
		i = end
	}
	return out.String(), nil
}

func safeValueTypes(src string) map[string]string {
	result := map[string]string{}
	sanitized := strings.ReplaceAll(src, "?.", ".")
	parsed, err := parser.ParseFile(token.NewFileSet(), "safe.go", "package main\n\n"+sanitized, 0)
	if err != nil {
		parsed, err = parser.ParseFile(token.NewFileSet(), "safe.go", "package main\n\nfunc __gopp_scope() {\n"+sanitized+"\n}\n", 0)
	}
	if err != nil {
		return result
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncDecl:
			if value.Type.Params != nil {
				for _, field := range value.Type.Params.List {
					typeName, err := formatNode(field.Type)
					if err != nil {
						continue
					}
					for _, name := range field.Names {
						result[name.Name] = strings.Join(strings.Fields(typeName), " ")
					}
				}
			}
		case *ast.ValueSpec:
			typeName := safeASTTypeName(value.Type)
			for index, name := range value.Names {
				inferred := typeName
				if inferred == "" && index < len(value.Values) {
					inferred = astExpressionTypeKey(value.Values[index])
				}
				if inferred != "" {
					result[name.Name] = inferred
				}
			}
		case *ast.AssignStmt:
			for index, left := range value.Lhs {
				name, ok := left.(*ast.Ident)
				if !ok || index >= len(value.Rhs) {
					continue
				}
				if inferred := astExpressionTypeKey(value.Rhs[index]); inferred != "" {
					result[name.Name] = inferred
				}
			}
		}
		return true
	})
	return result
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
	return strings.HasPrefix(name, "*") || name == target.InterfaceName || name == "__gopp_"+target.Class.Name || name == "Gopp"+target.Class.Name
}

func safeMemberType(class *ClassDecl, classes map[string]*ClassDecl, name string, visiting map[string]bool) (string, bool, error) {
	if visiting[class.Name] {
		return "", false, nil
	}
	visiting[class.Name] = true
	defer delete(visiting, class.Name)

	for _, field := range class.Fields {
		if field.Name == name {
			return field.Type, false, nil
		}
	}
	for _, method := range class.Methods {
		if method.Name == name {
			if strings.TrimSpace(method.Result) == "" {
				return "", true, fmt.Errorf("safe method %s must return a value", name)
			}
			return strings.TrimSpace(method.Result), true, nil
		}
	}
	for _, parentName := range class.Parents {
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
