package runtime

import (
	"fmt"

	"gopkg.d7z.net/go-mini/core/ast"
)

func (e *Executor) setSource(tasks []Task, node ast.Node) []Task {
	if node == nil {
		return tasks
	}
	base := node.GetBase()
	_, isStmt := node.(ast.Stmt)
	ref := &SourceRef{
		ID:          base.ID,
		Meta:        base.Meta,
		IsStmtStart: isStmt,
	}
	if base.Loc != nil {
		ref.File = base.Loc.F
		ref.Line = base.Loc.L
		ref.Col = base.Loc.C
	}
	for i := range tasks {
		if tasks[i].Source == nil {
			tasks[i].Source = ref
		}
	}
	return tasks
}

func (e *Executor) TasksForStmt(stmt ast.Stmt) []Task {
	return e.tasksForStmt(stmt, nil)
}

func (e *Executor) tasksForStmt(stmt ast.Stmt, data interface{}) []Task {
	if tasks, ok := e.lowerStmtTasks(stmt, data); ok {
		res := e.setSource(tasks, stmt)
		// Prepend OpLineStep for debugging
		if stmt != nil {
			lineStep := Task{Op: OpLineStep}
			lineStep = e.setSource([]Task{lineStep}, stmt)[0]
			res = append(res, lineStep)
		}
		return res
	}
	panic(fmt.Sprintf("runtime lowering missing for stmt %T", stmt))
}

func (e *Executor) tasksForExpr(expr ast.Expr) []Task {
	if tasks, ok := e.lowerExprTasks(expr); ok {
		return e.setSource(tasks, expr)
	}
	panic(fmt.Sprintf("runtime lowering missing for expr %T", expr))
}

func (e *Executor) tasksForLHS(expr ast.Expr) []Task {
	if tasks, ok := e.lowerLHSTasks(expr); ok {
		return e.setSource(tasks, expr)
	}
	panic(fmt.Sprintf("runtime lowering missing for lhs %T", expr))
}

