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

// TestModuleComprehensive 综合测试：函数、变量、常量、结构体导出
func TestModuleComprehensive(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	if err := executor.UseSurface(surface.Library("lib", surface.GoFile("lib.mgo", `
			package lib
			
			const PI = 314
			var Version = "1.0.0"

			type Point struct {
				X int
				Y int
			}

			func NewPoint(x int, y int) Point {
				return Point{X: x, Y: y}
			}
			`))); err != nil {
		t.Fatalf("register lib surface: %v", err)
	}

	code := `
	package main
	import "lib"

	func main() {
		// 1. 常量导出测试
		if lib.PI != 314 {
			panic("const export failed")
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
	executor := engine.MustNewMiniExecutor()

	err := executor.UseSurface(surface.Libraries(
		surface.LibraryModule{Path: "a", Files: []surface.LibraryFile{surface.GoFile("a.mgo", "package a; import \"b\"; func Run() {}")}},
		surface.LibraryModule{Path: "b", Files: []surface.LibraryFile{surface.GoFile("b.mgo", "package b; import \"a\"; func Run() {}")}},
	))
	if err == nil {
		t.Fatal("Should fail due to circular dependency")
	}
	expectedErr := "circular import dependency"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("Expected error containing '%s', got: %v", expectedErr, err)
	}
}

// TestNestedFFIPath 嵌套 FFI 路径匹配测试
func TestNestedFFIPath(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	testsurface.UseRoute(t, executor, "net/http.Get", nil, 1, runtime.MustParseRuntimeFuncSig("function(String) String"), "")

	code := `
	package main
	import "net/http"

	func main() {
		// 导入路径和 FFI route 都保持 canonical slash path。
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
	executor := engine.MustNewMiniExecutor()

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
	expectedErr := "import is only allowed at package scope"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("Expected error '%s', got: %v", expectedErr, err)
	}
}
