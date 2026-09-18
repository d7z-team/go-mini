package ffigo

import "testing"

func TestSplitGenericTypeNested(t *testing.T) {
	base, args, ok := SplitGenericType("Map<String, Array<Tuple2<Int64, String>>>")
	if !ok {
		t.Fatal("expected generic type")
	}
	if base != "Map" {
		t.Fatalf("base = %q, want Map", base)
	}
	if len(args) != 2 || args[0] != "String" || args[1] != "Array<Tuple2<Int64, String>>" {
		t.Fatalf("args = %#v", args)
	}
}

func TestRefAndMarkerTypeStrings(t *testing.T) {
	if inner, ok := RefElementType("HostRef<demo.Type>"); !ok || inner != "demo.Type" {
		t.Fatalf("HostRef inner = %q, %v", inner, ok)
	}
	if inner, ok := RefElementType("Ptr<demo.Type>"); !ok || inner != "demo.Type" {
		t.Fatalf("Ptr inner = %q, %v", inner, ok)
	}
	if !IsBytesRefTypeString("HostRef<ffigo.BytesRef>") {
		t.Fatal("expected BytesRef marker")
	}
	if !IsArrayRefTypeString("ffigo.ArrayRef<Int64>") {
		t.Fatal("expected ArrayRef marker")
	}
}

func TestAsyncTupleAndCollectionTypeStrings(t *testing.T) {
	if elem, ok := AsyncElemTypeString("Async<Int64>"); !ok || elem != "Int64" {
		t.Fatalf("Async elem = %q, %v", elem, ok)
	}
	if elems, ok := Tuple2ElemTypeStrings("Tuple2<Int64, String>"); !ok || len(elems) != 2 || elems[0] != "Int64" || elems[1] != "String" {
		t.Fatalf("Tuple2 elems = %#v, %v", elems, ok)
	}
	if elem, ok := ReadArrayItemType("Array<String>"); !ok || elem != "String" {
		t.Fatalf("Array elem = %q, %v", elem, ok)
	}
	if elem, ok := ReadArrayItemType("Ptr<String>"); ok || elem != "" {
		t.Fatalf("Ptr array elem = %q, %v, want empty false", elem, ok)
	}
	if key, value, ok := ReadMapKeyValueTypes("Map<String, Array<Int64>>"); !ok || key != "String" || value != "Array<Int64>" {
		t.Fatalf("Map parts = %q, %q, %v", key, value, ok)
	}
	if key, value, ok := ReadMapKeyValueTypes("Map<String, tuple(Int64, Error)>"); !ok || key != "String" || value != "tuple(Int64, Error)" {
		t.Fatalf("Nested map parts = %q, %q, %v", key, value, ok)
	}
}
