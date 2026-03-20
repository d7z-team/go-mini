package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/debugger"
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
	mu sync.RWMutex

	Loader  func(path string) (*ast.ProgramStmt, error)
	bridges map[uint32]ffigo.FFIBridge
	routes  map[string]runtime.FFIRoute
	specs   map[ast.Ident]ast.GoMiniType // 用于验证的函数签名

	registry *ffigo.HandleRegistry
	modules  map[string]*ast.ProgramStmt // 预加载的模块蓝图
}

type MiniProgram struct {
	Source   string
	Program  *ast.ProgramStmt
	executor *runtime.Executor

	// LSP / Debugger 支撑
	parentMap map[ast.Node]ast.Node
	parentMu  sync.RWMutex // 保护 parentMap 读写（虽然 Program 是只读的，但缓存按需构建）
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

func (p *MiniProgram) SetStepLimit(limit int64) {
	p.executor.StepLimit = limit
}

func (p *MiniProgram) Execute(ctx context.Context) error {
	return p.executor.Execute(ctx)
}

// Eval 在当前程序的语境下执行单个 Go 表达式
// 这允许你调用程序中定义的函数或访问全局变量
func (p *MiniProgram) Eval(ctx context.Context, exprStr string, env map[string]interface{}) (*runtime.Var, error) {
	converter := ffigo.NewGoToASTConverter()
	expr, err := converter.ConvertExprSource(exprStr)
	if err != nil {
		return nil, fmt.Errorf("表达式解析失败: %w", err)
	}

	// 创建基于当前程序蓝图的 session
	session := &runtime.StackContext{
		Context:        ctx,
		Executor:       p.executor,
		Stack:          &runtime.Stack{MemoryPtr: make(map[string]*runtime.Var), Scope: "eval", Depth: 1},
		ModuleCache:    make(map[string]*runtime.Var),
		LoadingModules: make(map[string]bool),
		ActiveHandles:  make([]runtime.HandleRef, 0),
		Debugger:       debugger.GetDebugger(ctx),
	}

	defer func() {
		session.Stack.RunDefers()
		for _, h := range session.ActiveHandles {
			if h.Bridge != nil && h.ID != 0 {
				_ = h.Bridge.DestroyHandle(h.ID)
			}
		}
	}()

	// 注入环境
	_ = session.AddVariable("nil", nil)
	for k, v := range env {
		norm, err := normalizeValue(v)
		if err != nil {
			return nil, fmt.Errorf("环境变量 %s 规范化失败: %w", k, err)
		}
		_ = session.AddVariable(k, p.executor.ToVar(session, norm, nil))
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

func (p *MiniProgram) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.executor.GetProgram())
}

func (p *MiniProgram) MarshalIndentJSON(prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(p.executor.GetProgram(), prefix, indent)
}

func NewMiniExecutor() *MiniExecutor {
	res := &MiniExecutor{
		bridges:  make(map[uint32]ffigo.FFIBridge),
		routes:   make(map[string]runtime.FFIRoute),
		specs:    make(map[ast.Ident]ast.GoMiniType),
		registry: ffigo.NewHandleRegistry(),
		modules:  make(map[string]*ast.ProgramStmt),
	}
	// 默认注册 panic 签名以便通过验证
	res.specs["panic"] = "function(String) Void"
	res.specs["recover"] = "function() Any"
	res.specs["String"] = "function(Any) String"
	res.specs["TypeBytes"] = "function(Any) TypeBytes"
	res.specs["len"] = "function(Any) Int64"
	res.specs["make"] = "function(String, ...Int64) Any"
	res.specs["new"] = "function(String) Any"
	res.specs["append"] = "function(Any, ...Any) Any"
	res.specs["delete"] = "function(Any, Any) Void"
	res.specs["Int64"] = "function(Any) Int64"
	res.specs["Float64"] = "function(Any) Float64"
	res.specs["require"] = "function(String) TypeModule"
	return res
}

func (o *MiniExecutor) SetLoader(loader func(path string) (*ast.ProgramStmt, error)) {
	o.Loader = loader
}

// RegisterModule 注册一个预编译的模块，使得脚本可以通过 import 直接引用
func (o *MiniExecutor) RegisterModule(path string, prog *MiniProgram) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.modules[path] = prog.GetAst()
}

func (o *MiniExecutor) HandleRegistry() *ffigo.HandleRegistry {
	return o.registry
}

// RegisterFFI 注册一个外部函数到特定的 Bridge 和 ID
func (o *MiniExecutor) RegisterFFI(name string, bridge ffigo.FFIBridge, methodID uint32, spec ast.GoMiniType, doc string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	returns := "Void"
	if callFunc, ok := spec.ReadCallFunc(); ok {
		returns = string(callFunc.Returns)
	}

	o.routes[name] = runtime.FFIRoute{Bridge: bridge, MethodID: methodID, Returns: returns, Spec: string(spec), Doc: doc}
	if spec != "" {
		o.specs[ast.Ident(name)] = spec
	}
}

