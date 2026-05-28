package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestArithmeticLoopAndBranchExecution(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
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
	executor := engine.MustNewMiniExecutor()
	code := `
	package main
	const PI = "3.14"
	const ONE = 1
	var globalMsg = "hello"

	func add(v int) int {
		return v + ONE
	}

	func main() {
		if globalMsg != "hello" {
			panic("global var failed")
		}
		if PI != "3.14" {
			panic("string constant failed")
		}
		if add(ONE) != 2 {
			panic("constant call argument failed")
		}
		ONE := 10
		if add(ONE) != 11 {
			panic("local variable should shadow constant")
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

	requireCompileErrorContains(t, executor, `
package main
const ONE = 1
func main() {
	ONE = 2
}`, "cannot assign to constant ONE")
}

func TestReferenceComparison(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
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
	executor := engine.MustNewMiniExecutor()
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
