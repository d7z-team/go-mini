package runtimecheck

import (
	"io/fs"
	"strings"

	"github.com/d7z-team/mini-go/runtime/bytecode"
	"github.com/d7z-team/mini-go/stdlib"
	"github.com/d7z-team/mini-go/tooling/fixtures"
)

// GenerateManifest records the distributed VM corpus and its standard-library inputs.
func GenerateManifest(root fs.FS) ([]byte, error) {
	files, err := fixtures.ReadTree(root)
	if err != nil {
		return nil, err
	}
	manifest := fixtures.Describe(files, func(name string) fixtures.Entry {
		entry := fixtures.Entry{Kind: "bytecode-input", Source: name, Oracle: "handwritten owner and memory contract"}
		switch {
		case strings.HasPrefix(name, "source/"):
			entry.Kind = "source"
			entry.Oracle = "independent execution expectations"
		case name == "execution.json.gz":
			entry = fixtures.Entry{Kind: "image-and-observation", Source: "source/", Oracle: "Go runtime and execution_expected.json", CompilerID: bytecode.CompilerIdentity}
		case name == "stdlib.json.gz":
			entry = fixtures.Entry{Kind: "image-and-observation", Source: "stdlib/src/*_test.mgo", Oracle: "standard-library test assertions", CompilerID: bytecode.CompilerIdentity}
		case name == "state.json":
			entry = fixtures.Entry{Kind: "image-and-observation", Source: "memory_*.json", Oracle: "Go owner state observations", CompilerID: bytecode.CompilerIdentity}
		case name == "wire.json":
			entry = fixtures.Entry{Kind: "wire-observation", Source: "tooling/runtimecheck/vectors.go", Oracle: "Go canonical bytecode model"}
		case name == "execution_expected.json":
			entry.Kind = "golden"
			entry.Oracle = "handwritten source semantics"
		}
		return entry
	})
	if err := manifest.AddInputs("stdlib/src/", stdlib.Open()); err != nil {
		return nil, err
	}
	return encodeJSON(manifest)
}
