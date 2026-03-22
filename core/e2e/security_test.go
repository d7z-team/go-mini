package e2e

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestSecurityAndAuditability(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	t.Run("StepLimitEnforcement", func(t *testing.T) {
		code := `
		package main
		func main() {
			i := 0
			for {
				i = i + 1
				if i > 1000 { break }
			}
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("failed to create runtime: %v", err)
		}

		// 设置一个很小的 StepLimit (10步)
		prog.SetStepLimit(10)

		err = prog.Execute(context.Background())
		if err == nil {
			t.Error("expected error due to step limit, but got nil")
		} else if !strings.Contains(err.Error(), "instruction limit exceeded") {
			t.Errorf("expected instruction limit error, got: %v", err)
		} else {
			t.Logf("StepLimit enforced: %v", err)
		}
	})

	t.Run("TypeConfusionPrevention", func(t *testing.T) {
		code := `
		package main
		import "os"
		func main() {
			// 在封闭架构中，os.Open 返回 (TypeHandle, string)
			h, err := os.Open("test.txt")
			// 尝试对 Handle 进行加法运算，应该触发 evalArithmetic 的类型校验
			bad := h + 1 
		}
		`
		prog, err := executor.NewRuntimeByGoCode(code)
		if err != nil {
			t.Logf("Validation correctly blocked: %v", err)
			return
		}
		err = prog.Execute(context.Background())
		if err == nil {
			t.Error("expected runtime error when performing arithmetic on handle, but got nil")
		} else if !strings.Contains(err.Error(), "arithmetic operation") {
			t.Errorf("expected arithmetic type error, got: %v", err)
		} else {
			t.Logf("Type confusion prevented: %v", err)
		}
	})
}
