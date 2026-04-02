package tests

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

func TestReferenceComparison(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	a := make([]Int64, 1)
	b := a
	if a != b {
		panic("array comparison failed")
	}
    
    m := make(map[String]Int64)
    m2 := m
    if m != m2 {
        panic("map comparison failed")
    }
`
	err := executor.Execute(context.Background(), code, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGlobalVarInitialization(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
package main
var res Any
func main() {
    res = true
    if res != true {
        panic("global var failed")
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
