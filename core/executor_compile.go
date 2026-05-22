package engine

import (
	"errors"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func (e *MiniExecutor) CompileProgram(program *ast.ProgramStmt) (*compiler.Artifact, error) {
	compiled, semanticCtx, err := e.newCompiler().CompileProgram("ast", "", program, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, program)
	}
	if err := e.prepareArtifactModules(compiled); err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	return compiled, nil
}

func (e *MiniExecutor) CompileFiles(files []SourceFile) (*compiler.Artifact, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileFiles(files, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	if err := e.prepareArtifactModules(compiled); err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	return compiled, nil
}

func (e *MiniExecutor) CompileDir(dir string) (*compiler.Artifact, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileDir(dir, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	if err := e.prepareArtifactModules(compiled); err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	return compiled, nil
}

func (e *MiniExecutor) NewRuntimeByCompiled(compiled *compiler.Artifact) (*MiniProgram, error) {
	if compiled == nil {
		return nil, errors.New("invalid compiled program")
	}
	if compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
		return nil, errors.New("compiled program missing executable bytecode")
	}

	executor, err := runtime.NewExecutorFromPrepared(compiled.Bytecode.Executable)
	if err != nil {
		return nil, err
	}
	if err := e.applyExecutorConfig(executor); err != nil {
		return nil, err
	}

	return &MiniProgram{
		Source:           compiled.Source,
		Program:          compiled.Program,
		Compiled:         compiled,
		TemplatePreviews: compiled.TemplatePreviews,
		executor:         executor,
	}, nil
}

func (e *MiniExecutor) NewRuntimeByFiles(files []SourceFile) (*MiniProgram, error) {
	compiled, err := e.CompileFiles(files)
	if err != nil {
		return nil, err
	}
	return e.NewRuntimeByCompiled(compiled)
}

func (e *MiniExecutor) NewRuntimeByDir(dir string) (*MiniProgram, error) {
	compiled, err := e.CompileDir(dir)
	if err != nil {
		return nil, err
	}
	return e.NewRuntimeByCompiled(compiled)
}

func (e *MiniExecutor) NewRuntimeByBytecode(program *bytecode.Program) (*MiniProgram, error) {
	compiled, err := compiler.ArtifactFromBytecode(program)
	if err != nil {
		return nil, err
	}
	return e.NewRuntimeByCompiled(compiled)
}

func (e *MiniExecutor) ArtifactFromBytecode(program *bytecode.Program) (*compiler.Artifact, error) {
	return compiler.ArtifactFromBytecode(program)
}

func (e *MiniExecutor) ArtifactFromBytecodeJSON(payload []byte) (*compiler.Artifact, error) {
	return compiler.ArtifactFromBytecodeJSON(payload)
}

func (e *MiniExecutor) NewRuntimeByBytecodeJSON(payload []byte) (*MiniProgram, error) {
	program, err := bytecode.UnmarshalJSON(payload)
	if err != nil {
		return nil, err
	}
	return e.NewRuntimeByBytecode(program)
}

func (e *MiniExecutor) CompileGoCode(code string) (*compiler.Artifact, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileSource("snippet", code, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	if err := e.prepareArtifactModules(compiled); err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	return compiled, nil
}

func (e *MiniExecutor) CompileGoFile(filename, code string) (*compiler.Artifact, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileSource(filename, code, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	if err := e.prepareArtifactModules(compiled); err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	return compiled, nil
}

func (e *MiniExecutor) NewRuntimeByGoCode(code string) (*MiniProgram, error) {
	prog, _, err := e.newMiniProgramByGoCode("snippet", code, false)
	return prog, err
}

func (e *MiniExecutor) NewRuntimeByGoFile(filename, code string) (*MiniProgram, error) {
	prog, _, err := e.newMiniProgramByGoCode(filename, code, false)
	return prog, err
}

func (e *MiniExecutor) NewMiniProgramByGoCodeTolerant(code string) (*MiniProgram, []error) {
	prog, errs, _ := e.newMiniProgramByGoCode("snippet", code, true)
	return prog, errs
}

func (e *MiniExecutor) NewMiniProgramByGoFileTolerant(filename, code string) (*MiniProgram, []error) {
	prog, errs, _ := e.newMiniProgramByGoCode(filename, code, true)
	return prog, errs
}

// AnalyzeProgramTolerant compiles an AST in analysis mode and returns collected
// diagnostics without treating the result as a runtime loading artifact.
//
// The sources map is optional. When provided, it enables source-based artifacts
// such as call template hover previews.
func (e *MiniExecutor) AnalyzeProgramTolerant(program *ast.ProgramStmt, sources map[string]string) (*MiniProgram, []error) {
	var errs []error
	compiled, _, err := e.newCompiler().CompileProgramWithSources("ast", "", program, true, sources)
	if err != nil {
		errs = append(errs, err)
	}
	res := &MiniProgram{
		Program:  program,
		Compiled: compiled,
		executor: &runtime.Executor{},
	}
	if compiled != nil {
		res.TemplatePreviews = compiled.TemplatePreviews
	}
	return res, errs
}

func (e *MiniExecutor) newMiniProgramByGoCode(filename, code string, tolerant bool) (*MiniProgram, []error, error) {
	compiled, errs, semanticCtx, err := e.newCompiler().CompileSource(filename, code, tolerant)
	if err != nil {
		if !tolerant {
			return nil, nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
		}
		errs = append(errs, err)
	}

	var res *MiniProgram
	if compiled == nil {
		return nil, errs, errors.New("failed to compile source")
	}

	if tolerant {
		res = &MiniProgram{
			Program:          compiled.Program,
			Compiled:         compiled,
			TemplatePreviews: compiled.TemplatePreviews,
			executor:         &runtime.Executor{},
		}
		res.Source = code
		return res, errs, nil
	}
	if err := e.prepareArtifactModules(compiled); err != nil {
		return nil, nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}

	executor, err := e.NewRuntimeByCompiled(compiled)
	if err != nil {
		return nil, nil, err
	}
	res = executor
	if res != nil {
		res.Source = code
	}
	return res, errs, nil
}
