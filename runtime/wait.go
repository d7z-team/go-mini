package runtime

import (
	"fmt"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

type waitTokenState struct {
	ID            int64
	Signaled      bool
	Canceled      bool
	cancelHook    func() error
	registrations map[waitRegistration]struct{}
}

type waitDirection uint8

const (
	waitDirectionReceive waitDirection = iota + 1
	waitDirectionSend
)

type waitRegistrationTarget interface {
	removeWaiter(*waitTokenState, waitDirection)
}

type waitRegistration struct {
	target    waitRegistrationTarget
	direction waitDirection
}

func (token *waitTokenState) register(target waitRegistrationTarget, direction waitDirection) {
	if token == nil || target == nil || token.Canceled || token.Signaled {
		return
	}
	if token.registrations == nil {
		token.registrations = make(map[waitRegistration]struct{})
	}
	token.registrations[waitRegistration{target: target, direction: direction}] = struct{}{}
}

func (token *waitTokenState) unregisterAll() {
	if token == nil || len(token.registrations) == 0 {
		return
	}
	registrations := token.registrations
	token.registrations = nil
	for registration := range registrations {
		registration.target.removeWaiter(token, registration.direction)
	}
}

func (token *waitTokenState) signal() {
	if token == nil || token.Canceled {
		return
	}
	token.Signaled = true
	token.cancelHook = nil
	token.unregisterAll()
}

func (token *waitTokenState) cancel() error {
	if token == nil || token.Canceled {
		return nil
	}
	hook := token.cancelHook
	token.cancelHook = nil
	token.Canceled = true
	token.unregisterAll()
	if hook == nil {
		return nil
	}
	return hook()
}

type waitSetState struct {
	Tokens []*waitTokenState
}

type WaitBlockedError struct {
	Message string
}

func (e WaitBlockedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "wait set blocked"
}

func (vm *vm) newWaitTokenValue() vmValue {
	vm.nextWaitTokenID++
	return newVMValue("WaitToken", &waitTokenState{ID: vm.nextWaitTokenID})
}

func newWaitSetValue() vmValue {
	return newVMValue("WaitSet", &waitSetState{})
}

func signalWaitToken(value vmValue) error {
	token, err := waitToken(value)
	if err != nil {
		return err
	}
	if token.Canceled {
		return fmt.Errorf("wait token %d is canceled", token.ID)
	}
	token.signal()
	return nil
}

func cancelWaitToken(value vmValue) error {
	token, err := waitToken(value)
	if err != nil {
		return err
	}
	return token.cancel()
}

func (vm *vm) addWaitSetToken(waitSetValue, tokenValue vmValue) (vmValue, error) {
	waitSet, err := waitSet(waitSetValue)
	if err != nil {
		return vmValue{}, err
	}
	token, err := waitToken(tokenValue)
	if err != nil {
		return vmValue{}, err
	}
	length := int64(len(waitSet.Tokens)) + 1
	if _, _, err := vm.checkCollectionSize(length, length); err != nil {
		return vmValue{}, err
	}
	capacity := int64(cap(waitSet.Tokens))
	if length > capacity {
		maxCapacity := int64(^uint(0) >> 1)
		if capacity > maxCapacity/2 {
			capacity = maxCapacity
		} else {
			capacity *= 2
		}
		if capacity < length {
			capacity = length
		}
		if limit := int64(vm.limits.MaxCollectionElements); limit > 0 && capacity > limit {
			capacity = limit
		}
	}
	newLength, newCapacity, err := vm.checkCollectionSize(length, capacity)
	if err != nil {
		return vmValue{}, err
	}
	if newCapacity > cap(waitSet.Tokens) {
		if int64(newCapacity-cap(waitSet.Tokens)) > int64(^uint64(0)>>1)/ir.RuntimeSlotBytes {
			return vmValue{}, ResourceLimitError{Code: "execution.allocation_limit", Message: "waitset allocation size overflow"}
		}
		if err := vm.chargeAllocationBytes(int64(newCapacity-cap(waitSet.Tokens)) * ir.RuntimeSlotBytes); err != nil {
			return vmValue{}, err
		}
		grown := make([]*waitTokenState, len(waitSet.Tokens), newCapacity)
		copy(grown, waitSet.Tokens)
		waitSet.Tokens = grown
	}
	waitSet.Tokens = waitSet.Tokens[:newLength]
	waitSet.Tokens[newLength-1] = token
	return waitSetValue, nil
}

func pollWaitSet(machine *vm, value vmValue) (vmValue, error) {
	waitSet, err := waitSet(value)
	if err != nil {
		return vmValue{}, err
	}
	return newVMValue("Int", int64(waitSetReadyIndex(machine, waitSet))), nil
}

func parkWaitSet(machine *vm, value vmValue) (vmValue, error) {
	waitSet, err := waitSet(value)
	if err != nil {
		return vmValue{}, err
	}
	index := waitSetReadyIndex(machine, waitSet)
	if index < 0 {
		return vmValue{}, WaitBlockedError{Message: "waitset has no ready token"}
	}
	return newVMValue("Int", int64(index)), nil
}

func cancelWaitSet(value vmValue) error {
	waitSet, err := waitSet(value)
	if err != nil {
		return err
	}
	var firstErr error
	for _, token := range waitSet.Tokens {
		if token != nil {
			if err := token.cancel(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	waitSet.Tokens = nil
	return firstErr
}

func waitSetReadyIndex(machine *vm, waitSet *waitSetState) int {
	ready := make([]int, 0, len(waitSet.Tokens))
	for i, token := range waitSet.Tokens {
		if token != nil && token.Signaled && !token.Canceled {
			ready = append(ready, i)
		}
	}
	return machine.chooseReadyIndex(ready)
}

func (vm *vm) chooseReadyIndex(ready []int) int {
	if len(ready) == 0 {
		return -1
	}
	if len(ready) == 1 || vm == nil {
		return ready[0]
	}
	state := vm.selectState
	if state == 0 {
		state = 0x9e3779b97f4a7c15
	}
	state ^= state << 13
	state ^= state >> 7
	state ^= state << 17
	vm.selectState = state
	return ready[state%uint64(len(ready))]
}

func waitToken(value vmValue) (*waitTokenState, error) {
	token, ok := value.Data.(*waitTokenState)
	if !ok || token == nil {
		return nil, fmt.Errorf("expected WaitToken, got %s", value.Type)
	}
	return token, nil
}

func waitSet(value vmValue) (*waitSetState, error) {
	waitSet, ok := value.Data.(*waitSetState)
	if !ok || waitSet == nil {
		return nil, fmt.Errorf("expected WaitSet, got %s", value.Type)
	}
	return waitSet, nil
}
