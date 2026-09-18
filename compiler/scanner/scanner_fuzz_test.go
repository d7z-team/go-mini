package scanner_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/scanner"
	"github.com/d7z-team/mini-go/compiler/token"
)

func FuzzScanPreservesSource(f *testing.F) {
	f.Add("package main\nfunc main() {}\n")
	f.Add("package p\n// comment\nvar value = `raw`\n")
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 1<<20 {
			t.Skip()
		}
		result := scanner.Scan("fuzz.mgo", source)
		again := scanner.Scan("fuzz.mgo", source)
		if !reflect.DeepEqual(result, again) {
			t.Fatal("scanner output is not deterministic")
		}
		var raw strings.Builder
		for _, element := range result.Elements {
			raw.WriteString(element.Lexeme)
		}
		if raw.String() != source {
			t.Fatalf("lossless scan changed source")
		}
		if len(result.Diagnostics) > scanner.DefaultMaxDiagnostics+1 {
			t.Fatalf("diagnostic budget exceeded: %d", len(result.Diagnostics))
		}
		previous := 0
		for _, element := range result.Elements {
			if !element.Span.Valid() || element.Span.Start.Offset != previous || element.Span.End.Offset < previous || element.Span.End.Offset > len(source) {
				t.Fatalf("invalid lossless element span: %#v", element.Span)
			}
			previous = element.Span.End.Offset
		}
		if previous != len(source) {
			t.Fatalf("elements end at %d, want %d", previous, len(source))
		}
		eof := 0
		for i, scanned := range result.Tokens {
			if !scanned.Span.Valid() || scanned.Span.Start.Offset < 0 || scanned.Span.End.Offset > len(source) {
				t.Fatalf("token %d has invalid span: %#v", i, scanned.Span)
			}
			if i != 0 && scanned.Span.Start.Offset < result.Tokens[i-1].Span.Start.Offset {
				t.Fatalf("tokens are not source ordered")
			}
			if scanned.Kind == token.EOF {
				eof++
			} else if scanned.Kind != token.Semicolon || scanned.Lexeme != "\n" {
				if scanned.Span.Start.Offset < scanned.Span.End.Offset && source[scanned.Span.Start.Offset:scanned.Span.End.Offset] != scanned.Lexeme {
					t.Fatalf("token lexeme does not match its source span: %#v", scanned)
				}
			}
		}
		if eof != 1 || len(result.Tokens) == 0 || result.Tokens[len(result.Tokens)-1].Kind != token.EOF {
			t.Fatalf("scanner emitted %d EOF tokens", eof)
		}
	})
}
