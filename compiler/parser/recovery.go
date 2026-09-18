package parser

import (
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/token"
)

func (p *parser) recoverTo(sync map[token.Kind]bool) {
	for !p.at(token.EOF) {
		if sync[p.current().Kind] {
			if p.pos == p.syncPos && p.syncCount < 10 {
				p.syncCount++
				return
			}
			if p.pos > p.syncPos {
				p.syncPos = p.pos
				p.syncCount = 0
				return
			}
		}
		p.advance()
	}
}

func (p *parser) ensureProgress(before int, code string, sync map[token.Kind]bool) {
	if p.pos != before {
		return
	}
	p.errorAtCurrent(code, "parser made no progress")
	if sync != nil {
		p.recoverTo(sync)
	}
	if p.pos == before && !p.at(token.EOF) {
		p.advance()
	}
}

func (p *parser) enterNest(code, message string) bool {
	p.nestLevel++
	if p.nestLevel <= p.limits.MaxNesting {
		return true
	}
	p.errorAtCurrent(code, message)
	p.recoverTo(exprEnd)
	return false
}

func (p *parser) leaveNest() {
	if p.nestLevel > 0 {
		p.nestLevel--
	}
}

func (p *parser) add(code, message string, primary source.Span) {
	if len(p.diagnostics) >= p.limits.MaxDiagnostics {
		if !p.diagLimited {
			p.diagLimited = true
			p.diagnostics = append(p.diagnostics, source.Diagnostic{
				Code: source.DiagnosticTruncated, Severity: source.SeverityError,
				Message: "diagnostic limit reached; additional diagnostics were omitted",
			})
		}
		return
	}
	p.diagnostics = append(p.diagnostics, source.Diagnostic{
		Code:     source.DiagnosticCode(code),
		Severity: "error",
		Message:  message,
		Primary:  primary,
	})
}

func isRecoveryBoundary(kind token.Kind) bool {
	switch kind {
	case token.Comma, token.Colon, token.Semicolon, token.Rparen, token.Rbrack, token.Rbrace, token.EOF:
		return true
	default:
		return false
	}
}

func (p *parser) errorAtCurrent(code, message string) {
	p.add(code, message, p.current().Span)
}
