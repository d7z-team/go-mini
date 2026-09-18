package parser

import (
	"strconv"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/scanner"
	"github.com/d7z-team/mini-go/compiler/token"
)

func (p *parser) parseTypeParams() []ast.TypeParam {
	if !p.at(token.Lbrack) || p.peek(1).Kind != token.Ident || !startsTypeParamConstraint(p.peek(2).Kind) {
		return nil
	}
	p.advance()
	var params []ast.TypeParam
	seen := map[string]struct{}{}
	for !p.at(token.Rbrack) && !p.at(token.EOF) {
		start := p.current()
		names := []scanner.Token{p.expect(token.Ident, "parser.type_param.name", "expected type parameter name")}
		for p.at(token.Comma) && p.peek(1).Kind == token.Ident && p.peek(2).Kind != token.Rbrack {
			p.advance()
			names = append(names, p.advance())
		}
		constraint := p.parseTypeConstraint()
		for _, name := range names {
			if name.Lexeme == "_" {
				// Blank type parameters occupy a position but introduce no binding.
			} else if _, exists := seen[name.Lexeme]; exists {
				p.add("parser.type_param.duplicate", "duplicate type parameter name", name.Span)
			} else {
				seen[name.Lexeme] = struct{}{}
			}
			params = append(params, ast.TypeParam{
				Name:       name.Lexeme,
				NameID:     identifier(name),
				Constraint: constraint,
				Span:       spanJoin(start.Span, constraint.Span),
			})
		}
		if !p.match(token.Comma) {
			break
		}
	}
	p.expect(token.Rbrack, "parser.type_param.close", "expected ] after type parameters")
	return params
}

func startsTypeParamConstraint(kind token.Kind) bool {
	switch kind {
	case token.Comma, token.Ident, token.Lparen, token.Tilde, token.Mul, token.Lbrack, token.Map,
		token.Chan, token.Arrow, token.Func, token.Struct, token.Interface:
		return true
	default:
		return false
	}
}

func (p *parser) parseTypeConstraint() ast.TypeExpr {
	var terms []ast.TypeTerm
	for {
		approx := p.match(token.Tilde)
		typ := p.parseType()
		terms = append(terms, ast.TypeTerm{Type: typ, Approx: approx, Span: typ.Span})
		if !p.match(token.Or) {
			break
		}
	}
	if len(terms) == 1 && !terms[0].Approx {
		return terms[0].Type
	}
	return ast.TypeExpr{Kind: ast.TypeInterface, Terms: terms, Span: spanJoin(terms[0].Span, terms[len(terms)-1].Span)}
}

func (p *parser) parseSignature() ([]ast.Field, []ast.Field) {
	params := p.parseParameterList()
	var results []ast.Field
	if p.at(token.Lparen) {
		results = p.parseParameterList()
	} else if p.canStartType() {
		results = []ast.Field{{Type: p.parseType()}}
	}
	return params, results
}

func (p *parser) parseParameterList() []ast.Field {
	p.expect(token.Lparen, "parser.params", "expected (")
	fields := p.parseFieldGroup(token.Rparen, true)
	p.expect(token.Rparen, "parser.params", "expected )")
	return fields
}

func (p *parser) parseFieldGroup(end token.Kind, allowVariadic bool) []ast.Field {
	var fields []ast.Field
	for !p.at(end) && !p.at(token.EOF) {
		before := p.pos
		p.consumeSemicolons()
		if p.at(end) {
			break
		}
		fields = append(fields, p.parseField(end, allowVariadic)...)
		if !p.match(token.Comma) {
			p.optionalSemicolon()
		}
		p.ensureProgress(before, "parser.progress.field", exprEnd)
	}
	return fields
}

