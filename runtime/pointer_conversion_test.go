package runtime

import (
	"fmt"
	"testing"

	"github.com/d7z-team/mini-go/compiler/types"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func FuzzPointerConversionViews(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte{2, 1, 0, 2})
	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 256 {
			t.Skip()
		}
		testPointerConversionViews(t, operations)
	})
}

func TestPointerConversionViewsShareOriginalStorage(t *testing.T) {
	operations := make([]byte, 3000)
	for i := range operations {
		operations[i] = byte(i % 3)
	}
	testPointerConversionViews(t, operations)
}

func testPointerConversionViews(t *testing.T, operations []byte) {
	t.Helper()
	table := types.NewTable()
	decls := make(map[string]types.TypeNode)
	for _, name := range []string{"A", "B", "C"} {
		node := types.TypeNode{ID: types.TypeID(name), Kind: types.Named, Identity: types.TypeKey{ModulePath: "example", DeclID: types.DeclID(name)}, Underlying: types.Builtin(types.PrimitiveInt)}
		if err := table.Add(node); err != nil {
			t.Fatal(err)
		}
		decls[name] = node
	}
	registry := newModuleRegistry()
	if err := registry.addExecutable(&executable{Artifact: ir.Artifact{Module: ir.Module{Path: "example"}, TypeTable: *table}, Types: decls}); err != nil {
		t.Fatal(err)
	}
	module, _ := registry.module("example")
	stored := newVMValue("example.A", int64(1))
	root := reflectCellPointer(module, "example.A", "stored", &stored)
	p := root
	for i, operation := range operations {
		var err error
		p, err = module.convertValue(p, []string{"Ptr<example.B>", "Ptr<example.C>", "Ptr<example.A>"}[operation%3])
		if err != nil {
			t.Fatal(err)
		}
		pointer := p.Data.(*vmPointer)
		if pointer != root.Data && pointer.original != root.Data {
			t.Fatal("pointer view retained an intermediate conversion")
		}
		if err := storePointer(p, newVMValue(pointer.Type, int64(i))); err != nil {
			t.Fatal(err)
		}
		loaded, err := derefPointer(p)
		if err != nil || loaded.materializedData() != int64(i) || stored.Type.String() != "example.A" || stored.materializedData() != int64(i) {
			t.Fatalf("load=%v storage=%v err=%v", loaded, stored, err)
		}
	}
}

func FuzzSliceArrayPointerStorage(f *testing.F) {
	f.Add([]byte{1, 2, 3}, uint8(1), uint8(2), false)
	f.Add([]byte{}, uint8(0), uint8(0), true)
	f.Add([]byte{7}, uint8(0), uint8(2), true)
	f.Fuzz(func(t *testing.T, input []byte, offset, length uint8, bytes bool) {
		if len(input) > 256 {
			t.Skip()
		}
		module := &moduleInstance{}
		elem := "Int"
		values := make([]vmValue, len(input))
		for i, v := range input {
			values[i] = newVMValue(elem, int64(v))
		}
		original := newOwnedSliceValue("Slice<Int>", values)
		if bytes {
			elem = "Uint8"
			original = newByteSliceValue("Slice<Uint8>", string(input))
		}
		header := original.Data.(*vmSlice)
		start := int(offset) % (len(input) + 1)
		view := newSliceViewValue(original.Type, header, start, len(input)-start, len(input)-start)
		target := fmt.Sprintf("Ptr<Array<%d, %s>>", length, elem)
		p, err := module.convertValue(view, target)
		if int(length) > len(input)-start {
			if err == nil {
				t.Fatal("short slice conversion succeeded")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		other := newSliceViewValue(original.Type, header, start, len(input)-start, len(input)-start)
		q, err := module.convertValue(other, target)
		if err != nil {
			t.Fatal(err)
		}
		if equal, err := module.equalValues(p, q); err != nil || !equal {
			t.Fatalf("same address: %v %v", equal, err)
		}
		loaded, err := derefPointer(p)
		if err != nil {
			t.Fatal(err)
		}
		items := loaded.Data.([]vmValue)
		if len(items) != int(length) {
			t.Fatal("array length mismatch")
		}
		if length > 0 {
			if bytes {
				items[0] = newVMValue(elem, uint64(99))
			} else {
				items[0] = newVMValue(elem, int64(99))
			}
			if err := storePointer(p, loaded); err != nil {
				t.Fatal(err)
			}
			got, err := numericAsInt64(header.valueAt(start))
			if err != nil || got != 99 {
				t.Fatalf("shared write: %d %v", got, err)
			}
		}
	})
}

func TestNilSliceToZeroArrayPointer(t *testing.T) {
	module := &moduleInstance{}
	for _, elem := range []string{"Int", "Uint8"} {
		target := "Ptr<Array<0, " + elem + ">>"
		p, err := module.convertValue(newVMValue("Slice<"+elem+">", (*vmSlice)(nil)), target)
		if err != nil || p.Data.(*vmPointer) != nil {
			t.Fatalf("nil conversion: %#v %v", p, err)
		}
		if equal, err := module.equalValues(p, newVMValue(target, nil)); err != nil || !equal {
			t.Fatalf("nil pointer equality: %v %v", equal, err)
		}
		value := newOwnedSliceValue("Slice<Int>", []vmValue{})
		if elem == "Uint8" {
			value = newByteSliceValue("Slice<Uint8>", "")
		}
		p, err = module.convertValue(value, target)
		if err != nil || p.Data.(*vmPointer) == nil {
			t.Fatalf("empty conversion: %#v %v", p, err)
		}
	}
}
