package runtime

import ir "github.com/d7z-team/mini-go/runtime/bytecode"

type runtimeValueWalker struct {
	visitRevision func(*instanceRevision)
	enter         func(vmValue) bool
	leave         func()
	stopped       bool
	seenPointers  map[*vmPointer]pointerVisit
	seenSlices    map[*vmSlice]bool
	seenMaps      map[*vmMap]bool
	seenStructs   map[*vmStruct]bool
	seenSlots     map[*slot]bool
	seenWaitables map[*waitableResource]bool
	seenTokens    map[*waitTokenState]bool
	seenWaitSets  map[*waitSetState]bool
}

type pointerVisit uint8

const (
	pointerVisited pointerVisit = 1 << iota
	pointerReading
)

func newRuntimeValueWalker(visitRevision func(*instanceRevision)) *runtimeValueWalker {
	return &runtimeValueWalker{
		visitRevision: visitRevision,
		seenPointers:  make(map[*vmPointer]pointerVisit), seenSlices: make(map[*vmSlice]bool),
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
	if !cell.initialized {
		return
	}
	walker.value(cell.value)
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
		if data != nil && walker.seenPointers[data]&pointerVisited == 0 {
			walker.seenPointers[data] |= pointerVisited
			if data.original != nil {
				walker.value(newVMValue(pointerType(data.original.Type), data.original))
				return
			}
			if pointed := walker.pointerContents(data); pointed.Type.Valid() || pointed.Data != nil {
				walker.value(pointed)
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

// pointerContents observes stored references without initializing slots, coercing
// values or expanding byte-backed array views. Storage accounting follows owners
// separately; revision inspection follows the selected address only.
func (walker *runtimeValueWalker) pointerContents(pointer *vmPointer) vmValue {
	if pointer == nil || walker.stopped || walker.seenPointers[pointer]&pointerReading != 0 {
		return vmValue{}
	}
	walker.seenPointers[pointer] |= pointerReading
	defer func() {
		walker.seenPointers[pointer] &^= pointerReading
		if walker.seenPointers[pointer] == 0 {
			delete(walker.seenPointers, pointer)
		}
	}()
	if pointer.original != nil {
		return walker.addressContents(newVMValue("", pointer.original), nil, nil, true)
	}
	switch pointer.target {
	case pointerCell:
		if pointer.cell != nil {
			return *pointer.cell
		}
	case pointerField:
		return walker.addressContents(pointer.parent, []ir.AddressPathSegment{{Kind: "field", Field: pointer.field}}, nil, true)
	case pointerIndex:
		return walker.addressContents(pointer.parent, []ir.AddressPathSegment{{Kind: "index"}}, []vmValue{newVMValue("Int", pointer.index)}, true)
	case pointerArray:
		if pointer.array != nil && !pointer.array.ByteBacked {
			return newVMValue(pointer.Type, pointer.array.Backing[pointer.array.Start:pointer.array.Start+pointer.arrayLen])
		}
	case pointerSlot:
		if pointer.slot != nil && pointer.slot.initialized {
			return walker.addressContents(pointer.slot.value, pointer.path, pointer.indexes, false)
		}
	}
	return vmValue{}
}

func (walker *runtimeValueWalker) addressContents(value vmValue, path []ir.AddressPathSegment, indexes []vmValue, indirect bool) vmValue {
	indexPosition := 0
	for position := 0; ; position++ {
		if walker.stopped {
			return vmValue{}
		}
		if position < len(path) {
			if walker.enter != nil {
				if !walker.enter(value) {
					return vmValue{}
				}
				defer walker.leave()
			}
			indirect = true
		}
		if pointer, ok := value.Data.(*vmPointer); ok && indirect {
			if walker.enter != nil {
				if !walker.enter(value) {
					return vmValue{}
				}
				defer walker.leave()
			}
			value = walker.pointerContents(pointer)
		}
		if position == len(path) {
			return value
		}
		indirect = false
		segment := path[position]
		switch segment.Kind {
		case "indirect":
		case "field":
			value, _ = structValueField(value.Data, segment.Field)
		case "index":
			if indexPosition >= len(indexes) {
				return vmValue{}
			}
			index, err := asInt64(indexes[indexPosition])
			indexPosition++
			if err != nil || index < 0 {
				return vmValue{}
			}
			switch data := value.Data.(type) {
			case *vmSlice:
				if data == nil || data.ByteBacked || index >= int64(data.Len) {
					return vmValue{}
				}
				value = data.Backing[data.Start+int(index)]
			case []vmValue:
				if index >= int64(len(data)) {
					return vmValue{}
				}
				value = data[index]
			default:
				return vmValue{}
			}
		default:
			return vmValue{}
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
