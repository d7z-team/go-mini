package runtime

import (
	"fmt"
	"reflect"

	"gopkg.d7z.net/go-mini/core/ast"
)

var builtinSymbols = map[string]struct{}{
	"append":  {},
	"cap":     {},
	"close":   {},
	"complex": {},
	"copy":    {},
	"delete":  {},
	"imag":    {},
	"len":     {},
	"make":    {},
	"new":     {},
	"panic":   {},
	"print":   {},
	"println": {},
	"real":    {},
	"recover": {},
	"require": {},
}

type loweringFuncState struct {
	nextLocal   int
	nextUpvalue int
	captures    map[string]SymbolRef
	order       []SymbolRef
}

type loweringScope struct {
	parent   *loweringScope
	bindings map[string]SymbolRef
	fn       *loweringFuncState
	root     bool
}

func (e *Executor) newRootLoweringScope() *loweringScope {
	scope := &loweringScope{
		bindings: make(map[string]SymbolRef),
		root:     true,
	}
	for name := range e.globals {
		scope.bindings[string(name)] = SymbolRef{Name: string(name), Kind: SymbolGlobal, Slot: -1}
	}
	for name := range e.functions {
		scope.bindings[string(name)] = SymbolRef{Name: string(name), Kind: SymbolGlobal, Slot: -1}
	}
	for name := range builtinSymbols {
		scope.bindings[name] = SymbolRef{Name: name, Kind: SymbolBuiltin, Slot: -1}
	}
	return scope
}

func (s *loweringScope) childBlock() *loweringScope {
	return &loweringScope{
		parent:   s,
		bindings: make(map[string]SymbolRef),
		fn:       s.fn,
	}
}

func (s *loweringScope) childFunction() *loweringScope {
	return &loweringScope{
		parent:   s,
		bindings: make(map[string]SymbolRef),
		fn: &loweringFuncState{
			captures: make(map[string]SymbolRef),
		},
	}
}

func (s *loweringScope) declare(name string) SymbolRef {
	if existing, ok := s.bindings[name]; ok {
		return existing
	}
	if s.fn == nil {
		sym := SymbolRef{Name: name, Kind: SymbolGlobal, Slot: -1}
		s.bindings[name] = sym
		return sym
	}
	sym := SymbolRef{Name: name, Kind: SymbolLocal, Slot: s.fn.nextLocal}
	s.fn.nextLocal++
	s.bindings[name] = sym
	return sym
}

func (s *loweringScope) addBinding(sym SymbolRef) {
	s.bindings[sym.Name] = sym
}

func (s *loweringScope) resolve(name string) (SymbolRef, bool) {
	if sym, ok := s.bindings[name]; ok {
		return sym, true
	}
	if s.parent == nil {
		return SymbolRef{}, false
	}
	parentSym, ok := s.parent.resolve(name)
	if !ok {
		return SymbolRef{}, false
	}
	if s.fn == nil || s.parent.fn == s.fn {
		return parentSym, true
	}
	if parentSym.Kind == SymbolLocal || parentSym.Kind == SymbolUpvalue {
		if captured, ok := s.fn.captures[name]; ok {
			return captured, true
		}
		captured := SymbolRef{Name: name, Kind: SymbolUpvalue, Slot: s.fn.nextUpvalue}
		s.fn.nextUpvalue++
		s.fn.captures[name] = captured
		s.fn.order = append(s.fn.order, captured)
		s.bindings[name] = captured
		return captured, true
	}
	return parentSym, true
}

func (s *loweringScope) resolveOrImplicit(name string) SymbolRef {
	if sym, ok := s.resolve(name); ok {
		return sym
	}
	if _, ok := builtinSymbols[name]; ok {
		return SymbolRef{Name: name, Kind: SymbolBuiltin, Slot: -1}
	}
	return SymbolRef{Name: name, Kind: SymbolGlobal, Slot: -1}
}

func loadTasksForSymbol(sym SymbolRef) []Task {
	switch sym.Kind {
	case SymbolLocal:
		return []Task{{Op: OpLoadLocal, Data: sym}}
	case SymbolUpvalue:
		return []Task{{Op: OpLoadUpvalue, Data: sym}}
	default:
		return []Task{{Op: OpLoadVar, Data: &LoadVarData{Name: sym.Name, Sym: sym}}}
	}
}

