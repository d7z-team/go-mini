package typespec

import "testing"

func TestBinaryResultTypeRejectsMixedEquality(t *testing.T) {
	if _, err := BinaryResultType(OpEq, Int64, String); err == nil {
		t.Fatal("expected Int64/String equality to be rejected")
	}
}

func TestAssignableToAnyRejectsHostIdentity(t *testing.T) {
	for _, typ := range []Type{"Ptr<Int64>", "HostRef<demo.Handle>", "Chan<Int64>", Module} {
		if typ.IsAssignableTo(Any) {
			t.Fatalf("expected %s to be rejected by Any assignment", typ)
		}
	}
}

func TestArrayTypesAreInvariant(t *testing.T) {
	if Type("Array<Int64>").IsAssignableTo("Array<Any>") {
		t.Fatal("expected Array<Int64> to be rejected for Array<Any>")
	}
}
