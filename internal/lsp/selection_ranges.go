package lsp

import "github.com/telgatech/gpp/compiler"

// syntaxSelectionRanges returns source spans for syntax nodes surrounding the
// given byte offset. The compiler AST keeps these offsets in the original
// source, so selection expansion can follow expression and statement
// structure instead of jumping directly from a token to its line.
func syntaxSelectionRanges(file *compiler.File, source string, offset int) []Range {
	if file == nil {
		return nil
	}
	var ranges []Range
	addSpan := func(span compiler.Span) {
		if span.Start < 0 || span.End <= span.Start || span.End > len(source) || offset < span.Start || offset >= span.End {
			return
		}
		ranges = append(ranges, rangeForOffsets(source, span.Start, span.End))
	}
	var visitExpr func(compiler.ExprNode)
	var visitType func(compiler.TypeNode)
	var visitBlock func(*compiler.BlockStmt)
	var visitStmt func(compiler.Stmt)
	visitType = func(node compiler.TypeNode) {
		if node == nil {
			return
		}
		addSpan(node.Span())
		switch value := node.(type) {
		case *compiler.NamedType:
			for _, argument := range value.Arguments {
				visitType(argument)
			}
		case *compiler.PointerType:
			visitType(value.Element)
		case *compiler.SliceType:
			visitType(value.Element)
		case *compiler.ArrayType:
			visitExpr(value.Length)
			visitType(value.Element)
		case *compiler.MapType:
			visitType(value.Key)
			visitType(value.Value)
		case *compiler.ChannelType:
			visitType(value.Element)
		case *compiler.VariadicType:
			visitType(value.Element)
		case *compiler.FunctionType:
			for _, parameter := range value.Parameters {
				visitType(parameter.Type)
			}
			for _, result := range value.Results {
				visitType(result)
			}
		case *compiler.StructType:
			for _, field := range value.Fields {
				addSpan(field.Span())
				visitType(field.Type)
			}
		case *compiler.InterfaceType:
			for _, embedded := range value.Embeds {
				visitType(embedded)
			}
			for _, method := range value.Methods {
				addSpan(method.Span())
				visitType(method.Signature)
			}
		case *compiler.TupleType:
			for _, element := range value.Elements {
				visitType(element)
			}
		case *compiler.UnderlyingType:
			visitType(value.Element)
		case *compiler.UnionType:
			for _, term := range value.Terms {
				visitType(term)
			}
		}
	}
	visitExpr = func(node compiler.ExprNode) {
		if node == nil {
			return
		}
		addSpan(node.Span())
		switch value := node.(type) {
		case *compiler.InterpolatedStringExpr:
			for _, segment := range value.Segments {
				visitExpr(segment.Expression)
			}
		case *compiler.UnaryExpr:
			visitExpr(value.Operand)
		case *compiler.BinaryExpr:
			visitExpr(value.Left)
			visitExpr(value.Right)
		case *compiler.AssignmentExpr:
			for _, expression := range value.Left {
				visitExpr(expression)
			}
			for _, expression := range value.Right {
				visitExpr(expression)
			}
		case *compiler.SelectorExpr:
			visitExpr(value.Receiver)
		case *compiler.IndexExpr:
			visitExpr(value.Receiver)
			visitExpr(value.Index)
		case *compiler.IndexListExpr:
			visitExpr(value.Receiver)
			for _, index := range value.Indices {
				visitExpr(index)
			}
		case *compiler.SliceExpr:
			visitExpr(value.Receiver)
			visitExpr(value.Low)
			visitExpr(value.High)
			visitExpr(value.Max)
		case *compiler.TypeAssertExpr:
			visitExpr(value.Expression)
			visitType(value.Type)
		case *compiler.PostfixExpr:
			visitExpr(value.Expression)
		case *compiler.SpreadExpr:
			visitExpr(value.Expression)
		case *compiler.TypeExpr:
			visitType(value.Type)
		case *compiler.SendExpr:
			visitExpr(value.Channel)
			visitExpr(value.Value)
		case *compiler.FunctionLiteralExpr:
			visitType(value.Type)
			visitBlock(value.Body)
		case *compiler.CallExpr:
			visitExpr(value.Callee)
			for _, argument := range value.Arguments {
				visitExpr(argument.Value)
			}
		case *compiler.ParenthesizedExpr:
			visitExpr(value.Inner)
		case *compiler.CompositeLiteralExpr:
			visitType(value.Type)
			for _, element := range value.Elements {
				visitExpr(element.Key)
				visitExpr(element.Value)
			}
		case *compiler.LambdaExpr:
			visitExpr(value.Body)
			visitBlock(value.BlockBody)
		}
	}
	visitStmt = func(node compiler.Stmt) {
		if node == nil {
			return
		}
		addSpan(node.Span())
		switch value := node.(type) {
		case *compiler.TokenStmt:
			for _, expression := range value.Exprs {
				visitExpr(expression)
			}
			visitBlock(value.Body)
			for _, child := range value.Children {
				visitStmt(child)
			}
		case *compiler.ExpressionStmt:
			visitExpr(value.Expression)
		case *compiler.DeclarationStmt:
			visitType(value.Type)
			for _, expression := range value.Values {
				visitExpr(expression)
			}
		case *compiler.TypeDeclarationStmt:
			visitType(value.Type)
		case *compiler.AssignmentStmt:
			for _, expression := range value.Left {
				visitExpr(expression)
			}
			for _, expression := range value.Right {
				visitExpr(expression)
			}
		case *compiler.ReturnStmt:
			for _, expression := range value.Values {
				visitExpr(expression)
			}
		case *compiler.ThrowStmt:
			visitExpr(value.Value)
		case *compiler.DeferStmt:
			visitExpr(value.Expression)
		case *compiler.GoStmt:
			visitExpr(value.Expression)
		case *compiler.SendStmt:
			visitExpr(value.Channel)
			visitExpr(value.Value)
		case *compiler.IncDecStmt:
			visitExpr(value.Expression)
		case *compiler.IfStmt:
			visitExpr(value.Init)
			visitExpr(value.Condition)
			visitBlock(value.Body)
			visitBlock(value.Else)
			if value.ElseIf != nil {
				visitStmt(value.ElseIf)
			}
		case *compiler.TryStmt:
			visitBlock(value.Body)
			for _, clause := range value.Catches {
				addSpan(clause.Span())
				visitBlock(clause.Body)
			}
			visitBlock(value.Finally)
		case *compiler.ForStmt:
			visitExpr(value.Init)
			visitExpr(value.Condition)
			visitExpr(value.Post)
			for _, expression := range value.RangeKey {
				visitExpr(expression)
			}
			visitExpr(value.RangeExpr)
			visitBlock(value.Body)
		case *compiler.SwitchStmt:
			visitExpr(value.Init)
			visitExpr(value.Tag)
			visitBlock(value.Body)
		case *compiler.CaseStmt:
			addSpan(value.Clause.Span())
			for _, expression := range value.Clause.Expressions {
				visitExpr(expression)
			}
			visitBlock(value.Clause.Body)
		case *compiler.BlockStmt:
			visitBlock(value)
		}
	}
	visitBlock = func(block *compiler.BlockStmt) {
		if block == nil {
			return
		}
		addSpan(block.Span())
		for _, statement := range block.Statements {
			visitStmt(statement)
		}
	}
	var visitMethod func(*compiler.Method)
	visitMethod = func(method *compiler.Method) {
		if method == nil {
			return
		}
		addSpan(method.Span())
		addSpan(method.BodySpan)
		for _, parameter := range method.ParameterAST {
			addSpan(parameter.Span())
			visitType(parameter.Type)
			visitExpr(parameter.Default)
		}
		visitType(method.ResultAST)
		for _, result := range method.ResultFieldsAST {
			addSpan(result.Span())
			visitType(result.Type)
		}
		visitBlock(method.BodyAST)
	}
	for _, declaration := range file.Decls {
		addSpan(declaration.Span())
		switch value := declaration.(type) {
		case *compiler.FunctionDecl:
			visitMethod(&value.Method)
		case *compiler.ClassDecl:
			for _, field := range value.Fields {
				addSpan(field.Span())
				visitType(field.TypeAST)
			}
			for index := range value.Methods {
				visitMethod(&value.Methods[index])
			}
		case *compiler.ExtendDecl:
			for _, target := range value.TargetAST {
				visitType(target)
			}
			for index := range value.Methods {
				visitMethod(&value.Methods[index])
			}
		case *compiler.ValueDecl:
			visitType(value.Type)
			for _, expression := range value.Values {
				visitExpr(expression)
			}
		case *compiler.EnumDecl:
			visitType(value.BackingTypeAST)
			for _, member := range value.Members {
				visitExpr(member.ValueAST)
			}
		case *compiler.MixedDecl:
			for _, function := range value.Functions {
				if function != nil {
					visitMethod(&function.Method)
				}
			}
		}
	}
	return ranges
}
