package types

import (
	"reflect"
	"testing"
)

func TestTypeViewExposesNamedAliasAndShape(t *testing.T) {
	table := &TypeTable{}
	intType := Builtin(PrimitiveInt64)
	if err := table.Add(TypeNode{ID: "node.score", Kind: Named, Identity: TypeKey{ModulePath: "example", DeclID: "Score"}, Underlying: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.alias", Kind: Named, Identity: TypeKey{ModulePath: "example", DeclID: "Alias"}, Alias: true, AliasTarget: TypeRef{Kind: Named, Node: "node.score"}}); err != nil {
		t.Fatal(err)
	}
	score := View(table, TypeRef{Kind: Named, Node: "node.score"})
	alias := View(table, TypeRef{Kind: Named, Node: "node.alias"})
	if !score.IsNamed() || score.IsAlias() || score.Shape() != Primitive {
		t.Fatalf("unexpected defined type view: named=%v alias=%v shape=%s", score.IsNamed(), score.IsAlias(), KindName(score.Shape()))
	}
	if !alias.IsNamed() || !alias.IsAlias() || alias.Underlying() != intType {
		t.Fatalf("unexpected alias view: named=%v alias=%v underlying=%s", alias.IsNamed(), alias.IsAlias(), FormatWithTable(table, alias.Underlying()))
	}
	if info, ok := alias.NumericInfo(); !ok || info.Primitive != PrimitiveInt64 || info.Bits != 64 {
		t.Fatalf("alias numeric info was not resolved: %#v ok=%v", info, ok)
	}
}

func TestTypeViewContainerAccessors(t *testing.T) {
	table := &TypeTable{}
	intType := Builtin(PrimitiveInt64)
	stringType := Builtin(PrimitiveString)
	if err := table.Add(TypeNode{ID: "node.slice", Kind: Slice, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.array", Kind: Array, Length: 4, Elem: stringType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.map", Kind: Map, Key: stringType, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.pointer", Kind: Pointer, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	if elem, ok := View(table, TypeRef{Kind: Slice, Node: "node.slice"}).Elem(); !ok || elem != intType {
		t.Fatalf("slice elem = %s ok=%v", FormatWithTable(table, elem), ok)
	}
	if length, elem, ok := View(table, TypeRef{Kind: Array, Node: "node.array"}).Array(); !ok || length != 4 || elem != stringType {
		t.Fatalf("array = len %d elem %s ok=%v", length, FormatWithTable(table, elem), ok)
	}
	if key, elem, ok := View(table, TypeRef{Kind: Map, Node: "node.map"}).Map(); !ok || key != stringType || elem != intType {
		t.Fatalf("map = %s -> %s ok=%v", FormatWithTable(table, key), FormatWithTable(table, elem), ok)
	}
	if elem, ok := View(table, TypeRef{Kind: Pointer, Node: "node.pointer"}).Elem(); !ok || elem != intType {
		t.Fatalf("pointer elem = %s ok=%v", FormatWithTable(table, elem), ok)
	}
}

func TestTypeViewFunctionInterfaceAndWaitableAccessors(t *testing.T) {
	table := &TypeTable{}
	intType := Builtin(PrimitiveInt64)
	signature := FunctionSignature{Params: []TypeParam{{Type: intType}}, Results: []TypeRef{intType}}
	method := Method{Name: "Read", ModulePath: "example", Signature: FunctionSignature{Results: []TypeRef{intType}}}
	if err := table.Add(TypeNode{ID: "node.func", Kind: Function, Signature: &signature}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.waitable", Kind: Waitable, Direction: ChannelReceive, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.interface", Kind: Interface, Methods: []Method{method}}); err != nil {
		t.Fatal(err)
	}
	if got, ok := View(table, TypeRef{Kind: Function, Node: "node.func"}).Function(); !ok || !reflect.DeepEqual(got, signature) {
		t.Fatalf("function signature = %#v ok=%v", got, ok)
	}
	if got, ok := View(table, Builtin(PrimitiveFunction)).Function(); !ok || len(got.Params) != 0 || len(got.Results) != 0 {
		t.Fatalf("primitive function = %#v ok=%v", got, ok)
	}
	if dir, elem, ok := View(table, TypeRef{Kind: Waitable, Node: "node.waitable"}).Waitable(); !ok || dir != ChannelReceive || elem != intType {
		t.Fatalf("waitable = dir %d elem %s ok=%v", dir, FormatWithTable(table, elem), ok)
	}
	if methods, terms, typeSet, ok := View(table, TypeRef{Kind: Interface, Node: "node.interface"}).Interface(); !ok || typeSet || len(terms) != 0 || len(methods) != 1 || methods[0].Name != "Read" {
		t.Fatalf("interface view = methods %#v terms %#v typeSet %v ok=%v", methods, terms, typeSet, ok)
	}
}

func TestTypeViewGoSpecRelations(t *testing.T) {
	table := &TypeTable{}
	intType := Builtin(PrimitiveInt64)
	stringType := Builtin(PrimitiveString)
	if err := table.Add(TypeNode{ID: "node.interface", Kind: Interface}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.pointer", Kind: Pointer, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.slice", Kind: Slice, Elem: intType}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.function", Kind: Function, Signature: &FunctionSignature{Results: []TypeRef{intType}}}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.strict_struct", Kind: Struct, Fields: []Field{{Name: "A", Type: intType}, {Name: "B", Type: stringType}}}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.interface_struct", Kind: Struct, Fields: []Field{{Name: "A", Type: TypeRef{Kind: Interface, Node: "node.interface"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.slice_struct", Kind: Struct, Fields: []Field{{Name: "A", Type: TypeRef{Kind: Slice, Node: "node.slice"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := table.Add(TypeNode{ID: "node.array", Kind: Array, Length: 2, Elem: TypeRef{Kind: Struct, Node: "node.strict_struct"}}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		ref        TypeRef
		comparable bool
		strict     bool
		nilable    bool
		ordered    bool
	}{
		{name: "int", ref: intType, comparable: true, strict: true, ordered: true},
		{name: "string", ref: stringType, comparable: true, strict: true, ordered: true},
		{name: "complex", ref: Builtin(PrimitiveComplex128), comparable: true, strict: true},
		{name: "any", ref: AnyType(), comparable: true, nilable: true},
		{name: "interface", ref: TypeRef{Kind: Interface, Node: "node.interface"}, comparable: true, nilable: true},
		{name: "pointer", ref: TypeRef{Kind: Pointer, Node: "node.pointer"}, comparable: true, strict: true, nilable: true},
		{name: "slice", ref: TypeRef{Kind: Slice, Node: "node.slice"}, nilable: true},
		{name: "function", ref: TypeRef{Kind: Function, Node: "node.function"}, nilable: true},
		{name: "strict struct", ref: TypeRef{Kind: Struct, Node: "node.strict_struct"}, comparable: true, strict: true},
		{name: "interface struct", ref: TypeRef{Kind: Struct, Node: "node.interface_struct"}, comparable: true},
		{name: "slice struct", ref: TypeRef{Kind: Struct, Node: "node.slice_struct"}},
		{name: "array", ref: TypeRef{Kind: Array, Node: "node.array"}, comparable: true, strict: true},
	}
	for _, tt := range tests {
		view := View(table, tt.ref)
		if got := view.Comparable(); got != tt.comparable {
			t.Fatalf("%s comparable = %v, want %v", tt.name, got, tt.comparable)
		}
		if got := view.StrictlyComparable(); got != tt.strict {
			t.Fatalf("%s strictly comparable = %v, want %v", tt.name, got, tt.strict)
		}
		if got := view.Nilable(); got != tt.nilable {
			t.Fatalf("%s nilable = %v, want %v", tt.name, got, tt.nilable)
		}
		if got := view.Ordered(); got != tt.ordered {
			t.Fatalf("%s ordered = %v, want %v", tt.name, got, tt.ordered)
		}
	}
}

func TestTypeViewPreservesNamedStructFieldMetadata(t *testing.T) {
	table := &TypeTable{}
	intType := Builtin(PrimitiveInt)
	shape := TypeNode{ID: "node.embedded_shape", Kind: Struct, Fields: []Field{{Name: "Base", Type: intType}}}
	if err := table.Add(shape); err != nil {
		t.Fatal(err)
	}
	named := TypeNode{
		ID:         "type.example.Wrapper",
		Kind:       Named,
		Identity:   TypeKey{ModulePath: "example", DeclID: "Wrapper"},
		Underlying: Ref(shape),
		Fields:     []Field{{Name: "Base", Type: intType, Embedded: true, Tag: `json:"base"`}},
	}
	if err := table.Add(named); err != nil {
		t.Fatal(err)
	}
	fields, ok := View(table, Ref(named)).StructFields()
	if !ok || len(fields) != 1 || !fields[0].Embedded || fields[0].Tag != `json:"base"` {
		t.Fatalf("named struct fields = %#v, %v", fields, ok)
	}
}
