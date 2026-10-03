package compiler

import (
	"fmt"
	"go/types"
	"strings"

	templateparse "text/template/parse"
)

// templateValueType describes the parts of a Go type that template actions can
// observe. Unknown values are intentionally left unchecked: interface values,
// dynamic functions, and runtime-loaded data cannot be proven statically.
type templateValueType struct {
	name   string
	class  *ClassDecl
	goType types.Type
	kind   string
	elem   *templateValueType
	key    *templateValueType
	fields map[string]*templateValueType
}

type templateTypeChecker struct {
	template  *TemplateDecl
	context   constructorContext
	templates map[string]*TemplateDecl
}

func validateTypedTemplateBody(template *TemplateDecl, root *templateparse.ListNode, context constructorContext) error {
	if template == nil || root == nil {
		return nil
	}
	checker := &templateTypeChecker{template: template, context: context, templates: context.Templates}
	parameters, err := templateParameterInfos(template)
	if err != nil {
		return err
	}
	var dot *templateValueType
	switch len(parameters) {
	case 0:
		dot = &templateValueType{kind: "unknown"}
	case 1:
		dot = checker.resolveType(parameters[0].TypeAST)
	default:
		fields := map[string]*templateValueType{}
		for _, parameter := range parameters {
			fields[parameter.Name] = checker.resolveType(parameter.TypeAST)
		}
		dot = &templateValueType{kind: "struct", fields: fields}
	}
	vars := map[string]*templateValueType{"$": dot}
	return checker.walkList(root, dot, vars)
}

