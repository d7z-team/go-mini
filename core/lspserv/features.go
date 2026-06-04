package lspserv

import (
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"regexp"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
)

var missingImportPattern = regexp.MustCompile(`package\s+([A-Za-z_][A-Za-z0-9_]*)\s+resolved but not imported`)

func (s *LSPServer) GetCompletions(uri string, line, char int) []CompletionItem {
	_, combined := s.programForURI(uri)
	if combined == nil {
		return nil
	}
	items := combined.GetCompletionsAtFile(uri, line+1, char+1)
	res := make([]CompletionItem, 0, len(items))
	for _, it := range items {
		res = append(res, CompletionItem{
			Label:         it.Label,
			Kind:          MapKind(it.Kind),
			Detail:        string(it.Type),
			InsertText:    it.Label,
			Documentation: it.Doc,
		})
	}
	return res
}

func (s *LSPServer) GetHover(uri string, line, char int) *Hover {
	_, combined := s.programForURI(uri)
	if combined == nil {
		return nil
	}
	info := combined.GetHoverAtFile(uri, line+1, char+1)
	if info == nil {
		return nil
	}
	value := info.Markdown
	if value == "" {
		value = fmt.Sprintf("```go\n%s\n```\n%s", info.Signature, info.Doc)
	}
	return &Hover{Contents: MarkupContent{Kind: "markdown", Value: value}}
}

func (s *LSPServer) GetDefinition(uri string, line, char int) []Location {
	pkg, combined := s.programForURI(uri)
	if pkg == nil || combined == nil {
		return nil
	}
	def := combined.GetDefinitionAtFile(uri, line+1, char+1)
	if def == nil {
		return nil
	}
	defLoc := def.GetBase().Loc
	targetURI := uri
	if defLoc != nil && defLoc.F != "" {
		targetURI = defLoc.F
	}
	return []Location{{URI: targetURI, Range: rangeForPackagePosition(pkg, targetURI, defLoc)}}
}

func (s *LSPServer) GetReferences(uri string, line, char int, includeDeclaration bool) []Location {
	pkg, combined := s.programForURI(uri)
	if pkg == nil || combined == nil {
		return nil
	}
	refs := combined.GetReferencesAtFile(uri, line+1, char+1, includeDeclaration)
	res := make([]Location, 0, len(refs))
	for _, r := range refs {
		loc := r.GetBase().Loc
		if loc == nil {
			continue
		}
		targetURI := uri
		if loc.F != "" {
			targetURI = loc.F
		}
		res = append(res, Location{URI: targetURI, Range: rangeForPackagePosition(pkg, targetURI, loc)})
	}
	return res
}

func (s *LSPServer) GetSignatureHelp(uri string, line, char int) *SignatureHelp {
	_, combined := s.programForURI(uri)
	if combined == nil {
		return nil
	}
	provider, ok := combined.(signatureHelpProgramView)
	if !ok {
		return nil
	}
	return signatureHelpFromAnalysis(provider.GetSignatureHelpAtFile(uri, line+1, char+1))
}

func (s *LSPServer) GetDocumentSymbols(uri string) []DocumentSymbol {
	_, combined := s.programForURI(uri)
	if combined == nil {
		return nil
	}
	provider, ok := combined.(documentSymbolProgramView)
	if !ok {
		return nil
	}
	items := provider.GetDocumentSymbolsAtFile(uri)
	code := s.codeForURI(uri)
	res := make([]DocumentSymbol, 0, len(items))
	for _, item := range items {
		res = append(res, documentSymbolFromAnalysis(item, code))
	}
	return res
}

func (s *LSPServer) GetSemanticTokens(uri string) *SemanticTokens {
	code := s.codeForURI(uri)
	if code == "" {
		return &SemanticTokens{Data: []uint32{}}
	}
	return semanticTokensForCode(uri, code)
}

func (s *LSPServer) GetCodeActions(uri string, diagnostics []Diagnostic) []CodeAction {
	_, combined := s.programForURI(uri)
	resolver, _ := combined.(importPathResolverProgramView)
	if len(diagnostics) == 0 {
		if pkg := s.packageForURI(uri); pkg != nil {
			pkg.mu.RLock()
			diagnostics = append([]Diagnostic(nil), pkg.publishedDiagnostics[uri]...)
			pkg.mu.RUnlock()
		}
	}
	code := s.codeForURI(uri)
	actions := make([]CodeAction, 0)
	seen := make(map[string]struct{})
	for _, diag := range diagnostics {
		matches := missingImportPattern.FindStringSubmatch(diag.Message)
		if len(matches) < 2 {
			continue
		}
		path := matches[1]
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		if resolver != nil {
			if resolvedPath := resolver.ResolveImportPathForPackage(path); resolvedPath != "" {
				path = resolvedPath
			}
		}
		actions = append(actions, CodeAction{
			Title:       fmt.Sprintf("Import %q", path),
			Kind:        "quickfix",
			Diagnostics: []Diagnostic{diag},
			Edit: &WorkspaceEdit{Changes: map[string][]TextEdit{
				uri: {importTextEdit(code, path)},
			}},
		})
	}
	return actions
}

