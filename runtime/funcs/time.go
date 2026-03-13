package funcs

import (
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/runtime/types"
)

func InitTime(executor *engine.MiniExecutor) {
	executor.MustAddPackageFunc("time", "Now", Now, "获取当前时间对象")
	executor.MustAddPackageFunc("time", "Parse", ParseTime, "解析时间字符串")
	executor.MustAddPackageFunc("time", "Unix", UnixTime, "根据秒数和纳秒数创建时间对象")
}

func Now() *types.MiniTime {
	return types.NewMiniTime(time.Now())
}

func ParseTime(layout, value *ast.MiniString) (*types.MiniTime, error) {
	t, err := time.Parse(layout.GoString(), value.GoString())
	if err != nil {
		return nil, err
	}
	return types.NewMiniTime(t), nil
}

func UnixTime(sec, nsec *ast.MiniInt64) *types.MiniTime {
	return types.NewMiniTime(time.Unix(sec.GoValue().(int64), nsec.GoValue().(int64)))
}
