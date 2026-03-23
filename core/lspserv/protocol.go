package lspserv

import (
	"gopkg.d7z.net/go-mini/core/ast"
)

// Position 对应 LSP 的 Position
type Position struct {
	Line      int `json:"line"`      // 0-based
	Character int `json:"character"` // 0-based
}

// Range 对应 LSP 的 Range
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Diagnostic 对应 LSP 的 Diagnostic
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"` // 1: Error, 2: Warning
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// CompletionItem 对应 LSP 的 CompletionItem
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind,omitempty"` // LSP Kind 常量
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	InsertText    string `json:"insertText,omitempty"`
}

// LSP Kind 常量 (参考 LSP 规范)
const (
	KindText          = 1
	KindMethod        = 2
	KindFunction      = 3
	KindConstructor   = 4
	KindField         = 5
	KindVariable      = 6
	KindClass         = 7
	KindInterface     = 8
	KindModule        = 9
	KindProperty      = 10
	KindUnit          = 11
	KindValue         = 12
	KindEnum          = 13
	KindKeyword       = 14
	KindSnippet       = 15
	KindColor         = 16
	KindFile          = 17
	KindReference     = 18
	KindFolder        = 19
	KindEnumMember    = 20
	KindConstant      = 21
	KindStruct        = 22
	KindEvent         = 23
	KindOperator      = 24
	KindTypeParameter = 25
)

// MapKind 将 go-mini 内部 Kind 映射为 LSP Kind
func MapKind(internalKind string) int {
	switch internalKind {
	case "var":
		return KindVariable
	case "func":
		return KindFunction
	case "struct":
		return KindStruct
	case "interface":
		return KindInterface
	case "package":
		return KindModule
	case "keyword":
		return KindKeyword
	case "builtin":
		return KindFunction
	case "field":
		return KindField
	case "method":
		return KindMethod
	default:
		return KindText
	}
}

// Hover 对应 LSP 的 Hover 响应
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"` // "plaintext" 或 "markdown"
	Value string `json:"value"`
}

// Location 对应 LSP 的 Location (用于跳转定义)
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// FromInternalPos 将 go-mini 的 1-based Position 转换为 LSP 的 0-based Position
func FromInternalPos(p *ast.Position) Range {
	if p == nil {
		return Range{}
	}
	return Range{
		Start: Position{Line: p.L - 1, Character: p.C - 1},
		End:   Position{Line: p.EL - 1, Character: p.EC - 1},
	}
}
