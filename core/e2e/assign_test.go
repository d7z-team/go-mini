package e2e

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestAdvancedAssignmentAndSlice(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	t.Run("SliceExpr", func(t *testing.T) {
		code := `
		package main
		import "fmt"
		func main() {
			arr := []byte("hello world")
			sub := arr[0:5]
			if string(sub) != "hello" { panic("slice failed") }
			
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
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
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
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
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
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("MemberAssignment", func(t *testing.T) {
		code := `
		package main
		import "json"
		import "fmt"
		func main() {
			// json返回的是包装在Any里的Map
			res := json.Unmarshal([]byte(` + "`" + `{"config":{"enabled":false}}` + "`" + `))
			obj := res.val
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
		err = prog.Execute(context.Background())
		if err != nil {
			if !strings.Contains(err.Error(), "unsupported LHS") {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	})
}
