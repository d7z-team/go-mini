package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/ffilib/byteslib"
	"gopkg.d7z.net/go-mini/core/ffilib/crypto/md5lib"
	"gopkg.d7z.net/go-mini/core/ffilib/crypto/sha256lib"
	"gopkg.d7z.net/go-mini/core/ffilib/encoding/base64lib"
	"gopkg.d7z.net/go-mini/core/ffilib/encoding/hexlib"
	"gopkg.d7z.net/go-mini/core/ffilib/errorslib"
	"gopkg.d7z.net/go-mini/core/ffilib/filepathlib"
	"gopkg.d7z.net/go-mini/core/ffilib/fmtlib"
	"gopkg.d7z.net/go-mini/core/ffilib/imagelib"
	"gopkg.d7z.net/go-mini/core/ffilib/iolib"
	"gopkg.d7z.net/go-mini/core/ffilib/jsonlib"
	"gopkg.d7z.net/go-mini/core/ffilib/math/randlib"
	"gopkg.d7z.net/go-mini/core/ffilib/mathlib"
	"gopkg.d7z.net/go-mini/core/ffilib/net/urllib"
	"gopkg.d7z.net/go-mini/core/ffilib/oslib"
	"gopkg.d7z.net/go-mini/core/ffilib/regexplib"
	"gopkg.d7z.net/go-mini/core/ffilib/sortlib"
	"gopkg.d7z.net/go-mini/core/ffilib/strconvlib"
	"gopkg.d7z.net/go-mini/core/ffilib/stringslib"
	"gopkg.d7z.net/go-mini/core/ffilib/timelib"
	"gopkg.d7z.net/go-mini/core/ffilib/unicode/utf8lib"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type MiniExecutor struct {
	mu sync.RWMutex

	Loader    func(path string) (*ast.ProgramStmt, error)
	bridges   map[uint32]ffigo.FFIBridge
	routes    map[string]runtime.FFIRoute
	constants map[string]string            // 全局常量表
	specs     map[ast.Ident]ast.GoMiniType // 用于验证的函数签名

	registry    *ffigo.HandleRegistry
	modules     map[string]*ast.ProgramStmt  // 预加载的模块蓝图
	structSpecs map[ast.Ident]ast.GoMiniType // 用于验证的结构体签名

	MaxTypeDepth int // 递归类型检查深度限制
}

func (e *MiniExecutor) SetMaxTypeDepth(depth int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.MaxTypeDepth = depth
}

type MiniProgram struct {
	Source   string
	Program  *ast.ProgramStmt
	Compiled *compiler.Artifact
	executor *runtime.Executor

	// LSP / Debugger 支撑
	parentMap map[ast.Node]ast.Node
	parentMu  sync.RWMutex // 保护 parentMap 读写（虽然 Program 是只读的，但缓存按需构建）
}

type StackContext = runtime.StackContext

// ReleaseLSPCache 释放 LSP 相关的缓存（ParentMap 等），以节省内存。
// 只有在不再需要进行 IDE 交互式查询时建议调用。
func (p *MiniProgram) ReleaseLSPCache() {
	p.parentMu.Lock()
	defer p.parentMu.Unlock()
	p.parentMap = nil
}

// ToVar 将 Go 侧数据转换为脚本侧 Var。主要用于宿主注入。
func (p *MiniProgram) ToVar(ctx *runtime.StackContext, val interface{}, bridge ffigo.FFIBridge) *runtime.Var {
	return p.executor.ToVar(ctx, val, bridge)
}

func (p *MiniProgram) Executor() *runtime.Executor {
	return p.executor
}

func (p *MiniProgram) Compilation() *compiler.Artifact {
	return p.Compiled
}

func (p *MiniProgram) CheckSatisfaction(val *runtime.Var, interfaceType ast.GoMiniType) (*runtime.Var, error) {
	return p.executor.CheckSatisfaction(val, interfaceType)
}

func (p *MiniProgram) ExecExpr(ctx *runtime.StackContext, s ast.Expr) (*runtime.Var, error) {
	return p.executor.ExecExpr(ctx, s)
}

