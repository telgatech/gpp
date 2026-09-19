package compiler

// addDefaultObjectMethods adds the concrete-class defaults for String and
// Dump. These are ordinary generated methods; they do not introduce a root
// class or alter the existing inheritance representation.
func addDefaultObjectMethods(model *SemanticModel) {
	for packageName, pkg := range model.Packages {
		for _, class := range pkg.Classes {
			for _, name := range []string{"String", "Dump"} {
				if hasEffectiveUserMethod(model, packageName, class, name, map[*ClassDecl]bool{}) {
					continue
				}
				if hasGeneratedObjectMethod(class, name) {
					continue
				}
				class.Methods = append(class.Methods, Method{
					Name:      name,
					Generated: true,
					ResultAST: parseTypeText("string"),
				})
			}
		}
	}
}

func hasGeneratedObjectMethod(class *ClassDecl, name string) bool {
	for _, method := range class.Methods {
		if method.Generated && !method.IsStatic && method.Name == name && methodResultSource(method) == "string" {
			return true
		}
	}
	return false
}

func hasDefaultObjectMethods(classes map[string]*ClassDecl) bool {
	for _, class := range classes {
		for _, method := range class.Methods {
			if method.Generated && isDefaultObjectMethod(method) {
				return true
			}
		}
	}
	return false
}

func hasEffectiveUserMethod(model *SemanticModel, packageName string, class *ClassDecl, name string, visiting map[*ClassDecl]bool) bool {
	if visiting[class] {
		return false
	}
	visiting[class] = true
	defer delete(visiting, class)

	for _, method := range class.Methods {
		if !method.IsStatic && !method.Generated && method.Name == name && methodParametersSource(method) == "" {
			return true
		}
	}

	pkg := model.Packages[packageName]
	for _, parentName := range classParentNames(class) {
		parent, parentPackage := resolveParentClass(model, pkg, parentName)
		if parent != nil && hasEffectiveUserMethod(model, parentPackage, parent, name, visiting) {
			return true
		}
	}
	return false
}

func resolveParentClass(model *SemanticModel, pkg *PackageSymbols, parentName string) (*ClassDecl, string) {
	if parent, ok := pkg.Classes[parentName]; ok {
		return parent, pkg.Name
	}
	if parent, ok := pkg.ImportedClasses[parentName]; ok {
		for packageName, candidate := range model.Packages {
			if candidate.Classes[parent.Name] == parent {
				return parent, packageName
			}
		}
	}
	return nil, ""
}
