package lowering

import (
	"fmt"
	"strconv"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
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
	"real":    {},
	"recover": {},
	"require": {},
}

var fallbackAnyType = runtime.RuntimeType{Kind: runtime.RuntimeTypeAny, Raw: runtime.SpecAny}

type loweringFuncState struct {
	nextLocal   int
	nextUpvalue int
	captures    map[string]runtime.SymbolRef
	order       []runtime.SymbolRef
}

type loweringScope struct {
	parent   *loweringScope
	bindings map[string]runtime.SymbolRef
	fn       *loweringFuncState
	root     bool
}

func (b *builder) newRootLoweringScope() *loweringScope {
	scope := &loweringScope{
		bindings: make(map[string]runtime.SymbolRef),
		root:     true,
	}
	for name := range b.globals {
		scope.bindings[name] = runtime.SymbolRef{Name: name, Kind: runtime.SymbolGlobal, Slot: -1}
	}
	for name := range b.functions {
		scope.bindings[name] = runtime.SymbolRef{Name: name, Kind: runtime.SymbolGlobal, Slot: -1}
	}
	for name := range builtinSymbols {
		scope.bindings[name] = runtime.SymbolRef{Name: name, Kind: runtime.SymbolBuiltin, Slot: -1}
	}
	return scope
}

func (s *loweringScope) childBlock() *loweringScope {
	return &loweringScope{
		parent:   s,
		bindings: make(map[string]runtime.SymbolRef),
		fn:       s.fn,
	}
}

func (s *loweringScope) childFunction() *loweringScope {
	return &loweringScope{
		parent:   s,
		bindings: make(map[string]runtime.SymbolRef),
		fn: &loweringFuncState{
			captures: make(map[string]runtime.SymbolRef),
		},
	}
}

func (s *loweringScope) declare(name string) runtime.SymbolRef {
	if existing, ok := s.bindings[name]; ok {
		return existing
	}
	if s.fn == nil {
		sym := runtime.SymbolRef{Name: name, Kind: runtime.SymbolGlobal, Slot: -1}
		s.bindings[name] = sym
		return sym
	}
	sym := runtime.SymbolRef{Name: name, Kind: runtime.SymbolLocal, Slot: s.fn.nextLocal}
	s.fn.nextLocal++
	s.bindings[name] = sym
	return sym
}

func (s *loweringScope) declareParam(name string) {
	if s.fn == nil {
		s.declare(name)
		return
	}
	sym := runtime.SymbolRef{Name: name, Kind: runtime.SymbolLocal, Slot: s.fn.nextLocal}
	s.fn.nextLocal++
	if name != "" && name != "_" {
		s.bindings[name] = sym
	}
}

func (s *loweringScope) resolve(name string) (runtime.SymbolRef, bool) {
	if sym, ok := s.bindings[name]; ok {
		return sym, true
	}
	if s.parent == nil {
		return runtime.SymbolRef{}, false
	}
	parentSym, ok := s.parent.resolve(name)
	if !ok {
		return runtime.SymbolRef{}, false
	}
	if s.fn == nil || s.parent.fn == s.fn {
		return parentSym, true
	}
	if parentSym.Kind == runtime.SymbolLocal || parentSym.Kind == runtime.SymbolUpvalue {
		if captured, ok := s.fn.captures[name]; ok {
			return captured, true
		}
		captured := runtime.SymbolRef{Name: name, Kind: runtime.SymbolUpvalue, Slot: s.fn.nextUpvalue}
		s.fn.nextUpvalue++
		s.fn.captures[name] = captured
		s.fn.order = append(s.fn.order, captured)
		s.bindings[name] = captured
		return captured, true
	}
	return parentSym, true
}

func (s *loweringScope) resolveOrImplicit(name string) runtime.SymbolRef {
	if sym, ok := s.resolve(name); ok {
		return sym
	}
	if _, ok := builtinSymbols[name]; ok {
		return runtime.SymbolRef{Name: name, Kind: runtime.SymbolBuiltin, Slot: -1}
	}
	return runtime.SymbolRef{Name: name, Kind: runtime.SymbolGlobal, Slot: -1}
}

func loadTasksForSymbol(sym runtime.SymbolRef) []runtime.Task {
	switch sym.Kind {
	case runtime.SymbolLocal:
		return []runtime.Task{{Op: runtime.OpLoadLocal, Data: sym}}
	case runtime.SymbolUpvalue:
		return []runtime.Task{{Op: runtime.OpLoadUpvalue, Data: sym}}
	default:
		return []runtime.Task{{Op: runtime.OpLoadVar, Data: &runtime.LoadVarData{Name: sym.Name, Sym: sym}}}
	}
}

func storeTasksForSymbol(sym runtime.SymbolRef) []runtime.Task {
	switch sym.Kind {
	case runtime.SymbolLocal:
		return []runtime.Task{{Op: runtime.OpStoreLocal, Data: sym}}
	case runtime.SymbolUpvalue:
		return []runtime.Task{{Op: runtime.OpStoreUpvalue, Data: sym}}
	default:
		return []runtime.Task{
			{Op: runtime.OpAssign},
			{Op: runtime.OpEvalLHS, Data: &runtime.LHSData{Kind: runtime.LHSTypeEnv, Name: sym.Name, Sym: sym}},
		}
	}
}

