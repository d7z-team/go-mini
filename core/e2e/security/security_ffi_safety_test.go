package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type securitySimpleBridge struct {
	Callback func(methodID uint32, args []byte) ([]byte, error)
}

func (b *securitySimpleBridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return b.Callback(req.MethodID, req.Args)
}

func (b *securitySimpleBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, nil
}

func (b *securitySimpleBridge) DestroyHandle(handle uint32) error {
	return nil
}

func TestFFIOverflowCheck(t *testing.T) {
	e := engine.NewMiniExecutor()
	bridge := &securitySimpleBridge{
		Callback: func(methodID uint32, args []byte) ([]byte, error) {
			reader := ffigo.NewReader(args)
			tmp := reader.ReadVarint()
			if tmp < -128 || tmp > 127 {
				panic(fmt.Sprintf("ffi: int8 overflow: %d", tmp))
			}
			return nil, nil
		},
	}
	e.RegisterFFISchema("test.int8", bridge, 1001, runtime.MustParseRuntimeFuncSig("function(Int64) Void"), "Test int8 overflow")

	code := `package main
		import "test"
		func main() {
			test.int8(100)
		}`
	prog, err := e.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("Normal call failed: %v", err)
	}

	codeOverflow := `package main
		import "test"
		func main() {
			test.int8(300)
		}`
	progOverflow, err := e.NewRuntimeByGoCode(codeOverflow)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	err = progOverflow.Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ffi: int8 overflow") {
		t.Errorf("Expected int8 overflow error, but got: %v", err)
	}
}

func TestFFIReturnBytesAreDeepCopied(t *testing.T) {
	e := engine.NewMiniExecutor()
	original := []byte("hello")
	bridge := &securitySimpleBridge{
		Callback: func(methodID uint32, args []byte) ([]byte, error) {
			buf := ffigo.GetBuffer()
			defer ffigo.ReleaseBuffer(buf)
			buf.WriteBytes(original)
			return buf.Bytes(), nil
		},
	}
	e.RegisterFFISchema("test.getBytes", bridge, 1002, runtime.MustParseRuntimeFuncSig("function() TypeBytes"), "Test deep copy")

	code := `package main
		import "test"
		func main() {
			b := test.getBytes()
			b[0] = 88
		}`
	prog, err := e.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	_ = prog.Execute(context.Background())

	if original[0] == 'X' {
		t.Error("FFI return value should be deep copied, but original buffer was modified")
	}
}
