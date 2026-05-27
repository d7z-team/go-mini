package engine

import (
	"context"
	"errors"
	"fmt"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type ExecutableProgram struct {
	Source   string
	compiled *compiler.Artifact
	executor *runtime.Executor
}

type StackContext = runtime.StackContext

// ToVar 将 Go 侧数据转换为脚本侧 Var。主要用于宿主注入。
func (p *ExecutableProgram) ToVar(ctx *runtime.StackContext, val interface{}, bridge ffigo.FFIBridge) (*runtime.Var, error) {
	return p.executor.ToVar(ctx, val, bridge)
}

func (p *ExecutableProgram) Executor() *runtime.Executor {
	return p.executor
}

func (p *ExecutableProgram) Compilation() *compiler.Artifact {
	if p == nil {
		return nil
	}
	return p.compiled
}

func (p *ExecutableProgram) CheckSatisfaction(val *runtime.Var, interfaceType string) (*runtime.Var, error) {
	return p.executor.CheckSatisfaction(val, interfaceType)
}

func (p *ExecutableProgram) SetStepLimit(limit int64) {
	p.executor.StepLimit = limit
}

func (p *ExecutableProgram) Start(ctx context.Context) (*runtime.RunHandle, error) {
	return p.StartWithEnv(ctx, nil)
}

func (p *ExecutableProgram) StartWithEnv(ctx context.Context, env map[string]*runtime.Var) (*runtime.RunHandle, error) {
	return p.executor.StartWithEnv(ctx, env)
}

func (p *ExecutableProgram) Execute(ctx context.Context) error {
	return p.executor.Execute(ctx)
}

// ExecuteWithEnv 执行脚本，并允许注入初始环境变量
func (p *ExecutableProgram) ExecuteWithEnv(ctx context.Context, env map[string]*runtime.Var) error {
	return p.executor.ExecuteWithEnv(ctx, env)
}

func (p *ExecutableProgram) SharedState() *runtime.SharedStateSnapshot {
	if p == nil || p.executor == nil {
		return nil
	}
	return p.executor.SharedStateSnapshot()
}

func unpackEvalResult(expr ast.Expr, res *runtime.Var) []*runtime.Var {
	typ := runtime.TypeSpec("")
	if expr != nil {
		typ = runtime.TypeSpec(expr.GetBase().Type)
	}
	if typ.IsEmpty() && res != nil {
		typ = res.RawType()
	}
	if typ.IsVoid() {
		return []*runtime.Var{}
	}
	if !typ.IsTuple() {
		if res == nil {
			return []*runtime.Var{}
		}
		if res.VType == runtime.TypeAny {
			if inner, ok := res.Ref.(*runtime.Var); ok && inner != nil {
				return []*runtime.Var{inner}
			}
		}
		return []*runtime.Var{res}
	}
	if res == nil || res.VType != runtime.TypeArray {
		if res == nil {
			return []*runtime.Var{}
		}
		return []*runtime.Var{res}
	}
	if arr, ok := res.Ref.(*runtime.VMArray); ok {
		return arr.Data
	}
	return []*runtime.Var{res}
}

// Eval 在当前程序的语境下执行单个 Go 表达式
// 这允许你调用程序中定义的函数或访问全局变量
func (p *ExecutableProgram) Eval(ctx context.Context, exprStr string, env map[string]interface{}) ([]*runtime.Var, error) {
	expr, err := compiler.New(compiler.Config{}).CompileExprSource(exprStr)
	if err != nil {
		return nil, fmt.Errorf("表达式解析失败: %w", err)
	}

	fn, err := compiler.CompileEvalFunction("__eval__", expr)
	if err != nil {
		return nil, err
	}
	res, err := p.executor.EvalPreparedFunction(ctx, fn, env)
	if err != nil {
		return nil, err
	}
	return unpackEvalResult(expr, res), nil
}

// MustEval 类似于 Eval，但在出错时会触发 panic
func (p *ExecutableProgram) MustEval(ctx context.Context, exprStr string, env map[string]interface{}) []*runtime.Var {
	res, err := p.Eval(ctx, exprStr, env)
	if err != nil {
		panic(err)
	}
	return res
}

func (p *ExecutableProgram) Disassemble() string {
	if p == nil {
		return "; Error: invalid or uninitialized program\n"
	}
	if p.compiled != nil && p.compiled.Bytecode != nil {
		return p.compiled.Bytecode.Disassemble()
	}
	if p.executor == nil {
		return "; Error: invalid or uninitialized program\n"
	}
	return p.executor.Disassemble()
}

func (p *ExecutableProgram) MarshalJSON() ([]byte, error) {
	return p.MarshalBytecodeJSON()
}

func (p *ExecutableProgram) MarshalIndentJSON(prefix, indent string) ([]byte, error) {
	return p.MarshalIndentBytecodeJSON(prefix, indent)
}

func (p *ExecutableProgram) MarshalBytecodeJSON() ([]byte, error) {
	if p == nil || p.compiled == nil {
		return nil, errors.New("cannot marshal bytecode from empty program")
	}
	return p.compiled.MarshalBytecodeJSON()
}

func (p *ExecutableProgram) MarshalIndentBytecodeJSON(prefix, indent string) ([]byte, error) {
	if p == nil || p.compiled == nil {
		return nil, errors.New("cannot marshal bytecode from empty program")
	}
	return p.compiled.MarshalIndentBytecodeJSON(prefix, indent)
}

func (p *ExecutableProgram) Bytecode() (*bytecode.Program, error) {
	if p == nil || p.compiled == nil || p.compiled.Bytecode == nil {
		return nil, errors.New("program does not contain bytecode")
	}
	return p.compiled.Bytecode, nil
}
