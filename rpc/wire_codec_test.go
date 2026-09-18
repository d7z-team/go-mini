package rpc

import (
	"bytes"
	"math"
	"reflect"
	"testing"
)

func TestMethodAndResourceWireEncoding(t *testing.T) {
	method := Method{ID: "i", Service: "s", Name: "n", ContractHash: "c", ResourceTypeHash: "r"}
	var encodedMethod wireEncoder
	encodeWireMethod(&encodedMethod, method)
	wantMethod := []byte{1, 'i', 1, 's', 1, 'n', 1, 'c', 1, 'r'}
	if !bytes.Equal(encodedMethod.data, wantMethod) {
		t.Fatalf("method encoding = %x, want %x", encodedMethod.data, wantMethod)
	}
	for size := 0; size < len(wantMethod); size++ {
		if _, err := decodeWireMethod(newWireDecoder(wantMethod[:size], 1024)); err == nil {
			t.Fatalf("accepted method truncated at byte %d", size)
		}
	}
	decoder := newWireDecoder(wantMethod, 1024)
	gotMethod, err := decodeWireMethod(decoder)
	if err != nil || gotMethod != method {
		t.Fatalf("method = %#v, %v", gotMethod, err)
	}
	if err := decoder.Done(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		ref  *ResourceRef
		wire []byte
	}{
		{name: "absent", wire: []byte{0}},
		{name: "present", ref: &ResourceRef{Epoch: 3, ObjectID: 300, TypeHash: "r"}, wire: []byte{1, 3, 0xac, 2, 1, 'r'}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var encoder wireEncoder
			encodeWireResource(&encoder, tt.ref)
			if !bytes.Equal(encoder.data, tt.wire) {
				t.Fatalf("resource encoding = %x, want %x", encoder.data, tt.wire)
			}
			for size := 0; size < len(tt.wire); size++ {
				if _, err := decodeWireResource(newWireDecoder(tt.wire[:size], 1024)); err == nil {
					t.Fatalf("accepted resource truncated at byte %d", size)
				}
			}
			decoder := newWireDecoder(tt.wire, 1024)
			ref, err := decodeWireResource(decoder)
			if err != nil || !reflect.DeepEqual(ref, tt.ref) {
				t.Fatalf("resource = %#v, %v", ref, err)
			}
			if err := decoder.Done(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWireLabelsCanonicalEncoding(t *testing.T) {
	labels := map[string]string{"b": "2", "a": "1"}
	want := []byte{2, 1, 'a', 1, '1', 1, 'b', 1, '2'}
	var encoder wireEncoder
	encodeWireLabels(&encoder, labels)
	if !bytes.Equal(encoder.data, want) {
		t.Fatalf("labels encoding = %x, want %x", encoder.data, want)
	}
	decoder := newWireDecoder(want, 1024)
	decoded, err := decodeWireLabels(decoder, 2)
	if err != nil || !reflect.DeepEqual(decoded, labels) {
		t.Fatalf("labels = %v, %v", decoded, err)
	}
	if err := decoder.Done(); err != nil {
		t.Fatal(err)
	}
	decoded["a"] = "changed"
	if labels["a"] != "1" || !bytes.Equal(want, encoder.data) {
		t.Fatal("decoded labels share mutable state with the input")
	}
	for _, empty := range []map[string]string{nil, {}} {
		var encoder wireEncoder
		encodeWireLabels(&encoder, empty)
		if !bytes.Equal(encoder.data, []byte{0}) {
			t.Fatalf("empty labels encoding = %x", encoder.data)
		}
		decoded, err := decodeWireLabels(newWireDecoder(encoder.data, 1024), 0)
		if err != nil || decoded != nil {
			t.Fatalf("empty labels = %v, %v", decoded, err)
		}
	}
}

func TestWireLabelsRejectMalformedInput(t *testing.T) {
	for _, tt := range []struct {
		name  string
		data  []byte
		limit int
	}{
		{"count", []byte{0x80}, 2},
		{"limit", []byte{2}, 1},
		{"key", []byte{1, 2, 'a'}, 1},
		{"value", []byte{1, 1, 'a', 2, 'b'}, 1},
		{"empty_key", []byte{1, 0, 0}, 1},
		{"duplicate", []byte{2, 1, 'a', 0, 1, 'a', 0}, 2},
		{"key_utf8", []byte{1, 1, 0xff, 0}, 1},
		{"value_utf8", []byte{1, 1, 'a', 1, 0xff}, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeWireLabels(newWireDecoder(tt.data, 1024), tt.limit); err == nil {
				t.Fatal("malformed labels accepted")
			}
		})
	}
}

func TestCodecRoundTrip(t *testing.T) {
	var encoder wireEncoder
	encoder.Bool(true)
	encoder.Int(-42)
	encoder.Uint(99)
	encoder.Float32(1.5)
	encoder.Float64(-2.25)
	encoder.Complex64(complex(3, -4))
	encoder.Complex128(complex(-5, 6))
	encoder.String("MiniGo")
	encoder.Raw([]byte{0, 1, 2})

	decoder := newWireDecoder(encoder.Bytes(), 1<<20)
	boolean, err := decoder.Bool()
	if err != nil || !boolean {
		t.Fatalf("Bool = %v, %v", boolean, err)
	}
	integer, err := decoder.Int()
	if err != nil || integer != -42 {
		t.Fatalf("Int = %d, %v", integer, err)
	}
	unsigned, err := decoder.Uint()
	if err != nil || unsigned != 99 {
		t.Fatalf("Uint = %d, %v", unsigned, err)
	}
	float32Value, err := decoder.Float32()
	if err != nil || float32Value != 1.5 {
		t.Fatalf("Float32 = %v, %v", float32Value, err)
	}
	float64Value, err := decoder.Float64()
	if err != nil || float64Value != -2.25 {
		t.Fatalf("Float64 = %v, %v", float64Value, err)
	}
	complex64Value, err := decoder.Complex64()
	if err != nil || complex64Value != complex64(complex(3, -4)) {
		t.Fatalf("Complex64 = %v, %v", complex64Value, err)
	}
	complex128Value, err := decoder.Complex128()
	if err != nil || complex128Value != complex(-5, 6) {
		t.Fatalf("Complex128 = %v, %v", complex128Value, err)
	}
	text, err := decoder.String()
	if err != nil || text != "MiniGo" {
		t.Fatalf("String = %q, %v", text, err)
	}
	bytes, err := decoder.Raw()
	if err != nil || len(bytes) != 3 || bytes[2] != 2 {
		t.Fatalf("Raw = %v, %v", bytes, err)
	}
	if err := decoder.Done(); err != nil {
		t.Fatal(err)
	}

	var nonFinite wireEncoder
	nonFinite.Float64(math.Inf(1))
	decoded, err := newWireDecoder(nonFinite.Bytes(), 1<<20).Float64()
	if err != nil || !math.IsInf(decoded, 1) {
		t.Fatalf("infinity = %v, %v", decoded, err)
	}
}

func TestDecoderRejectsMalformedValues(t *testing.T) {
	if _, err := newWireDecoder([]byte{2}, 1<<20).Bool(); err == nil {
		t.Fatal("invalid bool was accepted")
	}
	if _, err := newWireDecoder([]byte{0x80}, 1<<20).Uint(); err == nil {
		t.Fatal("truncated varint was accepted")
	}
	if _, err := newWireDecoder([]byte{0x80, 0}, 1<<20).Uint(); err == nil {
		t.Fatal("non-canonical varint was accepted")
	}
	if _, err := newWireDecoder([]byte{3, 1}, 1<<20).Raw(); err == nil {
		t.Fatal("truncated bytes were accepted")
	}
	if _, err := newWireDecoder([]byte{1, 0xff}, 1<<20).String(); err == nil {
		t.Fatal("invalid UTF-8 was accepted")
	}
}
