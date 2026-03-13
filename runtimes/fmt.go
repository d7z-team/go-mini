package runtimes

import (
	"errors"
	"fmt"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func InitFmt(executor *engine.MiniExecutor) {
	executor.MustAddPackageFunc("fmt", "Sprintf", func(d []any) (ast.MiniString, error) {
		if len(d) == 0 {
			return ast.NewMiniString(""), nil
		}

		format, err := toString(d[0])
		if err != nil {
			return ast.NewMiniString(""), fmt.Errorf("fmt.Sprintf format must be a string, got %T", d[0])
		}

		args := make([]any, len(d)-1)
		for i := 1; i < len(d); i++ {
			args[i-1] = toGoValue(d[i])
		}

		return ast.NewMiniString(fmt.Sprintf(format, args...)), nil
	}, "格式化并返回字符串")
}

func toString(v any) (string, error) {
	if gv, ok := v.(ast.GoMiniValue); ok {
		v = gv.GoValue()
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	return "", errors.New("not a string")
}

func toGoValue(v any) any {
	if v == nil {
		return nil
	}
	if gv, ok := v.(ast.GoMiniValue); ok {
		return gv.GoValue()
	}
	return v
}
