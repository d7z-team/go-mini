package runtime

import (
	"bytes"
	"context"
	"errors"
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
	if err := exec.serializeVarToAny(buf, v); err != nil {
		t.Fatalf("serializeVarToAny failed: %v", err)
	}

	decoded, err := ffigo.NewReader(buf.Bytes()).ReadAny()
	if err != nil {
		t.Fatalf("ReadAny failed: %v", err)
	}
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

func TestToVarDecodesStructAnyValues(t *testing.T) {
	exec := &Executor{}

	structVal, err := exec.ToVar(nil, &ffigo.VMStruct{Fields: []ffigo.StructField{
		{Name: "Msg", Value: "ok"},
		{Name: "Count", Value: int64(2)},
	}}, nil)
	if err != nil {
		t.Fatalf("ToVar failed: %v", err)
	}
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

func TestSerializeVarToAnyRejectsHostRef(t *testing.T) {
	exec := &Executor{}
	v := &Var{VType: TypeHostRef, Handle: 42, Bridge: testFFIBridge{}}

	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	err := exec.serializeVarToAny(buf, v)
	if err == nil || !strings.Contains(err.Error(), "cannot carry host reference") {
		t.Fatalf("expected host reference rejection, got %v", err)
	}
}

func TestSerializeVarToAnyRejectsSlotPointer(t *testing.T) {
	exec := &Executor{}
	v := exec.newSlotPointer(MustParseRuntimeType("Int64"), NewSlot(MustParseRuntimeType("Int64"), NewInt(1)))

	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	err := exec.serializeVarToAny(buf, v)
	if err == nil || !strings.Contains(err.Error(), "cannot carry VM pointer") {
		t.Fatalf("expected VM pointer rejection, got %v", err)
	}
}

func TestAnyWireDoesNotEncodeHostReferenceHandle(t *testing.T) {
	buf := ffigo.GetBuffer()
	buf.WriteAny(uint32(42))
	reader := ffigo.NewReader(buf.Bytes())
	ffigo.ReleaseBuffer(buf)

	if got, err := reader.ReadAny(); err != nil || got != nil {
		t.Fatalf("expected uint32 to be omitted from Any wire, got %T %#v", got, got)
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

func TestDeserializeChannelRejectsDirectionMismatch(t *testing.T) {
	exec, err := NewExecutorFromPrepared(&PreparedProgram{
		Globals:   map[string]*PreparedGlobal{},
		Functions: map[string]*PreparedFunction{},
		MainTasks: []Task{},
	})
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	id := exec.channelRegistry().RegisterChannel(ffigo.ChannelEndpointFuncs{
		Elem: "Int64",
		Dir:  ffigo.ChannelSend,
	})
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteUvarint(id)

	typ := MustParseRuntimeType(RecvChanType(SpecInt64))
	_, err = exec.deserializeParsedType(nil, ffigo.NewReader(buf.Bytes()), typ, nil)
	if err == nil || !strings.Contains(err.Error(), "FFI channel direction mismatch") {
		t.Fatalf("expected direction mismatch, got %v", err)
	}
}

func TestApplyBoundFFISurfaceRejectsConflictingRouteDefinitions(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
		routes:   make(map[string]FFIRoute),
	}
	first := NewBoundFFISurface(nil)
	first.AddRoute("demo", "Call", FFIRoute{
		Name:     "demo.Call",
		MethodID: 1,
		FuncSig:  MustParseRuntimeFuncSig("function(String) Void"),
	})
	if err := exec.ApplyBoundFFISurface(first); err != nil {
		t.Fatalf("apply first route failed: %v", err)
	}

	second := NewBoundFFISurface(nil)
	second.AddRoute("demo", "Call", FFIRoute{
		Name:     "demo.Call",
		MethodID: 2,
		FuncSig:  MustParseRuntimeFuncSig("function(String) Void"),
	})
	err := exec.ApplyBoundFFISurface(second)
	var conflict *SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "route" {
		t.Fatalf("expected route conflict, got %T %v", err, err)
	}
}

func TestApplyBoundFFISurfaceRejectsConflictingStructDefinitions(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
	}
	exec.metadata.registerStructSchema("demo.Type", MustParseRuntimeStructSpec("demo.Type", StructOwnershipVMValue, "struct { Value Int64; }"))

	surface := NewBoundFFISurface(nil)
	surface.AddStruct("demo.Type", MustParseRuntimeStructSpec("demo.Type", StructOwnershipVMValue, "struct { Value Int64; Name String; }"))
	err := exec.ApplyBoundFFISurface(surface)
	var conflict *SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "struct schema" {
		t.Fatalf("expected struct schema conflict, got %T %v", err, err)
	}
}

func TestRuntimeMetadataNilStructSchemaClearsCanonicalIndex(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
	}
	schema := MustParseRuntimeStructSpec("demo.Type", StructOwnershipVMValue, "struct { Value Int64; }")
	exec.metadata.registerStructSchema("demo.Type", schema)
	exec.metadata.registerStructSchema("demo.Type", nil)

	if resolved, ok := exec.lookupStructSchema(MustParseRuntimeType("demo.Type")); ok || resolved != nil {
		t.Fatalf("expected struct schema canonical index to be cleared, got %#v", resolved)
	}
}

func TestApplyBoundFFISurfaceReportsParamModeConflict(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
		routes:   make(map[string]FFIRoute),
	}
	first := NewBoundFFISurface(nil)
	first.AddRoute("demo", "Mutate", FFIRoute{
		Name:    "demo.Mutate",
		FuncSig: MustParseRuntimeFuncSigWithModes("function(TypeBytes) Void", FFIParamInOutBytes),
	})
	if err := exec.ApplyBoundFFISurface(first); err != nil {
		t.Fatalf("register route failed: %v", err)
	}

	second := NewBoundFFISurface(nil)
	second.AddRoute("demo", "Mutate", FFIRoute{
		Name:    "demo.Mutate",
		FuncSig: MustParseRuntimeFuncSigWithModes("function(TypeBytes) Void", FFIParamIn),
	})
	err := exec.ApplyBoundFFISurface(second)
	var conflict *SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "route" {
		t.Fatalf("expected route conflict error, got %T %v", err, err)
	}
}

