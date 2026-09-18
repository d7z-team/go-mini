package lower

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// canonicalStringRaw encodes a string without depending on the host
// encoding/json implementation's choice of equivalent JSON escapes.
func canonicalStringRaw(value string) json.RawMessage {
	const hex = "0123456789abcdef"
	var out strings.Builder
	out.Grow(len(value) + 2)
	out.WriteByte('"')
	for offset := 0; offset < len(value); {
		r, size := utf8.DecodeRuneInString(value[offset:])
		offset += size
		switch r {
		case utf8.RuneError:
			out.WriteString(`\ufffd`)
		case '\\':
			out.WriteString(`\\`)
		case '"':
			out.WriteString(`\"`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		case '<':
			out.WriteString(`\u003c`)
		case '>':
			out.WriteString(`\u003e`)
		case '&':
			out.WriteString(`\u0026`)
		case '\u2028':
			out.WriteString(`\u2028`)
		case '\u2029':
			out.WriteString(`\u2029`)
		default:
			if r < 0x20 {
				out.WriteString(`\u00`)
				out.WriteByte(hex[byte(r)>>4])
				out.WriteByte(hex[byte(r)&0xf])
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return json.RawMessage(out.String())
}
