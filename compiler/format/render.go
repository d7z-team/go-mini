package format

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/scanner"
	"github.com/d7z-team/mini-go/compiler/token"
)

type braceFrame struct {
	role     braceRole
	indented bool
	empty    bool
}

type formatWriter struct {
	text          strings.Builder
	facts         syntaxFacts
	indent        int
	lineStart     bool
	lineBreaks    int
	pendingSpace  bool
	pendingLines  int
	requiredLines int
	previous      token.Kind
	previousRole  operatorRole
	previousColon colonRole
	braceStack    []braceFrame
}

func render(elements []scanner.Element, facts syntaxFacts) string {
	writer := formatWriter{facts: facts, lineStart: true}
	for index, element := range elements {
		if _, ok := facts.blankBefore[element.Span.Start.Offset]; ok && writer.text.Len() != 0 && writer.requiredLines < 2 {
			writer.requiredLines = 2
		}
		switch element.Kind {
		case scanner.ElementWhitespace:
			writer.pendingSpace = true
		case scanner.ElementNewline:
			writer.pendingLines++
		case scanner.ElementLineComment:
			if !writer.lineStart {
				writer.pendingSpace = true
			}
			writer.flushPending(false)
			writer.write(element.Lexeme)
			writer.newline()
		case scanner.ElementBlockComment:
			writer.flushPending(!writer.lineStart)
			writer.write(element.Lexeme)
			if strings.Contains(element.Lexeme, "\n") {
				writer.lineStart = strings.HasSuffix(element.Lexeme, "\n")
			}
			writer.pendingSpace = true
		case scanner.ElementBOM:
			writer.write(element.Lexeme)
		case scanner.ElementToken:
			next := nextTokenKind(elements, index+1)
			writer.writeToken(element, next, element.Token == token.Lbrace && emptyBrace(elements, index+1),
				lineCommentFollows(elements, index+1, element.Span.End.Line))
		}
	}
	text := strings.TrimRight(writer.text.String(), " \t\r\n")
	if text != "" {
		text += "\n"
	}
	return text
}

func lineCommentFollows(elements []scanner.Element, start, line int) bool {
	for index := start; index < len(elements); index++ {
		element := elements[index]
		if element.Span.Start.Line != line || element.Kind == scanner.ElementNewline {
			return false
		}
		switch element.Kind {
		case scanner.ElementWhitespace:
			continue
		case scanner.ElementLineComment:
			return true
		default:
			return false
		}
	}
	return false
}

func emptyBrace(elements []scanner.Element, start int) bool {
	for index := start; index < len(elements); index++ {
		switch elements[index].Kind {
		case scanner.ElementWhitespace, scanner.ElementNewline:
			continue
		case scanner.ElementToken:
			return elements[index].Token == token.Rbrace
		default:
			return false
		}
	}
	return false
}

func nextTokenKind(elements []scanner.Element, start int) token.Kind {
	for index := start; index < len(elements); index++ {
		if elements[index].Kind == scanner.ElementToken {
			return elements[index].Token
		}
	}
	return token.EOF
}

func (w *formatWriter) writeToken(element scanner.Element, next token.Kind, emptyBrace, inlineComment bool) {
	kind := element.Token
	offset := element.Span.Start.Offset
	if _, ok := w.facts.caseOutdent[offset]; ok && w.indent > 0 {
		w.indent--
	}
	operator := w.facts.operators[offset]
	colon := w.facts.colons[offset]
	groupParen, isGroupParen := w.facts.groupParens[offset]
	if kind == token.Semicolon {
		if _, clause := w.facts.clauseSemicolon[offset]; clause {
			w.pendingLines = 0
			w.requiredLines = 0
			w.flushPending(false)
			w.write(element.Lexeme)
			w.pendingSpace = true
			w.previous = kind
			w.previousRole = operatorDefault
			w.previousColon = colonDefault
			return
		}
		if inlineComment {
			w.pendingSpace = true
		} else {
			w.newline()
		}
		w.previous = kind
		w.previousRole = operatorDefault
		return
	}

	role := w.facts.braces[offset]
	if kind == token.Rbrace && len(w.braceStack) != 0 {
		frame := w.braceStack[len(w.braceStack)-1]
		if frame.empty {
			w.pendingSpace = false
			w.pendingLines = 0
			w.requiredLines = 0
		}
		if frame.indented {
			w.indent--
			if w.indent < 0 {
				w.indent = 0
			}
		}
		if frame.indented && (frame.role == braceBlock || frame.role == braceType) && !w.lineStart {
			w.newline()
		} else if frame.role == braceComposite && w.pendingLines > 0 {
			w.flushPending(false)
		}
		role = frame.role
	}
	if kind == token.Rparen && isGroupParen && !groupParen {
		if w.indent > 0 {
			w.indent--
		}
		if !w.lineStart {
			w.newline()
		}
	}

	forceSpace := tokenNeedsSpace(w.previous, kind, w.previousRole, operator, role, emptyBrace)
	if _, ok := w.facts.spaceBefore[offset]; ok {
		forceSpace = true
	}
	if kind == token.Colon {
		forceSpace = false
		w.pendingSpace = false
	}
	if kind == token.Rparen || kind == token.Rbrack || kind == token.Rbrace || kind == token.Comma || kind == token.Period {
		w.pendingSpace = false
	}
	if w.previousRole == operatorPrefix {
		forceSpace = false
		w.pendingSpace = false
	}
	if w.previousColon == colonTight {
		forceSpace = false
		w.pendingSpace = false
	}
	w.flushPending(forceSpace)
	w.write(element.Lexeme)

	switch kind {
	case token.Lparen:
		if isGroupParen && groupParen {
			w.indent++
			w.newline()
		}
	case token.Lbrace:
		frame := braceFrame{role: role, empty: emptyBrace}
		if !emptyBrace && (role == braceBlock || role == braceType || role == braceComposite) {
			frame.indented = true
			w.indent++
		}
		w.braceStack = append(w.braceStack, frame)
		if !emptyBrace && (role == braceBlock || role == braceType) && !inlineComment {
			w.newline()
		}
	case token.Rbrace:
		if len(w.braceStack) != 0 {
			w.braceStack = w.braceStack[:len(w.braceStack)-1]
		}
		if role == braceBlock {
			if next == token.Else {
				w.pendingSpace = true
			} else if next != token.Lparen && next != token.Lbrack && next != token.Period && next != token.Comma && next != token.Colon && next != token.Rparen && next != token.Rbrack && next != token.Rbrace && next != token.Semicolon {
				w.requiredLines = 1
			}
		}
	case token.Comma:
		w.pendingSpace = true
	case token.Colon:
		switch colon {
		case colonTight:
			w.pendingSpace = false
		case colonClause:
			w.indent++
			if inlineComment {
				w.pendingSpace = true
			} else {
				w.newline()
			}
		case colonLabel:
			if inlineComment {
				w.pendingSpace = true
			} else {
				w.newline()
			}
		default:
			w.pendingSpace = true
		}
	}
	if operator == operatorBinary || operator == operatorChanSend {
		w.pendingSpace = true
	}
	w.previous = kind
	w.previousRole = operator
	w.previousColon = colon
}

