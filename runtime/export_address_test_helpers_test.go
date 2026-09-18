package runtime

import ir "github.com/d7z-team/mini-go/runtime/bytecode"

func exportAddressArtifacts() (ir.Artifact, ir.Artifact) {
	root := ir.NewArtifact("address/root", "main")
	root.Requirements = []ir.Requirement{{Kind: "source", ModulePath: "address/state", Exports: []string{"Item"}}}
	root.Globals = []ir.Global{{ID: "global.saved", Type: testType("Ptr<Int64>")}}
	root.Functions = []ir.Function{{ID: "fn.entry", Signature: testSignature("function() Int64"), Instructions: []ir.Instruction{
		{Op: string(ir.OpAddressOf), Payload: testPayload(ir.AddressPayload{Kind: "export", ModulePath: "address/state", Export: "Item", Path: []ir.AddressPathSegment{{Kind: "field", Field: "N"}}})},
		{Op: string(ir.OpStoreGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.saved"})},
		{Op: string(ir.OpLoadGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.saved"})},
		{Op: string(ir.OpLoadIndirect)},
		{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
	}}}
	dependency := ir.NewArtifact("address/state", "state")
	dependency.Globals = []ir.Global{{ID: "global.item", Type: testType("struct{N:Int64}")}}
	dependency.Exports = []ir.Export{{Name: "Item", Kind: "global", ID: "global.item", Type: testType("struct{N:Int64}")}}
	dependency.Constants = []ir.Constant{{ID: "const.initial", Type: testType("Int64"), Value: []byte("1")}}
	dependency.Functions = []ir.Function{{ID: "fn.init", Signature: testSignature("function() Void"), Instructions: []ir.Instruction{
		{Op: string(ir.OpAddressOf), Payload: testPayload(ir.AddressPayload{Kind: "global", Global: "global.item", Path: []ir.AddressPathSegment{{Kind: "field", Field: "N"}}})},
		{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.initial"})},
		{Op: string(ir.OpStoreIndirect)},
		{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})},
	}}}
	return root, dependency
}
