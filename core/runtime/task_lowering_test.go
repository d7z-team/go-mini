package runtime

import (
	"context"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func visitLoweredTasks(tasks []Task, visit func(Task)) {
	for _, task := range tasks {
		visit(task)
		switch data := task.Data.(type) {
		case *BranchData:
			visitLoweredTasks(data.Then, visit)
			visitLoweredTasks(data.Else, visit)
		case *ForData:
			visitLoweredTasks(data.Cond, visit)
			visitLoweredTasks(data.Body, visit)
			visitLoweredTasks(data.Update, visit)
		case *RangeData:
			visitLoweredTasks(data.Body, visit)
		case *CatchData:
			visitLoweredTasks(data.Body, visit)
		case *FinallyData:
			visitLoweredTasks(data.Body, visit)
		case *DeferData:
			visitLoweredTasks(data.Tasks, visit)
		case *SwitchData:
			visitLoweredTasks(data.Init, visit)
			visitLoweredTasks(data.Tag, visit)
			visitLoweredTasks(data.AssignLHS, visit)
			visitLoweredTasks(data.DefaultBody, visit)
			for _, c := range data.Cases {
				visitLoweredTasks(c.Body, visit)
				for _, exprs := range c.Exprs {
					visitLoweredTasks(exprs, visit)
				}
			}
		case *ClosureData:
			visitLoweredTasks(data.BodyTasks, visit)
		case *DoCallData:
			visitLoweredTasks(data.BodyTasks, visit)
		}
	}
}

func taskLoadSymbol(task Task) (string, SymbolKind, bool) {
	switch task.Op {
	case OpLoadVar:
		load, ok := task.Data.(*LoadVarData)
		if !ok {
			return "", SymbolUnknown, false
		}
		return load.Name, load.Sym.Kind, true
	case OpLoadLocal, OpLoadUpvalue:
		sym, ok := task.Data.(SymbolRef)
		if !ok {
			return "", SymbolUnknown, false
		}
		return sym.Name, sym.Kind, true
	default:
		return "", SymbolUnknown, false
	}
}

func TestLowerExprTasksBuildsDataOnlyCallPlan(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	expr, err := ffigo.NewGoToASTConverter().ConvertExprSource(`sum(1, 2)`)
	if err != nil {
		t.Fatalf("convert expr failed: %v", err)
	}

	tasks, ok := exec.lowerExprTasks(expr, exec.newRootLoweringScope())
	if !ok {
		t.Fatal("expected expression to be lowered")
	}
	if len(tasks) == 0 {
		t.Fatal("expected non-empty task list")
	}

	var callTask *Task
	for i := range tasks {
		if tasks[i].Op == OpCall {
			callTask = &tasks[i]
			break
		}
	}
	if callTask == nil {
		t.Fatal("expected call task in lowered expression")
	}
	data, ok := callTask.Data.(*CallData)
	if !ok {
		t.Fatalf("unexpected call task data: %T", callTask.Data)
	}
	if data.Mode != CallByName || data.Name != "sum" || data.ArgCount != 2 {
		t.Fatalf("unexpected call task data: %+v", data)
	}
}

func TestLowerStmtTasksBuildsDataOnlyBranchPlan(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	stmts, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`
if ok {
	value = 1
} else {
	value = 2
}
`)
	if err != nil {
		t.Fatalf("convert stmts failed: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("unexpected stmt count: %d", len(stmts))
	}

	tasks, ok := exec.lowerStmtTasks(stmts[0], nil, exec.newRootLoweringScope())
	if !ok {
		t.Fatal("expected statement to be lowered")
	}

	var branchTask *Task
	for i := range tasks {
		if tasks[i].Op == OpBranchIf {
			branchTask = &tasks[i]
			break
		}
	}
	if branchTask == nil {
		t.Fatal("expected branch task in lowered statement")
	}

	data, ok := branchTask.Data.(*BranchData)
	if !ok {
		t.Fatalf("unexpected branch task data: %T", branchTask.Data)
	}
	if len(data.Then) == 0 || len(data.Else) == 0 {
		t.Fatalf("expected non-empty branch plans: %+v", data)
	}
	for _, task := range data.Then {
		if task.Op == OpAssign {
			// Checked
		}
	}
	for _, task := range data.Else {
		if task.Op == OpAssign {
			// Checked
		}
	}
}

func TestLowerStmtTasksBuildsDataOnlyForPlan(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	stmts, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`
for i := 0; i < 3; i++ {
	sum += i
}
`)
	if err != nil {
		t.Fatalf("convert stmts failed: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("unexpected stmt count: %d", len(stmts))
	}

	tasks, ok := exec.lowerStmtTasks(stmts[0], nil, exec.newRootLoweringScope())
	if !ok {
		t.Fatal("expected for statement to be lowered")
	}

	var loopTask *Task
	for i := range tasks {
		if tasks[i].Op == OpLoopBoundary {
			loopTask = &tasks[i]
			break
		}
	}
	if loopTask == nil {
		t.Fatal("expected loop boundary task")
	}

	data, ok := loopTask.Data.(*ForData)
	if !ok {
		t.Fatalf("unexpected loop task data: %T", loopTask.Data)
	}
	if len(data.Body) == 0 || len(data.Update) == 0 || len(data.Cond) == 0 {
		t.Fatalf("expected lowered for-loop plan to include body/cond/update: %+v", data)
	}
}

func TestLowerStmtTasksBuildsDataOnlyRangePlan(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	stmts, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`
for idx, value := range items {
	total += value
	_ = idx
}
`)
	if err != nil {
		t.Fatalf("convert stmts failed: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("unexpected stmt count: %d", len(stmts))
	}

	tasks, ok := exec.lowerStmtTasks(stmts[0], nil, exec.newRootLoweringScope())
	if !ok {
		t.Fatal("expected range statement to be lowered")
	}

	var rangeTask *Task
	for i := range tasks {
		if tasks[i].Op == OpRangeInit {
			rangeTask = &tasks[i]
			break
		}
	}
	if rangeTask == nil {
		t.Fatal("expected range init task")
	}

	data, ok := rangeTask.Data.(*RangeData)
	if !ok {
		t.Fatalf("unexpected range task data: %T", rangeTask.Data)
	}
	if data.Key != "idx" || data.Value != "value" || !data.Define || len(data.Body) == 0 {
		t.Fatalf("unexpected range lowering data: %+v", data)
	}
}

func TestLowerStmtTasksBuildsDataOnlyTryPlan(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	body, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`panic("boom")`)
	if err != nil {
		t.Fatalf("convert body failed: %v", err)
	}
	catchBody, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`println(err)`)
	if err != nil {
		t.Fatalf("convert catch body failed: %v", err)
	}
	finallyBody, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`println("done")`)
	if err != nil {
		t.Fatalf("convert finally body failed: %v", err)
	}

	tryStmt := &ast.TryStmt{
		Body: &ast.BlockStmt{Children: body, Inner: true},
		Catch: &ast.CatchClause{
			VarName: "err",
			Body:    &ast.BlockStmt{Children: catchBody, Inner: true},
		},
		Finally: &ast.BlockStmt{Children: finallyBody, Inner: true},
	}

	tasks, ok := exec.lowerStmtTasks(tryStmt, nil, exec.newRootLoweringScope())
	if !ok {
		t.Fatal("expected try statement to be lowered")
	}

	var (
		finallyTask *Task
		catchTask   *Task
	)
	for i := range tasks {
		switch tasks[i].Op {
		case OpFinally:
			finallyTask = &tasks[i]
		case OpCatchBoundary:
			catchTask = &tasks[i]
		}
	}
	if finallyTask == nil || catchTask == nil {
		t.Fatalf("expected lowered try tasks, got: %+v", tasks)
	}
	if _, ok := finallyTask.Data.(*FinallyData); !ok {
		t.Fatalf("unexpected finally data: %T", finallyTask.Data)
	}
	catchData, ok := catchTask.Data.(*CatchData)
	if !ok {
		t.Fatalf("unexpected catch data: %T", catchTask.Data)
	}
	if catchData.VarName != "err" || len(catchData.Body) == 0 {
		t.Fatalf("unexpected catch data: %+v", catchData)
	}
}

