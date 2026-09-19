package compiler

import (
	"fmt"
	"strings"
)

const serializationPackage = "gpp.encoding"

// expandSerializableClasses materializes the methods promised by the
// encoding.Serializable annotation. Keeping the expansion in the AST makes
// the methods visible to normal semantic resolution, calls, and metadata.
func expandSerializableClasses(program *Program, model *SemanticModel, modulePath string) error {
	for _, file := range program.Files {
		pkg := model.Packages[file.Package]
		if pkg == nil {
			continue
		}
		scope, err := annotationScopeForFile(file, model, modulePath)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			class, ok := declaration.(*ClassDecl)
			if !ok {
				continue
			}
			qualifier, ok := serializationQualifier(class.Annotations, scope)
			if !ok {
				continue
			}
			if err := expandSerializableClass(class, qualifier); err != nil {
				return err
			}
		}
	}
	return nil
}

func serializationQualifier(uses []AnnotationUse, scope map[string]*AnnotationDecl) (string, bool) {
	for _, use := range uses {
		declaration := use.Declaration
		if declaration == nil {
			declaration = scope[use.Name]
		}
		if declaration != nil && annotationFullName(declaration) == serializationPackage+".Serializable" {
			if dot := strings.LastIndex(use.Name, "."); dot >= 0 {
				return use.Name[:dot], true
			}
			return "", true
		}
		if use.Name == "encoding.Serializable" {
			return "encoding", true
		}
		if use.Name == serializationPackage+".Serializable" {
			return "encoding", true
		}
	}
	return "", false
}

func expandSerializableClass(class *ClassDecl, qualifier string) error {
	callPrefix := ""
	if qualifier != "" {
		callPrefix = qualifier + "."
	}
	methods := []struct {
		name          string
		static        bool
		parameters    string
		result        string
		parametersAST []ParameterNode
		resultAST     TypeNode
		body          func() ExprNode
	}{
		{name: "ToJSON", result: "([]byte, error)", resultAST: serializationBytesErrorType(), body: func() ExprNode { return serializationCall(callPrefix+"ToJSON", &NameExpr{Name: "this"}) }},
		{name: "ToYAML", result: "([]byte, error)", resultAST: serializationBytesErrorType(), body: func() ExprNode { return serializationCall(callPrefix+"ToYAML", &NameExpr{Name: "this"}) }},
		{name: "FromJSON", static: true, parameters: "data []byte", parametersAST: serializationDataParameter(), result: "(" + class.Name + ", error)", resultAST: serializationValueErrorType(class.Name), body: func() ExprNode { return serializationGenericCall(callPrefix+"FromJSON", class.Name) }},
		{name: "FromYAML", static: true, parameters: "data []byte", parametersAST: serializationDataParameter(), result: "(" + class.Name + ", error)", resultAST: serializationValueErrorType(class.Name), body: func() ExprNode { return serializationGenericCall(callPrefix+"FromYAML", class.Name) }},
		{name: "ToGOB", result: "([]byte, error)", resultAST: serializationBytesErrorType(), body: func() ExprNode { return serializationCall(callPrefix+"ToGOB", &NameExpr{Name: "this"}) }},
		{name: "FromGOB", static: true, parameters: "data []byte", parametersAST: serializationDataParameter(), result: "(" + class.Name + ", error)", resultAST: serializationValueErrorType(class.Name), body: func() ExprNode { return serializationGenericCall(callPrefix+"FromGOB", class.Name) }},
	}

	for _, generated := range methods {
		found := false
		for _, existing := range class.Methods {
			if existing.Name != generated.name || existing.IsStatic != generated.static {
				continue
			}
			if !sameGeneratedParameters(methodParametersSource(existing), generated.parameters) {
				continue
			}
			found = true
			if !compatibleGeneratedMethod(existing, generated.parameters, generated.result) {
				kind := "method"
				if generated.static {
					kind = "static method"
				}
				return fmt.Errorf(
					"%s: encoding.Serializable requires %s.%s%s, but %s defines an incompatible %s",
					classLocation(class), class.Name, generated.name, generatedSignature(generated.parameters, generated.result), class.Name, kind,
				)
			}
			break
		}
		if found {
			continue
		}
		bodyExpression := generated.body()
		bodyAST := &BlockStmt{Statements: []Stmt{&ReturnStmt{Values: []ExprNode{bodyExpression}}}}
		class.Methods = append(class.Methods, Method{
			Name:         generated.name,
			IsStatic:     generated.static,
			Generated:    true,
			ParameterAST: generated.parametersAST,
			ResultAST:    generated.resultAST,
			BodyAST:      bodyAST,
		})
	}
	return nil
}

func serializationNamed(name string) TypeNode {
	parts := strings.Split(name, ".")
	return &NamedType{Parts: parts}
}

func serializationBytesErrorType() TypeNode {
	return &TupleType{Elements: []TypeNode{
		&SliceType{Element: serializationNamed("byte")},
		serializationNamed("error"),
	}}
}

func serializationValueErrorType(name string) TypeNode {
	return &TupleType{Elements: []TypeNode{serializationNamed(name), serializationNamed("error")}}
}

func serializationDataParameter() []ParameterNode {
	return []ParameterNode{{Name: "data", Type: &SliceType{Element: serializationNamed("byte")}}}
}

func serializationQualifiedName(name string) ExprNode {
	parts := strings.Split(name, ".")
	var expression ExprNode = &NameExpr{Name: parts[0]}
	for _, part := range parts[1:] {
		expression = &SelectorExpr{Receiver: expression, Name: part}
	}
	return expression
}

func serializationCall(name string, argument ExprNode) ExprNode {
	return &CallExpr{Callee: serializationQualifiedName(name), Arguments: []CallArg{{Value: argument}}}
}

func serializationGenericCall(name, typeName string) ExprNode {
	return &CallExpr{
		Callee:    &IndexExpr{Receiver: serializationQualifiedName(name), Index: &TypeExpr{Type: serializationNamed(typeName)}},
		Arguments: []CallArg{{Value: &NameExpr{Name: "data"}}},
	}
}

func sameGeneratedParameters(actual, expected string) bool {
	actualParameters, err := parseParameterInfos(actual)
	if err != nil {
		return false
	}
	expectedParameters, err := parseParameterInfos(expected)
	if err != nil || len(actualParameters) != len(expectedParameters) {
		return false
	}
	for index := range actualParameters {
		if strings.Join(strings.Fields(actualParameters[index].typeText()), " ") != strings.Join(strings.Fields(expectedParameters[index].typeText()), " ") {
			return false
		}
	}
	return true
}

func compatibleGeneratedMethod(method Method, parameters, result string) bool {
	actualParameters, err := parameterInfosForMethod(method)
	if err != nil {
		return false
	}
	expectedParameters, err := parseParameterInfos(parameters)
	if err != nil || len(actualParameters) != len(expectedParameters) {
		return false
	}
	for index := range actualParameters {
		if strings.Join(strings.Fields(actualParameters[index].typeText()), " ") != strings.Join(strings.Fields(expectedParameters[index].typeText()), " ") {
			return false
		}
	}
	return normalizeGeneratedType(methodResultSource(method)) == normalizeGeneratedType(result)
}

func normalizeGeneratedType(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func generatedSignature(parameters, result string) string {
	return "(" + parameters + ") " + result
}
