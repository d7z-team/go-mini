package source

import "testing"

func TestDiagnosticCollectorBudgetsSortsAndDeduplicates(t *testing.T) {
	file := NewFile("file.0", "main.mgo", "a\nb\n")
	first, _ := file.Span(2, 3)
	second, _ := file.Span(0, 1)
	collector := NewDiagnosticCollector(2)
	collector.Add(Diagnostic{Code: "semantic.name", Severity: SeverityError, Message: "name", Primary: first})
	collector.Add(Diagnostic{Code: "scanner.byte", Severity: SeverityWarning, Message: "byte", Primary: second})
	collector.Add(Diagnostic{Code: "semantic.name", Severity: SeverityError, Message: "duplicate", Primary: first})
	diagnostics := collector.Diagnostics()
	if len(diagnostics) != 3 || diagnostics[0].Code != "scanner.byte" || diagnostics[2].Code != DiagnosticTruncated {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	if !HasErrors(diagnostics) {
		t.Fatal("truncated diagnostics must block compilation")
	}
}

func TestNormalizeDiagnosticsUsesCodeAndSpanIdentity(t *testing.T) {
	file := NewFile("file.0", "main.mgo", "value")
	span, _ := file.Span(0, 5)
	diagnostics := NormalizeDiagnostics([]Diagnostic{
		{Code: "parser.name", Severity: SeverityError, Message: "second", Primary: span},
		{Code: "parser.name", Severity: SeverityError, Message: "first", Primary: span},
	})
	if len(diagnostics) != 1 || diagnostics[0].Message != "first" {
		t.Fatalf("unexpected normalized diagnostics: %#v", diagnostics)
	}
}
