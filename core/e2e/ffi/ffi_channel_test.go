package tests

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
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
	recvCancel atomic.Int64
	sendCancel atomic.Int64
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
					b.recvCancel.Add(1)
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
					b.sendCancel.Add(1)
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
	case 3:
		endpoint, err := lookupFFIChannelArg(req)
		if err != nil {
			return nil, err
		}
		return ffigo.AsyncValue[int64](
			ffigo.AsyncFunc[int64](func(ctx context.Context, done ffigo.Completion[int64]) (ffigo.WaitHandle, error) {
				runCtx, cancel := context.WithCancel(ctx)
				go func() {
					var sum int64
					for {
						payload, ok, recvErr := endpoint.Recv(runCtx)
						if recvErr != nil {
							done.Complete(0, recvErr)
							return
						}
						if !ok {
							done.Complete(sum, nil)
							return
						}
						sum += ffigo.NewReader(payload).ReadVarint()
					}
				}()
				return ffigo.NewWaitHandle(ffigo.WaitExternal, "chanlib.Sum", cancel), nil
			}),
			func(buf *ffigo.Buffer, value int64) error {
				buf.WriteVarint(value)
				return nil
			},
		), nil
	case 4:
		endpoint, err := lookupFFIChannelArg(req)
		if err != nil {
			return nil, err
		}
		return ffigo.AsyncValue[ffigo.Void](
			ffigo.AsyncFunc[ffigo.Void](func(ctx context.Context, done ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
				runCtx, cancel := context.WithCancel(ctx)
				go func() {
					if err := endpoint.Send(runCtx, encodeFFIInt64(7)); err != nil {
						done.Complete(ffigo.Void{}, err)
						return
					}
					if err := endpoint.Send(runCtx, encodeFFIInt64(8)); err != nil {
						done.Complete(ffigo.Void{}, err)
						return
					}
					done.Complete(ffigo.Void{}, nil)
				}()
				return ffigo.NewWaitHandle(ffigo.WaitExternal, "chanlib.Fill", cancel), nil
			}),
			func(*ffigo.Buffer, ffigo.Void) error { return nil },
		), nil
	case 5:
		endpoint, err := lookupFFIChannelArg(req)
		if err != nil {
			return nil, err
		}
		if err := endpoint.Close(); err != nil {
			return nil, err
		}
		return nil, nil
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

func TestFFIChannelHostReceivesVMChannelArgument(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := newFFIChannelBridge()
	executor.RegisterFFISchema("chanlib.Sum", bridge, 3, runtime.MustParseRuntimeFuncSig("function(RecvChan<Int64>) Int64"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "chanlib"

func sendAndClose(ch chan Int64) {
	ch <- 4
	ch <- 6
	close(ch)
}

func main() {
	ch := make(chan Int64)
	go sendAndClose(ch)
	sum := chanlib.Sum(ch)
	if sum != 10 {
		panic("host did not receive VM channel values")
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

func TestFFIChannelHostSendsToVMChannelArgument(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := newFFIChannelBridge()
	executor.RegisterFFISchema("chanlib.Fill", bridge, 4, runtime.MustParseRuntimeFuncSig("function(SendChan<Int64>) Void"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "chanlib"

func recv(ch chan Int64, done chan Int64) {
	a := <-ch
	b := <-ch
	done <- a * 10 + b
}

func main() {
	ch := make(chan Int64)
	done := make(chan Int64)
	go recv(ch, done)
	chanlib.Fill(ch)
	if <-done != 78 {
		panic("host did not send VM channel values")
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

func TestFFIChannelHostClosesVMChannelArgument(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := newFFIChannelBridge()
	executor.RegisterFFISchema("chanlib.Close", bridge, 5, runtime.MustParseRuntimeFuncSig("function(SendChan<Int64>) Void"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "chanlib"

func main() {
	ch := make(chan Int64)
	chanlib.Close(ch)
	v, ok := <-ch
	if v != 0 || ok {
		panic("host close did not close VM channel")
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

func TestFFIChannelSelectCancelsUnchosenHostReceive(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := newFFIChannelBridge()
	executor.RegisterFFISchema("chanlib.Values", bridge, 1, runtime.MustParseRuntimeFuncSig("function() RecvChan<Int64>"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "chanlib"

func send(ch chan Int64) {
	ch <- 23
}

func main() {
	host := chanlib.Values()
	vm := make(chan Int64)
	go send(vm)
	picked := 0
	select {
	case picked = <-host:
		panic("host receive should have been cancelled")
	case picked = <-vm:
	}
	if picked != 23 {
		panic("VM receive case not selected")
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waitFFIChannelCounter(ctx, t, &bridge.recvCancel, 1, "host receive cancel")
}

func TestFFIChannelSelectCancelsUnchosenHostSend(t *testing.T) {
	executor := engine.NewMiniExecutor()
	bridge := newFFIChannelBridge()
	executor.RegisterFFISchema("chanlib.Sink", bridge, 2, runtime.MustParseRuntimeFuncSig("function() SendChan<Int64>"), "")

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "chanlib"

func send(ch chan Int64) {
	ch <- 31
}

func main() {
	host := chanlib.Sink()
	vm := make(chan Int64)
	go send(vm)
	picked := 0
	select {
	case host <- 99:
		panic("host send should have been cancelled")
	case picked = <-vm:
	}
	if picked != 31 {
		panic("VM receive case not selected")
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waitFFIChannelCounter(ctx, t, &bridge.sendCancel, 1, "host send cancel")
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

func waitFFIChannelCounter(ctx context.Context, t *testing.T, counter *atomic.Int64, want int64, name string) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if got := counter.Load(); got >= want {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s: got %d want %d", name, counter.Load(), want)
		}
	}
}

func lookupFFIChannelArg(req *ffigo.FFICallRequest) (ffigo.ChannelEndpoint, error) {
	if req == nil || req.Channels == nil {
		return nil, errors.New("missing channel registry")
	}
	id := ffigo.NewReader(req.Args).ReadUvarint()
	endpoint, ok := req.Channels.LookupChannel(id)
	if !ok || endpoint == nil {
		return nil, fmt.Errorf("unknown channel endpoint %d", id)
	}
	return endpoint, nil
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
