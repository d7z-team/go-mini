package funcs

import (
	"path/filepath"

	"github.com/spf13/afero"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/runtime/types"
)

func InitFs(executor *engine.MiniExecutor) {
	executor.MustAddPackageFunc("fs", "OS", OsFs, "获取操作系统文件系统实例")
	executor.MustAddPackageFunc("fs", "Memory", MemoryFs, "获取内存文件系统实例")

	// filepath related
	executor.MustAddPackageFunc("fs", "Join", PathJoin, "连接路径片段")
	executor.MustAddPackageFunc("fs", "Abs", PathAbs, "获取绝对路径")
	executor.MustAddPackageFunc("fs", "Base", PathBase, "获取路径的最后一个元素")
	executor.MustAddPackageFunc("fs", "Dir", PathDir, "获取路径的目录部分")
	executor.MustAddPackageFunc("fs", "Ext", PathExt, "获取文件扩展名")
	executor.MustAddPackageFunc("fs", "Clean", PathClean, "清理路径")
	executor.MustAddPackageFunc("fs", "IsAbs", PathIsAbs, "判断是否为绝对路径")
}

func OsFs() *types.MiniFs {
	return types.NewMiniFs(afero.NewOsFs())
}

func MemoryFs() *types.MiniFs {
	return types.NewMiniFs(afero.NewMemMapFs())
}

func PathJoin(elem []ast.MiniString) ast.MiniString {
	parts := make([]string, len(elem))
	for i, e := range elem {
		parts[i] = e.GoString()
	}
	return ast.NewMiniString(filepath.Join(parts...))
}

func PathAbs(path *ast.MiniString) (ast.MiniString, error) {
	res, err := filepath.Abs(path.GoString())
	return ast.NewMiniString(res), err
}

func PathBase(path *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(filepath.Base(path.GoString()))
}

func PathDir(path *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(filepath.Dir(path.GoString()))
}

func PathExt(path *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(filepath.Ext(path.GoString()))
}

func PathClean(path *ast.MiniString) ast.MiniString {
	return ast.NewMiniString(filepath.Clean(path.GoString()))
}

func PathIsAbs(path *ast.MiniString) ast.MiniBool {
	return ast.NewMiniBool(filepath.IsAbs(path.GoString()))
}
