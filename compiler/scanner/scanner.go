// Package scanner tokenizes Mini-Go source while preserving source positions.
package scanner

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/token"
)

const byteOrderMark = '\uFEFF'

const (
	DefaultMaxSourceBytes = 16 << 20
	DefaultMaxTokens      = 1_000_000
	DefaultMaxDiagnostics = 100
)

type Limits struct {
	MaxSourceBytes int
	MaxTokens      int
	MaxDiagnostics int
}

type Token struct {
	Kind    token.Kind  `json:"kind"`
	Lexeme  string      `json:"lexeme,omitempty"`
	Literal string      `json:"literal,omitempty"`
	Span    source.Span `json:"span"`
}

type ElementKind string

const (
	ElementToken        ElementKind = "token"
	ElementWhitespace   ElementKind = "whitespace"
	ElementNewline      ElementKind = "newline"
	ElementLineComment  ElementKind = "line_comment"
	ElementBlockComment ElementKind = "block_comment"
	ElementBOM          ElementKind = "bom"
)

type Element struct {
	Kind   ElementKind `json:"kind"`
	Token  token.Kind  `json:"token,omitempty"`
	Lexeme string      `json:"lexeme,omitempty"`
	Span   source.Span `json:"span"`
}

type Result struct {
	File        source.File         `json:"file"`
	Elements    []Element           `json:"elements,omitempty"`
	Tokens      []Token             `json:"tokens,omitempty"`
	Diagnostics []source.Diagnostic `json:"diagnostics,omitempty"`
}

func Scan(path, text string) Result {
	return ScanFile(source.NewFile("file.0", path, text))
}

func ScanFile(file source.File) Result {
	return ScanFileWithLimits(file, Limits{})
}

func ScanFileWithLimits(file source.File, limits Limits) Result {
	return scanFileWithLimits(file, limits, false)
}

// ScanHeaderFileWithLimits scans through the import prefix and stops at the
// first top-level declaration.
func ScanHeaderFileWithLimits(file source.File, limits Limits) Result {
	return scanFileWithLimits(file, limits, true)
}

func scanFileWithLimits(file source.File, limits Limits, headerOnly bool) Result {
	limits = normalizeLimits(limits)
	file = source.InitializeFile(file)
	if len(file.Text) > limits.MaxSourceBytes {
		span, _ := file.Span(0, len(file.Text))
		return Result{File: file, Diagnostics: []source.Diagnostic{{
			Code: "scanner.source.limit", Severity: source.SeverityError,
			Message: "source file exceeds scanner byte limit", Primary: span,
		}}}
	}
	s := scanner{file: file, limits: limits, headerOnly: headerOnly}
	s.scan()
	return Result{
		File:        file,
		Elements:    s.elements,
		Tokens:      s.tokens,
		Diagnostics: s.diagnostics,
	}
}

type scanner struct {
	file         source.File
	offset       int
	insertSemi   bool
	elements     []Element
	tokens       []Token
	diagnostics  []source.Diagnostic
	limits       Limits
	stopped      bool
	diagLimited  bool
	tokenLimit   bool
	headerOnly   bool
	headerState  int
	headerDepth  int
	spanStart    int
	spanEnd      int
	spanValue    source.Span
	spanCached   bool
	positionAt   int
	positionLine int
}

func (s *scanner) scan() {
	s.scanSourceTextDiagnostics()
	if strings.HasPrefix(s.file.Text, string(byteOrderMark)) {
		s.emitTrivia(ElementBOM, 0, len(string(byteOrderMark)))
		s.offset = len(string(byteOrderMark))
	}
	for !s.stopped {
		if s.offset >= len(s.file.Text) {
			break
		}
		if s.skipWhitespaceAndComments() {
			continue
		}
		if s.offset >= len(s.file.Text) {
			break
		}
		start := s.offset
		r, size := s.peekRune()
		if r == utf8.RuneError && size == 1 {
			s.offset++
			s.addDiagnostic("scanner.utf8.invalid", "invalid UTF-8 encoding", start, s.offset)
			s.emitToken(token.Illegal, s.file.Text[start:s.offset], start, s.offset)
			continue
		}
		if r == 0 || r == byteOrderMark {
			s.offset += size
			s.emitToken(token.Illegal, s.file.Text[start:s.offset], start, s.offset)
			continue
		}
		if token.IsIdentifierStart(r) {
			s.scanIdent(start)
			continue
		}
		if token.IsDecimalDigit(r) || (r == '.' && s.hasDigitAfterDot()) {
			s.scanNumber(start)
			continue
		}
		switch r {
		case '"':
			s.scanInterpretedString(start)
		case '`':
			s.scanRawString(start)
		case '\'':
			s.scanRune(start)
		default:
			s.scanOperatorOrDelimiter(start)
		}
	}
	if s.insertSemi && !s.stopped {
		s.emitSynthetic(token.Semicolon, "\n", s.offset, s.offset)
		s.insertSemi = false
	}
	s.emitSynthetic(token.EOF, "", s.offset, s.offset)
}

