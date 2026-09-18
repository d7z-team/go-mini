package mrpc

import (
	"sort"
	"strconv"
	"strings"
)

type parser struct {
	file        Source
	tokens      []token
	index       int
	diagnostics []Diagnostic
}

// Header contains the dependency information available from an MRPC source.
type Header struct {
	Syntax    string
	Namespace string
	Options   map[string]string
	Imports   []string
}

// ScanHeader returns the contract identity and direct schema imports.
func ScanHeader(source Source) (Header, []Diagnostic) {
	file, diagnostics := Parse(source)
	header := Header{Syntax: file.Syntax, Namespace: file.Namespace, Options: make(map[string]string)}
	for _, option := range file.Options {
		header.Options[option.Name] = option.Value
	}
	for _, imported := range file.Imports {
		header.Imports = append(header.Imports, imported.Path)
	}
	sort.Strings(header.Imports)
	return header, diagnostics
}

// Parse parses one MRPC source file and returns normalized diagnostics.
func Parse(source Source) (File, []Diagnostic) {
	if source.Hash == "" {
		source.Hash = HashText(source.Text)
	}
	tokens, diagnostics := lex(source)
	p := parser{file: source, tokens: tokens, diagnostics: diagnostics}
	syntax, namespace, namespaceSpan := p.parsePreamble()
	out := File{Path: source.Path, Text: source.Text, Hash: source.Hash, Syntax: syntax, Namespace: namespace, NamespaceSpan: namespaceSpan}
	declarationsStarted := false
	for p.peek().kind != tokenEOF {
		start := p.peek()
		switch start.text {
		case "option":
			if declarationsStarted {
				p.add("mrpc.option.order", "options must appear before declarations", start)
			}
			out.Options = append(out.Options, p.parseOption())
		case "import":
			if declarationsStarted {
				p.add("mrpc.import.order", "imports must appear before declarations", start)
			}
			out.Imports = append(out.Imports, p.parseImport())
		case "message":
			declarationsStarted = true
			out.Messages = append(out.Messages, p.parseMessage())
		case "enum":
			declarationsStarted = true
			out.Enums = append(out.Enums, p.parseEnum())
		case "resource":
			declarationsStarted = true
			out.Resources = append(out.Resources, p.parseResource())
		case "service":
			declarationsStarted = true
			out.Services = append(out.Services, p.parseService())
		default:
			p.add("mrpc.declaration", "expected option, import, enum, message, resource, or service", start)
			p.recoverDeclaration()
		}
	}
	return out, NormalizeDiagnostics(p.diagnostics)
}

func (p *parser) parseEnum() Enum {
	start := p.next()
	name, _ := p.take(tokenIdent)
	p.expect(tokenLBrace, "expected { after enum name")
	item := Enum{Doc: p.leadingDoc(start.start), Name: name.text, NameSpan: p.span(name.start, name.end)}
	for p.peek().kind != tokenRBrace && p.peek().kind != tokenEOF {
		valueName, _ := p.take(tokenIdent)
		p.expect(tokenAssign, "expected = before enum value")
		number, ok := p.take(tokenNumber)
		value := EnumValue{Doc: p.leadingDoc(valueName.start), Name: valueName.text, NameSpan: p.span(valueName.start, valueName.end)}
		if ok {
			value.Number, _ = strconv.ParseInt(number.text, 10, 64)
		}
		end := p.expect(tokenSemicolon, "expected ; after enum value")
		value.Span = p.span(valueName.start, end.end)
		item.Values = append(item.Values, value)
	}
	end := p.expect(tokenRBrace, "expected } after enum")
	item.Span = p.span(start.start, end.end)
	return item
}

func (p *parser) parsePreamble() (string, string, Span) {
	syntax := ""
	p.expectKeyword("syntax")
	p.expect(tokenAssign, "expected = after syntax")
	if value, ok := p.take(tokenString); ok {
		parsed, err := strconv.Unquote(value.text)
		if err != nil || parsed != Syntax {
			p.add("mrpc.syntax.version", "syntax must be \""+Syntax+"\"", value)
		} else {
			syntax = parsed
		}
	}
	p.expect(tokenSemicolon, "expected ; after syntax")
	p.expectKeyword("namespace")
	start, ok := p.take(tokenIdent)
	if !ok {
		return syntax, "", Span{}
	}
	parts := []string{start.text}
	end := start
	for p.peek().kind == tokenPeriod {
		p.next()
		part, partOK := p.take(tokenIdent)
		if !partOK {
			break
		}
		parts = append(parts, part.text)
		end = part
	}
	p.expect(tokenSemicolon, "expected ; after namespace")
	return syntax, strings.Join(parts, "."), p.span(start.start, end.end)
}

