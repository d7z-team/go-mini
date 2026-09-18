package runtime_test

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestRuntimeTaskStack_NoAST(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	sourceProgram := `
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

	compiledArtifact, err := testExecutor.CompileGoCode(sourceProgram)
	if err != nil {
		t.Fatal(err)
	}

	executable := compiledArtifact.Bytecode.Executable
	if executable == nil {
		t.Fatal("compiled bytecode missing executable task plan")
	}
	for _, global := range executable.Globals {
		if global == nil {
			continue
		}
		for _, task := range global.InitPlan {
			assertTaskDataHasNoAST(t, task)
		}
	}
	for _, fn := range executable.Functions {
		if fn == nil {
			continue
		}
		for _, task := range fn.BodyTasks {
			assertTaskDataHasNoAST(t, task)
		}
	}
	for _, task := range executable.MainTasks {
		assertTaskDataHasNoAST(t, task)
	}
}

func assertTaskDataHasNoAST(t *testing.T, task runtime.Task) {
	// Traverse task.Data if it's a known Data struct and check for AST nodes
	switch d := task.Data.(type) {
	case *runtime.BranchData:
		for _, nestedTask := range d.Then {
			assertTaskDataHasNoAST(t, nestedTask)
		}
		for _, nestedTask := range d.Else {
			assertTaskDataHasNoAST(t, nestedTask)
		}
	case *runtime.ForData:
		for _, nestedTask := range d.Cond {
			assertTaskDataHasNoAST(t, nestedTask)
		}
		for _, nestedTask := range d.Body {
			assertTaskDataHasNoAST(t, nestedTask)
		}
		for _, nestedTask := range d.Update {
			assertTaskDataHasNoAST(t, nestedTask)
		}
	case *runtime.SwitchData:
		for _, nestedTask := range d.Init {
			assertTaskDataHasNoAST(t, nestedTask)
		}
		for _, nestedTask := range d.Tag {
			assertTaskDataHasNoAST(t, nestedTask)
		}
		for _, nestedTask := range d.AssignLHS {
			assertTaskDataHasNoAST(t, nestedTask)
		}
		for _, switchCase := range d.Cases {
			for _, caseExprTasks := range switchCase.Exprs {
				for _, nestedTask := range caseExprTasks {
					assertTaskDataHasNoAST(t, nestedTask)
				}
			}
			for _, nestedTask := range switchCase.Body {
				assertTaskDataHasNoAST(t, nestedTask)
			}
		}
		for _, nestedTask := range d.DefaultBody {
			assertTaskDataHasNoAST(t, nestedTask)
		}
	case *runtime.DoCallData:
		for _, nestedTask := range d.BodyTasks {
			assertTaskDataHasNoAST(t, nestedTask)
		}
	case *runtime.ClosureData:
		for _, nestedTask := range d.BodyTasks {
			assertTaskDataHasNoAST(t, nestedTask)
		}
	case *runtime.FinallyData:
		for _, nestedTask := range d.Body {
			assertTaskDataHasNoAST(t, nestedTask)
		}
	case *runtime.CatchData:
		for _, nestedTask := range d.Body {
			assertTaskDataHasNoAST(t, nestedTask)
		}
	}
}
