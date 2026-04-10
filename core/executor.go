package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
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

	ModuleLoader func(path string) (*ast.ProgramStmt, error)
	bridges      map[uint32]ffigo.FFIBridge
	routes       map[string]runtime.FFIRoute
	constants    map[string]string

	registry       *ffigo.HandleRegistry
	modules        map[string]*ast.ProgramStmt // 预加载的模块蓝图
	moduleBytecode map[string]*bytecode.Program
	funcSchemas    map[ast.Ident]*runtime.RuntimeFuncSig
	structsMeta    map[ast.Ident]*runtime.RuntimeStructSpec

	MaxTypeDepth int // 递归类型检查深度限制
}

type ExportedSchemaSnapshot struct {
	Funcs   map[ast.Ident]*runtime.RuntimeFuncSig
	Structs map[ast.Ident]*runtime.RuntimeStructSpec
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
			if root.Globals != nil {
				for k, v := range root.Globals {
					session.Stack.Globals[k] = v
				}
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
	return p.MarshalBytecodeJSON()
}

func (p *MiniProgram) MarshalIndentJSON(prefix, indent string) ([]byte, error) {
	return p.MarshalIndentBytecodeJSON(prefix, indent)
}

func (p *MiniProgram) MarshalBytecodeJSON() ([]byte, error) {
	if p == nil || p.Compiled == nil {
		return nil, errors.New("cannot marshal bytecode from empty program")
	}
	return p.Compiled.MarshalBytecodeJSON()
}

func (p *MiniProgram) MarshalIndentBytecodeJSON(prefix, indent string) ([]byte, error) {
	if p == nil || p.Compiled == nil {
		return nil, errors.New("cannot marshal bytecode from empty program")
	}
	return p.Compiled.MarshalIndentBytecodeJSON(prefix, indent)
}

func (p *MiniProgram) Bytecode() (*bytecode.Program, error) {
	if p == nil || p.Compiled == nil || p.Compiled.Bytecode == nil {
		return nil, errors.New("program does not contain bytecode")
	}
	return p.Compiled.Bytecode, nil
}

func NewMiniExecutor() *MiniExecutor {
	res := &MiniExecutor{
		bridges:        make(map[uint32]ffigo.FFIBridge),
		routes:         make(map[string]runtime.FFIRoute),
		constants:      make(map[string]string),
		registry:       ffigo.NewHandleRegistry(),
		modules:        make(map[string]*ast.ProgramStmt),
		moduleBytecode: make(map[string]*bytecode.Program),
		funcSchemas:    make(map[ast.Ident]*runtime.RuntimeFuncSig),
		structsMeta:    make(map[ast.Ident]*runtime.RuntimeStructSpec),
		MaxTypeDepth:   256,
	}

	// 默认注册 panic 签名以便通过验证
	res.mustAddFuncSchemaLocked("panic", runtime.MustParseRuntimeFuncSig("function(String) Void"))
	res.mustAddFuncSchemaLocked("recover", runtime.MustParseRuntimeFuncSig("function() Any"))
	res.mustAddFuncSchemaLocked("String", runtime.MustParseRuntimeFuncSig("function(Any) String"))
	res.mustAddFuncSchemaLocked("TypeBytes", runtime.MustParseRuntimeFuncSig("function(Any) TypeBytes"))
	res.mustAddFuncSchemaLocked("len", runtime.MustParseRuntimeFuncSig("function(Any) Int64"))
	res.mustAddFuncSchemaLocked("cap", runtime.MustParseRuntimeFuncSig("function(Any) Int64"))
	res.mustAddFuncSchemaLocked("make", runtime.MustParseRuntimeFuncSig("function(String, ...Int64) Any"))
	res.mustAddFuncSchemaLocked("new", runtime.MustParseRuntimeFuncSig("function(String) Any"))
	res.mustAddFuncSchemaLocked("append", runtime.MustParseRuntimeFuncSig("function(Any, ...Any) Any"))
	res.mustAddFuncSchemaLocked("delete", runtime.MustParseRuntimeFuncSig("function(Any, Any) Void"))
	res.mustAddFuncSchemaLocked("Int64", runtime.MustParseRuntimeFuncSig("function(Any) Int64"))
	res.mustAddFuncSchemaLocked("Float64", runtime.MustParseRuntimeFuncSig("function(Any) Float64"))
	res.mustAddFuncSchemaLocked("require", runtime.MustParseRuntimeFuncSig("function(String) TypeModule"))

	// Inject non-IO libraries by default
	errorslib.RegisterErrors(res, &errorslib.ErrorsHost{}, res.registry)
	res.RegisterFFISchema("errors.Is", nil, 999999999, runtime.MustParseRuntimeFuncSig("function(Error, TypeHandle) Bool"), "Check if an error matches a target handle")
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

func (e *MiniExecutor) moduleASTLoader() func(path string) (*ast.ProgramStmt, error) {
	return func(path string) (*ast.ProgramStmt, error) {
		e.mu.RLock()
		defer e.mu.RUnlock()
		if astNode, ok := e.modules[path]; ok {
			return astNode, nil
		}
		if e.ModuleLoader != nil {
			return e.ModuleLoader(path)
		}
		return nil, fmt.Errorf("module not found: %s", path)
	}
}

func (e *MiniExecutor) modulePlanLoader() func(path string) (*ast.ProgramStmt, *runtime.PreparedProgram, error) {
	return func(path string) (*ast.ProgramStmt, *runtime.PreparedProgram, error) {
		e.mu.RLock()
		if prog, ok := e.modules[path]; ok {
			if bc, ok := e.moduleBytecode[path]; ok && bc != nil && bc.Executable != nil {
				e.mu.RUnlock()
				return prog, bc.Executable, nil
			}
			e.mu.RUnlock()
			return prog, nil, nil
		}
		loader := e.ModuleLoader
		e.mu.RUnlock()
		if loader != nil {
			prog, err := loader(path)
			return prog, nil, err
		}
		return nil, nil, fmt.Errorf("module not found: %s", path)
	}
}

func (e *MiniExecutor) applyExecutorConfig(executor *runtime.Executor) {
	if executor == nil {
		return
	}
	executor.ModuleLoader = e.moduleASTLoader()
	executor.ModulePlanLoader = e.modulePlanLoader()

	e.mu.RLock()
	defer e.mu.RUnlock()
	for name, route := range e.routes {
		executor.RegisterRoute(name, route)
	}
	for name, spec := range e.structsMeta {
		executor.RegisterStructSchema(string(name), spec)
	}
	for name, val := range e.constants {
		executor.RegisterConstant(name, val)
	}
}

func (e *MiniExecutor) newCompiler() *compiler.Compiler {
	schema := e.ExportedSchema()
	return compiler.New(compiler.Config{
		ModuleLoader:  e.moduleASTLoader(),
		FuncSchemas:   schema.Funcs,
		StructSchemas: schema.Structs,
		Constants:     e.GetExportedConstants(),
		MaxTypeDepth:  e.MaxTypeDepth,
	})
}

func (e *MiniExecutor) SetModuleLoader(loader func(path string) (*ast.ProgramStmt, error)) {
	e.ModuleLoader = loader
}

// RegisterModule 注册一个预编译的模块，使得脚本可以通过 import 直接引用
func (e *MiniExecutor) RegisterModule(path string, prog *MiniProgram) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.modules[path] = prog.GetAst()
	if prog != nil && prog.Compiled != nil && prog.Compiled.Bytecode != nil {
		e.moduleBytecode[path] = prog.Compiled.Bytecode
	}
}

func (e *MiniExecutor) CompileGoCodeToBytecodeJSON(code string) ([]byte, error) {
	compiled, err := e.CompileGoCode(code)
	if err != nil {
		return nil, err
	}
	return compiled.MarshalBytecodeJSON()
}

func (e *MiniExecutor) CompileGoCodeToBytecode(code string) (*bytecode.Program, error) {
	compiled, err := e.CompileGoCode(code)
	if err != nil {
		return nil, err
	}
	if compiled == nil || compiled.Bytecode == nil {
		return nil, errors.New("compiled program missing bytecode")
	}
	return compiled.Bytecode, nil
}

func (e *MiniExecutor) CompileGoFileToBytecode(filename, code string) (*bytecode.Program, error) {
	compiled, err := e.CompileGoFile(filename, code)
	if err != nil {
		return nil, err
	}
	if compiled == nil || compiled.Bytecode == nil {
		return nil, errors.New("compiled program missing bytecode")
	}
	return compiled.Bytecode, nil
}

func (e *MiniExecutor) HandleRegistry() *ffigo.HandleRegistry {
	return e.registry
}

func (e *MiniExecutor) Executor() *runtime.Executor {
	prepared, _ := runtime.PrepareProgram(&ast.ProgramStmt{})
	executor, _ := runtime.NewExecutorFromPrepared(&ast.ProgramStmt{}, prepared)
	e.applyExecutorConfig(executor)
	return executor
}

func (e *MiniExecutor) RegisterFFISchema(name string, bridge ffigo.FFIBridge, methodID uint32, sig *runtime.RuntimeFuncSig, doc string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registerFFISchemaLocked(name, bridge, methodID, sig, doc)
}

// RegisterConstant 注册一个全局常量到执行器
func (e *MiniExecutor) RegisterConstant(name, val string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.constants[name] = val
}

func (e *MiniExecutor) RegisterBridge(methodID uint32, bridge ffigo.FFIBridge) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bridges[methodID] = bridge
}

