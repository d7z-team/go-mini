package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/target"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestInstantiateRejectsNegativeRuntimeLimits(t *testing.T) {
	artifact := ir.NewArtifact("test/limits", "main")
	artifact.Functions = []ir.Function{{
		ID: "fn.main", Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})}},
	}}
	program := patchTestProgram(t, artifact, "negative-limits")
	tests := []Limits{
		{MaxSteps: -2},
		{MaxCallDepth: -1},
		{MaxTasks: -1},
		{MaxAllocatedBytes: -1},
		{MaxStringBytes: -1},
		{MaxCollectionElements: -1},
		{MaxPendingEvents: -1},
		{MaxBoundaryDepth: -1},
		{MaxBoundaryBytes: -1},
		{MaxRetainedRevisions: -1},
		{MaxDynamicTypes: -1},
		{MaxDynamicTypeBytes: -1},
	}
	for _, limits := range tests {
		instance, err := program.Instantiate(context.Background(), InstanceOptions{Limits: limits})
		if instance != nil || err == nil || !strings.Contains(err.Error(), "cannot be negative") {
			t.Fatalf("Instantiate(%#v) = %v, %v", limits, instance, err)
		}
	}
}

func TestArtifactLoadLimitsRejectOversizedShape(t *testing.T) {
	artifact := ir.NewArtifact("test/load", "load")
	artifact.Functions = []ir.Function{{
		ID: "fn.main",
		Instructions: []ir.Instruction{
			{Op: string(ir.OpZero), Payload: testTypePayload("Bool")},
			{Op: string(ir.OpPop)},
			{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})},
		},
	}}
	err := validateArtifactLoad(&artifact, normalizeLoadOptions(LoadOptions{MaxInstructions: 1}))
	if err == nil || !strings.Contains(err.Error(), "instruction limit exceeded") {
		t.Fatalf("validate artifact limit error = %v", err)
	}
}

func TestProgramIntrospectionReturnsCopies(t *testing.T) {
	artifact := ir.NewArtifact("test/introspection", "introspection")
	artifact.Functions = []ir.Function{{ID: "fn.main", Signature: testSignature("function() Void"), Instructions: []ir.Instruction{{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})}}}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main", Type: testType("function() Void")}}
	attachRuntimeTestTypeNodes(&artifact)
	executable, err := newLoader().load(artifact)
	if err != nil {
		t.Fatal(err)
	}
	program := &Program{code: &programCode{root: executable}}
	exports := program.Exports()
	if len(exports) != 1 || exports[0].Name != "Main" {
		t.Fatalf("exports = %#v", exports)
	}
	exports[0].Name = "changed"
	if program.Exports()[0].Name != "Main" {
		t.Fatal("program introspection exposed mutable metadata")
	}
}

func TestLoadExecutionImageOwnsInput(t *testing.T) {
	artifact := ir.NewArtifact("test/owned", "owned")
	artifact.Functions = []ir.Function{{
		ID: "fn.main", Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})}},
	}}
	artifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}
	attachRuntimeTestTypeNodes(&artifact)
	artifactJSON, artifactHash, err := ir.EncodeJSONAndHash(&artifact)
	if err != nil {
		t.Fatal(err)
	}
	buildTarget, err := target.Normalize(target.Target{})
	if err != nil {
		t.Fatal(err)
	}
	image := ir.ExecutionImage{
		Format: ir.ExecutionFormat, Version: ir.ExecutionVersion,
		CompilerID: ir.CompilerIdentity, ContractID: ir.ExecutionContract,
		Target: buildTarget, Root: artifact.Module.Path,
		Entries:  []ir.Entry{{Name: ir.DefaultEntryName, ModulePath: artifact.Module.Path, FunctionID: "fn.main"}},
		Packages: map[string]ir.PackageArchive{artifact.Module.Path: {Artifact: artifactJSON, ArtifactHash: artifactHash}},
	}
	image.Hash, err = ir.HashExecutionImage(image)
	if err != nil {
		t.Fatal(err)
	}
	program, err := LoadExecutionImage(image)
	if err != nil {
		t.Fatal(err)
	}

	image.Target.Tags[0] = "changed"
	image.Entries[0].Name = "changed"
	archive := image.Packages[artifact.Module.Path]
	archive.Artifact[0] = 'x'
	delete(image.Packages, artifact.Module.Path)
	if program.code.image.Target.Tags[0] == "changed" || program.code.image.Entries[0].Name != ir.DefaultEntryName {
		t.Fatal("program retained caller-owned target or entries")
	}
	if archive, ok := program.code.image.Packages[artifact.Module.Path]; !ok || len(archive.Artifact) != 0 {
		t.Fatal("program retained raw package artifact bytes")
	}
}

func TestLoadExecutionImageRejectsCrossModuleCallShapeMismatch(t *testing.T) {
	rootArtifact := ir.NewArtifact("test/root", "main")
	rootArtifact.Requirements = []ir.Requirement{{Kind: ir.RequirementSource, ModulePath: "test/dependency", Exports: []string{"Target"}}}
	rootArtifact.Functions = []ir.Function{{
		ID: "fn.main", Signature: testSignature("function() Void"),
		Instructions: []ir.Instruction{{
			Op: string(ir.OpCallDirect), Payload: testPayload(ir.CallPayload{ModulePath: "test/dependency", Function: "fn.target"}),
		}, {Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})}},
	}}
	rootArtifact.Exports = []ir.Export{{Name: "Main", Kind: "function", ID: "fn.main"}}
	dependencyArtifact := ir.NewArtifact("test/dependency", "dependency")
	dependencyArtifact.Functions = []ir.Function{{
		ID: "fn.target", Signature: testSignature("function(Int) Void"),
		Locals:       []ir.Local{{ID: "local.value", Type: testType("Int")}},
		Instructions: []ir.Instruction{{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})}},
	}}
	dependencyArtifact.Exports = []ir.Export{{Name: "Target", Kind: "function", ID: "fn.target"}}
	attachRuntimeTestTypeNodes(&rootArtifact)
	attachRuntimeTestTypeNodes(&dependencyArtifact)
	rootData, rootHash, err := ir.EncodeJSONAndHash(&rootArtifact)
	if err != nil {
		t.Fatal(err)
	}
	dependencyData, dependencyHash, err := ir.EncodeJSONAndHash(&dependencyArtifact)
	if err != nil {
		t.Fatal(err)
	}
	buildTarget, err := target.Normalize(target.Target{})
	if err != nil {
		t.Fatal(err)
	}
	image := ir.ExecutionImage{
		Format: ir.ExecutionFormat, Version: ir.ExecutionVersion,
		CompilerID: ir.CompilerIdentity, ContractID: ir.ExecutionContract,
		Target: buildTarget, Root: rootArtifact.Module.Path,
		Entries: []ir.Entry{{Name: ir.DefaultEntryName, ModulePath: rootArtifact.Module.Path, FunctionID: "fn.main"}},
		Packages: map[string]ir.PackageArchive{
			rootArtifact.Module.Path:       {Artifact: rootData, ArtifactHash: rootHash},
			dependencyArtifact.Module.Path: {Artifact: dependencyData, ArtifactHash: dependencyHash},
		},
	}
	image.Hash, err = ir.HashExecutionImage(image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExecutionImage(image); err == nil || !strings.Contains(err.Error(), "call shape") {
		t.Fatalf("LoadExecutionImage() = %v, want call shape mismatch", err)
	}
}
