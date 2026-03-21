package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestMultiAssignment(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	t.Run("ResultDestructuring", func(t *testing.T) {
		code := `
		package main
		import "json"
		func main() {
			// 1. Test Result destructuring (val, err)
			// Use backticks for raw string to avoid escape hell
			v, err := json.Unmarshal([]byte(` + "`" + `{"age": 25}` + "`" + `))
			if err != nil {
				panic("Unmarshal failed: " + err)
			}
			
			if v.age != 25 {
				panic("Value mismatch")
			}

			// 2. Test failed Unmarshal
			v2, err2 := json.Unmarshal([]byte("{invalid}"))
			if err2 == nil {
				panic("Should have failed")
			}
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
	})

	t.Run("ArrayDestructuring", func(t *testing.T) {
		code := `
		package main
		func main() {
			a, b, c := []int{10, 20, 30}
			if a != 10 || b != 20 || c != 30 {
				panic("Array destructuring failed")
			}

			// Test reassignment
			a, b = []int{100, 200}
			if a != 100 || b != 200 {
				panic("Reassignment failed")
			}
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
	})

	t.Run("TupleDestructuring", func(t *testing.T) {
		// Mock a bridge that returns a Tuple
		bridge := &mockTupleBridge{}
		executor.RegisterFFI("math.DivMod", bridge, 1, "function(Int64, Int64) tuple(Int64, Int64)", "")
		executor.AddFuncSpec("math.DivMod", "function(Int64, Int64) tuple(Int64, Int64)")

		code := `
		package main
		import "math"
		func main() {
			q, r := math.DivMod(10, 3)
			if q != 3 || r != 1 {
				panic("Tuple destructuring failed")
			}
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
	})

	t.Run("ComplexDestructuring", func(t *testing.T) {
		code := `
		package main
		
		type Point struct {
			X int
			Y int
		}

		func getCoords() []int {
			return []int{100, 200}
		}

		func main() {
			arr := []int{0, 0}
			p := Point{X: 0, Y: 0}

			// Complex LHS: array index and member expression
			arr[1], p.X = getCoords()

			if arr[1] != 100 {
				panic("arr[1] mismatch")
			}
			if p.X != 200 {
				panic("p.X mismatch")
			}
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
	})
}

type mockTupleBridge struct{}

func (b *mockTupleBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	reader := ffigo.NewReader(args)
	a := reader.ReadInt64()
	bVal := reader.ReadInt64()

	q := a / bVal
	r := a % bVal

	buf := ffigo.GetBuffer()
	buf.WriteAny(q)
	buf.WriteAny(r)
	return buf.Bytes(), nil
}

func (b *mockTupleBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return nil, nil
}

func (b *mockTupleBridge) DestroyHandle(handle uint32) error { return nil }