type RoutedBridge struct {
	Router func(ctx context.Context, methodID uint32, args []byte) ([]byte, error)
}

func (b *RoutedBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return b.Router(ctx, methodID, args)
}

func (b *RoutedBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return nil, errors.New("invoke not supported on RoutedBridge")
}

func (b *RoutedBridge) DestroyHandle(handle uint32) error {
	return nil // Base wrapper doesn't manage registry
}

type RoutedHandleBridge struct {
	Registry *ffigo.HandleRegistry
	Router   func(ctx context.Context, reg *ffigo.HandleRegistry, methodID uint32, args []byte) ([]byte, error)
}

func (b *RoutedHandleBridge) Call(ctx context.Context, methodID uint32, args []byte) ([]byte, error) {
	return b.Router(ctx, b.Registry, methodID, args)
}

func (b *RoutedHandleBridge) Invoke(ctx context.Context, method string, args []byte) ([]byte, error) {
	return nil, errors.New("invoke not supported on RoutedHandleBridge")
}

func (b *RoutedHandleBridge) DestroyHandle(handle uint32) error {
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

func (e *MiniExecutor) GetExportedConstants() map[string]string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	res := make(map[string]string)
	for k, v := range e.constants {
		res[k] = v
	}
	return res
}

// DeclareFuncSchema 仅用于在验证阶段声明一个合法的外部函数。
func (e *MiniExecutor) DeclareFuncSchema(name string, sig *runtime.RuntimeFuncSig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if sig == nil {
		delete(e.funcSchemas, ast.Ident(name))
		return
	}
	e.funcSchemas[ast.Ident(name)] = sig
}

