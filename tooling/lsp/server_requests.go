package lsp

import (
	"context"

	"go.lsp.dev/jsonrpc2"
	protocol "go.lsp.dev/protocol"
)

func (s *ProtocolServer) Hover(_ context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	if params == nil {
		return nil, missingParams("hover")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	value := engine.Hover(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position))
	if value == nil {
		return nil, nil
	}
	result := &protocol.Hover{Contents: &protocol.MarkupContent{Kind: protocol.MarkupKind(value.Contents.Kind), Value: value.Contents.Value}}
	if value.Range != nil {
		selected := wireRange(*value.Range)
		result.Range = &selected
	}
	return result, nil
}

func (s *ProtocolServer) Definition(_ context.Context, params *protocol.DefinitionParams) (protocol.DefinitionResult, error) {
	if params == nil {
		return nil, missingParams("definition")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	return protocol.LocationSlice(wireLocations(engine.Definition(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position)))), nil
}

func (s *ProtocolServer) TypeDefinition(_ context.Context, params *protocol.TypeDefinitionParams) (protocol.DefinitionResult, error) {
	if params == nil {
		return nil, missingParams("typeDefinition")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	return protocol.LocationSlice(wireLocations(engine.TypeDefinition(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position)))), nil
}

func (s *ProtocolServer) Implementation(_ context.Context, params *protocol.ImplementationParams) (protocol.DefinitionResult, error) {
	if params == nil {
		return nil, missingParams("implementation")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	return protocol.LocationSlice(wireLocations(engine.Implementation(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position)))), nil
}

func (s *ProtocolServer) References(_ context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	if params == nil {
		return nil, missingParams("references")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	return wireLocations(engine.References(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position), params.Context.IncludeDeclaration)), nil
}

func (s *ProtocolServer) DocumentHighlight(_ context.Context, params *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	if params == nil {
		return nil, missingParams("documentHighlight")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	values := engine.Highlights(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position))
	result := make([]protocol.DocumentHighlight, len(values))
	for index, value := range values {
		result[index] = protocol.DocumentHighlight{Range: wireRange(value.Range), Kind: protocol.DocumentHighlightKind(value.Kind)}
	}
	return result, nil
}

func (s *ProtocolServer) Completion(_ context.Context, params *protocol.CompletionParams) (protocol.CompletionResult, error) {
	if params == nil {
		return nil, missingParams("completion")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	return wireCompletionList(engine.Completion(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position))), nil
}

