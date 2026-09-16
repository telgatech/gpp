package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

var enumBackingTypes = map[string]bool{
	"string": true,
	"int":    true,
	"int8":   true,
	"int16":  true,
	"int32":  true,
	"int64":  true,
	"uint":   true,
	"uint8":  true,
	"uint16": true,
	"uint32": true,
	"uint64": true,
}

func parseEnums(src string, start int) ([]*EnumDecl, int, error) {
	pos := skipSpace(src, start+len("enum"))
	if pos < len(src) && src[pos] == '(' {
		close, err := findMatchingParen(src, pos)
		if err != nil {
			return nil, 0, err
		}
		enums := []*EnumDecl{}
		for cursor := skipSpace(src, pos+1); cursor < close; cursor = skipSpace(src, cursor) {
			enum, next, err := parseEnumDeclaration(src, cursor)
			if err != nil {
				return nil, 0, err
			}
			enums = append(enums, enum)
			cursor = next
		}
		if len(enums) == 0 {
			return nil, 0, fmt.Errorf("enum group cannot be empty")
		}
		return enums, close + 1, nil
	}
	enum, end, err := parseEnumDeclaration(src, pos)
	if err != nil {
		return nil, 0, err
	}
	return []*EnumDecl{enum}, end, nil
}

func parseEnumDeclaration(src string, start int) (*EnumDecl, int, error) {
	name, length := readIdent(src[start:])
	if length == 0 {
		return nil, 0, fmt.Errorf("enum requires a name")
	}
	pos := skipSpace(src, start+length)
	backingType, typeLength := readIdent(src[pos:])
	if typeLength == 0 || !enumBackingTypes[backingType] {
		return nil, 0, fmt.Errorf("enum %s requires a supported scalar backing type", name)
	}
	pos = skipSpace(src, pos+typeLength)
	if pos >= len(src) || src[pos] != '{' {
		return nil, 0, fmt.Errorf("enum %s requires {", name)
	}
	close, err := findMatchingBrace(src, pos)
	if err != nil {
		return nil, 0, err
	}
	enum := &EnumDecl{Name: name, BackingType: backingType}
	if err := parseEnumMembers(enum, src[pos+1:close]); err != nil {
		return nil, 0, err
	}
	return enum, close + 1, nil
}

func parseEnumMembers(enum *EnumDecl, body string) error {
	seenNames := map[string]bool{}
	seenValues := map[string]string{}
	integerNext := int64(0)
	unsignedNext := uint64(0)
	for pos := 0; pos < len(body); {
		end := lineEnd(body, pos)
		line := strings.TrimSpace(enumLineWithoutComment(body[pos:end]))
		pos = end
		if line == "" {
			continue
		}
		name, length := readIdent(line)
		if length == 0 {
			return fmt.Errorf("enum %s has invalid member declaration %q", enum.Name, line)
		}
		if seenNames[name] {
			return fmt.Errorf("enum %s declares member %s more than once", enum.Name, name)
		}
		seenNames[name] = true
		rest := strings.TrimSpace(line[length:])
		valueSource := ""
		if rest == "" {
			switch enum.BackingType {
			case "string":
				valueSource = strconv.Quote(name)
			case "int", "int8", "int16", "int32", "int64":
				valueSource = strconv.FormatInt(integerNext, 10)
				integerNext++
			case "uint", "uint8", "uint16", "uint32", "uint64":
				valueSource = strconv.FormatUint(unsignedNext, 10)
				unsignedNext++
			}
		} else {
			if !strings.HasPrefix(rest, "=") {
				return fmt.Errorf("enum member %s has invalid value syntax", name)
			}
			valueSource = strings.TrimSpace(strings.TrimPrefix(rest, "="))
			if valueSource == "" {
				return fmt.Errorf("enum member %s requires a value after =", name)
			}
		}
		canonical, nextInt, nextUint, err := normalizeEnumValue(enum.BackingType, valueSource)
		if err != nil {
			return fmt.Errorf("enum member %s: %w", name, err)
		}
		if previous, exists := seenValues[canonical]; exists {
			return fmt.Errorf("duplicate enum value %s for members %s and %s", canonical, previous, name)
		}
		seenValues[canonical] = name
		if enum.BackingType == "string" {
			// String members do not have a sequence to reset or advance.
		} else if strings.HasPrefix(enum.BackingType, "u") || enum.BackingType == "uint" {
			unsignedNext = nextUint
		} else {
			integerNext = nextInt
		}
		enum.Members = append(enum.Members, EnumMember{Name: name, Value: canonical})
	}
	if len(enum.Members) == 0 {
		return fmt.Errorf("enum %s cannot be empty", enum.Name)
	}
	return nil
}

