package lsp

import (
	"encoding/json"
	"strings"

	protocol "go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func enginePosition(value protocol.Position) Position {
	return Position{Line: int(value.Line), Character: int(value.Character)}
}

func wirePosition(value Position) protocol.Position {
	return protocol.Position{Line: uint32(value.Line), Character: uint32(value.Character)}
}

func engineRange(value protocol.Range) Range {
	return Range{Start: enginePosition(value.Start), End: enginePosition(value.End)}
}

func wireRange(value Range) protocol.Range {
	return protocol.Range{Start: wirePosition(value.Start), End: wirePosition(value.End)}
}

func wireLocation(value Location) protocol.Location {
	return protocol.Location{URI: uri.URI(value.URI), Range: wireRange(value.Range)}
}

func wireLocations(values []Location) []protocol.Location {
	result := make([]protocol.Location, len(values))
	for index, value := range values {
		result[index] = wireLocation(value)
	}
	return result
}

func wireTextEdits(values []TextEdit) []protocol.TextEdit {
	result := make([]protocol.TextEdit, len(values))
	for index, value := range values {
		result[index] = protocol.TextEdit{Range: wireRange(value.Range), NewText: value.NewText}
	}
	return result
}

func wireDiagnostics(values []Diagnostic) []protocol.Diagnostic {
	result := make([]protocol.Diagnostic, len(values))
	for index, value := range values {
		result[index] = protocol.Diagnostic{Range: wireRange(value.Range), Severity: protocol.DiagnosticSeverity(value.Severity), Code: protocol.String(value.Code), Source: protocol.NewOptional(value.Source), Message: protocol.String(value.Message)}
		for _, related := range value.RelatedInformation {
			result[index].RelatedInformation = append(result[index].RelatedInformation, protocol.DiagnosticRelatedInformation{Location: protocol.Location{URI: uri.URI(related.Location.URI), Range: wireRange(related.Location.Range)}, Message: related.Message})
		}
	}
	return result
}

func engineDiagnostics(values []protocol.Diagnostic) []Diagnostic {
	result := make([]Diagnostic, len(values))
	for index, value := range values {
		sourceName, _ := value.Source.Get()
		message := ""
		switch content := value.Message.(type) {
		case protocol.String:
			message = string(content)
		case *protocol.MarkupContent:
			message = content.Value
		}
		result[index] = Diagnostic{Range: engineRange(value.Range), Severity: int(value.Severity), Source: sourceName, Message: message}
	}
	return result
}

func wireCompletionList(value CompletionList) *protocol.CompletionList {
	items := make([]protocol.CompletionItem, len(value.Items))
	for index, item := range value.Items {
		items[index] = *wireCompletionItem(item)
	}
	return &protocol.CompletionList{IsIncomplete: value.IsIncomplete, Items: items}
}

func wireCompletionItem(value CompletionItem) *protocol.CompletionItem {
	result := &protocol.CompletionItem{Label: value.Label, Kind: protocol.CompletionItemKind(value.Kind), Data: wireData(value.Data)}
	if value.Detail != "" {
		result.Detail = protocol.NewOptional(value.Detail)
	}
	if value.SortText != "" {
		result.SortText = protocol.NewOptional(value.SortText)
	}
	if value.Documentation.Value != "" {
		result.Documentation = &protocol.MarkupContent{Kind: protocol.MarkupKind(value.Documentation.Kind), Value: value.Documentation.Value}
	}
	return result
}

func engineCompletionItem(value *protocol.CompletionItem) CompletionItem {
	result := CompletionItem{Label: value.Label, Kind: int(value.Kind)}
	if detail, ok := value.Detail.Get(); ok {
		result.Detail = detail
	}
	if sortText, ok := value.SortText.Get(); ok {
		result.SortText = sortText
	}
	_ = json.Unmarshal(value.Data, &result.Data)
	return result
}

