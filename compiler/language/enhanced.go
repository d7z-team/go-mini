package language

import (
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/format"
	"github.com/d7z-team/mini-go/compiler/parser"
	"github.com/d7z-team/mini-go/compiler/scanner"
	check "github.com/d7z-team/mini-go/compiler/semantic"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/token"
)

var (
	SemanticTokenTypes     = []string{"namespace", "type", "typeParameter", "parameter", "variable", "property", "function", "method", "keyword", "comment", "string", "number", "operator"}
	SemanticTokenModifiers = []string{"declaration", "readonly"}
)

type semanticToken struct {
	line, character, length, kind, modifiers int
}

func (e *Engine) SemanticTokens(uri DocumentURI, selected *Range) SemanticTokens {
	document, ok := e.snapshot.documents[uri]
	if !ok {
		return SemanticTokens{Data: []uint32{}}
	}
	result := scanner.Scan(document.Identity.Path, document.Text)
	var values []semanticToken
	for _, element := range result.Elements {
		kind := -1
		switch element.Kind {
		case scanner.ElementLineComment, scanner.ElementBlockComment:
			kind = 9
		case scanner.ElementToken:
			switch element.Token {
			case token.Ident:
				kind = 4
			case token.String, token.Char:
				kind = 10
			case token.Int, token.Float, token.Imag:
				kind = 11
			default:
				if tokenKeyword(element.Token) {
					kind = 8
				} else if tokenOperator(element.Token) {
					kind = 12
				}
			}
		}
		if kind < 0 || !element.Span.Valid() {
			continue
		}
		span, err := document.Index.Range(element.Span.Start.Offset, element.Span.End.Offset)
		if err != nil || span.Start.Line != span.End.Line {
			continue
		}
		if selected != nil && !RangesOverlap(span, *selected) {
			continue
		}
		values = append(values, semanticToken{line: span.Start.Line, character: span.Start.Character, length: span.End.Character - span.Start.Character, kind: kind})
	}
	for _, occurrence := range e.snapshot.occurrences[uri] {
		span, err := document.Index.Range(occurrence.Name.Span.Start.Offset, occurrence.Name.Span.End.Offset)
		if err != nil || span.Start.Line != span.End.Line || selected != nil && !RangesOverlap(span, *selected) {
			continue
		}
		kind := 4
		modifiers := 0
		if occurrence.Role == ast.NameDefinition {
			modifiers |= 1
		}
		if pkg, ok := e.snapshot.packages[occurrence.ModulePath]; ok {
			if object, found := pkg.Checked.Info.Objects[occurrence.Object]; found {
				switch object.Kind {
				case check.ObjectType:
					kind = 1
				case check.ObjectTypeParam:
					kind = 2
				case check.ObjectField:
					kind = 5
				case check.ObjectFunc:
					if occurrence.Role == ast.NameSelector {
						kind = 7
					} else {
						kind = 6
					}
				case check.ObjectConst:
					modifiers |= 2
				}
			}
		}
		values = append(values, semanticToken{line: span.Start.Line, character: span.Start.Character, length: span.End.Character - span.Start.Character, kind: kind, modifiers: modifiers})
	}
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].line != values[j].line {
			return values[i].line < values[j].line
		}
		if values[i].character != values[j].character {
			return values[i].character < values[j].character
		}
		return values[i].kind < values[j].kind
	})
	dedup := values[:0]
	for _, value := range values {
		if value.length <= 0 {
			continue
		}
		if len(dedup) != 0 && dedup[len(dedup)-1].line == value.line && dedup[len(dedup)-1].character == value.character {
			dedup[len(dedup)-1] = value
			continue
		}
		dedup = append(dedup, value)
	}
	data := make([]uint32, 0, len(dedup)*5)
	previousLine, previousCharacter := 0, 0
	for _, value := range dedup {
		deltaLine := value.line - previousLine
		deltaCharacter := value.character
		if deltaLine == 0 {
			deltaCharacter -= previousCharacter
		}
		data = append(data, uint32(deltaLine), uint32(deltaCharacter), uint32(value.length), uint32(value.kind), uint32(value.modifiers))
		previousLine, previousCharacter = value.line, value.character
	}
	return SemanticTokens{ResultID: e.snapshot.resultIDs[uri], Data: data}
}

