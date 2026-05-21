package typespec

import "testing"

func TestConstructorsRenderCanonicalTypes(t *testing.T) {
	got := Func([]Param{
		{Type: HostRef("io.File")},
		{Type: Bytes},
		{Type: Array(Int64)},
	}, Tuple(Int64, Error), false)
	want := Type("function(HostRef<io.File>, TypeBytes, Array<Int64>) tuple(Int64, Error)")
	if got != want {
		t.Fatalf("Func() = %q, want %q", got, want)
	}
}

func TestConstructorsRenderNamesVariadicAndContainers(t *testing.T) {
	if got := Tuple(); got != Void {
		t.Fatalf("Tuple() = %q, want Void", got)
	}
	if got := Tuple(String); got != String {
		t.Fatalf("single Tuple() = %q, want String", got)
	}
	got := Func([]Param{{Name: "format", Type: String}, {Name: "args", Type: Any}}, String, true)
	if want := Type("function(String, ...Any) String"); got != want {
		t.Fatalf("Func() = %q, want %q", got, want)
	}
	methods := map[string]Function{
		"Write": {Params: []Param{{Type: Bytes}}, Return: Tuple(Int64, Error)},
		"Close": {Return: Error},
	}
	if got, want := Interface(SortedMethods(methods)), Type("interface{Close() Error;Write(TypeBytes) tuple(Int64, Error);}"); got != want {
		t.Fatalf("Interface() = %q, want %q", got, want)
	}
}

func TestParseNestedTypes(t *testing.T) {
	spec := Type("function(Map<String, Array<HostRef<io.File>>>, function(Int64) String) tuple(Int64, Error)")
	parsed, err := Parse(spec)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if parsed.Kind != KindFunction {
		t.Fatalf("kind = %v, want function", parsed.Kind)
	}
	if len(parsed.Function.Params) != 2 {
		t.Fatalf("params = %d, want 2", len(parsed.Function.Params))
	}
	if parsed.Function.Return != "tuple(Int64, Error)" {
		t.Fatalf("return = %q", parsed.Function.Return)
	}
}

func TestShapeSpecificReaders(t *testing.T) {
	if elem, ok := Type("Array<Ptr<demo.Type>>").ReadArrayItemType(); !ok || elem != "Ptr<demo.Type>" {
		t.Fatalf("Array elem = %q, %v", elem, ok)
	}
	if elem, ok := Type("Ptr<demo.Type>").ReadArrayItemType(); ok || elem != "" {
		t.Fatalf("Ptr ReadArrayItemType() = %q, %v, want empty false", elem, ok)
	}
	if elem, ok := Type("HostRef<demo.Type>").RefElement(); !ok || elem != "demo.Type" {
		t.Fatalf("RefElement() = %q, %v", elem, ok)
	}
	key, value, ok := Type("Map<String, tuple(Int64, Error)>").MapTypes()
	if !ok || key != String || value != "tuple(Int64, Error)" {
		t.Fatalf("MapTypes() = %q, %q, %v", key, value, ok)
	}
}

func TestParseStructInterfaceAndNamedTraversal(t *testing.T) {
	structSpec := Struct([]Member{
		{Name: "Name", Type: String},
		{Name: "Open", Type: Func([]Param{{Type: String}}, Tuple(HostRef("demo.File"), Error), false)},
	})
	fields, ok := structSpec.StructFields()
	if !ok || len(fields) != 2 || fields[1].Type != "function(String) tuple(HostRef<demo.File>, Error)" {
		t.Fatalf("StructFields() = %#v, %v", fields, ok)
	}

	spec := Func([]Param{
		{Type: Map(String, Array(HostRef("demo.File")))},
		{Type: Interface([]Method{{
			Name: "Read",
			Sig: Function{
				Params: []Param{{Type: "demo.Buffer"}},
				Return: Tuple("demo.Count", Error),
			},
		}})},
	}, Struct([]Member{{Name: "Next", Type: Ptr("demo.Node")}}), false)
	var refs []Type
	WalkNamedTypes(spec, func(t Type) { refs = append(refs, t) })
	want := []Type{"demo.File", "demo.Buffer", "demo.Count", "demo.Node"}
	if len(refs) != len(want) {
		t.Fatalf("refs = %#v, want %#v", refs, want)
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Fatalf("refs = %#v, want %#v", refs, want)
		}
	}

	nestedStruct := Struct([]Member{{
		Name: "Reader",
		Type: Interface([]Method{{
			Name: "Read",
			Sig:  Function{Params: []Param{{Type: Bytes}}, Return: Tuple(Int64, Error)},
		}}),
	}})
	if fields, ok := nestedStruct.StructFields(); !ok || len(fields) != 1 || !fields[0].Type.IsInterface() {
		t.Fatalf("nested interface struct fields = %#v, %v", fields, ok)
	}
	nestedInterface := Type("interface{Factory() interface{Close() Error;};}")
	methods, ok := nestedInterface.InterfaceMethods()
	if !ok || len(methods) != 1 || methods[0].Sig.Return != "interface{Close() Error;}" {
		t.Fatalf("nested interface methods = %#v, %v", methods, ok)
	}
}

func TestRejectGoStyleTypes(t *testing.T) {
	for _, typ := range []Type{"[]int", "*File", "map[string]int", "interface{}", "Ptr<[]int>", "HostRef<gopkg.d7z.net/demo.Type>", "Async<Int64>"} {
		t.Run(typ.String(), func(t *testing.T) {
			if typ.IsCanonical() {
				t.Fatalf("%q unexpectedly canonical", typ)
			}
			if err := typ.ValidateCanonical(); err == nil {
				t.Fatalf("ValidateCanonical(%q) unexpectedly succeeded", typ)
			}
		})
	}
}

func TestCanonicalIDBaseNameAndAssignabilityDepth(t *testing.T) {
	if got := CanonicalTypeID("Ptr<HostRef<demo.Type>>"); got != "demo.Type" {
		t.Fatalf("CanonicalTypeID() = %q, want demo.Type", got)
	}
	if got := Type("Map<String, Array<HostRef<demo.Type>>>").BaseName(); got != "demo.Type" {
		t.Fatalf("BaseName() = %q, want demo.Type", got)
	}
	deepPtr := Ptr(Ptr(Int64))
	if deepPtr.IsAssignableToWithMaxDepth(Int64, 1) {
		t.Fatalf("%q unexpectedly assignable to Int64 with depth 1", deepPtr)
	}
	if !deepPtr.IsAssignableToWithMaxDepth(Int64, 2) {
		t.Fatalf("%q should be assignable to Int64 with depth 2", deepPtr)
	}
}
