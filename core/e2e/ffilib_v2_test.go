package e2e

import (
	"context"
	"os"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestFFILibV2(t *testing.T) {
	code := `
		package main
		import "os"
		import "fmt"
		import "time"
		import "io"

		func main() {
			// 1. 测试 OS 增强
			os.Setenv("GO_MINI_TEST", "rocks")
			if os.Getenv("GO_MINI_TEST") != "rocks" { panic("os.Getenv failed") }

			// 2. 测试 fmt.Fprintf 和 io.ReadAll 的联动
			resC = os.Create("v2_test.txt")
			f = resC.val
			
			// 向文件句柄写入
			fmt.Fprintf(f, "Hello %s %d", "Mini", 2026)
			f.Close()

			// 3. 测试 iolib.ReadAll (模拟读取刚才的文件)
			resO = os.Open("v2_test.txt")
			f2 = resO.val
			
			resData = io.ReadAll(f2)
			if string(resData.val) != "Hello Mini 2026" {
				panic("io.ReadAll content mismatch: " + string(resData.val))
			}
			f2.Close()

			// 4. 测试 time 增强
			now := time.Unix()
			if now <= 0 { panic("time.Unix invalid") }
			
			elapsed := time.Since(now * 1000000000) // 传入纳秒
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

	if os.Getenv("GO_MINI_TEST") != "rocks" {
		t.Error("env should have been set in host")
	}
	_ = os.Unsetenv("GO_MINI_TEST")
}
