package types

import "testing"

func TestNumericTypeInfoUsesStructuredPrimitiveIdentity(t *testing.T) {
	for _, test := range []struct {
		kind NumericKind
		bits int
		ref  TypeRef
	}{
		{NumericSigned, 64, Builtin(PrimitiveInt)},
		{NumericSigned, 32, Builtin(PrimitiveInt32)},
		{NumericUnsigned, 64, Builtin(PrimitiveUintptr)},
		{NumericFloat, 32, Builtin(PrimitiveFloat32)},
		{NumericComplex, 128, Builtin(PrimitiveComplex128)},
	} {
		info, ok := NumericTypeInfo(nil, test.ref)
		if !ok || info.Kind != test.kind || info.Bits != test.bits || !info.Type.Equal(test.ref) {
			t.Fatalf("numeric info for %#v = %#v, %v", test.ref, info, ok)
		}
	}
}

func TestNumericTypeInfoResolvesDefinedTypeUnderlying(t *testing.T) {
	table := &TypeTable{}
	if err := table.Add(TypeNode{
		ID:         "type.score",
		Kind:       Named,
		Identity:   TypeKey{ModulePath: "example", DeclID: "Score"},
		Underlying: Builtin(PrimitiveInt16),
	}); err != nil {
		t.Fatal(err)
	}
	ref := TypeRef{Kind: Named, Node: "type.score", Named: TypeKey{ModulePath: "example", DeclID: "Score"}}
	info, ok := NumericTypeInfo(table, ref)
	if !ok || info.Type != Builtin(PrimitiveInt16) || info.Bits != 16 || info.Kind != NumericSigned {
		t.Fatalf("defined numeric type info = %#v, %v", info, ok)
	}
}

func TestPrimitiveByNameUsesCanonicalNames(t *testing.T) {
	if primitive, ok := PrimitiveByName(" Float32 "); !ok || primitive != PrimitiveFloat32 {
		t.Fatalf("PrimitiveByName(Float32) = %v, %v", primitive, ok)
	}
	if _, ok := PrimitiveByName("float32"); ok {
		t.Fatal("source spelling must be normalized before the canonical boundary")
	}
}
