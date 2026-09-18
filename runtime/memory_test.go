package runtime

import (
	"errors"
	"testing"
)

func TestAllocationLimitSweepsDiscardedGuestValues(t *testing.T) {
	machine := &vm{limits: normalizeLimits(Limits{MaxAllocatedBytes: 512})}
	machine.owner.Store(true)
	for range 100 {
		if err := machine.chargeAllocationBytes(128); err != nil {
			t.Fatalf("discarded allocation exhausted live budget: %v", err)
		}
	}
	if got := machine.refreshLiveGuestBytes(); got != 0 {
		t.Fatalf("live guest bytes = %d, want 0", got)
	}
	if got := machine.totalAllocatedBytes.Load(); got != 12_800 {
		t.Fatalf("total allocated bytes = %d, want 12800", got)
	}
}

func TestAllocationLimitRejectsRetainedGuestValues(t *testing.T) {
	cell := &slot{initialized: true, value: newVMValue("String", string(make([]byte, 513)))}
	module := &moduleInstance{state: &moduleState{globals: map[string]*slot{"value": cell}}}
	registry := &moduleRegistry{modules: map[string]*moduleInstance{"test": module}}
	machine := &vm{limits: normalizeLimits(Limits{MaxAllocatedBytes: 512})}
	machine.owner.Store(true)
	machine.revision.Store(&instanceRevision{modules: registry})
	machine.allocatedSinceSweep.Store(512)
	err := machine.chargeAllocationBytes(1)
	var limit ResourceLimitError
	if !errors.As(err, &limit) || limit.Code != "execution.allocation_limit" {
		t.Fatalf("retained allocation error = %T %v", err, err)
	}
}

func TestHostResultDoesNotConsumeGuestBudget(t *testing.T) {
	limits := normalizeLimits(Limits{MaxAllocatedBytes: 1, MaxStringBytes: 1024})
	result, err := hostResult(vmResult{Values: []vmValue{newVMValue("String", "host-owned")}}, limits)
	if err != nil || len(result.Values) != 1 {
		t.Fatalf("host result = %#v, %v", result, err)
	}
	text, ok := result.Values[0].StringValue()
	if !ok || text != "host-owned" {
		t.Fatalf("host result = %#v, %v", result, err)
	}
}

func TestWaitableQueueClearsConsumedSlots(t *testing.T) {
	resource := &waitableResource{Capacity: 2, Buffer: make([]vmValue, 0, 2)}
	resource.appendBuffer(newVMValue("String", "first"))
	resource.appendBuffer(newVMValue("String", "second"))
	if got := resource.popBuffer().Data; got != "first" {
		t.Fatalf("first value = %v", got)
	}
	if resource.Buffer[0].Data != nil {
		t.Fatalf("consumed queue slot retained %#v", resource.Buffer[0])
	}
	resource.appendBuffer(newVMValue("String", "third"))
	if resource.bufferHead != 0 || resource.bufferLen() != 2 {
		t.Fatalf("compacted queue = head %d length %d", resource.bufferHead, resource.bufferLen())
	}
	if got := resource.popBuffer().Data; got != "second" {
		t.Fatalf("second value = %v", got)
	}
	if got := resource.popBuffer().Data; got != "third" {
		t.Fatalf("third value = %v", got)
	}
}

func TestSliceViewsShareLogicalBacking(t *testing.T) {
	root := newSliceValue("Slice<String>", []vmValue{newVMValue("String", "a"), newVMValue("String", "b")})
	header := root.Data.(*vmSlice)
	view := newSliceViewValue("Slice<String>", header, 1, 1, 1)
	sizer := newRuntimeValueSizer()
	sizer.value(root)
	rootBytes := sizer.bytes
	sizer.value(view)
	if added := sizer.bytes - rootBytes; added != 128 {
		t.Fatalf("slice view added %d bytes, want header only", added)
	}
}

func TestRuntimeValueSizerHandlesDeepGraph(t *testing.T) {
	value := newVMValue("String", "leaf")
	for range 10_000 {
		value = newVMValue("Any", value)
	}
	sizer := newRuntimeValueSizer()
	sizer.value(value)
	if sizer.bytes <= 0 || len(sizer.pendingValues) != 0 || sizer.walking {
		t.Fatalf("deep graph scan did not settle: bytes=%d pending=%d walking=%v", sizer.bytes, len(sizer.pendingValues), sizer.walking)
	}
}

func FuzzRuntimeValueSizerTerminatesOnCycles(f *testing.F) {
	f.Add(uint8(3), "value")
	f.Fuzz(func(t *testing.T, entries uint8, text string) {
		if entries > 32 {
			entries = 32
		}
		valueMap := newVMMap(int(entries))
		root := newVMValue("Map<String,Any>", valueMap)
		for index := uint8(0); index < entries; index++ {
			key := vmMapKey{Kind: vmMapKeyString, Text: string(rune(index))}
			valueMap.storeEntry(key, vmMapEntry{Key: newVMValue("String", key.Text), Value: newVMValue("Any", root)})
		}
		valueMap.storeEntry(vmMapKey{Kind: vmMapKeyString, Text: "text"}, vmMapEntry{Key: newVMValue("String", "text"), Value: newVMValue("String", text)})
		first := newRuntimeValueSizer()
		first.value(root)
		second := newRuntimeValueSizer()
		second.value(root)
		if first.bytes != second.bytes || first.bytes < 0 {
			t.Fatalf("unstable logical size %d/%d", first.bytes, second.bytes)
		}
	})
}