func RangesOverlap(left, right Range) bool {
	return positionLess(left.Start, right.End) && positionLess(right.Start, left.End)
}

func positionLess(left, right Position) bool {
	return left.Line < right.Line || left.Line == right.Line && left.Character < right.Character
}

func tokenKeyword(kind token.Kind) bool {
	switch kind {
	case token.Break, token.Case, token.Chan, token.Const, token.Continue, token.Default,
		token.Defer, token.Else, token.Fallthrough, token.For, token.Func, token.Go,
		token.Goto, token.If, token.Import, token.Interface, token.Map, token.Package,
		token.Range, token.Return, token.Select, token.Struct, token.Switch, token.Type, token.Var:
		return true
	default:
		return false
	}
}

func tokenOperator(kind token.Kind) bool {
	switch kind {
	case token.Add, token.Sub, token.Mul, token.Quo, token.Rem, token.And, token.Or, token.Xor,
		token.Shl, token.Shr, token.AndNot, token.Assign, token.Define, token.Eq, token.Ne,
		token.Lt, token.Le, token.Gt, token.Ge, token.Land, token.Lor, token.Not, token.Arrow:
		return true
	default:
		return false
	}
}

func (e *Engine) FoldingRanges(uri DocumentURI) []FoldingRange {
	document, ok := e.snapshot.documents[uri]
	if !ok {
		return nil
	}
	result := scanner.Scan(document.Identity.Path, document.Text)
	var stack []Position
	var out []FoldingRange
	for _, scanned := range result.Tokens {
		position, err := document.Index.Position(scanned.Span.Start.Offset)
		if err != nil {
			continue
		}
		switch scanned.Kind {
		case token.Lbrace, token.Lparen, token.Lbrack:
			stack = append(stack, position)
		case token.Rbrace, token.Rparen, token.Rbrack:
			if len(stack) == 0 {
				continue
			}
			start := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if position.Line > start.Line {
				out = append(out, FoldingRange{StartLine: start.Line, StartCharacter: start.Character, EndLine: position.Line, EndCharacter: position.Character})
			}
		}
	}
	return out
}

func (e *Engine) SelectionRanges(uri DocumentURI, positions []Position) []SelectionRange {
	document, ok := e.snapshot.documents[uri]
	if !ok {
		return nil
	}
	whole, _ := document.Index.Range(0, len(document.Text))
	parsed := parser.ParseDocument(document.Identity.ModulePath, document.Identity.Path, document.Text)
	out := make([]SelectionRange, 0, len(positions))
	for _, position := range positions {
		offset, err := document.Index.Offset(position)
		var spans []source.Span
		if err == nil {
			ast.WalkSpans(&parsed.Program, func(span source.Span) {
				if span.Valid() && span.Start.Offset <= offset && offset < span.End.Offset {
					spans = append(spans, span)
				}
			})
		}
		sort.SliceStable(spans, func(i, j int) bool {
			return spans[i].End.Offset-spans[i].Start.Offset > spans[j].End.Offset-spans[j].Start.Offset
		})
		parent := &SelectionRange{Range: whole}
		for _, span := range spans {
			selected, err := document.Index.Range(span.Start.Offset, span.End.Offset)
			if err == nil && selected != parent.Range && !positionLess(selected.Start, parent.Range.Start) && !positionLess(parent.Range.End, selected.End) {
				parent = &SelectionRange{Range: selected, Parent: parent}
			}
		}
		if occurrence, found := e.occurrence(uri, position); found {
			selected, _ := document.Index.Range(occurrence.Name.Span.Start.Offset, occurrence.Name.Span.End.Offset)
			if selected != parent.Range && !positionLess(selected.Start, parent.Range.Start) && !positionLess(parent.Range.End, selected.End) {
				parent = &SelectionRange{Range: selected, Parent: parent}
			}
		}
		out = append(out, *parent)
	}
	return out
}

