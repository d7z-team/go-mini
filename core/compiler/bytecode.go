package compiler

import (
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/lowering"
)

func buildBytecode(program *ast.ProgramStmt) (*bytecode.Program, error) {
	if program == nil {
		return nil, nil
	}
	bc := bytecode.NewProgram()
	prepared, err := lowering.PrepareProgram(program)
	if err != nil {
		return nil, err
	}
	bc.Executable = prepared
	bc.RefreshDisplayFromExecutable()
	return bc, nil
}
