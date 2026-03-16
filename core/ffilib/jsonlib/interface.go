package jsonlib

type JSON interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte) (any, error)
}