func (b *builder) setSource(tasks []runtime.Task, node ast.Node) []runtime.Task {
	if isNilNode(node) {
		return tasks
	}
	base := node.GetBase()
	_, isStmt := node.(ast.Stmt)
	ref := &runtime.SourceRef{
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

func (b *builder) fail(op string, node ast.Node, err error) {
	if b.err != nil {
		return
	}
	lowerErr := &Error{Op: op, NodeType: fmt.Sprintf("%T", node), Err: err}
	if !isNilNode(node) {
		base := node.GetBase()
		lowerErr.Meta = base.Meta
		lowerErr.ID = base.ID
		if base.Loc != nil {
			lowerErr.File = base.Loc.F
			lowerErr.Line = base.Loc.L
			lowerErr.Col = base.Loc.C
		}
	}
	b.err = lowerErr
}

func (b *builder) unsupported(op string, node ast.Node) {
	b.fail(op, node, fmt.Errorf("runtime lowering missing for %s %T", op, node))
}

func (b *builder) runtimeType(spec ast.GoMiniType, node ast.Node, op string) runtime.RuntimeType {
	parsed, err := runtime.ParseRuntimeType(spec)
	if err != nil {
		b.fail(op, node, fmt.Errorf("invalid canonical type %q: %w", spec, err))
		return fallbackAnyType
	}
	return parsed
}

func isNilNode(node ast.Node) bool {
	return ast.IsNilNode(node)
}

func literalDirect(n *ast.LiteralExpr) (*runtime.Var, error) {
	switch n.Type {
	case ast.GoMiniType(runtime.SpecInt64):
		v, _ := strconv.ParseInt(n.Value, 10, 64)
		return runtime.NewInt(v), nil
	case ast.GoMiniType(runtime.SpecFloat64):
		v, _ := strconv.ParseFloat(n.Value, 64)
		return runtime.NewFloat(v), nil
	case ast.GoMiniType(runtime.SpecString):
		return runtime.NewString(n.Value), nil
	case ast.GoMiniType(runtime.SpecBool):
		return runtime.NewBool(n.Value == "true"), nil
	}
	return nil, fmt.Errorf("unknown literal %s", n.Type)
}

func (b *builder) constantPushTasks(name string) ([]runtime.Task, bool) {
	val, ok := b.consts[name]
	if !ok {
		return nil, false
	}
	return []runtime.Task{{Op: runtime.OpPush, Data: val.ToVar()}}, true
}

func parseTypedConstLiteral(val string, typ runtime.RuntimeType) (runtime.FFIConstValue, error) {
	switch {
	case typ.IsString():
		return runtime.ConstString(val), nil
	case typ.IsInt():
		v, err := strconv.ParseInt(val, 0, 64)
		if err != nil {
			return runtime.FFIConstValue{}, err
		}
		return runtime.ConstInt64(v), nil
	case typ.Raw == runtime.SpecFloat64:
		v, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return runtime.FFIConstValue{}, err
		}
		return runtime.ConstFloat64(v), nil
	case typ.IsBool():
		if val == "true" {
			return runtime.ConstBool(true), nil
		}
		if val == "false" {
			return runtime.ConstBool(false), nil
		}
		return runtime.FFIConstValue{}, fmt.Errorf("invalid bool literal %q", val)
	}
	return runtime.FFIConstValue{}, fmt.Errorf("unsupported constant type %s", typ.Raw)
}

func (b *builder) tasksForStmt(stmt ast.Stmt, data interface{}) []runtime.Task {
	return b.tasksForStmtInScope(stmt, data, b.newRootLoweringScope())
}

func (b *builder) buildStmtPlanWithScope(stmts []ast.Stmt, scope *loweringScope) []runtime.Task {
	if len(stmts) == 0 {
		return nil
	}
	plan := make([]runtime.Task, 0)
	for _, stmt := range stmts {
		if b.err != nil {
			return nil
		}
		plan = append(b.tasksForStmtInScope(stmt, nil, scope), plan...)
	}
	return plan
}

func (b *builder) tasksForStmtInScope(stmt ast.Stmt, data interface{}, scope *loweringScope) []runtime.Task {
	if b.err != nil {
		return nil
	}
	if tasks, ok := b.lowerStmtTasks(stmt, data, scope); ok {
		res := b.setSource(tasks, stmt)
		// Prepend runtime.OpLineStep for debugging
		if stmt != nil {
			lineStep := runtime.Task{Op: runtime.OpLineStep}
			lineStep = b.setSource([]runtime.Task{lineStep}, stmt)[0]
			res = append(res, lineStep)
		}
		return res
	}
	b.unsupported("stmt", stmt)
	return nil
}

func (b *builder) tasksForExprInScope(expr ast.Expr, scope *loweringScope) []runtime.Task {
	if b.err != nil {
		return nil
	}
	if tasks, ok := b.lowerExprTasks(expr, scope); ok {
		return b.setSource(tasks, expr)
	}
	b.unsupported("expr", expr)
	return nil
}

func (b *builder) tasksForLHSInScope(expr ast.Expr, scope *loweringScope) []runtime.Task {
	if b.err != nil {
		return nil
	}
	if tasks, ok := b.lowerLHSTasks(expr, scope); ok {
		return b.setSource(tasks, expr)
	}
	b.unsupported("lhs", expr)
	return nil
}

func (b *builder) lowerStmtTasks(stmt ast.Stmt, data interface{}, scope *loweringScope) ([]runtime.Task, bool) {
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
		body := make([]runtime.Task, 0)
		for _, child := range n.Children {
			body = append(b.tasksForStmtInScope(child, data, childScope), body...)
		}
		out := make([]runtime.Task, 0, len(body)+2)
		if !n.Inner {
			out = append(out, runtime.Task{Op: runtime.OpScopeExit})
		}
		out = append(out, body...)
		if !n.Inner {
			out = append(out, runtime.Task{Op: runtime.OpScopeEnter, Data: "block"})
		}
		return out, true
	case *ast.GenDeclStmt:
		if n == nil {
			return nil, true
		}
		mode := runtime.VarDeclInitPerBinding
		if len(n.Values) == 0 {
			mode = runtime.VarDeclInitZero
		} else if len(n.Values) == 1 && len(n.Bindings) > 1 {
			mode = runtime.VarDeclInitDestructure
		}
		data := &runtime.VarDeclData{
			Bindings:   make([]runtime.DeclareVarData, 0, len(n.Bindings)),
			ValueCount: len(n.Values),
			Mode:       mode,
		}
		valueTasks := make([]runtime.Task, 0)
		for i := len(n.Values) - 1; i >= 0; i-- {
			valueTasks = append(valueTasks, b.tasksForExprInScope(n.Values[i], scope)...)
		}
		for _, binding := range n.Bindings {
			name := string(binding.Name)
			sym := runtime.SymbolRef{Name: name, Kind: runtime.SymbolUnknown, Slot: -1}
			if name != "" && name != "_" {
				sym = scope.declare(name)
			}
			data.Bindings = append(data.Bindings, runtime.DeclareVarData{
				Name: name,
				Kind: b.runtimeType(binding.Kind, n, "declaration"),
				Sym:  sym,
			})
		}
		out := []runtime.Task{{Op: runtime.OpDeclareInitVars, Data: data}}
		out = append(out, valueTasks...)
		return out, true
	case *ast.AssignmentStmt:
		if n == nil {
			return nil, true
		}
		if n.Kind == ast.AssignDefine {
			rhsTasks := b.tasksForExprInScope(n.Value, scope)
			if ident, ok := n.LHS.(*ast.IdentifierExpr); ok && ident != nil {
				name := string(ident.Name)
				if name != "_" {
					if _, exists := scope.bindings[name]; !exists {
						scope.declare(name)
					}
				}
				sym := scope.resolveOrImplicit(name)
				switch sym.Kind {
				case runtime.SymbolLocal:
					out := []runtime.Task{{Op: runtime.OpStoreLocal, Data: sym}}
					out = append(out, rhsTasks...)
					return out, true
				case runtime.SymbolUpvalue:
					out := []runtime.Task{{Op: runtime.OpStoreUpvalue, Data: sym}}
					out = append(out, rhsTasks...)
					return out, true
				}
			}
			out := []runtime.Task{{Op: runtime.OpAssign}}
			out = append(out, rhsTasks...)
			out = append(out, b.tasksForLHSInScope(n.LHS, scope)...)
			return out, true
		}
		if _, ok := data.(*runtime.Var); !ok {
			if ident, ok := n.LHS.(*ast.IdentifierExpr); ok && ident != nil {
				sym := scope.resolveOrImplicit(string(ident.Name))
				switch sym.Kind {
				case runtime.SymbolLocal:
					out := []runtime.Task{{Op: runtime.OpStoreLocal, Data: sym}}
					out = append(out, b.tasksForExprInScope(n.Value, scope)...)
					return out, true
				case runtime.SymbolUpvalue:
					out := []runtime.Task{{Op: runtime.OpStoreUpvalue, Data: sym}}
					out = append(out, b.tasksForExprInScope(n.Value, scope)...)
					return out, true
				}
			}
		}
		out := []runtime.Task{{Op: runtime.OpAssign}}
		if v, ok := data.(*runtime.Var); ok {
			out = append(out, runtime.Task{Op: runtime.OpPush, Data: v})
			out = append(out, b.tasksForLHSInScope(n.LHS, scope)...)
			return out, true
		}
		out = append(out, b.tasksForExprInScope(n.Value, scope)...)
		out = append(out, b.tasksForLHSInScope(n.LHS, scope)...)
		return out, true
	case *ast.MultiAssignmentStmt:
		if n == nil {
			return nil, true
		}
		mode := runtime.MultiAssignPerBinding
		if len(n.Values) == 1 && len(n.LHS) > 1 {
			mode = runtime.MultiAssignDestructure
		}
		data := &runtime.MultiAssignData{
			LHSCount:   len(n.LHS),
			ValueCount: len(n.Values),
			Mode:       mode,
		}
		if n.Kind == ast.AssignDefine {
			valueTasks := make([]runtime.Task, 0)
			for i := len(n.Values) - 1; i >= 0; i-- {
				valueTasks = append(valueTasks, b.tasksForExprInScope(n.Values[i], scope)...)
			}
			for _, lhs := range n.LHS {
				ident, ok := lhs.(*ast.IdentifierExpr)
				if !ok || ident == nil || ident.Name == "_" {
					continue
				}
				name := string(ident.Name)
				if _, exists := scope.bindings[name]; !exists {
					scope.declare(name)
				}
			}
			out := []runtime.Task{{Op: runtime.OpMultiAssign, Data: data}}
			out = append(out, valueTasks...)
			for i := len(n.LHS) - 1; i >= 0; i-- {
				out = append(out, b.tasksForLHSInScope(n.LHS[i], scope)...)
			}
			return out, true
		}
		out := []runtime.Task{{Op: runtime.OpMultiAssign, Data: data}}
		for i := len(n.Values) - 1; i >= 0; i-- {
			out = append(out, b.tasksForExprInScope(n.Values[i], scope)...)
		}
		for i := len(n.LHS) - 1; i >= 0; i-- {
			out = append(out, b.tasksForLHSInScope(n.LHS[i], scope)...)
		}
		return out, true
	case *ast.IncDecStmt:
		if n == nil {
			return nil, true
		}
		out := []runtime.Task{{Op: runtime.OpIncDec, Data: string(n.Operator)}}
		out = append(out, b.tasksForLHSInScope(n.Operand, scope)...)
		return out, true
	case *ast.SendStmt:
		if n == nil {
			return nil, true
		}
		out := []runtime.Task{{Op: runtime.OpChanSend}}
		out = append(out, b.tasksForExprInScope(n.Value, scope)...)
		out = append(out, b.tasksForExprInScope(n.Channel, scope)...)
		return out, true
	case *ast.ReturnStmt:
		if n == nil {
			return nil, true
		}
		out := []runtime.Task{{Op: runtime.OpReturn, Data: len(n.Results)}}
		for i := len(n.Results) - 1; i >= 0; i-- {
			out = append(out, b.tasksForExprInScope(n.Results[i], scope)...)
		}
		return out, true
	case *ast.InterruptStmt:
		if n == nil {
			return nil, true
		}
		return []runtime.Task{{Op: runtime.OpInterrupt, Data: n.InterruptType}}, true
	case *ast.IfStmt:
		if n == nil {
			return nil, true
		}
		branch := &runtime.BranchData{
			Then: b.tasksForStmtInScope(n.Body, nil, scope.childBlock()),
		}
		if n.ElseBody != nil {
			branch.Else = b.tasksForStmtInScope(n.ElseBody, nil, scope.childBlock())
		}
		out := []runtime.Task{{Op: runtime.OpBranchIf, Data: branch}}
		out = append(out, b.tasksForExprInScope(n.Cond, scope)...)
		return out, true
	case *ast.ForStmt:
		if n == nil {
			return nil, true
		}
		loopScope := scope.childBlock()
		initTasks := make([]runtime.Task, 0)
		if n.Init != nil {
			initStmt, ok := n.Init.(ast.Stmt)
			if !ok {
				return nil, false
			}
			initTasks = b.tasksForStmtInScope(initStmt, nil, loopScope)
		}
		bodyScope := loopScope.childBlock()
		bodyStmt, ok := n.Body.(ast.Stmt)
		if !ok {
			return nil, false
		}
		copyIn := make([]runtime.Task, 0)
		copyBack := make([]runtime.Task, 0)
		for name, outerSym := range loopScope.bindings {
			if outerSym.Kind == runtime.SymbolLocal {
				innerSym := bodyScope.declare(name)
				copyIn = append(copyIn, storeTasksForSymbol(innerSym)...)
				copyIn = append(copyIn, loadTasksForSymbol(outerSym)...)
				copyBack = append(copyBack, storeTasksForSymbol(outerSym)...)
				copyBack = append(copyBack, loadTasksForSymbol(innerSym)...)
			}
		}
		loop := &runtime.ForData{
			Body: append(append(copyBack, b.tasksForStmtInScope(bodyStmt, nil, bodyScope)...), copyIn...),
		}
		if n.Cond != nil {
			loop.Cond = b.tasksForExprInScope(n.Cond, loopScope)
		}
		if n.Update != nil {
			update, ok := n.Update.(ast.Stmt)
			if !ok {
				return nil, false
			}
			loop.Update = b.tasksForStmtInScope(update, nil, loopScope)
		}
		out := []runtime.Task{
			{Op: runtime.OpScopeExit},
			{Op: runtime.OpForStart, Data: loop},
		}
		out = append(out, initTasks...)
		out = append(out, runtime.Task{Op: runtime.OpScopeEnter, Data: "for"})
		return out, true
	case *ast.RangeStmt:
		if n == nil {
			return nil, true
		}
		rangeScope := scope.childBlock()
		var keySym, valueSym runtime.SymbolRef
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
		keyType := b.runtimeType(ast.GoMiniType(runtime.SpecInt64), n, "range key type")
		valType := fallbackAnyType
		if n.X != nil {
			objType := n.X.GetBase().Type
			if objType.IsMap() {
				if keyT, valueT, ok := objType.GetMapKeyValueTypes(); ok {
					keyType = b.runtimeType(keyT, n.X, "range map key type")
					valType = b.runtimeType(valueT, n.X, "range map value type")
				}
			} else if objType.IsArray() {
				if elemT, ok := objType.ReadArrayItemType(); ok {
					valType = b.runtimeType(elemT, n.X, "range array item type")
				}
			} else if objType.IsChan() {
				if elemT, ok := objType.ReadChanElemType(); ok {
					keyType = b.runtimeType(elemT, n.X, "range channel item type")
					valType = b.runtimeType(ast.TypeVoid, n.X, "range channel value type")
				}
			}
		}
		rData := &runtime.RangeData{
			Key:     string(n.Key),
			Value:   string(n.Value),
			KeySym:  keySym,
			ValSym:  valueSym,
			KeyType: keyType,
			ValType: valType,
			Define:  n.Define,
			Body:    b.tasksForStmtInScope(n.Body, nil, rangeScope),
		}
		out := []runtime.Task{{Op: runtime.OpRangeInit, Data: rData}}
		out = append(out, b.tasksForExprInScope(n.X, scope)...)
		return out, true
	case *ast.TryStmt:
		if n == nil {
			return nil, true
		}
		out := make([]runtime.Task, 0, 3)
		if n.Finally != nil {
			out = append(out, runtime.Task{Op: runtime.OpFinally, Data: &runtime.FinallyData{
				Body: b.tasksForStmt(n.Finally, nil),
			}})
		}
		if n.Catch != nil {
			catchScope := scope.childBlock()
			if n.Catch.VarName != "" {
				catchScope.declare(string(n.Catch.VarName))
			}
			out = append(out, runtime.Task{Op: runtime.OpCatchBoundary, Data: &runtime.CatchData{
				VarName: string(n.Catch.VarName),
				Sym:     catchScope.resolveOrImplicit(string(n.Catch.VarName)),
				Body:    b.tasksForStmtInScope(n.Catch.Body, nil, catchScope),
			}})
		}
		out = append(out, b.tasksForStmtInScope(n.Body, nil, scope.childBlock())...)
		return out, true
	case *ast.DeferStmt:
		if n == nil {
			return nil, true
		}
		call, ok := n.Call.(*ast.CallExprStmt)
		if !ok {
			return nil, false
		}
		return []runtime.Task{{Op: runtime.OpScheduleDefer, Data: &runtime.DeferData{
			Tasks:     b.tasksForExprInScope(call, scope),
			PopResult: !call.GetBase().Type.IsVoid(),
		}}}, true
	case *ast.GoStmt:
		if n == nil {
			return nil, true
		}
		call, ok := n.Call.(*ast.CallExprStmt)
		if !ok || call == nil {
			return nil, false
		}
		data := &runtime.CallData{
			Mode:     runtime.CallByValue,
			ArgCount: len(call.Args),
			Ellipsis: call.Ellipsis,
		}
		switch fn := call.Func.(type) {
		case *ast.IdentifierExpr:
			data.Mode = runtime.CallByName
			data.Name = string(fn.Name)
			data.Sym = scope.resolveOrImplicit(data.Name)
		case *ast.ConstRefExpr:
			data.Mode = runtime.CallByName
			data.Name = string(fn.Name)
			data.Sym = scope.resolveOrImplicit(data.Name)
		case *ast.MemberExpr:
			data.Mode = runtime.CallByMember
			data.Name = string(fn.Property)
			data.ReceiverType = b.runtimeType(fn.Object.GetBase().Type, fn.Object, "method receiver type")
		}

		out := []runtime.Task{{Op: runtime.OpGo, Data: data}}
		for i := len(call.Args) - 1; i >= 0; i-- {
			out = append(out, b.tasksForExprInScope(call.Args[i], scope)...)
		}
		if member, ok := call.Func.(*ast.MemberExpr); ok {
			out = append(out, b.tasksForExprInScope(member.Object, scope)...)
		} else if data.Mode == runtime.CallByValue {
			out = append(out, b.tasksForExprInScope(call.Func, scope)...)
		}
		return out, true
	case *ast.SelectStmt:
		if n == nil {
			return nil, true
		}
		selectScope := scope.childBlock()
		plan := &runtime.SelectData{Cases: make([]runtime.SelectCaseData, 0, len(n.Cases))}
		operandTasksByCase := make([][]runtime.Task, len(n.Cases))
		for i := range n.Cases {
			c := n.Cases[i]
			caseScope := selectScope.childBlock()
			caseData := runtime.SelectCaseData{
				Kind: runtime.SelectCommDefault,
			}
			switch comm := c.Comm.(type) {
			case nil:
			case *ast.SendStmt:
				caseData.Kind = runtime.SelectCommSend
				tasks := []runtime.Task{}
				tasks = append(tasks, b.tasksForExprInScope(comm.Value, selectScope)...)
				tasks = append(tasks, b.tasksForExprInScope(comm.Channel, selectScope)...)
				operandTasksByCase[i] = tasks
			case *ast.ExpressionStmt:
				recv, ok := comm.X.(*ast.ReceiveExpr)
				if !ok {
					return nil, false
				}
				caseData.Kind = runtime.SelectCommRecv
				if elem, ok := recv.GetBase().Type.ReadChanElemType(); ok {
					caseData.RecvType = b.runtimeType(elem, recv, "select receive type")
				} else {
					caseData.RecvType = b.runtimeType(recv.GetBase().Type, recv, "select receive type")
				}
				operandTasksByCase[i] = b.tasksForExprInScope(recv.Channel, selectScope)
			case *ast.AssignmentStmt:
				recv, ok := comm.Value.(*ast.ReceiveExpr)
				if !ok {
					return nil, false
				}
				caseData.Kind = runtime.SelectCommRecv
				caseData.Define = comm.Kind == ast.AssignDefine
				if ident, ok := comm.LHS.(*ast.IdentifierExpr); ok && ident != nil {
					caseData.RecvName = string(ident.Name)
					if caseData.RecvName != "" && caseData.RecvName != "_" {
						if caseData.Define {
							caseData.RecvSym = caseScope.declare(caseData.RecvName)
						} else {
							caseData.RecvSym = selectScope.resolveOrImplicit(caseData.RecvName)
						}
					}
				}
				caseData.RecvType = b.runtimeType(recv.GetBase().Type, recv, "select receive type")
				operandTasksByCase[i] = b.tasksForExprInScope(recv.Channel, selectScope)
			case *ast.MultiAssignmentStmt:
				if len(comm.Values) != 1 {
					return nil, false
				}
				recv, ok := comm.Values[0].(*ast.ReceiveExpr)
				if !ok {
					return nil, false
				}
				caseData.Kind = runtime.SelectCommRecv
				caseData.Define = comm.Kind == ast.AssignDefine
				if len(comm.LHS) > 0 {
					if ident, ok := comm.LHS[0].(*ast.IdentifierExpr); ok && ident != nil {
						caseData.RecvName = string(ident.Name)
						if caseData.RecvName != "" && caseData.RecvName != "_" {
							if caseData.Define {
								caseData.RecvSym = caseScope.declare(caseData.RecvName)
							} else {
								caseData.RecvSym = selectScope.resolveOrImplicit(caseData.RecvName)
							}
						}
					}
				}
				if len(comm.LHS) > 1 {
					if ident, ok := comm.LHS[1].(*ast.IdentifierExpr); ok && ident != nil {
						caseData.RecvOK = string(ident.Name)
						if caseData.RecvOK != "" && caseData.RecvOK != "_" {
							if caseData.Define {
								caseData.RecvOKSym = caseScope.declare(caseData.RecvOK)
							} else {
								caseData.RecvOKSym = selectScope.resolveOrImplicit(caseData.RecvOK)
							}
						}
					}
				}
				if recvType := recv.GetBase().Type; recvType.IsTuple() {
					items, _ := recvType.ReadTuple()
					if len(items) > 0 {
						caseData.RecvType = b.runtimeType(items[0], recv, "select receive value type")
					}
				}
				if caseData.RecvType.IsEmpty() {
					caseData.RecvType = b.runtimeType(ast.TypeAny, recv, "select receive value type")
				}
				caseData.OKType = b.runtimeType(ast.TypeBool, recv, "select receive ok type")
				operandTasksByCase[i] = b.tasksForExprInScope(recv.Channel, selectScope)
			default:
				return nil, false
			}
			caseData.Body = b.tasksForStmtInScope(&ast.BlockStmt{Children: c.Body, Inner: true}, nil, caseScope)
			plan.Cases = append(plan.Cases, caseData)
		}
		out := []runtime.Task{{Op: runtime.OpSelect, Data: plan}}
		for i := len(operandTasksByCase) - 1; i >= 0; i-- {
			out = append(out, operandTasksByCase[i]...)
		}
		return out, true
	case *ast.SwitchStmt:
		if n == nil {
			return nil, true
		}
		switchScope := scope.childBlock()
		plan := &runtime.SwitchData{
			IsType:    n.IsType,
			HasTag:    n.Tag != nil,
			HasAssign: n.Assign != nil,
		}
		if n.Init != nil {
			plan.Init = b.tasksForStmtInScope(n.Init, nil, switchScope)
		}
		if n.Tag != nil {
			plan.Tag = b.tasksForExprInScope(n.Tag, switchScope)
		}
		if n.Assign != nil {
			if n.IsType {
				switch assign := n.Assign.(type) {
				case *ast.AssignmentStmt:
					if assign.Kind == ast.AssignDefine {
						if ident, ok := assign.LHS.(*ast.IdentifierExpr); ok && ident != nil && ident.Name != "_" {
							switchScope.declare(string(ident.Name))
						}
					}
					plan.AssignLHS = b.tasksForLHSInScope(assign.LHS, switchScope)
				case *ast.BlockStmt:
					var lhs ast.Expr
					for _, child := range assign.Children {
						if asg, ok := child.(*ast.AssignmentStmt); ok {
							lhs = asg.LHS
							if asg.Kind == ast.AssignDefine {
								if ident, ok := asg.LHS.(*ast.IdentifierExpr); ok && ident != nil && ident.Name != "_" {
									switchScope.declare(string(ident.Name))
								}
							}
						}
					}
					if lhs == nil {
						return nil, false
					}
					plan.AssignLHS = b.tasksForLHSInScope(lhs, switchScope)
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
				plan.DefaultBody = b.tasksForStmtInScope(&ast.BlockStmt{Children: clause.Body, Inner: true}, nil, switchScope.childBlock())
				continue
			}
			caseData := runtime.SwitchCaseData{
				Body: b.tasksForStmtInScope(&ast.BlockStmt{Children: clause.Body, Inner: true}, nil, switchScope.childBlock()),
			}
			for _, expr := range clause.List {
				if n.IsType {
					var targetType runtime.RuntimeType
					if id, ok := expr.(*ast.IdentifierExpr); ok {
						targetType = b.runtimeType(ast.GoMiniType(id.Name), expr, "type switch case")
					} else if ref, ok := expr.(*ast.ConstRefExpr); ok {
						targetType = b.runtimeType(ast.GoMiniType(ref.Name), expr, "type switch case")
					} else {
						targetType = b.runtimeType(expr.GetBase().Type, expr, "type switch case")
					}
					caseData.TypeNames = append(caseData.TypeNames, targetType)
				} else {
					caseData.Exprs = append(caseData.Exprs, b.tasksForExprInScope(expr, scope))
				}
			}
			plan.Cases = append(plan.Cases, caseData)
		}

		out := []runtime.Task{{Op: runtime.OpSwitchStart, Data: plan}}
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
		out := make([]runtime.Task, 0)
		if n.X != nil && !n.GetBase().Type.IsVoid() {
			out = append(out, runtime.Task{Op: runtime.OpPop})
		}
		out = append(out, b.tasksForExprInScope(n.X, scope)...)
		return out, true
	case *ast.CallExprStmt:
		if n == nil {
			return nil, true
		}
		out := make([]runtime.Task, 0)
		if !n.GetBase().Type.IsVoid() {
			out = append(out, runtime.Task{Op: runtime.OpPop})
		}
		out = append(out, b.tasksForExprInScope(n, scope)...)
		return out, true
	case *ast.ProgramStmt, *ast.FunctionStmt, *ast.StructStmt, *ast.InterfaceStmt:
		// Metadata nodes handled at initialization, not in execution path
		return nil, false
	default:
		return nil, false
	}
}

func (b *builder) lowerExprTasks(expr ast.Expr, scope *loweringScope) ([]runtime.Task, bool) {
	switch n := expr.(type) {
	case nil:
		return []runtime.Task{{Op: runtime.OpPush}}, true
	case *ast.BadExpr:
		return nil, false
	case *ast.LiteralExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		val, err := literalDirect(n)
		if err != nil {
			return nil, false
		}
		return []runtime.Task{{Op: runtime.OpPush, Data: val}}, true
	case *ast.IdentifierExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		if n.ResolvedConstant {
			return b.constantPushTasks(string(n.Name))
		}
		sym := scope.resolveOrImplicit(string(n.Name))
		switch sym.Kind {
		case runtime.SymbolLocal:
			return []runtime.Task{{Op: runtime.OpLoadLocal, Data: sym}}, true
		case runtime.SymbolUpvalue:
			return []runtime.Task{{Op: runtime.OpLoadUpvalue, Data: sym}}, true
		default:
			return []runtime.Task{{Op: runtime.OpLoadVar, Data: &runtime.LoadVarData{Name: string(n.Name), Sym: sym}}}, true
		}
	case *ast.ConstRefExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		return b.constantPushTasks(string(n.Name))
	case *ast.UnaryExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		out := []runtime.Task{{Op: runtime.OpApplyUnary, Data: string(n.Operator)}}
		out = append(out, b.tasksForExprInScope(n.Operand, scope)...)
		return out, true
	case *ast.BinaryExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		op := string(n.Operator)
		if op == "&&" || op == "And" || op == "||" || op == "Or" {
			out := []runtime.Task{{Op: runtime.OpJumpIf, Data: &runtime.JumpData{
				Operator: op,
				Right:    b.tasksForExprInScope(n.Right, scope),
			}}}
			out = append(out, b.tasksForExprInScope(n.Left, scope)...)
			return out, true
		}
		out := []runtime.Task{{Op: runtime.OpApplyBinary, Data: op}}
		out = append(out, b.tasksForExprInScope(n.Right, scope)...)
		out = append(out, b.tasksForExprInScope(n.Left, scope)...)
		return out, true
	case *ast.IndexExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		resultType := n.GetBase().Type
		if resultType.IsEmpty() {
			resultType = ast.GoMiniType(runtime.SpecAny)
		}
		out := []runtime.Task{{Op: runtime.OpIndex, Data: &runtime.IndexData{
			Multi:      n.Multi,
			ResultType: b.runtimeType(resultType, n, "index result type"),
		}}}
		out = append(out, b.tasksForExprInScope(n.Index, scope)...)
		out = append(out, b.tasksForExprInScope(n.Object, scope)...)
		return out, true
	case *ast.MemberExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		out := []runtime.Task{{Op: runtime.OpMember, Data: &runtime.MemberData{
			Property:   string(n.Property),
			ObjectType: b.runtimeType(n.Object.GetBase().Type, n.Object, "member object type"),
		}}}
		out = append(out, b.tasksForExprInScope(n.Object, scope)...)
		return out, true
	case *ast.TypeAssertExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		resultType := n.GetBase().Type
		if resultType.IsEmpty() {
			resultType = ast.GoMiniType(runtime.SpecAny)
		}
		out := []runtime.Task{{Op: runtime.OpAssert, Data: &runtime.AssertData{
			TargetType: b.runtimeType(n.Type, n, "type assertion target"),
			Multi:      n.Multi,
			ResultType: b.runtimeType(resultType, n, "type assertion result"),
		}}}
		out = append(out, b.tasksForExprInScope(n.X, scope)...)
		return out, true
	case *ast.ReceiveExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		resultType := n.GetBase().Type
		if resultType.IsEmpty() {
			resultType = ast.GoMiniType(runtime.SpecAny)
		}
		out := []runtime.Task{{Op: runtime.OpChanRecv, Data: &runtime.ChanRecvData{
			Multi:      n.Multi,
			ResultType: b.runtimeType(resultType, n, "channel receive result"),
		}}}
		out = append(out, b.tasksForExprInScope(n.Channel, scope)...)
		return out, true
	case *ast.CompositeExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		entries := make([]runtime.CompositeEntryData, len(n.Values))
		out := []runtime.Task{{Op: runtime.OpComposite, Data: &runtime.CompositeData{
			Type:    b.runtimeType(n.Type, n, "composite type"),
			Entries: entries,
		}}}
		for i := len(n.Values) - 1; i >= 0; i-- {
			v := n.Values[i]
			if ident, ok := v.Key.(*ast.IdentifierExpr); ok {
				entries[i].IdentKey = string(ident.Name)
			} else if v.Key != nil {
				entries[i].HasExprKey = true
			}
			out = append(out, b.tasksForExprInScope(v.Value, scope)...)
			if entries[i].HasExprKey {
				out = append(out, b.tasksForExprInScope(v.Key, scope)...)
			}
		}
		return out, true
	case *ast.SliceExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		out := []runtime.Task{{Op: runtime.OpSlice, Data: &runtime.SliceData{
			HasLow:  n.Low != nil,
			HasHigh: n.High != nil,
		}}}
		if n.High != nil {
			out = append(out, b.tasksForExprInScope(n.High, scope)...)
		}
		if n.Low != nil {
			out = append(out, b.tasksForExprInScope(n.Low, scope)...)
		}
		out = append(out, b.tasksForExprInScope(n.X, scope)...)
		return out, true
	case *ast.StarExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		out := []runtime.Task{{Op: runtime.OpApplyUnary, Data: "Dereference"}}
		out = append(out, b.tasksForExprInScope(n.X, scope)...)
		return out, true
	case *ast.AddressExpr:
		if n == nil || n.Target == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		if _, ok := n.Target.(*ast.CompositeExpr); ok {
			targetTasks := b.tasksForExprInScope(n.Target, scope)
			out := []runtime.Task{{Op: runtime.OpAddressAlloc, Data: b.runtimeType(n.Target.GetBase().Type, n.Target, "address target")}}
			out = append(out, targetTasks...)
			return out, true
		}
		lhsTasks, ok := b.lowerLHSTasks(n.Target, scope)
		if !ok {
			return nil, false
		}
		out := []runtime.Task{{Op: runtime.OpAddressOf}}
		out = append(out, lhsTasks...)
		return out, true
	case *ast.CallExprStmt:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		data := &runtime.CallData{
			Mode:          runtime.CallByValue,
			ArgCount:      len(n.Args),
			Ellipsis:      n.Ellipsis,
			CaptureArgLHS: true,
		}
		switch fn := n.Func.(type) {
		case *ast.IdentifierExpr:
			data.Mode = runtime.CallByName
			data.Name = string(fn.Name)
			data.Sym = scope.resolveOrImplicit(data.Name)
		case *ast.ConstRefExpr:
			data.Mode = runtime.CallByName
			data.Name = string(fn.Name)
			data.Sym = scope.resolveOrImplicit(data.Name)
		case *ast.MemberExpr:
			data.Mode = runtime.CallByMember
			data.Name = string(fn.Property)
			data.ReceiverType = b.runtimeType(fn.Object.GetBase().Type, fn.Object, "method receiver type")
		}

		out := []runtime.Task{{Op: runtime.OpCall, Data: data}}
		for i := len(n.Args) - 1; i >= 0; i-- {
			out = append(out, b.tasksForCallArgInScope(n.Args[i], scope)...)
		}
		if member, ok := n.Func.(*ast.MemberExpr); ok {
			out = append(out, b.tasksForExprInScope(member.Object, scope)...)
		} else if data.Mode == runtime.CallByValue {
			out = append(out, b.tasksForExprInScope(n.Func, scope)...)
		}
		return out, true
	case *ast.ImportExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		return []runtime.Task{{Op: runtime.OpImportInit, Data: &runtime.ImportInitData{Path: n.Path}}}, true
	case *ast.FuncLitExpr:
		if n == nil {
			return []runtime.Task{{Op: runtime.OpPush}}, true
		}
		fnScope := scope.childFunction()
		seenParams := make(map[string]struct{}, len(n.Params))
		for _, p := range n.Params {
			name := string(p.Name)
			if name != "" && name != "_" {
				if _, exists := seenParams[name]; exists {
					return nil, false
				}
				seenParams[name] = struct{}{}
			}
			fnScope.declareParam(name)
		}
		captures := make([]string, len(n.CaptureNames))
		copy(captures, n.CaptureNames)
		sig, err := funcSigFromFunction(n.FunctionType)
		if err != nil {
			return nil, false
		}
		return []runtime.Task{{Op: runtime.OpMakeClosure, Data: &runtime.ClosureData{
			FunctionSig:  sig,
			BodyTasks:    b.tasksForStmtInScope(n.Body, nil, fnScope),
			CaptureNames: captures,
			CaptureRefs:  append([]runtime.SymbolRef(nil), fnScope.fn.order...),
		}}}, true
	default:
		return nil, false
	}
}

func (b *builder) tasksForCallArgInScope(expr ast.Expr, scope *loweringScope) []runtime.Task {
	lhsTasks, ok := b.lowerLHSTasks(expr, scope)
	if !ok {
		lhsTasks = []runtime.Task{{Op: runtime.OpEvalLHS, Data: &runtime.LHSData{Kind: runtime.LHSTypeNone}}}
	} else {
		lhsTasks = b.setSource(lhsTasks, expr)
	}
	exprTasks := b.tasksForExprInScope(expr, scope)
	out := make([]runtime.Task, 0, len(lhsTasks)+len(exprTasks))
	out = append(out, lhsTasks...)
	out = append(out, exprTasks...)
	return out
}

func (b *builder) lowerLHSTasks(lhsExpr ast.Expr, scope *loweringScope) ([]runtime.Task, bool) {
	switch lhs := lhsExpr.(type) {
	case nil:
		return []runtime.Task{{
			Op: runtime.OpEvalLHS,
			Data: &runtime.LHSData{
				Kind: runtime.LHSTypeNone,
			},
		}}, true
	case *ast.IdentifierExpr:
		if lhs == nil || lhs.Name == "_" {
			return []runtime.Task{{
				Op: runtime.OpEvalLHS,
				Data: &runtime.LHSData{
					Kind: runtime.LHSTypeNone,
				},
			}}, true
		}
		if lhs.ResolvedConstant {
			return []runtime.Task{{
				Op: runtime.OpEvalLHS,
				Data: &runtime.LHSData{
					Kind: runtime.LHSTypeNone,
				},
			}}, true
		}
		return []runtime.Task{{
			Op: runtime.OpEvalLHS,
			Data: &runtime.LHSData{
				Kind: runtime.LHSTypeEnv,
				Name: string(lhs.Name),
				Sym:  scope.resolveOrImplicit(string(lhs.Name)),
			},
		}}, true
	case *ast.IndexExpr:
		if lhs == nil {
			return []runtime.Task{{
				Op: runtime.OpEvalLHS,
				Data: &runtime.LHSData{
					Kind: runtime.LHSTypeNone,
				},
			}}, true
		}
		out := []runtime.Task{{Op: runtime.OpEvalLHS, Data: &runtime.LHSData{Kind: runtime.LHSTypeIndex}}}
		out = append(out, b.tasksForExprInScope(lhs.Index, scope)...)
		out = append(out, b.tasksForExprInScope(lhs.Object, scope)...)
		return out, true
	case *ast.MemberExpr:
		if lhs == nil {
			return []runtime.Task{{
				Op: runtime.OpEvalLHS,
				Data: &runtime.LHSData{
					Kind: runtime.LHSTypeNone,
				},
			}}, true
		}
		out := []runtime.Task{{Op: runtime.OpEvalLHS, Data: &runtime.LHSData{
			Kind:     runtime.LHSTypeMember,
			Property: string(lhs.Property),
		}}}
		out = append(out, b.tasksForExprInScope(lhs.Object, scope)...)
		return out, true
	case *ast.StarExpr:
		if lhs == nil {
			return []runtime.Task{{
				Op: runtime.OpEvalLHS,
				Data: &runtime.LHSData{
					Kind: runtime.LHSTypeNone,
				},
			}}, true
		}
		out := []runtime.Task{{Op: runtime.OpEvalLHS, Data: &runtime.LHSData{Kind: runtime.LHSTypeStar}}}
		out = append(out, b.tasksForExprInScope(lhs.X, scope)...)
		return out, true
	case *ast.SliceExpr:
		if lhs == nil {
			return []runtime.Task{{
				Op: runtime.OpEvalLHS,
				Data: &runtime.LHSData{
					Kind: runtime.LHSTypeNone,
				},
			}}, true
		}
		out := []runtime.Task{{Op: runtime.OpEvalLHS, Data: &runtime.LHSData{
			Kind:    runtime.LHSTypeSlice,
			HasLow:  lhs.Low != nil,
			HasHigh: lhs.High != nil,
		}}}
		if lhs.High != nil {
			out = append(out, b.tasksForExprInScope(lhs.High, scope)...)
		}
		if lhs.Low != nil {
			out = append(out, b.tasksForExprInScope(lhs.Low, scope)...)
		}
		out = append(out, b.tasksForExprInScope(lhs.X, scope)...)
		return out, true
	default:
		return nil, false
	}
}