func (e *Engine) CodeActions(uri DocumentURI, diagnostics []Diagnostic) []CodeAction {
	document, exists := e.snapshot.documents[uri]
	if !exists {
		return nil
	}
	var actions []CodeAction
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "semantic.import.unused" {
			continue
		}
		edit := &WorkspaceEdit{DocumentChanges: []TextDocumentEdit{{
			TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: document.Version},
			Edits:        []TextEdit{{Range: diagnostic.Range, NewText: ""}},
		}}}
		actions = append(actions, CodeAction{Title: "Remove unused import", Kind: "quickfix", Diagnostics: []Diagnostic{diagnostic}, Edit: edit})
	}
	if edits, err := e.Format(uri); err == nil && len(edits) != 0 {
		workspaceEdit := &WorkspaceEdit{DocumentChanges: []TextDocumentEdit{{
			TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: document.Version}, Edits: edits,
		}}}
		actions = append(actions, CodeAction{Title: "Format document", Kind: "source.format", Diagnostics: diagnostics, Edit: workspaceEdit, Data: "format|" + string(uri)})
	}
	if edits, err := e.OrganizeImports(uri); err == nil && len(edits) != 0 {
		workspaceEdit := &WorkspaceEdit{DocumentChanges: []TextDocumentEdit{{
			TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: document.Version}, Edits: edits,
		}}}
		actions = append(actions, CodeAction{Title: "Organize imports", Kind: "source.organizeImports", Edit: workspaceEdit, Data: "imports|" + string(uri)})
	}
	return actions
}

func (e *Engine) ResolveCodeAction(action CodeAction) CodeAction {
	if action.Edit != nil || action.Data == "" {
		return action
	}
	parts := strings.SplitN(action.Data, "|", 2)
	if len(parts) != 2 {
		return action
	}
	uri := DocumentURI(parts[1])
	var edits []TextEdit
	var err error
	if parts[0] == "imports" {
		edits, err = e.OrganizeImports(uri)
	} else {
		edits, err = e.Format(uri)
	}
	if err != nil || len(edits) == 0 {
		return action
	}
	document := e.snapshot.documents[uri]
	action.Edit = &WorkspaceEdit{DocumentChanges: []TextDocumentEdit{{TextDocument: VersionedTextDocumentIdentifier{URI: document.Identity.URI, Version: document.Version}, Edits: edits}}}
	return action
}

func (e *Engine) OrganizeImports(uri DocumentURI) ([]TextEdit, error) {
	document, ok := e.snapshot.documents[uri]
	if !ok {
		return nil, ErrDocumentNotFound
	}
	type byteRange struct{ start, end int }
	var removals []byteRange
	for _, diagnostic := range e.snapshot.diagnostics[uri] {
		if diagnostic.Code != "semantic.import.unused" {
			continue
		}
		start, startErr := document.Index.Offset(diagnostic.Range.Start)
		end, endErr := document.Index.Offset(diagnostic.Range.End)
		if startErr == nil && endErr == nil && start < end {
			removals = append(removals, byteRange{start: start, end: end})
		}
	}
	if len(removals) == 0 {
		return nil, nil
	}
	sort.Slice(removals, func(i, j int) bool { return removals[i].start > removals[j].start })
	text := document.Text
	for _, removal := range removals {
		text = text[:removal.start] + text[removal.end:]
	}
	formatted := format.Source(document.Identity.ModulePath, document.Identity.Path, text)
	if source.HasErrors(formatted.Diagnostics) || formatted.Text == document.Text {
		return nil, nil
	}
	whole, _ := document.Index.Range(0, len(document.Text))
	return []TextEdit{{Range: whole, NewText: formatted.Text}}, nil
}