func (p *parser) parseOption() Option {
	start := p.next()
	name, _ := p.take(tokenIdent)
	p.expect(tokenAssign, "expected = after option name")
	value, ok := p.take(tokenString)
	item := Option{Name: name.text, NameSpan: p.span(name.start, name.end)}
	if ok {
		item.Value, _ = strconv.Unquote(value.text)
		item.ValueSpan = p.span(value.start, value.end)
	}
	end := p.expect(tokenSemicolon, "expected ; after option")
	item.Span = p.span(start.start, end.end)
	return item
}

func (p *parser) parseImport() Import {
	start := p.next()
	item := Import{}
	if p.peek().kind == tokenIdent {
		alias := p.next()
		item.Alias = alias.text
		item.AliasSpan = p.span(alias.start, alias.end)
	}
	pathToken, ok := p.take(tokenString)
	if ok {
		item.Path, _ = strconv.Unquote(pathToken.text)
		item.PathSpan = p.span(pathToken.start, pathToken.end)
	}
	end := p.expect(tokenSemicolon, "expected ; after import")
	item.Span = p.span(start.start, end.end)
	return item
}

func (p *parser) parseMessage() Message {
	start := p.next()
	name, _ := p.take(tokenIdent)
	p.expect(tokenLBrace, "expected { after message name")
	item := Message{Doc: p.leadingDoc(start.start), Name: name.text, NameSpan: p.span(name.start, name.end)}
	for p.peek().kind != tokenRBrace && p.peek().kind != tokenEOF {
		item.Fields = append(item.Fields, p.parseField())
		p.expect(tokenSemicolon, "expected ; after message field")
	}
	end := p.expect(tokenRBrace, "expected } after message")
	item.Span = p.span(start.start, end.end)
	return item
}

func (p *parser) parseResource() Resource {
	start := p.next()
	name, _ := p.take(tokenIdent)
	p.expect(tokenLBrace, "expected { after resource name")
	item := Resource{Doc: p.leadingDoc(start.start), Name: name.text, NameSpan: p.span(name.start, name.end)}
	for p.peek().kind != tokenRBrace && p.peek().kind != tokenEOF {
		item.Methods = append(item.Methods, p.parseMethod())
	}
	end := p.expect(tokenRBrace, "expected } after resource")
	item.Span = p.span(start.start, end.end)
	return item
}

func (p *parser) parseService() Service {
	start := p.next()
	name, _ := p.take(tokenIdent)
	p.expect(tokenLBrace, "expected { after service name")
	item := Service{Doc: p.leadingDoc(start.start), Name: name.text, NameSpan: p.span(name.start, name.end)}
	for p.peek().kind != tokenRBrace && p.peek().kind != tokenEOF {
		item.Methods = append(item.Methods, p.parseMethod())
	}
	end := p.expect(tokenRBrace, "expected } after service")
	item.Span = p.span(start.start, end.end)
	return item
}

func (p *parser) parseMethod() Method {
	start, _ := p.take(tokenIdent)
	item := Method{Doc: p.leadingDoc(start.start), Name: start.text, NameSpan: p.span(start.start, start.end)}
	p.expect(tokenLParen, "expected ( after method name")
	item.Params = p.parseFieldList(tokenRParen)
	p.expect(tokenRParen, "expected ) after method parameters")
	p.expectKeyword("returns")
	p.expect(tokenLParen, "expected ( after returns")
	item.Results = p.parseFieldList(tokenRParen)
	p.expect(tokenRParen, "expected ) after method results")
	end := p.expect(tokenSemicolon, "expected ; after method")
	item.Span = p.span(start.start, end.end)
	return item
}

func (p *parser) parseFieldList(end tokenKind) []Field {
	var fields []Field
	for p.peek().kind != end && p.peek().kind != tokenEOF {
		fields = append(fields, p.parseField())
		if p.peek().kind != tokenComma {
			break
		}
		p.next()
	}
	return fields
}

func (p *parser) parseField() Field {
	start := p.peek()
	name := ""
	var nameSpan Span
	if p.peek().kind == tokenIdent && (p.look(1).kind == tokenIdent || p.look(1).kind == tokenLBracket) {
		nameToken := p.next()
		name = nameToken.text
		nameSpan = p.span(nameToken.start, nameToken.end)
	}
	typ := p.parseType()
	p.expect(tokenAssign, "expected = before field ID")
	idToken, ok := p.take(tokenNumber)
	id := 0
	if ok {
		id, _ = strconv.Atoi(idToken.text)
	}
	return Field{Doc: p.leadingDoc(start.start), Name: name, NameSpan: nameSpan, Type: typ, ID: id, IDSpan: p.span(idToken.start, idToken.end), Span: p.span(start.start, idToken.end)}
}

