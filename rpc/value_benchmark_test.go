package rpc

import "testing"

func BenchmarkEncodeValues(b *testing.B) {
	values := []Value{{Type: "example.Benchmark", Data: []Field{
		{ID: 1, Value: Value{Type: "string", Data: "benchmark"}},
		{ID: 2, Value: Value{Type: "[]uint8", Data: []byte("payload")}},
		{ID: 3, Value: Value{Type: "map[string]int64", Data: []MapEntry{
			{Key: Value{Type: "string", Data: "second"}, Value: Value{Type: "int64", Data: int64(2)}},
			{Key: Value{Type: "string", Data: "first"}, Value: Value{Type: "int64", Data: int64(1)}},
		}}},
	}}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := EncodeValues(values, Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeValues(b *testing.B) {
	payload, err := EncodeValues([]Value{{Type: "example.Benchmark", Data: []Field{
		{ID: 1, Value: Value{Type: "string", Data: "benchmark"}},
		{ID: 2, Value: Value{Type: "[]uint8", Data: []byte("payload")}},
	}}}, Limits{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeValues(payload, Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeLargeBytes(b *testing.B) {
	values := []Value{{Type: "[]uint8", Data: make([]byte, 1<<20)}}
	b.ReportAllocs()
	b.SetBytes(1 << 20)
	b.ResetTimer()
	for range b.N {
		if _, err := EncodeValues(values, Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeLargeBytes(b *testing.B) {
	payload, err := EncodeValues([]Value{{Type: "[]uint8", Data: make([]byte, 1<<20)}}, Limits{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(1 << 20)
	b.ResetTimer()
	for range b.N {
		if _, err := DecodeValues(payload, Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}
