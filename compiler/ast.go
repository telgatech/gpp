package compiler

type Program struct {
	Files []*File
}

type File struct {
	Name        string
	Package     string
	Annotations []AnnotationUse
	Decls       []Decl
}

type Decl interface {
	decl()
}

type RawDecl struct {
	Code                 string
	AnnotationPlacements []AnnotationPlacement
}

func (*RawDecl) decl() {}

type ClassDecl struct {
	Name        string
	SourceFile  string
	SourceLine  int
	Parents     []string
	Annotations []AnnotationUse
	Fields      []Field
	Methods     []Method
}

func (*ClassDecl) decl() {}

type ExtendDecl struct {
	Targets           []string
	TargetConstraints map[string]string
	SourceFile        string
	SourceLine        int
	Methods           []Method
}

func (*ExtendDecl) decl() {}

type Field struct {
	Name        string
	Type        string
	Annotations []AnnotationUse
}

type Method struct {
	Name                 string
	TypeParams           string
	GoName               string
	IsStatic             bool
	Generated            bool
	Parameters           string
	ParameterAnnotations map[string][]AnnotationUse
	Result               string
	Body                 string
	Annotations          []AnnotationUse
}

type AnnotationTarget string

const (
	AnnotationTargetClass     AnnotationTarget = "class"
	AnnotationTargetField     AnnotationTarget = "field"
	AnnotationTargetMethod    AnnotationTarget = "method"
	AnnotationTargetFunction  AnnotationTarget = "function"
	AnnotationTargetParameter AnnotationTarget = "parameter"
	AnnotationTargetType      AnnotationTarget = "type"
	AnnotationTargetPackage   AnnotationTarget = "package"
)

type AnnotationDecl struct {
	Name       string
	Params     string
	Targets    []AnnotationTarget
	Package    string
	SourceFile string
	SourceLine int
	Exported   bool
}

func (*AnnotationDecl) decl() {}

type AnnotationUse struct {
	Name         string
	Arguments    string
	HasArguments bool
	SourceFile   string
	SourceLine   int
	Declaration  *AnnotationDecl
}

type AnnotationPlacement struct {
	Use    AnnotationUse
	Target AnnotationTarget
}
