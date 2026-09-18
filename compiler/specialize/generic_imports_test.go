package specialize

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/types"
)

func TestImportedNamedTypesPreserveRecursiveReferences(t *testing.T) {
	table := types.TypeTable{}
	cloner := types.TypeRef{
		Kind:  types.Named,
		Named: types.TypeKey{ModulePath: "hash", DeclID: "Cloner"},
		Node:  "hash.Cloner",
	}
	clonerInterface := types.TypeRef{Kind: types.Interface, Node: "hash.Cloner.interface"}
	if err := table.Add(types.TypeNode{
		ID:         cloner.Node,
		Kind:       types.Named,
		Identity:   cloner.Named,
		Underlying: clonerInterface,
	}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(types.TypeNode{
		ID:   clonerInterface.Node,
		Kind: types.Interface,
		Methods: []types.Method{{
			Name: "Clone",
			Signature: types.FunctionSignature{
				Results: []types.TypeRef{cloner, types.Builtin(types.PrimitiveError)},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	imported := importedNamedTypes(cache.PackageData{ModulePath: "hash", TypeTable: table})
	got := imported["Cloner"]
	if got.Kind != ast.TypeInterface || len(got.Methods) != 1 || len(got.Methods[0].Results) != 2 {
		t.Fatalf("unexpected imported interface: %#v", got)
	}
	result := got.Methods[0].Results[0].Type
	if result.Kind != ast.TypeName || result.Name != "hash.Cloner" {
		t.Fatalf("recursive result = %#v, want named hash.Cloner", result)
	}
}
