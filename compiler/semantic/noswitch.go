package semantic

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/types"
)

type noSwitchValidator struct {
	analyzer *analyzer
}

func (a *analyzer) validateNoSwitchFunction(decl *ast.FuncDecl) {
	if decl == nil || !decl.NoSwitch {
		return
	}
	validator := noSwitchValidator{analyzer: a}
	validator.checkBlock(&decl.Body)
}

func (v noSwitchValidator) reject(message string, span source.Span) {
	v.analyzer.addDiagnostic("semantic.noswitch.operation", message, span)
}

func (v noSwitchValidator) checkBlock(block *ast.BlockStmt) {
	if block == nil {
		return
	}
	for index := range block.Stmts {
		v.checkStatement(&block.Stmts[index])
	}
}

func (v noSwitchValidator) checkStatement(stmt *ast.Statement) {
	if stmt == nil {
		return
	}
	if _, overloaded := v.analyzer.info.Operators[stmt.NodeID]; overloaded {
		v.reject("operator overload is not permitted in a no-switch function", stmt.Span)
	}
	switch stmt.Kind {
	case ast.StmtGo:
		v.reject("go statement is not permitted in a no-switch function", stmt.Span)
		return
	case ast.StmtDefer:
		v.reject("defer statement is not permitted in a no-switch function", stmt.Span)
		return
	case ast.StmtSelect:
		v.reject("select statement is not permitted in a no-switch function", stmt.Span)
		return
	case ast.StmtSend:
		v.reject("channel send is not permitted in a no-switch function", stmt.Span)
		return
	case ast.StmtRange:
		if stmt.Range != nil && v.rangeCanSchedule(v.analyzer.info.Exprs[stmt.Range.NodeID].Type) {
			v.reject("blocking range is not permitted in a no-switch function", stmt.Span)
			return
		}
	}

	for index := range stmt.Decls {
		decl := &stmt.Decls[index]
		switch decl.Kind {
		case ast.DeclConst:
			for valueIndex := range decl.Const.Values {
				v.checkExpression(&decl.Const.Values[valueIndex])
			}
		case ast.DeclVar:
			for valueIndex := range decl.Var.Values {
				v.checkExpression(&decl.Var.Values[valueIndex])
			}
		}
	}
	v.checkExpression(stmt.Expr)
	for index := range stmt.Left {
		v.checkExpression(&stmt.Left[index])
	}
	for index := range stmt.Right {
		v.checkExpression(&stmt.Right[index])
	}
	v.checkBlock(&stmt.Body)
	v.checkStatement(stmt.Init)
	v.checkExpression(stmt.Cond)
	v.checkStatement(stmt.Post)
	v.checkStatement(stmt.Else)
	v.checkExpression(stmt.Key)
	v.checkExpression(stmt.Value)
	v.checkExpression(stmt.Range)
	for index := range stmt.Cases {
		clause := &stmt.Cases[index]
		for valueIndex := range clause.Values {
			v.checkExpression(&clause.Values[valueIndex])
		}
		v.checkStatement(clause.Comm)
		v.checkBlock(&clause.Body)
	}
	for index := range stmt.Results {
		v.checkExpression(&stmt.Results[index])
	}
}

func (v noSwitchValidator) rangeCanSchedule(ref types.TypeRef) bool {
	shape := v.analyzer.info.Relations.View(ref).Shape()
	if shape == types.Waitable || shape == types.Function {
		return true
	}
	if ref.Kind != types.TypeParameter {
		return false
	}
	terms, ok := v.analyzer.typeParameterTerms(ref)
	if !ok {
		return false
	}
	for _, term := range terms {
		shape = v.analyzer.info.Relations.View(term).Shape()
		if shape == types.Waitable || shape == types.Function {
			return true
		}
	}
	return false
}

func (v noSwitchValidator) checkExpression(expr *ast.Expression) {
	if expr == nil {
		return
	}
	info := v.analyzer.info
	if _, overloaded := info.Operators[expr.NodeID]; overloaded {
		v.reject("operator overload is not permitted in a no-switch function", expr.Span)
	}
	switch expr.Kind {
	case ast.ExprFunc:
		return
	case ast.ExprIdent:
		if object, ok := info.Object(info.Uses[expr.NodeID]); ok && object.ModulePath != "" && object.ExportName != "" {
			if _, constant := info.Constants[expr.NodeID]; !constant {
				v.reject("package value access is not permitted in a no-switch function", expr.Span)
			}
		}
	case ast.ExprReceive:
		v.reject("channel receive is not permitted in a no-switch function", expr.Span)
	case ast.ExprCall:
		call := info.Calls[expr.NodeID]
		switch call.Kind {
		case CallConversion:
		case CallBuiltin:
			object, ok := info.Object(call.Callee)
			if !ok {
				v.reject("unresolved builtin call is not permitted in a no-switch function", expr.Span)
			} else if object.Name == "print" || object.Name == "println" {
				v.reject("print and println may call host output", expr.Span)
			} else if object.Name == "min" || object.Name == "max" {
				if _, constant := info.Constants[expr.NodeID]; !constant {
					v.reject("builtin call requires a generated helper in a no-switch function", expr.Span)
				}
			}
		default:
			v.reject("function call is not permitted in a no-switch function", expr.Span)
		}
		for index := range expr.Args {
			v.checkExpression(&expr.Args[index])
		}
		return
	case ast.ExprSelector:
		if selection, ok := info.Selections[expr.NodeID]; ok && selection.Kind == SelectionPackageMember {
			if _, constant := info.Constants[expr.NodeID]; !constant {
				v.reject("package value access is not permitted in a no-switch function", expr.Span)
			}
		}
	}

	v.checkExpression(expr.Left)
	v.checkExpression(expr.Right)
	v.checkExpression(expr.Operand)
	v.checkExpression(expr.Callee)
	for index := range expr.Args {
		v.checkExpression(&expr.Args[index])
	}
	v.checkExpression(expr.Index)
	v.checkExpression(expr.Start)
	v.checkExpression(expr.End)
	v.checkExpression(expr.Max)
	for index := range expr.Elements {
		v.checkExpression(&expr.Elements[index])
	}
	for index := range expr.Entries {
		v.checkExpression(expr.Entries[index].Key)
		v.checkExpression(&expr.Entries[index].Value)
	}
	for index := range expr.Items {
		v.checkExpression(expr.Items[index].Key)
		v.checkExpression(&expr.Items[index].Value)
	}
}
