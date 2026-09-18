package parser

import (
	"strings"
	"unicode"

	"github.com/d7z-team/mini-go/compiler/scanner"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/token"
)

const noSwitchDirective = "//minigo:noswitch"

func scanNoSwitchDirectives(scanned scanner.Result) (map[int]source.Span, []source.Diagnostic) {
	directives := map[int]source.Span{}
	var diagnostics []source.Diagnostic
	for index, element := range scanned.Elements {
		if element.Kind != scanner.ElementLineComment || !strings.HasPrefix(element.Lexeme, noSwitchDirective) {
			continue
		}
		if len(element.Lexeme) > len(noSwitchDirective) && !unicode.IsSpace(rune(element.Lexeme[len(noSwitchDirective)])) {
			continue
		}
		if strings.TrimSpace(element.Lexeme[len(noSwitchDirective):]) != "" {
			diagnostics = append(diagnostics, source.Diagnostic{
				Code: "parser.noswitch.directive", Severity: source.SeverityError,
				Message: "//minigo:noswitch does not accept arguments", Primary: element.Span,
			})
			continue
		}
		target := nextDirectiveToken(scanned.Elements, index+1)
		if target < 0 || scanned.Elements[target].Token != token.Func {
			diagnostics = append(diagnostics, source.Diagnostic{
				Code: "parser.noswitch.declaration", Severity: source.SeverityError,
				Message: "//minigo:noswitch must precede a named function or method declaration", Primary: element.Span,
			})
			continue
		}
		directives[scanned.Elements[target].Span.Start.Offset] = element.Span
	}
	return directives, diagnostics
}
