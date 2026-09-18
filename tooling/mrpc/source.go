package mrpc

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// Source is one immutable MRPC input.
type Source struct {
	Path string
	Text string
	Hash string
}

// Position identifies one byte offset and display location in an MRPC file.
type Position struct {
	File   string
	Offset int
	Line   int
	Column int
}

// Span identifies a half-open source range.
type Span struct {
	Start Position
	End   Position
}

type (
	// DiagnosticCode is a stable machine-readable MRPC diagnostic identity.
	DiagnosticCode string
	// DiagnosticSeverity classifies an MRPC diagnostic.
	DiagnosticSeverity string
)

// SeverityError marks a diagnostic that prevents generation.
const SeverityError DiagnosticSeverity = "error"

// Diagnostic reports one parser or validator finding.
type Diagnostic struct {
	Code     DiagnosticCode
	Severity DiagnosticSeverity
	Message  string
	Primary  Span
}

// Span converts byte offsets in source to a source span.
func (source Source) Span(start, end int) (Span, bool) {
	if source.Path == "" || start < 0 || end < start || end > len(source.Text) {
		return Span{}, false
	}
	position := func(offset int) Position {
		line, column := 1, 0
		for index := 0; index < offset; index++ {
			if source.Text[index] == '\n' {
				line, column = line+1, 0
			} else {
				column++
			}
		}
		return Position{File: source.Path, Offset: offset, Line: line, Column: column}
	}
	return Span{Start: position(start), End: position(end)}, true
}

// HashText returns the canonical content hash of text.
func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// HasErrors reports whether diagnostics contain a generation-blocking error.
func HasErrors(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "" || diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

// NormalizeDiagnostics fills default severity, sorts findings, and removes duplicates.
func NormalizeDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	out := append([]Diagnostic(nil), diagnostics...)
	for index := range out {
		if out[index].Severity == "" {
			out[index].Severity = SeverityError
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.Primary.Start.File != right.Primary.Start.File {
			return left.Primary.Start.File < right.Primary.Start.File
		}
		if left.Primary.Start.Offset != right.Primary.Start.Offset {
			return left.Primary.Start.Offset < right.Primary.Start.Offset
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Message < right.Message
	})
	unique := out[:0]
	for _, diagnostic := range out {
		if len(unique) != 0 {
			previous := unique[len(unique)-1]
			if previous.Code == diagnostic.Code && previous.Primary.Start.File == diagnostic.Primary.Start.File && previous.Primary.Start.Offset == diagnostic.Primary.Start.Offset && previous.Primary.End.Offset == diagnostic.Primary.End.Offset {
				continue
			}
		}
		unique = append(unique, diagnostic)
	}
	return unique
}