func (p *parser) parseField(end token.Kind, allowVariadic bool) []ast.Field {
	start := p.current()
	if p.at(token.Ident) {
		if p.looksLikeTypeOnlyField(end) {
			typ := p.parseType()
			tag := ""
			if p.at(token.String) {
				tagToken := p.advance()
				if unquoted, err := strconv.Unquote(tagToken.Lexeme); err == nil {
					tag = unquoted
				} else {
					tag = tagToken.Lexeme
				}
			}
			return []ast.Field{{Type: typ, Tag: tag, Span: typ.Span}}
		}
		first := p.advance()
		names := []scanner.Token{first}
		for p.match(token.Comma) {
			if !p.at(token.Ident) {
				if allowVariadic && p.at(token.Ellipsis) {
					p.pos--
					break
				}
				p.errorAtCurrent("parser.field.name", "expected field name")
				break
			}
			next := p.advance()
			names = append(names, next)
		}
		if p.at(end) || p.at(token.Semicolon) || p.at(token.Comma) {
			out := make([]ast.Field, 0, len(names))
			for _, name := range names {
				typ := ast.TypeExpr{Kind: ast.TypeName, Name: name.Lexeme, NameID: identifier(name), Span: name.Span}
				out = append(out, ast.Field{Type: typ, Span: typ.Span})
			}
			return out
		}
		typ, variadic := p.parseFieldType(allowVariadic)
		tag := ""
		if p.at(token.String) {
			tagToken := p.advance()
			if unquoted, err := strconv.Unquote(tagToken.Lexeme); err == nil {
				tag = unquoted
			} else {
				tag = tagToken.Lexeme
			}
		}
		out := make([]ast.Field, 0, len(names))
		for _, name := range names {
			out = append(out, ast.Field{
				Name:     name.Lexeme,
				NameID:   identifier(name),
				Type:     typ,
				Tag:      tag,
				Variadic: variadic,
				Span:     spanJoin(startSpan(start), typ.Span),
			})
		}
		return out
	}
	typ, variadic := p.parseFieldType(allowVariadic)
	tag := ""
	if p.at(token.String) {
		tagToken := p.advance()
		if unquoted, err := strconv.Unquote(tagToken.Lexeme); err == nil {
			tag = unquoted
		}
	}
	return []ast.Field{{Type: typ, Tag: tag, Variadic: variadic, Span: typ.Span}}
}

func (p *parser) parseFieldType(allowVariadic bool) (ast.TypeExpr, bool) {
	if allowVariadic && p.match(token.Ellipsis) {
		start := p.previous()
		elem := p.parseType()
		return ast.TypeExpr{Kind: ast.TypeSlice, Elem: &elem, Span: spanJoin(startSpan(start), elem.Span)}, true
	}
	return p.parseType(), false
}

func (p *parser) looksLikeTypeOnlyField(end token.Kind) bool {
	pos := p.pos
	for {
		probe := *p
		probe.diagnostics = nil
		probe.pos = pos
		typ := probe.parseType()
		if typ.Kind == ast.TypeInvalid || len(probe.diagnostics) != 0 {
			return false
		}
		if probe.at(token.String) {
			probe.advance()
		}
		if probe.at(token.Comma) {
			pos = probe.pos + 1
			continue
		}
		return probe.at(end) || probe.at(token.Semicolon)
	}
}

