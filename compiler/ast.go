package compiler

import (
	"go/ast"
	"go/token"
)

type Program struct {
	Files []*File
}

type File struct {
	Name            string
	SourcePath      string
	Source          string
	Tokens          []Token
	Comments        []Token
	Package         string
	PackageAST      *PackageDecl
	Imports         []ImportDecl
	Official        bool
	OfficialPackage string
	Doc             string
	Annotations     []AnnotationUse
	Decls           []Decl
}

type PackageDecl struct {
	Name        string
	Annotations []AnnotationUse
	SpanValue   Span
}

func (*PackageDecl) node() {}
func (declaration *PackageDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SpanValue
}

type ImportDecl struct {
	Alias          string
	Path           string
	LogicalPackage bool
	SpanValue      Span
}

func (*ImportDecl) node() {}
func (declaration *ImportDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SpanValue
}

type Node interface {
	node()
	Span() Span
}

type Decl interface {
	Node
	decl()
}

type MixedDecl struct {
	GoASTDecls   []ast.Decl
	GoASTFileSet *token.FileSet
	// Functions are structured metadata for top-level Go++ functions contained
	// by this source declaration. MixedDecl remains the compatibility container
	// while transformations still need to see adjacent top-level declarations
	// together (for example record inference and overload lowering).
	Functions            []*FunctionDecl
	Tokens               []Token
	AnnotationPlacements []AnnotationPlacement
	Owner                *File
	SourceSpan           Span
	SourceFile           string
	SourceLine           int
}

func (*MixedDecl) node() {}
func (*MixedDecl) decl() {}
func (declaration *MixedDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SourceSpan
}

// GoDecl is the structured representation of a declaration group that is
// valid ordinary Go syntax. It deliberately retains Go's AST as the syntax
// tree instead of wrapping the source in MixedDecl; source text is recovered
// from the owning File and SourceSpan only when a lowering or diagnostic needs
// to reproduce it.
type GoDecl struct {
	Declarations         []ast.Decl
	FileSet              *token.FileSet
	Tokens               []Token
	AnnotationPlacements []AnnotationPlacement
	Owner                *File
	SourceSpan           Span
	SourceFile           string
	SourceLine           int
}

func (*GoDecl) node() {}
func (*GoDecl) decl() {}
func (declaration *GoDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SourceSpan
}

// ValueDecl is a Go++ value declaration whose initializer expressions require
// Go++ lowering (for example `let user = record(...)` or a `??` expression).
// Its names, type, and values are structured nodes; the token slice is kept
// only for exact source spans and comments.
type ValueDecl struct {
	Keyword    string
	Names      []Token
	Type       TypeNode
	Values     []ExprNode
	Tokens     []Token
	Owner      *File
	SourceSpan Span
	SourceFile string
	SourceLine int
}

func (*ValueDecl) node() {}
func (*ValueDecl) decl() {}
func (declaration *ValueDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SourceSpan
}

// FunctionDecl is the structured source representation of a top-level
// function. Method carries the parsed signature, body tokens, and body AST;
// the Go declaration is retained only for Go-compatible metadata and source
// tooling. This keeps top-level executable code on the same AST path as class
// and extension methods.
type FunctionDecl struct {
	Name         string
	Doc          string
	Method       Method
	GoAST        *ast.FuncDecl
	GoASTFileSet *token.FileSet
	Owner        *File
	SourceSpan   Span
	SourceFile   string
	SourceLine   int
	Annotations  []AnnotationUse
}

func (*FunctionDecl) node() {}
func (*FunctionDecl) decl() {}
func (declaration *FunctionDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SourceSpan
}

type EmbedDecl struct {
	Entries    []EmbedEntry
	SpanValue  Span
	SourceFile string
	SourceLine int
}

type EmbedEntry struct {
	Name       string
	Path       string
	Directory  bool
	SourceFile string
	SourceLine int
}

func (*EmbedDecl) node() {}
func (*EmbedDecl) decl() {}
func (declaration *EmbedDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SpanValue
}

type TemplateDecl struct {
	Name         string
	ParameterAST []ParameterNode
	Layout       string
	Doc          string
	Annotations  []AnnotationUse
	// Body remains the opaque template payload; template actions are parsed in
	// later migration stages. It is not executable Go++ declaration syntax.
	Body       string
	BodyTokens []Token
	BodySpan   Span
	SpanValue  Span
	SourceFile string
	SourceLine int
}

func (*TemplateDecl) node() {}
func (*TemplateDecl) decl() {}
func (declaration *TemplateDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SpanValue
}

