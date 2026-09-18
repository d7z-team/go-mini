package runtime

import "testing"

func TestWaitTokenCancelRemovesResourceRegistrations(t *testing.T) {
	resource := &waitableResource{}
	token := &waitTokenState{ID: 1}
	value := newVMValue("Waitable<Int64>", resource)
	if err := waitableWaitRecvValue(&moduleInstance{}, value, newVMValue("WaitToken", token)); err != nil {
		t.Fatalf("register receive waiter failed: %v", err)
	}

	if err := cancelWaitToken(newVMValue("WaitToken", token)); err != nil {
		t.Fatalf("cancelWaitToken failed: %v", err)
	}
	if len(resource.RecvWaiters) != 0 {
		t.Fatalf("canceled token remains registered: %#v", resource.RecvWaiters)
	}
	if len(token.registrations) != 0 {
		t.Fatalf("canceled token retains registrations: %#v", token.registrations)
	}
}

func TestWaitTokenSignalRemovesAllResourceRegistrations(t *testing.T) {
	first := &waitableResource{}
	second := &waitableResource{}
	token := &waitTokenState{ID: 2}
	if err := waitableWaitRecvValue(&moduleInstance{}, newVMValue("Waitable<Int64>", first), newVMValue("WaitToken", token)); err != nil {
		t.Fatalf("register receive waiter failed: %v", err)
	}
	if err := waitableWaitSendValue(&moduleInstance{}, newVMValue("Waitable<Int64>", second), newVMValue("WaitToken", token)); err != nil {
		t.Fatalf("register send waiter failed: %v", err)
	}

	waitableSignalOneRecvWaiter(first)

	if !token.Signaled {
		t.Fatal("resource signal did not signal token")
	}
	if len(first.RecvWaiters) != 0 || len(second.SendWaiters) != 0 {
		t.Fatalf("signaled token remains registered: first=%#v second=%#v", first.RecvWaiters, second.SendWaiters)
	}
	if len(token.registrations) != 0 {
		t.Fatalf("signaled token retains registrations: %#v", token.registrations)
	}
}

func TestWaitSetCancelRemovesAllResourceRegistrations(t *testing.T) {
	first := &waitableResource{}
	second := &waitableResource{}
	token := &waitTokenState{ID: 3}
	if err := waitableWaitRecvValue(&moduleInstance{}, newVMValue("Waitable<Int64>", first), newVMValue("WaitToken", token)); err != nil {
		t.Fatalf("register receive waiter failed: %v", err)
	}
	if err := waitableWaitSendValue(&moduleInstance{}, newVMValue("Waitable<Int64>", second), newVMValue("WaitToken", token)); err != nil {
		t.Fatalf("register send waiter failed: %v", err)
	}
	waitSet := &waitSetState{Tokens: []*waitTokenState{token}}

	if err := cancelWaitSet(newVMValue("WaitSet", waitSet)); err != nil {
		t.Fatalf("cancelWaitSet failed: %v", err)
	}
	if len(first.RecvWaiters) != 0 || len(second.SendWaiters) != 0 {
		t.Fatalf("wait-set cancellation left resource registrations: first=%#v second=%#v", first.RecvWaiters, second.SendWaiters)
	}
	if len(token.registrations) != 0 {
		t.Fatalf("wait-set cancellation left token registrations: %#v", token.registrations)
	}
	if len(waitSet.Tokens) != 0 {
		t.Fatalf("wait-set cancellation retained tokens: %#v", waitSet.Tokens)
	}
}

