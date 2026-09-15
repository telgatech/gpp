package main

import "fmt"

type GppType struct {
	Name string
}
type GppAnnotationType struct {
	Name     string
	FullName string
}

func (annotation *GppAnnotationType) GppAnnotationFullName() string {
	if annotation == nil {
		return ""
	}
	return annotation.FullName
}

type GppAnnotation struct {
	Name     string
	FullName string
	Args     []any
	Type     any
}

type GppAnnotations []GppAnnotation

func (annotations GppAnnotations) Has(annotation any) bool {
	return annotations.find(annotation) >= 0
}

func (annotations GppAnnotations) Get(annotation any) *GppAnnotation {
	index := annotations.find(annotation)
	if index < 0 {
		return nil
	}
	return &annotations[index]
}

func (annotations GppAnnotations) All(annotation any) []GppAnnotation {
	result := []GppAnnotation{}
	for _, value := range annotations {
		if annotationMatches(value, annotation) {
			result = append(result, value)
		}
	}
	return result
}

func (annotations GppAnnotations) find(annotation any) int {
	for index, value := range annotations {
		if annotationMatches(value, annotation) {
			return index
		}
	}
	return -1
}

func annotationMatches(value GppAnnotation, annotation any) bool {
	switch key := annotation.(type) {
	case interface{ GppAnnotationFullName() string }:
		return value.FullName == key.GppAnnotationFullName()
	case string:
		return value.Name == key || value.FullName == key
	default:
		return false
	}
}

type GppField struct {
	Name        string
	Owner       *GppClass
	Type        *GppType
	Get         func(any) any
	Set         func(any, any)
	Addr        func(any) any
	Annotations GppAnnotations
}

type GppMethod struct {
	Name        string
	Owner       *GppClass
	Parameters  []GppParameter
	Result      *GppType
	Static      bool
	Annotations GppAnnotations
}

type GppParameter struct {
	Name        string
	Type        *GppType
	Annotations GppAnnotations
}

type GppClass struct {
	Name        string
	Parents     []*GppClass
	Fields      []GppField
	Methods     []GppMethod
	Annotations GppAnnotations
}

type Person struct {
	Name string
	Age  int
}

func (this *Person) Greet() string {

	return fmt.Sprintf("Hello %v, age %v", this.Name, this.Age)

}

type __gpp_Person interface {
	GppRuntimeClass() *GppClass
	Greet() string
}

type GppPerson = __gpp_Person

func (this Person) GppRuntimeClass() *GppClass {
	return GppPersonClass
}

var GppPersonClass = &GppClass{Name: "Person", Parents: nil, Annotations: GppAnnotations{}, Methods: []GppMethod{
	{Name: "Greet", Parameters: nil, Result: &GppType{Name: "string"}, Annotations: GppAnnotations{}},
}, Fields: []GppField{
	{Name: "Name", Owner: nil, Type: &GppType{Name: "string"}, Annotations: GppAnnotations{},
		Get: func(root any) any {
			switch value := root.(type) {
			case *Person:
				return value.Name
			case Person:
				return value.Name
			default:
				return nil
			}
		},
		Set: func(root any, input any) {
			value, ok := root.(*Person)
			if !ok {
				return
			}
			converted, ok := input.(string)
			if !ok {
				return
			}
			value.Name = converted
		},
		Addr: func(root any) any {
			value, ok := root.(*Person)
			if !ok {
				return nil
			}
			return &value.Name
		},
	},
	{Name: "Age", Owner: nil, Type: &GppType{Name: "int"}, Annotations: GppAnnotations{},
		Get: func(root any) any {
			switch value := root.(type) {
			case *Person:
				return value.Age
			case Person:
				return value.Age
			default:
				return nil
			}
		},
		Set: func(root any, input any) {
			value, ok := root.(*Person)
			if !ok {
				return
			}
			converted, ok := input.(int)
			if !ok {
				return
			}
			value.Age = converted
		},
		Addr: func(root any) any {
			value, ok := root.(*Person)
			if !ok {
				return nil
			}
			return &value.Age
		},
	},
},
}

func init() {
	GppPersonClass.Fields[0].Owner = GppPersonClass
	GppPersonClass.Fields[1].Owner = GppPersonClass
	GppPersonClass.Methods[0].Owner = GppPersonClass
}

func main() {
	p := Person{Name: "Bob", Age: 42}

	fmt.Println(p.Greet())
}
