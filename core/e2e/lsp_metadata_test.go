package e2e

import (
	"encoding/json"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestExportMetadata(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `package main
const VERSION = "1.0.0"
type Point struct { X, Y int }
func Add(a, b int) int { return a + b }
func (p Point) Distance() int { return 0 }
func main() {}`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	// 注册为模块，这样 ExportMetadata 才能看到
	executor.RegisterModule("main", prog)

	jsonStr := executor.ExportMetadata()
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &meta); err != nil {
		t.Fatal(err)
	}

	// 验证内置函数
	builtins := meta["builtins"].(map[string]interface{})
	if _, ok := builtins["len"]; !ok {
		t.Error("len not found in builtins")
	}

	// 验证 FFI 模块
	modules := meta["modules"].(map[string]interface{})
	if _, ok := modules["fmt"]; !ok {
		t.Error("fmt module not found")
	}
	fmtMod := modules["fmt"].(map[string]interface{})
	fmtFuncs := fmtMod["functions"].(map[string]interface{})
	if _, ok := fmtFuncs["Printf"]; !ok {
		t.Error("fmt.Printf not found")
	}

	// 验证脚本模块 (注册在 main)
	if _, ok := modules["main"]; !ok {
		t.Error("main module not found")
	}
	mainMod := modules["main"].(map[string]interface{})
	mainFuncs := mainMod["functions"].(map[string]interface{})
	if sig, ok := mainFuncs["Add"]; ok {
		t.Logf("Add signature: %v", sig)
		// 检查是否包含完整签名 (参数和返回类型)
		sigStr := sig.(string)
		if !strings.Contains(sigStr, "Int64") {
			t.Errorf("Signature should contain types, got %s", sigStr)
		}
	} else {
		t.Error("Add function not found in main module")
	}

	// 验证常量
	mainConstants := mainMod["constants"].(map[string]interface{})
	if val, ok := mainConstants["VERSION"]; ok {
		if val != "1.0.0" {
			t.Errorf("Expected VERSION 1.0.0, got %v", val)
		}
	} else {
		t.Error("VERSION constant not found")
	}
}
