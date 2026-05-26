package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

// TestModuleConsistency 验证导出变量读取来自显式模块导出表，并反映模块内部更新
func TestModuleConsistency(t *testing.T) {
	executor := engine.NewMiniExecutor()

	if err := executor.UseSurface(surface.Library("counter", surface.GoFile("counter.mgo", `
			package counter
			
			var Val = 0

			func GetVal() int {
				return Val
			}

			func Inc() {
				Val++
			}
			`))); err != nil {
		t.Fatalf("register counter surface: %v", err)
	}

	code := `
	package main
	import "counter"

	func main() {
		// 1. 初始值验证
		if counter.Val != 0 {
			panic("initial value should be 0")
		}

		// 2. 模块内部函数修改导出变量
		counter.Inc()

		// 3. 验证导出读取看到模块内部更新
		if counter.Val != 1 {
			panic("exported read failed")
		}

		// 4. 验证模块内部函数读取一致
		if counter.GetVal() != 1 {
			panic("internal function read failed")
		}

		// 5. 再次验证模块内部修改
		counter.Inc()
		if counter.Val != 2 {
			panic("increment sync failed")
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

// TestReadOnlyModuleProtection 验证 FFI 等虚拟模块的只读属性
func TestReadOnlyModuleProtection(t *testing.T) {
	executor := engine.NewMiniExecutor()
	testsurface.UseRoute(t, executor, "mock.Print", nil, 1, runtime.MustParseRuntimeFuncSig("function(String) Void"), "")

	code := `
	package main
	import "mock"

	func main() {
		// 尝试覆盖 FFI 函数，应该报错
		mock.Print = 1
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		if strings.Contains(err.Error(), "mock.Print") || strings.Contains(err.Error(), "assignment type mismatch") {
			return
		}
		t.Fatalf("Compile failed with unexpected error: %v", err)
	}

	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("Should fail when modifying read-only module")
	}

	if !strings.Contains(err.Error(), "is read-only") {
		t.Fatalf("Expected 'read-only' error, got: %v", err)
	}
}

func TestSourceModuleExportsAreReadOnly(t *testing.T) {
	executor := engine.NewMiniExecutor()
	if err := executor.UseSurface(surface.Library("counter", surface.GoFile("counter.mgo", `
package counter

var Val = 0
`))); err != nil {
		t.Fatalf("register counter surface: %v", err)
	}

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "counter"

func main() {
	counter.Val = 1
}
`)
	if err != nil {
		t.Fatalf("compile source module assignment: %v", err)
	}
	err = prog.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "module counter is read-only") {
		t.Fatalf("expected source module export assignment to be read-only, got %v", err)
	}
}

// TestModuleClosureConsistency 验证被闭包捕获的变量在模块内部修改时的表现
func TestModuleClosureConsistency(t *testing.T) {
	executor := engine.NewMiniExecutor()

	if err := executor.UseSurface(surface.Library("auth", surface.GoFile("auth.mgo", `
			package auth
			var Token = "guest"
			
			func GetTokenFunc() func() string {
				// 返回一个捕获了 Token 的闭包
				return func() string {
					return Token
				}
			}

			func SetToken(v string) {
				Token = v
			}
			`))); err != nil {
		t.Fatalf("register auth surface: %v", err)
	}

	code := `
	package main
	import "auth"

	func main() {
		f := auth.GetTokenFunc()
		if f() != "guest" {
			panic("initial closure value wrong")
		}

		// 通过模块导出函数修改模块变量
		auth.SetToken("admin")

		// 验证闭包是否看到了更新 (证明 Cell 同步成功)
		if f() != "admin" {
			panic("closure did not see the update")
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
