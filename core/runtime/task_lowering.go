package runtime

import "gopkg.d7z.net/go-mini/core/ast"

func (e *Executor) tasksForStmt(stmt ast.Stmt, data interface{}) []Task {
	if tasks, ok := e.lowerStmtTasks(stmt, data); ok {
		return tasks
	}
	return []Task{{Op: OpExec, Node: stmt, Data: data}}
}

func (e *Executor) tasksForExpr(expr ast.Expr) []Task {
	if tasks, ok := e.lowerExprTasks(expr); ok {
		return tasks
	}
	return []Task{{Op: OpEval, Node: expr}}
}

func (e *Executor) tasksForLHS(expr ast.Expr) []Task {
	if tasks, ok := e.lowerLHSTasks(expr); ok {
		return tasks
	}
	return []Task{{Op: OpEvalLHS, Node: expr}}
}

func (e *Executor) lowerStmtTasks(stmt ast.Stmt, data interface{}) ([]Task, bool) {
	switch n := stmt.(type) {
	case nil:
		return nil, true
	case *ast.BlockStmt:
		out := make([]Task, 0)
		if !n.Inner {
			out = append(out, Task{Op: OpScopeExit})
		}
		for i := len(n.Children) - 1; i >= 0; i-- {
			out = append(out, e.tasksForStmt(n.Children[i], data)...)
		}
		if !n.Inner {
			out = append(out, Task{Op: OpScopeEnter, Data: "block"})
		}
		return out, true
	case *ast.GenDeclStmt:
		return []Task{{
			Op: OpDeclareVar,
			Data: &DeclareVarData{
				Name: string(n.Name),
				Kind: n.Kind,
			},
		}}, true
	case *ast.AssignmentStmt:
		out := []Task{{Op: OpAssign}}
		if v, ok := data.(*Var); ok {
			out = append(out, Task{Op: OpPush, Data: v})
			out = append(out, e.tasksForLHS(n.LHS)...)
			return out, true
		}
		out = append(out, e.tasksForExpr(n.Value)...)
		out = append(out, e.tasksForLHS(n.LHS)...)
		return out, true
	case *ast.MultiAssignmentStmt:
		out := []Task{{Op: OpMultiAssign, Data: len(n.LHS)}}
		out = append(out, e.tasksForExpr(n.Value)...)
		for i := len(n.LHS) - 1; i >= 0; i-- {
			out = append(out, e.tasksForLHS(n.LHS[i])...)
		}
		return out, true
	case *ast.IncDecStmt:
		out := []Task{{Op: OpIncDec, Data: string(n.Operator)}}
		out = append(out, e.tasksForLHS(n.Operand)...)
		return out, true
	case *ast.ReturnStmt:
		out := []Task{{Op: OpReturn, Data: len(n.Results)}}
		for i := len(n.Results) - 1; i >= 0; i-- {
			out = append(out, e.tasksForExpr(n.Results[i])...)
		}
		return out, true
	case *ast.InterruptStmt:
		return []Task{{Op: OpInterrupt, Data: n.InterruptType}}, true
	case *ast.IfStmt:
		branch := &BranchData{
			Then: e.tasksForStmt(n.Body, nil),
		}
		if n.ElseBody != nil {
			branch.Else = e.tasksForStmt(n.ElseBody, nil)
		}
		out := []Task{{Op: OpBranchIf, Data: branch}}
		out = append(out, e.tasksForExpr(n.Cond)...)
		return out, true
	case *ast.ExpressionStmt:
		out := make([]Task, 0)
		if n.X != nil && !n.GetBase().Type.IsVoid() {
			out = append(out, Task{Op: OpPop})
		}
		out = append(out, e.tasksForExpr(n.X)...)
		return out, true
	case *ast.CallExprStmt:
		out := make([]Task, 0)
		if !n.GetBase().Type.IsVoid() {
			out = append(out, Task{Op: OpPop})
		}
		out = append(out, e.tasksForExpr(n)...)
		return out, true
	default:
		return nil, false
	}
}