func enumLineWithoutComment(line string) string {
	for index := 0; index+1 < len(line); index++ {
		if line[index] != '/' || line[index+1] != '/' {
			continue
		}
		quoted := false
		escaped := false
		for cursor := 0; cursor < index; cursor++ {
			switch line[cursor] {
			case '\\':
				escaped = !escaped
			case '"':
				if !escaped {
					quoted = !quoted
				}
				escaped = false
			default:
				escaped = false
			}
		}
		if !quoted {
			return line[:index]
		}
	}
	return line
}

func normalizeEnumValue(backingType, source string) (string, int64, uint64, error) {
	source = strings.TrimSpace(source)
	if backingType == "string" {
		value, err := strconv.Unquote(source)
		if err != nil {
			return "", 0, 0, fmt.Errorf("value %q is not a string literal; expected string", source)
		}
		return strconv.Quote(value), 0, 0, nil
	}
	negative := strings.HasPrefix(source, "-")
	literalSource := source
	if negative {
		literalSource = strings.TrimSpace(strings.TrimPrefix(source, "-"))
	}
	literal := constant.MakeFromLiteral(literalSource, token.INT, 0)
	if literal.Kind() != constant.Int {
		return "", 0, 0, fmt.Errorf("value %q is not an integer; expected %s", source, backingType)
	}
	if negative {
		literal = constant.UnaryOp(token.SUB, literal, 0)
	}
	if strings.HasPrefix(backingType, "u") || backingType == "uint" {
		if constant.Sign(literal) < 0 {
			return "", 0, 0, fmt.Errorf("value %q is negative; expected %s", source, backingType)
		}
		value, ok := constant.Uint64Val(literal)
		if !ok || value > enumUnsignedLimit(backingType) {
			return "", 0, 0, fmt.Errorf("value %q overflows %s", source, backingType)
		}
		return strconv.FormatUint(value, 10), 0, value + 1, nil
	}
	value, ok := constant.Int64Val(literal)
	if !ok || value < enumSignedMin(backingType) || value > enumSignedMax(backingType) {
		return "", 0, 0, fmt.Errorf("value %q overflows %s", source, backingType)
	}
	return strconv.FormatInt(value, 10), value + 1, 0, nil
}

func enumSignedMin(backingType string) int64 {
	bits := enumBitSize(backingType)
	if bits == 64 {
		return -1 << 63
	}
	return -1 << (bits - 1)
}

func enumSignedMax(backingType string) int64 {
	bits := enumBitSize(backingType)
	if bits == 64 {
		return 1<<63 - 1
	}
	return 1<<(bits-1) - 1
}

func enumUnsignedLimit(backingType string) uint64 {
	bits := enumBitSize(backingType)
	if bits == 64 {
		return ^uint64(0)
	}
	return 1<<bits - 1
}

func enumBitSize(backingType string) uint {
	switch backingType {
	case "int8", "uint8":
		return 8
	case "int16", "uint16":
		return 16
	case "int32", "uint32":
		return 32
	default:
		return 64
	}
}

func configureEnumSignatures(context *constructorContext) {
	if context.FunctionSignatures == nil {
		context.FunctionSignatures = map[string][]callableSignature{}
	}
	for name, enum := range context.Enums {
		if strings.Contains(name, ".") {
			continue
		}
		context.FunctionSignatures[enumFromName(enum)] = []callableSignature{{
			Name:       enumFromName(enum),
			Parameters: []parameterInfo{{Name: "value", Type: enum.BackingType}},
			Result:     "(" + enum.Name + ", error)",
		}}
	}
}

func configureImportedEnums(context *constructorContext, file *File, model *SemanticModel, modulePath string) {
	imports, err := goImports(file)
	if err != nil {
		return
	}
	for _, spec := range imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		logicalPackage, ok := logicalPackageForImport(importPath, modulePath, model)
		if !ok {
			continue
		}
		pkg := model.Packages[logicalPackage]
		if pkg == nil {
			continue
		}
		alias := path.Base(importPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias == "_" {
			continue
		}
		for name, enum := range pkg.Enums {
			key := alias + "." + name
			if alias == "." {
				key = name
			}
			context.Enums[key] = enum
		}
	}
}

func enumFromName(enum *EnumDecl) string {
	return "GppEnum_" + enum.Name + "From"
}

