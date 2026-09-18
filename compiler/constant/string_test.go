package constant

import (
	"encoding/json"
	"testing"
)

func TestStringConstantOperations(t *testing.T) {
	left := String("<prefix>\n", "String", true)
	right := String("\x00\xff", "example.Label", false)
	value, ok := Binary("+", left, right)
	if !ok || value.Text != `"<prefix>\n\x00\xff"` || value.Type != "example.Label" || value.Untyped {
		t.Fatalf("concatenation: %+v %v", value, ok)
	}
	if _, ok := value.Int64(); ok {
		t.Fatal("string accepted as integer")
	}
	if _, ok := Unary("-", value); ok {
		t.Fatal("string accepted as numeric operand")
	}
	raw := json.RawMessage(`"123"`)
	str, ok := FromJSON(raw, "String", true)
	if !ok || str.Text != `"123"` || string(str.JSON()) != string(raw) {
		t.Fatalf("numeric-looking string: %+v", str)
	}
}
