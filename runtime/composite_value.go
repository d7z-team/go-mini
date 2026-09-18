package runtime

import (
	"errors"
	"fmt"
	"sort"

	"github.com/d7z-team/mini-go/compiler/types"
)

type vmSlice struct {
	storage     *vmSliceStorage
	Backing     []vmValue
	ByteBacking []byte
	ByteBacked  bool
	Start       int
	Len         int
	Cap         int
}

type vmSliceStorage struct{ _ byte }

type vmMap struct {
	Entries            map[vmMapKey]vmMapEntry
	nextNonReflexiveID uint64
	nextEntryID        uint64
	entryKeys          map[uint64]vmMapKey
}

type vmMapKey struct {
	Kind         uint8
	TypeIdentity string
	Bool         bool
	Int64        int64
	Uint64       uint64
	Text         string
	NonReflexive bool
	Sequence     uint64
}

const (
	vmMapKeyGeneric uint8 = iota
	vmMapKeyBool
	vmMapKeyString
	vmMapKeyInt
	vmMapKeyUint
)

func (key vmMapKey) String() string {
	if key.NonReflexive {
		return fmt.Sprintf("%s:nonreflexive:%d", key.Text, key.Sequence)
	}
	switch key.Kind {
	case vmMapKeyBool:
		return fmt.Sprintf("%s:bool:%t", key.TypeIdentity, key.Bool)
	case vmMapKeyString:
		return fmt.Sprintf("%s:string:%s", key.TypeIdentity, key.Text)
	case vmMapKeyInt:
		return fmt.Sprintf("%s:int:%d", key.TypeIdentity, key.Int64)
	case vmMapKeyUint:
		return fmt.Sprintf("%s:uint:%d", key.TypeIdentity, key.Uint64)
	default:
		return key.Text
	}
}

type vmMapEntry struct {
	identity uint64
	Key      vmValue
	Value    vmValue
}

func (data *vmMap) storeEntry(key vmMapKey, entry vmMapEntry) {
	entry.identity = data.Entries[key].identity
	if entry.identity == 0 {
		data.nextEntryID++
		entry.identity = data.nextEntryID
		if data.entryKeys == nil {
			data.entryKeys = make(map[uint64]vmMapKey)
		}
		data.entryKeys[entry.identity] = key
	}
	data.Entries[key] = entry
}

func newVMMap(size int) *vmMap {
	return &vmMap{Entries: make(map[vmMapKey]vmMapEntry, size)}
}

func (data *vmMap) keyForStore(key vmMapKey) vmMapKey {
	if data == nil || !key.NonReflexive {
		return key
	}
	data.nextNonReflexiveID++
	key.Sequence = data.nextNonReflexiveID
	return key
}

