package ffigo

import "testing"

func TestCanonicalBuiltinTypeNameCoversGoIntegerAliases(t *testing.T) {
	converter := NewGoToASTConverter()
	for _, name := range []string{"uint64", "uintptr", "byte", "rune"} {
		if got := converter.canonicalBuiltinTypeName(name); got != "Int64" {
			t.Fatalf("expected %s to normalize to Int64, got %s", name, got)
		}
	}
}
