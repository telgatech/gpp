package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
)

const parsedFileCacheVersion = 3

type parsedFileCacheEntry struct {
	Version  int
	Key      string
	File     *File
	FileSets []cachedFileSet
}

type cachedFileSet struct {
	DeclarationIndex int
	Mixed            bool
	Function         bool
	Name             string
	Base             int
	Size             int
	Lines            []int
}

type gobGoDecl struct {
	Declarations         []ast.Decl
	Tokens               []Token
	AnnotationPlacements []AnnotationPlacement
	SourceSpan           Span
	SourceFile           string
	SourceLine           int
}

type gobMixedDecl struct {
	GoASTDecls           []ast.Decl
	Functions            []*FunctionDecl
	Tokens               []Token
	AnnotationPlacements []AnnotationPlacement
	SourceSpan           Span
	SourceFile           string
	SourceLine           int
}

type gobFunctionDecl struct {
	Name        string
	Doc         string
	Method      Method
	GoAST       *ast.FuncDecl
	SourceSpan  Span
	SourceFile  string
	SourceLine  int
	Annotations []AnnotationUse
}

func (declaration *GoDecl) GobEncode() ([]byte, error) {
	var data bytes.Buffer
	err := gob.NewEncoder(&data).Encode(gobGoDecl{
		Declarations:         declaration.Declarations,
		Tokens:               declaration.Tokens,
		AnnotationPlacements: declaration.AnnotationPlacements,
		SourceSpan:           declaration.SourceSpan,
		SourceFile:           declaration.SourceFile,
		SourceLine:           declaration.SourceLine,
	})
	return data.Bytes(), err
}

func (declaration *GoDecl) GobDecode(data []byte) error {
	var decoded gobGoDecl
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&decoded); err != nil {
		return err
	}
	declaration.Declarations = decoded.Declarations
	declaration.Tokens = decoded.Tokens
	declaration.AnnotationPlacements = decoded.AnnotationPlacements
	declaration.SourceSpan = decoded.SourceSpan
	declaration.SourceFile = decoded.SourceFile
	declaration.SourceLine = decoded.SourceLine
	return nil
}

func (declaration *MixedDecl) GobEncode() ([]byte, error) {
	var data bytes.Buffer
	err := gob.NewEncoder(&data).Encode(gobMixedDecl{
		GoASTDecls:           declaration.GoASTDecls,
		Functions:            declaration.Functions,
		Tokens:               declaration.Tokens,
		AnnotationPlacements: declaration.AnnotationPlacements,
		SourceSpan:           declaration.SourceSpan,
		SourceFile:           declaration.SourceFile,
		SourceLine:           declaration.SourceLine,
	})
	return data.Bytes(), err
}

func (declaration *MixedDecl) GobDecode(data []byte) error {
	var decoded gobMixedDecl
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&decoded); err != nil {
		return err
	}
	declaration.GoASTDecls = decoded.GoASTDecls
	declaration.Functions = decoded.Functions
	declaration.Tokens = decoded.Tokens
	declaration.AnnotationPlacements = decoded.AnnotationPlacements
	declaration.SourceSpan = decoded.SourceSpan
	declaration.SourceFile = decoded.SourceFile
	declaration.SourceLine = decoded.SourceLine
	return nil
}

func (declaration *FunctionDecl) GobEncode() ([]byte, error) {
	var data bytes.Buffer
	err := gob.NewEncoder(&data).Encode(gobFunctionDecl{
		Name:        declaration.Name,
		Doc:         declaration.Doc,
		Method:      declaration.Method,
		GoAST:       declaration.GoAST,
		SourceSpan:  declaration.SourceSpan,
		SourceFile:  declaration.SourceFile,
		SourceLine:  declaration.SourceLine,
		Annotations: declaration.Annotations,
	})
	return data.Bytes(), err
}

func (declaration *FunctionDecl) GobDecode(data []byte) error {
	var decoded gobFunctionDecl
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&decoded); err != nil {
		return err
	}
	declaration.Name = decoded.Name
	declaration.Doc = decoded.Doc
	declaration.Method = decoded.Method
	declaration.GoAST = decoded.GoAST
	declaration.SourceSpan = decoded.SourceSpan
	declaration.SourceFile = decoded.SourceFile
	declaration.SourceLine = decoded.SourceLine
	declaration.Annotations = decoded.Annotations
	return nil
}

