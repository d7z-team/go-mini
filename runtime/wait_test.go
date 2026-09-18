package runtime

import "testing"

func TestWaitSetSelectionUsesAllReadyCases(t *testing.T) {
	machine := &vm{}
	waitSet := &waitSetState{Tokens: []*waitTokenState{{Signaled: true}, {Signaled: true}}}
	seen := map[int]bool{}
	for range 32 {
		seen[waitSetReadyIndex(machine, waitSet)] = true
	}
	if !seen[0] || !seen[1] {
		t.Fatalf("ready selections = %#v, want both cases", seen)
	}
}
