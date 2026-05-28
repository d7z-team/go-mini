package lowering

import (
	"errors"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/gofrontend"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func emptyProgram() *ast.ProgramStmt {
	return &ast.ProgramStmt{
		BaseNode:   ast.BaseNode{ID: "test"},
		Constants:  make(map[string]string),
		Variables:  make(map[ast.Ident]ast.Expr),
		Types:      make(map[ast.Ident]ast.GoMiniType),
		Structs:    make(map[ast.Ident]*ast.StructStmt),
		Interfaces: make(map[ast.Ident]*ast.InterfaceStmt),
		Functions:  make(map[ast.Ident]*ast.FunctionStmt),
	}
}

func visitLoweredTasks(tasks []runtime.Task, visit func(runtime.Task)) {
	for _, task := range tasks {
		visit(task)
		switch data := task.Data.(type) {
		case *runtime.BranchData:
			visitLoweredTasks(data.Then, visit)
			visitLoweredTasks(data.Else, visit)
		case *runtime.ForData:
			visitLoweredTasks(data.Cond, visit)
			visitLoweredTasks(data.Body, visit)
			visitLoweredTasks(data.Update, visit)
		case *runtime.RangeData:
			visitLoweredTasks(data.Body, visit)
		case *runtime.CatchData:
			visitLoweredTasks(data.Body, visit)
		case *runtime.FinallyData:
			visitLoweredTasks(data.Body, visit)
		case *runtime.DeferData:
			visitLoweredTasks(data.Tasks, visit)
		case *runtime.SwitchData:
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
		case *runtime.ClosureData:
			visitLoweredTasks(data.BodyTasks, visit)
		case *runtime.DoCallData:
			visitLoweredTasks(data.BodyTasks, visit)
		}
	}
}

func taskLoadSymbol(task runtime.Task) (string, runtime.SymbolKind, bool) {
	switch task.Op {
	case runtime.OpLoadVar:
		load, ok := task.Data.(*runtime.LoadVarData)
		if !ok {
			return "", runtime.SymbolUnknown, false
		}
		return load.Name, load.Sym.Kind, true
	case runtime.OpLoadLocal, runtime.OpLoadUpvalue:
		sym, ok := task.Data.(runtime.SymbolRef)
		if !ok {
			return "", runtime.SymbolUnknown, false
		}
		return sym.Name, sym.Kind, true
	default:
		return "", runtime.SymbolUnknown, false
	}
}

func TestLowerExprTasksBuildsDataOnlyCallPlan(t *testing.T) {
	program := emptyProgram()
	b := newBuilder(nil, program.Variables, program.Functions)
	expr, err := gofrontend.NewConverter().ConvertExprSource(`sum(1, 2)`)
	if err != nil {
		t.Fatalf("convert expr failed: %v", err)
	}

	tasks, ok := b.lowerExprTasks(expr, b.newRootLoweringScope())
	if !ok {
		t.Fatal("expected expression to be lowered")
	}

	var callTask *runtime.Task
	for i := range tasks {
		if tasks[i].Op == runtime.OpCall {
			callTask = &tasks[i]
			break
		}
	}
	if callTask == nil {
		t.Fatal("expected call task in lowered expression")
	}
	data, ok := callTask.Data.(*runtime.CallData)
	if !ok {
		t.Fatalf("unexpected call task data: %T", callTask.Data)
	}
	if data.Mode != runtime.CallByName || data.Name != "sum" || data.ArgCount != 2 || !data.CaptureArgLHS {
		t.Fatalf("unexpected call task data: %+v", data)
	}
}

func TestLoweringAnnotatesSymbols(t *testing.T) {
	program := emptyProgram()
	program.Variables["g"] = nil
	b := newBuilder(nil, program.Variables, program.Functions)

	expr, err := gofrontend.NewConverter().ConvertExprSource(`func(x Int64) Int64 { var y Int64; len("abc"); return x + y + g }`)
	if err != nil {
		t.Fatalf("convert expr failed: %v", err)
	}

	tasks, ok := b.lowerExprTasks(expr, b.newRootLoweringScope())
	if !ok {
		t.Fatal("expected expression to lower")
	}
	if len(tasks) != 1 || tasks[0].Op != runtime.OpMakeClosure {
		t.Fatalf("unexpected outer tasks: %+v", tasks)
	}
	data, ok := tasks[0].Data.(*runtime.ClosureData)
	if !ok {
		t.Fatalf("unexpected closure data: %T", tasks[0].Data)
	}

	var sawLocalDecl, sawParamLoad, sawLocalLoad, sawGlobalLoad, sawBuiltinCall bool
	visitLoweredTasks(data.BodyTasks, func(task runtime.Task) {
		switch task.Op {
		case runtime.OpDeclareInitVars:
			decl := task.Data.(*runtime.VarDeclData)
			for _, binding := range decl.Bindings {
				if binding.Name == "y" && binding.Sym.Kind == runtime.SymbolLocal && decl.Mode == runtime.VarDeclInitZero {
					sawLocalDecl = true
				}
			}
		case runtime.OpLoadVar, runtime.OpLoadLocal, runtime.OpLoadUpvalue:
			name, kind, ok := taskLoadSymbol(task)
			if !ok {
				return
			}
			switch name {
			case "x":
				sawParamLoad = kind == runtime.SymbolLocal
			case "y":
				sawLocalLoad = kind == runtime.SymbolLocal
			case "g":
				sawGlobalLoad = kind == runtime.SymbolGlobal
			}
		case runtime.OpCall:
			call := task.Data.(*runtime.CallData)
			if call.Name == "len" {
				sawBuiltinCall = call.Sym.Kind == runtime.SymbolBuiltin
			}
		}
	})

	if !sawLocalDecl || !sawParamLoad || !sawLocalLoad || !sawGlobalLoad || !sawBuiltinCall {
		t.Fatalf("missing symbol annotations: decl=%v param=%v local=%v global=%v builtin=%v", sawLocalDecl, sawParamLoad, sawLocalLoad, sawGlobalLoad, sawBuiltinCall)
	}
}

