package runtime

import (
	"context"
	"testing"
)

func TestEvalUnaryDereferenceUnwrapsAnyAndCell(t *testing.T) {
	exec := newEmptyExecutor(t)

	ptr := &Var{
		VType:  TypeHandle,
		Handle: 1,
		Type:   "Ptr<Int64>",
		Ref:    NewInt(7),
	}
	cell := &Var{VType: TypeCell, Ref: &Cell{Value: ptr}}
	anyWrapped := &Var{VType: TypeAny, Type: "Any", Ref: cell}

	got, err := exec.evalUnaryExprDirect("Dereference", anyWrapped)
	if err != nil {
		t.Fatalf("dereference failed: %v", err)
	}
	if got == nil || got.VType != TypeInt || got.I64 != 7 {
		t.Fatalf("unexpected dereference result: %#v", got)
	}
}

func TestEvalMemberAndBuiltinLenUnwrapAnyContainers(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	innerMap := &Var{
		VType: TypeMap,
		Type:  "Map<String,Any>",
		Ref: &VMMap{Data: map[string]*Var{
			"name": NewString("mini"),
			"x":    NewInt(1),
		}},
	}
	anyMap := &Var{VType: TypeAny, Type: "Any", Ref: innerMap}

	got, err := exec.evalMemberExprDirect(session, anyMap, "name")
	if err != nil {
		t.Fatalf("member access failed: %v", err)
	}
	if got == nil || got.VType != TypeString || got.Str != "mini" {
		t.Fatalf("unexpected member result: %#v", got)
	}

	if err := exec.invokeCall(session, "len", nil, nil, nil, []*Var{anyMap}); err != nil {
		t.Fatalf("len call failed: %v", err)
	}
	lenRes := session.ValueStack.Pop()
	if lenRes == nil || lenRes.VType != TypeInt || lenRes.I64 != 2 {
		t.Fatalf("unexpected len result: %#v", lenRes)
	}
}

func TestOpCallEllipsisUnwrapsAnyArray(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	base := &Var{VType: TypeArray, Ref: &VMArray{Data: []*Var{NewInt(1)}}, Type: "[]Int64"}
	tailInner := &Var{VType: TypeArray, Ref: &VMArray{Data: []*Var{NewInt(2), NewInt(3)}}, Type: "[]Int64"}
	tail := &Var{VType: TypeAny, Type: "Any", Ref: tailInner}

	session.ValueStack.Push(base)
	session.ValueStack.Push(tail)
	if err := exec.dispatch(session, Task{Op: OpCall, Data: &CallData{
		Mode:     CallByName,
		Name:     "append",
		ArgCount: 2,
		Ellipsis: true,
	}}); err != nil {
		t.Fatalf("dispatch call failed: %v", err)
	}

	got := session.ValueStack.Pop()
	if got == nil || got.VType != TypeArray {
		t.Fatalf("unexpected append result: %#v", got)
	}
	data := got.Ref.(*VMArray).Data
	if len(data) != 3 || data[0].I64 != 1 || data[1].I64 != 2 || data[2].I64 != 3 {
		t.Fatalf("unexpected appended data: %#v", data)
	}
}

func TestOpIndexMultiUnwrapsAnyMap(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	inner := &Var{
		VType: TypeMap,
		Type:  "Map<String,Int64>",
		Ref: &VMMap{Data: map[string]*Var{
			"k": NewInt(9),
		}},
	}
	wrapped := &Var{VType: TypeAny, Type: "Any", Ref: inner}
	session.ValueStack.Push(wrapped)
	session.ValueStack.Push(NewString("k"))

	if err := exec.dispatch(session, Task{Op: OpIndex, Data: &IndexData{
		Multi:      true,
		ResultType: "tuple(Int64, Bool)",
	}}); err != nil {
		t.Fatalf("dispatch index failed: %v", err)
	}

	got := session.ValueStack.Pop()
	if got == nil || got.VType != TypeArray {
		t.Fatalf("unexpected multi-index result: %#v", got)
	}
	items := got.Ref.(*VMArray).Data
	if len(items) != 2 || items[0].I64 != 9 || !items[1].Bool {
		t.Fatalf("unexpected multi-index tuple: %#v", items)
	}
}
