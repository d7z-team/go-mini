package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestEndpointFrameBinaryRoundTrip(t *testing.T) {
	limits := normalizeLimits(Limits{})
	wiredLimits := limitsToWire(limits)
	method := Method{
		ID: "example/service::Service.Value", Service: "example/service::Service", Name: "Value",
		ContractHash: testContractHash, ResourceTypeHash: testResourceHash,
	}
	ref := &ResourceRef{Epoch: 3, ObjectID: 5, TypeHash: testResourceHash}
	valuePayload, err := EncodeValues([]Value{{Type: "[]uint8", Data: []byte{0, 1, 2}}}, limits)
	if err != nil {
		t.Fatal(err)
	}
	frames := []endpointFrame{
		{Kind: "hello", Origin: "peer", Protocol: EndpointProtocol, Limits: &wiredLimits, LeaseTTL: int64(time.Minute), AdmissionTimeout: int64(10 * time.Second)},
		{Kind: "ready", Origin: "peer"},
		{Kind: "bind", Origin: "peer", ID: 1, Contract: &Contract{Protocol: ContractProtocol, Methods: []Method{method}}, Options: BindOptions{AffinityKey: "key", Labels: map[string]string{"b": "2", "a": "1"}}, Hops: 2},
		{Kind: "bind", Origin: "peer", Reply: true, TargetID: 1, Binding: 2, Epoch: 3},
		{Kind: "call", Origin: "peer", ID: 2, Binding: 2, Call: &wireCall{Method: method, Receiver: ref, Arguments: valuePayload}, Timeout: 10},
		{Kind: "call", Origin: "peer", Reply: true, TargetID: 4, Binding: 2, Values: valuePayload},
		{Kind: "decision", Origin: "peer", ID: 4, Binding: 2, TargetID: 4, Accept: true},
		{Kind: "drop", Origin: "peer", ID: 4, Binding: 2, Ref: ref},
		{Kind: "close", Origin: "peer", ID: 5, Binding: 2},
		{Kind: "cancel", Origin: "peer", TargetID: 5},
		{Kind: "renew", Origin: "peer", ID: 6, Values: []byte{0}},
		{Kind: "call", Origin: "peer", Reply: true, TargetID: 7, Code: CodeInternal, Message: "failed"},
	}
	for _, frame := range frames {
		t.Run(frame.Kind, func(t *testing.T) {
			encoded, err := encodeEndpointFrame(frame, limits.MaxMessageBytes)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(encoded, append([]byte(endpointFrameMagic), endpointFrameVersion)) {
				t.Fatalf("endpoint frame header = %x", encoded)
			}
			decoded, err := decodeEndpointFrame(encoded, limits)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, frame) {
				t.Fatalf("decoded frame differs:\n got: %#v\nwant: %#v", decoded, frame)
			}
			again, err := encodeEndpointFrame(decoded, limits.MaxMessageBytes)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, again) {
				t.Fatal("endpoint frame encoding is not canonical")
			}
		})
	}
}

func TestEndpointRejectsMalformedBinaryFrames(t *testing.T) {
	limits := normalizeLimits(Limits{})
	ready, err := encodeEndpointFrame(endpointFrame{Kind: "ready", Origin: "peer"}, limits.MaxMessageBytes)
	if err != nil {
		t.Fatal(err)
	}
	tests := [][]byte{
		nil,
		[]byte("{}"),
		ready[:len(ready)-1],
		append(append([]byte(nil), ready...), 0),
		append([]byte(nil), ready...),
	}
	tests[len(tests)-1][len(endpointFrameMagic)]++
	for _, payload := range tests {
		if _, err := decodeEndpointFrame(payload, limits); err == nil {
			t.Fatalf("decodeEndpointFrame accepted malformed frame %x", payload)
		}
	}
}

