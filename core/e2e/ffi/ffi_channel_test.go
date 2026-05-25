package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type ffiChannelBridge struct {
	values     chan int64
	sink       chan int64
	recvWaiter chan struct{}
	sendWaiter chan struct{}
}

func newFFIChannelBridge() *ffiChannelBridge {
	return &ffiChannelBridge{
		values:     make(chan int64),
		sink:       make(chan int64),
		recvWaiter: make(chan struct{}, 8),
		sendWaiter: make(chan struct{}, 8),
	}
}

func (b *ffiChannelBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req.Channels == nil {
		return nil, errors.New("missing channel registry")
	}
	switch req.MethodID {
	case 1:
		id := req.Channels.RegisterChannel(ffigo.ChannelEndpointFuncs{
			Elem: "Int64",
			Dir:  ffigo.ChannelRecv,
			OnRecv: func(ctx context.Context) ([]byte, bool, error) {
				signalFFIChannelWaiter(b.recvWaiter)
				select {
				case value, ok := <-b.values:
					if !ok {
						return nil, false, nil
					}
					return encodeFFIInt64(value), true, nil
				case <-ctx.Done():
					return nil, false, ctx.Err()
				}
			},
			OnTryRecv: func() ([]byte, bool, bool, error) {
				select {
				case value, ok := <-b.values:
					if !ok {
						return nil, false, true, nil
					}
					return encodeFFIInt64(value), true, true, nil
				default:
					return nil, false, false, nil
				}
			},
		})
		return encodeFFIUvarint(id), nil
	case 2:
		id := req.Channels.RegisterChannel(ffigo.ChannelEndpointFuncs{
			Elem: "Int64",
			Dir:  ffigo.ChannelSend,
			OnSend: func(ctx context.Context, payload []byte) error {
				value := ffigo.NewReader(payload).ReadVarint()
				signalFFIChannelWaiter(b.sendWaiter)
				select {
				case b.sink <- value:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
			OnTrySend: func(payload []byte) (bool, error) {
				value := ffigo.NewReader(payload).ReadVarint()
				select {
				case b.sink <- value:
					return true, nil
				default:
					return false, nil
				}
			},
		})
		return encodeFFIUvarint(id), nil
	default:
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
}

func (b *ffiChannelBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, errors.New("unexpected invoke")
}

func (b *ffiChannelBridge) DestroyHandle(uint32) error {
	return nil
}

func TestFFIChannelReceiveWaitsOnHostEndpoint(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := newFFIChannelBridge()
	executor.RegisterFFISchema("chanlib.Values", bridge, 1, runtime.MustParseRuntimeFuncSig("function() RecvChan<Int64>"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "chanlib"

func main() {
	ch := chanlib.Values()
	got := <-ch
	if got != 42 {
		panic("bad ffi channel receive")
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	errCh := executeProgramAsync(ctx, prog)
	waitFFIChannelSignal(ctx, t, bridge.recvWaiter)
	bridge.values <- 42
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestFFIChannelSendWaitsOnHostEndpoint(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := newFFIChannelBridge()
	executor.RegisterFFISchema("chanlib.Sink", bridge, 2, runtime.MustParseRuntimeFuncSig("function() SendChan<Int64>"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "chanlib"

func main() {
	ch := chanlib.Sink()
	ch <- 33
}
`)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	errCh := executeProgramAsync(ctx, prog)
	waitFFIChannelSignal(ctx, t, bridge.sendWaiter)
	value := <-bridge.sink
	if value != 33 {
		t.Fatalf("bad ffi channel send: got %d", value)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestFFIChannelSelectUsesHostEndpoint(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := newFFIChannelBridge()
	bridge.values = make(chan int64, 1)
	bridge.values <- 8
	executor.RegisterFFISchema("chanlib.Values", bridge, 1, runtime.MustParseRuntimeFuncSig("function() RecvChan<Int64>"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "chanlib"

func main() {
	ch := chanlib.Values()
	picked := 0
	select {
	case picked = <-ch:
	default:
		picked = -1
	}
	if picked != 8 {
		panic("select missed ready ffi channel")
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

func TestFFIChannelSelectWaitsOnHostEndpoint(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := newFFIChannelBridge()
	executor.RegisterFFISchema("chanlib.Values", bridge, 1, runtime.MustParseRuntimeFuncSig("function() RecvChan<Int64>"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "chanlib"

func main() {
	ch := chanlib.Values()
	picked := 0
	select {
	case picked = <-ch:
	}
	if picked != 19 {
		panic("blocking select missed ffi channel")
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	errCh := executeProgramAsync(ctx, prog)
	waitFFIChannelSignal(ctx, t, bridge.recvWaiter)
	bridge.values <- 19
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestFFIChannelForRangeWaitsUntilHostClose(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := newFFIChannelBridge()
	executor.RegisterFFISchema("chanlib.Values", bridge, 1, runtime.MustParseRuntimeFuncSig("function() RecvChan<Int64>"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "chanlib"

func main() {
	ch := chanlib.Values()
	sum := 0
	for v := range ch {
		sum = sum + v
	}
	if sum != 3 {
		panic("bad ffi channel range sum")
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	errCh := executeProgramAsync(ctx, prog)
	waitFFIChannelSignal(ctx, t, bridge.recvWaiter)
	bridge.values <- 1
	waitFFIChannelSignal(ctx, t, bridge.recvWaiter)
	bridge.values <- 2
	waitFFIChannelSignal(ctx, t, bridge.recvWaiter)
	close(bridge.values)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

type executableProgram interface {
	Execute(context.Context) error
}

func executeProgramAsync(ctx context.Context, prog executableProgram) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- prog.Execute(ctx)
	}()
	return errCh
}

func waitFFIChannelSignal(ctx context.Context, t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for FFI channel endpoint: %v", ctx.Err())
	}
}

func signalFFIChannelWaiter(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func encodeFFIInt64(value int64) []byte {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteVarint(value)
	return append([]byte(nil), buf.Bytes()...)
}

func encodeFFIUvarint(value uint64) []byte {
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteUvarint(value)
	return append([]byte(nil), buf.Bytes()...)
}
