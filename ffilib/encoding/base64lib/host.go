package base64lib

import (
	"encoding/base64"

	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/surface"
)

// ffigen:global encoding/base64 StdEncoding HostRef<encoding/base64.Encoding>
var StdEncoding = &Encoding{Enc: base64.StdEncoding}

// ffigen:global encoding/base64 URLEncoding HostRef<encoding/base64.Encoding>
var URLEncoding = &Encoding{Enc: base64.URLEncoding}

// ffigen:global encoding/base64 RawStdEncoding HostRef<encoding/base64.Encoding>
var RawStdEncoding = &Encoding{Enc: base64.RawStdEncoding}

// ffigen:global encoding/base64 RawURLEncoding HostRef<encoding/base64.Encoding>
var RawURLEncoding = &Encoding{Enc: base64.RawURLEncoding}

type ModuleHost struct{}

func (h *ModuleHost) NewEncoding(encoder string) *Encoding {
	return &Encoding{Enc: base64.NewEncoding(encoder)}
}

// ffigen:methods
type Encoding struct {
	Enc *base64.Encoding
}

func (e *Encoding) Encode(dst *ffigo.BytesRef, src []byte) {
	if e == nil || e.Enc == nil || dst == nil {
		return
	}
	e.Enc.Encode(dst.Value, src)
}

func (e *Encoding) Decode(dst *ffigo.BytesRef, src []byte) (int64, error) {
	if e == nil || e.Enc == nil || dst == nil {
		return 0, nil
	}
	n, err := e.Enc.Decode(dst.Value, src)
	return int64(n), err
}

func (e *Encoding) EncodeToString(src []byte) string {
	if e == nil || e.Enc == nil {
		return ""
	}
	return e.Enc.EncodeToString(src)
}

func (e *Encoding) DecodeString(s string) ([]byte, error) {
	if e == nil || e.Enc == nil {
		return nil, nil
	}
	return e.Enc.DecodeString(s)
}

func (e *Encoding) AppendEncode(dst, src []byte) []byte {
	if e == nil || e.Enc == nil {
		return dst
	}
	return e.Enc.AppendEncode(dst, src)
}

func (e *Encoding) AppendDecode(dst, src []byte) ([]byte, error) {
	if e == nil || e.Enc == nil {
		return dst, nil
	}
	return e.Enc.AppendDecode(dst, src)
}

func (e *Encoding) EncodedLen(n int64) int64 {
	if e == nil || e.Enc == nil {
		return 0
	}
	return int64(e.Enc.EncodedLen(int(n)))
}

func (e *Encoding) DecodedLen(n int64) int64 {
	if e == nil || e.Enc == nil {
		return 0
	}
	return int64(e.Enc.DecodedLen(int(n)))
}

func (e *Encoding) WithPadding(padding int64) *Encoding {
	if e == nil || e.Enc == nil {
		return nil
	}
	return &Encoding{Enc: e.Enc.WithPadding(rune(padding))}
}

func (e *Encoding) Strict() *Encoding {
	if e == nil || e.Enc == nil {
		return nil
	}
	return &Encoding{Enc: e.Enc.Strict()}
}

func Surface() *surface.Bundle {
	return surface.Merge(
		SurfaceModule(&ModuleHost{}),
		SurfaceEncoding(),
		SurfaceGlobals(),
	)
}
