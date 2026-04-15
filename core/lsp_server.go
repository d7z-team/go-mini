package engine

import (
	"os"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/lspserv"
)

type lspAnalyzerAdapter struct {
	executor *MiniExecutor
}

func (a lspAnalyzerAdapter) AnalyzeProgramTolerant(program *ast.ProgramStmt) (lspserv.ProgramView, []error) {
	return a.executor.AnalyzeProgramTolerant(program)
}

func (e *MiniExecutor) StartStdLspServer() error {
	return lspserv.ServeStream(lspserv.NewLSPServer(lspAnalyzerAdapter{executor: e}), os.Stdin, os.Stdout, os.Stderr)
}