func storeTasksForSymbol(sym SymbolRef) []Task {
	switch sym.Kind {
	case SymbolLocal:
		return []Task{{Op: OpStoreLocal, Data: sym}}
	case SymbolUpvalue:
		return []Task{{Op: OpStoreUpvalue, Data: sym}}
	default:
		return []Task{
			{Op: OpAssign},
			{Op: OpEvalLHS, Data: &LHSData{Kind: LHSTypeEnv, Name: sym.Name, Sym: sym}},
		}
	}
}

func predeclareInnerBlockBindings(stmt ast.Stmt, scope *loweringScope) {
	if stmt == nil || scope == nil {
		return
	}
	switch n := stmt.(type) {
	case *ast.BlockStmt:
		if n == nil || !n.Inner {
			return
		}
		for _, child := range n.Children {
			predeclareInnerBlockBindings(child, scope)
		}
	case *ast.GenDeclStmt:
		if n == nil {
			return
		}
		scope.declare(string(n.Name))
	}
}

func (e *Executor) setSource(tasks []Task, node ast.Node) []Task {
	if isNilNode(node) {
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

func isNilNode(node ast.Node) bool {
	if node == nil {
		return true
	}
	value := reflect.ValueOf(node)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (e *Executor) TasksForStmt(stmt ast.Stmt) []Task {
	return e.tasksForStmtInScope(stmt, nil, e.newRootLoweringScope())
}

func (e *Executor) tasksForStmt(stmt ast.Stmt, data interface{}) []Task {
	return e.tasksForStmtInScope(stmt, data, e.newRootLoweringScope())
}

func (e *Executor) tasksForStmtInScope(stmt ast.Stmt, data interface{}, scope *loweringScope) []Task {
	if tasks, ok := e.lowerStmtTasks(stmt, data, scope); ok {
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
	return e.tasksForExprInScope(expr, e.newRootLoweringScope())
}

func (e *Executor) tasksForExprInScope(expr ast.Expr, scope *loweringScope) []Task {
	if tasks, ok := e.lowerExprTasks(expr, scope); ok {
		return e.setSource(tasks, expr)
	}
	panic(fmt.Sprintf("runtime lowering missing for expr %T", expr))
}

func (e *Executor) tasksForLHS(expr ast.Expr) []Task {
	return e.tasksForLHSInScope(expr, e.newRootLoweringScope())
}

func (e *Executor) tasksForLHSInScope(expr ast.Expr, scope *loweringScope) []Task {
	if tasks, ok := e.lowerLHSTasks(expr, scope); ok {
		return e.setSource(tasks, expr)
	}
	panic(fmt.Sprintf("runtime lowering missing for lhs %T", expr))
}

func (e *Executor) lowerStmtTasks(stmt ast.Stmt, data interface{}, scope *loweringScope) ([]Task, bool) {
	switch n := stmt.(type) {
	case nil:
		return nil, true
	case *ast.BadStmt:
		return nil, false // Will be handled by the caller or panic
	case *ast.BlockStmt:
		if n == nil {
			return nil, true
		}
		childScope := scope
		if !n.Inner {
			childScope = scope.childBlock()
		}
		for _, child := range n.Children {
			switch stmt := child.(type) {
			case *ast.GenDeclStmt:
				childScope.declare(string(stmt.Name))
			case *ast.BlockStmt:
				predeclareInnerBlockBindings(stmt, childScope)
			case *ast.RangeStmt:
				if stmt != nil && stmt.Define {
					if stmt.Key != "" {
						childScope.declare(string(stmt.Key))
					}
					if stmt.Value != "" {
						childScope.declare(string(stmt.Value))
					}
				}
			case *ast.TryStmt:
				if stmt != nil && stmt.Catch != nil && stmt.Catch.VarName != "" {
					childScope.declare(string(stmt.Catch.VarName))
				}
			}
		}
		out := make([]Task, 0)
		if !n.Inner {
			out = append(out, Task{Op: OpScopeExit})
		}
		for i := len(n.Children) - 1; i >= 0; i-- {
			out = append(out, e.tasksForStmtInScope(n.Children[i], data, childScope)...)
		}
		if !n.Inner {
			out = append(out, Task{Op: OpScopeEnter, Data: "block"})
		}
		return out, true
	case *ast.GenDeclStmt:
		if n == nil {
			return nil, true
		}
		sym := scope.declare(string(n.Name))
		return []Task{{
			Op: OpDeclareVar,
			Data: &DeclareVarData{
				Name: string(n.Name),
				Kind: n.Kind,
				Sym:  sym,
			},
		}}, true
	case *ast.AssignmentStmt:
		if n == nil {
			return nil, true
		}
		if _, ok := data.(*Var); !ok {
			if ident, ok := n.LHS.(*ast.IdentifierExpr); ok && ident != nil {
				sym := scope.resolveOrImplicit(string(ident.Name))
				switch sym.Kind {
				case SymbolLocal:
					out := []Task{{Op: OpStoreLocal, Data: sym}}
					out = append(out, e.tasksForExprInScope(n.Value, scope)...)
					return out, true
				case SymbolUpvalue:
					out := []Task{{Op: OpStoreUpvalue, Data: sym}}
					out = append(out, e.tasksForExprInScope(n.Value, scope)...)
					return out, true
				}
			}
		}
		out := []Task{{Op: OpAssign}}
		if v, ok := data.(*Var); ok {
			out = append(out, Task{Op: OpPush, Data: v})
			out = append(out, e.tasksForLHSInScope(n.LHS, scope)...)
			return out, true
		}
		out = append(out, e.tasksForExprInScope(n.Value, scope)...)
		out = append(out, e.tasksForLHSInScope(n.LHS, scope)...)
		return out, true
	case *ast.MultiAssignmentStmt:
		if n == nil {
			return nil, true
		}
		out := []Task{{Op: OpMultiAssign, Data: len(n.LHS)}}
		out = append(out, e.tasksForExprInScope(n.Value, scope)...)
		for i := len(n.LHS) - 1; i >= 0; i-- {
			out = append(out, e.tasksForLHSInScope(n.LHS[i], scope)...)
		}
		return out, true
	case *ast.IncDecStmt:
		if n == nil {
			return nil, true
		}
		out := []Task{{Op: OpIncDec, Data: string(n.Operator)}}
		out = append(out, e.tasksForLHSInScope(n.Operand, scope)...)
		return out, true
	case *ast.ReturnStmt:
		if n == nil {
			return nil, true
		}
		out := []Task{{Op: OpReturn, Data: len(n.Results)}}
		for i := len(n.Results) - 1; i >= 0; i-- {
			out = append(out, e.tasksForExprInScope(n.Results[i], scope)...)
		}
		return out, true
	case *ast.InterruptStmt:
		if n == nil {
			return nil, true
		}
		return []Task{{Op: OpInterrupt, Data: n.InterruptType}}, true
	case *ast.IfStmt:
		if n == nil {
			return nil, true
		}
		branch := &BranchData{
			Then: e.tasksForStmtInScope(n.Body, nil, scope.childBlock()),
		}
		if n.ElseBody != nil {
			branch.Else = e.tasksForStmtInScope(n.ElseBody, nil, scope.childBlock())
		}
		out := []Task{{Op: OpBranchIf, Data: branch}}
		out = append(out, e.tasksForExprInScope(n.Cond, scope)...)
		return out, true
	case *ast.ForStmt:
		if n == nil {
			return nil, true
		}
		loopScope := scope.childBlock()
		initTasks := make([]Task, 0)
		if n.Init != nil {
			initStmt, ok := n.Init.(ast.Stmt)
			if !ok {
				return nil, false
			}
			initTasks = e.tasksForStmtInScope(initStmt, nil, loopScope)
		}
		bodyScope := loopScope.childBlock()
		bodyStmt, ok := n.Body.(ast.Stmt)
		if !ok {
			return nil, false
		}
		copyIn := make([]Task, 0)
		copyBack := make([]Task, 0)
		for name, outerSym := range loopScope.bindings {
			if outerSym.Kind == SymbolLocal {
				innerSym := bodyScope.declare(name)
				copyIn = append(copyIn, storeTasksForSymbol(innerSym)...)
				copyIn = append(copyIn, loadTasksForSymbol(outerSym)...)
				copyBack = append(copyBack, storeTasksForSymbol(outerSym)...)
				copyBack = append(copyBack, loadTasksForSymbol(innerSym)...)
			}
		}
		loop := &ForData{
			Body: append(append(copyBack, e.tasksForStmtInScope(bodyStmt, nil, bodyScope)...), copyIn...),
		}
		if n.Cond != nil {
			loop.Cond = e.tasksForExprInScope(n.Cond, loopScope)
		}
		if n.Update != nil {
			update, ok := n.Update.(ast.Stmt)
			if !ok {
				return nil, false
			}
			loop.Update = e.tasksForStmtInScope(update, nil, loopScope)
		}
		out := []Task{
			{Op: OpScopeExit},
			{Op: OpLoopBoundary, Data: loop},
		}
		out = append(out, initTasks...)
		out = append(out, Task{Op: OpScopeEnter, Data: "for"})
		return out, true
	case *ast.RangeStmt:
		if n == nil {
			return nil, true
		}
		rangeScope := scope.childBlock()
		var keySym, valueSym SymbolRef
		if n.Key != "" {
			if n.Define {
				keySym = rangeScope.declare(string(n.Key))
			} else {
				keySym = scope.resolveOrImplicit(string(n.Key))
			}
		}
		if n.Value != "" {
			if n.Define {
				valueSym = rangeScope.declare(string(n.Value))
			} else {
				valueSym = scope.resolveOrImplicit(string(n.Value))
			}
		}
		rData := &RangeData{
			Key:    string(n.Key),
			Value:  string(n.Value),
			KeySym: keySym,
			ValSym: valueSym,
			Define: n.Define,
			Body:   e.tasksForStmtInScope(n.Body, nil, rangeScope),
		}
		out := []Task{{Op: OpRangeInit, Data: rData}}
		out = append(out, e.tasksForExprInScope(n.X, scope)...)
		return out, true
	case *ast.TryStmt:
		if n == nil {
			return nil, true
		}
		out := make([]Task, 0, 3)
		if n.Finally != nil {
			out = append(out, Task{Op: OpFinally, Data: &FinallyData{
				Body: e.tasksForStmt(n.Finally, nil),
			}})
		}
		if n.Catch != nil {
			catchScope := scope.childBlock()
			if n.Catch.VarName != "" {
				catchScope.declare(string(n.Catch.VarName))
			}
			out = append(out, Task{Op: OpCatchBoundary, Data: &CatchData{
				VarName: string(n.Catch.VarName),
				Sym:     catchScope.resolveOrImplicit(string(n.Catch.VarName)),
				Body:    e.tasksForStmtInScope(n.Catch.Body, nil, catchScope),
			}})
		}
		out = append(out, e.tasksForStmtInScope(n.Body, nil, scope.childBlock())...)
		return out, true
	case *ast.DeferStmt:
		if n == nil {
			return nil, true
		}
		call, ok := n.Call.(*ast.CallExprStmt)
		if !ok {
			return nil, false
		}
		return []Task{{Op: OpScheduleDefer, Data: &DeferData{
			Tasks:     e.tasksForExprInScope(call, scope),
			PopResult: !call.GetBase().Type.IsVoid(),
		}}}, true
	case *ast.SwitchStmt:
		if n == nil {
			return nil, true
		}
		plan := &SwitchData{
			IsType:    n.IsType,
			HasTag:    n.Tag != nil,
			HasAssign: n.Assign != nil,
		}
		if n.Init != nil {
			plan.Init = e.tasksForStmtInScope(n.Init, nil, scope.childBlock())
		}
		if n.Tag != nil {
			plan.Tag = e.tasksForExprInScope(n.Tag, scope)
		}
		if n.Assign != nil {
			if n.IsType {
				switch assign := n.Assign.(type) {
				case *ast.AssignmentStmt:
					plan.AssignLHS = e.tasksForLHSInScope(assign.LHS, scope)
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
					plan.AssignLHS = e.tasksForLHSInScope(lhs, scope)
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
				plan.DefaultBody = e.tasksForStmtInScope(&ast.BlockStmt{Children: clause.Body, Inner: true}, nil, scope.childBlock())
				continue
			}
			caseData := SwitchCaseData{
				Body: e.tasksForStmtInScope(&ast.BlockStmt{Children: clause.Body, Inner: true}, nil, scope.childBlock()),
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
					caseData.Exprs = append(caseData.Exprs, e.tasksForExprInScope(expr, scope))
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
		if n == nil {
			return nil, true
		}
		out := make([]Task, 0)
		if n.X != nil && !n.GetBase().Type.IsVoid() {
			out = append(out, Task{Op: OpPop})
		}
		out = append(out, e.tasksForExprInScope(n.X, scope)...)
		return out, true
	case *ast.CallExprStmt:
		if n == nil {
			return nil, true
		}
		out := make([]Task, 0)
		if !n.GetBase().Type.IsVoid() {
			out = append(out, Task{Op: OpPop})
		}
		out = append(out, e.tasksForExprInScope(n, scope)...)
		return out, true
	case *ast.ProgramStmt, *ast.FunctionStmt, *ast.StructStmt, *ast.InterfaceStmt:
		// Metadata nodes handled at initialization, not in execution path
		return nil, false
	default:
		return nil, false
	}
}

func (e *Executor) lowerExprTasks(expr ast.Expr, scope *loweringScope) ([]Task, bool) {
	switch n := expr.(type) {
	case nil:
		return []Task{{Op: OpPush}}, true
	case *ast.BadExpr:
		return nil, false
	case *ast.LiteralExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		val, err := e.evalLiteralDirect(n)
		if err != nil {
			return nil, false
		}
		return []Task{{Op: OpPush, Data: val}}, true
	case *ast.IdentifierExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		sym := scope.resolveOrImplicit(string(n.Name))
		switch sym.Kind {
		case SymbolLocal:
			return []Task{{Op: OpLoadLocal, Data: sym}}, true
		case SymbolUpvalue:
			return []Task{{Op: OpLoadUpvalue, Data: sym}}, true
		default:
			return []Task{{Op: OpLoadVar, Data: &LoadVarData{Name: string(n.Name), Sym: sym}}}, true
		}
	case *ast.ConstRefExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		if val, ok := e.consts[string(n.Name)]; ok {
			return []Task{{Op: OpPush, Data: e.evalLiteralToVar(val)}}, true
		}
		return nil, false
	case *ast.UnaryExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		out := []Task{{Op: OpApplyUnary, Data: string(n.Operator)}}
		out = append(out, e.tasksForExprInScope(n.Operand, scope)...)
		return out, true
	case *ast.BinaryExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		op := string(n.Operator)
		if op == "&&" || op == "And" || op == "||" || op == "Or" {
			out := []Task{{Op: OpJumpIf, Data: &JumpData{
				Operator: op,
				Right:    e.tasksForExprInScope(n.Right, scope),
			}}}
			out = append(out, e.tasksForExprInScope(n.Left, scope)...)
			return out, true
		}
		out := []Task{{Op: OpApplyBinary, Data: op}}
		out = append(out, e.tasksForExprInScope(n.Right, scope)...)
		out = append(out, e.tasksForExprInScope(n.Left, scope)...)
		return out, true
	case *ast.IndexExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		out := []Task{{Op: OpIndex, Data: &IndexData{
			Multi:      n.Multi,
			ResultType: n.GetBase().Type,
		}}}
		out = append(out, e.tasksForExprInScope(n.Index, scope)...)
		out = append(out, e.tasksForExprInScope(n.Object, scope)...)
		return out, true
	case *ast.MemberExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		out := []Task{{Op: OpMember, Data: string(n.Property)}}
		out = append(out, e.tasksForExprInScope(n.Object, scope)...)
		return out, true
	case *ast.TypeAssertExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		out := []Task{{Op: OpAssert, Data: &AssertData{
			TargetType: n.Type,
			Multi:      n.Multi,
			ResultType: n.GetBase().Type,
		}}}
		out = append(out, e.tasksForExprInScope(n.X, scope)...)
		return out, true
	case *ast.CompositeExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
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
			out = append(out, e.tasksForExprInScope(v.Value, scope)...)
			if entries[i].HasExprKey {
				out = append(out, e.tasksForExprInScope(v.Key, scope)...)
			}
		}
		return out, true
	case *ast.SliceExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		out := []Task{{Op: OpSlice, Data: &SliceData{
			HasLow:  n.Low != nil,
			HasHigh: n.High != nil,
		}}}
		if n.High != nil {
			out = append(out, e.tasksForExprInScope(n.High, scope)...)
		}
		if n.Low != nil {
			out = append(out, e.tasksForExprInScope(n.Low, scope)...)
		}
		out = append(out, e.tasksForExprInScope(n.X, scope)...)
		return out, true
	case *ast.StarExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		out := []Task{{Op: OpApplyUnary, Data: "Dereference"}}
		out = append(out, e.tasksForExprInScope(n.X, scope)...)
		return out, true
	case *ast.CallExprStmt:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		data := &CallData{
			Mode:          CallByValue,
			ArgCount:      len(n.Args),
			Ellipsis:      n.Ellipsis,
			CaptureArgLHS: true,
		}
		switch fn := n.Func.(type) {
		case *ast.IdentifierExpr:
			data.Mode = CallByName
			data.Name = string(fn.Name)
			data.Sym = scope.resolveOrImplicit(data.Name)
		case *ast.ConstRefExpr:
			data.Mode = CallByName
			data.Name = string(fn.Name)
			data.Sym = scope.resolveOrImplicit(data.Name)
		case *ast.MemberExpr:
			data.Mode = CallByMember
			data.Name = string(fn.Property)
		}

		out := []Task{{Op: OpCall, Data: data}}
		for i := len(n.Args) - 1; i >= 0; i-- {
			out = append(out, e.tasksForCallArgInScope(n.Args[i], scope)...)
		}
		if member, ok := n.Func.(*ast.MemberExpr); ok {
			out = append(out, e.tasksForExprInScope(member.Object, scope)...)
		} else if data.Mode == CallByValue {
			out = append(out, e.tasksForExprInScope(n.Func, scope)...)
		}
		return out, true
	case *ast.ImportExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		return []Task{{Op: OpImportInit, Data: &ImportInitData{Path: n.Path}}}, true
	case *ast.FuncLitExpr:
		if n == nil {
			return []Task{{Op: OpPush}}, true
		}
		fnScope := scope.childFunction()
		for _, p := range n.Params {
			fnScope.declare(string(p.Name))
		}
		captures := make([]string, len(n.CaptureNames))
		copy(captures, n.CaptureNames)
		return []Task{{Op: OpMakeClosure, Data: &ClosureData{
			FunctionType: n.FunctionType,
			BodyTasks:    e.tasksForStmtInScope(n.Body, nil, fnScope),
			CaptureNames: captures,
			CaptureRefs:  append([]SymbolRef(nil), fnScope.fn.order...),
		}}}, true
	default:
		return nil, false
	}
}

func (e *Executor) tasksForCallArgInScope(expr ast.Expr, scope *loweringScope) []Task {
	lhsTasks, ok := e.lowerLHSTasks(expr, scope)
	if !ok {
		lhsTasks = []Task{{Op: OpEvalLHS, Data: &LHSData{Kind: LHSTypeNone}}}
	} else {
		lhsTasks = e.setSource(lhsTasks, expr)
	}
	exprTasks := e.tasksForExprInScope(expr, scope)
	out := make([]Task, 0, len(lhsTasks)+len(exprTasks))
	out = append(out, lhsTasks...)
	out = append(out, exprTasks...)
	return out
}

func (e *Executor) lowerLHSTasks(lhsExpr ast.Expr, scope *loweringScope) ([]Task, bool) {
	switch lhs := lhsExpr.(type) {
	case nil:
		return []Task{{
			Op: OpEvalLHS,
			Data: &LHSData{
				Kind: LHSTypeNone,
			},
		}}, true
	case *ast.IdentifierExpr:
		if lhs == nil {
			return []Task{{
				Op: OpEvalLHS,
				Data: &LHSData{
					Kind: LHSTypeNone,
				},
			}}, true
		}
		return []Task{{
			Op: OpEvalLHS,
			Data: &LHSData{
				Kind: LHSTypeEnv,
				Name: string(lhs.Name),
				Sym:  scope.resolveOrImplicit(string(lhs.Name)),
			},
		}}, true
	case *ast.IndexExpr:
		if lhs == nil {
			return []Task{{
				Op: OpEvalLHS,
				Data: &LHSData{
					Kind: LHSTypeNone,
				},
			}}, true
		}
		out := []Task{{Op: OpEvalLHS, Data: &LHSData{Kind: LHSTypeIndex}}}
		out = append(out, e.tasksForExprInScope(lhs.Index, scope)...)
		out = append(out, e.tasksForExprInScope(lhs.Object, scope)...)
		return out, true
	case *ast.MemberExpr:
		if lhs == nil {
			return []Task{{
				Op: OpEvalLHS,
				Data: &LHSData{
					Kind: LHSTypeNone,
				},
			}}, true
		}
		out := []Task{{Op: OpEvalLHS, Data: &LHSData{
			Kind:     LHSTypeMember,
			Property: string(lhs.Property),
		}}}
		out = append(out, e.tasksForExprInScope(lhs.Object, scope)...)
		return out, true
	case *ast.StarExpr:
		if lhs == nil {
			return []Task{{
				Op: OpEvalLHS,
				Data: &LHSData{
					Kind: LHSTypeNone,
				},
			}}, true
		}
		out := []Task{{Op: OpEvalLHS, Data: &LHSData{Kind: LHSTypeStar}}}
		out = append(out, e.tasksForExprInScope(lhs.X, scope)...)
		return out, true
	default:
		return nil, false
	}
}
