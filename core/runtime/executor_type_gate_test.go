package runtime

import (
	"context"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestEvalComparisonRejectsDifferentPrimitiveTypes(t *testing.T) {
	exec := newEmptyExecutor(t)

	_, err := exec.evalComparison("==", NewInt(10), NewString("10"))
	if err == nil {
		t.Fatal("expected primitive type mismatch comparison to fail")
	}
	if !strings.Contains(err.Error(), "unsupported equality comparison") {
		t.Fatalf("unexpected comparison error: %v", err)
	}
}

func TestEvalPlusRejectsStringNumberMix(t *testing.T) {
	exec := newEmptyExecutor(t)

	_, err := exec.evalBinaryExprDirect("Plus", NewString("x"), NewInt(1))
	if err == nil {
		t.Fatal("expected mixed string and number addition to fail")
	}
	if !strings.Contains(err.Error(), "non-numeric") {
		t.Fatalf("unexpected addition error: %v", err)
	}
}

func TestAddressAssignmentEnforcesMapKeyAndValueTypes(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	m := &Var{
		VType:    TypeMap,
		TypeInfo: MustParseRuntimeType("Map<Int64,String>"),
		Ref:      &VMMap{Data: map[string]*Var{"i:1": NewString("one")}},
	}

	err := exec.assignAddress(session, &LHSIndex{Obj: m, Index: NewString("1")}, NewString("bad"))
	if err == nil || !strings.Contains(err.Error(), "expected Int64") {
		t.Fatalf("expected map key type rejection, got %v", err)
	}

	err = exec.assignAddress(session, &LHSIndex{Obj: m, Index: NewInt(1)}, NewInt(2))
	if err == nil || !strings.Contains(err.Error(), "cannot assign Int64 to String") {
		t.Fatalf("expected map value type rejection, got %v", err)
	}
}

func TestAppendRejectsElementTypeMismatch(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	arr := &Var{
		VType:    TypeArray,
		TypeInfo: MustParseRuntimeType("Array<Int64>"),
		Ref:      &VMArray{Data: []*Var{NewInt(1)}},
	}

	err := exec.invokeCall(session, "append", nil, nil, nil, []*Var{arr, NewString("x")}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot assign String to Int64") {
		t.Fatalf("expected append element type rejection, got %v", err)
	}
}

func TestPrepareAnyRejectsNestedPointer(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	ptr := exec.newSlotPointer(MustParseRuntimeType("Int64"), NewSlot(MustParseRuntimeType("Int64"), NewInt(1)))
	arr := &Var{
		VType:    TypeArray,
		TypeInfo: MustParseRuntimeType("Array<Any>"),
		Ref:      &VMArray{Data: []*Var{ptr}},
	}

	_, err := exec.prepareValueForType(session, arr, MustParseRuntimeType("Any"))
	if err == nil || !strings.Contains(err.Error(), "VM pointer") {
		t.Fatalf("expected nested pointer Any rejection, got %v", err)
	}
}

func TestFFIEncodePrimitiveRejectsWrongRuntimeType(t *testing.T) {
	exec := newEmptyExecutor(t)
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)

	err := exec.serializeParsedType(buf, NewString("1"), MustParseRuntimeType("Int64"))
	if err == nil || !strings.Contains(err.Error(), "expected Int64") {
		t.Fatalf("expected FFI primitive type rejection, got %v", err)
	}
}
