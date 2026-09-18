package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestMethodReceiver(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
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

		// 2. value receiver follows value semantics: mutation does not affect p
		p.SetAge(30)
		if p.GetAge() != 20 {
			panic("SetAge mutated value receiver")
		}

		// 3. value receiver Inc also does not affect p
		p.Inc()
		if p.GetAge() != 20 {
			panic("Inc mutated value receiver")
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