func (e *MiniExecutor) RegisterStructSchema(name string, spec *runtime.RuntimeStructSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registerStructSchemaLocked(name, spec)
}

// DeclareStructSchema 仅用于在验证阶段声明一个合法的外部结构体 schema。
func (e *MiniExecutor) DeclareStructSchema(name string, spec *runtime.RuntimeStructSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.registerStructSchemaLocked(name, spec)
}

func (e *MiniExecutor) ExportedSchema() *ExportedSchemaSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	res := &ExportedSchemaSnapshot{
		Funcs:   make(map[ast.Ident]*runtime.RuntimeFuncSig, len(e.funcSchemas)),
		Structs: make(map[ast.Ident]*runtime.RuntimeStructSpec, len(e.structsMeta)),
	}
	for k, v := range e.funcSchemas {
		res.Funcs[k] = cloneRuntimeFuncSig(v)
	}
	for k, v := range e.structsMeta {
		res.Structs[k] = cloneRuntimeStructSpec(v)
	}
	return res
}

func (e *MiniExecutor) NewRuntimeByProgram(program *ast.ProgramStmt) (*MiniProgram, error) {
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
	if compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
		return nil, errors.New("compiled program missing executable bytecode")
	}

	executor, err := runtime.NewExecutorFromPrepared(compiled.Program, compiled.Bytecode.Executable)
	if err != nil {
		return nil, err
	}
	e.applyExecutorConfig(executor)
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

