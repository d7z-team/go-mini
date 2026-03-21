package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestNamedInterface(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	type Describer interface {
		Describe() String
	}

	func printDesc(d Describer) String {
		return d.Describe()
	}

	func main() {
		obj := make(map[String]Any)
		obj["Describe"] = func() String { return "I am a map" }
		
		res := printDesc(obj)
		if res != "I am a map" {
			panic("named interface call failed: " + res)
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

func TestTypeAssertion(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	type Reader interface {
		Read() String
	}

	func main() {
		var a Any = make(map[String]Any)
		a["Read"] = func() String { return "content" }
		
		// 1. 成功断言
		r := a.(Reader)
		if r.Read() != "content" {
			panic("assertion failed")
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

func TestTypeAssertionFailure(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	type Reader interface {
		Read() String
	}

	func main() {
		var a Any = 123
		r := a.(Reader) // 应该报错
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected assertion error, but got nil")
	}
}

func TestInvokeCallable(t *testing.T) {
	// 这个测试验证 InvokeCallable 是否能被宿主用来回调脚本
	executor := engine.NewMiniExecutor()
	code := `
	package main
	var callback Any
	func main() {
		callback = func(msg String) String {
			return "Echo: " + msg
		}
	}
	`
	runtimeObj, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	// 1. 运行脚本以设置回调
	err = runtimeObj.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 2. 模拟宿主侧获取回调闭包并执行
	session := runtimeObj.LastSession()
	cbVar, err := session.Load("callback")
	if err != nil {
		t.Fatal(err)
	}

	if cbVar == nil || cbVar.VType == runtime.TypeAny && cbVar.Ref == nil {
		t.Fatal("callback variable is nil after script execution")
	}

	// 构造参数
	arg := &runtime.Var{VType: runtime.TypeString, Str: "Hello VM"}

	// 宿主调用 InvokeCallable
	res, err := runtimeObj.InvokeCallable(session, cbVar, []*runtime.Var{arg})
	if err != nil {
		t.Fatal(err)
	}

	if res == nil || res.Str != "Echo: Hello VM" {
		t.Fatalf("callback result mismatch: %v", res)
	}
}