func (s *scanner) scanSourceTextDiagnostics() {
	for start := 0; start < len(s.file.Text); {
		offset := strings.IndexByte(s.file.Text[start:], 0)
		if offset < 0 {
			break
		}
		offset += start
		s.addDiagnostic("scanner.nul.invalid", "NUL character is not allowed in source text", offset, offset+1)
		start = offset + 1
	}
	bom := string(byteOrderMark)
	for start := 1; start < len(s.file.Text); {
		offset := strings.Index(s.file.Text[start:], bom)
		if offset < 0 {
			break
		}
		offset += start
		s.addDiagnostic("scanner.bom.invalid", "byte order mark is only allowed at the start of source text", offset, offset+len(bom))
		start = offset + len(bom)
	}
}

func (s *scanner) skipWhitespaceAndComments() bool {
	for s.offset < len(s.file.Text) {
		switch s.file.Text[s.offset] {
		case ' ', '\t', '\r':
			start := s.offset
			for s.offset < len(s.file.Text) {
				current := s.file.Text[s.offset]
				if current != ' ' && current != '\t' && current != '\r' {
					break
				}
				s.offset++
			}
			s.emitTrivia(ElementWhitespace, start, s.offset)
			continue
		case '\n':
			pos := s.offset
			s.offset++
			s.emitTrivia(ElementNewline, pos, s.offset)
			if s.insertSemi {
				s.emitSynthetic(token.Semicolon, "\n", pos, pos+1)
				s.insertSemi = false
				return true
			}
			continue
		case '/':
			if s.matchAt("//") {
				start := s.offset
				s.offset += 2
				for s.offset < len(s.file.Text) && s.file.Text[s.offset] != '\n' {
					s.offset++
				}
				s.emitTrivia(ElementLineComment, start, s.offset)
				continue
			}
			if s.matchAt("/*") {
				start := s.offset
				newline := -1
				insertSemi := s.insertSemi
				s.offset += 2
				for s.offset < len(s.file.Text) && !s.matchAt("*/") {
					if s.file.Text[s.offset] == '\n' && newline < 0 {
						newline = s.offset
					}
					s.offset++
				}
				if s.offset >= len(s.file.Text) {
					s.emitTrivia(ElementBlockComment, start, s.offset)
					s.addDiagnostic("scanner.comment.unterminated", "unterminated block comment", start, len(s.file.Text))
					return false
				}
				s.offset += 2
				s.emitTrivia(ElementBlockComment, start, s.offset)
				if newline >= 0 && insertSemi {
					s.emitSynthetic(token.Semicolon, "\n", newline, newline+1)
				}
				continue
			}
			return false
		default:
			return false
		}
	}
	return false
}

func (s *scanner) scanIdent(start int) {
	for s.offset < len(s.file.Text) {
		r, size := s.peekRune()
		if r == utf8.RuneError && size == 1 {
			break
		}
		if !token.IsIdentifierPart(r) {
			break
		}
		s.offset += size
	}
	lexeme := s.file.Text[start:s.offset]
	s.emitToken(token.Lookup(lexeme), lexeme, start, s.offset)
}

