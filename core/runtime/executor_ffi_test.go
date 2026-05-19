package runtime

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type testFFIBridge struct{}

func (testFFIBridge) Call(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, nil
}

func (testFFIBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, nil
}
func (testFFIBridge) DestroyHandle(uint32) error { return nil }

func TestSerializeVarToAnyUsesStructSchemaOrder(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
	}
	schema := MustParseRuntimeStructSpec("demo.Point", StructOwnershipVMValue, "struct { X Int64; Y Int64; }")
	exec.metadata.registerStructSchema(schema.Name, schema)

	v := &Var{
		VType:    TypeStruct,
		TypeInfo: MustParseRuntimeType("demo.Point"),
		Ref: &VMStruct{
			Spec: schema,
			Fields: []*Slot{
				NewSlot(MustParseRuntimeType("Int64"), NewInt(10)),
				NewSlot(MustParseRuntimeType("Int64"), NewInt(20)),
			},
			ByName: map[string]int{"X": 0, "Y": 1},
		},
	}

	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	exec.serializeVarToAny(buf, v)

	decoded := ffigo.NewReader(buf.Bytes()).ReadAny()
	vmStruct, ok := decoded.(*ffigo.VMStruct)
	if !ok {
		t.Fatalf("expected VMStruct, got %T", decoded)
	}
	if len(vmStruct.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(vmStruct.Fields))
	}
	if vmStruct.Fields[0].Name != "X" || vmStruct.Fields[1].Name != "Y" {
		t.Fatalf("unexpected field order: %#v", vmStruct.Fields)
	}
}

func TestToVarDecodesPointerAndStructAnyValues(t *testing.T) {
	exec := &Executor{}

	ptrVal := exec.ToVar(nil, &ffigo.VMPointer{Value: int64(7)}, nil)
	if ptrVal == nil || ptrVal.VType != TypeHandle {
		t.Fatalf("expected handle-like pointer, got %#v", ptrVal)
	}
	slot, ok := ptrVal.Ref.(*Slot)
	if !ok || slot.Value == nil || slot.Value.VType != TypeInt || slot.Value.I64 != 7 {
		t.Fatalf("unexpected pointer payload: %#v", ptrVal.Ref)
	}
	if ptrVal.Bridge != nil {
		t.Fatalf("vm pointer should not carry host bridge: %#v", ptrVal)
	}

	structVal := exec.ToVar(nil, &ffigo.VMStruct{Fields: []ffigo.StructField{
		{Name: "Msg", Value: "ok"},
		{Name: "Count", Value: int64(2)},
	}}, nil)
	if structVal == nil || structVal.VType != TypeStruct {
		t.Fatalf("expected VM struct, got %#v", structVal)
	}
	data := structVal.Ref.(*VMStruct)
	msg, _ := data.Field("Msg")
	count, _ := data.Field("Count")
	if msg.Value.Str != "ok" || count.Value.I64 != 2 {
		t.Fatalf("unexpected decoded struct data: %#v", data)
	}
}

func TestLookupStructSchemaUsesCanonicalIndexes(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
	}
	schema := MustParseRuntimeStructSpec("demo.Type", StructOwnershipVMValue, "struct { Value Int64; }")
	exec.metadata.registerStructSchema("demo.Type", schema)

	typ, err := ParseRuntimeType("Ptr<demo.Type>")
	if err != nil {
		t.Fatalf("ParseRuntimeType failed: %v", err)
	}
	resolved, ok := exec.lookupStructSchema(typ)
	if !ok || resolved == nil {
		t.Fatal("expected canonical struct schema lookup to succeed")
	}
	if resolved.TypeID != "demo.Type" {
		t.Fatalf("unexpected resolved schema: %+v", resolved)
	}
}

func TestSerializeVarToAnyKeepsHostRefOpaque(t *testing.T) {
	exec := &Executor{}
	v := &Var{VType: TypeHostRef, Handle: 42, Bridge: testFFIBridge{}}

	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	exec.serializeVarToAny(buf, v)

	decoded := ffigo.NewReader(buf.Bytes()).ReadAny()
	id, ok := decoded.(uint32)
	if !ok || id != 42 {
		t.Fatalf("expected opaque handle id, got %#v", decoded)
	}
}

func TestSerializeHostRefRejectsVMCreatedValue(t *testing.T) {
	exec := &Executor{}
	typ := MustParseRuntimeType("HostRef<demo.Handle>")
	vmValue := &Var{
		VType:    TypeMap,
		TypeInfo: MustParseRuntimeType("demo.Handle"),
		Ref:      &VMMap{Data: map[string]*Var{}},
	}

	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	err := exec.serializeParsedType(buf, vmValue, typ)
	if err == nil || !strings.Contains(err.Error(), "expected opaque host reference") {
		t.Fatalf("expected HostRef serialization rejection, got %v", err)
	}
}

