package engine

import (
	"context"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// Eval 执行单个 Go 表达式字符串
func (e *MiniExecutor) Eval(ctx context.Context, exprStr string, env map[string]interface{}) ([]*runtime.Var, error) {
	expr, err := e.newCompiler().CompileExprSource(exprStr)
	if err != nil {
		return nil, fmt.Errorf("表达式解析失败: %w", err)
	}

	// 创建最小化的无状态执行器
	executor, err := newEmptyRuntimeExecutor()
	if err != nil {
		return nil, err
	}
	if err := e.applyExecutorConfig(executor); err != nil {
		return nil, err
	}

	fn, err := compiler.CompileEvalFunction("__eval__", expr)
	if err != nil {
		return nil, err
	}
	res, err := executor.EvalPreparedFunction(ctx, fn, env)
	if err != nil {
		return nil, err
	}
	return unpackEvalResult(expr, res), nil
}

// MustEval 类似于 Eval，但在出错时会触发 panic
func (e *MiniExecutor) MustEval(ctx context.Context, exprStr string, env map[string]interface{}) []*runtime.Var {
	res, err := e.Eval(ctx, exprStr, env)
	if err != nil {
		panic(err)
	}
	return res
}

// Execute 执行脚本代码片段（无需 package 声明），支持注入环境变量。
// 注意：本方法使用“单次快照模式”，每次调用均创建全新的执行器上下文。
// 若需持久化的全局变量或复杂的跨模块交互，建议使用 NewRuntimeByGoCode。
func (e *MiniExecutor) Execute(ctx context.Context, code string, env map[string]interface{}) error {
	stmts, err := e.newCompiler().CompileStatementsSource(code)
	if err != nil {
		return err
	}

	// 构建临时程序以便验证
	program := &ast.ProgramStmt{
		BaseNode:   ast.BaseNode{ID: "snippet", Meta: "boot"},
		Main:       stmts,
		Structs:    make(map[ast.Ident]*ast.StructStmt),
		Interfaces: make(map[ast.Ident]*ast.InterfaceStmt),
		Constants:  make(map[string]string),
	}
	// 注入所有已注册的模块中的符号，以便在 Snippet 中使用
	e.mu.RLock()
	for _, s := range e.moduleSources {
		for name, sDef := range s.Structs {
			program.Structs[name] = sDef
		}
		for name, iDef := range s.Interfaces {
			program.Interfaces[name] = iDef
		}
		for name, cDef := range s.Constants {
			program.Constants[name] = cDef
		}
	}
	e.mu.RUnlock()

	compiled, semanticCtx, err := e.newCompiler().CompileProgram("snippet", code, program, false)
	if err != nil {
		return newMiniAstError(err, semanticCtx, program)
	}
	if err := e.prepareCompiledArtifact(compiled, semanticCtx); err != nil {
		return err
	}

	executor, err := e.NewRuntimeByCompiled(compiled)
	if err != nil {
		return err
	}

	runtimeEnv := make(map[string]*runtime.Var, len(env))
	for k, v := range env {
		runtimeEnv[k] = executor.executor.ToVar(nil, v, nil)
	}
	return executor.ExecuteWithEnv(ctx, runtimeEnv)
}

// MustExecute 类似于 Execute，但在出错时会触发 panic
func (e *MiniExecutor) MustExecute(ctx context.Context, code string, env map[string]interface{}) {
	err := e.Execute(ctx, code, env)
	if err != nil {
		panic(err)
	}
}

// Import 手动加载一个模块并返回其导出的成员对象
func (e *MiniExecutor) Import(ctx context.Context, path string) (*runtime.Var, error) {
	// 创建一个最简的执行器环境来执行加载
	executor, err := newEmptyRuntimeExecutor()
	if err != nil {
		return nil, err
	}
	if err := e.applyExecutorConfig(executor); err != nil {
		return nil, err
	}

	session := executor.NewSession(ctx, "loader")
	defer executor.CleanupSession(session)

	return executor.ImportModulePath(session, path)
}
