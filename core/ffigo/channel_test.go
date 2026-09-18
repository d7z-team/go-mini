package ffigo

import "testing"

func TestChannelRegistryUnregister(t *testing.T) {
	registry := NewChannelRegistry()
	id := registry.RegisterChannel(ChannelEndpointFuncs{Elem: "Int64", Dir: ChannelBoth})
	if id == 0 {
		t.Fatalf("expected channel id")
	}
	if _, ok := registry.LookupChannel(id); !ok {
		t.Fatalf("registered endpoint not found")
	}
	if !registry.UnregisterChannel(id) {
		t.Fatalf("expected unregister to remove endpoint")
	}
	if _, ok := registry.LookupChannel(id); ok {
		t.Fatalf("unregistered endpoint should not be found")
	}
	if registry.UnregisterChannel(id) {
		t.Fatalf("unregister should be idempotent")
	}
}
