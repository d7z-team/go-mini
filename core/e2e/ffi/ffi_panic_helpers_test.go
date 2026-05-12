package tests

import (
	"context"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type panicInterceptBridge struct{}

func (b *panicInterceptBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	switch methodID {
	case 1:
		panic("bridge-call-boom")
	default:
		return nil, fmt.Errorf("unexpected method id %d", methodID)
	}
}

func (b *panicInterceptBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	switch method {
	case "InvokeBoom", "sandbox.InvokeBoom":
		panic("bridge-invoke-boom")
	default:
		return nil, fmt.Errorf("unexpected method %s", method)
	}
}

func (b *panicInterceptBridge) DestroyHandle(handle uint32) error {
	return nil
}

// querySelectorNullBridge 模拟浏览器 page.Eval() 执行 document.querySelector('.down')
// 的行为——当 .down 元素不存在时，querySelector 返回 null，后续 .click() 抛出 TypeError。
// Mini 脚本中 page.Eval(`document.querySelector('.down').click()`) 会导致此 FFI panic。
type querySelectorNullBridge struct{}

func (b *querySelectorNullBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return nil, fmt.Errorf("unexpected call method %d", methodID)
}

func (b *querySelectorNullBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	panic("TypeError: Cannot read properties of null (reading 'click')")
}

func (b *querySelectorNullBridge) DestroyHandle(handle uint32) error {
	return nil
}

// mockContinueBridge 模拟 1.mgo 中的 continue 测试场景。
// MethodID 1: ShouldContinue — 当 item >= 3 时返回 true，触发 continue
// MethodID 2: DoFFICall — 无操作，模拟 continue 后应跳过的 FFI 调用
type mockContinueBridge struct{}

func (b *mockContinueBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	reader := ffigo.NewReader(args)
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)

	switch methodID {
	case 1: // ShouldContinue
		val := reader.ReadVarint()
		buf.WriteBool(val >= 3)
		return buf.Bytes(), nil
	case 2: // DoFFICall — should NOT be reached when continue works
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected method id %d", methodID)
	}
}

func (b *mockContinueBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return nil, fmt.Errorf("unexpected invoke: %s", method)
}

func (b *mockContinueBridge) DestroyHandle(handle uint32) error {
	return nil
}
