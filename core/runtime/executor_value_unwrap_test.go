package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestEvalUnaryDereferenceUnwrapsAnyAndCell(t *testing.T) {
	exec := newEmptyExecutor(t)

	ptr := &Var{
		VType:    TypeHandle,
		Handle:   1,
		TypeInfo: MustParseRuntimeType("Ptr<Int64>"),
		Ref:      NewInt(7),
	}
	cell := &Var{VType: TypeCell, Ref: &Cell{Value: ptr}}
	anyWrapped := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: cell}

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
		VType:    TypeMap,
		TypeInfo: MustParseRuntimeType("Map<String,Any>"),
		Ref: &VMMap{Data: map[string]*Var{
			"name": NewString("mini"),
			"x":    NewInt(1),
		}},
	}
	anyMap := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: innerMap}

	got, err := exec.evalMemberExprDirect(session, anyMap, "name")
	if err != nil {
		t.Fatalf("member access failed: %v", err)
	}
	if got == nil || got.VType != TypeString || got.Str != "mini" {
		t.Fatalf("unexpected member result: %#v", got)
	}

	if err := exec.invokeCall(session, "len", nil, nil, nil, []*Var{anyMap}, nil); err != nil {
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

	base := &Var{VType: TypeArray, Ref: &VMArray{Data: []*Var{NewInt(1)}}, TypeInfo: MustParseRuntimeType("[]Int64")}
	tailInner := &Var{VType: TypeArray, Ref: &VMArray{Data: []*Var{NewInt(2), NewInt(3)}}, TypeInfo: MustParseRuntimeType("[]Int64")}
	tail := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: tailInner}

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
		VType:    TypeMap,
		TypeInfo: MustParseRuntimeType("Map<String,Int64>"),
		Ref: &VMMap{Data: map[string]*Var{
			"k": NewInt(9),
		}},
	}
	wrapped := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: inner}
	session.ValueStack.Push(wrapped)
	session.ValueStack.Push(NewString("k"))

	if err := exec.dispatch(session, Task{Op: OpIndex, Data: &IndexData{
		Multi:      true,
		ResultType: MustParseRuntimeType("tuple(Int64, Bool)"),
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

func TestEvalIndexAndSliceUnwrapAnyContainers(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	innerArray := &Var{
		VType:    TypeArray,
		TypeInfo: MustParseRuntimeType("Array<Any>"),
		Ref: &VMArray{Data: []*Var{
			NewString("zero"),
			NewString("one"),
			NewString("two"),
		}},
	}
	anyArray := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: innerArray}
	anyIndex := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: NewInt(1)}

	got, err := exec.evalIndexExprDirect(session, anyArray, anyIndex)
	if err != nil {
		t.Fatalf("single index failed: %v", err)
	}
	if got == nil || got.VType != TypeString || got.Str != "one" {
		t.Fatalf("unexpected single index result: %#v", got)
	}

	low := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: NewInt(1)}
	high := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: NewInt(3)}
	sliced, err := exec.evalSliceExprDirect(session, anyArray, low, high)
	if err != nil {
		t.Fatalf("slice failed: %v", err)
	}
	if sliced == nil || sliced.VType != TypeArray {
		t.Fatalf("unexpected slice result: %#v", sliced)
	}
	items := sliced.Ref.(*VMArray).Data
	if len(items) != 2 || items[0].Str != "one" || items[1].Str != "two" {
		t.Fatalf("unexpected slice items: %#v", items)
	}
}

func TestEvalMemberChainOnMapStringAnyStillWorks(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	leaf := &Var{
		VType:    TypeMap,
		TypeInfo: MustParseRuntimeType("Map<String,Any>"),
		Ref: &VMMap{Data: map[string]*Var{
			"d": NewString("ok"),
		}},
	}
	levelC := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: leaf}
	levelB := &Var{
		VType:    TypeMap,
		TypeInfo: MustParseRuntimeType("Map<String,Any>"),
		Ref: &VMMap{Data: map[string]*Var{
			"c": levelC,
		}},
	}
	levelA := &Var{
		VType:    TypeMap,
		TypeInfo: MustParseRuntimeType("Map<String,Any>"),
		Ref: &VMMap{Data: map[string]*Var{
			"b": &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: levelB},
		}},
	}
	root := &Var{
		VType:    TypeMap,
		TypeInfo: MustParseRuntimeType("Map<String,Any>"),
		Ref: &VMMap{Data: map[string]*Var{
			"a": &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: levelA},
		}},
	}

	a, err := exec.evalMemberExprDirect(session, root, "a")
	if err != nil {
		t.Fatalf("member a failed: %v", err)
	}
	b, err := exec.evalMemberExprDirect(session, a, "b")
	if err != nil {
		t.Fatalf("member b failed: %v", err)
	}
	c, err := exec.evalMemberExprDirect(session, b, "c")
	if err != nil {
		t.Fatalf("member c failed: %v", err)
	}
	d, err := exec.evalMemberExprDirect(session, c, "d")
	if err != nil {
		t.Fatalf("member d failed: %v", err)
	}
	if d == nil || d.VType != TypeString || d.Str != "ok" {
		t.Fatalf("unexpected chained member result: %#v", d)
	}
}

func TestEvalMemberExprRejectsAnyWrappedScalar(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	scalar := &Var{VType: TypeAny, TypeInfo: MustParseRuntimeType("Any"), Ref: NewFloat(1.5)}
	_, err := exec.evalMemberExprDirect(session, scalar, "something")
	if err == nil {
		t.Fatal("expected Any-wrapped scalar member access to fail")
	}
	if err.Error() == "" || !strings.Contains(err.Error(), "does not support member access") {
		t.Fatalf("unexpected member access error: %v", err)
	}
}
