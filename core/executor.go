package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type MiniExecutor struct {
	Loader  func(path string) (*ast.ProgramStmt, error)
	bridges map[uint32]ffigo.FFIBridge
	routes  map[string]runtime.FFIRoute
	specs   map[ast.Ident]ast.GoMiniType // 用于验证的函数签名
}

type MiniProgram struct {
	Source   string
	executor *runtime.Executor
}

func (p *MiniProgram) Execute(ctx context.Context) error {
	return p.executor.Execute(ctx)
}

func (p *MiniProgram) GetProgram() *ast.ProgramStmt {
	return p.executor.GetProgram()
}

func NewMiniExecutor() *MiniExecutor {
	res := &MiniExecutor{
		bridges: make(map[uint32]ffigo.FFIBridge),
		routes:  make(map[string]runtime.FFIRoute),
		specs:   make(map[ast.Ident]ast.GoMiniType),
	}
	// 默认注册 panic 签名以便通过验证
	res.specs["panic"] = "function(String) Void"
	return res
}

func (o *MiniExecutor) SetLoader(loader func(path string) (*ast.ProgramStmt, error)) {
	o.Loader = loader
}

// RegisterFFI 注册一个外部函数到特定的 Bridge 和 ID
func (o *MiniExecutor) RegisterFFI(name string, bridge ffigo.FFIBridge, methodID uint32, spec ast.GoMiniType) {
	o.routes[name] = runtime.FFIRoute{Bridge: bridge, MethodID: methodID}
	if spec != "" {
		o.specs[ast.Ident(name)] = spec
	}
}

func (o *MiniExecutor) RegisterBridge(methodID uint32, bridge ffigo.FFIBridge, spec ast.GoMiniType) {
	o.bridges[methodID] = bridge
}

// AddFuncSpec 仅用于在验证阶段声明一个合法的外部函数
func (o *MiniExecutor) AddFuncSpec(name string, spec ast.GoMiniType) {
	o.specs[ast.Ident(name)] = spec
}

func (o *MiniExecutor) NewRuntimeByGoCode(code string) (*MiniProgram, error) {
	converter := ffigo.NewGoToASTConverter()
	if !strings.HasPrefix(strings.TrimSpace(code), "package ") {
		code = "package main\n" + code
	}
	astTree, err := converter.ConvertSource(code)
	if err != nil {
		return nil, err
	}
	return o.NewRuntimeByAst(astTree)
}

func (o *MiniExecutor) NewRuntimeByGoExpr(code string) (*MiniProgram, error) {
	return o.NewRuntimeByGoCode(`func main(){
` + code + `
}`)
}

func (o *MiniExecutor) NewRuntimeByJSON(data []byte) (*MiniProgram, error) {
	node, err := Unmarshal(data)
	if err != nil {
		return nil, err
	}
	return o.NewRuntimeByAst(node)
}

func (o *MiniExecutor) NewRuntimeByAst(tree ast.Node) (*MiniProgram, error) {
	var src bytes.Buffer
	encoder := json.NewEncoder(&src)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(tree)

	optimize, logs, err := ValidateAndOptimizeWithLoader(tree, o.Loader, func(v *ast.ValidContext) error {
		for name, spec := range o.specs {
			if err := v.AddFuncSpec(name, spec); err != nil {
				return err
			}
		}
		return nil
	})
	var astError *ast.MiniAstError
	if err != nil {
		if !errors.As(err, &astError) {
			return nil, &ast.MiniAstError{Err: err, Logs: logs, Node: tree}
		}
		return nil, err
	}

	executor, err := runtime.NewExecutor(optimize)
	if err != nil {
		if !errors.As(err, &astError) {
			return nil, &ast.MiniAstError{Err: err, Logs: logs, Node: tree}
		}
		return nil, err
	}
	
	// Pass routes to executor
	for name, route := range o.routes {
		executor.RegisterRoute(name, route.Bridge, route.MethodID)
	}

	return &MiniProgram{
		Source:   src.String(),
		executor: executor,
	}, nil
}