func (s *scanner) scanOperatorOrDelimiter(start int) {
	candidates := []struct {
		text string
		kind token.Kind
	}{
		{"&^=", token.AndNotAssign},
		{"<<=", token.ShlAssign},
		{">>=", token.ShrAssign},
		{"...", token.Ellipsis},
		{":=", token.Define},
		{"<-", token.Arrow},
		{"++", token.Inc},
		{"--", token.Dec},
		{"&&", token.Land},
		{"||", token.Lor},
		{"==", token.Eq},
		{"!=", token.Ne},
		{"<=", token.Le},
		{">=", token.Ge},
		{"<<", token.Shl},
		{">>", token.Shr},
		{"+=", token.AddAssign},
		{"-=", token.SubAssign},
		{"*=", token.MulAssign},
		{"/=", token.QuoAssign},
		{"%=", token.RemAssign},
		{"&=", token.AndAssign},
		{"|=", token.OrAssign},
		{"^=", token.XorAssign},
		{"&^", token.AndNot},
	}
	for _, candidate := range candidates {
		if s.matchAt(candidate.text) {
			s.offset += len(candidate.text)
			s.emitToken(candidate.kind, candidate.text, start, s.offset)
			return
		}
	}
	if kind, ok := singleCharToken(s.file.Text[start]); ok {
		s.offset++
		s.emitToken(kind, s.file.Text[start:s.offset], start, s.offset)
		return
	}
	_, size := s.peekRune()
	s.offset += size
	s.addDiagnostic("scanner.token.illegal", fmt.Sprintf("illegal character %q", s.file.Text[start:s.offset]), start, s.offset)
	s.emitToken(token.Illegal, s.file.Text[start:s.offset], start, s.offset)
}

func singleCharToken(ch byte) (token.Kind, bool) {
	switch ch {
	case '+':
		return token.Add, true
	case '-':
		return token.Sub, true
	case '*':
		return token.Mul, true
	case '/':
		return token.Quo, true
	case '%':
		return token.Rem, true
	case '&':
		return token.And, true
	case '|':
		return token.Or, true
	case '^':
		return token.Xor, true
	case '~':
		return token.Tilde, true
	case '<':
		return token.Lt, true
	case '>':
		return token.Gt, true
	case '=':
		return token.Assign, true
	case '!':
		return token.Not, true
	case '(':
		return token.Lparen, true
	case '[':
		return token.Lbrack, true
	case '{':
		return token.Lbrace, true
	case ',':
		return token.Comma, true
	case '.':
		return token.Period, true
	case ';':
		return token.Semicolon, true
	case ':':
		return token.Colon, true
	case ')':
		return token.Rparen, true
	case ']':
		return token.Rbrack, true
	case '}':
		return token.Rbrace, true
	default:
		return token.Illegal, false
	}
}

func (s *scanner) emitToken(kind token.Kind, lexeme string, start, end int) {
	s.emitElement(ElementToken, kind, lexeme, start, end)
	s.emitSynthetic(kind, lexeme, start, end)
}

func (s *scanner) emitSynthetic(kind token.Kind, lexeme string, start, end int) {
	if kind != token.EOF && len(s.tokens) >= s.limits.MaxTokens {
		if !s.tokenLimit {
			s.tokenLimit = true
			s.addDiagnostic("scanner.token.limit", "source file exceeds scanner token limit", start, end)
		}
		s.stopped = true
		return
	}
	span, ok := s.span(start, end)
	if !ok {
		span = source.Span{}
	}
	s.tokens = append(s.tokens, Token{
		Kind:   kind,
		Lexeme: lexeme,
		Span:   span,
	})
	s.advanceHeader(kind)
	if kind != token.Comment && kind != token.EOF && kind != token.Semicolon {
		s.insertSemi = token.CanEndStatement(kind)
		return
	}
	if kind == token.Semicolon {
		s.insertSemi = false
	}
}

func (s *scanner) emitTrivia(kind ElementKind, start, end int) {
	s.emitElement(kind, "", s.file.Text[start:end], start, end)
}

func (s *scanner) emitElement(kind ElementKind, tokenKind token.Kind, lexeme string, start, end int) {
	span, ok := s.span(start, end)
	if !ok {
		span = source.Span{}
	}
	s.elements = append(s.elements, Element{Kind: kind, Token: tokenKind, Lexeme: lexeme, Span: span})
}

