package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// Eval 执行单个 Go 表达式字符串
func (e *MiniExecutor) Eval(ctx context.Context, exprStr string, env map[string]interface{}) ([]*runtime.Var, error) {
	compiler := e.newCompiler()
	expr, err := compiler.CompileExprSource(exprStr)
	if err != nil {
		return nil, fmt.Errorf("表达式解析失败: %w", err)
	}
	program := buildEvalProgram(expr, env, nil, nil)
	compiled, semanticCtx, err := compiler.CompileProgram("eval", "", program, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, program)
	}
	if err := e.prepareCompiledArtifact(compiled, semanticCtx); err != nil {
		return nil, err
	}
	if compiled == nil || compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
		return nil, errors.New("eval did not produce executable bytecode")
	}
	artifact, err := executableArtifactFromCompiled(compiled)
	if err != nil {
		return nil, err
	}
	prepared, err := e.preparedProgramForArtifact(artifact)
	if err != nil {
		return nil, err
	}
	executor, err := runtime.NewExecutorFromPrepared(prepared)
	if err != nil {
		return nil, err
	}
	if err := e.applyExecutorConfig(executor); err != nil {
		return nil, err
	}
	if err := executor.ValidateModuleRequirements(); err != nil {
		return nil, err
	}
	fn := compiled.Bytecode.Executable.Functions["__eval__"]
	if fn == nil {
		return nil, errors.New("eval function __eval__ was not prepared")
	}
	res, err := executor.EvalPreparedFunction(ctx, fn, env)
	if err != nil {
		return nil, err
	}
	return unpackEvalResult(expr, res), nil
}

func buildEvalProgram(expr ast.Expr, env map[string]interface{}, importAliases map[string]string, prepared *runtime.PreparedProgram) *ast.ProgramStmt {
	variables := make(map[ast.Ident]ast.Expr, len(importAliases))
	imports := make([]ast.ImportSpec, 0, len(importAliases))
	params := make([]ast.FunctionParam, 0, len(env))
	constants := make(map[string]string)
	constantTypes := make(map[string]ast.GoMiniType)

	envNames := make([]string, 0, len(env))
	for name := range env {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)
	for _, envName := range envNames {
		params = append(params, ast.FunctionParam{Name: ast.Ident(envName), Type: evalEnvType(env[envName])})
	}

	aliases := make([]string, 0, len(importAliases))
	for alias := range importAliases {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		path := importAliases[alias]
		if alias == "" || path == "" {
			continue
		}
		imports = append(imports, ast.ImportSpec{Alias: alias, Path: path})
		variables[ast.Ident(alias)] = &ast.ImportExpr{
			BaseNode: ast.BaseNode{ID: "eval_import_" + alias, Meta: "import", Type: ast.TypeModule},
			Path:     path,
		}
	}
	if prepared != nil {
		names := make([]string, 0, len(prepared.Constants))
		for name := range prepared.Constants {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			value := prepared.Constants[name]
			constants[name] = value.DisplayString()
			typ := ast.GoMiniType(value.Type)
			if declared, ok := prepared.ConstantTypes[name]; ok && !declared.IsEmpty() {
				typ = ast.GoMiniType(declared.Raw)
			}
			if typ != "" {
				constantTypes[name] = typ
			}
		}
	}

	program := &ast.ProgramStmt{
		BaseNode:      ast.BaseNode{ID: "eval", Meta: "boot"},
		Imports:       imports,
		Variables:     variables,
		Constants:     constants,
		ConstantTypes: constantTypes,
		Types:         map[ast.Ident]ast.GoMiniType{},
		Structs:       map[ast.Ident]*ast.StructStmt{},
		Interfaces:    map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"__eval__": {
				BaseNode:     ast.BaseNode{ID: "eval_fn", Meta: "function"},
				Name:         "__eval__",
				FunctionType: ast.FunctionType{Params: params, Return: ast.TypeAny},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{ID: "eval_body", Meta: "block"},
					Children: []ast.Stmt{
						&ast.ReturnStmt{Results: []ast.Expr{expr}},
					},
				},
			},
		},
	}
	return program
}

func evalEnvType(value interface{}) ast.GoMiniType {
	switch typed := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return ast.TypeInt64
	case float32, float64:
		return ast.TypeFloat64
	case bool:
		return ast.TypeBool
	case string:
		return ast.TypeString
	case []byte:
		return ast.CreateArrayType(ast.TypeByte)
	case *runtime.Var:
		if typed != nil && !typed.RawType().IsEmpty() {
			return ast.GoMiniType(typed.RawType())
		}
		return ast.TypeAny
	default:
		return ast.TypeAny
	}
}

// MustEval 类似于 Eval，但在出错时会触发 panic
func (e *MiniExecutor) MustEval(ctx context.Context, exprStr string, env map[string]interface{}) []*runtime.Var {
	res, err := e.Eval(ctx, exprStr, env)
	if err != nil {
		panic(err)
	}
	return res
}

func (e *MiniExecutor) buildSnippetRuntime(code string) (*ExecutableProgram, error) {
	stmts, err := e.newCompiler().CompileStatementsSource(code)
	if err != nil {
		return nil, err
	}

	program := &ast.ProgramStmt{
		BaseNode:      ast.BaseNode{ID: "snippet", Meta: "boot"},
		Main:          stmts,
		Structs:       make(map[ast.Ident]*ast.StructStmt),
		Interfaces:    make(map[ast.Ident]*ast.InterfaceStmt),
		Constants:     make(map[string]string),
		ConstantTypes: make(map[string]ast.GoMiniType),
	}

	compiled, semanticCtx, err := e.newCompiler().CompileProgram("snippet", code, program, false)
	if err != nil {
		return nil, newMiniAstError(err, semanticCtx, program)
	}
	if err := e.prepareCompiledArtifact(compiled, semanticCtx); err != nil {
		return nil, err
	}

	artifact, err := executableArtifactFromCompiled(compiled)
	if err != nil {
		return nil, err
	}
	return e.NewRuntimeByArtifact(artifact)
}

// Execute 执行脚本代码片段（无需 package 声明），支持注入环境变量。
// 注意：本方法使用“单次快照模式”，每次调用均创建全新的执行器上下文。
// 若需持久化的全局变量或复杂的跨模块交互，建议使用 NewRuntimeByGoCode。
func (e *MiniExecutor) Execute(ctx context.Context, code string, env map[string]interface{}) error {
	executor, err := e.buildSnippetRuntime(code)
	if err != nil {
		return err
	}
	runtimeEnv := make(map[string]*runtime.Var, len(env))
	for k, v := range env {
		converted, err := executor.executor.ToVar(nil, v, nil)
		if err != nil {
			return err
		}
		runtimeEnv[k] = converted
	}
	return executor.executor.ExecuteWithEnv(ctx, runtimeEnv)
}

func (e *MiniExecutor) StartExecute(ctx context.Context, code string, env map[string]interface{}) (*runtime.RunHandle, error) {
	executor, err := e.buildSnippetRuntime(code)
	if err != nil {
		return nil, err
	}
	runtimeEnv := make(map[string]*runtime.Var, len(env))
	for k, v := range env {
		converted, err := executor.executor.ToVar(nil, v, nil)
		if err != nil {
			return nil, err
		}
		runtimeEnv[k] = converted
	}
	return executor.executor.StartWithEnv(ctx, runtimeEnv)
}

// MustExecute 类似于 Execute，但在出错时会触发 panic
func (e *MiniExecutor) MustExecute(ctx context.Context, code string, env map[string]interface{}) {
	err := e.Execute(ctx, code, env)
	if err != nil {
		panic(err)
	}
}
