package format_test

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/format"
	"github.com/d7z-team/mini-go/compiler/parser"
	"github.com/d7z-team/mini-go/compiler/source"
)

func FuzzSourceIsStable(f *testing.F) {
	for _, source := range []string{
		"package main\nfunc main() {}\n",
		"package p\n// comment\nvar value = `a  b`\n",
		"package p\nfunc F(v int) int { if v > 0 { return -v }; return v }\n",
		"package A//",
		"package A func A(A){A{0}}",
	} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		first := format.Source("fuzz/module", "fuzz.mgo", source)
		if len(first.Diagnostics) != 0 {
			if first.Text != source {
				t.Fatal("invalid source was modified")
			}
			return
		}
		second := format.Source("fuzz/module", "fuzz.mgo", first.Text)
		if len(second.Diagnostics) != 0 || second.Text != first.Text {
			t.Fatalf("formatting is not stable: first=%q second=%q diagnostics=%v", first.Text, second.Text, second.Diagnostics)
		}
	})
}

func FuzzRangeIsStable(f *testing.F) {
	f.Add("package main\nfunc main(){println(1)}\n", 0, 39)
	f.Add("package p\nvar x=1\nvar y=2\n", 10, 18)
	f.Fuzz(func(t *testing.T, text string, start, end int) {
		if len(text) > 64<<10 || start < 0 || end < start || end > len(text) {
			return
		}
		document := parser.ParseDocument("fuzz/module", "fuzz.mgo", text)
		span, ok := source.NewFile("fuzz", "fuzz.mgo", text).Span(start, end)
		if !ok {
			return
		}
		edits, diagnostics := format.Range(document, span)
		if len(diagnostics) != 0 || len(edits) == 0 {
			return
		}
		candidate := text
		for i := len(edits) - 1; i >= 0; i-- {
			edit := edits[i]
			if !edit.Span.Valid() || edit.Span.Start.Offset < 0 || edit.Span.End.Offset > len(candidate) {
				t.Fatalf("invalid range edit: %#v", edit)
			}
			candidate = candidate[:edit.Span.Start.Offset] + edit.NewText + candidate[edit.Span.End.Offset:]
		}
		reparsed := parser.ParseDocument("fuzz/module", "fuzz.mgo", candidate)
		if source.HasErrors(reparsed.Diagnostics) {
			t.Fatalf("range edit produced invalid source: %#v", reparsed.Diagnostics)
		}
	})
}
