package engine

import (
	"errors"
	"fmt"
	"go/scanner"
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/gofrontend"
	"gopkg.d7z.net/go-mini/core/lowering"
	"gopkg.d7z.net/go-mini/core/lspserv"
	"gopkg.d7z.net/go-mini/core/runtime"
)

const (
	lspSourceSyntax   = "go-mini-syntax"
	lspSourceSemantic = "go-mini-semantic"
	lspSourceCompile  = "go-mini-compile"
	lspSourceLowering = "go-mini-lowering"
	lspSourceRuntime  = "go-mini-runtime"
)

func (e *MiniExecutor) AnalyzeSnapshot(snapshot lspserv.PackageSnapshot, options lspserv.AnalysisOptions) (lspserv.AnalysisResult, error) {
	combined, sources, diagnostics, fallbackURI, hasFrontEndErrors, err := e.parseLSPSnapshot(snapshot)
	if err != nil {
		appendLSPErrorDiagnostic(diagnostics, sources, fallbackURI, err, lspSourceSemantic, "package.merge")
		return lspserv.AnalysisResult{Diagnostics: normalizedLSPDiagnostics(diagnostics)}, nil
	}
	if combined == nil {
		return lspserv.AnalysisResult{Diagnostics: normalizedLSPDiagnostics(diagnostics)}, nil
	}

	compiled, _, analysisErr := e.newCompiler().AnalyzeProgramWithSources("lsp", "", combined, true, sources)
	appendLSPErrorDiagnostic(diagnostics, sources, fallbackURI, analysisErr, lspSourceSemantic, "semantic")

	if options.CompileParity && !hasFrontEndErrors && analysisErr == nil {
		compileProgram, compileSources, _, compileFallbackURI, _, parseErr := e.parseLSPSnapshot(snapshot)
		if parseErr != nil {
			appendLSPErrorDiagnostic(diagnostics, compileSources, compileFallbackURI, parseErr, lspSourceSemantic, "package.merge")
		} else if compileProgram != nil {
			_, _, compileErr := e.newCompiler().CompileProgramWithSources("lsp", "", compileProgram, true, compileSources)
			appendLSPErrorDiagnostic(diagnostics, compileSources, compileFallbackURI, compileErr, compileDiagnosticSource(compileErr), "compile")
		}
	}

	var program lspserv.ProgramView
	if compiled != nil {
		program = newAnalysisProgram("", compiled, compiled.Program)
	}
	return lspserv.AnalysisResult{
		Program:     program,
		Diagnostics: normalizedLSPDiagnostics(diagnostics),
	}, nil
}

func (e *MiniExecutor) parseLSPSnapshot(snapshot lspserv.PackageSnapshot) (*ast.ProgramStmt, map[string]string, map[string][]lspserv.Diagnostic, string, bool, error) {
	sources := make(map[string]string, len(snapshot.Files))
	diagnostics := make(map[string][]lspserv.Diagnostic)
	programs := make([]*ast.ProgramStmt, 0, len(snapshot.Files))
	fallbackURI := ""
	hasErrors := false

	for _, file := range snapshot.Files {
		if fallbackURI == "" {
			fallbackURI = file.URI
		}
		sources[file.URI] = file.Code
		node, errs := gofrontend.NewConverter().ConvertSourceTolerant(file.URI, file.Code)
		if len(errs) > 0 {
			hasErrors = true
		}
		for _, err := range errs {
			appendLSPErrorDiagnostic(diagnostics, sources, file.URI, err, lspSourceSyntax, "syntax")
		}
		if prog, ok := node.(*ast.ProgramStmt); ok && prog != nil {
			programs = append(programs, prog)
		}
	}

	if len(programs) == 0 {
		return nil, sources, diagnostics, fallbackURI, hasErrors, nil
	}
	combined, err := compiler.MergePrograms(programs)
	if err != nil {
		return nil, sources, diagnostics, fallbackURI, hasErrors, err
	}
	return combined, sources, diagnostics, fallbackURI, hasErrors, nil
}

func appendLSPErrorDiagnostic(diagnostics map[string][]lspserv.Diagnostic, codeByURI map[string]string, fallbackURI string, err error, source, code string) {
	if err == nil {
		return
	}

	var scanErr scanner.Error
	if errors.As(err, &scanErr) {
		uri := scanErr.Pos.Filename
		if uri == "" {
			uri = fallbackURI
		}
		diagnostics[uri] = append(diagnostics[uri], lspserv.Diagnostic{
			Range:    lspserv.RangeForScannerError(codeByURI[uri], scanErr),
			Severity: 1,
			Code:     "syntax.parse",
			Source:   lspSourceSyntax,
			Message:  scanErr.Msg,
		})
		return
	}

	var convertErr *gofrontend.ConvertError
	if errors.As(err, &convertErr) {
		uri := fallbackURI
		if convertErr.Pos != nil && convertErr.Pos.F != "" {
			uri = convertErr.Pos.F
		}
		diagnostics[uri] = append(diagnostics[uri], lspserv.Diagnostic{
			Range:    lspserv.RangeFromInternalPos(codeByURI[uri], convertErr.Pos),
			Severity: 1,
			Code:     "syntax.convert",
			Source:   lspSourceSyntax,
			Message:  convertErr.Message,
		})
		return
	}

	var astErr *ast.MiniAstError
	if errors.As(err, &astErr) {
		appendMiniAstDiagnostics(diagnostics, codeByURI, fallbackURI, astErr, source, code)
		return
	}

	var loweringErr *lowering.Error
	if errors.As(err, &loweringErr) {
		uri := loweringErr.File
		if uri == "" {
			uri = fallbackURI
		}
		diagnostics[uri] = append(diagnostics[uri], lspserv.Diagnostic{
			Range:    lspserv.RangeFromInternalPos(codeByURI[uri], &ast.Position{F: uri, L: loweringErr.Line, C: loweringErr.Col}),
			Severity: 1,
			Code:     "compile.lowering",
			Source:   lspSourceLowering,
			Message:  loweringErr.Error(),
		})
		return
	}

	var vmErr *runtime.VMError
	if errors.As(err, &vmErr) && len(vmErr.Frames) > 0 {
		frame := vmErr.Frames[0]
		uri := frame.Filename
		if uri == "" {
			uri = fallbackURI
		}
		diagnostics[uri] = append(diagnostics[uri], lspserv.Diagnostic{
			Range:    lspserv.RangeFromInternalPos(codeByURI[uri], &ast.Position{F: uri, L: frame.Line, C: frame.Column}),
			Severity: 1,
			Code:     "compile.runtime-validation",
			Source:   lspSourceRuntime,
			Message:  vmErr.Message,
		})
		return
	}

	if strings.TrimSpace(fallbackURI) == "" {
		return
	}
	diagnostics[fallbackURI] = append(diagnostics[fallbackURI], lspserv.Diagnostic{
		Range:    lspserv.RangeFromInternalPos(codeByURI[fallbackURI], &ast.Position{F: fallbackURI, L: 1, C: 1}),
		Severity: 1,
		Code:     code,
		Source:   source,
		Message:  err.Error(),
	})
}

