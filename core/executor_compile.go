package engine

import (
	"context"
	"errors"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/frontend"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func (e *MiniExecutor) CompileAST(program *ast.ProgramStmt) (*compiler.Artifact, error) {
	compiled, semanticCtx, err := e.newCompiler().CompileProgram("ast", "", program, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, program)
	}
	if err := e.prepareCompiledArtifact(compiled, semanticCtx); err != nil {
		return nil, err
	}
	return compiled, nil
}

func (e *MiniExecutor) CompileWithFrontend(ctx context.Context, fe frontend.Frontend, files []SourceFile) (*compiler.Artifact, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileWithFrontend(ctx, fe, files, "", false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	if err := e.prepareCompiledArtifact(compiled, semanticCtx); err != nil {
		return nil, err
	}
	return compiled, nil
}

func (e *MiniExecutor) CompileFiles(files []SourceFile) (*compiler.Artifact, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileFiles(files, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	if err := e.prepareCompiledArtifact(compiled, semanticCtx); err != nil {
		return nil, err
	}
	return compiled, nil
}

func (e *MiniExecutor) CompileDir(dir string) (*compiler.Artifact, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileDir(dir, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	if err := e.prepareCompiledArtifact(compiled, semanticCtx); err != nil {
		return nil, err
	}
	return compiled, nil
}

func (e *MiniExecutor) NewRuntimeByCompiled(compiled *compiler.Artifact) (*ExecutableProgram, error) {
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
	if err := executor.ValidateExternalRequirements(); err != nil {
		return nil, err
	}

	executableArtifact, err := compiler.ArtifactFromBytecode(compiled.Bytecode)
	if err != nil {
		return nil, err
	}
	executableArtifact.Filename = compiled.Filename
	executableArtifact.Source = compiled.Source
	return &ExecutableProgram{
		Source:   compiled.Source,
		compiled: executableArtifact,
		executor: executor,
	}, nil
}

func (e *MiniExecutor) NewRuntimeByFiles(files []SourceFile) (*ExecutableProgram, error) {
	compiled, err := e.CompileFiles(files)
	if err != nil {
		return nil, err
	}
	return e.NewRuntimeByCompiled(compiled)
}

func (e *MiniExecutor) NewRuntimeByDir(dir string) (*ExecutableProgram, error) {
	compiled, err := e.CompileDir(dir)
	if err != nil {
		return nil, err
	}
	return e.NewRuntimeByCompiled(compiled)
}

func (e *MiniExecutor) NewRuntimeByBytecode(program *bytecode.Program) (*ExecutableProgram, error) {
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

func (e *MiniExecutor) NewRuntimeByBytecodeJSON(payload []byte) (*ExecutableProgram, error) {
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
	if err := e.prepareCompiledArtifact(compiled, semanticCtx); err != nil {
		return nil, err
	}
	return compiled, nil
}

func (e *MiniExecutor) CompileGoFile(filename, code string) (*compiler.Artifact, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileSource(filename, code, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	if err := e.prepareCompiledArtifact(compiled, semanticCtx); err != nil {
		return nil, err
	}
	return compiled, nil
}

func (e *MiniExecutor) NewRuntimeByGoCode(code string) (*ExecutableProgram, error) {
	return e.newRuntimeByGoCode("snippet", code)
}

func (e *MiniExecutor) NewRuntimeByGoFile(filename, code string) (*ExecutableProgram, error) {
	return e.newRuntimeByGoCode(filename, code)
}

func (e *MiniExecutor) AnalyzeGoCodeTolerant(code string) (*AnalysisProgram, []error) {
	return e.AnalyzeGoFileTolerant("snippet", code)
}

func (e *MiniExecutor) AnalyzeGoFileTolerant(filename, code string) (*AnalysisProgram, []error) {
	compiled, errs, _, err := e.newCompiler().CompileSource(filename, code, true)
	if err != nil {
		errs = append(errs, err)
	}
	if compiled == nil {
		return nil, errs
	}
	return newAnalysisProgram(code, compiled, compiled.Program), errs
}

// AnalyzeProgramTolerant compiles an AST in analysis mode and returns collected
// diagnostics without treating the result as a runtime loading artifact.
//
// The sources map is optional. When provided, it enables source-based artifacts
// such as call template hover previews.
func (e *MiniExecutor) AnalyzeProgramTolerant(program *ast.ProgramStmt, sources map[string]string) (*AnalysisProgram, []error) {
	var errs []error
	compiled, _, err := e.newCompiler().CompileProgramWithSources("ast", "", program, true, sources)
	if err != nil {
		errs = append(errs, err)
	}
	return newAnalysisProgram("", compiled, program), errs
}

func (e *MiniExecutor) newRuntimeByGoCode(filename, code string) (*ExecutableProgram, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileSource(filename, code, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, compiledProgramNode(compiled))
	}
	if compiled == nil {
		return nil, errors.New("failed to compile source")
	}
	if err := e.prepareCompiledArtifact(compiled, semanticCtx); err != nil {
		return nil, err
	}

	res, err := e.NewRuntimeByCompiled(compiled)
	if err != nil {
		return nil, err
	}
	res.Source = code
	return res, nil
}
