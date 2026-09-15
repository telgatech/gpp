package compiler

import (
	"fmt"
	"strings"
)

type gobSerializationField struct {
	Name      string
	Type      string
	ValueExpr string
}

func emitGeneratedGobSupport(out *strings.Builder, class *ClassDecl, context constructorContext) error {
	if !hasGeneratedSerializationMethod(class, "ToGOB", false) {
		return nil
	}
	fields, err := gobSerializationFields(class, context)
	if err != nil {
		return err
	}
	qualifier := serializationQualifierForClass(class)
	if qualifier == "" {
		qualifier = "encoding"
	} else if qualifier == "." {
		qualifier = ""
	}
	callPrefix := ""
	if qualifier != "" {
		callPrefix = qualifier + "."
	}

	fmt.Fprintf(out, "func (this %s) GobEncode() ([]byte, error) {\n", class.Name)
	fmt.Fprintf(out, "\treturn %sToGOB(struct {\n", callPrefix)
	for _, field := range fields {
		fmt.Fprintf(out, "\t\t%s %s\n", field.Name, field.Type)
	}
	out.WriteString("\t}{\n")
	for _, field := range fields {
		fmt.Fprintf(out, "\t\t%s: %s,\n", field.Name, field.ValueExpr)
	}
	out.WriteString("\t})\n}\n\n")

	fmt.Fprintf(out, "func (this *%s) GobDecode(data []byte) error {\n", class.Name)
	fmt.Fprintf(out, "\tvar value struct {\n")
	for _, field := range fields {
		fmt.Fprintf(out, "\t\t%s %s\n", field.Name, field.Type)
	}
	out.WriteString("\t}\n")
	fmt.Fprintf(out, "\tif err := %sDecodeGOB(data, &value); err != nil {\n", callPrefix)
	out.WriteString("\t\treturn err\n\t}\n")
	for _, field := range fields {
		fmt.Fprintf(out, "\t%s = value.%s\n", field.ValueExpr, field.Name)
	}
	out.WriteString("\treturn nil\n}\n\n")
	return nil
}

func hasGeneratedSerializationMethod(class *ClassDecl, name string, static bool) bool {
	for _, method := range class.Methods {
		if method.Generated && method.Name == name && method.IsStatic == static {
			return true
		}
	}
	return false
}

func serializationQualifierForClass(class *ClassDecl) string {
	for _, use := range class.Annotations {
		if use.Declaration == nil || annotationFullName(use.Declaration) != serializationPackage+".Serializable" {
			continue
		}
		if dot := strings.LastIndex(use.Name, "."); dot >= 0 {
			return use.Name[:dot]
		}
		return "."
	}
	return "encoding"
}

func gobSerializationFields(class *ClassDecl, context constructorContext) ([]gobSerializationField, error) {
	fields, err := constructorFields(class, classesForClass(context, class), nil, map[string]bool{})
	if err != nil {
		return nil, err
	}
	result := []gobSerializationField{}
	used := map[string]bool{}
	for _, field := range fields {
		if !isExportedGoPlusName(field.Name) || hasSerializationIgnore(field.Annotations) {
			continue
		}
		name := gobFieldName(field)
		for used[name] {
			name += "_"
		}
		used[name] = true
		path := append(append([]string{"this"}, field.Path...), field.Name)
		result = append(result, gobSerializationField{
			Name:      name,
			Type:      transformPolymorphicType(field.Type, context),
			ValueExpr: strings.Join(path, "."),
		})
	}
	return result, nil
}

func gobFieldName(field constructorField) string {
	parts := append(append([]string(nil), field.Path...), field.Name)
	name := strings.Join(parts, "_")
	var result strings.Builder
	for _, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' {
			result.WriteRune(character)
		} else {
			result.WriteByte('_')
		}
	}
	return result.String()
}

func hasSerializationIgnore(uses []AnnotationUse) bool {
	for _, use := range uses {
		if serializationAnnotationKind(use) == "Ignore" {
			return true
		}
	}
	return false
}
