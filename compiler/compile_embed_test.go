package compiler

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/types"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestCompileBinaryEmbedUsesByteSliceConstant(t *testing.T) {
	data := []byte{0, 0xb8, 0xff, '\n'}
	compiled, err := compileTestPackage(workspace.SourcePackage{
		ModulePath: "example/embed",
		Files:      []source.File{{Path: "embed.mgo", Text: "package embedded\nimport _ \"embed\"\n//go:embed data.bin\nvar Data []byte\n"}},
		Resources:  []workspace.ResourceFile{{Path: "data.bin", Data: data}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !compiled.OK() {
		t.Fatalf("binary embed diagnostics = %#v", compiled.Diagnostics)
	}
	for _, constant := range compiled.Artifact.Constants {
		view := types.View(&compiled.Artifact.TypeTable, constant.Type)
		elem, ok := view.Elem()
		primitive, primitiveOK := types.View(&compiled.Artifact.TypeTable, elem).Primitive()
		if view.Shape() != types.Slice || !ok || !primitiveOK || primitive != types.PrimitiveUint8 {
			continue
		}
		var encoded string
		if err := json.Unmarshal(constant.Value, &encoded); err != nil {
			t.Fatal(err)
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if string(decoded) != string(data) {
			t.Fatalf("embedded bytes = %v, want %v", decoded, data)
		}
		return
	}
	t.Fatal("compiled package has no byte slice constant")
}
