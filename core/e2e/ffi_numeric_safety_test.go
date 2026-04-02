package e2e

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// NumericSafetyAPI 定义测试接口
type NumericSafetyAPI interface {
	AcceptInt8(v int8)
	AcceptUint16(v uint16)
	AcceptInt32(v int32)
}

// NumericSafetyImpl 实现
type NumericSafetyImpl struct {
	LastVal int64
}

func (n *NumericSafetyImpl) AcceptInt8(v int8)     { n.LastVal = int64(v) }
func (n *NumericSafetyImpl) AcceptUint16(v uint16) { n.LastVal = int64(v) }
func (n *NumericSafetyImpl) AcceptInt32(v int32)   { n.LastVal = int64(v) }

// NumericSafetyBridge 模拟 ffigen 生成的桥接器，包含溢出检查
type NumericSafetyBridge struct {
	impl *NumericSafetyImpl
}

func (b *NumericSafetyBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	reader := ffigo.NewReader(args)
	// fmt.Printf("Bridge.Call methodID=%d\n", methodID)
	switch methodID {
	case 1: // AcceptInt8(int8)
		var v int8
		{
			tmp := reader.ReadVarint()
			if tmp < -128 || tmp > 127 {
				panic(fmt.Sprintf("ffi: int8 overflow: %d", tmp))
			}
			v = int8(tmp)
		}
		b.impl.AcceptInt8(v)
		return nil, nil
	case 2: // AcceptUint16(uint16)
		var v uint16
		{
			tmp := reader.ReadVarint()
			if tmp < 0 || tmp > 65535 {
				panic(fmt.Sprintf("ffi: uint16 overflow: %d", tmp))
			}
			v = uint16(tmp)
		}
		b.impl.AcceptUint16(v)
		return nil, nil
	case 3: // AcceptInt32(int32)
		var v int32
		{
			tmp := reader.ReadVarint()
			if tmp < -2147483648 || tmp > 2147483647 {
				panic(fmt.Sprintf("ffi: int32 overflow: %d", tmp))
			}
			v = int32(tmp)
		}
		b.impl.AcceptInt32(v)
		return nil, nil
	}
	return nil, errors.New("unknown method")
}

func (b *NumericSafetyBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return nil, nil
}
func (b *NumericSafetyBridge) DestroyHandle(id uint32) error { return nil }

func TestFFINumericSafety(t *testing.T) {
	impl := &NumericSafetyImpl{}
	bridge := &NumericSafetyBridge{impl: impl}
	executor := engine.NewMiniExecutor()

	// 注册全局 FFI 函数 (不带 api. 前缀，省去 import 麻烦)
	executor.RegisterFFISchema("AcceptInt8", bridge, 1, runtime.MustParseRuntimeFuncSig("function(Int64) Void"), "")
	executor.RegisterFFISchema("AcceptUint16", bridge, 2, runtime.MustParseRuntimeFuncSig("function(Int64) Void"), "")
	executor.RegisterFFISchema("AcceptInt32", bridge, 3, runtime.MustParseRuntimeFuncSig("function(Int64) Void"), "")

	tests := []struct {
		name      string
		code      string
		expectErr string
		checkVal  int64
	}{
		{
			"Int8 Normal",
			`package main; func main() { AcceptInt8(100) }`,
			"",
			100,
		},
		{
			"Int8 Overflow Positive",
			`package main; func main() { AcceptInt8(200) }`,
			"ffi: int8 overflow: 200",
			0,
		},
		{
			"Int8 Overflow Negative",
			`package main; func main() { AcceptInt8(-129) }`,
			"ffi: int8 overflow: -129",
			0,
		},
		{
			"Uint16 Normal",
			`package main; func main() { AcceptUint16(60000) }`,
			"",
			60000,
		},
		{
			"Uint16 Overflow Negative",
			`package main; func main() { AcceptUint16(-1) }`,
			"ffi: uint16 overflow: -1",
			0,
		},
		{
			"Int32 Normal",
			`package main; func main() { AcceptInt32(1000000) }`,
			"",
			1000000,
		},
		{
			"Int32 Overflow",
			`package main; func main() { AcceptInt32(3000000000) }`,
			"ffi: int32 overflow: 3000000000",
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			impl.LastVal = 0 // 重置
			vm, err := executor.NewRuntimeByGoCode(tt.code)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			res, err := vm.Eval(context.Background(), "main()", nil)
			if tt.expectErr == "" {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if res != nil && res.VType == runtime.TypeError {
					t.Errorf("Unexpected TypeError: %v", res.Ref)
				}
			} else {
				if err == nil {
					t.Errorf("Expected error containing %q, but got none", tt.expectErr)
				} else if !strings.Contains(err.Error(), tt.expectErr) {
					t.Errorf("Expected error containing %q, but got %v", tt.expectErr, err)
				}
			}

			if tt.expectErr == "" && impl.LastVal != tt.checkVal {
				t.Errorf("Expected value %d, got %d", tt.checkVal, impl.LastVal)
			}
		})
	}
}
