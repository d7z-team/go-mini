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

func requireSchemaConflict(t *testing.T, err error, kind string) {
	t.Helper()
	var conflict *SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != kind {
		t.Fatalf("expected %s conflict, got %T %v", kind, err, err)
	}
}

func readSerializedAny(t *testing.T, exec *Executor, v *Var) interface{} {
	t.Helper()
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	if err := exec.serializeVarToAny(buf, v); err != nil {
		t.Fatalf("serializeVarToAny failed: %v", err)
	}
	decoded, err := ffigo.NewReader(buf.Bytes()).ReadAny()
	if err != nil {
		t.Fatalf("ReadAny failed: %v", err)
	}
	return decoded
}

func TestSerializeVarToAnyProjectsStructAsMap(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
	}
	schema := MustParseRuntimeStructSpec("demo.Point", StructOwnershipVMValue, "struct { X Int64; Data Array<Byte>; }")
	exec.metadata.registerStructSchema(schema.Name, schema)

	v := &Var{
		VType:    TypeStruct,
		TypeInfo: MustParseRuntimeType("demo.Point"),
		Ref: &VMStruct{
			Spec: schema,
			Fields: []*Slot{
				NewSlot(MustParseRuntimeType("Int64"), NewInt(10)),
				NewSlot(MustParseRuntimeType(ArrayType(SpecByte)), NewByteArray([]byte("ok"))),
			},
			ByName: map[string]int{"X": 0, "Data": 1},
		},
	}

	decoded := readSerializedAny(t, exec, v)
	fields, ok := decoded.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map projection, got %T", decoded)
	}
	if got, ok := fields["X"].(int64); !ok || got != 10 {
		t.Fatalf("unexpected X field: %T %#v", fields["X"], fields["X"])
	}
	if got, ok := fields["Data"].([]byte); !ok || !bytes.Equal(got, []byte("ok")) {
		t.Fatalf("unexpected Data field: %T %#v", fields["Data"], fields["Data"])
	}
}

func TestSerializeVarToAnyProjectsCompositePureValues(t *testing.T) {
	exec := &Executor{}

	byteArray := NewByteArray([]byte{1, 2, 3})
	decoded := readSerializedAny(t, exec, byteArray)
	if got, ok := decoded.([]byte); !ok || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("expected []byte projection, got %T %#v", decoded, decoded)
	}

	array := &Var{VType: TypeArray, Ref: &VMArray{Data: []*Var{
		exec.wrapAnyVar(NewByteArray([]byte("ab"))),
		exec.wrapAnyVar(NewString("tail")),
	}}}
	array.SetRawType(ArrayType(SpecAny).String())
	decodedArrayRaw := readSerializedAny(t, exec, array)
	decodedArray, ok := decodedArrayRaw.([]interface{})
	if !ok || len(decodedArray) != 2 {
		t.Fatalf("expected []any projection, got %T %#v", decodedArrayRaw, decodedArrayRaw)
	}
	if got, ok := decodedArray[0].([]byte); !ok || !bytes.Equal(got, []byte("ab")) {
		t.Fatalf("unexpected array bytes item: %T %#v", decodedArray[0], decodedArray[0])
	}
	if got, ok := decodedArray[1].(string); !ok || got != "tail" {
		t.Fatalf("unexpected array string item: %T %#v", decodedArray[1], decodedArray[1])
	}

	vmMap := &VMMap{Data: make(map[string]*Var), KeyVars: make(map[string]*Var)}
	vmMap.StoreWithKey("payload", NewString("payload"), exec.wrapAnyVar(NewByteArray([]byte("xy"))))
	vmMap.StoreWithKey("count", NewString("count"), exec.wrapAnyVar(NewInt(2)))
	mapVar := &Var{VType: TypeMap, Ref: vmMap}
	mapVar.SetRawType(MapType(SpecString, SpecAny).String())

	decodedMapRaw := readSerializedAny(t, exec, mapVar)
	decodedMap, ok := decodedMapRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map projection, got %T", decodedMapRaw)
	}
	if got, ok := decodedMap["payload"].([]byte); !ok || !bytes.Equal(got, []byte("xy")) {
		t.Fatalf("unexpected map bytes value: %T %#v", decodedMap["payload"], decodedMap["payload"])
	}
	if got, ok := decodedMap["count"].(int64); !ok || got != 2 {
		t.Fatalf("unexpected map count value: %T %#v", decodedMap["count"], decodedMap["count"])
	}
}

