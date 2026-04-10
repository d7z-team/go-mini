package runtime

import (
	"context"
	"testing"
)

func TestEvalFFICopyBackWritesToMemberField(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	holder := &Var{
		VType: TypeMap,
		Ref: &VMMap{Data: map[string]*Var{
			"buf": NewBytes([]byte("xy")),
		}},
	}

	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  copyBackFFIBridge{returnValue: []byte("ret")},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(TypeBytes) TypeBytes", FFIParamInOutBytes),
	}

	arg := holder.Ref.(*VMMap).Data["buf"]
	res, err := exec.evalFFI(session, route, []*Var{arg}, []LHSValue{&LHSMember{Obj: holder, Property: "buf"}})
	if err != nil {
		t.Fatalf("evalFFI failed: %v", err)
	}
	if res == nil || string(res.B) != "ret" {
		t.Fatalf("unexpected ffi return: %#v", res)
	}
	got := holder.Ref.(*VMMap).Data["buf"]
	if got == nil || got.VType != TypeBytes || string(got.B) != "XY!" {
		t.Fatalf("unexpected member copy-back: %#v", got)
	}
}

func TestEvalFFICopyBackWritesToArrayIndex(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")

	arr := &Var{
		VType: TypeArray,
		Ref: &VMArray{Data: []*Var{
			NewBytes([]byte("aa")),
			NewBytes([]byte("bc")),
		}},
	}

	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  copyBackFFIBridge{returnValue: []byte("ret")},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(TypeBytes) TypeBytes", FFIParamInOutBytes),
	}

	arg := arr.Ref.(*VMArray).Data[1]
	_, err := exec.evalFFI(session, route, []*Var{arg}, []LHSValue{&LHSIndex{Obj: arr, Index: NewInt(1)}})
	if err != nil {
		t.Fatalf("evalFFI failed: %v", err)
	}
	got := arr.Ref.(*VMArray).Data[1]
	if got == nil || got.VType != TypeBytes || string(got.B) != "BC!" {
		t.Fatalf("unexpected index copy-back: %#v", got)
	}
}
