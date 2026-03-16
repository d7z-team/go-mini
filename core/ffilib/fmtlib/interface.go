package fmtlib

type Fmt interface {
	Print(args ...any)
	Println(args ...any)
	Printf(format string, args ...any)
	Sprintf(format string, args ...any) string
}
