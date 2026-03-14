package go_mini

import (
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/runtimes"
)

func NewMiniScriptExecutor() *engine.MiniExecutor {
	executor := engine.NewMiniExecutor()
	runtimes.InitAll(executor)
	return executor
}
