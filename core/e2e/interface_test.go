package e2e

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestInterfaceMap(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func main() {
		// 定义一个简单的对象 (Map)
		myReader := make(map[String]Any)
		myReader["Read"] = func() String {
			return "data from map"
		}
		
		// 赋值给接口 (使用正规 Go 语法 Read())
		var i interface{Read()} = myReader
		
		// 调用接口方法
		res := i.Read()
		if res != "data from map" {
			panic("interface call failed: " + res)
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

func TestInterfaceHandle(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
	package main
	import "fmt"
	
	func main() {
		// Module (fmt) as an interface
		var i interface{Printf(String, Any)} = fmt
		i.Printf("hello %s\n", "interface")
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

func TestInterfaceAssignmentFailure(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func main() {
		badObj := make(map[String]Any)
		badObj["NotRead"] = func() {}
		
		// 应该报错：missing method Read
		var i interface{Read()} = badObj
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected error for missing method, but got nil")
	}
}

func TestInterfaceMethodWhitelist(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func main() {
		obj := make(map[String]Any)
		obj["Read"] = func() String { return "ok" }
		obj["Secret"] = func() String { return "hidden" }
		
		var i interface{Read()} = obj
		
		// 应该可以调用 Read
		if i.Read() != "ok" {
			panic("Read failed")
		}
		
		// 应该在编译期或运行期报错：Secret 不在接口契约中
		res := i.Secret()
	}
	`
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("expected error for calling method not in interface, but got nil")
	}
}

func TestInterfaceToInterface(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func main() {
		obj := make(map[String]Any)
		obj["Read"] = func() String { return "r" }
		obj["Write"] = func() String { return "w" }
		
		var rw interface{Read(); Write()} = obj
		
		// 宽接口赋值给窄接口
		var r interface{Read()} = rw
		
		if r.Read() != "r" {
			panic("Read failed")
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

func TestInterfaceSignatureMismatch(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func main() {
		obj := make(map[String]Any)
		// 接口要求 Read(String) String，但这里提供了 Read(Int64) String
		obj["Read"] = func(a Int64) String {
			return "wrong"
		}
		
		var i interface{Read(String) String} = obj
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected signature mismatch error, but got nil")
	}
	if !strings.Contains(err.Error(), "incompatible method Read") {
		t.Fatalf("expected incompatible method error, got: %v", err)
	}
}

func TestInterfaceAnyPenetration(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	import "json"
	
	func main() {
		// json.Unmarshal 返回的是 (Any, string)，里面包裹的是真正的脚本 Map (TypeAny -> TypeMap)
		data, err := json.Unmarshal([]byte(` + "`" + `{"name": "mini", "Read": "captured"}` + "`" + `))
		if err != nil { panic(err) }
		
		// 我们手工给 data (TypeAny) 注入一个方法模拟复杂场景
		data["Read"] = func() String { return "from nested" }
		
		// 接口应该能穿透 Any 看到内部 Map 的方法
		var i interface{Read()} = data
		if i.Read() != "from nested" {
			panic("Any penetration failed")
		}
	}
	`
	executor.InjectStandardLibraries()
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
