package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

type ComplexNested struct {
	Data map[string][]int64
}

type ComplexBridge struct {
	t *testing.T
}

func (b *ComplexBridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	switch req.MethodID {
	case 1:
		i, err := reader.ReadVarint()
		if err != nil {
			return nil, err
		}
		s, err := reader.ReadString()
		if err != nil {
			return nil, err
		}
		bl, err := reader.ReadBool()
		if err != nil {
			return nil, err
		}
		rawPtr, err := reader.ReadUvarint()
		if err != nil {
			return nil, err
		}
		ptr := uint32(rawPtr)
		if i != 0 || s != "" || bl != false || ptr != 0 {
			b.t.Errorf("Expected zero values, got: %d, %q, %v, %d", i, s, bl, ptr)
		}
	case 2:
		count, err := reader.ReadUvarint()
		if err != nil {
			return nil, err
		}
		if count != 1 {
			b.t.Errorf("Expected map count 1, got %d", count)
		}
		k, err := reader.ReadString()
		if err != nil {
			return nil, err
		}
		if k != "key" {
			b.t.Errorf("Expected key 'key', got %q", k)
		}
		arrLen, err := reader.ReadUvarint()
		if err != nil {
			return nil, err
		}
		if arrLen != 2 {
			b.t.Errorf("Expected array len 2, got %d", arrLen)
		}
	}
	return nil, nil
}

func (b *ComplexBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, nil
}

func (b *ComplexBridge) DestroyHandle(handle uint32) error { return nil }

func TestFFISerializationEdgeCases(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	bridge := &ComplexBridge{t: t}

	schema := runtime.NewFFISurfaceSchema()
	schema.AddStruct("test.Handle", runtime.MustParseRuntimeStructSpec("test.Handle", runtime.StructOwnershipHostOpaque, "struct {}"))
	schema.AddRouteDecls([]runtime.FFIRouteDecl{
		testsurface.Route("test.Zero", 1, runtime.MustParseRuntimeFuncSig("function(Int64, String, Bool, HostRef<test.Handle>) Void"), ""),
		testsurface.Route("test.Nested", 2, runtime.MustParseRuntimeFuncSig("function(Map<String, Array<Int64>>) Void"), ""),
	})
	if err := executor.UseSurface(testsurface.SchemaBundle(schema, bridge)); err != nil {
		t.Fatal(err)
	}

	code := `
package main
import "test"

func main() {
	var i int
	var s string
	var b bool
	test.Zero(i, s, b, nil)

	m := make(map[string][]int)
	m["key"] = []int{1, 2}
	test.Nested(m)
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
