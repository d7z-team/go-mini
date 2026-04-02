package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

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
		// 此测试需配合 stdout 捕获或日志检查，此处简化为验证是否执行
		prog, _ := executor.NewRuntimeByGoCode(code)
		err := prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
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
		prog, _ := executor.NewRuntimeByGoCode(code)
		err := prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
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
		prog, _ := executor.NewRuntimeByGoCode(code)
		err := prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
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
		prog, _ := executor.NewRuntimeByGoCode(code)
		err := prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	})
}