func TestCheckPublicFFIRouteSchemaRejectsPublicSchemaEscapes(t *testing.T) {
	err := CheckPublicFFIRouteSchema("demo.Ptr", FFIRoute{
		Name:    "demo.Ptr",
		FuncSig: MustParseRuntimeFuncSig("function(Ptr<Int64>) Void"),
	})
	if err == nil || !strings.Contains(err.Error(), "Ptr<T>") {
		t.Fatalf("expected Ptr<T> rejection, got %v", err)
	}

	err = CheckPublicFFIRouteSchema("demo.AnyRef", FFIRoute{
		Name:    "demo.AnyRef",
		FuncSig: MustParseRuntimeFuncSig("function(HostRef<Any>) Void"),
	})
	if err == nil || !strings.Contains(err.Error(), "HostRef<Any>") {
		t.Fatalf("expected HostRef<Any> rejection, got %v", err)
	}

	err = CheckPublicFFIRouteSchema("demo.Unschematized", FFIRoute{Name: "demo.Unschematized"})
	if err == nil || !strings.Contains(err.Error(), "missing schema") {
		t.Fatalf("expected missing schema rejection, got %v", err)
	}
}

func TestCheckPublicFFISurfaceSchemaRequiresConstType(t *testing.T) {
	schema := NewFFISurfaceSchema()
	schema.AddConst("demo", "Value", FFIConstValue{})
	err := CheckPublicFFISurfaceSchema(schema)
	if err == nil {
		t.Fatal("expected FFI constant without type to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid ffi const type") {
		t.Fatalf("unexpected FFI constant validation error: %v", err)
	}
}

func TestRuntimeApplyBoundFFISurfaceConflictDoesNotPolluteRoutes(t *testing.T) {
	exec := &Executor{
		metadata:      newRuntimeMetadataRegistry(),
		routes:        make(map[string]FFIRoute),
		packageValues: make(map[string]*BoundPackageValue),
		consts:        make(map[string]FFIConstValue),
		ffiPackages:   make(map[string]*BoundFFIPackage),
	}
	exec.metadata.registerStructSchema("demo.Payload", MustParseRuntimeStructSpec("demo.Payload", StructOwnershipVMValue, "struct { Msg String; }"))

	surface := NewBoundFFISurface(nil)
	surface.AddRoute("demo", "Call", FFIRoute{
		Name:    "demo.Call",
		FuncSig: MustParseRuntimeFuncSig("function(String) Void"),
	})
	surface.AddStruct("demo.Payload", MustParseRuntimeStructSpec("demo.Payload", StructOwnershipVMValue, "struct { Msg String; Count Int64; }"))

	err := exec.ApplyBoundFFISurface(surface)
	var conflict *SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "struct schema" {
		t.Fatalf("expected struct schema conflict, got %T %v", err, err)
	}
	if _, ok := exec.routes["demo.Call"]; ok {
		t.Fatalf("failed surface registration polluted route state: %+v", exec.routes)
	}
	if pkg, ok := exec.lookupFFIPackage("demo"); ok && len(pkg.Members) != 0 {
		t.Fatalf("failed surface registration polluted package members: %+v", pkg.Members)
	}
}

func TestRuntimeApplyBoundFFISurfaceConflictDoesNotPollutePackageMembers(t *testing.T) {
	exec := &Executor{
		metadata:      newRuntimeMetadataRegistry(),
		routes:        make(map[string]FFIRoute),
		packageValues: make(map[string]*BoundPackageValue),
		consts:        make(map[string]FFIConstValue),
		ffiPackages:   make(map[string]*BoundFFIPackage),
	}
	exec.packageValues["demo.Value"] = &BoundPackageValue{
		Name:  "demo.Value",
		Spec:  &ValueSpec{Type: MustParseRuntimeType("String"), ReadOnly: true},
		Value: NewString("old"),
	}

	surface := NewBoundFFISurface(nil)
	surface.AddRoute("demo", "Call", FFIRoute{
		Name:    "demo.Call",
		FuncSig: MustParseRuntimeFuncSig("function(String) Void"),
	})
	surface.AddPackageValue("demo", "Value", &ValueSpec{Type: MustParseRuntimeType("Int64"), ReadOnly: true}, NewInt(1))

	err := exec.ApplyBoundFFISurface(surface)
	var conflict *SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "package value" {
		t.Fatalf("expected package value conflict, got %T %v", err, err)
	}
	if _, ok := exec.routes["demo.Call"]; ok {
		t.Fatalf("failed surface registration polluted route state: %+v", exec.routes)
	}
	if pkg, ok := exec.lookupFFIPackage("demo"); ok && len(pkg.Members) != 0 {
		t.Fatalf("failed surface registration polluted package members: %+v", pkg.Members)
	}
	if got := exec.packageValues["demo.Value"]; got == nil || got.Spec == nil || got.Spec.Type.Raw != "String" {
		t.Fatalf("existing package value should remain unchanged, got %#v", got)
	}
}

func TestValidateExternalRequirementsChecksMethodID(t *testing.T) {
	sig := MustParseRuntimeFuncSig("function(String) Void")
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
		routes: map[string]FFIRoute{
			"demo.Call": {Name: "demo.Call", MethodID: 2, FuncSig: sig},
		},
		externalRequirements: []ExternalRequirement{
			{
				Version:     FFISurfaceHashVersion,
				PackagePath: "demo",
				MemberName:  "Call",
				Kind:        FFIMemberFunc,
				Type:        sig.Spec,
				MethodID:    1,
				Hash:        FuncRouteHash(1, sig),
			},
		},
	}

	err := exec.ValidateExternalRequirements()
	if err == nil || !strings.Contains(err.Error(), "method id mismatch") {
		t.Fatalf("expected method id mismatch, got %v", err)
	}
}

