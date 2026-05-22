package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type MiniProgram struct {
	Source   string
	Program  *ast.ProgramStmt
	Compiled *compiler.Artifact
	// TemplatePreviews contains source-based call template render previews used by LSP hover.
	TemplatePreviews []calltemplate.TemplatePreview
	executor         *runtime.Executor

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

func (p *MiniProgram) CheckSatisfaction(val *runtime.Var, interfaceType string) (*runtime.Var, error) {
	return p.executor.CheckSatisfaction(val, interfaceType)
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
	return p.GetNodeAtFile("", line, col)
}

// GetNodeAtFile 获取指定文件位置的节点
func (p *MiniProgram) GetNodeAtFile(file string, line, col int) ast.Node {
	return ast.FindNodeAtFile(p.Program, file, line, col)
}

// GetDefinitionAt 获取指定位置符号的定义
func (p *MiniProgram) GetDefinitionAt(line, col int) ast.Node {
	return p.GetDefinitionAtFile("", line, col)
}

// GetDefinitionAtFile 获取指定文件位置符号的定义
func (p *MiniProgram) GetDefinitionAtFile(file string, line, col int) ast.Node {
	node := p.GetNodeAtFile(file, line, col)
	if node == nil {
		return nil
	}
	p.BuildAllCache() // 确保 parentMap 已就绪
	return ast.FindDefinition(p.Program, node, p.parentMap)
}

// GetHoverAt 获取指定位置符号的悬浮信息
func (p *MiniProgram) GetHoverAt(line, col int) *ast.HoverInfo {
	return p.GetHoverAtFile("", line, col)
}

// GetHoverAtFile 获取指定文件位置符号的悬浮信息
func (p *MiniProgram) GetHoverAtFile(file string, line, col int) *ast.HoverInfo {
	for _, preview := range p.TemplatePreviews {
		if preview.Contains(file, line, col) {
			return &ast.HoverInfo{Markdown: preview.Markdown()}
		}
	}
	node := p.GetNodeAtFile(file, line, col)
	if node == nil {
		return nil
	}
	p.BuildAllCache()
	return ast.FindHoverInfo(p.Program, node, p.parentMap)
}

// GetReferencesAt 获取指定位置符号的所有引用
func (p *MiniProgram) GetReferencesAt(line, col int, includeDeclaration bool) []ast.Node {
	return p.GetReferencesAtFile("", line, col, includeDeclaration)
}

// GetReferencesAtFile 获取指定文件位置符号的所有引用
func (p *MiniProgram) GetReferencesAtFile(file string, line, col int, includeDeclaration bool) []ast.Node {
	node := p.GetNodeAtFile(file, line, col)
	if node == nil {
		return nil
	}
	p.BuildAllCache()
	def := ast.FindDefinition(p.Program, node, p.parentMap)
	if def == nil {
		return nil
	}
	return ast.FindAllReferences(p.Program, def, p.parentMap, includeDeclaration)
}

// GetCompletionsAt 获取指定位置的代码补全建议
func (p *MiniProgram) GetCompletionsAt(line, col int) []ast.CompletionItem {
	return p.GetCompletionsAtFile("", line, col)
}

// GetCompletionsAtFile 获取指定文件位置的代码补全建议
func (p *MiniProgram) GetCompletionsAtFile(file string, line, col int) []ast.CompletionItem {
	return ast.FindCompletionsAtFile(p.Program, file, line, col)
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

func (p *MiniProgram) SharedState() *runtime.SharedStateSnapshot {
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

func initEvalReturnSlot(session *runtime.StackContext, expr ast.Expr) error {
	if session == nil {
		return errors.New("missing eval session")
	}
	typ := ast.GoMiniType("Any")
	if expr != nil && expr.GetBase() != nil && !expr.GetBase().Type.IsEmpty() {
		typ = expr.GetBase().Type
	}
	typeInfo, err := runtime.ParseRuntimeType(typ)
	if err != nil {
		typeInfo = runtime.MustParseRuntimeType("Any")
	}
	return session.InitReturn(typeInfo)
}

// Eval 在当前程序的语境下执行单个 Go 表达式
// 这允许你调用程序中定义的函数或访问全局变量
func (p *MiniProgram) Eval(ctx context.Context, exprStr string, env map[string]interface{}) ([]*runtime.Var, error) {
	expr, err := compiler.New(compiler.Config{}).CompileExprSource(exprStr)
	if err != nil {
		return nil, fmt.Errorf("表达式解析失败: %w", err)
	}

	// 创建基于当前程序蓝图的 session
	session := p.executor.NewSession(ctx, "eval")
	defer p.executor.CleanupSession(session)

	if err := p.executor.EnsureSharedStateInitialized(ctx, nil); err != nil {
		return nil, err
	}

	// 注入环境
	for k, v := range env {
		_ = session.AddVariable(k, p.executor.ToVar(session, v, nil))
	}
	if err := initEvalReturnSlot(session, expr); err != nil {
		return nil, err
	}

	tasks, err := compiler.CompileEvalTasks(expr)
	if err != nil {
		return nil, err
	}
	if err := p.executor.ExecuteTasks(session, tasks); err != nil {
		return nil, err
	}
	res, err := session.LoadReturn()
	if err != nil {
		return nil, err
	}
	return unpackEvalResult(expr, res), nil
}

// MustEval 类似于 Eval，但在出错时会触发 panic
func (p *MiniProgram) MustEval(ctx context.Context, exprStr string, env map[string]interface{}) []*runtime.Var {
	res, err := p.Eval(ctx, exprStr, env)
	if err != nil {
		panic(err)
	}
	return res
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
