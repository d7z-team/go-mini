package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/ffilib/errorslib"
	"gopkg.d7z.net/go-mini/core/ffilib/fmtlib"
	"gopkg.d7z.net/go-mini/core/ffilib/iolib"
	"gopkg.d7z.net/go-mini/core/ffilib/jsonlib"
	"gopkg.d7z.net/go-mini/core/ffilib/oslib"
	"gopkg.d7z.net/go-mini/core/ffilib/timelib"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type MiniExecutor struct {
	Loader  func(path string) (*ast.ProgramStmt, error)
	bridges map[uint32]ffigo.FFIBridge
	routes  map[string]runtime.FFIRoute
	specs   map[ast.Ident]ast.GoMiniType // 用于验证的函数签名

	registry *ffigo.HandleRegistry
}

type MiniProgram struct {
	Source   string
	executor *runtime.Executor
}

func (p *MiniProgram) SetStepLimit(limit int64) {
	p.executor.StepLimit = limit
}

func (p *MiniProgram) Execute(ctx context.Context) error {
	return p.executor.Execute(ctx)
}

func (p *MiniProgram) GetAst() *ast.ProgramStmt {
	return p.executor.GetProgram()
}

func NewMiniExecutor() *MiniExecutor {
	res := &MiniExecutor{
		bridges:  make(map[uint32]ffigo.FFIBridge),
		routes:   make(map[string]runtime.FFIRoute),
		specs:    make(map[ast.Ident]ast.GoMiniType),
		registry: ffigo.NewHandleRegistry(),
	}
	// 默认注册 panic 签名以便通过验证
	res.specs["panic"] = "function(String) Void"
	res.specs["string"] = "function(Any) String"
	res.specs["[]byte"] = "function(Any) TypeBytes"
	res.specs["len"] = "function(Any) Int64"
	return res
}

func (o *MiniExecutor) SetLoader(loader func(path string) (*ast.ProgramStmt, error)) {
	o.Loader = loader
}

// RegisterFFI 注册一个外部函数到特定的 Bridge 和 ID
func (o *MiniExecutor) RegisterFFI(name string, bridge ffigo.FFIBridge, methodID uint32, spec ast.GoMiniType) {
	returns := "Void"
	if callFunc, ok := spec.ReadCallFunc(); ok {
		retType := callFunc.Returns
		if retType.IsTuple() {
			if types, ok := retType.ReadTuple(); ok && len(types) > 0 {
				retType = types[0]
			}
		}

		// 映射 ast.GoMiniType 到 evalFFI 识别的字符串标签
		if retType.IsResult() {
			returns = string(retType)
		} else {
			returns = string(retType)
			switch {
			case retType == "String":
				returns = "String"
			case retType == "Int64":
				returns = "Int64"
			case retType == "Bool":
				returns = "Bool"
			case retType == "TypeBytes" || strings.Contains(string(retType), "Array<Uint8>"):
				returns = "TypeBytes"
			case strings.HasPrefix(string(retType), "Ptr<") || retType == "TypeHandle":
				returns = "TypeHandle"
			case retType.IsMap():
				returns = string(retType) // evaluation uses full type for Map
			}
		}
	}

	o.routes[name] = runtime.FFIRoute{Bridge: bridge, MethodID: methodID, Returns: returns, Spec: string(spec)}
	if spec != "" {
		o.specs[ast.Ident(name)] = spec
	}
}

func (o *MiniExecutor) RegisterBridge(methodID uint32, bridge ffigo.FFIBridge, spec ast.GoMiniType) {
	o.bridges[methodID] = bridge
}

type BridgeWrapper struct {
	Router func(ctx context.Context, methodID uint32, args []byte) ([]byte, error)
}

func (b *BridgeWrapper) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return b.Router(ctx, methodID, args)
}

func (b *BridgeWrapper) DestroyHandle(handle uint32) error {
	return nil // Base wrapper doesn't manage registry
}

type HandleBridgeWrapper struct {
	Registry *ffigo.HandleRegistry
	Router   func(ctx context.Context, reg *ffigo.HandleRegistry, methodID uint32, args []byte) ([]byte, error)
}

func (b *HandleBridgeWrapper) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return b.Router(ctx, b.Registry, methodID, args)
}

func (b *HandleBridgeWrapper) DestroyHandle(handle uint32) error {
	b.Registry.Remove(handle)
	return nil
}

func (o *MiniExecutor) InjectStandardLibraries() {
	// 1. Inject fmt
	fmtlib.RegisterFMTLIBFmtLibrary(o, "fmt", &fmtlib.FmtHost{}, o.registry)

	// 2. Inject os
	oslib.RegisterOSLIBOSLibrary(o, "os", &oslib.OSHost{}, o.registry)

	// 3. Inject errors
	errorslib.RegisterERRORSLIBErrorsLibrary(o, "errors", &errorslib.ErrorsHost{}, o.registry)

	// 4. Inject io
	iolib.RegisterIOLIBIOLibrary(o, "io", &iolib.IOHost{}, o.registry)

	// 5. Inject json
	jsonlib.RegisterJSONLIBJSONLibrary(o, "json", &jsonlib.JSONHost{}, o.registry)

	// 6. Inject time
	timelib.RegisterTIMELIBTimeLibrary(o, "time", &timelib.TimeHost{}, o.registry)
}

// AddFuncSpec 仅用于在验证阶段声明一个合法的外部函数
func (o *MiniExecutor) AddFuncSpec(name string, spec ast.GoMiniType) {
	o.specs[ast.Ident(name)] = spec
}

func (o *MiniExecutor) NewRuntimeByAst(program *ast.ProgramStmt) (*MiniProgram, error) {
	executor, err := runtime.NewExecutor(program)
	if err != nil {
		return nil, err
	}

	// Pass routes to executor
	for name, route := range o.routes {
		executor.RegisterRoute(name, route)
	}

	return &MiniProgram{
		Source:   "",
		executor: executor,
	}, nil
}

func (o *MiniExecutor) NewRuntimeByGoCode(code string) (*MiniProgram, error) {
	converter := ffigo.NewGoToASTConverter()
	node, err := converter.ConvertSource(code)
	if err != nil {
		return nil, err
	}

	program := node.(*ast.ProgramStmt)

	// Validate and Optimize
	validator, _ := ast.NewValidator(program)
	for name, spec := range o.specs {
		validator.AddVariable(name, spec)
	}

	semanticCtx := ast.NewSemanticContext(validator)
	err = program.Check(semanticCtx)
	if err != nil {
		// Serialize program for debug
		data, _ := json.MarshalIndent(program, "", "  ")
		return nil, fmt.Errorf("验证失败:\n\n%s\n\n%w", string(data), err)
	}

	optimizeCtx := ast.NewOptimizeContext(validator)
	program.Optimize(optimizeCtx)

	return o.NewRuntimeByAst(program)
}
