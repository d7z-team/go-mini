package rpc

import "testing"

func TestEncodeValuesCanonicalizesMessageFieldOrder(t *testing.T) {
	first, err := EncodeValues([]Value{{Type: "example.Message", Data: []Field{
		{ID: 2, Value: Value{Type: "int64", Data: int64(2)}},
		{ID: 1, Value: Value{Type: "string", Data: "one"}},
	}}}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncodeValues([]Value{{Type: "example.Message", Data: []Field{
		{ID: 1, Value: Value{Type: "string", Data: "one"}},
		{ID: 2, Value: Value{Type: "int64", Data: int64(2)}},
	}}}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("field order changed canonical bytes: %x != %x", first, second)
	}
}

func TestValidateValuesChecksDeclaredWireShape(t *testing.T) {
	valid := []Value{
		{Type: "string", Data: "value"},
		{Type: "[]int64", Data: []Value{{Type: "int64", Data: int64(1)}}},
		{Type: "map[string][]uint8", Data: []MapEntry{{
			Key:   Value{Type: "string", Data: "key"},
			Value: Value{Type: "[]uint8", Data: []byte("value")},
		}}},
		{Type: "example.Message", Data: []Field{{ID: 1, Value: Value{Type: "string", Data: "ok"}}}},
		{Type: "optional[string]", Data: Value{Type: "string", Data: "value"}},
		{Type: "optional[string]"},
	}
	if err := validateValues(valid, normalizeLimits(Limits{})); err != nil {
		t.Fatalf("valid values: %v", err)
	}

	invalid := []Value{
		{Type: "string", Data: int64(1)},
		{Type: "optional[string]", Data: "value"},
		{Type: "optional[string]", Data: Value{Type: "int64", Data: int64(1)}},
		{Type: "[]string", Data: []Value{{Type: "int64", Data: int64(1)}}},
		{Type: "map[string]int64", Data: []MapEntry{{
			Key:   Value{Type: "int64", Data: int64(1)},
			Value: Value{Type: "int64", Data: int64(1)},
		}}},
		{Type: "example.Resource", Resource: &ResourceRef{Epoch: 1, ObjectID: 1, TypeHash: testResourceHash}},
		{Type: "example.Message", Data: []Field{{ID: 1, Value: Value{Type: testResourceHash, Resource: &ResourceRef{Epoch: 1, ObjectID: 1, TypeHash: testResourceHash}}}}},
	}
	for _, value := range invalid {
		if err := validateValues([]Value{value}, normalizeLimits(Limits{})); err == nil {
			t.Fatalf("accepted invalid value %#v", value)
		}
	}
}

func TestValueCodecPreservesNilAndEmptyCollections(t *testing.T) {
	values := []Value{
		{Type: "[]uint8"},
		{Type: "[]uint8", Data: []byte{}},
		{Type: "[]string"},
		{Type: "[]string", Data: []Value{}},
		{Type: "map[string]int64"},
		{Type: "map[string]int64", Data: []MapEntry{}},
	}
	wire, err := EncodeValues(values, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeValues(wire, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if decoded[0].Data != nil || decoded[2].Data != nil || decoded[4].Data != nil {
		t.Fatalf("nil collections became non-nil: %#v", decoded)
	}
	bytes, ok := decoded[1].Data.([]byte)
	if !ok || bytes == nil || len(bytes) != 0 {
		t.Fatalf("empty bytes = %#v", decoded[1].Data)
	}
	items, ok := decoded[3].Data.([]Value)
	if !ok || items == nil || len(items) != 0 {
		t.Fatalf("empty slice = %#v", decoded[3].Data)
	}
	entries, ok := decoded[5].Data.([]MapEntry)
	if !ok || entries == nil || len(entries) != 0 {
		t.Fatalf("empty map = %#v", decoded[5].Data)
	}
}