func sortedVMMapKeys(data *vmMap) []vmMapKey {
	if data == nil || len(data.Entries) == 0 {
		return nil
	}
	keys := make([]vmMapKey, 0, len(data.Entries))
	for key := range data.Entries {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	return keys
}

func newSliceValue(typ any, values []vmValue) vmValue {
	backing := make([]vmValue, len(values))
	copy(backing, values)
	return newSliceHeaderValue(typ, backing, 0, len(backing), cap(backing))
}

func newOwnedSliceValue(typ any, backing []vmValue) vmValue {
	return newSliceHeaderValue(typ, backing, 0, len(backing), cap(backing))
}

func newByteSliceValue(typ any, text string) vmValue {
	backing := []byte(text)
	return newByteSliceHeaderValue(typ, backing, 0, len(backing), cap(backing))
}

func newByteSliceHeaderValue(typ any, backing []byte, start, length, capacity int) vmValue {
	if required := start + capacity; required > len(backing) && required <= cap(backing) {
		backing = backing[:required]
	}
	return newVMValue(typ, &vmSlice{
		storage:     &vmSliceStorage{},
		ByteBacking: backing,
		ByteBacked:  true,
		Start:       start,
		Len:         length,
		Cap:         capacity,
	})
}

func (s *vmSlice) valueAt(index int) vmValue {
	if s.ByteBacked {
		return newVMValue("Uint8", uint64(s.ByteBacking[s.Start+index]))
	}
	return s.Backing[s.Start+index]
}

func (s *vmSlice) setValueAt(index int, value vmValue) error {
	if s.ByteBacked {
		n, err := numericAsUint64(value)
		if err != nil {
			return err
		}
		s.ByteBacking[s.Start+index] = byte(n)
		return nil
	}
	s.Backing[s.Start+index] = value
	return nil
}

func newSliceHeaderValue(typ any, backing []vmValue, start, length, capacity int) vmValue {
	if backing == nil && start == 0 && length == 0 && capacity == 0 {
		return newVMValue(typ, (*vmSlice)(nil))
	}
	if required := start + capacity; required > len(backing) && required <= cap(backing) {
		backing = backing[:required]
	}
	return newVMValue(typ, &vmSlice{
		storage: &vmSliceStorage{},
		Backing: backing,
		Start:   start,
		Len:     length,
		Cap:     capacity,
	})
}

func newSliceViewValue(typ any, source *vmSlice, start, length, capacity int) vmValue {
	if source == nil {
		return newSliceHeaderValue(typ, nil, start, length, capacity)
	}
	return newVMValue(typ, &vmSlice{
		storage: source.storage, Backing: source.Backing, ByteBacking: source.ByteBacking,
		ByteBacked: source.ByteBacked, Start: start, Len: length, Cap: capacity,
	})
}

func sliceValues(value vmValue) ([]vmValue, bool) {
	switch data := value.Data.(type) {
	case *vmSlice:
		if data == nil {
			return nil, true
		}
		if data.Len == 0 {
			return []vmValue{}, true
		}
		if data.ByteBacked {
			values := make([]vmValue, data.Len)
			for i := range values {
				values[i] = data.valueAt(i)
			}
			return values, true
		}
		return data.Backing[data.Start : data.Start+data.Len], true
	default:
		return nil, false
	}
}

func sliceLen(value vmValue) (int, bool) {
	switch data := value.Data.(type) {
	case *vmSlice:
		if data == nil {
			return 0, true
		}
		return data.Len, true
	default:
		return 0, false
	}
}

func sliceCap(value vmValue) (int, bool) {
	switch data := value.Data.(type) {
	case *vmSlice:
		if data == nil {
			return 0, true
		}
		return data.Cap, true
	default:
		return 0, false
	}
}

func newSequenceValue(module *moduleInstance, typ any, values []vmValue) (vmValue, error) {
	runtimeType := module.resolvedRuntimeType(typ)
	typeText := runtimeType.String()
	isArray := false
	if length, _, ok := module.arrayType(typeText); ok {
		isArray = true
		if int64(len(values)) != length {
			return vmValue{}, fmt.Errorf("array length mismatch: got %d, want %d", len(values), length)
		}
	}
	elemType := module.arrayElemType(typeText)
	out := make([]vmValue, len(values))
	for i, value := range values {
		normalized, err := module.coerceAssignableValue(value, elemType)
		if err != nil {
			return vmValue{}, fmt.Errorf("array element %d: %w", i, err)
		}
		out[i] = module.cloneValueForStore(normalized)
	}
	if !isArray && module.isSliceType(typeText) {
		if module.sameRuntimeType(elemType, "Uint8") {
			bytes := make([]byte, len(out))
			for i, value := range out {
				n, err := numericAsUint64(value)
				if err != nil {
					return vmValue{}, fmt.Errorf("byte slice element %d: %w", i, err)
				}
				bytes[i] = byte(n)
			}
			return newByteSliceHeaderValue(runtimeType, bytes, 0, len(bytes), cap(bytes)), nil
		}
		return newSliceValue(runtimeType, out), nil
	}
	return newVMValue(runtimeType, out), nil
}

func newMapValue(module *moduleInstance, typ any, pairs []vmValue) (vmValue, error) {
	return newMapValueWithCapacity(module, typ, pairs, len(pairs)/2)
}

func newMapValueWithCapacity(module *moduleInstance, typ any, pairs []vmValue, capacity int) (vmValue, error) {
	runtimeType := module.resolvedRuntimeType(typ)
	typeText := runtimeType.String()
	if len(pairs)%2 != 0 {
		return vmValue{}, errors.New("map construction requires key/value pairs")
	}
	if capacity < len(pairs)/2 {
		capacity = len(pairs) / 2
	}
	type preparedMapEntry struct {
		key   vmMapKey
		entry vmMapEntry
	}
	entries := make([]preparedMapEntry, 0, len(pairs)/2)
	keyType, valueType, typedMap := module.mapKeyValueTypes(typeText)
	for i := 0; i < len(pairs); i += 2 {
		keyValue := pairs[i]
		valueToStore := pairs[i+1]
		if typedMap {
			normalizedKey, err := module.coerceAssignableValue(pairs[i], keyType)
			if err != nil {
				return vmValue{}, fmt.Errorf("map key %d: %w", i/2, err)
			}
			keyValue = normalizedKey
			normalizedValue, err := module.coerceAssignableValue(pairs[i+1], valueType)
			if err != nil {
				return vmValue{}, fmt.Errorf("map value %d: %w", i/2, err)
			}
			valueToStore = normalizedValue
		}
		key, err := module.mapKey(keyValue)
		if err != nil {
			return vmValue{}, err
		}
		entries = append(entries, preparedMapEntry{
			key: key, entry: vmMapEntry{Key: module.cloneValueForStore(keyValue), Value: module.cloneValueForStore(valueToStore)},
		})
	}
	if module != nil && module.vm != nil {
		_, checkedCapacity, err := module.vm.checkCollectionSize(int64(len(entries)), int64(capacity))
		if err != nil {
			return vmValue{}, err
		}
		capacity = checkedCapacity
		if err := module.vm.chargeRuntimeObject(0, capacity); err != nil {
			return vmValue{}, err
		}
	}
	out := newVMMap(capacity)
	for _, prepared := range entries {
		out.storeEntry(out.keyForStore(prepared.key), prepared.entry)
	}
	return newVMValue(runtimeType, out), nil
}

func newStructValue(module *moduleInstance, typ any, fields []string, values []vmValue) (vmValue, error) {
	runtimeType := module.resolvedRuntimeType(typ)
	schema, ok := module.structSchema(runtimeType)
	if !ok {
		return vmValue{}, fmt.Errorf("unknown struct type %s", runtimeType)
	}
	return newStructValueWithSchema(module, runtimeType, schema, fields, values)
}

func newStructValueWithSchema(module *moduleInstance, runtimeType vmType, schema *structSchema, fields []string, values []vmValue) (vmValue, error) {
	if len(fields) != len(values) {
		return vmValue{}, fmt.Errorf("struct construction has %d fields and %d values", len(fields), len(values))
	}
	if schema == nil {
		return vmValue{}, fmt.Errorf("unknown struct type %s", runtimeType)
	}
	var slots []vmValue
	if len(fields) != 0 {
		slots = make([]vmValue, len(schema.fields))
	}
	for i, field := range fields {
		index, fieldInfo, ok := schema.field(field)
		if !ok {
			return vmValue{}, fmt.Errorf("unknown field %q for %s", field, runtimeType)
		}
		normalized, err := module.coerceAssignableRuntimeValue(values[i], fieldInfo.RuntimeType, fieldInfo.Variadic)
		if err != nil {
			return vmValue{}, fmt.Errorf("field %q: %w", field, err)
		}
		slots[index] = module.cloneValueForStore(normalized)
	}
	return newVMValue(runtimeType, &vmStruct{schema: schema, values: slots}), nil
}

func (m *moduleInstance) cloneValueForStore(value vmValue) vmValue {
	if !value.Type.Valid() || value.Type.Ref.Kind == types.Any {
		return value
	}
	switch value.Type.Ref.Kind {
	case types.Void, types.Primitive, types.Map, types.Pointer, types.Waitable, types.Function:
		return value
	}
	switch value.Type.ShapeKind() {
	case types.Slice:
		return value
	case types.Array:
		items, ok := value.Data.([]vmValue)
		if !ok {
			return value
		}
		out := make([]vmValue, len(items))
		for i, item := range items {
			out[i] = m.cloneValueForStore(item)
		}
		value.Data = out
		return value
	case types.Struct:
		data, ok := value.Data.(*vmStruct)
		if !ok || data == nil {
			return value
		}
		if data.shared {
			return value
		}
		var values []vmValue
		for index, field := range data.values {
			if !field.Type.Valid() {
				continue
			}
			cloned := m.cloneValueForStore(field)
			shape := field.Type.ShapeKind()
			changed := shape == types.Array
			if shape == types.Struct {
				clonedStruct, _ := cloned.Data.(*vmStruct)
				originalStruct, _ := field.Data.(*vmStruct)
				changed = clonedStruct != originalStruct
			}
			if changed {
				if values == nil {
					values = append([]vmValue(nil), data.values...)
				}
				values[index] = cloned
			}
		}
		data.shared = true
		if values != nil {
			value.Data = &vmStruct{schema: data.schema, values: values, sparse: data.sparse}
		}
		return value
	}
	return value
}

func (m *moduleInstance) cloneValueForResult(value vmValue) vmValue {
	if value.Type.ShapeKind() == types.Interface {
		if inner, ok := value.Data.(vmValue); ok {
			value.Data = m.cloneValueForResult(inner)
			return value
		}
	}
	return m.cloneValueForStore(value)
}
