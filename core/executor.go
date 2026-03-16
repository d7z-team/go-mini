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
	fmtHost := &fmtlib.FmtHost{}
	fmtBridge := &BridgeWrapper{
		Router: func(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
			return fmtlib.FmtHostRouter(ctx, fmtHost, o.registry, methodID, args)
		},
	}
	o.RegisterFFI("fmt.Print", fmtBridge, fmtlib.MethodID_Fmt_Print, "function(...Any) Void")
	o.RegisterFFI("fmt.Println", fmtBridge, fmtlib.MethodID_Fmt_Println, "function(...Any) Void")
	o.RegisterFFI("fmt.Printf", fmtBridge, fmtlib.MethodID_Fmt_Printf, "function(String, ...Any) Void")
	o.RegisterFFI("fmt.Sprintf", fmtBridge, fmtlib.MethodID_Fmt_Sprintf, "function(String, ...Any) String")

	// 2. Inject os
	osHost := &oslib.OSHost{}
	osBridge := &HandleBridgeWrapper{
		Registry: o.registry,
		Router: func(ctx context.Context, reg *ffigo.HandleRegistry, methodID uint32, args []byte) ([]byte, error) {
			return oslib.OSHostRouter(ctx, osHost, reg, methodID, args)
		},
	}
	o.RegisterFFI("os.Open", osBridge, oslib.MethodID_OS_Open, "function(String) Result<TypeHandle>")
	o.RegisterFFI("os.Create", osBridge, oslib.MethodID_OS_Create, "function(String) Result<TypeHandle>")
	o.RegisterFFI("os.ReadFile", osBridge, oslib.MethodID_OS_ReadFile, "function(String) Result<TypeBytes>")
	o.RegisterFFI("os.WriteFile", osBridge, oslib.MethodID_OS_WriteFile, "function(String, TypeBytes) Result<Void>")
	o.RegisterFFI("os.Remove", osBridge, oslib.MethodID_OS_Remove, "function(String) Result<Void>")
	o.RegisterFFI("os.Read", osBridge, oslib.MethodID_OS_Read, "function(TypeHandle, TypeBytes) Result<Int64>")
	o.RegisterFFI("os.Write", osBridge, oslib.MethodID_OS_Write, "function(TypeHandle, TypeBytes) Result<Int64>")
	o.RegisterFFI("os.Close", osBridge, oslib.MethodID_OS_Close, "function(TypeHandle) Result<Void>")

	// 3. Inject errors
	errorsHost := &errorslib.ErrorsHost{}
	errorsBridge := &BridgeWrapper{
		Router: func(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
			return errorslib.ErrorsHostRouter(ctx, errorsHost, o.registry, methodID, args)
		},
	}
	o.RegisterFFI("errors.New", errorsBridge, errorslib.MethodID_Errors_New, "function(String) Result<Void>")

	// 4. Inject io
	ioHost := &iolib.IOHost{}
	ioBridge := &BridgeWrapper{
		Router: func(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
			return iolib.IOHostRouter(ctx, ioHost, o.registry, methodID, args)
		},
	}
	o.RegisterFFI("io.ReadAll", ioBridge, iolib.MethodID_IO_ReadAll, "function(Any) Result<TypeBytes>")

	// 5. Inject json
	jsonHost := &jsonlib.JSONHost{}
	jsonBridge := &BridgeWrapper{
		Router: func(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
			return jsonlib.JSONHostRouter(ctx, jsonHost, o.registry, methodID, args)
		},
	}
	o.RegisterFFI("json.Marshal", jsonBridge, jsonlib.MethodID_JSON_Marshal, "function(Any) Result<TypeBytes>")
	o.RegisterFFI("json.Unmarshal", jsonBridge, jsonlib.MethodID_JSON_Unmarshal, "function(TypeBytes) Result<Any>")

	// 6. Inject time
	timeHost := &timelib.TimeHost{}
	timeBridge := &BridgeWrapper{
		Router: func(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
			return timelib.TimeHostRouter(ctx, timeHost, o.registry, methodID, args)
		},
	}
	o.RegisterFFI("time.Now", timeBridge, timelib.MethodID_Time_Now, "function() String")
	o.RegisterFFI("time.Sleep", timeBridge, timelib.MethodID_Time_Sleep, "function(Int64) Void")
	o.RegisterFFI("time.Since", timeBridge, timelib.MethodID_Time_Since, "function(String) Int64")
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
