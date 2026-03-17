package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestScriptVariadic(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
	package main
	
	func sum(args ...int) int {
		total := 0
		for _, v := range args {
			total = total + v
		}
		return total
	}

	func sum64(args ...int64) int64 {
		var total int64 = 0
		for _, v := range args {
			total = total + v
		}
		return total
	}

	func main() {
		if sum() != 0 {
			panic("sum() mismatch")
		}
		if sum(1, 2, 3) != 6 {
			panic("sum(1, 2, 3) mismatch")
		}
		if sum(10, 20) != 30 {
			panic("sum(10, 20) mismatch")
		}
		if sum64(10, 20, 30, 40) != 100 {
			panic("sum64 mismatch")
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
}
