package jsonlib

import (
	"encoding/json"
)

type JSONHost struct{}

func (h *JSONHost) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (h *JSONHost) Unmarshal(data []byte) (any, error) {
	var v any
	err := json.Unmarshal(data, &v)
	return v, err
}
