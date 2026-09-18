package rpc

import (
	"bytes"
	"testing"
)

func TestFFICodecRoundTripAndCanonicalLabels(t *testing.T) {
	method := Method{ID: "example::Service.Call", Service: "example::Service", Name: "Call", ContractHash: testContractHash}
	request := ffiRequest{
		Version: FFIProtocol, Operation: "call", Target: "node", Lease: 7,
		Contract: Contract{Protocol: ContractProtocol, Methods: []Method{method}},
		Options:  BindOptions{AffinityKey: "account", Labels: map[string]string{"zone": "b", "role": "primary"}},
		Method:   method, Receiver: &ResourceRef{Epoch: 3, ObjectID: 5, TypeHash: testContractHash},
		Payload: []byte("payload"), Resource: &ResourceRef{Epoch: 3, ObjectID: 8, TypeHash: testContractHash},
		RequestID: 11, Code: CodeCanceled, Message: "stopped", TimeoutNanos: 5_000_000,
	}
	encoded := encodeFFIRequest(request)
	decoded, err := decodeFFIRequest(encoded, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != request.Version || decoded.Operation != request.Operation || decoded.Lease != request.Lease || decoded.RequestID != request.RequestID ||
		decoded.Method != method || decoded.TimeoutNanos != request.TimeoutNanos || !bytes.Equal(decoded.Payload, request.Payload) || decoded.Options.Labels["role"] != "primary" {
		t.Fatalf("decoded request = %#v", decoded)
	}
	reordered := request
	reordered.Options.Labels = map[string]string{"role": "primary", "zone": "b"}
	if !bytes.Equal(encoded, encodeFFIRequest(reordered)) {
		t.Fatal("FFI request label order changed canonical encoding")
	}
	response := ffiResponse{Version: FFIProtocol, Operation: "call", Lease: 9, RequestID: 13, Method: method, Receiver: request.Receiver, Payload: []byte{1, 2}, Code: CodeUnavailable, Message: "closed"}
	decodedResponse, err := decodeFFIResponse(encodeFFIResponse(response))
	if err != nil || decodedResponse.Version != response.Version || decodedResponse.Operation != response.Operation || decodedResponse.Lease != response.Lease || decodedResponse.RequestID != response.RequestID || decodedResponse.Method != response.Method ||
		decodedResponse.Code != response.Code || decodedResponse.Message != response.Message || !bytes.Equal(decodedResponse.Payload, response.Payload) {
		t.Fatalf("decoded response = %#v, %v", decodedResponse, err)
	}
}

func FuzzFFIResponseCodec(f *testing.F) {
	f.Add(encodeFFIResponse(ffiResponse{Version: FFIProtocol, Operation: "call", RequestID: 1}))
	f.Add([]byte{0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		response, err := decodeFFIResponse(data)
		if err != nil {
			return
		}
		again, err := decodeFFIResponse(encodeFFIResponse(response))
		if err != nil || again.Version != response.Version || again.Operation != response.Operation || again.RequestID != response.RequestID {
			t.Fatalf("canonical response = %#v, %v", again, err)
		}
	})
}

func FuzzFFICodec(f *testing.F) {
	f.Add(encodeFFIRequest(ffiRequest{Version: FFIProtocol, Operation: "close", Lease: 1}))
	f.Add([]byte{0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		request, err := decodeFFIRequest(data, Limits{MaxMessageBytes: 1 << 20})
		if err != nil {
			return
		}
		again, err := decodeFFIRequest(encodeFFIRequest(request), Limits{MaxMessageBytes: 1 << 20})
		if err != nil || again.Version != request.Version || again.Operation != request.Operation || again.Lease != request.Lease || again.TimeoutNanos != request.TimeoutNanos {
			t.Fatalf("canonical request = %#v, %v", again, err)
		}
	})
}
