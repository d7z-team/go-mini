//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg fmtlib -out fmt_ffigen.go interface.go
package fmtlib

// Fmt 接口定义了格式化输出操作
//
// ffigen:module fmt
type Fmt interface {
	Print(args ...any)
	Println(args ...any)
	Printf(format string, args ...any)
	Sprintf(format string, args ...any) string
}