func (s *scanner) addDiagnostic(code, message string, start, end int) {
	if len(s.diagnostics) >= s.limits.MaxDiagnostics {
		if !s.diagLimited {
			s.diagLimited = true
			s.diagnostics = append(s.diagnostics, source.Diagnostic{
				Code: source.DiagnosticTruncated, Severity: source.SeverityError,
				Message: "diagnostic limit reached; additional diagnostics were omitted",
			})
		}
		return
	}
	if end < start {
		end = start
	}
	if end > len(s.file.Text) {
		end = len(s.file.Text)
	}
	span, ok := s.span(start, end)
	if !ok {
		span = source.Span{}
	}
	s.diagnostics = append(s.diagnostics, source.Diagnostic{
		Code:     source.DiagnosticCode(code),
		Severity: source.SeverityError,
		Message:  message,
		Primary:  span,
	})
}

func (s *scanner) advanceHeader(kind token.Kind) {
	if !s.headerOnly || kind == token.EOF {
		return
	}
	switch s.headerState {
	case 0:
		if kind == token.Package {
			s.headerState = 1
		}
	case 1:
		if kind == token.Ident {
			s.headerState = 2
		}
	case 2:
		if kind == token.Semicolon {
			s.headerState = 3
		}
	case 3:
		if kind == token.Semicolon {
			return
		}
		if kind == token.Import {
			s.headerState = 4
			return
		}
		s.stopped = true
	case 4:
		switch kind {
		case token.Lparen:
			s.headerDepth++
		case token.Rparen:
			if s.headerDepth > 0 {
				s.headerDepth--
			}
		case token.Semicolon:
			if s.headerDepth == 0 {
				s.headerState = 3
			}
		}
	}
}

func (s *scanner) span(start, end int) (source.Span, bool) {
	if s.spanCached && start == s.spanStart && end == s.spanEnd {
		return s.spanValue, true
	}
	if start < 0 || end < start || end > len(s.file.Text) {
		return source.Span{}, false
	}
	startPosition, ok := s.position(start)
	if !ok {
		return source.Span{}, false
	}
	endPosition, ok := s.position(end)
	if !ok {
		return source.Span{}, false
	}
	span := source.Span{Start: startPosition, End: endPosition}
	s.spanStart, s.spanEnd, s.spanValue, s.spanCached = start, end, span, true
	return span, true
}

func (s *scanner) position(offset int) (source.Position, bool) {
	if offset < 0 || offset > len(s.file.Text) || s.file.Path == "" || len(s.file.LineStarts) == 0 {
		return source.Position{}, false
	}
	if offset < s.positionAt {
		s.positionLine = 0
	}
	for s.positionLine+1 < len(s.file.LineStarts) && s.file.LineStarts[s.positionLine+1] <= offset {
		s.positionLine++
	}
	s.positionAt = offset
	return source.Position{
		File: s.file.Path, Offset: offset, Line: s.positionLine + 1,
		Column: offset - s.file.LineStarts[s.positionLine],
	}, true
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxSourceBytes <= 0 || limits.MaxSourceBytes > DefaultMaxSourceBytes {
		limits.MaxSourceBytes = DefaultMaxSourceBytes
	}
	if limits.MaxTokens <= 0 || limits.MaxTokens > DefaultMaxTokens {
		limits.MaxTokens = DefaultMaxTokens
	}
	if limits.MaxDiagnostics <= 0 || limits.MaxDiagnostics > DefaultMaxDiagnostics {
		limits.MaxDiagnostics = DefaultMaxDiagnostics
	}
	return limits
}

func (s *scanner) matchAt(text string) bool {
	return strings.HasPrefix(s.file.Text[s.offset:], text)
}

func (s *scanner) hasDigitAfterDot() bool {
	return s.offset+1 < len(s.file.Text) && s.file.Text[s.offset] == '.' && token.IsDecimalDigit(rune(s.file.Text[s.offset+1]))
}

func (s *scanner) peekByte() byte {
	if s.offset >= len(s.file.Text) {
		return 0
	}
	return s.file.Text[s.offset]
}

func (s *scanner) peekRune() (rune, int) {
	if s.offset >= len(s.file.Text) {
		return 0, 0
	}
	return utf8.DecodeRuneInString(s.file.Text[s.offset:])
}

func (s *scanner) peekRuneValue() rune {
	r, _ := s.peekRune()
	return r
}
