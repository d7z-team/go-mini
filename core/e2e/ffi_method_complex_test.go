package e2e

import (
	"context"
	"os"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestFFIMethodComplex(t *testing.T) {
	code := `
		package main
		import "os"
		import "fmt"

		func main() {
			// 1. 创建两个文件
			f1, err := os.Create("complex_1.txt")
			if err != nil { panic(err) }

			f2, err1 := os.Create("complex_2.txt")
			if err1 != nil { panic(err1) }

			// 2. 交叉写入
			f1.Write([]byte("file 1 content"))
			f2.Write([]byte("file 2 content"))

			// 3. 关闭
			f1.Close()
			f2.Close()

			// 4. 验证内容
			data1, err2 := os.ReadFile("complex_1.txt")
			if err2 != nil { panic(err2) }
			data2, err3 := os.ReadFile("complex_2.txt")
			if err3 != nil { panic(err3) }
			
			if string(data1) != "file 1 content" {
				panic("f1 content mismatch")
			}
			if string(data2) != "file 2 content" {
				panic("f2 content mismatch")
			}

			// 5. 清理
			os.Remove("complex_1.txt")
			os.Remove("complex_2.txt")
		}
	`

	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	runtime, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	err = runtime.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 最终清理验证
	for _, name := range []string{"complex_1.txt", "complex_2.txt"} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("file %s should have been removed", name)
			_ = os.Remove(name)
		}
	}
}

func TestMethodEdgeCases(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	t.Run("CallMethodOnNil", func(t *testing.T) {
		code := `
		package main
		import "os"
		func main() {
			var f Ptr<File> // nil handle
			f.Close()
		}
		`
		runtime, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			// 如果验证器拦截了对 nil 的调用，这也是正确的行为
			return
		}
		err = runtime.Execute(context.Background())
		if err == nil {
			t.Error("expected error when calling method on nil handle, but got nil")
		}
	})

	t.Run("HandleLeakPrevention", func(t *testing.T) {
		// 验证执行器结束时句柄被清理（通过最终析构逻辑隐式测试）
		code := `
		package main
		import "os"
		func main() {
			for i := 0; i < 10; i++ {
				os.Create("leak_test.txt") // 产生未关闭的句柄
				os.Remove("leak_test.txt")
			}
		}
		`
		runtime, _ := executor.NewRuntimeByGoCode(code)
		err := runtime.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	})
}
