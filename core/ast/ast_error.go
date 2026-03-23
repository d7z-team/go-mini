package ast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type MiniCodeError struct {
	Line    int
	Message string
}

func (e *MiniCodeError) Error() string {
	return fmt.Sprintf("在 L%d 出现错误", e.Line)
}

type MiniAstError struct {
	Err  error
	Logs []Logs
	Node Node
}

func (e *MiniAstError) Error() string {
	var decode bytes.Buffer
	// 优先显示 Logs 中的详细语义错误信息，以便测试用例能够匹配到
	if len(e.Logs) > 0 {
		for _, log := range e.Logs {
			loc := ""
			if log.Node != nil && log.Node.GetBase().Loc != nil {
				l := log.Node.GetBase().Loc
				loc = fmt.Sprintf("%d:%d", l.L, l.C)
			}
			if loc != "" {
				fmt.Fprintf(&decode, "[%s] %s; ", loc, log.Message)
			} else {
				fmt.Fprintf(&decode, "%s; ", log.Message)
			}
		}
		return strings.TrimSuffix(decode.String(), "; ")
	}

	if e.Node != nil {
		decode.WriteString("\n\n")
		encoder := json.NewEncoder(&decode)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(e.Node)
	}
	return e.Err.Error() + decode.String()
}

// LSP 容错分析节点 (Fault-Tolerant AST Nodes)

// BadExpr 表示解析失败的表达式，用于 IDE 容错分析
type BadExpr struct {
	BaseNode
	RawText string `json:"raw_text,omitempty"`
}

func (b *BadExpr) exprNode() {}

func (b *BadExpr) Check(ctx *SemanticContext) error {
	ctx.AddErrorf("语法错误：无法解析的表达式")
	b.Type = "Any"
	return nil // 软处理：不中断校验流程
}

func (b *BadExpr) Optimize(ctx *OptimizeContext) Node {
	return b
}

// BadStmt 表示解析失败的语句
type BadStmt struct {
	BaseNode
	RawText string `json:"raw_text,omitempty"`
}

func (b *BadStmt) stmtNode() {}

func (b *BadStmt) Check(ctx *SemanticContext) error {
	ctx.AddErrorf("语法错误：无法解析的语句块")
	return nil // 软处理：不中断校验流程
}

func (b *BadStmt) Optimize(ctx *OptimizeContext) Node {
	return b
}