func wireSignatureHelp(value *SignatureHelp) *protocol.SignatureHelp {
	if value == nil {
		return nil
	}
	activeSignature := uint32(value.ActiveSignature)
	result := &protocol.SignatureHelp{ActiveSignature: &activeSignature, ActiveParameter: protocol.NewNullable(uint32(value.ActiveParameter)), Signatures: make([]protocol.SignatureInformation, len(value.Signatures))}
	for index, signature := range value.Signatures {
		parameters := make([]protocol.ParameterInformation, len(signature.Parameters))
		for i, parameter := range signature.Parameters {
			parameters[i] = protocol.ParameterInformation{Label: protocol.String(parameter.Label)}
		}
		result.Signatures[index] = protocol.SignatureInformation{Label: signature.Label, Documentation: &protocol.MarkupContent{Kind: protocol.MarkupKind(signature.Documentation.Kind), Value: signature.Documentation.Value}, Parameters: parameters}
	}
	return result
}

func wireDocumentSymbol(value DocumentSymbol) protocol.DocumentSymbol {
	result := protocol.DocumentSymbol{Name: value.Name, Kind: protocol.SymbolKind(value.Kind), Range: wireRange(value.Range), SelectionRange: wireRange(value.SelectionRange), Children: make([]protocol.DocumentSymbol, len(value.Children))}
	if value.Detail != "" {
		result.Detail = &value.Detail
	}
	for index, child := range value.Children {
		result.Children[index] = wireDocumentSymbol(child)
	}
	return result
}

func wireSelectionRange(value SelectionRange) protocol.SelectionRange {
	result := protocol.SelectionRange{Range: wireRange(value.Range)}
	if value.Parent != nil {
		parent := wireSelectionRange(*value.Parent)
		result.Parent = &parent
	}
	return result
}

func wireWorkspaceEdit(value WorkspaceEdit, capabilities protocol.ClientCapabilities) *protocol.WorkspaceEdit {
	result := &protocol.WorkspaceEdit{}
	documentChanges := capabilities.Workspace != nil && capabilities.Workspace.WorkspaceEdit != nil && capabilities.Workspace.WorkspaceEdit.DocumentChanges != nil && *capabilities.Workspace.WorkspaceEdit.DocumentChanges
	if !documentChanges {
		result.Changes = make(map[uri.URI][]protocol.TextEdit, len(value.DocumentChanges))
		for _, change := range value.DocumentChanges {
			result.Changes[uri.URI(change.TextDocument.URI)] = wireTextEdits(change.Edits)
		}
		return result
	}
	for _, change := range value.DocumentChanges {
		version := int32(change.TextDocument.Version)
		edits := wireTextEdits(change.Edits)
		elements := make([]protocol.TextDocumentEditElement, len(edits))
		for index := range edits {
			elements[index] = &edits[index]
		}
		result.DocumentChanges = append(result.DocumentChanges, &protocol.TextDocumentEdit{TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri.URI(change.TextDocument.URI)}, Version: &version}, Edits: elements})
	}
	return result
}

func wireCodeAction(value CodeAction, capabilities protocol.ClientCapabilities) *protocol.CodeAction {
	kind := protocol.CodeActionKind(value.Kind)
	result := &protocol.CodeAction{Title: value.Title, Kind: &kind, Diagnostics: wireDiagnostics(value.Diagnostics), Data: wireData(value.Data)}
	if value.Edit != nil {
		result.Edit = wireWorkspaceEdit(*value.Edit, capabilities)
	}
	return result
}

func engineCodeAction(value *protocol.CodeAction) CodeAction {
	result := CodeAction{Title: value.Title}
	if value.Kind != nil {
		result.Kind = string(*value.Kind)
	}
	_ = json.Unmarshal(value.Data, &result.Data)
	return result
}

func wireData(value string) protocol.LSPAny {
	if value == "" {
		return nil
	}
	encoded, _ := json.Marshal(value)
	return protocol.LSPAny(encoded)
}

func codeActionRequested(kind string, requested []protocol.CodeActionKind) bool {
	for _, candidate := range requested {
		if kind == string(candidate) || strings.HasPrefix(kind, string(candidate)+".") {
			return true
		}
	}
	return false
}