func (o *MiniExecutor) RegisterBridge(methodID uint32, bridge ffigo.FFIBridge, spec ast.GoMiniType) {
	o.mu.Lock()
	defer o.mu.Unlock()
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
	fmtlib.RegisterFmt(o, &fmtlib.FmtHost{}, o.registry)

	// 2. Inject os
	oslib.RegisterOS(o, &oslib.OSHost{}, o.registry)
	oslib.RegisterFileMethods(o, &oslib.FileMethodsHost{}, o.registry)

	// 3. Inject errors
	errorslib.RegisterErrors(o, &errorslib.ErrorsHost{}, o.registry)

	// 4. Inject io
	iolib.RegisterIO(o, &iolib.IOHost{}, o.registry)

	// 5. Inject json
	jsonlib.RegisterJSON(o, &jsonlib.JSONHost{}, o.registry)

	// 6. Inject time
	timelib.RegisterTime(o, &timelib.TimeHost{}, o.registry)
}

// AddFuncSpec 仅用于在验证阶段声明一个合法的外部函数
func (o *MiniExecutor) AddFuncSpec(name string, spec ast.GoMiniType) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.specs[ast.Ident(name)] = spec
}

func (o *MiniExecutor) NewRuntimeByAst(program *ast.ProgramStmt) (*MiniProgram, error) {
	executor, err := runtime.NewExecutor(program)
	if err != nil {
		return nil, err
	}

	// 自动合并 Loader，优先查找已注册模块
	executor.Loader = func(path string) (*ast.ProgramStmt, error) {
		o.mu.RLock()
		defer o.mu.RUnlock()
		if astNode, ok := o.modules[path]; ok {
			return astNode, nil
		}
		if o.Loader != nil {
			return o.Loader(path)
		}
		return nil, fmt.Errorf("module not found: %s", path)
	}

	// Pass routes to executor
	o.mu.RLock()
	defer o.mu.RUnlock()
	for name, route := range o.routes {
		executor.RegisterRoute(name, route)
	}

	return &MiniProgram{
		Source:   "",
		Program:  program,
		executor: executor,
	}, nil
}

// normalizeValue 将复杂的宿主对象（如 struct）规范化为 VM 可直接处理的类型（map/slice/primitives）。
// 该函数位于 engine 边界层，允许使用反射以提供极致的开发便利性。
func normalizeValue(val interface{}) (interface{}, error) {
	if val == nil {
		return nil, nil
	}

	// 检查是否已经是引擎原生变量，若是则直接穿透
	if _, ok := val.(*runtime.Var); ok {
		return val, nil
	}

	v := reflect.ValueOf(val)
	// 处理指针
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint()), nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.String:
		return v.String(), nil
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Slice, reflect.Array:
		// 特殊处理 []byte
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return v.Bytes(), nil
		}
		res := make([]interface{}, v.Len())
		for i := 0; i < v.Len(); i++ {
			var err error
			res[i], err = normalizeValue(v.Index(i).Interface())
			if err != nil {
				return nil, err
			}
		}
		return res, nil
	case reflect.Map:
		res := make(map[string]interface{})
		for _, key := range v.MapKeys() {
			if key.Kind() != reflect.String {
				return nil, fmt.Errorf("不支持非字符串类型的 Map Key: %v", key.Kind())
			}
			var err error
			res[key.String()], err = normalizeValue(v.MapIndex(key).Interface())
			if err != nil {
				return nil, err
			}
		}
		return res, nil
	case reflect.Struct:
		res := make(map[string]interface{})
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			// 仅处理导出的字段
			if field.PkgPath != "" {
				continue
			}
			name := field.Name
			// 优先使用 json 标签作为键名
			if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
				name = strings.Split(tag, ",")[0]
			}
			var err error
			res[name], err = normalizeValue(v.Field(i).Interface())
			if err != nil {
				return nil, err
			}
		}
		return res, nil
	default:
		return nil, fmt.Errorf("不支持将类型 %T 注入脚本环境", val)
	}
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
	// 在验证阶段也要支持已注册模块
	validator.SetLoader(func(path string) (*ast.ProgramStmt, error) {
		o.mu.RLock()
		defer o.mu.RUnlock()
		if astNode, ok := o.modules[path]; ok {
			return astNode, nil
		}
		if o.Loader != nil {
			return o.Loader(path)
		}
		return nil, fmt.Errorf("module not found: %s", path)
	})

	o.mu.RLock()
	for name, spec := range o.specs {
		validator.AddVariable(name, spec)
	}
	o.mu.RUnlock()

	semanticCtx := ast.NewSemanticContext(validator)
	err = program.Check(semanticCtx)
	if err != nil {
		return nil, &ast.MiniAstError{Err: err, Logs: validator.Logs(), Node: program}
	}

	optimizeCtx := ast.NewOptimizeContext(validator)
	program.Optimize(optimizeCtx)

	res, err := o.NewRuntimeByAst(program)
	if err != nil {
		return nil, err
	}
	res.Source = code
	return res, nil
}

