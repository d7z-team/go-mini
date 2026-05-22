package engine

import (
	"os"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/lspserv"
)

type lspAnalyzerAdapter struct {
	executor *MiniExecutor
}

func (a lspAnalyzerAdapter) AnalyzeProgramTolerant(program *ast.ProgramStmt, sources map[string]string) (lspserv.ProgramView, []error) {
	return a.executor.AnalyzeProgramTolerant(program, sources)
}

func (e *MiniExecutor) StartStdLspServer() error {
	return lspserv.ServeStream(lspserv.NewLSPServer(lspAnalyzerAdapter{executor: e}), os.Stdin, os.Stdout, os.Stderr)
}
