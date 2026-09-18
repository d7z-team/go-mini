package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/d7z-team/mini-go/compiler/types"
	"github.com/d7z-team/mini-go/ffi"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestFFICallStatusAndSettlement(t *testing.T) {
	program := patchTestProgram(t, patchFFIArtifact(), "ffi-status")
	for _, test := range []struct {
		name   string
		bridge ffi.Bridge
		status int64
	}{
		{name: "no session", status: 1},
		{name: "missing route", status: 1, bridge: ffi.CallFunc(func(context.Context, ffi.Request, ffi.Completion) (ffi.Call, error) {
			return nil, fmt.Errorf("missing: %w", ffi.ErrRouteUnavailable)
		})},
		{name: "ordinary failure", status: 2, bridge: ffi.CallFunc(func(context.Context, ffi.Request, ffi.Completion) (ffi.Call, error) {
			return nil, errors.New("permission denied")
		})},
		{name: "completed", bridge: ffi.CallFunc(func(_ context.Context, _ ffi.Request, done ffi.Completion) (ffi.Call, error) {
			done(ffi.Result{Payload: []byte("ok")})
			return nil, nil
		})},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, err := program.Instantiate(context.Background(), InstanceOptions{FFI: test.bridge})
			if err != nil {
				t.Fatal(err)
			}
			cleanupTestInstance(t, instance)
			result, err := instance.Call(context.Background(), "run")
			if err != nil || len(result.Values) != 3 {
				t.Fatalf("call = %#v, %v", result, err)
			}
			if status, ok := result.Values[2].Int64(); !ok || status != test.status {
				t.Fatalf("status = %#v", result.Values[2])
			}
			stats, err := instance.RuntimeStats(context.Background())
			if err != nil || stats.PendingFFICalls != 0 || instance.vm.pendingEvents.Load() != 0 {
				t.Fatalf("settled state = %#v, %v", stats, err)
			}
		})
	}
}

func TestFFIPayloadBytesRequiresStructuredByteSliceType(t *testing.T) {
	table := &types.TypeTable{}
	ref, err := types.NewParser("ffi-test", table).Parse("Slice<Uint8>")
	if err != nil {
		t.Fatal(err)
	}
	byteSliceType := runtimeTypeWithTable(ref, table)

	tests := []struct {
		name  string
		value vmValue
		want  string
	}{
		{name: "nil", value: newVMValue(byteSliceType, (*vmSlice)(nil))},
		{name: "empty", value: newByteSliceHeaderValue(byteSliceType, []byte{}, 0, 0, 0)},
		{name: "bytes", value: newByteSliceHeaderValue(byteSliceType, []byte("mini"), 0, 4, 4), want: "mini"},
		{name: "values", value: newSliceValue(byteSliceType, []vmValue{newVMValue("Uint8", uint64('g')), newVMValue("Uint8", uint64('o'))}), want: "go"},
	}
	for _, test := range tests {
		got, err := ffiPayloadBytes(nil, test.value)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if string(got) != test.want {
			t.Fatalf("%s: payload = %q, want %q", test.name, got, test.want)
		}
	}

	identity := types.TypeKey{ModulePath: "example/ffi", DeclID: "Bytes"}
	node := types.TypeNode{ID: "decl.Bytes", Kind: types.Named, Identity: identity, Underlying: ref}
	if err := table.Add(node); err != nil {
		t.Fatal(err)
	}
	defined := runtimeTypeWithTable(types.Ref(node), table)
	if payload, err := ffiPayloadBytes(nil, newVMValue(defined, (*vmSlice)(nil))); err != nil || payload != nil {
		t.Fatalf("defined nil byte slice = %q, %v", payload, err)
	}
	aliasNode := types.TypeNode{
		ID: "decl.ByteAlias", Kind: types.Named,
		Identity: types.TypeKey{ModulePath: "example/ffi", DeclID: "ByteAlias"},
		Alias:    true, AliasTarget: ref, Underlying: ref,
	}
	if err := table.Add(aliasNode); err != nil {
		t.Fatal(err)
	}
	alias := runtimeTypeWithTable(types.Ref(aliasNode), table)
	if payload, err := ffiPayloadBytes(nil, newVMValue(alias, (*vmSlice)(nil))); err != nil || payload != nil {
		t.Fatalf("alias nil byte slice = %q, %v", payload, err)
	}

	incomplete := vmType{Ref: ref, text: "Slice<Uint8>"}
	if _, err := ffiPayloadBytes(nil, newVMValue(incomplete, (*vmSlice)(nil))); err == nil {
		t.Fatal("tableless composite type was accepted through its display text")
	}
}

