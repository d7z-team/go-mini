package engine

import (
	"errors"

	"gopkg.d7z.net/go-mini/core/bytecode"
)

func (e *MiniExecutor) CompileGoCodeToBytecodeJSON(code string) ([]byte, error) {
	compiled, err := e.CompileGoCode(code)
	if err != nil {
		return nil, err
	}
	return compiled.MarshalBytecodeJSON()
}

func (e *MiniExecutor) CompileGoCodeToBytecode(code string) (*bytecode.Program, error) {
	compiled, err := e.CompileGoCode(code)
	if err != nil {
		return nil, err
	}
	if compiled == nil || compiled.Bytecode == nil {
		return nil, errors.New("compiled program missing bytecode")
	}
	return compiled.Bytecode, nil
}

func (e *MiniExecutor) CompileGoFileToBytecode(filename, code string) (*bytecode.Program, error) {
	compiled, err := e.CompileGoFile(filename, code)
	if err != nil {
		return nil, err
	}
	if compiled == nil || compiled.Bytecode == nil {
		return nil, errors.New("compiled program missing bytecode")
	}
	return compiled.Bytecode, nil
}
