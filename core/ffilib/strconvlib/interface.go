//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg strconvlib -module strconv -path gopkg.d7z.net/go-mini/core/ffilib/strconvlib -out strconv_ffigen.go interface.go
package strconvlib

import (
	"strconv"
)

// Strconv 接口定义了类型转换操作

// ffigen:module strconv
const (
	IntSize = strconv.IntSize
)

// ffigen:module strconv
type Strconv interface {
	Atoi(s string) (int, error)
	Itoa(i int) string
	ParseBool(str string) (bool, error)
	ParseFloat(s string, bitSize int) (float64, error)
	ParseInt(s string, base, bitSize int) (int64, error)
	FormatBool(b bool) string
	FormatFloat(f float64, format byte, prec, bitSize int) string
	FormatInt(i int64, base int) string
	Quote(s string) string
	Unquote(s string) (string, error)
}
