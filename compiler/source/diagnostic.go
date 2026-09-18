package source

import "sort"

const DiagnosticTruncated DiagnosticCode = "compiler.diagnostics.truncated"

// DiagnosticCollector applies one deterministic budget and normalization policy.
type DiagnosticCollector struct {
	limit     int
	values    []Diagnostic
	truncated bool
}

func NewDiagnosticCollector(limit int) *DiagnosticCollector {
	return &DiagnosticCollector{limit: limit}
}

func (c *DiagnosticCollector) Add(diagnostic Diagnostic) {
	if c == nil {
		return
	}
	if diagnostic.Severity == "" {
		diagnostic.Severity = SeverityError
	}
	if c.limit > 0 && len(c.values) >= c.limit {
		c.truncated = true
		return
	}
	c.values = append(c.values, diagnostic)
}

func (c *DiagnosticCollector) AddAll(diagnostics ...Diagnostic) {
	for _, diagnostic := range diagnostics {
		c.Add(diagnostic)
	}
}

func (c *DiagnosticCollector) Diagnostics() []Diagnostic {
	if c == nil {
		return nil
	}
	values := append([]Diagnostic(nil), c.values...)
	if c.truncated {
		values = append(values, Diagnostic{
			Code: DiagnosticTruncated, Severity: SeverityError,
			Message: "diagnostic limit reached; additional diagnostics were omitted",
		})
	}
	return NormalizeDiagnostics(values)
}

func HasErrors(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "" || diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

func NormalizeDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	values := append([]Diagnostic(nil), diagnostics...)
	for i := range values {
		if values[i].Severity == "" {
			values[i].Severity = SeverityError
		}
	}
	sort.SliceStable(values, func(i, j int) bool {
		left, right := values[i], values[j]
		if left.Code == DiagnosticTruncated || right.Code == DiagnosticTruncated {
			return left.Code != DiagnosticTruncated
		}
		if left.ModulePath != right.ModulePath {
			return left.ModulePath < right.ModulePath
		}
		if left.Primary.Start.File != right.Primary.Start.File {
			return left.Primary.Start.File < right.Primary.Start.File
		}
		if left.Primary.Start.Offset != right.Primary.Start.Offset {
			return left.Primary.Start.Offset < right.Primary.Start.Offset
		}
		if severityRank(left.Severity) != severityRank(right.Severity) {
			return severityRank(left.Severity) < severityRank(right.Severity)
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Message < right.Message
	})
	out := values[:0]
	for _, diagnostic := range values {
		if len(out) != 0 && sameDiagnosticIdentity(out[len(out)-1], diagnostic) {
			continue
		}
		out = append(out, diagnostic)
	}
	return out
}

func sameDiagnosticIdentity(left, right Diagnostic) bool {
	return left.Code == right.Code &&
		left.ModulePath == right.ModulePath &&
		left.Primary.Start.File == right.Primary.Start.File &&
		left.Primary.Start.Offset == right.Primary.Start.Offset &&
		left.Primary.End.File == right.Primary.End.File &&
		left.Primary.End.Offset == right.Primary.End.Offset
}

func severityRank(severity DiagnosticSeverity) int {
	switch severity {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInformation:
		return 2
	case SeverityHint:
		return 3
	default:
		return 4
	}
}
