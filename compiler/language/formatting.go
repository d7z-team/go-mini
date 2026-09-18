package language

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/format"
	"github.com/d7z-team/mini-go/compiler/parser"
	"github.com/d7z-team/mini-go/compiler/source"
)

func (e *Engine) Format(uri DocumentURI) ([]TextEdit, error) {
	document, ok := e.snapshot.documents[uri]
	if !ok {
		return nil, ErrDocumentNotFound
	}
	result := e.formatSource(document.Identity.ModulePath, document.Identity.Path, document.Text)
	if source.HasErrors(result.Diagnostics) || result.Text == document.Text {
		return nil, nil
	}
	selected, _ := document.Index.Range(0, len(document.Text))
	return []TextEdit{{Range: selected, NewText: result.Text}}, nil
}

func (e *Engine) FormatRange(uri DocumentURI, selected Range) ([]TextEdit, error) {
	document, ok := e.snapshot.documents[uri]
	if !ok {
		return nil, ErrDocumentNotFound
	}
	if strings.HasSuffix(document.Identity.Path, ".mrpc") {
		return e.Format(uri)
	}
	start, err := document.Index.Offset(selected.Start)
	if err != nil {
		return nil, err
	}
	end, err := document.Index.Offset(selected.End)
	if err != nil {
		return nil, err
	}
	sourceSpan, _ := source.NewFile("lsp", document.Identity.Path, document.Text).Span(start, end)
	edits, _ := format.Range(parser.ParseDocument(document.Identity.ModulePath, document.Identity.Path, document.Text), sourceSpan)
	out := make([]TextEdit, 0, len(edits))
	for _, edit := range edits {
		editRange, rangeErr := document.Index.Range(edit.Span.Start.Offset, edit.Span.End.Offset)
		if rangeErr == nil {
			out = append(out, TextEdit{Range: editRange, NewText: edit.NewText})
		}
	}
	return out, nil
}

func (e *Engine) OnTypeFormat(uri DocumentURI, position Position, character string) ([]TextEdit, error) {
	document, ok := e.snapshot.documents[uri]
	if !ok {
		return nil, ErrDocumentNotFound
	}
	if strings.HasSuffix(document.Identity.Path, ".mrpc") {
		return nil, nil
	}
	offset, err := document.Index.Offset(position)
	if err != nil {
		return nil, err
	}
	var typed rune
	for _, value := range character {
		typed = value
		break
	}
	edits, diagnostics := format.OnType(parser.ParseDocument(document.Identity.ModulePath, document.Identity.Path, document.Text), offset, typed)
	if source.HasErrors(diagnostics) {
		return nil, nil
	}
	out := make([]TextEdit, 0, len(edits))
	for _, edit := range edits {
		selected, rangeErr := document.Index.Range(edit.Span.Start.Offset, edit.Span.End.Offset)
		if rangeErr == nil {
			out = append(out, TextEdit{Range: selected, NewText: edit.NewText})
		}
	}
	return out, nil
}
