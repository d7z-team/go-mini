package runtimes

import (
	engine "gopkg.d7z.net/go-mini/core"
)

func InitAll(executor *engine.MiniExecutor) {
	InitIO(executor)   // io contains io.File, which fs depends on
	InitTime(executor) // time contains time.Time, which fs depends on
	InitFmt(executor)
	InitFs(executor)
	InitOS(executor)
	InitStrings(executor)
}
