package service

import (
	"context"
	"errors"
	"strconv"

	"github.com/d7z-team/mini-go/compiler/language"
)

type Query struct {
	Snapshot           string
	Operation          string
	URI                language.DocumentURI
	Position           language.Position
	Range              *language.Range
	Positions          []language.Position
	Text               string
	IncludeDeclaration bool
	Completion         language.CompletionItem
	Action             language.CodeAction
	Diagnostics        []language.Diagnostic
}

// Query returns an owned language DTO, never compiler AST or mutable analysis
// maps. Snapshot references are checked before executing resolve operations.
func (s *Session) Query(ctx context.Context, query Query) (any, error) {
	ctx, finish, beginErr := s.beginRequest(ctx)
	if beginErr != nil {
		return nil, beginErr
	}
	defer finish()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	if query.Snapshot != strconv.FormatUint(s.snapshot, 10) {
		s.mu.Unlock()
		return nil, ErrStale
	}
	engine := s.published
	s.mu.Unlock()
	var result any
	var err error
	switch query.Operation {
	case "hover":
		result = engine.Hover(query.URI, query.Position)
	case "completion":
		result = engine.Completion(query.URI, query.Position)
	case "completion/resolve":
		result = engine.ResolveCompletion(query.Completion)
	case "signatureHelp":
		result = engine.SignatureHelp(query.URI, query.Position)
	case "definition":
		result = engine.Definition(query.URI, query.Position)
	case "typeDefinition":
		result = engine.TypeDefinition(query.URI, query.Position)
	case "implementation":
		result = engine.Implementation(query.URI, query.Position)
	case "references":
		result = engine.References(query.URI, query.Position, query.IncludeDeclaration)
	case "documentHighlight":
		result = engine.Highlights(query.URI, query.Position)
	case "documentSymbol":
		result = engine.DocumentSymbols(query.URI)
	case "workspaceSymbol":
		result = engine.WorkspaceSymbols(query.Text)
	case "prepareRename":
		result, err = engine.PrepareRename(query.URI, query.Position)
	case "rename":
		result, err = engine.Rename(query.URI, query.Position, query.Text)
	case "diagnostic":
		result = engine.Diagnostics(query.URI, query.Text)
	case "formatting":
		result, err = engine.Format(query.URI)
	case "rangeFormatting":
		if query.Range == nil {
			return nil, errors.New("missing formatting range")
		}
		result, err = engine.FormatRange(query.URI, *query.Range)
	case "onTypeFormatting":
		result, err = engine.OnTypeFormat(query.URI, query.Position, query.Text)
	case "semanticTokens":
		result = engine.SemanticTokens(query.URI, query.Range)
	case "foldingRange":
		result = engine.FoldingRanges(query.URI)
	case "selectionRange":
		result = engine.SelectionRanges(query.URI, query.Positions)
	case "codeAction":
		result = engine.CodeActions(query.URI, query.Diagnostics)
	case "codeAction/resolve":
		result = engine.ResolveCodeAction(query.Action)
	case "organizeImports":
		result, err = engine.OrganizeImports(query.URI)
	default:
		return nil, errors.New("unknown language operation")
	}
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
