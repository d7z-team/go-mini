package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestSemanticsV2(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	t.Run("Loop Capture Semantics (Go 1.22+)", func(t *testing.T) {
		code := `
		package main
		func main() {
			fns := make([]any, 3)
			for i := 0; i < 3; i++ {
				fns[i] = func() int64 { return i }
			}
			
			// 在 Go 1.22+ 语义中，这里应该是 0, 1, 2
			// 如果是旧语义，则是 3, 3, 3
			v0 := fns[0]()
			if v0 != 0 { panic("loop capture 0 failed, got " + string(v0)) }
			v1 := fns[1]()
			if v1 != 1 { panic("loop capture 1 failed, got " + string(v1)) }
			v2 := fns[2]()
			if v2 != 2 { panic("loop capture 2 failed, got " + string(v2)) }
		}
		`
		runtime, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		err = runtime.Execute(context.Background())
		if err != nil {
			t.Fatalf("loop capture test failed: %v", err)
		}
	})

	t.Run("Range Loop Capture Semantics", func(t *testing.T) {
		code := `
		package main
		func main() {
			arr := []int64{10, 20, 30}
			fns := make([]any, 3)
			for i, v := range arr {
				fns[i] = func() int64 { return v }
			}
			
			v0 := fns[0]()
			if v0 != 10 { panic("range loop capture 0 failed, got " + string(v0)) }
			v1 := fns[1]()
			if v1 != 20 { panic("range loop capture 1 failed, got " + string(v1)) }
			v2 := fns[2]()
			if v2 != 30 { panic("range loop capture 2 failed, got " + string(v2)) }
		}
		`
		runtime, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		err = runtime.Execute(context.Background())
		if err != nil {
			t.Fatalf("range loop capture test failed: %v", err)
		}
	})

	t.Run("Method Value Extraction", func(t *testing.T) {
		code := `
		package main
		import "os"
		func main() {
			f, err := os.Create("method_val.txt")
			if err != nil { panic(err) }
			
			// 提取方法为变量
			writeFn := f.Write
			
			// 通过变量调用 (Implicitly bound to f)
			writeFn([]byte("Hello Method Value"))
			f.Close()
			
			// 验证
			data, err1 := os.ReadFile("method_val.txt")
			if err1 != nil { panic(err1) }
			if string(data) != "Hello Method Value" {
				panic("method value call failed: " + string(data))
			}
			os.Remove("method_val.txt")
		}
		`
		runtime, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		err = runtime.Execute(context.Background())
		if err != nil {
			t.Fatalf("method value test failed: %v", err)
		}
	})
}
