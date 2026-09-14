package compiler

type Program struct {
	Files []*File
}

type File struct {
	Name    string
	Package string
	Decls   []Decl
}

type Decl interface {
	decl()
}

type RawDecl struct {
	Code string
}

func (*RawDecl) decl() {}

type ClassDecl struct {
	Name       string
	SourceFile string
	SourceLine int
	Parents    []string
	Fields     []Field
	Methods    []Method
}

func (*ClassDecl) decl() {}

type ExtendDecl struct {
	Target     string
	SourceFile string
	SourceLine int
	Methods    []Method
}

func (*ExtendDecl) decl() {}

type Field struct {
	Name string
	Type string
}

type Method struct {
	Name       string
	TypeParams string
	GoName     string
	Parameters string
	Result     string
	Body       string
}
