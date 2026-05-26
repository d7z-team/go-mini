package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

type bytesCopyBackBridge struct{}

func (bytesCopyBackBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	input := reader.ReadBytes()
	mutated := append([]byte(strings.ToUpper(string(input))), '!')

	buf := ffigo.GetBuffer()
	buf.WriteUvarint(1)
	buf.WriteBytes(mutated)
	buf.WriteVarint(int64(len(mutated)))
	out := append([]byte(nil), buf.Bytes()...)
	ffigo.ReleaseBuffer(buf)
	return out, nil
}

func (b bytesCopyBackBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return b.Call(ctx, req)
}

func (bytesCopyBackBridge) DestroyHandle(uint32) error { return nil }

func TestFFIBytesCopyBackUpdatesScriptVariable(t *testing.T) {
	executor := engine.NewMiniExecutor()
	testsurface.UseRoute(t, executor, "demo.Mutate", bytesCopyBackBridge{}, 1, runtime.MustParseRuntimeFuncSigWithModes("function(TypeBytes) Int64", runtime.FFIParamInOutBytes), "")

	code := `
	package main
	import "demo"
	func main() {
		buf := []byte("go")
		n := demo.Mutate(buf)
		if n != 3 {
			panic("unexpected len")
		}
		if String(buf) != "GO!" {
			panic("copy-back failed")
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

func TestFFIBytesCopyBackUpdatesScriptMemberAndIndex(t *testing.T) {
	executor := engine.NewMiniExecutor()
	testsurface.UseRoute(t, executor, "demo.Mutate", bytesCopyBackBridge{}, 1, runtime.MustParseRuntimeFuncSigWithModes("function(TypeBytes) Int64", runtime.FFIParamInOutBytes), "")

	code := `
	package main
	import "demo"

	type Holder struct {
		Buf TypeBytes
	}

	func main() {
		holder := Holder{Buf: []byte("xy")}
		arr := []TypeBytes{[]byte("aa"), []byte("bc")}

		n1 := demo.Mutate(holder.Buf)
		n2 := demo.Mutate(arr[1])

		if n1 != 3 || n2 != 3 {
			panic("unexpected len")
		}
		if String(holder.Buf) != "XY!" {
			panic("member copy-back failed")
		}
		if String(arr[1]) != "BC!" {
			panic("index copy-back failed")
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

func TestFFIBytesCopyBackUpdatesDereferencedPointer(t *testing.T) {
	executor := engine.NewMiniExecutor()
	testsurface.UseRoute(t, executor, "demo.Mutate", bytesCopyBackBridge{}, 1, runtime.MustParseRuntimeFuncSigWithModes("function(TypeBytes) Int64", runtime.FFIParamInOutBytes), "")

	code := `
	package main
	import "demo"

	func main() {
		p := new(TypeBytes)
		*p = []byte("ok")
		n := demo.Mutate(*p)
		if n != 3 {
			panic("unexpected len")
		}
		if String(*p) != "OK!" {
			panic("deref copy-back failed")
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

func TestFFIBytesCopyBackUpdatesSliceWindow(t *testing.T) {
	executor := engine.NewMiniExecutor()
	testsurface.UseRoute(t, executor, "demo.Mutate", bytesCopyBackBridge{}, 1, runtime.MustParseRuntimeFuncSigWithModes("function(TypeBytes) Int64", runtime.FFIParamInOutBytes), "")

	code := `
	package main
	import "demo"

	func main() {
		buf := []byte("abcdef")
		n := demo.Mutate(buf[1:4])
		if n != 4 {
			panic("unexpected len")
		}
		if String(buf) != "aBCD!ef" {
			panic("slice copy-back failed")
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
