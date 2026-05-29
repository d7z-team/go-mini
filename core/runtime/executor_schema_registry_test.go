package runtime

import (
	"strings"
	"testing"
)

func TestMergeStructSchemaRejectsTagMismatch(t *testing.T) {
	left := MustParseRuntimeStructSpec("demo.User", StructOwnershipVMValue, "struct { Name String; }")
	left.Fields[0].Tag = "json:\"name\""
	leftField := left.ByName["Name"]
	leftField.Tag = "json:\"name\""
	left.ByName["Name"] = leftField
	left.TypeInfo.Fields[0].Tag = "json:\"name\""

	right := MustParseRuntimeStructSpec("demo.User", StructOwnershipVMValue, "struct { Name String; }")
	right.Fields[0].Tag = "json:\"label\""
	rightField := right.ByName["Name"]
	rightField.Tag = "json:\"label\""
	right.ByName["Name"] = rightField
	right.TypeInfo.Fields[0].Tag = "json:\"label\""

	_, err := MergeStructSchema("demo.User", left, right)
	if err == nil {
		t.Fatal("expected tag mismatch to reject struct schema merge")
	}
	if !strings.Contains(err.Error(), "json") {
		t.Fatalf("expected tag detail in error, got %v", err)
	}
}
