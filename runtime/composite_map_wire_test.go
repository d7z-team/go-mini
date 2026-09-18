package runtime

import (
	"math"
	"testing"
)

func TestMapKeyUsesGoFloatEquality(t *testing.T) {
	module := &moduleInstance{}
	positiveZero, err := module.mapKey(newVMValue("Float64", float64(0)))
	if err != nil {
		t.Fatal(err)
	}
	negativeZero, err := module.mapKey(newVMValue("Float64", math.Copysign(0, -1)))
	if err != nil {
		t.Fatal(err)
	}
	if positiveZero != negativeZero {
		t.Fatalf("zero keys differ: %#v != %#v", positiveZero, negativeZero)
	}

	nanKey, err := module.mapKey(newVMValue("Float64", math.NaN()))
	if err != nil {
		t.Fatal(err)
	}
	data := newVMMap(2)
	first := data.keyForStore(nanKey)
	second := data.keyForStore(nanKey)
	if !nanKey.NonReflexive || first == second || first == nanKey || second == nanKey {
		t.Fatalf("NaN keys are reflexive: base=%#v first=%#v second=%#v", nanKey, first, second)
	}
	complexZero, err := module.mapKey(newVMValue("Complex128", complex(0, math.Copysign(0, -1))))
	if err != nil {
		t.Fatal(err)
	}
	positiveComplexZero, err := module.mapKey(newVMValue("Complex128", complex(0, 0)))
	if err != nil || complexZero != positiveComplexZero {
		t.Fatalf("complex zero keys differ: %#v != %#v, err=%v", complexZero, positiveComplexZero, err)
	}
	arrayKey, err := module.mapKey(newVMValue("Array<1, Float64>", []vmValue{newVMValue("Float64", math.NaN())}))
	if err != nil || !arrayKey.NonReflexive {
		t.Fatalf("array NaN key = %#v, err=%v", arrayKey, err)
	}
	structValue, err := newStructValue(module, "struct{Value:Float64}", []string{"Value"}, []vmValue{newVMValue("Float64", math.NaN())})
	if err != nil {
		t.Fatal(err)
	}
	structKey, err := module.mapKey(structValue)
	if err != nil || !structKey.NonReflexive {
		t.Fatalf("struct NaN key = %#v, err=%v", structKey, err)
	}
}

func TestScalarMapKeyPreservesCanonicalIdentity(t *testing.T) {
	module := &moduleInstance{}
	left, err := module.mapKey(newVMValue("example.Key", int64(7)))
	if err != nil {
		t.Fatal(err)
	}
	right, err := module.mapKey(newVMValue("Int", int64(7)))
	if err != nil {
		t.Fatal(err)
	}
	if left.Kind != vmMapKeyInt || left == right {
		t.Fatalf("scalar keys lost type identity: left=%#v right=%#v", left, right)
	}
}
