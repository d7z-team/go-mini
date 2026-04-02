package tests

import (
	"context"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
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

	// 3. 执行 InvokeCallable
	res, err := runtimeObj.InvokeCallable(session, cbVar, "", []*runtime.Var{arg})
	if err != nil {
		t.Fatal(err)
	}

	if res == nil || res.Str != "Echo: Hello VM" {
		t.Fatalf("callback result mismatch: %v", res)
	}
}

func TestFFIInterfaceReturn(t *testing.T) {
	// 1. 设置宿主侧环境，模拟返回一个 InterfaceData
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	type Logger interface {
		Log(String) String
	}

	var hostLogger Any

	func main() {
		// hostLogger 是由宿主注入的接口
		res := hostLogger.Log("Hello from Sandbox")
		if String(res) != "Logged: Hello from Sandbox" {
			panic("FFI interface call failed: " + String(res))
		}
	}
	`
	runtimeObj, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	// 2. 模拟宿主侧：注册一个 Handle 和其对应的方法路由
	// 我们创建一个假的 FFIBridge 来响应 Log 调用
	bridge := &mockLoggerBridge{}

	// 构造 InterfaceData
	ifaceData := ffigo.InterfaceData{
		Handle: 101, // 模拟 Handle ID
		Methods: map[string]string{
			"Log": "function(String) String",
		},
	}

	// 准备注入环境。由于 ExecuteWithEnv 会创建自己的 session，
	// 我们需要一个能构造 Var 的临时 session 或者直接用 nil。
	// 幸运的是 ToVar 已经改进，我们可以利用临时 session。
	tempSession := &runtime.StackContext{Executor: runtimeObj.Executor()}
	v := runtimeObj.ToVar(tempSession, ifaceData, bridge)

	env := map[string]*runtime.Var{
		"hostLogger": v,
	}

	// 3. 执行脚本 (带环境注入)
	err = runtimeObj.ExecuteWithEnv(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
}

type mockLoggerBridge struct{}

func (m *mockLoggerBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return nil, nil // Not used in this test
}

func (m *mockLoggerBridge) Invoke(ctx context.Context, methodName string, args []byte) ([]byte, error) {
	if methodName == "Log" {
		reader := ffigo.NewReader(args)
		// 动态接口调用第一个参数是 receiver (Any)
		_ = reader.ReadAny() // Skip receiver handle

		// 第二个参数是 msg (String, passed as Any)
		msgRaw := reader.ReadAny()
		msg, _ := msgRaw.(string)

		buf := ffigo.GetBuffer()
		buf.WriteAny("Logged: " + msg)
		return buf.Bytes(), nil
	}
	return nil, fmt.Errorf("unknown method: %s", methodName)
}

func (m *mockLoggerBridge) DestroyHandle(handle uint32) error {
	return nil
}
