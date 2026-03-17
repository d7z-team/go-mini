//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg fmtlib -out fmt_ffigen.go interface.go
package fmtlib

type Fmt interface {
	Print(args ...any)
	Println(args ...any)
	Printf(format string, args ...any)
	Sprintf(format string, args ...any) string
}
