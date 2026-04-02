package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestClosureStress(t *testing.T) {
	executor := engine.NewMiniExecutor()

	tests := []struct {
		name string
		code string
	}{
		{
			name: "Shared Capture Modification",
			code: `
			package main
			func main() {
				x := 100
				inc := func() { x += 1 }
				dec := func() { x -= 1 }
				
				inc()
				inc()
				dec()
				if x != 101 { panic("shared capture failed: " + string(x)) }
			}`,
		},
		{
			name: "Deeply Nested Capture",
			code: `
			package main
			func main() {
				a := 1
				// f1 是匿名函数 (L0)
				f1 := func() func() func() int64 {
					// L1
					return func() func() int64 {
						// L2
						return func() int64 {
							return a
						}
					}
				}
				// f1() -> L1
				// f1()() -> L2
				// f1()()() -> a (1)
				res := f1()()()
				if res != 1 { panic("deep capture failed, got " + string(res)) }
			}`,
		},
		{
			name: "Nested Upvalue Forwarding Mutation",
			code: `
			package main
			func main() {
				x := 1
				mid := func() func() int64 {
					return func() int64 {
						x += 1
						return x
					}
				}
				inc := mid()
				if inc() != 2 { panic("nested upvalue mutation failed") }
				if x != 2 { panic("outer value not updated by nested closure") }
			}`,
		},
		{
			name: "Variable Shadowing in Closure",
			code: `
			package main
			func main() {
				x := 10
				f := func() int64 {
					x := 20 // Shadow outer x
					return x
				}
				if f() != 20 { panic("shadowing in closure failed") }
				if x != 10 { panic("outer variable corrupted by shadow") }
			}`,
		},
		{
			name: "Loop Capture Semantics",
			code: `
			package main
			func main() {
				// 在 go-mini 中，for 循环的 init 变量在整个循环生命周期内共享（类似 Go 1.22 之前）
				// 如果需要每个闭包捕获不同的值，必须在循环体内部重新定义
				fns := make([]any, 3)
				for i := 0; i < 3; i++ {
					val := i // 局部重新定义
					fns[i] = func() int64 { return val }
				}
				
				if fns[0]() != 0 { panic("loop local capture 0 failed") }
				if fns[1]() != 1 { panic("loop local capture 1 failed") }
				if fns[2]() != 2 { panic("loop local capture 2 failed") }
			}`,
		},
		{
			name: "Closure as Argument",
			code: `
			package main
			func exec(f any) int64 {
				return f()
			}
			func main() {
				base := 100
				res := exec(func() int64 {
					return base + 50
				})
				if res != 150 { panic("closure as arg failed") }
			}`,
		},
		{
			name: "Named Recursive Closure",
			code: `
			package main
			func main() {
				var fib any
				fib = func(n int64) int64 {
					if n <= 1 { return n }
					return fib(n-1) + fib(n-2)
				}
				if fib(7) != 13 { panic("recursive closure failed") }
			}`,
		},
		{
			name: "Closure Capturing Map",
			code: `
			package main
			func main() {
				data := make(map[string]any)
				data["val"] = 100
				
				f := func() {
					data["val"] = 200
				}
				f()
				if data["val"] != 200 { panic("map capture modification failed") }
			}`,
		},
		{
			name: "Closure Capturing Closure (Sharing)",
			code: `
			package main
			func main() {
				val := 10
				f1 := func() int64 { return val }
				f2 := func() { val = 20 }
				
				if f1() != 10 { panic("f1 initial failed") }
				f2()
				if f1() != 20 { panic("f1 after f2 failed") }
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime, err := executor.NewRuntimeByGoCode(tt.code)
			if err != nil {
				t.Fatalf("compile failed: %v", err)
			}
			err = runtime.Execute(context.Background())
			if err != nil {
				t.Fatalf("execute failed: %v", err)
			}
		})
	}
}
