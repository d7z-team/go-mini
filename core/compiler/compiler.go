package compiler

import (
	"encoding/json"
	"errors"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type Config struct {
	ModuleLoader     func(path string) (*ast.ProgramStmt, error)
	FuncSchemas      map[ast.Ident]*runtime.RuntimeFuncSig
	StructSchemas    map[ast.Ident]*runtime.RuntimeStructSpec
	InterfaceSchemas map[ast.Ident]*runtime.RuntimeInterfaceSpec
	Constants        map[string]string
	MaxTypeDepth     int
}

type Compiler struct {
	cfg Config
}

type Artifact struct {
	Filename        string
	Source          string
	Program         *ast.ProgramStmt
	GlobalInitOrder []ast.Ident
	Bytecode        *bytecode.Program
}

func ArtifactFromBytecode(program *bytecode.Program) (*Artifact, error) {
	if program == nil {
		return nil, errors.New("invalid bytecode program")
	}
	if err := program.Validate(); err != nil {
		return nil, err
	}
	rebuilt, err := program.RebuildProgram()
	if err != nil {
		return nil, err
	}
	artifact := &Artifact{
		Filename: "bytecode",
		Program:  rebuilt,
		Bytecode: program,
	}
	if program.Executable != nil {
		artifact.GlobalInitOrder = append([]ast.Ident(nil), program.Executable.GlobalInitOrder...)
	}
	return artifact, nil
}

func ArtifactFromBytecodeJSON(data []byte) (*Artifact, error) {
	program, err := bytecode.UnmarshalJSON(data)
	if err != nil {
		return nil, err
	}
	return ArtifactFromBytecode(program)
}

func New(cfg Config) *Compiler {
	return &Compiler{cfg: cfg}
}

func (a *Artifact) MarshalJSON() ([]byte, error) {
	return marshalArtifactBytecode(a, "", "")
}

func (a *Artifact) MarshalIndentJSON(prefix, indent string) ([]byte, error) {
	return marshalArtifactBytecode(a, prefix, indent)
}

func (a *Artifact) MarshalBytecodeJSON() ([]byte, error) {
	return marshalArtifactBytecode(a, "", "")
}

func (a *Artifact) MarshalIndentBytecodeJSON(prefix, indent string) ([]byte, error) {
	return marshalArtifactBytecode(a, prefix, indent)
}

func (c *Compiler) CompileSource(filename, code string, tolerant bool) (*Artifact, []error, *ast.SemanticContext, error) {
	return c.compileSources([]SourceFile{{Filename: filename, Code: code}}, code, tolerant)
}

func (c *Compiler) CompileFiles(files []SourceFile, tolerant bool) (*Artifact, []error, *ast.SemanticContext, error) {
	return c.compileSources(files, "", tolerant)
}

func (c *Compiler) CompileDir(dir string, tolerant bool) (*Artifact, []error, *ast.SemanticContext, error) {
	files, err := CompileDirInputs(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	return c.compileSources(files, "", tolerant)
}

func (c *Compiler) compileSources(files []SourceFile, source string, tolerant bool) (*Artifact, []error, *ast.SemanticContext, error) {
	programs, errs, err := ParseSourceFiles(files, tolerant)
	if err != nil {
		return nil, errs, nil, err
	}
	program, err := MergePrograms(programs)
	if err != nil {
		return nil, errs, nil, err
	}
	artifact, sem, compileErr := c.CompileProgram(files[0].Filename, "", program, tolerant)
	if source != "" {
		artifact.Source = source
	}
	return artifact, errs, sem, compileErr
}

func (c *Compiler) CompileProgram(filename, source string, program *ast.ProgramStmt, tolerant bool) (*Artifact, *ast.SemanticContext, error) {
	if program == nil {
		return nil, nil, errors.New("invalid program")
	}

	artifact := &Artifact{
		Filename: filename,
		Source:   source,
		Program:  program,
	}

	validator, err := ast.NewValidator(program, c.resolvedSpecs(), c.cfg.Constants, tolerant)
	if err != nil {
		return artifact, nil, err
	}
	if c.cfg.MaxTypeDepth > 0 {
		validator.Root().MaxTypeDepth = c.cfg.MaxTypeDepth
		ast.DefaultMaxTypeDepth = c.cfg.MaxTypeDepth
	}
	validator.SetModuleLoader(c.cfg.ModuleLoader)

	semanticCtx := ast.NewSemanticContext(validator)
	if err := program.Check(semanticCtx); err != nil {
		_ = fillArtifactGlobalInitOrder(artifact, program, false)
		return artifact, semanticCtx, err
	}

	optimizeCtx := ast.NewOptimizeContext(validator)
	if prog, ok := program.Optimize(optimizeCtx).(*ast.ProgramStmt); ok {
		artifact.Program = prog
	}

	if err := fillArtifactGlobalInitOrder(artifact, artifact.Program, true); err != nil {
		return artifact, semanticCtx, err
	}
	artifact.Bytecode = buildBytecode(artifact.Program, artifact.GlobalInitOrder)
	return artifact, semanticCtx, nil
}

func (c *Compiler) resolvedSpecs() map[ast.Ident]ast.GoMiniType {
	size := len(c.cfg.FuncSchemas) + len(c.cfg.StructSchemas) + len(c.cfg.InterfaceSchemas)
	if size == 0 {
		return nil
	}

	res := make(map[ast.Ident]ast.GoMiniType, size)
	for k, v := range c.cfg.FuncSchemas {
		if v == nil {
			continue
		}
		res[k] = v.Spec.Ast()
	}
	for k, v := range c.cfg.StructSchemas {
		if v == nil {
			continue
		}
		res[k] = v.Spec.Ast()
	}
	for k, v := range c.cfg.InterfaceSchemas {
		if v == nil {
			continue
		}
		res[k] = v.Spec.Ast()
	}
	return res
}

func (c *Compiler) CompileExprSource(code string) (ast.Expr, error) {
	return newConverter().ConvertExprSource(code)
}

func (c *Compiler) CompileStatementsSource(code string) ([]ast.Stmt, error) {
	return newConverter().ConvertStmtsSource(code)
}

func marshalArtifactBytecode(a *Artifact, prefix, indent string) ([]byte, error) {
	if a == nil || a.Bytecode == nil {
		return nil, errors.New("cannot marshal empty bytecode artifact")
	}
	if indent == "" && prefix == "" {
		return json.Marshal(a.Bytecode)
	}
	return json.MarshalIndent(a.Bytecode, prefix, indent)
}

func fillArtifactGlobalInitOrder(artifact *Artifact, program *ast.ProgramStmt, allowFallback bool) error {
	if artifact == nil || program == nil {
		return errors.New("invalid program")
	}
	order, err := program.GlobalInitOrder()
	if err != nil {
		if allowFallback {
			artifact.GlobalInitOrder = program.DeclaredGlobalOrder()
		}
		return err
	}
	artifact.GlobalInitOrder = order
	return nil
}

func newConverter() *ffigo.GoToASTConverter {
	return ffigo.NewGoToASTConverter()
}
