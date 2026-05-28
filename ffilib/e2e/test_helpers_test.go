package e2e_test

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/surface"
	"gopkg.d7z.net/go-mini/ffilib"
)

func newStdExecutor() *engine.MiniExecutor {
	executor := engine.MustNewMiniExecutor()
	if err := executor.UseSurface(ffilib.Surface()); err != nil {
		panic(err)
	}
	return executor
}

func buildPipelineFixture(t *testing.T, modulePath, helperSource, mainSource string) (*engine.MiniExecutor, *compiler.Artifact) {
	t.Helper()

	exec := newStdExecutor()
	if err := exec.UseSurface(surface.Library(modulePath, surface.GoFile(modulePath+".mgo", helperSource))); err != nil {
		t.Fatalf("register helper module failed: %v", err)
	}

	compiled, err := exec.CompileGoCode(mainSource)
	if err != nil {
		t.Fatalf("compile main program failed: %v", err)
	}
	return exec, compiled
}
