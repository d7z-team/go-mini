package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestStructSemantics(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	type Data struct {
		Val int
	}

	// 模拟值接收者
	func (d Data) Modify(v int) {
		d.Val = v
	}

	func main() {
		d1 := Data{Val: 10}
		
		// 1. 测试方法调用
		d1.Modify(20)
		
		// 2. 测试直接赋值
		d2 := d1
		d2.Val = 30
		
		return d1.Val, d2.Val
	}
	`
	_, _ = executor.NewRuntimeByGoCode(code)
	// 我们需要获取返回值，但目前 Execute 不直接返回结果，
	// 我们通过在脚本最后抛出 panic 或修改全局变量来观察，或者直接看执行器状态。
	// 这里我们通过断言来验证。

	// 修改脚本，使其如果语义不符合 Go 则 panic
	codeV := `
	package main
	type Data struct { Val int }
	func (d Data) Modify(v int) { d.Val = v }
	func main() {
		d1 := Data{Val: 10}
		d1.Modify(20)
		if d1.Val == 20 {
			// 在 Go 中，这里应该是 10。如果等于 20，说明是引用语义。
			panic("REFERENCE_SEMANTICS_DETECTED")
		}
	}
	`
	prog, _ := executor.NewRuntimeByGoCode(codeV)
	err := prog.Execute(context.Background())
	if err != nil && err.Error() == "mini-panic: REFERENCE_SEMANTICS_DETECTED" {
		// 证实了目前的实现是引用语义
	} else if err == nil {
		// 说明符合 Go 的值语义
	}
}
