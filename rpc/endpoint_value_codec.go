package rpc

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
)

// EncodeValues serializes validated RPC values using the transport-neutral
// canonical wire shape. It is used by generated MRPC bindings and the FFI
// extension; Endpoint framing remains a separate concern.
func EncodeValues(values []Value, limits Limits) ([]byte, error) {
	limits = normalizeLimits(limits)
	if err := validateValues(values, limits); err != nil {
		return nil, err
	}
	var encoder wireEncoder
	encoder.Uint(uint64(len(values)))
	for _, value := range values {
		if err := encodeBinaryValue(&encoder, value); err != nil {
			return nil, err
		}
	}
	encoded := encoder.data
	if len(encoded) > limits.MaxMessageBytes {
		return nil, fmt.Errorf("%w: encoded bytes", errValueLimit)
	}
	return encoded, nil
}

// DecodeValues decodes one complete canonical value payload.
func DecodeValues(payload []byte, limits Limits) ([]Value, error) {
	limits = normalizeLimits(limits)
	if len(payload) > limits.MaxMessageBytes {
		return nil, errors.New("rpc value byte limit exceeded")
	}
	decoder := newWireDecoder(payload, limits.MaxMessageBytes)
	count, err := decoder.Uint()
	if err != nil || count > uint64(limits.MaxValueElements) {
		return nil, errors.New("decode RPC values: invalid value count")
	}
	values := make([]Value, int(count))
	for index := range values {
		values[index], err = decodeBinaryValue(decoder, 0, limits)
		if err != nil {
			return nil, fmt.Errorf("decode RPC value %d: %w", index, err)
		}
	}
	if err := decoder.Done(); err != nil {
		return nil, err
	}
	if err := validateValues(values, limits); err != nil {
		return nil, err
	}
	return values, nil
}

const (
	wireNil byte = iota
	wireBool
	wireInt
	wireUint
	wireFloat
	wireComplex
	wireString
	wireBytes
	wireSlice
	wireMap
	wireStruct
	wireOptional
	wireResource
)

func encodeBinaryValue(encoder *wireEncoder, value Value) error {
	encoder.String(value.Type)
	if value.Resource != nil {
		encoder.Uint(uint64(wireResource))
		encoder.Uint(value.Resource.Epoch)
		encoder.Uint(value.Resource.ObjectID)
		encoder.String(value.Resource.TypeHash)
		return nil
	}
	switch data := value.Data.(type) {
	case nil:
		encoder.Uint(uint64(wireNil))
	case bool:
		encoder.Uint(uint64(wireBool))
		encoder.Bool(data)
	case int64:
		encoder.Uint(uint64(wireInt))
		encoder.Int(data)
	case uint64:
		encoder.Uint(uint64(wireUint))
		encoder.Uint(data)
	case float64:
		encoder.Uint(uint64(wireFloat))
		encoder.Float64(data)
	case complex128:
		encoder.Uint(uint64(wireComplex))
		encoder.Complex128(data)
	case string:
		encoder.Uint(uint64(wireString))
		encoder.String(data)
	case []byte:
		encoder.Uint(uint64(wireBytes))
		encoder.Raw(data)
	case []Value:
		encoder.Uint(uint64(wireSlice))
		encoder.Uint(uint64(len(data)))
		for _, item := range data {
			if err := encodeBinaryValue(encoder, item); err != nil {
				return err
			}
		}
	case []MapEntry:
		encoder.Uint(uint64(wireMap))
		entries := make([]struct {
			key   []byte
			value MapEntry
		}, len(data))
		for index, entry := range data {
			var key wireEncoder
			if err := encodeBinaryValue(&key, entry.Key); err != nil {
				return err
			}
			entries[index].key, entries[index].value = key.data, entry
		}
		sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].key, entries[j].key) < 0 })
		encoder.Uint(uint64(len(entries)))
		for _, entry := range entries {
			encoder.data = append(encoder.data, entry.key...)
			if err := encodeBinaryValue(encoder, entry.value.Value); err != nil {
				return err
			}
		}
	case []Field:
		encoder.Uint(uint64(wireStruct))
		fields := append([]Field(nil), data...)
		sort.Slice(fields, func(i, j int) bool { return fields[i].ID < fields[j].ID })
		encoder.Uint(uint64(len(fields)))
		for _, field := range fields {
			encoder.Uint(uint64(field.ID))
			if err := encodeBinaryValue(encoder, field.Value); err != nil {
				return err
			}
		}
	case Value:
		encoder.Uint(uint64(wireOptional))
		return encodeBinaryValue(encoder, data)
	default:
		return fmt.Errorf("unsupported rpc value %T", value.Data)
	}
	return nil
}

