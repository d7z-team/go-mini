package go_mini

import (
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/runtime/funcs"
	"gopkg.d7z.net/go-mini/runtime/types"
)

func NewMiniScriptExecutor() *engine.MiniExecutor {
	executor := engine.NewMiniExecutor()
	executor.AddNativeStruct((*types.MiniFile)(nil))
	executor.AddNativeStruct((*types.MiniNamedFile)(nil))
	executor.AddNativeStruct((*types.MiniTime)(nil))
	executor.AddNativeStruct((*types.MiniFs)(nil))
	executor.AddNativeStruct((*types.MiniFileInfo)(nil))
	executor.AddNativeStruct((*types.MiniFsFile)(nil))
	executor.AddNativeStruct((*types.MiniBuffer)(nil))

	// init
	funcs.InitSyscall(executor)
	funcs.InitFmt(executor)
	funcs.InitTime(executor)
	funcs.InitStrings(executor)
	funcs.InitFs(executor)
	funcs.InitOS(executor)
	funcs.InitIO(executor)

	return executor
}
