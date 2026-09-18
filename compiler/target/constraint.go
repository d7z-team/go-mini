package target

import (
	"fmt"
	"strings"
)

// ConstraintError describes an invalid build expression at a source byte offset.
type ConstraintError struct {
	Offset  int
	Message string
}

func (e ConstraintError) Error() string { return e.Message }

type expression interface {
	eval(Target) bool
}

type tagExpr string

func (e tagExpr) eval(target Target) bool { return target.Has(string(e)) }

type notExpr struct{ value expression }

func (e notExpr) eval(target Target) bool { return !e.value.eval(target) }

type binaryExpr struct {
	left        expression
	right       expression
	disjunction bool
}

func (e binaryExpr) eval(target Target) bool {
	if e.disjunction {
		return e.left.eval(target) || e.right.eval(target)
	}
	return e.left.eval(target) && e.right.eval(target)
}

const (
	tokenInvalid = iota
	tokenEOF
	tokenTag
	tokenNot
	tokenAnd
	tokenOr
	tokenLeft
	tokenRight
)

type constraintToken struct {
	kind   int
	text   string
	offset int
}

type constraintParser struct {
	source string
	base   int
	offset int
	token  constraintToken
}

// MatchSource reports whether the leading //go:build expression selects source.
func MatchSource(source string, buildTarget Target) (bool, error) {
	buildTarget, err := Normalize(buildTarget)
	if err != nil {
		return false, err
	}
	expr, found, err := sourceConstraint(source)
	if err != nil || !found {
		return !found && err == nil, err
	}
	parser := constraintParser{source: expr.text, base: expr.offset}
	parser.next()
	parsed, err := parser.parseOr()
	if err != nil {
		return false, err
	}
	if parser.token.kind != tokenEOF {
		return false, parser.error("unexpected " + fmt.Sprintf("%q", parser.token.text))
	}
	return parsed.eval(buildTarget), nil
}

type sourceExpression struct {
	text   string
	offset int
}

func sourceConstraint(source string) (sourceExpression, bool, error) {
	offset := 0
	found := sourceExpression{}
	inBlockComment := false
	for offset < len(source) {
		end := strings.IndexByte(source[offset:], '\n')
		if end < 0 {
			end = len(source) - offset
		}
		line := source[offset : offset+end]
		cursor := 0
		for {
			for cursor < len(line) && (line[cursor] == ' ' || line[cursor] == '\t' || line[cursor] == '\r') {
				cursor++
			}
			if inBlockComment {
				closeOffset := strings.Index(line[cursor:], "*/")
				if closeOffset < 0 {
					break
				}
				cursor += closeOffset + 2
				inBlockComment = false
				continue
			}
			if cursor == len(line) {
				break
			}
			rest := line[cursor:]
			if strings.HasPrefix(rest, "/*") {
				inBlockComment = true
				cursor += 2
				continue
			}
			if strings.HasPrefix(rest, "//") {
				if rest == "//go:build" || strings.HasPrefix(rest, "//go:build ") || strings.HasPrefix(rest, "//go:build\t") {
					text := strings.TrimSpace(rest[len("//go:build"):])
					if found.text != "" {
						return sourceExpression{}, false, ConstraintError{Offset: offset + cursor, Message: "multiple //go:build directives"}
					}
					if text == "" {
						return sourceExpression{}, false, ConstraintError{Offset: offset + cursor, Message: "empty //go:build expression"}
					}
					found = sourceExpression{text: text, offset: offset + strings.Index(line, text)}
				}
				break
			}
			return found, found.text != "", nil
		}
		offset += end
		if offset < len(source) && source[offset] == '\n' {
			offset++
		}
	}
	return found, found.text != "", nil
}

func (p *constraintParser) parseOr() (expression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.token.kind == tokenOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{left: left, right: right, disjunction: true}
	}
	return left, nil
}

func (p *constraintParser) parseAnd() (expression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.token.kind == tokenAnd {
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = binaryExpr{left: left, right: right}
	}
	return left, nil
}

func (p *constraintParser) parseUnary() (expression, error) {
	if p.token.kind == tokenNot {
		p.next()
		value, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return notExpr{value: value}, nil
	}
	if p.token.kind == tokenTag {
		value := tagExpr(p.token.text)
		p.next()
		return value, nil
	}
	if p.token.kind == tokenLeft {
		p.next()
		value, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.token.kind != tokenRight {
			return nil, p.error("missing closing parenthesis")
		}
		p.next()
		return value, nil
	}
	if p.token.kind == tokenEOF {
		return nil, p.error("expected build tag expression")
	}
	return nil, p.error("expected build tag, !, or (")
}

func (p *constraintParser) next() {
	for p.offset < len(p.source) && (p.source[p.offset] == ' ' || p.source[p.offset] == '\t') {
		p.offset++
	}
	start := p.offset
	if start >= len(p.source) {
		p.token = constraintToken{kind: tokenEOF, offset: start}
		return
	}
	switch p.source[start] {
	case '!':
		p.offset++
		p.token = constraintToken{kind: tokenNot, text: "!", offset: start}
	case '(':
		p.offset++
		p.token = constraintToken{kind: tokenLeft, text: "(", offset: start}
	case ')':
		p.offset++
		p.token = constraintToken{kind: tokenRight, text: ")", offset: start}
	case '&':
		if start+1 < len(p.source) && p.source[start+1] == '&' {
			p.offset += 2
			p.token = constraintToken{kind: tokenAnd, text: "&&", offset: start}
			return
		}
		p.offset++
		p.token = constraintToken{text: "&", offset: start}
	case '|':
		if start+1 < len(p.source) && p.source[start+1] == '|' {
			p.offset += 2
			p.token = constraintToken{kind: tokenOr, text: "||", offset: start}
			return
		}
		p.offset++
		p.token = constraintToken{text: "|", offset: start}
	default:
		for p.offset < len(p.source) && validTagByte(p.source[p.offset]) {
			p.offset++
		}
		if p.offset == start {
			p.offset++
			p.token = constraintToken{text: p.source[start:p.offset], offset: start}
			return
		}
		p.token = constraintToken{kind: tokenTag, text: p.source[start:p.offset], offset: start}
	}
}

func validTagByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') || ch == '_' || ch == '.'
}

func (p *constraintParser) error(message string) error {
	return ConstraintError{Offset: p.base + p.token.offset, Message: message}
}
