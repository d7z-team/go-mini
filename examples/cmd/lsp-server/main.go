package main

import (
	"os"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/ffilib"
)

func main() {
	executor := engine.NewMiniExecutor()
	if err := executor.UseSurface(ffilib.Surface()); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	if err := executor.StartStdLspServer(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