func tokenNeedsSpace(left, right token.Kind, leftRole, rightRole operatorRole, brace braceRole, emptyBrace bool) bool {
	if left == "" || left == token.Lparen || left == token.Lbrack || left == token.Period ||
		right == token.Rparen || right == token.Rbrack || right == token.Comma || right == token.Period || right == token.Colon {
		return false
	}
	if rightRole == operatorBinary {
		return true
	}
	if rightRole == operatorPrefix {
		return isWordToken(left)
	}
	if rightRole == operatorChanSend {
		return left != token.Chan
	}
	if leftRole == operatorBinary || leftRole == operatorChanSend {
		return true
	}
	if right == token.Lbrace {
		switch brace {
		case braceComposite:
			return false
		case braceType:
			return !emptyBrace
		case braceBlock:
			return true
		}
	}
	if right == token.Lparen {
		switch left {
		case token.If, token.For, token.Switch, token.Select:
			return true
		default:
			return false
		}
	}
	if isWordToken(left) && isWordToken(right) {
		return true
	}
	return isOperatorToken(left) || isOperatorToken(right)
}

func (w *formatWriter) flushPending(forceSpace bool) {
	lines := w.pendingLines
	if w.requiredLines > lines {
		lines = w.requiredLines
	}
	if lines > 0 {
		if lines > 1 {
			w.ensureLines(2)
		} else {
			w.ensureLines(1)
		}
	} else if (w.pendingSpace || forceSpace) && !w.lineStart {
		w.write(" ")
	}
	w.pendingLines = 0
	w.requiredLines = 0
	w.pendingSpace = false
}

func (w *formatWriter) newline() {
	w.ensureLines(1)
}

func (w *formatWriter) ensureLines(count int) {
	for w.lineBreaks < count {
		w.text.WriteByte('\n')
		w.lineBreaks++
	}
	w.lineStart = true
	w.pendingLines = 0
	w.requiredLines = 0
	w.pendingSpace = false
}

func (w *formatWriter) write(value string) {
	if value == "" {
		return
	}
	if w.lineStart {
		for range w.indent {
			w.text.WriteByte('\t')
		}
		w.lineStart = false
		w.lineBreaks = 0
	}
	w.text.WriteString(value)
	w.lineBreaks = 0
	for index := len(value) - 1; index >= 0 && value[index] == '\n'; index-- {
		w.lineBreaks++
	}
	w.lineStart = w.lineBreaks > 0
}

func isWordToken(kind token.Kind) bool {
	return kind == token.Ident || kind == token.Int || kind == token.Float || kind == token.Imag || kind == token.Char || kind == token.String || token.IsKeyword(kind)
}

func isOperatorToken(kind token.Kind) bool {
	switch kind {
	case token.Add, token.Sub, token.Mul, token.Quo, token.Rem, token.And, token.Or, token.Xor, token.Tilde,
		token.Shl, token.Shr, token.AndNot, token.AddAssign, token.SubAssign, token.MulAssign, token.QuoAssign,
		token.RemAssign, token.AndAssign, token.OrAssign, token.XorAssign, token.ShlAssign, token.ShrAssign,
		token.AndNotAssign, token.Land, token.Lor, token.Arrow, token.Eq, token.Lt, token.Gt, token.Assign,
		token.Not, token.Ne, token.Le, token.Ge, token.Define:
		return true
	default:
		return false
	}
}