// GetParent 获取节点的父节点
func (p *MiniProgram) GetParent(node ast.Node) ast.Node {
	p.parentMu.RLock()
	if p.parentMap != nil {
		if parent, ok := p.parentMap[node]; ok {
			p.parentMu.RUnlock()
			return parent
		}
	}
	p.parentMu.RUnlock()

	p.parentMu.Lock()
	defer p.parentMu.Unlock()
	if p.parentMap == nil {
		p.parentMap = ast.BuildParentMap(p.Program)
	}
	return p.parentMap[node]
}

// BuildAllCache 预构建所有缓存
func (p *MiniProgram) BuildAllCache() {
	p.parentMu.Lock()
	defer p.parentMu.Unlock()
	if p.parentMap == nil {
		p.parentMap = ast.BuildParentMap(p.Program)
	}
}

// GetNodeAt 获取指定位置的节点
func (p *MiniProgram) GetNodeAt(line, col int) ast.Node {
	return ast.FindNodeAt(p.Program, line, col)
}

// GetDefinitionAt 获取指定位置符号的定义
func (p *MiniProgram) GetDefinitionAt(line, col int) ast.Node {
	node := p.GetNodeAt(line, col)
	if node == nil {
		return nil
	}
	p.BuildAllCache() // 确保 parentMap 已就绪
	return ast.FindDefinition(p.Program, node, p.parentMap)
}

// GetHoverAt 获取指定位置符号的悬浮信息
func (p *MiniProgram) GetHoverAt(line, col int) *ast.HoverInfo {
	node := p.GetNodeAt(line, col)
	if node == nil {
		return nil
	}
	p.BuildAllCache()
	return ast.FindHoverInfo(p.Program, node, p.parentMap)
}

// GetReferencesAt 获取指定位置符号的所有引用
func (p *MiniProgram) GetReferencesAt(line, col int) []ast.Node {
	node := p.GetNodeAt(line, col)
	if node == nil {
		return nil
	}
	p.BuildAllCache()
	def := ast.FindDefinition(p.Program, node, p.parentMap)
	if def == nil {
		return nil
	}
	return ast.FindAllReferences(p.Program, def, p.parentMap)
}

// GetCompletionsAt 获取指定位置的代码补全建议
func (p *MiniProgram) GetCompletionsAt(line, col int) []ast.CompletionItem {
	return ast.FindCompletionsAt(p.Program, line, col)
}

func (p *MiniProgram) SetStepLimit(limit int64) {
	p.executor.StepLimit = limit
}

func (p *MiniProgram) Execute(ctx context.Context) error {
	return p.executor.Execute(ctx)
}

// ExecuteWithEnv 执行脚本，并允许注入初始环境变量
func (p *MiniProgram) ExecuteWithEnv(ctx context.Context, env map[string]*runtime.Var) error {
	return p.executor.ExecuteWithEnv(ctx, env)
}

func (p *MiniProgram) LastSession() *runtime.StackContext {
	return p.executor.LastSession()
}

func (p *MiniProgram) InvokeCallable(ctx *runtime.StackContext, callable *runtime.Var, methodName string, args []*runtime.Var) (*runtime.Var, error) {
	return p.executor.InvokeCallable(ctx, callable, methodName, args)
}

