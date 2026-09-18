package compiler

import "testing"

func TestCompileRejectsIntrinsicFunctionEscape(t *testing.T) {
	result, err := compileTestSource("reflect", "intrinsic.mgo", `package reflect
func runtimeTypeOf(value any) any { return value }
var escaped = runtimeTypeOf
func Main() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "hirgen.intrinsic.escape" {
			return
		}
	}
	t.Fatalf("missing intrinsic escape diagnostic: %#v", result.Diagnostics)
}
