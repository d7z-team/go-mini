package engine

import (
	"testing"
	"time"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestUnpackEvalResultSingleValue(t *testing.T) {
	expr := &ast.LiteralExpr{}
	expr.BaseNode.Type = "Int64"
	res := runtime.NewInt(7)

	got := unpackEvalResult(expr, res)
	if len(got) != 1 {
		t.Fatalf("unpackEvalResult() returned %d values, want 1", len(got))
	}
	if got[0] == nil || got[0].I64 != 7 {
		t.Fatalf("unexpected result: %#v", got[0])
	}
}

func TestUnpackEvalResultTupleValue(t *testing.T) {
	expr := &ast.CallExprStmt{}
	expr.BaseNode.Type = "tuple(Int64, String)"
	res := &runtime.Var{
		VType:    runtime.TypeArray,
		TypeInfo: runtime.MustParseRuntimeType("tuple(Int64, String)"),
		Ref: &runtime.VMArray{Data: []*runtime.Var{
			runtime.NewInt(7),
			runtime.NewString("ok"),
		}},
	}

	got := unpackEvalResult(expr, res)
	if len(got) != 2 {
		t.Fatalf("unpackEvalResult() returned %d values, want 2", len(got))
	}
	if got[0] == nil || got[0].I64 != 7 {
		t.Fatalf("unexpected first result: %#v", got[0])
	}
	if got[1] == nil || got[1].Str != "ok" {
		t.Fatalf("unexpected second result: %#v", got[1])
	}
}

func TestModuleASTLoaderDoesNotHoldLockDuringExternalLoader(t *testing.T) {
	exec := NewMiniExecutor()
	exec.SetModuleLoader(func(path string) (*ast.ProgramStmt, error) {
		exec.RegisterConstant("loaderTouched", "1")
		return &ast.ProgramStmt{
			BaseNode:   ast.BaseNode{ID: "module", Meta: "boot"},
			Package:    path,
			Constants:  map[string]string{},
			Variables:  map[ast.Ident]ast.Expr{},
			Types:      map[ast.Ident]ast.GoMiniType{},
			Structs:    map[ast.Ident]*ast.StructStmt{},
			Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
			Functions:  map[ast.Ident]*ast.FunctionStmt{},
		}, nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := exec.moduleASTLoader()("dep")
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("module loader failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("module loader appears to hold executor lock while invoking external loader")
	}
}

func TestRegisterModuleNilUnregistersPreparedModule(t *testing.T) {
	exec := NewMiniExecutor()
	prog, err := exec.NewRuntimeByGoCode(`
package helper
func Answer() Int64 { return 42 }
`)
	if err != nil {
		t.Fatalf("compile helper failed: %v", err)
	}

	exec.RegisterModule("helper", prog)
	if exec.modules["helper"] == nil {
		t.Fatal("expected prepared module to be registered")
	}

	exec.RegisterModule("helper", nil)
	if exec.modules["helper"] != nil {
		t.Fatal("expected prepared module to be removed")
	}
	if exec.moduleSources["helper"] != nil {
		t.Fatal("expected source module to be removed")
	}
}
