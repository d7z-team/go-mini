package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type arrayCopyBackBridge struct{}

func (arrayCopyBackBridge) Call(_ context.Context, _ uint32, args []byte) ([]byte, error) {
	reader := ffigo.NewReader(args)
	count := int(reader.ReadUvarint())
	values := make([]int64, count)
	for i := range values {
		values[i] = reader.ReadVarint()
	}
	mutated := make([]int64, 0, len(values)+1)
	for _, item := range values {
		mutated = append(mutated, item*10)
	}
	mutated = append(mutated, 99)

	copyBackBuf := ffigo.GetBuffer()
	copyBackBuf.WriteUvarint(uint64(len(mutated)))
	for _, item := range mutated {
		copyBackBuf.WriteVarint(item)
	}
	buf := ffigo.GetBuffer()
	buf.WriteUvarint(1)
	buf.WriteBytes(copyBackBuf.Bytes())
	buf.WriteVarint(int64(len(mutated)))

	out := append([]byte(nil), buf.Bytes()...)
	ffigo.ReleaseBuffer(copyBackBuf)
	ffigo.ReleaseBuffer(buf)
	return out, nil
}

func (b arrayCopyBackBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return b.Call(ctx, 0, args)
}

func (arrayCopyBackBridge) DestroyHandle(uint32) error { return nil }

func TestFFIArrayCopyBackUpdatesWholeArray(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.RegisterFFISchema(
		"demo.Rewrite",
		arrayCopyBackBridge{},
		1,
		runtime.MustParseRuntimeFuncSigWithModes("function(Array<Int64>) Int64", runtime.FFIParamInOutArray),
		"",
	)

	code := `
	package main
	import "demo"

	func main() {
		arr := []int64{1, 2, 3}
		n := demo.Rewrite(arr)
		if n != 4 {
			panic("unexpected len")
		}
		if len(arr) != 4 {
			panic("array length copy-back failed")
		}
		if arr[0] != 10 || arr[1] != 20 || arr[2] != 30 || arr[3] != 99 {
			panic("array copy-back failed")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFFIArrayCopyBackRejectsSliceWindow(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.RegisterFFISchema(
		"demo.Rewrite",
		arrayCopyBackBridge{},
		1,
		runtime.MustParseRuntimeFuncSigWithModes("function(Array<Int64>) Int64", runtime.FFIParamInOutArray),
		"",
	)

	code := `
	package main
	import "demo"

	func main() {
		arr := []int64{1, 2, 3, 4}
		demo.Rewrite(arr[1:3])
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	err = prog.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "does not support slice/window left values") {
		t.Fatalf("expected slice/window error, got %v", err)
	}
}
