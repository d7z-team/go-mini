package e2e

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/e2e/storagelib"
)

func TestFFIStorageOverflow(t *testing.T) {
	impl := &storagelib.StorageImpl{}
	executor := engine.NewMiniExecutor()

	// 1. 手动注册 FFI 路由，避免点号带来的模块加载问题
	// 直接模拟 RegisterStorageAPI 的核心逻辑，但改名以简化测试
	bridge := &storagelib.StorageAPI_Bridge{Impl: impl, Registry: nil}
	executor.RegisterFFI("StorageSetCapacity", bridge, storagelib.MethodID_StorageAPI_SetCapacity, "function(Int64) Void", "")
	executor.RegisterFFI("StorageGetStatus", bridge, storagelib.MethodID_StorageAPI_GetStatus, "function() Int64", "")

	tests := []struct {
		name      string
		code      string
		expectErr string
		checkCap  uint32
	}{
		{
			"Uint32 Normal",
			`package main; func main() { StorageSetCapacity(5000) }`,
			"",
			5000,
		},
		{
			"Uint32 Overflow",
			`package main; func main() { StorageSetCapacity(-1) }`,
			"ffi: uint32 overflow: -1",
			0,
		},
		{
			"GetStatus Normal",
			`package main; func main() { if StorageGetStatus() != 1024 { panic("wrong status") } }`,
			"",
			0, // 不检查 Capacity，只通过上面的 panic 验证返回值
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			impl.Capacity = 0 // 重置
			vm, err := executor.NewRuntimeByGoCode(tt.code)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			_, err = vm.Eval(context.Background(), "main()", nil)
			if tt.expectErr == "" {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Expected error containing %q, but got none", tt.expectErr)
				} else if !strings.Contains(err.Error(), tt.expectErr) {
					t.Errorf("Expected error containing %q, but got %v", tt.expectErr, err)
				}
			}

			if tt.expectErr == "" && tt.checkCap != 0 && impl.Capacity != tt.checkCap {
				t.Errorf("Expected capacity %d, got %d", tt.checkCap, impl.Capacity)
			}
		})
	}
}