// Eval 在当前程序的语境下执行单个 Go 表达式
// 这允许你调用程序中定义的函数或访问全局变量
func (p *MiniProgram) Eval(ctx context.Context, exprStr string, env map[string]interface{}) (*runtime.Var, error) {
	expr, err := compiler.New(compiler.Config{}).CompileExprSource(exprStr)
	if err != nil {
		return nil, fmt.Errorf("表达式解析失败: %w", err)
	}

	// 创建基于当前程序蓝图的 session
	session := p.executor.NewSession(ctx, "eval")
	defer p.executor.CleanupSession(session)

	// 继承上次执行的状态（如 import 的模块和全局变量）
	if last := p.executor.LastSession(); last != nil {
		// 模块缓存继承
		if last.ModuleCache != nil {
			for k, v := range last.ModuleCache {
				session.ModuleCache[k] = v
			}
		}
		// 全局变量继承：追溯到最顶层作用域
		root := last.Stack
		if root != nil {
			for root.Parent != nil {
				root = root.Parent
			}
			if root.MemoryPtr != nil {
				for k, v := range root.MemoryPtr {
					session.Stack.MemoryPtr[k] = v
				}
			}
		}
	} else {
		if err := p.executor.InitializeSession(session, nil, false); err != nil {
			return nil, err
		}
	}

	// 注入环境
	for k, v := range env {
		_ = session.AddVariable(k, p.executor.ToVar(session, v, nil))
	}

	return p.executor.ExecExpr(session, expr)
}

// MustEval 类似于 Eval，但在出错时会触发 panic
func (p *MiniProgram) MustEval(ctx context.Context, exprStr string, env map[string]interface{}) *runtime.Var {
	res, err := p.Eval(ctx, exprStr, env)
	if err != nil {
		panic(err)
	}
	return res
}

func (p *MiniProgram) GetAst() *ast.ProgramStmt {
	return p.executor.GetProgram()
}

func (p *MiniProgram) Disassemble() string {
	if p == nil {
		return "; Error: invalid or uninitialized program\n"
	}
	if p.Compiled != nil && p.Compiled.Bytecode != nil {
		return p.Compiled.Bytecode.Disassemble()
	}
	if p.executor == nil {
		return "; Error: invalid or uninitialized program\n"
	}
	return p.executor.Disassemble()
}

func (p *MiniProgram) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.executor.GetProgram())
}

func (p *MiniProgram) MarshalIndentJSON(prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(p.executor.GetProgram(), prefix, indent)
}

func NewMiniExecutor() *MiniExecutor {
	res := &MiniExecutor{
		bridges:      make(map[uint32]ffigo.FFIBridge),
		routes:       make(map[string]runtime.FFIRoute),
		constants:    make(map[string]string),
		specs:        make(map[ast.Ident]ast.GoMiniType),
		registry:     ffigo.NewHandleRegistry(),
		modules:      make(map[string]*ast.ProgramStmt),
		structSpecs:  make(map[ast.Ident]ast.GoMiniType),
		MaxTypeDepth: 256,
	}

	// 默认注册 panic 签名以便通过验证
	res.specs["panic"] = "function(String) Void"
	res.specs["recover"] = "function() Any"
	res.specs["String"] = "function(Any) String"
	res.specs["TypeBytes"] = "function(Any) TypeBytes"
	res.specs["len"] = "function(Any) Int64"
	res.specs["cap"] = "function(Any) Int64"
	res.specs["make"] = "function(String, ...Int64) Any"
	res.specs["new"] = "function(String) Any"
	res.specs["append"] = "function(Any, ...Any) Any"
	res.specs["delete"] = "function(Any, Any) Void"
	res.specs["Int64"] = "function(Any) Int64"
	res.specs["Float64"] = "function(Any) Float64"
	res.specs["require"] = "function(String) TypeModule"

	// Inject non-IO libraries by default
	errorslib.RegisterErrors(res, &errorslib.ErrorsHost{}, res.registry)
	res.RegisterFFI("errors.Is", nil, 999999999, "function(Error, TypeHandle) Bool", "Check if an error matches a target handle")
	jsonlib.RegisterJSON(res, &jsonlib.JSONHost{}, res.registry)
	timelib.RegisterTimeAll(res, &timelib.TimeHost{}, res.registry)
	stringslib.RegisterStrings(res, &stringslib.StringsHost{}, res.registry)
	mathlib.RegisterMath(res, &mathlib.MathHost{}, res.registry)
	filepathlib.RegisterFilepath(res, &filepathlib.FilepathHost{}, res.registry)
	strconvlib.RegisterStrconv(res, &strconvlib.StrconvHost{}, res.registry)
	byteslib.RegisterBytes(res, &byteslib.BytesHost{}, res.registry)
	sortlib.RegisterSort(res, &sortlib.SortHost{}, res.registry)
	regexplib.RegisterRegexp(res, &regexplib.RegexpHost{}, res.registry)
	randlib.RegisterRand(res, &randlib.RandHost{}, res.registry)
	utf8lib.RegisterUTF8(res, &utf8lib.UTF8Host{}, res.registry)
	base64lib.RegisterBase64(res, &base64lib.Base64Host{}, res.registry)
	hexlib.RegisterHex(res, &hexlib.HexHost{}, res.registry)
	md5lib.RegisterMD5(res, &md5lib.MD5Host{}, res.registry)
	sha256lib.RegisterSHA256(res, &sha256lib.SHA256Host{}, res.registry)
	urllib.RegisterURL(res, &urllib.URLHost{}, res.registry)

	// Inject fmt by default (supports context-based redirection)
	fmtImpl := &fmtlib.FmtHost{}
	fmtlib.RegisterFmt(res, fmtImpl, res.registry)
	fmtlib.RegisterFmtAliases(res, fmtImpl, res.registry)

	return res
}

