package runtime

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func patchErrorCode(err error) string {
	var patchError PatchError
	if errors.As(err, &patchError) {
		return patchError.Code
	}
	return ""
}

func patchTestProgram(t *testing.T, artifact ir.Artifact, hash string) *Program {
	t.Helper()
	attachRuntimeTestTypeNodes(&artifact)
	code, err := newLoader().load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	modules := map[string]*executable{artifact.Module.Path: code}
	moduleOrder, err := prepareModuleReferences(modules)
	if err != nil {
		t.Fatal(err)
	}
	const entryName = "run"
	return &Program{code: &programCode{
		image: ir.ExecutionImage{
			Root: artifact.Module.Path,
			Hash: hash,
			Entries: []ir.Entry{{
				Name: entryName, ModulePath: artifact.Module.Path, FunctionID: "fn.entry",
			}},
		},
		root: code, modules: modules, moduleOrder: moduleOrder,
		entries: map[string]string{entryName: "fn.entry"},
	}, symbols: testSymbolIndex(&artifact)}
}

func patchMultiModuleProgram(t *testing.T, rootArtifact, dependencyArtifact ir.Artifact, hash string) *Program {
	t.Helper()
	attachRuntimeTestTypeNodes(&rootArtifact)
	attachRuntimeTestTypeNodes(&dependencyArtifact)
	root, err := newLoader().load(rootArtifact)
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := newLoader().load(dependencyArtifact)
	if err != nil {
		t.Fatal(err)
	}
	modules := map[string]*executable{
		rootArtifact.Module.Path:       root,
		dependencyArtifact.Module.Path: dependency,
	}
	moduleOrder, err := prepareModuleReferences(modules)
	if err != nil {
		t.Fatal(err)
	}
	return &Program{code: &programCode{
		image: ir.ExecutionImage{
			Root: rootArtifact.Module.Path, Hash: hash,
			Entries: []ir.Entry{{Name: "run", ModulePath: rootArtifact.Module.Path, FunctionID: "fn.entry"}},
		},
		root: root, modules: modules, moduleOrder: moduleOrder,
		entries: map[string]string{"run": "fn.entry"},
	}, symbols: testSymbolIndex(&rootArtifact)}
}

