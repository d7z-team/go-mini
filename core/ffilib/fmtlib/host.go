package fmtlib

import (
	"context"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/ffilib"
)

// Outputter 接口定义了自定义输出操作
type Outputter interface {
	Print(context.Context, string)
}

const FMTKey ffilib.CtxKey = "gomini.fmt.Outputter"

// WithOutputter 将 Outputter 注入 context
func WithOutputter(ctx context.Context, o Outputter) context.Context {
	return context.WithValue(ctx, FMTKey, o)
}

type FmtHost struct{}

func (h *FmtHost) Print(ctx context.Context, args ...any) {
	if o, ok := ctx.Value(FMTKey).(Outputter); ok {
		o.Print(ctx, fmt.Sprint(args...))
		return
	}
	fmt.Print(args...) //nolint:forbidigo // native implementation
}

func (h *FmtHost) Println(ctx context.Context, args ...any) {
	if o, ok := ctx.Value(FMTKey).(Outputter); ok {
		o.Print(ctx, fmt.Sprintln(args...))
		return
	}
	fmt.Println(args...) //nolint:forbidigo // native implementation
}

func (h *FmtHost) Printf(ctx context.Context, format string, args ...any) {
	if o, ok := ctx.Value(FMTKey).(Outputter); ok {
		o.Print(ctx, fmt.Sprintf(format, args...))
		return
	}
	fmt.Printf(format, args...) //nolint:forbidigo // native implementation
}

func (h *FmtHost) Sprintf(_ context.Context, format string, args ...any) string {
	return fmt.Sprintf(format, args...)
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
