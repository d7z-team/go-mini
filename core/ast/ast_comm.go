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
	s := string(i)
	if strings.Contains(s, ".") {
		parts := strings.SplitN(s, ".", 2)
		if realPkg, ok := v.root.Imports[parts[0]]; ok {
			return Ident(fmt.Sprintf("%s.%s", realPkg, parts[1]))
		}
		return i // fallback
	}
	// For normal identifiers, we only mangle them if they are top-level Structs or Functions
	// Wait, local variables shouldn't be mangled!
	// So Ident shouldn't blindly mangle here. We will mangle specifically where needed.
	return i
}

func (i Ident) Valid(ctx *ValidContext) bool {
	if strings.TrimSpace(string(i)) == "" {
		ctx.AddErrorf("identifier must not be empty")
		return false
	}
	return true
}

// BaseNode 是所有节点的基类
type BaseNode struct {
	ID      string     `json:"id"`
	Meta    string     `json:"meta"`
	Type    GoMiniType `json:"type,omitempty"`    // 表达式为任何类型，否则为 Void
	Message string     `json:"message,omitempty"` // 表达式带有的附加信息
}

func (b *BaseNode) EnsureID(ctx *ValidContext) {
	if b.ID == "" {
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