func patchCallArtifact(base, value int64) ir.Artifact {
	artifact := ir.NewArtifact("patch/main", "main")
	artifact.Constants = []ir.Constant{
		{ID: "const.base", Type: testType("Int64"), Value: json.RawMessage(jsonInt(base))},
		{ID: "const.value", Type: testType("Int64"), Value: json.RawMessage(jsonInt(value))},
	}
	entry := ir.Function{ID: "fn.entry", Signature: testSignature("function() Int64"), Instructions: []ir.Instruction{
		{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.base"})},
	}}
	appendPollDelay(&entry,
		ir.Instruction{Op: string(ir.OpCallDirect), Payload: testPayload(ir.CallPayload{Function: "fn.value", ResultCount: 1})},
		ir.Instruction{Op: string(ir.OpBinary), Payload: testPayload(ir.OperatorPayload{Operator: "+"})},
		ir.Instruction{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
	)
	artifact.Functions = []ir.Function{
		entry,
		{ID: "fn.value", Signature: testSignature("function() Int64"), Instructions: []ir.Instruction{
			{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.value"})},
			{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
		}},
	}
	artifact.Exports = []ir.Export{{Name: "Run", Kind: "function", ID: "fn.entry"}}
	return artifact
}

func patchTailCallArtifact(value int64) ir.Artifact {
	artifact := ir.NewArtifact("patch/tail", "main")
	artifact.Constants = []ir.Constant{{ID: "const.value", Type: testType("Int64"), Value: json.RawMessage(jsonInt(value))}}
	entry := ir.Function{ID: "fn.entry", Signature: testSignature("function() Int64")}
	appendPollDelay(&entry, ir.Instruction{
		Op: string(ir.OpTailCallDirect), Payload: testPayload(ir.CallPayload{Function: "fn.value", ResultCount: 1}),
	})
	artifact.Functions = []ir.Function{
		entry,
		{ID: "fn.value", Signature: testSignature("function() Int64"), Instructions: []ir.Instruction{
			{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.value"})},
			{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
		}},
	}
	artifact.Exports = []ir.Export{{Name: "Run", Kind: "function", ID: "fn.entry"}}
	return artifact
}

func patchMultiRootArtifact(base int64) ir.Artifact {
	artifact := ir.NewArtifact("patch/root", "main")
	artifact.Constants = []ir.Constant{{ID: "const.base", Type: testType("Int64"), Value: json.RawMessage(jsonInt(base))}}
	artifact.Requirements = []ir.Requirement{{Kind: "source", ModulePath: "patch/dep"}}
	entry := ir.Function{ID: "fn.entry", Signature: testSignature("function() Int64"), Instructions: []ir.Instruction{
		{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.base"})},
	}}
	appendPollDelay(&entry,
		ir.Instruction{Op: string(ir.OpCallDirect), Payload: testPayload(ir.CallPayload{ModulePath: "patch/dep", Function: "fn.value", ResultCount: 1})},
		ir.Instruction{Op: string(ir.OpBinary), Payload: testPayload(ir.OperatorPayload{Operator: "+"})},
		ir.Instruction{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
	)
	artifact.Functions = []ir.Function{entry}
	artifact.Exports = []ir.Export{{Name: "Run", Kind: "function", ID: "fn.entry"}}
	return artifact
}

func patchMultiDependencyArtifact(value int64) ir.Artifact {
	artifact := ir.NewArtifact("patch/dep", "dep")
	artifact.Constants = []ir.Constant{{ID: "const.value", Type: testType("Int64"), Value: json.RawMessage(jsonInt(value))}}
	artifact.Functions = []ir.Function{{
		ID: "fn.value", Signature: testSignature("function() Int64"),
		Instructions: []ir.Instruction{
			{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.value"})},
			{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
		},
	}}
	artifact.Exports = []ir.Export{{Name: "Value", Kind: "function", ID: "fn.value"}}
	return artifact
}

func patchClosureArtifact(value int64) ir.Artifact {
	artifact := ir.NewArtifact("patch/closure", "main")
	artifact.Constants = []ir.Constant{{ID: "const.value", Type: testType("Int64"), Value: json.RawMessage(jsonInt(value))}}
	entry := ir.Function{ID: "fn.entry", Signature: testSignature("function() Int64"), Instructions: []ir.Instruction{{
		Op: string(ir.OpMakeClosure), Payload: testPayload(ir.ClosurePayload{Function: "fn.literal.1"}),
	}}}
	appendPollDelay(&entry,
		ir.Instruction{Op: string(ir.OpCallValue), Payload: testPayload(ir.CallPayload{ResultCount: 1})},
		ir.Instruction{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
	)
	artifact.Functions = []ir.Function{
		entry,
		{ID: "fn.literal.1", RevisionLocal: true, Signature: testSignature("function() Int64"), Instructions: []ir.Instruction{
			{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.value"})},
			{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
		}},
	}
	artifact.Exports = []ir.Export{{Name: "Run", Kind: "function", ID: "fn.entry"}}
	return artifact
}

func patchGlobalArtifact(delta int64) ir.Artifact {
	artifact := ir.NewArtifact("patch/global", "main")
	artifact.Constants = []ir.Constant{{ID: "const.delta", Type: testType("Int64"), Value: json.RawMessage(jsonInt(delta))}}
	artifact.Globals = []ir.Global{{ID: "global.total", Type: testType("Int64")}}
	artifact.Functions = []ir.Function{{
		ID: "fn.entry", Signature: testSignature("function() Int64"),
		Instructions: []ir.Instruction{
			{Op: string(ir.OpLoadGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.total"})},
			{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.delta"})},
			{Op: string(ir.OpBinary), Payload: testPayload(ir.OperatorPayload{Operator: "+"})},
			{Op: string(ir.OpStoreGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.total"})},
			{Op: string(ir.OpLoadGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.total"})},
			{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 1})},
		},
	}}
	artifact.Exports = []ir.Export{{Name: "Run", Kind: "function", ID: "fn.entry"}}
	return artifact
}

func patchPanicArtifact(message string) ir.Artifact {
	artifact := ir.NewArtifact("patch/panic", "main")
	data, _ := json.Marshal(message)
	artifact.Constants = []ir.Constant{{ID: "const.message", Type: testType("String"), Value: data}}
	entry := ir.Function{ID: "fn.entry", Signature: testSignature("function() Void")}
	appendPollDelay(&entry,
		ir.Instruction{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.message"})},
		ir.Instruction{Op: string(ir.OpPanic)},
	)
	artifact.Functions = []ir.Function{entry}
	artifact.Exports = []ir.Export{{Name: "Run", Kind: "function", ID: "fn.entry"}}
	return artifact
}

func patchFFIArtifact() ir.Artifact {
	artifact := ir.NewArtifact("patch/ffi", "main")
	artifact.Constants = []ir.Constant{
		{ID: "const.route", Type: testType("String"), Value: json.RawMessage(`"patch"`)},
		{ID: "const.payload", Type: testType("Slice<Uint8>"), Value: json.RawMessage(`null`)},
	}
	artifact.Functions = []ir.Function{{
		ID: "fn.entry", Signature: testSignature("function() tuple(Slice<Uint8>, String, Int)"),
		Instructions: []ir.Instruction{
			{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.route"})},
			{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.payload"})},
			{Op: string(ir.OpCallFFI), Payload: testPayload(ir.CallFFIPayload{ArgCount: 2, ResultCount: 3})},
			{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{ResultCount: 3})},
		},
	}}
	artifact.Exports = []ir.Export{{Name: "Run", Kind: "function", ID: "fn.entry"}}
	return artifact
}

func appendPollDelay(function *ir.Function, tail ...ir.Instruction) {
	function.Instructions = append(function.Instructions,
		ir.Instruction{Op: string(ir.OpZero), Payload: testTypePayload("Bool")},
		ir.Instruction{Op: string(ir.OpPop)},
		ir.Instruction{Op: string(ir.OpZero), Payload: testTypePayload("Bool")},
		ir.Instruction{Op: string(ir.OpPop)},
	)
	function.Instructions = append(function.Instructions, tail...)
}

func jsonInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
