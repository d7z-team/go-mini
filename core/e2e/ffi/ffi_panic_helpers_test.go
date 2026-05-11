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
