package fmtlib

import (
	"fmt"
	"io"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

type FmtHost struct{}

func (h *FmtHost) Print(args ...any) {
	fmt.Print(args...) //nolint:forbidigo // native implementation
}

func (h *FmtHost) Println(args ...any) {
	fmt.Println(args...) //nolint:forbidigo // native implementation
}

func (h *FmtHost) Printf(format string, args ...any) {
	fmt.Printf(format, args...) //nolint:forbidigo // native implementation
}

func (h *FmtHost) Sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func (h *FmtHost) Fprint(w any, args ...any) {
	if writer, ok := w.(io.Writer); ok {
		fmt.Fprint(writer, args...)
	}
}

func (h *FmtHost) Fprintf(w any, format string, args ...any) {
	if writer, ok := w.(io.Writer); ok {
		fmt.Fprintf(writer, format, args...)
	}
}

func (h *FmtHost) Fprintln(w any, args ...any) {
	if writer, ok := w.(io.Writer); ok {
		fmt.Fprintln(writer, args...)
	}
}

// RegisterFmtAliases 注册全局的 print 和 println 别名
func RegisterFmtAliases(executor interface {
	RegisterFFI(string, ffigo.FFIBridge, uint32, ast.GoMiniType, string)
}, impl Fmt, registry *ffigo.HandleRegistry,
) {
	bridge := &Fmt_Bridge{Impl: impl, Registry: registry}
	executor.RegisterFFI("print", bridge, MethodID_Fmt_Print, "function(...Any) Void", "Print values to stdout")
	executor.RegisterFFI("println", bridge, MethodID_Fmt_Println, "function(...Any) Void", "Print values to stdout with newline")
}
