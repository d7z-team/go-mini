package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestFFILibV2(t *testing.T) {
	t.Setenv("GO_MINI_TEST", "rocks")

	code := `
		package main
		import "os"
		import "fmt"
		import "time"
		import "io"

		func main() {
			// 1. 测试 OS 增强
			if os.Getenv("GO_MINI_TEST") != "rocks" { panic("os.Getenv failed") }

			// 2. 测试 f.Write 和 io.ReadAll 的联动
			f, err = os.Create("v2_test.txt")

			// 向文件句柄写入
			f.Write([]byte("Hello Mini 2026"))
			f.Close()
			// 3. 测试 iolib.ReadAll (模拟读取刚才的文件)
			f2, err = os.Open("v2_test.txt")
			
			resData, err = io.ReadAll(f2)
			if string(resData) != "Hello Mini 2026" {
				panic("io.ReadAll content mismatch: " + string(resData))
			}
			f2.Close()

			// 4. 测试 time 增强
			now := time.Now()
			u := now.Unix()
			if u <= 0 { panic("time.Unix invalid") }
			
			elapsed := time.Since(now)
			if elapsed < 0 { panic("time.Since invalid") }

			os.Remove("v2_test.txt")
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
}
