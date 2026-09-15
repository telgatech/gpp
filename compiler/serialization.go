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
		name       string
		static     bool
		parameters string
		result     string
		body       string
	}{
		{name: "ToJSON", result: "([]byte, error)", body: "return " + callPrefix + "ToJSON(this)"},
		{name: "ToYAML", result: "([]byte, error)", body: "return " + callPrefix + "ToYAML(this)"},
		{name: "FromJSON", static: true, parameters: "data []byte", result: "(" + class.Name + ", error)", body: "return " + callPrefix + "FromJSON[" + class.Name + "](data)"},
		{name: "FromYAML", static: true, parameters: "data []byte", result: "(" + class.Name + ", error)", body: "return " + callPrefix + "FromYAML[" + class.Name + "](data)"},
		{name: "ToGOB", result: "([]byte, error)", body: "return " + callPrefix + "ToGOB(this)"},
		{name: "FromGOB", static: true, parameters: "data []byte", result: "(" + class.Name + ", error)", body: "return " + callPrefix + "FromGOB[" + class.Name + "](data)"},
	}

	for _, generated := range methods {
		found := false
		for _, existing := range class.Methods {
			if existing.Name != generated.name || existing.IsStatic != generated.static {
				continue
			}
			if !sameGeneratedParameters(existing.Parameters, generated.parameters) {
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
		class.Methods = append(class.Methods, Method{
			Name:       generated.name,
			IsStatic:   generated.static,
			Generated:  true,
			Parameters: generated.parameters,
			Result:     generated.result,
			Body:       generated.body,
		})
	}
	return nil
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
		if strings.Join(strings.Fields(actualParameters[index].Type), " ") != strings.Join(strings.Fields(expectedParameters[index].Type), " ") {
			return false
		}
	}
	return true
}

func compatibleGeneratedMethod(method Method, parameters, result string) bool {
	actualParameters, err := parseParameterInfos(method.Parameters)
	if err != nil {
		return false
	}
	expectedParameters, err := parseParameterInfos(parameters)
	if err != nil || len(actualParameters) != len(expectedParameters) {
		return false
	}
	for index := range actualParameters {
		if strings.Join(strings.Fields(actualParameters[index].Type), " ") != strings.Join(strings.Fields(expectedParameters[index].Type), " ") {
			return false
		}
	}
	return normalizeGeneratedType(method.Result) == normalizeGeneratedType(result)
}

func normalizeGeneratedType(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func generatedSignature(parameters, result string) string {
	return "(" + parameters + ") " + result
}