func decodeBinaryValue(decoder *wireDecoder, depth int, limits Limits) (Value, error) {
	if depth > limits.MaxValueDepth {
		return Value{}, errors.New("rpc value depth limit exceeded")
	}
	typeName, err := decoder.String()
	if err != nil || typeName == "" {
		return Value{}, errors.New("invalid rpc value type")
	}
	kind, err := decoder.Uint()
	if err != nil {
		return Value{}, err
	}
	value := Value{Type: typeName}
	switch kind {
	case uint64(wireNil):
	case uint64(wireBool):
		value.Data, err = decoder.Bool()
	case uint64(wireInt):
		value.Data, err = decoder.Int()
	case uint64(wireUint):
		value.Data, err = decoder.Uint()
	case uint64(wireFloat):
		value.Data, err = decoder.Float64()
	case uint64(wireComplex):
		value.Data, err = decoder.Complex128()
	case uint64(wireString):
		value.Data, err = decoder.String()
	case uint64(wireBytes):
		var data []byte
		data, err = decoder.rawView()
		value.Data = append([]byte{}, data...)
	case uint64(wireSlice):
		var count uint64
		count, err = decoder.Uint()
		if err == nil && count <= uint64(limits.MaxValueElements) {
			items := make([]Value, int(count))
			for index := range items {
				items[index], err = decodeBinaryValue(decoder, depth+1, limits)
				if err != nil {
					break
				}
			}
			value.Data = items
		} else if err == nil {
			err = errors.New("rpc value element limit exceeded")
		}
	case uint64(wireMap):
		var count uint64
		count, err = decoder.Uint()
		if err == nil && count <= uint64(limits.MaxValueElements) {
			entries := make([]MapEntry, int(count))
			for index := range entries {
				entries[index].Key, err = decodeBinaryValue(decoder, depth+1, limits)
				if err == nil {
					entries[index].Value, err = decodeBinaryValue(decoder, depth+1, limits)
				}
				if err != nil {
					break
				}
			}
			value.Data = entries
		} else if err == nil {
			err = errors.New("rpc value element limit exceeded")
		}
	case uint64(wireStruct):
		var count uint64
		count, err = decoder.Uint()
		if err == nil && count <= uint64(limits.MaxValueElements) {
			fields := make([]Field, int(count))
			for index := range fields {
				var id uint64
				id, err = decoder.Uint()
				if err == nil && (id == 0 || id > uint64(^uint32(0))) {
					err = errors.New("invalid rpc field ID")
				}
				fields[index].ID = uint32(id)
				if err == nil {
					fields[index].Value, err = decodeBinaryValue(decoder, depth+1, limits)
				}
				if err != nil {
					break
				}
			}
			value.Data = fields
		} else if err == nil {
			err = errors.New("rpc value element limit exceeded")
		}
	case uint64(wireOptional):
		value.Data, err = decodeBinaryValue(decoder, depth+1, limits)
	case uint64(wireResource):
		var ref ResourceRef
		ref.Epoch, err = decoder.Uint()
		if err == nil {
			ref.ObjectID, err = decoder.Uint()
		}
		if err == nil {
			ref.TypeHash, err = decoder.String()
		}
		value.Resource = &ref
	default:
		err = errors.New("unknown rpc value kind")
	}
	return value, err
}
