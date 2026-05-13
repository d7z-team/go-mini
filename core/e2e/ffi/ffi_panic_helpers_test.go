package tests

import (
	"context"
	"fmt"
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
