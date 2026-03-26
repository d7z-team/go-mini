package e2e

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// MockResource 模拟一个宿主侧的句柄资源
type MockResource struct {
	ID uint32
}

func (m *MockResource) GetID() int64 {
	return int64(m.ID)
}

// lifecycleMockBridge 实现 ffigo.FFIBridge 接口用于生命周期测试
type lifecycleMockBridge struct {
	registry *ffigo.HandleRegistry
	t        *testing.T
}

func (m *lifecycleMockBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	switch methodID {
	case 1: // 模拟 Screenshot
		res := &MockResource{ID: 12}
		id := m.registry.Register(res)

		buf := ffigo.GetBuffer()
		defer ffigo.ReleaseBuffer(buf)
		buf.WriteUvarint(uint64(id))
		return buf.Bytes(), nil

	case 2: // 模拟 GetWidth
		reader := ffigo.NewReader(args)
		id := uint32(reader.ReadUvarint())

		obj, ok := m.registry.Get(id)
		if !ok {
			return nil, ffigo.ErrorData{Message: fmt.Sprintf("invalid handle ID: %d", id)}
		}
		res := obj.(*MockResource)
		buf := ffigo.GetBuffer()
		defer ffigo.ReleaseBuffer(buf)
		buf.WriteVarint(res.GetID())
		return buf.Bytes(), nil

	case 3: // 模拟 GC 压力
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
		runtime.GC()
		return nil, nil
	}
	return nil, nil
}

func (m *lifecycleMockBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return nil, nil
}

func (m *lifecycleMockBridge) DestroyHandle(id uint32) error {
	m.registry.Remove(id)
	return nil
}

// TestHandleGCLifecycleRegression 验证句柄在 GC 压力下的生命周期，防止 "invalid handle ID" 回归
func TestHandleGCLifecycleRegression(t *testing.T) {
	executor := engine.NewMiniExecutor()
	registry := ffigo.NewHandleRegistry()
	bridge := &lifecycleMockBridge{registry: registry, t: t}

	executor.RegisterFFI("Screenshot", bridge, 1, "function() TypeHandle", "")
	executor.RegisterFFI("GetWidth", bridge, 2, "function(TypeHandle) Int64", "")
	executor.RegisterFFI("TriggerGC", bridge, 3, "function()", "")

	code := `
		package main
		func main() {
			img := Screenshot()
			imgCopy := img
			TriggerGC() 
			w := GetWidth(imgCopy)
			if w != 12 { panic("wrong width") }
		}
	`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 50; i++ {
		err = prog.Execute(context.Background())
		if err != nil {
			t.Fatalf("Iteration %d failed: %v", i, err)
		}
	}
}
