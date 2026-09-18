package bytecode

import (
	"strings"
	"testing"
)

func TestExportAddressValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload AddressPayload
		want    string
	}{
		{"valid", AddressPayload{Kind: "export", ModulePath: "lib", Export: "Value"}, ""},
		{"missing_module", AddressPayload{Kind: "export", Export: "Value"}, "requires module path"},
		{"missing_export", AddressPayload{Kind: "export", ModulePath: "lib"}, "requires module path"},
		{"mixed_target", AddressPayload{Kind: "export", ModulePath: "lib", Export: "Value", Local: "x"}, "cannot contain local"},
		{"local_with_export", AddressPayload{Kind: "local", Local: "x", Export: "Value"}, "cannot contain export identity"},
		{"unknown_module", AddressPayload{Kind: "export", ModulePath: "other", Export: "Value"}, "unknown module requirement"},
		{"unknown_export", AddressPayload{Kind: "export", ModulePath: "lib", Export: "Other"}, "unknown module export"},
		{"unknown_index", AddressPayload{Kind: "export", ModulePath: "lib", Export: "Value", Path: []AddressPathSegment{{Kind: "index", Local: "missing"}}}, "unknown local"},
	} {
		t.Run(test.name, func(t *testing.T) {
			artifact := NewArtifact("app", "main")
			artifact.Requirements = []Requirement{{Kind: "source", ModulePath: "lib", Exports: []string{"Value"}}}
			artifact.Functions = []Function{{ID: "fn.main", Signature: testSignature("function() Ptr<Int64>"), Instructions: []Instruction{
				{Op: string(OpAddressOf), Payload: testPayload(test.payload)},
				{Op: string(OpReturn), Payload: testPayload(ReturnPayload{ResultCount: 1})},
			}}}
			err := testValidateArtifact(&artifact)
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}

func TestClosureCaptureAddressValidation(t *testing.T) {
	artifact := NewArtifact("app", "main")
	artifact.Functions = []Function{
		{ID: "fn.child", Signature: testSignature("function() Void"), Upvalues: []Upvalue{{ID: "x", Type: testType("Int64")}}},
		{ID: "fn.main", Signature: testSignature("function() Void"), Instructions: []Instruction{
			{Op: string(OpMakeClosure), Payload: testPayload(ClosurePayload{Function: "fn.child", Captures: []AddressPayload{{Kind: "global", Global: "value", ModulePath: "lib", Export: "Value"}}})},
			{Op: string(OpPop)},
		}},
	}
	if err := testValidateArtifact(&artifact); err == nil || !strings.Contains(err.Error(), "capture cannot contain export identity") {
		t.Fatalf("capture validation: %v", err)
	}
}