func TestAbortTaskCancelsBlockedWaitableOperations(t *testing.T) {
	machine := &executionMachine{}

	sendResource := &waitableResource{
		Pending:   []pendingWaitableSend{{TaskID: 7}, {TaskID: 8}},
		Completed: map[int64]struct{}{7: {}, 8: {}},
	}
	machine.abortTask(&executionTask{id: 7, blocked: &blockedOperation{kind: "send", resource: sendResource}})
	if len(sendResource.Pending) != 1 || sendResource.Pending[0].TaskID != 8 {
		t.Fatalf("pending sends after abort = %#v", sendResource.Pending)
	}
	if _, ok := sendResource.Completed[7]; ok {
		t.Fatal("aborted send retained completion state")
	}

	recvResource := &waitableResource{}
	recvToken := &waitTokenState{ID: 9}
	recvTokenValue := newVMValue("WaitToken", recvToken)
	if err := waitableWaitRecvValue(&moduleInstance{}, newVMValue("Waitable<Int64>", recvResource), recvTokenValue); err != nil {
		t.Fatalf("register receive waiter failed: %v", err)
	}
	recvTask := &executionTask{id: 9, blocked: &blockedOperation{kind: "recv", recvToken: recvTokenValue}}
	machine.abortTask(recvTask)
	if recvTask.blocked != nil || !recvToken.Canceled || len(recvResource.RecvWaiters) != 0 {
		t.Fatalf("receive abort retained state: task=%#v token=%#v waiters=%#v", recvTask.blocked, recvToken, recvResource.RecvWaiters)
	}

	waitResource := &waitableResource{}
	waitToken := &waitTokenState{ID: 10}
	waitTokenValue := newVMValue("WaitToken", waitToken)
	if err := waitableWaitRecvValue(&moduleInstance{}, newVMValue("Waitable<Int64>", waitResource), waitTokenValue); err != nil {
		t.Fatalf("register wait-set token failed: %v", err)
	}
	waitSet := &waitSetState{Tokens: []*waitTokenState{waitToken}}
	waitTask := &executionTask{id: 10, blocked: &blockedOperation{kind: "waitset", waitSet: newVMValue("WaitSet", waitSet)}}
	machine.abortTask(waitTask)
	if waitTask.blocked != nil || len(waitSet.Tokens) != 0 || len(waitResource.RecvWaiters) != 0 {
		t.Fatalf("wait-set abort retained state: task=%#v set=%#v waiters=%#v", waitTask.blocked, waitSet.Tokens, waitResource.RecvWaiters)
	}

	canceled := false
	vm := &vm{ffiCalls: map[*pendingFFICall]struct{}{}}
	call := &pendingFFICall{vm: vm, cancel: func() { canceled = true }}
	vm.ffiCalls[call] = struct{}{}
	vm.pendingEvents.Add(1)
	ffiTask := &executionTask{id: 11, blocked: &blockedOperation{kind: "ffi", ffi: call}}
	machine.abortTask(ffiTask)
	if ffiTask.blocked != nil || !canceled || call.state != pendingFFICanceled || call.vm != nil || len(vm.ffiCalls) != 0 || vm.pendingEvents.Load() != 0 {
		t.Fatalf("FFI abort retained state: task=%#v canceled=%t call=%#v calls=%d pending=%d", ffiTask.blocked, canceled, call, len(vm.ffiCalls), vm.pendingEvents.Load())
	}
}

func TestWaitableCloseDropsBlockedSendsButKeepsAcceptedValues(t *testing.T) {
	module := &moduleInstance{}
	resource := &waitableResource{
		ElemType: runtimeTypeFromText("Int64"),
		Buffer:   []vmValue{newVMValue("Int64", int64(1))},
		Pending: []pendingWaitableSend{
			{Value: newVMValue("Int64", int64(2))},
			{Value: newVMValue("Int64", int64(3)), TaskID: 12},
		},
	}
	waitable := newVMValue("Waitable<Int64>", resource)
	if err := waitableCloseValue(module, waitable); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if len(resource.Pending) != 1 || resource.Pending[0].TaskID != 0 {
		t.Fatalf("pending sends after close = %#v", resource.Pending)
	}
	for _, want := range []int64{1, 2} {
		value, ok, _, err := waitableTryRecvValue(module, waitable)
		if err != nil || !ok {
			t.Fatalf("receive accepted value failed: ok=%t err=%v", ok, err)
		}
		if got, _ := asInt64(value); got != want {
			t.Fatalf("received %d, want %d", got, want)
		}
	}
	if _, ok, closed, err := waitableTryRecvValue(module, waitable); err != nil || ok || !closed {
		t.Fatalf("closed empty receive = ok %t closed %t err %v", ok, closed, err)
	}
}
