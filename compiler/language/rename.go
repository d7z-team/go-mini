package language

import (
	"errors"
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/scanner"
	"github.com/d7z-team/mini-go/compiler/token"
)

func (e *Engine) PrepareRename(uri DocumentURI, position Position) (*Range, error) {
	occurrence, ok := e.occurrence(uri, position)
	if !ok || occurrence.Key == "" || occurrence.Role == ast.NameImport {
		return nil, errors.New("symbol cannot be renamed")
	}
	document := e.snapshot.documents[uri]
	selected, err := document.Index.Range(occurrence.Name.Span.Start.Offset, occurrence.Name.Span.End.Offset)
	return &selected, err
}

func (e *Engine) Rename(uri DocumentURI, position Position, newName string) (WorkspaceEdit, error) {
	if !validIdentifier(newName) {
		return WorkspaceEdit{}, errors.New("invalid identifier")
	}
	occurrence, ok := e.occurrence(uri, position)
	if !ok || occurrence.Key == "" {
		return WorkspaceEdit{}, errors.New("symbol cannot be renamed")
	}
	for targetURI, occurrences := range e.snapshot.occurrences {
		for _, candidate := range occurrences {
			if candidate.Role == ast.NameDefinition && candidate.Name.Text == newName && candidate.Key != occurrence.Key && e.snapshot.documents[targetURI].Identity.ModulePath == e.snapshot.documents[uri].Identity.ModulePath {
				return WorkspaceEdit{}, errors.New("rename conflicts with an existing declaration")
			}
		}
	}
	if strings.HasPrefix(occurrence.Key, "export|") && newName[0] >= 'a' && newName[0] <= 'z' {
		owner := e.snapshot.documents[uri].Identity.ModulePath
		for _, location := range e.snapshot.references[occurrence.Key] {
			if e.snapshot.documents[location.URI].Identity.ModulePath != owner {
				return WorkspaceEdit{}, errors.New("exported symbol cannot become unexported while referenced by another package")
			}
		}
	}
	byURI := map[DocumentURI][]TextEdit{}
	for _, location := range e.snapshot.references[occurrence.Key] {
		byURI[location.URI] = append(byURI[location.URI], TextEdit{Range: location.Range, NewText: newName})
	}
	edit := WorkspaceEdit{}
	for target, edits := range byURI {
		document := e.snapshot.documents[target]
		edit.DocumentChanges = append(edit.DocumentChanges, TextDocumentEdit{TextDocument: VersionedTextDocumentIdentifier{URI: target, Version: document.Version}, Edits: edits})
	}
	sort.Slice(edit.DocumentChanges, func(i, j int) bool {
		return edit.DocumentChanges[i].TextDocument.URI < edit.DocumentChanges[j].TextDocument.URI
	})
	return edit, nil
}

func validIdentifier(name string) bool {
	result := scanner.Scan("rename.mgo", name)
	return len(result.Diagnostics) == 0 && len(result.Tokens) >= 2 && result.Tokens[0].Kind == token.Ident && result.Tokens[0].Lexeme == name && result.Tokens[1].Kind == token.Semicolon
}
