package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/surface"
)

// TestModulePrivateScope 验证跨模块调用时，被调模块的私有变量作用域是否丢失
func TestModulePrivateScope(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	if err := executor.UseSurface(surface.Library("service", surface.GoFile("service.mgo", `
			package service
			
			// 私有变量 (未导出)
			var utils = "INTERNAL_SECRET"

			func Process() string {
				// 这里应该能访问到 utils，即便是在 main 中被调用
				return "Result: " + utils
			}
			`))); err != nil {
		t.Fatalf("register service surface: %v", err)
	}

	code := `
	package main
	import "service"

	func main() {
		// main 调用 service.Process
		// 如果 Bug 存在，会报错 undefined: utils
		res := service.Process()
		if res != "Result: INTERNAL_SECRET" {
			panic("Wrong result: " + res)
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
