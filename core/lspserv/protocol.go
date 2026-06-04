package lspserv

import (
	"strings"

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
	case "constant":
		return KindConstant
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

// Location 对应 LSP 的 Location
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature,omitempty"`
	ActiveParameter int                    `json:"activeParameter,omitempty"`
}

type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation *MarkupContent         `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

type ParameterInformation struct {
	Label string `json:"label"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type SemanticTokens struct {
	Data []uint32 `json:"data"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

type CodeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
	Edit        *WorkspaceEdit `json:"edit,omitempty"`
}

type DiagnosticRelatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

type Diagnostic struct {
	Range              Range                          `json:"range"`
	Severity           int                            `json:"severity,omitempty"`
	Code               string                         `json:"code,omitempty"`
	Source             string                         `json:"source,omitempty"`
	Message            string                         `json:"message"`
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
}

// FromInternalPos 将 go-mini 的 1-based Position 转换为 LSP 的 0-based Position
func FromInternalPos(p *ast.Position) Range {
	return RangeFromInternalPos("", p)
}

func RangeFromInternalPos(code string, p *ast.Position) Range {
	if p == nil {
		return Range{}
	}
	start := Position{Line: normalizeLine(p.L), Character: normalizeColumn(code, p.L, p.C)}
	endLine := p.EL
	if endLine <= 0 {
		endLine = p.L
	}
	end := Position{Line: normalizeLine(endLine), Character: normalizeColumn(code, endLine, p.EC)}

	// 如果结束位置无效（比如为 0），则退化为开始位置 + 1
	if p.EL <= 0 {
		end.Line = start.Line
		end.Character = start.Character + 1
	}
	if p.EC <= 0 && p.EL > 0 {
		end.Character = start.Character + 1
	}

	// 确保不出现负值
	if end.Line < 0 {
		end.Line = 0
	}
	if end.Character < 0 {
		end.Character = 0
	}

	return Range{
		Start: start,
		End:   end,
	}
}

func normalizeLine(line int) int {
	if line <= 0 {
		return 0
	}
	return line - 1
}

func normalizeColumn(code string, line, col int) int {
	if col <= 0 {
		return 0
	}
	if code == "" {
		return col - 1
	}
	lines := strings.Split(code, "\n")
	lineIndex := line - 1
	if lineIndex < 0 || lineIndex >= len(lines) {
		return col - 1
	}
	return utf16CharacterForByteColumn(lines[lineIndex], col)
}

func utf16CharacterForByteColumn(line string, col int) int {
	byteLimit := col - 1
	if byteLimit <= 0 {
		return 0
	}
	if byteLimit > len(line) {
		byteLimit = len(line)
	}
	count := 0
	for offset, r := range line {
		if offset >= byteLimit {
			break
		}
		if r > 0xffff {
			count += 2
		} else {
			count++
		}
	}
	return count
}
