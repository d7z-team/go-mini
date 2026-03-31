package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestNoASTInRuntime(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	func main() {
		a := 10
		b := 20
		if a < b {
			sum := 0
			for i := 0; i < 5; i++ {
				sum = sum + i
			}
		}
		
		f := func(x int) int {
			return x * 2
		}
		_ = f(10)
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeTaskStack_NoAST(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	func main() int {
		a := 10
		b := 20
		if a < b {
			return a + b
		}
		return 0
	}
	`
	runtimeExec := executor.Executor()
	
	artifact, err := executor.CompileGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	
	for _, stmt := range artifact.Program.Main {
		tasks := runtimeExec.TasksForStmt(stmt)
		for _, task := range tasks {
			checkTaskDataNoAST(t, task)
		}
	}
}

func checkTaskDataNoAST(t *testing.T, task runtime.Task) {
	// Traverse task.Data if it's a known Data struct and check for AST nodes
	switch d := task.Data.(type) {
	case *runtime.BranchData:
		for _, tt := range d.Then { checkTaskDataNoAST(t, tt) }
		for _, tt := range d.Else { checkTaskDataNoAST(t, tt) }
	case *runtime.ForData:
		for _, tt := range d.Cond { checkTaskDataNoAST(t, tt) }
		for _, tt := range d.Body { checkTaskDataNoAST(t, tt) }
		for _, tt := range d.Update { checkTaskDataNoAST(t, tt) }
	case *runtime.SwitchData:
		for _, tt := range d.Init { checkTaskDataNoAST(t, tt) }
		for _, tt := range d.Tag { checkTaskDataNoAST(t, tt) }
		for _, tt := range d.AssignLHS { checkTaskDataNoAST(t, tt) }
		for _, c := range d.Cases {
			for _, ee := range c.Exprs {
				for _, tt := range ee { checkTaskDataNoAST(t, tt) }
			}
			for _, tt := range c.Body { checkTaskDataNoAST(t, tt) }
		}
		for _, tt := range d.DefaultBody { checkTaskDataNoAST(t, tt) }
	case *runtime.DoCallData:
		for _, tt := range d.BodyTasks { checkTaskDataNoAST(t, tt) }
	case *runtime.ClosureData:
		for _, tt := range d.BodyTasks { checkTaskDataNoAST(t, tt) }
	case *runtime.FinallyData:
		for _, tt := range d.Body { checkTaskDataNoAST(t, tt) }
	case *runtime.CatchData:
		for _, tt := range d.Body { checkTaskDataNoAST(t, tt) }
	}
}
