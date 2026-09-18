package runtime

import (
	"context"
	"testing"
)

func byteArrayText(t *testing.T, exec *Executor, v *Var) string {
	t.Helper()
	data, err := exec.byteSliceFromArray(v)
	if err != nil {
		t.Fatalf("read byte array failed: %v", err)
	}
	return string(data)
}

func TestEvalFFICopyBackWritesToMemberField(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	holder := &Var{
		VType:    TypeMap,
		TypeInfo: MustParseRuntimeType("Map<String,Array<Byte>>"),
		Ref: &VMMap{Data: map[string]*Var{
			"buf": NewByteArray([]byte("xy")),
		}},
	}

	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  copyBackFFIBridge{returnValue: []byte("ret")},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Array<Byte>", FFIParamInOutBytes),
	}

	arg := holder.Ref.(*VMMap).Data["buf"]
	res, err := exec.evalFFI(session, route, []*Var{arg}, []LHSValue{&LHSMember{Obj: holder, Property: "buf"}})
	if err != nil {
		t.Fatalf("evalFFI failed: %v", err)
	}
	if byteArrayText(t, exec, res) != "ret" {
		t.Fatalf("unexpected ffi return: %#v", res)
	}
	got := holder.Ref.(*VMMap).Data["buf"]
	if got == nil || got.VType != TypeArray || byteArrayText(t, exec, got) != "XY!" {
		t.Fatalf("unexpected member copy-back: %#v", got)
	}
}

func TestEvalFFICopyBackWritesToArrayIndex(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	arr := &Var{
		VType:    TypeArray,
		TypeInfo: MustParseRuntimeType("Array<Array<Byte>>"),
		Ref: &VMArray{Data: []*Var{
			NewByteArray([]byte("aa")),
			NewByteArray([]byte("bc")),
		}},
	}

	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  copyBackFFIBridge{returnValue: []byte("ret")},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Array<Byte>", FFIParamInOutBytes),
	}

	arg := arr.Ref.(*VMArray).Data[1]
	_, err := exec.evalFFI(session, route, []*Var{arg}, []LHSValue{&LHSIndex{Obj: arr, Index: NewInt(1)}})
	if err != nil {
		t.Fatalf("evalFFI failed: %v", err)
	}
	got := arr.Ref.(*VMArray).Data[1]
	if got == nil || got.VType != TypeArray || byteArrayText(t, exec, got) != "BC!" {
		t.Fatalf("unexpected index copy-back: %#v", got)
	}
}

func TestEvalFFICopyBackWritesWholeArrayBackToCaller(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	if err := session.NewVar("arr", MustParseRuntimeType("Array<Int64>")); err != nil {
		t.Fatalf("new var failed: %v", err)
	}

	arr := &Var{
		VType: TypeArray,
		Ref: &VMArray{Data: []*Var{
			NewInt(1),
			NewInt(2),
			NewInt(3),
			NewInt(4),
		}},
		TypeInfo: MustParseRuntimeType("Array<Int64>"),
	}

	route := FFIRoute{
		Name:   "demo.Rewrite",
		Bridge: arrayCopyBackFFIBridge{returnValue: int64(3), replace: []int64{20, 30, 40}},
		FuncSig: MustParseRuntimeFuncSigWithModes(
			"function(Array<Int64>) Int64",
			FFIParamInOutArray,
		),
	}

	if err := session.Store("arr", arr); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	arg, err := session.Load("arr")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	res, err := exec.evalFFI(session, route, []*Var{arg}, []LHSValue{&LHSEnv{Name: "arr"}})
	if err != nil {
		t.Fatalf("evalFFI failed: %v", err)
	}
	if res == nil || res.VType != TypeInt || res.I64 != 3 {
		t.Fatalf("unexpected ffi return: %#v", res)
	}
	updated, err := session.Load("arr")
	if err != nil {
		t.Fatalf("load updated failed: %v", err)
	}
	items := updated.Ref.(*VMArray).Snapshot()
	if len(items) != 3 || items[0].I64 != 20 || items[1].I64 != 30 || items[2].I64 != 40 {
		t.Fatalf("unexpected array copy-back result: %#v", items)
	}
}

func TestEvalFFICopyBackMutatesSharedArrayArgument(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	if err := session.NewVar("caller", MustParseRuntimeType("Array<Byte>")); err != nil {
		t.Fatalf("new caller failed: %v", err)
	}
	if err := session.NewVar("param", MustParseRuntimeType("Array<Byte>")); err != nil {
		t.Fatalf("new param failed: %v", err)
	}
	shared := NewByteArray([]byte(".."))
	if err := session.Store("caller", shared); err != nil {
		t.Fatalf("store caller failed: %v", err)
	}
	if err := session.Store("param", shared); err != nil {
		t.Fatalf("store param failed: %v", err)
	}

	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  copyBackFFIBridge{returnValue: []byte("ret")},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Array<Byte>", FFIParamInOutBytes),
	}
	arg, err := session.Load("param")
	if err != nil {
		t.Fatalf("load param failed: %v", err)
	}
	_, err = exec.evalFFI(session, route, []*Var{arg}, []LHSValue{&LHSEnv{Name: "param"}})
	if err != nil {
		t.Fatalf("evalFFI failed: %v", err)
	}
	caller, err := session.Load("caller")
	if err != nil {
		t.Fatalf("load caller failed: %v", err)
	}
	if got := byteArrayText(t, exec, caller); got != "..!" {
		t.Fatalf("shared caller array was not mutated, got %q", got)
	}
}

func TestEvalFFICopyBackWritesSliceWindowArgument(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	arr := &Var{
		VType: TypeArray,
		Ref: &VMArray{Data: []*Var{
			NewInt(1),
			NewInt(2),
			NewInt(3),
			NewInt(4),
		}},
		TypeInfo: MustParseRuntimeType("Array<Int64>"),
	}

	route := FFIRoute{
		Name:   "demo.Rewrite",
		Bridge: arrayCopyBackFFIBridge{returnValue: int64(0), replace: []int64{9}},
		FuncSig: MustParseRuntimeFuncSigWithModes(
			"function(Array<Int64>) Int64",
			FFIParamInOutArray,
		),
	}

	_, err := exec.evalFFI(session, route, []*Var{
		{VType: TypeArray, Ref: &VMArray{Data: []*Var{NewInt(1), NewInt(2)}}},
	}, []LHSValue{
		&LHSSlice{Obj: arr, Low: NewInt(0), High: NewInt(2)},
	})
	if err != nil {
		t.Fatalf("evalFFI failed: %v", err)
	}
	items := arr.Ref.(*VMArray).Snapshot()
	if len(items) != 3 || items[0].I64 != 9 || items[1].I64 != 3 || items[2].I64 != 4 {
		t.Fatalf("unexpected slice copy-back result: %#v", items)
	}
}
