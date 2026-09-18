package rpc

import (
	"context"
	"testing"
)

func TestProviderCallInfoUsesSelectedProviderAndImmutablePeer(t *testing.T) {
	method := Method{ID: "example.callinfo::Service.Call", Service: "example.callinfo::Service", Name: "Call", ContractHash: testContractHash}
	peer := PeerInfo{Identity: "client", Attributes: map[string]string{"role": "worker"}}
	provider, err := NewProvider(MethodBinding{Method: method, Invoke: func(ctx context.Context, _ []Value) ([]Value, error) {
		info, ok := CallInfoFromContext(ctx)
		if !ok || info.Method != method || info.Provider != "selected" || info.Peer.Identity != "client" || info.Peer.Attributes["role"] != "worker" {
			t.Fatalf("call info = %#v, %t", info, ok)
		}
		info.Peer.Attributes["role"] = "changed"
		return nil, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := provider.BindRPC(context.Background(), BindRequest{Contract: testContract(method), Peer: peer, Provider: "selected"})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if _, err := lease.Invoke(context.Background(), method, nil); err != nil {
		t.Fatal(err)
	}
	if peer.Attributes["role"] != "worker" {
		t.Fatalf("caller peer was mutated: %#v", peer)
	}
}