type ClassDecl struct {
	Name          string
	Owner         *File
	TypeParamsAST []TypeParameterNode
	Doc           string
	SourceFile    string
	SourceLine    int
	ParentAST     []TypeNode
	Annotations   []AnnotationUse
	Fields        []Field
	Methods       []Method
	SpanValue     Span
}

func (*ClassDecl) node() {}
func (*ClassDecl) decl() {}
func (declaration *ClassDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SpanValue
}

// ParentNames returns the source spelling of the structured parent types.
// Consumers that need inheritance metadata should use this accessor instead
// of relying on a duplicate string field in ClassDecl.
func (declaration *ClassDecl) ParentNames() []string {
	return classParentNames(declaration)
}

type EnumDecl struct {
	Name           string
	BackingTypeAST TypeNode
	Doc            string
	Members        []EnumMember
	SpanValue      Span
	SourceFile     string
	SourceLine     int
}

func (*EnumDecl) node() {}
func (*EnumDecl) decl() {}
func (declaration *EnumDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SpanValue
}

type EnumMember struct {
	Name        string
	ValueAST    ExprNode
	ValueTokens []Token
	Doc         string
}

type ExtendDecl struct {
	TargetAST         []TypeNode
	TargetConstraints map[string]TypeNode
	Doc               string
	SourceFile        string
	SourceLine        int
	Methods           []Method
	SpanValue         Span
}

func (*ExtendDecl) node() {}
func (*ExtendDecl) decl() {}
func (declaration *ExtendDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SpanValue
}

type Field struct {
	Name        string
	TypeAST     TypeNode
	Owner       *File
	TypeSpan    Span
	SpanValue   Span
	Doc         string
	Annotations []AnnotationUse
}

func (*Field) node() {}
func (field *Field) Span() Span {
	if field == nil {
		return Span{}
	}
	return field.SpanValue
}

type Method struct {
	Name                 string
	NameSpan             Span
	Doc                  string
	TypeParamsAST        []TypeParameterNode
	GoName               string
	IsStatic             bool
	Generated            bool
	ParametersSpan       Span
	ParameterAST         []ParameterNode
	ParameterAnnotations map[string][]AnnotationUse
	ResultSpan           Span
	ResultAST            TypeNode
	ResultFieldsAST      []ParameterNode
	Owner                *File
	BodySpan             Span
	SpanValue            Span
	BodyTokens           []Token
	BodyAST              *BlockStmt
	Annotations          []AnnotationUse
}

func (*Method) node() {}
func (method *Method) Span() Span {
	if method == nil {
		return Span{}
	}
	return method.SpanValue
}

type ParameterNode struct {
	Name          string
	Type          TypeNode
	Default       ExprNode
	DefaultTokens []Token
	HasDefault    bool
	SpanValue     Span
}

type TypeParameterNode struct {
	Name       string
	Constraint TypeNode
	SpanValue  Span
}

func (*TypeParameterNode) node() {}
func (parameter *TypeParameterNode) Span() Span {
	if parameter == nil {
		return Span{}
	}
	return parameter.SpanValue
}

func (*ParameterNode) node() {}
func (parameter *ParameterNode) Span() Span {
	if parameter == nil {
		return Span{}
	}
	return parameter.SpanValue
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
	AnnotationTargetTemplate  AnnotationTarget = "template"
)

type AnnotationDecl struct {
	Name         string
	ParameterAST []ParameterNode
	Targets      []AnnotationTarget
	Package      string
	Doc          string
	SourceFile   string
	SourceLine   int
	Exported     bool
	SpanValue    Span
}

func (*AnnotationDecl) node() {}
func (*AnnotationDecl) decl() {}
func (declaration *AnnotationDecl) Span() Span {
	if declaration == nil {
		return Span{}
	}
	return declaration.SpanValue
}

type AnnotationUse struct {
	Name           string
	ArgumentTokens [][]Token
	ArgumentsAST   []ExprNode
	HasArguments   bool
	SourceFile     string
	SourceLine     int
	Declaration    *AnnotationDecl
}

// ArgumentTexts renders annotation argument tokens at an API boundary that
// still needs source-shaped values. The annotation AST does not store a
// duplicate comma-separated argument string.
func (use AnnotationUse) ArgumentTexts() []string {
	if !use.HasArguments {
		return nil
	}
	result := make([]string, 0, len(use.ArgumentTokens))
	for _, tokens := range use.ArgumentTokens {
		result = append(result, expressionTokensSource(tokens))
	}
	if len(result) == 0 && len(use.ArgumentsAST) > 0 {
		for _, expression := range use.ArgumentsAST {
			if source, err := expressionNodeSource(expression); err == nil {
				result = append(result, source)
			}
		}
	}
	return result
}

type AnnotationPlacement struct {
	Use    AnnotationUse
	Target AnnotationTarget
}
