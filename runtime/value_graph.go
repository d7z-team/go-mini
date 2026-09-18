package runtime

import "github.com/d7z-team/mini-go/compiler/types"

type runtimeValueWalker struct {
	visitRevision func(*instanceRevision)
	enter         func(vmValue) bool
	leave         func()
	stopped       bool
	seenPointers  map[*vmPointer]bool
	seenSlices    map[*vmSlice]bool
	seenMaps      map[*vmMap]bool
	seenStructs   map[*vmStruct]bool
	seenSlots     map[*slot]bool
	seenWaitables map[*waitableResource]bool
	seenTokens    map[*waitTokenState]bool
	seenWaitSets  map[*waitSetState]bool
}

func newRuntimeValueWalker(visitRevision func(*instanceRevision)) *runtimeValueWalker {
	return &runtimeValueWalker{
		visitRevision: visitRevision,
		seenPointers:  make(map[*vmPointer]bool), seenSlices: make(map[*vmSlice]bool),
		seenMaps: make(map[*vmMap]bool), seenSlots: make(map[*slot]bool),
		seenStructs:   make(map[*vmStruct]bool),
		seenWaitables: make(map[*waitableResource]bool), seenTokens: make(map[*waitTokenState]bool),
		seenWaitSets: make(map[*waitSetState]bool),
	}
}

func (walker *runtimeValueWalker) slot(cell *slot) {
	if walker.enter != nil {
		if walker.stopped || !walker.enter(vmValue{}) {
			return
		}
		defer walker.leave()
	}
	if walker.stopped || cell == nil || walker.seenSlots[cell] {
		return
	}
	walker.seenSlots[cell] = true
	if walker.enter != nil && !cell.initialized {
		return
	}
	walker.value(cell.load())
}

func (walker *runtimeValueWalker) value(value vmValue) {
	if walker.stopped {
		return
	}
	if walker.enter != nil {
		if !walker.enter(value) {
			return
		}
		defer walker.leave()
	}
	switch data := value.Data.(type) {
	case functionRef:
		if data.exact != nil {
			walker.visitRevision(data.exact.revision)
		}
		for _, cell := range data.upvalues {
			if walker.stopped {
				return
			}
			walker.slot(cell)
		}
	case reflectMethodTarget:
		if data.Module != nil {
			walker.visitRevision(data.Module.revision)
		}
		walker.value(data.Receiver)
	case reflectUnboundMethodTarget:
		if data.Module != nil {
			walker.visitRevision(data.Module.revision)
		}
	case reflectMakeFuncTarget:
		if data.handlerModule != nil {
			walker.visitRevision(data.handlerModule.revision)
		}
		walker.value(newVMValue(data.functionType, data.handler))
	case vmValue:
		walker.value(data)
	case *vmPointer:
		if data != nil && !walker.seenPointers[data] {
			walker.seenPointers[data] = true
			if data.original != nil {
				walker.value(newVMValue(pointerType(data.original.Type), data.original))
				return
			}
			if walker.enter != nil {
				// Byte storage cannot retain code. Loading an array view would
				// materialize every byte before the next inspection budget check.
				_, elem, array := data.Type.ArrayInfo()
				if data.Type.Primitive(types.PrimitiveUint8) || array && elem.Primitive(types.PrimitiveUint8) {
					return
				}
			}
			if walker.enter != nil && data.slot != nil {
				if !data.slot.initialized {
					return
				}
				if len(data.path) == 0 {
					walker.slot(data.slot)
					return
				}
			}
			if data.load != nil || data.slot != nil {
				if pointed, err := data.loadValue(); err == nil {
					walker.value(pointed)
				}
			}
		}
	case *vmSlice:
		if data != nil && !walker.seenSlices[data] {
			walker.seenSlices[data] = true
			for _, item := range data.Backing {
				if walker.stopped {
					return
				}
				walker.value(item)
			}
		}
	case *vmMap:
		if data != nil && !walker.seenMaps[data] {
			walker.seenMaps[data] = true
			for _, entry := range data.Entries {
				if walker.stopped {
					return
				}
				walker.value(entry.Key)
				walker.value(entry.Value)
			}
		}
	case []vmValue:
		for _, item := range data {
			if walker.stopped {
				return
			}
			walker.value(item)
		}
	case *vmStruct:
		if data != nil && !walker.seenStructs[data] {
			walker.seenStructs[data] = true
			for _, field := range data.values {
				if walker.stopped {
					return
				}
				if field.Type.Valid() {
					walker.value(field)
				}
			}
		}
	case *waitableResource:
		if data != nil && !walker.seenWaitables[data] {
			walker.seenWaitables[data] = true
			for _, item := range data.Buffer[data.bufferHead:] {
				if walker.stopped {
					return
				}
				walker.value(item)
			}
			for _, pending := range data.Pending[data.pendingHead:] {
				if walker.stopped {
					return
				}
				walker.value(pending.Value)
			}
			for _, token := range data.RecvWaiters {
				if walker.stopped {
					return
				}
				walker.waitToken(token)
			}
			for _, token := range data.SendWaiters {
				if walker.stopped {
					return
				}
				walker.waitToken(token)
			}
		}
	case *waitTokenState:
		walker.waitToken(data)
	case *waitSetState:
		if data != nil && !walker.seenWaitSets[data] {
			walker.seenWaitSets[data] = true
			for _, token := range data.Tokens {
				if walker.stopped {
					return
				}
				walker.waitToken(token)
			}
		}
	}
}

func (walker *runtimeValueWalker) waitToken(token *waitTokenState) {
	if walker.enter != nil {
		if walker.stopped || !walker.enter(vmValue{}) {
			return
		}
		defer walker.leave()
	}
	if token == nil || walker.seenTokens[token] {
		return
	}
	walker.seenTokens[token] = true
	for registration := range token.registrations {
		if walker.stopped {
			return
		}
		if resource, ok := registration.target.(*waitableResource); ok {
			walker.value(newVMValue(resource.Type, resource))
		}
	}
}
