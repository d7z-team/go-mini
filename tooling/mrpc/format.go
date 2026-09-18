package mrpc

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type formatLexemeKind uint8

const (
	formatWord formatLexemeKind = iota + 1
	formatPunctuation
	formatLineComment
	formatBlockComment
)

type formatLexeme struct {
	kind formatLexemeKind
	text string
}

// Format returns a canonical MRPC layout while preserving every logical token
// and comment byte-for-byte. Invalid input is returned unchanged.
func Format(file Source) (string, []Diagnostic) {
	_, diagnostics := Parse(file)
	if HasErrors(diagnostics) {
		return file.Text, diagnostics
	}
	lexemes := scanFormatLexemes(file.Text)
	var out strings.Builder
	indent := 0
	lineStart := true
	previous := formatLexeme{}
	writeIndent := func() {
		if lineStart {
			out.WriteString(strings.Repeat("\t", indent))
			lineStart = false
		}
	}
	space := func() {
		if !lineStart && out.Len() != 0 {
			text := out.String()
			if text[len(text)-1] != ' ' && text[len(text)-1] != '\n' && text[len(text)-1] != '\t' {
				out.WriteByte(' ')
			}
		}
	}
	newline := func() {
		text := out.String()
		for len(text) != 0 && (text[len(text)-1] == ' ' || text[len(text)-1] == '\t') {
			text = text[:len(text)-1]
		}
		out.Reset()
		out.WriteString(text)
		if len(text) == 0 || text[len(text)-1] != '\n' {
			out.WriteByte('\n')
		}
		lineStart = true
	}
	for index, item := range lexemes {
		next := formatLexeme{}
		if index+1 < len(lexemes) {
			next = lexemes[index+1]
		}
		switch item.kind {
		case formatLineComment:
			writeIndent()
			if previous.kind != 0 && !lineStart {
				space()
			}
			out.WriteString(item.text)
			newline()
			if strings.HasPrefix(item.text, "//go:build ") {
				out.WriteByte('\n')
			}
		case formatBlockComment:
			writeIndent()
			if previous.kind != 0 {
				space()
			}
			out.WriteString(item.text)
			if strings.Contains(item.text, "\n") || next.kind == formatLineComment {
				newline()
			} else {
				out.WriteByte(' ')
			}
		case formatPunctuation:
			switch item.text {
			case "{":
				writeIndent()
				space()
				out.WriteByte('{')
				indent++
				newline()
			case "}":
				if !lineStart {
					newline()
				}
				if indent > 0 {
					indent--
				}
				writeIndent()
				out.WriteByte('}')
				if next.text != ";" {
					newline()
				}
			case ";":
				writeIndent()
				out.WriteByte(';')
				newline()
			case ",":
				writeIndent()
				out.WriteString(", ")
			case "=":
				space()
				out.WriteString("= ")
			default:
				writeIndent()
				out.WriteString(item.text)
			}
		default:
			writeIndent()
			if previous.kind == formatWord || previous.kind == formatBlockComment || previous.text == "]" || previous.text == ")" {
				space()
			}
			out.WriteString(item.text)
		}
		previous = item
	}
	formatted := strings.TrimRight(out.String(), " \t\n") + "\n"
	if !sameFormatLexemes(lexemes, scanFormatLexemes(formatted)) {
		span, _ := file.Span(0, 0)
		return file.Text, []Diagnostic{{Code: "mrpc.format.tokens", Severity: SeverityError, Message: "formatter changed the logical token stream", Primary: span}}
	}
	formattedFile := Source{Path: file.Path, Text: formatted}
	if _, reparsed := Parse(formattedFile); HasErrors(reparsed) {
		span, _ := file.Span(0, 0)
		return file.Text, []Diagnostic{{Code: "mrpc.format.syntax", Severity: SeverityError, Message: "formatter produced invalid MRPC syntax", Primary: span}}
	}
	return formatted, nil
}

func scanFormatLexemes(text string) []formatLexeme {
	var out []formatLexeme
	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		if unicode.IsSpace(r) {
			offset += size
			continue
		}
		start := offset
		if strings.HasPrefix(text[offset:], "//") {
			offset = len(text)
			if end := strings.IndexByte(text[start:], '\n'); end >= 0 {
				offset = start + end
			}
			out = append(out, formatLexeme{kind: formatLineComment, text: text[start:offset]})
			continue
		}
		if strings.HasPrefix(text[offset:], "/*") {
			offset = len(text)
			if end := strings.Index(text[start+2:], "*/"); end >= 0 {
				offset = start + end + 4
			}
			out = append(out, formatLexeme{kind: formatBlockComment, text: text[start:offset]})
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
			out = append(out, formatLexeme{kind: formatWord, text: text[start:offset]})
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			offset += size
			for offset < len(text) {
				r, size = utf8.DecodeRuneInString(text[offset:])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
					break
				}
				offset += size
			}
			out = append(out, formatLexeme{kind: formatWord, text: text[start:offset]})
			continue
		}
		offset += size
		out = append(out, formatLexeme{kind: formatPunctuation, text: text[start:offset]})
	}
	return out
}

func sameFormatLexemes(left, right []formatLexeme) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
