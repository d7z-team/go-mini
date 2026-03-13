package funcs

import (
	"os"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func InitOS(executor *engine.MiniExecutor) {
	executor.MustAddPackageFunc("os", "Getwd", OsGetwd, "获取当前工作目录")
}

func OsGetwd() (ast.MiniString, error) {
	dir, err := os.Getwd()
	return ast.NewMiniString(dir), err
}