func (e *MiniExecutor) getLoader() func(path string) (*ast.ProgramStmt, error) {
	return func(path string) (*ast.ProgramStmt, error) {
		e.mu.RLock()
		defer e.mu.RUnlock()
		if astNode, ok := e.modules[path]; ok {
			return astNode, nil
		}
		if e.Loader != nil {
			return e.Loader(path)
		}
		return nil, fmt.Errorf("module not found: %s", path)
	}
}

func (e *MiniExecutor) prepareExecutor(program *ast.ProgramStmt) (*runtime.Executor, error) {
	executor, err := runtime.NewExecutor(program)
	if err != nil {
		return nil, err
	}

	executor.Loader = e.getLoader()

	e.mu.RLock()
	defer e.mu.RUnlock()
	for name, route := range e.routes {
		executor.RegisterRoute(name, route)
	}
	for name, val := range e.constants {
		executor.RegisterConstant(name, val)
	}
	return executor, nil
}

func (e *MiniExecutor) newCompiler() *compiler.Compiler {
	return compiler.New(compiler.Config{
		Loader:       e.getLoader(),
		Specs:        e.GetExportedSpecs(),
		Constants:    e.GetExportedConstants(),
		MaxTypeDepth: e.MaxTypeDepth,
	})
}

func (e *MiniExecutor) SetLoader(loader func(path string) (*ast.ProgramStmt, error)) {
	e.Loader = loader
}

// RegisterModule 注册一个预编译的模块，使得脚本可以通过 import 直接引用
func (e *MiniExecutor) RegisterModule(path string, prog *MiniProgram) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.modules[path] = prog.GetAst()
}

func (e *MiniExecutor) HandleRegistry() *ffigo.HandleRegistry {
	return e.registry
}

// RegisterFFI 注册一个外部函数到特定的 Bridge 和 ID
func (e *MiniExecutor) RegisterFFI(name string, bridge ffigo.FFIBridge, methodID uint32, spec ast.GoMiniType, doc string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	returns := "Void"
	callFunc, ok := spec.ReadCallFunc()
	if ok {
		returns = string(callFunc.Returns)
	}

	e.routes[name] = runtime.FFIRoute{Name: name, Bridge: bridge, MethodID: methodID, Returns: returns, Spec: string(spec), Doc: doc}
	if spec != "" {
		e.specs[ast.Ident(name)] = spec
	}
}

// RegisterConstant 注册一个全局常量到执行器
func (e *MiniExecutor) RegisterConstant(name string, val string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.constants[name] = val
}

func (e *MiniExecutor) RegisterBridge(methodID uint32, bridge ffigo.FFIBridge, spec ast.GoMiniType) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bridges[methodID] = bridge
}

type BridgeWrapper struct {
	Router func(ctx context.Context, methodID uint32, args []byte) ([]byte, error)
}

func (b *BridgeWrapper) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return b.Router(ctx, methodID, args)
}