func (e *MiniExecutor) NewRuntimeByBytecode(program *bytecode.Program) (*MiniProgram, error) {
	compiled, err := compiler.ArtifactFromBytecode(program)
	if err != nil {
		return nil, err
	}
	return e.NewRuntimeByCompiled(compiled)
}

func (e *MiniExecutor) ArtifactFromBytecode(program *bytecode.Program) (*compiler.Artifact, error) {
	return compiler.ArtifactFromBytecode(program)
}

func (e *MiniExecutor) ArtifactFromBytecodeJSON(payload []byte) (*compiler.Artifact, error) {
	return compiler.ArtifactFromBytecodeJSON(payload)
}

func (e *MiniExecutor) NewRuntimeByBytecodeJSON(payload []byte) (*MiniProgram, error) {
	program, err := bytecode.UnmarshalJSON(payload)
	if err != nil {
		return nil, err
	}
	return e.NewRuntimeByBytecode(program)
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

func (e *MiniExecutor) NewMiniProgramByProgramTolerant(program *ast.ProgramStmt) (*MiniProgram, []error) {
	var errs []error
	compiled, _, err := e.newCompiler().CompileProgram("ast", "", program, true)
	if err != nil {
		errs = append(errs, err)
	}
	return &MiniProgram{
		Program:  program,
		Compiled: compiled,
		executor: &runtime.Executor{},
	}, errs
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

	if tolerant {
		res = &MiniProgram{
			Program:  compiled.Program,
			Compiled: compiled,
			executor: &runtime.Executor{},
		}
		res.Source = code
		return res, errs, nil
	}

	executor, err := e.NewRuntimeByCompiled(compiled)
	if err != nil {
		return nil, nil, err
	}
	res = executor
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
	base := &ast.ProgramStmt{BaseNode: ast.BaseNode{ID: "eval", Meta: "boot"}}
	prepared, _ := runtime.PrepareProgram(base)
	executor, _ := runtime.NewExecutorFromPrepared(base, prepared)
	e.applyExecutorConfig(executor)

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
	base := &ast.ProgramStmt{BaseNode: ast.BaseNode{ID: "import_loader", Meta: "boot"}}
	prepared, _ := runtime.PrepareProgram(base)
	executor, _ := runtime.NewExecutorFromPrepared(base, prepared)
	e.applyExecutorConfig(executor)

	session := executor.NewSession(ctx, "loader")
	defer executor.CleanupSession(session)

	return executor.ImportModule(session, &ast.ImportExpr{Path: path})
}

// NewRuntimeByJSON 从序列化后的 bytecode JSON 数据加载并构建执行环境。
func (e *MiniExecutor) NewRuntimeByJSON(data []byte) (*MiniProgram, error) {
	var probe struct {
		Format string `json:"format"`
	}
	if err := json.Unmarshal(data, &probe); err == nil && probe.Format == bytecode.FormatGoMiniBytecode {
		return e.NewRuntimeByBytecodeJSON(data)
	}
	return nil, errors.New("invalid json payload: expected go-mini bytecode")
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

	// 1. 导出 builtin 函数签名
	for name, spec := range e.funcSchemas {
		sName := string(name)
		if !strings.Contains(sName, ".") && !strings.HasPrefix(sName, "__") {
			meta.Builtins[sName] = e.formatSchemaWithDoc(spec.Spec, "", spec)
		}
	}

	// 2. 处理 FFI Routes 和 Methods
	for routeName, route := range e.routes {
		sig := e.formatRouteSchema(route)
		// 处理新版 Type.Method 格式
		if strings.Contains(routeName, ".") && strings.Count(routeName, ".") == 1 {
			parts := strings.SplitN(routeName, ".", 2)
			typeName, methodName := parts[0], parts[1]
			// 如果 typeName 看起来像是一个已经注册的结构体
			if _, ok := e.structsMeta[ast.Ident(typeName)]; ok {
				getStruct("ffi", typeName).Methods[methodName] = sig
				continue
			}
		}

		if strings.Count(routeName, ".") >= 2 {
			parts := strings.SplitN(routeName, ".", 3)
			modName, typeName, methodName := parts[0], parts[1], parts[2]
			getStruct(modName, typeName).Methods[methodName] = sig
			continue
		}
		if strings.Contains(routeName, ".") {
			parts := strings.SplitN(routeName, ".", 2)
			modName, funcName := parts[0], parts[1]
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

func (e *MiniExecutor) registerFFISchemaLocked(name string, bridge ffigo.FFIBridge, methodID uint32, funcSig *runtime.RuntimeFuncSig, doc string) {
	next := runtime.FFIRoute{
		Name:     name,
		Bridge:   bridge,
		MethodID: methodID,
		Doc:      doc,
		FuncSig:  funcSig,
	}
	if existing, ok := e.routes[name]; ok {
		ensureCompatibleRoute(name, existing, next)
	}
	e.routes[name] = next
	if funcSig != nil {
		if existing, ok := e.funcSchemas[ast.Ident(name)]; ok && !sameRuntimeFuncSig(existing, funcSig) {
			panic(fmt.Sprintf("ffi schema conflict for %s: existing=%s new=%s", name, existing.Spec, funcSig.Spec))
		}
		e.funcSchemas[ast.Ident(name)] = funcSig
	}
}

func (e *MiniExecutor) registerStructSchemaLocked(name string, spec *runtime.RuntimeStructSpec) {
	if spec != nil {
		if existing, ok := e.structsMeta[ast.Ident(name)]; ok {
			merged, ok := mergeRuntimeStructSpec(existing, spec)
			if !ok {
				panic(fmt.Sprintf("ffi struct schema conflict for %s: existing=%s new=%s", name, existing.Spec, spec.Spec))
			}
			spec = merged
		}
		e.structsMeta[ast.Ident(name)] = spec
		return
	}
	delete(e.structsMeta, ast.Ident(name))
}

func (e *MiniExecutor) mustAddFuncSchemaLocked(name string, sig *runtime.RuntimeFuncSig) {
	if sig == nil {
		panic("invalid builtin function schema: " + name)
	}
	e.funcSchemas[ast.Ident(name)] = sig
}

func (e *MiniExecutor) formatRouteSchema(route runtime.FFIRoute) string {
	spec := ast.GoMiniType("")
	if route.FuncSig != nil {
		spec = route.FuncSig.Spec
	}
	return e.formatSchemaWithDoc(spec, route.Doc, route.FuncSig)
}

func (e *MiniExecutor) formatSchemaWithDoc(spec ast.GoMiniType, doc string, parsed *runtime.RuntimeFuncSig) string {
	sig := string(spec)
	if parsed != nil {
		sig = string(parsed.Spec)
	}
	if doc != "" {
		sig += " // " + strings.ReplaceAll(doc, "\n", " ")
	}
	return sig
}

func routeConflictError(name string, existing, next runtime.FFIRoute) string {
	return fmt.Sprintf(
		"ffi route conflict for %s: existing(method=%d sig=%s bridge=%s) new(method=%d sig=%s bridge=%s)",
		name,
		existing.MethodID,
		runtimeRouteSignature(existing),
		bridgeIdentity(existing.Bridge),
		next.MethodID,
		runtimeRouteSignature(next),
		bridgeIdentity(next.Bridge),
	)
}

func ensureCompatibleRoute(name string, existing, next runtime.FFIRoute) {
	if existing.Name != next.Name ||
		existing.MethodID != next.MethodID ||
		existing.Doc != next.Doc ||
		!sameRuntimeFuncSig(existing.FuncSig, next.FuncSig) ||
		!sameBridge(existing.Bridge, next.Bridge) {
		panic(routeConflictError(name, existing, next))
	}
}

func runtimeRouteSignature(route runtime.FFIRoute) string {
	if route.FuncSig != nil {
		return string(route.FuncSig.Spec)
	}
	return ""
}

func sameRuntimeFuncSig(a, b *runtime.RuntimeFuncSig) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	default:
		return a.Spec == b.Spec
	}
}

func sameRuntimeStructSpec(a, b *runtime.RuntimeStructSpec) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	default:
		return a.TypeID == b.TypeID && a.Spec == b.Spec && a.Name == b.Name
	}
}