// Eval 执行单个 Go 表达式字符串
func (o *MiniExecutor) Eval(ctx context.Context, exprStr string, env map[string]interface{}) (*runtime.Var, error) {
	converter := ffigo.NewGoToASTConverter()
	expr, err := converter.ConvertExprSource(exprStr)
	if err != nil {
		return nil, fmt.Errorf("表达式解析失败: %w", err)
	}

	// 创建最小化的无状态执行器
	executor, _ := runtime.NewExecutor(&ast.ProgramStmt{
		BaseNode: ast.BaseNode{ID: "eval", Meta: "boot"},
	})

	// 继承模块查找逻辑
	executor.Loader = func(path string) (*ast.ProgramStmt, error) {
		o.mu.RLock()
		defer o.mu.RUnlock()
		if astNode, ok := o.modules[path]; ok {
			return astNode, nil
		}
		if o.Loader != nil {
			return o.Loader(path)
		}
		return nil, fmt.Errorf("module not found: %s", path)
	}

	o.mu.RLock()
	for name, route := range o.routes {
		executor.RegisterRoute(name, route)
	}
	o.mu.RUnlock()

	session := &runtime.StackContext{
		Context:        ctx,
		Executor:       executor,
		Stack:          &runtime.Stack{MemoryPtr: make(map[string]*runtime.Var), Scope: "eval", Depth: 1},
		ModuleCache:    make(map[string]*runtime.Var),
		LoadingModules: make(map[string]bool),
		ActiveHandles:  make([]runtime.HandleRef, 0),
		Debugger:       debugger.GetDebugger(ctx),
	}

	defer func() {
		session.Stack.RunDefers()
		for _, h := range session.ActiveHandles {
			if h.Bridge != nil && h.ID != 0 {
				_ = h.Bridge.DestroyHandle(h.ID)
			}
		}
	}()

	// 注入环境
	_ = session.AddVariable("nil", nil)
	for k, v := range env {
		norm, err := normalizeValue(v)
		if err != nil {
			return nil, fmt.Errorf("环境变量 %s 规范化失败: %w", k, err)
		}
		_ = session.AddVariable(k, executor.ToVar(session, norm, nil))
	}

	return executor.ExecExpr(session, expr)
}

// MustEval 类似于 Eval，但在出错时会触发 panic
func (o *MiniExecutor) MustEval(ctx context.Context, exprStr string, env map[string]interface{}) *runtime.Var {
	res, err := o.Eval(ctx, exprStr, env)
	if err != nil {
		panic(err)
	}
	return res
}

// Execute 执行脚本代码片段（无需 package 声明），支持注入环境变量。
// 注意：本方法使用“单次快照模式”，每次调用均创建全新的执行器上下文。
// 若需持久化的全局变量或复杂的跨模块交互，建议使用 NewRuntimeByGoCode。
func (o *MiniExecutor) Execute(ctx context.Context, code string, env map[string]interface{}) error {
	converter := ffigo.NewGoToASTConverter()
	stmts, err := converter.ConvertStmtsSource(code)
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
	o.mu.RLock()
	for _, s := range o.modules {
		for name, sDef := range s.Structs {
			program.Structs[name] = sDef
		}
		for name, cDef := range s.Constants {
			program.Constants[name] = cDef
		}
	}
	o.mu.RUnlock()

	// 语义校验
	v, err := ast.NewValidator(program)
	if err != nil {
		return err
	}
	// 注入 FFI 和内建函数规格
	o.mu.RLock()
	for name, spec := range o.specs {
		v.AddVariable(name, spec)
	}
	o.mu.RUnlock()

	if err := program.Check(ast.NewSemanticContext(v)); err != nil {
		return err
	}

	executor, _ := runtime.NewExecutor(program)

	executor.Loader = func(path string) (*ast.ProgramStmt, error) {
		o.mu.RLock()
		defer o.mu.RUnlock()
		if astNode, ok := o.modules[path]; ok {
			return astNode, nil
		}
		if o.Loader != nil {
			return o.Loader(path)
		}
		return nil, fmt.Errorf("module not found: %s", path)
	}

	o.mu.RLock()
	for name, route := range o.routes {
		executor.RegisterRoute(name, route)
	}
	o.mu.RUnlock()

	session := &runtime.StackContext{
		Context:        ctx,
		Executor:       executor,
		Stack:          &runtime.Stack{MemoryPtr: make(map[string]*runtime.Var), Scope: "snippet", Depth: 1},
		ModuleCache:    make(map[string]*runtime.Var),
		LoadingModules: make(map[string]bool),
		ActiveHandles:  make([]runtime.HandleRef, 0),
		Debugger:       debugger.GetDebugger(ctx),
	}

	defer func() {
		session.Stack.RunDefers()
		for _, h := range session.ActiveHandles {
			if h.Bridge != nil && h.ID != 0 {
				_ = h.Bridge.DestroyHandle(h.ID)
			}
		}
	}()

	// 注入环境
	_ = session.AddVariable("nil", nil)
	for k, v := range env {
		norm, err := normalizeValue(v)
		if err != nil {
			return fmt.Errorf("环境变量 %s 规范化失败: %w", k, err)
		}
		_ = session.AddVariable(k, executor.ToVar(session, norm, nil))
	}

	return executor.ExecuteStmts(session, stmts)
}