func (b *BridgeWrapper) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return nil, errors.New("invoke not supported on BridgeWrapper")
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

func (b *HandleBridgeWrapper) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return nil, errors.New("invoke not supported on HandleBridgeWrapper")
}

func (b *HandleBridgeWrapper) DestroyHandle(handle uint32) error {
	b.Registry.Remove(handle)
	return nil
}

func (e *MiniExecutor) InjectStandardLibraries() {
	// 1. Inject os
	oslib.RegisterOS(e, &oslib.OSHost{}, e.registry)

	// 2. Inject io
	iolib.RegisterIOAll(e, &iolib.IOHost{}, e.registry)

	// 3. Inject image
	imagelib.RegisterImageAll(e, &imagelib.ImageHost{}, e.registry)

	// 4. Inject math
	mathlib.RegisterMath(e, &mathlib.MathHost{}, e.registry)
}

// GetExportedSpecs 返回所有注册的 FFI 函数签名
func (e *MiniExecutor) GetExportedConstants() map[string]string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	res := make(map[string]string)
	for k, v := range e.constants {
		res[k] = v
	}
	return res
}

func (e *MiniExecutor) GetExportedSpecs() map[ast.Ident]ast.GoMiniType {
	e.mu.RLock()
	defer e.mu.RUnlock()
	res := make(map[ast.Ident]ast.GoMiniType)
	for k, v := range e.specs {
		res[k] = v
	}
	for k, v := range e.structSpecs {
		res[k] = v
	}
	return res
}

// AddFuncSpec 仅用于在验证阶段声明一个合法的外部函数
func (e *MiniExecutor) AddFuncSpec(name string, spec ast.GoMiniType) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.specs[ast.Ident(name)] = spec
}

func (e *MiniExecutor) RegisterStructSpec(name string, spec ast.GoMiniType) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.structSpecs[ast.Ident(name)] = spec
}

func (e *MiniExecutor) AddStructSpec(name string, spec ast.GoMiniType) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.structSpecs[ast.Ident(name)] = spec
}

func (e *MiniExecutor) GetExportedStructs() map[ast.Ident]ast.GoMiniType {
	e.mu.RLock()
	defer e.mu.RUnlock()
	res := make(map[ast.Ident]ast.GoMiniType)
	for k, v := range e.structSpecs {
		res[k] = v
	}
	return res
}

func (e *MiniExecutor) NewRuntimeByAst(program *ast.ProgramStmt) (*MiniProgram, error) {
	compiled, semanticCtx, err := e.newCompiler().CompileProgram("ast", "", program, false)
	if err != nil {
		var logs []ast.Logs
		if semanticCtx != nil {
			logs = semanticCtx.Logs()
		}
		return nil, &ast.MiniAstError{Err: err, Logs: logs, Node: program}
	}
	return e.NewRuntimeByCompiled(compiled)
}

func (e *MiniExecutor) NewRuntimeByCompiled(compiled *compiler.Artifact) (*MiniProgram, error) {
	if compiled == nil || compiled.Program == nil {
		return nil, errors.New("invalid compiled program")
	}

	executor, err := e.prepareExecutor(compiled.Program)
	if err != nil {
		return nil, err
	}
	if len(compiled.GlobalInitOrder) > 0 {
		executor.SetGlobalInitOrder(compiled.GlobalInitOrder)
	}

	return &MiniProgram{
		Source:   compiled.Source,
		Program:  compiled.Program,
		Compiled: compiled,
		executor: executor,
	}, nil
}

func (e *MiniExecutor) CompileGoCode(code string) (*compiler.Artifact, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileSource("snippet", code, false)
	if err != nil {
		var logs []ast.Logs
		if semanticCtx != nil {
			logs = semanticCtx.Logs()
		}
		node := (*ast.ProgramStmt)(nil)
		if compiled != nil {
			node = compiled.Program
		}
		return nil, &ast.MiniAstError{Err: err, Logs: logs, Node: node}
	}
	return compiled, nil
}

