package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestFinalizeDirectArtifactRequirementsPreservesInputOwnership(t *testing.T) {
	stored := ir.NewArtifact("example/main", "main")
	stored.Requirements = []ir.Requirement{{Kind: "source", ModulePath: "example/lib"}}
	linked := stored
	var diagnostics []source.Diagnostic
	if !finalizeDirectArtifactRequirements("example/main", &linked, map[string]string{"example/lib": "artifact-hash"}, &diagnostics) {
		t.Fatalf("finalize failed: %#v", diagnostics)
	}
	if stored.Requirements[0].Hash != "" {
		t.Fatalf("finalize mutated stored artifact: %#v", stored.Requirements)
	}
	if linked.Requirements[0].Hash != "artifact-hash" {
		t.Fatalf("finalize did not bind copied artifact: %#v", linked.Requirements)
	}
}
