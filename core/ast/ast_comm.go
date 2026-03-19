package ast

import (
	"fmt"
	"strings"
)

// Node 是所有AST节点的接口
type Node interface {
	// Check 执行全量语义校验，不修改 AST 结构。
	// 无论代码是否可达（如 if false 内部），都必须通过此检查。
	Check(ctx *SemanticContext) error

	// Optimize 执行 AST 优化与语法糖降级（Lowering）。
	// 在 Check 通过后运行，负责常量折叠、死代码消除和高级语法展开。
	Optimize(ctx *OptimizeContext) Node

	GetBase() *BaseNode
}

// SemanticContext 语义检查上下文
type SemanticContext struct {
	ValidContext // 继承原有的 ValidContext 功能
	// 后续可以增加符号表、类型栈等专门用于 Check 的字段
}

func NewSemanticContext(ctx *ValidContext) *SemanticContext {
	return &SemanticContext{ValidContext: *ctx}
}

// OptimizeContext 优化上下文
type OptimizeContext struct {
	// 用于控制优化级别、常量池等
	*ValidContext
}

func NewOptimizeContext(ctx *ValidContext) *OptimizeContext {
	return &OptimizeContext{ValidContext: ctx}
}

// Expr 是表达式节点的接口
type Expr interface {
	Node
	exprNode()
}

// Stmt 是语句节点的接口
type Stmt interface {
	Node
	stmtNode()
}

type Ident string

func (i Ident) Resolve(v *ValidContext) Ident {
	return i
}

func (i Ident) Valid(ctx *ValidContext) bool {
	if strings.TrimSpace(string(i)) == "" {
		ctx.AddErrorf("identifier must not be empty")
		return false
	}
	return true
}

// Position 定义物理源码位置
type Position struct {
	F string `json:"f,omitempty"` // File: 跨文件时使用
	L int    `json:"l"`           // Line: 物理行号 (1-based)
	C int    `json:"c,omitempty"` // Col: 物理列号 (1-based, 可选)
}

// BaseNode 是所有节点的基类
type BaseNode struct {
	ID   string     `json:"id"`             // 确定性 ID: 基于 Loc + Meta 的哈希值
	Meta string     `json:"meta"`           // 反序列化开关: if, call, binary...
	Type GoMiniType `json:"type,omitempty"` // 表达式为任何类型，否则为 Void
	Loc  *Position  `json:"loc,omitempty"`  // 源码位置映射: 仅语句和关键表达式包含
}

func (b *BaseNode) EnsureID(ctx *ValidContext) {
	if b.ID == "" {
		// 如果没有通过 Converter 生成确定性 ID，则回退到序列号 ID
		b.ID = fmt.Sprintf("node_%04d", ctx.NextID())
	}
}

// exprNode 标记表达式节点
func (b *BaseNode) exprNode() {}

// stmtNode 标记语句节点
func (b *BaseNode) stmtNode() {}

func (b *BaseNode) GetBase() *BaseNode {
	return b
}

func (b *BaseNode) Check(ctx *SemanticContext) error {
	// 默认实现：不执行任何操作
	return nil
}

func (b *BaseNode) Optimize(ctx *OptimizeContext) Node {
	// 默认实现：返回自身
	return b
}