func (e *MiniExecutor) CompileGoFile(filename, code string) (*compiler.Artifact, error) {
	compiled, _, semanticCtx, err := e.newCompiler().CompileSource(filename, code, false)
	if err != nil {
		var logs []ast.Logs
		if semanticCtx != nil {
			logs = semanticCtx.Logs()
		}
		node := (*ast.ProgramStmt)(nil)
		if compiled != nil {
			node = compiled.Program
		}
		return nil, &ast.MiniAstError{Err: err, Logs: logs, Node: node}
	}
	return compiled, nil
}

func (e *MiniExecutor) NewRuntimeByGoCode(code string) (*MiniProgram, error) {
	prog, _, err := e.newMiniProgramByGoCode("snippet", code, false)
	return prog, err
}

func (e *MiniExecutor) NewRuntimeByGoFile(filename, code string) (*MiniProgram, error) {
	prog, _, err := e.newMiniProgramByGoCode(filename, code, false)
	return prog, err
}

func (e *MiniExecutor) NewMiniProgramByGoCodeTolerant(code string) (*MiniProgram, []error) {
	prog, errs, _ := e.newMiniProgramByGoCode("snippet", code, true)
	return prog, errs
}

func (e *MiniExecutor) NewMiniProgramByGoFileTolerant(filename, code string) (*MiniProgram, []error) {
	prog, errs, _ := e.newMiniProgramByGoCode(filename, code, true)
	return prog, errs
}

func (e *MiniExecutor) NewMiniProgramByAstTolerant(program *ast.ProgramStmt) (*MiniProgram, []error) {
	var errs []error
	compiled, _, err := e.newCompiler().CompileProgram("ast", "", program, true)
	if err != nil {
		errs = append(errs, err)
	}

	res, rErr := e.NewRuntimeByCompiled(compiled)
	if rErr != nil {
		errs = append(errs, rErr)
		res = &MiniProgram{
			Program:  program,
			Compiled: compiled,
			executor: &runtime.Executor{},
		}
	}
	return res, errs
}

func (e *MiniExecutor) newMiniProgramByGoCode(filename, code string, tolerant bool) (*MiniProgram, []error, error) {
	compiled, errs, semanticCtx, err := e.newCompiler().CompileSource(filename, code, tolerant)
	if err != nil {
		if !tolerant {
			var logs []ast.Logs
			if semanticCtx != nil {
				logs = semanticCtx.Logs()
			}
			node := (*ast.ProgramStmt)(nil)
			if compiled != nil {
				node = compiled.Program
			}
			return nil, nil, &ast.MiniAstError{Err: err, Logs: logs, Node: node}
		}
		errs = append(errs, err)
	}

	var res *MiniProgram
	if compiled == nil {
		return nil, errs, errors.New("failed to compile source")
	}

	executor, err := e.NewRuntimeByCompiled(compiled)
	if err != nil {
		if !tolerant {
			return nil, nil, err
		}
		errs = append(errs, err)
		// 创建一个仅包含 AST 的半成品 MiniProgram 供 LSP 使用
		res = &MiniProgram{
			Program:  compiled.Program,
			Compiled: compiled,
			executor: &runtime.Executor{}, // 空执行器
		}
	} else {
		res = executor
	}
	if res != nil {
		res.Source = code
	}
	return res, errs, nil
}

func (e *MiniExecutor) injectEnv(session *runtime.StackContext, env map[string]interface{}) {
	if env == nil {
		return
	}
	for k, v := range env {
		_ = session.AddVariable(k, session.Executor.ToVar(session, v, nil))
	}
}

// Eval 执行单个 Go 表达式字符串
func (e *MiniExecutor) Eval(ctx context.Context, exprStr string, env map[string]interface{}) (*runtime.Var, error) {
	expr, err := e.newCompiler().CompileExprSource(exprStr)
	if err != nil {
		return nil, fmt.Errorf("表达式解析失败: %w", err)
	}

	// 创建最小化的无状态执行器
	executor, _ := e.prepareExecutor(&ast.ProgramStmt{
		BaseNode: ast.BaseNode{ID: "eval", Meta: "boot"},
	})

	session := executor.NewSession(ctx, "eval")
	defer executor.CleanupSession(session)

	e.injectEnv(session, env)

	return executor.ExecExpr(session, expr)
}