func (e *Executor) lowerStmtTasks(stmt ast.Stmt, data interface{}) ([]Task, bool) {
	switch n := stmt.(type) {
	case nil:
		return nil, true
	case *ast.BadStmt:
		return nil, false // Will be handled by the caller or panic
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
	case *ast.ForStmt:
		bodyStmt, ok := n.Body.(ast.Stmt)
		if !ok {
			return nil, false
		}
		loop := &ForData{
			Body: e.tasksForStmt(bodyStmt, nil),
		}
		if n.Cond != nil {
			loop.Cond = e.tasksForExpr(n.Cond)
		}
		if n.Update != nil {
			update, ok := n.Update.(ast.Stmt)
			if !ok {
				return nil, false
			}
			loop.Update = e.tasksForStmt(update, nil)
		}
		out := []Task{
			{Op: OpScopeExit},
			{Op: OpLoopBoundary, Data: loop},
		}
		if n.Init != nil {
			initStmt, ok := n.Init.(ast.Stmt)
			if !ok {
				return nil, false
			}
			out = append(out, e.tasksForStmt(initStmt, nil)...)
		}
		out = append(out, Task{Op: OpScopeEnter, Data: "for"})
		return out, true
	case *ast.RangeStmt:
		rData := &RangeData{
			Key:    string(n.Key),
			Value:  string(n.Value),
			Define: n.Define,
			Body:   e.tasksForStmt(n.Body, nil),
		}
		out := []Task{{Op: OpRangeInit, Data: rData}}
		out = append(out, e.tasksForExpr(n.X)...)
		return out, true
	case *ast.TryStmt:
		out := make([]Task, 0, 3)
		if n.Finally != nil {
			out = append(out, Task{Op: OpFinally, Data: &FinallyData{
				Body: e.tasksForStmt(n.Finally, nil),
			}})
		}
		if n.Catch != nil {
			out = append(out, Task{Op: OpCatchBoundary, Data: &CatchData{
				VarName: string(n.Catch.VarName),
				Body:    e.tasksForStmt(n.Catch.Body, nil),
			}})
		}
		out = append(out, e.tasksForStmt(n.Body, nil)...)
		return out, true
	case *ast.DeferStmt:
		call, ok := n.Call.(*ast.CallExprStmt)
		if !ok {
			return nil, false
		}
		return []Task{{Op: OpScheduleDefer, Data: &DeferData{
			Tasks:     e.tasksForExpr(call),
			PopResult: !call.GetBase().Type.IsVoid(),
		}}}, true
	case *ast.SwitchStmt:
		plan := &SwitchData{
			IsType:    n.IsType,
			HasTag:    n.Tag != nil,
			HasAssign: n.Assign != nil,
		}
		if n.Init != nil {
			plan.Init = e.tasksForStmt(n.Init, nil)
		}
		if n.Tag != nil {
			plan.Tag = e.tasksForExpr(n.Tag)
		}
		if n.Assign != nil {
			if n.IsType {
				switch assign := n.Assign.(type) {
				case *ast.AssignmentStmt:
					plan.AssignLHS = e.tasksForLHS(assign.LHS)
				case *ast.BlockStmt:
					var lhs ast.Expr
					for _, child := range assign.Children {
						if asg, ok := child.(*ast.AssignmentStmt); ok {
							lhs = asg.LHS
						}
					}
					if lhs == nil {
						return nil, false
					}
					plan.AssignLHS = e.tasksForLHS(lhs)
				default:
					return nil, false
				}
			}
		}
		for _, child := range n.Body.Children {
			clause, ok := child.(*ast.CaseClause)
			if !ok {
				return nil, false
			}
			if clause.List == nil {
				plan.DefaultBody = e.tasksForStmt(&ast.BlockStmt{Children: clause.Body, Inner: true}, nil)
				continue
			}
			caseData := SwitchCaseData{
				Body: e.tasksForStmt(&ast.BlockStmt{Children: clause.Body, Inner: true}, nil),
			}
			for _, expr := range clause.List {
				if n.IsType {
					var targetType ast.GoMiniType
					if id, ok := expr.(*ast.IdentifierExpr); ok {
						targetType = ast.GoMiniType(id.Name)
					} else {
						targetType = expr.GetBase().Type
					}
					caseData.TypeNames = append(caseData.TypeNames, targetType)
				} else {
					caseData.Exprs = append(caseData.Exprs, e.tasksForExpr(expr))
				}
			}
			plan.Cases = append(plan.Cases, caseData)
		}

		out := []Task{
			{Op: OpLoopBoundary, Data: plan},
			{Op: OpSwitchTag, Data: plan},
		}
		if n.Tag != nil {
			out = append(out, plan.Tag...)
		}
		if n.Init != nil {
			out = append(out, plan.Init...)
		}
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
	case *ast.ProgramStmt, *ast.FunctionStmt, *ast.StructStmt, *ast.InterfaceStmt:
		// Metadata nodes handled at initialization, not in execution path
		return nil, false
	default:
		return nil, false
	}
}

func (e *Executor) lowerExprTasks(expr ast.Expr) ([]Task, bool) {
	switch n := expr.(type) {
	case nil:
		return []Task{{Op: OpPush}}, true
	case *ast.BadExpr:
		return nil, false
	case *ast.LiteralExpr:
		val, err := e.evalLiteralDirect(n)
		if err != nil {
			return nil, false
		}
		return []Task{{Op: OpPush, Data: val}}, true
	case *ast.IdentifierExpr:
		return []Task{{Op: OpLoadVar, Data: string(n.Name)}}, true
	case *ast.ConstRefExpr:
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
	case *ast.FuncLitExpr:
		captures := make([]string, len(n.CaptureNames))
		copy(captures, n.CaptureNames)
		return []Task{{Op: OpMakeClosure, Data: &ClosureData{
			FunctionType: n.FunctionType,
			BodyTasks:    e.tasksForStmt(n.Body, nil),
			CaptureNames: captures,
		}}}, true
	default:
		return nil, false
	}
}

func (e *Executor) lowerLHSTasks(lhsExpr ast.Expr) ([]Task, bool) {
	switch lhs := lhsExpr.(type) {
	case nil:
		return []Task{{
			Op: OpEvalLHS,
			Data: &LHSData{
				Kind: LHSTypeNone,
			},
		}}, true
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
