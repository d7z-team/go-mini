package types

import "testing"

func TestParserKeepsSignatureBindingsDistinct(t *testing.T) {
	table := &TypeTable{}
	parser := NewParser("example", table)
	var previous TypeRef
	for _, id := range []TypeID{"first.T", "second.T"} {
		parameter := TypeRef{Kind: TypeParameter, Node: id}
		if err := table.Add(TypeNode{ID: id, Kind: TypeParameter, Constraint: AnyType()}); err != nil {
			t.Fatal(err)
		}
		parser.Bindings = map[string]TypeRef{"T": parameter}
		ref, err := parser.Parse("function(T) T")
		if err != nil {
			t.Fatal(err)
		}
		signature, ok := NewRelations(table).View(ref).Function()
		if !ok || len(signature.Params) != 1 || len(signature.Results) != 1 || signature.Params[0].Type != parameter || signature.Results[0] != parameter {
			t.Fatalf("signature lost parameter binding: %+v", signature)
		}
		if ref == previous {
			t.Fatal("distinct declaration bindings share a signature node")
		}
		repeated, err := parser.Parse("function(T) T")
		if err != nil || repeated != ref {
			t.Fatalf("repeated signature changed identity: %v %v", repeated, err)
		}
		previous = ref
	}
}
