package ast

import (
	"fmt"
	"strings"
)

// Node 是所有AST节点的接口
type Node interface {
	Validate(ctx *ValidContext) (Node, bool)
	GetBase() *BaseNode
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
