//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg jsonlib -out json_ffigen.go interface.go
package jsonlib

type JSON interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte) (any, error)
}
