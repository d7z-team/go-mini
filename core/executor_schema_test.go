package engine

import (
	"strings"
	"testing"
)

func TestMiniExecutorExportsParsedSchema(t *testing.T) {
	exec := NewMiniExecutor()
	exec.RegisterFFI("demo.Call", nil, 1, "function(String, ...Any) tuple(Void, String)", "demo route")
	exec.RegisterStructSpec("demo.Payload", "struct { Msg String; Count Int64; }")

	schema := exec.GetExportedSchema()
	if schema == nil {
		t.Fatal("expected schema snapshot")
	}

	funcSig := schema.Funcs["demo.Call"]
	if funcSig == nil {
		t.Fatal("expected parsed function schema")
	}
	if !funcSig.Function.Variadic {
		t.Fatal("expected variadic function schema")
	}
	if got := string(funcSig.Function.Return); got != "tuple(Void, String)" {
		t.Fatalf("unexpected return type: %s", got)
	}

	structSpec := schema.Structs["demo.Payload"]
	if structSpec == nil {
		t.Fatal("expected parsed struct schema")
	}
	if len(structSpec.Fields) != 2 {
		t.Fatalf("unexpected struct field count: %d", len(structSpec.Fields))
	}
	if structSpec.Fields[0].Name != "Msg" || structSpec.Fields[1].Name != "Count" {
		t.Fatalf("unexpected struct field order: %+v", structSpec.Fields)
	}

	exportedSpecs := exec.GetExportedSpecs()
	if got := exportedSpecs["demo.Call"]; got != "function(String, ...Any) tuple(Void, String)" {
		t.Fatalf("unexpected exported function spec: %s", got)
	}
	if got := exportedSpecs["demo.Payload"]; got != "struct { Msg String; Count Int64; }" {
		t.Fatalf("unexpected exported struct spec: %s", got)
	}

	exportedStructs := exec.GetExportedStructs()
	if got := exportedStructs["demo.Payload"]; got != "struct { Msg String; Count Int64; }" {
		t.Fatalf("unexpected exported struct-only spec: %s", got)
	}
}

func TestExportMetadataIncludesRegisteredFFISignatures(t *testing.T) {
	exec := NewMiniExecutor()
	exec.RegisterFFI("demo.Call", nil, 1, "function(String, ...Any) tuple(Void, String)", "demo route")

	meta := exec.ExportMetadata()
	if !strings.Contains(meta, `"Call": "function(String, ...Any) tuple(Void, String) // demo route"`) {
		t.Fatalf("expected exported metadata to include parsed route signature, got:\n%s", meta)
	}
}
