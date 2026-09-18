package compiler

import "testing"

func TestCompileRejectsInvalidVarDeclarationShapes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		code   string
	}{
		{
			name:   "package missing type and initializer",
			source: "package main\nvar Value\nfunc main() {}",
			code:   "hirgen.var.type_or_init",
		},
		{
			name:   "local missing type and initializer",
			source: "package main\nfunc main() { var value; _ = value }",
			code:   "hirgen.var.type_or_init",
		},
		{
			name:   "package extra initializer",
			source: "package main\nvar Value = 1, 2\nfunc main() {}",
			code:   "semantic.decl.value_count",
		},
		{
			name:   "local extra initializer",
			source: "package main\nfunc main() { var value = 1, 2; _ = value }",
			code:   "semantic.decl.value_count",
		},
		{
			name: "package mixed multi result",
			source: `package main
func Pair() (int, int) { return 1, 2 }
var First, Second = 1, Pair()
func main() {}`,
			code: "semantic.decl.value_count",
		},
		{
			name: "package mixed multi result matching flattened count",
			source: `package main
func Pair() (int, int) { return 1, 2 }
var First, Second, Third = Pair(), 3
func main() {}`,
			code: "semantic.decl.value_count",
		},
		{
			name: "local mixed multi result",
			source: `package main
func Pair() (int, int) { return 1, 2 }
func main() { var first, second = 1, Pair(); _, _ = first, second }`,
			code: "semantic.decl.value_count",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", tc.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			if result.OK() {
				t.Fatalf("expected %s diagnostic", tc.code)
			}
			for _, diagnostic := range result.Diagnostics {
				if string(diagnostic.Code) == tc.code {
					return
				}
			}
			t.Fatalf("expected %s diagnostic, got %#v", tc.code, result.Diagnostics)
		})
	}
}

func TestCompileRejectsIncompatibleReusedShortDeclarationTargets(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "call", body: `var value string; value, ok := Pair(); _, _ = value, ok`},
		{name: "map index", body: `var value string; values := map[string]int{"x": 1}; value, ok := values["x"]; _, _ = value, ok`},
		{name: "type assertion", body: `var value string; var subject any = 1; value, ok := subject.(int); _, _ = value, ok`},
		{name: "receive", body: `var value string; ch := make(chan int, 1); value, ok := <-ch; _, _ = value, ok`},
		{name: "variadic function", body: `var value func([]int) int; value, other := FunctionPair(); _, _ = value, other`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `package main
func Pair() (int, int) { return 1, 2 }
func Variadic(values ...int) int { return len(values) }
func Fixed(value int) int { return value }
func FunctionPair() (func(...int) int, func(int) int) { return Variadic, Fixed }
func main() { ` + tc.body + ` }
`
			result, err := compileTestSource("example/main", "main.mgo", source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			if result.OK() {
				t.Fatal("expected reused short target type diagnostic")
			}
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "semantic.assign.type" {
					return
				}
			}
			t.Fatalf("expected semantic.assign.type, got %#v", result.Diagnostics)
		})
	}
}
