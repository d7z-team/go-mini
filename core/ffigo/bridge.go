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

// WaitKind tells the VM scheduler whether an async FFI wait can wake without
// executing another VM context.
type WaitKind uint8

const (
	// WaitDependsOnVM marks waits that need another VM context to make progress.
	WaitDependsOnVM WaitKind = iota
	// WaitExternal marks waits backed by host-side timers, I/O, or goroutines.
	WaitExternal
)

// WaitSnapshot is a cheap, non-blocking view of an async FFI wait.
type WaitSnapshot struct {
	Kind   WaitKind
	Reason string
}

// WaitHandle describes and cancels a pending async FFI wait.
type WaitHandle interface {
	Snapshot() WaitSnapshot
	Cancel()
}

type waitHandle struct {
	kind   WaitKind
	reason string
	cancel func()
}

// NewWaitHandle creates a static wait handle for an async FFI call.
func NewWaitHandle(kind WaitKind, reason string, cancel func()) WaitHandle {
	return waitHandle{kind: kind, reason: reason, cancel: cancel}
}

func (h waitHandle) Snapshot() WaitSnapshot {
	return WaitSnapshot{Kind: h.kind, Reason: h.reason}
}

func (h waitHandle) Cancel() {
	if h.cancel != nil {
		h.cancel()
	}
}

type Async[T any] interface {
	Start(ctx context.Context, done Completion[T]) (WaitHandle, error)
}

type AsyncFunc[T any] func(context.Context, Completion[T]) (WaitHandle, error)

func (f AsyncFunc[T]) Start(ctx context.Context, done Completion[T]) (WaitHandle, error) {
	return f(ctx, done)
}

type WireCompletion interface {
	CompleteWire(ret []byte, err error) bool
}

type AsyncCall interface {
	isAsyncCall()
	StartWire(ctx context.Context, done WireCompletion) (WaitHandle, error)
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

func (a asyncValue[T]) StartWire(ctx context.Context, done WireCompletion) (WaitHandle, error) {
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
	Channels ChannelRegistry
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

type BridgeIdentity interface {
	BridgeID() string
}

type RouterFunc func(context.Context, *FFICallRequest) (FFIReturn, error)

type RouterBridge struct {
	Registry *HandleRegistry
	Router   RouterFunc
}

func NewRouterBridge(registry *HandleRegistry, router RouterFunc) *RouterBridge {
	return &RouterBridge{Registry: registry, Router: router}
}

func (b *RouterBridge) BridgeID() string {
	return "ffigo.RouterBridge"
}

func (b *RouterBridge) Call(ctx context.Context, req *FFICallRequest) (FFIReturn, error) {
	if req == nil {
		return nil, errors.New("ffigo: missing FFI request")
	}
	if b == nil || b.Router == nil {
		return nil, errors.New("ffigo: missing FFI router")
	}
	if req.Channels != nil {
		ctx = ContextWithChannelRegistry(ctx, req.Channels)
	}
	return b.Router(ctx, req)
}

func (b *RouterBridge) Invoke(ctx context.Context, req *FFICallRequest) (FFIReturn, error) {
	return b.Call(ctx, req)
}

func (b *RouterBridge) DestroyHandle(handle uint32) error {
	if b != nil && b.Registry != nil {
		b.Registry.Remove(handle)
	}
	return nil
}
