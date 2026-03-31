package runtime

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestLowerExprTasksBuildsDataOnlyCallPlan(t *testing.T) {
	exec, err := NewExecutor(&ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})
	if err != nil {
		t.Fatalf("new executor failed: %v", err)
	}

	expr, err := ffigo.NewGoToASTConverter().ConvertExprSource(`sum(1, 2)`)
	if err != nil {
		t.Fatalf("convert expr failed: %v", err)
	}

	tasks, ok := exec.lowerExprTasks(expr)
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
		if tasks[i].Node != nil {
			t.Fatalf("unexpected AST node embedded in lowered task: %+v", tasks[i])
		}
	}
	if callTask == nil {
		t.Fatal("expected call task in lowered expression")
	}
	if callTask.Node != nil {
		t.Fatalf("expected lowered call task without AST node, got %T", callTask.Node)
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
	exec, err := NewExecutor(&ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})
	if err != nil {
		t.Fatalf("new executor failed: %v", err)
	}

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

	tasks, ok := exec.lowerStmtTasks(stmts[0], nil)
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
	if branchTask.Node != nil {
		t.Fatalf("expected lowered branch task without AST node, got %T", branchTask.Node)
	}

	data, ok := branchTask.Data.(*BranchData)
	if !ok {
		t.Fatalf("unexpected branch task data: %T", branchTask.Data)
	}
	if len(data.Then) == 0 || len(data.Else) == 0 {
		t.Fatalf("expected non-empty branch plans: %+v", data)
	}
	for _, task := range data.Then {
		if task.Op == OpAssign && task.Node != nil {
			t.Fatalf("expected lowered then-branch assignment without AST node, got %T", task.Node)
		}
	}
	for _, task := range data.Else {
		if task.Op == OpAssign && task.Node != nil {
			t.Fatalf("expected lowered else-branch assignment without AST node, got %T", task.Node)
		}
	}
}

func TestLowerStmtTasksBuildsDataOnlyForPlan(t *testing.T) {
	exec, err := NewExecutor(&ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})
	if err != nil {
		t.Fatalf("new executor failed: %v", err)
	}

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

	tasks, ok := exec.lowerStmtTasks(stmts[0], nil)
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
	if loopTask.Node != nil {
		t.Fatalf("expected lowered for-loop without AST node, got %T", loopTask.Node)
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
	exec, err := NewExecutor(&ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})
	if err != nil {
		t.Fatalf("new executor failed: %v", err)
	}

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

	tasks, ok := exec.lowerStmtTasks(stmts[0], nil)
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
	if rangeTask.Node != nil {
		t.Fatalf("expected lowered range without AST node, got %T", rangeTask.Node)
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
	exec, err := NewExecutor(&ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})
	if err != nil {
		t.Fatalf("new executor failed: %v", err)
	}

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

	tasks, ok := exec.lowerStmtTasks(tryStmt, nil)
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
	if finallyTask.Node != nil || catchTask.Node != nil {
		t.Fatalf("expected lowered try tasks without AST nodes: finally=%T catch=%T", finallyTask.Node, catchTask.Node)
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
	exec, err := NewExecutor(&ast.ProgramStmt{
		BaseNode:  ast.BaseNode{ID: "test"},
		Constants: make(map[string]string),
		Variables: make(map[ast.Ident]ast.Expr),
		Types:     make(map[ast.Ident]ast.GoMiniType),
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Functions: make(map[ast.Ident]*ast.FunctionStmt),
	})
	if err != nil {
		t.Fatalf("new executor failed: %v", err)
	}

	stmts, err := ffigo.NewGoToASTConverter().ConvertStmtsSource(`defer cleanup(1)`)
	if err != nil {
		t.Fatalf("convert stmts failed: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("unexpected stmt count: %d", len(stmts))
	}

	tasks, ok := exec.lowerStmtTasks(stmts[0], nil)
	if !ok {
		t.Fatal("expected defer statement to be lowered")
	}
	if len(tasks) != 1 || tasks[0].Op != OpScheduleDefer {
		t.Fatalf("unexpected defer lowering: %+v", tasks)
	}
	if tasks[0].Node != nil {
		t.Fatalf("expected lowered defer task without AST node, got %T", tasks[0].Node)
	}
	data, ok := tasks[0].Data.(*DeferData)
	if !ok {
		t.Fatalf("unexpected defer data: %T", tasks[0].Data)
	}
	if len(data.Tasks) == 0 {
		t.Fatalf("expected deferred task payload, got: %+v", data)
	}
}
