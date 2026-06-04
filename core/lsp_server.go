package engine

import (
	"os"

	"gopkg.d7z.net/go-mini/core/lspserv"
)

type lspAnalyzerAdapter struct {
	executor *MiniExecutor
}

func (a lspAnalyzerAdapter) AnalyzeSnapshot(snapshot lspserv.PackageSnapshot, options lspserv.AnalysisOptions) (lspserv.AnalysisResult, error) {
	return a.executor.AnalyzeSnapshot(snapshot, options)
}

func (e *MiniExecutor) StartStdLspServer() error {
	return lspserv.ServeStream(lspserv.NewLSPServer(lspAnalyzerAdapter{executor: e}), os.Stdin, os.Stdout, os.Stderr)
}
