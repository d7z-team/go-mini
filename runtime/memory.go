package runtime

import (
	"math"

	artifact "github.com/d7z-team/mini-go/runtime/bytecode"
)

// refreshLiveGuestBytes replaces the conservative allocation delta with a
// deterministic census of mutable values still reachable by the VM owner.
func (vm *vm) refreshLiveGuestBytes() int64 {
	if vm == nil {
		return 0
	}
	sizer := newRuntimeValueSizer()
	for _, value := range vm.allocationRoots {
		sizer.value(value)
	}
	sizer.add(vm.dynamicTypeBytes)
	if revision := vm.revision.Load(); revision != nil && revision.modules != nil {
		for _, module := range revision.modules.modules {
			if module == nil || module.state == nil {
				continue
			}
			for _, cell := range module.state.globals {
				sizer.slot(cell)
			}
		}
	}
	if machine := vm.machine; machine != nil {
		sizer.task(machine.running)
		for _, task := range machine.runnableTasks() {
			sizer.task(task)
		}
		for _, task := range machine.blocked {
			sizer.task(task)
		}
		sizer.task(machine.paused)
	}
	for _, timer := range vm.timers {
		if timer != nil {
			sizer.value(timer.signal)
		}
	}
	for _, value := range vm.reflectTypeValues {
		sizer.value(value)
	}
	vm.liveGuestBytes.Store(sizer.bytes)
	vm.allocatedSinceSweep.Store(0)
	updateAtomicMaximum(&vm.peakGuestBytes, sizer.bytes)
	return sizer.bytes
}

type runtimeValueSizer struct {
	bytes         int64
	seenPointers  map[*vmPointer]bool
	seenSlices    map[*vmSlice]bool
	seenStorage   map[*vmSliceStorage]bool
	seenMaps      map[*vmMap]bool
	seenStructs   map[*vmStruct]bool
	seenSlots     map[*slot]bool
	seenWaitables map[*waitableResource]bool
	seenTokens    map[*waitTokenState]bool
	seenWaitSets  map[*waitSetState]bool
	seenTasks     map[*executionTask]bool
	pendingValues []vmValue
	walking       bool
}

func newRuntimeValueSizer() *runtimeValueSizer {
	return &runtimeValueSizer{
		seenPointers: make(map[*vmPointer]bool), seenSlices: make(map[*vmSlice]bool),
		seenStorage: make(map[*vmSliceStorage]bool),
		seenMaps:    make(map[*vmMap]bool), seenStructs: make(map[*vmStruct]bool),
		seenSlots: make(map[*slot]bool), seenWaitables: make(map[*waitableResource]bool),
		seenTokens: make(map[*waitTokenState]bool), seenWaitSets: make(map[*waitSetState]bool),
		seenTasks: make(map[*executionTask]bool),
	}
}

func (sizer *runtimeValueSizer) add(bytes int64) {
	if bytes <= 0 || sizer.bytes == math.MaxInt64 {
		return
	}
	if bytes > math.MaxInt64-sizer.bytes {
		sizer.bytes = math.MaxInt64
		return
	}
	sizer.bytes += bytes
}

func (sizer *runtimeValueSizer) slot(cell *slot) {
	if cell == nil || sizer.seenSlots[cell] {
		return
	}
	sizer.seenSlots[cell] = true
	if cell.initialized {
		sizer.value(cell.value)
	}
}

func (sizer *runtimeValueSizer) value(value vmValue) {
	sizer.pendingValues = append(sizer.pendingValues, value)
	if sizer.walking {
		return
	}
	sizer.walking = true
	for len(sizer.pendingValues) != 0 {
		last := len(sizer.pendingValues) - 1
		value = sizer.pendingValues[last]
		sizer.pendingValues[last] = vmValue{}
		sizer.pendingValues = sizer.pendingValues[:last]
		sizer.visitValue(value)
	}
	sizer.walking = false
}

