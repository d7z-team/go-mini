package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// TestModuleConsistency 验证跨模块变量修改的强一致性
func TestModuleConsistency(t *testing.T) {
	executor := engine.NewMiniExecutor()

	executor.SetLoader(func(path string) (*ast.ProgramStmt, error) {
		if path == "counter" {
			code := `
			package counter
			
			var Val = 0

			func GetVal() int {
				return Val
			}

			func Inc() {
				Val++
			}
			`
			converter := ffigo.NewGoToASTConverter()
			node, _ := converter.ConvertSource(code)
			return node.(*ast.ProgramStmt), nil
		}
		return nil, fmt.Errorf("module not found: %s", path)
	})

	code := `
	package main
	import "counter"

	func main() {
		// 1. 初始值验证
		if counter.Val != 0 {
			panic("initial value should be 0")
		}

		// 2. 跨模块直接修改
		counter.Val = 100
		
		// 3. 验证导出读取
		if counter.Val != 100 {
			panic("direct read failed")
		}

		// 4. 验证模块内部函数读取 (证明 Context 同步成功)
		if counter.GetVal() != 100 {
			panic("internal function read failed")
		}

		// 5. 验证模块内部修改
		counter.Inc()
		if counter.Val != 101 {
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
	executor.InjectStandardLibraries()

	code := `
	package main
	import "fmt"

	func main() {
		// 尝试覆盖 FFI 函数，应该报错
		fmt.Println = 1
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("Should fail when modifying read-only module")
	}

	if !strings.Contains(err.Error(), "is read-only") {
		t.Fatalf("Expected 'read-only' error, got: %v", err)
	}
}

// TestModuleClosureConsistency 验证被闭包捕获的变量在跨模块修改时的表现
func TestModuleClosureConsistency(t *testing.T) {
	executor := engine.NewMiniExecutor()

	executor.SetLoader(func(path string) (*ast.ProgramStmt, error) {
		if path == "auth" {
			code := `
			package auth
			var Token = "guest"
			
			func GetTokenFunc() func() string {
				// 返回一个捕获了 Token 的闭包
				return func() string {
					return Token
				}
			}
			`
			converter := ffigo.NewGoToASTConverter()
			node, _ := converter.ConvertSource(code)
			return node.(*ast.ProgramStmt), nil
		}
		return nil, fmt.Errorf("module not found: %s", path)
	})

	code := `
	package main
	import "auth"

	func main() {
		f := auth.GetTokenFunc()
		if f() != "guest" {
			panic("initial closure value wrong")
		}

		// 修改模块变量
		auth.Token = "admin"

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
