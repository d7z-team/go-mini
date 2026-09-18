package rpc

import (
	"bytes"
	"context"
	"testing"
)

func TestEndpointFragmentEncoderReusesWriterBuffer(t *testing.T) {
	buffer := make([]byte, 0, 256)
	first, err := encodeEndpointFragmentTo(buffer, 1, 3, 0, []byte("abc"), 256)
	if err != nil {
		t.Fatal(err)
	}
	second, err := encodeEndpointFragmentTo(first[:0], 2, 3, 0, []byte("def"), 256)
	if err != nil {
		t.Fatal(err)
	}
	if &first[0] != &second[0] {
		t.Fatal("fragment encoder did not reuse writer-owned buffer")
	}
}

func TestEndpointTransparentlyFragmentsLargeValues(t *testing.T) {
	method := Method{ID: "example/bytes::Service.Echo", Service: "example/bytes::Service", Name: "Echo", ContractHash: testContractHash}
	binder := newTestBinder(testBinderOptions{})
	if err := binder.Register(newTestProvider(t, method, func(_ context.Context, arguments []Value) ([]Value, error) {
		return arguments, nil
	})); err != nil {
		t.Fatal(err)
	}
	options := EndpointOptions{Limits: Limits{MaxFrameBytes: 256, MaxMessageBytes: 16 << 10, MaxInFlightBytes: 32 << 10}}
	client, _ := openEndpointPair(t, nil, binder, options)
	routes, err := client.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Abort()
	want := bytes.Repeat([]byte{0, 1, 2, 3, 4}, 1000)
	result, err := routes.Call(context.Background(), Call{Method: method, Arguments: []Value{{Type: "[]uint8", Data: want}}})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Discard(context.Background())
	got, ok := result.Values[0].Data.([]byte)
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("fragmented result = %#v", result.Values)
	}
}

func TestEndpointOversizedCallDoesNotCloseConnection(t *testing.T) {
	method := Method{ID: "example/size::Service.Echo", Service: "example/size::Service", Name: "Echo", ContractHash: testContractHash}
	binder := newTestBinder(testBinderOptions{})
	if err := binder.Register(newTestProvider(t, method, func(_ context.Context, arguments []Value) ([]Value, error) {
		return arguments, nil
	})); err != nil {
		t.Fatal(err)
	}
	options := EndpointOptions{Limits: Limits{MaxFrameBytes: 256, MaxMessageBytes: 2048, MaxInFlightBytes: 4096}}
	client, _ := openEndpointPair(t, nil, binder, options)
	routes, err := client.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Abort()
	_, err = routes.Call(context.Background(), Call{Method: method, Arguments: []Value{{Type: "[]uint8", Data: make([]byte, 4096)}}})
	if codeOf(err) != CodeResourceExhausted {
		t.Fatalf("oversized call error = %v", err)
	}
	result, err := routes.Call(context.Background(), Call{Method: method, Arguments: []Value{{Type: "string", Data: "still open"}}})
	if err != nil {
		t.Fatalf("small call after oversized call: %v", err)
	}
	_ = result.Discard(context.Background())
}

func TestEndpointOversizedResultDoesNotCloseConnection(t *testing.T) {
	method := Method{ID: "example/result-size::Service.Value", Service: "example/result-size::Service", Name: "Value", ContractHash: testContractHash}
	binder := newTestBinder(testBinderOptions{})
	if err := binder.Register(newTestProvider(t, method, func(_ context.Context, arguments []Value) ([]Value, error) {
		if len(arguments) != 0 && arguments[0].Data == "large" {
			return []Value{{Type: "[]uint8", Data: make([]byte, 4096)}}, nil
		}
		return []Value{{Type: "string", Data: "small"}}, nil
	})); err != nil {
		t.Fatal(err)
	}
	options := EndpointOptions{Limits: Limits{MaxFrameBytes: 256, MaxMessageBytes: 2048, MaxInFlightBytes: 4096}}
	client, _ := openEndpointPair(t, nil, binder, options)
	routes, err := client.Bind(context.Background(), testBindRequest(method))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Abort()
	_, err = routes.Call(context.Background(), Call{Method: method, Arguments: []Value{{Type: "string", Data: "large"}}})
	if codeOf(err) != CodeResourceExhausted {
		t.Fatalf("oversized result error = %v", err)
	}
	result, err := routes.Call(context.Background(), Call{Method: method})
	if err != nil {
		t.Fatalf("small call after oversized result: %v", err)
	}
	defer result.Discard(context.Background())
	if len(result.Values) != 1 || result.Values[0].Data != "small" {
		t.Fatalf("small result = %#v", result.Values)
	}
}

func TestEndpointFragmentDecoderRejectsMalformedPayload(t *testing.T) {
	limits := normalizeLimits(Limits{MaxFrameBytes: 128, MaxMessageBytes: 1024, MaxInFlightBytes: 1024})
	valid, err := encodeEndpointFragment(1, 3, 0, []byte("abc"), limits.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	wrongVersion := append([]byte(nil), valid...)
	wrongVersion[len(endpointFragmentMagic)]++
	overMessage, err := encodeEndpointFragment(1, 2048, 0, []byte("a"), limits.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{
		nil,
		[]byte(endpointFragmentMagic),
		append([]byte("BAD!"), valid[len(endpointFragmentMagic):]...),
		wrongVersion,
		valid[:len(endpointFragmentMagic)+2],
		overMessage,
	} {
		if _, err := decodeEndpointFragment(payload, limits); err == nil {
			t.Fatalf("fragment decoder accepted %x", payload)
		}
	}
}

func TestEndpointFragmentAssemblyRejectsInvalidSequence(t *testing.T) {
	limits := normalizeLimits(Limits{MaxFrameBytes: 128, MaxMessageBytes: 1024, MaxInFlightBytes: 1024})
	encoded, err := encodeEndpointFragment(1, 6, 0, []byte("abc"), limits.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := decodeEndpointFragment(encoded, limits)
	if err != nil {
		t.Fatal(err)
	}
	var assembly endpointAssembly
	var previous uint64
	message, err := assembly.append(fragment, &previous)
	if err != nil || message != nil {
		t.Fatalf("first fragment: message=%q err=%v", message, err)
	}
	encoded, err = encodeEndpointFragment(1, 6, 4, []byte("ef"), limits.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err = decodeEndpointFragment(encoded, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assembly.append(fragment, &previous); err == nil {
		t.Fatal("assembly accepted a non-contiguous fragment")
	}
	assembly = endpointAssembly{}
	previous = 1
	encoded, err = encodeEndpointFragment(3, 3, 0, []byte("abc"), limits.MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err = decodeEndpointFragment(encoded, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assembly.append(fragment, &previous); err == nil {
		t.Fatal("assembly accepted a skipped message ID")
	}
}

func FuzzEndpointFragmentDecoder(f *testing.F) {
	limits := normalizeLimits(Limits{MaxFrameBytes: 1024, MaxMessageBytes: 4096, MaxInFlightBytes: 4096})
	seed, _ := encodeEndpointFragment(1, 3, 0, []byte("abc"), limits.MaxFrameBytes)
	f.Add(seed)
	f.Add([]byte(endpointFragmentMagic))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > limits.MaxFrameBytes {
			t.Skip()
		}
		fragment, err := decodeEndpointFragment(payload, limits)
		if err != nil {
			return
		}
		encoded, err := encodeEndpointFragment(fragment.messageID, int(fragment.total), int(fragment.offset), fragment.data, limits.MaxFrameBytes)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(payload, encoded) {
			t.Fatal("accepted non-canonical fragment")
		}
	})
}