func (sizer *runtimeValueSizer) visitValue(value vmValue) {
	switch data := value.Data.(type) {
	case string:
		sizer.add(int64(len(data)) * artifact.RuntimeByteBytes)
	case functionRef:
		sizer.add(artifact.RuntimeNodeBytes + int64(len(data.upvalues))*artifact.RuntimeSlotBytes)
		for _, cell := range data.upvalues {
			sizer.slot(cell)
		}
	case reflectMethodTarget:
		sizer.add(artifact.RuntimeNodeBytes)
		sizer.value(data.Receiver)
	case reflectUnboundMethodTarget:
		sizer.add(artifact.RuntimeNodeBytes)
	case reflectMakeFuncTarget:
		sizer.add(artifact.RuntimeNodeBytes)
		sizer.value(newVMValue(data.functionType, data.handler))
	case vmValue:
		sizer.add(artifact.RuntimeNodeBytes)
		sizer.value(data)
	case *vmPointer:
		if data == nil || sizer.seenPointers[data] {
			return
		}
		sizer.seenPointers[data] = true
		sizer.add(artifact.RuntimeNodeBytes + int64(len(data.path)+len(data.indexes))*artifact.RuntimeSlotBytes)
		if data.original != nil {
			sizer.value(newVMValue(pointerType(data.original.Type), data.original))
			return
		}
		sizer.slot(data.slot)
		for _, index := range data.indexes {
			sizer.value(index)
		}
		if data.slot == nil && data.load != nil {
			if pointed, err := data.loadValue(); err == nil {
				sizer.value(pointed)
			}
		}
	case *vmSlice:
		if data == nil || sizer.seenSlices[data] {
			return
		}
		sizer.seenSlices[data] = true
		sizer.add(artifact.RuntimeNodeBytes)
		if data.storage != nil && sizer.seenStorage[data.storage] {
			return
		}
		if data.storage != nil {
			sizer.seenStorage[data.storage] = true
		}
		if data.ByteBacked {
			sizer.add(int64(cap(data.ByteBacking)) * artifact.RuntimeByteBytes)
			return
		}
		sizer.add(int64(cap(data.Backing)) * artifact.RuntimeSlotBytes)
		for _, item := range data.Backing {
			sizer.value(item)
		}
	case *vmMap:
		if data == nil || sizer.seenMaps[data] {
			return
		}
		sizer.seenMaps[data] = true
		sizer.add(artifact.RuntimeNodeBytes + int64(len(data.Entries))*artifact.RuntimeMapEntryBytes)
		for _, entry := range data.Entries {
			sizer.value(entry.Key)
			sizer.value(entry.Value)
		}
	case []vmValue:
		sizer.add(artifact.RuntimeNodeBytes + int64(cap(data))*artifact.RuntimeSlotBytes)
		for _, item := range data {
			sizer.value(item)
		}
	case *vmStruct:
		if data == nil || sizer.seenStructs[data] {
			return
		}
		sizer.seenStructs[data] = true
		sizer.add(artifact.RuntimeNodeBytes + int64(cap(data.values))*artifact.RuntimeSlotBytes)
		for _, field := range data.values {
			if field.Type.Valid() {
				sizer.value(field)
			}
		}
	case *waitableResource:
		if data == nil || sizer.seenWaitables[data] {
			return
		}
		sizer.seenWaitables[data] = true
		sizer.add(artifact.RuntimeNodeBytes + int64(cap(data.Buffer)+cap(data.Pending)+cap(data.RecvWaiters)+cap(data.SendWaiters))*artifact.RuntimeSlotBytes)
		for _, item := range data.Buffer[data.bufferHead:] {
			sizer.value(item)
		}
		for _, pending := range data.Pending[data.pendingHead:] {
			sizer.value(pending.Value)
		}
		for _, token := range data.RecvWaiters {
			sizer.waitToken(token)
		}
		for _, token := range data.SendWaiters {
			sizer.waitToken(token)
		}
	case *waitTokenState:
		sizer.waitToken(data)
	case *waitSetState:
		if data == nil || sizer.seenWaitSets[data] {
			return
		}
		sizer.seenWaitSets[data] = true
		sizer.add(artifact.RuntimeNodeBytes + int64(cap(data.Tokens))*artifact.RuntimeSlotBytes)
		for _, token := range data.Tokens {
			sizer.waitToken(token)
		}
	}
}

func (sizer *runtimeValueSizer) waitToken(token *waitTokenState) {
	if token == nil || sizer.seenTokens[token] {
		return
	}
	sizer.seenTokens[token] = true
	sizer.add(artifact.RuntimeNodeBytes + int64(len(token.registrations))*artifact.RuntimeSlotBytes)
	for registration := range token.registrations {
		if resource, ok := registration.target.(*waitableResource); ok {
			sizer.value(newVMValue(resource.Type, resource))
		}
	}
}

func (sizer *runtimeValueSizer) task(task *executionTask) {
	if task == nil || sizer.seenTasks[task] {
		return
	}
	sizer.seenTasks[task] = true
	for _, active := range task.frames {
		if active == nil || active.frame == nil {
			continue
		}
		frame := active.frame
		for _, iterator := range frame.mapIterators {
			sizer.add(iterator.logicalBytes())
			sizer.value(iterator.object)
		}
		sizer.add(artifact.RuntimeNodeBytes + int64(cap(frame.localStorage)+cap(frame.upvalueCells)+cap(frame.stack)+cap(frame.popValues)+cap(frame.returnValues))*artifact.RuntimeSlotBytes)
		for _, cell := range frame.localCells {
			sizer.slot(cell)
		}
		for _, cell := range frame.upvalueCells {
			sizer.slot(cell)
		}
		for _, value := range frame.stack {
			sizer.value(value)
		}
		for _, value := range frame.popValues {
			sizer.value(value)
		}
		for _, value := range frame.returnValues {
			sizer.value(value)
		}
		for _, deferred := range frame.defers {
			sizer.value(newVMValue("Function", deferred.ref))
		}
		if active.completion != nil {
			for _, value := range active.completion.returnValues {
				sizer.value(value)
			}
			if active.completion.panic != nil {
				sizer.value(active.completion.panic.err.Value)
			}
		}
		if active.recoveredPanic != nil {
			sizer.value(active.recoveredPanic.err.Value)
		}
	}
	if blocked := task.blocked; blocked != nil {
		sizer.value(blocked.waitable)
		sizer.value(blocked.waitSet)
		sizer.value(blocked.recvToken)
		if blocked.resource != nil {
			sizer.value(newVMValue(blocked.resource.Type, blocked.resource))
		}
	}
}
