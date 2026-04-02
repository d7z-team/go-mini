package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// SimpleBridge implements ffigo.FFIBridge for testing
type SimpleBridge struct {
	Callback func(methodID uint32, args []byte) ([]byte, error)
}

func (b *SimpleBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return b.Callback(methodID, args)
}

func (b *SimpleBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return nil, nil
}

func (b *SimpleBridge) DestroyHandle(handle uint32) error {
	return nil
}

func TestSecurityV3(t *testing.T) {
	e := engine.NewMiniExecutor()

	t.Run("FFI_Overflow_Check", func(t *testing.T) {
		bridge := &SimpleBridge{
			Callback: func(methodID uint32, args []byte) ([]byte, error) {
				reader := ffigo.NewReader(args)
				// Simulate ffigen generated code for int8
				tmp := reader.ReadVarint()
				if tmp < -128 || tmp > 127 {
					panic(fmt.Sprintf("ffi: int8 overflow: %d", tmp))
				}
				return nil, nil
			},
		}
		e.RegisterFFISchema("test.int8", bridge, 1001, runtime.MustParseRuntimeFuncSig("function(Int64) Void"), "Test int8 overflow")

		// 正常调用 (需要 import "test")
		code := `package main
		import "test"
		func main() {
			test.int8(100)
		}`
		prog, err := e.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		if err := prog.Execute(context.Background()); err != nil {
			t.Fatalf("Normal call failed: %v", err)
		}

		// 溢出调用
		codeOverflow := `package main
		import "test"
		func main() {
			test.int8(300) // 300 超过 int8(127)
		}`
		progOverflow, err := e.NewRuntimeByGoCode(codeOverflow)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		err = progOverflow.Execute(context.Background())
		if err == nil || !strings.Contains(err.Error(), "ffi: int8 overflow") {
			t.Errorf("Expected int8 overflow error, but got: %v", err)
		}
	})

	t.Run("Deep_Copy_Isolation", func(t *testing.T) {
		original := []byte("hello")
		bridge := &SimpleBridge{
			Callback: func(methodID uint32, args []byte) ([]byte, error) {
				buf := ffigo.GetBuffer()
				defer ffigo.ReleaseBuffer(buf)
				buf.WriteBytes(original)
				return buf.Bytes(), nil
			},
		}
		e.RegisterFFISchema("test.getBytes", bridge, 1002, runtime.MustParseRuntimeFuncSig("function() TypeBytes"), "Test deep copy")

		code := `package main
		import "test"
		func main() {
			b := test.getBytes()
			b[0] = 88 // 'X'
		}`
		prog, err := e.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		prog.Execute(context.Background())

		if original[0] == 'X' {
			t.Error("FFI return value should be deep copied, but original buffer was modified")
		}
	})

	t.Run("Map_Key_Dynamic_Validation", func(t *testing.T) {
		code := `package main
		func identity(v any) any { return v }
		func main() {
			m := make(map[string]int64)
			k := identity(123)
			_ = m[k] // 编译期应通过 (any 可赋值给 string)，运行时应报错
		}`
		prog, err := e.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		err = prog.Execute(context.Background())
		if err == nil || !strings.Contains(err.Error(), "invalid map key type") {
			t.Errorf("Expected runtime map key type error, but got: %v", err)
		}
	})

	t.Run("Recursion_Depth_Limit", func(t *testing.T) {
		e.SetMaxTypeDepth(5) // 设置一个很小的深度

		// 构造一个深层嵌套类型
		deepType := "int64"
		for i := 0; i < 10; i++ {
			deepType = "*" + deepType
		}

		code := fmt.Sprintf(`package main
		func main() {
			var x %s
			var y int64 = x // 触发递归解引用，深度 10 > 5
		}`, deepType)

		_, err := e.NewRuntimeByGoCode(code)
		if err == nil {
			t.Error("Expected recursion depth error during semantic check, but got nil")
		}
	})

	t.Run("Configurable_Recursion_Depth", func(t *testing.T) {
		e.SetMaxTypeDepth(100)

		deepType := "int64"
		for i := 0; i < 40; i++ {
			deepType = "*" + deepType
		}

		code := fmt.Sprintf(`package main
		func main() {
			var x %s
			var y int64 = x
		}`, deepType)

		_, err := e.NewRuntimeByGoCode(code)
		if err != nil {
			t.Errorf("Expected depth 40 to pass with MaxTypeDepth 100, but got: %v", err)
		}
	})
}
