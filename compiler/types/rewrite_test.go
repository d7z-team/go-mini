package types

import "testing"

func TestRewriteCanonicalTextResolvesNestedNames(t *testing.T) {
	resolve := func(name string) (string, bool) {
		switch name {
		case "int":
			return "Int", true
		case "byte":
			return "Uint8", true
		case "Alias":
			return "Map<string, []byte>", true
		case "string":
			return "String", true
		case "[]byte":
			return "Slice<byte>", true
		default:
			return "", false
		}
	}
	got, ok := RewriteCanonicalText("Array<3, Alias>", resolve)
	if !ok {
		t.Fatalf("rewrite failed")
	}
	want := "Array<3, Map<String, Slice<Uint8>>>"
	if got != want {
		t.Fatalf("rewrite = %q, want %q", got, want)
	}
}

func TestRewriteCanonicalTextPreservesFunctionStructAndInterfaceShape(t *testing.T) {
	resolve := func(name string) (string, bool) {
		switch name {
		case "int":
			return "Int", true
		case "string":
			return "String", true
		case "Alias":
			return "Slice<int>", true
		default:
			return "", false
		}
	}
	input := "interface{~Alias|int, Next:function(variadic Slice<string>) tuple(Alias, int), Embed}"
	got, ok := RewriteCanonicalText(input, resolve)
	if !ok {
		t.Fatalf("rewrite failed")
	}
	want := "interface{~Slice<Int>|Int,Next:function(variadic Slice<String>) tuple(Slice<Int>, Int),Embed}"
	if got != want {
		t.Fatalf("rewrite = %q, want %q", got, want)
	}

	got, ok = RewriteCanonicalText("struct{Name:Alias `json:\"name\"`, Count:int}", resolve)
	if !ok {
		t.Fatalf("rewrite struct failed")
	}
	want = "struct{Name:Slice<Int> `json:\"name\"`,Count:Int}"
	if got != want {
		t.Fatalf("rewrite struct = %q, want %q", got, want)
	}

	got, ok = RewriteCanonicalText("struct{embedded Alias:Alias}", resolve)
	if !ok || got != "struct{embedded Alias:Slice<Int>}" {
		t.Fatalf("rewrite embedded struct = %q, %t", got, ok)
	}
	table := &TypeTable{}
	ref, err := NewParser("test", table).Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	node, ok := table.Node(ref)
	if !ok || len(node.Fields) != 1 || !node.Fields[0].Embedded || FormatWithTable(table, ref) != got {
		t.Fatalf("embedded struct canonical round trip lost metadata: %#v", node.Fields)
	}
}
