package runtime

import (
	"errors"
	"fmt"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func appendValue(module *moduleInstance, object vmValue, values []vmValue, expand bool) (vmValue, error) {
	if _, _, ok := module.arrayType(object.Type); ok {
		return vmValue{}, fmt.Errorf("cannot append to array %s", object.Type)
	}
	var expandedString string
	stringExpansion := false
	if expand {
		if len(values) != 1 {
			return vmValue{}, errors.New("expanded append requires exactly one source argument")
		}
		if _, _, ok := module.arrayType(values[0].Type); ok {
			return vmValue{}, fmt.Errorf("expanded append source must be slice, got array %s", values[0].Type)
		}
		if text, ok := values[0].Data.(string); ok {
			if !module.sameRuntimeType(module.arrayElemType(object.Type), "Uint8") {
				return vmValue{}, fmt.Errorf("expanded string append requires a byte slice, got %s", object.Type)
			}
			expandedString = text
			stringExpansion = true
		} else {
			expanded, ok := module.sliceValues(values[0])
			if !ok {
				return vmValue{}, fmt.Errorf("cannot expand append argument %s", values[0].Type)
			}
			values = expanded
		}
	}
	if !module.isSliceType(object.Type) {
		return vmValue{}, fmt.Errorf("cannot append to %s", object.Type)
	}
	elemType := module.arrayElemType(object.Type)
	var normalizedValues []vmValue
	valueCount := len(expandedString)
	if !stringExpansion {
		valueCount = len(values)
		normalizedValues = make([]vmValue, len(values))
		for i, value := range values {
			normalized, err := module.coerceAssignableValue(value, elemType)
			if err != nil {
				return vmValue{}, fmt.Errorf("append element %d: %w", i, err)
			}
			normalizedValues[i] = module.cloneValueForStore(normalized)
		}
	}
	oldLen, _ := sliceLen(object)
	oldCap, _ := sliceCap(object)
	newLength := int64(oldLen) + int64(valueCount)
	newCapacity := int64(oldCap)
	if newLength > newCapacity {
		newCapacity = newLength
		if doubled := int64(oldCap) * 2; doubled > newCapacity {
			newCapacity = doubled
		}
	}
	newLen, newCap, err := module.vm.checkCollectionSize(newLength, newCapacity)
	if err != nil {
		return vmValue{}, err
	}
	if module.vm != nil && newCap > oldCap {
		if err := module.vm.chargeAllocationBytes(int64(newCap-oldCap) * ir.RuntimeSlotBytes); err != nil {
			return vmValue{}, err
		}
	}
	if module.sameRuntimeType(elemType, "Uint8") {
		var byteBacking []byte
		var source *vmSlice
		start := 0
		if slice, ok := object.Data.(*vmSlice); ok && slice != nil {
			source = slice
			start = slice.Start
			if slice.ByteBacked {
				byteBacking = slice.ByteBacking
			} else {
				byteBacking = make([]byte, slice.Cap)
				for i := 0; i < slice.Len; i++ {
					n, err := numericAsUint64(slice.valueAt(i))
					if err != nil {
						return vmValue{}, fmt.Errorf("append existing byte %d: %w", i, err)
					}
					byteBacking[i] = byte(n)
				}
				start = 0
			}
		}
		if newLen > oldCap {
			grown := make([]byte, newCap)
			copy(grown, byteBacking[start:start+oldLen])
			byteBacking = grown
			start = 0
		}
		if stringExpansion {
			copy(byteBacking[start+oldLen:start+newLen], expandedString)
		} else {
			for i, value := range normalizedValues {
				n, err := numericAsUint64(value)
				if err != nil {
					return vmValue{}, fmt.Errorf("append byte %d: %w", i, err)
				}
				byteBacking[start+oldLen+i] = byte(n)
			}
		}
		if source != nil && byteBacking != nil && newLen <= oldCap && source.ByteBacked {
			return newSliceViewValue(object.Type.String(), source, start, newLen, newCap), nil
		}
		return newByteSliceHeaderValue(object.Type.String(), byteBacking, start, newLen, newCap), nil
	}
	var backing []vmValue
	var source *vmSlice
	start := 0
	if slice, ok := object.Data.(*vmSlice); ok {
		if slice != nil {
			source = slice
			backing = slice.Backing
			start = slice.Start
		}
	} else if object.Data != nil {
		return vmValue{}, fmt.Errorf("invalid slice backing for %s", object.Type)
	}
	if newLen > oldCap {
		newBacking := make([]vmValue, newCap)
		visible, ok := sliceValues(object)
		if !ok {
			return vmValue{}, fmt.Errorf("invalid slice backing for %s", object.Type)
		}
		copy(newBacking, visible)
		for i := len(visible); i < len(newBacking); i++ {
			newBacking[i] = module.zeroValue(elemType)
		}
		backing = newBacking
		start = 0
	}
	for i, value := range normalizedValues {
		backing[start+oldLen+i] = value
	}
	if source != nil && newLen <= oldCap {
		return newSliceViewValue(object.Type.String(), source, start, newLen, newCap), nil
	}
	return newSliceHeaderValue(object.Type.String(), backing, start, newLen, newCap), nil
}

func deleteValue(module *moduleInstance, object, key vmValue) error {
	mapKey, err := module.normalizedMapKey(object, key)
	if err != nil {
		return err
	}
	switch data := object.Data.(type) {
	case *vmMap:
		if data != nil {
			delete(data.entryKeys, data.Entries[mapKey].identity)
			delete(data.Entries, mapKey)
		}
	case nil:
		if !module.isMapType(object.Type) {
			return fmt.Errorf("cannot delete from %s", object.Type)
		}
	default:
		return fmt.Errorf("cannot delete from %s", object.Type)
	}
	return nil
}

func mapKeysValue(module *moduleInstance, object vmValue) (vmValue, error) {
	keyType := "Any"
	if parsedKeyType, _, ok := module.mapKeyValueTypes(object.Type); ok {
		keyType = parsedKeyType
	}
	var values []vmValue
	switch data := object.Data.(type) {
	case *vmMap:
		keys := sortedVMMapKeys(data)
		values = make([]vmValue, 0, len(keys))
		for _, key := range keys {
			values = append(values, module.cloneValueForStore(data.Entries[key].Key))
		}
	case nil:
		if !module.isMapType(object.Type) {
			return vmValue{}, fmt.Errorf("cannot get map keys from %s", object.Type)
		}
	default:
		return vmValue{}, fmt.Errorf("cannot get map keys from %s", object.Type)
	}
	return newSliceValue("Slice<"+keyType+">", values), nil
}

func clearValue(module *moduleInstance, object vmValue) error {
	if _, _, ok := module.arrayType(object.Type); ok {
		return fmt.Errorf("cannot clear array %s", object.Type)
	}
	switch data := object.Data.(type) {
	case *vmSlice:
		if data == nil {
			return nil
		}
		elemType := module.arrayElemType(object.Type)
		for i := 0; i < data.Len; i++ {
			if data.ByteBacked {
				data.ByteBacking[data.Start+i] = 0
				continue
			}
			if module != nil {
				data.Backing[data.Start+i] = module.zeroValue(elemType)
				continue
			}
			data.Backing[data.Start+i] = zeroVMValue(elemType)
		}
		return nil
	case []vmValue:
		if module.isSliceType(object.Type) {
			return fmt.Errorf("invalid slice backing for %s", object.Type)
		}
		elemType := module.arrayElemType(object.Type)
		for i := range data {
			if module != nil {
				data[i] = module.zeroValue(elemType)
				continue
			}
			data[i] = zeroVMValue(elemType)
		}
		return nil
	case *vmMap:
		if data != nil {
			clear(data.Entries)
			clear(data.entryKeys)
		}
		return nil
	case nil:
		if module.isSliceType(object.Type) || module.isMapType(object.Type) {
			return nil
		}
		return fmt.Errorf("cannot clear %s", object.Type)
	default:
		return fmt.Errorf("cannot clear %s", object.Type)
	}
}

func copyValue(module *moduleInstance, dst, src vmValue) (vmValue, error) {
	if _, _, ok := module.arrayType(dst.Type); ok {
		return vmValue{}, fmt.Errorf("copy destination must be slice, got array %s", dst.Type)
	}
	if _, _, ok := module.arrayType(src.Type); ok {
		return vmValue{}, fmt.Errorf("copy source must be slice or string, got array %s", src.Type)
	}
	if !module.isSliceType(dst.Type) {
		return vmValue{}, fmt.Errorf("copy destination must be array or slice, got %s", dst.Type)
	}
	elemType := module.arrayElemType(dst.Type)
	dstSlice, ok := dst.Data.(*vmSlice)
	if !ok {
		return vmValue{}, fmt.Errorf("copy destination must be slice, got %s", dst.Type)
	}
	dstLen := 0
	if dstSlice != nil {
		dstLen = dstSlice.Len
	}
	switch data := src.Data.(type) {
	case *vmSlice:
		if !module.sameRuntimeType(module.arrayElemType(src.Type), elemType) {
			return vmValue{}, fmt.Errorf("copy element 0: %s is not %s", module.arrayElemType(src.Type), elemType)
		}
		srcLen := 0
		if data != nil {
			srcLen = data.Len
		}
		count := min(dstLen, srcLen)
		copied := make([]vmValue, count)
		for i := 0; i < count; i++ {
			normalized, err := module.coerceAssignableValue(data.valueAt(i), elemType)
			if err != nil {
				return vmValue{}, fmt.Errorf("copy element %d: %w", i, err)
			}
			copied[i] = module.cloneValueForStore(normalized)
		}
		for i := range copied {
			if err := dstSlice.setValueAt(i, copied[i]); err != nil {
				return vmValue{}, fmt.Errorf("copy element %d: %w", i, err)
			}
		}
		return newVMValue("Int", int64(count)), nil
	case []vmValue:
		if module.isSliceType(src.Type) {
			return vmValue{}, fmt.Errorf("invalid slice backing for %s", src.Type)
		}
		if !module.sameRuntimeType(module.arrayElemType(src.Type), elemType) {
			return vmValue{}, fmt.Errorf("copy element 0: %s is not %s", module.arrayElemType(src.Type), elemType)
		}
		count := min(dstLen, len(data))
		for i := 0; i < count; i++ {
			normalized, err := module.coerceAssignableValue(data[i], elemType)
			if err != nil {
				return vmValue{}, fmt.Errorf("copy element %d: %w", i, err)
			}
			if err := dstSlice.setValueAt(i, module.cloneValueForStore(normalized)); err != nil {
				return vmValue{}, fmt.Errorf("copy element %d: %w", i, err)
			}
		}
		return newVMValue("Int", int64(count)), nil
	case string:
		count := min(dstLen, len(data))
		for i := 0; i < count; i++ {
			normalized, err := module.coerceAssignableValue(newVMValue("Uint8", uint64(data[i])), elemType)
			if err != nil {
				return vmValue{}, fmt.Errorf("copy byte %d: %w", i, err)
			}
			if err := dstSlice.setValueAt(i, normalized); err != nil {
				return vmValue{}, fmt.Errorf("copy byte %d: %w", i, err)
			}
		}
		return newVMValue("Int", int64(count)), nil
	case nil:
		return newVMValue("Int", int64(0)), nil
	default:
		return vmValue{}, fmt.Errorf("copy source must be array, slice, or string, got %s", src.Type)
	}
}
