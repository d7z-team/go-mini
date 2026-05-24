package e2e_test

import (
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/ffilib"
)

func newStdExecutor() *engine.MiniExecutor {
	executor := engine.NewMiniExecutor()
	if err := executor.UseSurface(ffilib.Surface()); err != nil {
		panic(err)
	}
	return executor
}

func buildPipelineFixture(t *testing.T, modulePath, helperSource, mainSource string) (*engine.MiniExecutor, *compiler.Artifact) {
	t.Helper()

	exec := newStdExecutor()
	helperCompiled, err := exec.CompileGoCode(helperSource)
	if err != nil {
		t.Fatalf("compile helper module failed: %v", err)
	}
	helperProg, err := exec.NewRuntimeByCompiled(helperCompiled)
	if err != nil {
		t.Fatalf("load helper module failed: %v", err)
	}
	exec.SetModuleLoader(func(path string) (*ast.ProgramStmt, error) {
		if path == modulePath {
			return helperCompiled.Program, nil
		}
		return nil, fmt.Errorf("module not found: %s", path)
	})
	exec.RegisterModule(modulePath, helperProg)

	compiled, err := exec.CompileGoCode(mainSource)
	if err != nil {
		t.Fatalf("compile main program failed: %v", err)
	}
	return exec, compiled
}