// MustEval 类似于 Eval，但在出错时会触发 panic
func (e *MiniExecutor) MustEval(ctx context.Context, exprStr string, env map[string]interface{}) *runtime.Var {
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
		BaseNode:  ast.BaseNode{ID: "snippet", Meta: "boot"},
		Main:      stmts,
		Structs:   make(map[ast.Ident]*ast.StructStmt),
		Constants: make(map[string]string),
	}
	// 注入所有已注册的模块中的符号，以便在 Snippet 中使用
	e.mu.RLock()
	for _, s := range e.modules {
		for name, sDef := range s.Structs {
			program.Structs[name] = sDef
		}
		for name, cDef := range s.Constants {
			program.Constants[name] = cDef
		}
	}
	e.mu.RUnlock()

	compiled, semanticCtx, err := e.newCompiler().CompileProgram("snippet", code, program, false)
	if err != nil {
		var logs []ast.Logs
		if semanticCtx != nil {
			logs = semanticCtx.Logs()
		}
		return &ast.MiniAstError{Err: err, Logs: logs, Node: program}
	}

	executor, err := e.NewRuntimeByCompiled(compiled)
	if err != nil {
		return err
	}

	session := executor.executor.NewSession(ctx, "snippet")
	defer executor.executor.CleanupSession(session)

	e.injectEnv(session, env)

	return executor.executor.ExecuteStmts(session, compiled.Program.Main)
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
	executor, _ := e.prepareExecutor(&ast.ProgramStmt{
		BaseNode: ast.BaseNode{ID: "import_loader", Meta: "boot"},
	})

	session := executor.NewSession(ctx, "loader")
	defer executor.CleanupSession(session)

	return executor.ImportModule(session, &ast.ImportExpr{Path: path})
}

// NewRuntimeByJSON 从序列化后的 JSON AST 数据加载并构建执行环境
func (e *MiniExecutor) NewRuntimeByJSON(data []byte) (*MiniProgram, error) {
	node, err := Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("JSON 反序列化失败: %w", err)
	}

	program, ok := node.(*ast.ProgramStmt)
	if !ok {
		// 如果不是 ProgramStmt，则封装为一个
		program = &ast.ProgramStmt{
			BaseNode:  ast.BaseNode{ID: "boot", Meta: "boot"},
			Constants: make(map[string]string),
			Variables: make(map[ast.Ident]ast.Expr),
			Types:     make(map[ast.Ident]ast.GoMiniType),
			Structs:   make(map[ast.Ident]*ast.StructStmt),
			Functions: make(map[ast.Ident]*ast.FunctionStmt),
			Main:      make([]ast.Stmt, 0),
		}
		if block, ok := node.(*ast.BlockStmt); ok {
			program.Main = block.Children
		}
	}

	compiled, semanticCtx, err := e.newCompiler().CompileProgram("json", "", program, false)
	if err != nil {
		var logs []ast.Logs
		if semanticCtx != nil {
			logs = semanticCtx.Logs()
		}
		return nil, &ast.MiniAstError{Err: err, Logs: logs, Node: program}
	}

	return e.NewRuntimeByCompiled(compiled)
}

// LSP Metadata Export

type ExportedMetadata struct {
	Builtins  map[string]string          `json:"builtins"`  // 内置函数名 -> 签名
	Modules   map[string]*ExportedModule `json:"modules"`   // 模块名 -> 模块信息
	Constants map[string]string          `json:"constants"` // 全局常量
}

type ExportedModule struct {
	Functions map[string]string          `json:"functions"` // 函数名 -> 签名
	Structs   map[string]*ExportedStruct `json:"structs"`   // 结构体名 -> 结构体信息
	Constants map[string]string          `json:"constants"` // 模块内常量
	Doc       string                     `json:"doc,omitempty"`
}

