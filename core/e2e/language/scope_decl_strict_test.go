package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestStrictScopeAndDeclarationValidation(t *testing.T) {
	cases := []struct {
		name string
		code string
		want string
	}{
		{
			name: "global explicit initializer mismatch",
			code: `package main
var X Int64 = "bad"
func main() {}`,
			want: "类型不匹配",
		},
		{
			name: "global explicit assignment mismatch",
			code: `package main
var X Int64
func main() {
	X = "bad"
}`,
			want: "类型不匹配",
		},
		{
			name: "local var redeclared in same block",
			code: `package main
func main() {
	var x Int64
	var x String
}`,
			want: "redeclared",
		},
		{
			name: "short declaration rejects complex lhs",
			code: `package main
func main() {
	arr := []Int64{0}
	arr[0] := 1
}`,
			want: "non-name on left side of :=",
		},
		{
			name: "short declaration rejects duplicate lhs",
			code: `package main
func main() {
	x, x := 1, 2
	_ = x
}`,
			want: "repeated on left side of :=",
		},
		{
			name: "range short declaration rejects duplicate names",
			code: `package main
func main() {
	for i, i := range []Int64{7} {
		_ = i
	}
}`,
			want: "repeated on left side of :=",
		},
		{
			name: "duplicate params rejected",
			code: `package main
func f(x Int64, x Int64) Int64 { return x }
func main() { _ = f(1, 2) }`,
			want: "parameter redeclared",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor := engine.NewMiniExecutor()
			_, err := executor.NewRuntimeByGoCode(tc.code)
			if err == nil {
				t.Fatal("expected compile error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected compile error:\n%v", err)
			}
		})
	}
}

func TestAnyValueCannotPolluteConcreteSlot(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `package main
func get() Any { return "bad" }
func main() {
	var x Int64
	x = get()
}`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err == nil {
		t.Fatal("expected runtime assignment error")
	} else if !strings.Contains(err.Error(), "cannot assign") && !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("unexpected runtime error: %v", err)
	}
}

func TestInferredGlobalKeepsInitializerType(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `package main
var X = 41
func main() {
	X = X + 1
	if X != 42 {
		panic("bad inferred global")
	}
}`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestExplicitAnyNeverNarrowsStatically(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `package main
func get() Any { return 1 }
func main() {
	var x Any = 1
	x = "ok"
	var y = get()
	y = 2
	y = "still ok"
}`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestInferredConcreteVariableRejectsDifferentAssignment(t *testing.T) {
	executor := engine.NewMiniExecutor()
	_, err := executor.NewRuntimeByGoCode(`package main
func main() {
	var x = 1
	x = "bad"
}`)
	if err == nil {
		t.Fatal("expected compile error")
	}
	if !strings.Contains(err.Error(), "类型不匹配") {
		t.Fatalf("unexpected compile error: %v", err)
	}
}

func TestVarDeclarationDestructuresTupleReturn(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `package main
func pair() (Int64, String) { return 7, "go" }
func main() {
	var a, b = pair()
	if a != 7 || b != "go" {
		panic("bad tuple declaration")
	}
}`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestGlobalVarDeclarationDestructuresTupleReturnOnce(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `package main
var Hits = 0
var A, B = pair()
func pair() (Int64, String) {
	Hits = Hits + 1
	return 7, "go"
}
func main() {
	if Hits != 1 || A != 7 || B != "go" {
		panic("bad global tuple declaration")
	}
}`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestVarDeclarationDestructuresAnyArrayAtRuntime(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `package main
func values() Any { return []Any{1, "go"} }
func main() {
	var a, b = values()
	if a != 1 || b != "go" {
		panic("bad dynamic declaration")
	}
}`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestShortDeclarationDestructuresArbitraryTupleReturn(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `package main
func triple() (Int64, String, Bool) { return 7, "go", true }
func main() {
	a, b, c := triple()
	if a != 7 || b != "go" || !c {
		panic("bad triple declaration")
	}
}`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestTupleReturnCannotOccupySingleValueSlots(t *testing.T) {
	cases := []struct {
		name string
		code string
	}{
		{
			name: "var inferred",
			code: `package main
func pair() (Int64, String) { return 1, "go" }
func main() {
	var x = pair()
	_ = x
}`,
		},
		{
			name: "var explicit any",
			code: `package main
func pair() (Int64, String) { return 1, "go" }
func main() {
	var x Any = pair()
	_ = x
}`,
		},
		{
			name: "short declaration single lhs",
			code: `package main
func pair() (Int64, String) { return 1, "go" }
func main() {
	x := pair()
	_ = x
}`,
		},
		{
			name: "multi short declaration expression list",
			code: `package main
func pair() (Int64, String) { return 1, "go" }
func main() {
	a, b := pair(), 3
	_, _ = a, b
}`,
		},
		{
			name: "multi assignment expression list",
			code: `package main
func pair() (Int64, String) { return 1, "go" }
func main() {
	var a Any
	var b Int64
	a, b = pair(), 3
}`,
		},
		{
			name: "return any",
			code: `package main
func pair() (Int64, String) { return 1, "go" }
func bad() Any { return pair() }
func main() { _ = bad() }`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor := engine.NewMiniExecutor()
			_, err := executor.NewRuntimeByGoCode(tc.code)
			if err == nil {
				t.Fatal("expected compile error")
			}
			if !strings.Contains(err.Error(), "multiple-value") {
				t.Fatalf("unexpected compile error: %v", err)
			}
		})
	}
}

func TestDeclarationRHSUsesOuterBindingBeforeNewNamesEnterScope(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `package main
func main() {
	x := 5
	{
		var x = x
		if x != 5 {
			panic("var declaration RHS did not read outer x")
		}
	}
	y := 9
	{
		y, z := y, 7
		if y != 9 || z != 7 {
			panic("short declaration RHS did not read outer y")
		}
	}
}`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}
