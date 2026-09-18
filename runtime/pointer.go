package runtime

import (
	"errors"
	"fmt"

	"github.com/d7z-team/mini-go/compiler/types"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

type vmPointer struct {
	Type           vmType
	Identity       string
	original       *vmPointer
	load           func() (vmValue, error)
	store          func(vmValue) error
	commitMutation func(vmValue) error
	slot           *slot
	path           []ir.AddressPathSegment
	indexes        []vmValue
}

func newPointerValue(typ any, load func() (vmValue, error), store func(vmValue) error) vmValue {
	pointerTypeValue := coerceRuntimeType(typ)
	pointer := &vmPointer{Type: pointerTypeValue, load: load, store: store}
	pointer.Identity = fmt.Sprintf("pointer:%p", pointer)
	return newVMValue(pointerType(pointerTypeValue), pointer)
}

func newPointerValueWithIdentity(typ any, identity string, load func() (vmValue, error), store func(vmValue) error) vmValue {
	return newPointerValueWithMutation(typ, identity, load, store, store)
}

func newPointerValueWithMutation(typ any, identity string, load func() (vmValue, error), store, commitMutation func(vmValue) error) vmValue {
	pointerTypeValue := coerceRuntimeType(typ)
	pointer := &vmPointer{Type: pointerTypeValue, Identity: identity, load: load, store: store, commitMutation: commitMutation}
	if pointer.Identity == "" {
		pointer.Identity = fmt.Sprintf("pointer:%p", pointer)
	}
	return newVMValue(pointerType(pointerTypeValue), pointer)
}

func newSlotPointerValue(typ any, identity string, cell *slot) vmValue {
	pointerTypeValue := coerceRuntimeType(typ)
	return newVMValue(pointerType(pointerTypeValue), &vmPointer{Type: pointerTypeValue, Identity: identity, slot: cell})
}

func newPathPointerValue(typ any, identity string, cell *slot, path []ir.AddressPathSegment, indexes []vmValue) vmValue {
	pointerTypeValue := coerceRuntimeType(typ)
	return newVMValue(pointerType(pointerTypeValue), &vmPointer{
		Type: pointerTypeValue, Identity: identity, slot: cell,
		path: path, indexes: indexes,
	})
}

func (pointer *vmPointer) loadValue() (vmValue, error) {
	if pointer.original != nil {
		value, err := pointer.original.loadValue()
		value.Type = pointer.Type
		return value, err
	}
	if pointer.load != nil {
		return pointer.load()
	}
	if pointer.slot == nil {
		return vmValue{}, errors.New("invalid pointer")
	}
	if len(pointer.path) == 0 {
		return pointer.slot.load(), nil
	}
	return loadAddressPath(pointer.slot.module, pointer.slot.load(), pointer.path, pointer.indexes)
}

func (pointer *vmPointer) storeValue(value vmValue, mutation bool) error {
	if pointer.original != nil {
		value.Type = pointer.original.Type
		return pointer.original.storeValue(value, mutation)
	}
	if mutation && pointer.commitMutation != nil {
		return pointer.commitMutation(value)
	}
	if pointer.store != nil {
		return pointer.store(value)
	}
	if pointer.slot == nil {
		return errors.New("invalid pointer")
	}
	if len(pointer.path) == 0 {
		if mutation {
			pointer.slot.value = value
			pointer.slot.initialized = true
			return nil
		}
		return pointer.slot.store(value)
	}
	updated, err := storeAddressPathMode(pointer.slot.module, pointer.slot.load(), pointer.path, pointer.indexes, value, !mutation)
	if err != nil {
		return err
	}
	pointer.slot.value = updated
	pointer.slot.initialized = true
	return nil
}

func commitPointerMutation(pointerValueInput, value vmValue) error {
	pointer, err := pointerValue(pointerValueInput)
	if err != nil {
		return err
	}
	return pointer.storeValue(value, true)
}

func pointerType(elem vmType) vmType {
	return runtimeTypeFromText("Ptr<" + elem.String() + ">")
}

func derefPointer(value vmValue) (vmValue, error) {
	pointer, err := pointerValue(value)
	if err != nil {
		return vmValue{}, err
	}
	return pointer.loadValue()
}

func storePointer(pointerValueInput, value vmValue) error {
	pointer, err := pointerValue(pointerValueInput)
	if err != nil {
		return err
	}
	return pointer.storeValue(value, false)
}

func pointerValue(value vmValue) (*vmPointer, error) {
	if value.Data == nil && value.Type.ShapeKind() == types.Pointer {
		return nil, newGuestPanic(errors.New("invalid memory address or nil pointer dereference"))
	}
	pointer, ok := value.Data.(*vmPointer)
	if !ok {
		return nil, fmt.Errorf("expected pointer payload for %s, got %T", value.Type, value.Data)
	}
	if pointer == nil {
		return nil, newGuestPanic(errors.New("invalid memory address or nil pointer dereference"))
	}
	if pointer.original == nil && (pointer.load == nil || pointer.store == nil) && pointer.slot == nil {
		return nil, errors.New("invalid pointer")
	}
	return pointer, nil
}