func enumMemberName(enum *EnumDecl, member EnumMember) string {
	return "GppEnum_" + enum.Name + "_" + member.Name
}

func enumMemberMetaName(enum *EnumDecl, member EnumMember) string {
	return enumMemberName(enum, member) + "Meta"
}

func enumValuesName(enum *EnumDecl) string {
	return "GppEnum_" + enum.Name + "Values"
}

func enumMetaTypeName(enum *EnumDecl) string {
	return "GppEnumValue_" + enum.Name
}

func enumResultTypes(result string) []string {
	result = strings.TrimSpace(result)
	if !strings.HasPrefix(result, "(") || !strings.HasSuffix(result, ")") {
		if result == "" {
			return nil
		}
		return []string{result}
	}
	parts := strings.Split(strings.TrimSpace(result[1:len(result)-1]), ",")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

// emitEnum lowers an enum to a named Go scalar type, exported constants, and
// the small amount of metadata needed by the Go++ enum selectors.
func emitEnum(out *strings.Builder, enum *EnumDecl, errorAlias, jsonAlias, driverAlias string) {
	fmt.Fprintf(out, "type %s %s\n\n", enum.Name, enum.BackingType)
	fmt.Fprintln(out, "const (")
	for _, member := range enum.Members {
		fmt.Fprintf(out, "\t%s %s = %s\n", enumMemberName(enum, member), enum.Name, member.Value)
	}
	fmt.Fprint(out, ")\n\n")

	fmt.Fprintf(out, "type %s struct { Name string; Value %s }\n\n", enumMetaTypeName(enum), enum.BackingType)
	for _, member := range enum.Members {
		fmt.Fprintf(out, "var %s = %s{Name: %q, Value: %s}\n", enumMemberMetaName(enum, member), enumMetaTypeName(enum), member.Name, member.Value)
	}
	fmt.Fprintf(out, "var %s = []%s{\n", enumValuesName(enum), enumMetaTypeName(enum))
	for _, member := range enum.Members {
		fmt.Fprintf(out, "\t%s,\n", enumMemberMetaName(enum, member))
	}
	fmt.Fprint(out, "}\n\n")

	fmt.Fprintf(out, "func %s(value %s) (%s, error) {\n", enumFromName(enum), enum.BackingType, enum.Name)
	fmt.Fprintln(out, "\tswitch value {")
	for _, member := range enum.Members {
		fmt.Fprintf(out, "\tcase %s:\n\t\treturn %s, nil\n", member.Value, enumMemberName(enum, member))
	}
	fmt.Fprintf(out, "\tdefault:\n\t\tvar zero %s\n\t\treturn zero, %s.New(%q)\n", enum.Name, errorAlias, "invalid "+enum.Name+" value")
	fmt.Fprint(out, "\t}\n}\n\n")

	fmt.Fprintf(out, "func GppEnum_%sName(value %s) string {\n", enum.Name, enum.Name)
	fmt.Fprintln(out, "\tswitch value {")
	for _, member := range enum.Members {
		fmt.Fprintf(out, "\tcase %s:\n\t\treturn %q\n", enumMemberName(enum, member), member.Name)
	}
	fmt.Fprint(out, "\tdefault:\n\t\treturn \"\"\n\t}\n}\n\n")

	fmt.Fprintf(out, "func GppEnum_%sValue(value %s) %s { return %s }\n\n", enum.Name, enum.Name, enum.BackingType, enum.BackingType+"(value)")

	fmt.Fprintf(out, "func (value %s) MarshalJSON() ([]byte, error) {\n", enum.Name)
	fmt.Fprintf(out, "\tif _, err := %s(%s(value)); err != nil { return nil, err }\n", enumFromName(enum), enum.BackingType)
	fmt.Fprintf(out, "\treturn %s.Marshal(%s(value))\n}\n\n", jsonAlias, enum.BackingType)
	fmt.Fprintf(out, "func (value *%s) UnmarshalJSON(data []byte) error {\n", enum.Name)
	fmt.Fprintf(out, "\tvar raw %s\n", enum.BackingType)
	fmt.Fprintf(out, "\tif err := %s.Unmarshal(data, &raw); err != nil { return err }\n", jsonAlias)
	fmt.Fprintf(out, "\tconverted, err := %s(raw)\n\tif err != nil { return err }\n\t*value = converted\n\treturn nil\n}\n\n", enumFromName(enum))
	fmt.Fprintf(out, "func (value %s) GobEncode() ([]byte, error) { return value.MarshalJSON() }\n", enum.Name)
	fmt.Fprintf(out, "func (value *%s) GobDecode(data []byte) error { return value.UnmarshalJSON(data) }\n\n", enum.Name)

	fmt.Fprintf(out, "func (value %s) Value() (%s.Value, error) {\n", enum.Name, driverAlias)
	fmt.Fprintf(out, "\tif _, err := %s(%s(value)); err != nil { return nil, err }\n", enumFromName(enum), enum.BackingType)
	if enum.BackingType == "string" {
		fmt.Fprintf(out, "\treturn %s(value), nil\n}\n\n", enum.BackingType)
	} else if strings.HasPrefix(enum.BackingType, "u") || enum.BackingType == "uint" {
		fmt.Fprintf(out, "\traw := %s(value)\n\tif uint64(raw) > uint64(1<<63-1) { return nil, %s.New(%q) }\n\treturn int64(raw), nil\n}\n\n", enum.BackingType, errorAlias, "enum "+enum.Name+" value exceeds SQL integer range")
	} else {
		fmt.Fprintf(out, "\treturn int64(%s(value)), nil\n}\n\n", enum.BackingType)
	}

	fmt.Fprintf(out, "func (value *%s) Scan(src any) error {\n", enum.Name)
	if enum.BackingType == "string" {
		fmt.Fprintln(out, "\tvar raw string")
		fmt.Fprintln(out, "\tswitch typed := src.(type) {")
		fmt.Fprintln(out, "\tcase string:")
		fmt.Fprintln(out, "\t\traw = typed")
		fmt.Fprintln(out, "\tcase []byte:")
		fmt.Fprintln(out, "\t\traw = string(typed)")
		fmt.Fprintf(out, "\tdefault:\n\t\treturn %s.New(%q)\n\t}\n", errorAlias, "cannot scan "+enum.Name+" from SQL value")
	} else if strings.HasPrefix(enum.BackingType, "u") || enum.BackingType == "uint" {
		fmt.Fprintf(out, "\tvar raw %s\n\tswitch typed := src.(type) {\n\tcase int64:\n\t\tif typed < 0 { return %s.New(%q) }\n\t\traw = %s(typed)\n\tdefault:\n\t\treturn %s.New(%q)\n\t}\n", enum.BackingType, errorAlias, "cannot scan negative value into "+enum.Name, enum.BackingType, errorAlias, "cannot scan "+enum.Name+" from SQL value")
	} else {
		fmt.Fprintf(out, "\tvar raw %s\n\tswitch typed := src.(type) {\n\tcase int64:\n\t\traw = %s(typed)\n\tdefault:\n\t\treturn %s.New(%q)\n\t}\n", enum.BackingType, enum.BackingType, errorAlias, "cannot scan "+enum.Name+" from SQL value")
	}
	fmt.Fprintf(out, "\tconverted, err := %s(raw)\n\tif err != nil { return err }\n\t*value = converted\n\treturn nil\n}\n\n", enumFromName(enum))
}

func enumErrorImport(file *File) (string, bool) {
	return enumImportAlias(file, "errors", "gppEnumErrors")
}

func enumJSONImport(file *File) (string, bool) {
	return enumImportAlias(file, "encoding/json", "gppEnumJSON")
}

func enumDriverImport(file *File) (string, bool) {
	return enumImportAlias(file, "database/sql/driver", "gppEnumDriver")
}

func enumImportAlias(file *File, importPath, preferred string) (string, bool) {
	imports, err := goImports(file)
	if err != nil {
		return preferred, true
	}
	existing := map[string]bool{}
	for _, spec := range imports {
		currentPath, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			continue
		}
		alias := path.Base(currentPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		existing[alias] = true
		if currentPath == importPath && alias != "_" && alias != "." {
			return alias, false
		}
	}
	alias := preferred
	for index := 2; existing[alias]; index++ {
		alias = fmt.Sprintf("%s%d", preferred, index)
	}
	return alias, true
}

func hasEnumDeclarations(file *File) bool {
	for _, decl := range file.Decls {
		if _, ok := decl.(*EnumDecl); ok {
			return true
		}
	}
	return false
}

func enumReference(expr ast.Expr, context constructorContext) (string, *EnumDecl, bool) {
	switch value := expr.(type) {
	case *ast.Ident:
		enum, ok := context.Enums[value.Name]
		return value.Name, enum, ok
	case *ast.SelectorExpr:
		qualifier, ok := value.X.(*ast.Ident)
		if !ok {
			return "", nil, false
		}
		key := qualifier.Name + "." + value.Sel.Name
		enum, exists := context.Enums[key]
		return key, enum, exists
	default:
		return "", nil, false
	}
}

func enumReferencePrefix(key string) string {
	if dot := strings.IndexByte(key, '.'); dot >= 0 {
		return key[:dot] + "."
	}
	return ""
}

func enumReferenceType(key string, enum *EnumDecl) string {
	return enumReferencePrefix(key) + enum.Name
}

func enumValuesReference(expr ast.Expr, context constructorContext) (string, *EnumDecl, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "values" {
		return "", nil, false
	}
	return enumReference(selector.X, context)
}

func enumGeneratedMember(key string, enum *EnumDecl, name string) string {
	return enumReferencePrefix(key) + "GppEnum_" + enum.Name + "_" + name
}

func enumGeneratedMeta(key string, enum *EnumDecl, name string) string {
	return enumGeneratedMember(key, enum, name) + "Meta"
}

func enumMember(enum *EnumDecl, name string) (EnumMember, bool) {
	for _, member := range enum.Members {
		if member.Name == name {
			return member, true
		}
	}
	return EnumMember{}, false
}

func enumForValue(expr ast.Expr, context constructorContext, valueTypes map[string]string) (string, *EnumDecl, bool) {
	typeName := strings.TrimPrefix(strings.TrimSpace(expressionStaticType(expr, context, valueTypes)), "*")
	if enum, ok := context.Enums[typeName]; ok {
		return typeName, enum, true
	}
	return "", nil, false
}

func enumForMetadataValue(expr ast.Expr, context constructorContext, valueTypes map[string]string) (string, *EnumDecl, bool) {
	typeName := strings.TrimSpace(expressionStaticType(expr, context, valueTypes))
	for key, enum := range context.Enums {
		if typeName == enumReferencePrefix(key)+enumMetaTypeName(enum) {
			return key, enum, true
		}
	}
	return "", nil, false
}

func transformEnums(src string, context constructorContext) (string, error) {
	parsed, fileSet, prefixLength, err := parseExceptionSource(src, context)
	if err != nil {
		return src, nil
	}
	valueTypes := polymorphicValueTypes(parsed, context)
	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	addEdit := func(node ast.Node, replacement string) {
		start := fileSet.Position(node.Pos()).Offset - prefixLength
		end := fileSet.Position(node.End()).Offset - prefixLength
		if start >= 0 && end <= len(src) && start <= end {
			edits = append(edits, edit{start: start, end: end, text: replacement})
		}
	}

	ast.Inspect(parsed, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if selector.Sel.Name == "name" || selector.Sel.Name == "value" {
			if memberSelector, memberOK := selector.X.(*ast.SelectorExpr); memberOK {
				if key, enum, ok := enumReference(memberSelector.X, context); ok {
					if member, exists := enumMember(enum, memberSelector.Sel.Name); exists {
						field := "Name"
						if selector.Sel.Name == "value" {
							field = "Value"
						}
						addEdit(selector, enumGeneratedMeta(key, enum, member.Name)+"."+field)
						return false
					}
				}
			}
			if key, enum, ok := enumForValue(selector.X, context, valueTypes); ok {
				helper := "GppEnum_" + enum.Name + "Name"
				if selector.Sel.Name == "value" {
					helper = "GppEnum_" + enum.Name + "Value"
				}
				addEdit(selector, enumReferencePrefix(key)+helper+"("+sourceNodeText(selector.X, fileSet, prefixLength, src)+")")
				return false
			}
			if _, _, ok := enumForMetadataValue(selector.X, context, valueTypes); ok {
				field := "Name"
				if selector.Sel.Name == "value" {
					field = "Value"
				}
				addEdit(selector, sourceNodeText(selector.X, fileSet, prefixLength, src)+"."+field)
				return false
			}
		}

		if key, enum, ok := enumReference(selector.X, context); ok {
			switch selector.Sel.Name {
			case "From":
				addEdit(selector, enumReferencePrefix(key)+enumFromName(enum))
				return false
			case "values":
				addEdit(selector, enumReferencePrefix(key)+enumValuesName(enum))
				return false
			default:
				if member, exists := enumMember(enum, selector.Sel.Name); exists {
					addEdit(selector, enumGeneratedMember(key, enum, member.Name))
					return false
				}
			}
		}
		return true
	})

	if len(edits) == 0 {
		return src, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, change := range edits {
		src = src[:change.start] + change.text + src[change.end:]
	}
	return src, nil
}