func mergeRuntimeStructSpec(existing, next *runtime.RuntimeStructSpec) (*runtime.RuntimeStructSpec, bool) {
	switch {
	case existing == nil || next == nil:
		return next, existing == next
	case sameRuntimeStructSpec(existing, next):
		return existing, true
	case existing.TypeID != next.TypeID || existing.Name != next.Name:
		return nil, false
	}

	existingFields := make(map[string]runtime.RuntimeStructField, len(existing.Fields))
	for _, field := range existing.Fields {
		existingFields[field.Name] = field
	}
	nextFields := make(map[string]runtime.RuntimeStructField, len(next.Fields))
	for _, field := range next.Fields {
		nextFields[field.Name] = field
	}

	for name, field := range existingFields {
		if other, ok := nextFields[name]; ok {
			if field.TypeInfo.Raw != other.TypeInfo.Raw {
				return nil, false
			}
			continue
		}
		if field.TypeInfo.Kind != runtime.RuntimeTypeFunction {
			return nil, false
		}
	}
	for name, field := range nextFields {
		if _, ok := existingFields[name]; ok {
			continue
		}
		if field.TypeInfo.Kind != runtime.RuntimeTypeFunction {
			return nil, false
		}
	}

	if len(next.Fields) >= len(existing.Fields) {
		return next, true
	}
	return existing, true
}

