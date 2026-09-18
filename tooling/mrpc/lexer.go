package mrpc

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind uint8

const (
	tokenEOF tokenKind = iota
	tokenIdent
	tokenNumber
	tokenString
	tokenLBrace
	tokenRBrace
	tokenLParen
	tokenRParen
	tokenLBracket
	tokenRBracket
	tokenComma
	tokenSemicolon
	tokenPeriod
	tokenAssign
)

type token struct {
	kind       tokenKind
	text       string
	start, end int
}

func lex(file Source) ([]token, []Diagnostic) {
	text := file.Text
	var tokens []token
	var diagnostics []Diagnostic
	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		if unicode.IsSpace(r) {
			offset += size
			continue
		}
		if strings.HasPrefix(text[offset:], "//") {
			if end := strings.IndexByte(text[offset:], '\n'); end >= 0 {
				offset += end + 1
			} else {
				offset = len(text)
			}
			continue
		}
		if strings.HasPrefix(text[offset:], "/*") {
			end := strings.Index(text[offset+2:], "*/")
			if end < 0 {
				span, _ := file.Span(offset, len(text))
				diagnostics = append(diagnostics, Diagnostic{Code: "mrpc.comment", Severity: SeverityError, Message: "unterminated block comment", Primary: span})
				break
			}
			offset += end + 4
			continue
		}
		start := offset
		if unicode.IsLetter(r) || r == '_' {
			offset += size
			for offset < len(text) {
				r, size = utf8.DecodeRuneInString(text[offset:])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
					break
				}
				offset += size
			}
			tokens = append(tokens, token{kind: tokenIdent, text: text[start:offset], start: start, end: offset})
			continue
		}
		if unicode.IsDigit(r) || r == '-' && offset+1 < len(text) && text[offset+1] >= '0' && text[offset+1] <= '9' {
			offset += size
			for offset < len(text) {
				r, size = utf8.DecodeRuneInString(text[offset:])
				if !unicode.IsDigit(r) {
					break
				}
				offset += size
			}
			tokens = append(tokens, token{kind: tokenNumber, text: text[start:offset], start: start, end: offset})
			continue
		}
		if r == '"' {
			offset += size
			for offset < len(text) {
				r, size = utf8.DecodeRuneInString(text[offset:])
				offset += size
				if r == '\\' && offset < len(text) {
					_, escaped := utf8.DecodeRuneInString(text[offset:])
					offset += escaped
					continue
				}
				if r == '"' {
					break
				}
			}
			tokens = append(tokens, token{kind: tokenString, text: text[start:offset], start: start, end: offset})
			continue
		}
		kind := map[rune]tokenKind{'{': tokenLBrace, '}': tokenRBrace, '(': tokenLParen, ')': tokenRParen, '[': tokenLBracket, ']': tokenRBracket, ',': tokenComma, ';': tokenSemicolon, '.': tokenPeriod, '=': tokenAssign}[r]
		if kind == tokenEOF {
			span, _ := file.Span(start, start+size)
			diagnostics = append(diagnostics, Diagnostic{Code: "mrpc.character", Severity: SeverityError, Message: fmt.Sprintf("unexpected character %q", r), Primary: span})
			offset += size
			continue
		}
		offset += size
		tokens = append(tokens, token{kind: kind, text: text[start:offset], start: start, end: offset})
	}
	tokens = append(tokens, token{kind: tokenEOF, start: len(text), end: len(text)})
	return tokens, diagnostics
}