func TestEndpointLeaseTargetsRoundTrip(t *testing.T) {
	now := time.Now()
	bindings := map[uint64]time.Time{19: now.Add(time.Minute), 3: now.Add(time.Minute), 11: now.Add(time.Minute)}
	operations := map[uint64]time.Time{27: now.Add(time.Minute), 7: now.Add(time.Minute)}
	targets := encodeLeaseTargetList(collectLeaseTargets(bindings, operations, now))
	if got, want := hex.EncodeToString(targets), "050103010b01130207021b"; got != want {
		t.Fatalf("lease target encoding = %s, want canonical %s", got, want)
	}
	if again := encodeLeaseTargetList(collectLeaseTargets(bindings, operations, now)); !bytes.Equal(targets, again) {
		t.Fatal("lease target encoding is not deterministic")
	}
	decoded, err := decodeLeaseTargets(targets, NormalizeLimits(Limits{}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.bindings, []uint64{3, 11, 19}) || !reflect.DeepEqual(decoded.operations, []uint64{7, 27}) {
		t.Fatalf("lease targets = %#v", decoded)
	}
	if _, err := decodeLeaseTargets([]byte{1, 3, 0}, NormalizeLimits(Limits{})); err == nil {
		t.Fatal("accepted invalid lease target kind")
	}
}

func TestEndpointLeaseTargetBatchWireAndRotation(t *testing.T) {
	targets := []leaseTarget{
		{kind: leaseTargetBinding, id: 1},
		{kind: leaseTargetBinding, id: 2},
		{kind: leaseTargetBinding, id: 3},
		{kind: leaseTargetOperation, id: 4},
		{kind: leaseTargetOperation, id: 5},
	}
	for _, test := range []struct {
		name                string
		limit, offset, next int
		wire                []byte
	}{
		{"prefix", 2, 0, 2, []byte{2, 1, 1, 1, 2}},
		{"wrap", 2, 4, 1, []byte{2, 2, 5, 1, 1}},
		{"reduced target count", 2, 7, 4, []byte{2, 1, 3, 2, 4}},
		{"negative offset", 2, -3, 2, []byte{2, 1, 1, 1, 2}},
		{"whole set", 5, 3, 3, []byte{5, 1, 1, 1, 2, 1, 3, 2, 4, 2, 5}},
		{"unbounded", 0, 3, 3, []byte{5, 1, 1, 1, 2, 1, 3, 2, 4, 2, 5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			offset := test.offset
			payload := encodeLeaseTargetsBatch(targets, test.limit, &offset)
			if !reflect.DeepEqual(payload, test.wire) || offset != test.next {
				t.Fatalf("batch = %v, offset = %d; want %v, %d", payload, offset, test.wire, test.next)
			}
		})
	}
	if got := encodeLeaseTargetsBatch(targets, 2, nil); !reflect.DeepEqual(got, []byte{2, 1, 1, 1, 2}) {
		t.Fatalf("batch without cursor = %v", got)
	}
	if got := encodeLeaseTargetsBatch(nil, 2, nil); !reflect.DeepEqual(got, []byte{0}) {
		t.Fatalf("empty batch = %v", got)
	}
}

func TestEndpointLeaseTargetBatchCoversAllOwners(t *testing.T) {
	now := time.Now()
	bindings := map[uint64]time.Time{1: now.Add(time.Minute), 2: now.Add(time.Minute), 3: now.Add(time.Minute)}
	operations := map[uint64]time.Time{4: now.Add(time.Minute), 5: now.Add(time.Minute)}
	offset := 0
	seen := make(map[uint64]bool)
	for round := 0; round < 3; round++ {
		payload := encodeLeaseTargetsBatch(collectLeaseTargets(bindings, operations, now), 2, &offset)
		targets, err := decodeLeaseTargets(payload, NormalizeLimits(Limits{MaxPendingControls: 2}))
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range append(targets.bindings, targets.operations...) {
			seen[id] = true
		}
	}
	if len(seen) != len(bindings)+len(operations) {
		t.Fatalf("renewal batches covered %d owners, want %d", len(seen), len(bindings)+len(operations))
	}
}

func TestSharedLeaseTargetCorpus(t *testing.T) {
	data, err := os.ReadFile("../testdata/rpc/wire/leases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name       string   `json:"name"`
		Targets    string   `json:"targets"`
		Invalid    bool     `json:"invalid"`
		Bindings   []uint64 `json:"bindings"`
		Operations []uint64 `json:"operations"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			payload, err := hex.DecodeString(fixture.Targets)
			if err != nil {
				t.Fatal(err)
			}
			targets, err := decodeLeaseTargets(payload, NormalizeLimits(Limits{}))
			if fixture.Invalid {
				if err == nil {
					t.Fatal("invalid lease targets accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(targets.bindings, fixture.Bindings) && !(len(targets.bindings) == 0 && len(fixture.Bindings) == 0) ||
				!reflect.DeepEqual(targets.operations, fixture.Operations) && !(len(targets.operations) == 0 && len(fixture.Operations) == 0) {
				t.Fatalf("targets = %#v", targets)
			}
		})
	}
}

func FuzzEndpointFrameDecoder(f *testing.F) {
	limits := normalizeLimits(Limits{MaxMessageBytes: 1 << 20, MaxInFlightBytes: 1 << 20})
	ready, _ := encodeEndpointFrame(endpointFrame{Kind: "ready", Origin: "peer"}, limits.MaxMessageBytes)
	f.Add(ready)
	f.Add([]byte("{}"))
	f.Add([]byte(endpointFrameMagic))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > limits.MaxMessageBytes {
			t.Skip()
		}
		frame, err := decodeEndpointFrame(payload, limits)
		if err != nil {
			return
		}
		canonical, err := encodeEndpointFrame(frame, limits.MaxMessageBytes)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeEndpointFrame(canonical, limits)
		if err != nil {
			t.Fatal(err)
		}
		again, err := encodeEndpointFrame(decoded, limits.MaxMessageBytes)
		if err != nil || !bytes.Equal(canonical, again) {
			t.Fatal("canonical endpoint frame did not round trip")
		}
	})
}
