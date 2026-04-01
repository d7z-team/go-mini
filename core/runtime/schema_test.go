package runtime

import "testing"

func TestParseRuntimeFuncSig(t *testing.T) {
	sig, err := ParseRuntimeFuncSig("function(String, ...Any) tuple(Void, String)")
	if err != nil {
		t.Fatalf("ParseRuntimeFuncSig failed: %v", err)
	}
	if sig == nil {
		t.Fatal("expected non-nil function signature")
	}
	if !sig.Function.Variadic {
		t.Fatal("expected variadic signature")
	}
	if len(sig.Function.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(sig.Function.Params))
	}
	if got := string(sig.Function.Return); got != "tuple(Void, String)" {
		t.Fatalf("unexpected return type: %s", got)
	}
}

func TestParseRuntimeStructSpec(t *testing.T) {
	spec, err := ParseRuntimeStructSpec("Example", "struct { Msg String; Value Int64; Child Ptr<demo.Type>; }")
	if err != nil {
		t.Fatalf("ParseRuntimeStructSpec failed: %v", err)
	}
	if spec == nil {
		t.Fatal("expected non-nil struct spec")
	}
	if len(spec.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(spec.Fields))
	}
	if spec.Fields[0].Name != "Msg" || spec.Fields[0].Type != "String" {
		t.Fatalf("unexpected first field: %+v", spec.Fields[0])
	}
	if spec.Fields[2].Name != "Child" || spec.Fields[2].Type != "Ptr<demo.Type>" {
		t.Fatalf("unexpected third field: %+v", spec.Fields[2])
	}
	if got := spec.ByName["Value"].Type; got != "Int64" {
		t.Fatalf("unexpected field lookup result: %s", got)
	}
	if spec.TypeID != "Example" {
		t.Fatalf("unexpected type id: %s", spec.TypeID)
	}
}

func TestParseRuntimeInterfaceSpec(t *testing.T) {
	spec, err := ParseRuntimeInterfaceSpec("interface{Read(TypeBytes) tuple(Int64, Error); Close() Error;}")
	if err != nil {
		t.Fatalf("ParseRuntimeInterfaceSpec failed: %v", err)
	}
	if spec == nil {
		t.Fatal("expected non-nil interface spec")
	}
	if len(spec.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(spec.Methods))
	}
	if spec.ByName["Read"] == nil || spec.ByName["Close"] == nil {
		t.Fatalf("missing parsed interface methods: %+v", spec.ByName)
	}
	if got := spec.MethodStringMap()["Close"]; got != "function() Error" {
		t.Fatalf("unexpected close signature: %s", got)
	}
}

func TestParseRuntimeTypeAndCanonicalID(t *testing.T) {
	typ, err := ParseRuntimeType("Ptr<gopkg.d7z.net/demo.Type>")
	if err != nil {
		t.Fatalf("ParseRuntimeType failed: %v", err)
	}
	if typ.Kind != RuntimeTypePointer {
		t.Fatalf("unexpected type kind: %v", typ.Kind)
	}
	if typ.TypeID != "gopkg.d7z.net/demo.Type" {
		t.Fatalf("unexpected canonical type id: %s", typ.TypeID)
	}
	if got := CanonicalTypeID("Ptr<gopkg.d7z.net/demo.Type>"); got != "gopkg.d7z.net/demo.Type" {
		t.Fatalf("unexpected canonical id helper result: %s", got)
	}
}