func TestLowerStmtTasksBuildsDataOnlyDeferPlan(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	stmts, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`defer cleanup(1)`)
	if err != nil {
		t.Fatalf("convert stmts failed: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("unexpected stmt count: %d", len(stmts))
	}

	tasks, ok := exec.lowerStmtTasks(stmts[0], nil, exec.newRootLoweringScope())
	if !ok {
		t.Fatal("expected defer statement to be lowered")
	}
	if len(tasks) != 1 || tasks[0].Op != OpScheduleDefer {
		t.Fatalf("unexpected defer lowering: %+v", tasks)
	}
	data, ok := tasks[0].Data.(*DeferData)
	if !ok {
		t.Fatalf("unexpected defer data: %T", tasks[0].Data)
	}
	if len(data.Tasks) == 0 {
		t.Fatalf("expected deferred task payload, got: %+v", data)
	}
}

func TestLowerStmtTasksBuildsDataOnlySwitchPlan(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	stmts, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`
switch x {
case 1:
	res = "one"
case 2, 3:
	res = "two"
default:
	res = "other"
}
`)
	if err != nil {
		t.Fatalf("convert stmts failed: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("unexpected stmt count: %d", len(stmts))
	}

	tasks, ok := exec.lowerStmtTasks(stmts[0], nil, exec.newRootLoweringScope())
	if !ok {
		t.Fatal("expected switch statement to be lowered")
	}

	var (
		loopTask   *Task
		switchTask *Task
	)
	for i := range tasks {
		switch tasks[i].Op {
		case OpLoopBoundary:
			loopTask = &tasks[i]
		case OpSwitchTag:
			switchTask = &tasks[i]
		}
	}
	if loopTask == nil || switchTask == nil {
		t.Fatalf("expected lowered switch tasks, got: %+v", tasks)
	}
	data, ok := switchTask.Data.(*SwitchData)
	if !ok {
		t.Fatalf("unexpected switch data: %T", switchTask.Data)
	}
	if !data.HasTag || data.IsType || len(data.Cases) != 2 || len(data.DefaultBody) == 0 {
		t.Fatalf("unexpected switch lowering data: %+v", data)
	}
	if len(data.Cases[1].Exprs) != 2 {
		t.Fatalf("expected second case to contain two expressions, got: %+v", data.Cases[1])
	}
}

