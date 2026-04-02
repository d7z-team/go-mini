package testutil

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func NewExecutor(tb testing.TB) *engine.MiniExecutor {
	tb.Helper()
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()
	return executor
}

func Program(tb testing.TB, code string) *engine.MiniProgram {
	tb.Helper()
	prog, err := NewExecutor(tb).NewRuntimeByGoCode(code)
	if err != nil {
		tb.Fatalf("compile failed: %v", err)
	}
	return prog
}

func Run(tb testing.TB, code string) {
	tb.Helper()
	if err := Program(tb, code).Execute(context.Background()); err != nil {
		tb.Fatalf("execute failed: %v", err)
	}
}

func Eval(tb testing.TB, code, expr string) *runtime.Var {
	tb.Helper()
	res, err := Program(tb, code).Eval(context.Background(), expr, nil)
	if err != nil {
		tb.Fatalf("eval %q failed: %v", expr, err)
	}
	return res
}