func TestPendingFFICallDetachesCompletionRelay(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	machine := &vm{ffiCalls: make(map[*pendingFFICall]struct{})}
	hostCancels := 0
	pending := &pendingFFICall{vm: machine, cancel: cancel, call: ffi.CancelFunc(func() { hostCancels++ })}
	relay := &ffiCompletionRelay{pending: pending}
	pending.relay = relay
	machine.ffiCalls[pending] = struct{}{}
	machine.pendingEvents.Add(1)

	pending.stop()
	pending.stop()
	relay.complete(ffi.Result{Payload: []byte("late")})

	relay.mu.Lock()
	attached := relay.pending != nil
	relay.mu.Unlock()
	if attached || pending.vm != nil || pending.scope != nil || pending.call != nil || pending.relay != nil {
		t.Fatalf("canceled call retained runtime state: %#v", pending)
	}
	if pending.state != pendingFFICanceled || len(machine.ffiCalls) != 0 || machine.pendingEvents.Load() != 0 || hostCancels != 1 {
		t.Fatalf("canceled call state=%d calls=%d pending=%d host cancels=%d", pending.state, len(machine.ffiCalls), machine.pendingEvents.Load(), hostCancels)
	}
}

func TestPendingFFICallRejectsOversizedResponse(t *testing.T) {
	machine := &vm{limits: normalizeLimits(Limits{MaxBoundaryBytes: 4})}
	pending := &pendingFFICall{vm: machine, state: pendingFFIWaiting}
	pending.complete(ffi.Result{Payload: []byte("large")})
	pending.mu.Lock()
	result, state := pending.result, pending.state
	pending.mu.Unlock()
	if state != pendingFFIReady || result.Err == nil || len(result.Payload) != 0 {
		t.Fatalf("oversized completion = state %d, result %#v", state, result)
	}
}

func TestPendingFFICallTransfersBoundaryPayloadToGuest(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	machine := &vm{limits: normalizeLimits(Limits{MaxBoundaryBytes: 32, MaxAllocatedBytes: 1 << 20}), ffiCalls: make(map[*pendingFFICall]struct{})}
	machine.owner.Store(true)
	pending := &pendingFFICall{vm: machine, cancel: cancel, state: pendingFFIWaiting, boundaryBytes: 4}
	relay := &ffiCompletionRelay{pending: pending}
	pending.relay = relay
	machine.ffiCalls[pending] = struct{}{}
	machine.pendingEvents.Store(1)
	machine.pendingBoundaryBytes.Store(4)
	payload := []byte("done")
	pending.complete(ffi.Result{Payload: payload})
	if got := machine.pendingBoundaryBytes.Load(); got != 8 {
		t.Fatalf("pending boundary bytes = %d, want 8", got)
	}
	values, ready := pending.take()
	if !ready || len(values) != 3 {
		t.Fatalf("take = %#v, %v", values, ready)
	}
	guest := values[0].Data.(*vmSlice)
	if &guest.ByteBacking[0] != &payload[0] {
		t.Fatal("FFI response payload was copied during ownership transfer")
	}
	if machine.pendingEvents.Load() != 0 || machine.pendingBoundaryBytes.Load() != 0 || len(machine.ffiCalls) != 0 {
		t.Fatalf("settled FFI retained pending state: events=%d bytes=%d calls=%d", machine.pendingEvents.Load(), machine.pendingBoundaryBytes.Load(), len(machine.ffiCalls))
	}
}

