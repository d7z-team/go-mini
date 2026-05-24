package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func requireCompileErrorContains(t *testing.T, executor *engine.MiniExecutor, code, want string) {
	t.Helper()

	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatalf("expected compile error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected compile error containing %q, got %v", want, err)
	}
}

func TestAdvancedAssignmentAndSlice(t *testing.T) {
	executor := engine.NewMiniExecutor()

	execute := func(t *testing.T, prog *engine.ExecutableProgram) {
		t.Helper()
		if err := prog.Execute(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("SliceExpr", func(t *testing.T) {
		code := `
		package main
		func main() {
			arr := []byte("hello world")
			sub := arr[0:5]
			if string(sub) != "hello" { panic("slice failed") }

			s := "hello world"
			subStr := s[0:5]
			if subStr != "hello" { panic("string slice failed") }
			
			arr2 := []any{1, 2, 3, 4, 5}
			sub2 := arr2[1:3]
			if len(sub2) != 2 { panic("array slice len mismatch") }
			if sub2[0] != 2 { panic("array slice value mismatch") }
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("validation failed: %v", err)
		}
		execute(t, prog)
	})

	t.Run("MapIndexAssignment", func(t *testing.T) {
		code := `
		package main
		func main() {
			m := map[string]any{"a": 1}
			m["b"] = 2
			m["a"] = 100
			
			if m["a"] != 100 { panic("map update failed") }
			if m["b"] != 2 { panic("map insert failed") }
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("validation failed: %v", err)
		}
		execute(t, prog)
	})

	t.Run("ArrayIndexAssignment", func(t *testing.T) {
		code := `
		package main
		func main() {
			arr := []any{1, 2, 3}
			arr[1] = 200
			if arr[1] != 200 { panic("array assign failed") }
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("validation failed: %v", err)
		}
		execute(t, prog)
	})

	t.Run("MemberAssignment", func(t *testing.T) {
		code := `
		package main
		func main() {
			obj := map[string]any{"config": map[string]any{"enabled": false}}
			obj.config.enabled = true
			
			if obj.config.enabled != true { panic("member assign failed") }
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Logf("Validation correctly blocked or passed: %v", err)
			return
		}
		err = prog.Execute(context.Background())
		if err != nil {
			if !strings.Contains(err.Error(), "unsupported LHS") {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	})

	t.Run("MultiAssignment", func(t *testing.T) {
		code := `
		package main
		func main() {
			a, b := 1, 2
			a, b = b, a
			if a != 2 || b != 1 {
				panic("multi assignment failed")
			}
		}
		`
		runtime, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		err = runtime.Execute(context.Background())
		if err != nil {
			t.Fatalf("multi assignment test failed: %v", err)
		}
	})

	t.Run("Assignment Evaluation Order (LHS before RHS)", func(t *testing.T) {
		code := `
		package main
		var order string
		func f() int64 { order = order + "L"; return 0 }
		func g() int64 { order = order + "R"; return 1 }
		func main() {
			arr := make([]int64, 2)
			// Go 规范：LHS 先求值，RHS 后求值
			arr[f()] = g()
			
			if order != "LR" {
				panic("assignment order failed: expected LR, got " + order)
			}
		}
		`
		runtime, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		err = runtime.Execute(context.Background())
		if err != nil {
			t.Fatalf("assignment order test failed: %v", err)
		}
	})

	t.Run("UndeclaredSingleAssignmentFails", func(t *testing.T) {
		requireCompileErrorContains(t, executor, `
		package main
		func main() {
			x = 1
		}
		`, "undefined identifier in assignment: x")
	})

	t.Run("UndeclaredIncDecFails", func(t *testing.T) {
		requireCompileErrorContains(t, executor, `
		package main
		func main() {
			counter++
		}
		`, "undefined identifier in assignment: counter")
	})

	t.Run("BlockScopedShortDeclDoesNotLeak", func(t *testing.T) {
		requireCompileErrorContains(t, executor, `
		package main
		func main() {
			if true {
				page := 1
				_ = page
			}
			page = 2
		}
		`, "undefined identifier in assignment: page")
	})

	t.Run("ForInitShortDeclDoesNotLeak", func(t *testing.T) {
		requireCompileErrorContains(t, executor, `
		package main
		func main() {
			for i := 0; i < 3; i++ {
				_ = i
			}
			i = 3
		}
		`, "undefined identifier in assignment: i")
	})

	t.Run("RangeAssignRequiresExistingVariables", func(t *testing.T) {
		requireCompileErrorContains(t, executor, `
		package main
		func main() {
			for i = range []Int64{1, 2, 3} {
				_ = i
			}
		}
		`, "undefined identifier in assignment: i")
	})
}
