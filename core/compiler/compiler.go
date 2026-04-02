package compiler

import (
	"encoding/json"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type Config struct {
	ModuleLoader  func(path string) (*ast.ProgramStmt, error)
	FuncSchemas   map[ast.Ident]*runtime.RuntimeFuncSig
	StructSchemas map[ast.Ident]*runtime.RuntimeStructSpec
	Constants     map[string]string
	MaxTypeDepth  int
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
		return nil, fmt.Errorf("invalid bytecode program")
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

func New(cfg Config) *Compiler {
	return &Compiler{cfg: cfg}
}

func (a *Artifact) MarshalJSON() ([]byte, error) {
	return a.MarshalBytecodeJSON()
}

func (a *Artifact) MarshalIndentJSON(prefix, indent string) ([]byte, error) {
	return a.MarshalIndentBytecodeJSON(prefix, indent)
}

func (a *Artifact) MarshalBytecodeJSON() ([]byte, error) {
	if a == nil || a.Bytecode == nil {
		return nil, fmt.Errorf("cannot marshal empty bytecode artifact")
	}
	return json.Marshal(a.Bytecode)
}

func (a *Artifact) MarshalIndentBytecodeJSON(prefix, indent string) ([]byte, error) {
	if a == nil || a.Bytecode == nil {
		return nil, fmt.Errorf("cannot marshal empty bytecode artifact")
	}
	return json.MarshalIndent(a.Bytecode, prefix, indent)
}

func (c *Compiler) CompileSource(filename, code string, tolerant bool) (*Artifact, []error, *ast.SemanticContext, error) {
	converter := ffigo.NewGoToASTConverter()

	var (
		node ast.Node
		errs []error
		err  error
	)
	if tolerant {
		node, errs = converter.ConvertSourceTolerant(filename, code)
	} else {
		node, err = converter.ConvertSource(filename, code)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	if node == nil {
		return nil, errs, nil, fmt.Errorf("failed to parse source")
	}

	program, ok := node.(*ast.ProgramStmt)
	if !ok {
		return nil, errs, nil, fmt.Errorf("unexpected root node type: %T", node)
	}

	artifact, sem, compileErr := c.CompileProgram(filename, code, program, tolerant)
	return artifact, errs, sem, compileErr
}

func (c *Compiler) CompileProgram(filename, source string, program *ast.ProgramStmt, tolerant bool) (*Artifact, *ast.SemanticContext, error) {
	if program == nil {
		return nil, nil, fmt.Errorf("invalid program")
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
		if order, orderErr := program.GlobalInitOrder(); orderErr == nil {
			artifact.GlobalInitOrder = order
		}
		return artifact, semanticCtx, err
	}

	optimizeCtx := ast.NewOptimizeContext(validator)
	if optimized := program.Optimize(optimizeCtx); optimized != nil {
		if prog, ok := optimized.(*ast.ProgramStmt); ok {
			artifact.Program = prog
		}
	}

	order, orderErr := artifact.Program.GlobalInitOrder()
	if orderErr != nil {
		artifact.GlobalInitOrder = artifact.Program.DeclaredGlobalOrder()
		return artifact, semanticCtx, orderErr
	}
	artifact.GlobalInitOrder = order
	artifact.Bytecode = buildBytecode(artifact.Program, artifact.GlobalInitOrder)
	return artifact, semanticCtx, nil
}

func (c *Compiler) resolvedSpecs() map[ast.Ident]ast.GoMiniType {
	size := len(c.cfg.FuncSchemas) + len(c.cfg.StructSchemas)
	if size == 0 {
		return nil
	}

	res := make(map[ast.Ident]ast.GoMiniType, size)
	for k, v := range c.cfg.FuncSchemas {
		if v == nil {
			continue
		}
		res[k] = v.Spec
	}
	for k, v := range c.cfg.StructSchemas {
		if v == nil {
			continue
		}
		res[k] = v.Spec
	}
	return res
}

func (c *Compiler) CompileExprSource(code string) (ast.Expr, error) {
	return ffigo.NewGoToASTConverter().ConvertExprSource(code)
}

func (c *Compiler) CompileStatementsSource(code string) ([]ast.Stmt, error) {
	return ffigo.NewGoToASTConverter().ConvertStmtsSource(code)
}
