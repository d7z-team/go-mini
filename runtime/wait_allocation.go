package runtime

import ir "github.com/d7z-team/mini-go/runtime/bytecode"

// reserveWaiter charges both owners before publishing either registration.
func reserveWaiter(machine *vm, resource *waitableResource, token *waitTokenState, direction waitDirection) error {
	waiters := &resource.RecvWaiters
	if direction == waitDirectionSend {
		waiters = &resource.SendWaiters
	}
	length := len(*waiters) + 1
	if _, _, err := machine.checkCollectionSize(int64(length), int64(length)); err != nil {
		return err
	}
	capacity := cap(*waiters)
	if capacity < length {
		capacity = length
	}
	slots := capacity - cap(*waiters)
	if _, registered := token.registrations[waitRegistration{target: resource, direction: direction}]; !registered {
		slots++
	}
	if machine != nil {
		previous := machine.allocationRoots
		machine.allocationRoots = []vmValue{newVMValue(resource.Type, resource), newVMValue("WaitToken", token)}
		err := machine.chargeAllocationBytes(int64(slots) * ir.RuntimeSlotBytes)
		machine.allocationRoots = previous
		if err != nil {
			return err
		}
	}
	if capacity > cap(*waiters) {
		grown := make([]*waitTokenState, len(*waiters), capacity)
		copy(grown, *waiters)
		*waiters = grown
	}
	return nil
}
