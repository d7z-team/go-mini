package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	miniruntime "gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

type packageValueCounter struct {
	value int64
}

type packageValueBridge struct {
	registry   *ffigo.HandleRegistry
	seen       []int64
	lastHandle uint32
}

func (b *packageValueBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	if req == nil {
		return nil, errors.New("missing request")
	}
	reader := ffigo.NewReader(req.Args)
	switch req.MethodID {
	case 1:
		rawID, err := reader.ReadUvarint()
		if err != nil {
			return nil, err
		}
		id := uint32(rawID)
		b.lastHandle = id
		obj, err := b.registry.GetTypedWithAudit(id, "mock.Counter")
		if err != nil {
			return nil, err
		}
		counter, ok := obj.(*packageValueCounter)
		if !ok {
			return nil, fmt.Errorf("unexpected counter object %T", obj)
		}
		buf := ffigo.GetBuffer()
		buf.WriteVarint(counter.value)
		return buf.Bytes(), nil
	case 2:
		v, err := reader.ReadVarint()
		if err != nil {
			return nil, err
		}
		b.seen = append(b.seen, v)
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown method %d", req.MethodID)
	}
}

func (b *packageValueBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return b.Call(ctx, req)
}

func (b *packageValueBridge) DestroyHandle(handle uint32) error {
	if b.registry != nil {
		b.registry.Remove(handle)
	}
	return nil
}

func TestFFIPackageHostRefValue(t *testing.T) {
	executor, bridge := newPackageValueExecutor(t)

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "mock"

func main() {
	mock.Assert(mock.Default.Value())
	c := mock.Default
	mock.Assert(c.Value())
}
`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := prog.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(bridge.seen) != "[42 42]" {
		t.Fatalf("unexpected package HostRef values: %v", bridge.seen)
	}
	if bridge.lastHandle == 0 {
		t.Fatal("package HostRef handle was not observed")
	}
	if err := bridge.DestroyHandle(bridge.lastHandle); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.registry.GetTypedWithAudit(bridge.lastHandle, "mock.Counter"); err != nil {
		t.Fatalf("pinned package value was removed: %v", err)
	}
}

func TestFFIPackageValueIsReadOnly(t *testing.T) {
	executor, _ := newPackageValueExecutor(t)
	_, err := executor.CompileGoCode(`
package main

import "mock"

func main() {
	mock.Default = mock.Default
}
`)
	if err == nil {
		t.Fatal("expected read-only package value assignment to fail")
	}
	if !strings.Contains(err.Error(), "read-only external symbol") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBytecodeRequiresRegisteredPackageValueSurface(t *testing.T) {
	executor, _ := newPackageValueExecutor(t)
	compiled, err := executor.CompileGoCode(`
package main

import "mock"

func main() {
	mock.Assert(mock.Default.Value())
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if compiled == nil || compiled.Bytecode == nil || compiled.Bytecode.Executable == nil || len(compiled.Bytecode.Executable.ModuleRequirements) == 0 {
		t.Fatal("compiled bytecode missing module requirements")
	}
	payload, err := compiled.MarshalBytecodeJSON()
	if err != nil {
		t.Fatal(err)
	}

	loader := engine.MustNewMiniExecutor()
	_, err = loader.NewRuntimeByBytecodeJSON(payload)
	if err == nil {
		t.Fatal("expected bytecode load to reject missing external surface")
	}
	if !strings.Contains(err.Error(), "missing ffi module mock") {
		t.Fatalf("unexpected error: %v", err)
	}

	partial := engine.MustNewMiniExecutor()
	partialBridge := &packageValueBridge{}
	registerMockCounterSurface(t, partial, partialBridge, false)
	_, err = partial.NewRuntimeByBytecodeJSON(payload)
	if err == nil {
		t.Fatal("expected bytecode load to reject missing external method route")
	}
	if !strings.Contains(err.Error(), "missing external FFI function mock.Counter.Value") {
		t.Fatalf("unexpected method route error: %v", err)
	}
}

func newPackageValueExecutor(t *testing.T) (*engine.MiniExecutor, *packageValueBridge) {
	t.Helper()
	executor := engine.MustNewMiniExecutor()
	bridge := &packageValueBridge{}
	registerMockCounterSurface(t, executor, bridge, true)
	return executor, bridge
}

func registerMockCounterSurface(t *testing.T, executor *engine.MiniExecutor, bridge *packageValueBridge, withValueMethod bool) {
	t.Helper()
	counterType := miniruntime.TypeSpec("mock.Counter")
	hostRefType := miniruntime.MustParseRuntimeType(miniruntime.HostRefType(counterType))

	schema := miniruntime.NewFFISurfaceSchema()
	if err := schema.AddStruct("mock", "Counter", miniruntime.MustParseRuntimeStructSpec(
		"mock.Counter",
		miniruntime.StructOwnershipHostOpaque,
		"struct { Value function(HostRef<mock.Counter>) Int64; }",
	)); err != nil {
		t.Fatal(err)
	}
	if withValueMethod {
		if err := schema.AddRouteDecls([]miniruntime.FFIRouteDecl{
			{
				TypePackagePath: "mock",
				TypeMemberName:  "Counter",
				MethodName:      "Value",
				RouteName:       "mock.Counter.Value",
				MethodID:        1,
				Sig:             miniruntime.MustParseRuntimeFuncSig("function(HostRef<mock.Counter>) Int64"),
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := schema.AddRouteDecls([]miniruntime.FFIRouteDecl{
		testsurface.Route("mock.Assert", 2, miniruntime.MustParseRuntimeFuncSig("function(Int64) Void"), ""),
	}); err != nil {
		t.Fatal(err)
	}
	if err := schema.AddValue("mock", "Default", &miniruntime.ValueSpec{Type: hostRefType, ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if err := executor.UseSurface(surface.New(schema, func(ctx miniruntime.FFIBindContext) (*miniruntime.BoundFFISurface, error) {
		bridge.registry = ctx.Registry
		bound := miniruntime.NewBoundFFISurfaceFromSchema(schema)
		if err := bound.BindSchemaRoutes(schema, bridge); err != nil {
			return nil, err
		}
		value, err := (miniruntime.StaticHostRefProvider{
			ElementType: counterType,
			Value:       &packageValueCounter{value: 42},
			Bridge:      bridge,
		}).Bind(ctx)
		if err != nil {
			return nil, err
		}
		bound.AddPackageValue("mock", "Default", &miniruntime.ValueSpec{Type: hostRefType, ReadOnly: true}, value)
		return bound, nil
	})); err != nil {
		t.Fatal(err)
	}
}
