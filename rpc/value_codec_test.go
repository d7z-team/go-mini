package rpc

import (
	"bytes"
	"testing"
)

func TestValueCodecCanonicalizesMapOrder(t *testing.T) {
	first := []Value{{Type: "map[string]int64", Data: []MapEntry{
		{Key: Value{Type: "string", Data: "z"}, Value: Value{Type: "int64", Data: int64(2)}},
		{Key: Value{Type: "string", Data: "a"}, Value: Value{Type: "int64", Data: int64(1)}},
	}}}
	second := []Value{{Type: "map[string]int64", Data: []MapEntry{first[0].Data.([]MapEntry)[1], first[0].Data.([]MapEntry)[0]}}}
	encodedFirst, err := EncodeValues(first, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	encodedSecond, err := EncodeValues(second, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodedFirst, encodedSecond) {
		t.Fatal("map insertion order changed canonical MRPC encoding")
	}
	decoded, err := DecodeValues(encodedFirst, Limits{})
	if err != nil || len(decoded) != 1 {
		t.Fatalf("decode = %#v, %v", decoded, err)
	}
}

func TestValueCodecRejectsOutOfRangeKind(t *testing.T) {
	var encoded wireEncoder
	encoded.Uint(1)
	encoded.String("int64")
	encoded.Uint(256 + uint64(wireNil))
	if _, err := DecodeValues(encoded.Bytes(), Limits{}); err == nil {
		t.Fatal("out-of-range value kind was accepted")
	}
}

func TestValueCodecDecodedBytesOwnTheirStorage(t *testing.T) {
	encoded, err := EncodeValues([]Value{{Type: "[]uint8", Data: []byte("payload")}}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	wireSnapshot := append([]byte(nil), encoded...)
	decoded, err := DecodeValues(encoded, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	decoded[0].Data.([]byte)[0] = 'P'
	if !bytes.Equal(encoded, wireSnapshot) {
		t.Fatal("decoded byte value aliases its wire payload")
	}
}

func FuzzValueCodec(f *testing.F) {
	seed, _ := EncodeValues([]Value{{Type: "[]string", Data: []Value{{Type: "string", Data: "seed"}}}}, Limits{})
	f.Add(seed)
	f.Add([]byte{0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		values, err := DecodeValues(data, Limits{MaxMessageBytes: 1 << 20})
		if err != nil {
			return
		}
		canonical, err := EncodeValues(values, Limits{MaxMessageBytes: 1 << 20})
		if err != nil {
			t.Fatal(err)
		}
		again, err := DecodeValues(canonical, Limits{MaxMessageBytes: 1 << 20})
		if err != nil || len(again) != len(values) {
			t.Fatalf("canonical decode = %#v, %v", again, err)
		}
	})
}
