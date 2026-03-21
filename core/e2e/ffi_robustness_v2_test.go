package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type ComplexNested struct {
	Data map[string][]int64
}

type ComplexBridge struct {
	t *testing.T
}

func (b *ComplexBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	reader := ffigo.NewReader(args)
	// 期望接收到零值或复杂嵌套数据
	switch methodID {
	case 1: // TestZeroValues(int, string, bool, ptr)
		i := reader.ReadInt64()
		s := reader.ReadString()
		bl := reader.ReadBool()
		ptr := reader.ReadUint32()
		if i != 0 || s != "" || bl != false || ptr != 0 {
			b.t.Errorf("Expected zero values, got: %d, %q, %v, %d", i, s, bl, ptr)
		}
	case 2: // TestNested(ComplexNested)
		// 解析嵌套 Map
		count := reader.ReadUint32()
		if count != 1 {
			b.t.Errorf("Expected map count 1, got %d", count)
		}
		k := reader.ReadString()
		if k != "key" {
			b.t.Errorf("Expected key 'key', got %q", k)
		}
		arrLen := reader.ReadUint32()
		if arrLen != 2 {
			b.t.Errorf("Expected array len 2, got %d", arrLen)
		}
	}
	return nil, nil
}

func (b *ComplexBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return nil, nil
}

func (b *ComplexBridge) DestroyHandle(handle uint32) error { return nil }

func TestFFISerializationEdgeCases(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &ComplexBridge{t: t}

	executor.RegisterFFI("test.Zero", bridge, 1, "function(Int64, String, Bool, Ptr<Any>) Void", "")
	executor.RegisterFFI("test.Nested", bridge, 2, "function(Map<String, Array<Int64>>) Void", "")

	code := `
package main
import "test"

func main() {
	// 1. 测试零值传递
	var i int
	var s string
	var b bool
	// nil 指针在脚本中直接用全局 nil
	test.Zero(i, s, b, nil)

	// 2. 测试复杂嵌套
	m := make(map[string][]int)
	m["key"] = []int{1, 2}
	test.Nested(m)
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
