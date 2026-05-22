package compiler

import (
	"encoding/json"
	"errors"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/gofrontend"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type Config struct {
	ModuleLoader     func(path string) (*ast.ProgramStmt, error)
	FuncSchemas      map[ast.Ident]*runtime.RuntimeFuncSig
	StructSchemas    map[ast.Ident]*runtime.RuntimeStructSpec
	InterfaceSchemas map[ast.Ident]*runtime.RuntimeInterfaceSpec
	Constants        map[string]string
	MaxTypeDepth     int
	// Templates contains compiler-only call templates. The compiler exposes
	// their signatures during the first semantic check, expands matching calls,
	// and rejects any residual template artifacts before bytecode generation.
	Templates *calltemplate.Registry
}

type Compiler struct {
	cfg Config
}

type Artifact struct {
	Filename        string
	Source          string
	Program         *ast.ProgramStmt
	GlobalInitOrder []string
	Bytecode        *bytecode.Program
	// ImportedPrograms contains AST modules resolved during compilation; callers
	// can compile them into prepared modules before runtime execution starts.
	ImportedPrograms map[string]*ast.ProgramStmt
}

func ArtifactFromBytecode(program *bytecode.Program) (*Artifact, error) {
	if program == nil {
		return nil, errors.New("invalid bytecode program")
	}
	if err := program.Validate(); err != nil {
		return nil, err
	}
	artifact := &Artifact{
		Filename: "bytecode",
		Bytecode: program,
	}
	if program.Executable != nil {
		artifact.GlobalInitOrder = append([]string(nil), program.Executable.GlobalInitOrder...)
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
	importedPrograms := map[string]*ast.ProgramStmt{}
	templatePlan, err := c.buildTemplatePlan(importedPrograms)
	if err != nil {
		return artifact, nil, err
	}

	if err := calltemplate.ValidateReservedDeclarations(program, c.cfg.Templates); err != nil {
		return artifact, nil, err
	}

	newValidator := func(target *ast.ProgramStmt, includeTemplates bool) (*ast.ValidContext, error) {
		specs, err := c.resolvedTypeSpecs(includeTemplates, templatePlan)
		if err != nil {
			return nil, err
		}
		validator, err := ast.NewValidatorWithExternalTypes(target, specs, c.cfg.Constants, tolerant)
		if err != nil {
			return nil, err
		}
		if c.cfg.MaxTypeDepth > 0 {
			validator.Root().MaxTypeDepth = c.cfg.MaxTypeDepth
			ast.DefaultMaxTypeDepth = c.cfg.MaxTypeDepth
		}
		if c.cfg.ModuleLoader != nil {
			validator.SetModuleLoader(func(path string) (*ast.ProgramStmt, error) {
				if prog, ok := importedPrograms[path]; ok {
					return prog, nil
				}
				prog, err := c.cfg.ModuleLoader(path)
				if err == nil && prog != nil {
					importedPrograms[path] = prog
				}
				return prog, err
			})
		}
		if includeTemplates && c.cfg.Templates != nil {
			validator.SetTemplateBuiltins(c.cfg.Templates.CompletionSchemas())
		}
		return validator, nil
	}

	validator, err := newValidator(program, true)
	if err != nil {
		return artifact, nil, err
	}
	semanticCtx := ast.NewSemanticContext(validator)
	if err := program.Check(semanticCtx); err != nil {
		_ = fillArtifactGlobalInitOrder(artifact, program, false)
		return artifact, semanticCtx, err
	}

	expanded, err := calltemplate.ExpandProgram(artifact.Program, c.cfg.Templates, templatePlan)
	if err != nil {
		return artifact, semanticCtx, err
	}
	if err := calltemplate.AssertNoResidualTemplateRefs(artifact.Program, c.cfg.Templates); err != nil {
		return artifact, semanticCtx, err
	}

	activeValidator := validator
	if expanded.Changed {
		activeValidator, err = newValidator(artifact.Program, false)
		if err != nil {
			return artifact, semanticCtx, err
		}
		semanticCtx = ast.NewSemanticContext(activeValidator)
		if err := artifact.Program.Check(semanticCtx); err != nil {
			_ = fillArtifactGlobalInitOrder(artifact, artifact.Program, false)
			return artifact, semanticCtx, err
		}
	}
	if err := calltemplate.AssertNoCompileOnlyArtifacts(artifact.Program); err != nil {
		return artifact, semanticCtx, err
	}
	if expanded.Changed {
		if err := expanded.CheckTypes(); err != nil {
			return artifact, semanticCtx, err
		}
	}

	if prog, ok := artifact.Program.Optimize(ast.NewOptimizeContext(activeValidator)).(*ast.ProgramStmt); ok {
		artifact.Program = prog
	}

	if err := fillArtifactGlobalInitOrder(artifact, artifact.Program, true); err != nil {
		return artifact, semanticCtx, err
	}
	bytecodeProgram, err := buildBytecode(artifact.Program, artifact.GlobalInitOrder)
	if err != nil {
		return artifact, semanticCtx, err
	}
	artifact.Bytecode = bytecodeProgram
	if kept := pruneImportedPrograms(importedPrograms, artifact.Program); len(kept) > 0 {
		artifact.ImportedPrograms = kept
	}
	return artifact, semanticCtx, nil
}

func (c *Compiler) resolvedTypeSpecs(includeTemplates bool, plan *calltemplate.Plan) (map[ast.Ident]ast.ExternalTypeSpec, error) {
	templateFuncs := map[ast.Ident]*runtime.RuntimeFuncSig(nil)
	if includeTemplates && plan != nil {
		templateFuncs = plan.FuncSchemas()
	}
	size := len(c.cfg.FuncSchemas) + len(c.cfg.StructSchemas) + len(c.cfg.InterfaceSchemas) + len(templateFuncs)
	if size == 0 {
		return nil, nil
	}

	res := make(map[ast.Ident]ast.ExternalTypeSpec, size)
	for k, v := range c.cfg.FuncSchemas {
		if v == nil {
			continue
		}
		res[k] = ast.ExternalTypeSpec{Type: ast.GoMiniType(v.Spec), Ownership: ast.StructOwnershipVMValue}
	}
	for k, v := range templateFuncs {
		if v == nil {
			continue
		}
		spec := ast.ExternalTypeSpec{Type: ast.GoMiniType(v.Spec), Ownership: ast.StructOwnershipVMValue}
		if _, ok := res[k]; ok {
			continue
		}
		res[k] = spec
	}
	for k, v := range c.cfg.StructSchemas {
		if v == nil {
			continue
		}
		ownership := ast.StructOwnershipVMValue
		if v.Ownership == runtime.StructOwnershipHostOpaque {
			ownership = ast.StructOwnershipHostOpaque
		}
		if _, ok := res[k]; ok {
			return nil, errors.New("external struct schema conflicts with existing symbol " + string(k))
		}
		res[k] = ast.ExternalTypeSpec{Type: ast.GoMiniType(v.Spec), Ownership: ownership}
	}
	for k, v := range c.cfg.InterfaceSchemas {
		if v == nil {
			continue
		}
		if _, ok := res[k]; ok {
			return nil, errors.New("external interface schema conflicts with existing symbol " + string(k))
		}
		res[k] = ast.ExternalTypeSpec{Type: ast.GoMiniType(v.Spec), Ownership: ast.StructOwnershipVMValue}
	}
	return res, nil
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
			artifact.GlobalInitOrder = identSliceToStrings(program.DeclaredGlobalOrder())
		}
		return err
	}
	artifact.GlobalInitOrder = identSliceToStrings(order)
	return nil
}

func identSliceToStrings(items []ast.Ident) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = string(item)
	}
	return out
}

func newConverter() *gofrontend.Converter {
	return gofrontend.NewConverter()
}