func TestStartFFICallRejectsOversizedRequest(t *testing.T) {
	machine := &vm{limits: normalizeLimits(Limits{MaxBoundaryBytes: 4}), activeRunID: 1}
	scheduler := &executionMachine{vm: machine, scopes: map[int64]*executionScope{
		1: {id: 1, done: make(chan struct{}), budget: &executionBudget{}},
	}}
	machine.machine = scheduler
	if _, err := machine.startFFICall("example", []byte("large")); err == nil {
		t.Fatal("oversized FFI request was accepted")
	}
	if machine.pendingEvents.Load() != 0 || len(machine.ffiCalls) != 0 {
		t.Fatalf("rejected FFI request changed runtime state: pending=%d calls=%d", machine.pendingEvents.Load(), len(machine.ffiCalls))
	}
}

func TestBackgroundFFICallOutlivesInvocationAndReleasesWithScope(t *testing.T) {
	artifact := lifecycleArtifact([]ir.Instruction{
		{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.route"})},
		{Op: string(ir.OpConst), Payload: testPayload(ir.ConstPayload{Constant: "const.payload"})},
		{Op: string(ir.OpCallFFI), Payload: testPayload(ir.CallFFIPayload{ArgCount: 2, ResultCount: 3})},
		{Op: string(ir.OpZero), Payload: testTypePayload("Int")},
		{Op: string(ir.OpBinary), Payload: testPayload(ir.OperatorPayload{Operator: "=="})},
		{Op: string(ir.OpStoreGlobal), Payload: testPayload(ir.GlobalPayload{Global: "global.completed"})},
		{Op: string(ir.OpPop)},
		{Op: string(ir.OpPop)},
		{Op: string(ir.OpReturn), Payload: testPayload(ir.ReturnPayload{})},
	})
	artifact.Constants = []ir.Constant{
		{ID: "const.route", Type: testType("String"), Value: json.RawMessage(`"background"`)},
		{ID: "const.payload", Type: testType("Slice<Uint8>"), Value: json.RawMessage(`null`)},
	}
	program := patchTestProgram(t, artifact, "background-ffi")
	completions := make(chan ffi.Completion, 2)
	var canceled atomic.Int32
	bridge := ffi.CallFunc(func(_ context.Context, request ffi.Request, complete ffi.Completion) (ffi.Call, error) {
		if request.Route != "background" {
			t.Fatalf("request route = %q", request.Route)
		}
		completions <- complete
		return ffi.CancelFunc(func() { canceled.Add(1) }), nil
	})
	instance, err := program.Instantiate(context.Background(), InstanceOptions{FFI: bridge})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)

	execution, err := instance.Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats, err := execution.ScopeStats(context.Background())
	if err != nil || stats.RootState != ExecutionCompleted || stats.Tasks != 1 || stats.FFICalls != 1 {
		t.Fatalf("pending FFI scope = %#v, %v", stats, err)
	}
	var complete ffi.Completion
	select {
	case complete = <-completions:
	case <-time.After(time.Second):
		t.Fatal("background FFI call was not started")
	}
	complete(ffi.Result{Payload: []byte("done")})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stats, err = execution.WaitScope(ctx)
	if err != nil || !stats.Done || stats.Tasks != 0 || stats.FFICalls != 0 {
		t.Fatalf("completed FFI scope = %#v, %v", stats, err)
	}
	if err := instance.vm.enterOwnerContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed := instance.vm.rootModule().state.globals["global.completed"].load()
	instance.vm.leaveOwner()
	if value, _ := completed.Data.(bool); !value {
		t.Fatalf("background completion flag = %#v", completed)
	}

	canceledExecution, err := instance.Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := canceledExecution.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case complete = <-completions:
	case <-time.After(time.Second):
		t.Fatal("second background FFI call was not started")
	}
	canceledExecution.Cancel()
	if _, err := canceledExecution.WaitScope(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait canceled FFI scope = %v", err)
	}
	complete(ffi.Result{Payload: []byte("late")})
	runtimeStats, err := instance.RuntimeStats(context.Background())
	if err != nil || runtimeStats.ActiveScopes != 0 || runtimeStats.PendingFFICalls != 0 || canceled.Load() != 1 {
		t.Fatalf("canceled FFI runtime = %#v, cancel=%d, %v", runtimeStats, canceled.Load(), err)
	}
}
