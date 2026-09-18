package runtime

type runtimeValueWalker struct {
	visitRevision func(*instanceRevision)
	seenPointers  map[*vmPointer]bool
	seenSlices    map[*vmSlice]bool
	seenMaps      map[*vmMap]bool
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
		seenWaitables: make(map[*waitableResource]bool), seenTokens: make(map[*waitTokenState]bool),
		seenWaitSets: make(map[*waitSetState]bool),
	}
}

func (walker *runtimeValueWalker) slot(cell *slot) {
	if cell == nil || walker.seenSlots[cell] {
		return
	}
	walker.seenSlots[cell] = true
	walker.value(cell.load())
}

func (walker *runtimeValueWalker) value(value vmValue) {
	switch data := value.Data.(type) {
	case functionRef:
		if data.exact != nil {
			walker.visitRevision(data.exact.revision)
		}
		for _, cell := range data.upvalues {
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
				walker.value(item)
			}
		}
	case *vmMap:
		if data != nil && !walker.seenMaps[data] {
			walker.seenMaps[data] = true
			for _, entry := range data.Entries {
				walker.value(entry.Key)
				walker.value(entry.Value)
			}
		}
	case []vmValue:
		for _, item := range data {
			walker.value(item)
		}
	case *vmStruct:
		if data != nil {
			for _, field := range data.values {
				if field.Type.Valid() {
					walker.value(field)
				}
			}
		}
	case *waitableResource:
		if data != nil && !walker.seenWaitables[data] {
			walker.seenWaitables[data] = true
			for _, item := range data.Buffer[data.bufferHead:] {
				walker.value(item)
			}
			for _, pending := range data.Pending[data.pendingHead:] {
				walker.value(pending.Value)
			}
			for _, token := range data.RecvWaiters {
				walker.waitToken(token)
			}
			for _, token := range data.SendWaiters {
				walker.waitToken(token)
			}
		}
	case *waitTokenState:
		walker.waitToken(data)
	case *waitSetState:
		if data != nil && !walker.seenWaitSets[data] {
			walker.seenWaitSets[data] = true
			for _, token := range data.Tokens {
				walker.waitToken(token)
			}
		}
	}
}

func (walker *runtimeValueWalker) waitToken(token *waitTokenState) {
	if token == nil || walker.seenTokens[token] {
		return
	}
	walker.seenTokens[token] = true
	for registration := range token.registrations {
		if resource, ok := registration.target.(*waitableResource); ok {
			walker.value(newVMValue(resource.Type, resource))
		}
	}
}
