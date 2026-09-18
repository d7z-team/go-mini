package parser

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/scanner"
	"github.com/d7z-team/mini-go/compiler/token"
)

func (p *parser) current() scanner.Token {
	if p.pos >= len(p.tokens) {
		return scanner.Token{Kind: token.EOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) previous() scanner.Token {
	if p.pos == 0 {
		return p.current()
	}
	return p.tokens[p.pos-1]
}

func (p *parser) peek(distance int) scanner.Token {
	index := p.pos + distance
	if index >= len(p.tokens) {
		return scanner.Token{Kind: token.EOF}
	}
	return p.tokens[index]
}

func (p *parser) at(kind token.Kind) bool {
	return p.current().Kind == kind
}

func (p *parser) match(kind token.Kind) bool {
	if !p.at(kind) {
		return false
	}
	p.advance()
	return true
}

func (p *parser) matchIdent(name string) bool {
	if !p.at(token.Ident) || p.current().Lexeme != name {
		return false
	}
	p.advance()
	return true
}

func (p *parser) advance() scanner.Token {
	current := p.current()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return current
}

func (p *parser) expect(kind token.Kind, code, message string) scanner.Token {
	if p.at(kind) {
		return p.advance()
	}
	current := p.current()
	p.errorAtCurrent(code, message)
	if !p.at(token.EOF) && !isRecoveryBoundary(p.current().Kind) {
		p.advance()
	}
	return scanner.Token{Kind: kind, Span: current.Span}
}

func (p *parser) expectIdentToken(code, message string) scanner.Token {
	return p.expect(token.Ident, code, message)
}

func identifier(value scanner.Token) ast.Identifier {
	return ast.Identifier{Text: value.Lexeme, Span: value.Span}
}

func (p *parser) consumeSemicolons() {
	for p.match(token.Semicolon) {
	}
}

func (p *parser) optionalSemicolon() {
	p.match(token.Semicolon)
}

func (p *parser) stopAt(kinds ...token.Kind) bool {
	for _, kind := range kinds {
		if p.at(kind) {
			return true
		}
	}
	return false
}

func containsKind(kinds []token.Kind, want token.Kind) bool {
	for _, kind := range kinds {
		if kind == want {
			return true
		}
	}
	return false
}