func (e *Executor) lowerExprTasks(expr ast.Expr) ([]Task, bool) {
	switch n := expr.(type) {
	case nil:
		return []Task{{Op: OpPush}}, true
	case *ast.LiteralExpr:
		val, err := e.evalLiteralDirect(n)
		if err != nil {
			return nil, false
		}
		return []Task{{Op: OpPush, Data: val}}, true
	case *ast.IdentifierExpr:
		return []Task{{Op: OpLoadVar, Data: string(n.Name)}}, true
	case *ast.ConstRefExpr:
		if e.program != nil {
			if val, ok := e.program.Constants[string(n.Name)]; ok {
				return []Task{{Op: OpPush, Data: e.evalLiteralToVar(val)}}, true
			}
		}
		if val, ok := e.consts[string(n.Name)]; ok {
			return []Task{{Op: OpPush, Data: e.evalLiteralToVar(val)}}, true
		}
		return nil, false
	case *ast.UnaryExpr:
		out := []Task{{Op: OpApplyUnary, Data: string(n.Operator)}}
		out = append(out, e.tasksForExpr(n.Operand)...)
		return out, true
	case *ast.BinaryExpr:
		op := string(n.Operator)
		if op == "&&" || op == "And" || op == "||" || op == "Or" {
			out := []Task{{Op: OpJumpIf, Data: &JumpData{
				Operator: op,
				Right:    e.tasksForExpr(n.Right),
			}}}
			out = append(out, e.tasksForExpr(n.Left)...)
			return out, true
		}
		out := []Task{{Op: OpApplyBinary, Data: op}}
		out = append(out, e.tasksForExpr(n.Right)...)
		out = append(out, e.tasksForExpr(n.Left)...)
		return out, true
	case *ast.IndexExpr:
		out := []Task{{Op: OpIndex, Data: &IndexData{
			Multi:      n.Multi,
			ResultType: n.GetBase().Type,
		}}}
		out = append(out, e.tasksForExpr(n.Index)...)
		out = append(out, e.tasksForExpr(n.Object)...)
		return out, true
	case *ast.MemberExpr:
		out := []Task{{Op: OpMember, Data: string(n.Property)}}
		out = append(out, e.tasksForExpr(n.Object)...)
		return out, true
	case *ast.TypeAssertExpr:
		out := []Task{{Op: OpAssert, Data: &AssertData{
			TargetType: n.Type,
			Multi:      n.Multi,
			ResultType: n.GetBase().Type,
		}}}
		out = append(out, e.tasksForExpr(n.X)...)
		return out, true
	case *ast.CompositeExpr:
		entries := make([]CompositeEntryData, len(n.Values))
		out := []Task{{Op: OpComposite, Data: &CompositeData{
			Type:    n.Type,
			Entries: entries,
		}}}
		for i := len(n.Values) - 1; i >= 0; i-- {
			v := n.Values[i]
			if ident, ok := v.Key.(*ast.IdentifierExpr); ok {
				entries[i].IdentKey = string(ident.Name)
			} else if v.Key != nil {
				entries[i].HasExprKey = true
			}
			out = append(out, e.tasksForExpr(v.Value)...)
			if entries[i].HasExprKey {
				out = append(out, e.tasksForExpr(v.Key)...)
			}
		}
		return out, true
	case *ast.SliceExpr:
		out := []Task{{Op: OpSlice, Data: &SliceData{
			HasLow:  n.Low != nil,
			HasHigh: n.High != nil,
		}}}
		if n.High != nil {
			out = append(out, e.tasksForExpr(n.High)...)
		}
		if n.Low != nil {
			out = append(out, e.tasksForExpr(n.Low)...)
		}
		out = append(out, e.tasksForExpr(n.X)...)
		return out, true
	case *ast.StarExpr:
		out := []Task{{Op: OpApplyUnary, Data: "Dereference"}}
		out = append(out, e.tasksForExpr(n.X)...)
		return out, true
	case *ast.CallExprStmt:
		data := &CallData{
			Mode:     CallByValue,
			ArgCount: len(n.Args),
			Ellipsis: n.Ellipsis,
		}
		switch fn := n.Func.(type) {
		case *ast.IdentifierExpr:
			data.Mode = CallByName
			data.Name = string(fn.Name)
		case *ast.ConstRefExpr:
			data.Mode = CallByName
			data.Name = string(fn.Name)
		case *ast.MemberExpr:
			data.Mode = CallByMember
			data.Name = string(fn.Property)
		}

		out := []Task{{Op: OpCall, Data: data}}
		for i := len(n.Args) - 1; i >= 0; i-- {
			out = append(out, e.tasksForExpr(n.Args[i])...)
		}
		if member, ok := n.Func.(*ast.MemberExpr); ok {
			out = append(out, e.tasksForExpr(member.Object)...)
		} else if data.Mode == CallByValue {
			out = append(out, e.tasksForExpr(n.Func)...)
		}
		return out, true
	case *ast.ImportExpr:
		return []Task{{Op: OpImportInit, Data: &ImportInitData{Path: n.Path}}}, true
	default:
		return nil, false
	}
}

func (e *Executor) lowerLHSTasks(lhsExpr ast.Expr) ([]Task, bool) {
	switch lhs := lhsExpr.(type) {
	case *ast.IdentifierExpr:
		return []Task{{
			Op: OpEvalLHS,
			Data: &LHSData{
				Kind: LHSTypeEnv,
				Name: string(lhs.Name),
			},
		}}, true
	case *ast.IndexExpr:
		out := []Task{{Op: OpEvalLHS, Data: &LHSData{Kind: LHSTypeIndex}}}
		out = append(out, e.tasksForExpr(lhs.Index)...)
		out = append(out, e.tasksForExpr(lhs.Object)...)
		return out, true
	case *ast.MemberExpr:
		out := []Task{{Op: OpEvalLHS, Data: &LHSData{
			Kind:     LHSTypeMember,
			Property: string(lhs.Property),
		}}}
		out = append(out, e.tasksForExpr(lhs.Object)...)
		return out, true
	case *ast.StarExpr:
		out := []Task{{Op: OpEvalLHS, Data: &LHSData{Kind: LHSTypeStar}}}
		out = append(out, e.tasksForExpr(lhs.X)...)
		return out, true
	default:
		return nil, false
	}
}