func init() {
	for _, node := range []any{
		(*AnnotationDecl)(nil), (*ClassDecl)(nil), (*EmbedDecl)(nil), (*EnumDecl)(nil),
		(*ExtendDecl)(nil), (*FunctionDecl)(nil), (*GoDecl)(nil), (*MixedDecl)(nil),
		(*TemplateDecl)(nil), (*ValueDecl)(nil),
		(*AssignmentExpr)(nil), (*BinaryExpr)(nil), (*CallExpr)(nil), (*CompositeLiteralExpr)(nil),
		(*FunctionLiteralExpr)(nil), (*IndexExpr)(nil), (*IndexListExpr)(nil), (*InterpolatedStringExpr)(nil),
		(*LambdaExpr)(nil), (*LiteralExpr)(nil), (*NameExpr)(nil), (*ParenthesizedExpr)(nil),
		(*PostfixExpr)(nil), (*SelectorExpr)(nil), (*SendExpr)(nil), (*SliceExpr)(nil),
		(*SpreadExpr)(nil), (*TokenExpr)(nil), (*TypeAssertExpr)(nil), (*TypeExpr)(nil), (*UnaryExpr)(nil),
		(*AssignmentStmt)(nil), (*BlockStmt)(nil), (*BranchStmt)(nil), (*CaseStmt)(nil),
		(*DeclarationStmt)(nil), (*DeferStmt)(nil), (*ExpressionStmt)(nil), (*ForStmt)(nil),
		(*GoStmt)(nil), (*IfStmt)(nil), (*IncDecStmt)(nil), (*LabelStmt)(nil), (*ReturnStmt)(nil),
		(*SendStmt)(nil), (*SwitchStmt)(nil), (*ThrowStmt)(nil), (*TokenStmt)(nil), (*TryStmt)(nil),
		(*TypeDeclarationStmt)(nil),
		(*ArrayType)(nil), (*ChannelType)(nil), (*FunctionType)(nil), (*InterfaceType)(nil),
		(*MapType)(nil), (*NamedType)(nil), (*PointerType)(nil), (*SliceType)(nil), (*StructType)(nil),
		(*TokenType)(nil), (*TupleType)(nil), (*UnderlyingType)(nil), (*UnionType)(nil), (*VariadicType)(nil),
		(*ast.ArrayType)(nil), (*ast.AssignStmt)(nil), (*ast.BadDecl)(nil), (*ast.BadExpr)(nil),
		(*ast.BadStmt)(nil), (*ast.BasicLit)(nil), (*ast.BinaryExpr)(nil), (*ast.BlockStmt)(nil),
		(*ast.BranchStmt)(nil), (*ast.CallExpr)(nil), (*ast.CaseClause)(nil), (*ast.ChanType)(nil),
		(*ast.CommClause)(nil), (*ast.CompositeLit)(nil), (*ast.DeclStmt)(nil), (*ast.DeferStmt)(nil),
		(*ast.Ellipsis)(nil), (*ast.EmptyStmt)(nil), (*ast.ExprStmt)(nil), (*ast.ForStmt)(nil),
		(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil), (*ast.FuncType)(nil), (*ast.GenDecl)(nil),
		(*ast.GoStmt)(nil), (*ast.Ident)(nil), (*ast.IfStmt)(nil), (*ast.ImportSpec)(nil),
		(*ast.IncDecStmt)(nil), (*ast.IndexExpr)(nil), (*ast.IndexListExpr)(nil), (*ast.InterfaceType)(nil),
		(*ast.KeyValueExpr)(nil), (*ast.LabeledStmt)(nil), (*ast.MapType)(nil), (*ast.ParenExpr)(nil),
		(*ast.RangeStmt)(nil), (*ast.ReturnStmt)(nil), (*ast.SelectStmt)(nil), (*ast.SelectorExpr)(nil),
		(*ast.SendStmt)(nil), (*ast.SliceExpr)(nil), (*ast.StarExpr)(nil), (*ast.StructType)(nil),
		(*ast.SwitchStmt)(nil), (*ast.TypeAssertExpr)(nil), (*ast.TypeSpec)(nil), (*ast.TypeSwitchStmt)(nil),
		(*ast.UnaryExpr)(nil), (*ast.ValueSpec)(nil),
	} {
		gob.Register(node)
	}
}

func parsedFileCacheLocation(outputDir, filename, source string) (string, string) {
	pathDigest := sha256.Sum256([]byte(filepath.Clean(filename)))
	keyHash := sha256.New()
	writePackageHashPart(keyHash, fmt.Sprintf("parser-cache-version=%d", parsedFileCacheVersion))
	writePackageHashPart(keyHash, filepath.Clean(filename))
	writePackageHashPart(keyHash, source)
	return filepath.Join(outputDir, ".gpp-parse-cache", hex.EncodeToString(pathDigest[:])+".gob"), hex.EncodeToString(keyHash.Sum(nil))
}

// ParseFileWithCache parses source and stores its syntax tree in cacheDir.
// Cache read and write errors are treated as misses so the cache cannot make a
// valid compilation fail.
func ParseFileWithCache(name, filename, source, cacheDir string) (*File, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	if cacheDir != "" {
		path, key := parsedFileCacheLocation(cacheDir, absolute, source)
		if file, fileSets, cacheErr := loadParsedFileCache(path, key); cacheErr == nil && file != nil {
			file.SourcePath = absolute
			restoreParsedFile(file, fileSets)
			return file, nil
		}
		file, err := ParseFile(name, source)
		if err != nil {
			return nil, err
		}
		file.SourcePath = absolute
		_ = writeParsedFileCache(path, key, file)
		return file, nil
	}
	file, err := ParseFile(name, source)
	if err != nil {
		return nil, err
	}
	file.SourcePath = absolute
	return file, nil
}