func TestToVarDecodesHostPureAnyValues(t *testing.T) {
	exec := &Executor{}

	value, err := exec.ToVar(nil, map[string]interface{}{
		"Msg":   "ok",
		"Count": uint32(2),
		"Data":  []byte("go"),
	}, nil)
	if err != nil {
		t.Fatalf("ToVar failed: %v", err)
	}
	if value == nil || value.VType != TypeMap {
		t.Fatalf("expected VM map, got %#v", value)
	}
	data := value.Ref.(*VMMap)
	msg, _ := data.Load("Msg")
	count, _ := data.Load("Count")
	rawBytes, _ := data.Load("Data")

	if got := exec.unwrapValue(msg); got == nil || got.VType != TypeString || got.Str != "ok" {
		t.Fatalf("unexpected Msg value: %#v", msg)
	}
	if got := exec.unwrapValue(count); got == nil || got.VType != TypeInt || got.I64 != 2 {
		t.Fatalf("unexpected Count value: %#v", count)
	}
	if got := exec.unwrapValue(rawBytes); got == nil || got.VType != TypeArray || !isByteArrayType(got.RuntimeType()) {
		t.Fatalf("unexpected Data value: %#v", rawBytes)
	}
}

func TestToVarDecodesInterfaceDataWithoutHostRefAnyRawType(t *testing.T) {
	exec := &Executor{}

	value, err := exec.ToVar(nil, ffigo.InterfaceData{
		Handle: 9,
		Methods: map[string]string{
			"Ping": "function() Void",
		},
	}, testFFIBridge{})
	if err != nil {
		t.Fatalf("ToVar failed: %v", err)
	}
	if value == nil || value.VType != TypeInterface {
		t.Fatalf("expected VM interface, got %#v", value)
	}
	iface := value.Ref.(*VMInterface)
	if iface.Spec == nil || len(iface.Spec.Methods) != 1 || iface.Spec.Methods[0].Name != "Ping" {
		t.Fatalf("unexpected interface spec: %#v", iface.Spec)
	}
	if iface.Target == nil || iface.Target.VType != TypeHostRef || iface.Target.Handle != 9 {
		t.Fatalf("unexpected interface target: %#v", iface.Target)
	}
	if got := iface.Target.RawType(); got != "" {
		t.Fatalf("interface target must not expose synthetic raw type, got %q", got)
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

func TestAnyWireEncodesUint32AsNumber(t *testing.T) {
	buf := ffigo.GetBuffer()
	buf.WriteAny(uint32(42))
	reader := ffigo.NewReader(buf.Bytes())
	ffigo.ReleaseBuffer(buf)

	got, err := reader.ReadAny()
	if err != nil {
		t.Fatalf("ReadAny failed: %v", err)
	}
	if got != int64(42) {
		t.Fatalf("expected uint32 to project as Int64, got %T %#v", got, got)
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

func TestFFIByteRuneScalarCodecPreservesTypeAndRejectsInvalidValues(t *testing.T) {
	exec := &Executor{}
	byteType := MustParseRuntimeType(SpecByte)
	runeType := MustParseRuntimeType(SpecRune)

	buf := ffigo.GetBuffer()
	buf.WriteVarint(65)
	byteValue, err := exec.deserializeParsedType(nil, ffigo.NewReader(buf.Bytes()), byteType, nil)
	ffigo.ReleaseBuffer(buf)
	if err != nil {
		t.Fatalf("deserialize byte failed: %v", err)
	}
	if byteValue == nil || byteValue.I64 != 65 || byteValue.RuntimeType().Raw != SpecByte {
		t.Fatalf("bad byte value: %#v", byteValue)
	}

	buf = ffigo.GetBuffer()
	buf.WriteVarint(256)
	if _, err := exec.deserializeParsedType(nil, ffigo.NewReader(buf.Bytes()), byteType, nil); err == nil || !strings.Contains(err.Error(), "overflows Byte") {
		ffigo.ReleaseBuffer(buf)
		t.Fatalf("expected byte overflow, got %v", err)
	}
	ffigo.ReleaseBuffer(buf)

	buf = ffigo.GetBuffer()
	buf.WriteVarint(-1)
	runeValue, err := exec.deserializeParsedType(nil, ffigo.NewReader(buf.Bytes()), runeType, nil)
	if err != nil {
		ffigo.ReleaseBuffer(buf)
		t.Fatalf("deserialize negative rune failed: %v", err)
	}
	if runeValue == nil || runeValue.I64 != -1 || runeValue.RuntimeType().Raw != SpecRune {
		ffigo.ReleaseBuffer(buf)
		t.Fatalf("bad rune value: %#v", runeValue)
	}
	ffigo.ReleaseBuffer(buf)

	buf = ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	if err := exec.serializeRuntimeType(buf, NewInt(256), byteType); err == nil || !strings.Contains(err.Error(), "overflows Byte") {
		t.Fatalf("expected byte encode overflow, got %v", err)
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
	requireSchemaConflict(t, exec.ApplyBoundFFISurface(second), "route")
}

func TestApplyBoundFFISurfaceRejectsConflictingStructDefinitions(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
	}
	exec.metadata.registerStructSchema("demo.Type", MustParseRuntimeStructSpec("demo.Type", StructOwnershipVMValue, "struct { Value Int64; }"))

	surface := NewBoundFFISurface(nil)
	surface.AddStruct("demo", "Type", MustParseRuntimeStructSpec("demo.Type", StructOwnershipVMValue, "struct { Value Int64; Name String; }"))
	requireSchemaConflict(t, exec.ApplyBoundFFISurface(surface), "struct schema")
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
		FuncSig: MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Void", FFIParamInOutBytes),
	})
	if err := exec.ApplyBoundFFISurface(first); err != nil {
		t.Fatalf("register route failed: %v", err)
	}

	second := NewBoundFFISurface(nil)
	second.AddRoute("demo", "Mutate", FFIRoute{
		Name:    "demo.Mutate",
		FuncSig: MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Void", FFIParamIn),
	})
	requireSchemaConflict(t, exec.ApplyBoundFFISurface(second), "route")
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
	if err := schema.AddConst("demo", "Value", FFIConstValue{}); err != nil {
		t.Fatal(err)
	}
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
	surface.AddStruct("demo", "Payload", MustParseRuntimeStructSpec("demo.Payload", StructOwnershipVMValue, "struct { Msg String; Count Int64; }"))

	requireSchemaConflict(t, exec.ApplyBoundFFISurface(surface), "struct schema")
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

	requireSchemaConflict(t, exec.ApplyBoundFFISurface(surface), "package value")
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

func TestValidateModuleRequirementsChecksMethodID(t *testing.T) {
	sig := MustParseRuntimeFuncSig("function(String) Void")
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
		routes: map[string]FFIRoute{
			"demo.Call": {Name: "demo.Call", MethodID: 2, FuncSig: sig},
		},
		modules: newRuntimeModuleRegistry(),
		moduleRequirements: []ModuleRequirement{
			{
				Version:    FFISurfaceHashVersion,
				Path:       "demo",
				Kind:       ModuleKindFFI,
				MemberName: "Call",
				MemberKind: FFIMemberFunc,
				Type:       sig.Spec,
				MethodID:   1,
				Hash:       FuncRouteHash(1, sig),
			},
		},
	}
	if err := exec.modules.RegisterFFIPackage(&BoundFFIPackage{
		Path: "demo",
		Members: map[string]*BoundFFIMember{
			"Call": {Name: "Call", Kind: FFIMemberFunc, ReadOnly: true, RouteName: "demo.Call"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	err := exec.ValidateModuleRequirements()
	if err == nil || !strings.Contains(err.Error(), "method id mismatch") {
		t.Fatalf("expected method id mismatch, got %v", err)
	}
}

func TestSurfaceRouteDeclsBindTypeMethods(t *testing.T) {
	sig := MustParseRuntimeFuncSigWithModes("function(HostRef<demo.Handle>) Error", FFIParamIn)
	schema := NewFFISurfaceSchema()
	if err := schema.AddStruct("demo", "Handle", MustParseRuntimeStructSpec("demo.Handle", StructOwnershipHostOpaque, "struct { Close function(HostRef<demo.Handle>) Error; }")); err != nil {
		t.Fatal(err)
	}
	if err := schema.AddRouteDecls([]FFIRouteDecl{{
		TypePackagePath: "demo",
		TypeMemberName:  "Handle",
		MethodName:      "Close",
		MethodID:        9,
		Sig:             sig,
	}}); err != nil {
		t.Fatal(err)
	}

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
	if pkg := bound.Packages["demo"]; pkg == nil || pkg.Members["Handle"] == nil || pkg.Members["Handle"].Kind != FFIMemberType {
		t.Fatalf("expected type method to create package type member: %#v", bound.Packages)
	}
}

func TestSurfaceSchemaRecordsInvalidTypeRouteOwner(t *testing.T) {
	schema := NewFFISurfaceSchema()
	err := schema.AddRouteDecls([]FFIRouteDecl{{
		TypePackagePath: "demo",
		MethodName:      "Close",
		RouteName:       "demo.Handle.Close",
		MethodID:        1,
		Sig:             MustParseRuntimeFuncSig("function(HostRef<demo.Handle>) Error"),
	}})
	if err == nil || !strings.Contains(err.Error(), "incomplete owner") {
		t.Fatalf("expected incomplete owner error, got %v", err)
	}
	if err := CheckPublicFFISurfaceSchema(schema); err == nil || !strings.Contains(err.Error(), "incomplete owner") {
		t.Fatalf("expected schema validation to retain add error, got %v", err)
	}
}

func TestSurfaceSchemaMergeTypeMethodConflictDoesNotPollute(t *testing.T) {
	left := NewFFISurfaceSchema()
	if err := left.AddRouteDecls([]FFIRouteDecl{{
		TypePackagePath: "demo",
		TypeMemberName:  "Handle",
		MethodName:      "Close",
		MethodID:        1,
		Sig:             MustParseRuntimeFuncSig("function(HostRef<demo.Handle>) Error"),
	}}); err != nil {
		t.Fatal(err)
	}
	right := NewFFISurfaceSchema()
	if err := right.AddRouteDecls([]FFIRouteDecl{{
		TypePackagePath: "demo",
		TypeMemberName:  "Handle",
		MethodName:      "Close",
		MethodID:        2,
		Sig:             MustParseRuntimeFuncSig("function(HostRef<demo.Handle>) Error"),
	}}); err != nil {
		t.Fatal(err)
	}

	requireSchemaConflict(t, left.Merge(right), "surface type method")
	if got := left.Types["demo.Handle"].Methods["Close"].MethodID; got != 1 {
		t.Fatalf("failed merge polluted existing method id: %d", got)
	}
}

func TestBindSchemaRoutesRejectsDuplicateRouteNameConflict(t *testing.T) {
	schema := NewFFISurfaceSchema()
	if err := schema.AddFunc("demo", "Call", "demo.Shared", 1, MustParseRuntimeFuncSig("function() Void"), ""); err != nil {
		t.Fatal(err)
	}
	if err := schema.AddTypeMethod("demo", "Handle", "Close", "demo.Shared", 2, MustParseRuntimeFuncSig("function(HostRef<demo.Handle>) Void"), ""); err != nil {
		t.Fatal(err)
	}

	requireSchemaConflict(t, NewBoundFFISurfaceFromSchema(schema).BindSchemaRoutes(schema, testFFIBridge{}), "route")
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
	if err := session.NewVar("buf", MustParseRuntimeType("Array<Byte>")); err != nil {
		t.Fatalf("new var failed: %v", err)
	}
	initial := NewByteArray([]byte("ab"))
	if err := session.Store("buf", initial); err != nil {
		t.Fatalf("store failed: %v", err)
	}

	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  copyBackFFIBridge{returnValue: []byte("ret")},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Array<Byte>", FFIParamInOutBytes),
	}

	arg, err := session.Load("buf")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	res, err := exec.evalFFI(session, route, []*Var{arg}, []LHSValue{&LHSEnv{Name: "buf"}})
	if err != nil {
		t.Fatalf("evalFFI failed: %v", err)
	}
	if res == nil || res.VType != TypeArray || byteArrayText(t, exec, res) != "ret" {
		t.Fatalf("unexpected ffi return: %#v", res)
	}

	updated, err := session.Load("buf")
	if err != nil {
		t.Fatalf("load updated failed: %v", err)
	}
	if updated == nil || updated.VType != TypeArray || byteArrayText(t, exec, updated) != "AB!" {
		t.Fatalf("unexpected copy-back bytes: %#v", updated)
	}
}

func TestEvalFFICopyBackRejectsNonAssignableArgument(t *testing.T) {
	exec := newEmptyExecutor(t)
	session := exec.NewSession(context.Background(), "global")
	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  copyBackFFIBridge{returnValue: []byte("ret")},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Array<Byte>", FFIParamInOutBytes),
	}

	_, err := exec.evalFFI(session, route, []*Var{NewByteArray([]byte("ab"))}, []LHSValue{nil})
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
	if err := session.NewVar("buf", MustParseRuntimeType("Array<Byte>")); err != nil {
		t.Fatalf("new var failed: %v", err)
	}
	if err := session.Store("buf", NewByteArray([]byte("original"))); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	arg, err := session.Load("buf")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	route := FFIRoute{
		Name:    "demo.Mutate",
		Bridge:  malformedCopyBackFFIBridge{},
		FuncSig: MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Void", FFIParamInOutBytes),
	}

	_, err = exec.evalFFI(session, route, []*Var{arg}, []LHSValue{&LHSEnv{Name: "buf"}})
	if err == nil {
		t.Fatal("expected malformed copy-back error")
	}
	updated, err := session.Load("buf")
	if err != nil {
		t.Fatalf("load updated failed: %v", err)
	}
	if updated == nil || byteArrayText(t, exec, updated) != "original" {
		t.Fatalf("copy-back mutated value after decode failure: %#v", updated)
	}
}
