package go_mini

import (
	engine "gopkg.d7z.net/go-mini/core"
)

func NewMiniScriptExecutor() *engine.MiniExecutor {
	executor := engine.NewMiniExecutor()
	return executor
}
