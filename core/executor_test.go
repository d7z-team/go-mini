package engine

import (
	"testing"

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
		VType: runtime.TypeArray,
		Type:  "tuple(Int64, String)",
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
