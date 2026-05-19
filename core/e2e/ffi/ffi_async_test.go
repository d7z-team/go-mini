package tests

import (
	"context"
	"fmt"
	"strings"
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