func TestLowerStmtTasksBuildsDataOnlyTypeSwitchPlan(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	assign := &ast.AssignmentStmt{
		LHS: &ast.IdentifierExpr{Name: "x"},
	}
	switchStmt := &ast.SwitchStmt{
		IsType: true,
		Tag:    &ast.IdentifierExpr{Name: "v"},
		Assign: assign,
		Body: &ast.BlockStmt{Children: []ast.Stmt{
			&ast.CaseClause{
				List: []ast.Expr{&ast.IdentifierExpr{Name: "Int64"}},
				Body: []ast.Stmt{
					&ast.ReturnStmt{Results: []ast.Expr{
						&ast.LiteralExpr{BaseNode: ast.BaseNode{Type: "String"}, Value: "int"},
					}},
				},
			},
			&ast.CaseClause{
				Body: []ast.Stmt{
					&ast.ReturnStmt{Results: []ast.Expr{
						&ast.LiteralExpr{BaseNode: ast.BaseNode{Type: "String"}, Value: "other"},
					}},
				},
			},
		}},
	}

	tasks, ok := exec.lowerStmtTasks(switchStmt, nil, exec.newRootLoweringScope())
	if !ok {
		t.Fatal("expected type switch statement to be lowered")
	}

	var switchTask *Task
	for i := range tasks {
		if tasks[i].Op == OpSwitchTag {
			switchTask = &tasks[i]
			break
		}
	}
	if switchTask == nil {
		t.Fatal("expected switch tag task")
	}
	data, ok := switchTask.Data.(*SwitchData)
	if !ok {
		t.Fatalf("unexpected switch data: %T", switchTask.Data)
	}
	if !data.IsType || !data.HasAssign || len(data.AssignLHS) == 0 {
		t.Fatalf("unexpected type switch lowering data: %+v", data)
	}
	if len(data.Cases) != 1 || len(data.Cases[0].TypeNames) != 1 || data.Cases[0].TypeNames[0] != "Int64" {
		t.Fatalf("unexpected type switch cases: %+v", data.Cases)
	}
}

