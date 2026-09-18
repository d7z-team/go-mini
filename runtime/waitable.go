package runtime

import (
	"errors"
	"fmt"

	"github.com/d7z-team/mini-go/compiler/types"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

// waitableResource is the runtime's concrete implementation of the language-neutral
// waitable resource protocol. Source-level resource/select semantics are
// lowered by the compiler; this object only stores resource state and waiters.
type waitableResource struct {
	Type        vmType
	ElemType    vmType
	Capacity    int
	Buffer      []vmValue
	bufferHead  int
	Pending     []pendingWaitableSend
	pendingHead int
	Completed   map[int64]struct{}
	RecvWaiters []*waitTokenState
	SendWaiters []*waitTokenState
	Closed      bool
}

type pendingWaitableSend struct {
	Value  vmValue
	TaskID int64
}

func makeWaitableValue(module *moduleInstance, typ any, capacity vmValue) (vmValue, error) {
	runtimeType := module.resolvedRuntimeType(typ)
	elemType := module.resolvedRuntimeType("Any")
	if info, ok := runtimeType.WaitableInfo(); ok {
		elemType = info.Elem
	}
	n, err := asInt64(capacity)
	if err != nil {
		return vmValue{}, err
	}
	if n < 0 {
		return vmValue{}, newGuestPanic(fmt.Errorf("waitable capacity must be non-negative, got %d", n))
	}
	_, capacityInt, err := module.vm.checkCollectionSize(0, n)
	if err != nil {
		return vmValue{}, err
	}
	if module.vm != nil {
		if err := module.vm.chargeRuntimeObject(capacityInt, 0); err != nil {
			return vmValue{}, err
		}
	}
	resource := &waitableResource{
		Type:     runtimeType,
		ElemType: elemType,
		Capacity: capacityInt,
		Buffer:   make([]vmValue, 0, capacityInt),
	}
	return newVMValue(runtimeType, resource), nil
}

func waitableSendTaskValue(module *moduleInstance, waitableValue, value vmValue, taskID int64) (*waitableResource, bool, error) {
	resource, err := waitableValueData(module, waitableValue)
	if err != nil {
		return nil, false, err
	}
	if !module.waitableCanSend(waitableValue.Type) {
		return nil, false, fmt.Errorf("cannot send on receive-only waitable %s", waitableValue.Type)
	}
	if resource == nil {
		return nil, true, nil
	}
	if resource.Closed {
		return resource, false, newGuestPanic(errors.New("send on closed waitable"))
	}
	normalized, err := normalizeWaitableSendValue(module, resource, value)
	if err != nil {
		return resource, false, err
	}
	if resource.Capacity == 0 || resource.bufferLen() >= resource.Capacity {
		if err := resource.appendPending(module.vm, pendingWaitableSend{Value: normalized, TaskID: taskID}); err != nil {
			return resource, false, err
		}
		waitableSignalOneRecvWaiter(resource)
		return resource, true, nil
	}
	resource.appendBuffer(normalized)
	waitableSignalOneRecvWaiter(resource)
	return resource, false, nil
}

func waitableTrySendValue(module *moduleInstance, waitableValue, value vmValue) (bool, error) {
	resource, err := waitableValueData(module, waitableValue)
	if err != nil {
		return false, err
	}
	if !module.waitableCanSend(waitableValue.Type) {
		return false, fmt.Errorf("cannot send on receive-only waitable %s", waitableValue.Type)
	}
	if resource == nil {
		return false, nil
	}
	if resource.Closed {
		return false, newGuestPanic(errors.New("send on closed waitable"))
	}
	normalized, err := normalizeWaitableSendValue(module, resource, value)
	if err != nil {
		return false, err
	}
	if resource.Capacity == 0 {
		if !waitableSendReady(resource) {
			return false, nil
		}
		if err := resource.appendPending(module.vm, pendingWaitableSend{Value: normalized}); err != nil {
			return false, err
		}
		waitableSignalOneRecvWaiter(resource)
		return true, nil
	}
	if resource.bufferLen() >= resource.Capacity {
		return false, nil
	}
	resource.appendBuffer(normalized)
	waitableSignalOneRecvWaiter(resource)
	return true, nil
}

func waitableTryRecvValues(module *moduleInstance, waitableValue vmValue) (vmValue, vmValue, error) {
	value, ok, _, err := waitableTryRecvValue(module, waitableValue)
	if err != nil {
		return vmValue{}, vmValue{}, err
	}
	return value, newVMValue("Bool", ok), nil
}

func waitableReadyRecvValue(module *moduleInstance, waitableValue vmValue) (vmValue, error) {
	resource, err := waitableValueData(module, waitableValue)
	if err != nil {
		return vmValue{}, err
	}
	if !module.waitableCanRecv(waitableValue.Type) {
		return vmValue{}, fmt.Errorf("cannot receive from send-only waitable %s", waitableValue.Type)
	}
	return newVMValue("Bool", waitableRecvReady(resource)), nil
}

func waitableReadySendValue(module *moduleInstance, waitableValue vmValue) (vmValue, error) {
	resource, err := waitableValueData(module, waitableValue)
	if err != nil {
		return vmValue{}, err
	}
	if !module.waitableCanSend(waitableValue.Type) {
		return vmValue{}, fmt.Errorf("cannot send on receive-only waitable %s", waitableValue.Type)
	}
	return newVMValue("Bool", waitableSendReady(resource)), nil
}

func waitableTryRecvValue(module *moduleInstance, waitableValue vmValue) (vmValue, bool, bool, error) {
	resource, err := waitableValueData(module, waitableValue)
	if err != nil {
		return vmValue{}, false, false, err
	}
	if !module.waitableCanRecv(waitableValue.Type) {
		return vmValue{}, false, false, fmt.Errorf("cannot receive from send-only waitable %s", waitableValue.Type)
	}
	if resource == nil {
		return waitableZeroValue(module, waitableValue.Type, nil), false, false, nil
	}
	if resource.bufferLen() == 0 {
		if resource.pendingLen() != 0 {
			pending := resource.popPending()
			resource.completeSend(pending.TaskID)
			waitableFillBufferedPending(resource)
			waitableSignalOneSendWaiter(resource)
			return pending.Value, true, resource.Closed, nil
		}
		return waitableZeroValue(module, waitableValue.Type, resource), false, resource.Closed, nil
	}
	value := resource.popBuffer()
	waitableFillBufferedPending(resource)
	waitableSignalOneSendWaiter(resource)
	return value, true, resource.Closed, nil
}

func waitableFillBufferedPending(resource *waitableResource) {
	if resource == nil || resource.Capacity <= 0 {
		return
	}
	for resource.pendingLen() != 0 && resource.bufferLen() < resource.Capacity {
		pending := resource.popPending()
		resource.appendBuffer(pending.Value)
		resource.completeSend(pending.TaskID)
	}
}

func (resource *waitableResource) completeSend(taskID int64) {
	if resource == nil || taskID <= 0 {
		return
	}
	if resource.Completed == nil {
		resource.Completed = make(map[int64]struct{})
	}
	resource.Completed[taskID] = struct{}{}
}

func (resource *waitableResource) takeCompletedSend(taskID int64) bool {
	if resource == nil || taskID <= 0 {
		return false
	}
	if _, ok := resource.Completed[taskID]; !ok {
		return false
	}
	delete(resource.Completed, taskID)
	return true
}

func (resource *waitableResource) cancelSend(taskID int64) {
	if resource == nil || taskID <= 0 {
		return
	}
	delete(resource.Completed, taskID)
	active := resource.Pending[resource.pendingHead:]
	pending := resource.Pending[:0]
	for _, send := range active {
		if send.TaskID != taskID {
			pending = append(pending, send)
		}
	}
	clear(resource.Pending[len(pending):])
	resource.Pending = pending
	resource.pendingHead = 0
}

func waitableCloseValue(module *moduleInstance, waitableValue vmValue) error {
	resource, err := waitableValueData(module, waitableValue)
	if err != nil {
		return err
	}
	if !module.waitableCanSend(waitableValue.Type) {
		return fmt.Errorf("cannot close receive-only waitable %s", waitableValue.Type)
	}
	if resource == nil {
		return newGuestPanic(errors.New("close of nil waitable"))
	}
	if resource.Closed {
		return newGuestPanic(errors.New("close of closed waitable"))
	}
	resource.Closed = true
	active := resource.Pending[resource.pendingHead:]
	pending := resource.Pending[:0]
	for _, send := range active {
		if send.TaskID == 0 {
			pending = append(pending, send)
		}
	}
	clear(resource.Pending[len(pending):])
	resource.Pending = pending
	resource.pendingHead = 0
	waitableSignalAllRecvWaiters(resource)
	waitableSignalAllSendWaiters(resource)
	return nil
}

func waitableWaitRecvValue(module *moduleInstance, waitableValue, tokenValue vmValue) error {
	resource, err := waitableValueData(module, waitableValue)
	if err != nil {
		return err
	}
	if !module.waitableCanRecv(waitableValue.Type) {
		return fmt.Errorf("cannot receive from send-only waitable %s", waitableValue.Type)
	}
	token, err := waitToken(tokenValue)
	if err != nil {
		return err
	}
	if token.Canceled {
		return fmt.Errorf("wait token %d is canceled", token.ID)
	}
	if resource == nil {
		return nil
	}
	if waitableRecvReady(resource) {
		token.signal()
		return nil
	}
	if err := reserveWaiter(module.vm, resource, token, waitDirectionReceive); err != nil {
		return err
	}
	resource.RecvWaiters = append(resource.RecvWaiters, token)
	token.register(resource, waitDirectionReceive)
	return nil
}

func waitableRecvReady(resource *waitableResource) bool {
	if resource == nil {
		return false
	}
	return resource.bufferLen() != 0 || resource.pendingLen() != 0 || resource.Closed
}

func waitableWaitSendValue(module *moduleInstance, waitableValue, tokenValue vmValue) error {
	resource, err := waitableValueData(module, waitableValue)
	if err != nil {
		return err
	}
	if !module.waitableCanSend(waitableValue.Type) {
		return fmt.Errorf("cannot send on receive-only waitable %s", waitableValue.Type)
	}
	token, err := waitToken(tokenValue)
	if err != nil {
		return err
	}
	if token.Canceled {
		return fmt.Errorf("wait token %d is canceled", token.ID)
	}
	if resource == nil {
		return nil
	}
	if waitableSendReady(resource) {
		token.signal()
		return nil
	}
	if err := reserveWaiter(module.vm, resource, token, waitDirectionSend); err != nil {
		return err
	}
	resource.SendWaiters = append(resource.SendWaiters, token)
	token.register(resource, waitDirectionSend)
	return nil
}

func waitableSendReady(resource *waitableResource) bool {
	if resource == nil {
		return false
	}
	if resource.Closed {
		return true
	}
	if resource.Capacity == 0 {
		for _, token := range resource.RecvWaiters {
			if token != nil && !token.Canceled {
				return true
			}
		}
		return false
	}
	return resource.bufferLen() < resource.Capacity
}

func waitableSignalOneRecvWaiter(resource *waitableResource) {
	if len(resource.RecvWaiters) == 0 {
		return
	}
	original := resource.RecvWaiters
	waiters := original[:0]
	var selected *waitTokenState
	for _, token := range resource.RecvWaiters {
		if token == nil || token.Canceled || token.Signaled {
			continue
		}
		if selected == nil {
			selected = token
			continue
		}
		waiters = append(waiters, token)
	}
	clear(original[len(waiters):])
	resource.RecvWaiters = waiters
	if selected != nil {
		selected.signal()
	}
}

func waitableSignalAllRecvWaiters(resource *waitableResource) {
	if len(resource.RecvWaiters) == 0 {
		return
	}
	waiters := resource.RecvWaiters
	resource.RecvWaiters = nil
	for _, token := range waiters {
		if token != nil && !token.Canceled {
			token.signal()
		}
	}
}

func waitableSignalOneSendWaiter(resource *waitableResource) {
	if len(resource.SendWaiters) == 0 {
		return
	}
	original := resource.SendWaiters
	waiters := original[:0]
	var selected *waitTokenState
	for _, token := range resource.SendWaiters {
		if token == nil || token.Canceled || token.Signaled {
			continue
		}
		if selected == nil {
			selected = token
			continue
		}
		waiters = append(waiters, token)
	}
	clear(original[len(waiters):])
	resource.SendWaiters = waiters
	if selected != nil {
		selected.signal()
	}
}

func waitableSignalAllSendWaiters(resource *waitableResource) {
	if len(resource.SendWaiters) == 0 {
		return
	}
	waiters := resource.SendWaiters
	resource.SendWaiters = nil
	for _, token := range waiters {
		if token != nil && !token.Canceled {
			token.signal()
		}
	}
}

func waitableLenValue(resource *waitableResource) vmValue {
	if resource == nil {
		return newVMValue("Int", int64(0))
	}
	return newVMValue("Int", int64(resource.bufferLen()))
}

func waitableCapValue(resource *waitableResource) vmValue {
	if resource == nil {
		return newVMValue("Int", int64(0))
	}
	return newVMValue("Int", int64(resource.Capacity))
}

func (resource *waitableResource) removeWaiter(token *waitTokenState, direction waitDirection) {
	if resource == nil || token == nil {
		return
	}
	if direction == waitDirectionReceive {
		resource.RecvWaiters = removeWaiterFrom(resource.RecvWaiters, token)
		return
	}
	if direction == waitDirectionSend {
		resource.SendWaiters = removeWaiterFrom(resource.SendWaiters, token)
	}
}

func removeWaiterFrom(waiters []*waitTokenState, target *waitTokenState) []*waitTokenState {
	kept := waiters[:0]
	for _, token := range waiters {
		if token != nil && token != target && !token.Canceled && !token.Signaled {
			kept = append(kept, token)
		}
	}
	clear(waiters[len(kept):])
	return kept
}

func (resource *waitableResource) bufferLen() int {
	if resource == nil {
		return 0
	}
	return len(resource.Buffer) - resource.bufferHead
}

func (resource *waitableResource) pendingLen() int {
	if resource == nil {
		return 0
	}
	return len(resource.Pending) - resource.pendingHead
}

func (resource *waitableResource) appendBuffer(value vmValue) {
	if resource.bufferHead > 0 && len(resource.Buffer) == cap(resource.Buffer) {
		active := copy(resource.Buffer, resource.Buffer[resource.bufferHead:])
		clear(resource.Buffer[active:])
		resource.Buffer = resource.Buffer[:active]
		resource.bufferHead = 0
	}
	resource.Buffer = append(resource.Buffer, value)
}

func (resource *waitableResource) popBuffer() vmValue {
	value := resource.Buffer[resource.bufferHead]
	resource.Buffer[resource.bufferHead] = vmValue{}
	resource.bufferHead++
	if resource.bufferHead == len(resource.Buffer) {
		resource.Buffer = resource.Buffer[:0]
		resource.bufferHead = 0
	}
	return value
}

func (resource *waitableResource) appendPending(machine *vm, send pendingWaitableSend) error {
	length := resource.pendingLen() + 1
	if _, _, err := machine.checkCollectionSize(int64(length), int64(length)); err != nil {
		return err
	}
	if length > cap(resource.Pending) {
		if machine != nil {
			previous := machine.allocationRoots
			machine.allocationRoots = []vmValue{newVMValue(resource.Type, resource), send.Value}
			err := machine.chargeAllocationBytes(int64(length-cap(resource.Pending)) * ir.RuntimeSlotBytes)
			machine.allocationRoots = previous
			if err != nil {
				return err
			}
		}
		grown := make([]pendingWaitableSend, resource.pendingLen(), length)
		copy(grown, resource.Pending[resource.pendingHead:])
		resource.Pending = grown
		resource.pendingHead = 0
	}
	if resource.pendingHead > 0 && (resource.pendingHead >= 64 || len(resource.Pending) == cap(resource.Pending)) {
		active := copy(resource.Pending, resource.Pending[resource.pendingHead:])
		clear(resource.Pending[active:])
		resource.Pending = resource.Pending[:active]
		resource.pendingHead = 0
	}
	resource.Pending = append(resource.Pending, send)
	return nil
}

func (resource *waitableResource) popPending() pendingWaitableSend {
	send := resource.Pending[resource.pendingHead]
	resource.Pending[resource.pendingHead] = pendingWaitableSend{}
	resource.pendingHead++
	if resource.pendingHead == len(resource.Pending) {
		resource.Pending = resource.Pending[:0]
		resource.pendingHead = 0
	}
	return send
}

func waitableValueData(module *moduleInstance, value vmValue) (*waitableResource, error) {
	resource, ok := value.Data.(*waitableResource)
	if ok {
		return resource, nil
	}
	if value.Data != nil {
		return nil, fmt.Errorf("invalid waitable resource backing for %s", value.Type)
	}
	if !module.isWaitableTypeName(value.Type) {
		return nil, fmt.Errorf("expected waitable resource, got %s", value.Type)
	}
	return nil, nil
}

func normalizeWaitableSendValue(module *moduleInstance, resource *waitableResource, value vmValue) (vmValue, error) {
	elemType := resource.ElemType
	if !elemType.Valid() {
		elemType = module.resolvedRuntimeType("Any")
	}
	normalized, err := module.coerceAssignableValue(value, elemType)
	if err != nil {
		return vmValue{}, fmt.Errorf("waitable send value: %w", err)
	}
	return module.cloneValueForStore(normalized), nil
}

func waitableZeroValue(module *moduleInstance, waitableType any, resource *waitableResource) vmValue {
	var elemType any = "Any"
	if resource != nil && resource.ElemType.Valid() {
		elemType = resource.ElemType
	} else if module != nil {
		if info, ok := module.resolvedRuntimeType(waitableType).WaitableInfo(); ok {
			elemType = info.Elem
		}
	} else if info, ok := coerceRuntimeType(waitableType).WaitableInfo(); ok {
		elemType = info.Elem
	}
	if module != nil {
		return module.zeroValue(elemType)
	}
	return zeroVMValue(coerceRuntimeType(elemType).String())
}

func (m *moduleInstance) waitableCanSend(typ any) bool {
	info, ok := m.resolvedRuntimeType(typ).WaitableInfo()
	return !ok || info.Direction != types.ChannelReceive
}

func (m *moduleInstance) waitableCanRecv(typ any) bool {
	info, ok := m.resolvedRuntimeType(typ).WaitableInfo()
	return !ok || info.Direction != types.ChannelSend
}

func (m *moduleInstance) isWaitableTypeName(typ any) bool {
	_, ok := m.resolvedRuntimeType(typ).WaitableInfo()
	return ok
}
