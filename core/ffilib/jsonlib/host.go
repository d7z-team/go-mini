package jsonlib

import (
	"encoding/json"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type JSONHost struct{}

func (h *JSONHost) Marshal(v any) ([]byte, error) {
	return json.Marshal(normalizeJSONValue(v))
}

func (h *JSONHost) Unmarshal(data []byte) (any, error) {
	var v any
	err := json.Unmarshal(data, &v)
	return v, err
}

func normalizeJSONValue(v any) any {
	switch x := v.(type) {
	case *ffigo.VMStruct:
		if x == nil {
			return nil
		}
		m := make(map[string]any, len(x.Fields))
		for _, field := range x.Fields {
			m[field.Name] = normalizeJSONValue(field.Value)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, v := range x {
			m[k] = normalizeJSONValue(v)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeJSONValue(item)
		}
		return out
	default:
		return v
	}
}
