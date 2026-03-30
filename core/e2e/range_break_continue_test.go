package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestRangeBreakContinue(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	t.Run("RangeArrayBreak", func(t *testing.T) {
		code := `
		package main
		func main() {
			arr := []any{1, 2, 3, 4, 5}
			sum := 0
			for _, v := range arr {
				if v > 3 {
					break
				}
				sum = sum + v
			}
			if sum != 6 { panic("sum should be 6 (1+2+3), got " + sum) }
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("RangeArrayContinue", func(t *testing.T) {
		code := `
		package main
		func main() {
			arr := []any{1, 2, 3, 4, 5}
			sum := 0
			for _, v := range arr {
				if v == 3 {
					continue
				}
				sum = sum + v
			}
			if sum != 12 { panic("sum should be 12 (1+2+4+5), got " + sum) }
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("SwitchBreak", func(t *testing.T) {
		code := `
		package main
		func main() {
			x := 1
			count := 0
			switch x {
			case 1:
				count = 1
				break
				count = 2
			}
			if count != 1 { panic("count should be 1, got " + count) }
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ForSwitchBreak", func(t *testing.T) {
		code := `
		package main
		func main() {
			sum := 0
			for i := 0; i < 5; i++ {
				switch i {
				case 2:
					break // This should break the switch, not the loop
				}
				sum = sum + i
			}
			if sum != 10 { panic("sum should be 10 (0+1+2+3+4), got " + sum) }
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatal(err)
		}
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	})
}
