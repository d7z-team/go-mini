package tests

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestControlFlow(t *testing.T) {
	executor := engine.NewMiniExecutor()

	t.Run("DeferLIFO", func(t *testing.T) {
		code := `
		package main
		var Trace = ""
		func mark(s string) { Trace = Trace + s + "|" }
		func main() {
			defer mark("defer 1")
			defer mark("defer 2")
			mark("main body")
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		if err := prog.Execute(t.Context()); err != nil {
			t.Fatal(err)
		}
		trace, ok := prog.SharedState().LoadGlobal("Trace")
		if !ok || trace == nil || trace.Str != "main body|defer 2|defer 1|" {
			t.Fatalf("unexpected defer order: %#v", trace)
		}
	})

	t.Run("RangeArray", func(t *testing.T) {
		code := `
		package main
		func main() {
			arr := []any{10, 20, 30}
			sum := 0
			for i, v := range arr {
				sum = sum + v
			}
			if sum != 60 { panic("sum mismatch") }
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		if err := prog.Execute(t.Context()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("RangeMap", func(t *testing.T) {
		code := `
		package main
		func main() {
			m := map[string]any{"a": 1, "b": 2}
			sum := 0
			for k, v := range m {
				sum = sum + v
			}
			if sum != 3 { panic("sum mismatch") }
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		if err := prog.Execute(t.Context()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SwitchStatement", func(t *testing.T) {
		code := `
		package main
		func main() {
			x := 2
			res := ""
			switch x {
			case 1:
				res = "one"
			case 2, 3:
				res = "two or three"
			default:
				res = "other"
			}
			if res != "two or three" { panic("switch failed: " + res) }
			
			// Bool switch
			y := 10
			switch {
			case y < 5:
				panic("should not be here")
			case y >= 10:
				_ = y
			}
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		if err := prog.Execute(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}
