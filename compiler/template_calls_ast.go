package compiler

import (
	"sort"
	"strconv"
)

// lowerStaticTemplateCallNode lowers a template facade call while it is still
// represented by compiler expression nodes. The source-edit implementation
// below remains only for compatibility fragments that cannot use direct body
// emission.
func lowerStaticTemplateCallNode(call *CallExpr, context constructorContext) (ExprNode, bool) {
	if call == nil {
		return nil, false
	}
	selector, ok := call.Callee.(*SelectorExpr)
	if !ok {
		return nil, false
	}
	receiver, ok := selector.Receiver.(*NameExpr)
	if !ok {
		return nil, false
	}
	for alias, importPath := range context.AvailableImports {
		if alias != receiver.Name {
			continue
		}
		if importPath != "gpp/tpl" && (context.ModulePath == "" || importPath != context.ModulePath+"/gpp/tpl") {
			continue
		}
		scope := templatePackageScope(context.ModulePath, context.Package)
		if selector.Name == "Execute" || selector.Name == "Render" || selector.Name == "LoadFile" || selector.Name == "LoadDir" || selector.Name == "Reload" {
			arguments := make([]CallArg, 0, len(call.Arguments)+1)
			arguments = append(arguments, CallArg{Value: &LiteralExpr{Text: strconv.Quote(scope), Kind: TokenString}})
			arguments = append(arguments, call.Arguments...)
			return &CallExpr{
				Callee:    &SelectorExpr{Receiver: selector.Receiver, Name: selector.Name + "In", SpanValue: selector.Span()},
				Arguments: arguments,
				SpanValue: call.SpanValue,
			}, true
		}
		if context.Templates[selector.Name] == nil {
			return nil, false
		}
		return &CallExpr{
			Callee:    &NameExpr{Name: "__gpp_tpl_" + selector.Name, SpanValue: selector.Span()},
			Arguments: append([]CallArg(nil), call.Arguments...),
			SpanValue: call.SpanValue,
		}, true
	}
	return nil, false
}

func transformStaticTemplateCallsAST(src string, context constructorContext) (string, bool, error) {
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
	scope := strconv.Quote(templatePackageScope(context.ModulePath, context.Package))
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
			if !ok || !aliases[receiver.Name] {
				return
			}
			span := selector.Span()
			if context.Templates[selector.Name] == nil {
				return
			}
			edits = append(edits, edit{start: span.Start, end: span.End, text: "__gpp_tpl_" + selector.Name})
		})
	}
	// Some otherwise valid Go expressions remain opaque TokenExpr nodes in the
	// Go++ body AST. Rewrite scoped facade calls from the lossless token stream
	// as well, so calls such as tpl.Execute(&buffer, ...) still carry the source
	// package scope through the compatibility lowering path.
	tokens, err := LexSource("template calls", src)
	if err == nil {
		for index := 0; index+3 < len(tokens); index++ {
			if tokens[index].Kind != TokenIdentifier || tokens[index+1].Text != "." || tokens[index+2].Kind != TokenIdentifier || tokens[index+3].Text != "(" {
				continue
			}
			if index > 0 && tokens[index-1].Text == "." {
				continue
			}
			if !aliases[tokens[index].Text] {
				continue
			}
			method, ok := scopedTemplateMethod(tokens[index+2].Text)
			if !ok {
				continue
			}
			start := tokens[index].Span.Start
			end := tokens[index+2].Span.End
			edits = append(edits, edit{start: start, end: end, text: tokens[index].Text + "." + method})
			arguments := scope
			if index+4 < len(tokens) && tokens[index+4].Text != ")" {
				arguments += ", "
			}
			insertAt := tokens[index+3].Span.End
			edits = append(edits, edit{start: insertAt, end: insertAt, text: arguments})
		}
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

func scopedTemplateMethod(name string) (string, bool) {
	switch name {
	case "Execute", "Render", "LoadFile", "LoadDir", "Reload":
		return name + "In", true
	default:
		return "", false
	}
}
