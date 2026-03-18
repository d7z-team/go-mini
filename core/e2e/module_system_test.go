package e2e

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// TestModuleComprehensive 综合测试：函数、变量、常量、结构体导出
func TestModuleComprehensive(t *testing.T) {
	executor := engine.NewMiniExecutor()

	executor.SetLoader(func(path string) (*ast.ProgramStmt, error) {
		if path == "lib" {
			code := `
			package lib
			
			const PI = "3.14"
			var Version = "1.0.0"

			type Point struct {
				X int
				Y int
			}

			func NewPoint(x int, y int) Point {
				return Point{X: x, Y: y}
			}
			`
			converter := ffigo.NewGoToASTConverter()
			node, err := converter.ConvertSource(code)
			if err != nil {
				return nil, err
			}
			return node.(*ast.ProgramStmt), nil
		}
		return nil, fmt.Errorf("module not found: %s", path)
	})

	code := `
	package main
	import "lib"

	func main() {
		// 1. 常量导出测试
		if lib.PI != "3.14" {
			panic("const export failed: " + string(lib.PI))
		}

		// 2. 变量导出测试
		if lib.Version != "1.0.0" {
			panic("var export failed")
		}

		// 3. 结构体导出与实例化测试
		p1 := lib.Point{X: 10, Y: 20}
		if p1.X != 10 {
			panic("struct initialization failed")
		}

		// 4. 函数导出返回结构体测试
		p2 := lib.NewPoint(30, 40)
		if p2.Y != 40 {
			panic("function returning struct failed")
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

// TestCircularDependency 循环依赖拦截测试
func TestCircularDependency(t *testing.T) {
	executor := engine.NewMiniExecutor()

	executor.SetLoader(func(path string) (*ast.ProgramStmt, error) {
		var code string
		if path == "a" {
			code = "package a; import \"b\"; func Run() {}"
		} else if path == "b" {
			code = "package b; import \"a\"; func Run() {}"
		} else {
			return nil, errors.New("not found")
		}
		converter := ffigo.NewGoToASTConverter()
		node, _ := converter.ConvertSource(code)
		return node.(*ast.ProgramStmt), nil
	})

	code := "package main; import \"a\"; func main() { a.Run() }"
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("Should fail due to circular dependency")
	}
	expectedErr := "circular dependency detected"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("Expected error containing '%s', got: %v", expectedErr, err)
	}
}

// TestNestedFFIPath 嵌套 FFI 路径匹配测试
func TestNestedFFIPath(t *testing.T) {
	executor := engine.NewMiniExecutor()

	// 模拟注册一个嵌套路径的 FFI
	executor.RegisterFFI("net.http.Get", nil, 1, "function(String) String")

	code := `
	package main
	import "net/http"

	func main() {
		// 虽然导入的是 net/http，但应能匹配到 net.http.Get
		res := http.Get("url")
	}
	`
	// 这里我们只需要验证编译通过，因为 nil bridge 执行会 panic，但 Check 能过
	_, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Nested FFI path matching failed: %v", err)
	}
}

// TestImportScope 非法导入作用域测试
func TestImportScope(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
	package main
	func main() {
		m := require("math") // 模拟非法作用域导入
	}
	`
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("Should fail because import is inside function")
	}
	expectedErr := "import 只能在包级作用域中使用"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("Expected error '%s', got: %v", expectedErr, err)
	}
}
