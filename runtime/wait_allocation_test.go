package runtime

import (
	"context"
	"errors"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestWaitSetInstructionGrowthCensusAndCancellation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "growth", true: "census-failure"}[fail], func(t *testing.T) {
			artifact := ir.NewArtifact("wait/allocation", "main")
			instructions := []ir.Instruction{
				{Op: string(ir.OpMakeWaitToken)},
				{Op: string(ir.OpStoreLocal), Payload: testPayload(ir.LocalPayload{Local: "token"})},
				{Op: string(ir.OpMakeWaitSet)},
			}
			for range 4 {
				instructions = append(instructions, ir.Instruction{Op: string(ir.OpLoadLocal), Payload: testPayload(ir.LocalPayload{Local: "token"})}, ir.Instruction{Op: string(ir.OpWaitSetAdd)})
			}
			instructions = append(instructions, ir.Instruction{Op: string(ir.OpWaitSetCancel)}, ir.Instruction{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})})
			artifact.Functions = []ir.Function{{ID: "fn.entry", Signature: testSignature("function() Void"), Locals: []ir.Local{{ID: "token", Type: testType("WaitToken")}}, Instructions: instructions}}
			instance, err := patchTestProgram(t, artifact, "wait-census").Instantiate(context.Background(), InstanceOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			execution, err := instance.Start("run")
			if err != nil {
				t.Fatal(err)
			}
			if state, steps, err := execution.PollSteps(4); err != nil || state != ExecutionRunning || steps != 4 {
				t.Fatalf("setup: %v %d %v", state, steps, err)
			}
			frame := instance.vm.machine.runnableTasks()[0].frames[0].frame
			set := frame.stack[0].Data.(*waitSetState)
			token := frame.stack[1].Data.(*waitTokenState)
			if fail {
				baseline := instance.vm.refreshLiveGuestBytes()
				instance.vm.limits.MaxAllocatedBytes = baseline + ir.RuntimeSlotBytes - 1
				instance.vm.allocatedSinceSweep.Store(instance.vm.limits.MaxAllocatedBytes)
				if _, _, err := execution.PollSteps(1); err == nil {
					t.Fatal("unpaid growth survived census")
				}
				if len(set.Tokens) != 0 || cap(set.Tokens) != 0 {
					t.Fatal("failure changed backing")
				}
				return
			}
			before := instance.vm.totalAllocatedBytes.Load()
			for index := range 4 {
				steps := 2
				if index == 0 {
					steps = 1
				}
				if _, executed, err := execution.PollSteps(steps); err != nil || executed != steps {
					t.Fatalf("add: %d %v", executed, err)
				}
				if len(set.Tokens) != index+1 || set.Tokens[index] != token {
					t.Fatal("duplicate token order changed")
				}
			}
			if charged := instance.vm.totalAllocatedBytes.Load() - before; charged != 4*ir.RuntimeSlotBytes {
				t.Fatalf("backing charge: %d", charged)
			}
			if _, _, err := execution.PollSteps(1); err != nil {
				t.Fatal(err)
			}
			if set.Tokens != nil || !token.Canceled || len(token.registrations) != 0 {
				t.Fatal("cancellation retained token roots")
			}
		})
	}
}

func TestWaitSetGrowthChargesBeforeMutation(t *testing.T) {
	machine := &vm{limits: normalizeLimits(Limits{MaxAllocatedBytes: ir.RuntimeSlotBytes, MaxCollectionElements: 2})}
	set := newWaitSetValue()
	token := machine.newWaitTokenValue()
	if _, err := machine.addWaitSetToken(set, token); err != nil {
		t.Fatal(err)
	}
	state := set.Data.(*waitSetState)
	if len(state.Tokens) != 1 || machine.totalAllocatedBytes.Load() != ir.RuntimeSlotBytes {
		t.Fatal("first backing slot was not charged")
	}
	_, err := machine.addWaitSetToken(set, token)
	var limit ResourceLimitError
	if !errors.As(err, &limit) || limit.Code != "execution.allocation_limit" {
		t.Fatalf("growth error = %v", err)
	}
	if len(state.Tokens) != 1 || cap(state.Tokens) != 1 || state.Tokens[0] != token.Data.(*waitTokenState) {
		t.Fatal("failed growth changed the wait set")
	}
}

func TestWaitSetCollectionLimit(t *testing.T) {
	machine := &vm{limits: normalizeLimits(Limits{MaxCollectionElements: 1})}
	set := newWaitSetValue()
	token := machine.newWaitTokenValue()
	if _, err := machine.addWaitSetToken(set, token); err != nil {
		t.Fatal(err)
	}
	if _, err := machine.addWaitSetToken(set, token); err == nil {
		t.Fatal("wait set exceeded collection limit")
	}
	if len(set.Data.(*waitSetState).Tokens) != 1 {
		t.Fatal("collection failure changed wait set")
	}
}

func TestWaiterRegistrationFailurePreservesBothOwnersDuringCensus(t *testing.T) {
	machine := &vm{limits: normalizeLimits(Limits{MaxAllocatedBytes: 2*ir.RuntimeNodeBytes + 2*ir.RuntimeSlotBytes - 1})}
	machine.owner.Store(true)
	machine.allocatedSinceSweep.Store(machine.limits.MaxAllocatedBytes)
	resource := &waitableResource{Type: coerceRuntimeType("Waitable<Int64>")}
	token := &waitTokenState{ID: 1}
	err := reserveWaiter(machine, resource, token, waitDirectionReceive)
	var limit ResourceLimitError
	if !errors.As(err, &limit) || limit.Code != "execution.allocation_limit" {
		t.Fatalf("reservation: %v", err)
	}
	if len(resource.RecvWaiters) != 0 || cap(resource.RecvWaiters) != 0 || len(token.registrations) != 0 || machine.allocationRoots != nil {
		t.Fatal("failed reservation mutated ownership")
	}
	if machine.liveGuestBytes.Load() != 2*ir.RuntimeNodeBytes {
		t.Fatalf("census lost temporary owners: %d", machine.liveGuestBytes.Load())
	}
}

func TestPendingSendBudgetFailureKeepsQueuedValue(t *testing.T) {
	machine := &vm{limits: normalizeLimits(Limits{MaxAllocatedBytes: ir.RuntimeSlotBytes})}
	resource := &waitableResource{}
	first := pendingWaitableSend{Value: newVMValue("Int64", int64(1)), TaskID: 1}
	if err := resource.appendPending(machine, first); err != nil {
		t.Fatal(err)
	}
	if err := resource.appendPending(machine, pendingWaitableSend{Value: newVMValue("Int64", int64(2)), TaskID: 2}); err == nil {
		t.Fatal("unpaid pending send accepted")
	}
	if resource.pendingLen() != 1 || resource.Pending[0].TaskID != 1 {
		t.Fatal("failed pending send changed queue")
	}
}