func TestDeserializeHostRefCreatesHostRefValue(t *testing.T) {
	exec := &Executor{}
	typ := MustParseRuntimeType("HostRef<demo.Handle>")
	buf := ffigo.GetBuffer()
	buf.WriteUvarint(7)
	reader := ffigo.NewReader(buf.Bytes())
	ffigo.ReleaseBuffer(buf)

	v, err := exec.deserializeParsedType(nil, reader, typ, testFFIBridge{})
	if err != nil {
		t.Fatalf("deserializeParsedType failed: %v", err)
	}
	if v == nil || v.VType != TypeHostRef || v.Handle != 7 || v.RawType() != "HostRef<demo.Handle>" {
		t.Fatalf("unexpected host ref value: %#v", v)
	}
}

func TestRegisterRouteRejectsConflictingDefinitions(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
		routes:   make(map[string]FFIRoute),
	}
	exec.RegisterRoute("demo.Call", FFIRoute{
		Name:     "demo.Call",
		MethodID: 1,
		FuncSig:  MustParseRuntimeFuncSig("function(String) Void"),
	})

	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "ffi route conflict") {
			t.Fatalf("expected ffi route conflict panic, got %v", r)
		}
	}()

	exec.RegisterRoute("demo.Call", FFIRoute{
		Name:     "demo.Call",
		MethodID: 2,
		FuncSig:  MustParseRuntimeFuncSig("function(String) Void"),
	})
}

func TestRegisterStructSchemaRejectsConflictingDefinitions(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
	}
	exec.RegisterStructSchema("demo.Type", MustParseRuntimeStructSpec("demo.Type", StructOwnershipVMValue, "struct { Value Int64; }"))

	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "ffi struct schema conflict") {
			t.Fatalf("expected ffi struct schema conflict panic, got %v", r)
		}
	}()

	exec.RegisterStructSchema("demo.Type", MustParseRuntimeStructSpec("demo.Type", StructOwnershipVMValue, "struct { Value Int64; Name String; }"))
}

type copyBackFFIBridge struct {
	returnValue []byte
}

func (b copyBackFFIBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	input := bytes.ToUpper(reader.ReadBytes())
	input = append(input, '!')

	buf := ffigo.GetBuffer()
	buf.WriteUvarint(1)
	buf.WriteBytes(input)
	buf.WriteBytes(b.returnValue)

	out := append([]byte(nil), buf.Bytes()...)
	ffigo.ReleaseBuffer(buf)
	return out, nil
}

func (b copyBackFFIBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return b.Call(ctx, req)
}

func (b copyBackFFIBridge) DestroyHandle(uint32) error { return nil }

type arrayCopyBackFFIBridge struct {
	returnValue int64
	replace     []int64
}

func (b arrayCopyBackFFIBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	count := int(reader.ReadUvarint())
	input := make([]int64, count)
	for i := range input {
		input[i] = reader.ReadVarint()
	}

	resBuf := ffigo.GetBuffer()
	copyBackBuf := ffigo.GetBuffer()
	copyBackBuf.WriteUvarint(uint64(len(b.replace)))
	for _, item := range b.replace {
		copyBackBuf.WriteVarint(item)
	}
	resBuf.WriteUvarint(1)
	resBuf.WriteBytes(copyBackBuf.Bytes())
	resBuf.WriteVarint(b.returnValue)

	out := append([]byte(nil), resBuf.Bytes()...)
	ffigo.ReleaseBuffer(copyBackBuf)
	ffigo.ReleaseBuffer(resBuf)
	return out, nil
}

func (b arrayCopyBackFFIBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return b.Call(ctx, req)
}

func (b arrayCopyBackFFIBridge) DestroyHandle(uint32) error { return nil }

func TestEvalFFICopyBackWritesInOutBytesBackToCaller(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	if err := session.NewVar("buf", MustParseRuntimeType("TypeBytes")); err != nil {
		t.Fatalf("new var failed: %v", err)
	}
	initial := NewBytes([]byte("ab"))
	if err := session.Store("buf", initial); err != nil {
		t.Fatalf("store failed: %v", err)
	}

	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  copyBackFFIBridge{returnValue: []byte("ret")},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(TypeBytes) TypeBytes", FFIParamInOutBytes),
	}

	arg, err := session.Load("buf")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	res, err := exec.evalFFI(session, route, []*Var{arg}, []LHSValue{&LHSEnv{Name: "buf"}})
	if err != nil {
		t.Fatalf("evalFFI failed: %v", err)
	}
	if res == nil || res.VType != TypeBytes || string(res.B) != "ret" {
		t.Fatalf("unexpected ffi return: %#v", res)
	}

	updated, err := session.Load("buf")
	if err != nil {
		t.Fatalf("load updated failed: %v", err)
	}
	if updated == nil || updated.VType != TypeBytes || string(updated.B) != "AB!" {
		t.Fatalf("unexpected copy-back bytes: %#v", updated)
	}
}

func TestEvalFFICopyBackRejectsNonAssignableArgument(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  copyBackFFIBridge{returnValue: []byte("ret")},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(TypeBytes) TypeBytes", FFIParamInOutBytes),
	}

	_, err := exec.evalFFI(session, route, []*Var{NewBytes([]byte("ab"))}, []LHSValue{nil})
	if err == nil || !strings.Contains(err.Error(), "assignable left value") {
		t.Fatalf("expected assignable left value error, got %v", err)
	}
}
