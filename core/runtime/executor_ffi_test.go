package runtime

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestSerializeVarToAnyUsesStructSchemaOrder(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
	}
	schema := MustParseRuntimeStructSpec("demo.Point", ast.GoMiniType("struct { X Int64; Y Int64; }"))
	exec.metadata.registerStructSchema(schema.Name, schema)

	v := &Var{
		VType: TypeMap,
		Type:  "demo.Point",
		Ref: &VMMap{Data: map[string]*Var{
			"Y": NewInt(20),
			"X": NewInt(10),
		}},
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
	inner, ok := ptrVal.Ref.(*Var)
	if !ok || inner.VType != TypeInt || inner.I64 != 7 {
		t.Fatalf("unexpected pointer payload: %#v", ptrVal.Ref)
	}

	structVal := exec.ToVar(nil, &ffigo.VMStruct{Fields: []ffigo.StructField{
		{Name: "Msg", Value: "ok"},
		{Name: "Count", Value: int64(2)},
	}}, nil)
	if structVal == nil || structVal.VType != TypeMap {
		t.Fatalf("expected map-backed struct, got %#v", structVal)
	}
	data := structVal.Ref.(*VMMap).Data
	if data["Msg"].Str != "ok" || data["Count"].I64 != 2 {
		t.Fatalf("unexpected decoded struct data: %#v", data)
	}
}

func TestLookupStructSchemaUsesCanonicalIndexes(t *testing.T) {
	exec := &Executor{
		metadata: newRuntimeMetadataRegistry(),
	}
	schema := MustParseRuntimeStructSpec("demo.Type", ast.GoMiniType("struct { Value Int64; }"))
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