func TestSurfaceRouteDeclsBindTypeMethods(t *testing.T) {
	sig := MustParseRuntimeFuncSigWithModes("function(HostRef<demo.Handle>) Error", FFIParamIn)
	schema := NewFFISurfaceSchema()
	schema.AddStruct("demo.Handle", MustParseRuntimeStructSpec("demo.Handle", StructOwnershipHostOpaque, "struct { Close function(HostRef<demo.Handle>) Error; }"))
	schema.AddRouteDecls([]FFIRouteDecl{{
		TypeName:   "demo.Handle",
		MethodName: "Close",
		MethodID:   9,
		Sig:        sig,
	}})

	if err := CheckPublicFFISurfaceSchema(schema); err != nil {
		t.Fatalf("schema validation failed: %v", err)
	}
	bound := NewBoundFFISurfaceFromSchema(schema)
	if _, ok := bound.Structs["demo.Handle"]; !ok {
		t.Fatalf("expected schema-bound struct, got %#v", bound.Structs)
	}
	if err := bound.BindSchemaRoutes(schema, testFFIBridge{}); err != nil {
		t.Fatalf("bind schema routes failed: %v", err)
	}

	route := bound.Routes["demo.Handle.Close"]
	if route.Name != "demo.Handle.Close" || route.MethodID != 9 || !SameRuntimeFuncSchema(route.FuncSig, sig) || route.Bridge == nil {
		t.Fatalf("unexpected bound method route: %#v", route)
	}
	if len(bound.Packages) != 0 {
		t.Fatalf("type method routes should not create package members: %#v", bound.Packages)
	}
}