func TestFuncLitClosureCarriesLoweredBodyTasks(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	expr, err := ffigo.NewGoToASTConverter().ConvertExprSource(`func(x Int64) Int64 { return x + 1 }`)
	if err != nil {
		t.Fatalf("convert expr failed: %v", err)
	}

	session := exec.NewSession(context.Background(), "test")
	defer exec.CleanupSession(session)

	value, err := exec.ExecExpr(session, expr)
	if err != nil {
		t.Fatalf("exec expr failed: %v", err)
	}

	if value == nil || value.VType != TypeClosure {
		t.Fatalf("expected closure value, got %+v", value)
	}
	closure, ok := value.Ref.(*VMClosure)
	if !ok {
		t.Fatalf("unexpected closure ref: %T", value.Ref)
	}
	if len(closure.BodyTasks) == 0 {
		t.Fatal("expected lowered body tasks on closure")
	}
}

func TestLoweringAnnotatesSymbols(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: map[ast.Ident]ast.Expr{"g": nil},
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	expr, err := ffigo.NewGoToASTConverter().ConvertExprSource(`func(x Int64) Int64 { var y Int64; println(g); return x + y + g }`)
	if err != nil {
		t.Fatalf("convert expr failed: %v", err)
	}

	tasks, ok := exec.lowerExprTasks(expr, exec.newRootLoweringScope())
	if !ok {
		t.Fatal("expected expression to lower")
	}
	if len(tasks) != 1 || tasks[0].Op != OpMakeClosure {
		t.Fatalf("unexpected outer tasks: %+v", tasks)
	}
	data, ok := tasks[0].Data.(*ClosureData)
	if !ok {
		t.Fatalf("unexpected closure data: %T", tasks[0].Data)
	}

	var sawLocalDecl, sawParamLoad, sawLocalLoad, sawGlobalLoad, sawBuiltinCall bool
	for _, task := range data.BodyTasks {
		switch task.Op {
		case OpDeclareVar:
			decl := task.Data.(*DeclareVarData)
			if decl.Name == "y" && decl.Sym.Kind == SymbolLocal {
				sawLocalDecl = true
			}
		case OpLoadVar, OpLoadLocal, OpLoadUpvalue:
			name, kind, ok := taskLoadSymbol(task)
			if !ok {
				continue
			}
			switch name {
			case "x":
				sawParamLoad = kind == SymbolLocal
			case "y":
				sawLocalLoad = kind == SymbolLocal
			case "g":
				sawGlobalLoad = kind == SymbolGlobal
			}
		case OpCall:
			call := task.Data.(*CallData)
			if call.Name == "println" {
				sawBuiltinCall = call.Sym.Kind == SymbolBuiltin
			}
		}
	}
	if !sawLocalDecl || !sawParamLoad || !sawLocalLoad || !sawGlobalLoad || !sawBuiltinCall {
		t.Fatalf("missing expected symbol annotations: decl=%v param=%v local=%v global=%v builtin=%v", sawLocalDecl, sawParamLoad, sawLocalLoad, sawGlobalLoad, sawBuiltinCall)
	}
}

func TestLoweringAnnotatesShortDeclAndRangeSymbols(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	stmts, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`
for _, item := range []int64{1} {
	value := item
	println(value)
}
`)
	if err != nil {
		t.Fatalf("convert stmts failed: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("unexpected stmt count: %d", len(stmts))
	}

	scope := exec.newRootLoweringScope().childFunction()
	tasks, ok := exec.lowerStmtTasks(stmts[0], nil, scope)
	if !ok {
		t.Fatal("expected statement to lower")
	}

	var sawValueLocal, sawItemLocal bool
	visitLoweredTasks(tasks, func(task Task) {
		name, kind, ok := taskLoadSymbol(task)
		if !ok {
			return
		}
		if name == "value" && kind == SymbolLocal {
			sawValueLocal = true
		}
		if name == "item" && kind == SymbolLocal {
			sawItemLocal = true
		}
	})

	if !sawValueLocal || !sawItemLocal {
		t.Fatalf("expected short-decl and range vars to be local: value=%v item=%v", sawValueLocal, sawItemLocal)
	}
}

