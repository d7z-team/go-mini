package main

import (
	"os"

	engine "gopkg.d7z.net/go-mini/core"
)

func main() {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()
	if err := executor.StartStdLspServer(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
