package compiler

import (
	"fmt"
)

// bindStaticSelfTypes makes self an implicit generic type parameter on static
// methods that mention it in their signature. Calls through a subclass bind
// that parameter to the class used at the call site.
func bindStaticSelfTypes(program *Program) error {
	for _, file := range program.Files {
		for _, declaration := range file.Decls {
			class, ok := declaration.(*ClassDecl)
			if !ok {
				continue
			}
			for index := range class.Methods {
				method := &class.Methods[index]
				if !method.IsStatic {
					continue
				}
				usesSelf, err := methodSignatureUsesType(method, "self")
				if err != nil {
					return fmt.Errorf("%s: %w", classLocation(class), err)
				}
				if !usesSelf {
					continue
				}
				method.ContextualSelf = true
				if hasTypeParameter(method.TypeParamsAST, "self") {
					continue
				}
				method.TypeParamsAST = append(method.TypeParamsAST, TypeParameterNode{
					Name:       "self",
					Constraint: &NamedType{Parts: []string{"any"}},
				})
			}
		}
	}
	return nil
}

func methodSignatureUsesType(method *Method, name string) (bool, error) {
	if method == nil {
		return false, nil
	}
	types := make([]TypeNode, 0, len(method.ParameterAST)+1)
	for _, parameter := range method.ParameterAST {
		types = append(types, parameter.Type)
	}
	types = append(types, methodResultTypeNode(*method))
	for _, typeNode := range types {
		if typeNode == nil {
			continue
		}
		source, err := typeNodeSource(typeNode)
		if err != nil {
			return false, err
		}
		tokens, err := LexSource("static self type", source)
		if err != nil {
			return false, err
		}
		for index, token := range tokens {
			if token.Kind != TokenIdentifier || token.Text != name {
				continue
			}
			if index > 0 && tokens[index-1].Text == "." {
				continue
			}
			return true, nil
		}
	}
	return false, nil
}

func hasTypeParameter(parameters []TypeParameterNode, name string) bool {
	for _, parameter := range parameters {
		if parameter.Name == name {
			return true
		}
	}
	return false
}
