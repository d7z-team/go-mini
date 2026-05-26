package tests

import (
	"context"
	"errors"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

func TestVariadic(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func sum(args ...Int64) Int64 {
		var s Int64 = 0
		for _, v := range args {
			s = s + v
		}
		return s
	}
	
	func main() {
		// Case 1: Normal variadic call
		s1 := sum(1, 2, 3)
		if s1 != 6 { panic("s1 should be 6") }
		
		// Case 2: Ellipsis call
		args := []Int64{4, 5, 6}
		s2 := sum(args...)
		if s2 != 15 { panic("s2 should be 15") }
		
		// Case 3: Mixed (not supported in Go for user functions with ..., but we test expansion)
		// Actually Go doesn't allow mixed, but let's see how our expansion handles it.
		// In Go, f(a, b, c...) is allowed if c is the variadic part.
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

func TestFFIVariadicEllipsis(t *testing.T) {
	executor := engine.NewMiniExecutor()
	testsurface.UseRoute(t, executor, "mock.Printf", emptyVariadicBridge{}, 1, runtime.MustParseRuntimeFuncSigWithModes("function(String, ...Any) Void", runtime.FFIParamIn, runtime.FFIParamIn), "")
	code := `
	package main
	import "mock"
	
	func main() {
		args := []Any{"hello %s %d", "world", 42}
		format := args[0].(String)
		params := args[1:]
		mock.Printf(format + "\n", params...)
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

type emptyVariadicBridge struct{}

func (emptyVariadicBridge) Call(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, nil
}

func (emptyVariadicBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, errors.New("unexpected invoke")
}

func (emptyVariadicBridge) DestroyHandle(uint32) error {
	return nil
}