func (checker *templateTypeChecker) walkList(list *templateparse.ListNode, dot *templateValueType, vars map[string]*templateValueType) error {
	if list == nil {
		return nil
	}
	for _, node := range list.Nodes {
		switch current := node.(type) {
		case *templateparse.ActionNode:
			if _, err := checker.pipelineType(current.Pipe, dot, vars); err != nil {
				return checker.at(current, err)
			}
		case *templateparse.IfNode:
			branchVars := cloneTemplateVars(vars)
			if _, err := checker.pipelineType(current.Pipe, dot, branchVars); err != nil {
				return checker.at(current, err)
			}
			if err := checker.walkList(current.List, dot, branchVars); err != nil {
				return err
			}
			if err := checker.walkList(current.ElseList, dot, cloneTemplateVars(vars)); err != nil {
				return err
			}
		case *templateparse.WithNode:
			inner := cloneTemplateVars(vars)
			value, err := checker.pipelineType(current.Pipe, dot, inner)
			if err != nil {
				return checker.at(current, err)
			}
			if err := checker.walkList(current.List, value, inner); err != nil {
				return err
			}
			if err := checker.walkList(current.ElseList, dot, cloneTemplateVars(vars)); err != nil {
				return err
			}
		case *templateparse.RangeNode:
			inner := cloneTemplateVars(vars)
			value, err := checker.pipelineType(current.Pipe, dot, inner)
			if err != nil {
				return checker.at(current, err)
			}
			element, key := templateRangeTypes(value)
			checker.bindRangeVars(current.Pipe, element, key, inner)
			if err := checker.walkList(current.List, element, inner); err != nil {
				return err
			}
			if err := checker.walkList(current.ElseList, dot, cloneTemplateVars(vars)); err != nil {
				return err
			}
		case *templateparse.TemplateNode:
			if current.Pipe == nil {
				if declaration := checker.templates[current.Name]; declaration != nil {
					parameters, err := templateParameterInfos(declaration)
					if err != nil {
						return err
					}
					if len(parameters) != 0 {
						return checker.at(current, fmt.Errorf("template %s expects %d argument(s), got 0", current.Name, len(parameters)))
					}
				}
			}
			if current.Pipe != nil {
				value, err := checker.pipelineType(current.Pipe, dot, vars)
				if err != nil {
					return checker.at(current, err)
				}
				if declaration := checker.templates[current.Name]; declaration != nil {
					parameters, parameterErr := templateParameterInfos(declaration)
					if parameterErr != nil {
						return parameterErr
					}
					if len(parameters) == 1 {
						if err := checker.checkAssignable(value, checker.resolveType(parameters[0].TypeAST)); err != nil {
							return checker.at(current, fmt.Errorf("template %s: %w", current.Name, err))
						}
					} else if len(parameters) > 1 {
						for _, parameter := range parameters {
							field := value.fields[parameter.Name]
							if field == nil {
								continue // Dynamic/non-struct data remains a runtime check.
							}
							if err := checker.checkAssignable(field, checker.resolveType(parameter.TypeAST)); err != nil {
								return checker.at(current, fmt.Errorf("template %s argument %s: %w", current.Name, parameter.Name, err))
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func cloneTemplateVars(vars map[string]*templateValueType) map[string]*templateValueType {
	clone := make(map[string]*templateValueType, len(vars))
	for name, value := range vars {
		clone[name] = value
	}
	return clone
}

func (checker *templateTypeChecker) bindPipeVars(pipe *templateparse.PipeNode, value *templateValueType, vars map[string]*templateValueType) {
	if pipe == nil {
		return
	}
	for _, variable := range pipe.Decl {
		vars[strings.Join(variable.Ident, ".")] = value
	}
}

func (checker *templateTypeChecker) bindRangeVars(pipe *templateparse.PipeNode, element, key *templateValueType, vars map[string]*templateValueType) {
	if pipe == nil {
		return
	}
	declarations := pipe.Decl
	if len(declarations) == 1 {
		vars[strings.Join(declarations[0].Ident, ".")] = element
	} else if len(declarations) >= 2 {
		vars[strings.Join(declarations[0].Ident, ".")] = key
		vars[strings.Join(declarations[1].Ident, ".")] = element
	}
}

func templateRangeTypes(value *templateValueType) (*templateValueType, *templateValueType) {
	if value == nil {
		return &templateValueType{kind: "unknown"}, &templateValueType{kind: "unknown"}
	}
	for value.kind == "pointer" && value.elem != nil {
		value = value.elem
	}
	switch value.kind {
	case "slice", "array", "channel":
		return value.elem, &templateValueType{kind: "scalar", name: "int"}
	case "map":
		return value.elem, value.key
	case "string":
		return &templateValueType{kind: "scalar", name: "rune"}, &templateValueType{kind: "scalar", name: "int"}
	}
	if value.goType != nil {
		switch underlying := value.goType.Underlying().(type) {
		case *types.Slice:
			return checkerGoType(underlying.Elem()), &templateValueType{kind: "scalar", name: "int"}
		case *types.Array:
			return checkerGoType(underlying.Elem()), &templateValueType{kind: "scalar", name: "int"}
		case *types.Map:
			return checkerGoType(underlying.Elem()), checkerGoType(underlying.Key())
		case *types.Chan:
			return checkerGoType(underlying.Elem()), &templateValueType{kind: "scalar", name: "int"}
		}
	}
	return &templateValueType{kind: "unknown"}, &templateValueType{kind: "unknown"}
}

func (checker *templateTypeChecker) pipelineType(pipe *templateparse.PipeNode, dot *templateValueType, vars map[string]*templateValueType) (*templateValueType, error) {
	if pipe == nil || len(pipe.Cmds) == 0 {
		return &templateValueType{kind: "unknown"}, nil
	}
	var result *templateValueType
	var piped *templateValueType
	for _, command := range pipe.Cmds {
		var err error
		result, err = checker.commandType(command, dot, vars, piped)
		if err != nil {
			return nil, err
		}
		piped = result
	}
	checker.bindPipeVars(pipe, result, vars)
	return result, nil
}

func (checker *templateTypeChecker) commandType(command *templateparse.CommandNode, dot *templateValueType, vars map[string]*templateValueType, piped *templateValueType) (*templateValueType, error) {
	if command == nil || len(command.Args) == 0 {
		return &templateValueType{kind: "unknown"}, nil
	}
	if identifier, ok := command.Args[0].(*templateparse.IdentifierNode); ok {
		if declaration := checker.templates[identifier.Ident]; declaration != nil {
			parameters, err := templateParameterInfos(declaration)
			if err != nil {
				return nil, err
			}
			arguments := command.Args[1:]
			argumentCount := len(arguments)
			if piped != nil {
				argumentCount++
			}
			if argumentCount != len(parameters) {
				return nil, fmt.Errorf("template %s expects %d argument(s), got %d", identifier.Ident, len(parameters), argumentCount)
			}
			for index, parameter := range parameters {
				var actual *templateValueType
				if index < len(arguments) {
					var err error
					actual, err = checker.nodeType(arguments[index], dot, vars)
					if err != nil {
						return nil, err
					}
				} else {
					actual = piped
				}
				if err := checker.checkAssignable(actual, checker.resolveType(parameter.TypeAST)); err != nil {
					return nil, fmt.Errorf("template %s argument %s: %w", identifier.Ident, parameter.Name, err)
				}
			}
			return &templateValueType{kind: "scalar", name: "html/template.HTML"}, nil
		}
		// Built-ins and registered runtime functions may be dynamically typed.
		for _, argument := range command.Args[1:] {
			if _, err := checker.nodeType(argument, dot, vars); err != nil {
				return nil, err
			}
		}
		return &templateValueType{kind: "unknown"}, nil
	}
	if len(command.Args) > 1 || piped != nil {
		if base, names, ok := checker.selectorPath(command.Args[0], dot, vars); ok && len(names) > 0 {
			arguments := make([]*templateValueType, 0, len(command.Args)-1+1)
			for _, argument := range command.Args[1:] {
				argumentType, err := checker.nodeType(argument, dot, vars)
				if err != nil {
					return nil, err
				}
				arguments = append(arguments, argumentType)
			}
			if piped != nil {
				arguments = append(arguments, piped)
			}
			return checker.selectPath(base, names, arguments)
		}
	}
	return checker.nodeType(command.Args[0], dot, vars)
}

func (checker *templateTypeChecker) selectorPath(node templateparse.Node, dot *templateValueType, vars map[string]*templateValueType) (*templateValueType, []string, bool) {
	switch current := node.(type) {
	case *templateparse.FieldNode:
		return dot, current.Ident, true
	case *templateparse.VariableNode:
		if len(current.Ident) == 0 {
			return nil, nil, false
		}
		return vars[current.Ident[0]], current.Ident[1:], true
	case *templateparse.ChainNode:
		base, err := checker.nodeType(current.Node, dot, vars)
		if err != nil {
			return nil, nil, false
		}
		return base, current.Field, true
	default:
		return nil, nil, false
	}
}

func (checker *templateTypeChecker) nodeType(node templateparse.Node, dot *templateValueType, vars map[string]*templateValueType) (*templateValueType, error) {
	switch current := node.(type) {
	case *templateparse.DotNode:
		return dot, nil
	case *templateparse.FieldNode:
		return checker.selectPath(dot, current.Ident, nil)
	case *templateparse.VariableNode:
		name := "$"
		if len(current.Ident) > 0 {
			name = current.Ident[0]
		}
		value := vars[name]
		if value == nil {
			return &templateValueType{kind: "unknown"}, nil
		}
		return checker.selectPath(value, current.Ident[1:], nil)
	case *templateparse.ChainNode:
		value, err := checker.nodeType(current.Node, dot, vars)
		if err != nil {
			return nil, err
		}
		return checker.selectPath(value, current.Field, nil)
	case *templateparse.PipeNode:
		return checker.pipelineType(current, dot, cloneTemplateVars(vars))
	case *templateparse.StringNode:
		return &templateValueType{kind: "string", name: "string"}, nil
	case *templateparse.BoolNode:
		return &templateValueType{kind: "scalar", name: "bool"}, nil
	case *templateparse.NumberNode:
		if current.IsInt {
			return &templateValueType{kind: "scalar", name: "int"}, nil
		}
		if current.IsUint {
			return &templateValueType{kind: "scalar", name: "uint"}, nil
		}
		if current.IsFloat || current.IsComplex {
			return &templateValueType{kind: "scalar", name: "float64"}, nil
		}
		return &templateValueType{kind: "unknown"}, nil
	case *templateparse.NilNode:
		return &templateValueType{kind: "unknown"}, nil
	default:
		return &templateValueType{kind: "unknown"}, nil
	}
}

func (checker *templateTypeChecker) selectPath(value *templateValueType, names []string, explicitArgs []*templateValueType) (*templateValueType, error) {
	var err error
	for index, name := range names {
		value, err = checker.selectMember(value, name, index == len(names)-1, explicitArgs)
		if err != nil {
			return nil, err
		}
	}
	return value, nil
}

func (checker *templateTypeChecker) selectMember(value *templateValueType, name string, final bool, explicitArgs []*templateValueType) (*templateValueType, error) {
	if value == nil || value.kind == "unknown" || value.kind == "interface" {
		return &templateValueType{kind: "unknown"}, nil
	}
	for value.kind == "pointer" && value.elem != nil {
		value = value.elem
	}
	if value.fields != nil {
		if field := value.fields[name]; field != nil {
			if final && len(explicitArgs) > 0 {
				return nil, fmt.Errorf("field %s is not callable", name)
			}
			return field, nil
		}
		if value.goType == nil {
			return nil, fmt.Errorf("%s has no field %s", value.name, name)
		}
	}
	if value.class != nil {
		if field, ok := checker.classField(value.class, name, map[string]bool{}); ok {
			return checker.resolveTypeInClass(field.TypeAST, value.class), nil
		}
		if method, class := checker.classMethod(value.class, name, map[string]bool{}); method != nil {
			parameters, _ := parameterInfosForMethod(*method)
			if !final || len(parameters) != len(explicitArgs) {
				return nil, fmt.Errorf("method %s.%s expects %d argument(s), got %d", class.Name, name, len(parameters), len(explicitArgs))
			}
			for index, parameter := range parameters {
				if err := checker.checkAssignable(explicitArgs[index], checker.resolveTypeInClass(parameter.TypeAST, class)); err != nil {
					return nil, fmt.Errorf("method %s.%s argument %s: %w", class.Name, name, parameter.Name, err)
				}
			}
			return checker.resolveTypeInClass(methodResultTypeNode(*method), class), nil
		}
		return nil, fmt.Errorf("%s has no field or method %s", value.class.Name, name)
	}
	if value.goType != nil {
		if fieldType, ok := nativeTemplateField(value.goType, name); ok {
			if final && len(explicitArgs) > 0 {
				return nil, fmt.Errorf("field %s is not callable", name)
			}
			return checkerGoType(fieldType), nil
		}
		if method, ok := nativeTemplateMethod(value.goType, name); ok {
			if !final || method.Params().Len() != len(explicitArgs) {
				return nil, fmt.Errorf("method %s expects %d argument(s), got %d", name, method.Params().Len(), len(explicitArgs))
			}
			for index, argument := range explicitArgs {
				if err := checker.checkAssignable(argument, checkerGoType(method.Params().At(index).Type())); err != nil {
					return nil, fmt.Errorf("method %s argument %d: %w", name, index+1, err)
				}
			}
			if method.Results() == nil || method.Results().Len() == 0 {
				return &templateValueType{kind: "unknown"}, nil
			}
			return checkerGoType(method.Results().At(0).Type()), nil
		}
		return nil, fmt.Errorf("%s has no exported field or method %s", value.name, name)
	}
	if value.kind == "map" && value.key != nil && value.key.name == "string" {
		return value.elem, nil
	}
	if value.kind == "scalar" || value.kind == "string" || value.kind == "bool" {
		return nil, fmt.Errorf("%s has no field or method %s", value.name, name)
	}
	return &templateValueType{kind: "unknown"}, nil
}

func (checker *templateTypeChecker) classField(class *ClassDecl, name string, seen map[string]bool) (*Field, bool) {
	if class == nil || seen[class.Name] {
		return nil, false
	}
	seen[class.Name] = true
	for index := range class.Fields {
		if class.Fields[index].Name == name {
			return &class.Fields[index], true
		}
	}
	for _, parentName := range class.ParentNames() {
		if parent := checker.classInScope(class, parentName); parent != nil {
			if field, ok := checker.classField(parent, name, seen); ok {
				return field, true
			}
		}
	}
	return nil, false
}

func (checker *templateTypeChecker) classMethod(class *ClassDecl, name string, seen map[string]bool) (*Method, *ClassDecl) {
	if class == nil || seen[class.Name] {
		return nil, nil
	}
	seen[class.Name] = true
	for index := range class.Methods {
		if class.Methods[index].Name == name && !class.Methods[index].IsStatic {
			return &class.Methods[index], class
		}
	}
	for _, parentName := range class.ParentNames() {
		if parent := checker.classInScope(class, parentName); parent != nil {
			if method, owner := checker.classMethod(parent, name, seen); method != nil {
				return method, owner
			}
		}
	}
	return nil, nil
}

func (checker *templateTypeChecker) classInScope(owner *ClassDecl, name string) *ClassDecl {
	if target, ok := checker.context.Targets[name]; ok && target.Class != nil {
		return target.Class
	}
	for _, target := range checker.context.Targets {
		if target.Class != owner {
			continue
		}
		if class := target.Classes[name]; class != nil {
			return class
		}
	}
	return nil
}

func (checker *templateTypeChecker) resolveTypeInClass(node TypeNode, owner *ClassDecl) *templateValueType {
	switch current := node.(type) {
	case *PointerType:
		return &templateValueType{kind: "pointer", name: "*" + typeNodeText(current.Element), elem: checker.resolveTypeInClass(current.Element, owner)}
	case *SliceType:
		return &templateValueType{kind: "slice", name: "[]" + typeNodeText(current.Element), elem: checker.resolveTypeInClass(current.Element, owner)}
	case *ArrayType:
		return &templateValueType{kind: "array", name: typeNodeText(current), elem: checker.resolveTypeInClass(current.Element, owner)}
	case *MapType:
		return &templateValueType{kind: "map", name: typeNodeText(current), key: checker.resolveTypeInClass(current.Key, owner), elem: checker.resolveTypeInClass(current.Value, owner)}
	case *NamedType:
		if _, ok := checker.context.Targets[typeNodeText(current)]; ok {
			return checker.resolveType(node)
		}
		if class := checker.classInScope(owner, typeNodeText(current)); class != nil {
			return &templateValueType{kind: "class", name: class.Name, class: class}
		}
	}
	return checker.resolveType(node)
}

func nativeTemplateField(value types.Type, name string) (types.Type, bool) {
	if pointer, ok := value.(*types.Pointer); ok {
		value = pointer.Elem()
	}
	structure, ok := value.Underlying().(*types.Struct)
	if !ok {
		return nil, false
	}
	for index := 0; index < structure.NumFields(); index++ {
		field := structure.Field(index)
		if field.Name() == name && field.Exported() {
			return field.Type(), true
		}
	}
	return nil, false
}

func nativeTemplateMethod(value types.Type, name string) (*types.Signature, bool) {
	sets := []types.Type{value}
	if _, ok := value.(*types.Pointer); !ok {
		sets = append(sets, types.NewPointer(value))
	}
	for _, candidate := range sets {
		method := types.NewMethodSet(candidate).Lookup(nil, name)
		if method == nil {
			continue
		}
		if signature, ok := method.Type().(*types.Signature); ok {
			return signature, true
		}
	}
	return nil, false
}

func checkerGoType(value types.Type) *templateValueType {
	if value == nil {
		return &templateValueType{kind: "unknown"}
	}
	switch underlying := value.Underlying().(type) {
	case *types.Pointer:
		return &templateValueType{kind: "pointer", name: types.TypeString(value, nil), elem: checkerGoType(underlying.Elem()), goType: value}
	case *types.Slice:
		return &templateValueType{kind: "slice", name: types.TypeString(value, nil), elem: checkerGoType(underlying.Elem()), goType: value}
	case *types.Array:
		return &templateValueType{kind: "array", name: types.TypeString(value, nil), elem: checkerGoType(underlying.Elem()), goType: value}
	case *types.Map:
		return &templateValueType{kind: "map", name: types.TypeString(value, nil), key: checkerGoType(underlying.Key()), elem: checkerGoType(underlying.Elem()), goType: value}
	case *types.Chan:
		return &templateValueType{kind: "channel", name: types.TypeString(value, nil), elem: checkerGoType(underlying.Elem()), goType: value}
	case *types.Interface:
		return &templateValueType{kind: "interface", name: types.TypeString(value, nil), goType: value}
	case *types.Struct:
		fields := map[string]*templateValueType{}
		for index := 0; index < underlying.NumFields(); index++ {
			field := underlying.Field(index)
			if field.Exported() {
				fields[field.Name()] = checkerGoType(field.Type())
			}
		}
		return &templateValueType{kind: "struct", name: types.TypeString(value, nil), fields: fields, goType: value}
	default:
		return &templateValueType{kind: "scalar", name: types.TypeString(value, nil), goType: value}
	}
}

func (checker *templateTypeChecker) resolveType(node TypeNode) *templateValueType {
	switch current := node.(type) {
	case *PointerType:
		return &templateValueType{kind: "pointer", name: "*" + typeNodeText(current.Element), elem: checker.resolveType(current.Element)}
	case *SliceType:
		return &templateValueType{kind: "slice", name: "[]" + typeNodeText(current.Element), elem: checker.resolveType(current.Element)}
	case *ArrayType:
		return &templateValueType{kind: "array", name: typeNodeText(current), elem: checker.resolveType(current.Element)}
	case *MapType:
		return &templateValueType{kind: "map", name: typeNodeText(current), key: checker.resolveType(current.Key), elem: checker.resolveType(current.Value)}
	case *StructType:
		fields := map[string]*templateValueType{}
		for _, field := range current.Fields {
			for _, name := range field.Names {
				fields[name] = checker.resolveType(field.Type)
			}
		}
		return &templateValueType{kind: "struct", name: "struct", fields: fields}
	case *InterfaceType:
		return &templateValueType{kind: "interface", name: typeNodeText(current)}
	case *NamedType:
		name := typeNodeText(current)
		if strings.HasPrefix(name, "[]") {
			return &templateValueType{kind: "slice", name: name, elem: checker.resolveType(parseTypeText(strings.TrimPrefix(name, "[]")))}
		}
		if target, ok := checker.context.Targets[name]; ok && target.Class != nil {
			return &templateValueType{kind: "class", name: name, class: target.Class}
		}
		if checker.context.Enums[name] != nil {
			return &templateValueType{kind: "scalar", name: name}
		}
		if strings.HasPrefix(name, "map[") {
			if close := strings.IndexByte(name, ']'); close > 4 {
				return &templateValueType{kind: "map", name: name, key: checker.resolveType(parseTypeText(name[4:close])), elem: checker.resolveType(parseTypeText(name[close+1:]))}
			}
		}
		if name == "any" || strings.HasPrefix(name, "interface{") {
			return &templateValueType{kind: "interface", name: name}
		}
		if native, ok := nativeGoType(name, checker.context); ok {
			return checkerGoType(native)
		}
		if name == "string" {
			return &templateValueType{kind: "string", name: name}
		}
		if name == "bool" {
			return &templateValueType{kind: "bool", name: name}
		}
		if basic := types.Universe.Lookup(name); basic != nil {
			if _, ok := basic.Type().(*types.Basic); ok {
				return &templateValueType{kind: "scalar", name: name, goType: basic.Type()}
			}
		}
		return &templateValueType{kind: "unknown", name: name}
	default:
		return &templateValueType{kind: "unknown"}
	}
}

func typeNodeText(node TypeNode) string {
	if node == nil {
		return ""
	}
	text, err := typeNodeSource(node)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

func (checker *templateTypeChecker) checkAssignable(actual, expected *templateValueType) error {
	if actual == nil || expected == nil || actual.kind == "unknown" || expected.kind == "unknown" || actual.kind == "interface" || expected.kind == "interface" {
		return nil
	}
	if actual.name == expected.name || strings.TrimPrefix(actual.name, "*") == strings.TrimPrefix(expected.name, "*") {
		return nil
	}
	if actual.class != nil && expected.class != nil && checker.classAssignable(actual.class, expected.class, map[string]bool{}) {
		return nil
	}
	if actual.goType != nil && expected.goType != nil && types.AssignableTo(actual.goType, expected.goType) {
		return nil
	}
	return fmt.Errorf("cannot use %s as %s", printableTemplateType(actual), printableTemplateType(expected))
}

func (checker *templateTypeChecker) classAssignable(actual, expected *ClassDecl, seen map[string]bool) bool {
	if actual == expected {
		return true
	}
	if actual == nil || seen[actual.Name] {
		return false
	}
	seen[actual.Name] = true
	for _, parentName := range actual.ParentNames() {
		parent := checker.classInScope(actual, parentName)
		if parent == expected || checker.classAssignable(parent, expected, seen) {
			return true
		}
	}
	return false
}

func printableTemplateType(value *templateValueType) string {
	if value == nil || value.name == "" {
		return "unknown"
	}
	return value.name
}

func (checker *templateTypeChecker) at(node templateparse.Node, err error) error {
	if err == nil || node == nil {
		return err
	}
	position := int(node.Position())
	if position < 0 {
		position = 0
	}
	if position > len(checker.template.Body) {
		position = len(checker.template.Body)
	}
	line := checker.template.BodySpan.Line + strings.Count(checker.template.Body[:position], "\n")
	if line < 1 {
		line = checker.template.SourceLine
	}
	return sourceLineError(Span{Line: line}, err)
}
