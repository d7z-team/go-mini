package ffigo

import (
	"context"
	"errors"
	"fmt"
)

type Void struct{}

type Tuple2[A, B any] struct {
	V0 A
	V1 B
}

type Completion[T any] interface {
	Complete(value T, err error) bool
}

type Async[T any] interface {
	Start(ctx context.Context, done Completion[T]) (cancel func(), err error)
}

type AsyncFunc[T any] func(context.Context, Completion[T]) (func(), error)

func (f AsyncFunc[T]) Start(ctx context.Context, done Completion[T]) (func(), error) {
	return f(ctx, done)
}

type WireCompletion interface {
	CompleteWire(ret []byte, err error) bool
}

type AsyncCall interface {
	isAsyncCall()
	StartWire(ctx context.Context, done WireCompletion) (cancel func(), err error)
}

type Encoder[T any] func(*Buffer, T) error

type asyncValue[T any] struct {
	async  Async[T]
	encode Encoder[T]
}

func AsyncValue[T any](async Async[T], encode Encoder[T]) AsyncCall {
	return asyncValue[T]{async: async, encode: encode}
}

func (a asyncValue[T]) isAsyncCall() {}

func (a asyncValue[T]) StartWire(ctx context.Context, done WireCompletion) (func(), error) {
	if a.async == nil {
		return nil, errors.New("missing async FFI value")
	}
	return a.async.Start(ctx, typedCompletion[T]{done: done, encode: a.encode})
}

type typedCompletion[T any] struct {
	done   WireCompletion
	encode Encoder[T]
}

func (c typedCompletion[T]) Complete(value T, err error) bool {
	if c.done == nil {
		return false
	}
	if err != nil {
		return c.done.CompleteWire(nil, err)
	}
	if c.encode == nil {
		return c.done.CompleteWire(nil, nil)
	}
	buf := GetBuffer()
	defer ReleaseBuffer(buf)
	if encodeErr := c.encode(buf, value); encodeErr != nil {
		return c.done.CompleteWire(nil, encodeErr)
	}
	return c.done.CompleteWire(append([]byte(nil), buf.Bytes()...), nil)
}

type FFIReturn = any

type FFICallRequest struct {
	MethodID uint32
	Method   string
	Args     []byte
}

func SyncBytes(ret FFIReturn) ([]byte, error) {
	switch v := ret.(type) {
	case nil:
		return nil, nil
	case []byte:
		return v, nil
	case AsyncCall:
		return nil, errors.New("ffi proxy received async result")
	default:
		return nil, fmt.Errorf("unsupported ffi return %T", ret)
	}
}

// FFIBridge 是 VM 和 Host 通信的唯一物理通道
type FFIBridge interface {
	Call(ctx context.Context, req *FFICallRequest) (ret FFIReturn, err error)
	Invoke(ctx context.Context, req *FFICallRequest) (ret FFIReturn, err error)
	// DestroyHandle 释放由 Host 创建的句柄
	DestroyHandle(handle uint32) error
}
