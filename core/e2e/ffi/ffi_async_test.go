package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type asyncBridge struct{}

func (asyncBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	switch req.MethodID {
	case 1:
		return ffigo.AsyncValue[int64](
			ffigo.AsyncFunc[int64](func(_ context.Context, done ffigo.Completion[int64]) (func(), error) {
				timer := time.AfterFunc(time.Millisecond, func() {
					done.Complete(42, nil)
				})
				return func() { timer.Stop() }, nil
			}),
			func(buf *ffigo.Buffer, value int64) error {
				buf.WriteVarint(value)
				return nil
			},
		), nil
	case 2:
		args := append([]byte(nil), req.Args...)
		return ffigo.AsyncValue[ffigo.Void](
			ffigo.AsyncFunc[ffigo.Void](func(_ context.Context, done ffigo.Completion[ffigo.Void]) (func(), error) {
				timer := time.AfterFunc(time.Millisecond, func() {
					done.Complete(ffigo.Void{}, nil)
				})
				return func() { timer.Stop() }, nil
			}),
			func(buf *ffigo.Buffer, _ ffigo.Void) error {
				reader := ffigo.NewReader(args)
				mutated := append([]byte(strings.ToUpper(string(reader.ReadBytes()))), '!')
				buf.WriteUvarint(1)
				buf.WriteBytes(mutated)
				return nil
			},
		), nil
	default:
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
}

func (asyncBridge) Invoke(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, fmt.Errorf("unexpected invoke %s", req.Method)
}

func (asyncBridge) DestroyHandle(uint32) error {
	return nil
}

func TestAsyncFFIResumesWithReturnAndCopyBack(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := asyncBridge{}
	executor.RegisterFFISchema("async.Value", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Int64"), "")
	executor.RegisterFFISchema(
		"async.Mutate",
		bridge,
		2,
		runtime.MustParseRuntimeFuncSigWithModes("function(TypeBytes) Void", runtime.FFIParamInOutBytes),
		"",
	)

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "async"

func main() {
	v := async.Value()
	if v != 42 {
		panic("unexpected async value")
	}
	buf := []byte("go")
	async.Mutate(buf)
	if String(buf) != "GO!" {
		panic("async copy-back failed")
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type burstAsyncBridge struct {
	target      int64
	release     chan struct{}
	releaseOnce sync.Once
	started     atomic.Int64
	attempted   atomic.Int64
	cancelled   atomic.Int64
}

func newBurstAsyncBridge(target int64) *burstAsyncBridge {
	return &burstAsyncBridge{
		target:  target,
		release: make(chan struct{}),
	}
}

func (b *burstAsyncBridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	switch req.MethodID {
	case 1:
		return ffigo.AsyncValue[ffigo.Void](
			ffigo.AsyncFunc[ffigo.Void](func(ctx context.Context, done ffigo.Completion[ffigo.Void]) (func(), error) {
				cancelled := make(chan struct{})
				var cancelOnce sync.Once
				b.started.Add(1)
				go func() {
					select {
					case <-b.release:
						done.Complete(ffigo.Void{}, nil)
						b.attempted.Add(1)
					case <-ctx.Done():
						done.Complete(ffigo.Void{}, ctx.Err())
						b.attempted.Add(1)
					case <-cancelled:
					}
				}()
				return func() {
					cancelOnce.Do(func() {
						b.cancelled.Add(1)
						close(cancelled)
					})
				}, nil
			}),
			func(*ffigo.Buffer, ffigo.Void) error { return nil },
		), nil
	case 2:
		buf := ffigo.GetBuffer()
		defer ffigo.ReleaseBuffer(buf)
		buf.WriteVarint(b.started.Load())
		return append([]byte(nil), buf.Bytes()...), nil
	case 3:
		b.releaseOnce.Do(func() { close(b.release) })
		deadline := time.After(2 * time.Second)
		for b.attempted.Load() < b.target {
			select {
			case <-deadline:
				return nil, fmt.Errorf("timed out waiting for async completion attempts: got %d want %d", b.attempted.Load(), b.target)
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				time.Sleep(time.Millisecond)
			}
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
}

func (b *burstAsyncBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, errors.New("unexpected invoke")
}

func (b *burstAsyncBridge) DestroyHandle(uint32) error {
	return nil
}

func TestAsyncFFICompletionBurstDoesNotDropTokens(t *testing.T) {
	const workers = 1200

	executor := engine.NewMiniExecutor()
	bridge := newBurstAsyncBridge(workers)
	executor.RegisterFFISchema("gate.Wait", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")
	executor.RegisterFFISchema("gate.Started", bridge, 2, runtime.MustParseRuntimeFuncSig("function() Int64"), "")
	executor.RegisterFFISchema("gate.Release", bridge, 3, runtime.MustParseRuntimeFuncSig("function() Void"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "gate"
import "time"

var done = 0

func worker() {
	gate.Wait()
	done = done + 1
}

func main() {
	for i := 0; i < 1200; i++ {
		go worker()
	}
	for gate.Started() < 1200 {
		time.Sleep(1000000)
	}
	gate.Release()
	time.Sleep(50000000)
	if done != 1200 {
		panic("lost async completion")
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := bridge.cancelled.Load(); got != 0 {
		t.Fatalf("burst completions should not be cancelled, got %d cancellations", got)
	}
}

type blockingAsyncBridge struct {
	cancelled atomic.Int64
}

func (b *blockingAsyncBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req.MethodID != 1 {
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
	return ffigo.AsyncValue[ffigo.Void](
		ffigo.AsyncFunc[ffigo.Void](func(context.Context, ffigo.Completion[ffigo.Void]) (func(), error) {
			return func() { b.cancelled.Add(1) }, nil
		}),
		func(*ffigo.Buffer, ffigo.Void) error { return nil },
	), nil
}

func (b *blockingAsyncBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, errors.New("unexpected invoke")
}

func (b *blockingAsyncBridge) DestroyHandle(uint32) error {
	return nil
}

func TestAsyncFFICancelledOnContextDeadline(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := &blockingAsyncBridge{}
	executor.RegisterFFISchema("block.Wait", bridge, 1, runtime.MustParseRuntimeFuncSig("function() Void"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "block"

func main() {
	block.Wait()
}
`)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = prog.Execute(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline, got %T %v", err, err)
	}
	if got := bridge.cancelled.Load(); got != 1 {
		t.Fatalf("expected pending async FFI cancel once, got %d", got)
	}
}
