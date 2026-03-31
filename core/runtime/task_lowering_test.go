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
