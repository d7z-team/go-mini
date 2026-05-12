package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffilib/fmtlib"
)

type outputRecorder struct {
	sb strings.Builder
}

func (o *outputRecorder) Print(_ context.Context, s string) {
	o.sb.WriteString(s)
}

func executeWithCapturedOutput(t *testing.T, prog *engine.MiniProgram) string {
	t.Helper()

	recorder := &outputRecorder{}
	ctx := fmtlib.WithOutputter(context.Background(), recorder)
	if err := prog.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	return recorder.sb.String()
}

func TestControlFlow(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	t.Run("DeferLIFO", func(t *testing.T) {
		code := `
		package main
		import "fmt"
		func main() {
			defer fmt.Println("defer 1")
			defer fmt.Println("defer 2")
			fmt.Println("main body")
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		output := executeWithCapturedOutput(t, prog)
		if output != "main body\ndefer 2\ndefer 1\n" {
			t.Fatalf("unexpected defer output order: %q", output)
		}
	})

	t.Run("RangeArray", func(t *testing.T) {
		code := `
		package main
		import "fmt"
		func main() {
			arr := []any{10, 20, 30}
			sum := 0
			for i, v := range arr {
				sum = sum + v
			}
			if sum != 60 { panic("sum mismatch") }
			fmt.Println("Range array OK")
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		output := executeWithCapturedOutput(t, prog)
		if !strings.Contains(output, "Range array OK\n") {
			t.Fatalf("expected range-array marker in output, got %q", output)
		}
	})

	t.Run("RangeMap", func(t *testing.T) {
		code := `
		package main
		import "fmt"
		func main() {
			m := map[string]any{"a": 1, "b": 2}
			sum := 0
			for k, v := range m {
				sum = sum + v
			}
			if sum != 3 { panic("sum mismatch") }
			fmt.Println("Range map OK")
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		output := executeWithCapturedOutput(t, prog)
		if !strings.Contains(output, "Range map OK\n") {
			t.Fatalf("expected range-map marker in output, got %q", output)
		}
	})

	t.Run("SwitchStatement", func(t *testing.T) {
		code := `
		package main
		import "fmt"
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
				fmt.Println("Bool switch OK")
			}
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		output := executeWithCapturedOutput(t, prog)
		if !strings.Contains(output, "Bool switch OK\n") {
			t.Fatalf("expected bool-switch marker in output, got %q", output)
		}
	})
}
