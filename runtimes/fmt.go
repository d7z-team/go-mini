package runtimes

import (
	"errors"
	"fmt"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func InitFmt(executor *engine.MiniExecutor) {
	executor.MustAddPackageFunc("fmt", "Sprintf", func(d ast.MiniArray) (ast.MiniString, error) {
		if d.Len() == 0 {
			return ast.NewMiniString(""), nil
		}

		fObj, _ := d.Get(0)
		format, err := toString(fObj)
		if err != nil {
			return ast.NewMiniString(""), fmt.Errorf("fmt.Sprintf format must be a string, got %T", fObj)
		}

		args := make([]any, d.Len()-1)
		for i := 1; i < d.Len(); i++ {
			item, _ := d.Get(i)
			args[i-1] = toGoValue(item)
		}

		return ast.NewMiniString(fmt.Sprintf(format, args...)), nil
	}, "格式化并返回字符串")
}

func toString(v any) (string, error) {
	v = toGoValue(v)
	if s, ok := v.(string); ok {
		return s, nil
	}
	return "", errors.New("not a string")
}

func toGoValue(v any) any {
	if v == nil {
		return nil
	}
	v = runtime.UnwrapProxy(v)
	if gv, ok := v.(ast.GoMiniValue); ok {
		return gv.GoValue()
	}
	return v
}