func appendMiniAstDiagnostics(diagnostics map[string][]lspserv.Diagnostic, codeByURI map[string]string, fallbackURI string, astErr *ast.MiniAstError, source, code string) {
	if astErr == nil {
		return
	}
	if len(astErr.Logs) == 0 {
		appendLSPFallbackDiagnostic(diagnostics, codeByURI, fallbackURI, astErr.Error(), source, code)
		return
	}
	for _, log := range astErr.Logs {
		if skipTolerantSyntaxSemanticLog(log) {
			continue
		}
		uri := fallbackURI
		var loc *ast.Position
		if log.Node != nil && log.Node.GetBase() != nil {
			loc = log.Node.GetBase().Loc
		}
		if loc != nil && loc.F != "" {
			uri = loc.F
		}
		diagnostics[uri] = append(diagnostics[uri], lspserv.Diagnostic{
			Range:    lspserv.RangeFromInternalPos(codeByURI[uri], loc),
			Severity: 1,
			Code:     code,
			Source:   source,
			Message:  log.Message,
		})
	}
}

func appendLSPFallbackDiagnostic(diagnostics map[string][]lspserv.Diagnostic, codeByURI map[string]string, fallbackURI, message, source, code string) {
	if fallbackURI == "" {
		return
	}
	diagnostics[fallbackURI] = append(diagnostics[fallbackURI], lspserv.Diagnostic{
		Range:    lspserv.RangeFromInternalPos(codeByURI[fallbackURI], &ast.Position{F: fallbackURI, L: 1, C: 1}),
		Severity: 1,
		Code:     code,
		Source:   source,
		Message:  message,
	})
}

func skipTolerantSyntaxSemanticLog(log ast.Logs) bool {
	if !strings.HasPrefix(log.Message, "语法错误：") || log.Node == nil || log.Node.GetBase() == nil {
		return false
	}
	switch log.Node.(type) {
	case *ast.BadExpr, *ast.BadStmt:
		return true
	default:
		return log.Node.GetBase().Meta == "bad_expr" || log.Node.GetBase().Meta == "bad_stmt"
	}
}

func compileDiagnosticSource(err error) string {
	if err == nil {
		return lspSourceCompile
	}
	var loweringErr *lowering.Error
	if errors.As(err, &loweringErr) {
		return lspSourceLowering
	}
	var vmErr *runtime.VMError
	if errors.As(err, &vmErr) {
		return lspSourceRuntime
	}
	return lspSourceCompile
}

func normalizedLSPDiagnostics(in map[string][]lspserv.Diagnostic) map[string][]lspserv.Diagnostic {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]lspserv.Diagnostic, len(in))
	for uri, diags := range in {
		if len(diags) == 0 {
			continue
		}
		sort.SliceStable(diags, func(i, j int) bool {
			a, b := diags[i], diags[j]
			if a.Range.Start.Line != b.Range.Start.Line {
				return a.Range.Start.Line < b.Range.Start.Line
			}
			if a.Range.Start.Character != b.Range.Start.Character {
				return a.Range.Start.Character < b.Range.Start.Character
			}
			if a.Source != b.Source {
				return a.Source < b.Source
			}
			return a.Message < b.Message
		})
		current := make([]lspserv.Diagnostic, 0, len(diags))
		seen := make(map[string]struct{}, len(diags))
		for _, diag := range diags {
			key := diagnosticDedupeKey(diag)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			current = append(current, diag)
		}
		if len(current) > 0 {
			out[uri] = current
		}
	}
	return out
}

func diagnosticDedupeKey(diag lspserv.Diagnostic) string {
	var b strings.Builder
	b.WriteString(diag.Source)
	b.WriteByte('\x00')
	b.WriteString(diag.Code)
	b.WriteByte('\x00')
	b.WriteString(diag.Message)
	b.WriteByte('\x00')
	b.WriteString(fmt.Sprintf("%d:%d", diag.Range.Start.Line, diag.Range.Start.Character))
	b.WriteByte('\x00')
	b.WriteString(fmt.Sprintf("%d:%d", diag.Range.End.Line, diag.Range.End.Character))
	return b.String()
}
