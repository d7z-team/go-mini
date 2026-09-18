package typespec

import "testing"

func TestBinaryResultTypeRejectsMixedEquality(t *testing.T) {
	if _, err := BinaryResultType(OpEq, Int64, String); err == nil {
		t.Fatal("expected Int64/String equality to be rejected")
	}
}

func TestAssignableToAnyAllowsVMRuntimeValues(t *testing.T) {
	for _, typ := range []Type{"Ptr<Int64>", "HostRef<demo.Handle>", "Chan<Int64>", Module, Closure, "Array<Ptr<Int64>>", "Map<String, HostRef<demo.Handle>>"} {
		if !typ.IsAssignableTo(Any) {
			t.Fatalf("expected %s to be assignable to Any", typ)
		}
	}
}

func TestArrayTypesAreInvariant(t *testing.T) {
	if Type("Array<Int64>").IsAssignableTo("Array<Any>") {
		t.Fatal("expected Array<Int64> to be rejected for Array<Any>")
	}
}

func TestEqualityComparableAllowsIdentityTypes(t *testing.T) {
	for _, pair := range [][2]Type{
		{Type("Map<String,Int64>"), Type("Map<String,Int64>")},
		{Module, Module},
		{Closure, Closure},
	} {
		if !EqualityComparable(pair[0], pair[1]) {
			t.Fatalf("expected %s and %s to be equality comparable", pair[0], pair[1])
		}
	}
}
