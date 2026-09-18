// Package format formats Mini-Go and MRPC source documents.
package format

import (
	"strings"

	core "github.com/d7z-team/mini-go/compiler/format"
	"github.com/d7z-team/mini-go/compiler/parser"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/tooling/mrpc"
)

type (
	Result   = core.Result
	TextEdit = core.TextEdit
)

func Source(modulePath, path, text string) Result {
	if !strings.HasSuffix(path, ".mrpc") {
		return core.Source(modulePath, path, text)
	}
	formatted, diagnostics := mrpc.Format(mrpc.Source{Path: path, Text: text})
	converted := make([]source.Diagnostic, len(diagnostics))
	for index, diagnostic := range diagnostics {
		converted[index] = source.Diagnostic{
			Code: source.DiagnosticCode(diagnostic.Code), Severity: source.DiagnosticSeverity(diagnostic.Severity), Message: diagnostic.Message,
			Primary: source.Span{
				Start: source.Position{File: diagnostic.Primary.Start.File, Offset: diagnostic.Primary.Start.Offset, Line: diagnostic.Primary.Start.Line, Column: diagnostic.Primary.Start.Column},
				End:   source.Position{File: diagnostic.Primary.End.File, Offset: diagnostic.Primary.End.Offset, Line: diagnostic.Primary.End.Line, Column: diagnostic.Primary.End.Column},
			},
		}
	}
	return Result{Text: formatted, Diagnostics: converted}
}
func Document(document parser.Document) Result { return core.Document(document) }
func Range(document parser.Document, selected source.Span) ([]TextEdit, []source.Diagnostic) {
	return core.Range(document, selected)
}

func OnType(document parser.Document, offset int, typed rune) ([]TextEdit, []source.Diagnostic) {
	return core.OnType(document, offset, typed)
}