func (p *parser) leadingDoc(offset int) string {
	if offset <= 0 || offset > len(p.file.Text) {
		return ""
	}
	text := strings.TrimRight(p.file.Text[:offset], " \t\r")
	if !strings.HasSuffix(text, "\n") {
		return ""
	}
	text = strings.TrimRight(strings.TrimSuffix(text, "\n"), " \t\r")
	if strings.HasSuffix(text, "\n") {
		return ""
	}
	if strings.HasSuffix(text, "*/") {
		start := strings.LastIndex(text, "/*")
		if start < 0 {
			return ""
		}
		body := strings.TrimSpace(text[start+2 : len(text)-2])
		parts := strings.Split(body, "\n")
		for index := range parts {
			parts[index] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(parts[index]), "*"))
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	var lines []string
	for {
		lineStart := strings.LastIndexByte(text, '\n') + 1
		line := strings.TrimSpace(text[lineStart:])
		if !strings.HasPrefix(line, "//") {
			break
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "//"))
		lines = append(lines, line)
		if lineStart == 0 {
			break
		}
		text = strings.TrimRight(text[:lineStart-1], " \t\r")
		if strings.HasSuffix(text, "\n") {
			break
		}
	}
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	return strings.Join(lines, "\n")
}

func (p *parser) parseType() Type {
	start := p.peek()
	if start.kind == tokenLBracket && p.look(1).kind == tokenRBracket {
		p.next()
		p.next()
		elem := p.parseType()
		return Type{Kind: TypeSlice, Elem: &elem, Span: p.span(start.start, elem.Span.End.Offset)}
	}
	name, ok := p.take(tokenIdent)
	if !ok {
		p.add("mrpc.type", "expected type", start)
		if p.peek().kind != tokenEOF {
			p.next()
		}
		return Type{Span: p.span(start.start, start.end)}
	}
	if name.text == "map" && p.peek().kind == tokenLBracket {
		p.next()
		key := p.parseType()
		p.expect(tokenRBracket, "expected ] after map key")
		elem := p.parseType()
		return Type{Kind: TypeMap, Key: &key, Elem: &elem, Span: p.span(start.start, elem.Span.End.Offset)}
	}
	if name.text == "optional" && p.peek().kind == tokenLBracket {
		p.next()
		elem := p.parseType()
		end := p.expect(tokenRBracket, "expected ] after optional type")
		return Type{Kind: TypeOptional, Elem: &elem, Span: p.span(start.start, end.end)}
	}
	typ := Type{Kind: TypeName, Name: name.text, NameSpan: p.span(name.start, name.end), Span: p.span(name.start, name.end)}
	if p.peek().kind == tokenPeriod {
		p.next()
		member, _ := p.take(tokenIdent)
		typ.Qualifier, typ.Name = typ.Name, member.text
		typ.QualifierSpan = typ.NameSpan
		typ.NameSpan = p.span(member.start, member.end)
		typ.Span = p.span(name.start, member.end)
	}
	return typ
}

func (p *parser) expectKeyword(keyword string) {
	item := p.peek()
	if item.kind != tokenIdent || item.text != keyword {
		p.add("mrpc.syntax", "expected "+keyword, item)
		return
	}
	p.next()
}

func (p *parser) expect(kind tokenKind, message string) token {
	item := p.peek()
	if item.kind != kind {
		p.add("mrpc.syntax", message, item)
		return item
	}
	return p.next()
}

func (p *parser) take(kind tokenKind) (token, bool) {
	item := p.peek()
	if item.kind != kind {
		p.add("mrpc.syntax", "unexpected "+item.text, item)
		return item, false
	}
	return p.next(), true
}

func (p *parser) recoverDeclaration() {
	for p.peek().kind != tokenEOF {
		if p.peek().kind == tokenSemicolon {
			p.next()
			return
		}
		if p.peek().kind == tokenIdent {
			switch p.peek().text {
			case "option", "import", "enum", "message", "resource", "service":
				return
			}
		}
		p.next()
	}
}

func (p *parser) peek() token { return p.look(0) }

func (p *parser) look(offset int) token {
	index := p.index + offset
	if index >= len(p.tokens) {
		return token{kind: tokenEOF, start: len(p.file.Text), end: len(p.file.Text)}
	}
	return p.tokens[index]
}

func (p *parser) next() token {
	item := p.peek()
	if p.index < len(p.tokens) {
		p.index++
	}
	return item
}

func (p *parser) add(code, message string, item token) {
	p.diagnostics = append(p.diagnostics, Diagnostic{Code: DiagnosticCode(code), Severity: SeverityError, Message: message, Primary: p.span(item.start, item.end)})
}

func (p *parser) span(start, end int) Span {
	span, _ := p.file.Span(start, end)
	return span
}
