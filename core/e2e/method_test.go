package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestMethodReceiver(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	type Person struct {
		Age int
	}

	func (p Person) GetAge() int {
		return p.Age
	}

	func (p Person) SetAge(newAge int) {
		p.Age = newAge
	}

	func (p Person) Inc() {
		p.Age++
	}

	func main() {
		p := Person{Age: 20}
		
		// 1. 测试基础调用
		if p.GetAge() != 20 {
			panic("GetAge failed")
		}

		// 2. 测试带参数调用
		p.SetAge(30)
		if p.GetAge() != 30 {
			panic("SetAge failed")
		}

		// 3. 测试无参修改
		p.Inc()
		if p.GetAge() != 31 {
			panic("Inc failed")
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