// MustExecute 类似于 Execute，但在出错时会触发 panic
func (o *MiniExecutor) MustExecute(ctx context.Context, code string, env map[string]interface{}) {
	err := o.Execute(ctx, code, env)
	if err != nil {
		panic(err)
	}
}

// Import 手动加载一个模块并返回其导出的成员对象
func (o *MiniExecutor) Import(ctx context.Context, path string) (*runtime.Var, error) {
	// 创建一个最简的执行器环境来执行加载
	executor, _ := runtime.NewExecutor(&ast.ProgramStmt{
		BaseNode: ast.BaseNode{ID: "import_loader", Meta: "boot"},
	})

	executor.Loader = func(path string) (*ast.ProgramStmt, error) {
		o.mu.RLock()
		defer o.mu.RUnlock()
		if astNode, ok := o.modules[path]; ok {
			return astNode, nil
		}
		if o.Loader != nil {
			return o.Loader(path)
		}
		return nil, fmt.Errorf("module not found: %s", path)
	}

	o.mu.RLock()
	for name, route := range o.routes {
		executor.RegisterRoute(name, route)
	}
	o.mu.RUnlock()

	session := &runtime.StackContext{
		Context:        ctx,
		Executor:       executor,
		Stack:          &runtime.Stack{MemoryPtr: make(map[string]*runtime.Var), Scope: "loader", Depth: 1},
		ModuleCache:    make(map[string]*runtime.Var),
		LoadingModules: make(map[string]bool),
		ActiveHandles:  make([]runtime.HandleRef, 0),
		Debugger:       debugger.GetDebugger(ctx),
	}

	defer func() {
		session.Stack.RunDefers()
		for _, h := range session.ActiveHandles {
			if h.Bridge != nil && h.ID != 0 {
				_ = h.Bridge.DestroyHandle(h.ID)
			}
		}
	}()

	return executor.ImportModule(session, &ast.ImportExpr{Path: path})
}

// NewRuntimeByJSON 从序列化后的 JSON AST 数据加载并构建执行环境
func (o *MiniExecutor) NewRuntimeByJSON(data []byte) (*MiniProgram, error) {
	node, err := Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("JSON 反序列化失败: %w", err)
	}

	program, logs, err := ValidateAndOptimizeWithLoader(node, o.Loader, func(v *ast.ValidContext) error {
		o.mu.RLock()
		defer o.mu.RUnlock()
		for name, spec := range o.specs {
			v.AddVariable(name, spec)
		}
		return nil
	})
	if err != nil {
		return nil, &ast.MiniAstError{Err: err, Logs: logs, Node: program}
	}

	return o.NewRuntimeByAst(program)
}

// ----------------------------------------------------------------------------
// LSP Metadata Export
// ----------------------------------------------------------------------------

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
func (o *MiniExecutor) ExportMetadata() string {
	o.mu.RLock()
	defer o.mu.RUnlock()

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
	for name, spec := range o.specs {
		sName := string(name)
		if !strings.Contains(sName, ".") && !strings.HasPrefix(sName, "__") {
			meta.Builtins[sName] = string(spec)
		}
	}

	// 2. 处理 FFI Routes 和 Methods
	for routeName, route := range o.routes {
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
	for modName, prog := range o.modules {
		mod := getModule(modName)
		// 导出函数
		for fnName, fnStmt := range prog.Functions {
			if len(fnName) > 0 && fnName[0] >= 'A' && fnName[0] <= 'Z' {
				// 我们需要一种方式导出函数文档，目前 ExportedModule.Functions 只是 map[string]string (name -> sig)
				// 临时将文档附加到签名后面，或者未来重构 ExportedModule.Functions
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
