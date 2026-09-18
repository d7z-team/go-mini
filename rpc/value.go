package rpc

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var errValueLimit = errors.New("rpc value limit exceeded")

type Value struct {
	Type     string
	Data     any
	Resource *ResourceRef
}

type MapEntry struct {
	Key   Value
	Value Value
}

type Field struct {
	ID    uint32
	Value Value
}

func cloneValues(values []Value) []Value {
	out := make([]Value, len(values))
	for i, value := range values {
		out[i] = cloneValue(value)
	}
	return out
}

func cloneValue(value Value) Value {
	out := value
	if value.Resource != nil {
		ref := *value.Resource
		out.Resource = &ref
	}
	switch data := value.Data.(type) {
	case []byte:
		out.Data = append([]byte{}, data...)
	case []Value:
		out.Data = cloneValues(data)
	case []MapEntry:
		entries := make([]MapEntry, len(data))
		for index, entry := range data {
			entries[index] = MapEntry{Key: cloneValue(entry.Key), Value: cloneValue(entry.Value)}
		}
		out.Data = entries
	case []Field:
		fields := make([]Field, len(data))
		for index, field := range data {
			fields[index] = Field{ID: field.ID, Value: cloneValue(field.Value)}
		}
		out.Data = fields
	case Value:
		out.Data = cloneValue(data)
	}
	return out
}

func validateValues(values []Value, limits Limits) error {
	bytes, elements := 0, 0
	var validate func(Value, int) error
	validate = func(value Value, depth int) error {
		if value.Type == "" {
			return errors.New("rpc value has no type")
		}
		if !utf8.ValidString(value.Type) {
			return errors.New("rpc value type is not valid UTF-8")
		}
		if depth > limits.MaxValueDepth {
			return fmt.Errorf("%w: depth", errValueLimit)
		}
		bytes += len(value.Type)
		elements++
		if bytes > limits.MaxMessageBytes || elements > limits.MaxValueElements {
			return fmt.Errorf("%w: size", errValueLimit)
		}
		if value.Resource != nil {
			if depth != 0 {
				return errors.New("rpc resource must be a top-level value")
			}
			if value.Data != nil || validateResourceRef(*value.Resource) != nil || strings.TrimSpace(value.Type) != value.Resource.TypeHash {
				return errors.New("invalid rpc resource value")
			}
			return nil
		}
		if err := validateValueShape(value.Type, value.Data); err != nil {
			return err
		}
		switch data := value.Data.(type) {
		case nil, bool, int64, uint64, float64, complex128:
			return nil
		case string:
			if !utf8.ValidString(data) {
				return errors.New("rpc string value is not valid UTF-8")
			}
			bytes += len(data)
		case []byte:
			bytes += len(data)
		case []Value:
			for _, item := range data {
				if err := validate(item, depth+1); err != nil {
					return err
				}
			}
		case []MapEntry:
			for _, entry := range data {
				if err := validate(entry.Key, depth+1); err != nil {
					return err
				}
				if err := validate(entry.Value, depth+1); err != nil {
					return err
				}
			}
		case []Field:
			seen := make(map[uint32]struct{}, len(data))
			for _, field := range data {
				if field.ID == 0 {
					return errors.New("rpc struct field has no ID")
				}
				if _, duplicate := seen[field.ID]; duplicate {
					return errors.New("duplicate rpc struct field")
				}
				seen[field.ID] = struct{}{}
				if err := validate(field.Value, depth+1); err != nil {
					return err
				}
			}
		case Value:
			return validate(data, depth+1)
		default:
			return fmt.Errorf("unsupported rpc value %T", value.Data)
		}
		if bytes > limits.MaxMessageBytes || elements > limits.MaxValueElements {
			return fmt.Errorf("%w: size", errValueLimit)
		}
		return nil
	}
	for _, value := range values {
		if err := validate(value, 0); err != nil {
			return err
		}
	}
	return nil
}

func validateValueShape(typ string, data any) error {
	typ = strings.TrimSpace(typ)
	if data == nil {
		return nil
	}
	if strings.HasPrefix(typ, "optional[") && strings.HasSuffix(typ, "]") {
		elementType := strings.TrimSpace(typ[len("optional[") : len(typ)-1])
		value, ok := data.(Value)
		if !ok || strings.TrimSpace(value.Type) != elementType {
			return fmt.Errorf("rpc value %s has data %T", typ, data)
		}
		return nil
	}
	if strings.HasPrefix(typ, "[]") {
		if typ == "[]uint8" {
			if _, ok := data.([]byte); ok {
				return nil
			}
		} else if values, ok := data.([]Value); ok {
			elementType := strings.TrimSpace(typ[2:])
			for index, value := range values {
				if strings.TrimSpace(value.Type) != elementType {
					return fmt.Errorf("rpc value %s item %d has type %s", typ, index, value.Type)
				}
			}
			return nil
		}
		return fmt.Errorf("rpc value %s has data %T", typ, data)
	}
	if strings.HasPrefix(typ, "map[") {
		keyType, valueType, ok := rpcMapTypes(typ)
		if entries, entriesOK := data.([]MapEntry); ok && entriesOK {
			for index, entry := range entries {
				if strings.TrimSpace(entry.Key.Type) != keyType || strings.TrimSpace(entry.Value.Type) != valueType {
					return fmt.Errorf("rpc value %s entry %d has types %s and %s", typ, index, entry.Key.Type, entry.Value.Type)
				}
			}
			return nil
		}
		return fmt.Errorf("rpc value %s has data %T", typ, data)
	}
	var valid bool
	switch typ {
	case "bool":
		_, valid = data.(bool)
	case "string":
		_, valid = data.(string)
	case "uint8", "uint16", "uint32", "uint64":
		_, valid = data.(uint64)
	case "int8", "int16", "int32", "int64":
		_, valid = data.(int64)
	case "float32", "float64":
		_, valid = data.(float64)
	case "complex64", "complex128":
		_, valid = data.(complex128)
	default:
		_, valid = data.([]Field)
		// Named MRPC values are messages or signed 32-bit enums. The exact
		// declaration is selected by the bound schema contract.
		if enum, ok := data.(int64); ok {
			valid = strings.Contains(typ, ".") && enum >= -1<<31 && enum <= 1<<31-1
		}
	}
	if !valid {
		return fmt.Errorf("rpc value %s has data %T", typ, data)
	}
	return nil
}

func rpcMapTypes(typ string) (string, string, bool) {
	depth := 1
	for index := len("map["); index < len(typ); index++ {
		switch typ[index] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				key, value := strings.TrimSpace(typ[len("map["):index]), strings.TrimSpace(typ[index+1:])
				return key, value, key != "" && value != ""
			}
		}
	}
	return "", "", false
}
