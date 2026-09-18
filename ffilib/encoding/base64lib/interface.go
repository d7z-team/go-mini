//go:generate go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg base64lib -out base64_ffigen.go interface.go host.go
package base64lib

import "encoding/base64"

// ffigen:module encoding/base64
const (
	StdPadding = base64.StdPadding
	NoPadding  = base64.NoPadding
)

// ffigen:module encoding/base64
type Module interface {
	NewEncoding(encoder string) *Encoding
}
