package lsp

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
)

func (s *server) nativeGoInlayHints(state *fileState, start, end int) []InlayHint {
	cache, file := s.nativeGoInfoForFile(state)
	if cache == nil || file == nil || cache.info == nil {
		return nil
	}
	var result []InlayHint
	add := func(name *ast.Ident) {
		if name == nil || name.Name == "_" {
			return
		}
		position := cache.fset.Position(name.End()).Offset
		if position < start || position > end {
			return
		}
		variable, ok := cache.info.Defs[name].(*types.Var)
		if !ok || variable.Type() == nil {
			return
		}
		typeName := types.TypeString(variable.Type(), func(pkg *types.Package) string {
			if pkg.Name() == file.Name.Name {
				return ""
			}
			return pkg.Name()
		})
		if typeName == "" || typeName == "invalid type" {
			return
		}
		result = append(result, InlayHint{
			Position:    positionAtOffset(state.Source, position),
			Label:       ": " + typeName,
			Kind:        1,
			PaddingLeft: true,
		})
	}
	for _, declaration := range file.Decls {
		ast.Inspect(declaration, func(node ast.Node) bool {
			if s.cancelled() {
				return false
			}
			switch value := node.(type) {
			case *ast.AssignStmt:
				if value.Tok == token.DEFINE {
					for _, expression := range value.Lhs {
						if name, ok := expression.(*ast.Ident); ok {
							add(name)
						}
					}
				}
			case *ast.RangeStmt:
				if value.Tok == token.DEFINE {
					if name, ok := value.Key.(*ast.Ident); ok {
						add(name)
					}
					if name, ok := value.Value.(*ast.Ident); ok {
						add(name)
					}
				}
			}
			return true
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := result[i].Position, result[j].Position
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Character < right.Character
	})
	return result
}
