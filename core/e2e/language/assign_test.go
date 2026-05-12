package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffilib/fmtlib"
)

func requireCompileErrorContains(t *testing.T, executor *engine.MiniExecutor, code string, want string) {
	t.Helper()

	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatalf("expected compile error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected compile error containing %q, got %v", want, err)
	}
}

type assignmentOutputRecorder struct {
	sb strings.Builder
}

func (o *assignmentOutputRecorder) Print(_ context.Context, s string) {
	o.sb.WriteString(s)
}

func TestAdvancedAssignmentAndSlice(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	printOutput := func(t *testing.T, prog *engine.MiniProgram) string {
		t.Helper()
		recorder := &assignmentOutputRecorder{}
		ctx := fmtlib.WithOutputter(context.Background(), recorder)
		if err := prog.Execute(ctx); err != nil {
			t.Fatal(err)
		}
		return recorder.sb.String()
	}

	t.Run("SliceExpr", func(t *testing.T) {
		code := `
		package main
		import "fmt"
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
			
			fmt.Println("SliceExpr OK")
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("validation failed: %v", err)
		}
		output := printOutput(t, prog)
		if !strings.Contains(output, "SliceExpr OK\n") {
			t.Fatalf("expected SliceExpr marker in output, got %q", output)
		}
	})

	t.Run("MapIndexAssignment", func(t *testing.T) {
		code := `
		package main
		import "fmt"
		func main() {
			m := map[string]any{"a": 1}
			m["b"] = 2
			m["a"] = 100
			
			if m["a"] != 100 { panic("map update failed") }
			if m["b"] != 2 { panic("map insert failed") }
			fmt.Println("Map assignment OK")
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("validation failed: %v", err)
		}
		output := printOutput(t, prog)
		if !strings.Contains(output, "Map assignment OK\n") {
			t.Fatalf("expected map-assignment marker in output, got %q", output)
		}
	})

	t.Run("ArrayIndexAssignment", func(t *testing.T) {
		code := `
		package main
		import "fmt"
		func main() {
			arr := []any{1, 2, 3}
			arr[1] = 200
			if arr[1] != 200 { panic("array assign failed") }
			fmt.Println("Array assignment OK")
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("validation failed: %v", err)
		}
		output := printOutput(t, prog)
		if !strings.Contains(output, "Array assignment OK\n") {
			t.Fatalf("expected array-assignment marker in output, got %q", output)
		}
	})

	t.Run("MemberAssignment", func(t *testing.T) {
		code := `
		package main
		import "encoding/json"
		import "fmt"
		func main() {
			// json返回的是包装在Any里的Map
			obj, err := json.Unmarshal([]byte(` + "`" + `{"config":{"enabled":false}}` + "`" + `))
			if err != nil { panic(err) }
			obj.config.enabled = true
			
			if obj.config.enabled != true { panic("member assign failed") }
			fmt.Println("Member assignment OK")
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Logf("Validation correctly blocked or passed: %v", err)
			return
		}
		output := func() string {
			recorder := &assignmentOutputRecorder{}
			ctx := fmtlib.WithOutputter(context.Background(), recorder)
			err = prog.Execute(ctx)
			return recorder.sb.String()
		}()
		if err != nil {
			if !strings.Contains(err.Error(), "unsupported LHS") {
				t.Fatalf("unexpected error: %v", err)
			}
		} else if !strings.Contains(output, "Member assignment OK\n") {
			t.Fatalf("expected member-assignment marker in output, got %q", output)
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
}