func (s *LSPServer) codeForURI(uri string) string {
	if uri == "" {
		return ""
	}
	if pkg := s.packageForURI(uri); pkg != nil {
		pkg.mu.RLock()
		if file := pkg.files[uri]; file != nil {
			code := file.code
			pkg.mu.RUnlock()
			return code
		}
		pkg.mu.RUnlock()
	}
	if local := localPathForURI(uri); local != "" {
		if data, err := os.ReadFile(local); err == nil {
			return string(data)
		}
	}
	return ""
}

func importTextEdit(code, importPath string) TextEdit {
	lines := strings.Split(code, "\n")
	insertLine := 0
	for idx, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "package ") {
			insertLine = idx + 1
			break
		}
	}
	return TextEdit{
		Range:   Range{Start: Position{Line: insertLine}, End: Position{Line: insertLine}},
		NewText: fmt.Sprintf("import %q\n", importPath),
	}
}

func signatureHelpFromAnalysis(info *ast.SignatureHelpInfo) *SignatureHelp {
	if info == nil || len(info.Signatures) == 0 {
		return nil
	}
	res := &SignatureHelp{
		ActiveSignature: info.ActiveSignature,
		ActiveParameter: info.ActiveParameter,
		Signatures:      make([]SignatureInformation, 0, len(info.Signatures)),
	}
	for _, sig := range info.Signatures {
		current := SignatureInformation{
			Label:      sig.Label,
			Parameters: make([]ParameterInformation, 0, len(sig.Parameters)),
		}
		if sig.Documentation != "" {
			current.Documentation = &MarkupContent{Kind: "markdown", Value: sig.Documentation}
		}
		for _, param := range sig.Parameters {
			current.Parameters = append(current.Parameters, ParameterInformation{Label: param.Label})
		}
		res.Signatures = append(res.Signatures, current)
	}
	return res
}

func documentSymbolFromAnalysis(item ast.DocumentSymbolInfo, code string) DocumentSymbol {
	symbol := DocumentSymbol{
		Name:           item.Name,
		Detail:         item.Detail,
		Kind:           MapKind(item.Kind),
		Range:          RangeFromInternalPos(code, item.Loc),
		SelectionRange: RangeFromInternalPos(code, item.SelectionLoc),
		Children:       make([]DocumentSymbol, 0, len(item.Children)),
	}
	for _, child := range item.Children {
		symbol.Children = append(symbol.Children, documentSymbolFromAnalysis(child, code))
	}
	return symbol
}

const (
	semanticTokenVariable = iota
	semanticTokenKeyword
	semanticTokenString
	semanticTokenNumber
	semanticTokenOperator
	semanticTokenComment
)

var semanticTokenTypes = []string{
	semanticTokenVariable: "variable",
	semanticTokenKeyword:  "keyword",
	semanticTokenString:   "string",
	semanticTokenNumber:   "number",
	semanticTokenOperator: "operator",
	semanticTokenComment:  "comment",
}

func semanticTokensForCode(uri, code string) *SemanticTokens {
	fset := token.NewFileSet()
	file := fset.AddFile(uri, -1, len(code))
	var scan scanner.Scanner
	scan.Init(file, []byte(code), nil, scanner.ScanComments)
	lines := strings.Split(code, "\n")

	data := make([]uint32, 0)
	prevLine, prevChar := 0, 0
	for {
		pos, tok, lit := scan.Scan()
		if tok == token.EOF {
			break
		}
		tokenType, ok := semanticTokenTypeForToken(tok)
		if !ok {
			continue
		}
		text := lit
		if text == "" {
			text = tok.String()
		}
		if text == "" {
			continue
		}
		position := fset.Position(pos)
		line := position.Line - 1
		if line < 0 {
			line = 0
		}
		lineText := ""
		if line < len(lines) {
			lineText = lines[line]
		}
		char := utf16CharacterForByteColumn(lineText, position.Column)
		length := utf16CharacterForByteColumn(text, len(text)+1)
		if length <= 0 {
			continue
		}
		lineDelta := line - prevLine
		charDelta := char
		if lineDelta == 0 {
			charDelta = char - prevChar
		}
		data = append(data, uint32(lineDelta), uint32(charDelta), uint32(length), uint32(tokenType), 0)
		prevLine, prevChar = line, char
	}
	return &SemanticTokens{Data: data}
}

func semanticTokenTypeForToken(tok token.Token) (int, bool) {
	switch {
	case tok.IsKeyword():
		return semanticTokenKeyword, true
	case tok.IsLiteral():
		if tok == token.STRING || tok == token.CHAR {
			return semanticTokenString, true
		}
		return semanticTokenNumber, true
	case tok == token.IDENT:
		return semanticTokenVariable, true
	case tok == token.COMMENT:
		return semanticTokenComment, true
	case tok.IsOperator():
		return semanticTokenOperator, true
	default:
		return 0, false
	}
}

func rangeForPackagePosition(pkg *packageState, uri string, pos *ast.Position) Range {
	if pkg == nil || pos == nil {
		return RangeFromInternalPos("", pos)
	}
	pkg.mu.RLock()
	file := pkg.files[uri]
	var code string
	if file != nil {
		code = file.code
	}
	rootPath := pkg.rootPath
	pkg.mu.RUnlock()
	if code == "" && rootPath != "" {
		name := localPathForURI(uri)
		if name != "" {
			if data, err := os.ReadFile(name); err == nil {
				code = string(data)
			}
		}
	}
	return RangeFromInternalPos(code, pos)
}
