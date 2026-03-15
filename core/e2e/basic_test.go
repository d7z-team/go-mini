package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestComprehensive(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func add(a int, b int) int {
		return a + b
	}

	func main() {
		sum := 0
		for i := 0; i < 10; i++ {
			sum = add(sum, i)
		}
		
		finalRes := 0
		if sum == 45 {
			finalRes = 1
		} else {
			finalRes = -1
		}
		
		// 校验结果
		if finalRes != 1 {
			panic("logic failed")
		}
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
}

func TestConstAndVar(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	const PI = "3.14"
	var globalMsg = "hello"

	func main() {
		if globalMsg != "hello" {
			panic("global var failed")
		}
		// 注意：目前 ConstRefExpr 在执行器中直接从 Program.Constants 读取字符串
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
}