type ExportedStruct struct {
	Fields  map[string]string `json:"fields"`  // 字段名 -> 类型
	Methods map[string]string `json:"methods"` // 方法名 -> 签名
	Doc     string            `json:"doc,omitempty"`
}

// ExportMetadata 导出当前执行器中注册的所有符号，供 IDE 和 LSP 提供代码补全
func (e *MiniExecutor) ExportMetadata() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	meta := &ExportedMetadata{
		Builtins:  make(map[string]string),
		Modules:   make(map[string]*ExportedModule),
		Constants: make(map[string]string),
	}

	getModule := func(name string) *ExportedModule {
		if _, ok := meta.Modules[name]; !ok {
			meta.Modules[name] = &ExportedModule{
				Functions: make(map[string]string),
				Structs:   make(map[string]*ExportedStruct),
				Constants: make(map[string]string),
			}
		}
		return meta.Modules[name]
	}

	getStruct := func(modName, structName string) *ExportedStruct {
		mod := getModule(modName)
		if _, ok := mod.Structs[structName]; !ok {
			mod.Structs[structName] = &ExportedStruct{
				Fields:  make(map[string]string),
				Methods: make(map[string]string),
			}
		}
		return mod.Structs[structName]
	}

	// 1. 处理 Builtins 和 全局 Specs
	for name, spec := range e.specs {
		sName := string(name)
		if !strings.Contains(sName, ".") && !strings.HasPrefix(sName, "__") {
			meta.Builtins[sName] = string(spec)
		}
	}

	// 2. 处理 FFI Routes 和 Methods
	for routeName, route := range e.routes {
		if strings.HasPrefix(routeName, "__method_") {
			// __method_TypeName_MethodName
			parts := strings.Split(routeName, "_")
			if len(parts) >= 5 { // ["", "", "method", "TypeName", "MethodName"]
				typeName := parts[3]
				methodName := strings.Join(parts[4:], "_")
				sig := route.Spec
				if route.Doc != "" {
					sig += " // " + strings.ReplaceAll(route.Doc, "\n", " ")
				}
				// FFI 结构体通常归属于 "__ffi__"
				st := getStruct("__ffi__", typeName)
				st.Methods[methodName] = sig
			}
		} else if strings.Contains(routeName, ".") {
			parts := strings.SplitN(routeName, ".", 2)
			modName, funcName := parts[0], parts[1]
			sig := route.Spec
			if route.Doc != "" {
				sig += " // " + strings.ReplaceAll(route.Doc, "\n", " ")
			}
			getModule(modName).Functions[funcName] = sig
		}
	}

	// 3. 处理已加载的 Modules (脚本模块)
	for modName, prog := range e.modules {
		mod := getModule(modName)
		// 导出函数
		for fnName, fnStmt := range prog.Functions {
			if len(fnName) > 0 && fnName[0] >= 'A' && fnName[0] <= 'Z' {
				// 我们需要一种方式导出函数文档，目前 ExportedModule.Functions 只是 map[string]string (name -> sig)

				sig := string(fnStmt.FunctionType.MiniType())
				if fnStmt.Doc != "" {
					sig = sig + " // " + strings.ReplaceAll(fnStmt.Doc, "\n", " ")
				}
				mod.Functions[string(fnName)] = sig
			}
		}
		// 导出结构体
		for stName, stStmt := range prog.Structs {
			if len(stName) > 0 && stName[0] >= 'A' && stName[0] <= 'Z' {
				st := getStruct(modName, string(stName))
				st.Doc = stStmt.Doc
				for fName, fType := range stStmt.Fields {
					st.Fields[string(fName)] = string(fType)
				}
			}
		}
		// 导出常量
		for cName, cVal := range prog.Constants {
			if len(cName) > 0 && cName[0] >= 'A' && cName[0] <= 'Z' {
				mod.Constants[cName] = cVal
			}
		}
	}

	data, _ := json.MarshalIndent(meta, "", "  ")
	return string(data)
}