func loadParsedFileCache(path, key string) (*File, []cachedFileSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var entry parsedFileCacheEntry
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&entry); err != nil {
		return nil, nil, err
	}
	if entry.Version != parsedFileCacheVersion || entry.Key != key || entry.File == nil {
		return nil, nil, nil
	}
	return entry.File, entry.FileSets, nil
}

func writeParsedFileCache(path, key string, file *File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	identifiers, objects := clearParsedGoObjects(file)
	setParsedFileOwners(file, nil)
	entry := parsedFileCacheEntry{Version: parsedFileCacheVersion, Key: key, File: file, FileSets: snapshotFileSets(file)}
	var data bytes.Buffer
	err := gob.NewEncoder(&data).Encode(entry)
	for index, ident := range identifiers {
		ident.Obj = objects[index]
	}
	setParsedFileOwners(file, file)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data.Bytes(), 0644); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func clearParsedGoObjects(file *File) ([]*ast.Ident, []*ast.Object) {
	var identifiers []*ast.Ident
	var objects []*ast.Object
	for _, declaration := range file.Decls {
		var nodes []ast.Decl
		switch value := declaration.(type) {
		case *GoDecl:
			nodes = value.Declarations
		case *MixedDecl:
			nodes = value.GoASTDecls
		case *FunctionDecl:
			if value.GoAST != nil {
				nodes = []ast.Decl{value.GoAST}
			}
		}
		for _, node := range nodes {
			ast.Inspect(node, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if ok && ident.Obj != nil {
					identifiers = append(identifiers, ident)
					objects = append(objects, ident.Obj)
					ident.Obj = nil
				}
				return true
			})
		}
	}
	return identifiers, objects
}

func snapshotFileSets(file *File) []cachedFileSet {
	var result []cachedFileSet
	for index, declaration := range file.Decls {
		var fileSet *token.FileSet
		mixed := false
		function := false
		switch value := declaration.(type) {
		case *GoDecl:
			fileSet = value.FileSet
		case *MixedDecl:
			fileSet = value.GoASTFileSet
			mixed = true
		case *FunctionDecl:
			fileSet = value.GoASTFileSet
			function = true
		}
		if fileSet == nil {
			continue
		}
		fileSet.Iterate(func(file *token.File) bool {
			result = append(result, cachedFileSet{
				DeclarationIndex: index,
				Mixed:            mixed,
				Function:         function,
				Name:             file.Name(),
				Base:             file.Base(),
				Size:             file.Size(),
				Lines:            append([]int(nil), file.Lines()...),
			})
			return true
		})
	}
	return result
}

func restoreParsedFile(file *File, fileSets []cachedFileSet) {
	setParsedFileOwners(file, file)
	for _, snapshot := range fileSets {
		if snapshot.DeclarationIndex < 0 || snapshot.DeclarationIndex >= len(file.Decls) {
			continue
		}
		fileSet := token.NewFileSet()
		tokenFile := fileSet.AddFile(snapshot.Name, snapshot.Base, snapshot.Size)
		if tokenFile == nil {
			continue
		}
		_ = tokenFile.SetLines(snapshot.Lines)
		if snapshot.Function {
			if declaration, ok := file.Decls[snapshot.DeclarationIndex].(*FunctionDecl); ok {
				declaration.GoASTFileSet = fileSet
			}
		} else if snapshot.Mixed {
			if declaration, ok := file.Decls[snapshot.DeclarationIndex].(*MixedDecl); ok {
				declaration.GoASTFileSet = fileSet
			}
		} else if declaration, ok := file.Decls[snapshot.DeclarationIndex].(*GoDecl); ok {
			declaration.FileSet = fileSet
		}
	}
}

func setParsedFileOwners(file, owner *File) {
	for _, declaration := range file.Decls {
		switch value := declaration.(type) {
		case *GoDecl:
			value.Owner = owner
		case *MixedDecl:
			value.Owner = owner
			for _, function := range value.Functions {
				function.Owner = owner
				function.Method.Owner = owner
			}
		case *ValueDecl:
			value.Owner = owner
		case *FunctionDecl:
			value.Owner = owner
			value.Method.Owner = owner
		case *ClassDecl:
			value.Owner = owner
			for index := range value.Fields {
				value.Fields[index].Owner = owner
			}
			for index := range value.Methods {
				value.Methods[index].Owner = owner
			}
		case *ExtendDecl:
			for index := range value.Methods {
				value.Methods[index].Owner = owner
			}
		}
	}
}
