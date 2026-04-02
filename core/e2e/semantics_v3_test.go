package e2e

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestSemanticsV3(t *testing.T) {
	e := engine.NewMiniExecutor()

	t.Run("StrictMapKeyValidation", func(t *testing.T) {
		code := `package main
		func main() {
			m := make(map[string]int64)
			return m[1] // 静态检查应报错：键类型不匹配
		}`
		_, err := e.NewRuntimeByGoCode(code)
		if err == nil {
			t.Error("Expected validation error for map key mismatch (int key for string map), but got nil")
		}
	})

	t.Run("InvalidCapArgument", func(t *testing.T) {
		code := `package main
		func main() {
			return cap(123) // cap 不支持数值
		}`
		_, err := e.NewRuntimeByGoCode(code)
		if err == nil {
			t.Error("Expected validation error for cap(int), but got nil")
		}
	})

	t.Run("ErrorToStringAssignment", func(t *testing.T) {
		// 这里验证语义层面的兼容性
		// 虽然无法直接在脚本写 Error 类型，但我们可以模拟 FFI 返回 Error 的场景
		e.DeclareFuncSchema("getErr", runtime.MustParseRuntimeFuncSig("function() Error"))

		code := `package main
		func main() string {
			var s string
			s = getErr() // 应该允许，因为我们开启了自动转换
			return s
		}`
		_, err := e.NewRuntimeByGoCode(code)
		if err != nil {
			t.Errorf("Expected Error to String assignment to pass validation, but got: %v", err)
		}
	})
}