func TestSurfaceSchemaMergeTypeMethodConflictDoesNotPollute(t *testing.T) {
	left := NewFFISurfaceSchema()
	left.AddRouteDecls([]FFIRouteDecl{{
		TypeName:   "demo.Handle",
		MethodName: "Close",
		MethodID:   1,
		Sig:        MustParseRuntimeFuncSig("function(HostRef<demo.Handle>) Error"),
	}})
	right := NewFFISurfaceSchema()
	right.AddRouteDecls([]FFIRouteDecl{{
		TypeName:   "demo.Handle",
		MethodName: "Close",
		MethodID:   2,
		Sig:        MustParseRuntimeFuncSig("function(HostRef<demo.Handle>) Error"),
	}})

	err := left.Merge(right)
	var conflict *SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "surface type method" {
		t.Fatalf("expected type method conflict, got %T %v", err, err)
	}
	if got := left.Types["demo.Handle"].Methods["Close"].MethodID; got != 1 {
		t.Fatalf("failed merge polluted existing method id: %d", got)
	}
}

func TestBindSchemaRoutesRejectsDuplicateRouteNameConflict(t *testing.T) {
	schema := NewFFISurfaceSchema()
	schema.AddFunc("demo", "Call", "demo.Shared", 1, MustParseRuntimeFuncSig("function() Void"), "")
	schema.AddTypeMethod("demo.Handle", "Close", "demo.Shared", 2, MustParseRuntimeFuncSig("function(HostRef<demo.Handle>) Void"), "")

	err := NewBoundFFISurfaceFromSchema(schema).BindSchemaRoutes(schema, testFFIBridge{})
	var conflict *SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "route" {
		t.Fatalf("expected duplicate route conflict, got %T %v", err, err)
	}
}

type copyBackFFIBridge struct {
	returnValue []byte
}

func (b copyBackFFIBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	rawInput, err := reader.ReadBytes()
	if err != nil {
		return nil, err
	}
	input := bytes.ToUpper(rawInput)
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

type malformedCopyBackFFIBridge struct{}

func (malformedCopyBackFFIBridge) Call(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteUvarint(1)
	buf.WriteUvarint(5)
	_ = buf.WriteByte('x')
	return append([]byte(nil), buf.Bytes()...), nil
}

func (b malformedCopyBackFFIBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return b.Call(ctx, req)
}

func (malformedCopyBackFFIBridge) DestroyHandle(uint32) error { return nil }

type arrayCopyBackFFIBridge struct {
	returnValue int64
	replace     []int64
}

func (b arrayCopyBackFFIBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	rawCount, err := reader.ReadUvarint()
	if err != nil {
		return nil, err
	}
	count := int(rawCount)
	input := make([]int64, count)
	for i := range input {
		input[i], err = reader.ReadVarint()
		if err != nil {
			return nil, err
		}
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
	if err == nil || !strings.Contains(err.Error(), "requires assignable argument") {
		t.Fatalf("expected non-assignable inout rejection, got %v", err)
	}
}

func TestFinishFFIReturnsErrorForMalformedPayload(t *testing.T) {
	exec := &Executor{}
	route := FFIRoute{
		Name:    "demo.Bad",
		FuncSig: MustParseRuntimeFuncSig("function() String"),
	}

	_, err := exec.finishFFI(nil, route, nil, []byte{5, 'x'}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid payload") {
		t.Fatalf("expected invalid payload error, got %v", err)
	}
}

func TestDecodeChannelPayloadReturnsErrorForMalformedPayload(t *testing.T) {
	exec := &Executor{}
	_, err := exec.decodeChannelPayload([]byte{5, 'x'}, MustParseRuntimeType("String"))
	if err == nil {
		t.Fatal("expected malformed channel payload error")
	}
}

func TestEvalFFICopyBackRejectsMalformedPayloadWithoutWriting(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	if err := session.NewVar("buf", MustParseRuntimeType("TypeBytes")); err != nil {
		t.Fatalf("new var failed: %v", err)
	}
	if err := session.Store("buf", NewBytes([]byte("original"))); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	arg, err := session.Load("buf")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  malformedCopyBackFFIBridge{},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(TypeBytes) Void", FFIParamInOutBytes),
	}

	_, err = exec.evalFFI(session, route, []*Var{arg}, []LHSValue{&LHSEnv{Name: "buf"}})
	if err == nil {
		t.Fatal("expected malformed copy-back error")
	}
	updated, err := session.Load("buf")
	if err != nil {
		t.Fatalf("load updated failed: %v", err)
	}
	if updated == nil || string(updated.B) != "original" {
		t.Fatalf("copy-back mutated value after decode failure: %#v", updated)
	}
}
