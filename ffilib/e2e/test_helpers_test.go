package e2e_test

import (
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/ffilib"
)

func newStdExecutor() *engine.MiniExecutor {
	executor := engine.NewMiniExecutor()
	ffilib.RegisterAll(executor)
	return executor
}