func TestFuncLitClosureCarriesLoweredBodyTasks(t *testing.T) {
	expr, err := gofrontend.NewConverter().ConvertExprSource(`func(x Int64) Int64 { return x + 1 }`)
	if err != nil {
		t.Fatalf("convert expr failed: %v", err)
	}
	program := emptyProgram()
	program.Main = []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{expr}}}

	prepared, err := PrepareProgram(program)
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	var closure *runtime.ClosureData
	visitLoweredTasks(prepared.MainTasks, func(task runtime.Task) {
		if task.Op == runtime.OpMakeClosure {
			closure, _ = task.Data.(*runtime.ClosureData)
		}
	})
	if closure == nil || len(closure.BodyTasks) == 0 {
		t.Fatalf("expected lowered closure body tasks, got %+v", prepared.MainTasks)
	}
}

func TestPrepareProgramReturnsErrorForUnsupportedStmt(t *testing.T) {
	program := emptyProgram()
	program.Main = []ast.Stmt{&ast.BadStmt{
		BaseNode: ast.BaseNode{ID: "bad", Meta: "bad_stmt", Loc: &ast.Position{F: "bad.mgo", L: 3, C: 2}},
		RawText:  "}",
	}}

	_, err := PrepareProgram(program)
	var loweringErr *Error
	if !errors.As(err, &loweringErr) {
		t.Fatalf("expected lowering error, got %T %v", err, err)
	}
	if loweringErr.Op != "stmt" || loweringErr.Line != 3 || loweringErr.Col != 2 {
		t.Fatalf("unexpected lowering error metadata: %+v", loweringErr)
	}
}

func TestPrepareProgramReturnsErrorForInvalidCanonicalType(t *testing.T) {
	program := emptyProgram()
	program.Main = []ast.Stmt{&ast.GenDeclStmt{
		BaseNode: ast.BaseNode{ID: "decl", Meta: "decl", Loc: &ast.Position{F: "bad.mgo", L: 4, C: 1}},
		Bindings: []ast.VarBinding{{
			Name: "items",
			Kind: "[]Int64",
		}},
	}}

	_, err := PrepareProgram(program)
	var loweringErr *Error
	if !errors.As(err, &loweringErr) {
		t.Fatalf("expected lowering error, got %T %v", err, err)
	}
	if loweringErr.Op != "declaration" {
		t.Fatalf("unexpected lowering op: %+v", loweringErr)
	}
}

func TestLoweringHandlesTypedNilASTNodes(t *testing.T) {
	program := emptyProgram()
	b := newBuilder(nil, program.Variables, program.Functions)

	var nilBlock *ast.BlockStmt
	var nilExpr *ast.IdentifierExpr
	var nilIndex *ast.IndexExpr
	var nilMember *ast.MemberExpr
	var nilStar *ast.StarExpr

	stmtCases := []ast.Stmt{
		nilBlock,
		&ast.IfStmt{Body: nilBlock, ElseBody: nilBlock},
		&ast.RangeStmt{Define: true, Value: "item", Body: nilBlock},
		&ast.TryStmt{Body: nilBlock, Catch: &ast.CatchClause{VarName: "err", Body: nilBlock}, Finally: nilBlock},
	}
	for _, stmt := range stmtCases {
		if _, ok := b.lowerStmtTasks(stmt, nil, b.newRootLoweringScope()); !ok {
			t.Fatalf("expected typed-nil stmt %T to lower safely", stmt)
		}
	}

	exprCases := []ast.Expr{
		nilExpr,
		nilIndex,
		(*ast.CallExprStmt)(nil),
	}
	for _, expr := range exprCases {
		if _, ok := b.lowerExprTasks(expr, b.newRootLoweringScope()); !ok {
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
		if _, ok := b.lowerLHSTasks(expr, b.newRootLoweringScope()); !ok {
			t.Fatalf("expected typed-nil lhs %T to lower safely", expr)
		}
	}
}
