package compiler

import "sort"

func transformStaticTemplateCallsAST(src string, context constructorContext) (string, bool, error) {
	if len(context.Templates) == 0 {
		return src, false, nil
	}
	aliases := map[string]bool{}
	for alias, importPath := range context.AvailableImports {
		if importPath == "gpp/tpl" || (context.ModulePath != "" && importPath == context.ModulePath+"/gpp/tpl") {
			aliases[alias] = true
		}
	}
	if len(aliases) == 0 {
		return src, true, nil
	}
	bodies := []*BlockStmt{}
	functions := parseTopLevelFunctions("template calls", "main", src, 0, src, "", 0)
	if len(functions) > 0 {
		for _, function := range functions {
			if function == nil || function.Method.BodyAST == nil {
				return src, false, nil
			}
			bodies = append(bodies, function.Method.BodyAST)
		}
	} else {
		tokens, err := LexSource("template calls", src)
		if err != nil {
			return src, false, nil
		}
		block, err := ParseBodyAST(tokens)
		if err != nil || block == nil {
			return src, false, nil
		}
		bodies = append(bodies, block)
	}

	type edit struct {
		start int
		end   int
		text  string
	}
	edits := []edit{}
	for _, body := range bodies {
		walkIntrospectionBlockExpressions(body, func(expression ExprNode) {
			call, ok := expression.(*CallExpr)
			if !ok {
				return
			}
			selector, ok := call.Callee.(*SelectorExpr)
			if !ok {
				return
			}
			receiver, ok := selector.Receiver.(*NameExpr)
			if !ok || !aliases[receiver.Name] || context.Templates[selector.Name] == nil {
				return
			}
			span := selector.Span()
			edits = append(edits, edit{start: span.Start, end: span.End, text: "__gpp_tpl_" + selector.Name})
		})
	}
	if len(edits) == 0 {
		return src, true, nil
	}
	sort.SliceStable(edits, func(left, right int) bool { return edits[left].start > edits[right].start })
	for _, edit := range edits {
		if edit.start >= 0 && edit.end <= len(src) && edit.start <= edit.end {
			src = src[:edit.start] + edit.text + src[edit.end:]
		}
	}
	return src, true, nil
}
