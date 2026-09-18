package rpc

import (
	"math"
	"reflect"
	"testing"
)

func FuzzEndpointValueCodecRoundTrip(f *testing.F) {
	f.Add(uint8(0), int64(-1), uint64(1), uint64(0x3ff0000000000000), uint64(0x8000000000000000), "value", []byte("bytes"))
	f.Add(uint8(8), int64(42), uint64(7), uint64(0x7ff8000000000001), uint64(0x0000000000000001), "key", []byte{0, 1, 2})
	f.Add(uint8(10), int64(0), uint64(0), uint64(0), uint64(0), "optional", []byte(nil))
	f.Add(uint8(11), int64(0), uint64(99), uint64(0), uint64(0), "", []byte(nil))

	f.Fuzz(func(t *testing.T, kind uint8, signed int64, unsigned, realBits, imagBits uint64, text string, data []byte) {
		if len(text) > 4096 || len(data) > 4096 {
			t.Skip()
		}
		var value Value
		switch kind % 13 {
		case 0:
			value = Value{Type: "bool", Data: unsigned&1 != 0}
		case 1:
			value = Value{Type: "int64", Data: signed}
		case 2:
			value = Value{Type: "uint64", Data: unsigned}
		case 3:
			value = Value{Type: "float64", Data: math.Float64frombits(realBits)}
		case 4:
			value = Value{Type: "complex128", Data: complex(math.Float64frombits(realBits), math.Float64frombits(imagBits))}
		case 5:
			value = Value{Type: "string", Data: text}
		case 6:
			value = Value{Type: "[]uint8", Data: append([]byte(nil), data...)}
		case 7:
			value = Value{Type: "[]int64", Data: []Value{{Type: "int64", Data: signed}, {Type: "int64", Data: int64(unsigned)}}}
		case 8:
			value = Value{Type: "map[string][]uint8", Data: []MapEntry{{
				Key:   Value{Type: "string", Data: text},
				Value: Value{Type: "[]uint8", Data: append([]byte(nil), data...)},
			}}}
		case 9:
			value = Value{Type: "fuzz.Message", Data: []Field{
				{ID: 1, Value: Value{Type: "string", Data: text}},
				{ID: 2, Value: Value{Type: "int64", Data: signed}},
			}}
		case 10:
			value = Value{Type: "optional[string]", Data: Value{Type: "string", Data: text}}
		case 11:
			value = Value{Type: testResourceHash, Resource: &ResourceRef{
				Epoch: unsigned | 1, ObjectID: realBits | 1, TypeHash: testResourceHash,
			}}
		case 12:
			value = Value{Type: "optional[string]"}
		}

		limits := normalizeLimits(Limits{})
		encoded, err := EncodeValues([]Value{value}, limits)
		if err != nil {
			return
		}
		decoded, err := DecodeValues(encoded, limits)
		if err != nil {
			t.Fatalf("encoded value failed to decode: %v", err)
		}
		again, err := EncodeValues(decoded, limits)
		if err != nil {
			t.Fatalf("decoded value failed to encode: %v", err)
		}
		if !reflect.DeepEqual(encoded, again) {
			t.Fatalf("wire value changed after round trip: %#v != %#v", encoded, again)
		}
	})
}