func (s *ProtocolServer) CompletionResolve(_ context.Context, item *protocol.CompletionItem) (*protocol.CompletionItem, error) {
	if item == nil {
		return nil, missingParams("completion resolve")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	resolved := engine.ResolveCompletion(engineCompletionItem(item))
	return wireCompletionItem(resolved), nil
}

func (s *ProtocolServer) SignatureHelp(_ context.Context, params *protocol.SignatureHelpParams) (*protocol.SignatureHelp, error) {
	if params == nil {
		return nil, missingParams("signatureHelp")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	return wireSignatureHelp(engine.SignatureHelp(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position))), nil
}

func (s *ProtocolServer) DocumentSymbol(_ context.Context, params *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error) {
	if params == nil {
		return nil, missingParams("documentSymbol")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	values := engine.DocumentSymbols(DocumentURI(params.TextDocument.URI.String()))
	result := make(protocol.DocumentSymbolSlice, len(values))
	for index, value := range values {
		result[index] = wireDocumentSymbol(value)
	}
	return result, nil
}

func (s *ProtocolServer) Symbols(_ context.Context, params *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error) {
	if params == nil {
		return nil, missingParams("workspaceSymbol")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	values := engine.WorkspaceSymbols(params.Query)
	result := make(protocol.SymbolInformationSlice, len(values))
	for index, value := range values {
		var container *string
		if value.ContainerName != "" {
			container = &value.ContainerName
		}
		result[index] = protocol.SymbolInformation{BaseSymbolInformation: protocol.BaseSymbolInformation{Name: value.Name, Kind: protocol.SymbolKind(value.Kind), ContainerName: container}, Location: wireLocation(value.Location)}
	}
	return result, nil
}

func (s *ProtocolServer) Formatting(_ context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	if params == nil {
		return nil, missingParams("formatting")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	values, err := engine.Format(DocumentURI(params.TextDocument.URI.String()))
	return wireTextEdits(values), err
}

func (s *ProtocolServer) RangeFormatting(_ context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	if params == nil {
		return nil, missingParams("rangeFormatting")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	values, err := engine.FormatRange(DocumentURI(params.TextDocument.URI.String()), engineRange(params.Range))
	return wireTextEdits(values), err
}

func (s *ProtocolServer) OnTypeFormatting(_ context.Context, params *protocol.DocumentOnTypeFormattingParams) ([]protocol.TextEdit, error) {
	if params == nil {
		return nil, missingParams("onTypeFormatting")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	values, err := engine.OnTypeFormat(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position), params.Ch)
	return wireTextEdits(values), err
}

func (s *ProtocolServer) PrepareRename(_ context.Context, params *protocol.PrepareRenameParams) (protocol.PrepareRenameResult, error) {
	if params == nil {
		return nil, missingParams("prepareRename")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	value, err := engine.PrepareRename(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position))
	if value == nil || err != nil {
		return nil, err
	}
	selected := wireRange(*value)
	return &selected, nil
}

func (s *ProtocolServer) Rename(_ context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	if params == nil {
		return nil, missingParams("rename")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	value, err := engine.Rename(DocumentURI(params.TextDocument.URI.String()), enginePosition(params.Position), params.NewName)
	if err != nil {
		return nil, err
	}
	return wireWorkspaceEdit(value, s.capabilities), nil
}

func (s *ProtocolServer) Diagnostic(_ context.Context, params *protocol.DocumentDiagnosticParams) (protocol.DocumentDiagnosticReport, error) {
	if params == nil {
		return nil, missingParams("diagnostic")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	previous := ""
	if params.PreviousResultID != nil {
		previous = *params.PreviousResultID
	}
	report := engine.Diagnostics(DocumentURI(params.TextDocument.URI.String()), previous)
	if report.Kind == "unchanged" {
		return &protocol.RelatedUnchangedDocumentDiagnosticReport{UnchangedDocumentDiagnosticReport: protocol.UnchangedDocumentDiagnosticReport{Kind: "unchanged", ResultID: report.ResultID}}, nil
	}
	resultID := report.ResultID
	return &protocol.RelatedFullDocumentDiagnosticReport{FullDocumentDiagnosticReport: protocol.FullDocumentDiagnosticReport{Kind: "full", ResultID: &resultID, Items: wireDiagnostics(report.Items)}}, nil
}

func (s *ProtocolServer) SemanticTokensFull(_ context.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	if params == nil {
		return nil, missingParams("semanticTokens/full")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	value := engine.SemanticTokens(DocumentURI(params.TextDocument.URI.String()), nil)
	resultID := value.ResultID
	return &protocol.SemanticTokens{ResultID: &resultID, Data: value.Data}, nil
}

func (s *ProtocolServer) SemanticTokensRange(_ context.Context, params *protocol.SemanticTokensRangeParams) (*protocol.SemanticTokens, error) {
	if params == nil {
		return nil, missingParams("semanticTokens/range")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	selected := engineRange(params.Range)
	value := engine.SemanticTokens(DocumentURI(params.TextDocument.URI.String()), &selected)
	resultID := value.ResultID
	return &protocol.SemanticTokens{ResultID: &resultID, Data: value.Data}, nil
}

func (s *ProtocolServer) FoldingRanges(_ context.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	if params == nil {
		return nil, missingParams("foldingRange")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	values := engine.FoldingRanges(DocumentURI(params.TextDocument.URI.String()))
	result := make([]protocol.FoldingRange, len(values))
	for index, value := range values {
		start, end := uint32(value.StartCharacter), uint32(value.EndCharacter)
		result[index] = protocol.FoldingRange{StartLine: uint32(value.StartLine), StartCharacter: &start, EndLine: uint32(value.EndLine), EndCharacter: &end, Kind: protocol.FoldingRangeKind(value.Kind)}
	}
	return result, nil
}

func (s *ProtocolServer) SelectionRange(_ context.Context, params *protocol.SelectionRangeParams) ([]protocol.SelectionRange, error) {
	if params == nil {
		return nil, missingParams("selectionRange")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	positions := make([]Position, len(params.Positions))
	for index, value := range params.Positions {
		positions[index] = enginePosition(value)
	}
	values := engine.SelectionRanges(DocumentURI(params.TextDocument.URI.String()), positions)
	result := make([]protocol.SelectionRange, len(values))
	for index, value := range values {
		result[index] = wireSelectionRange(value)
	}
	return result, nil
}

func (s *ProtocolServer) CodeAction(_ context.Context, params *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	if params == nil {
		return nil, missingParams("codeAction")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	diagnostics := engineDiagnostics(params.Context.Diagnostics)
	selected := engineRange(params.Range)
	filtered := diagnostics[:0]
	for _, diagnostic := range diagnostics {
		if RangesOverlap(diagnostic.Range, selected) {
			filtered = append(filtered, diagnostic)
		}
	}
	diagnostics = filtered
	values := engine.CodeActions(DocumentURI(params.TextDocument.URI.String()), diagnostics)
	result := make([]protocol.CommandOrCodeAction, 0, len(values))
	for _, value := range values {
		if len(params.Context.Only) != 0 && !codeActionRequested(value.Kind, params.Context.Only) {
			continue
		}
		result = append(result, wireCodeAction(value, s.capabilities))
	}
	return result, nil
}

func (s *ProtocolServer) CodeActionResolve(_ context.Context, action *protocol.CodeAction) (*protocol.CodeAction, error) {
	if action == nil {
		return nil, missingParams("codeAction resolve")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	engine, err := s.engineForRequest()
	if err != nil {
		return nil, err
	}
	resolved := engine.ResolveCodeAction(engineCodeAction(action))
	return wireCodeAction(resolved, s.capabilities), nil
}

func (s *ProtocolServer) Request(context.Context, string, any) (any, error) {
	return nil, jsonrpc2.ErrNotHandled
}

func missingParams(method string) error {
	return jsonrpc2.NewError(jsonrpc2.InvalidParams, "missing "+method+" params")
}

var _ protocol.Server = (*ProtocolServer)(nil)