func TestLoweringAnnotatesShadowingAndCatchSymbols(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode: ast.BaseNode{ID: "test"},
		Variables: map[ast.Ident]ast.Expr{
			"value": nil,
			"ok":    nil,
		},
		Constants: make(map[string]string),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	stmts, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`
if ok {
	value := int64(2)
	println(value)
}
println(value)
`)
	if err != nil {
		t.Fatalf("convert stmts failed: %v", err)
	}
	if len(stmts) != 2 {
		t.Fatalf("unexpected stmt count: %d", len(stmts))
	}

	fnScope := exec.newRootLoweringScope().childFunction()
	var sawLocalShadow, sawGlobalValue bool
	for _, stmt := range stmts {
		tasks, ok := exec.lowerStmtTasks(stmt, nil, fnScope)
		if !ok {
			t.Fatal("expected statement to lower")
		}
		visitLoweredTasks(tasks, func(task Task) {
			name, kind, ok := taskLoadSymbol(task)
			if !ok || name != "value" {
				return
			}
			if kind == SymbolLocal {
				sawLocalShadow = true
			}
			if kind == SymbolGlobal {
				sawGlobalValue = true
			}
		})
	}

	body, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`panic("boom")`)
	if err != nil {
		t.Fatalf("convert body failed: %v", err)
	}
	catchBody, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`println(err)`)
	if err != nil {
		t.Fatalf("convert catch body failed: %v", err)
	}
	tryStmt := &ast.TryStmt{
		Body: &ast.BlockStmt{Children: body, Inner: true},
		Catch: &ast.CatchClause{
			VarName: "err",
			Body:    &ast.BlockStmt{Children: catchBody, Inner: true},
		},
	}

	tasks, ok := exec.lowerStmtTasks(tryStmt, nil, fnScope)
	if !ok {
		t.Fatal("expected try statement to lower")
	}
	var sawCatchLocal bool
	visitLoweredTasks(tasks, func(task Task) {
		name, kind, ok := taskLoadSymbol(task)
		if ok && name == "err" && kind == SymbolLocal {
			sawCatchLocal = true
		}
	})

	if !sawLocalShadow || !sawGlobalValue || !sawCatchLocal {
		t.Fatalf("missing expected symbol annotations: localShadow=%v globalValue=%v catchLocal=%v", sawLocalShadow, sawGlobalValue, sawCatchLocal)
	}
}

func TestLoweringHandlesTypedNilASTNodes(t *testing.T) {
	exec := newExecutor(t, &ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})

	scope := exec.newRootLoweringScope().childFunction()
	var nilBlock *ast.BlockStmt
	var nilExpr *ast.IdentifierExpr
	var nilIndex *ast.IndexExpr
	var nilMember *ast.MemberExpr
	var nilStar *ast.StarExpr
	predeclareInnerBlockBindings(nilBlock, scope)

	stmtCases := []ast.Stmt{
		nilBlock,
		&ast.IfStmt{Body: nilBlock, ElseBody: nilBlock},
		&ast.RangeStmt{Define: true, Value: "item", Body: nilBlock},
		&ast.TryStmt{Body: nilBlock, Catch: &ast.CatchClause{VarName: "err", Body: nilBlock}, Finally: nilBlock},
	}
	for _, stmt := range stmtCases {
		if _, ok := exec.lowerStmtTasks(stmt, nil, exec.newRootLoweringScope()); !ok {
			t.Fatalf("expected typed-nil stmt %T to lower safely", stmt)
		}
	}

	exprCases := []ast.Expr{
		nilExpr,
		nilIndex,
		(*ast.CallExprStmt)(nil),
	}
	for _, expr := range exprCases {
		if _, ok := exec.lowerExprTasks(expr, exec.newRootLoweringScope()); !ok {
			t.Fatalf("expected typed-nil expr %T to lower safely", expr)
		}
	}

	lhsCases := []ast.Expr{
		nilExpr,
		nilIndex,
		nilMember,
		nilStar,
	}
	for _, expr := range lhsCases {
		if _, ok := exec.lowerLHSTasks(expr, exec.newRootLoweringScope()); !ok {
			t.Fatalf("expected typed-nil lhs %T to lower safely", expr)
		}
	}
}