func sameBridge(a, b ffigo.FFIBridge) bool {
	if a == nil || b == nil {
		return a == b
	}
	ta := reflect.TypeOf(a)
	tb := reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	return true
}

func bridgeIdentity(bridge ffigo.FFIBridge) string {
	if bridge == nil {
		return "<nil>"
	}
	v := reflect.ValueOf(bridge)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return fmt.Sprintf("%T@0x%x", bridge, v.Pointer())
	default:
		return fmt.Sprintf("%T:%v", bridge, bridge)
	}
}

func cloneRuntimeFuncSig(sig *runtime.RuntimeFuncSig) *runtime.RuntimeFuncSig {
	if sig == nil {
		return nil
	}
	res := *sig
	res.Function.Params = append([]ast.FunctionParam(nil), sig.Function.Params...)
	res.ParamTypes = append([]runtime.RuntimeType(nil), sig.ParamTypes...)
	return &res
}

func cloneRuntimeStructSpec(spec *runtime.RuntimeStructSpec) *runtime.RuntimeStructSpec {
	if spec == nil {
		return nil
	}
	res := &runtime.RuntimeStructSpec{
		Name:     spec.Name,
		TypeID:   spec.TypeID,
		Spec:     spec.Spec,
		TypeInfo: spec.TypeInfo,
		Fields:   append([]runtime.RuntimeStructField(nil), spec.Fields...),
		ByName:   make(map[string]runtime.RuntimeStructField, len(spec.ByName)),
	}
	for k, v := range spec.ByName {
		res.ByName[k] = v
	}
	return res
}