func (p *parser) parseType() ast.TypeExpr {
	if !p.enterNest("parser.type.depth", "type nesting too deep") {
		return ast.TypeExpr{Kind: ast.TypeInvalid, Span: p.current().Span}
	}
	defer p.leaveNest()
	start := p.current()
	switch start.Kind {
	case token.Lparen:
		p.advance()
		typ := p.parseType()
		end := p.expect(token.Rparen, "parser.type.paren", "expected ) after type")
		typ.Span = spanJoin(startSpan(start), end.Span)
		return typ
	case token.Ident:
		p.advance()
		name := start.Lexeme
		nameID := identifier(start)
		qualifierID := ast.Identifier{}
		if p.match(token.Period) {
			part := p.expect(token.Ident, "parser.type.selector", "expected selector")
			name += "." + part.Lexeme
			qualifierID = nameID
			nameID = identifier(part)
		}
		base := ast.TypeExpr{Kind: ast.TypeName, Name: name, NameID: nameID, QualifierID: qualifierID, Span: spanJoin(start.Span, p.previous().Span)}
		if !p.match(token.Lbrack) {
			return base
		}
		var args []ast.TypeExpr
		for !p.at(token.Rbrack) && !p.at(token.EOF) {
			args = append(args, p.parseType())
			if !p.match(token.Comma) {
				break
			}
		}
		end := p.expect(token.Rbrack, "parser.type_args.close", "expected ] after type arguments")
		return ast.TypeExpr{Kind: ast.TypeInstance, Base: &base, TypeArgs: args, Span: spanJoin(base.Span, end.Span)}
	case token.Mul:
		p.advance()
		elem := p.parseType()
		return ast.TypeExpr{Kind: ast.TypePointer, Elem: &elem, Span: spanJoin(startSpan(start), elem.Span)}
	case token.Lbrack:
		p.advance()
		if p.match(token.Rbrack) {
			elem := p.parseType()
			return ast.TypeExpr{Kind: ast.TypeSlice, Elem: &elem, Span: spanJoin(startSpan(start), elem.Span)}
		}
		if p.match(token.Ellipsis) {
			p.expect(token.Rbrack, "parser.type.array", "expected ] after array length")
			elem := p.parseType()
			return ast.TypeExpr{Kind: ast.TypeArray, LenInfer: true, Elem: &elem, Span: spanJoin(startSpan(start), elem.Span)}
		}
		length := p.parseExpression(1)
		p.expect(token.Rbrack, "parser.type.array", "expected ] after array length")
		elem := p.parseType()
		return ast.TypeExpr{Kind: ast.TypeArray, Len: &length, Elem: &elem, Span: spanJoin(startSpan(start), elem.Span)}
	case token.Map:
		p.advance()
		p.expect(token.Lbrack, "parser.type.map", "expected [ after map")
		key := p.parseType()
		p.expect(token.Rbrack, "parser.type.map", "expected ] after map key")
		elem := p.parseType()
		return ast.TypeExpr{Kind: ast.TypeMap, Key: &key, Elem: &elem, Span: spanJoin(startSpan(start), elem.Span)}
	case token.Chan:
		p.advance()
		direction := ""
		if p.match(token.Arrow) {
			direction = "send"
		}
		elem := p.parseType()
		return ast.TypeExpr{Kind: ast.TypeChan, Elem: &elem, Direction: direction, Span: spanJoin(startSpan(start), elem.Span)}
	case token.Arrow:
		p.advance()
		p.expect(token.Chan, "parser.type.chan", "expected chan after <-")
		elem := p.parseType()
		return ast.TypeExpr{Kind: ast.TypeChan, Elem: &elem, Direction: "recv", Span: spanJoin(startSpan(start), elem.Span)}
	case token.Func:
		p.advance()
		params, results := p.parseSignature()
		out := ast.TypeExpr{Kind: ast.TypeFunc, Params: params, Results: results, Span: startSpan(start)}
		if len(results) != 0 {
			out.Span = spanJoin(out.Span, results[len(results)-1].Span)
		} else if len(params) != 0 {
			out.Span = spanJoin(out.Span, params[len(params)-1].Span)
		}
		return out
	case token.Struct:
		return p.parseStructType()
	case token.Interface:
		return p.parseInterfaceType()
	}
	p.errorAtCurrent("parser.type", "expected type")
	if !p.at(token.EOF) {
		p.advance()
	}
	return ast.TypeExpr{Kind: ast.TypeInvalid, Span: startSpan(start)}
}

func (p *parser) parseStructType() ast.TypeExpr {
	start := p.expect(token.Struct, "parser.type.struct", "expected struct")
	p.expect(token.Lbrace, "parser.type.struct", "expected { after struct")
	fields := p.parseFieldGroup(token.Rbrace, false)
	end := p.expect(token.Rbrace, "parser.type.struct", "expected } after struct fields")
	return ast.TypeExpr{Kind: ast.TypeStruct, Fields: fields, Span: join(start, end)}
}

func (p *parser) parseInterfaceType() ast.TypeExpr {
	start := p.expect(token.Interface, "parser.type.interface", "expected interface")
	p.expect(token.Lbrace, "parser.type.interface", "expected { after interface")
	var methods []ast.FuncDecl
	var embeds []ast.TypeExpr
	var terms []ast.TypeTerm
	for !p.at(token.Rbrace) && !p.at(token.EOF) {
		before := p.pos
		p.consumeSemicolons()
		if p.at(token.Rbrace) {
			break
		}
		if p.at(token.Ident) && p.peek(1).Kind == token.Lparen {
			name := p.advance()
			params, results := p.parseSignature()
			methods = append(methods, ast.FuncDecl{Name: name.Lexeme, NameID: identifier(name), Params: params, Results: results})
		} else {
			parsedTerms := p.parseInterfaceTerms()
			if len(parsedTerms) == 1 && !parsedTerms[0].Approx {
				embeds = append(embeds, parsedTerms[0].Type)
			} else {
				terms = append(terms, parsedTerms...)
			}
		}
		p.optionalSemicolon()
		p.ensureProgress(before, "parser.progress.interface", exprEnd)
	}
	end := p.expect(token.Rbrace, "parser.type.interface", "expected } after interface")
	return ast.TypeExpr{Kind: ast.TypeInterface, Methods: methods, Embeds: embeds, Terms: terms, Span: join(start, end)}
}

func (p *parser) parseInterfaceTerms() []ast.TypeTerm {
	var terms []ast.TypeTerm
	for {
		approx := p.match(token.Tilde)
		typ := p.parseType()
		terms = append(terms, ast.TypeTerm{Type: typ, Approx: approx, Span: typ.Span})
		if !p.match(token.Or) {
			break
		}
	}
	return terms
}
